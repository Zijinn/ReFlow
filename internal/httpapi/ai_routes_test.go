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
	"github.com/Zijinn/ReFlow/internal/service"
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

func createAIAAPITestPaper(t *testing.T, db *sql.DB, paper domain.ResearchPaper) string {
	t.Helper()
	created, err := storage.CreateResearchPaper(context.Background(), db, domain.DefaultProfileID, paper)
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func TestAIPaperChatAndDailyDigestAPI(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		joined := ""
		for _, message := range request.Messages {
			joined += message.Content
		}
		if !strings.Contains(joined, "Digital yuan and CBDC") {
			t.Errorf("research prompt missing paper context: %s", joined)
		}
		// The digest prompt asks for the structured JSON document; a compliant
		// provider answers with `sections`, while the paper chat keeps the old
		// prose shape. Both must survive the same job/session transport.
		answer := "Focus on the overdue submission."
		if strings.Contains(joined, "Today's progress plan") {
			answer = `{"sections":[{"kicker":"Manuscripts · status","headline":"Focus on the overdue submission.","blocks":[{"type":"status-rows","items":[{"status":"stuck on data","title":"Digital yuan and CBDC","next":"finish the robustness table"}]}]}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		response, err := json.Marshal(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": answer}}},
			"usage":   map[string]int{"prompt_tokens": 25, "completion_tokens": 7, "total_tokens": 32},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(response)
	}))
	defer provider.Close()

	db, apiServer := newAIAPITestServer(t)
	profileResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/profiles", map[string]any{
		"provider": "openai_compatible", "name": "Research AI", "endpoint": provider.URL + "/v1",
		"model": "fixture-model", "api_key": "research-route-secret", "allow_private_network": true,
		"remote_content_approved": true, "is_default": true,
	})
	var profile struct {
		ID string `json:"id"`
	}
	decodeResponse(t, profileResponse, &profile)

	researchID := createAIAAPITestPaper(t, db, domain.ResearchPaper{
		Kind: domain.ResearchKindResearch, Title: "Digital yuan and CBDC", Priority: "high",
		Stages: []domain.ResearchStage{{Name: "Robustness checks", Done: false}},
	})

	// Paper-context chat: 202 with an ai.research job and a session with no entry id.
	chatResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/paper-chat", map[string]any{
		"profile_id": profile.ID, "paper_ids": []string{researchID}, "message": "What should I do first?",
	})
	if chatResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("paper chat: %d %s", chatResponse.StatusCode, readBody(t, chatResponse))
	}
	var chatStarted struct {
		Job     domain.Job           `json:"job"`
		Session domain.AIChatSession `json:"session"`
	}
	decodeResponse(t, chatResponse, &chatStarted)
	if chatStarted.Job.Kind != "ai.research" {
		t.Fatalf("paper chat job kind: %q", chatStarted.Job.Kind)
	}
	if chatStarted.Session.EntryID != nil {
		t.Fatalf("paper chat session should not bind an article")
	}
	waitForJobState(t, apiServer.URL, chatStarted.Job.ID, "succeeded")
	chatDetail, err := http.Get(apiServer.URL + "/api/v1/ai/chats/" + chatStarted.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	chatBody := readBody(t, chatDetail)
	if !strings.Contains(chatBody, "What should I do first?") || !strings.Contains(chatBody, "overdue submission") {
		t.Fatalf("unexpected paper chat: %s", chatBody)
	}

	// Daily digest: reads the whole workspace, returns the same job/session shape.
	digestResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/daily-digest", map[string]any{
		"profile_id": profile.ID, "language": "Simplified Chinese",
	})
	if digestResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("daily digest: %d %s", digestResponse.StatusCode, readBody(t, digestResponse))
	}
	var digestStarted struct {
		Job     domain.Job           `json:"job"`
		Session domain.AIChatSession `json:"session"`
	}
	decodeResponse(t, digestResponse, &digestStarted)
	if digestStarted.Job.Kind != "ai.research" {
		t.Fatalf("digest job kind: %q", digestStarted.Job.Kind)
	}
	waitForJobState(t, apiServer.URL, digestStarted.Job.ID, "succeeded")
	digestDetail, err := http.Get(apiServer.URL + "/api/v1/ai/chats/" + digestStarted.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	var digestSession domain.AIChatSession
	decodeResponse(t, digestDetail, &digestSession)
	// The digest session still holds exactly one assistant message; its content
	// is now the structured `sections` document (stored verbatim, unvalidated —
	// providers that answer in prose keep working through the client fallback).
	if len(digestSession.Messages) != 1 || digestSession.Messages[0].Role != "assistant" ||
		!strings.Contains(digestSession.Messages[0].Content, `"sections"`) ||
		!strings.Contains(digestSession.Messages[0].Content, "overdue submission") {
		t.Fatalf("unexpected digest session: %+v", digestSession.Messages)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two provider calls, got %d", calls.Load())
	}

	// The job payload must not carry the API key.
	var payloadJSON string
	if err := db.QueryRowContext(context.Background(), "SELECT payload_json FROM jobs WHERE id = ?", digestStarted.Job.ID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloadJSON, "research-route-secret") {
		t.Fatalf("research job payload leaked API key: %s", payloadJSON)
	}
}

func TestAIPaperChatAndDigestValidation(t *testing.T) {
	_, apiServer := newAIAPITestServer(t)
	profileResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/profiles", map[string]any{
		"provider": "openai_compatible", "name": "Validation AI", "endpoint": "http://127.0.0.1:1/v1",
		"model": "m", "api_key": "k", "allow_private_network": true, "remote_content_approved": true, "is_default": true,
	})
	var profile struct {
		ID string `json:"id"`
	}
	decodeResponse(t, profileResponse, &profile)

	// Unknown paper id resolves through the research store, so it is a 404.
	missing := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/paper-chat", map[string]any{
		"profile_id": profile.ID, "paper_ids": []string{"does-not-exist"}, "message": "hi",
	})
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown paper id: %d %s", missing.StatusCode, readBody(t, missing))
	}
	// Empty paper list is rejected before enqueue.
	empty := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/paper-chat", map[string]any{
		"profile_id": profile.ID, "paper_ids": []string{}, "message": "hi",
	})
	if empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty paper list: %d %s", empty.StatusCode, readBody(t, empty))
	}
	// Digest over an empty workspace is a 400, not a 500.
	digest := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/daily-digest", map[string]any{"profile_id": profile.ID})
	if digest.StatusCode != http.StatusBadRequest {
		t.Fatalf("digest with no papers: %d %s", digest.StatusCode, readBody(t, digest))
	}
}

func TestAIMetadataFillAPI(t *testing.T) {
	var calls atomic.Int32
	answer := `{"choices":[{"message":{"role":"assistant","content":"{\"title\":\"Digital trade\",\"authors\":[\"Smith J\"],\"journal\":\"Journal of Trade\",\"year\":\"2026\",\"volume\":\"42\",\"issue\":\"3\",\"pages\":\"12-25\",\"doi\":\"https://doi.org/10.1234/xyz\"}"}}],"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}}`
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		joined := ""
		for _, message := range request.Messages {
			joined += message.Content
		}
		if !strings.Contains(joined, "Digital trade rules[J]") {
			t.Errorf("metadata fill prompt missing the pasted reference: %s", joined)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, answer)
	}))
	defer provider.Close()

	_, apiServer := newAIAPITestServer(t)
	profileResponse := requestJSON(t, http.MethodPost, apiServer.URL+"/api/v1/ai/profiles", map[string]any{
		"provider": "openai_compatible", "name": "Metadata AI", "endpoint": provider.URL + "/v1",
		"model": "fixture-model", "api_key": "metadata-route-secret", "allow_private_network": true,
		"remote_content_approved": true, "is_default": true,
	})
	var profile struct {
		ID string `json:"id"`
	}
	decodeResponse(t, profileResponse, &profile)

	fillURL := apiServer.URL + "/api/v1/ai/metadata-fill"
	raw := "Smith J. Digital trade rules[J]. Journal of Trade, 2026, 42(3): 12-25."

	// Happy path: synchronous 200 with whitelisted, normalized fields.
	okResponse := requestJSON(t, http.MethodPost, fillURL, map[string]any{"profile_id": profile.ID, "raw": raw})
	if okResponse.StatusCode != http.StatusOK {
		t.Fatalf("metadata fill: %d %s", okResponse.StatusCode, readBody(t, okResponse))
	}
	var filled service.AIMetadataFill
	decodeResponse(t, okResponse, &filled)
	if filled.Title != "Digital trade" || filled.Journal != "Journal of Trade" || filled.Year != "2026" ||
		filled.Volume != "42" || filled.Issue != "3" || filled.Pages != "12-25" || filled.DOI != "10.1234/xyz" ||
		len(filled.Authors) != 1 || filled.Authors[0] != "Smith J" {
		t.Fatalf("unexpected fill payload: %+v", filled)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one provider call, got %d", calls.Load())
	}

	// Missing raw is a 400 before any provider call.
	missing := requestJSON(t, http.MethodPost, fillURL, map[string]any{"profile_id": profile.ID})
	if missing.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing raw: %d %s", missing.StatusCode, readBody(t, missing))
	}
	// Unknown profile is a 404.
	unknown := requestJSON(t, http.MethodPost, fillURL, map[string]any{"profile_id": "does-not-exist", "raw": raw})
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown profile: %d %s", unknown.StatusCode, readBody(t, unknown))
	}
	if calls.Load() != 1 {
		t.Fatalf("validation reached the provider: calls=%d", calls.Load())
	}

	// A provider answer without usable fields is a stable 422.
	answer = `{"choices":[{"message":{"role":"assistant","content":"I cannot help with that."}}],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}`
	unparseable := requestJSON(t, http.MethodPost, fillURL, map[string]any{"profile_id": profile.ID, "raw": raw})
	if unparseable.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unparseable answer: %d %s", unparseable.StatusCode, readBody(t, unparseable))
	}
	unparseableBody := readBody(t, unparseable)
	if !strings.Contains(unparseableBody, "ai_metadata_unparseable") {
		t.Fatalf("422 body missing the stable code: %s", unparseableBody)
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
