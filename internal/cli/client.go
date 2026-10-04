package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// defaultHost mirrors the server's own bind address, so `reflow` needs no setup
// next to a locally running ReFlow Server.
const defaultHost = "127.0.0.1:7381"

func defaultBaseURL() string { return "http://" + defaultHost }

func readEnv(name string) string { return os.Getenv(name) }

// TransportError is a call that never produced a server answer: unreachable,
// timed out, or a body that is not the JSON the endpoint documents.
type TransportError struct{ Reason string }

func (e *TransportError) Error() string { return e.Reason }

// APIError is a rejected call. It keeps the server's problem document intact so
// an agent can branch on `code` rather than re-reading prose out of a status line.
type APIError struct {
	Method    string
	Path      string
	Status    int
	Code      string
	Title     string
	Detail    string
	RequestID string
	// Hint names the fix for the handful of rejections that are configuration
	// rather than a bad request: an agent can act on it without reading the docs.
	Hint string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Detail != "" {
		return fmt.Sprintf("%s %s: %d %s: %s", e.Method, e.Path, e.Status, e.Code, e.Detail)
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, e.Detail)
	}
	return fmt.Sprintf("%s %s: %d", e.Method, e.Path, e.Status)
}

// Client is the ReFlow Server HTTP surface one command talks through.
type Client struct {
	endpoint *url.URL
	token    string
	http     *http.Client
	// err holds a bad --url, surfaced on the first call rather than at startup,
	// because `reflow --url nonsense describe` should still print the catalog.
	err error
}

func newClient(opts *globals) *Client {
	out := &Client{http: &http.Client{Timeout: opts.timeout}, token: opts.token}
	base := strings.TrimRight(strings.TrimSpace(opts.url), "/")
	if base == "" {
		out.err = usage("--url is empty")
		return out
	}
	parsed, err := url.Parse(base)
	if err != nil {
		out.err = usage("invalid --url %q: %v", base, err)
		return out
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		out.err = usage("--url must be an http or https URL, got %q", base)
		return out
	}
	if parsed.Host == "" {
		out.err = usage("--url %q has no host", base)
		return out
	}
	out.endpoint = parsed
	return out
}

// Get reads one endpoint and hands back the untouched response body.
func (c *Client) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.Call(ctx, http.MethodGet, path, query, nil)
}

// GetInto reads one endpoint and decodes its body into out.
func (c *Client) GetInto(ctx context.Context, path string, query url.Values, out any) error {
	raw, err := c.Get(ctx, path, query)
	if err != nil {
		return err
	}
	return decode(raw, path, out)
}

// Call performs one request. A nil body sends no payload, and 204 returns nil.
func (c *Client) Call(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	if c.err != nil {
		return nil, c.err
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, &TransportError{Reason: fmt.Sprintf("encode request for %s %s: %v", method, path, err)}
		}
		payload = bytes.NewReader(encoded)
	}
	target := *c.endpoint
	target.Path = strings.TrimSuffix(c.endpoint.Path, "/") + path
	if len(query) > 0 {
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), payload)
	if err != nil {
		return nil, &TransportError{Reason: fmt.Sprintf("build request: %v", err)}
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	// A loopback peer needs no token: the server treats it as the machine's own
	// traffic. Off-machine calls run against REFLOW_LAN_MODE=true and must carry a
	// paired device token.
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, &TransportError{
			Reason: fmt.Sprintf("%s %s: %v (is ReFlow Server running, and does --url point at it?)", method, path, err),
		}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, &TransportError{Reason: fmt.Sprintf("%s %s: read response: %v", method, path, err)}
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, problemError(method, path, response, raw)
	}
	if response.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" &&
		!strings.Contains(contentType, "json") {
		return nil, &TransportError{
			Reason: fmt.Sprintf("%s %s: expected JSON, got content type %q", method, path, contentType),
		}
	}
	return json.RawMessage(bytes.TrimSpace(raw)), nil
}

// CallInto performs one request and decodes the answer into out.
func (c *Client) CallInto(ctx context.Context, method, path string, query url.Values, body, out any) error {
	raw, err := c.Call(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	return decode(raw, path, out)
}

func problemError(method, path string, response *http.Response, raw []byte) error {
	out := &APIError{Method: method, Path: path, Status: response.StatusCode}
	var problem struct {
		Title     string `json:"title"`
		Detail    string `json:"detail"`
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(raw, &problem); err == nil && (problem.Code != "" || problem.Detail != "") {
		out.Code = problem.Code
		out.Title = problem.Title
		out.Detail = problem.Detail
		out.RequestID = problem.RequestID
		out.Hint = problemHint(out.Code)
		return out
	}
	// A proxy or a non-API listener can answer with a body that is not a problem
	// document; keep a slice of it rather than dropping the only evidence.
	snippet := strings.TrimSpace(string(raw))
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	out.Detail = snippet
	return out
}

// problemHint covers the rejections whose cause is how the CLI was pointed at the
// server, not what it asked for.
func problemHint(code string) string {
	switch code {
	case "authentication_required", "invalid_device_token":
		return "run `reflow device pair --code ...` and pass the token with --token or REFLOW_TOKEN; " +
			"a loopback call to 127.0.0.1 needs no token at all"
	case "host_not_allowed":
		return "the loopback listener only answers requests addressed to a loopback host; check --url"
	case "origin_not_allowed":
		return "the CLI sends no Origin header; this answer means something other than reflow hit the API"
	case "ai_unavailable":
		return "this server has no credential encryption configured, so AI endpoints are off"
	default:
		return ""
	}
}

func decode(raw json.RawMessage, path string, out any) error {
	if out == nil {
		return nil
	}
	if len(raw) == 0 {
		return &TransportError{Reason: fmt.Sprintf("%s returned no body to decode", path)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &TransportError{Reason: fmt.Sprintf("decode %s: %v", path, err)}
	}
	return nil
}
