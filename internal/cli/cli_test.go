package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// call is one request the fake server saw.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
	Raw    string
	Auth   string
}

type reply struct {
	status int
	body   string
}

// fakeServer stands in for ReFlow Server. It answers from a route table and keeps
// the calls it received, so a test asserts on what a command actually sent rather
// than on what its handler intended to send.
//
// A route is a queue: the first call gets the first reply, the second gets the
// second, and the last reply stays, so a command that writes N rows can fail on
// exactly one of them.
//
// The mutex is the house pattern for a recording handler in this repo: the handler
// runs on the server's goroutine while the test reads the log, and CI runs -race.
type fakeServer struct {
	mu      sync.Mutex
	routes  map[string][]reply
	calls   []call
	answers map[string]int
}

func (f *fakeServer) answer(key string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[key] = append(f.routes[key], reply{status: status, body: body})
}

// seen copies the call log, newest last.
func (f *fakeServer) seen() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func (f *fakeServer) last() call {
	calls := f.seen()
	return calls[len(calls)-1]
}

func (f *fakeServer) callsTo(method, path string) []call {
	out := make([]call, 0, 2)
	for _, item := range f.seen() {
		if item.Method == method && item.Path == path {
			out = append(out, item)
		}
	}
	return out
}

func newFake(t *testing.T) (*fakeServer, string) {
	t.Helper()
	fake := &fakeServer{routes: map[string][]reply{}, answers: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := new(bytes.Buffer)
		_, _ = raw.ReadFrom(r.Body)
		body := map[string]any{}
		if strings.TrimSpace(raw.String()) != "" {
			if err := json.Unmarshal(raw.Bytes(), &body); err != nil {
				t.Errorf("command sent a body that is not JSON: %q", raw.String())
			}
		}
		// A query-qualified key wins, which is how one path can answer differently
		// per research kind.
		fake.mu.Lock()
		key := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			if exact := key + "?" + r.URL.RawQuery; len(fake.routes[exact]) > 0 {
				key = exact
			}
		}
		fake.calls = append(fake.calls, call{Method: r.Method, Path: r.URL.Path,
			Query: r.URL.Query(), Body: body, Raw: raw.String(), Auth: r.Header.Get("Authorization")})
		accepted := reply{status: http.StatusOK, body: `{}`}
		if queue := fake.routes[key]; len(queue) > 0 {
			accepted = queue[min(fake.answers[key], len(queue)-1)]
			fake.answers[key]++
			if accepted.status == 0 {
				accepted.status = http.StatusOK
			}
		}
		fake.mu.Unlock()
		if accepted.status >= http.StatusBadRequest {
			w.Header().Set("Content-Type", "application/problem+json")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(accepted.status)
		if accepted.body != "" {
			_, _ = w.Write([]byte(accepted.body))
		}
	}))
	t.Cleanup(server.Close)
	return fake, server.URL
}

// runCLI drives the real entry point, since exit codes and argv handling are only
// observable from outside Run.
func runCLI(t *testing.T, base string, args ...string) (int, map[string]any, string) {
	t.Helper()
	// The developer's own environment must not decide where a test points.
	t.Setenv("REFLOW_URL", "")
	t.Setenv("REFLOW_TOKEN", "")
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	code := Run(context.Background(), append([]string{"--url", base}, args...),
		AllCommands(), stdout, stderr)
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("stdout is not exactly one JSON document: %q (%v)", stdout.String(), err)
	}
	return code, body, stderr.String()
}

func mustOK(t *testing.T, code int, body map[string]any) map[string]any {
	t.Helper()
	if code != ExitOK {
		t.Fatalf("exit %d: %v", code, body)
	}
	if body["ok"] != true {
		t.Fatalf("expected ok:true, got %v", body)
	}
	data, _ := body["data"].(map[string]any)
	return data
}

func errorOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	out, _ := body["error"].(map[string]any)
	if out == nil {
		t.Fatalf("no error object in %v", body)
	}
	return out
}

func messageOf(t *testing.T, body map[string]any) string {
	t.Helper()
	message, _ := errorOf(t, body)["message"].(string)
	return message
}

func TestPaperCreateThenWritesSetFields(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("POST /api/v1/research/papers", http.StatusCreated,
		`{"id":"p-1","kind":"submitted","title":"投出去的那篇"}`)
	fake.answer("PATCH /api/v1/research/papers/p-1", http.StatusOK,
		`{"id":"p-1","kind":"submitted","current_journal":"JCF"}`)
	code, body, _ := runCLI(t, base, "paper", "create", "--kind", "submitted",
		"--title", "投出去的那篇", "--author", "李四", "--author", "王五",
		"--set", "current_journal=JCF", "--set", "submission_date=2026-10-01")
	data := mustOK(t, code, body)
	if got := data["current_journal"]; got != "JCF" {
		t.Fatalf("create should answer with the patched paper, got %v", got)
	}
	if len(fake.seen()) != 2 {
		t.Fatalf("want create then patch, got %d calls: %v", len(fake.seen()), fake.seen())
	}
	create := fake.seen()[0]
	if create.Method != http.MethodPost {
		t.Fatalf("first call should be the create, got %s", create.Method)
	}
	if create.Body["kind"] != "submitted" || create.Body["title"] != "投出去的那篇" {
		t.Fatalf("create body lost its kind or title: %v", create.Body)
	}
	if authors, _ := create.Body["authors"].([]any); len(authors) != 2 {
		t.Fatalf("create body lost the authors: %v", create.Body)
	}
	patch := fake.seen()[1]
	if patch.Method != http.MethodPatch || patch.Path != "/api/v1/research/papers/p-1" {
		t.Fatalf("second call should patch the new row, got %s %s", patch.Method, patch.Path)
	}
	if patch.Body["current_journal"] != "JCF" || patch.Body["submission_date"] != "2026-10-01" {
		t.Fatalf("PATCH body lost fields: %v", patch.Body)
	}
}

