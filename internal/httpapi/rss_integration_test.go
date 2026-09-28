package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/storage"
)

const integrationRSS = `<?xml version="1.0"?><rss version="2.0"><channel>
<title>Integration feed</title><link>https://example.com/</link><description>Test</description>
<item><guid>one</guid><title>First entry</title><link>https://example.com/one</link>
<pubDate>Fri, 17 Jul 2026 01:00:00 GMT</pubDate><description><![CDATA[<p>Readable content</p>]]></description></item>
</channel></rss>`

func TestRSSAPICoreFlow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"integration-v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"integration-v1"`)
		_, _ = io.WriteString(w, integrationRSS)
	}))
	defer upstream.Close()

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fetcher := feedcore.NewFetcher()
	fetcher.Policy.AllowPrivate = true
	server := NewWithFetcher(db, slog.New(slog.NewTextHandler(io.Discard, nil)), "", fetcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	createdResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/feeds", map[string]string{"url": upstream.URL})
	if createdResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create feed returned %d: %s", createdResponse.StatusCode, readBody(t, createdResponse))
	}
	var created struct {
		ID string `json:"id"`
	}
	decodeResponse(t, createdResponse, &created)
	if created.ID == "" {
		t.Fatal("create feed did not return an ID")
	}

	entriesResponse, err := http.Get(httpServer.URL + "/api/v1/entries?state=unread")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	decodeResponse(t, entriesResponse, &page)
	if len(page.Items) != 1 || page.Items[0].Title != "First entry" {
		t.Fatalf("unexpected entries: %+v", page.Items)
	}

	stateResponse := requestJSON(t, http.MethodPatch, httpServer.URL+"/api/v1/entries/"+page.Items[0].ID+"/state", map[string]any{"mutation_id": "api-mutation-1", "is_read": true})
	if stateResponse.StatusCode != http.StatusOK {
		t.Fatalf("update state returned %d: %s", stateResponse.StatusCode, readBody(t, stateResponse))
	}
	stateResponse.Body.Close()

	refreshResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/feeds/"+created.ID+"/refresh", nil)
	if refreshResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("refresh returned %d: %s", refreshResponse.StatusCode, readBody(t, refreshResponse))
	}
	var queued struct {
		ID string `json:"id"`
	}
	decodeResponse(t, refreshResponse, &queued)
	waitForJobState(t, httpServer.URL, queued.ID, "succeeded")

	exportResponse, err := http.Get(httpServer.URL + "/api/v1/exports/opml")
	if err != nil {
		t.Fatal(err)
	}
	exported := readBody(t, exportResponse)
	if !strings.Contains(exported, upstream.URL) || !strings.Contains(exported, "Integration feed") {
		t.Fatalf("unexpected OPML export: %s", exported)
	}

	deleteRequest, _ := http.NewRequest(http.MethodDelete, httpServer.URL+"/api/v1/feeds/"+created.ID, nil)
	deleteResponse, err := http.DefaultClient.Do(deleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("delete returned %d", deleteResponse.StatusCode)
	}
}

