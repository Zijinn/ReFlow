package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestDuplicateDOIIsNotInsertedTwice(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	doi := "10.1038/s41586-026-00002-8"
	first := domain.ParsedFeed{
		Title: "Nature", Format: "rss",
		Entries: []domain.ParsedEntry{{Title: "原始版本", PublishedAt: time.Now().UTC(), ContentHash: "d1", DOI: &doi}},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://www.nature.com/feed", "https://www.nature.com/feed", first, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	second := domain.ParsedFeed{
		Title: "Aggregator", Format: "rss",
		Entries: []domain.ParsedEntry{{Title: "转载版本", PublishedAt: time.Now().UTC(), ContentHash: "d2", DOI: &doi}},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://agg.example.com/feed", "https://agg.example.com/feed", second, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "原始版本" {
		t.Fatalf("DOI duplicate should be skipped, got %+v", page.Items)
	}
}

func TestReconcileEntryDOIsMergesExistingDuplicates(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	doi := "10.1016/j.cell.2026.01.001"
	// Insert the same DOI twice via raw SQL to simulate a pre-dedup library.
	feed := domain.ParsedFeed{
		Title: "Cell", Format: "rss",
		Entries: []domain.ParsedEntry{{Title: "论文", PublishedAt: time.Now().UTC(), ContentHash: "m1", DOI: &doi}},
	}
	saved, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://www.cell.com/feed", "https://www.cell.com/feed", feed, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	duplicateID := "dup-" + t.Name()
	now := formatTime(time.Now().UTC())
	if _, err := db.ExecContext(ctx, `
		INSERT INTO entries (id, feed_id, title, published_at, discovered_at, content_hash, doi, created_at, updated_at)
		VALUES (?, ?, '论文（聚合转载）', ?, ?, 'm2', ?, ?, ?)`,
		duplicateID, saved.ID, now, now, doi, now, now); err != nil {
		t.Fatal(err)
	}

	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("seed duplicates: %v (items=%d)", err, len(page.Items))
	}
	var survivorID string
	for _, item := range page.Items {
		if item.ID != duplicateID {
			survivorID = item.ID
		}
	}
	truth := true
	if _, err := UpdateEntryState(ctx, db, domain.DefaultProfileID, duplicateID, domain.EntryStatePatch{MutationID: "star-dup", IsStarred: &truth}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateEntryAnnotation(ctx, db, domain.DefaultProfileID, duplicateID, domain.EntryAnnotation{Style: "highlight", Quote: "转载"}); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileEntryDOIs(ctx, db); err != nil {
		t.Fatal(err)
	}
	page, err = ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != survivorID {
		t.Fatalf("DOI reconcile should keep one survivor, got %+v", page.Items)
	}
	survivor, err := GetEntry(ctx, db, domain.DefaultProfileID, survivorID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !survivor.State.IsStarred {
		t.Fatal("survivor should inherit the duplicate's star")
	}
	annotations, err := ListEntryAnnotations(ctx, db, domain.DefaultProfileID, survivorID)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 {
		t.Fatalf("duplicate's annotation should move to the survivor, got %+v", annotations)
	}
}
