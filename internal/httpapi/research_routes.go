package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/service"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func (s *Server) listResearchPapers(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if !storage.IsResearchKind(kind) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_research_kind", "Invalid kind", "Kind must be research, submitted, or published.")
		return
	}
	items, err := storage.ListResearchPapers(r.Context(), s.db, domain.DefaultProfileID, kind)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getResearchPaper(w http.ResponseWriter, r *http.Request) {
	paper, err := storage.GetResearchPaper(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("paperID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, paper)
}

func (s *Server) createResearchPaper(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Kind    string   `json:"kind"`
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if !storage.IsResearchKind(request.Kind) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_research_kind", "Invalid kind", "Kind must be research, submitted, or published.")
		return
	}
	paper := domain.ResearchPaper{Kind: request.Kind, Title: request.Title, Authors: request.Authors}
	if paper.Authors == nil {
		paper.Authors = []string{}
	}
	created, err := storage.CreateResearchPaper(r.Context(), s.db, domain.DefaultProfileID, paper)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("research.updated", map[string]string{"paper_id": created.ID, "kind": created.Kind})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateResearchPaper(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Title           *string                    `json:"title"`
		Authors         *[]string                  `json:"authors"`
		Keywords        *[]string                  `json:"keywords"`
		FilePath        *string                    `json:"file_path"`
		NextAction      *string                    `json:"next_action"`
		Notes           *string                    `json:"notes"`
		ResearchArea    *string                    `json:"research_area"`
		Status          *string                    `json:"status"`
		Priority        *string                    `json:"priority"`
		TargetJournal   *string                    `json:"target_journal"`
		Stages          *[]domain.ResearchStage    `json:"stages"`
		CurrentJournal  *string                    `json:"current_journal"`
		SubmissionDate  *string                    `json:"submission_date"`
		ManuscriptID    *string                    `json:"manuscript_id"`
		SubmissionCount *int                       `json:"submission_count"`
		TargetLevel     *string                    `json:"target_level"`
		Editor          *string                    `json:"editor"`
		Deadline        *string                    `json:"deadline"`
		History         *[]domain.SubmissionRecord `json:"history"`
		Abstract        *string                    `json:"abstract"`
		Journal         *string                    `json:"journal"`
		Language        *string                    `json:"language"`
		Year            *string                    `json:"year"`
		Volume          *string                    `json:"volume"`
		Issue           *string                    `json:"issue"`
		Pages           *string                    `json:"pages"`
		DOI             *string                    `json:"doi"`
		Citations       json.RawMessage            `json:"citations"`
		CitationSource  *string                    `json:"citation_source"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	patch := domain.ResearchPaperPatch{
		Title: request.Title, Authors: request.Authors, Keywords: request.Keywords,
		FilePath: request.FilePath, NextAction: request.NextAction, Notes: request.Notes,
		ResearchArea: request.ResearchArea, Status: request.Status, Priority: request.Priority,
		TargetJournal: request.TargetJournal, Stages: request.Stages,
		CurrentJournal: request.CurrentJournal, SubmissionDate: request.SubmissionDate,
		ManuscriptID: request.ManuscriptID, SubmissionCount: request.SubmissionCount,
		TargetLevel: request.TargetLevel, Editor: request.Editor, Deadline: request.Deadline,
		History:  request.History,
		Abstract: request.Abstract, Journal: request.Journal, Language: request.Language,
		Year: request.Year, Volume: request.Volume, Issue: request.Issue, Pages: request.Pages,
		DOI: request.DOI, CitationSource: request.CitationSource,
	}
	if len(request.Citations) > 0 {
		patch.SetCitations = true
		if string(request.Citations) != "null" {
			var count int
			if err := json.Unmarshal(request.Citations, &count); err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_citations", "Invalid citations", "Citations must be an integer or null.")
				return
			}
			patch.Citations = &count
		}
	}
	updated, err := storage.UpdateResearchPaper(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("paperID"), patch)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("research.updated", map[string]string{"paper_id": updated.ID, "kind": updated.Kind})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteResearchPaper(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteResearchPaper(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("paperID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("research.updated", map[string]string{"deleted_paper_id": r.PathValue("paperID")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderResearchPapers(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Kind     string   `json:"kind"`
		PaperIDs []string `json:"paper_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if !storage.IsResearchKind(request.Kind) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_research_kind", "Invalid kind", "Kind must be research, submitted, or published.")
		return
	}
	if err := storage.ReorderResearchPapers(r.Context(), s.db, domain.DefaultProfileID, request.Kind, request.PaperIDs); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("research.updated", map[string]string{"kind": request.Kind, "reordered": "true"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) moveResearchPaper(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Kind string `json:"kind"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if !storage.IsResearchKind(request.Kind) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_research_kind", "Invalid kind", "Kind must be research, submitted, or published.")
		return
	}
	moved, err := storage.MoveResearchPaper(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("paperID"), request.Kind)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("research.updated", map[string]string{"paper_id": moved.ID, "kind": moved.Kind})
	writeJSON(w, http.StatusOK, moved)
}

func (s *Server) fetchResearchCitation(w http.ResponseWriter, r *http.Request) {
	paper, err := s.research.FetchCitation(r.Context(), r.PathValue("paperID"))
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			s.storageError(w, r, err)
		case errors.Is(err, service.ErrCitationNotEnglish):
			writeProblem(w, r, http.StatusConflict, "citation_not_english", "Crossref unavailable for this paper", err.Error())
		case errors.Is(err, service.ErrCitationNoDOI):
			writeProblem(w, r, http.StatusUnprocessableEntity, "citation_no_doi", "DOI required", err.Error())
		case errors.Is(err, service.ErrDoiNotFound):
			writeProblem(w, r, http.StatusUnprocessableEntity, "doi_not_found", "DOI not found in Crossref", err.Error())
		case errors.Is(err, service.ErrCrossrefUnavailable):
			writeProblem(w, r, http.StatusBadGateway, "crossref_unavailable", "Crossref unavailable", err.Error())
		default:
			s.internalError(w, r, err)
		}
		return
	}
	s.events.Publish("research.updated", map[string]string{"paper_id": paper.ID, "kind": paper.Kind})
	writeJSON(w, http.StatusOK, paper)
}
