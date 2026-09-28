package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/google/uuid"
)

func TestReconcileEntryIdentitiesMergesHistoricalDuplicate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	publishedAt := time.Date(2026, time.July, 18, 2, 0, 0, 0, time.UTC)
	guid := "stable-guid"
	url := "https://example.com/article"
	created, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "Example", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &url, Title: "Same article", PublishedAt: publishedAt,
			ContentHash: "hash-one", IdentityHash: "identity-one", PlainText: "body",
		}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	duplicateID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO entries (id, feed_id, guid, canonical_url, title, author, summary, published_at, discovered_at,
			content_hash, identity_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		duplicateID, created.ID, "unstable-guid", "https://example.com/article?utm_source=mail", "Same article", nil, nil,
		formatTime(publishedAt), formatTime(time.Now().UTC()), "hash-two", formatTime(time.Now().UTC()), formatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO entry_contents (entry_id, plain_text, updated_at) VALUES (?, ?, ?)`, duplicateID, "body", formatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileEntryIdentities(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries WHERE feed_id = ?", created.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected historical duplicate to be merged, count=%d err=%v", count, err)
	}
}
