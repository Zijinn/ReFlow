package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResearchTagRoutes(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	created := do(http.MethodPost, "/api/v1/research/tags", `{"name":"  急件  "}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var createBody struct {
		Tag struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Position int    `json:"position"`
		} `json:"tag"`
	}
	if err := json.NewDecoder(created.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if createBody.Tag.Name != "急件" || createBody.Tag.ID == "" {
		t.Fatalf("unexpected tag: %+v", createBody.Tag)
	}

	duplicate := do(http.MethodPost, "/api/v1/research/tags", `{"name":"急件"}`)
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

	list := do(http.MethodGet, "/api/v1/research/tags", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", list.Code)
	}
	var listBody struct {
		Tags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Tags) != 1 || listBody.Tags[0].ID != createBody.Tag.ID {
		t.Fatalf("unexpected palette: %+v", listBody.Tags)
	}

	renamed := do(http.MethodPatch, "/api/v1/research/tags/"+createBody.Tag.ID, `{"name":"特急"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", renamed.Code, renamed.Body.String())
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
