package reminders

import (
	"context"
	"fmt"
)

// Task is the ReFlow half of the bridge. Only the fields Apple Reminders can
// represent are carried; tags, start dates, paper links and ordering stay local.
type Task struct {
	ID        string
	Title     string
	Notes     string
	DueDate   string
	Completed bool
	Position  int
}

// Reminder is the Apple Reminders half, already stripped of the Reflow marker.
type Reminder struct {
	ID         string
	Name       string
	Notes      string
	DueDate    string
	Completed  bool
	ModifiedAt string
}

type List struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type LocalStore interface {
	ListTasks(ctx context.Context) ([]Task, error)
	CreateTask(ctx context.Context, task Task) (Task, error)
	UpdateTask(ctx context.Context, id string, task Task) error
}

type RemoteOperation struct {
	Kind       string `json:"kind"`          // create | update
	Key        string `json:"key,omitempty"` // ReFlow id, used to correlate a create result
	ReminderID string `json:"reminder_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Notes      string `json:"notes,omitempty"`
	DueDate    string `json:"due_date,omitempty"`
	Completed  bool   `json:"completed,omitempty"`
}

type RemoteOperationResult struct {
	Key        string
	ReminderID string
	Kind       string
	OK         bool
	Error      string
}

type RemoteStatus struct {
	Exists         bool
	List           List
	ReminderCount  int
	PermissionHint bool
}

type RemoteSnapshot struct {
	Exists    bool
	List      List
	Reminders []Reminder
}

type RemoteApplyResult struct {
	List      List
	Results   []RemoteOperationResult
	Reminders []Reminder
}

type RemoteStore interface {
	Status(ctx context.Context, listName, listID string) (RemoteStatus, error)
	Snapshot(ctx context.Context, listName, listID string) (RemoteSnapshot, error)
	Apply(ctx context.Context, listName, listID string, operations []RemoteOperation) (RemoteApplyResult, error)
}

type Action struct {
	Action     string `json:"action"`
	ReflowID   string `json:"reflow_id,omitempty"`
	ReminderID string `json:"reminder_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type Result struct {
	Direction          string   `json:"direction"`
	DryRun             bool     `json:"dry_run"`
	List               List     `json:"list"`
	StateFile          string   `json:"state_file"`
	CreatedInReminders int      `json:"created_in_reminders"`
	CreatedInReflow    int      `json:"created_in_reflow"`
	UpdatedInReminders int      `json:"updated_in_reminders"`
	UpdatedInReflow    int      `json:"updated_in_reflow"`
	Conflicts          []Action `json:"conflicts,omitempty"`
	Orphans            []Action `json:"orphans,omitempty"`
	Skipped            []Action `json:"skipped,omitempty"`
	Actions            []Action `json:"actions,omitempty"`
}

type Status struct {
	Available          bool   `json:"available"`
	ListName           string `json:"list_name"`
	ListID             string `json:"list_id,omitempty"`
	ListExists         bool   `json:"list_exists"`
	ReminderCount      int    `json:"reminder_count"`
	LinkedTasks        int    `json:"linked_tasks"`
	LastSync           string `json:"last_sync,omitempty"`
	PermissionRequired bool   `json:"permission_required,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

type Options struct {
	ListName  string
	StatePath string
	Direction string
	DryRun    bool
}

// PermissionError means macOS refused the Apple Events authorization.
type PermissionError struct{ Reason string }

func (e *PermissionError) Error() string { return e.Reason }

// UnsupportedError means this build or machine cannot talk to Reminders.
type UnsupportedError struct{ Reason string }

func (e *UnsupportedError) Error() string { return e.Reason }

// TransportError is an osascript/JXA failure that is not an authorization one.
type TransportError struct{ Reason string }

func (e *TransportError) Error() string { return e.Reason }

type UsageError struct{ Reason string }

func (e *UsageError) Error() string { return e.Reason }

func usage(format string, args ...any) error {
	return &UsageError{Reason: fmt.Sprintf(format, args...)}
}
