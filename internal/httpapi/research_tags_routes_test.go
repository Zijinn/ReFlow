package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func TestResearchTagRoutes(t *testing.T) {
	server := newTestServer(t)
	resetResearchTagPalette(t, server)
	handler := server.Handler()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	created := do(http.MethodPost, "/api/v1/research/tags", `{"name":"  急件  ","color":"red"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createBody struct {
		Tag struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Position int    `json:"position"`
			Color    string `json:"color"`
		} `json:"tag"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if createBody.Tag.Name != "急件" || createBody.Tag.ID == "" {
		t.Fatalf("unexpected tag: %+v", createBody.Tag)
	}
	if createBody.Tag.Color != "red" {
		t.Fatalf("the chosen colour did not come back: %+v", createBody.Tag)
	}
	// Omitting the colour is the documented "no explicit choice", not a mistake.
	untilted := do(http.MethodPost, "/api/v1/research/tags", `{"name":"慢工"}`)
	if untilted.Code != http.StatusCreated {
		t.Fatalf("create without colour: expected 201, got %d: %s", untilted.Code, untilted.Body.String())
	}
	var untiltedBody struct {
		Tag struct {
			Color string `json:"color"`
		} `json:"tag"`
	}
	if err := json.NewDecoder(untilted.Body).Decode(&untiltedBody); err != nil {
		t.Fatalf("decode colourless tag: %v", err)
	}
	if untiltedBody.Tag.Color != "" {
		t.Fatalf("expected the empty colour, got %q", untiltedBody.Tag.Color)
	}

	duplicate := do(http.MethodPost, "/api/v1/research/tags", `{"name":"急件","color":"teal"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
	if code := problemCode(t, duplicate); code != "duplicate_research_tag" {
		t.Fatalf("duplicate: unexpected problem code %q", code)
	}

	blank := do(http.MethodPost, "/api/v1/research/tags", `{"name":"   "}`)
	if blank.Code != http.StatusBadRequest {
		t.Fatalf("blank: expected 400, got %d", blank.Code)
	}
	if code := problemCode(t, blank); code != "invalid_research_tag" {
		t.Fatalf("blank: unexpected problem code %q", code)
	}

	// A colour the palette has no swatch for is the client's mistake to fix.
	badColor := do(http.MethodPost, "/api/v1/research/tags", `{"name":"染色","color":"crimson"}`)
	if badColor.Code != http.StatusBadRequest {
		t.Fatalf("bad colour: expected 400, got %d: %s", badColor.Code, badColor.Body.String())
	}
	if code := problemCode(t, badColor); code != "invalid_research_tag" {
		t.Fatalf("bad colour: unexpected problem code %q", code)
	}

	list := do(http.MethodGet, "/api/v1/research/tags", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", list.Code)
	}
	var listBody struct {
		Tags []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Tags) != 2 || listBody.Tags[0].ID != createBody.Tag.ID {
		t.Fatalf("unexpected palette: %+v", listBody.Tags)
	}
	if listBody.Tags[0].Color != "red" || listBody.Tags[1].Color != "" {
		t.Fatalf("the list route lost the colours: %+v", listBody.Tags)
	}

	renamed := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, `{"name":"特急"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", renamed.Code, renamed.Body.String())
	}
	var renamedBody struct {
		Tag struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"tag"`
	}
	if err := json.NewDecoder(renamed.Body).Decode(&renamedBody); err != nil {
		t.Fatalf("decode rename body: %v", err)
	}
	if renamedBody.Tag.Name != "特急" || renamedBody.Tag.Color != "red" {
		t.Fatalf("a rename that never mentioned colour answered %+v", renamedBody.Tag)
	}
	missing := do(http.MethodPatch, "/api/v1/research/tags/nope", `{"name":"特急"}`)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("rename unknown: expected 404, got %d", missing.Code)
	}

	deleted := do(http.MethodDelete, "/api/v1/research/tags/"+createBody.Tag.ID, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", deleted.Code, deleted.Body.String())
	}
	if again := do(http.MethodDelete, "/api/v1/research/tags/"+createBody.Tag.ID, ""); again.Code != http.StatusNotFound {
		t.Fatalf("delete twice: expected 404, got %d", again.Code)
	}
}

// The colour picker sends one field at a time, so PATCH has to accept a name, a
// colour, or both, and keep answering 404 for a tag that is already gone even
// when the name it asked for is taken.
func TestUpdateResearchTagColorRoute(t *testing.T) {
	server := newTestServer(t)
	resetResearchTagPalette(t, server)
	handler := server.Handler()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, target, strings.NewReader(body)))
		return recorder
	}

	created := do(http.MethodPost, "/api/v1/research/tags", `{"name":"在改","color":"gray"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createBody struct {
		Tag struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Position     int    `json:"position"`
			Color        string `json:"color"`
			ColorEnabled bool   `json:"color_enabled"`
		} `json:"tag"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	// A label the palette just gained has to answer painted: the settings pane
	// shows a switch, and one that starts off reads as a broken control.
	if !createBody.Tag.ColorEnabled {
		t.Fatalf("a created tag should come back colour-enabled, got %s", created.Body.String())
	}
	taken := do(http.MethodPost, "/api/v1/research/tags", `{"name":"已投","color":"blue"}`)
	if taken.Code != http.StatusCreated {
		t.Fatalf("create second: expected 201, got %d: %s", taken.Code, taken.Body.String())
	}

	type tagReply struct {
		Tag struct {
			Name         string `json:"name"`
			Color        string `json:"color"`
			ColorEnabled bool   `json:"color_enabled"`
		} `json:"tag"`
	}
	decode := func(t *testing.T, recorder *httptest.ResponseRecorder) tagReply {
		t.Helper()
		var reply tagReply
		if err := json.NewDecoder(recorder.Body).Decode(&reply); err != nil {
			t.Fatalf("decode tag: %v", err)
		}
		return reply
	}

	for _, patch := range []struct{ label, body, name, color string }{
		{"colour only", `{"color":"violet"}`, "在改", "violet"},
		{"name only", `{"name":" 在评审 "}`, "在评审", "violet"},
		{"both", `{"name":"合作者","color":" teal "}`, "合作者", "teal"},
		{"cleared", `{"color":""}`, "合作者", ""},
	} {
		recorder := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, patch.body)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", patch.label, recorder.Code, recorder.Body.String())
		}
		reply := decode(t, recorder)
		if reply.Tag.Name != patch.name || reply.Tag.Color != patch.color {
			t.Fatalf("%s: got %+v, want name %q colour %q", patch.label, reply.Tag, patch.name, patch.color)
		}
	}

	// An empty patch is not a write, but it is still a valid answer.
	empty := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, `{}`)
	if empty.Code != http.StatusOK {
		t.Fatalf("empty patch: expected 200, got %d: %s", empty.Code, empty.Body.String())
	}
	if reply := decode(t, empty); reply.Tag.Name != "合作者" || reply.Tag.Color != "" {
		t.Fatalf("empty patch changed the tag: %+v", reply.Tag)
	}

	// A body the server has no field for stays a 400 rather than being ignored,
	// so a typo cannot silently drop the user's colour.
	unknown := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, `{"colour":"teal"}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: expected 400, got %d: %s", unknown.Code, unknown.Body.String())
	}

	badColor := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, `{"color":"magenta"}`)
	if badColor.Code != http.StatusBadRequest {
		t.Fatalf("bad colour: expected 400, got %d: %s", badColor.Code, badColor.Body.String())
	}
	if code := problemCode(t, badColor); code != "invalid_research_tag" {
		t.Fatalf("bad colour: unexpected problem code %q", code)
	}
	// The colour was refused, so the palette still reads what it did before.
	reloaded := do(http.MethodGet, "/api/v1/research/tags", "")
	var listBody struct {
		Tags []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(reloaded.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode palette: %v", err)
	}
	if len(listBody.Tags) != 2 || listBody.Tags[0].Name != "合作者" || listBody.Tags[0].Color != "" {
		t.Fatalf("a refused patch reached the palette: %+v", listBody.Tags)
	}

	// The colour switch mutes a label without forgetting its shade, which is the
	// whole reason it is a second field rather than a ninth colour name.
	off := decode(t, do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID,
		`{"color":"red","color_enabled":false}`))
	if off.Tag.ColorEnabled || off.Tag.Color != "red" {
		t.Fatalf("expected a muted tag that still remembers red, got %+v", off.Tag)
	}
	on := decode(t, do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID,
		`{"color_enabled":true}`))
	if !on.Tag.ColorEnabled || on.Tag.Color != "red" {
		t.Fatalf("switching back on should restore the picked colour, got %+v", on.Tag)
	}

	// 404 still beats 409 for a tag that no longer exists, whichever field the
	// patch carried.
	if err := storage.DeleteResearchTag(context.Background(), server.db, domain.DefaultProfileID, createBody.Tag.ID); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	for _, body := range []string{`{"name":"已投"}`, `{"name":"已投","color":"teal"}`} {
		recorder := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, body)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("deleted tag %s: expected 404, got %d: %s", body, recorder.Code, recorder.Body.String())
		}
	}
	if recorder := do(http.MethodPatch, "/api/v1/research/tags/no-such-tag", `{"name":"新名字"}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown tag: expected 404, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

// A brand-new database has to answer the three priority labels with the colours
// the owner asked for, because that is the palette the workbench renders first.
func TestResearchTagSeededPaletteOverHTTP(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/research/tags", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Tags []struct {
			Name     string `json:"name"`
			Position int    `json:"position"`
			Color    string `json:"color"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode palette: %v", err)
	}
	want := []struct{ name, color string }{{"High", "red"}, {"Medium", "amber"}, {"Low", "green"}}
	if len(body.Tags) != len(want) {
		t.Fatalf("expected the seeded priority palette, got %+v", body.Tags)
	}
	for index, expected := range want {
		tag := body.Tags[index]
		if tag.Name != expected.name || tag.Color != expected.color || tag.Position != index {
			t.Fatalf("tag %d should be %s/%s at position %d, got %+v", index, expected.name, expected.color, index, tag)
		}
	}
}

