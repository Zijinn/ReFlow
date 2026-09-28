package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func (s *Server) createPairingCode(w http.ResponseWriter, r *http.Request) {
	code, expiresAt, err := storage.CreatePairingCode(r.Context(), s.db, 10*time.Minute)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expires_at": expiresAt})
}

func (s *Server) pairDevice(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_pairing_request", "Invalid pairing request")
		return
	}
	device, token, err := storage.PairDevice(r.Context(), s.db, request.Code, request.Name, request.Platform)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidPairingCode) {
			writeProblem(w, r, http.StatusBadRequest, "invalid_pairing_code", "Pairing failed", "The pairing code is invalid or expired.")
			return
		}
		s.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "reflow_device", Value: token, Path: "/api", MaxAge: 365 * 24 * 60 * 60,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"device": device, "token": token})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListDevices(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	if err := storage.RevokeDevice(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("deviceID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
