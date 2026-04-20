package desktop

import (
	"errors"
	"runtime"
	"testing"
)

func TestNormalizeOptionsDefaults(t *testing.T) {
	options, err := normalizeOptions(Options{})
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	if options.Title != defaultTitle {
		t.Fatalf("default title = %q, want %q", options.Title, defaultTitle)
	}
	if options.Width != defaultWidth {
		t.Fatalf("default width = %d, want %d", options.Width, defaultWidth)
	}
	if options.Height != defaultHeight {
		t.Fatalf("default height = %d, want %d", options.Height, defaultHeight)
	}
	if options.URL != defaultURL {
		t.Fatalf("default url = %q, want %q", options.URL, defaultURL)
	}
}

func TestNormalizeOptionsRejectsNUL(t *testing.T) {
	_, err := normalizeOptions(Options{Title: "bad\x00title"})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestNormalizeOptionsRejectsURLAndHTML(t *testing.T) {
	_, err := normalizeOptions(Options{URL: "https://example.test", HTML: "<h1>ok</h1>"})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestRunUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("windows desktop backend is supported on this architecture")
	}
	err := Run(Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

func TestNewUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("windows desktop backend is supported on this architecture")
	}
	_, err := New(Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

// TestUnsupportedAppRejectsAllBridgeAndWindowCalls locks down the stub
// surface: every newly-added method on the platformApp interface must
// report ErrUnsupported when the backend isn't implemented. Prevents the
// macOS/Linux stubs from silently returning nil success on the new
// PostMessage / ExecuteScript / OpenDevTools / window-state APIs.
func TestUnsupportedAppRejectsAllBridgeAndWindowCalls(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("windows desktop backend is supported on this architecture")
	}
	stub := unsupportedApp{}

	checks := []struct {
		name string
		err  error
	}{
		{"PostMessage", stub.PostMessage("noop")},
		{"ExecuteScript", stub.ExecuteScript("1")},
		{"OpenDevTools", stub.OpenDevTools()},
		{"PrependBootstrapScript", stub.PrependBootstrapScript("noop")},
		{"Minimize", stub.Minimize()},
		{"Maximize", stub.Maximize()},
		{"Restore", stub.Restore()},
		{"Focus", stub.Focus()},
		{"SetTitle", stub.SetTitle("ok")},
	}
	for _, c := range checks {
		if !errors.Is(c.err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", c.name, c.err)
		}
	}
}
