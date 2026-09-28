package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zijinn/ReFlow/internal/domain"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func TestFetchReadabilityStoresSanitizedFullText(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Full text test</title><link>%s</link><description>Test</description><item><guid>one</guid><title>Readable article</title><link>%s/article</link><description>Excerpt only</description></item></channel></rss>`, server.URL, server.URL)
		case "/article":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<html><head><title>Readable article</title></head><body><nav>Navigation</nav><article><h1>Readable article</h1>%s<img src="/image.jpg" onerror="alert(1)"><script>alert(1)</script></article></body></html>`, strings.Repeat("<p>This is substantial readable content with enough words for extraction and indexing.</p>", 20))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fetcher := feedcore.NewFetcher()
	fetcher.Policy.AllowPrivate = true
	service := NewFeedService(db, fetcher)
	created, err := service.AddFeed(context.Background(), AddFeedInput{URL: server.URL + "/feed.xml"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := storage.ListEntries(context.Background(), db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: created.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list entry: %+v, %v", page, err)
	}
	if err := service.FetchReadability(context.Background(), page.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	detail, err := storage.GetEntry(context.Background(), db, domain.DefaultProfileID, page.Items[0].ID)
	if err != nil || detail.ReadabilityHTML == nil {
		t.Fatalf("read detail: %+v, %v", detail, err)
	}
	lower := strings.ToLower(*detail.ReadabilityHTML)
	if !strings.Contains(lower, "substantial readable content") || strings.Contains(lower, "<script") || strings.Contains(lower, "onerror") {
		t.Fatalf("unexpected readability HTML: %s", *detail.ReadabilityHTML)
	}
}
