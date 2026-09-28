package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestPruneReadEntriesProtectsStarredAnnotatedAndUnread(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	old := time.Now().UTC().Add(-90 * 24 * time.Hour)
	feed := domain.ParsedFeed{
		Title: "Retention feed", Format: "rss",
		Entries: []domain.ParsedEntry{
			{Title: "old read", PublishedAt: old, ContentHash: "r1"},
			{Title: "old unread", PublishedAt: old, ContentHash: "r2"},
			{Title: "old starred", PublishedAt: old, ContentHash: "r3"},
			{Title: "old annotated", PublishedAt: old, ContentHash: "r4"},
			{Title: "fresh read", PublishedAt: time.Now().UTC(), ContentHash: "r5"},
		},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/retention", "https://example.com/retention", feed, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil || len(page.Items) != 5 {
		t.Fatalf("seed entries: %v (items=%d)", err, len(page.Items))
	}
	byTitle := make(map[string]string, 5)
	for _, item := range page.Items {
		byTitle[item.Title] = item.ID
	}
	truth := true
	markRead := func(titles ...string) {
		for _, title := range titles {
			if _, err := UpdateEntryState(ctx, db, domain.DefaultProfileID, byTitle[title], domain.EntryStatePatch{
				MutationID: "test-" + title, IsRead: &truth,
			}); err != nil {
				t.Fatalf("mark %q read: %v", title, err)
			}
		}
	}
	markRead("old read", "old starred", "old annotated", "fresh read")
	if _, err := UpdateEntryState(ctx, db, domain.DefaultProfileID, byTitle["old starred"], domain.EntryStatePatch{
		MutationID: "star", IsStarred: &truth,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, byTitle["old annotated"], domain.EntryAnnotation{
		Style: "highlight", Quote: "old annotated",
	}); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneReadEntries(ctx, db, domain.DefaultProfileID, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("pruned %d entries, want exactly 1 (old read)", pruned)
	}

	remaining, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Items) != 4 {
		t.Fatalf("remaining %d entries, want 4", len(remaining.Items))
	}
	for _, item := range remaining.Items {
		if item.Title == "old read" {
			t.Fatal("old read entry should have been pruned")
		}
	}
	// The pruned entry must also be gone from the search index.
	found, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Query: "old read", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range found.Items {
		if item.ID == byTitle["old read"] {
			t.Fatal("pruned entry still searchable")
		}
	}
}

func TestRetentionPreferenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if days, err := RetentionDays(ctx, db, domain.DefaultProfileID); err != nil || days != 0 {
		t.Fatalf("unset retention should be 0, got %d, %v", days, err)
	}
	if err := SetPreference(ctx, db, domain.DefaultProfileID, PreferenceRetentionDays, []byte("45")); err != nil {
		t.Fatal(err)
	}
	if days, err := RetentionDays(ctx, db, domain.DefaultProfileID); err != nil || days != 45 {
		t.Fatalf("retention should be 45, got %d, %v", days, err)
	}
	if err := SetPreference(ctx, db, domain.DefaultProfileID, PreferenceRetentionDays, []byte("not json")); err == nil {
		t.Fatal("invalid JSON preference should be rejected")
	}
}
