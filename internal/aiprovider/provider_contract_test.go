package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicMessageDoesNotExposeProviderBody(t *testing.T) {
	message := PublicMessage(&Error{
		Code: "http_error", StatusCode: http.StatusBadGateway,
		Err: errors.New("internal prompt and metadata-service body"),
	})
	if message != "The AI provider returned HTTP 502." {
		t.Fatalf("unexpected public message: %q", message)
	}
}

func TestOpenAICompatibleContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected authorization header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "gpt-test" || body["tools"] != nil {
			t.Errorf("unexpected OpenAI request: %+v", body)
		}
		serveAIFixture(t, w, "openai_chat.json")
	}))
	defer server.Close()
	provider, err := New("openai_compatible", server.URL+"/v1", "gpt-test", "test-key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "Summarize"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "A concise cached-ready summary." || response.Usage.TotalTokens != 138 || response.Usage.InputTokens != 120 {
		t.Fatalf("unexpected OpenAI response: %+v", response)
	}
}

func TestOllamaContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Tools  any    `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "qwen3:8b" || body.Stream || body.Tools != nil {
			t.Errorf("unexpected Ollama request: %+v", body)
		}
		serveAIFixture(t, w, "ollama_chat.json")
	}))
	defer server.Close()
	provider, err := New("ollama", server.URL, "qwen3:8b", "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "Extract key points"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "Three local key points." || response.Usage.TotalTokens != 102 || response.Usage.OutputTokens != 12 {
		t.Fatalf("unexpected Ollama response: %+v", response)
	}
}

func TestProviderErrorClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
	}))
	defer server.Close()
	provider, err := New("openai_compatible", server.URL, "model", "key", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "test"}}})
	if err == nil || ErrorCode(err) != "rate_limited" || !Retryable(err) {
		t.Fatalf("unexpected classified error: %#v", err)
	}
}

func serveAIFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, string(body))
}
