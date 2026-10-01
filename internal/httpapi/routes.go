package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/job"
	"github.com/Zijinn/ReFlow/internal/opml"
	"github.com/Zijinn/ReFlow/internal/service"
	"github.com/Zijinn/ReFlow/internal/storage"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/feeds", s.listFeeds)
	mux.HandleFunc("POST /api/v1/feeds", s.createFeed)
	mux.HandleFunc("POST /api/v1/feeds/discover", s.discoverFeeds)
	mux.HandleFunc("GET /api/v1/feeds/{feedID}", s.getFeed)
	mux.HandleFunc("PATCH /api/v1/feeds/{feedID}", s.updateFeed)
	mux.HandleFunc("DELETE /api/v1/feeds/{feedID}", s.deleteFeed)
	mux.HandleFunc("POST /api/v1/feeds/{feedID}/refresh", s.refreshFeed)
	mux.HandleFunc("GET /api/v1/subscriptions", s.listSubscriptions)
	mux.HandleFunc("GET /api/v1/folders", s.listFolders)
	mux.HandleFunc("POST /api/v1/folders", s.createFolder)
	mux.HandleFunc("PATCH /api/v1/folders/{folderID}", s.updateFolder)
	mux.HandleFunc("DELETE /api/v1/folders/{folderID}", s.deleteFolder)
	mux.HandleFunc("GET /api/v1/entries", s.listEntries)
	mux.HandleFunc("POST /api/v1/entries/mark-read", s.markEntriesRead)
	mux.HandleFunc("GET /api/v1/entries/{entryID}", s.getEntry)
	mux.HandleFunc("GET /api/v1/preferences", s.listPreferences)
	mux.HandleFunc("PUT /api/v1/preferences/{key}", s.putPreference)
	mux.HandleFunc("PATCH /api/v1/entries/{entryID}/state", s.updateEntryState)
	mux.HandleFunc("POST /api/v1/entries/{entryID}/readability", s.fetchReadability)
	mux.HandleFunc("PUT /api/v1/entries/{entryID}/tags", s.setEntryTags)
	mux.HandleFunc("GET /api/v1/entries/{entryID}/annotations", s.listEntryAnnotations)
	mux.HandleFunc("POST /api/v1/entries/{entryID}/annotations", s.createEntryAnnotation)
	mux.HandleFunc("PATCH /api/v1/entries/{entryID}/annotations/{annotationID}", s.updateEntryAnnotation)
	mux.HandleFunc("DELETE /api/v1/entries/{entryID}/annotations/{annotationID}", s.deleteEntryAnnotation)
	mux.HandleFunc("GET /api/v1/tags", s.listTags)
	mux.HandleFunc("POST /api/v1/tags", s.createTag)
	mux.HandleFunc("DELETE /api/v1/tags/{tagID}", s.deleteTag)
	mux.HandleFunc("GET /api/v1/rules", s.listRules)
	mux.HandleFunc("POST /api/v1/rules", s.createRule)
	mux.HandleFunc("DELETE /api/v1/rules/{ruleID}", s.deleteRule)
	mux.HandleFunc("GET /api/v1/saved-filters", s.listSavedFilters)
	mux.HandleFunc("POST /api/v1/saved-filters", s.createSavedFilter)
	mux.HandleFunc("DELETE /api/v1/saved-filters/{filterID}", s.deleteSavedFilter)
	mux.HandleFunc("POST /api/v1/imports/opml", s.importOPML)
	mux.HandleFunc("GET /api/v1/exports/opml", s.exportOPML)
	mux.HandleFunc("GET /api/v1/jobs/{jobID}", s.getJob)
	mux.HandleFunc("POST /api/v1/jobs/{jobID}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/v1/events", s.streamEvents)
	mux.HandleFunc("GET /api/v1/backup", s.exportBackup)
	mux.HandleFunc("POST /api/v1/restore", s.restoreBackup)
	mux.HandleFunc("POST /api/v1/devices/pairing-code", s.createPairingCode)
	mux.HandleFunc("POST /api/v1/devices/pair", s.pairDevice)
	mux.HandleFunc("GET /api/v1/devices", s.listDevices)
	mux.HandleFunc("DELETE /api/v1/devices/{deviceID}", s.revokeDevice)
	mux.HandleFunc("GET /api/v1/sync/providers", s.listSyncProviders)
	mux.HandleFunc("GET /api/v1/sync/accounts", s.listSyncAccounts)
	mux.HandleFunc("POST /api/v1/sync/accounts", s.createSyncAccount)
	mux.HandleFunc("POST /api/v1/sync/accounts/test", s.testSyncAccountConnection)
	mux.HandleFunc("PATCH /api/v1/sync/accounts/{accountID}", s.updateSyncAccount)
	mux.HandleFunc("DELETE /api/v1/sync/accounts/{accountID}", s.deleteSyncAccount)
	mux.HandleFunc("POST /api/v1/sync/accounts/{accountID}/sync", s.runSyncAccount)
	mux.HandleFunc("GET /api/v1/ai/providers", s.listAIProviders)
	mux.HandleFunc("GET /api/v1/ai/profiles", s.listAIProfiles)
	mux.HandleFunc("POST /api/v1/ai/profiles", s.createAIProfile)
	mux.HandleFunc("PATCH /api/v1/ai/profiles/{profileID}", s.updateAIProfile)
	mux.HandleFunc("DELETE /api/v1/ai/profiles/{profileID}", s.deleteAIProfile)
	mux.HandleFunc("GET /api/v1/ai/usage", s.getAIUsage)
	mux.HandleFunc("GET /api/v1/entries/{entryID}/ai-results", s.listAIResults)
	mux.HandleFunc("POST /api/v1/entries/{entryID}/ai/{operation}", s.runAIOperation)
	mux.HandleFunc("POST /api/v1/entries/{entryID}/ai-chat", s.startAIChat)
	mux.HandleFunc("POST /api/v1/ai/library-chat", s.startAILibraryChat)
	mux.HandleFunc("POST /api/v1/ai/paper-chat", s.startAIPaperChat)
	mux.HandleFunc("POST /api/v1/ai/daily-digest", s.startAIDailyDigest)
	mux.HandleFunc("POST /api/v1/ai/metadata-fill", s.fillAIMetadata)
	mux.HandleFunc("GET /api/v1/ai/chats/{sessionID}", s.getAIChat)
	mux.HandleFunc("GET /api/v1/research/papers", s.listResearchPapers)
	mux.HandleFunc("POST /api/v1/research/papers", s.createResearchPaper)
	mux.HandleFunc("POST /api/v1/research/papers/reorder", s.reorderResearchPapers)
	mux.HandleFunc("GET /api/v1/research/papers/{paperID}", s.getResearchPaper)
	mux.HandleFunc("PATCH /api/v1/research/papers/{paperID}", s.updateResearchPaper)
	mux.HandleFunc("DELETE /api/v1/research/papers/{paperID}", s.deleteResearchPaper)
	mux.HandleFunc("POST /api/v1/research/papers/{paperID}/move", s.moveResearchPaper)
	mux.HandleFunc("POST /api/v1/research/papers/{paperID}/citation", s.fetchResearchCitation)
	mux.HandleFunc("GET /api/v1/research/tags", s.listResearchTags)
	mux.HandleFunc("POST /api/v1/research/tags", s.createResearchTag)
	mux.HandleFunc("PATCH /api/v1/research/tags/{tagID}", s.renameResearchTag)
	mux.HandleFunc("DELETE /api/v1/research/tags/{tagID}", s.deleteResearchTag)
	mux.HandleFunc("GET /api/v1/integrations/zotero/status", s.getZoteroStatus)
	mux.HandleFunc("GET /api/v1/entries/{entryID}/zotero", s.getEntryZoteroStatus)
	mux.HandleFunc("POST /api/v1/entries/{entryID}/zotero", s.saveEntryToZotero)
}

