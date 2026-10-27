package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestTaskCreateCarriesTagsDatesAndPaper(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("POST /api/v1/tasks/quick", http.StatusCreated,
		`{"id":"t-1","title":"写摘要","tags":["写作","urgent"]}`)
	code, body, _ := runCLI(t, base, "task", "create",
		"--title", "写摘要",
		"--notes", "先写贡献段",
		"--tag", "写作", "--tag", "urgent",
		"--start", "2026-10-26", "--due", "2026-10-30",
		"--paper-id", "p-1")
	data := mustOK(t, code, body)
	if data["id"] != "t-1" {
		t.Fatalf("create should answer with the new task, got %v", data)
	}
	call := fake.lastTo(t, http.MethodPost, "/api/v1/tasks/quick")
	if call.Body["title"] != "写摘要" || call.Body["notes"] != "先写贡献段" {
		t.Fatalf("create body lost its text fields: %v", call.Body)
	}
	if call.Body["start_date"] != "2026-10-26" || call.Body["due_date"] != "2026-10-30" {
		t.Fatalf("create body lost its dates: %v", call.Body)
	}
	if call.Body["paper_id"] != "p-1" {
		t.Fatalf("create body lost its paper link: %v", call.Body)
	}
	tags, _ := call.Body["tags"].([]any)
	if len(tags) != 2 || tags[0] != "写作" || tags[1] != "urgent" {
		t.Fatalf("create body lost its tags: %v", call.Body)
	}
}

func TestTaskSetClearsFieldsAndReopens(t *testing.T) {
	fake, base := newFake(t)
	fake.answer("PATCH /api/v1/tasks/quick/t-1", http.StatusOK, `{"id":"t-1","completed":false}`)
	code, body, _ := runCLI(t, base, "task", "set", "t-1",
		"--clear-tags", "--paper-id=", "--completed=false", "--notes", "改完了")
	mustOK(t, code, body)
	call := fake.lastTo(t, http.MethodPatch, "/api/v1/tasks/quick/t-1")
	if tags, ok := call.Body["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("--clear-tags must send an empty array, got %v", call.Body["tags"])
	}
	if value, exists := call.Body["paper_id"]; !exists || value != nil {
		t.Fatalf("--paper-id= must send an explicit null, got %v", call.Body["paper_id"])
	}
	if call.Body["completed"] != false {
		t.Fatalf("--completed=false must send false, got %v", call.Body["completed"])
	}
	if call.Body["notes"] != "改完了" {
		t.Fatalf("set body lost notes: %v", call.Body)
	}
}

func TestTaskSetRejectsTagAndClearTogether(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "task", "set", "t-1", "--tag", "x", "--clear-tags")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "not both") {
		t.Fatalf("expected a usage error, got %d %v", code, body)
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("a rejected edit must not reach the server: %v", fake.seen())
	}
}

func TestTaskDeleteRequiresYes(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "task", "delete", "t-1")
	if code != ExitUsage || !strings.Contains(messageOf(t, body), "--yes") {
		t.Fatalf("delete without --yes should refuse, got %d %v", code, body)
	}
	if len(fake.seen()) != 0 {
		t.Fatalf("refused delete must not reach the server: %v", fake.seen())
	}

	fake.answer("DELETE /api/v1/tasks/quick/t-1", http.StatusNoContent, "")
	code, body, _ = runCLI(t, base, "task", "delete", "t-1", "--yes")
	data := mustOK(t, code, body)
	if data["deleted"] != "t-1" {
		t.Fatalf("delete should name the removed task: %v", data)
	}
	fake.lastTo(t, http.MethodDelete, "/api/v1/tasks/quick/t-1")
}

func TestDescribeIncludesTaskCommands(t *testing.T) {
	fake, base := newFake(t)
	code, body, _ := runCLI(t, base, "describe")
	data := mustOK(t, code, body)
	if len(fake.seen()) != 0 {
		t.Fatalf("describe must not call the API, got %v", fake.seen())
	}
	commands, _ := data["commands"].([]any)
	found := false
	for _, item := range commands {
		entry, _ := item.(map[string]any)
		if entry["path"] == "task create" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("describe should advertise task create")
	}
}
