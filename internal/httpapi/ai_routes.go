package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/service"
	"github.com/Zijinn/ReFlow/internal/storage"
)

type aiProfileRequest struct {
	Provider              string             `json:"provider"`
	Name                  string             `json:"name"`
	Endpoint              string             `json:"endpoint"`
	Model                 string             `json:"model"`
	APIKey                string             `json:"api_key"`
	Settings              service.AISettings `json:"settings"`
	Enabled               *bool              `json:"enabled"`
	AllowPrivateNetwork   bool               `json:"allow_private_network"`
	RemoteContentApproved bool               `json:"remote_content_approved"`
	IsDefault             bool               `json:"is_default"`
}

type aiProfilePatchRequest struct {
	Name                  *string             `json:"name"`
	Endpoint              *string             `json:"endpoint"`
	Model                 *string             `json:"model"`
	APIKey                *string             `json:"api_key"`
	Settings              *service.AISettings `json:"settings"`
	Enabled               *bool               `json:"enabled"`
	AllowPrivateNetwork   *bool               `json:"allow_private_network"`
	RemoteContentApproved *bool               `json:"remote_content_approved"`
	IsDefault             *bool               `json:"is_default"`
}

func (s *Server) listAIProviders(w http.ResponseWriter, r *http.Request) {
	providers := service.SupportedAIProviders()
	keys := make([]string, 0, len(providers))
	for key := range providers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, map[string]string{"id": key, "name": providers[key]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listAIProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	items, err := s.ai.ListProfiles(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createAIProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request aiProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	created, err := s.ai.CreateProfile(r.Context(), service.AIProfileInput{
		Provider: request.Provider, Name: request.Name, Endpoint: request.Endpoint, Model: request.Model,
		APIKey: request.APIKey, Settings: request.Settings, Enabled: request.Enabled,
		AllowPrivateNetwork: request.AllowPrivateNetwork, RemoteContentApproved: request.RemoteContentApproved,
		IsDefault: request.IsDefault,
	})
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateAIProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request aiProfilePatchRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if request.Name == nil && request.Endpoint == nil && request.Model == nil && request.APIKey == nil &&
		request.Settings == nil && request.Enabled == nil && request.AllowPrivateNetwork == nil &&
		request.RemoteContentApproved == nil && request.IsDefault == nil {
		writeProblem(w, r, http.StatusBadRequest, "empty_patch", "No changes provided", "Provide at least one AI profile field.")
		return
	}
	updated, err := s.ai.UpdateProfile(r.Context(), r.PathValue("profileID"), service.AIProfileUpdate{
		Name: request.Name, Endpoint: request.Endpoint, Model: request.Model, APIKey: request.APIKey,
		Settings: request.Settings, Enabled: request.Enabled, AllowPrivateNetwork: request.AllowPrivateNetwork,
		RemoteContentApproved: request.RemoteContentApproved, IsDefault: request.IsDefault,
	})
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteAIProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	if err := s.ai.DeleteProfile(r.Context(), r.PathValue("profileID")); err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAIUsage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	usage, err := s.ai.UsageTotals(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) listAIResults(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	if _, err := storage.GetAIEntryContent(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	items, err := s.ai.ListResults(r.Context(), r.PathValue("entryID"))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) runAIOperation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request struct {
		ProfileID string `json:"profile_id"`
		Language  string `json:"language"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	operation := strings.ReplaceAll(r.PathValue("operation"), "-", "_")
	cached, payload, err := s.ai.PrepareOperation(r.Context(), r.PathValue("entryID"), request.ProfileID, operation, request.Language)
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	if cached != nil {
		writeJSON(w, http.StatusOK, map[string]any{"cached": true, "result": cached})
		return
	}
	pending, err := storage.FindPendingAIOperationJob(r.Context(), s.db, payload.EntryID, payload.Operation, payload.Language, payload.InputHash)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if pending != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"cached": false, "job": pending})
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "ai.operation", payload)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"cached": false, "job": queued})
}

func (s *Server) startAIChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request struct {
		ProfileID string `json:"profile_id"`
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	session, payload, err := s.ai.PrepareChat(r.Context(), r.PathValue("entryID"), request.ProfileID, request.SessionID, request.Message)
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "ai.chat", payload)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": queued, "session": session})
}

func (s *Server) startAILibraryChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request struct {
		ProfileID string   `json:"profile_id"`
		SessionID string   `json:"session_id"`
		Message   string   `json:"message"`
		EntryIDs  []string `json:"entry_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	session, payload, err := s.ai.PrepareLibraryChat(r.Context(), request.EntryIDs, request.ProfileID, request.SessionID, request.Message)
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "ai.chat", payload)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": queued, "session": session})
}

func (s *Server) startAIPaperChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request struct {
		ProfileID string   `json:"profile_id"`
		SessionID string   `json:"session_id"`
		Message   string   `json:"message"`
		PaperIDs  []string `json:"paper_ids"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	session, payload, err := s.ai.PreparePaperChat(r.Context(), request.PaperIDs, request.ProfileID, request.SessionID, request.Message)
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "ai.research", payload)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": queued, "session": session})
}

// startAIDailyDigest enqueues the whole-workspace progress briefing. Response
// and polling match the chat endpoints: the client waits for the job then reads
// the assistant message from GET /api/v1/ai/chats/{session_id}.
func (s *Server) startAIDailyDigest(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request struct {
		ProfileID string `json:"profile_id"`
		Language  string `json:"language"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	session, payload, err := s.ai.PrepareDigest(r.Context(), request.ProfileID, request.Language)
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "ai.research", payload)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": queued, "session": session})
}

func (s *Server) getAIChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	session, err := s.ai.GetChat(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

// fillAIMetadata answers a small synchronous metadata-fill request (no job
// queue): one pasted reference in, whitelisted bibliographic fields out. The
// service bounds the provider call with its own context timeout; an unusable
// model answer is a stable 422.
func (s *Server) fillAIMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.requireAI(w, r) {
		return
	}
	var request struct {
		ProfileID string `json:"profile_id"`
		Raw       string `json:"raw"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	filled, err := s.ai.FillMetadata(r.Context(), request.ProfileID, request.Raw)
	if err != nil {
		s.aiRequestError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, filled)
}

func (s *Server) requireAI(w http.ResponseWriter, r *http.Request) bool {
	if s.ai != nil {
		return true
	}
	writeProblem(w, r, http.StatusServiceUnavailable, "ai_unavailable", "AI unavailable", "Credential encryption is not configured on this ReFlow server.")
	return false
}

func (s *Server) aiRequestError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		s.storageError(w, r, err)
	case errors.Is(err, service.ErrAIPrivacyApprovalRequired):
		writeProblem(w, r, http.StatusPreconditionRequired, "privacy_approval_required", "Privacy approval required", "Approve remote article transmission in the AI profile before using it.")
	case errors.Is(err, service.ErrAIProfileDisabled):
		writeProblem(w, r, http.StatusConflict, "ai_profile_disabled", "AI profile disabled", err.Error())
	case errors.Is(err, service.ErrAIMetadataUnparseable):
		writeProblem(w, r, http.StatusUnprocessableEntity, "ai_metadata_unparseable", "AI metadata unparseable", "The AI answer did not contain usable reference metadata.")
	case errors.Is(err, context.DeadlineExceeded):
		writeProblem(w, r, http.StatusGatewayTimeout, "ai_timeout", "AI request timed out", "The AI provider did not answer in time.")
	default:
		writeProblem(w, r, http.StatusBadRequest, "invalid_ai_request", "Invalid AI request", err.Error())
	}
}
