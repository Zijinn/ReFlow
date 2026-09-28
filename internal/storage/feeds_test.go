package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestFeedEntryDedupSearchAndMutationIdempotency(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	guid := "entry-guid"
	entryURL := "https://example.com/entry"
	initial := domain.ParsedFeed{
		Title: "Example", Format: "rss",
		Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "Initial title",
			PublishedAt: time.Now().UTC(), ContentHash: "hash-one",
			SanitizedHTML: "<p>Initial body</p>", PlainText: "Initial body",
		}},
	}
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", initial, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	subscriptions, err := ListSubscriptions(ctx, db, domain.DefaultProfileID)
	if err != nil || len(subscriptions) != 1 || subscriptions[0].UnreadCount != 1 {
		t.Fatalf("unexpected subscriptions: %+v, %v", subscriptions, err)
	}

	updated := initial
	updated.Entries = []domain.ParsedEntry{{
		GUID: &guid, CanonicalURL: &entryURL, Title: "Updated searchable title",
		PublishedAt: initial.Entries[0].PublishedAt, ContentHash: "hash-two",
		SanitizedHTML: "<p>Updated body</p>", PlainText: "needle content",
	}}
	inserted, err := SaveFeedRefresh(ctx, db, domain.DefaultProfileID, created.ID, updated, nil, nil)
	if err != nil || len(inserted) != 0 {
		t.Fatalf("expected deduplicated update, inserted=%v err=%v", inserted, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries WHERE feed_id = ?", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one entry, count=%d err=%v", count, err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Query: "needle", Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Updated searchable title" {
		t.Fatalf("unexpected FTS result: %+v, %v", page, err)
	}

	entryID := page.Items[0].ID
	truth := true
	falsehood := false
	state, err := UpdateEntryState(ctx, db, domain.DefaultProfileID, entryID, domain.EntryStatePatch{MutationID: "mutation-1", IsRead: &truth})
	if err != nil || !state.IsRead {
		t.Fatalf("mark read: %+v, %v", state, err)
	}
	state, err = UpdateEntryState(ctx, db, domain.DefaultProfileID, entryID, domain.EntryStatePatch{MutationID: "mutation-1", IsRead: &falsehood})
	if err != nil || !state.IsRead {
		t.Fatalf("duplicate mutation should be ignored: %+v, %v", state, err)
	}
	state, err = UpdateEntryState(ctx, db, domain.DefaultProfileID, entryID, domain.EntryStatePatch{MutationID: "mutation-2", IsRead: &falsehood})
	if err != nil || state.IsRead {
		t.Fatalf("new mutation should apply: %+v, %v", state, err)
	}
	future := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	state, err = UpdateEntryState(ctx, db, domain.DefaultProfileID, entryID, domain.EntryStatePatch{
		MutationID: "mutation-device-time", IsStarred: &truth, DeviceTime: &future,
	})
	if err != nil || !state.IsStarred {
		t.Fatalf("device time mutation failed: %+v, %v", state, err)
	}
	if state.UpdatedAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("device_time must not determine LWW time: updated_at=%s device_time=%s", state.UpdatedAt, future)
	}
}

func TestFeedEntryIdentityHashDeduplicatesEntriesWithoutStableGUIDOrURL(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	publishedAt := time.Date(2026, time.July, 18, 2, 0, 0, 0, time.UTC)
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Example", Format: "rss", Entries: []domain.ParsedEntry{{
			Title: "An article without a GUID", PublishedAt: publishedAt,
			ContentHash: "body-before", IdentityHash: "stable-identity",
			SanitizedHTML: "<p>Before</p>", PlainText: "Before",
		}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	inserted, err := SaveFeedRefresh(ctx, db, domain.DefaultProfileID, created.ID, domain.ParsedFeed{
		Title: "Example", Format: "rss", Entries: []domain.ParsedEntry{{
			Title: "An article without a GUID", PublishedAt: publishedAt,
			ContentHash: "body-after", IdentityHash: "stable-identity",
			SanitizedHTML: "<p>After</p>", PlainText: "After",
		}},
	}, nil, nil)
	if err != nil || len(inserted) != 0 {
		t.Fatalf("expected identity-hash deduplication, inserted=%v err=%v", inserted, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries WHERE feed_id = ?", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one entry after refresh, count=%d err=%v", count, err)
	}
}

