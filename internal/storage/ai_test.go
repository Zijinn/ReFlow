package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestAIResultCacheIsUniquePerInput(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := CreateAIProfile(ctx, db, CreateAIProfileParams{
		ID: "ai-1", Provider: "ollama", Name: "Local", Endpoint: "http://127.0.0.1:11434",
		Model: "qwen3:8b", Enabled: true, AllowPrivateNetwork: true,
	}); err != nil {
		t.Fatal(err)
	}
	guid := "ai-entry"
	entryURL := "https://example.com/ai-entry"
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/feed", "https://example.com/feed", domain.ParsedFeed{
		Title: "AI feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "AI article",
			PublishedAt: time.Now().UTC(), ContentHash: "ai-hash",
			SanitizedHTML: "<p>body</p>", PlainText: "body",
		}},
	}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Query: "AI article", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("entry: %+v, %v", page, err)
	}
	params := SaveAIResultParams{
		ProfileID: domain.DefaultProfileID, AIProfileID: "ai-1", EntryID: page.Items[0].ID,
		Operation: "summary", Language: "en", InputHash: "same-input",
		ResultText: "first", Provider: "ollama", Model: "qwen3:8b",
	}
	first, inserted, err := SaveAIResultAndUsage(ctx, db, params)
	if err != nil || !inserted {
		t.Fatalf("first result: %+v inserted=%v err=%v", first, inserted, err)
	}
	params.ResultText = "second"
	second, inserted, err := SaveAIResultAndUsage(ctx, db, params)
	if err != nil || inserted {
		t.Fatalf("duplicate result must be ignored: %+v inserted=%v err=%v", second, inserted, err)
	}
	var results, usage int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_results WHERE profile_id = ? AND entry_id = ?", domain.DefaultProfileID, page.Items[0].ID).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_usage WHERE entry_id = ?", page.Items[0].ID).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if results != 1 || usage != 1 {
		t.Fatalf("cache must keep one result and one usage row, got results=%d usage=%d", results, usage)
	}
	if second.ResultText != "first" {
		t.Fatalf("duplicate must return the cached result, got %q", second.ResultText)
	}
}
