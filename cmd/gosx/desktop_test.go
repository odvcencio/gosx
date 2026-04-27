package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/gosx/desktop"
)

func TestDesktopListenAddrDefaultsToLoopback(t *testing.T) {
	addr, err := desktopListenAddr("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("addr = %q, want loopback host", addr)
	}
	if strings.TrimPrefix(addr, "127.0.0.1:") == "" {
		t.Fatalf("addr = %q, missing port", addr)
	}
}

func TestDesktopListenAddrNormalizesBarePort(t *testing.T) {
	addr, err := desktopListenAddr(":4321")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "127.0.0.1:4321" {
		t.Fatalf("addr = %q, want 127.0.0.1:4321", addr)
	}
}

func TestDesktopListenAddrPreservesExplicitHost(t *testing.T) {
	addr, err := desktopListenAddr("localhost:4321")
	if err != nil {
		t.Fatal(err)
	}
	if addr != "localhost:4321" {
		t.Fatalf("addr = %q, want localhost:4321", addr)
	}
}

func TestDesktopDirectMode(t *testing.T) {
	if !desktopDirectMode(DesktopRunOptions{URL: " https://example.test "}) {
		t.Fatal("expected URL option to enable direct mode")
	}
	if !desktopDirectMode(DesktopRunOptions{HTML: "<h1>ok</h1>"}) {
		t.Fatal("expected HTML option to enable direct mode")
	}
	if !desktopDirectMode(DesktopRunOptions{BundleDir: "dist/offline"}) {
		t.Fatal("expected bundle option to enable direct mode")
	}
	if desktopDirectMode(DesktopRunOptions{}) {
		t.Fatal("did not expect empty options to enable direct mode")
	}
	if got := desktopDirectModeConflictCount(DesktopRunOptions{URL: "https://example.test", BundleDir: "dist/offline"}); got != 2 {
		t.Fatalf("conflict count = %d, want 2", got)
	}
}

func TestDesktopArgsBeforeParseConsumesDevAlias(t *testing.T) {
	args, dev := desktopArgsBeforeParse([]string{"dev", "--devtools", "examples/app"})
	if !dev {
		t.Fatal("expected leading dev alias to be detected")
	}
	if got := strings.Join(args, " "); got != "--devtools examples/app" {
		t.Fatalf("args = %q", got)
	}
}

func TestDesktopArgsBeforeParseLeavesFlagsFirst(t *testing.T) {
	args, dev := desktopArgsBeforeParse([]string{"--devtools", "dev", "examples/app"})
	if dev {
		t.Fatal("did not expect dev alias before flag parsing")
	}
	if got := strings.Join(args, " "); got != "--devtools dev examples/app" {
		t.Fatalf("args = %q", got)
	}
}

func TestDesktopArgsAfterParseConsumesDevAlias(t *testing.T) {
	args, dev := desktopArgsAfterParse([]string{"dev", "examples/app"})
	if !dev {
		t.Fatal("expected parsed dev alias")
	}
	if got := strings.Join(args, " "); got != "examples/app" {
		t.Fatalf("args = %q", got)
	}
}

func TestWaitForDesktopProxyReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gosx/dev/info" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	errs := make(chan error, 1)
	if err := waitForDesktopProxyReady(server.URL, errs, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForDesktopProxyReadyReportsServerFailure(t *testing.T) {
	errs := make(chan error, 1)
	errs <- fmt.Errorf("bind failed")
	err := waitForDesktopProxyReady("http://127.0.0.1:1", errs, time.Second)
	if err == nil || !strings.Contains(err.Error(), "bind failed") {
		t.Fatalf("err = %v, want bind failure", err)
	}
}

func TestWaitForDesktopProxyReadyRequiresDevInfoOK(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	errs := make(chan error, 1)
	err := waitForDesktopProxyReady(server.URL, errs, 120*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want readiness timeout", err)
	}
}

func TestDesktopBundleTargetPrefersOfflineBundle(t *testing.T) {
	dir := t.TempDir()
	offline := filepath.Join(dir, "offline")
	if err := os.MkdirAll(filepath.Join(offline, "static"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(offline, "offline-manifest.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(offline, "static", "index.html"), []byte("<h1>offline</h1>"), 0644); err != nil {
		t.Fatal(err)
	}

	root, entry, err := desktopBundleTarget(DesktopRunOptions{BundleDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if root != offline {
		t.Fatalf("root = %q, want %q", root, offline)
	}
	if entry != "app://gosx/static/" {
		t.Fatalf("entry = %q, want app://gosx/static/", entry)
	}
}

func TestDesktopBundleTargetRequiresAppScheme(t *testing.T) {
	dir := t.TempDir()
	_, _, err := desktopBundleTarget(DesktopRunOptions{
		BundleDir: dir,
		BundleURL: "https://example.test/",
	})
	if err == nil || !strings.Contains(err.Error(), "app://gosx/") {
		t.Fatalf("err = %v, want app scheme validation", err)
	}
}

func TestDesktopBundleHandlerServesStaticIndexWithoutListing(t *testing.T) {
	root := t.TempDir()
	staticDir := filepath.Join(root, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<h1>desktop</h1>"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := desktopBundleHandler(root)
	req := httptest.NewRequest(http.MethodGet, "app://gosx/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "desktop") {
		t.Fatalf("body = %q, want desktop page", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "app://gosx/static/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "desktop") {
		t.Fatalf("directory index status/body = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDesktopBundleHandlerRejectsUnsupportedMethods(t *testing.T) {
	root := t.TempDir()
	handler := desktopBundleHandler(root)
	req := httptest.NewRequest(http.MethodPost, "app://gosx/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want GET, HEAD", got)
	}
}

func TestRunDesktopUnsupportedPlatformShortCircuits(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("desktop backend is supported on this platform")
	}
	err := RunDesktop(".", DesktopRunOptions{})
	if !errors.Is(err, desktop.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
