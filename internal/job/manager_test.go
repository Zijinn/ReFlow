package job

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/event"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func TestManagerCancelsRunningJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := NewManager(db, event.NewHub(), slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	started := make(chan struct{})
	stopped := make(chan struct{})
	manager.Register("test.blocking", func(ctx context.Context, _ domain.Job, _ ProgressFunc) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	queued, err := manager.Enqueue(ctx, "test.blocking", map[string]string{"test": "cancel"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not start")
	}
	cancelled, err := manager.Cancel(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != "cancelled" {
		t.Fatalf("unexpected cancelled state: %+v", cancelled)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("running handler did not receive cancellation")
	}
	deadline := time.Now().Add(time.Second)
	for {
		var result string
		err := db.QueryRowContext(ctx, `SELECT COALESCE(result, '') FROM job_attempts
			WHERE job_id = ? ORDER BY attempt DESC LIMIT 1`, queued.ID).Scan(&result)
		if err == nil && result == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled attempt not recorded: result=%q err=%v", result, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagerCancelledClaimedJobDoesNotEnterHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	manager := NewManager(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	var called atomic.Int32
	manager.Register("test.queued-cancel", func(context.Context, domain.Job, ProgressFunc) error {
		called.Add(1)
		return nil
	})
	queued, err := manager.Enqueue(ctx, "test.queued-cancel", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := storage.ClaimNextJob(ctx, db)
	if err != nil || claimed == nil || claimed.ID != queued.ID {
		t.Fatalf("claim job: %+v, %v", claimed, err)
	}
	manager.queue <- *claimed
	if _, err := manager.Cancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	manager.signalWorker()
	time.Sleep(100 * time.Millisecond)
	if got := called.Load(); got != 0 {
		t.Fatalf("cancelled claimed job entered handler %d times", got)
	}
}

func TestManagerMaintenanceRequeuesClaimedAndPreventsStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	manager := NewManager(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	var called atomic.Int32
	manager.Register("test.maintenance", func(context.Context, domain.Job, ProgressFunc) error {
		called.Add(1)
		return nil
	})
	queued, err := manager.Enqueue(ctx, "test.maintenance", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := storage.ClaimNextJob(ctx, db)
	if err != nil || claimed == nil || claimed.ID != queued.ID {
		t.Fatalf("claim job: %+v, %v", claimed, err)
	}
	manager.queue <- *claimed
	maintenanceCtx, maintenanceCancel := context.WithTimeout(ctx, time.Second)
	defer maintenanceCancel()
	if err := manager.EnterMaintenance(maintenanceCtx, ""); err != nil {
		t.Fatal(err)
	}
	stored, err := storage.GetJob(ctx, db, queued.ID)
	if err != nil || stored.State != "queued" {
		t.Fatalf("claimed job was not requeued: %+v, %v", stored, err)
	}
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	manager.signalWorker()
	time.Sleep(100 * time.Millisecond)
	if got := called.Load(); got != 0 {
		t.Fatalf("handler started before maintenance exit: %d", got)
	}
	manager.ExitMaintenance()
	eventually(t, time.Second, func() bool { return called.Load() == 1 }, "job did not start after maintenance exit")
}

func TestManagerEnterMaintenanceCancelsAndWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	manager := NewManager(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	started := make(chan struct{})
	release := make(chan struct{})
	manager.Register("test.maintenance-cancel", func(ctx context.Context, _ domain.Job, _ ProgressFunc) error {
		close(started)
		<-ctx.Done()
		<-release
		return ctx.Err()
	})
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Enqueue(ctx, "test.maintenance-cancel", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	shortCtx, shortCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer shortCancel()
	if err := manager.EnterMaintenance(shortCtx, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline while waiting for handler, got %v", err)
	}
	close(release)
	waitCtx, waitCancel := context.WithTimeout(ctx, time.Second)
	defer waitCancel()
	if err := manager.EnterMaintenance(waitCtx, ""); err != nil {
		t.Fatal(err)
	}
}

func TestManagerRejectsEnqueueDuringMaintenance(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager := NewManager(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	if err := manager.EnterMaintenance(ctx, ""); err != nil {
		t.Fatal(err)
	}
	defer manager.ExitMaintenance()
	if _, err := manager.Enqueue(ctx, "test", nil); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("enqueue during maintenance: %v", err)
	}
}

func TestManagerMaintenanceIsExclusive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	manager := NewManager(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	firstCtx, firstCancel := context.WithTimeout(ctx, time.Second)
	defer firstCancel()
	if err := manager.EnterMaintenance(firstCtx, ""); err != nil {
		t.Fatal(err)
	}
	secondCtx, secondCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer secondCancel()
	if err := manager.EnterMaintenance(secondCtx, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent maintenance entered unexpectedly: %v", err)
	}
	if !manager.InMaintenance() {
		t.Fatal("timed-out maintenance request resumed dispatch")
	}
	manager.ExitMaintenance()
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
