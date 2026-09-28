package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Zijinn/ReFlow/internal/service"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func (s *Server) getZoteroStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	status, err := s.zotero.Status(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available":     false,
			"editable":      false,
			"error_message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) getEntryZoteroStatus(w http.ResponseWriter, r *http.Request) {
	exported, err := s.zotero.GetExport(r.Context(), r.PathValue("entryID"))
	if errors.Is(err, storage.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"saved": false})
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "export": exported})
}

func (s *Server) saveEntryToZotero(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := s.zotero.Save(ctx, r.PathValue("entryID"))
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			s.storageError(w, r, err)
		case errors.Is(err, service.ErrZoteroUnavailable):
			writeProblem(w, r, http.StatusServiceUnavailable, "zotero_unavailable", "Zotero unavailable", err.Error())
		case errors.Is(err, service.ErrZoteroTarget):
			writeProblem(w, r, http.StatusConflict, "zotero_target_required", "Zotero target required", err.Error())
		default:
			writeProblem(w, r, http.StatusBadGateway, "zotero_save_failed", "Zotero save failed", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}
