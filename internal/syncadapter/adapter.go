package syncadapter

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

var ErrAuthentication = errors.New("sync authentication failed")

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
	var syncError *Error
	if errors.As(err, &syncError) {
		return syncError.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "sync_failed"
}

func Retryable(err error) bool {
	var syncError *Error
	return errors.As(err, &syncError) && syncError.Retryable
}

type Credentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Token    string `json:"token,omitempty"`
}

type Subscription struct {
	RemoteID string `json:"remote_id"`
	Title    string `json:"title"`
	FeedURL  string `json:"feed_url"`
	Folder   string `json:"folder,omitempty"`
}

type ItemState struct {
	RemoteID      string `json:"remote_id"`
	FeedRemoteID  string `json:"feed_remote_id,omitempty"`
	GUID          string `json:"guid,omitempty"`
	CanonicalURL  string `json:"canonical_url,omitempty"`
	Read          *bool  `json:"read,omitempty"`
	Starred       *bool  `json:"starred,omitempty"`
	RemoteUpdated string `json:"remote_updated,omitempty"`
}

type Delta struct {
	Subscriptions []Subscription `json:"subscriptions"`
	States        []ItemState    `json:"states"`
	Cursor        string         `json:"cursor"`
}

type Adapter interface {
	Pull(ctx context.Context, cursor string) (Delta, error)
	Push(ctx context.Context, states []ItemState) error
}

func New(provider, endpoint string, credentials Credentials, client *http.Client) (Adapter, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("sync endpoint must be an HTTP or HTTPS URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	base := baseClient{endpoint: endpoint, credentials: credentials, http: client}
	switch strings.ToLower(provider) {
	case "freshrss", "google_reader":
		base.authStyle = "google_reader"
		return &googleReaderAdapter{baseClient: base}, nil
	case "miniflux":
		base.authStyle = "miniflux"
		return &minifluxAdapter{baseClient: base}, nil
	case "fever":
		return &feverAdapter{baseClient: base}, nil
	case "feedbin":
		return &feedbinAdapter{baseClient: base}, nil
	case "nextcloud_news":
		return &nextcloudAdapter{baseClient: base}, nil
	default:
		return nil, fmt.Errorf("unsupported sync provider %q", provider)
	}
}

func SecureHTTPClient(policy feedcore.URLPolicy) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = policy.DialContext
	transport.ResponseHeaderTimeout = 15 * time.Second
	return &http.Client{
		Timeout:   30 * time.Second,
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
	endpoint    string
	credentials Credentials
	http        *http.Client
	authStyle   string
}

func (c baseClient) request(ctx context.Context, method, route string, query url.Values, body any) (*http.Response, error) {
	target := c.endpoint + "/" + strings.TrimLeft(route, "/")
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		retryable := !errors.Is(err, context.Canceled)
		return nil, &Error{Code: "network_error", Retryable: retryable, Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		code := "http_error"
		retryable := response.StatusCode >= 500
		cause := errors.New(strings.TrimSpace(string(message)))
		if cause.Error() == "" {
			cause = errors.New(http.StatusText(response.StatusCode))
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			code = "authentication_error"
			cause = fmt.Errorf("%w: %v", ErrAuthentication, cause)
		} else if response.StatusCode == http.StatusTooManyRequests {
			code = "rate_limited"
			retryable = true
		}
		return nil, &Error{Code: code, StatusCode: response.StatusCode, Retryable: retryable, Err: cause}
	}
	return response, nil
}

func (c baseClient) form(ctx context.Context, route string, query, values url.Values) (*http.Response, error) {
	target := c.endpoint + "/" + strings.TrimLeft(route, "/")
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	parsed.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		retryable := !errors.Is(err, context.Canceled)
		return nil, &Error{Code: "network_error", Retryable: retryable, Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		code := "http_error"
		retryable := response.StatusCode >= 500
		cause := errors.New(http.StatusText(response.StatusCode))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			code = "authentication_error"
			cause = ErrAuthentication
		} else if response.StatusCode == http.StatusTooManyRequests {
			code = "rate_limited"
			retryable = true
		}
		return nil, &Error{Code: code, StatusCode: response.StatusCode, Retryable: retryable, Err: cause}
	}
	return response, nil
}

func (c baseClient) authorize(request *http.Request) {
	if c.credentials.Token != "" {
		switch c.authStyle {
		case "google_reader":
			request.Header.Set("Authorization", "GoogleLogin auth="+c.credentials.Token)
		case "miniflux":
			request.Header.Set("X-Auth-Token", c.credentials.Token)
		default:
			request.Header.Set("Authorization", "Bearer "+c.credentials.Token)
		}
	} else if c.credentials.Username != "" || c.credentials.Password != "" {
		request.SetBasicAuth(c.credentials.Username, c.credentials.Password)
	}
}

func decodeJSONResponse(response *http.Response, target any) error {
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, 20<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode sync response: %w", err)
	}
	return nil
}

func boolPointer(value bool) *bool { return &value }