// resetResearchTagPalette drops the palette migration 0018 seeds for every
// fresh profile, so a route test asserts on only the labels it created.
func resetResearchTagPalette(t *testing.T, server *Server) {
	t.Helper()
	if _, err := server.db.Exec("DELETE FROM research_paper_tags"); err != nil {
		t.Fatalf("clear tag associations: %v", err)
	}
	if _, err := server.db.Exec("DELETE FROM research_tags"); err != nil {
		t.Fatalf("clear palette: %v", err)
	}
}

func TestDeleteResearchTagLiftsOnlyThatLabelOverHTTP(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	tagIDs := make([]string, 2)
	for index, name := range []string{"在改", "已投"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		tagRecorder := httptest.NewRecorder()
		handler.ServeHTTP(tagRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/research/tags",
			strings.NewReader(string(body))))
		if tagRecorder.Code != http.StatusCreated {
			t.Fatalf("create tag: %d %s", tagRecorder.Code, tagRecorder.Body.String())
		}
		var tagBody struct {
			Tag struct {
				ID string `json:"id"`
			} `json:"tag"`
		}
		if err := json.NewDecoder(tagRecorder.Body).Decode(&tagBody); err != nil {
			t.Fatal(err)
		}
		tagIDs[index] = tagBody.Tag.ID
	}

	paperRecorder := httptest.NewRecorder()
	handler.ServeHTTP(paperRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/research/papers",
		strings.NewReader(`{"kind":"research","title":"Tagged"}`)))
	if paperRecorder.Code != http.StatusCreated {
		t.Fatalf("create paper: %d %s", paperRecorder.Code, paperRecorder.Body.String())
	}
	var paper struct {
		ID     string   `json:"id"`
		TagIDs []string `json:"tag_ids"`
	}
	if err := json.NewDecoder(paperRecorder.Body).Decode(&paper); err != nil {
		t.Fatal(err)
	}
	// A brand-new paper answers an empty list, never null.
	if paper.TagIDs == nil || len(paper.TagIDs) != 0 {
		t.Fatalf("expected a fresh paper to answer an empty tag list, got %#v", paper.TagIDs)
	}

	assignment, _ := json.Marshal(map[string]any{"tag_ids": tagIDs})
	patchRecorder := httptest.NewRecorder()
	handler.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, "/api/v1/research/papers/"+paper.ID,
		bytes.NewReader(assignment)))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("tag paper: %d %s", patchRecorder.Code, patchRecorder.Body.String())
	}
	var patched struct {
		TagIDs []string `json:"tag_ids"`
	}
	if err := json.NewDecoder(patchRecorder.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	if len(patched.TagIDs) != 2 || patched.TagIDs[0] != tagIDs[0] || patched.TagIDs[1] != tagIDs[1] {
		t.Fatalf("paper kept the wrong labels: %#v", patched.TagIDs)
	}

	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete,
		"/api/v1/research/tags/"+tagIDs[0], nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete tag: %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet,
		"/api/v1/research/papers/"+paper.ID, nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get paper: %d", getRecorder.Code)
	}
	var reloaded struct {
		TagIDs []string `json:"tag_ids"`
	}
	if err := json.NewDecoder(getRecorder.Body).Decode(&reloaded); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.TagIDs) != 1 || reloaded.TagIDs[0] != tagIDs[1] {
		t.Fatalf("deleting one tag should leave the other, got %#v", reloaded.TagIDs)
	}

	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet,
		"/api/v1/research/papers?kind=research", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list papers: %d", listRecorder.Code)
	}
	var listed struct {
		Items []struct {
			ID     string   `json:"id"`
			TagIDs []string `json:"tag_ids"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || len(listed.Items[0].TagIDs) != 1 ||
		listed.Items[0].TagIDs[0] != tagIDs[1] {
		t.Fatalf("the list route lost the labels: %+v", listed.Items)
	}
}

func TestReorderResearchTagsRoute(t *testing.T) {
	server := newTestServer(t)
	resetResearchTagPalette(t, server)
	handler := server.Handler()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	ids := make([]string, 3)
	for index, name := range []string{"急件", "在改", "合作者"} {
		payload, _ := json.Marshal(map[string]string{"name": name})
		created := do(http.MethodPost, "/api/v1/research/tags", string(payload))
		if created.Code != http.StatusCreated {
			t.Fatalf("create tag: %d %s", created.Code, created.Body.String())
		}
		var body struct {
			Tag struct {
				ID string `json:"id"`
			} `json:"tag"`
		}
		if err := json.NewDecoder(created.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		ids[index] = body.Tag.ID
	}

	ordered, _ := json.Marshal(map[string]any{"tag_ids": []string{ids[2], ids[1], ids[0], "never-a-tag"}})
	reordered := do(http.MethodPost, "/api/v1/research/tags/reorder", string(ordered))
	if reordered.Code != http.StatusNoContent {
		t.Fatalf("reorder: expected 204, got %d: %s", reordered.Code, reordered.Body.String())
	}

	list := do(http.MethodGet, "/api/v1/research/tags", "")
	var listBody struct {
		Tags []struct {
			ID       string `json:"id"`
			Position int    `json:"position"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listBody); err != nil {
		t.Fatal(err)
	}
	want := []string{ids[2], ids[1], ids[0]}
	if len(listBody.Tags) != len(want) {
		t.Fatalf("unexpected palette: %+v", listBody.Tags)
	}
	for index, tag := range listBody.Tags {
		if tag.ID != want[index] || tag.Position != index {
			t.Fatalf("tag %d should be %q at position %d, got %+v", index, want[index], index, tag)
		}
	}

	oversized := make([]string, 501)
	for index := range oversized {
		oversized[index] = "id"
	}
	payload, _ := json.Marshal(map[string]any{"tag_ids": oversized})
	tooMany := do(http.MethodPost, "/api/v1/research/tags/reorder", string(payload))
	if tooMany.Code != http.StatusBadRequest {
		t.Fatalf("oversized: expected 400, got %d: %s", tooMany.Code, tooMany.Body.String())
	}
	if code := problemCode(t, tooMany); code != "invalid_research_tag" {
		t.Fatalf("oversized: unexpected problem code %q", code)
	}
}

func TestUpdateResearchPaperRejectsTooManyTags(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/v1/research/papers",
		strings.NewReader(`{"kind":"research","title":"Crowded"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create paper: %d %s", created.Code, created.Body.String())
	}
	var paper struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&paper); err != nil {
		t.Fatal(err)
	}

	// Repeats collapse, so thirteen distinct ids is the smallest rejection.
	tagIDs := make([]string, 13)
	for index := range tagIDs {
		tagIDs[index] = fmt.Sprintf("tag-%d", index)
	}
	tagIDs = append(tagIDs, tagIDs[0])
	payload, _ := json.Marshal(map[string]any{"tag_ids": tagIDs})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/v1/research/papers/"+paper.ID,
		bytes.NewReader(payload)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if code := problemCode(t, recorder); code != "invalid_research_paper" {
		t.Fatalf("unexpected problem code %q", code)
	}
}

func problemCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return problem.Code
}
