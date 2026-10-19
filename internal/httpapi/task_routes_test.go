package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

// taskClient drives the 长任务 routes through the real routing table so a test
// sees what the board client sees: the whole nested tree on read, and every write
// answered by the row re-read through that tree, derived fields included.
type taskClient struct {
	t *testing.T
	// do is the one request shape every board route shares: method, path, body.
	do func(method, target, body string) *httptest.ResponseRecorder
}

func newTaskClient(t *testing.T) *taskClient {
	t.Helper()
	handler := newTestServer(t).Handler()
	return &taskClient{t: t, do: func(method, target, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		handler.ServeHTTP(recorder, request)
		return recorder
	}}
}

// call asserts the status the contract promises for this route and decodes the
// body into out when the caller asked for it. 204 carries no body, so decoding is
// skipped rather than failing on an empty reader.
func (c *taskClient) call(want int, method, target, body string, out any) {
	c.t.Helper()
	recorder := c.do(method, target, body)
	if recorder.Code != want {
		c.t.Fatalf("%s %s: expected %d, got %d: %s", method, target, want, recorder.Code, recorder.Body.String())
	}
	if out == nil || recorder.Code == http.StatusNoContent {
		return
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), out); err != nil {
		c.t.Fatalf("%s %s: decode body: %v", method, target, err)
	}
}

// threads reads the whole board document, the one read endpoint the board has.
func (c *taskClient) threads() []domain.TaskThread {
	c.t.Helper()
	var body struct {
		Items []domain.TaskThread `json:"items"`
	}
	c.call(http.StatusOK, http.MethodGet, "/api/v1/tasks/threads", "", &body)
	return body.Items
}

// thread reloads one 主线 with its full tree.
func (c *taskClient) thread(id string) domain.TaskThread {
	c.t.Helper()
	for _, thread := range c.threads() {
		if thread.ID == id {
			return thread
		}
	}
	c.t.Fatalf("thread %s missing from the board document", id)
	return domain.TaskThread{}
}

func (c *taskClient) createThread(name string) domain.TaskThread {
	c.t.Helper()
	var thread domain.TaskThread
	body := `{"name":` + jsonString(name) + `}`
	c.call(http.StatusCreated, http.MethodPost, "/api/v1/tasks/threads", body, &thread)
	return thread
}

func (c *taskClient) createLayer(threadID, rawBody string) domain.TaskLayer {
	c.t.Helper()
	var layer domain.TaskLayer
	c.call(http.StatusCreated, http.MethodPost,
		"/api/v1/tasks/threads/"+threadID+"/layers", rawBody, &layer)
	return layer
}

func (c *taskClient) createItem(threadID, rawBody string) domain.TaskItem {
	c.t.Helper()
	var item domain.TaskItem
	c.call(http.StatusCreated, http.MethodPost,
		"/api/v1/tasks/threads/"+threadID+"/items", rawBody, &item)
	return item
}

func (c *taskClient) createAssignment(itemID, rawBody string) domain.TaskAssignment {
	c.t.Helper()
	var assignment domain.TaskAssignment
	c.call(http.StatusCreated, http.MethodPost,
		"/api/v1/tasks/items/"+itemID+"/assignments", rawBody, &assignment)
	return assignment
}

func (c *taskClient) createStep(assignmentID, name string) domain.TaskStep {
	c.t.Helper()
	var step domain.TaskStep
	c.call(http.StatusCreated, http.MethodPost,
		"/api/v1/tasks/assignments/"+assignmentID+"/steps", `{"name":`+jsonString(name)+`}`, &step)
	return step
}

// problem asserts the status and the RFC 7807 code of a rejected board request.
func (c *taskClient) problem(want int, code, method, target, body string) {
	c.t.Helper()
	recorder := c.do(method, target, body)
	if recorder.Code != want {
		c.t.Fatalf("%s %s: expected %d, got %d: %s", method, target, want, recorder.Code, recorder.Body.String())
	}
	if got := problemCode(c.t, recorder); got != code {
		c.t.Fatalf("%s %s: expected problem code %q, got %q", method, target, code, got)
	}
}

