package httpapi

import (
	"encoding/json"
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

func TestDeleteResearchTagClearsPaperReference(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	tagRecorder := httptest.NewRecorder()
	handler.ServeHTTP(tagRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/research/tags",
		strings.NewReader(`{"name":"在改"}`)))
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

	paperRecorder := httptest.NewRecorder()
	handler.ServeHTTP(paperRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/research/papers",
		strings.NewReader(`{"kind":"research","title":"Tagged"}`)))
	if paperRecorder.Code != http.StatusCreated {
		t.Fatalf("create paper: %d %s", paperRecorder.Code, paperRecorder.Body.String())
	}
	var paper struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(paperRecorder.Body).Decode(&paper); err != nil {
		t.Fatal(err)
	}

	patchRecorder := httptest.NewRecorder()
	handler.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, "/api/v1/research/papers/"+paper.ID,
		strings.NewReader(`{"tag_id":"`+tagBody.Tag.ID+`"}`)))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("tag paper: %d %s", patchRecorder.Code, patchRecorder.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete,
		"/api/v1/research/tags/"+tagBody.Tag.ID, nil))
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
		TagID string `json:"tag_id"`
	}
	if err := json.NewDecoder(getRecorder.Body).Decode(&reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.TagID != "" {
		t.Fatalf("expected paper untagged, got %q", reloaded.TagID)
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
