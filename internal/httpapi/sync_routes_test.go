package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func TestSyncAccountAPIKeepsCredentialsPrivateAndRunsJob(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "sync-api-secret" {
			t.Errorf("missing sync token")
		}
		switch r.URL.Path {
		case "/v1/feeds":
			_, _ = io.WriteString(w, `[]`)
		case "/v1/entries":
			_, _ = io.WriteString(w, `{"entries":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server, httpServer := newSyncTestServer(t, nil)
	createResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts", map[string]any{
		"provider": "miniflux", "name": "Personal Miniflux", "endpoint": upstream.URL,
		"credentials":           map[string]string{"token": "sync-api-secret"},
		"allow_private_network": true, "sync_interval_minutes": 30,
	})
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create sync account returned %d: %s", createResponse.StatusCode, readBody(t, createResponse))
	}
	body := readBody(t, createResponse)
	if strings.Contains(body, "sync-api-secret") || strings.Contains(body, "credentials") {
		t.Fatalf("sync account response exposed credentials: %s", body)
	}
	var account struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &account); err != nil || account.ID == "" {
		t.Fatalf("decode account: %+v %v", account, err)
	}

	runResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/"+account.ID+"/sync", nil)
	if runResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("queue sync returned %d: %s", runResponse.StatusCode, readBody(t, runResponse))
	}
	var queued struct {
		ID string `json:"id"`
	}
	decodeResponse(t, runResponse, &queued)
	waitForJobState(t, httpServer.URL, queued.ID, "succeeded")

	accountsResponse, err := http.Get(httpServer.URL + "/api/v1/sync/accounts")
	if err != nil {
		t.Fatal(err)
	}
	accountsBody := readBody(t, accountsResponse)
	if !strings.Contains(accountsBody, `"last_sync_at":`) || strings.Contains(accountsBody, "sync-api-secret") {
		t.Fatalf("unexpected account list: %s", accountsBody)
	}

	deleteRequest, _ := http.NewRequest(http.MethodDelete, httpServer.URL+"/api/v1/sync/accounts/"+account.ID, nil)
	deleteResponse, err := http.DefaultClient.Do(deleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("delete sync account returned %d", deleteResponse.StatusCode)
	}
	_ = server
}

func TestSyncFailureDoesNotBlockLocalFeedRefresh(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = io.WriteString(w, integrationRSS)
		case "/sync/v1/feeds":
			http.Error(w, "remote unavailable", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	feedFetcher := feedcore.NewFetcher()
	feedFetcher.Policy.AllowPrivate = true
	_, httpServer := newSyncTestServer(t, feedFetcher)

	feedResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/feeds", map[string]string{"url": upstream.URL + "/feed"})
	if feedResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create feed returned %d: %s", feedResponse.StatusCode, readBody(t, feedResponse))
	}
	var feed struct {
		ID string `json:"id"`
	}
	decodeResponse(t, feedResponse, &feed)

	accountResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts", map[string]any{
		"provider": "miniflux", "name": "Failing remote", "endpoint": upstream.URL + "/sync",
		"credentials": map[string]string{"token": "test"}, "allow_private_network": true,
	})
	var account struct {
		ID string `json:"id"`
	}
	decodeResponse(t, accountResponse, &account)

	syncResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/"+account.ID+"/sync", nil)
	var syncJob struct {
		ID string `json:"id"`
	}
	decodeResponse(t, syncResponse, &syncJob)
	refreshResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/feeds/"+feed.ID+"/refresh", nil)
	var refreshJob struct {
		ID string `json:"id"`
	}
	decodeResponse(t, refreshResponse, &refreshJob)

	waitForJobState(t, httpServer.URL, syncJob.ID, "failed")
	waitForJobState(t, httpServer.URL, refreshJob.ID, "succeeded")
}

func TestWebDAVConnectionTestVerifiesWritesAndDoesNotPersistCredentials(t *testing.T) {
	var requests int
	expectedPassword := "saved-app-password"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		username, password, ok := r.BasicAuth()
		if !ok || username != "nutstore@example.com" || password != expectedPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/" && r.Header.Get("Depth") == "0":
			w.WriteHeader(http.StatusMultiStatus)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/dav/reflow-connection-test-"):
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/dav/reflow-connection-test-"):
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected WebDAV probe: %s %s depth=%q", r.Method, r.URL.Path, r.Header.Get("Depth"))
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	_, httpServer := newSyncTestServer(t, nil)
	createdResponse := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts", map[string]any{
		"provider": "webdav", "name": "Nutstore", "endpoint": upstream.URL + "/dav/",
		"credentials":           map[string]string{"username": "nutstore@example.com", "password": "saved-app-password"},
		"allow_private_network": true, "sync_interval_minutes": 30,
	})
	if createdResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create WebDAV account returned %d: %s", createdResponse.StatusCode, readBody(t, createdResponse))
	}
	var account struct {
		ID string `json:"id"`
	}
	decodeResponse(t, createdResponse, &account)

	failedTest := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/test", map[string]any{
		"account_id": account.ID, "credentials": map[string]string{"password": "wrong-password"},
		"allow_private_network": true,
	})
	if failedTest.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong WebDAV password returned %d: %s", failedTest.StatusCode, readBody(t, failedTest))
	}
	if body := readBody(t, failedTest); !strings.Contains(body, `"code":"authentication_error"`) {
		t.Fatalf("wrong WebDAV password returned unexpected problem: %s", body)
	}

	savedCredentialsTest := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/test", map[string]any{
		"account_id": account.ID, "allow_private_network": true,
	})
	if savedCredentialsTest.StatusCode != http.StatusOK {
		t.Fatalf("saved WebDAV credentials were changed by test: %d %s", savedCredentialsTest.StatusCode, readBody(t, savedCredentialsTest))
	}
	var result struct {
		OK       bool   `json:"ok"`
		Endpoint string `json:"endpoint"`
	}
	decodeResponse(t, savedCredentialsTest, &result)
	if !result.OK || result.Endpoint != upstream.URL+"/dav/reflow-library.json" {
		t.Fatalf("unexpected connection result: %+v", result)
	}

	unsavedTest := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/test", map[string]any{
		"provider": "webdav", "endpoint": upstream.URL + "/dav/",
		"credentials":           map[string]string{"username": "nutstore@example.com", "password": "saved-app-password"},
		"allow_private_network": true,
	})
	if unsavedTest.StatusCode != http.StatusOK {
		t.Fatalf("unsaved WebDAV test returned %d: %s", unsavedTest.StatusCode, readBody(t, unsavedTest))
	}
	_ = readBody(t, unsavedTest)

	updatedPassword := "updated-app-password"
	updateResponse := requestJSON(t, http.MethodPatch, httpServer.URL+"/api/v1/sync/accounts/"+account.ID, map[string]any{
		"credentials": map[string]string{"password": updatedPassword},
	})
	if updateResponse.StatusCode != http.StatusOK {
		t.Fatalf("update WebDAV application password returned %d: %s", updateResponse.StatusCode, readBody(t, updateResponse))
	}
	_ = readBody(t, updateResponse)
	expectedPassword = updatedPassword
	updatedCredentialsTest := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/test", map[string]any{
		"account_id": account.ID,
	})
	if updatedCredentialsTest.StatusCode != http.StatusOK {
		t.Fatalf("password-only update did not preserve username: %d %s", updatedCredentialsTest.StatusCode, readBody(t, updatedCredentialsTest))
	}
	_ = readBody(t, updatedCredentialsTest)

	accountsResponse, err := http.Get(httpServer.URL + "/api/v1/sync/accounts")
	if err != nil {
		t.Fatal(err)
	}
	var accounts struct {
		Items []json.RawMessage `json:"items"`
	}
	decodeResponse(t, accountsResponse, &accounts)
	if len(accounts.Items) != 1 {
		t.Fatalf("connection test persisted an account: %d accounts", len(accounts.Items))
	}
	if requests != 10 {
		t.Fatalf("expected one failed authentication probe and three write checks, got %d requests", requests)
	}
}

func TestWebDAVConnectionTestRejectsInvalidEndpoint(t *testing.T) {
	_, httpServer := newSyncTestServer(t, nil)
	response := requestJSON(t, http.MethodPost, httpServer.URL+"/api/v1/sync/accounts/test", map[string]any{
		"provider": "webdav", "endpoint": "not-a-webdav-url",
	})
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid WebDAV endpoint returned %d: %s", response.StatusCode, readBody(t, response))
	}
	if body := readBody(t, response); !strings.Contains(body, `"code":"connection_test_failed"`) {
		t.Fatalf("invalid WebDAV endpoint returned unexpected problem: %s", body)
	}
}

func newSyncTestServer(t *testing.T, fetcher *feedcore.Fetcher) (*Server, *httptest.Server) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	server := NewWithFetcher(db, slog.New(slog.NewTextHandler(io.Discard, nil)), "", fetcher)
	server.ConfigureSync(box)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return server, httpServer
}
