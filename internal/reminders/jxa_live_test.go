//go:build darwin

package reminders

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// This test writes to the real Reminders store, so it only runs when the owner
// opts in. It creates one temporary list and deletes it in cleanup.
func TestJXALive(t *testing.T) {
	if os.Getenv("REFLOW_REMINDERS_LIVE_TEST") != "1" {
		t.Skip("set REFLOW_REMINDERS_LIVE_TEST=1 to exercise the real Reminders app")
	}
	name := "Reflow Sync Audit " + time.Now().Format("20060102-150405")
	t.Cleanup(func() { deleteTestList(t, name) })
	store := NewJXAStore()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	status, err := store.Status(ctx, name, "")
	if err != nil {
		t.Fatalf("status before create: %v", err)
	}
	if status.Exists {
		t.Fatalf("temporary list unexpectedly exists: %+v", status)
	}

	created, err := store.Apply(ctx, name, "", []RemoteOperation{
		{Kind: "create", Key: "task-one", Name: "Audit one", Notes: "notes one", DueDate: "2026-11-05"},
		{Kind: "create", Key: "task-two", Name: "Audit two", Notes: "notes two", Completed: true},
	})
	if err != nil {
		t.Fatalf("create reminders: %v", err)
	}
	if len(created.Results) != 2 {
		t.Fatalf("create results = %#v", created.Results)
	}
	for _, item := range created.Results {
		if !item.OK || item.ReminderID == "" {
			t.Fatalf("create result = %#v", item)
		}
	}
	if len(created.Reminders) != 2 {
		t.Fatalf("snapshot after create = %#v", created.Reminders)
	}
	first := findReminderByKey(t, created.Reminders, "Audit one")
	if first.DueDate != "2026-11-05" {
		t.Fatalf("all-day due date shifted: %q", first.DueDate)
	}
	second := findReminderByKey(t, created.Reminders, "Audit two")
	if !second.Completed {
		t.Fatalf("completed reminder did not persist: %#v", second)
	}

	updated, err := store.Apply(ctx, name, "", []RemoteOperation{{
		Kind: "update", ReminderID: first.ID, Name: "Audit one updated",
		Notes: "notes updated", DueDate: "2026-11-06", Completed: true,
	}})
	if err != nil {
		t.Fatalf("update reminder: %v", err)
	}
	if len(updated.Results) != 1 || !updated.Results[0].OK {
		t.Fatalf("update result = %#v", updated.Results)
	}
	after := findReminderByID(t, updated.Reminders, first.ID)
	if after.Name != "Audit one updated" || after.Notes != "notes updated" ||
		after.DueDate != "2026-11-06" || !after.Completed {
		t.Fatalf("updated reminder = %#v", after)
	}

	reopened, err := store.Apply(ctx, name, "", []RemoteOperation{{
		Kind: "update", ReminderID: first.ID, Name: after.Name, Notes: after.Notes,
		DueDate: after.DueDate, Completed: false,
	}})
	if err != nil {
		t.Fatalf("reopen reminder: %v", err)
	}
	if reopened.Reminders[0].Completed {
		t.Fatalf("reopen did not clear completion: %#v", reopened.Reminders)
	}

	deleteTestList(t, name)
	status, err = store.Status(ctx, name, "")
	if err != nil {
		t.Fatalf("status after delete: %v", err)
	}
	if status.Exists {
		t.Fatalf("temporary list was not deleted: %+v", status)
	}
}

func findReminderByKey(t *testing.T, items []Reminder, name string) Reminder {
	t.Helper()
	for _, item := range items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("reminder %q not found in %#v", name, items)
	return Reminder{}
}

func findReminderByID(t *testing.T, items []Reminder, id string) Reminder {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("reminder %q not found in %#v", id, items)
	return Reminder{}
}

func deleteTestList(t *testing.T, name string) {
	t.Helper()
	script := `const app = Application("Reminders"); const name = ObjC.unwrap($.NSProcessInfo.processInfo.environment.objectForKey("REFLOW_REMINDERS_TEST_LIST")); for (const list of app.lists()) { if (list.name() === name) app.delete(list); }`
	command := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", script)
	command.Env = append(os.Environ(), "REFLOW_REMINDERS_TEST_LIST="+name)
	if output, err := command.CombinedOutput(); err != nil {
		t.Logf("delete temporary Reminders list: %v: %s", err, output)
	}
}
