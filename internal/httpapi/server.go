package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zijinn/ReFlow/internal/domain"
	"github.com/Zijinn/ReFlow/internal/event"
	feedcore "github.com/Zijinn/ReFlow/internal/feed"
	"github.com/Zijinn/ReFlow/internal/job"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/service"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/Zijinn/ReFlow/internal/version"
)

type Server struct {
	db       *sql.DB
	logger   *slog.Logger
	webDir   string
	feeds    *service.FeedService
	syncs    *service.SyncService
	ai       *service.AIService
	zotero   *service.ZoteroService
	research *service.ResearchService
	jobs     *job.Manager
	events   *event.Hub
	security SecurityConfig
	handler  http.Handler
}

func New(db *sql.DB, logger *slog.Logger, webDir string) *Server {
	return NewWithFetcher(db, logger, webDir, nil)
}

func NewWithFetcher(db *sql.DB, logger *slog.Logger, webDir string, fetcher *feedcore.Fetcher) *Server {
	hub := event.NewHub()
	feedService := service.NewFeedService(db, fetcher)
	manager := job.NewManager(db, hub, logger, 4)
	s := &Server{
		db: db, logger: logger, webDir: webDir, feeds: feedService,
		zotero: service.NewZoteroService(db, fetcher), research: service.NewResearchService(db),
		jobs: manager, events: hub,
	}
	manager.Register("feed.refresh", func(ctx context.Context, current domain.Job, progress job.ProgressFunc) error {
		var payload struct {
			FeedID string `json:"feed_id"`
		}
		if err := job.DecodePayload(current, &payload); err != nil {
			return err
		}
		if payload.FeedID == "" {
			return errors.New("feed ID is required")
		}
		progress(0, 1)
		_, err := feedService.RefreshFeed(ctx, payload.FeedID)
		if err == nil {
			progress(1, 1)
			hub.Publish("feed.updated", map[string]string{"id": payload.FeedID})
		}
		return err
	})
	manager.Register("opml.import", func(ctx context.Context, current domain.Job, progress job.ProgressFunc) error {
		var payload struct {
			Data string `json:"data"`
		}
		if err := job.DecodePayload(current, &payload); err != nil {
			return err
		}
		_, err := feedService.ImportOPML(ctx, []byte(payload.Data), service.ImportProgress(progress))
		if err == nil {
			hub.Publish("subscriptions.updated", map[string]string{"source": "opml"})
		}
		return err
	})
	manager.Register("entry.readability", func(ctx context.Context, current domain.Job, progress job.ProgressFunc) error {
		var payload struct {
			EntryID string `json:"entry_id"`
		}
		if err := job.DecodePayload(current, &payload); err != nil {
			return err
		}
		if payload.EntryID == "" {
			return errors.New("entry ID is required")
		}
		progress(0, 1)
		if err := feedService.FetchReadability(ctx, payload.EntryID); err != nil {
			return err
		}
		progress(1, 1)
		hub.Publish("entry.updated", map[string]string{"id": payload.EntryID})
		return nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/status", s.status)
	s.registerRoutes(mux)
	mux.HandleFunc("GET /api/v1/{$}", s.apiRoot)
	mux.HandleFunc("/api/", s.notFound)
	mux.HandleFunc("/", s.web)
	s.handler = s.requestID(s.cors(s.authenticate(s.recoverPanic(s.securityHeaders(s.requestLog(mux))))))
	return s
}

func (s *Server) Start(ctx context.Context) error {
	if s.security.RequireDeviceAuth || len(s.security.TrustedProxies) > 0 {
		code, expiresAt, err := storage.CreatePairingCode(ctx, s.db, 10*time.Minute)
		if err != nil {
			return err
		}
		s.logger.Warn("remote access pairing code", "code", code, "expires_at", expiresAt)
	}
	if err := s.jobs.Start(ctx); err != nil {
		return err
	}
	s.jobs.StartFeedScheduler(ctx, time.Minute)
	s.jobs.StartCleanupScheduler(ctx, 24*time.Hour)
	if s.syncs != nil {
		s.jobs.StartSyncScheduler(ctx, time.Minute)
	}
	return nil
}

func (s *Server) ConfigureSync(box *secretbox.Box) {
	syncService := service.NewSyncService(s.db, s.feeds, box)
	syncService.SetMaintenance(s.jobs)
	s.syncs = syncService
	s.jobs.Register("sync.account", func(ctx context.Context, current domain.Job, progress job.ProgressFunc) error {
		var payload struct {
			AccountID string `json:"account_id"`
			Mode      string `json:"mode"`
		}
		if err := job.DecodePayload(current, &payload); err != nil {
			return err
		}
		if payload.AccountID == "" {
			return errors.New("sync account ID is required")
		}
		result, err := syncService.Run(service.WithSyncJobID(ctx, current.ID), payload.AccountID, service.SyncProgressFunc(progress), payload.Mode)
		if err == nil {
			s.events.Publish("sync.completed", map[string]any{"account_id": payload.AccountID, "result": result})
			if result.Action == "pull" {
				s.events.Publish("library.restored", map[string]any{"account_id": payload.AccountID, "source": "cloud_sync"})
			}
		}
		return err
	})
}

func (s *Server) ConfigureAI(box *secretbox.Box) {
	aiService := service.NewAIService(s.db, box)
	s.ai = aiService
	s.jobs.Register("ai.operation", func(ctx context.Context, current domain.Job, progress job.ProgressFunc) error {
		var payload service.AIOperationPayload
		if err := job.DecodePayload(current, &payload); err != nil {
			return err
		}
		progress(0, 1)
		result, err := aiService.RunOperation(ctx, current.ID, payload)
		if err == nil {
			progress(1, 1)
			s.events.Publish("ai.result", map[string]any{"entry_id": payload.EntryID, "result": result})
		}
		return err
	})
	s.jobs.Register("ai.chat", func(ctx context.Context, current domain.Job, progress job.ProgressFunc) error {
		var payload service.AIChatPayload
		if err := job.DecodePayload(current, &payload); err != nil {
			return err
		}
		progress(0, 1)
		session, err := aiService.RunChat(ctx, current.ID, payload)
		if err == nil {
			progress(1, 1)
			s.events.Publish("ai.chat", map[string]any{"entry_id": payload.EntryID, "session_id": session.ID})
		}
		return err
	})
}

func (s *Server) ConfigureSecurity(requireDeviceAuth bool, allowedOrigins []string, trustedProxies []netip.Prefix) {
	s.security = SecurityConfig{
		RequireDeviceAuth: requireDeviceAuth,
		AllowedOrigins:    append([]string(nil), allowedOrigins...),
		TrustedProxies:    append([]netip.Prefix(nil), trustedProxies...),
	}
}

func (s *Server) SetRSSHubBase(base string) {
	s.feeds.SetRSSHubBase(base)
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	ready := storage.Ready(ctx, s.db) == nil
	state := "ready"
	if !ready {
		state = "degraded"
	}
	authRequired := s.deviceAuthRequired(s.classifyIngress(r.RemoteAddr))
	deviceAuthenticated := !authRequired
	if authRequired {
		if _, err := storage.AuthenticateDevice(r.Context(), s.db, deviceTokenFromRequest(r)); err == nil {
			deviceAuthenticated = true
		}
	}
	capabilities := []string{"sqlite", "migrations", "pwa", "rss", "atom", "json_feed", "opml", "sse", "fts5"}
	if s.syncs != nil {
		capabilities = append(capabilities, "external_sync", "webdav_sync", "icloud_sync")
	}
	if s.ai != nil {
		capabilities = append(capabilities, "ai")
	}
	capabilities = append(capabilities, "zotero_connector", "research_workspace")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":               state,
		"version":              version.Version,
		"api_version":          version.APIVersion,
		"database_ready":       ready,
		"capabilities":         capabilities,
		"device_auth_required": authRequired,
		"device_authenticated": deviceAuthenticated,
	})
}