func jsonString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func findTaskLayer(thread domain.TaskThread, id string) (domain.TaskLayer, bool) {
	for _, layer := range thread.Layers {
		if layer.ID == id {
			return layer, true
		}
	}
	return domain.TaskLayer{}, false
}

func findTaskItem(thread domain.TaskThread, id string) (domain.TaskItem, bool) {
	for _, item := range thread.Items {
		if item.ID == id {
			return item, true
		}
	}
	return domain.TaskItem{}, false
}

// TestTaskRoutesBoardLifecycle walks every board route once, in the order the owner
// uses them, and checks the derived fields the routes are supposed to answer with.
// A write that returns the stored row instead of the re-read tree would look fine
// here and break the client.
func TestTaskRoutesBoardLifecycle(t *testing.T) {
	client := newTaskClient(t)

	// An empty 主线 is a legal first response: the client paints a thread with no
	// layer, and the first layer added to it has no predecessor to wait for.
	thread := client.createThread("论文 + 项目")
	if thread.Name != "论文 + 项目" || len(thread.Layers) != 0 || len(thread.Items) != 0 {
		t.Fatalf("a new thread should come back empty: %+v", thread)
	}
	if thread.Stats != (domain.TaskStats{}) {
		t.Fatalf("an empty thread should report zeroed stats: %+v", thread.Stats)
	}

	first := client.createLayer(thread.ID, `{}`)
	if first.Name != "第1层" || first.Position != 0 || !first.Unlocked {
		t.Fatalf("a nameless layer takes the 第N层 default and opens first: %+v", first)
	}

	card := client.createItem(thread.ID, `{"title":"返修稿","layer_id":`+jsonString(first.ID)+`}`)
	if len(card.Assignments) != 1 {
		t.Fatalf("creating with layer_id should also place the card: %+v", card.Assignments)
	}
	firstPlacement := card.Assignments[0]

	// The second layer is shut, because layer one now holds an open placement.
	second := client.createLayer(thread.ID, `{"name":"终稿"}`)
	if second.Name != "终稿" || second.Position != 1 || second.Unlocked {
		t.Fatalf("layer two should start locked behind layer one: %+v", second)
	}

	start := time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")
	due := time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02")
	secondPlacement := client.createAssignment(card.ID,
		`{"layer_id":`+jsonString(second.ID)+`,"start_date":`+jsonString(start)+
			`,"due_date":`+jsonString(due)+`}`)
	if secondPlacement.LayerID != second.ID || secondPlacement.LayerPosition != 1 {
		t.Fatalf("the placement should carry its layer and that layer's position: %+v", secondPlacement)
	}
	if secondPlacement.StartDate != start || secondPlacement.DueDate != due {
		t.Fatalf("the window did not land: %+v", secondPlacement)
	}

	reply := client.createStep(firstPlacement.ID, "回信")
	figures := client.createStep(firstPlacement.ID, "改图")
	if reply.Done || reply.DoneAt != "" || reply.Position != 0 || figures.Position != 1 {
		t.Fatalf("a new step is undone and appended: %+v %+v", reply, figures)
	}
	// The second placement carries the three undone steps of the reference board's
	// case: the same task is 2/2 on one layer and 0/3 on the other.
	for _, name := range []string{"补实验 A", "补实验 B", "重跑稳健性"} {
		client.createStep(secondPlacement.ID, name)
	}

	// done_at follows the flip in both directions, and a flip back clears it rather
	// than leaving a stamp behind that the chip would still print.
	var ticked domain.TaskStep
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/steps/"+reply.ID, `{"done":true}`, &ticked)
	if !ticked.Done || ticked.DoneAt == "" {
		t.Fatalf("ticking should stamp the step: %+v", ticked)
	}
	var unticked domain.TaskStep
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/steps/"+reply.ID, `{"done":false}`, &unticked)
	if unticked.Done || unticked.DoneAt != "" {
		t.Fatalf("un-ticking should clear the stamp: %+v", unticked)
	}

	// The 完成 button closes the whole checklist of this placement in one call.
	var completed domain.TaskAssignment
	client.call(http.StatusOK, http.MethodPost,
		"/api/v1/tasks/assignments/"+firstPlacement.ID+"/complete", "", &completed)
	if !completed.Closed || completed.ClosedAt == "" || len(completed.Steps) != 2 {
		t.Fatalf("completing should tick every step and stamp the placement: %+v", completed)
	}
	for _, step := range completed.Steps {
		if !step.Done {
			t.Fatalf("the button left %q undone: %+v", step.Name, completed.Steps)
		}
	}

	// One read of the tree shows what those writes derived. Layer one's only
	// placement closed, so layer two opens; the card is 2 steps of 5, which is 40 on
	// both of its cards rather than 100 on one and 0 on the other.
	board := client.thread(thread.ID)
	firstLayer, ok := findTaskLayer(board, first.ID)
	if !ok || firstLayer.ItemCount != 1 || firstLayer.ClosedCount != 1 || !firstLayer.Unlocked {
		t.Fatalf("layer one pill should read 1/1: %+v", firstLayer)
	}
	secondLayer, ok := findTaskLayer(board, second.ID)
	if !ok || !secondLayer.Unlocked {
		t.Fatalf("layer two should open once layer one closed: %+v", secondLayer)
	}
	loaded, ok := findTaskItem(board, card.ID)
	if !ok {
		t.Fatalf("the card vanished from the tree")
	}
	if loaded.Progress != 40 || loaded.Stats.TotalSteps != 5 || loaded.Stats.DoneSteps != 2 {
		t.Fatalf("progress is the whole task's, across layers: %+v", loaded)
	}
	if loaded.LayerCount != 2 || len(loaded.Assignments) != 2 {
		t.Fatalf("跨 2 层 needs both placements: %+v", loaded.Assignments)
	}
	if loaded.Assignments[0].LayerID != first.ID || loaded.Assignments[1].LayerID != second.ID {
		t.Fatalf("placements should read in layer order: %+v", loaded.Assignments)
	}
	if loaded.Closed || loaded.Status != domain.TaskStatusActive {
		t.Fatalf("one open placement keeps the card active: %+v", loaded)
	}
	if loaded.PaperID != nil {
		t.Fatalf("an unlinked card should answer null, got %q", *loaded.PaperID)
	}
	if board.Stats.Total != 1 || board.Stats.Active != 1 || board.Stats.Slots != 2 || board.Stats.SlotsClosed != 1 {
		t.Fatalf("unexpected strip and header counts: %+v", board.Stats)
	}
	if board.Stats.Active+board.Stats.Locked+board.Stats.Unstarted+board.Stats.Overdue+board.Stats.Closed != board.Stats.Total {
		t.Fatalf("the five cells must sum to total: %+v", board.Stats)
	}

	// A rename that says nothing about paper_id must not clear the link, so the
	// field is absent from this body on purpose.
	var renamed domain.TaskItem
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/items/"+card.ID,
		`{"title":"返修稿（二审）","notes":"编辑部要两版"}`, &renamed)
	if renamed.Title != "返修稿（二审）" || renamed.Notes != "编辑部要两版" || renamed.Progress != 40 {
		t.Fatalf("patch should keep the derived fields it was not asked to change: %+v", renamed)
	}

	// A card created with no layer lands on the board with no placement, which is
	// unstarted rather than locked; placing it afterwards is what grows 跨 N 层.
	queued := client.createItem(thread.ID, `{"title":"新课题"}`)
	if len(queued.Assignments) != 0 || queued.LayerCount != 0 {
		t.Fatalf("a card created without layer_id has no placement: %+v", queued)
	}
	if queued.Status != domain.TaskStatusUnstarted {
		t.Fatalf("an unplaced card should read unstarted, got %q", queued.Status)
	}
	queuedPlacement := client.createAssignment(queued.ID, `{"layer_id":`+jsonString(second.ID)+`}`)
	if queuedPlacement.LayerID != second.ID {
		t.Fatalf("the placement should land on the queued card: %+v", queuedPlacement)
	}
	if placed, ok := findTaskItem(client.thread(thread.ID), queued.ID); !ok || placed.LayerCount != 1 {
		t.Fatalf("the card should now sit in one layer: %+v", placed)
	}

	client.call(http.StatusNoContent, http.MethodPost,
		"/api/v1/tasks/threads/"+thread.ID+"/items/reorder",
		`{"item_ids":[`+jsonString(queued.ID)+`,`+jsonString(card.ID)+`]}`, nil)
	board = client.thread(thread.ID)
	if len(board.Items) != 2 || board.Items[0].ID != queued.ID || board.Items[1].ID != card.ID {
		t.Fatalf("the drag did not stick: %+v", board.Items)
	}

	// Only the end of the window the patch mentions moves; the other end stays.
	later := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	var moved domain.TaskAssignment
	client.call(http.StatusOK, http.MethodPatch,
		"/api/v1/tasks/assignments/"+secondPlacement.ID, `{"due_date":`+jsonString(later)+`}`, &moved)
	if moved.DueDate != later || moved.StartDate != start {
		t.Fatalf("a one-ended patch should leave the other end alone: %+v", moved)
	}

	// Dropping a ticked step reopens nothing: the placement still ran out its
	// checklist, and the card loses one step from its own denominator (1 done of 4).
	client.call(http.StatusNoContent, http.MethodDelete, "/api/v1/tasks/steps/"+figures.ID, "", nil)
	loaded, ok = findTaskItem(client.thread(thread.ID), card.ID)
	if !ok || loaded.Progress != 25 || loaded.Stats.TotalSteps != 4 || loaded.Closed {
		t.Fatalf("the card should be all-ticked on layer one and open on layer two: %+v", loaded)
	}

	// The layer-wide 完成 runs out every checklist it can and leaves a stepless
	// placement open, because 本层已收口 is read from the steps, not from the button.
	var closedLayer domain.TaskLayer
	client.call(http.StatusOK, http.MethodPost,
		"/api/v1/tasks/layers/"+second.ID+"/close", "", &closedLayer)
	if closedLayer.ID != second.ID || closedLayer.ItemCount != 2 || closedLayer.ClosedCount != 1 {
		t.Fatalf("layer two should close one placement of two: %+v", closedLayer)
	}
	loaded, ok = findTaskItem(client.thread(thread.ID), card.ID)
	if !ok || !loaded.Closed || loaded.ClosedAt == "" || loaded.Status != domain.TaskStatusClosed ||
		loaded.Progress != 100 {
		t.Fatalf("both placements closed makes the card 已收口: %+v", loaded)
	}
	queuedCard, ok := findTaskItem(client.thread(thread.ID), queued.ID)
	if !ok || queuedCard.Closed || queuedCard.Status != domain.TaskStatusUnstarted {
		t.Fatalf("a stepless placement is neither closed nor started: %+v", queuedCard)
	}

	var renamedLayer domain.TaskLayer
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/layers/"+second.ID,
		`{"name":"终稿二审","forced_unlock":true}`, &renamedLayer)
	if renamedLayer.Name != "终稿二审" || !renamedLayer.ForcedUnlock {
		t.Fatalf("layer patch did not land: %+v", renamedLayer)
	}

	// Taking the card out of layer two leaves the placement that ran out its
	// checklist, so it stays 已收口 with one layer under it.
	client.call(http.StatusNoContent, http.MethodDelete,
		"/api/v1/tasks/assignments/"+secondPlacement.ID, "", nil)
	board = client.thread(thread.ID)
	loaded, ok = findTaskItem(board, card.ID)
	if !ok || !loaded.Closed || loaded.LayerCount != 1 ||
		loaded.Status != domain.TaskStatusClosed {
		t.Fatalf("a card whose only placement is closed is 已收口: %+v", loaded)
	}
	if board.Stats.Closed != 1 || board.Stats.Unstarted != 1 || board.Stats.Total != 2 ||
		board.Stats.Slots != 2 || board.Stats.SlotsClosed != 1 {
		t.Fatalf("unexpected counts after the removal: %+v", board.Stats)
	}

	client.call(http.StatusNoContent, http.MethodDelete, "/api/v1/tasks/items/"+queued.ID, "", nil)
	client.call(http.StatusNoContent, http.MethodDelete, "/api/v1/tasks/layers/"+second.ID, "", nil)
	board = client.thread(thread.ID)
	if len(board.Items) != 1 || len(board.Layers) != 1 {
		t.Fatalf("the deletes should leave one card and one layer: %+v", board)
	}

	// Archived threads stay in the document; the client hides them by the flag.
	var archived domain.TaskThread
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/threads/"+thread.ID,
		`{"archived":true}`, &archived)
	if !archived.Archived || archived.Name != "论文 + 项目" {
		t.Fatalf("archiving should not lose the name: %+v", archived)
	}
	if len(client.threads()) != 1 {
		t.Fatalf("an archived thread should still be listed: %+v", client.threads())
	}

	client.call(http.StatusNoContent, http.MethodDelete, "/api/v1/tasks/threads/"+thread.ID, "", nil)
	if got := client.threads(); len(got) != 0 {
		t.Fatalf("deleting the thread should empty the board: %+v", got)
	}
}

