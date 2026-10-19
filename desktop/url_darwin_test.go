//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestDarwinValidateOpenURL(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string // the URL handed to the system; "" for a refusal
	}{
		// Accepted schemes.
		{"https://example.com", "https://example.com"},
		{"http://127.0.0.1:8080/path?q=1#frag", "http://127.0.0.1:8080/path?q=1#frag"},
		{"HTTPS://Example.com/Docs", "HTTPS://Example.com/Docs"},
		{"mailto:someone@example.com", "mailto:someone@example.com"},
		{"mailto:someone@example.com?subject=Hello%20there", "mailto:someone@example.com?subject=Hello%20there"},
		{"file:///Users/someone/notes.txt", "file:///Users/someone/notes.txt"},
		{"  https://example.com/padded \n", "https://example.com/padded"},
		{"https://example.com/日本語", "https://example.com/日本語"},

		// Refused: nothing to open.
		{"", ""},
		{"   ", ""},
		{"\t\n", ""},

		// Refused: schemes outside the list.
		{"ftp://example.com/file", ""},
		{"javascript:alert(1)", ""},
		{"JavaScript:alert(1)", ""},
		{"data:text/html,<script>alert(1)</script>", ""},
		{"tel:+15551234567", ""},
		{"ssh://host", ""},
		{"x-apple.systempreferences:com.apple.preference.security", ""},
		{"vscode://file/etc/hosts", ""},
		{"about:blank", ""},

		// Refused: no scheme, or a scheme-relative or path-only reference.
		{"example.com", ""},
		{"example.com/path", ""},
		{"//example.com/path", ""},
		{"/etc/hosts", ""},
		{"localhost:8080", ""},

		// Refused: web URLs with nothing to connect to.
		{"https://", ""},
		{"http://", ""},
		{"http:///path", ""},

		// Refused: not a URL.
		{"https://exa mple.com/", ""},
		{"https://example.com/\x00", ""},
		{"https://example.com/\x7f", ""},
		{"%", ""},
		{"http://[::1", ""},
	} {
		got, err := validateOpenURL(tc.in)
		if tc.want == "" {
			if !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("validateOpenURL(%q) = %q, %v; want ErrInvalidOptions", tc.in, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("validateOpenURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// fakeWorkspace replaces the NSWorkspace call for the length of a test and records
// what OpenURL would have opened, and on which thread.
type fakeWorkspace struct {
	mu      sync.Mutex
	opened  []string
	threads []uint32
	err     error
}

func (f *fakeWorkspace) install(t *testing.T) {
	t.Helper()
	saved := openURLOnWorkspace
	t.Cleanup(func() { openURLOnWorkspace = saved })
	openURLOnWorkspace = func(target string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.opened = append(f.opened, target)
		f.threads = append(f.threads, currentThreadID())
		return f.err
	}
}

// OpenURL validates, then hands the cleaned URL to the workspace; a refusal never
// reaches it.
func TestDarwinOpenURLValidatesBeforeOpening(t *testing.T) {
	impl, _ := newPlatformApp(Options{})
	var fake fakeWorkspace
	fake.install(t)

	for _, bad := range []string{"", "javascript:alert(1)", "file", "ftp://example.com", "https://"} {
		if err := impl.OpenURL(bad); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("OpenURL(%q) = %v, want ErrInvalidOptions", bad, err)
		}
	}
	if len(fake.opened) != 0 {
		t.Fatalf("the workspace was asked to open %q for URLs that should have been refused", fake.opened)
	}

	if err := impl.OpenURL("  https://example.com/a  "); err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if err := impl.OpenURL("mailto:a@example.com"); err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	if got := strings.Join(fake.opened, " "); got != "https://example.com/a mailto:a@example.com" {
		t.Errorf("the workspace opened %q", got)
	}
}

func TestDarwinOpenURLReportsAFailedOpen(t *testing.T) {
	impl, _ := newPlatformApp(Options{})
	fake := fakeWorkspace{err: errors.New("no application is registered for this URL")}
	fake.install(t)
	if err := impl.OpenURL("https://example.com"); err == nil || !strings.Contains(err.Error(), "no application") {
		t.Errorf("OpenURL = %v, want the workspace's error", err)
	}
}

// With the app running, OpenURL from another goroutine is a main-thread call, like
// the rest of the platform surface.
func TestDarwinOpenURLRunsOnTheMainThreadWhileRunning(t *testing.T) {
	var fake fakeWorkspace
	fake.install(t)
	runTestApp(t, Options{}, func(app *App, da *darwinApp) {
		if err := app.OpenURL("https://example.com/running"); err != nil {
			t.Errorf("OpenURL: %v", err)
		}
	})
	if len(fake.opened) != 1 || fake.threads[0] != dispatchMainThread {
		t.Errorf("opened %q on threads %v, want one call on the main thread (%d)", fake.opened, fake.threads, dispatchMainThread)
	}
}

// The real NSWorkspace: a file that does not exist has no application to open it,
// so openURL: answers NO, and OpenURL says so. Nothing is launched.
func TestDarwinOpenURLNativeRefusesAMissingFile(t *testing.T) {
	requireGUI(t)
	impl, _ := newPlatformApp(Options{})
	err := impl.OpenURL("file:///nonexistent-gosx-desktop-test/missing.txt")
	if err == nil {
		t.Fatal("OpenURL of a missing file succeeded")
	}
	if errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), "could not open") {
		t.Errorf("OpenURL = %v, want the failed open reported (the URL itself is valid)", err)
	}
}

// Opens a page in the default browser for real. It needs a person to look at the
// result, so it only runs with GOSX_DARWIN_INTERACTIVE=1.
func TestDarwinOpenURLInteractive(t *testing.T) {
	requireInteractive(t)
	impl, _ := newPlatformApp(Options{})
	if err := impl.OpenURL("https://example.com/"); err != nil {
		t.Fatalf("OpenURL: %v", err)
	}
	t.Log("the default browser was asked to open https://example.com/")
}