func (s *Server) listFeeds(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListFeeds(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createFeed(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL           string  `json:"url"`
		FolderID      *string `json:"folder_id"`
		TitleOverride *string `json:"title_override"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if strings.TrimSpace(request.URL) == "" {
		writeProblem(w, r, http.StatusBadRequest, "url_required", "Feed URL required", "Provide an HTTP, HTTPS, or rsshub:// URL.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), feedcore.DefaultFetchTimeout)
	defer cancel()
	created, err := s.feeds.AddFeed(ctx, service.AddFeedInput{
		URL: request.URL, FolderID: request.FolderID, TitleOverride: request.TitleOverride,
	})
	if err != nil {
		s.serviceError(w, r, err)
		return
	}
	s.events.Publish("subscriptions.updated", map[string]string{"feed_id": created.ID})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) discoverFeeds(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if strings.TrimSpace(request.URL) == "" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "A URL is required.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), feedcore.DefaultFetchTimeout)
	defer cancel()
	items, err := s.feeds.Discover(ctx, request.URL)
	if err != nil {
		s.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getFeed(w http.ResponseWriter, r *http.Request) {
	item, err := storage.GetFeed(r.Context(), s.db, r.PathValue("feedID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateFeed(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FolderID               json.RawMessage `json:"folder_id"`
		TitleOverride          json.RawMessage `json:"title_override"`
		ViewMode               json.RawMessage `json:"view_mode"`
		RefreshPolicy          json.RawMessage `json:"refresh_policy"`
		RefreshIntervalMinutes json.RawMessage `json:"refresh_interval_minutes"`
		HideFromTimeline       json.RawMessage `json:"hide_from_timeline"`
		Position               json.RawMessage `json:"position"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	setFolder, folderID, err := optionalNullableString(request.FolderID)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_folder_id", "Invalid folder", err.Error())
		return
	}
	setTitle, titleOverride, err := optionalNullableString(request.TitleOverride)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_title_override", "Invalid title", err.Error())
		return
	}
	viewMode, err := optionalString(request.ViewMode)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_view_mode", "Invalid view mode", err.Error())
		return
	}
	if viewMode != nil {
		valid := map[string]bool{"compact": true, "standard": true, "card": true, "magazine": true, "image": true}
		if !valid[*viewMode] {
			writeProblem(w, r, http.StatusBadRequest, "invalid_view_mode", "Invalid view mode", "Choose compact, standard, card, magazine, or image.")
			return
		}
	}
	refreshPolicy, err := optionalString(request.RefreshPolicy)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_refresh_policy", "Invalid refresh policy", err.Error())
		return
	}
	if refreshPolicy != nil {
		valid := map[string]bool{"inherit": true, "fixed": true, "intelligent": true, "never": true}
		if !valid[*refreshPolicy] {
			writeProblem(w, r, http.StatusBadRequest, "invalid_refresh_policy", "Invalid refresh policy", "Choose inherit, fixed, intelligent, or never.")
			return
		}
	}
	refreshInterval, err := optionalInt(request.RefreshIntervalMinutes)
	if err != nil || (refreshInterval != nil && (*refreshInterval < 0 || *refreshInterval > 10080)) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_refresh_interval", "Invalid refresh interval", "Refresh interval must be from 0 to 10080 minutes.")
		return
	}
	hideFromTimeline, err := optionalBool(request.HideFromTimeline)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_visibility", "Invalid visibility", err.Error())
		return
	}
	position, err := optionalInt(request.Position)
	if err != nil || (position != nil && *position < 0) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_position", "Invalid position", "Position must be a non-negative integer.")
		return
	}
	updated, err := storage.UpdateSubscription(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("feedID"), domain.SubscriptionPatch{
		SetFolderID: setFolder, FolderID: folderID, SetTitleOverride: setTitle, TitleOverride: titleOverride,
		ViewMode: viewMode, RefreshPolicy: refreshPolicy, RefreshIntervalMinutes: refreshInterval, HideFromTimeline: hideFromTimeline, Position: position,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("subscriptions.updated", map[string]string{"feed_id": r.PathValue("feedID")})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteFeed(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteSubscription(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("feedID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("subscriptions.updated", map[string]string{"deleted_feed_id": r.PathValue("feedID")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) refreshFeed(w http.ResponseWriter, r *http.Request) {
	if _, err := storage.GetFeed(r.Context(), s.db, r.PathValue("feedID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	queued, err := s.jobs.EnqueueFeedRefresh(r.Context(), r.PathValue("feedID"))
	if err != nil {
		if errors.Is(err, job.ErrJobAlreadyQueued) {
			writeProblem(w, r, http.StatusConflict, "refresh_pending", "Refresh already pending", err.Error())
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, queued)
}

func (s *Server) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListSubscriptions(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListFolders(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listEntries(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_limit", "Invalid limit", "Limit must be between 1 and 100.")
			return
		}
		limit = parsed
	}
	state := r.URL.Query().Get("state")
	if state != "" && state != "all" && state != "unread" && state != "starred" && state != "read_later" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_state", "Invalid state filter", "State must be all, unread, starred, or read_later.")
		return
	}
	contentKind := r.URL.Query().Get("content_kind")
	switch contentKind {
	case "", domain.ContentKindGeneral, domain.ContentKindLiterature, domain.ContentKindVideo, domain.ContentKindSocial:
	default:
		writeProblem(w, r, http.StatusBadRequest, "invalid_content_kind", "Invalid content kind", "Content kind must be general, literature, video, or social.")
		return
	}
	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_since", "Invalid time boundary", err.Error())
		return
	}
	page, err := storage.ListEntries(r.Context(), s.db, domain.EntryFilter{
		ProfileID:   domain.DefaultProfileID,
		FeedID:      r.URL.Query().Get("feed_id"),
		FolderID:    r.URL.Query().Get("folder_id"),
		TagID:       r.URL.Query().Get("tag_id"),
		State:       state,
		Query:       r.URL.Query().Get("query"),
		ContentKind: contentKind,
		Cursor:      r.URL.Query().Get("cursor"),
		Limit:       limit,
		Since:       since,
		AILanguage:  r.URL.Query().Get("ai_language"),
	})
	if err != nil {
		if strings.Contains(err.Error(), "cursor") {
			writeProblem(w, r, http.StatusBadRequest, "invalid_cursor", "Invalid cursor", err.Error())
			return
		}
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) markEntriesRead(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FeedID      string     `json:"feed_id"`
		FolderID    string     `json:"folder_id"`
		TagID       string     `json:"tag_id"`
		State       string     `json:"state"`
		Query       string     `json:"query"`
		ContentKind string     `json:"content_kind"`
		Since       *time.Time `json:"since"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if request.State != "" && request.State != "all" && request.State != "unread" && request.State != "starred" && request.State != "read_later" {
		writeProblem(w, r, http.StatusBadRequest, "invalid_state", "Invalid state filter", "State must be all, unread, starred, or read_later.")
		return
	}
	count, err := storage.MarkEntriesRead(r.Context(), s.db, domain.EntryFilter{
		ProfileID: domain.DefaultProfileID, FeedID: request.FeedID, FolderID: request.FolderID,
		TagID: request.TagID, State: request.State, Query: request.Query, ContentKind: request.ContentKind, Since: request.Since,
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.events.Publish("entry.bulk_state", map[string]any{"is_read": true, "count": count})
	writeJSON(w, http.StatusOK, map[string]int64{"updated": count})
}

func (s *Server) getEntry(w http.ResponseWriter, r *http.Request) {
	item, err := storage.GetEntry(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"), r.URL.Query().Get("ai_language"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateEntryState(w http.ResponseWriter, r *http.Request) {
	var request struct {
		MutationID  string     `json:"mutation_id"`
		DeviceID    *string    `json:"device_id"`
		IsRead      *bool      `json:"is_read"`
		IsStarred   *bool      `json:"is_starred"`
		IsReadLater *bool      `json:"is_read_later"`
		DeviceTime  *time.Time `json:"device_time"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if request.MutationID == "" || (request.IsRead == nil && request.IsStarred == nil && request.IsReadLater == nil) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_state_patch", "Invalid state change", "A mutation_id and at least one state field are required.")
		return
	}
	var authenticatedDeviceID *string
	if device := deviceFromContext(r.Context()); device != nil {
		authenticatedDeviceID = &device.ID
		if request.DeviceID != nil && *request.DeviceID != device.ID {
			writeProblem(w, r, http.StatusConflict, "device_id_mismatch", "Device identity mismatch", "The body device_id does not match the authenticated device.")
			return
		}
	}
	state, err := storage.UpdateEntryState(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"), domain.EntryStatePatch{
		MutationID: request.MutationID, DeviceID: authenticatedDeviceID, IsRead: request.IsRead,
		IsStarred: request.IsStarred, IsReadLater: request.IsReadLater,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("entry.state", map[string]any{"entry_id": r.PathValue("entryID"), "state": state})
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) listPreferences(w http.ResponseWriter, r *http.Request) {
	items, err := storage.ListPreferences(r.Context(), s.db, domain.DefaultProfileID)
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) putPreference(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !storage.IsPreferenceKey(key) {
		writeProblem(w, r, http.StatusBadRequest, "unknown_preference", "Unknown preference", "This preference key is not supported.")
		return
	}
	var value json.RawMessage
	if err := decodeJSON(w, r, &value); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if key == storage.PreferenceRetentionDays {
		var days int
		if err := json.Unmarshal(value, &days); err != nil || days < 0 || days > 3650 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_preference", "Invalid preference", "retention_days must be an integer between 0 (keep forever) and 3650.")
			return
		}
	}
	if err := storage.SetPreference(r.Context(), s.db, domain.DefaultProfileID, key, value); err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]json.RawMessage{key: value})
}

func (s *Server) listEntryAnnotations(w http.ResponseWriter, r *http.Request) {
	annotations, err := storage.ListEntryAnnotations(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": annotations})
}

func (s *Server) createEntryAnnotation(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Style  string `json:"style"`
		Quote  string `json:"quote"`
		Prefix string `json:"prefix"`
		Suffix string `json:"suffix"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	if request.Style == "" {
		request.Style = "highlight"
	}
	annotation, err := storage.CreateEntryAnnotation(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"), domain.EntryAnnotation{
		Style: request.Style, Quote: request.Quote, Prefix: request.Prefix, Suffix: request.Suffix, Note: request.Note,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("entry.annotations", map[string]any{"entry_id": r.PathValue("entryID")})
	writeJSON(w, http.StatusCreated, annotation)
}

func (s *Server) updateEntryAnnotation(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Style *string `json:"style"`
		Note  *string `json:"note"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_request", "Invalid request")
		return
	}
	annotation, err := storage.UpdateEntryAnnotation(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"), r.PathValue("annotationID"), domain.EntryAnnotationPatch{
		Style: request.Style, Note: request.Note,
	})
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("entry.annotations", map[string]any{"entry_id": r.PathValue("entryID")})
	writeJSON(w, http.StatusOK, annotation)
}

func (s *Server) deleteEntryAnnotation(w http.ResponseWriter, r *http.Request) {
	if err := storage.DeleteEntryAnnotation(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID"), r.PathValue("annotationID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	s.events.Publish("entry.annotations", map[string]any{"entry_id": r.PathValue("entryID")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) fetchReadability(w http.ResponseWriter, r *http.Request) {
	if _, err := storage.GetEntry(r.Context(), s.db, domain.DefaultProfileID, r.PathValue("entryID")); err != nil {
		s.storageError(w, r, err)
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "entry.readability", map[string]string{"entry_id": r.PathValue("entryID")})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, queued)
}

func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	// OPML documents are user exports and can legitimately exceed the request
	// limit used by small JSON mutations. Keep this endpoint independent so
	// large libraries can be imported without an arbitrary size ceiling.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxOPMLBodyBytes+1))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_opml", "Invalid OPML", "The OPML document could not be read.")
		return
	}
	if len(body) > maxOPMLBodyBytes {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "Request body too large", "The OPML document exceeds the 16 MiB limit.")
		return
	}
	if len(body) == 0 {
		writeProblem(w, r, http.StatusBadRequest, "invalid_opml", "Invalid OPML", "Provide a non-empty OPML document.")
		return
	}
	if _, err := opml.Parse(body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_opml", "Invalid OPML", err.Error())
		return
	}
	queued, err := s.jobs.Enqueue(r.Context(), "opml.import", map[string]string{"data": string(body)})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, queued)
}

