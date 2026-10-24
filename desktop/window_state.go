package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	windowStateVersion         = 1
	defaultWindowStateDebounce = 250 * time.Millisecond
)

// WindowStateOptions controls optional, persisted window placement.
type WindowStateOptions struct {
	// Empty uses the per-application GoSX user config directory.
	Path string
	// Zero selects the default 250 ms debounce; negative values are invalid.
	Debounce time.Duration
	// OnError receives best-effort load and save errors.
	OnError func(error)
}

type windowStateFile struct {
	Version   int             `json:"version"`
	Placement WindowPlacement `json:"placement"`
}

// LoadWindowState reads a supported persisted window placement.
func LoadWindowState(path string) (WindowPlacement, error) {
	f, err := os.Open(path)
	if err != nil {
		return WindowPlacement{}, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var state windowStateFile
	if err := dec.Decode(&state); err != nil {
		return WindowPlacement{}, fmt.Errorf("desktop: decode window state: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return WindowPlacement{}, errors.New("desktop: trailing JSON in window state")
		}
		return WindowPlacement{}, fmt.Errorf("desktop: trailing data in window state: %w", err)
	}
	if state.Version != windowStateVersion {
		return WindowPlacement{}, fmt.Errorf("desktop: unsupported window state version %d", state.Version)
	}
	if state.Placement.Width < 200 || state.Placement.Height < 200 {
		return WindowPlacement{}, errors.New("desktop: window state placement is smaller than 200 pixels")
	}
	return state.Placement, nil
}

// SaveWindowState atomically replaces path with a versioned placement file.
func SaveWindowState(path string, placement WindowPlacement) error {
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%w: window state path contains NUL", ErrInvalidOptions)
	}
	if path == "" {
		return errors.New("desktop: window state path is empty")
	}
	if placement.Width < 200 || placement.Height < 200 {
		return errors.New("desktop: refusing to save an undersized window placement")
	}
	data, err := json.Marshal(windowStateFile{Version: windowStateVersion, Placement: placement})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".window-state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

func defaultWindowStatePath(appID string) (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "GoSX", url.PathEscape(appID), "window-state.json"), nil
}

func resolveWindowStatePath(path, appID string) (string, error) {
	if path != "" {
		return path, nil
	}
	return defaultWindowStatePath(appID)
}

type windowStatePersister struct {
	path            string
	debounce        time.Duration
	onError         func(error)
	mu              sync.Mutex
	saveMu          sync.Mutex
	timer           *time.Timer
	pending         WindowPlacement
	hasPending      bool
	timerGeneration uint64
	stopped         bool
}

func newWindowStatePersister(path string, debounce time.Duration, onError func(error)) *windowStatePersister {
	if debounce == 0 {
		debounce = defaultWindowStateDebounce
	}
	return &windowStatePersister{path: path, debounce: debounce, onError: onError}
}

func validWindowPlacement(p WindowPlacement) bool {
	return !p.IsZero() && p.Width >= 200 && p.Height >= 200
}

func (p *windowStatePersister) schedule(placement WindowPlacement) {
	if !validWindowPlacement(placement) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.pending, p.hasPending = placement, true
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timerGeneration++
	generation := p.timerGeneration
	p.timer = time.AfterFunc(p.debounce, func() { p.persistPending(generation) })
}

func (p *windowStatePersister) persistPending(generation uint64) {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.mu.Lock()
	if p.stopped || generation != p.timerGeneration || !p.hasPending {
		p.mu.Unlock()
		return
	}
	placement := p.pending
	p.hasPending = false
	p.timer = nil
	p.mu.Unlock()
	if err := SaveWindowState(p.path, placement); err != nil && p.onError != nil {
		p.onError(err)
	}
}

func (p *windowStatePersister) flush(closePlacement WindowPlacement) error {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.mu.Lock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	placement := closePlacement
	if !validWindowPlacement(placement) && p.hasPending {
		placement = p.pending
	}
	p.timerGeneration++
	p.pending, p.hasPending = WindowPlacement{}, false
	p.mu.Unlock()
	if !validWindowPlacement(placement) {
		return nil
	}
	err := SaveWindowState(p.path, placement)
	if err != nil && p.onError != nil {
		p.onError(err)
	}
	return err
}

func (p *windowStatePersister) stop() {
	p.mu.Lock()
	p.stopped = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.timerGeneration++
	p.pending, p.hasPending = WindowPlacement{}, false
	p.mu.Unlock()
}
