package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

// The 长任务 board routes are a thin shell over the storage functions: every write
// answers with the row re-read through the board, so the client gets the derived
// status, percentage and unlock state it paints from in the same round trip that
// changed them.

// defaultTaskItemTitle is what a card created with no title lands as: the board adds
// the card first and lets the owner name it after, the same way a nameless layer
// takes its 第N层 label. An explicit blank title is still a 400 — the default covers
// the absent field, not the empty one.
const defaultTaskItemTitle = "未命名任务"

func (s *Server) listTaskThreads(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListTaskThreads(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createTaskThread(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	thread, err := storage.CreateTaskThread(r.Context(), s.db, domain.DefaultProfileID, request.Name)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(thread.ID, nil)
	writeJSON(w, http.StatusCreated, thread)
}

func (s *Server) updateTaskThread(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("threadID")
	var request struct {
		Name        *string `json:"name"`
		Position    *int    `json:"position"`
		Archived    *bool   `json:"archived"`
		MapMarkdown *string `json:"map_markdown"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	thread, err := storage.UpdateTaskThread(r.Context(), s.db, domain.DefaultProfileID, threadID,
		domain.TaskThreadPatch{Name: request.Name, Position: request.Position, Archived: request.Archived, MapMarkdown: request.MapMarkdown})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, nil)
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) deleteTaskThread(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("threadID")
	if err := storage.DeleteTaskThread(r.Context(), s.db, domain.DefaultProfileID, threadID); err != nil {
		s.storageError(w, r, err)
		return
	}
	// The thread is gone, so the payload names it as deleted rather than resolving
	// an owner that no longer exists.
	s.publishTasksUpdated(threadID, map[string]string{"deleted_thread_id": threadID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createTaskLayer(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("threadID")
	var request struct {
		Name *string `json:"name"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	layer, err := storage.CreateTaskLayer(r.Context(), s.db, domain.DefaultProfileID, threadID, request.Name)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"layer_id": layer.ID})
	writeJSON(w, http.StatusCreated, layer)
}

func (s *Server) updateTaskLayer(w http.ResponseWriter, r *http.Request) {
	layerID := r.PathValue("layerID")
	var request struct {
		Name         *string `json:"name"`
		Position     *int    `json:"position"`
		ForcedUnlock *bool   `json:"forced_unlock"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	// Resolved before the write: a forced unlock or a move re-chains the layer
	// above it, and the client refreshes the whole board from the thread id.
	threadID := s.taskThreadID(r.Context(), "layer", layerID)
	layer, err := storage.UpdateTaskLayer(r.Context(), s.db, domain.DefaultProfileID, layerID,
		domain.TaskLayerPatch{Name: request.Name, Position: request.Position, ForcedUnlock: request.ForcedUnlock})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"layer_id": layer.ID})
	writeJSON(w, http.StatusOK, layer)
}

func (s *Server) deleteTaskLayer(w http.ResponseWriter, r *http.Request) {
	layerID := r.PathValue("layerID")
	threadID := s.taskThreadID(r.Context(), "layer", layerID)
	if err := storage.DeleteTaskLayer(r.Context(), s.db, domain.DefaultProfileID, layerID); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"deleted_layer_id": layerID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) closeTaskLayer(w http.ResponseWriter, r *http.Request) {
	layerID := r.PathValue("layerID")
	threadID := s.taskThreadID(r.Context(), "layer", layerID)
	layer, err := storage.CloseTaskLayer(r.Context(), s.db, domain.DefaultProfileID, layerID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"layer_id": layer.ID})
	writeJSON(w, http.StatusOK, layer)
}

func (s *Server) createTaskItem(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("threadID")
	var request struct {
		Title   *string `json:"title"`
		LayerID string  `json:"layer_id"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	// The board's 加一张卡 adds the card first and lets the owner title it after, so
	// an omitted title gets a placeholder rather than a 400. A title that is present
	// and blank is still the client's mistake, and storage rejects it.
	title := defaultTaskItemTitle
	if request.Title != nil {
		title = *request.Title
	}
	item, err := storage.CreateTaskItem(r.Context(), s.db, domain.DefaultProfileID, threadID,
		title, request.LayerID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"item_id": item.ID})
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateTaskItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemID")
	var request struct {
		Title    *string         `json:"title"`
		Notes    *string         `json:"notes"`
		Position *int            `json:"position"`
		PaperID  json.RawMessage `json:"paper_id"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	patch := domain.TaskItemPatch{Title: request.Title, Notes: request.Notes, Position: request.Position}
	// paper_id is tri-state, exactly like the research paper's citations field:
	// absent leaves the link alone, an explicit null unlinks the card, and a string
	// re-points it. A patch that only wants to rename must not clear the paper.
	if len(request.PaperID) > 0 {
		patch.SetPaperID = true
		if string(request.PaperID) != "null" {
			var paperID string
			if err := json.Unmarshal(request.PaperID, &paperID); err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_task_item", "Invalid paper link",
					"paper_id must be a paper id string or null.")
				return
			}
			if trimmed := strings.TrimSpace(paperID); trimmed != "" {
				patch.PaperID = &trimmed
			}
		}
	}
	threadID := s.taskThreadID(r.Context(), "item", itemID)
	item, err := storage.UpdateTaskItem(r.Context(), s.db, domain.DefaultProfileID, itemID, patch)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"item_id": item.ID})
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteTaskItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemID")
	threadID := s.taskThreadID(r.Context(), "item", itemID)
	if err := storage.DeleteTaskItem(r.Context(), s.db, domain.DefaultProfileID, itemID); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"deleted_item_id": itemID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderTaskItems(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("threadID")
	var request struct {
		ItemIDs []string `json:"item_ids"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if err := storage.ReorderTaskItems(r.Context(), s.db, domain.DefaultProfileID, threadID, request.ItemIDs); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"items_reordered": "true"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createTaskAssignment(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("itemID")
	var request struct {
		LayerID   string `json:"layer_id"`
		StartDate string `json:"start_date"`
		DueDate   string `json:"due_date"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	threadID := s.taskThreadID(r.Context(), "item", itemID)
	assignment, err := storage.CreateTaskAssignment(r.Context(), s.db, domain.DefaultProfileID,
		itemID, request.LayerID, request.StartDate, request.DueDate)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"item_id": itemID, "assignment_id": assignment.ID})
	writeJSON(w, http.StatusCreated, assignment)
}

func (s *Server) updateTaskAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID := r.PathValue("assignmentID")
	var request struct {
		StartDate *string `json:"start_date"`
		DueDate   *string `json:"due_date"`
		Position  *int    `json:"position"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	threadID := s.taskThreadID(r.Context(), "assignment", assignmentID)
	assignment, err := storage.UpdateTaskAssignment(r.Context(), s.db, domain.DefaultProfileID, assignmentID,
		domain.TaskAssignmentPatch{StartDate: request.StartDate, DueDate: request.DueDate, Position: request.Position})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"assignment_id": assignment.ID})
	writeJSON(w, http.StatusOK, assignment)
}

func (s *Server) deleteTaskAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID := r.PathValue("assignmentID")
	threadID := s.taskThreadID(r.Context(), "assignment", assignmentID)
	if err := storage.DeleteTaskAssignment(r.Context(), s.db, domain.DefaultProfileID, assignmentID); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"deleted_assignment_id": assignmentID})
	w.WriteHeader(http.StatusNoContent)
}

