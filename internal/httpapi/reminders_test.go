package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/reminders"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type fakeRemindersRemote struct {
	exists      bool
	list        reminders.List
	reminders   []reminders.Reminder
	next        int
	statusErr   error
	snapshotErr error
	applyErr    error
	applied     []reminders.RemoteOperation
}

func (f *fakeRemindersRemote) Status(context.Context, string, string) (reminders.RemoteStatus, error) {
	if f.statusErr != nil {
		return reminders.RemoteStatus{}, f.statusErr
	}
	return reminders.RemoteStatus{Exists: f.exists, List: f.list, ReminderCount: len(f.reminders)}, nil
}

func (f *fakeRemindersRemote) Snapshot(context.Context, string, string) (reminders.RemoteSnapshot, error) {
	if f.snapshotErr != nil {
		return reminders.RemoteSnapshot{}, f.snapshotErr
	}
	return reminders.RemoteSnapshot{Exists: f.exists, List: f.list, Reminders: append([]reminders.Reminder(nil), f.reminders...)}, nil
}

func (f *fakeRemindersRemote) Apply(_ context.Context, listName, _ string, operations []reminders.RemoteOperation) (reminders.RemoteApplyResult, error) {
	if f.applyErr != nil {
		return reminders.RemoteApplyResult{}, f.applyErr
	}
	f.applied = append(f.applied, operations...)
	if !f.exists {
		f.exists = true
		f.list = reminders.List{ID: "list-1", Name: listName}
	}
	results := make([]reminders.RemoteOperationResult, 0, len(operations))
	for _, operation := range operations {
		switch operation.Kind {
		case "create":
			f.next++
			reminder := reminders.Reminder{
				ID: fmt.Sprintf("reminder-%d", f.next), Name: operation.Name,
				Notes: operation.Notes, DueDate: operation.DueDate, Completed: operation.Completed,
			}
			f.reminders = append(f.reminders, reminder)
			results = append(results, reminders.RemoteOperationResult{
				Key: operation.Key, ReminderID: reminder.ID, Kind: "create", OK: true,
			})
		case "update":
			for index := range f.reminders {
				if f.reminders[index].ID != operation.ReminderID {
					continue
				}
				f.reminders[index].Name = operation.Name
				f.reminders[index].Notes = operation.Notes
				if operation.DueDate != "" {
					f.reminders[index].DueDate = operation.DueDate
				}
				f.reminders[index].Completed = operation.Completed
			}
			results = append(results, reminders.RemoteOperationResult{
				Key: operation.Key, ReminderID: operation.ReminderID, Kind: "update", OK: true,
			})
		}
	}
	return reminders.RemoteApplyResult{
		List: f.list, Results: results, Reminders: append([]reminders.Reminder(nil), f.reminders...),
	}, nil
}

func TestRemindersStatusSyncAndPermission(t *testing.T) {
	server := newTestServer(t)
	statePath := filepath.Join(t.TempDir(), "reminders-sync.json")
	server.ConfigureReminders("Reflow Audit", statePath)
	remote := &fakeRemindersRemote{}
	server.remindersRemote = remote

	if _, err := storage.CreateQuickTask(context.Background(), server.db, domain.DefaultProfileID,
		"First task", "notes", "", "2026-11-05", nil, nil); err != nil {
		t.Fatal(err)
	}

	statusRecorder := httptest.NewRecorder()
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/reminders", nil)
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	var status reminders.Status
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.ListExists || status.ListName != "Reflow Audit" {
		t.Fatalf("status = %+v", status)
	}

	dryRecorder := httptest.NewRecorder()
	dryRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/reminders/sync?direction=merge&dry_run=true", nil)
	server.Handler().ServeHTTP(dryRecorder, dryRequest)
	if dryRecorder.Code != http.StatusOK {
		t.Fatalf("dry run = %d %s", dryRecorder.Code, dryRecorder.Body.String())
	}
	var dry reminders.Result
	if err := json.Unmarshal(dryRecorder.Body.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.CreatedInReminders != 1 || remote.exists {
		t.Fatalf("dry run wrote: %+v / %+v", dry, remote)
	}

	syncRecorder := httptest.NewRecorder()
	syncRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/reminders/sync?direction=merge", nil)
	server.Handler().ServeHTTP(syncRecorder, syncRequest)
	if syncRecorder.Code != http.StatusOK {
		t.Fatalf("sync = %d %s", syncRecorder.Code, syncRecorder.Body.String())
	}
	if !remote.exists || len(remote.reminders) != 1 {
		t.Fatalf("sync did not create the reminder: %+v", remote.reminders)
	}
	if !strings.Contains(remote.reminders[0].Notes, "Reflow-ID: ") {
		t.Fatalf("reminder missing marker: %+v", remote.reminders[0])
	}

	remote.statusErr = &reminders.PermissionError{Reason: "not authorized"}
	remote.snapshotErr = &reminders.PermissionError{Reason: "not authorized"}
	deniedStatus := httptest.NewRecorder()
	server.Handler().ServeHTTP(deniedStatus, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/reminders", nil))
	if deniedStatus.Code != http.StatusOK {
		t.Fatalf("denied status = %d", deniedStatus.Code)
	}
	var denied reminders.Status
	if err := json.Unmarshal(deniedStatus.Body.Bytes(), &denied); err != nil {
		t.Fatal(err)
	}
	if denied.Available || !denied.PermissionRequired {
		t.Fatalf("denied status = %+v", denied)
	}
	deniedSync := httptest.NewRecorder()
	server.Handler().ServeHTTP(deniedSync, httptest.NewRequest(http.MethodPost, "/api/v1/tasks/reminders/sync", nil))
	if deniedSync.Code != http.StatusForbidden {
		t.Fatalf("denied sync = %d %s", deniedSync.Code, deniedSync.Body.String())
	}
}