// TestTaskRoutesRejectBadBoardWrites covers the 400/404/409 answers the board can
// produce, and that a lock is never one of them.
func TestTaskRoutesRejectBadBoardWrites(t *testing.T) {
	client := newTaskClient(t)
	thread := client.createThread("校验")
	first := client.createLayer(thread.ID, `{"name":"初审"}`)
	card := client.createItem(thread.ID, `{"title":"锁住下层","layer_id":`+jsonString(first.ID)+`}`)
	second := client.createLayer(thread.ID, `{"name":"终审"}`)

	// A shut layer is a status, not a permission: this is the reference board's
	// 第三层 holding three cards, and the route must not answer 409 for it.
	queued := client.createItem(thread.ID, `{"title":"排期","layer_id":`+jsonString(second.ID)+`}`)
	if len(queued.Assignments) != 1 {
		t.Fatalf("queuing into a locked layer should place the card: %+v", queued)
	}
	client.createAssignment(card.ID, `{"layer_id":`+jsonString(second.ID)+`}`)
	var patched domain.TaskItem
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/items/"+queued.ID,
		`{"notes":"锁定中也能改"}`, &patched)
	if patched.Notes != "锁定中也能改" {
		t.Fatalf("a locked card should still take an edit: %+v", patched)
	}
	// Omitting the title is how the board's 加一张卡 arrives: a placeholder rather
	// than a 400, since the owner names the card afterwards.
	placeholder := client.createItem(thread.ID, `{}`)
	if placeholder.Title != "未命名任务" || len(placeholder.Assignments) != 0 {
		t.Fatalf("a titleless card should get the placeholder: %+v", placeholder)
	}
	if layer, ok := findTaskLayer(client.thread(thread.ID), second.ID); !ok || layer.Unlocked {
		t.Fatalf("none of those writes should have opened the layer: %+v", layer)
	}

	badDates := []string{
		`{"layer_id":` + jsonString(second.ID) + `,"due_date":"31-10-2026"}`,
		`{"layer_id":` + jsonString(second.ID) + `,"start_date":"2026-13-01"}`,
		`{"layer_id":` + jsonString(second.ID) + `,"due_date":"2026-02-30"}`,
		`{"layer_id":` + jsonString(first.ID) + `,"start_date":"2026-10-12","due_date":"2026-10-11"}`,
	}
	// The date is checked before the duplicate lookup, so every one of these is a
	// 400 about the window rather than a 409 about the placement.
	for _, body := range badDates {
		client.problem(http.StatusBadRequest, "invalid_task_assignment",
			http.MethodPost, "/api/v1/tasks/items/"+card.ID+"/assignments", body)
	}

	// The card is already in 终审 from the queueing above, so this is the duplicate.
	client.problem(http.StatusConflict, "assignment_exists", http.MethodPost,
		"/api/v1/tasks/items/"+card.ID+"/assignments", `{"layer_id":`+jsonString(second.ID)+`}`)

	// Each entity reports its own code, so the card editor and the layer pill can
	// show the right field as broken.
	client.problem(http.StatusBadRequest, "invalid_task_thread", http.MethodPost,
		"/api/v1/tasks/threads", `{"name":"   "}`)
	client.problem(http.StatusBadRequest, "invalid_task_layer", http.MethodPost,
		"/api/v1/tasks/threads/"+thread.ID+"/layers", `{"name":""}`)
	client.problem(http.StatusBadRequest, "invalid_task_item", http.MethodPost,
		"/api/v1/tasks/threads/"+thread.ID+"/items", `{"title":" "}`)
	client.problem(http.StatusBadRequest, "invalid_task_item", http.MethodPost,
		"/api/v1/tasks/threads/"+thread.ID+"/items", `{"title":`+jsonString(strings.Repeat("长", 501))+`}`)
	client.problem(http.StatusBadRequest, "invalid_task_item", http.MethodPatch,
		"/api/v1/tasks/items/"+queued.ID, `{"notes":`+jsonString(strings.Repeat("字", 2001))+`}`)
	client.problem(http.StatusBadRequest, "invalid_task_step", http.MethodPost,
		"/api/v1/tasks/assignments/"+queued.Assignments[0].ID+"/steps", `{"name":"  "}`)

	// An id the board does not know is a 404 through the same path the research
	// routes take, and a field the contract does not have is the client's mistake.
	missing := "00000000-0000-4000-8000-000000000000"
	client.problem(http.StatusNotFound, "not_found", http.MethodPatch, "/api/v1/tasks/items/"+missing, `{"title":"没了"}`)
	client.problem(http.StatusNotFound, "not_found", http.MethodPost,
		"/api/v1/tasks/threads/"+missing+"/layers", `{}`)
	client.problem(http.StatusNotFound, "not_found", http.MethodDelete, "/api/v1/tasks/steps/"+missing, "")
	client.problem(http.StatusNotFound, "not_found", http.MethodDelete, "/api/v1/tasks/threads/"+missing, "")
	client.problem(http.StatusBadRequest, "invalid_request", http.MethodPatch,
		"/api/v1/tasks/threads/"+thread.ID, `{"nope":1}`)

	// A placement cannot reach across two boards.
	other := client.createThread("另一块板")
	otherLayer := client.createLayer(other.ID, `{"name":"隔壁"}`)
	client.problem(http.StatusBadRequest, "invalid_task_assignment", http.MethodPost,
		"/api/v1/tasks/items/"+card.ID+"/assignments", `{"layer_id":`+jsonString(otherLayer.ID)+`}`)
}

