package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
)

// newTaskTestDB opens a migrated database the same way the research tests do: the
// board lives in the same file as the rest of the library, so a test that seeds a
// paper link needs a real profiles row and a real research_papers table.
func newTaskTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "reflow.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// mustThread creates a board and returns its id.
func mustThread(t *testing.T, ctx context.Context, db *sql.DB, name string) string {
	t.Helper()
	thread, err := CreateTaskThread(ctx, db, domain.DefaultProfileID, name)
	if err != nil {
		t.Fatalf("create thread %q: %v", name, err)
	}
	return thread.ID
}

func mustLayer(t *testing.T, ctx context.Context, db *sql.DB, threadID string, name *string) domain.TaskLayer {
	t.Helper()
	layer, err := CreateTaskLayer(ctx, db, domain.DefaultProfileID, threadID, name)
	if err != nil {
		t.Fatalf("create layer: %v", err)
	}
	return layer
}

func mustItem(t *testing.T, ctx context.Context, db *sql.DB, threadID, title, layerID string) domain.TaskItem {
	t.Helper()
	item, err := CreateTaskItem(ctx, db, domain.DefaultProfileID, threadID, title, layerID)
	if err != nil {
		t.Fatalf("create item %q: %v", title, err)
	}
	return item
}

func mustStep(t *testing.T, ctx context.Context, db *sql.DB, assignmentID, name string) domain.TaskStep {
	t.Helper()
	step, err := CreateTaskStep(ctx, db, domain.DefaultProfileID, assignmentID, name)
	if err != nil {
		t.Fatalf("create step %q: %v", name, err)
	}
	return step
}

func mustDate(t *testing.T, ctx context.Context, db *sql.DB, assignmentID, start, due string) {
	t.Helper()
	if _, err := UpdateTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID,
		domain.TaskAssignmentPatch{StartDate: &start, DueDate: &due}); err != nil {
		t.Fatalf("set dates on assignment %s: %v", assignmentID, err)
	}
}

func doneValue(done bool) *bool { return &done }

// taskThread reloads one board.
func taskThread(t *testing.T, ctx context.Context, db *sql.DB, threadID string) domain.TaskThread {
	t.Helper()
	thread, err := GetTaskThread(ctx, db, domain.DefaultProfileID, threadID)
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	return thread
}

func findTaskItem(thread domain.TaskThread, id string) (domain.TaskItem, error) {
	for _, item := range thread.Items {
		if item.ID == id {
			return item, nil
		}
	}
	return domain.TaskItem{}, errors.New("item " + id + " missing from thread")
}

func findTaskLayer(thread domain.TaskThread, id string) (domain.TaskLayer, error) {
	for _, layer := range thread.Layers {
		if layer.ID == id {
			return layer, nil
		}
	}
	return domain.TaskLayer{}, errors.New("layer " + id + " missing from thread")
}

// twoLayerBoard is the smallest board that can express a lock: 层一 with open work
// keeps 层二 shut.
func twoLayerBoard(t *testing.T, ctx context.Context, db *sql.DB) (string, domain.TaskLayer, domain.TaskLayer) {
	t.Helper()
	threadID := mustThread(t, ctx, db, "论文返修")
	first := mustLayer(t, ctx, db, threadID, nil)
	second := mustLayer(t, ctx, db, threadID, nil)
	return threadID, first, second
}

