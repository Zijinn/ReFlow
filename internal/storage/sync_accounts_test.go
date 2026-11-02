package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestCompleteSyncSchedulesNextRunInRFC3339(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	account, err := CreateSyncAccount(ctx, db, CreateSyncAccountParams{
		Provider: "miniflux", Name: "Miniflux", Endpoint: "https://example.com", EncryptedCredentials: []byte("secret"),
		Enabled: true, SyncIntervalMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}

	completedAt := time.Now().UTC().Truncate(time.Second)
	if err := CompleteSync(ctx, db, account.ID, "cursor-1", completedAt); err != nil {
		t.Fatal(err)
	}

	record, err := GetSyncAccountRecord(ctx, db, domain.DefaultProfileID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Account.NextSyncAt == nil {
		t.Fatal("expected next_sync_at to be set")
	}
	next := *record.Account.NextSyncAt
	if next.Before(completedAt.Add(29*time.Minute)) || next.After(completedAt.Add(31*time.Minute)) {
		t.Fatalf("next sync not scheduled one interval out: %v (completed %v)", next, completedAt)
	}

	// The account must not be due again immediately after completing.
	due, err := ListDueSyncAccounts(ctx, db, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range due {
		if item.ID == account.ID {
			t.Fatalf("account due again right after completion: %+v", item)
		}
	}
}

func TestFindEntryForSyncDropsStaleMapping(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	guid := "stale-entry"
	entryURL := "https://example.com/stale-entry"
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/stale-feed", "https://example.com/stale-feed", domain.ParsedFeed{
		Title: "Stale feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "Stale article",
			PublishedAt: time.Now().UTC(), ContentHash: "stale-hash",
			SanitizedHTML: "<p>stale body</p>", PlainText: "stale body",
		}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: created.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list entries: %+v, %v", page, err)
	}
	account, err := CreateSyncAccount(ctx, db, CreateSyncAccountParams{
		Provider: "miniflux", Name: "Miniflux", Endpoint: "https://example.com",
		EncryptedCredentials: []byte("secret"), Enabled: true, SyncIntervalMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpsertSyncMapping(ctx, db, account.ID, "entry", page.Items[0].ID, "remote-stale"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM entries WHERE id = ?", page.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := FindEntryForSync(ctx, db, account.ID, "remote-stale", "", guid, entryURL); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale mapping should resolve to ErrNotFound, got %v", err)
	}
	var mappings int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sync_mappings WHERE account_id = ? AND remote_id = 'remote-stale'", account.ID).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if mappings != 0 {
		t.Fatalf("stale mapping was not removed: %d", mappings)
	}
}

func TestMigrationNormalizesDatetimeSyncSchedule(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	account, err := CreateSyncAccount(ctx, db, CreateSyncAccountParams{
		Provider: "fever", Name: "Fever", Endpoint: "https://example.com", EncryptedCredentials: []byte("secret"),
		Enabled: true, SyncIntervalMinutes: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a value written by the pre-fix datetime() expression.
	future := time.Now().UTC().Add(2 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.ExecContext(ctx, "UPDATE sync_accounts SET next_sync_at = ? WHERE id = ?", future, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE sync_accounts
		SET next_sync_at = strftime('%Y-%m-%dT%H:%M:%fZ', next_sync_at)
		WHERE next_sync_at IS NOT NULL AND next_sync_at NOT LIKE '%T%'`); err != nil {
		t.Fatal(err)
	}
	record, err := GetSyncAccountRecord(ctx, db, domain.DefaultProfileID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Account.NextSyncAt == nil || record.Account.NextSyncAt.Before(time.Now().UTC().Add(time.Hour)) {
		t.Fatalf("normalized next_sync_at lost: %+v", record.Account.NextSyncAt)
	}
}
