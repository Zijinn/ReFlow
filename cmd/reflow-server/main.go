package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Zijinn/ReFlow/internal/config"
	"github.com/Zijinn/ReFlow/internal/httpapi"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/Zijinn/ReFlow/internal/version"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fatal("load configuration", err)
	}
	logger := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := storage.Open(ctx, cfg.DBPath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	box, err := secretbox.LoadOrCreate(cfg.MasterKeyPath)
	if err != nil {
		logger.Error("load credential master key", "error", err)
		os.Exit(1)
	}

	api := httpapi.New(db, logger, cfg.WebDir)
	api.ConfigureSync(box)
	api.ConfigureAI(box)
	api.SetRSSHubBase(cfg.RSSHubBase)
	api.ConfigureSecurity(cfg.LANMode, cfg.AllowedOrigins, cfg.TrustedProxies)
	if err := api.Start(ctx); err != nil {
		logger.Error("start application services", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      310 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("ReFlow server started", "address", cfg.Address, "version", version.Version, "lan_mode", cfg.LANMode)
		var serveErr error
		if cfg.TLSCertPath != "" {
			serveErr = server.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath)
		} else {
			serveErr = server.ListenAndServe()
		}
		if err := serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errCh:
		logger.Error("server stopped", "error", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func newLogger(levelName string) *slog.Logger {
	level := slog.LevelInfo
	switch levelName {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func fatal(message string, err error) {
	slog.Error(message, "error", err)
	os.Exit(1)
}
