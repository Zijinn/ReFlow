package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestLANModeRequiresPairedDeviceToken(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(true, nil, nil)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	request.RemoteAddr = "192.168.1.20:40000"
	server.Handler().ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected remote request to require auth, got %d", unauthorized.Code)
	}

	codeResponse := httptest.NewRecorder()
	codeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pairing-code", nil)
	codeRequest.RemoteAddr = "127.0.0.1:40001"
	codeRequest.Host = "127.0.0.1:7381"
	server.Handler().ServeHTTP(codeResponse, codeRequest)
	if codeResponse.Code != http.StatusCreated {
		t.Fatalf("create pairing code: %d %s", codeResponse.Code, codeResponse.Body.String())
	}
	var codeBody struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(codeResponse.Body).Decode(&codeBody); err != nil || codeBody.Code == "" {
		t.Fatalf("decode pairing code: %+v, %v", codeBody, err)
	}

	pairBody, _ := json.Marshal(map[string]string{"code": codeBody.Code, "name": "iPad", "platform": "ipad"})
	pairResponse := httptest.NewRecorder()
	pairRequest := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", bytes.NewReader(pairBody))
	pairRequest.RemoteAddr = "192.168.1.20:40002"
	server.Handler().ServeHTTP(pairResponse, pairRequest)
	if pairResponse.Code != http.StatusCreated {
		t.Fatalf("pair device: %d %s", pairResponse.Code, pairResponse.Body.String())
	}
	var paired struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(pairResponse.Body).Decode(&paired); err != nil || paired.Token == "" {
		t.Fatalf("decode paired token: %+v, %v", paired, err)
	}

	authorized := httptest.NewRecorder()
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	authorizedRequest.RemoteAddr = "192.168.1.20:40003"
	authorizedRequest.Header.Set("Authorization", "Bearer "+paired.Token)
	server.Handler().ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("paired request failed: %d %s", authorized.Code, authorized.Body.String())
	}
}

func TestOriginValidationRejectsUntrustedWebsites(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(true, []string{"https://reader.example"}, nil)
	blocked := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	request.RemoteAddr = "127.0.0.1:40000"
	request.Header.Set("Origin", "https://evil.example")
	server.Handler().ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("expected untrusted origin to be rejected, got %d", blocked.Code)
	}
	allowed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodOptions, "/api/v1/feeds", nil)
	request.RemoteAddr = "192.168.1.20:40001"
	request.Header.Set("Origin", "https://reader.example")
	server.Handler().ServeHTTP(allowed, request)
	if allowed.Code != http.StatusNoContent || allowed.Header().Get("Access-Control-Allow-Origin") != "https://reader.example" {
		t.Fatalf("expected allowed preflight, got %d %+v", allowed.Code, allowed.Header())
	}
}

func TestWailsShellOriginCanCreateQuickTasks(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(false, nil, nil)

	for _, origin := range []string{"wails://localhost", "http://wails.localhost", "https://wails.localhost"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/quick",
			strings.NewReader(`{"title":"from desktop"}`))
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Wails-Window-ID", "main")
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("expected %s accepted for the Wails shell, got %d: %s", origin, recorder.Code, recorder.Body.String())
		}
	}

	blocked := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/quick",
		strings.NewReader(`{"title":"without shell header"}`))
	request.Header.Set("Origin", "wails://localhost")
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("expected a Wails origin without the shell header to be rejected, got %d", blocked.Code)
	}

	blocked = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/tasks/quick",
		strings.NewReader(`{"title":"from web"}`))
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Wails-Window-ID", "main")
	server.Handler().ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("expected the shell header not to admit a web origin, got %d", blocked.Code)
	}
}

func TestTrustedProxyIngressAllowsExternalHostButRequiresToken(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(true, nil, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	request.RemoteAddr = "127.0.0.1:42000"
	request.Host = "reader.example"
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("trusted proxy protected API: expected 401, got %d: %s", response.Code, response.Body.String())
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	statusRequest.RemoteAddr = "127.0.0.1:42001"
	statusRequest.Host = "reader.example"
	statusResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusResponse, statusRequest)
	var status struct {
		AuthRequired bool `json:"device_auth_required"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if statusResponse.Code != http.StatusOK || !status.AuthRequired {
		t.Fatalf("proxy status classification mismatch: code=%d body=%+v", statusResponse.Code, status)
	}
}

func TestTrustedProxyOnlyModeRequiresToken(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(false, nil, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	request.RemoteAddr = "127.0.0.1:42500"
	request.Host = "reader.example"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("proxy-only mode protected API: expected 401, got %d", response.Code)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	statusRequest.RemoteAddr = "127.0.0.1:42501"
	statusRequest.Host = "reader.example"
	statusResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusResponse, statusRequest)
	var status struct {
		AuthRequired bool `json:"device_auth_required"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if !status.AuthRequired {
		t.Fatal("proxy-only status must report device auth required")
	}
}

func TestProxyOnlyStartCreatesPairingCode(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(false, nil, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := server.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pairing_codes WHERE used_at IS NULL").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("proxy-only startup created %d pairing codes, want 1", count)
	}
}

func TestForwardedForDoesNotChangeAuthorizationClassification(t *testing.T) {
	server := newTestServer(t)
	server.ConfigureSecurity(true, nil, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	request.RemoteAddr = "192.168.1.20:43000"
	request.Header.Set("X-Forwarded-For", "127.0.0.1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("X-Forwarded-For must not grant loopback trust, got %d", response.Code)
	}
}

func TestLoopbackListenerRejectsForeignHostHeaders(t *testing.T) {
	server := newTestServer(t)

	for _, host := range []string{"evil.example", "evil.example:7381", "192.168.1.10:7381", "10.0.0.5"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
		request.RemoteAddr = "127.0.0.1:41000"
		request.Host = host
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("host %q from loopback peer: expected 403, got %d", host, response.Code)
		}
	}

	for _, host := range []string{"127.0.0.1:7381", "localhost:7381", "[::1]:7381", "localhost"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		request.RemoteAddr = "127.0.0.1:41001"
		request.Host = host
		server.Handler().ServeHTTP(response, request)
		if response.Code == http.StatusForbidden {
			t.Fatalf("loopback host %q should be accepted, got %d", host, response.Code)
		}
	}
}

func TestPairRateLimit(t *testing.T) {
	handler := newTestServer(t).Handler()
	body := `{"code":"WRONG","name":"iPad","platform":"ipad"}`
	for attempt := 0; attempt < 10; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400, got %d", attempt, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated pairing attempts, got %d", response.Code)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header")
	}
}

func TestSecurityHeadersKeepCSPForWebClients(t *testing.T) {
	handler := (&Server{}).securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected web requests to retain the content security policy")
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("expected content type protection to remain enabled")
	}
}

func TestSecurityHeadersAllowWailsResources(t *testing.T) {
	handler := (&Server{}).securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Wails-Window-ID", "main")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	csp := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "wails://") || !strings.Contains(csp, "form-action 'none'") {
		t.Fatalf("expected a Wails-aware CSP, got %q", csp)
	}
	if response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("expected framing to remain denied")
	}
	if response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("expected non-CSP security headers to remain enabled")
	}
	if response.Header().Get("Permissions-Policy") == "" {
		t.Fatal("expected permissions policy to remain enabled")
	}
}
