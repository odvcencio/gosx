package perf

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestDriverNoChromeError(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "")
	_, err := New()
	if err == nil {
		t.Fatal("expected error when Chrome not found")
	}
}

func TestDriverRemoteSelectionFallsBackToEnvWithoutLocalChrome(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "ws://user:secret@127.0.0.1:9222/devtools/browser/abc?token=secret")

	d, err := New()
	if err != nil {
		t.Fatalf("New remote: %v", err)
	}
	defer d.Close()

	selection := d.AllocatorSelection()
	if selection.Mode != AllocatorModeRemote {
		t.Fatalf("allocator mode = %q, want remote", selection.Mode)
	}
	if selection.SanitizedRemoteWebSocketURL != "ws://127.0.0.1:9222" {
		t.Fatalf("sanitized endpoint = %q", selection.SanitizedRemoteWebSocketURL)
	}
	if !strings.HasPrefix(selection.RemoteWebSocketURLSHA256, "sha256:") {
		t.Fatalf("endpoint hash = %q, want sha256 identity", selection.RemoteWebSocketURLSHA256)
	}
	if strings.Contains(selection.SanitizedRemoteWebSocketURL, "secret") || strings.Contains(selection.SanitizedRemoteWebSocketURL, "token") || strings.Contains(selection.SanitizedRemoteWebSocketURL, "devtools") {
		t.Fatalf("sanitized endpoint leaked secret: %q", selection.SanitizedRemoteWebSocketURL)
	}
}

func TestDriverRemoteOptionTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "ws://env.example.test:9222/devtools/browser/env")

	d, err := New(WithRemoteWebSocketURL("ws://option.example.test:9222/devtools/browser/option?key=value"))
	if err != nil {
		t.Fatalf("New remote option: %v", err)
	}
	defer d.Close()

	selection := d.AllocatorSelection()
	if selection.RemoteWebSocketURL != "ws://option.example.test:9222/devtools/browser/option?key=value" {
		t.Fatalf("remote URL = %q, want option URL", selection.RemoteWebSocketURL)
	}
	if selection.SanitizedRemoteWebSocketURL != "ws://option.example.test:9222" {
		t.Fatalf("sanitized endpoint = %q", selection.SanitizedRemoteWebSocketURL)
	}
}

func TestDriverWithTimeoutZeroKeepsBaseContextAlive(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "ws://127.0.0.1:9222")

	d, err := New(WithTimeout(0))
	if err != nil {
		t.Fatalf("New remote with zero timeout: %v", err)
	}
	defer d.Close()
	if err := d.Context().Err(); err != nil {
		t.Fatalf("zero timeout expired base context: %v", err)
	}
}

func TestDriverRejectsInvalidRemoteEndpointsBeforeChromeLookup(t *testing.T) {
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	for _, endpoint := range []string{
		"://broken",
		"ftp://127.0.0.1:9222",
		"ws:///devtools/browser/abc",
		"http://",
	} {
		t.Setenv("CHROME_WS_URL", endpoint)
		if _, err := New(); err == nil {
			t.Fatalf("New accepted invalid endpoint %q", endpoint)
		}
	}
}

func TestDriverOperationContextDoesNotExpireWarmBase(t *testing.T) {
	baseCtx, baseCancel := context.WithCancel(context.Background())
	defer baseCancel()
	d := &Driver{ctx: baseCtx}

	op1, cancel1 := d.WithOperationContext(context.Background(), 5*time.Millisecond)
	<-op1.Context().Done()
	cancel1()
	cancel1()
	if err := baseCtx.Err(); err != nil {
		t.Fatalf("base context expired after first operation: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	op2, cancel2 := d.WithOperationContext(context.Background(), 5*time.Millisecond)
	select {
	case <-op2.Context().Done():
		t.Fatal("second operation inherited expired context")
	default:
	}
	<-op2.Context().Done()
	cancel2()
	cancel2()
	if err := baseCtx.Err(); err != nil {
		t.Fatalf("base context expired after second operation: %v", err)
	}

	parent, parentCancel := context.WithCancel(context.Background())
	op3, cancel3 := d.WithOperationContext(parent, time.Minute)
	parentCancel()
	select {
	case <-op3.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("operation context ignored parent cancellation")
	}
	cancel3()
	cancel3()
}

func TestDriverLongLivedBaseSurvivesCanceledOperation(t *testing.T) {
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithHeadless(true), WithTimeout(0))
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so a browser is required: %v", err)
		}
		t.Skipf("skipping real browser lifecycle proof: %v", err)
	}
	defer d.Close()
	if err := d.Context().Err(); err != nil {
		t.Fatalf("zero timeout poisoned base before bind: %v", err)
	}
	if err := d.BindTarget(); err != nil {
		t.Fatalf("BindTarget: %v", err)
	}

	op, cancel := d.WithOperationContext(context.Background(), time.Millisecond)
	err = chromedp.Run(op.Context(), chromedp.Sleep(25*time.Millisecond))
	cancel()
	cancel()
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("bounded operation error = %v, want deadline", err)
	}
	if err := d.Context().Err(); err != nil {
		t.Fatalf("bounded operation poisoned base: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Still Alive</title></head><body></body></html>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate after canceled operation: %v", err)
	}
	var title string
	if err := d.Evaluate(`document.title`, &title); err != nil {
		t.Fatalf("Evaluate after canceled operation: %v", err)
	}
	if title != "Still Alive" {
		t.Fatalf("title after canceled operation = %q", title)
	}
}