// TestTaskUnlockChainIsGatedByThePreviousLayer fixes the meaning of a 层: the next
// one opens when every placement of the previous one closed, and an explicit forced
// unlock overrides that without touching the previous layer's work.
func TestTaskUnlockChainIsGatedByThePreviousLayer(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)

	// The first layer is always open, so it is the gate and never the locked one.
	layer, err := findTaskLayer(taskThread(t, ctx, db, threadID), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !layer.Unlocked {
		t.Fatalf("the first layer must always be open: %+v", layer)
	}

	// A queued card keeps layer two shut: 0/1 is not closed.
	open := mustItem(t, ctx, db, threadID, "补实验", first.ID)
	loaded, err := findTaskLayer(taskThread(t, ctx, db, threadID), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Unlocked {
		t.Fatalf("layer two opened while layer one still has %s open", open.Assignments[0].ID)
	}

	// Closing the only placement of layer one is what opens layer two.
	step := mustStep(t, ctx, db, open.Assignments[0].ID, "跑完回归")
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, step.ID,
		domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
		t.Fatalf("tick step: %v", err)
	}
	thread := taskThread(t, ctx, db, threadID)
	firstLayer, err := findTaskLayer(thread, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstLayer.ItemCount != 1 || firstLayer.ClosedCount != 1 {
		t.Fatalf("layer one pill should read 1/1, got %+v", firstLayer)
	}
	secondLayer, err := findTaskLayer(thread, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !secondLayer.Unlocked {
		t.Fatalf("layer two should open once layer one closed: %+v", secondLayer)
	}

	// Un-ticking the step closes layer one again and re-shuts layer two: the unlock
	// is a read of the checklists, not a latch that remembers the first closing.
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, step.ID,
		domain.TaskStepPatch{Done: doneValue(false)}); err != nil {
		t.Fatalf("un-tick step: %v", err)
	}
	secondLayer, err = findTaskLayer(taskThread(t, ctx, db, threadID), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondLayer.Unlocked {
		t.Fatalf("layer two stayed open after layer one reopened: %+v", secondLayer)
	}

	// The escape hatch opens a layer whose predecessor is stuck, and dropping it
	// back restores the chain.
	forced := true
	if _, err := UpdateTaskLayer(ctx, db, domain.DefaultProfileID, second.ID,
		domain.TaskLayerPatch{ForcedUnlock: &forced}); err != nil {
		t.Fatalf("force unlock: %v", err)
	}
	secondLayer, err = findTaskLayer(taskThread(t, ctx, db, threadID), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !secondLayer.Unlocked || !secondLayer.ForcedUnlock {
		t.Fatalf("forced unlock did not open the layer: %+v", secondLayer)
	}
	forced = false
	if _, err := UpdateTaskLayer(ctx, db, domain.DefaultProfileID, second.ID,
		domain.TaskLayerPatch{ForcedUnlock: &forced}); err != nil {
		t.Fatalf("clear forced unlock: %v", err)
	}
	secondLayer, err = findTaskLayer(taskThread(t, ctx, db, threadID), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondLayer.Unlocked {
		t.Fatalf("clearing the forced unlock should re-shut the layer: %+v", secondLayer)
	}
}

// TestTaskStatusPrecedence pins the order the four states are tested in. A card
// parked in a locked layer with a deadline that already passed reports locked,
// because the owner cannot act on a late task they cannot reach.
func TestTaskStatusPrecedence(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)

	// Something open in layer one is what locks layer two.
	mustItem(t, ctx, db, threadID, "锁住第一层", first.ID)

	past := time.Now().UTC().AddDate(0, 0, -3).Format(taskDateLayout)
	lockedLate := mustItem(t, ctx, db, threadID, "已超期但够不着", second.ID)
	mustDate(t, ctx, db, lockedLate.Assignments[0].ID, "", past)

	item, err := findTaskItem(taskThread(t, ctx, db, threadID), lockedLate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.TaskStatusLocked {
		t.Fatalf("a late card in a locked layer must read locked, got %q", item.Status)
	}

	// Forced open exposes the same card's lateness.
	forced := true
	if _, err := UpdateTaskLayer(ctx, db, domain.DefaultProfileID, second.ID,
		domain.TaskLayerPatch{ForcedUnlock: &forced}); err != nil {
		t.Fatalf("force unlock: %v", err)
	}
	item, err = findTaskItem(taskThread(t, ctx, db, threadID), lockedLate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.TaskStatusOverdue {
		t.Fatalf("an open late card must read overdue, got %q", item.Status)
	}

	// Closing the card wins over everything: 已收口 is the one state that survives a
	// late date, since finished work is not late any more.
	if _, err := CompleteTaskAssignment(ctx, db, domain.DefaultProfileID, item.Assignments[0].ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	step := mustStep(t, ctx, db, item.Assignments[0].ID, "补一步")
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, step.ID,
		domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
		t.Fatalf("tick: %v", err)
	}
	item, err = findTaskItem(taskThread(t, ctx, db, threadID), lockedLate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.TaskStatusClosed || !item.Closed {
		t.Fatalf("a card with every step ticked must read closed, got %q", item.Status)
	}

	// A card nobody has placed on a layer yet is unstarted, not locked: there is no
	// layer to be shut.
	fresh := mustItem(t, ctx, db, threadID, "刚拖上板", "")
	item, err = findTaskItem(taskThread(t, ctx, db, threadID), fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != domain.TaskStatusUnstarted {
		t.Fatalf("an unplaced card should read unstarted, got %q", item.Status)
	}
	if len(item.Assignments) != 0 || item.LayerCount != 0 {
		t.Fatalf("an unplaced card should carry no placements: %+v", item)
	}
}

// TestTaskProgressIsGlobalAcrossLayers is the reference board's 40: a task that is
// 2/2 in its first layer and 0/3 in its second is one task at 40%, and both of its
// cards say so. A per-layer percentage would read as two disagreeing tasks.
func TestTaskProgressIsGlobalAcrossLayers(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)

	card := mustItem(t, ctx, db, threadID, "返修稿", first.ID)
	assignment, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID, "", "")
	if err != nil {
		t.Fatalf("place card in layer two: %v", err)
	}
	for _, name := range []string{"回信", "改图"} {
		step := mustStep(t, ctx, db, card.Assignments[0].ID, name)
		if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, step.ID,
			domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
			t.Fatalf("tick %s: %v", name, err)
		}
	}
	for _, name := range []string{"补实验 A", "补实验 B", "重跑稳健性"} {
		mustStep(t, ctx, db, assignment.ID, name)
	}

	item, err := findTaskItem(taskThread(t, ctx, db, threadID), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Progress != 40 {
		t.Fatalf("2/2 plus 0/3 should be one task at 40, got %d", item.Progress)
	}
	if item.Stats.TotalSteps != 5 || item.Stats.DoneSteps != 2 {
		t.Fatalf("unexpected step tally: %+v", item.Stats)
	}
	if item.LayerCount != 2 {
		t.Fatalf("跨 2 层 needs layer_count 2, got %d", item.LayerCount)
	}
	// One layer closed, the other open: the card is not 已收口, so it stays active.
	if item.Assignments[0].Closed != true || item.Assignments[1].Closed != false {
		t.Fatalf("layer one's placement should be closed, layer two's not: %+v", item.Assignments)
	}
	if item.Closed || item.Status == domain.TaskStatusClosed {
		t.Fatalf("a card with an open placement is not 已收口: %+v", item)
	}
	if item.Status != domain.TaskStatusActive {
		t.Fatalf("expected active, got %q", item.Status)
	}

	// The layer pills count placements, and only the closed one shows 1/1.
	thread := taskThread(t, ctx, db, threadID)
	firstLayer, err := findTaskLayer(thread, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondLayer, err := findTaskLayer(thread, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The layer pills count placements on their own layer: layer one's single
	// placement ran out its checklist, so it reads 1/1, while layer two's holds the
	// same card at 0/3. The 40 above is the card's, not either layer's.
	if firstLayer.ClosedCount != 1 || secondLayer.ClosedCount != 0 {
		t.Fatalf("only layer one's placement should be closed on the layer tallies: %+v %+v",
			firstLayer, secondLayer)
	}
	if firstLayer.ItemCount != 1 || secondLayer.ItemCount != 1 {
		t.Fatalf("each layer should hold one placement: %+v %+v", firstLayer, secondLayer)
	}
	// Layer two only opened because layer one's single placement closed, which is
	// the chain this test's setup intends.
	if !firstLayer.Unlocked {
		t.Fatalf("layer one must be open: %+v", firstLayer)
	}
	if !secondLayer.Unlocked {
		t.Fatalf("layer two should open once layer one's only placement closed: %+v", secondLayer)
	}
}

// TestTaskAssignmentCloseIsDerivedFromSteps keeps closed_at a display stamp: the
// boolean comes from the checklist, and un-ticking a step clears the stamp instead
// of leaving a card that looks 已收口 with an open checkbox.
func TestTaskAssignmentCloseIsDerivedFromSteps(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID := mustThread(t, ctx, db, "盖章")
	layer := mustLayer(t, ctx, db, threadID, nil)
	card := mustItem(t, ctx, db, threadID, "两勾收口", layer.ID)
	assignmentID := card.Assignments[0].ID

	// No steps yet: the placement cannot be closed, stamp or no stamp.
	loaded, err := GetTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		t.Fatalf("load placement: %v", err)
	}
	if loaded.Closed || loaded.ClosedAt != "" {
		t.Fatalf("a stepless placement is not closed: %+v", loaded)
	}
	// The 完成 button on a stepless card has nothing to close, and it must not
	// pretend otherwise.
	if _, err := CompleteTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID); err != nil {
		t.Fatalf("complete a stepless placement: %v", err)
	}
	loaded, err = GetTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		t.Fatalf("reload placement: %v", err)
	}
	if loaded.Closed || loaded.ClosedAt != "" {
		t.Fatalf("completing a stepless placement should stay open: %+v", loaded)
	}

	first := mustStep(t, ctx, db, assignmentID, "署名确认")
	second := mustStep(t, ctx, db, assignmentID, "上传最终稿")
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, first.ID,
		domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
		t.Fatalf("tick first: %v", err)
	}
	loaded, err = GetTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		t.Fatalf("reload after one tick: %v", err)
	}
	if loaded.Closed || loaded.ClosedAt != "" {
		t.Fatalf("one of two steps is not 收口: %+v", loaded)
	}

	// The button closes the checklist in one click and stamps the placement and its
	// card, since the card's only placement just closed.
	closed, err := CompleteTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !closed.Closed || closed.ClosedAt == "" {
		t.Fatalf("completing the last step should close and stamp: %+v", closed)
	}
	item, err := findTaskItem(taskThread(t, ctx, db, threadID), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Closed || item.ClosedAt == "" || item.Status != domain.TaskStatusClosed {
		t.Fatalf("the card should now be 已收口: %+v", item)
	}
	stamped := item.ClosedAt

	// Un-ticking reopens the placement and clears both stamps.
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, second.ID,
		domain.TaskStepPatch{Done: doneValue(false)}); err != nil {
		t.Fatalf("un-tick last: %v", err)
	}
	reopened, err := GetTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		t.Fatalf("reload reopened placement: %v", err)
	}
	if reopened.Closed || reopened.ClosedAt != "" {
		t.Fatalf("an open step must clear the placement stamp: %+v", reopened)
	}
	item, err = findTaskItem(taskThread(t, ctx, db, threadID), card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Closed || item.ClosedAt != "" {
		t.Fatalf("an open placement must clear the card stamp (was %q): %+v", stamped, item)
	}

	// A repeat tick of an already-done step leaves the original stamp alone, so
	// closing a layer later does not rewrite when a card actually finished.
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, second.ID,
		domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
		t.Fatalf("re-tick: %v", err)
	}
	reClosed, err := GetTaskAssignment(ctx, db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		t.Fatalf("reload reclosed placement: %v", err)
	}
	if !reClosed.Closed || reClosed.ClosedAt == "" {
		t.Fatalf("re-ticking the last step should close again: %+v", reClosed)
	}
}

// TestTaskCascadeDeleteLeavesNoSteps is the orphan check: the board's only write
// gate is referential, so a deleted 主线 must not leave a checklist behind that no
// card can reach but the next board would inherit.
func TestTaskCascadeDeleteLeavesNoSteps(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)
	card := mustItem(t, ctx, db, threadID, "会被带走", first.ID)
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID, "", ""); err != nil {
		t.Fatalf("second placement: %v", err)
	}
	step := mustStep(t, ctx, db, card.Assignments[0].ID, "打勾前的最后一步")

	count := func(query string) int {
		var total int
		if err := db.QueryRowContext(ctx, query).Scan(&total); err != nil {
			t.Fatalf("count with %q: %v", query, err)
		}
		return total
	}
	if count("SELECT COUNT(*) FROM task_steps") != 1 {
		t.Fatalf("the step should exist before the delete")
	}

	if err := DeleteTaskThread(ctx, db, domain.DefaultProfileID, threadID); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	for query, label := range map[string]string{
		"SELECT COUNT(*) FROM task_threads":     "threads",
		"SELECT COUNT(*) FROM task_layers":      "layers",
		"SELECT COUNT(*) FROM task_items":       "items",
		"SELECT COUNT(*) FROM task_assignments": "placements",
		"SELECT COUNT(*) FROM task_steps":       "steps",
	} {
		if got := count(query); got != 0 {
			t.Fatalf("deleting the thread left %d %s rows", got, label)
		}
	}
	if _, err := GetTaskStep(ctx, db, domain.DefaultProfileID, step.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the cascaded step should answer not found, got %v", err)
	}
	if err := DeleteTaskThread(ctx, db, domain.DefaultProfileID, threadID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a thread twice should answer not found, got %v", err)
	}
}