// The server decodes a PATCH into a struct of pointers and drops keys it does not
// recognise, so a misspelled field would answer 200 while writing nothing. The CLI
// has to refuse before the request, and before a create writes a half-row.
func TestSetFieldTypoRefusesBeforeAnyRequest(t *testing.T) {
	fake, base := newFake(t)
	for _, args := range [][]string{
		{"paper", "set", "p-1", "--set", "titel=新标题"},
		{"paper", "create", "--kind", "submitted", "--title", "x", "--set", "titel=新标题"},
		{"paper", "set", "p-1", "--set", "citations=abc"},
		{"paper", "set", "p-1", "--set", "stages=not json"},
		{"paper", "set", "p-1", "--set", "no-equals-sign"},
	} {
		code, body, _ := runCLI(t, base, args...)
		if code != ExitUsage {
			t.Fatalf("%v: want exit 2, got %d: %v", args, code, body)
		}
		if len(fake.seen()) != 0 {
			t.Fatalf("%v: a refused edit must not touch the server, got %v", args, fake.seen())
		}
	}
	_, body, _ := runCLI(t, base, "paper", "set", "p-1", "--set", "titel=新标题")
	if message := messageOf(t, body); !strings.Contains(message, "titel") ||
		!strings.Contains(message, "target_level") {
		t.Fatalf("message should name the bad field and list the real ones: %q", message)
	}
}

func TestPaperCreateKeepsRowVisibleWhenFollowUpFails(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("POST /api/v1/research/papers", http.StatusCreated,
		`{"id":"p-9","kind":"submitted","title":"半篇"}`)
	fake.answer("PATCH /api/v1/research/papers/p-9", http.StatusBadRequest,
		`{"code":"invalid_request","status":400,"detail":"field notes exceeds 2000 characters"}`)
	code, body, _ := runCLI(t, base, "paper", "create", "--kind", "submitted",
		"--title", "半篇", "--set", "notes=写了很长")
	if code != ExitAPI {
		t.Fatalf("want the server rejection, got %d: %v", code, body)
	}
	if body["ok"] != false {
		t.Fatalf("a failed PATCH must not report ok, got %v", body)
	}
	// The row exists. Dropping its id would leave the agent guessing whether the
	// create landed at all.
	data, _ := body["data"].(map[string]any)
	if data == nil || data["paper_id"] != "p-9" {
		t.Fatalf("partial failure should carry the created row, got %v", body["data"])
	}
	fields, _ := data["fields_undone"].([]any)
	if len(fields) != 1 || fields[0] != "notes" {
		t.Fatalf("should list which fields did not land, got %v", data["fields_undone"])
	}
	if created, _ := data["created"].(map[string]any); created["title"] != "半篇" {
		t.Fatalf("should show the row as created, got %v", data["created"])
	}
	if len(fake.seen()) != 2 {
		t.Fatalf("want the create and the rejected patch, got %v", fake.seen())
	}
}

// `citations` is the one field where absent, null and 0 mean different things, so
// the CLI must send the key with a real JSON null instead of dropping it.
func TestNullableIntFieldSendsExplicitNull(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "paper", "set", "p-1",
		"--set", "citations=null", "--set", "authors=李四, 王五",
		"--set", `stages=[{"name":"投稿","done":false}]`)
	mustOK(t, code, body)
	patch := fake.seen()[0].Body
	value, present := patch["citations"]
	if !present || value != nil {
		t.Fatalf("citations=null must be sent as an explicit null, got %v (present %v)", value, present)
	}
	authors, _ := patch["authors"].([]any)
	if len(authors) != 2 || authors[0] != "李四" {
		t.Fatalf("a comma list should become an array: %v", patch["authors"])
	}
	stages, _ := patch["stages"].([]any)
	if len(stages) != 1 {
		t.Fatalf("a JSON array should pass through: %v", patch["stages"])
	}
}

func TestDeleteRefusesWithoutYes(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "paper", "delete", "p-1")
	if code != ExitUsage {
		t.Fatalf("delete without --yes must refuse, got %d: %v", code, body)
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("a refusal must not touch the server, got %v", fake.seen())
	}
	if !strings.Contains(messageOf(t, body), "--yes") {
		t.Fatalf("refusal should say how to confirm: %v", body)
	}
	fake.answer("DELETE /api/v1/research/papers/p-1", http.StatusNoContent, "")
	// --yes also belongs after the command path, where the command's own flag set
	// reads it.
	code, body, _ = runCLI(t, base, "paper", "delete", "p-1", "--yes")
	mustOK(t, code, body)
	if len(fake.seen()) != 1 || fake.seen()[0].Method != http.MethodDelete {
		t.Fatalf("--yes should delete, got %v", fake.seen())
	}
}

func TestTagScopePicksTheNamespace(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("POST /api/v1/research/tags", http.StatusCreated, `{"tag":{"id":"t-1"}}`)
	fake.answer("POST /api/v1/tags", http.StatusCreated, `{"id":"t-2","name":"周报"}`)
	code, body, _ := runCLI(t, base, "tag", "create", "--name", "急件", "--color", "red")
	mustOK(t, code, body)
	if len(fake.seen()) != 1 || fake.seen()[0].Path != "/api/v1/research/tags" {
		t.Fatalf("the default scope should be the research palette, got %v", fake.seen())
	}
	if fake.seen()[0].Body["color"] != "red" {
		t.Fatalf("research scope must send color, got %v", fake.seen()[0].Body)
	}
	code, body, _ = runCLI(t, base, "tag", "create", "--scope", "reader", "--name", "周报")
	mustOK(t, code, body)
	last := fake.last()
	if last.Path != "/api/v1/tags" {
		t.Fatalf("reader scope posted to %s", last.Path)
	}
	// The reader colour column is nullable: sending "" would store an empty colour
	// rather than no colour.
	if _, present := last.Body["color"]; present {
		t.Fatalf("reader scope should omit color, got %v", last.Body)
	}
}

