package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/Zijinn/ReFlow/internal/syncadapter"
)

func TestSyncServiceEncryptsCredentialsAndPreservesLocalConflictWinner(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	entryURL := "https://example.com/articles/101"
	guid := "local-guid-101"
	created, err := storage.SaveNewFeed(ctx, db, domain.DefaultProfileID,
		"https://example.com/miniflux.xml", "https://example.com/miniflux.xml",
		domain.ParsedFeed{Title: "Local feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "Mapped article",
			PublishedAt: time.Now().UTC(), ContentHash: "sync-entry-101",
			SanitizedHTML: "<p>Mapped</p>", PlainText: "Mapped",
		}}}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storage.ListEntries(ctx, db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: created.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list local entry: %+v %v", page, err)
	}
	entryID := page.Items[0].ID

	var mu sync.Mutex
	var writeBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "top-secret-token" {
			t.Errorf("missing Miniflux token")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/feeds":
			_, _ = io.WriteString(w, `[{"id":7,"title":"Remote feed","feed_url":"https://example.com/miniflux.xml","category":{"title":"Tech"}}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/entries":
			_, _ = io.WriteString(w, `{"entries":[{"id":101,"feed_id":7,"status":"unread","starred":true,"url":"https://example.com/articles/101","changed_at":"2024-03-09T16:00:00Z"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/entries/101":
			_, _ = io.WriteString(w, `{"id":101,"starred":false}`)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/entries":
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			writeBodies = append(writeBodies, string(body))
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/entries/101/bookmark":
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	feedService := NewFeedService(db, nil)
	syncService := newSyncService(db, feedService, box, func(bool) *http.Client { return server.Client() })
	account, err := syncService.CreateAccount(ctx, SyncAccountInput{
		Provider: "miniflux", Name: "Test Miniflux", Endpoint: server.URL,
		Credentials: syncadapter.Credentials{Token: "top-secret-token"}, SyncIntervalMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := storage.GetSyncAccountRecord(ctx, db, domain.DefaultProfileID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(record.EncryptedCredentials, []byte("top-secret-token")) {
		t.Fatal("sync credentials were stored in plaintext")
	}

	first, err := syncService.Run(ctx, account.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.PulledStates != 1 || first.SkippedRemoteStates != 0 {
		t.Fatalf("unexpected initial sync result: %+v", first)
	}
	entry, err := storage.GetEntry(ctx, db, domain.DefaultProfileID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.State.IsRead || !entry.State.IsStarred {
		t.Fatalf("remote initial state was not applied: %+v", entry.State)
	}

	read, starred := true, false
	if _, err := storage.UpdateEntryState(ctx, db, domain.DefaultProfileID, entryID, domain.EntryStatePatch{
		MutationID: "local-after-first-sync", IsRead: &read, IsStarred: &starred,
	}); err != nil {
		t.Fatal(err)
	}
	second, err := syncService.Run(ctx, account.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.PushedStates != 1 || second.SkippedRemoteStates != 1 {
		t.Fatalf("unexpected second sync result: %+v", second)
	}
	entry, err = storage.GetEntry(ctx, db, domain.DefaultProfileID, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.State.IsRead || entry.State.IsStarred {
		t.Fatalf("remote stale state overwrote local state: %+v", entry.State)
	}
	mu.Lock()
	writes := strings.Join(writeBodies, "\n")
	mu.Unlock()
	if !strings.Contains(writes, `"status":"read"`) {
		t.Fatalf("local read state was not pushed: %s", writes)
	}

	updated, err := storage.GetSyncAccountRecord(ctx, db, domain.DefaultProfileID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Account.LastSyncAt == nil || updated.Account.LastErrorCode != nil {
		t.Fatalf("sync status was not recorded: %+v", updated.Account)
	}
}
