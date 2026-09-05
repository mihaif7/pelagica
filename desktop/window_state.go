package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	windowStateFileName = "window-state.json"
	// Long enough to coalesce a drag-resize or drag-move into a single write, short
	// enough that the new geometry survives a force quit moments later.
	windowStateSaveDelay = time.Second
	// A restored window has to show at least this much of itself on a connected
	// display, otherwise it is recentred. Without this, quitting on an external
	// monitor and relaunching without it leaves the window off-screen.
	minVisibleWidth  = 200
	minVisibleHeight = 100
	// Reported bounds are truncated from the platform's floating-point frame, so a
	// window restored to a saved size can measure a pixel smaller than it was asked
	// for. Writing that back would shrink the window by a pixel on every launch, so
	// a difference this small is not treated as a change the user made.
	windowStateEpsilon = 1
)

// windowState is the geometry carried between launches. The bounds are the window's
// *normal* bounds: they are not updated while it is maximised, fullscreen or
// minimised, so un-maximising still restores a sensible size.
type windowState struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

// windowStateTracker follows the window's geometry and writes it to disk. Wails has
// no built-in window state persistence, so this is assembled from the resize/move/
// maximise events plus a debounced write.
type windowStateTracker struct {
	window *application.WebviewWindow

	mu    sync.Mutex
	state windowState
	// Bounds reported while the window is in any of these states describe the state,
	// not the size the user picked, so they are not recorded.
	maximised  bool
	fullscreen bool
	minimised  bool
	dirty      bool
	saveTimer  *time.Timer
}

// applySavedState seeds the window options from the previous session and primes the
// tracker with what was restored, so the first measurement is compared against it. A
// window restored off-screen is corrected once shown; see ensureWindowOnScreen.
func (t *windowStateTracker) applySavedState(options *application.WebviewWindowOptions) {
	saved, ok := loadWindowState()
	if !ok {
		return
	}
	t.state = saved
	options.Width = saved.Width
	options.Height = saved.Height
	options.X = saved.X
	options.Y = saved.Y
	options.InitialPosition = application.WindowXY
	if saved.Maximised {
		options.StartState = application.WindowStateMaximised
	}
}

// registerHooks wires the tracker to the window. The window must already be created.
func (t *windowStateTracker) registerHooks(window *application.WebviewWindow) {
	t.window = window

	window.RegisterHook(events.Common.WindowDidResize, func(*application.WindowEvent) { t.record() })
	window.RegisterHook(events.Common.WindowDidMove, func(*application.WindowEvent) { t.record() })

	// The window state flags are tracked from events rather than queried per resize,
	// since every getter is a synchronous hop onto the main thread.
	window.RegisterHook(events.Common.WindowMaximise, func(*application.WindowEvent) { t.setMaximised(true) })
	window.RegisterHook(events.Common.WindowUnMaximise, func(*application.WindowEvent) { t.setMaximised(false) })
	window.RegisterHook(events.Common.WindowFullscreen, func(*application.WindowEvent) { t.setFlag(&t.fullscreen, true) })
	window.RegisterHook(events.Common.WindowUnFullscreen, func(*application.WindowEvent) { t.setFlag(&t.fullscreen, false) })
	window.RegisterHook(events.Common.WindowMinimise, func(*application.WindowEvent) { t.setFlag(&t.minimised, true) })
	window.RegisterHook(events.Common.WindowUnMinimise, func(*application.WindowEvent) { t.setFlag(&t.minimised, false) })
}

// record captures the window's current bounds, unless it is in a state whose bounds
// should not be remembered.
func (t *windowStateTracker) record() {
	t.mu.Lock()
	skip := t.window == nil || t.maximised || t.fullscreen || t.minimised
	t.mu.Unlock()
	if skip {
		return
	}

	// Bounds() hops onto the main thread, so it must not be called under the lock.
	bounds := t.window.Bounds()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if withinEpsilon(bounds, t.state) {
		return
	}
	t.state.X = bounds.X
	t.state.Y = bounds.Y
	t.state.Width = bounds.Width
	t.state.Height = bounds.Height
	t.scheduleSaveLocked()
}

// withinEpsilon reports whether measured bounds are close enough to the stored state
// to be the same geometry, read back with a different rounding.
func withinEpsilon(bounds application.Rect, state windowState) bool {
	return abs(bounds.X-state.X) <= windowStateEpsilon &&
		abs(bounds.Y-state.Y) <= windowStateEpsilon &&
		abs(bounds.Width-state.Width) <= windowStateEpsilon &&
		abs(bounds.Height-state.Height) <= windowStateEpsilon
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (t *windowStateTracker) setMaximised(maximised bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.maximised = maximised
	t.state.Maximised = maximised
	t.scheduleSaveLocked()
}

func (t *windowStateTracker) setFlag(flag *bool, value bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*flag = value
}

// scheduleSaveLocked debounces the write. Must be called with the lock held.
func (t *windowStateTracker) scheduleSaveLocked() {
	t.dirty = true
	if t.saveTimer != nil {
		t.saveTimer.Stop()
	}
	t.saveTimer = time.AfterFunc(windowStateSaveDelay, t.flush)
}

// flush writes any pending state immediately. Used as the application's OnShutdown
// handler, so it only touches the in-memory copy - the window may already be gone.
func (t *windowStateTracker) flush() {
	t.mu.Lock()
	if t.saveTimer != nil {
		t.saveTimer.Stop()
		t.saveTimer = nil
	}
	if !t.dirty {
		t.mu.Unlock()
		return
	}
	t.dirty = false
	state := t.state
	t.mu.Unlock()

	saveWindowState(state)
}

// ensureWindowOnScreen recentres the window if its restored geometry does not leave a
// usable part of it on any connected display.
func ensureWindowOnScreen(app *application.App, window *application.WebviewWindow) {
	bounds := window.Bounds()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	for _, screen := range app.Screen.GetAll() {
		if isUsablyVisible(bounds, screen.WorkArea) {
			return
		}
	}
	window.Center()
}

// isUsablyVisible reports whether enough of bounds overlaps area for the user to grab
// the window back.
func isUsablyVisible(bounds, area application.Rect) bool {
	overlapWidth := min(bounds.X+bounds.Width, area.X+area.Width) - max(bounds.X, area.X)
	overlapHeight := min(bounds.Y+bounds.Height, area.Y+area.Height) - max(bounds.Y, area.Y)
	return overlapWidth >= minVisibleWidth && overlapHeight >= minVisibleHeight
}

func loadWindowState() (windowState, bool) {
	path, err := configFilePath(windowStateFileName)
	if err != nil {
		return windowState{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return windowState{}, false
	}
	var state windowState
	if err := json.Unmarshal(data, &state); err != nil {
		return windowState{}, false
	}
	if state.Width < windowMinWidth || state.Height < windowMinHeight {
		return windowState{}, false
	}
	return state, true
}

func saveWindowState(state windowState) {
	path, err := configFilePath(windowStateFileName)
	if err != nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