// The research routes answer `tags`/`tag` and the reader route answers `items` plus a
// bare object, so the CLI folds each verb into one shape. Without it, a caller that
// reads `.data.items` on the research scope sees an empty label set and creates a
// duplicate instead of reusing the palette.
func TestTagVerbsAnswerOneShapePerVerb(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/research/tags", http.StatusOK, `{"tags":[{"id":"t-1","name":"高优先级"}]}`)
	fake.answer("GET /api/v1/tags", http.StatusOK, `{"items":[{"id":"r-1","name":"周报"}]}`)
	fake.answer("POST /api/v1/tags", http.StatusCreated, `{"id":"r-2","name":"新标签"}`)

	for _, tc := range []struct {
		args  []string
		scope string
		id    string
	}{
		{[]string{"tag", "list"}, tagScopeResearch, "t-1"},
		{[]string{"tag", "list", "--scope", "reader"}, tagScopeReader, "r-1"},
		{[]string{"tag", "create", "--scope", "reader", "--name", "新标签"}, tagScopeReader, "r-2"},
	} {
		code, body, _ := runCLI(t, base, tc.args...)
		mustOK(t, code, body)
		data, ok := body["data"].(map[string]any)
		if !ok {
			t.Fatalf("%v: want an object in data, got %v", tc.args, body)
		}
		if data["scope"] != tc.scope {
			t.Fatalf("%v: data should name the scope, got %v", tc.args, data)
		}
		var id any
		switch listed, ok := data["items"].([]any); {
		case ok:
			if len(listed) != 1 {
				t.Fatalf("%v: want one item, got %v", tc.args, listed)
			}
			id = listed[0].(map[string]any)["id"]
		default:
			tag, ok := data["tag"].(map[string]any)
			if !ok {
				t.Fatalf("%v: want data.tag, got %v", tc.args, data)
			}
			id = tag["id"]
		}
		if id != tc.id {
			t.Fatalf("%v: want id %s, got %v", tc.args, tc.id, data)
		}
	}
}

func TestTagSetWritesTheColorSwitch(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("PATCH /api/v1/research/tags/t-1", http.StatusOK, `{"tag":{"id":"t-1"}}`)
	code, body, _ := runCLI(t, base, "tag", "set", "t-1", "--color-enabled", "off")
	mustOK(t, code, body)
	if fake.seen()[0].Body["color_enabled"] != false {
		t.Fatalf("--color-enabled off must write false, got %v", fake.seen()[0].Body)
	}
	if _, present := fake.seen()[0].Body["name"]; present {
		t.Fatalf("a patch should leave untouched fields out, got %v", fake.seen()[0].Body)
	}
	code, body, _ = runCLI(t, base, "tag", "set", "t-1")
	if code != ExitUsage || len(fake.seen()) != 1 {
		t.Fatalf("an empty tag set should refuse before calling, got %d: %v", code, body)
	}
}

func TestReaderTagHasNoUpdateRoute(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "tag", "set", "t-1", "--scope", "reader", "--name", "新名")
	if code != ExitUsage {
		t.Fatalf("want a usage error, got %d: %v", code, body)
	}
	if !strings.Contains(messageOf(t, body), "no update route") {
		t.Fatalf("message should explain the missing route: %v", body)
	}
	code, body, _ = runCLI(t, base, "tag", "reorder", "--scope", "reader", "--tag-id", "t-1")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "research palette") {
		t.Fatalf("reader reorder should refuse too, got %d: %v", code, body)
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("a refusal must not send a request, got %v", fake.seen())
	}
}

func TestChoiceAndServerRejections(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "paper", "list", "--kind", "nonsense")
	if code != ExitUsage {
		t.Fatalf("--kind is a choice flag, so the CLI should catch it, got %d: %v", code, body)
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("choice validation should fire before the request, got %v", fake.seen())
	}
	if message := messageOf(t, body); !strings.Contains(message, "research") ||
		!strings.Contains(message, "nonsense") {
		t.Fatalf("message should list the accepted values: %q", message)
	}
	fake.answer("GET /api/v1/research/papers", http.StatusBadRequest,
		`{"code":"invalid_research_kind","status":400,"detail":"Kind must be research, submitted, or published.","request_id":"req-7"}`)
	code, body, _ = runCLI(t, base, "paper", "list", "--kind", "submitted")
	if code != ExitAPI {
		t.Fatalf("a server 400 must be exit 3, got %d: %v", code, body)
	}
	problem := errorOf(t, body)
	if problem["class"] != "api" || problem["code"] != "invalid_research_kind" ||
		problem["status"] != float64(400) {
		t.Fatalf("problem document lost: %v", problem)
	}
	if problem["path"] != "/api/v1/research/papers" || problem["method"] != "GET" ||
		problem["request_id"] != "req-7" {
		t.Fatalf("error should identify the call that answered: %v", problem)
	}
}

func TestRequiredFlagIsRefusedLocally(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "paper", "reorder", "--kind", "research")
	if code != ExitUsage {
		t.Fatalf("a missing required flag is exit 2, got %d: %v", code, body)
	}
	if !strings.Contains(messageOf(t, body), "--paper-id") {
		t.Fatalf("message should name the missing flag: %q", messageOf(t, body))
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("want no request, got %v", fake.seen())
	}
	fake.answer("POST /api/v1/research/papers/reorder", http.StatusNoContent, "")
	code, body, _ = runCLI(t, base, "paper", "reorder", "--kind", "research",
		"--paper-id", "p-2", "--paper-id", "p-1")
	mustOK(t, code, body)
	ids, _ := fake.seen()[0].Body["paper_ids"].([]any)
	if len(ids) != 2 || ids[0] != "p-2" {
		t.Fatalf("reorder must keep the caller's order, got %v", fake.seen()[0].Body)
	}
}

