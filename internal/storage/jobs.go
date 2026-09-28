package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrJobNotCancellable = errors.New("job is not cancellable")
	ErrJobAlreadyQueued  = errors.New("job is already queued")
)

const jobColumns = `
	id, kind, state, payload_json, progress_current, progress_total,
	scheduled_at, started_at, finished_at, error_code, error_message,
	created_at, updated_at`

func CreateJob(ctx context.Context, db *sql.DB, kind string, payload any, scheduledAt time.Time) (domain.Job, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job payload: %w", err)
	}
	jobID := uuid.NewString()
	now := time.Now().UTC()
	_, err = db.ExecContext(ctx, `
		INSERT INTO jobs (
			id, kind, state, payload_json, scheduled_at, created_at, updated_at
		) VALUES (?, ?, 'queued', ?, ?, ?, ?)`,
		jobID, kind, string(body), formatTime(scheduledAt), formatTime(now), formatTime(now),
	)
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return GetJob(ctx, db, jobID)
}

func EnqueueFeedRefresh(ctx context.Context, db *sql.DB, feedID string, scheduledAt time.Time) (domain.Job, error) {
	return enqueueUniqueJob(ctx, db, "feed.refresh", map[string]string{"feed_id": feedID},
		"json_extract(payload_json, '$.feed_id') = ?", feedID, scheduledAt)
}

func EnqueueAccountSync(ctx context.Context, db *sql.DB, accountID, mode string, scheduledAt time.Time) (domain.Job, error) {
	return enqueueUniqueJob(ctx, db, "sync.account", map[string]string{"account_id": accountID, "mode": mode},
		"json_extract(payload_json, '$.account_id') = ?", accountID, scheduledAt)
}

func enqueueUniqueJob(ctx context.Context, db *sql.DB, kind string, payload any, equivalentSQL, equivalentValue string, scheduledAt time.Time) (domain.Job, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job payload: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, fmt.Errorf("begin enqueue job: %w", err)
	}
	defer tx.Rollback()

	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM jobs WHERE kind = ? AND state IN ('queued', 'running') AND ` + equivalentSQL + `)`
	if err := tx.QueryRowContext(ctx, query, kind, equivalentValue).Scan(&exists); err != nil {
		return domain.Job{}, fmt.Errorf("check equivalent job: %w", err)
	}
	if exists {
		return domain.Job{}, ErrJobAlreadyQueued
	}
	now := time.Now().UTC()
	jobID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs (
		id, kind, state, payload_json, scheduled_at, created_at, updated_at
	) VALUES (?, ?, 'queued', ?, ?, ?, ?)`, jobID, kind, string(body), formatTime(scheduledAt), formatTime(now), formatTime(now)); err != nil {
		return domain.Job{}, fmt.Errorf("enqueue job: %w", err)
	}
	job, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", jobID))
	if err != nil {
		return domain.Job{}, fmt.Errorf("read enqueued job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Job{}, fmt.Errorf("commit enqueued job: %w", err)
	}
	return job, nil
}

func GetJob(ctx context.Context, db *sql.DB, jobID string) (domain.Job, error) {
	job, err := scanJob(db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, ErrNotFound
	}
	return job, err
}

func ClaimNextJob(ctx context.Context, db *sql.DB) (*domain.Job, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim job: %w", err)
	}
	defer tx.Rollback()

	job, err := scanJob(tx.QueryRowContext(ctx, `
		SELECT `+jobColumns+` FROM jobs
		WHERE state = 'queued' AND scheduled_at <= ?
		ORDER BY scheduled_at, created_at, id LIMIT 1`, formatTime(time.Now().UTC())))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs SET state = 'running', started_at = ?, finished_at = NULL,
			error_code = NULL, error_message = NULL, updated_at = ?
		WHERE id = ? AND state = 'queued'`, formatTime(now), formatTime(now), job.ID)
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil, nil
	}
	job.State = "running"
	job.StartedAt = &now
	job.UpdatedAt = now
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claimed job: %w", err)
	}
	return &job, nil
}

func RecoverRunningJobs(ctx context.Context, db *sql.DB) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		UPDATE jobs SET state = 'queued', started_at = NULL,
			error_code = 'interrupted', error_message = 'Recovered after process restart',
			scheduled_at = ?, updated_at = ? WHERE state = 'running'`, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("recover running jobs: %w", err)
	}
	return nil
}

// RequeueJob returns a claimed job to the queue, used when the dispatcher
// claims a job but cannot hand it to a worker (shutdown, maintenance).
func RequeueJob(ctx context.Context, db *sql.DB, jobID string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		UPDATE jobs SET state = 'queued', started_at = NULL, updated_at = ?
		WHERE id = ? AND state = 'running'`, formatTime(now), jobID)
	if err != nil {
		return fmt.Errorf("requeue job: %w", err)
	}
	return nil
}

func UpdateJobProgress(ctx context.Context, db *sql.DB, jobID string, current, total int) error {
	if current < 0 || total < 0 || (total > 0 && current > total) {
		return errors.New("invalid job progress")
	}
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		UPDATE jobs SET progress_current = ?, progress_total = ?, updated_at = ?
		WHERE id = ? AND state = 'running'`, current, total, formatTime(now), jobID)
	if err != nil {
		return fmt.Errorf("update job progress: %w", err)
	}
	return nil
}