// TestTaskThreadStatsSumToTotal guards the five-cell strip: the client prints the
// counts side by side and prints slots_closed/slots in the header, so a card that
// fell through two branches would show as missing work and one counted twice would
// not add up to the number of cards on the board.
func TestTaskThreadStatsSumToTotal(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID := mustThread(t, ctx, db, "统计")
	first := mustLayer(t, ctx, db, threadID, nil)
	second := mustLayer(t, ctx, db, threadID, nil)
	past := time.Now().UTC().AddDate(0, 0, -1).Format(taskDateLayout)

	// closed: the only placement of the card, completed by the button.
	closedCard := mustItem(t, ctx, db, threadID, "已收口", first.ID)
	closedStep := mustStep(t, ctx, db, closedCard.Assignments[0].ID, "收尾")
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, closedStep.ID,
		domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
		t.Fatalf("tick closed card: %v", err)
	}
	// active: open work in the open layer, which is also what keeps layer two shut.
	activeCard := mustItem(t, ctx, db, threadID, "推进中", first.ID)
	firstStep := mustStep(t, ctx, db, activeCard.Assignments[0].ID, "一半")
	if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, firstStep.ID,
		domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
		t.Fatalf("tick active card: %v", err)
	}
	mustStep(t, ctx, db, activeCard.Assignments[0].ID, "另一半")
	// locked: a card queued into the shut layer.
	mustItem(t, ctx, db, threadID, "待解锁", second.ID)
	// unstarted: a card with no placement at all.
	mustItem(t, ctx, db, threadID, "未开始", "")
	// overdue: open work in the open layer with a date in the past.
	overdueCard := mustItem(t, ctx, db, threadID, "超期", first.ID)
	mustDate(t, ctx, db, overdueCard.Assignments[0].ID, "", past)
	mustStep(t, ctx, db, overdueCard.Assignments[0].ID, "还有一步")

	thread := taskThread(t, ctx, db, threadID)
	stats := thread.Stats
	sum := stats.Active + stats.Locked + stats.Unstarted + stats.Overdue + stats.Closed
	if sum != stats.Total {
		t.Fatalf("the five cells must add up to the card count: %+v total %d", stats, stats.Total)
	}
	if stats.Total != len(thread.Items) {
		t.Fatalf("total should be the card count: %+v items %d", stats, len(thread.Items))
	}
	if stats.Active != 1 || stats.Locked != 1 || stats.Unstarted != 1 ||
		stats.Overdue != 1 || stats.Closed != 1 {
		t.Fatalf("expected exactly one card per cell, got %+v", stats)
	}
	if stats.Total != 5 {
		t.Fatalf("expected five cards, got %+v", stats)
	}
	// Four placements exist (the unstarted card has none) and one is closed.
	if stats.Slots != 4 || stats.SlotsClosed != 1 {
		t.Fatalf("the header fraction should be 1/4, got %d/%d", stats.SlotsClosed, stats.Slots)
	}
	if thread.Layers[0].ItemCount != 3 || thread.Layers[0].ClosedCount != 1 {
		t.Fatalf("layer one pill should read 1/3, got %+v", thread.Layers[0])
	}
	if thread.Layers[1].ItemCount != 1 || thread.Layers[1].ClosedCount != 0 {
		t.Fatalf("layer two pill should read 0/1, got %+v", thread.Layers[1])
	}
}