func TestTokenOnlySentWhenGiven(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "feed", "list")
	mustOK(t, code, body)
	if fake.seen()[0].Auth != "" {
		t.Fatalf("loopback needs no credential, got %q", fake.seen()[0].Auth)
	}
	code, body, _ = runCLI(t, base, "--token", "dev-token", "feed", "list")
	mustOK(t, code, body)
	if fake.seen()[1].Auth != "Bearer dev-token" {
		t.Fatalf("a token should become a bearer header, got %q", fake.seen()[1].Auth)
	}
}

func TestAuthProblemCarriesAHint(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/research/tags", http.StatusUnauthorized,
		`{"code":"authentication_required","status":401,"detail":"Pair this device or provide its bearer token."}`)
	code, body, _ := runCLI(t, base, "tag", "list")
	if code != ExitAPI {
		t.Fatalf("401 should be exit 3, got %d: %v", code, body)
	}
	hint, _ := errorOf(t, body)["hint"].(string)
	if !strings.Contains(hint, "device pair") {
		t.Fatalf("401 should say how to pair, got %q", hint)
	}
}

func TestEntryListTimeBoundary(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/entries", http.StatusOK, `{"items":[],"next_cursor":null}`)
	code, body, _ := runCLI(t, base, "entry", "list", "--kind", "literature", "--days", "7")
	mustOK(t, code, body)
	since := fake.seen()[0].Query.Get("since")
	parsed, err := time.Parse(time.RFC3339, since)
	if err != nil {
		t.Fatalf("--days should produce an RFC3339 boundary, got %q", since)
	}
	if window := time.Since(parsed); window < 6*24*time.Hour || window > 8*24*time.Hour {
		t.Fatalf("--days 7 should land about seven days back, got %s", since)
	}
	if fake.seen()[0].Query.Get("content_kind") != "literature" {
		t.Fatalf("--kind did not reach the query: %v", fake.seen()[0].Query)
	}
	// A date-only boundary is accepted because a caller typing --since 2026-09-01
	// means midnight, not a parse failure it has to decode.
	code, body, _ = runCLI(t, base, "entry", "list", "--since", "2026-09-01", "--limit", "100")
	mustOK(t, code, body)
	if got := fake.seen()[1].Query.Get("since"); got != "2026-09-01T00:00:00Z" {
		t.Fatalf("date-only --since should normalise, got %q", got)
	}
	for _, args := range [][]string{
		{"entry", "list", "--since", "2026-09-01", "--days", "7"},
		{"entry", "list", "--since", "not a time"},
		{"entry", "list", "--limit", "0"},
		{"entry", "list", "--limit", "500"},
	} {
		code, body, _ = runCLI(t, base, args...)
		if code != ExitUsage {
			t.Fatalf("%v: want exit 2, got %d: %v", args, code, body)
		}
	}
	if len(fake.seen()) != 2 {
		t.Fatalf("a refused query must not be sent, got %v", fake.seen())
	}
}

