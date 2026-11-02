package reminders

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeLocal struct {
	tasks []Task
	next  int
}

func (f *fakeLocal) ListTasks(context.Context) ([]Task, error) {
	return append([]Task(nil), f.tasks...), nil
}

func (f *fakeLocal) CreateTask(_ context.Context, task Task) (Task, error) {
	f.next++
	task.ID = fmt.Sprintf("reflow-%d", f.next)
	f.tasks = append(f.tasks, task)
	return task, nil
}

func (f *fakeLocal) UpdateTask(_ context.Context, id string, task Task) error {
	for index := range f.tasks {
		if f.tasks[index].ID == id {
			task.ID = id
			f.tasks[index] = task
			return nil
		}
	}
	return fmt.Errorf("local task %s not found", id)
}

type fakeRemote struct {
	exists            bool
	list              List
	reminders         []Reminder
	next              int
	applied           []RemoteOperation
	scrambleCreateIDs bool
}

func (f *fakeRemote) Status(context.Context, string, string) (RemoteStatus, error) {
	return RemoteStatus{Exists: f.exists, List: f.list, ReminderCount: len(f.reminders)}, nil
}

func (f *fakeRemote) Snapshot(context.Context, string, string) (RemoteSnapshot, error) {
	return RemoteSnapshot{Exists: f.exists, List: f.list, Reminders: cloneReminders(f.reminders)}, nil
}

func (f *fakeRemote) Apply(_ context.Context, listName, _ string, operations []RemoteOperation) (RemoteApplyResult, error) {
	f.applied = append(f.applied, operations...)
	if !f.exists {
		f.exists = true
		f.list = List{ID: "list-1", Name: listName}
	}
	results := make([]RemoteOperationResult, 0, len(operations))
	for _, operation := range operations {
		switch operation.Kind {
		case "create":
			f.next++
			reminder := Reminder{ID: fmt.Sprintf("reminder-%d", f.next), Name: operation.Name,
				Notes: operation.Notes, DueDate: operation.DueDate, Completed: operation.Completed}
			f.reminders = append(f.reminders, reminder)
			resultID := reminder.ID
			if f.scrambleCreateIDs {
				resultID = "wrong-" + reminder.ID
			}
			results = append(results, RemoteOperationResult{
				Key: operation.Key, ReminderID: resultID, Kind: "create", OK: true,
			})
		case "update":
			found := false
			for index := range f.reminders {
				if f.reminders[index].ID != operation.ReminderID {
					continue
				}
				found = true
				f.reminders[index].Name = operation.Name
				f.reminders[index].Notes = operation.Notes
				if operation.DueDate != "" {
					f.reminders[index].DueDate = operation.DueDate
				}
				f.reminders[index].Completed = operation.Completed
			}
			if !found {
				return RemoteApplyResult{}, &TransportError{Reason: "missing reminder " + operation.ReminderID}
			}
			results = append(results, RemoteOperationResult{
				Key: operation.Key, ReminderID: operation.ReminderID, Kind: "update", OK: true,
			})
		}
	}
	return RemoteApplyResult{List: f.list, Results: results, Reminders: cloneReminders(f.reminders)}, nil
}

func cloneReminders(items []Reminder) []Reminder {
	return append([]Reminder(nil), items...)
}

func newTestSync(t *testing.T) (*fakeLocal, *fakeRemote, string) {
	t.Helper()
	return &fakeLocal{}, &fakeRemote{}, filepath.Join(t.TempDir(), "reminders-sync.json")
}