// TestTaskLayerDefaultName keeps the 第N层 defaulting on the server: the board adds
// a layer with one click and sends nothing, so the label has to come from the
// storage layer's own ordering rule.
func TestTaskLayerDefaultName(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID := mustThread(t, ctx, db, "命名")
	for index, want := range []string{"第1层", "第2层", "第3层"} {
		layer := mustLayer(t, ctx, db, threadID, nil)
		if layer.Name != want {
			t.Fatalf("layer %d should default to %q, got %q", index, want, layer.Name)
		}
		if layer.Position != index {
			t.Fatalf("layer %d should append at position %d, got %d", index, index, layer.Position)
		}
	}
	named := mustLayer(t, ctx, db, threadID, ptrName("终审"))
	if named.Name != "终审" {
		t.Fatalf("an explicit name wins over the default, got %q", named.Name)
	}

	var validation *TaskValidationError
	if _, err := CreateTaskThread(ctx, db, domain.DefaultProfileID, "   "); !errors.As(err, &validation) {
		t.Fatalf("a blank thread name should be rejected, got %v", err)
	}
	if validation.Code() != "invalid_task_thread" {
		t.Fatalf("unexpected code %q", validation.Code())
	}
	if _, err := CreateTaskLayer(ctx, db, domain.DefaultProfileID, threadID, ptrName("")); !errors.As(err, &validation) {
		t.Fatalf("an explicit blank layer name should be rejected, got %v", err)
	}
	if _, err := CreateTaskItem(ctx, db, domain.DefaultProfileID, threadID, " ", ""); !errors.As(err, &validation) {
		t.Fatalf("a blank card title should be rejected, got %v", err)
	}
	if _, err := CreateTaskThread(ctx, db, domain.DefaultProfileID,
		strings.Repeat("标", taskTextLimit+1)); !errors.As(err, &validation) {
		t.Fatalf("an overlong thread name should be rejected, got %v", err)
	}
}

