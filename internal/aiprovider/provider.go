package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	feedcore "github.com/Zijinn/ReFlow/internal/feed"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model       string
	Messages    []Message
	Temperature *float64
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type Response struct {
	Content string
	Usage   Usage
}

type Provider interface {
	Complete(ctx context.Context, request Request) (Response, error)
}

type Error struct {
	Code       string
	StatusCode int
	Retryable  bool
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s (HTTP %d): %v", e.Code, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func ErrorCode(err error) string {
	var providerError *Error
	if errors.As(err, &providerError) {
		return providerError.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "ai_failed"
}

func Retryable(err error) bool {
	var providerError *Error
	return errors.As(err, &providerError) && providerError.Retryable
}

func New(provider, endpoint, model, apiKey string, client *http.Client) (Provider, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("AI endpoint must be an HTTP or HTTPS URL without embedded credentials")
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("AI model is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	base := baseClient{endpoint: endpoint, model: strings.TrimSpace(model), apiKey: strings.TrimSpace(apiKey), http: client}
	switch provider {
	case "openai_compatible":
		return &openAIProvider{baseClient: base}, nil
	case "ollama":
		return &ollamaProvider{baseClient: base}, nil
	default:
		return nil, fmt.Errorf("unsupported AI provider %q", provider)
	}
}

func SecureHTTPClient(policy feedcore.URLPolicy) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = policy.DialContext
	transport.ResponseHeaderTimeout = 20 * time.Second
	return &http.Client{
		Timeout:   90 * time.Second,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			return policy.Validate(request.Context(), request.URL)
		},
	}
}

type baseClient struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

func (c baseClient) postJSON(ctx context.Context, route string, requestBody, responseBody any) error {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode AI request: %w", err)
	}
	target := c.endpoint
	if route != "" && !strings.HasSuffix(target, route) {
		target += route
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &Error{Code: "network_error", Retryable: !errors.Is(err, context.Canceled), Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		cause := errors.New(strings.TrimSpace(string(message)))
		if cause.Error() == "" {
			cause = errors.New(http.StatusText(response.StatusCode))
		}
		code := "http_error"
		retryable := response.StatusCode >= 500
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			code = "authentication_error"
		} else if response.StatusCode == http.StatusTooManyRequests {
			code = "rate_limited"
			retryable = true
		}
		return &Error{Code: code, StatusCode: response.StatusCode, Retryable: retryable, Err: cause}
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 10<<20))
	if err := decoder.Decode(responseBody); err != nil {
		return &Error{Code: "invalid_response", Err: err}
	}
	return nil
}