func TestLiteratureScanClassifiesByEvidence(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/feeds", http.StatusOK, `{"items":[`+
		`{"id":"f-lit","title":"JFE","content_kind":"literature"},`+
		`{"id":"f-gen","title":"一个博客","content_kind":"general"}]}`)
	fake.answer("GET /api/v1/research/papers?kind=research", http.StatusOK,
		`{"items":[{"id":"p-res-1","kind":"research","title":"Does Board Gender Change Pay?"}]}`)
	fake.answer("GET /api/v1/research/papers?kind=submitted", http.StatusOK, `{"items":[]}`)
	fake.answer("GET /api/v1/research/papers?kind=published", http.StatusOK,
		`{"items":[{"id":"p-pub-1","kind":"published","title":"Cash Flow and Corporate Investment",`+
			`"doi":"10.1234/jfe.2026.001"}]}`)
	fake.answer("GET /api/v1/entries", http.StatusOK, `{"items":[`+
		`{"id":"e-tracked","feed_id":"f-lit","title":"Cash Flow and Corporate Investment",`+
		`"published_at":"2026-09-30T08:00:00Z","doi":"https://doi.org/10.1234/JFE.2026.001"},`+
		`{"id":"e-new","feed_id":"f-lit","title":"Board Size and Diligence",`+
		`"published_at":"2026-10-01T08:00:00Z","doi":"doi:10.5555/NEW.2026","author":"李四; 王五"},`+
		`{"id":"e-authors","feed_id":"f-lit","title":"Trade Credit in Manufacturing",`+
		`"published_at":"2026-09-29T08:00:00Z","author":"张三"},`+
		`{"id":"e-bare","feed_id":"f-lit","title":"Weekend Reading",`+
		`"published_at":"2026-09-28T08:00:00Z"},`+
		`{"id":"e-bytitle","feed_id":"f-gen","title":"Does \"Board\" Gender, Change Pay?",`+
		`"published_at":"2026-09-27T08:00:00Z"}]}`)
	code, body, _ := runCLI(t, base, "literature", "scan")
	data := mustOK(t, code, body)
	if data["scanned"] != float64(5) {
		t.Fatalf("scanned = %v", data["scanned"])
	}
	byEntry := candidatesByEntry(t, data)
	if len(byEntry) != 5 {
		t.Fatalf("every read entry should be reported, got %v", data["candidates"])
	}
	// The freshest finding first: an agent triaging a backlog reads top-down.
	list, _ := data["candidates"].([]any)
	if first, _ := list[0].(map[string]any); first["entry_id"] != "e-new" {
		t.Fatalf("candidates should be newest-first, got %v", first["entry_id"])
	}
	if got := byEntry["e-new"]; got["confidence"] != "high" || got["will_import"] != true {
		t.Fatalf("a DOI is high confidence and importable: %v", got)
	}
	if got := byEntry["e-new"]["doi"]; got != "10.5555/new.2026" {
		t.Fatalf("a `doi:` label should be stripped and case folded, got %v", got)
	}
	authors, _ := byEntry["e-new"]["authors"].([]any)
	if len(authors) != 2 || authors[0] != "李四" {
		t.Fatalf("RSS authors split on semicolons: %v", byEntry["e-new"]["authors"])
	}
	if got := byEntry["e-authors"]; got["confidence"] != "medium" || got["will_import"] != true {
		t.Fatalf("a literature feed with authors is medium: %v", got)
	}
	if got := byEntry["e-bare"]; got["confidence"] != "low" || got["will_import"] != false {
		t.Fatalf("the default floor is medium, so a literature row without authors is not importable: %v", got)
	}
	tracked := byEntry["e-tracked"]
	if tracked["confidence"] != "high" || tracked["will_import"] != false {
		t.Fatalf("a DOI is high confidence, but a tracked row must not be imported again: %v", tracked)
	}
	if matched, _ := tracked["tracked"].(map[string]any); matched["paper_id"] != "p-pub-1" ||
		matched["kind"] != "published" || matched["match"] != "doi" {
		t.Fatalf("a resolver URL should match the workspace DOI: %v", tracked["tracked"])
	}
	byTitle := byEntry["e-bytitle"]
	if matched, _ := byTitle["tracked"].(map[string]any); matched["paper_id"] != "p-res-1" ||
		matched["match"] != "title" {
		t.Fatalf("punctuation and case should not defeat a title match: %v", byTitle["tracked"])
	}
	if !hasEvidence(byTitle["evidence"], "title_only") ||
		!hasEvidence(byTitle["evidence"], "already_in_workspace") {
		t.Fatalf("evidence should record both the weak read and the dedupe: %v", byTitle["evidence"])
	}
	// One page of 100 per round trip, and the scan defaults to literature feeds.
	entries := fake.last()
	if entries.Query.Get("content_kind") != "literature" || entries.Query.Get("limit") != "100" {
		t.Fatalf("scan should read literature entries in large pages: %v", entries.Query)
	}
	var listed int
	for _, call := range fake.seen() {
		if call.Path == "/api/v1/research/papers" {
			listed++
		}
	}
	if listed != 3 {
		t.Fatalf("the scan should dedupe against all three buckets, listed %d", listed)
	}
	for _, call := range fake.seen() {
		if call.Method == http.MethodPost || call.Method == http.MethodPatch {
			t.Fatalf("a scan without --import must be read-only, got %s %s", call.Method, call.Path)
		}
	}
	// --hide-tracked is the view an agent wants when it only has time for what is
	// new: the two matched rows drop out, the rest stay.
	code, body, _ = runCLI(t, base, "literature", "scan", "--hide-tracked")
	data = mustOK(t, code, body)
	if byEntry := candidatesByEntry(t, data); len(byEntry) != 3 {
		if _, still := byEntry["e-tracked"]; still {
			t.Fatalf("--hide-tracked left a matched row in: %v", data["candidates"])
		}
		t.Fatalf("--hide-tracked should report the three unmatched findings, got %v", data["candidates"])
	}
}

func TestLiteratureScanFiltersAndPlaceholder(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/feeds", http.StatusOK,
		`{"items":[{"id":"f-lit","title":"JFE","content_kind":"literature"}]}`)
	fake.answer("GET /api/v1/research/papers", http.StatusOK, `{"items":[]}`)
	fake.answer("GET /api/v1/entries", http.StatusOK, `{"items":[`+
		`{"id":"e-1","feed_id":"f-lit","title":"A","published_at":"2026-10-01T08:00:00Z","doi":"10.1/a"}]}`)
	code, body, _ := runCLI(t, base, "literature", "scan",
		"--all-kinds", "--feed", "f-lit", "--state", "unread", "--query", "董事会", "--days", "14")
	mustOK(t, code, body)
	entries := fake.callsTo(http.MethodGet, entriesPath)[0].Query
	if entries.Get("content_kind") != "" {
		t.Fatalf("--all-kinds should drop the category filter, got %q", entries.Get("content_kind"))
	}
	if entries.Get("feed_id") != "f-lit" || entries.Get("state") != "unread" ||
		entries.Get("query") != "董事会" {
		t.Fatalf("scan flags should reach the timeline query: %v", entries)
	}
	if _, err := time.Parse(time.RFC3339, entries.Get("since")); err != nil {
		t.Fatalf("--days should become an RFC3339 boundary, got %q", entries.Get("since"))
	}
	code, body, _ = runCLI(t, base, "literature", "scan", "--all-kinds", "--kind", "general")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "not both") {
		t.Fatalf("--kind and --all-kinds contradict, got %d: %v", code, body)
	}
	code, body, _ = runCLI(t, base, "literature", "scan", "--min-confidence", "low")
	data := mustOK(t, code, body)
	if byEntry := candidatesByEntry(t, data); len(byEntry) != 1 {
		t.Fatalf("candidates = %v", data["candidates"])
	}
	if data["min_confidence"] != "low" {
		t.Fatalf("the floor should be reported, got %v", data["min_confidence"])
	}
	// A placeholder DOI is what an unregistered feed puts in every item; treating it
	// as evidence would make every row look like a real paper and match nothing.
	fake.answer("GET /api/v1/entries", http.StatusOK, `{"items":[`+
		`{"id":"e-placeholder","feed_id":"f-lit","title":"B","published_at":"2026-10-02T08:00:00Z",`+
		`"doi":"10.xxxx/1","author":"张三"}]}`)
	code, body, _ = runCLI(t, base, "literature", "scan")
	data = mustOK(t, code, body)
	candidate := candidatesByEntry(t, data)["e-placeholder"]
	if _, present := candidate["doi"]; present {
		t.Fatalf("a 10.xxxx placeholder should be dropped, got %v", candidate["doi"])
	}
	if candidate["confidence"] != "medium" || !hasEvidence(candidate["evidence"], "feed_is_literature") {
		t.Fatalf("the row should fall back to feed category plus authors: %v", candidate)
	}
}

