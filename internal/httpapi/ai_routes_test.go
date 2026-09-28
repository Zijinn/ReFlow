package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func TestAIAPIPrivacyCachingChatAndSecretBoundaries(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer api-route-secret" {
			t.Errorf("missing API key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"API answer"}}],"usage":{"prompt_tokens":30,"completion_tokens":5,"total_tokens":35}}`)
	}))
	defer provider.Close()

	db, apiServer := newAIAPITestServer(t)
	entryID := createAIAPITestEntry(t, db)

	unapprovedResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/profiles", map[string]any{
		"provider": "openai_compatible", "name": "Unapproved remote", "endpoint": "https://api.example.test/v1",
		"model": "model", "api_key": "secret", "remote_content_approved": false,
	})
	var unapproved struct {
		ID string `json:"id"`
	}
	decodeResponse(t, unapprovedResponse, &unapproved)
	privacyResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/entries/"+entryID+"/ai/summary", map[string]string{"profile_id": unapproved.ID, "language": "English"})
	if privacyResponse.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("expected privacy gate, got %d: %s", privacyResponse.StatusCode, readBody(t, privacyResponse))
	}
	privacyResponse.Body.Close()

	profileResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/profiles", map[string]any{
		"provider": "openai_compatible", "name": "Fixture AI", "endpoint": provider.URL + "/v1",
		"model": "fixture-model", "api_key": "api-route-secret", "allow_private_network": true,
		"remote_content_approved": true, "is_default": true,
	})
	profileBody := readBody(t, profileResponse)
	if strings.Contains(profileBody, "api-route-secret") || strings.Contains(profileBody, "api_key") {
		t.Fatalf("profile response exposed API key: %s", profileBody)
	}
	var profile struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(profileBody), &profile); err != nil || profile.ID == "" {
		t.Fatalf("decode profile: %v", err)
	}

	operationURL := apiServer.URL + "/api/v1/entries/" + entryID + "/ai/summary"
	operationResponse := requestJSON(t, http.MethodPost, operationURL, map[string]string{"profile_id": profile.ID, "language": "English"})
	if operationResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("start summary: %d %s", operationResponse.StatusCode, readBody(t, operationResponse))
	}
	var started struct {
		Job domain.Job `json:"job"`
	}
	decodeResponse(t, operationResponse, &started)
	waitForJobState(t, apiServer.URL, started.Job.ID, "succeeded")
	if calls.Load() != 1 {
		t.Fatalf("expected one provider call, got %d", calls.Load())
	}

	var payloadJSON string
	if err := db.QueryRowContext(context.Background(), "SELECT payload_json FROM jobs WHERE id = ?", started.Job.ID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloadJSON, "api-route-secret") || strings.Contains(payloadJSON, "ReFlow AI API body") {
		t.Fatalf("job payload contains sensitive input: %s", payloadJSON)
	}

	resultsResponse, err := http.Get(apiServer.URL + "/api/v1/entries/" + entryID + "/ai-results")
	if err != nil {
		t.Fatal(err)
	}
	resultsBody := readBody(t, resultsResponse)
	if !strings.Contains(resultsBody, "API answer") {
		t.Fatalf("missing AI result: %s", resultsBody)
	}

	cachedResponse := requestJSON(t, http.MethodPost, operationURL, map[string]string{"profile_id": profile.ID, "language": "English"})
	if cachedResponse.StatusCode != http.StatusOK {
		t.Fatalf("cached summary: %d %s", cachedResponse.StatusCode, readBody(t, cachedResponse))
	}
	cachedBody := readBody(t, cachedResponse)
	if !strings.Contains(cachedBody, `"cached":true`) || calls.Load() != 1 {
		t.Fatalf("cache did not short-circuit: %s calls=%d", cachedBody, calls.Load())
	}

	chatResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/entries/"+entryID+"/ai-chat", map[string]string{
		"profile_id": profile.ID, "message": "What matters?",
	})
	if chatResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("start chat: %d %s", chatResponse.StatusCode, readBody(t, chatResponse))
	}
	var chatStarted struct {
		Job     domain.Job           `json:"job"`
		Session domain.AIChatSession `json:"session"`
	}
	decodeResponse(t, chatResponse, &chatStarted)
	waitForJobState(t, apiServer.URL, chatStarted.Job.ID, "succeeded")
	chatDetail, err := http.Get(apiServer.URL + "/api/v1/ai/chats/" + chatStarted.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	chatBody := readBody(t, chatDetail)
	if !strings.Contains(chatBody, "What matters?") || !strings.Contains(chatBody, "API answer") {
		t.Fatalf("unexpected chat: %s", chatBody)
	}

	usageResponse, err := http.Get(apiServer.URL + "/api/v1/ai/usage")
	if err != nil {
		t.Fatal(err)
	}
	var usage domain.AIUsage
	decodeResponse(t, usageResponse, &usage)
	if usage.TotalTokens != 70 || calls.Load() != 2 {
		t.Fatalf("unexpected usage=%+v calls=%d", usage, calls.Load())
	}
}

func newAIAPITestServer(t *testing.T) (*sql.DB, *httptest.Server) {
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
	server := New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	server.ConfigureAI(box)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return db, httpServer
}

func createAIAPITestEntry(t *testing.T, db *sql.DB) string {
	t.Helper()
	entryURL, guid := "https://example.com/ai-api-entry", "ai-api-guid"
	feed, err := storage.SaveNewFeed(context.Background(), db, domain.DefaultProfileID,
		"https://example.com/ai-api.xml", "https://example.com/ai-api.xml",
		domain.ParsedFeed{Title: "AI API feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "AI API article", PublishedAt: time.Now().UTC(),
			ContentHash: "ai-api-hash", SanitizedHTML: "<p>ReFlow AI API body</p>", PlainText: "ReFlow AI API body",
		}}}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storage.ListEntries(context.Background(), db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: feed.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list AI API entry: %+v %v", page, err)
	}
	return page.Items[0].ID
}
