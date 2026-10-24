package desktop

import (
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWindowStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "window-state.json")
	want := WindowPlacement{X: -1280, Y: 120, Width: 960, Height: 720, Maximized: true, Monitor: `\\.\DISPLAY2`}
	if err := SaveWindowState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWindowState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("placement = %+v, want %+v", got, want)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("temporary files remain: %v", entries)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestWindowStateRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadWindowState(filepath.Join(dir, "missing.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing error = %v", err)
	}
	for name, contents := range map[string]string{
		"malformed":         `{`,
		"trailing":          `{"version":1,"placement":{"x":1,"y":2,"width":300,"height":300}} {}`,
		"version":           `{"version":2,"placement":{"x":1,"y":2,"width":300,"height":300}}`,
		"undersized-width":  `{"version":1,"placement":{"x":1,"y":2,"width":199,"height":300}}`,
		"undersized-height": `{"version":1,"placement":{"x":1,"y":2,"width":300,"height":199}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadWindowState(path); err == nil {
				t.Fatal("expected invalid state error")
			}
		})
	}
}

func TestWindowStateInvalidPlacementDoesNotEraseSavedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-state.json")
	want := WindowPlacement{X: -10, Y: 20, Width: 600, Height: 400}
	if err := SaveWindowState(path, want); err != nil {
		t.Fatal(err)
	}
	p := newWindowStatePersister(path, 10*time.Millisecond, nil)
	p.schedule(WindowPlacement{})
	if err := p.flush(WindowPlacement{}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWindowState(path)
	if err != nil || got != want {
		t.Fatalf("saved state after invalid placement = %+v, %v; want %+v", got, err, want)
	}
}

func TestWindowStateSaveFailureReportsOnce(t *testing.T) {
	var reports int
	path := t.TempDir() // replacing a directory with the temporary file fails
	p := newWindowStatePersister(path, time.Millisecond, func(error) { reports++ })
	err := p.flush(WindowPlacement{X: 1, Y: 2, Width: 600, Height: 400})
	if err == nil {
		t.Fatal("expected save to fail")
	}
	if reports != 1 {
		t.Fatalf("OnError reports = %d, want 1", reports)
	}
}

func TestWindowStateDebouncesAndCloseFlushesLatest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-state.json")
	p := newWindowStatePersister(path, 30*time.Millisecond, nil)
	defer p.stop()
	p.schedule(WindowPlacement{X: 1, Y: 2, Width: 500, Height: 400})
	p.schedule(WindowPlacement{X: 3, Y: 4, Width: 500, Height: 400})
	latest := WindowPlacement{X: 7, Y: 8, Width: 500, Height: 400, Maximized: true}
	p.schedule(latest)
	time.Sleep(70 * time.Millisecond)
	if got, err := LoadWindowState(path); err != nil || got != latest {
		t.Fatalf("debounced state = %+v, %v; want %+v", got, err, latest)
	}
	p.schedule(WindowPlacement{X: 9, Y: 10, Width: 500, Height: 400})
	closed := WindowPlacement{X: 11, Y: 12, Width: 600, Height: 450}
	if err := p.flush(closed); err != nil {
		t.Fatal(err)
	}
	time.Sleep(70 * time.Millisecond)
	if got, err := LoadWindowState(path); err != nil || got != closed {
		t.Fatalf("close state = %+v, %v; want %+v", got, err, closed)
	}
}

func TestNormalizeWindowStateOptions(t *testing.T) {
	normalized, err := normalizeOptions(Options{})
	if err != nil || normalized.WindowState != nil {
		t.Fatalf("nil state options = %+v, %v", normalized.WindowState, err)
	}
	normalized, err = normalizeOptions(Options{WindowState: &WindowStateOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := normalized.WindowState.Debounce; got != 250*time.Millisecond {
		t.Fatalf("default debounce = %v", got)
	}
	for _, opt := range []*WindowStateOptions{{Debounce: -time.Millisecond}, {Path: "bad\x00path"}} {
		if _, err := normalizeOptions(Options{WindowState: opt}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v error = %v", opt, err)
		}
	}
}

func TestWindowStateDefaultPathEscapesAppID(t *testing.T) {
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveWindowStatePath("", "org.example/my app")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "GoSX", url.PathEscape("org.example/my app"), "window-state.json")
	if got != want {
		t.Fatalf("default path = %q, want %q", got, want)
	}
}

func TestWindowStateUnsupportedPlatformsReturnSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows implements persisted window state")
	}
	_, err := New(Options{WindowState: &WindowStateOptions{}})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("New error = %v, want ErrUnsupported", err)
	}
}
