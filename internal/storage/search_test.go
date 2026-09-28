package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestTrigramFTSMatchesCJKSubstrings(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	feed := domain.ParsedFeed{
		Title: "经济研究", Format: "rss",
		Entries: []domain.ParsedEntry{{
			Title:       "数据要素市场化配置与福利效应研究",
			PublishedAt: time.Now().UTC(), ContentHash: "cjk-hash",
			SanitizedHTML: "<p>正文讨论跨区域流动的均衡模型</p>", PlainText: "正文讨论跨区域流动的均衡模型",
		}},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/cjk", "https://example.com/cjk", feed, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	queries := map[string]int{
		"要素市场":   1, // substring inside a longer CJK run
		"福利效应":   1,
		"均衡模型":   1, // body text
		"不存在的词汇": 0,
	}
	for query, want := range queries {
		page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Query: query, Limit: 10})
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if len(page.Items) != want {
			t.Fatalf("query %q: got %d items, want %d", query, len(page.Items), want)
		}
	}
}

func TestSearchFallsBackToLIKEForShortTokens(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	feed := domain.ParsedFeed{
		Title: "Example", Format: "rss",
		Entries: []domain.ParsedEntry{{
			Title: "AI 时代的经济", PublishedAt: time.Now().UTC(),
			ContentHash: "short-hash", SanitizedHTML: "<p>body</p>", PlainText: "body",
		}},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://example.com/short", "https://example.com/short", feed, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"AI", "经济", "时"} {
		page, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Query: query, Limit: 10})
		if err != nil {
			t.Fatalf("short query %q should not error: %v", query, err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("short query %q: got %d items, want 1", query, len(page.Items))
		}
	}
}
