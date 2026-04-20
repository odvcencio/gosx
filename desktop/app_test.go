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