func TestLiteratureScanImportReportsEveryItem(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/feeds", http.StatusOK,
		`{"items":[{"id":"f-lit","title":"JFE","content_kind":"literature"}]}`)
	fake.answer("GET /api/v1/research/papers", http.StatusOK, `{"items":[]}`)
	fake.answer("GET /api/v1/entries", http.StatusOK, `{"items":[`+
		`{"id":"e-1","feed_id":"f-lit","title":"Board Size and Diligence",`+
		`"published_at":"2026-10-01T08:00:00Z","doi":"https://doi.org/10.5555/NEW.2026",`+
		`"canonical_url":"https://doi.org/10.5555/NEW.2026","author":"李四; 王五"},`+
		`{"id":"e-2","feed_id":"f-lit","title":"Trade Credit in Manufacturing",`+
		`"published_at":"2026-09-30T08:00:00Z","author":"张三"},`+
		`{"id":"e-3","feed_id":"f-lit","title":"Collateral and Credit Rationing",`+
		`"published_at":"2026-09-29T08:00:00Z","author":"赵六"},`+
		`{"id":"e-4","feed_id":"f-lit","title":"Weekend Reading",`+
		`"published_at":"2026-09-28T08:00:00Z"}]}`)
	fake.answer("POST /api/v1/research/papers", http.StatusCreated, `{"id":"p-new-1"}`)
	fake.answer("POST /api/v1/research/papers", http.StatusCreated, `{"id":"p-new-2"}`)
	fake.answer("POST /api/v1/research/papers", http.StatusBadRequest,
		`{"code":"invalid_research_title","status":400,"detail":"Title is required."}`)
	fake.answer("PATCH /api/v1/research/papers/p-new-1", http.StatusOK, `{"id":"p-new-1"}`)
	fake.answer("PATCH /api/v1/research/papers/p-new-2", http.StatusBadRequest,
		`{"code":"invalid_request","status":400,"detail":"field notes exceeds 2000 characters"}`)
	code, body, _ := runCLI(t, base, "literature", "scan", "--import", "--tag-id", "t-1")
	data := mustOK(t, code, body)
	if data["target_kind"] != "published" {
		t.Fatalf("feed literature belongs in 已发表, got %v", data["target_kind"])
	}
	imported, _ := data["imported"].([]any)
	if len(imported) != 3 {
		t.Fatalf("one item should be reported per attempt, got %v", data["imported"])
	}
	created, _ := imported[0].(map[string]any)
	if created["status"] != "created" || created["paper_id"] != "p-new-1" ||
		created["entry_id"] != "e-1" {
		t.Fatalf("first import = %v", created)
	}
	patched, _ := imported[1].(map[string]any)
	if patched["status"] != "failed" || patched["failed_at"] != "patch" ||
		patched["paper_id"] != "p-new-2" {
		t.Fatalf("a rejected follow-up should name the row it left behind: %v", patched)
	}
	failed, _ := imported[2].(map[string]any)
	if failed["status"] != "failed" || failed["failed_at"] != "create" ||
		failed["code"] != "invalid_research_title" {
		t.Fatalf("the run should continue past a rejection and keep its code: %v", failed)
	}
	// The low-confidence row is never attempted, and one rejection does not stop the
	// rows behind it.
	creates := fake.callsTo(http.MethodPost, "/api/v1/research/papers")
	if len(creates) != 3 {
		t.Fatalf("want three create attempts, got %d", len(creates))
	}
	if len(fake.callsTo(http.MethodPatch, "/api/v1/research/papers/p-new-1")) != 1 {
		t.Fatalf("a created row should get its follow-up write")
	}
	if len(fake.callsTo(http.MethodPatch, "/api/v1/research/papers/p-new-3")) != 0 {
		t.Fatalf("a create that failed should not be followed up")
	}
	first := creates[0].Body
	if first["kind"] != "published" || first["title"] != "Board Size and Diligence" {
		t.Fatalf("create body = %v", first)
	}
	authors, _ := first["authors"].([]any)
	if len(authors) != 2 || authors[1] != "王五" {
		t.Fatalf("import should carry the authors: %v", first["authors"])
	}
	// Provenance is written into 备注 because an imported row must say where it came
	// from: the scan is a bulk write, and the user did not choose these titles.
	patch := fake.callsTo(http.MethodPatch, "/api/v1/research/papers/p-new-1")[0].Body
	if !strings.Contains(patch["notes"].(string), "e-1") ||
		!strings.Contains(patch["notes"].(string), "https://doi.org/10.5555/NEW.2026") {
		t.Fatalf("notes should carry the entry id and source url: %v", patch["notes"])
	}
	if patch["doi"] != "10.5555/new.2026" {
		t.Fatalf("import must write the normalised DOI: %v", patch["doi"])
	}
	tags, _ := patch["tag_ids"].([]any)
	if len(tags) != 1 || tags[0] != "t-1" {
		t.Fatalf("--tag-id should attach on import, got %v", patch["tag_ids"])
	}
}

