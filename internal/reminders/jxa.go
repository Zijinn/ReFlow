package reminders

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const osascriptPath = "/usr/bin/osascript"
const remindersAppPath = "/System/Applications/Reminders.app"

// JXAStore talks to Reminders.app through the system osascript interpreter.
// There is no network hop and no third-party dependency; macOS owns the
// Automation prompt and the iCloud propagation.
type JXAStore struct{}

func NewJXAStore() *JXAStore { return &JXAStore{} }

func Available() (bool, string) {
	if runtime.GOOS != "darwin" {
		return false, "Apple Reminders sync is only available on macOS"
	}
	if _, err := os.Stat(osascriptPath); err != nil {
		return false, "osascript is not available on this Mac"
	}
	if _, err := os.Stat(remindersAppPath); err != nil {
		return false, "Reminders.app is not installed"
	}
	return true, ""
}

type jxaRequest struct {
	Action     string            `json:"action"`
	ListName   string            `json:"list_name"`
	ListID     string            `json:"list_id,omitempty"`
	EnsureList bool              `json:"ensure_list,omitempty"`
	Operations []RemoteOperation `json:"operations,omitempty"`
}

type jxaReminder struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Notes      string `json:"notes"`
	DueDate    string `json:"due_date"`
	Completed  bool   `json:"completed"`
	ModifiedAt string `json:"modified_at"`
}

type jxaList struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type jxaResult struct {
	Key        string `json:"key"`
	ReminderID string `json:"reminder_id"`
	Action     string `json:"action"`
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
}

type jxaResponse struct {
	Error         string        `json:"error"`
	Permission    bool          `json:"permission"`
	Exists        bool          `json:"exists"`
	List          *jxaList      `json:"list"`
	ReminderCount int           `json:"reminder_count"`
	Reminders     []jxaReminder `json:"reminders"`
	Results       []jxaResult   `json:"results"`
}

func (s *JXAStore) Status(ctx context.Context, listName, listID string) (RemoteStatus, error) {
	response, err := s.run(ctx, jxaRequest{Action: "status", ListName: listName, ListID: listID})
	if err != nil {
		return RemoteStatus{}, err
	}
	return RemoteStatus{
		Exists:        response.Exists,
		List:          listFromJXA(response.List),
		ReminderCount: response.ReminderCount,
	}, nil
}

func (s *JXAStore) Snapshot(ctx context.Context, listName, listID string) (RemoteSnapshot, error) {
	response, err := s.run(ctx, jxaRequest{Action: "snapshot", ListName: listName, ListID: listID})
	if err != nil {
		return RemoteSnapshot{}, err
	}
	return RemoteSnapshot{
		Exists:    response.Exists,
		List:      listFromJXA(response.List),
		Reminders: remindersFromJXA(response.Reminders),
	}, nil
}

func (s *JXAStore) Apply(ctx context.Context, listName, listID string, operations []RemoteOperation) (RemoteApplyResult, error) {
	response, err := s.run(ctx, jxaRequest{
		Action: "apply", ListName: listName, ListID: listID, EnsureList: true, Operations: operations,
	})
	if err != nil {
		return RemoteApplyResult{}, err
	}
	result := RemoteApplyResult{
		List:      listFromJXA(response.List),
		Reminders: remindersFromJXA(response.Reminders),
	}
	for _, item := range response.Results {
		result.Results = append(result.Results, RemoteOperationResult{
			Key: item.Key, ReminderID: item.ReminderID, Kind: item.Action, OK: item.OK, Error: item.Error,
		})
	}
	return result, nil
}

func listFromJXA(list *jxaList) List {
	if list == nil {
		return List{}
	}
	return List{ID: list.ID, Name: list.Name}
}

func remindersFromJXA(items []jxaReminder) []Reminder {
	out := make([]Reminder, 0, len(items))
	for _, item := range items {
		out = append(out, Reminder{
			ID: item.ID, Name: item.Name, Notes: item.Notes,
			DueDate: reminderDateToLocal(item.DueDate), Completed: item.Completed, ModifiedAt: item.ModifiedAt,
		})
	}
	return out
}

func (s *JXAStore) run(ctx context.Context, request jxaRequest) (jxaResponse, error) {
	if ok, reason := Available(); !ok {
		return jxaResponse{}, &UnsupportedError{Reason: reason}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return jxaResponse{}, &TransportError{Reason: "encode Reminders request: " + err.Error()}
	}
	file, err := os.CreateTemp("", "reflow-reminders-*.json")
	if err != nil {
		return jxaResponse{}, &TransportError{Reason: "create Reminders request file: " + err.Error()}
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return jxaResponse{}, &TransportError{Reason: "protect Reminders request file: " + err.Error()}
	}
	if _, err := file.Write(body); err != nil {
		file.Close()
		return jxaResponse{}, &TransportError{Reason: "write Reminders request file: " + err.Error()}
	}
	if err := file.Close(); err != nil {
		return jxaResponse{}, &TransportError{Reason: "close Reminders request file: " + err.Error()}
	}

	command := exec.CommandContext(ctx, osascriptPath, "-l", "JavaScript", "-e", jxaScript)
	command.Env = append(os.Environ(), "REFLOW_REMINDERS_REQUEST_FILE="+name)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if runErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = runErr.Error()
		}
		if ctx.Err() != nil {
			return jxaResponse{}, &TransportError{Reason: "Reminders sync timed out: " + ctx.Err().Error()}
		}
		if isPermissionMessage(message) {
			return jxaResponse{}, &PermissionError{Reason: message}
		}
		return jxaResponse{}, &TransportError{Reason: "osascript failed: " + message}
	}
	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = "osascript returned no data"
		}
		if isPermissionMessage(message) {
			return jxaResponse{}, &PermissionError{Reason: message}
		}
		return jxaResponse{}, &TransportError{Reason: message}
	}
	var response jxaResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return jxaResponse{}, &TransportError{Reason: "decode osascript response: " + err.Error()}
	}
	if response.Error != "" {
		if response.Permission || isPermissionMessage(response.Error) {
			return jxaResponse{}, &PermissionError{Reason: response.Error}
		}
		return jxaResponse{}, &TransportError{Reason: response.Error}
	}
	return response, nil
}

