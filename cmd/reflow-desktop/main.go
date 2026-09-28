//go:build desktop

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Zijinn/ReFlow/internal/config"
	"github.com/Zijinn/ReFlow/internal/httpapi"
	"github.com/Zijinn/ReFlow/internal/secretbox"
	"github.com/Zijinn/ReFlow/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	db, err := storage.Open(context.Background(), cfg.DBPath)
	if err != nil {
		slog.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	box, err := secretbox.LoadOrCreate(cfg.MasterKeyPath)
	if err != nil {
		slog.Error("load credential master key", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler := httpapi.New(db, logger, desktopWebDir(cfg.WebDir))
	handler.ConfigureSync(box)
	handler.ConfigureAI(box)
	handler.SetRSSHubBase(cfg.RSSHubBase)
	handler.ConfigureSecurity(cfg.LANMode, cfg.AllowedOrigins, cfg.TrustedProxies)
	appContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := handler.Start(appContext); err != nil {
		logger.Error("start application services", "error", err)
		os.Exit(1)
	}
	app := application.New(application.Options{
		Name:        "ReFlow",
		Description: "A private reading home for the open web",
		LogLevel:    slog.LevelError,
		Assets: application.AssetOptions{
			Handler: handler.Handler(),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		SingleInstance: func() *application.SingleInstanceOptions {
			if runtime.GOOS == "linux" {
				return nil
			}
			return &application.SingleInstanceOptions{UniqueID: "app.reflow.reader"}
		}(),
	})

	windowWidth, windowHeight := 1280, 820
	const minWindowWidth, minWindowHeight = 920, 620
	restoredSize, hasRestoredSize := loadWindowState(cfg.DataDir, minWindowWidth, minWindowHeight)
	if hasRestoredSize {
		windowWidth, windowHeight = restoredSize.Width, restoredSize.Height
	}
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "reflow-main-window",
		Title:            "ReFlow",
		Width:            windowWidth,
		Height:           windowHeight,
		MinWidth:         minWindowWidth,
		MinHeight:        minWindowHeight,
		URL:              "/",
		Frameless:        runtime.GOOS == "windows",
		BackgroundColour: application.NewRGB(255, 255, 255),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHidden,
		},
		Windows: application.WindowsWindow{
			DisableIcon:            true,
			NonClientRegionSupport: true,
			Theme:                  application.SystemDefault,
			CustomTheme: application.ThemeSettings{
				DarkModeActive: &application.WindowTheme{
					BorderColour:    application.NewRGBPtr(45, 47, 52),
					TitleBarColour:  application.NewRGBPtr(13, 14, 16),
					TitleTextColour: application.NewRGBPtr(241, 242, 244),
				},
				DarkModeInactive: &application.WindowTheme{
					BorderColour:    application.NewRGBPtr(34, 36, 40),
					TitleBarColour:  application.NewRGBPtr(13, 14, 16),
					TitleTextColour: application.NewRGBPtr(154, 157, 165),
				},
				LightModeActive: &application.WindowTheme{
					BorderColour:    application.NewRGBPtr(218, 221, 226),
					TitleBarColour:  application.NewRGBPtr(255, 255, 255),
					TitleTextColour: application.NewRGBPtr(24, 25, 28),
				},
				LightModeInactive: &application.WindowTheme{
					BorderColour:    application.NewRGBPtr(232, 234, 238),
					TitleBarColour:  application.NewRGBPtr(255, 255, 255),
					TitleTextColour: application.NewRGBPtr(104, 107, 114),
				},
			},
		},
	})
	window.Center()
	persistWindowSizeOnResize(window, cfg.DataDir)

	if err := app.Run(); err != nil {
		logger.Error("run desktop application", "error", err)
		os.Exit(1)
	}
}

func desktopWebDir(configured string) string {
	if filepath.IsAbs(configured) {
		return configured
	}
	executable, err := os.Executable()
	if err != nil {
		return configured
	}
	base := filepath.Dir(executable)
	candidates := []string{
		filepath.Join(base, configured),
		filepath.Join(base, "..", "Resources", configured),
	}
	for _, candidate := range candidates {
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			resolved, resolveErr := filepath.Abs(candidate)
			if resolveErr == nil {
				return resolved
			}
		}
	}
	return configured
}
