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

	"github.com/Zijinn/ReFlow/internal/storage"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
}

func TestHealth(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newTestServer(t).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request ID header")
	}
}

func TestStatusReportsDatabaseReady(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	newTestServer(t).Handler().ServeHTTP(recorder, request)

	var body struct {
		Status        string `json:"status"`
		DatabaseReady bool   `json:"database_ready"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ready" || !body.DatabaseReady {
		t.Fatalf("unexpected status: %+v", body)
	}
}

func TestPairRejectsOversizedJSONBody(t *testing.T) {
	response := httptest.NewRecorder()
	body := `{"code":"` + strings.Repeat("x", maxJSONBodyBytes) + `","name":"iPad","platform":"ipad"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", strings.NewReader(body))
	newTestServer(t).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", response.Code, response.Body.String())
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "request_body_too_large" {
		t.Fatalf("unexpected problem code %q", problem.Code)
	}
}

func TestPairKeepsInvalidPairingCodeForMalformedJSON(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", strings.NewReader(`{"code":`))
	newTestServer(t).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "invalid_pairing_request" {
		t.Fatalf("unexpected problem code %q", problem.Code)
	}
}

func TestOPMLImportRejectsBodyOverLimit(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/opml", strings.NewReader(strings.Repeat("x", maxOPMLBodyBytes+1)))
	newTestServer(t).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRestoreRejectsOversizedJSONBody(t *testing.T) {
	response := httptest.NewRecorder()
	body := io.MultiReader(strings.NewReader(`{"padding":"`), io.LimitReader(zeroReader{}, maxBackupBodyBytes), strings.NewReader(`"}`))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/restore", body)
	newTestServer(t).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", response.Code, response.Body.String())
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 'x'
	}
	return len(buffer), nil
}

func TestUnknownAPIRouteUsesProblemJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil)
	newTestServer(t).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/problem+json; charset=utf-8" {
		t.Fatalf("unexpected content type %q", got)
	}
}