func (s *Server) apiRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":        "ReFlow API",
		"api_version": version.APIVersion,
	})
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusNotFound, "route_not_found", "Route not found", "The requested API route does not exist.")
}

func (s *Server) web(w http.ResponseWriter, r *http.Request) {
	if s.webDir == "" {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.Abs(s.webDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stat, err := os.Stat(root)
	if err != nil || !stat.IsDir() {
		http.NotFound(w, r)
		return
	}

	rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if rel == "" {
		rel = "index.html"
	}
	requested := filepath.Join(root, filepath.FromSlash(rel))
	if !strings.HasPrefix(requested, root+string(filepath.Separator)) && requested != root {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(requested)
	if err != nil || info.IsDir() {
		requested = filepath.Join(root, "index.html")
	}
	if contentType := mime.TypeByExtension(filepath.Ext(requested)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeFile(w, r, requested)
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > 128 || !validRequestID(requestID) {
			var bytes [16]byte
			if _, err := rand.Read(bytes[:]); err != nil {
				requestID = time.Now().UTC().Format("20060102150405.000000000")
			} else {
				requestID = hex.EncodeToString(bytes[:])
			}
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(value string) bool {
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.InfoContext(r.Context(), "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// WKWebView loads the embedded frontend through the wails:// scheme. A
		// browser-oriented 'self' CSP blocks those module resources before they
		// reach this server, so retain CSP for HTTP/PWA clients only.
		if r.Header.Get("X-Wails-Window-ID") == "" {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https:; media-src 'self' https:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-src https:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(r.Context(), "panic recovered", "request_id", requestIDFrom(r.Context()))
				writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "The server could not complete the request.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type requestIDKey struct{}

func requestIDFrom(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":       "https://reflow.local/problems/" + code,
		"title":      title,
		"status":     status,
		"detail":     detail,
		"code":       code,
		"request_id": requestIDFrom(r.Context()),
	})
}