// TestTaskItemPaperLinkIsTriState pins the one PATCH field with three meanings,
// because a card editor that only renames must not silently unlink the paper.
func TestTaskItemPaperLinkIsTriState(t *testing.T) {
	client := newTaskClient(t)
	var paper struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	client.call(http.StatusCreated, http.MethodPost, "/api/v1/research/papers",
		`{"kind":"research","title":"面板数据"}`, &paper)
	if paper.ID == "" {
		t.Fatalf("the paper fixture should come back with an id")
	}

	thread := client.createThread("挂论文")
	layer := client.createLayer(thread.ID, `{}`)
	card := client.createItem(thread.ID, `{"title":"robustness","layer_id":`+jsonString(layer.ID)+`}`)

	var linked domain.TaskItem
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/items/"+card.ID,
		`{"paper_id":`+jsonString(paper.ID)+`}`, &linked)
	if linked.PaperID == nil || *linked.PaperID != paper.ID {
		t.Fatalf("a string should link the card: %+v", linked.PaperID)
	}

	// Absent means untouched.
	var rename domain.TaskItem
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/items/"+card.ID,
		`{"title":"robustness 二稿"}`, &rename)
	if rename.PaperID == nil || *rename.PaperID != paper.ID {
		t.Fatalf("a rename must not clear the link: %+v", rename.PaperID)
	}
	if rename.Title != "robustness 二稿" {
		t.Fatalf("the rename did not land: %+v", rename)
	}

	// Explicit null is the unlink signal, and it has to survive the JSON encoder.
	var unlinked domain.TaskItem
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/items/"+card.ID,
		`{"paper_id":null}`, &unlinked)
	if unlinked.PaperID != nil {
		t.Fatalf("null should clear the link, got %q", *unlinked.PaperID)
	}

	// Re-link, then check the tree carries the same value the PATCH answered with.
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/items/"+card.ID,
		`{"paper_id":`+jsonString(paper.ID)+`}`, &linked)
	loaded, ok := findTaskItem(client.thread(thread.ID), card.ID)
	if !ok || loaded.PaperID == nil || *loaded.PaperID != paper.ID {
		t.Fatalf("the board document lost the paper link: %+v", loaded)
	}

	// A paper the workspace does not have is rejected rather than stored dangling.
	client.problem(http.StatusBadRequest, "invalid_task_item", http.MethodPatch,
		"/api/v1/tasks/items/"+card.ID, `{"paper_id":"没有这篇论文"}`)

	// Deleting the paper leaves the card on the board with the link cleared, which is
	// why paper_id is nullable instead of a hard reference.
	client.call(http.StatusNoContent, http.MethodDelete, "/api/v1/research/papers/"+paper.ID, "", nil)
	loaded, ok = findTaskItem(client.thread(thread.ID), card.ID)
	if !ok {
		t.Fatalf("deleting a paper must not delete the card")
	}
	if loaded.PaperID != nil {
		t.Fatalf("the card should outlive its paper with a cleared link, got %q", *loaded.PaperID)
	}
}

