package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestDetectContentKind(t *testing.T) {
	doi := "10.1038/s41586-026-00001-x"
	cases := []struct {
		name    string
		feedURL string
		siteURL *string
		entries []domain.ParsedEntry
		want    string
	}{
		{"doi entries", "https://journal.example.com/feed", nil, []domain.ParsedEntry{{DOI: &doi}, {DOI: &doi}}, domain.ContentKindLiterature},
		{"academic host", "https://www.nature.com/nature.rss", nil, nil, domain.ContentKindLiterature},
		{"academic subdomain", "https://ieeexplore.ieee.org/rss", nil, nil, domain.ContentKindLiterature},
		{"arxiv site url", "https://export.arxiv.org/rss/cs.AI", nil, nil, domain.ContentKindLiterature},
		{"bilibili", "https://rsshub.example/bilibili/user/123", strPointer("https://space.bilibili.com/123"), nil, domain.ContentKindVideo},
		{"x host", "https://x.com/someone", nil, nil, domain.ContentKindSocial},
		{"news stays general", "https://www.example-news.com/feed", nil, nil, domain.ContentKindGeneral},
	}
	for _, tc := range cases {
		if got := domain.DetectContentKind(tc.feedURL, tc.siteURL, tc.entries); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestListEntriesFiltersByContentKind(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	doi := "10.1145/1234567.1234568"
	literature := domain.ParsedFeed{
		Title: "ACM Transactions", Format: "rss",
		Entries: []domain.ParsedEntry{{Title: "文献条目", PublishedAt: time.Now().UTC(), ContentHash: "k1", DOI: &doi}},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://dl.acm.org/feed", "https://dl.acm.org/feed", literature, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	news := domain.ParsedFeed{
		Title: "财经新闻", Format: "rss",
		Entries: []domain.ParsedEntry{{Title: "市场快讯", PublishedAt: time.Now().UTC(), ContentHash: "k2"}},
	}
	if _, err := SaveNewFeed(ctx, db, domain.DefaultProfileID, "https://news.example.com/feed", "https://news.example.com/feed", news, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	literaturePage, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, ContentKind: "literature", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(literaturePage.Items) != 1 || literaturePage.Items[0].Title != "文献条目" {
		t.Fatalf("literature scope returned %+v", literaturePage.Items)
	}
	all, err := ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 2 {
		t.Fatalf("unfiltered list returned %d items, want 2", len(all.Items))
	}

	// Mark-read must honour the same content-kind filter.
	updated, err := MarkEntriesRead(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, ContentKind: "literature"})
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("mark read with content kind updated %d, want 1", updated)
	}
	entry, err := GetEntry(ctx, db, domain.DefaultProfileID, literaturePage.Items[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.State.IsRead {
		t.Fatal("literature entry should be read after scoped mark-all")
	}
}

func strPointer(value string) *string { return &value }