func isPermissionMessage(message string) bool {
	lower := strings.ToLower(message)
	for _, fragment := range []string{
		"not authorized", "not permitted", "automation", "1743", "不允许", "未授权", "没有权限", "不能控制",
	} {
		if strings.Contains(lower, strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

const jxaScript = `
ObjC.import("Foundation");
const requestPath = ObjC.unwrap($.NSProcessInfo.processInfo.environment.objectForKey("REFLOW_REMINDERS_REQUEST_FILE"));
const requestText = ObjC.unwrap($.NSString.stringWithContentsOfFileEncodingError(requestPath, $.NSUTF8StringEncoding, null));
const request = JSON.parse(requestText);
const app = Application("Reminders");
const listName = String(request.list_name || "");
const wantedID = String(request.list_id || "");
const ensure = !!request.ensure_list;

function iso(value) {
  try { return value ? value.toISOString() : ""; } catch (error) { return ""; }
}

function findListByID(id) {
  if (!id) return null;
  for (const list of app.lists()) {
    if (list.id() === id) return list;
  }
  return null;
}

function findListsByName(name) {
  const matches = [];
  for (const list of app.lists()) {
    if (list.name() === name) matches.push(list);
  }
  return matches;
}

function chooseList() {
  const byID = findListByID(wantedID);
  if (byID) return byID;
  const matches = findListsByName(listName);
  if (matches.length > 1) {
    throw new Error('multiple Reminders lists are named "' + listName + '"; keep exactly one list with that name or set REFLOW_REMINDERS_LIST');
  }
  return matches.length === 1 ? matches[0] : null;
}

function createList() {
  app.lists.push(app.List({ name: listName }));
  const matches = findListsByName(listName);
  if (matches.length !== 1) {
    throw new Error('could not create the Reminders list "' + listName + '"');
  }
  return matches[0];
}

function listInfo(list) {
  return { id: list.id(), name: list.name() };
}

function snapshot(list) {
  const reminders = list.reminders();
  const ids = list.reminders.id();
  const names = list.reminders.name();
  const bodies = list.reminders.body();
  const completed = list.reminders.completed();
  const allDay = list.reminders.alldayDueDate();
  const due = list.reminders.dueDate();
  const modified = list.reminders.modificationDate();
  const output = [];
  for (let i = 0; i < ids.length; i++) {
    output.push({
      id: ids[i],
      name: names[i] || "",
      notes: bodies[i] || "",
      due_date: iso(allDay[i] || due[i]),
      completed: !!completed[i],
      modified_at: iso(modified[i])
    });
  }
  return output;
}

function findReminder(list, id) {
  const ids = list.reminders.id();
  const index = ids.indexOf(id);
  return index >= 0 ? list.reminders[index] : null;
}

function parseDate(value) {
  const parts = String(value).split("-");
  return new Date(Number(parts[0]), Number(parts[1]) - 1, Number(parts[2]));
}

function applyOperations(list, operations) {
  const results = [];
  for (const operation of operations) {
    try {
      if (operation.kind === "create") {
        const properties = { name: operation.name || "ReFlow 任务" };
        if (operation.notes) properties.body = operation.notes;
        if (operation.due_date) properties.alldayDueDate = parseDate(operation.due_date);
        list.reminders.push(app.Reminder(properties));
        const created = list.reminders[list.reminders.length - 1];
        const id = created.id();
        if (operation.completed) created.completed = true;
        results.push({ key: operation.key || "", reminder_id: id, action: "create", ok: true });
      } else if (operation.kind === "update") {
        const reminder = findReminder(list, operation.reminder_id);
        if (!reminder) throw new Error("Reminder not found: " + operation.reminder_id);
        reminder.name = operation.name || "ReFlow 任务";
        reminder.body = operation.notes || "";
        if (operation.due_date) reminder.alldayDueDate = parseDate(operation.due_date);
        reminder.completed = !!operation.completed;
        results.push({ key: operation.key || "", reminder_id: operation.reminder_id, action: "update", ok: true });
      } else {
        throw new Error("unknown Reminders operation: " + String(operation.kind));
      }
    } catch (error) {
      results.push({
        key: operation.key || "", reminder_id: operation.reminder_id || "",
        action: operation.kind || "", ok: false, error: String(error)
      });
    }
  }
  return results;
}

function permissionError(error) {
  return /not authorized|not permitted|automation|1743|不允许|未授权|没有权限|不能控制/i.test(String(error));
}

try {
  if (!listName) throw new Error("the ReFlow Reminders list name is empty");
  let list = chooseList();
  if (!list && ensure) list = createList();
  const response = { exists: !!list, list: list ? listInfo(list) : null };
  if (list && request.action === "status") {
    response.reminder_count = list.reminders.length;
  } else if (list && request.action === "snapshot") {
    response.reminders = snapshot(list);
  } else if (list && request.action === "apply") {
    response.results = applyOperations(list, request.operations || []);
    response.reminders = snapshot(list);
  }
  JSON.stringify(response);
} catch (error) {
  JSON.stringify({ error: String(error), permission: permissionError(error) });
}
`