// TestTaskAssignmentValidation covers the writes the client can get wrong: a date
// in the wrong shape, a range that runs backwards, a duplicate placement, and a
// placement that reaches across two boards.
func TestTaskAssignmentValidation(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)
	otherThread := mustThread(t, ctx, db, "另一块板")
	otherLayer := mustLayer(t, ctx, db, otherThread, nil)
	card := mustItem(t, ctx, db, threadID, "排期", first.ID)

	var validation *TaskValidationError
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID,
		"2026-10-12", "31-10-2026"); !errors.As(err, &validation) {
		t.Fatalf("a wrong-shaped due date should be rejected, got %v", err)
	}
	if validation.Code() != "invalid_task_assignment" {
		t.Fatalf("unexpected code %q", validation.Code())
	}
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID,
		"2026-10-12", "2026-10-11"); !errors.As(err, &validation) {
		t.Fatalf("a backwards window should be rejected, got %v", err)
	}
	// 2026-02-30 is not a day: a rolled-over date would move the deadline.
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID,
		"", "2026-02-30"); !errors.As(err, &validation) {
		t.Fatalf("an impossible date should be rejected, got %v", err)
	}
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, otherLayer.ID,
		"", ""); !errors.As(err, &validation) {
		t.Fatalf("a placement across two boards should be rejected, got %v", err)
	}
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID,
		"", ""); err != nil {
		t.Fatalf("a valid placement should succeed: %v", err)
	}
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID,
		"", ""); !errors.Is(err, ErrTaskAssignmentExists) {
		t.Fatalf("a second placement in one layer should collide, got %v", err)
	}

	// Patching one end of the window is still checked against the other end.
	start := "2026-10-12"
	due := "2026-10-31"
	patchCard := mustItem(t, ctx, db, threadID, "只改开始日", first.ID)
	assignment, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, patchCard.ID, second.ID, "", "")
	if err != nil {
		t.Fatalf("place third card: %v", err)
	}
	if _, err := UpdateTaskAssignment(ctx, db, domain.DefaultProfileID, assignment.ID,
		domain.TaskAssignmentPatch{StartDate: &start, DueDate: &due}); err != nil {
		t.Fatalf("set the window: %v", err)
	}
	if _, err := UpdateTaskAssignment(ctx, db, domain.DefaultProfileID, assignment.ID,
		domain.TaskAssignmentPatch{StartDate: ptrName("2026-11-02")}); !errors.As(err, &validation) {
		t.Fatalf("a start date past the due date should be rejected, got %v", err)
	}
	// Clearing either end lifts the range check, which is how the owner edits a
	// window without tripping over the half they have not retyped.
	cleared := ""
	reloaded, err := UpdateTaskAssignment(ctx, db, domain.DefaultProfileID, assignment.ID,
		domain.TaskAssignmentPatch{DueDate: &cleared, StartDate: ptrName("2026-11-02")})
	if err != nil {
		t.Fatalf("clear the due date: %v", err)
	}
	if reloaded.DueDate != "" || reloaded.StartDate != "2026-11-02" {
		t.Fatalf("the patch did not land: %+v", reloaded)
	}
}