func (s *Server) exportOPML(w http.ResponseWriter, r *http.Request) {
	body, err := s.feeds.ExportOPML(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="reflow-subscriptions.opml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	item, err := storage.GetJob(r.Context(), s.db, r.PathValue("jobID"))
	if err != nil {
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	item, err := s.jobs.Cancel(r.Context(), r.PathValue("jobID"))
	if err != nil {
		if errors.Is(err, storage.ErrJobNotCancellable) {
			writeProblem(w, r, http.StatusConflict, "job_not_cancellable", "Job cannot be cancelled", "Only queued or running jobs can be cancelled.")
			return
		}
		s.storageError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, "stream_unsupported", "Streaming unavailable", "The HTTP server does not support streaming.")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	events, unsubscribe := s.events.Subscribe()
	defer unsubscribe()
	_, _ = io.WriteString(w, "event: connected\ndata: {\"status\":\"ready\"}\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case message, open := <-events:
			if !open {
				return
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", message.ID, message.Type, message.Data)
			flusher.Flush()
		}
	}
}

func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	document, err := storage.ExportBackup(r.Context(), s.db)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="reflow-backup.json"`)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(document)
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var document storage.BackupDocument
	if err := decodeJSONLimit(w, r, &document, maxBackupBodyBytes, false); err != nil {
		writeJSONDecodeError(w, r, err, "invalid_backup", "Invalid backup")
		return
	}
	// Quiesce the job manager so in-flight refreshes and AI tasks do not write
	// into tables being replaced.
	maintenanceCtx, cancelMaintenance := context.WithTimeout(r.Context(), 30*time.Second)
	err := s.jobs.EnterMaintenance(maintenanceCtx, "")
	cancelMaintenance()
	if err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "restore_busy", "Backup restore could not start", "Background work did not stop before the restore deadline.")
		return
	}
	defer s.jobs.ExitMaintenance()
	if err := storage.RestoreBackup(r.Context(), s.db, document); err != nil {
		writeProblem(w, r, http.StatusUnprocessableEntity, "restore_failed", "Backup could not be restored", err.Error())
		return
	}
	s.events.Publish("library.restored", map[string]any{"created_at": document.CreatedAt})
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

const (
	maxJSONBodyBytes   = 1 << 20
	maxOPMLBodyBytes   = 16 << 20
	maxBackupBodyBytes = 512 << 20
)

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return decodeJSONLimit(w, r, target, maxJSONBodyBytes, true)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64, disallowUnknown bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSONDecodeError(w http.ResponseWriter, r *http.Request, err error, invalidCode, invalidTitle string) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "Request body too large", "The request body exceeds the limit for this endpoint.")
		return
	}
	writeProblem(w, r, http.StatusBadRequest, invalidCode, invalidTitle, err.Error())
}

func optionalNullableString(raw json.RawMessage) (bool, *string, error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	if string(raw) == "null" {
		return true, nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return true, nil, errors.New("value must be a string or null")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return true, nil, nil
	}
	return true, &value, nil
}

func optionalString(raw json.RawMessage) (*string, error) {
	present, value, err := optionalNullableString(raw)
	if err != nil || !present {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("value cannot be null")
	}
	return value, nil
}

func optionalInt(raw json.RawMessage) (*int, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("value must be an integer")
	}
	return &value, nil
}

func optionalBool(raw json.RawMessage) (*bool, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, errors.New("value must be a boolean")
	}
	return &value, nil
}

func parseSince(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errors.New("since must be an RFC3339 timestamp")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func (s *Server) storageError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		writeProblem(w, r, http.StatusNotFound, "not_found", "Resource not found", "The requested resource does not exist.")
		return
	}
	var annotationValidation *storage.AnnotationValidationError
	if errors.As(err, &annotationValidation) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_annotation", "Invalid annotation", annotationValidation.Reason)
		return
	}
	var researchValidation *storage.ResearchValidationError
	if errors.As(err, &researchValidation) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_research_paper", "Invalid research paper", researchValidation.Reason)
		return
	}
	if errors.Is(err, storage.ErrDuplicateResearchTag) {
		writeProblem(w, r, http.StatusConflict, "duplicate_research_tag", "Duplicate tag", "A tag with that name already exists in this profile.")
		return
	}
	var tagValidation *storage.ResearchTagValidationError
	if errors.As(err, &tagValidation) {
		writeProblem(w, r, http.StatusBadRequest, "invalid_research_tag", "Invalid research tag", tagValidation.Reason)
		return
	}
	s.internalError(w, r, err)
}

func (s *Server) serviceError(w http.ResponseWriter, r *http.Request, err error) {
	code := "feed_error"
	status := http.StatusUnprocessableEntity
	var fetchError *feedcore.FetchError
	if errors.As(err, &fetchError) {
		code = fetchError.Code
	}
	if errors.Is(err, feedcore.ErrBlockedAddress) || errors.Is(err, feedcore.ErrInvalidURL) {
		code = "blocked_address"
		if errors.Is(err, feedcore.ErrInvalidURL) {
			code = "invalid_url"
		}
		status = http.StatusBadRequest
	}
	writeProblem(w, r, status, code, "Feed could not be processed", err.Error())
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.ErrorContext(r.Context(), "API operation failed", "error", err, "request_id", requestIDFrom(r.Context()))
	writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "The server could not complete the request.")
}
