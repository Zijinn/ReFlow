//go:build desktop

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The window size is the user's workspace layout: restore it on launch and
// persist it (debounced) whenever the window is resized, so a drag survives
// restarts. Position stays managed by the platform to avoid restoring
// off-screen coordinates after a monitor change.
type windowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func windowStatePath(dataDir string) string {
	return filepath.Join(dataDir, "window.json")
}

func loadWindowState(dataDir string, minWidth, minHeight int) (windowState, bool) {
	raw, err := os.ReadFile(windowStatePath(dataDir))
	if err != nil {
		return windowState{}, false
	}
	var state windowState
	if err := json.Unmarshal(raw, &state); err != nil {
		return windowState{}, false
	}
	if state.Width < minWidth || state.Height < minHeight {
		return windowState{}, false
	}
	return state, true
}

// persistWindowSizeOnResize writes the window size after the user stops
// dragging (WindowDidResize fires continuously during the drag).
func persistWindowSizeOnResize(window *application.WebviewWindow, dataDir string) {
	var mu sync.Mutex
	var timer *time.Timer
	window.OnWindowEvent(events.Common.WindowDidResize, func(_ *application.WindowEvent) {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(400*time.Millisecond, func() {
			width, height := window.Size()
			if width <= 0 || height <= 0 {
				return
			}
			raw, err := json.Marshal(windowState{Width: width, Height: height})
			if err != nil {
				return
			}
			// Write via a temp file so a crash mid-write cannot corrupt the state.
			path := windowStatePath(dataDir)
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, raw, 0o600); err != nil {
				return
			}
			_ = os.Rename(tmp, path)
		})
	})
}
