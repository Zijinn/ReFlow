package storage

import (
	"context"
	"testing"

	"github.com/Zijinn/ReFlow/internal/domain"
)

func TestQuickTaskCRUDAndDateRules(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)

	first, err := CreateQuickTask(ctx, db, domain.DefaultProfileID,
		"整理报销单", "", "2026-10-20", "2026-10-24", nil)
	if err != nil {
		t.Fatalf("create quick task: %v", err)
	}
	if first.Completed || first.Position != 0 {
		t.Fatalf("unexpected new quick task: %+v", first)
	}

	second, err := CreateQuickTask(ctx, db, domain.DefaultProfileID, "预约体检", "", "", "", nil)
	if err != nil {
		t.Fatalf("create second quick task: %v", err)
	}
	if second.Position >= first.Position {
		t.Fatalf("new quick tasks must land on top: first=%d second=%d", first.Position, second.Position)
	}

	// A due date without a start date is legal; a window running backwards is not.
	if _, err := CreateQuickTask(ctx, db, domain.DefaultProfileID, "倒序", "", "2026-10-24", "2026-10-20", nil); err == nil {
		t.Fatal("due_date before start_date must be rejected")
	}
	if _, err := CreateQuickTask(ctx, db, domain.DefaultProfileID, "坏日期", "", "2026-02-30", "", nil); err == nil {
		t.Fatal("impossible calendar dates must be rejected")
	}

	completed := true
	done, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, first.ID,
		domain.QuickTaskPatch{Completed: &completed})
	if err != nil {
		t.Fatalf("complete quick task: %v", err)
	}
	if !done.Completed || done.CompletedAt == "" {
		t.Fatalf("completed quick task must carry a stamp: %+v", done)
	}

	// Completing again must not rewrite the original stamp.
	again, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, first.ID,
		domain.QuickTaskPatch{Completed: &completed})
	if err != nil {
		t.Fatalf("complete quick task again: %v", err)
	}
	if again.CompletedAt != done.CompletedAt {
		t.Fatalf("re-completing must keep the original stamp: %q vs %q", again.CompletedAt, done.CompletedAt)
	}

	reopened := false
	open, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, first.ID,
		domain.QuickTaskPatch{Completed: &reopened})
	if err != nil {
		t.Fatalf("reopen quick task: %v", err)
	}
	if open.Completed || open.CompletedAt != "" {
		t.Fatalf("reopened quick task must clear the stamp: %+v", open)
	}

	list, err := ListQuickTasks(ctx, db, domain.DefaultProfileID)
	if err != nil {
		t.Fatalf("list quick tasks: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected two quick tasks, got %+v", list)
	}
	if err := DeleteQuickTask(ctx, db, domain.DefaultProfileID, second.ID); err != nil {
		t.Fatalf("delete quick task: %v", err)
	}
	if _, err := GetQuickTask(ctx, db, domain.DefaultProfileID, second.ID); err == nil {
		t.Fatal("deleted quick task must disappear")
	}
}

func TestQuickTaskPaperLink(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO research_papers (id, profile_id, kind) VALUES (?, ?, 'research')`,
		"paper-1", domain.DefaultProfileID); err != nil {
		t.Fatalf("insert research paper: %v", err)
	}
	paperID := "paper-1"
	task, err := CreateQuickTask(ctx, db, domain.DefaultProfileID,
		"补齐稳健性检验", "", "", "", &paperID)
	if err != nil {
		t.Fatalf("create linked quick task: %v", err)
	}
	if task.PaperID == nil || *task.PaperID != paperID {
		t.Fatalf("quick task paper link did not round-trip: %+v", task)
	}
	loaded, err := GetQuickTask(ctx, db, domain.DefaultProfileID, task.ID)
	if err != nil {
		t.Fatalf("reload linked quick task: %v", err)
	}
	if loaded.PaperID == nil || *loaded.PaperID != paperID {
		t.Fatalf("reloaded quick task lost its paper link: %+v", loaded)
	}

	missing := "paper-missing"
	if _, err := CreateQuickTask(ctx, db, domain.DefaultProfileID,
		"坏关联", "", "", "", &missing); err == nil {
		t.Fatal("a quick task must not link to a paper outside the profile")
	}
	if _, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, task.ID,
		domain.QuickTaskPatch{SetPaperID: true, PaperID: &missing}); err == nil {
		t.Fatal("updating a quick task must reject a paper outside the profile")
	}
	unlinkedTask, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, task.ID,
		domain.QuickTaskPatch{SetPaperID: true, PaperID: nil})
	if err != nil {
		t.Fatalf("unlink quick task paper: %v", err)
	}
	if unlinkedTask.PaperID != nil {
		t.Fatalf("explicit null must unlink the paper: %+v", unlinkedTask)
	}
	relinked, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, task.ID,
		domain.QuickTaskPatch{SetPaperID: true, PaperID: &paperID})
	if err != nil || relinked.PaperID == nil || *relinked.PaperID != paperID {
		t.Fatalf("relink quick task paper: %+v, %v", relinked, err)
	}

	negative := -7
	if _, err := UpdateQuickTask(ctx, db, domain.DefaultProfileID, task.ID,
		domain.QuickTaskPatch{Position: &negative}); err != nil {
		t.Fatalf("quick task positions are negative by design: %v", err)
	}

	unlinked, err := CreateQuickTask(ctx, db, domain.DefaultProfileID,
		"独立任务", "", "", "", nil)
	if err != nil {
		t.Fatalf("create unlinked quick task: %v", err)
	}
	if unlinked.PaperID != nil {
		t.Fatalf("a manual quick task must stay unlinked: %+v", unlinked)
	}
}

func TestTaskThreadMapMarkdownRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newTaskTestDB(t)
	thread := mustThread(t, ctx, db, "论文项目")

	const source = "# 论文项目\n## 第一层\n- 修改正文\n  - 改摘要"
	if _, err := UpdateTaskThread(ctx, db, domain.DefaultProfileID, thread,
		domain.TaskThreadPatch{MapMarkdown: strPointer(source)}); err != nil {
		t.Fatalf("save map markdown: %v", err)
	}
	loaded := taskThread(t, ctx, db, thread)
	if loaded.MapMarkdown != source {
		t.Fatalf("map markdown did not round-trip: %q", loaded.MapMarkdown)
	}
}
