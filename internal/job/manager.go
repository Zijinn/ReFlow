package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/event"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type ProgressFunc func(current, total int)
type Handler func(ctx context.Context, job domain.Job, progress ProgressFunc) error

// ErrJobAlreadyQueued reports that an equivalent pending job exists; callers
// should treat it as a benign duplicate, not a failure.
var (
	ErrJobAlreadyQueued = storage.ErrJobAlreadyQueued
	ErrMaintenance      = errors.New("job manager is in maintenance mode")
)

type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Manager struct {
	db                 *sql.DB
	hub                *event.Hub
	logger             *slog.Logger
	workers            int
	handlers           map[string]Handler
	queue              chan domain.Job
	work               chan struct{}
	ready              chan struct{}
	maintenanceChanged chan struct{}
	start              sync.Once
	maintenanceMu      sync.Mutex
	admissionMu        sync.Mutex
	executions         map[string]execution
	maintenance        atomic.Bool
}

func NewManager(db *sql.DB, hub *event.Hub, logger *slog.Logger, workers int) *Manager {
	if workers < 1 {
		workers = 4
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		db: db, hub: hub, logger: logger, workers: workers,
		handlers: make(map[string]Handler), queue: make(chan domain.Job, workers),
		work: make(chan struct{}, workers), ready: make(chan struct{}, 1),
		maintenanceChanged: make(chan struct{}, 1), executions: make(map[string]execution),
	}
}

// EnterMaintenance prevents new handlers from starting, returns claimed jobs
// to the queue, cancels all running handlers except exceptJobID, and waits for
// the cancelled handlers to exit. The caller controls the wait deadline.
func (m *Manager) EnterMaintenance(ctx context.Context, exceptJobID string) error {
	if err := m.lockMaintenance(ctx); err != nil {
		return err
	}
	m.admissionMu.Lock()
	m.maintenance.Store(true)
	var waiting []<-chan struct{}
	for id, running := range m.executions {
		if id == exceptJobID {
			continue
		}
		running.cancel()
		waiting = append(waiting, running.done)
	}
	m.admissionMu.Unlock()
	m.signalMaintenanceChanged()

	m.drainClaimedJobs()
	for _, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			m.ExitMaintenance()
			return ctx.Err()
		}
	}
	return nil
}

// ExitMaintenance resumes dispatch and schedulers after a restore.
func (m *Manager) ExitMaintenance() {
	m.admissionMu.Lock()
	m.maintenance.Store(false)
	m.admissionMu.Unlock()
	m.signalMaintenanceChanged()
	m.signalDispatcher()
	m.maintenanceMu.Unlock()
}

func (m *Manager) lockMaintenance(ctx context.Context) error {
	for !m.maintenanceMu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return nil
}

// InMaintenance reports whether the manager is paused for a restore.
func (m *Manager) InMaintenance() bool {
	return m.maintenance.Load()
}

func (m *Manager) Cancel(ctx context.Context, jobID string) (domain.Job, error) {
	m.admissionMu.Lock()
	cancelled, err := storage.CancelJob(ctx, m.db, jobID)
	if err != nil {
		m.admissionMu.Unlock()
		return domain.Job{}, err
	}
	running, exists := m.executions[jobID]
	if exists {
		running.cancel()
	}
	m.admissionMu.Unlock()
	m.publish("job.cancelled", cancelled)
	return cancelled, nil
}

func (m *Manager) Register(kind string, handler Handler) {
	m.handlers[kind] = handler
}

func (m *Manager) Start(ctx context.Context) error {
	if err := storage.RecoverRunningJobs(ctx, m.db); err != nil {
		return err
	}
	m.start.Do(func() {
		for range m.workers {
			go m.worker(ctx)
		}
		go m.dispatch(ctx)
	})
	return nil
}

func (m *Manager) Enqueue(ctx context.Context, kind string, payload any) (domain.Job, error) {
	job, err := m.enqueueAdmitted(func() (domain.Job, error) {
		return storage.CreateJob(ctx, m.db, kind, payload, time.Now().UTC())
	})
	if err == nil {
		m.publish("job.queued", job)
	}
	return job, err
}

func (m *Manager) enqueueAdmitted(enqueue func() (domain.Job, error)) (domain.Job, error) {
	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	if m.maintenance.Load() {
		return domain.Job{}, ErrMaintenance
	}
	return enqueue()
}

