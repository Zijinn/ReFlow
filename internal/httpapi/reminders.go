package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/reminders"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type serverRemindersLocalStore struct{ db *sql.DB }

func (s serverRemindersLocalStore) ListTasks(ctx context.Context) ([]reminders.Task, error) {
	items, err := storage.ListQuickTasks(ctx, s.db, domain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	out := make([]reminders.Task, 0, len(items))
	for _, item := range items {
		out = append(out, reminders.Task{
			ID: item.ID, Title: item.Title, Notes: item.Notes,
			DueDate: item.DueDate, Completed: item.Completed, Position: item.Position,
		})
	}
	return out, nil
}

func (s serverRemindersLocalStore) CreateTask(ctx context.Context, task reminders.Task) (reminders.Task, error) {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = "未命名任务"
	}
	created, err := storage.CreateQuickTask(ctx, s.db, domain.DefaultProfileID,
		title, task.Notes, "", task.DueDate, nil, nil)
	if err != nil {
		return reminders.Task{}, err
	}
	if task.Completed {
		completed := true
		created, err = storage.UpdateQuickTask(ctx, s.db, domain.DefaultProfileID, created.ID,
			domain.QuickTaskPatch{Completed: &completed})
		if err != nil {
			return reminders.Task{}, err
		}
	}
	return taskFromQuickTask(created), nil
}

func (s serverRemindersLocalStore) UpdateTask(ctx context.Context, id string, task reminders.Task) error {
	title, notes, dueDate, completed := task.Title, task.Notes, task.DueDate, task.Completed
	if strings.TrimSpace(title) == "" {
		return errors.New("reminder title cannot be empty")
	}
	_, err := storage.UpdateQuickTask(ctx, s.db, domain.DefaultProfileID, id, domain.QuickTaskPatch{
		Title: &title, Notes: &notes, DueDate: &dueDate, Completed: &completed,
	})
	return err
}

func taskFromQuickTask(task domain.QuickTask) reminders.Task {
	return reminders.Task{
		ID: task.ID, Title: task.Title, Notes: task.Notes,
		DueDate: task.DueDate, Completed: task.Completed, Position: task.Position,
	}
}

func (s *Server) remindersRemoteStore() reminders.RemoteStore {
	if s.remindersRemote != nil {
		return s.remindersRemote
	}
	return reminders.NewJXAStore()
}

func (s *Server) remindersStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	status, err := reminders.QueryStatus(ctx, s.remindersRemoteStore(), s.remindersList, s.remindersStatePath)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) remindersSync(w http.ResponseWriter, r *http.Request) {
	if !s.remindersMu.TryLock() {
		writeProblem(w, r, http.StatusConflict, "reminders_sync_busy", "Reminders sync is running",
			"Wait for the current Reminders synchronization to finish, then try again.")
		return
	}
	defer s.remindersMu.Unlock()

	direction := strings.TrimSpace(r.URL.Query().Get("direction"))
	dryRun := false
	if raw := strings.TrimSpace(r.URL.Query().Get("dry_run")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_reminders_sync", "Invalid Reminders sync",
				"dry_run must be true or false.")
			return
		}
		dryRun = parsed
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	result, err := reminders.Sync(ctx, serverRemindersLocalStore{db: s.db}, s.remindersRemoteStore(), reminders.Options{
		ListName: s.remindersList, StatePath: s.remindersStatePath, Direction: direction, DryRun: dryRun,
	})
	if err != nil {
		var permission *reminders.PermissionError
		if errors.As(err, &permission) {
			writeProblem(w, r, http.StatusForbidden, "reminders_permission_denied", "Reminders permission required",
				"Allow ReFlow to control Reminders in System Settings > Privacy & Security > Automation, then retry.")
			return
		}
		var unsupported *reminders.UnsupportedError
		if errors.As(err, &unsupported) {
			writeProblem(w, r, http.StatusServiceUnavailable, "reminders_unavailable", "Reminders is not available",
				unsupported.Reason)
			return
		}
		var usageError *reminders.UsageError
		if errors.As(err, &usageError) {
			writeProblem(w, r, http.StatusBadRequest, "invalid_reminders_sync", "Reminders sync rejected",
				usageError.Reason)
			return
		}
		var transport *reminders.TransportError
		if errors.As(err, &transport) {
			s.logger.WarnContext(r.Context(), "reminders sync failed", "direction", direction, "error", err)
			writeProblem(w, r, http.StatusBadGateway, "reminders_sync_failed", "Reminders sync failed",
				transport.Reason)
			return
		}
		s.storageError(w, r, err)
		return
	}
	if !dryRun {
		s.publishTasksUpdated("", map[string]string{"reminders_sync": direction})
	}
	writeJSON(w, http.StatusOK, result)
}
