package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/Zijinn/ReFlow/internal/job"
	"github.com/Zijinn/ReFlow/internal/service"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/Zijinn/ReFlow/internal/syncadapter"
)

type syncAccountRequest struct {
	Provider            string                  `json:"provider"`
	Name                string                  `json:"name"`
	Endpoint            string                  `json:"endpoint"`
	Credentials         syncadapter.Credentials `json:"credentials"`
	Enabled             *bool                   `json:"enabled"`
	AllowPrivateNetwork bool                    `json:"allow_private_network"`
	SyncIntervalMinutes int                     `json:"sync_interval_minutes"`
}

type syncAccountPatchRequest struct {
	Name                *string                  `json:"name"`
	Endpoint            *string                  `json:"endpoint"`
	Credentials         *syncadapter.Credentials `json:"credentials"`
	Enabled             *bool                    `json:"enabled"`
	AllowPrivateNetwork *bool                    `json:"allow_private_network"`
	SyncIntervalMinutes *int                     `json:"sync_interval_minutes"`
}

type syncConnectionTestRequest struct {
	AccountID           string                  `json:"account_id"`
	Provider            string                  `json:"provider"`
	Endpoint            string                  `json:"endpoint"`
	Credentials         syncadapter.Credentials `json:"credentials"`
	AllowPrivateNetwork *bool                   `json:"allow_private_network"`
}

func (s *Server) listSyncProviders(w http.ResponseWriter, r *http.Request) {
	providers := service.SupportedSyncProviders()
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

func (s *Server) listSyncAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requireSync(w, r) {
		return
	}
	items, err := s.syncs.ListAccounts(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createSyncAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireSync(w, r) {
		return
	}
	var request syncAccountRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	created, err := s.syncs.CreateAccount(r.Context(), service.SyncAccountInput{
		Provider: request.Provider, Name: request.Name, Endpoint: request.Endpoint,
		Credentials: request.Credentials, Enabled: request.Enabled,
		AllowPrivateNetwork: request.AllowPrivateNetwork, SyncIntervalMinutes: request.SyncIntervalMinutes,
	})
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_sync_account", "Invalid sync account", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) updateSyncAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireSync(w, r) {
		return
	}
	var request syncAccountPatchRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if request.Name == nil && request.Endpoint == nil && request.Credentials == nil && request.Enabled == nil &&
		request.AllowPrivateNetwork == nil && request.SyncIntervalMinutes == nil {
		writeProblem(w, r, http.StatusBadRequest, "empty_patch", "No changes provided", "Provide at least one sync account field.")
		return
	}
	updated, err := s.syncs.UpdateAccount(r.Context(), r.PathValue("accountID"), service.SyncAccountUpdate{
		Name: request.Name, Endpoint: request.Endpoint, Credentials: request.Credentials,
		Enabled: request.Enabled, AllowPrivateNetwork: request.AllowPrivateNetwork,
		SyncIntervalMinutes: request.SyncIntervalMinutes,
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.storageError(w, r, err)
			return
		}
		writeProblem(w, r, http.StatusBadRequest, "invalid_sync_account", "Invalid sync account", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) testSyncAccountConnection(w http.ResponseWriter, r *http.Request) {
	if !s.requireSync(w, r) {
		return
	}
	var request syncConnectionTestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	result, err := s.syncs.TestConnection(r.Context(), service.SyncConnectionTestInput{
		AccountID: request.AccountID, Provider: request.Provider, Endpoint: request.Endpoint,
		Credentials: request.Credentials, AllowPrivateNetwork: request.AllowPrivateNetwork,
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.storageError(w, r, err)
			return
		}
		status := http.StatusBadRequest
		code := syncadapter.ErrorCode(err)
		switch code {
		case "authentication_error":
			status = http.StatusUnauthorized
		case "network_error", "http_error":
			status = http.StatusBadGateway
		default:
			code = "connection_test_failed"
		}
		writeProblem(w, r, status, code, "Connection test failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteSyncAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireSync(w, r) {
		return
	}
	if err := s.syncs.DeleteAccount(r.Context(), r.PathValue("accountID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runSyncAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireSync(w, r) {
		return
	}
	accounts, err := s.syncs.ListAccounts(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	found, enabled := false, false
	for _, account := range accounts {
		if account.ID == r.PathValue("accountID") {
			found, enabled = true, account.Enabled
			break
		}
	}
	if !found {
		s.storageError(w, r, storage.ErrNotFound)
		return
	}
	if !enabled {
		writeProblem(w, r, http.StatusConflict, "sync_disabled", "Sync account disabled", "Enable the account before starting synchronization.")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "push" && mode != "pull" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_sync_mode", "Invalid sync mode", "Mode must be auto, push, or pull.")
		return
	}
	queued, err := s.jobs.EnqueueAccountSync(r.Context(), r.PathValue("accountID"), mode)
	if err != nil {
		if errors.Is(err, job.ErrJobAlreadyQueued) {
			writeProblem(w, r, http.StatusConflict, "sync_pending", "Sync already pending", err.Error())
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, queued)
}

func (s *Server) requireSync(w http.ResponseWriter, r *http.Request) bool {
	if s.syncs != nil {
		return true
	}
	writeProblem(w, r, http.StatusServiceUnavailable, "sync_unavailable", "Synchronization unavailable", "Credential encryption is not configured on this ReFlow server.")
	return false
}