// completeTaskAssignment is the card-level 完成 button: one click closes the
// placement's whole checklist instead of asking for a tick per step.
func (s *Server) completeTaskAssignment(w http.ResponseWriter, r *http.Request) {
	assignmentID := r.PathValue("assignmentID")
	threadID := s.taskThreadID(r.Context(), "assignment", assignmentID)
	assignment, err := storage.CompleteTaskAssignment(r.Context(), s.db, domain.DefaultProfileID, assignmentID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"assignment_id": assignment.ID})
	writeJSON(w, http.StatusOK, assignment)
}

func (s *Server) createTaskStep(w http.ResponseWriter, r *http.Request) {
	assignmentID := r.PathValue("assignmentID")
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	threadID := s.taskThreadID(r.Context(), "assignment", assignmentID)
	step, err := storage.CreateTaskStep(r.Context(), s.db, domain.DefaultProfileID, assignmentID, request.Name)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"step_id": step.ID})
	writeJSON(w, http.StatusCreated, step)
}

func (s *Server) updateTaskStep(w http.ResponseWriter, r *http.Request) {
	stepID := r.PathValue("stepID")
	var request struct {
		Name     *string `json:"name"`
		Done     *bool   `json:"done"`
		Position *int    `json:"position"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	// Ticking a step is the write that changes the most on the board: it moves the
	// card's percentage, its 本层已收口 chip, the layer's tally, the next layer's
	// unlock and the strip counts, so the event names the thread and the client
	// refetches the tree.
	threadID := s.taskThreadID(r.Context(), "step", stepID)
	step, err := storage.UpdateTaskStep(r.Context(), s.db, domain.DefaultProfileID, stepID,
		domain.TaskStepPatch{Name: request.Name, Done: request.Done, Position: request.Position})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"step_id": step.ID})
	writeJSON(w, http.StatusOK, step)
}

func (s *Server) deleteTaskStep(w http.ResponseWriter, r *http.Request) {
	stepID := r.PathValue("stepID")
	threadID := s.taskThreadID(r.Context(), "step", stepID)
	if err := storage.DeleteTaskStep(r.Context(), s.db, domain.DefaultProfileID, stepID); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated(threadID, map[string]string{"deleted_step_id": stepID})
	w.WriteHeader(http.StatusNoContent)
}

// taskThreadID resolves the 主线 a board row belongs to so tasks.updated can name
// the tree the client should refetch. A row that is already gone resolves to "":
// the write committed, and a payload without a thread hint is a stale snapshot, not
// a failed request.
func (s *Server) taskThreadID(ctx context.Context, entity, id string) string {
	threadID, err := storage.ResolveTaskThreadID(ctx, s.db, domain.DefaultProfileID, entity, id)
	if err != nil {
		return ""
	}
	return threadID
}

func (s *Server) listQuickTasks(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListQuickTasks(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createQuickTask(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title     string   `json:"title"`
		Notes     string   `json:"notes"`
		Tags      []string `json:"tags"`
		StartDate string   `json:"start_date"`
		DueDate   string   `json:"due_date"`
		PaperID   *string  `json:"paper_id"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	task, err := storage.CreateQuickTask(r.Context(), s.db, domain.DefaultProfileID,
		request.Title, request.Notes, request.StartDate, request.DueDate, request.PaperID, request.Tags)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated("", map[string]string{"quick_task_id": task.ID})
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) updateQuickTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("quickTaskID")
	var request struct {
		Title     *string         `json:"title"`
		Notes     *string         `json:"notes"`
		Tags      *[]string       `json:"tags"`
		StartDate *string         `json:"start_date"`
		DueDate   *string         `json:"due_date"`
		PaperID   json.RawMessage `json:"paper_id"`
		Completed *bool           `json:"completed"`
		Position  *int            `json:"position"`
	}
	if err := decodeTaskJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	patch := domain.QuickTaskPatch{Title: request.Title, Notes: request.Notes, Tags: request.Tags,
		StartDate: request.StartDate, DueDate: request.DueDate, Completed: request.Completed, Position: request.Position}
	if len(request.PaperID) > 0 {
		patch.SetPaperID = true
		if string(request.PaperID) != "null" {
			var paperID string
			if err := json.Unmarshal(request.PaperID, &paperID); err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_quick_task", "Invalid paper link",
					"paper_id must be a paper id string or null.")
				return
			}
			if trimmed := strings.TrimSpace(paperID); trimmed != "" {
				patch.PaperID = &trimmed
			}
		}
	}
	task, err := storage.UpdateQuickTask(r.Context(), s.db, domain.DefaultProfileID, taskID, patch)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated("", map[string]string{"quick_task_id": task.ID})
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) deleteQuickTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("quickTaskID")
	if err := storage.DeleteQuickTask(r.Context(), s.db, domain.DefaultProfileID, taskID); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.publishTasksUpdated("", map[string]string{"deleted_quick_task_id": taskID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) publishTasksUpdated(threadID string, fields map[string]string) {
	payload := make(map[string]string, len(fields)+1)
	payload["thread_id"] = threadID
	for key, value := range fields {
		payload[key] = value
	}
	s.events.Publish("tasks.updated", payload)
}

// decodeTaskJSON reads a board request body. An omitted body is read as {}: every
// create call on the board has a server-side default for everything it accepts, and
// the owner adding a layer by clicking 加一层 should not have to invent an empty JSON
// document to send with it.
func decodeTaskJSON(w http.ResponseWriter, r *http.Request, target any) error {
	if r.ContentLength == 0 {
		r.Body = io.NopCloser(strings.NewReader("{}"))
	}
	return decodeJSON(w, r, target)
}