func syncOnce(t *testing.T, local *fakeLocal, remote *fakeRemote, statePath string, direction string, dryRun bool) Result {
	t.Helper()
	result, err := Sync(context.Background(), local, remote, Options{
		ListName: "Reflow", StatePath: statePath, Direction: direction, DryRun: dryRun,
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return result
}

func TestSyncPushesOnlyOpenLocalTasks(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{
		{ID: "a", Title: "First", Notes: "notes", DueDate: "2026-11-05", Position: 1},
		{ID: "b", Title: "Second", Completed: true, Position: 2},
	}
	result := syncOnce(t, local, remote, state, "merge", false)
	if result.CreatedInReminders != 1 || len(remote.reminders) != 1 {
		t.Fatalf("push result = %+v, reminders = %+v", result, remote.reminders)
	}
	first := remote.reminders[0]
	if first.DueDate != "2026-11-05" || first.Completed || first.Notes == "" {
		t.Fatalf("first reminder = %+v", first)
	}
	if _, marker := splitMarker(first.Notes); marker != "a" {
		t.Fatalf("first reminder marker = %q, notes = %q", marker, first.Notes)
	}
	stateFile, err := loadState(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(stateFile.Lists["Reflow"].Links) != 1 {
		t.Fatalf("completed task should not be linked: %+v", stateFile.Lists["Reflow"].Links)
	}
}

func TestSyncPullsOnlyOpenUnmarkedReminders(t *testing.T) {
	local, remote, state := newTestSync(t)
	remote.exists = true
	remote.list = List{ID: "list-1", Name: "Reflow"}
	remote.reminders = []Reminder{
		{ID: "r1", Name: "Manual one", Notes: "manual notes", DueDate: "2026-11-06"},
		{ID: "r2", Name: "Manual two", Completed: true},
	}
	result := syncOnce(t, local, remote, state, "merge", false)
	if result.CreatedInReflow != 1 || len(local.tasks) != 1 {
		t.Fatalf("pull result = %+v, local = %+v", result, local.tasks)
	}
	if local.tasks[0].DueDate != "2026-11-06" {
		t.Fatalf("imported tasks = %+v", local.tasks)
	}
	for _, reminder := range remote.reminders {
		_, marker := splitMarker(reminder.Notes)
		if reminder.ID == "r1" && marker == "" {
			t.Fatalf("imported reminder was not stamped: %+v", reminder)
		}
		if reminder.ID == "r2" && marker != "" {
			t.Fatalf("completed reminder should not be imported or stamped: %+v", reminder)
		}
	}
}

func TestSyncStopsLinkedTasksAfterCompletion(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{
		{ID: "local-done", Title: "Local done", Position: 1},
		{ID: "remote-done", Title: "Remote done", Position: 2},
	}
	syncOnce(t, local, remote, state, "merge", false)
	if len(remote.reminders) != 2 {
		t.Fatalf("initial sync = %+v", remote.reminders)
	}

	local.tasks[0].Completed = true
	local.tasks[0].Title = "Local completed edit"
	remote.reminders[1].Completed = true
	remote.reminders[1].Name = "Remote completed edit"
	result := syncOnce(t, local, remote, state, "merge", false)
	if result.UpdatedInReminders != 0 || result.UpdatedInReflow != 0 {
		t.Fatalf("completed links were synchronized: %+v", result)
	}
	if remote.reminders[0].Completed || remote.reminders[0].Name != "Local done" {
		t.Fatalf("local completion flowed to Reminders: %+v", remote.reminders[0])
	}
	if local.tasks[1].Completed || local.tasks[1].Title != "Remote done" {
		t.Fatalf("remote completion flowed to ReFlow: %+v", local.tasks[1])
	}
}

func TestSyncAdoptsMarkerAfterStateLoss(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "First", Position: 1}}
	remote.exists = true
	remote.list = List{ID: "list-1", Name: "Reflow"}
	remote.reminders = []Reminder{{ID: "r1", Name: "First", Notes: withMarker("", "a")}}
	result := syncOnce(t, local, remote, state, "merge", false)
	if result.CreatedInReminders != 0 || result.CreatedInReflow != 0 || len(remote.reminders) != 1 {
		t.Fatalf("marker adoption created duplicates: %+v / %+v", result, remote.reminders)
	}
	stateFile, _ := loadState(state)
	if stateFile.Lists["Reflow"].Links["a"] == nil {
		t.Fatalf("marker pair was not linked: %+v", stateFile.Lists["Reflow"].Links)
	}
}

func TestSyncUpdatesOnlyTheChangedSide(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "First", Notes: "one", DueDate: "2026-11-05"}}
	syncOnce(t, local, remote, state, "merge", false)

	local.tasks[0].Notes = "local edit"
	syncOnce(t, local, remote, state, "merge", false)
	if mustCleanNotes(remote.reminders[0].Notes) != "local edit" {
		t.Fatalf("local edit did not reach Reminders: %+v", remote.reminders[0])
	}

	remote.reminders[0].Name = "Remote edit"
	result := syncOnce(t, local, remote, state, "merge", false)
	if local.tasks[0].Title != "Remote edit" || result.UpdatedInReflow != 1 {
		t.Fatalf("remote edit did not reach ReFlow: %+v / %+v", local.tasks[0], result)
	}
}

func TestSyncConflictKeepsReflow(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "First"}}
	syncOnce(t, local, remote, state, "merge", false)
	local.tasks[0].Title = "Reflow wins"
	remote.reminders[0].Name = "Remote loses"
	result := syncOnce(t, local, remote, state, "merge", false)
	if remote.reminders[0].Name != "Reflow wins" {
		t.Fatalf("conflict did not keep ReFlow: %+v", remote.reminders[0])
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v", result.Conflicts)
	}
}

func TestSyncDryRunWritesNothing(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "First"}}
	result := syncOnce(t, local, remote, state, "merge", true)
	if result.CreatedInReminders != 1 || remote.exists || len(remote.applied) != 0 {
		t.Fatalf("dry run wrote: %+v / %+v", result, remote)
	}
	if _, err := loadState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); err == nil {
		t.Fatal("dry run created the mapping file")
	}
}