// TestTaskRoutesOverdueNeedsAnOpenLayer pins the order the strip is built in: a late
// card nobody can reach yet reads locked, and forcing the layer open turns the same
// card red without touching its dates.
func TestTaskRoutesOverdueNeedsAnOpenLayer(t *testing.T) {
	client := newTaskClient(t)
	thread := client.createThread("口径")
	first := client.createLayer(thread.ID, `{}`)
	client.createItem(thread.ID, `{"title":"堵住下层","layer_id":`+jsonString(first.ID)+`}`)
	second := client.createLayer(thread.ID, `{}`)

	past := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	// The card is created unplaced so its only placement is the one carrying the late
	// date, which keeps this test about the status order rather than about 409s.
	late := client.createItem(thread.ID, `{"title":"迟到的活"}`)
	placement := client.createAssignment(late.ID, `{"layer_id":`+jsonString(second.ID)+`}`)
	client.createStep(placement.ID, "还有一步")
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/assignments/"+placement.ID,
		`{"due_date":`+jsonString(past)+`}`, nil)

	loaded, ok := findTaskItem(client.thread(thread.ID), late.ID)
	if !ok {
		t.Fatalf("the late card is missing")
	}
	if loaded.Status != domain.TaskStatusLocked {
		t.Fatalf("a late card behind a shut layer reads locked, got %q", loaded.Status)
	}

	var forced domain.TaskLayer
	client.call(http.StatusOK, http.MethodPatch, "/api/v1/tasks/layers/"+second.ID,
		`{"forced_unlock":true}`, &forced)
	if !forced.Unlocked {
		t.Fatalf("the escape hatch should open the layer: %+v", forced)
	}
	loaded, _ = findTaskItem(client.thread(thread.ID), late.ID)
	if loaded.Status != domain.TaskStatusOverdue {
		t.Fatalf("the same card once reachable reads overdue, got %q", loaded.Status)
	}
	board := client.thread(thread.ID)
	if board.Stats.Overdue != 1 || board.Stats.Locked != 0 {
		t.Fatalf("the strip should move that card between cells: %+v", board.Stats)
	}
}