func TestEntryStateUsesAuthenticatedDeviceProvenance(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	guid := "provenance-entry"
	created, err := storage.SaveNewFeed(ctx, server.db, domain.DefaultProfileID, "https://example.com/provenance.xml", "https://example.com/provenance.xml", domain.ParsedFeed{
		Title: "Provenance", Format: "rss", Entries: []domain.ParsedEntry{{GUID: &guid, Title: "Entry", PublishedAt: time.Now().UTC(), ContentHash: "body"}},
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storage.ListEntries(ctx, server.db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: created.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list entry: %+v, %v", page, err)
	}
	entryID := page.Items[0].ID

	code, _, err := storage.CreatePairingCode(ctx, server.db, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	device, token, err := storage.PairDevice(ctx, server.db, code, "iPad", "ipad")
	if err != nil {
		t.Fatal(err)
	}
	server.ConfigureSecurity(true, nil, nil)

	mismatch := httptest.NewRequest(http.MethodPatch, "/api/v1/entries/"+entryID+"/state", strings.NewReader(`{"mutation_id":"remote-mismatch","device_id":"forged","is_read":true}`))
	mismatch.RemoteAddr = "192.168.1.20:44000"
	mismatch.Header.Set("Authorization", "Bearer "+token)
	mismatchResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(mismatchResponse, mismatch)
	if mismatchResponse.Code != http.StatusConflict {
		t.Fatalf("expected body/auth device conflict, got %d: %s", mismatchResponse.Code, mismatchResponse.Body.String())
	}

	authenticated := httptest.NewRequest(http.MethodPatch, "/api/v1/entries/"+entryID+"/state", strings.NewReader(`{"mutation_id":"remote-ok","device_id":"`+device.ID+`","device_time":"1970-01-01T00:00:00Z","is_read":true}`))
	authenticated.RemoteAddr = "192.168.1.20:44001"
	authenticated.Header.Set("Authorization", "Bearer "+token)
	authenticatedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authenticatedResponse, authenticated)
	if authenticatedResponse.Code != http.StatusOK {
		t.Fatalf("authenticated update failed: %d %s", authenticatedResponse.Code, authenticatedResponse.Body.String())
	}
	var storedDevice string
	if err := server.db.QueryRow("SELECT updated_by_device_id FROM entry_states WHERE entry_id = ?", entryID).Scan(&storedDevice); err != nil || storedDevice != device.ID {
		t.Fatalf("stored device = %q, err=%v", storedDevice, err)
	}

	direct := httptest.NewRequest(http.MethodPatch, "/api/v1/entries/"+entryID+"/state", strings.NewReader(`{"mutation_id":"loopback","device_id":"forged","device_time":"2999-01-01T00:00:00Z","is_starred":true}`))
	direct.RemoteAddr = "127.0.0.1:44002"
	direct.Host = "127.0.0.1:7381"
	directResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(directResponse, direct)
	if directResponse.Code != http.StatusOK {
		t.Fatalf("loopback update failed: %d %s", directResponse.Code, directResponse.Body.String())
	}
	var directDevice *string
	var updatedAt string
	if err := server.db.QueryRow("SELECT updated_by_device_id, updated_at FROM entry_states WHERE entry_id = ?", entryID).Scan(&directDevice, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if directDevice != nil {
		t.Fatalf("loopback request recorded forged device: %q", *directDevice)
	}
	if parsed, err := time.Parse(time.RFC3339Nano, updatedAt); err != nil || parsed.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("device_time influenced stored update time: %q, %v", updatedAt, err)
	}
}

func TestSSEPublishesEvents(t *testing.T) {
	server := newTestServer(t)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	request, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/events", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := http.DefaultClient.Do(request.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	connected, err := reader.ReadString('\n')
	if err != nil || connected != "event: connected\n" {
		t.Fatalf("unexpected connected event %q: %v", connected, err)
	}
	server.events.Publish("feed.updated", map[string]string{"id": "feed-1"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if line == "event: feed.updated\n" {
			return
		}
	}
	t.Fatal("did not receive feed.updated SSE")
}

func requestJSON(t *testing.T, method, url string, value any) *http.Response {
	t.Helper()
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func waitForJobState(t *testing.T, baseURL, jobID, wanted string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/api/v1/jobs/" + jobID)
		if err != nil {
			t.Fatal(err)
		}
		var current struct {
			State        string  `json:"state"`
			ErrorMessage *string `json:"error_message"`
		}
		decodeResponse(t, response, &current)
		if current.State == wanted {
			return
		}
		if current.State == "failed" {
			message := ""
			if current.ErrorMessage != nil {
				message = *current.ErrorMessage
			}
			t.Fatalf("job failed: %s", message)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", jobID, wanted)
}