// TestTaskLockedLayerStaysWritable is the reference board's 第三层: three cards sit
// in a layer nobody has opened, and the owner still queues and edits them. A 409 on
// a locked layer would make the lock a permission, which is not what it means.
func TestTaskLockedLayerStaysWritable(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)
	mustItem(t, ctx, db, threadID, "堵住下层", first.ID)

	if _, err := findTaskLayer(taskThread(t, ctx, db, threadID), second.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := findTaskLayer(taskThread(t, ctx, db, threadID), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Unlocked {
		t.Fatalf("the test board's second layer should be shut")
	}

	// Three cards queued into the shut layer, one step each; the first two are ticked
	// and the third is left open so the layer has both states to report.
	var queued []domain.TaskItem
	for index := 0; index < 3; index++ {
		card := mustItem(t, ctx, db, threadID, strings.Repeat("排", index+1)+"期", second.ID)
		step := mustStep(t, ctx, db, card.Assignments[0].ID, "准备工作")
		if index < 2 {
			if _, err := UpdateTaskStep(ctx, db, domain.DefaultProfileID, step.ID,
				domain.TaskStepPatch{Done: doneValue(true)}); err != nil {
				t.Fatalf("tick a queued card: %v", err)
			}
		}
		queued = append(queued, card)
	}
	thread := taskThread(t, ctx, db, threadID)
	third, err := findTaskLayer(thread, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if third.Unlocked || third.ClosedCount != 2 || third.ItemCount != 3 {
		t.Fatalf("a shut layer still counts its closed cards: %+v", third)
	}
	statusByTitle := map[string]string{}
	for _, item := range thread.Items {
		if len(item.Assignments) == 0 || item.Assignments[0].LayerID != second.ID {
			continue
		}
		statusByTitle[item.Title] = item.Status
	}
	if len(statusByTitle) != 3 {
		t.Fatalf("all three queued cards should be on the board: %+v", statusByTitle)
	}
	// 已收口 is tested before 待解锁, so a card whose checklist ran out is finished
	// even while its layer stays shut; the un-ticked card is the one still locked.
	if statusByTitle["排期"] != domain.TaskStatusClosed ||
		statusByTitle["排排期"] != domain.TaskStatusClosed ||
		statusByTitle["排排排期"] != domain.TaskStatusLocked {
		t.Fatalf("unexpected statuses for the queued cards: %+v", statusByTitle)
	}

	// The lock is a status, not a write gate: the card nobody can reach yet still
	// takes a rename and a new step, and a card can still be queued into the shut
	// layer. None of these is a 409.
	locked := queued[2]
	rename := "锁着也能改"
	if _, err := UpdateTaskItem(ctx, db, domain.DefaultProfileID, locked.ID,
		domain.TaskItemPatch{Title: &rename}); err != nil {
		t.Fatalf("renaming a card in a shut layer should be allowed: %v", err)
	}
	if _, err := CreateTaskStep(ctx, db, domain.DefaultProfileID, locked.Assignments[0].ID,
		"锁着也能加一步"); err != nil {
		t.Fatalf("adding a step to a card in a shut layer should be allowed: %v", err)
	}
	spare := mustItem(t, ctx, db, threadID, "事后排队", "")
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, spare.ID, third.ID, "", ""); err != nil {
		t.Fatalf("queuing into a shut layer should be allowed: %v", err)
	}
}

// TestTaskItemsOrderedAndReordered pins that the tree comes back in board order and
// that a drag sticks.
func TestTaskItemsOrderedAndReordered(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	threadID, first, second := twoLayerBoard(t, ctx, db)
	card := mustItem(t, ctx, db, threadID, "先看位置", first.ID)
	if _, err := CreateTaskAssignment(ctx, db, domain.DefaultProfileID, card.ID, second.ID, "", ""); err != nil {
		t.Fatalf("place in layer two: %v", err)
	}
	third := mustItem(t, ctx, db, threadID, "第三张", first.ID)

	loaded, err := GetTaskItem(ctx, db, domain.DefaultProfileID, card.ID)
	if err != nil {
		t.Fatalf("load card: %v", err)
	}
	// Placements read in layer order even though layer two's was written second.
	if len(loaded.Assignments) != 2 ||
		loaded.Assignments[0].LayerPosition > loaded.Assignments[1].LayerPosition {
		t.Fatalf("placements should follow layer order: %+v", loaded.Assignments)
	}
	if loaded.Assignments[0].LayerID != first.ID || loaded.Assignments[1].LayerID != second.ID {
		t.Fatalf("the two placements should be layer one then layer two: %+v", loaded.Assignments)
	}

	thread := taskThread(t, ctx, db, threadID)
	if thread.Items[0].ID != card.ID || thread.Items[1].ID != third.ID {
		t.Fatalf("cards should append in creation order: %+v", thread.Items)
	}
	if err := ReorderTaskItems(ctx, db, domain.DefaultProfileID, threadID,
		[]string{third.ID, card.ID, "不是这张板的id"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	thread = taskThread(t, ctx, db, threadID)
	if thread.Items[0].ID != third.ID || thread.Items[1].ID != card.ID {
		t.Fatalf("the drag did not stick: %+v", thread.Items)
	}
	if thread.Items[0].Position != 0 || thread.Items[1].Position != 1 {
		t.Fatalf("reorder should rewrite positions: %+v", thread.Items)
	}
}

func ptrName(value string) *string { return &value }
