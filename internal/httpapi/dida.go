package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/Zijinn/ReFlow/internal/cli"
	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

// serverDidaStore adapts the local quick-task table to the shared Dida sync
// engine. The engine never sees the HTTP API, so the CLI and the workbench run
// exactly the same mapping and conflict rules.
type serverDidaStore struct{ db *sql.DB }

func (s serverDidaStore) ListDidaSyncTasks(ctx context.Context) ([]cli.DidaSyncTask, error) {
	tasks, err := storage.ListQuickTasks(ctx, s.db, domain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	out := make([]cli.DidaSyncTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, quickTaskToSyncTask(task))
	}
	return out, nil
}

func (s serverDidaStore) CreateDidaSyncTask(ctx context.Context, task cli.DidaSyncTask) (cli.DidaSyncTask, error) {
	created, err := storage.CreateQuickTask(ctx, s.db, domain.DefaultProfileID,
		task.Title, task.Notes, task.StartDate, task.DueDate, nil, task.Tags)
	if err != nil {
		return cli.DidaSyncTask{}, err
	}
	if task.Completed {
		completed := true
		created, err = storage.UpdateQuickTask(ctx, s.db, domain.DefaultProfileID, created.ID,
			domain.QuickTaskPatch{Completed: &completed})
		if err != nil {
			return cli.DidaSyncTask{}, err
		}
	}
	return quickTaskToSyncTask(created), nil
}

func (s serverDidaStore) UpdateDidaSyncTask(ctx context.Context, id string, task cli.DidaSyncTask) error {
	title, notes, tags := task.Title, task.Notes, task.Tags
	startDate, dueDate, completed := task.StartDate, task.DueDate, task.Completed
	_, err := storage.UpdateQuickTask(ctx, s.db, domain.DefaultProfileID, id, domain.QuickTaskPatch{
		Title: &title, Notes: &notes, Tags: &tags,
		StartDate: &startDate, DueDate: &dueDate, Completed: &completed,
	})
	return err
}

func quickTaskToSyncTask(task domain.QuickTask) cli.DidaSyncTask {
	return cli.DidaSyncTask{
		ID: task.ID, Title: task.Title, Notes: task.Notes, Tags: task.Tags,
		StartDate: task.StartDate, DueDate: task.DueDate, PaperID: task.PaperID,
		CompletedAt: task.CompletedAt, Completed: task.Completed, Position: task.Position,
	}
}

func (s *Server) didaStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]any{"project": s.didaProject, "available": false}
	command, err := cli.ResolveDidaCommand(s.didaCommand)
	if err != nil {
		response["reason"] = err.Error()
	} else {
		response["available"] = true
		response["command"] = command
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) didaPush(w http.ResponseWriter, r *http.Request) {
	s.runDidaSync(w, r, "push")
}

func (s *Server) didaPull(w http.ResponseWriter, r *http.Request) {
	s.runDidaSync(w, r, "pull")
}

func (s *Server) runDidaSync(w http.ResponseWriter, r *http.Request, direction string) {
	if !s.didaMu.TryLock() {
		writeProblem(w, r, http.StatusConflict, "dida_sync_busy", "Dida sync is running",
			"Wait for the current Dida synchronization to finish, then try again.")
		return
	}
	defer s.didaMu.Unlock()

	command, err := cli.ResolveDidaCommand(s.didaCommand)
	if err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "dida_unavailable", "Dida is not available",
			"Install and sign in to the dida CLI on the machine running ReFlow, then retry.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	result, err := cli.SyncDida(ctx, serverDidaStore{db: s.db}, cli.DidaSyncOptions{
		Project: s.didaProject, Direction: direction, Command: command, StatePath: s.didaStatePath,
	})
	if err != nil {
		var usageError *cli.UsageError
		if errors.As(err, &usageError) {
			writeProblem(w, r, http.StatusBadRequest, "invalid_dida_sync", "Dida sync rejected",
				usageError.Reason)
			return
		}
		var transportError *cli.TransportError
		if errors.As(err, &transportError) {
			s.logger.WarnContext(r.Context(), "dida sync failed", "direction", direction, "error", err)
			writeProblem(w, r, http.StatusBadGateway, "dida_sync_failed", "Dida sync failed",
				"The dida CLI did not complete the request. Check its login and network access, then retry.")
			return
		}
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated("", map[string]string{"dida_sync": direction})
	writeJSON(w, http.StatusOK, result)
}