func TestSubscriptionRefreshPolicyReschedulesFeed(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{Title: "Example", Format: "rss"}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	fixed := "fixed"
	interval := 90
	if _, err := UpdateSubscription(ctx, db, domain.DefaultProfileID, created.ID, domain.SubscriptionPatch{RefreshPolicy: &fixed, RefreshIntervalMinutes: &interval}); err != nil {
		t.Fatal(err)
	}
	feed, err := GetFeed(ctx, db, created.ID)
	if err != nil || feed.NextCheckAt == nil {
		t.Fatalf("read fixed refresh schedule: %+v, %v", feed, err)
	}
	if remaining := time.Until(*feed.NextCheckAt); remaining < 89*time.Minute || remaining > 91*time.Minute {
		t.Fatalf("expected 90 minute schedule, got %s", remaining)
	}

	never := "never"
	if _, err := UpdateSubscription(ctx, db, domain.DefaultProfileID, created.ID, domain.SubscriptionPatch{RefreshPolicy: &never}); err != nil {
		t.Fatal(err)
	}
	feed, err = GetFeed(ctx, db, created.ID)
	if err != nil || feed.NextCheckAt == nil || time.Until(*feed.NextCheckAt) < 364*24*time.Hour {
		t.Fatalf("expected never policy to defer scheduler, feed=%+v err=%v", feed, err)
	}
}

func TestUpdateSubscriptionRollsBackWhenFeedRescheduleFails(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/rollback", "https://example.com/rollback", domain.ParsedFeed{Title: "Original", Format: "rss"}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER fail_feed_reschedule BEFORE UPDATE ON feeds
		BEGIN
			SELECT RAISE(FAIL, 'feed update blocked');
		END`); err != nil {
		t.Fatal(err)
	}

	title := "Changed"
	interval := 45
	if _, err := UpdateSubscription(ctx, db, domain.DefaultProfileID, created.ID, domain.SubscriptionPatch{
		SetTitleOverride:       true,
		TitleOverride:          &title,
		RefreshIntervalMinutes: &interval,
	}); err == nil {
		t.Fatal("expected feed reschedule failure")
	}
	var storedTitle sql.NullString
	var storedInterval int
	if err := db.QueryRowContext(ctx, `
		SELECT title_override, refresh_interval_minutes FROM subscriptions
		WHERE profile_id = ? AND feed_id = ?`, domain.DefaultProfileID, created.ID).Scan(&storedTitle, &storedInterval); err != nil {
		t.Fatal(err)
	}
	if storedTitle.Valid || storedInterval != 0 {
		t.Fatalf("subscription update was not rolled back: title=%v interval=%d", storedTitle, storedInterval)
	}
}

func TestUpdateSubscriptionNonRefreshPatchKeepsNextCheck(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/no-reschedule", "https://example.com/no-reschedule", domain.ParsedFeed{Title: "Example", Format: "rss"}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	const nextCheck = "2040-01-02T03:04:05Z"
	if _, err := db.ExecContext(ctx, "UPDATE feeds SET next_check_at = ? WHERE id = ?", nextCheck, created.ID); err != nil {
		t.Fatal(err)
	}
	viewMode := "compact"
	updated, err := UpdateSubscription(ctx, db, domain.DefaultProfileID, created.ID, domain.SubscriptionPatch{ViewMode: &viewMode})
	if err != nil || updated.ViewMode != viewMode {
		t.Fatalf("update non-refresh field: subscription=%+v err=%v", updated, err)
	}
	var storedNextCheck string
	if err := db.QueryRowContext(ctx, "SELECT next_check_at FROM feeds WHERE id = ?", created.ID).Scan(&storedNextCheck); err != nil {
		t.Fatal(err)
	}
	if storedNextCheck != nextCheck {
		t.Fatalf("non-refresh patch changed next_check_at: got %q want %q", storedNextCheck, nextCheck)
	}
}

func TestEmptySubscriptionHasZeroUnreadAndFailureBackoffUsesRFC3339(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/empty", "https://example.com/empty", domain.ParsedFeed{Title: "Empty", Format: "rss"}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	subscriptions, err := ListSubscriptions(ctx, db, domain.DefaultProfileID)
	if err != nil || subscriptions[0].UnreadCount != 0 {
		t.Fatalf("empty subscription should have zero unread: %+v, %v", subscriptions, err)
	}
	if err := MarkFeedFailure(ctx, db, created.ID, "network", "offline"); err != nil {
		t.Fatal(err)
	}
	failed, err := GetFeed(ctx, db, created.ID)
	if err != nil || failed.FailureCount != 1 || failed.NextCheckAt == nil {
		t.Fatalf("unexpected failed feed: %+v, %v", failed, err)
	}
	if _, err := time.Parse(time.RFC3339Nano, formatTime(*failed.NextCheckAt)); err != nil {
		t.Fatalf("next check is not RFC3339: %v", err)
	}
	remaining := time.Until(*failed.NextCheckAt)
	if remaining < 4*time.Minute || remaining > 6*time.Minute {
		t.Fatalf("unexpected first backoff %s", remaining)
	}
}