func TestLiteratureScanPagesUntilMax(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/feeds", http.StatusOK, `{"items":[]}`)
	fake.answer("GET /api/v1/research/papers", http.StatusOK, `{"items":[]}`)
	fake.answer("GET /api/v1/entries", http.StatusOK,
		`{"items":[{"id":"e-1","feed_id":"f-1","title":"A","published_at":"2026-10-01T08:00:00Z"}],`+
			`"next_cursor":"c-2"}`)
	fake.answer("GET /api/v1/entries", http.StatusOK,
		`{"items":[{"id":"e-2","feed_id":"f-1","title":"B","published_at":"2026-10-02T08:00:00Z"}],`+
			`"next_cursor":"c-3"}`)
	code, body, _ := runCLI(t, base, "literature", "scan", "--max", "2")
	data := mustOK(t, code, body)
	if data["scanned"] != float64(2) {
		t.Fatalf("--max should stop the walk, scanned = %v", data["scanned"])
	}
	pages := fake.callsTo(http.MethodGet, entriesPath)
	if len(pages) != 2 {
		t.Fatalf("want two pages of entries, got %d", len(pages))
	}
	if pages[0].Query.Get("cursor") != "" || pages[1].Query.Get("cursor") != "c-2" {
		t.Fatalf("pages should follow next_cursor: %v, %v", pages[0].Query, pages[1].Query)
	}
}

func candidatesByEntry(t *testing.T, data map[string]any) map[string]map[string]any {
	t.Helper()
	list, _ := data["candidates"].([]any)
	out := map[string]map[string]any{}
	for _, item := range list {
		candidate, _ := item.(map[string]any)
		id, _ := candidate["entry_id"].(string)
		if id == "" {
			t.Fatalf("candidate without an entry id: %v", item)
		}
		out[id] = candidate
	}
	return out
}

func hasEvidence(raw any, want string) bool {
	items, _ := raw.([]any)
	for _, item := range items {
		if text, _ := item.(string); text == want {
			return true
		}
	}
	return false
}

func stringsOf(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, _ := value.(string)
		out = append(out, text)
	}
	return out
}

func TestDescribeCatalogsEveryCommand(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "describe")
	data := mustOK(t, code, body)
	if len(fake.seen()) != 0 {
		t.Fatalf("describe must not call the API, got %v", fake.seen())
	}
	listed, _ := data["commands"].([]any)
	if len(listed) != len(AllCommands()) {
		t.Fatalf("catalog has %d commands, registry has %d", len(listed), len(AllCommands()))
	}
	codes, _ := data["exit_codes"].(map[string]any)
	for _, key := range []string{"0", "2", "3", "4"} {
		if text, _ := codes[key].(string); text == "" {
			t.Fatalf("exit %s is undocumented", key)
		}
	}
	if !strings.Contains(codes["3"].(string), "4xx") {
		t.Fatalf("exit 3 must be described as a server rejection, got %v", codes["3"])
	}
	fields, _ := data["paper_fields"].([]any)
	if len(fields) != len(paperFields) {
		t.Fatalf("paper_fields should list the whole PATCH surface, got %d of %d",
			len(fields), len(paperFields))
	}
	joined := strings.Join(stringsOf(fields), " ")
	for _, want := range []string{"doi=<text>", "citations=<int|null>", "authors=<a,b|json-array>",
		"stages=<json>"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("paper_fields lost the shape %q, got %v", want, joined)
		}
	}
	scopes, _ := data["tag_scopes"].(map[string]any)
	if scopes["reader"] == nil || scopes["research"] == nil {
		t.Fatalf("both tag namespaces must be described, got %v", scopes)
	}
	// Spot-check that the catalog carries the flags an agent needs to write a
	// submission, not just a list of names.
	var found bool
	for _, entry := range listed {
		command, _ := entry.(map[string]any)
		if command["path"] != "paper create" {
			continue
		}
		found = true
		if flags, _ := command["flags"].([]any); len(flags) != 4 {
			t.Fatalf("paper create flags = %v", command["flags"])
		}
		if endpoint, _ := command["endpoint"].(string); !strings.Contains(endpoint, "PATCH") {
			t.Fatalf("the catalog should show the two-call create, got %q", endpoint)
		}
	}
	if !found {
		t.Fatal("catalog is missing paper create")
	}
}

func TestUnknownVerbListsTheGroup(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "paper", "publish")
	if code != ExitUsage {
		t.Fatalf("want a usage error, got %d", code)
	}
	message := messageOf(t, body)
	for _, want := range []string{"paper", "set", "move", "citation"} {
		if !strings.Contains(message, want) {
			t.Fatalf("an unknown verb should list the real ones, got %q", message)
		}
	}
	code, body, _ = runCLI(t, base, "nope")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "describe") {
		t.Fatalf("an unknown command should point at the catalog, got %d: %v", code, body)
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("want no request, got %v", fake.seen())
	}
}