func (m *Manager) EnqueueFeedRefresh(ctx context.Context, feedID string) (domain.Job, error) {
	queued, err := m.enqueueAdmitted(func() (domain.Job, error) {
		return storage.EnqueueFeedRefresh(ctx, m.db, feedID, time.Now().UTC())
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("feed refresh: %w", err)
	}
	m.publish("job.queued", queued)
	return queued, nil
}

func (m *Manager) EnqueueAccountSync(ctx context.Context, accountID string, requestedMode ...string) (domain.Job, error) {
	mode := "auto"
	if len(requestedMode) > 0 && requestedMode[0] != "" {
		mode = requestedMode[0]
	}
	queued, err := m.enqueueAdmitted(func() (domain.Job, error) {
		return storage.EnqueueAccountSync(ctx, m.db, accountID, mode, time.Now().UTC())
	})
	if err != nil {
		return domain.Job{}, fmt.Errorf("account sync: %w", err)
	}
	m.publish("job.queued", queued)
	return queued, nil
}

func (m *Manager) StartFeedScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		m.scheduleDueFeeds(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.scheduleDueFeeds(ctx)
			}
		}
	}()
}

func (m *Manager) StartSyncScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		m.scheduleDueSyncAccounts(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.scheduleDueSyncAccounts(ctx)
			}
		}
	}()
}

// StartCleanupScheduler prunes bookkeeping tables once a day.
func (m *Manager) StartCleanupScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.runCleanup(ctx)
			}
		}
	}()
}

func (m *Manager) runCleanup(ctx context.Context) {
	m.admissionMu.Lock()
	defer m.admissionMu.Unlock()
	if m.maintenance.Load() {
		return
	}
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if pruned, err := storage.PruneProcessedMutations(ctx, m.db, cutoff); err != nil {
		m.logger.WarnContext(ctx, "prune processed mutations", "error", err)
	} else if pruned > 0 {
		m.logger.InfoContext(ctx, "pruned processed mutations", "rows", pruned)
	}
	retentionDays, err := storage.RetentionDays(ctx, m.db, domain.DefaultProfileID)
	if err != nil {
		m.logger.WarnContext(ctx, "read retention preference", "error", err)
		return
	}
	if retentionDays <= 0 {
		return
	}
	entryCutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	if pruned, err := storage.PruneReadEntries(ctx, m.db, domain.DefaultProfileID, entryCutoff); err != nil {
		m.logger.WarnContext(ctx, "prune read entries", "error", err)
	} else if pruned > 0 {
		m.logger.InfoContext(ctx, "pruned read entries", "rows", pruned, "retention_days", retentionDays)
	}
}

func DecodePayload(job domain.Job, target any) error {
	if err := json.Unmarshal([]byte(job.PayloadJSON), target); err != nil {
		return fmt.Errorf("decode %s job payload: %w", job.Kind, err)
	}
	return nil
}

func (m *Manager) dispatch(ctx context.Context) {
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.claimAvailable(ctx)
		case <-m.ready:
			m.claimAvailable(ctx)
		}
	}
}

// claimAvailable drains the pending queue until the worker channel is full,
// instead of claiming at most one job per tick.
func (m *Manager) claimAvailable(ctx context.Context) {
	for {
		m.admissionMu.Lock()
		if m.maintenance.Load() {
			m.admissionMu.Unlock()
			return
		}
		job, err := storage.ClaimNextJob(ctx, m.db)
		if err != nil {
			m.admissionMu.Unlock()
			if !errors.Is(err, context.Canceled) {
				m.logger.ErrorContext(ctx, "claim background job", "error", err)
			}
			return
		}
		if job == nil {
			m.admissionMu.Unlock()
			return
		}
		select {
		case m.queue <- *job:
			m.signalWorker()
			m.admissionMu.Unlock()
		case <-ctx.Done():
			m.requeueClaimed(job.ID, "shutdown")
			m.admissionMu.Unlock()
			return
		default:
			m.admissionMu.Unlock()
			m.requeueClaimed(job.ID, "full queue")
			return
		}
	}
}

func (m *Manager) signalDispatcher() {
	select {
	case m.ready <- struct{}{}:
	default:
	}
}

func (m *Manager) signalWorker() {
	select {
	case m.work <- struct{}{}:
	default:
	}
}

func (m *Manager) signalMaintenanceChanged() {
	select {
	case m.maintenanceChanged <- struct{}{}:
	default:
	}
}

func (m *Manager) drainClaimedJobs() {
	for {
		select {
		case claimed := <-m.queue:
			select {
			case <-m.work:
			default:
			}
			m.requeueClaimed(claimed.ID, "maintenance")
		default:
			return
		}
	}
}

func (m *Manager) requeueClaimed(jobID, reason string) {
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := storage.RequeueJob(writeCtx, m.db, jobID); err != nil {
		m.logger.WarnContext(writeCtx, "requeue claimed job", "job_id", jobID, "reason", reason, "error", err)
	}
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.maintenanceChanged:
			continue
		case <-m.work:
			if m.maintenance.Load() {
				continue
			}
			select {
			case current := <-m.queue:
				m.execute(ctx, current)
			default:
			}
		}
	}
}