func TestSyncDueClearKeepsReminderDateAndSuppressesPullBack(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "First", DueDate: "2026-11-05"}}
	syncOnce(t, local, remote, state, "merge", false)

	local.tasks[0].DueDate = ""
	result := syncOnce(t, local, remote, state, "merge", false)
	if remote.reminders[0].DueDate != "2026-11-05" {
		t.Fatalf("JXA cannot clear due dates; fixture changed: %+v", remote.reminders[0])
	}
	if len(result.Skipped) == 0 {
		t.Fatalf("expected a due-clear warning: %+v", result)
	}
	second := syncOnce(t, local, remote, state, "merge", false)
	if local.tasks[0].DueDate != "" || second.UpdatedInReflow != 0 {
		t.Fatalf("cleared due date was pulled back: %+v / %+v", local.tasks[0], second)
	}

	remote.reminders[0].DueDate = "2026-12-01"
	syncOnce(t, local, remote, state, "merge", false)
	if local.tasks[0].DueDate != "2026-12-01" {
		t.Fatalf("a new remote due date was not pulled: %+v", local.tasks[0])
	}
}

func TestSyncOrphanDoesNotDelete(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "First"}}
	syncOnce(t, local, remote, state, "merge", false)
	remote.reminders = nil
	result := syncOnce(t, local, remote, state, "merge", false)
	if len(result.Orphans) != 1 || len(local.tasks) != 1 {
		t.Fatalf("orphan handling = %+v / %+v", result, local.tasks)
	}
	if len(remote.reminders) != 0 {
		t.Fatalf("orphan recreated a deleted reminder: %+v", remote.reminders)
	}
}

func TestDirectionPullIgnoresUnlinkedLocalTasks(t *testing.T) {
	local, remote, state := newTestSync(t)
	local.tasks = []Task{{ID: "a", Title: "Local only"}}
	result := syncOnce(t, local, remote, state, "pull", false)
	if result.CreatedInReminders != 0 || len(remote.reminders) != 0 {
		t.Fatalf("pull pushed local tasks: %+v", result)
	}
}

func TestDirectionPushIgnoresUnlinkedReminders(t *testing.T) {
	local, remote, state := newTestSync(t)
	remote.exists = true
	remote.list = List{ID: "list-1", Name: "Reflow"}
	remote.reminders = []Reminder{{ID: "r1", Name: "Remote only"}}
	result := syncOnce(t, local, remote, state, "push", false)
	if result.CreatedInReflow != 0 || len(local.tasks) != 0 {
		t.Fatalf("push imported remote tasks: %+v", result)
	}
}

func TestSyncLinksCreatedRemindersByMarkerNotResultOrder(t *testing.T) {
	local, remote, state := newTestSync(t)
	remote.scrambleCreateIDs = true
	local.tasks = []Task{{ID: "a", Title: "First", Position: 2}, {ID: "b", Title: "Second", Position: 1}}
	syncOnce(t, local, remote, state, "merge", false)
	stateFile, err := loadState(state)
	if err != nil {
		t.Fatal(err)
	}
	links := stateFile.Lists["Reflow"].Links
	if len(links) != 2 {
		t.Fatalf("links = %+v", links)
	}
	for _, task := range local.tasks {
		link := links[task.ID]
		if link == nil {
			t.Fatalf("missing link for %s: %+v", task.ID, links)
		}
		var matched bool
		for _, reminder := range remote.reminders {
			if reminder.ID != link.ReminderID {
				continue
			}
			_, markerID := splitMarker(reminder.Notes)
			matched = markerID == task.ID
		}
		if !matched {
			t.Fatalf("link for %s points at the wrong reminder: %+v", task.ID, link)
		}
	}
}

func TestQueryStatusUsesStoreAvailability(t *testing.T) {
	_, remote, state := newTestSync(t)
	remote.exists = true
	remote.list = List{ID: "list-1", Name: "Reflow"}
	remote.reminders = []Reminder{{ID: "r1", Name: "One"}}
	status, err := QueryStatus(context.Background(), remote, "Reflow", state)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || !status.ListExists || status.ReminderCount != 1 {
		t.Fatalf("status = %+v", status)
	}
}

func TestReminderDateToLocalHonorsTimezone(t *testing.T) {
	shanghai := time.FixedZone("Asia/Shanghai", 8*3600)
	if got := reminderDateToLocalIn("2026-11-04T16:00:00Z", shanghai); got != "2026-11-05" {
		t.Fatalf("Asia/Shanghai all-day date = %q", got)
	}
	newYork := time.FixedZone("America/New_York", -5*3600)
	if got := reminderDateToLocalIn("2026-11-04T16:00:00Z", newYork); got != "2026-11-04" {
		t.Fatalf("America/New_York all-day date = %q", got)
	}
	if got := reminderDateToLocalIn("", shanghai); got != "" {
		t.Fatalf("empty date = %q", got)
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	notes := withMarker("line one\nline two", "task-1")
	if notes != "line one\nline two\n\nReflow-ID: task-1" {
		t.Fatalf("withMarker = %q", notes)
	}
	clean, id := splitMarker(notes)
	if clean != "line one\nline two" || id != "task-1" {
		t.Fatalf("splitMarker = %q / %q", clean, id)
	}
	replaced := withMarker(notes, "task-2")
	if strings.Count(replaced, markerPrefix) != 1 {
		t.Fatalf("withMarker duplicated the marker: %q", replaced)
	}
	clean, id = splitMarker(replaced)
	if clean != "line one\nline two" || id != "task-2" {
		t.Fatalf("replaced marker = %q / %q", clean, id)
	}
}
