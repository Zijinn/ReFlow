package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobLifecycleAndRecovery(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	created, err := CreateJob(ctx, db, "test", map[string]string{"value": "one"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := ClaimNextJob(ctx, db)
	if err != nil || claimed == nil || claimed.ID != created.ID || claimed.State != "running" {
		t.Fatalf("unexpected claimed job: %+v, %v", claimed, err)
	}
	if err := UpdateJobProgress(ctx, db, claimed.ID, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := RecoverRunningJobs(ctx, db); err != nil {
		t.Fatal(err)
	}
	recovered, err := GetJob(ctx, db, claimed.ID)
	if err != nil || recovered.State != "queued" || recovered.StartedAt != nil {
		t.Fatalf("unexpected recovered job: %+v, %v", recovered, err)
	}
	claimed, err = ClaimNextJob(ctx, db)
	if err != nil || claimed == nil {
		t.Fatalf("reclaim job: %+v, %v", claimed, err)
	}
	attemptID, err := BeginJobAttempt(ctx, db, claimed.ID)
	if err != nil || attemptID == "" {
		t.Fatalf("begin attempt: %s, %v", attemptID, err)
	}
	if err := FinishJobAttempt(ctx, db, attemptID, "succeeded", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := CompleteJob(ctx, db, claimed.ID); err != nil {
		t.Fatal(err)
	}
	completed, err := GetJob(ctx, db, claimed.ID)
	if err != nil || completed.State != "succeeded" || completed.FinishedAt == nil {
		t.Fatalf("unexpected completed job: %+v, %v", completed, err)
	}
}

func TestConcurrentUniqueJobEnqueue(t *testing.T) {
	tests := []struct {
		name    string
		enqueue func(context.Context, *sql.DB, string) error
		kind    string
	}{
		{
			name: "feed refresh",
			kind: "feed.refresh",
			enqueue: func(ctx context.Context, db *sql.DB, id string) error {
				_, err := EnqueueFeedRefresh(ctx, db, id, time.Now().UTC())
				return err
			},
		},
		{
			name: "account sync",
			kind: "sync.account",
			enqueue: func(ctx context.Context, db *sql.DB, id string) error {
				_, err := EnqueueAccountSync(ctx, db, id, "auto", time.Now().UTC())
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			const callers = 24
			start := make(chan struct{})
			var wg sync.WaitGroup
			var succeeded atomic.Int32
			errs := make(chan error, callers)
			for range callers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					err := test.enqueue(ctx, db, "same-id")
					if err == nil {
						succeeded.Add(1)
						return
					}
					if !errors.Is(err, ErrJobAlreadyQueued) {
						errs <- err
					}
				}()
			}
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Errorf("unexpected enqueue error: %v", err)
			}
			if got := succeeded.Load(); got != 1 {
				t.Fatalf("successful enqueues = %d, want 1", got)
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE kind = ?`, test.kind).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("stored jobs = %d, want 1", count)
			}
		})
	}
}