func TestHelpIsProseOnStdout(t *testing.T) {
	_, base := newFake(t)
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	code := Run(context.Background(), []string{"--url", base, "--help", "paper", "create"},
		AllCommands(), stdout, stderr)
	if code != ExitOK {
		t.Fatalf("--help is a successful read, got %d", code)
	}
	text := stdout.String()
	for _, want := range []string{"--kind <choice>", "[research|submitted|published]",
		"current_journal=<text>", "POST /api/v1/research/papers"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help should render the flag surface, missing %q in %q", want, text)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"--url", base}, AllCommands(), stdout, stderr); code != ExitUsage {
		t.Fatalf("no command given is exit 2, got %d", code)
	}
	if strings.Contains(stdout.String(), "reflow paper list") {
		t.Fatalf("the root help belongs on stderr, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "reflow literature scan") {
		t.Fatalf("help should list the commands, got %q", stderr.String())
	}
}

// The unknown-command message sends a caller to `reflow help`, so `help` has to be a
// command rather than a pointer at one that does not exist.
func TestHelpIsACommand(t *testing.T) {
	fake, base := newFake(t)
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	if code := Run(context.Background(), []string{"--url", base, "help"}, AllCommands(), stdout, stderr); code != ExitOK {
		t.Fatalf("help is a successful read, got %d", code)
	}
	if !strings.Contains(stdout.String(), "reflow literature scan") {
		t.Fatalf("help should list the commands on stdout, got %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "reflow paper list") {
		t.Fatalf("help should not write the list to stderr, got %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run(context.Background(), []string{"--url", base, "help", "paper", "set"}, AllCommands(), stdout, stderr)
	if code != ExitOK {
		t.Fatalf("help <command> is exit 0, got %d", code)
	}
	for _, want := range []string{"reflow paper set <paper-id>", "--set", "PATCH /api/v1/research/papers"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("command help should carry %q, got %q", want, stdout.String())
		}
	}

	for _, args := range [][]string{{"help", "paper"}, {"paper"}} {
		code, body, _ := runCLI(t, base, args...)
		if code != ExitUsage {
			t.Fatalf("%v: want a usage error, got %d: %v", args, code, body)
		}
		message := messageOf(t, body)
		for _, want := range []string{"set", "move", "citation"} {
			if !strings.Contains(message, want) {
				t.Fatalf("%v should list the group's verbs, got %q", args, message)
			}
		}
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("help must not reach the server, got %v", fake.seen())
	}
}

// The flag package reads options only until the first positional argument, so the
// CLI lifts options out of the list itself. Without that, `paper set p-1 --set ...`
// would read as three positionals.
func TestOptionsMayFollowThePositionalID(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("PATCH /api/v1/research/papers/p-1", http.StatusOK, `{"id":"p-1","doi":"10.1/a"}`)
	code, body, _ := runCLI(t, base, "paper", "set", "p-1", "--set", "doi=10.1/a")
	mustOK(t, code, body)
	if got := fake.seen()[0].Body["doi"]; got != "10.1/a" {
		t.Fatalf("the option after the id should be read, got %v", fake.seen()[0].Body)
	}
	// A value that looks like an option belongs to its flag rather than becoming a
	// flag of its own.
	code, body, _ = runCLI(t, base, "entry", "list", "--days", "-1")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "--days must not be negative") {
		t.Fatalf("--days -1 should reach the flag as its value, got %d: %v", code, body)
	}
	// Everything after `--` is an id, even one that starts with dashes.
	fake.answer("GET /api/v1/research/papers/--odd", http.StatusOK, `{"id":"--odd"}`)
	code, body, _ = runCLI(t, base, "paper", "get", "--", "--odd")
	mustOK(t, code, body)
	if fake.last().Path != "/api/v1/research/papers/--odd" {
		t.Fatalf("the terminator should protect the id, got %v", fake.seen())
	}
}

func TestUnreachableServerIsTransport(t *testing.T) {
	code, body, _ := runCLI(t, "http://127.0.0.1:1", "status")
	if code != ExitTransport {
		t.Fatalf("want exit 4, got %d: %v", code, body)
	}
	problem := errorOf(t, body)
	if problem["class"] != "transport" {
		t.Fatalf("class = %v", problem)
	}
	if !strings.Contains(problem["message"].(string), "is ReFlow Server running") {
		t.Fatalf("message should name the likely cause: %v", problem)
	}
}

func TestBadURLIsUsageButDescribeStillAnswers(t *testing.T) {
	// A caller that mistyped --url still needs the catalog, so the URL is checked
	// when a request is made, not at startup.
	code, body, _ := runCLI(t, "not-a-url", "status")
	if code != ExitUsage {
		t.Fatalf("an unusable --url is caller-fixable, got %d: %v", code, body)
	}
	if !strings.Contains(messageOf(t, body), "--url") {
		t.Fatalf("message should name the flag: %v", body)
	}
	code, body, _ = runCLI(t, "ftp://example.invalid", "describe")
	data := mustOK(t, code, body)
	if data["client"] != "reflow" {
		t.Fatalf("describe = %v", data)
	}
}

func TestAIFillReadsStdin(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("POST /api/v1/ai/metadata-fill", http.StatusOK, `{"title":"猜出来的标题"}`)
	original := stdin
	stdin = strings.NewReader("李四, 王五. Board Size and Diligence.\n")
	t.Cleanup(func() { stdin = original })
	code, body, _ := runCLI(t, base, "ai", "fill", "--raw", "-")
	data := mustOK(t, code, body)
	if data["title"] != "猜出来的标题" {
		t.Fatalf("fill = %v", data)
	}
	if !strings.Contains(fake.seen()[0].Body["raw"].(string), "Board Size") {
		t.Fatalf("the pasted reference should come from stdin, got %v", fake.seen()[0].Body)
	}
	if _, present := fake.seen()[0].Body["profile_id"]; present {
		t.Fatalf("an unset profile should be left out, got %v", fake.seen()[0].Body)
	}
}

func TestGlobalsMayFollowTheCommandPath(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("GET /api/v1/status", http.StatusOK, `{"ok":true}`)
	code, body, _ := runCLI(t, base, "status", "--url", base)
	data := mustOK(t, code, body)
	if data["ok"] != true {
		t.Fatalf("a trailing --url should be accepted, got %v", body)
	}
	if len(fake.seen()) != 1 || fake.seen()[0].Path != "/api/v1/status" {
		t.Fatalf("status = %v", fake.seen())
	}
	code, body, _ = runCLI(t, base, "--format", "yaml", "status")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "--format") {
		t.Fatalf("an unknown format is exit 2, got %d: %v", code, body)
	}
	if len(fake.seen()) != 1 {
		t.Fatalf("a bad --format must refuse first, got %v", fake.seen())
	}
}
