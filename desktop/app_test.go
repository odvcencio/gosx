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
	if options.AppID != "gosx.gosx" {
		t.Fatalf("default app id = %q, want gosx.gosx", options.AppID)
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

func TestNormalizeOptionsRejectsBadAppID(t *testing.T) {
	_, err := normalizeOptions(Options{AppID: `bad\id`})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error = %v, want ErrInvalidOptions", err)
	}
}

func TestNormalizeWindowOptions(t *testing.T) {
	base, err := normalizeOptions(Options{Title: "Base", Width: 900, Height: 700})
	if err != nil {
		t.Fatalf("normalize base: %v", err)
	}
	got, err := normalizeWindowOptions(base, WindowOptions{})
	if err != nil {
		t.Fatalf("normalize window: %v", err)
	}
	if got.Title != "Base" || got.Width != 900 || got.Height != 700 || got.URL != defaultURL {
		t.Fatalf("window options = %+v", got)
	}
}

func TestDevToolsEnabled(t *testing.T) {
	cases := []struct {
		name string
		opts Options
		want bool
	}{
		{name: "off", opts: Options{}, want: false},
		{name: "debug", opts: Options{Debug: true}, want: true},
		{name: "devtools", opts: Options{DevTools: true}, want: true},
		{name: "both", opts: Options{Debug: true, DevTools: true}, want: true},
	}
	for _, tc := range cases {
		if got := devToolsEnabled(tc.opts); got != tc.want {
			t.Fatalf("%s: devToolsEnabled = %v, want %v", tc.name, got, tc.want)
		}
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
