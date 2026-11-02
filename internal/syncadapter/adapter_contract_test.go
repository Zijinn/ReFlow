package syncadapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestGoogleReaderCompatibleContract(t *testing.T) {
	for _, provider := range []string{"freshrss", "google_reader"} {
		t.Run(provider, func(t *testing.T) {
			var pushed url.Values
			server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "GoogleLogin auth=reader-token" {
					t.Errorf("unexpected authorization header %q", got)
				}
				switch r.URL.Path {
				case "/reader/api/0/subscription/list":
					serveFixture(t, w, "google_reader_subscriptions.json")
				case "/reader/api/0/stream/items/ids":
					stream := r.URL.Query().Get("s")
					switch {
					case strings.Contains(stream, "starred"):
						serveFixture(t, w, "google_reader_starred.json")
					case strings.HasSuffix(stream, "/read"):
						serveFixture(t, w, "google_reader_read.json")
					default:
						if got := r.URL.Query().Get("ot"); got != "" {
							t.Errorf("deprecated older-than cursor was sent: %q", got)
						}
						if got := r.URL.Query().Get("nt"); got != "1700000000000000" {
							t.Errorf("newer-than cursor = %q", got)
						}
						serveFixture(t, w, "google_reader_unread.json")
					}
				case "/reader/api/0/stream/items/contents":
					if r.Method != http.MethodPost {
						t.Errorf("item contents must be POST, got %s", r.Method)
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if got := r.PostForm.Get("i"); got != "tag:google.com,2005:reader/item/0001" {
						t.Errorf("item contents ids = %q", got)
					}
					serveFixture(t, w, "google_reader_contents.json")
				case "/reader/api/0/edit-tag":
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					pushed = r.PostForm
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{}`)
				default:
					http.NotFound(w, r)
				}
			})
			defer server.Close()
			adapter := mustAdapter(t, provider, server.URL, Credentials{Token: "reader-token"}, server.Client())
			delta, err := adapter.Pull(context.Background(), "1700000000000000")
			if err != nil {
				t.Fatal(err)
			}
			if len(delta.Subscriptions) != 1 || delta.Subscriptions[0].Folder != "News" || delta.Cursor != "1710000000000001" {
				t.Fatalf("unexpected pull delta: %+v", delta)
			}
			state := stateByRemoteID(t, delta, "tag:google.com,2005:reader/item/0001")
			if state.Read == nil || *state.Read || state.Starred == nil || !*state.Starred {
				t.Fatalf("unexpected state: %+v", state)
			}
			if state.CanonicalURL != "https://example.com/articles/google-1" {
				t.Fatalf("Google Reader item metadata missing: %+v", state)
			}
			read, starred := true, false
			if err := adapter.Push(context.Background(), []ItemState{{RemoteID: state.RemoteID, Read: &read, Starred: &starred}}); err != nil {
				t.Fatal(err)
			}
			if pushed.Get("a") != "user/-/state/com.google/read" || pushed.Get("r") != "user/-/state/com.google/starred" {
				t.Fatalf("unexpected edit-tag form: %v", pushed)
			}
		})
	}
}

func TestMinifluxContract(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Auth-Token"); got != "miniflux-token" {
			t.Errorf("unexpected Miniflux token %q", got)
		}
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/feeds":
			serveFixture(t, w, "miniflux_feeds.json")
		case r.Method == http.MethodGet && r.URL.Path == "/v1/entries":
			serveFixture(t, w, "miniflux_entries.json")
		case r.Method == http.MethodGet && r.URL.Path == "/v1/entries/101":
			serveFixture(t, w, "miniflux_entry.json")
		case r.Method == http.MethodPut && (r.URL.Path == "/v1/entries" || r.URL.Path == "/v1/entries/101/bookmark"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	adapter := mustAdapter(t, "miniflux", server.URL, Credentials{Token: "miniflux-token"}, server.Client())
	delta, err := adapter.Pull(context.Background(), "1700000000")
	if err != nil {
		t.Fatal(err)
	}
	state := stateByRemoteID(t, delta, "101")
	if delta.Cursor != "1710000000" || state.CanonicalURL != "https://example.com/articles/101" || state.FeedRemoteID != "7" {
		t.Fatalf("unexpected Miniflux delta: %+v", delta)
	}
	read, starred := true, true
	if err := adapter.Push(context.Background(), []ItemState{{RemoteID: "101", Read: &read, Starred: &starred}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	joined := strings.Join(paths, "\n")
	mu.Unlock()
	for _, expected := range []string{"changed_after=1700000000", "order=id", "PUT /v1/entries", "GET /v1/entries/101", "PUT /v1/entries/101/bookmark"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in requests:\n%s", expected, joined)
		}
	}
	if strings.Contains(joined, "order=changed_at") {
		t.Fatalf("unsupported Miniflux order parameter was sent:\n%s", joined)
	}
}

func TestFeverContract(t *testing.T) {
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.PostForm.Get("api_key") != "fever-key" {
			t.Errorf("missing Fever API key")
		}
		serveFixture(t, w, "fever.json")
	})
	defer server.Close()
	adapter := mustAdapter(t, "fever", server.URL, Credentials{APIKey: "fever-key"}, server.Client())
	delta, err := adapter.Pull(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Subscriptions) != 1 || delta.Subscriptions[0].RemoteID != "4" {
		t.Fatalf("unexpected Fever subscriptions: %+v", delta.Subscriptions)
	}
	state := stateByRemoteID(t, delta, "201")
	if state.Read == nil || *state.Read || state.Starred == nil || !*state.Starred {
		t.Fatalf("unexpected Fever state: %+v", state)
	}
	read, starred := true, false
	if err := adapter.Push(context.Background(), []ItemState{{RemoteID: "201", Read: &read, Starred: &starred}}); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbinContract(t *testing.T) {
	var methods []string
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if user, password, ok := r.BasicAuth(); !ok || user != "feedbin-user" || password != "feedbin-password" {
			t.Errorf("unexpected Feedbin basic auth")
		}
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v2/subscriptions.json":
			serveFixture(t, w, "feedbin_subscriptions.json")
		case "/v2/unread_entries.json":
			if r.Method == http.MethodGet {
				serveFixture(t, w, "feedbin_unread.json")
			} else {
				_, _ = io.WriteString(w, `{}`)
			}
		case "/v2/starred_entries.json":
			if r.Method == http.MethodGet {
				serveFixture(t, w, "feedbin_starred.json")
			} else {
				_, _ = io.WriteString(w, `{}`)
			}
		case "/v2/entries.json":
			serveFixture(t, w, "feedbin_entries.json")
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	adapter := mustAdapter(t, "feedbin", server.URL, Credentials{Username: "feedbin-user", Password: "feedbin-password"}, server.Client())
	delta, err := adapter.Pull(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Subscriptions) != 1 || delta.Cursor != "301" {
		t.Fatalf("unexpected Feedbin delta: %+v", delta)
	}
	read, starred := true, false
	if err := adapter.Push(context.Background(), []ItemState{{RemoteID: "301", Read: &read, Starred: &starred}}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(methods, "\n")
	if !strings.Contains(joined, "DELETE /v2/unread_entries.json") || !strings.Contains(joined, "DELETE /v2/starred_entries.json") {
		t.Fatalf("unexpected Feedbin writes:\n%s", joined)
	}
}

func TestNextcloudNewsContract(t *testing.T) {
	var writes []string
	server := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if user, password, ok := r.BasicAuth(); !ok || user != "nextcloud-user" || password != "nextcloud-password" {
			t.Errorf("unexpected Nextcloud basic auth")
		}
		switch r.URL.Path {
		case "/index.php/apps/news/api/v1-3/feeds":
			serveFixture(t, w, "nextcloud_feeds.json")
		case "/index.php/apps/news/api/v1-3/items/updated":
			if got := r.URL.Query().Get("lastModified"); got != "400" {
				t.Errorf("lastModified cursor = %q", got)
			}
			serveFixture(t, w, "nextcloud_items.json")
		case "/index.php/apps/news/api/v1-3/items":
			serveFixture(t, w, "nextcloud_items.json")
		default:
			if r.Method == http.MethodPut {
				writes = append(writes, r.URL.Path)
				_, _ = io.WriteString(w, `{}`)
				return
			}
			http.NotFound(w, r)
		}
	})
	defer server.Close()
	adapter := mustAdapter(t, "nextcloud_news", server.URL, Credentials{Username: "nextcloud-user", Password: "nextcloud-password"}, server.Client())
	delta, err := adapter.Pull(context.Background(), "400")
	if err != nil {
		t.Fatal(err)
	}
	state := stateByRemoteID(t, delta, "401")
	if delta.Cursor != "1710000000" || state.GUID != "article-401" || state.FeedRemoteID != "12" {
		t.Fatalf("unexpected Nextcloud delta: %+v", delta)
	}
	read, starred := true, false
	if err := adapter.Push(context.Background(), []ItemState{{RemoteID: "401", Read: &read, Starred: &starred}}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(writes, "\n")
	if !strings.Contains(joined, "/401/read") || !strings.Contains(joined, "/401/unstar") {
		t.Fatalf("unexpected Nextcloud writes:\n%s", joined)
	}
}

func TestHTTPFailureClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()
	adapter := mustAdapter(t, "feedbin", server.URL, Credentials{Username: "user"}, server.Client())
	_, err := adapter.Pull(context.Background(), "")
	if err == nil || ErrorCode(err) != "rate_limited" || !Retryable(err) {
		t.Fatalf("unexpected classified error: %#v", err)
	}
}

func fixtureServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func serveFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func mustAdapter(t *testing.T, provider, endpoint string, credentials Credentials, client *http.Client) Adapter {
	t.Helper()
	adapter, err := New(provider, endpoint, credentials, client)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func stateByRemoteID(t *testing.T, delta Delta, remoteID string) ItemState {
	t.Helper()
	for _, state := range delta.States {
		if state.RemoteID == remoteID {
			return state
		}
	}
	t.Fatalf("state %s not found in %+v", remoteID, delta.States)
	return ItemState{}
}
