package storage

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

// Reapplying a rule that changes nothing must not refresh
// entry_states.updated_at, otherwise sync conflict detection treats every
// rule-matched entry as locally modified after each refresh.
func TestRuleReapplicationDoesNotTouchUpdatedAt(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	conditions, _ := json.Marshal(map[string]string{"title_contains": "reflow"})
	actions, _ := json.Marshal(map[string]any{"mark_read": true})
	if _, err := CreateRule(ctx, db, domain.DefaultProfileID, "Read ReFlow posts", true, 0, conditions, actions); err != nil {
		t.Fatal(err)
	}
	guid := "stable-rule-entry"
	feed, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/rules2", "https://example.com/rules2", domain.ParsedFeed{
		Title: "Rules", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, Title: "ReFlow stable", PublishedAt: time.Now().UTC(),
			ContentHash: "stable-hash", PlainText: "body",
		}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyRulesToFeed(ctx, db, domain.DefaultProfileID, feed.ID, nil); err != nil {
		t.Fatal(err)
	}
	var first string
	if err := db.QueryRowContext(ctx, `
		SELECT es.updated_at FROM entry_states es
		JOIN entries e ON e.id = es.entry_id WHERE e.feed_id = ?`, feed.ID).Scan(&first); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)
	if err := ApplyRulesToFeed(ctx, db, domain.DefaultProfileID, feed.ID, nil); err != nil {
		t.Fatal(err)
	}
	var second string
	if err := db.QueryRowContext(ctx, `
		SELECT es.updated_at FROM entry_states es
		JOIN entries e ON e.id = es.entry_id WHERE e.feed_id = ?`, feed.ID).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("unchanged rule application moved updated_at: %s -> %s", first, second)
	}
}

// Refreshing a feed whose entries are unchanged must neither rewrite the rows
// nor report insertions.
func TestUnchangedRefreshSkipsEntryWrites(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	publishedAt := time.Now().UTC().Add(-time.Hour)
	parsed := domain.ParsedFeed{
		Title: "Example", Format: "rss", Entries: []domain.ParsedEntry{{
			Title: "Stable entry", PublishedAt: publishedAt,
			ContentHash: "stable-content", PlainText: "body", SanitizedHTML: "<p>body</p>",
		}},
	}
	feed, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/stable", "https://example.com/stable", parsed, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var updatedAt string
	if err := db.QueryRowContext(ctx, "SELECT updated_at FROM entries WHERE feed_id = ?", feed.ID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)
	inserted, err := SaveFeedRefresh(ctx, db, domain.DefaultProfileID, feed.ID, parsed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 0 {
		t.Fatalf("unchanged refresh reported insertions: %v", inserted)
	}
	var after string
	if err := db.QueryRowContext(ctx, "SELECT updated_at FROM entries WHERE feed_id = ?", feed.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != updatedAt {
		t.Fatalf("unchanged refresh rewrote entry: %s -> %s", updatedAt, after)
	}
}