func (m *Manager) execute(ctx context.Context, current domain.Job) {
	jobCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.admissionMu.Lock()
	if m.maintenance.Load() {
		m.admissionMu.Unlock()
		cancel()
		m.requeueClaimed(current.ID, "maintenance")
		return
	}
	stored, err := storage.GetJob(jobCtx, m.db, current.ID)
	if err != nil || stored.State != "running" {
		m.admissionMu.Unlock()
		cancel()
		if err != nil && !errors.Is(err, storage.ErrNotFound) && !errors.Is(err, context.Canceled) {
			m.logger.WarnContext(ctx, "verify claimed job", "job_id", current.ID, "error", err)
		}
		return
	}
	m.executions[current.ID] = execution{cancel: cancel, done: done}
	m.admissionMu.Unlock()
	defer func() {
		cancel()
		m.admissionMu.Lock()
		delete(m.executions, current.ID)
		close(done)
		m.admissionMu.Unlock()
	}()
	current = stored
	m.publish("job.started", current)
	attemptID, attemptErr := storage.BeginJobAttempt(jobCtx, m.db, current.ID)
	if attemptErr != nil {
		m.logger.WarnContext(ctx, "record job attempt", "job_id", current.ID, "error", attemptErr)
	}
	handler, exists := m.handlers[current.Kind]
	if !exists {
		m.fail(ctx, current, attemptID, "unsupported_job", "No handler is registered for this job kind")
		return
	}
	progress := func(completed, total int) {
		if err := storage.UpdateJobProgress(jobCtx, m.db, current.ID, completed, total); err != nil {
			m.logger.WarnContext(ctx, "update job progress", "job_id", current.ID, "error", err)
			return
		}
		m.publish("job.progress", map[string]any{"id": current.ID, "current": completed, "total": total})
	}
	if err := handler(jobCtx, current, progress); err != nil {
		if stored, getErr := storage.GetJob(context.Background(), m.db, current.ID); getErr == nil && stored.State == "cancelled" {
			if attemptID != "" {
				_ = storage.FinishJobAttempt(context.Background(), m.db, attemptID, "cancelled", "cancelled", "Cancelled by user")
			}
			return
		}
		code := jobErrorCode(err)
		m.fail(jobCtx, current, attemptID, code, err.Error())
		return
	}
	if attemptID != "" {
		_ = storage.FinishJobAttempt(ctx, m.db, attemptID, "succeeded", "", "")
	}
	if err := storage.CompleteJob(ctx, m.db, current.ID); err != nil {
		m.logger.ErrorContext(ctx, "complete job", "job_id", current.ID, "error", err)
		return
	}
	if completed, err := storage.GetJob(ctx, m.db, current.ID); err == nil {
		m.publish("job.succeeded", completed)
	}
}

func (m *Manager) fail(_ context.Context, current domain.Job, attemptID, code, message string) {
	// The handler's context may already be cancelled (shutdown, user cancel);
	// record the failure with a fresh context so the job is not left running.
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if attemptID != "" {
		_ = storage.FinishJobAttempt(writeCtx, m.db, attemptID, "failed", code, message)
	}
	if err := storage.FailJob(writeCtx, m.db, current.ID, code, message); err != nil {
		m.logger.ErrorContext(writeCtx, "fail job", "job_id", current.ID, "error", err)
		return
	}
	if failed, err := storage.GetJob(writeCtx, m.db, current.ID); err == nil {
		m.publish("job.failed", failed)
	}
}

func (m *Manager) scheduleDueFeeds(ctx context.Context) {
	if m.maintenance.Load() {
		return
	}
	feeds, err := storage.ListDueFeeds(ctx, m.db, 100)
	if err != nil {
		m.logger.ErrorContext(ctx, "list due feeds", "error", err)
		return
	}
	for _, stored := range feeds {
		if _, err := m.EnqueueFeedRefresh(ctx, stored.ID); err != nil && !errors.Is(err, ErrJobAlreadyQueued) {
			m.logger.WarnContext(ctx, "schedule feed refresh", "feed_id", stored.ID, "error", err)
		}
	}
}

func (m *Manager) scheduleDueSyncAccounts(ctx context.Context) {
	if m.maintenance.Load() {
		return
	}
	accounts, err := storage.ListDueSyncAccounts(ctx, m.db, 100)
	if err != nil {
		m.logger.ErrorContext(ctx, "list due sync accounts", "error", err)
		return
	}
	for _, account := range accounts {
		if _, err := m.EnqueueAccountSync(ctx, account.ID); err != nil && !errors.Is(err, ErrJobAlreadyQueued) {
			m.logger.WarnContext(ctx, "schedule account sync", "account_id", account.ID, "error", err)
		}
	}
}

func jobErrorCode(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	var fetchError *feedcore.FetchError
	if errors.As(err, &fetchError) {
		return fetchError.Code
	}
	return "job_failed"
}

func (m *Manager) publish(eventType string, value any) {
	if m.hub != nil {
		m.hub.Publish(eventType, value)
	}
}
