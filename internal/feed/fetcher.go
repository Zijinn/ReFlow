package feed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

const (
	DefaultMaxResponseBytes int64 = 10 << 20
	DefaultFetchTimeout           = 300 * time.Second
	defaultUserAgent              = "ReFlow/0.1 (+https://github.com/Zijinn/ReFlow)"
)

type Validators struct {
	ETag         *string
	LastModified *string
}

type FetchResult struct {
	URL          string
	ContentType  string
	Body         []byte
	ETag         *string
	LastModified *string
	NotModified  bool
}

type FetchError struct {
	Code       string
	StatusCode int
	Temporary  bool
	Err        error
}

func (e *FetchError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s (HTTP %d): %v", e.Code, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *FetchError) Unwrap() error { return e.Err }

type Fetcher struct {
	Policy           URLPolicy
	Timeout          time.Duration
	MaxResponseBytes int64
	UserAgent        string
}

func NewFetcher() *Fetcher {
	return &Fetcher{
		Policy:           DefaultURLPolicy(),
		Timeout:          DefaultFetchTimeout,
		MaxResponseBytes: DefaultMaxResponseBytes,
		UserAgent:        defaultUserAgent,
	}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string, validators Validators) (FetchResult, error) {
	normalized, err := NormalizeURL(rawURL)
	if err != nil {
		return FetchResult{}, &FetchError{Code: "invalid_url", Err: err}
	}
	target, _ := url.Parse(normalized)
	if err := f.Policy.Validate(ctx, target); err != nil {
		return FetchResult{}, &FetchError{Code: "blocked_address", Err: err}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = f.Policy.DialContext
	transport.MaxIdleConns = 20
	transport.MaxIdleConnsPerHost = 2
	transport.ResponseHeaderTimeout = DefaultFetchTimeout
	defer transport.CloseIdleConnections()

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = DefaultFetchTimeout
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			return f.Policy.Validate(request.Context(), request.URL)
		},
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return FetchResult{}, &FetchError{Code: "invalid_url", Err: err}
	}
	request.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/feed+json, application/json;q=0.9, application/xml;q=0.8, text/xml;q=0.8, text/html;q=0.6, */*;q=0.1")
	request.Header.Set("User-Agent", valueOr(f.UserAgent, defaultUserAgent))
	if validators.ETag != nil && *validators.ETag != "" {
		request.Header.Set("If-None-Match", *validators.ETag)
	}
	if validators.LastModified != nil && *validators.LastModified != "" {
		request.Header.Set("If-Modified-Since", *validators.LastModified)
	}

	response, err := client.Do(request)
	if err != nil {
		code := "network_error"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = "timeout"
		}
		if errors.Is(err, ErrBlockedAddress) {
			code = "blocked_address"
		}
		return FetchResult{}, &FetchError{Code: code, Temporary: code != "blocked_address", Err: err}
	}
	defer response.Body.Close()

	result := FetchResult{
		URL:          response.Request.URL.String(),
		ContentType:  response.Header.Get("Content-Type"),
		ETag:         headerPointer(response.Header.Get("ETag")),
		LastModified: headerPointer(response.Header.Get("Last-Modified")),
	}
	if response.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		temporary := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		code := "http_error"
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			code = "authentication_error"
		} else if response.StatusCode == http.StatusTooManyRequests {
			code = "rate_limited"
		}
		return FetchResult{}, &FetchError{
			Code:       code,
			StatusCode: response.StatusCode,
			Temporary:  temporary,
			Err:        errors.New(http.StatusText(response.StatusCode)),
		}
	}

	maxBytes := f.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	if length := response.Header.Get("Content-Length"); length != "" {
		if parsed, parseErr := strconv.ParseInt(length, 10, 64); parseErr == nil && parsed > maxBytes {
			return FetchResult{}, &FetchError{Code: "response_too_large", Err: fmt.Errorf("response exceeds %d bytes", maxBytes)}
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return FetchResult{}, &FetchError{Code: "read_error", Temporary: true, Err: err}
	}
	if int64(len(body)) > maxBytes {
		return FetchResult{}, &FetchError{Code: "response_too_large", Err: fmt.Errorf("response exceeds %d bytes", maxBytes)}
	}
	decoded, err := decodeBody(body, result.ContentType)
	if err != nil {
		return FetchResult{}, &FetchError{Code: "charset_error", Err: err}
	}
	result.Body = decoded
	if finalURL, normalizeErr := NormalizeURL(result.URL); normalizeErr == nil {
		result.URL = finalURL
	}
	return result, nil
}

func decodeBody(body []byte, contentType string) ([]byte, error) {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(reader)
}

func headerPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