func CompleteJob(ctx context.Context, db *sql.DB, jobID string) error {
	now := time.Now().UTC()
	result, err := db.ExecContext(ctx, `
		UPDATE jobs SET state = 'succeeded', progress_current = CASE
			WHEN progress_total > 0 THEN progress_total ELSE progress_current END,
			finished_at = ?, updated_at = ? WHERE id = ? AND state = 'running'`,
		formatTime(now), formatTime(now), jobID)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func CancelJob(ctx context.Context, db *sql.DB, jobID string) (domain.Job, error) {
	now := time.Now().UTC()
	result, err := db.ExecContext(ctx, `UPDATE jobs SET state = 'cancelled', finished_at = ?,
		error_code = 'cancelled', error_message = 'Cancelled by user', updated_at = ?
		WHERE id = ? AND state IN ('queued', 'running')`, formatTime(now), formatTime(now), jobID)
	if err != nil {
		return domain.Job{}, fmt.Errorf("cancel job: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, getErr := GetJob(ctx, db, jobID); errors.Is(getErr, ErrNotFound) {
			return domain.Job{}, ErrNotFound
		}
		return domain.Job{}, ErrJobNotCancellable
	}
	return GetJob(ctx, db, jobID)
}

func FailJob(ctx context.Context, db *sql.DB, jobID, code, message string) error {
	now := time.Now().UTC()
	result, err := db.ExecContext(ctx, `
		UPDATE jobs SET state = 'failed', error_code = ?, error_message = ?,
			finished_at = ?, updated_at = ? WHERE id = ? AND state = 'running'`,
		code, message, formatTime(now), formatTime(now), jobID)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func HasPendingFeedRefresh(ctx context.Context, db *sql.DB, feedID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM jobs
			WHERE kind = 'feed.refresh' AND state IN ('queued', 'running')
				AND json_extract(payload_json, '$.feed_id') = ?
		)`, feedID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pending feed refresh: %w", err)
	}
	return exists, nil
}

func HasPendingAccountSync(ctx context.Context, db *sql.DB, accountID string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM jobs
			WHERE kind = 'sync.account' AND state IN ('queued', 'running')
				AND json_extract(payload_json, '$.account_id') = ?
		)`, accountID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pending account sync: %w", err)
	}
	return exists, nil
}

func FindPendingAIOperationJob(ctx context.Context, db *sql.DB, entryID, operation, language, inputHash string) (*domain.Job, error) {
	job, err := scanJob(db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs
		WHERE kind = 'ai.operation' AND state IN ('queued', 'running')
			AND json_extract(payload_json, '$.entry_id') = ?
			AND json_extract(payload_json, '$.operation') = ?
			AND json_extract(payload_json, '$.language') = ?
			AND json_extract(payload_json, '$.input_hash') = ?
		ORDER BY created_at LIMIT 1`, entryID, operation, language, inputHash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find pending AI operation: %w", err)
	}
	return &job, nil
}

func BeginJobAttempt(ctx context.Context, db *sql.DB, jobID string) (string, error) {
	attemptID := uuid.NewString()
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		INSERT INTO job_attempts (id, job_id, attempt, started_at)
		SELECT ?, ?, COALESCE(MAX(attempt), 0) + 1, ?
		FROM job_attempts WHERE job_id = ?`, attemptID, jobID, formatTime(now), jobID)
	if err != nil {
		return "", fmt.Errorf("begin job attempt: %w", err)
	}
	return attemptID, nil
}

func FinishJobAttempt(ctx context.Context, db *sql.DB, attemptID, result, code, message string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		UPDATE job_attempts SET finished_at = ?, result = ?, error_code = NULLIF(?, ''),
			error_message = NULLIF(?, '') WHERE id = ?`,
		formatTime(now), result, code, message, attemptID)
	if err != nil {
		return fmt.Errorf("finish job attempt: %w", err)
	}
	return nil
}

func scanJob(row scanner) (domain.Job, error) {
	var job domain.Job
	var scheduledAt, createdAt, updatedAt string
	var startedAt, finishedAt, errorCode, errorMessage sql.NullString
	if err := row.Scan(
		&job.ID, &job.Kind, &job.State, &job.PayloadJSON,
		&job.ProgressCurrent, &job.ProgressTotal, &scheduledAt,
		&startedAt, &finishedAt, &errorCode, &errorMessage,
		&createdAt, &updatedAt,
	); err != nil {
		return domain.Job{}, err
	}
	job.ScheduledAt = parseTime(scheduledAt)
	job.StartedAt = timePointer(startedAt)
	job.FinishedAt = timePointer(finishedAt)
	job.ErrorCode = stringPointer(errorCode)
	job.ErrorMessage = stringPointer(errorMessage)
	job.CreatedAt = parseTime(createdAt)
	job.UpdatedAt = parseTime(updatedAt)
	return job, nil
}
