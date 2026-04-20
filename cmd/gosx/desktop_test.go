package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	if desktopDirectMode(DesktopRunOptions{}) {
		t.Fatal("did not expect empty options to enable direct mode")
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

func TestRunDesktopUnsupportedPlatformShortCircuits(t *testing.T) {
	if runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		t.Skip("desktop backend is supported on this platform")
	}
	err := RunDesktop(".", DesktopRunOptions{})
	if !errors.Is(err, desktop.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
