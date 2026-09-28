package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
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

func TestAIServicePrivacyEncryptionCachingChatAndUsage(t *testing.T) {
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
	entryID := createAIServiceTestEntry(t, db)

	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer encrypted-test-key" {
			t.Errorf("missing API key")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["tools"] != nil {
			t.Errorf("AI request unexpectedly contains tools")
		}
		body, _ := json.Marshal(request["messages"])
		if !bytes.Contains(body, []byte("ReFlow article body")) {
			t.Errorf("article context missing: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Read-only answer"}}],"usage":{"prompt_tokens":40,"completion_tokens":6,"total_tokens":46}}`)
	}))
	defer upstream.Close()

	service := newAIService(db, box, func(bool) *http.Client { return upstream.Client() })
	unapproved, err := service.CreateProfile(ctx, AIProfileInput{
		Provider: "openai_compatible", Name: "Remote unapproved", Endpoint: "https://api.example.test/v1",
		Model: "model", APIKey: "secret", RemoteContentApproved: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.PrepareOperation(ctx, entryID, unapproved.ID, "summary", "English"); !errors.Is(err, ErrAIPrivacyApprovalRequired) {
		t.Fatalf("expected privacy approval error, got %v", err)
	}

	profile, err := service.CreateProfile(ctx, AIProfileInput{
		Provider: "openai_compatible", Name: "Local fixture", Endpoint: upstream.URL + "/v1",
		Model: "model", APIKey: "encrypted-test-key", AllowPrivateNetwork: true, IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := storage.GetAIProfileRecord(ctx, db, domain.DefaultProfileID, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(record.EncryptedAPIKey, []byte("encrypted-test-key")) {
		t.Fatal("AI API key was stored in plaintext")
	}

	cached, payload, err := service.PrepareOperation(ctx, entryID, profile.ID, "summary", "English")
	if err != nil || cached != nil {
		t.Fatalf("prepare operation: cached=%v err=%v", cached, err)
	}
	job, err := storage.CreateJob(ctx, db, "ai.operation", payload, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunOperation(ctx, job.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultText != "Read-only answer" || calls.Load() != 1 {
		t.Fatalf("unexpected AI result: %+v calls=%d", result, calls.Load())
	}
	cached, _, err = service.PrepareOperation(ctx, entryID, profile.ID, "summary", "English")
	if err != nil || cached == nil || cached.ID != result.ID || calls.Load() != 1 {
		t.Fatalf("cache miss: cached=%+v err=%v calls=%d", cached, err, calls.Load())
	}

	session, chatPayload, err := service.PrepareChat(ctx, entryID, profile.ID, "", "What is the article about?")
	if err != nil {
		t.Fatal(err)
	}
	chatJob, err := storage.CreateJob(ctx, db, "ai.chat", chatPayload, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.RunChat(ctx, chatJob.ID, chatPayload)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Messages) != 2 || session.Messages[0].Role != "user" || session.Messages[1].Role != "assistant" {
		t.Fatalf("unexpected chat session: %+v", session)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected two provider calls, got %d", calls.Load())
	}
	usage, err := service.UsageTotals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 80 || usage.OutputTokens != 12 || usage.TotalTokens != 92 {
		t.Fatalf("unexpected usage totals: %+v", usage)
	}
}

func TestTitleTranslationSendsOnlyTheTitleAndUsesTitleCacheKey(t *testing.T) {
	record := storage.AIProfileRecord{Profile: domain.AIProfile{
		ID: "translation-profile", Provider: "ollama", Endpoint: "http://127.0.0.1:11434", Model: "qwen3:8b",
	}}
	first := storage.AIEntryContent{Title: "A precise title", CanonicalURL: "https://example.com/one", Content: "private article body one"}
	second := storage.AIEntryContent{Title: "A precise title", CanonicalURL: "https://example.com/two", Content: "private article body two"}
	messages := operationMessages("title_translation", "Chinese", first)
	if len(messages) != 2 || !strings.Contains(messages[1].Content, first.Title) || strings.Contains(messages[1].Content, first.Content) || strings.Contains(messages[1].Content, first.CanonicalURL) {
		t.Fatalf("title translation envelope included more than the title: %+v", messages)
	}
	if firstHash, secondHash := aiInputHash(record, "title_translation", "Chinese", first), aiInputHash(record, "title_translation", "Chinese", second); firstHash != secondHash {
		t.Fatalf("title-only cache key changed with article content: %s %s", firstHash, secondHash)
	}
	operation, language, err := validateAIOperation("title_translation", "Chinese")
	if err != nil || operation != "title_translation" || language != "Chinese" {
		t.Fatalf("unexpected title translation validation: %q %q %v", operation, language, err)
	}
}

func TestAcademicTagsUseTitleAndAbstractAndParseStructuredOutput(t *testing.T) {
	record := storage.AIProfileRecord{Profile: domain.AIProfile{
		ID: "tag-profile", Provider: "ollama", Endpoint: "http://127.0.0.1:11434", Model: "qwen3:8b",
	}}
	first := storage.AIEntryContent{Title: "Digital trade and network centrality", CanonicalURL: "https://example.com/one", Content: "abstract body one"}
	second := storage.AIEntryContent{Title: first.Title, CanonicalURL: "https://example.com/two", Content: "abstract body two"}
	messages := operationMessages("academic_tags", "Chinese", first)
	// The abstract carries the discipline and method signals; the URL stays out.
	if len(messages) != 2 || !strings.Contains(messages[1].Content, first.Title) || !strings.Contains(messages[1].Content, first.Content) {
		t.Fatalf("academic tag envelope missing title or abstract: %+v", messages)
	}
	if strings.Contains(messages[1].Content, first.CanonicalURL) {
		t.Fatalf("academic tag envelope leaked the canonical URL: %+v", messages)
	}
	// The cache key now tracks the abstract, so differing abstracts must not
	// share cached tags while an identical one still hits the cache.
	if firstHash, secondHash := aiInputHash(record, "academic_tags", "Chinese", first), aiInputHash(record, "academic_tags", "Chinese", second); firstHash == secondHash {
		t.Fatalf("tag cache key ignored a changed abstract: %s", firstHash)
	}
	sameAbstract := storage.AIEntryContent{Title: first.Title, CanonicalURL: "https://example.com/three", Content: first.Content}
	if firstHash, sameHash := aiInputHash(record, "academic_tags", "Chinese", first), aiInputHash(record, "academic_tags", "Chinese", sameAbstract); firstHash != sameHash {
		t.Fatalf("tag cache key changed with the canonical URL: %s %s", firstHash, sameHash)
	}
	tags, err := parseAcademicTags("```json\n[\"Digital trade\", \"Network analysis\", \"digital trade\", \"China\", \"Panel data\", \"Extra\"]\n```")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Digital trade", "Network analysis", "China", "Panel data", "Extra"}
	if len(tags) != len(want) {
		t.Fatalf("unexpected tags: %#v", tags)
	}
	for index := range want {
		if tags[index] != want[index] {
			t.Fatalf("unexpected tags: %#v", tags)
		}
	}
	if _, err := parseAcademicTags("not json"); err == nil {
		t.Fatal("expected invalid academic tag response to fail")
	}
}

func createAIServiceTestEntry(t *testing.T, db *sql.DB) string {
	t.Helper()
	entryURL := "https://example.com/ai-entry"
	guid := "ai-entry-guid"
	feed, err := storage.SaveNewFeed(context.Background(), db, domain.DefaultProfileID,
		"https://example.com/ai.xml", "https://example.com/ai.xml",
		domain.ParsedFeed{Title: "AI feed", Format: "rss", Entries: []domain.ParsedEntry{{
			GUID: &guid, CanonicalURL: &entryURL, Title: "AI article",
			PublishedAt: time.Now().UTC(), ContentHash: "ai-entry-hash",
			SanitizedHTML: "<p>ReFlow article body</p>", PlainText: "ReFlow article body with facts.",
		}}}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := storage.ListEntries(context.Background(), db, domain.EntryFilter{ProfileID: domain.DefaultProfileID, FeedID: feed.ID, Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list AI entry: %+v %v", page, err)
	}
	return page.Items[0].ID
}
