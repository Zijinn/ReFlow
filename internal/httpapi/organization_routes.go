package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_folder", "Invalid folder")
		return
	}
	if strings.TrimSpace(request.Name) == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_folder", "Invalid folder", "Folder name is required.")
		return
	}
	item, err := storage.EnsureFolder(r.Context(), s.db, domain.DefaultProfileID, request.ParentID, strings.TrimSpace(request.Name))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.events.Publish("subscriptions.updated", map[string]string{"folder_id": item.ID})
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateFolder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name     json.RawMessage `json:"name"`
		ParentID json.RawMessage `json:"parent_id"`
		Position json.RawMessage `json:"position"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_folder", "Invalid folder")
		return
	}
	name, err := optionalString(request.Name)
	if err != nil || (name != nil && strings.TrimSpace(*name) == "") {
		writeProblem(w, r, http.StatusBadRequest, "invalid_folder", "Invalid folder", "Folder name cannot be empty.")
		return
	}
	setParent, parentID, err := optionalNullableString(request.ParentID)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_folder", "Invalid folder", err.Error())
		return
	}
	position, err := optionalInt(request.Position)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_folder", "Invalid folder", err.Error())
		return
	}
	item, err := storage.UpdateFolder(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("folderID"), setParent, parentID, name, position)
	if err != nil {
		if strings.Contains(err.Error(), "cycle") || strings.Contains(err.Error(), "itself") {
			writeProblem(w, r, http.StatusConflict, "folder_cycle", "Invalid folder nesting", err.Error())
			return
		}
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("subscriptions.updated", map[string]string{"folder_id": item.ID})
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteFolder(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("folderID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("subscriptions.updated", map[string]string{"deleted_folder_id": r.PathValue("folderID")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListTags(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name  string  `json:"name"`
		Color *string `json:"color"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_tag", "Invalid tag")
		return
	}
	item, err := storage.CreateTag(r.Context(), s.db, domain.DefaultProfileID, request.Name, request.Color)
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "tag_failed", "Tag could not be created", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteTag(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("tagID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setEntryTags(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TagIDs []string `json:"tag_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_tags", "Invalid tags")
		return
	}
	if err := storage.SetEntryTags(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"), request.TagIDs); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("entry.updated", map[string]string{"id": r.PathValue("entryID")})
	writeJSON(w, http.StatusOK, map[string]any{"tag_ids": request.TagIDs})
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListRules(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name       string          `json:"name"`
		Enabled    *bool           `json:"enabled"`
		Priority   int             `json:"priority"`
		Conditions json.RawMessage `json:"conditions"`
		Actions    json.RawMessage `json:"actions"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_rule", "Invalid rule")
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	item, err := storage.CreateRule(r.Context(), s.db, domain.DefaultProfileID, request.Name, enabled, request.Priority, request.Conditions, request.Actions)
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "rule_failed", "Rule could not be created", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteRule(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("ruleID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSavedFilters(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListSavedFilters(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createSavedFilter(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name  string          `json:"name"`
		Query json.RawMessage `json:"query"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_filter", "Invalid saved filter")
		return
	}
	item, err := storage.CreateSavedFilter(r.Context(), s.db, domain.DefaultProfileID, request.Name, request.Query)
	if err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "filter_failed", "Saved filter could not be created", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) deleteSavedFilter(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteSavedFilter(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("filterID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
