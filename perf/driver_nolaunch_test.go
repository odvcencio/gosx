package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
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
	assertNoRemoteSecretTokens(t, fmt.Sprintf("%+v", selection))
	encoded, err := json.Marshal(selection)
	if err != nil {
		t.Fatalf("marshal selection: %v", err)
	}
	assertNoRemoteSecretTokens(t, string(encoded))
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
	if selection.SanitizedRemoteWebSocketURL != "ws://option.example.test:9222" {
		t.Fatalf("sanitized endpoint = %q", selection.SanitizedRemoteWebSocketURL)
	}
	assertNoRemoteSecretTokens(t, fmt.Sprintf("%+v", selection))
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

func TestDriverRemoteContextFailsClosedWithoutEndpointLeak(t *testing.T) {
	raw := "ws://user:secret@127.0.0.1:1/devtools/browser/abc123?token=secret#frag"
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithRemoteWebSocketURL(raw), WithTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatalf("New remote: %v", err)
	}
	defer d.Close()

	err = chromedp.Run(d.Context())
	if err == nil {
		t.Fatal("remote status-only Context allowed chromedp.Run")
	}
	assertNoRemoteSecretTokens(t, err.Error())
}

func TestRemoteEndpointRedactionRemovesURLPathBrowserIDAndEscapedTokens(t *testing.T) {
	raw := "ws://user:secret@127.0.0.1:9222/devtools/browser/abc%20123?token=secret&debug=true#frag"
	msg := RedactChromeRemoteEndpointText(raw, "dial "+raw+" failed; retry /devtools/browser/abc%20123?token=secret and devtools/browser/abc%20123 plus abc%20123 frag token debug true user secret")
	for _, forbidden := range []string{
		"user",
		"secret",
		"token",
		"debug",
		"true",
		"devtools",
		"browser",
		"abc%20123",
		"frag",
		"127.0.0.1:9222/devtools",
	} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("redacted error leaked %q in %q", forbidden, msg)
		}
	}
	if !strings.Contains(msg, "remote-cdp-endpoint") {
		t.Fatalf("redacted error missing endpoint placeholder: %q", msg)
	}
	if !strings.Contains(msg, "remote-cdp-redacted") {
		t.Fatalf("redacted error missing token placeholder: %q", msg)
	}
}

func TestDriverRemoteBindTargetRedactsReturnedDialError(t *testing.T) {
	raw := "ws://user:secret@127.0.0.1:1/devtools/browser/abc123?token=secret#frag"
	t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithRemoteWebSocketURL(raw), WithTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatalf("New remote: %v", err)
	}
	defer d.Close()

	err = d.BindTarget()
	if err == nil {
		t.Fatal("expected remote dial error")
	}
	for _, forbidden := range []string{"user", "secret", "token", "devtools", "abc123", "frag", raw} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("returned error leaked %q in %q", forbidden, err)
		}
	}
}

func TestDriverRemoteZeroAndNegativeTimeoutBindTargetAreBounded(t *testing.T) {
	raw := "ws://user:secret@127.0.0.1:1/devtools/browser/abc123?token=secret#frag"
	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			t.Setenv("CHROME_PATH", "/nonexistent/chrome-binary")
			t.Setenv("CHROME_WS_URL", "")
			d, err := New(WithRemoteWebSocketURL(raw), WithTimeout(timeout))
			if err != nil {
				t.Fatalf("New remote: %v", err)
			}
			defer d.Close()

			start := time.Now()
			err = d.BindTarget()
			if err == nil {
				t.Fatal("expected remote dial error")
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("BindTarget took %s, want bounded quick failure", elapsed)
			}
			assertNoRemoteSecretTokens(t, err.Error())
		})
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

	op4, cancel4 := d.WithOperationContext(nil, 5*time.Millisecond)
	select {
	case <-op4.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("nil-parent operation context did not respect its timeout")
	}
	cancel4()
	cancel4()
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

	d.timeout = time.Millisecond
	err = d.Run(chromedp.Sleep(25 * time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("bounded operation error = %v, want deadline", err)
	}
	if err := d.Context().Err(); err != nil {
		t.Fatalf("bounded operation poisoned base: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	d.timeout = time.Second

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

func TestDriverPositiveTimeoutOperationDoesNotPoisonBase(t *testing.T) {
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithHeadless(true), WithTimeout(time.Second))
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so a browser is required: %v", err)
		}
		t.Skipf("skipping real browser timeout proof: %v", err)
	}
	defer d.Close()
	if err := d.BindTarget(); err != nil {
		t.Fatalf("BindTarget: %v", err)
	}
	d.timeout = 5 * time.Millisecond
	if err := d.Run(chromedp.Sleep(25 * time.Millisecond)); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("bounded Run error = %v, want deadline", err)
	}
	if err := d.Context().Err(); err != nil {
		t.Fatalf("operation timeout poisoned base context: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	d.timeout = time.Second
	var got int
	if err := d.Evaluate(`1 + 1`, &got); err != nil {
		t.Fatalf("Evaluate after timed-out operation: %v", err)
	}
	if got != 2 {
		t.Fatalf("Evaluate got %d, want 2", got)
	}
}

func TestDriverConcurrentRunAndRepeatedCancel(t *testing.T) {
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithHeadless(true), WithTimeout(time.Second))
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so a browser is required: %v", err)
		}
		t.Skipf("skipping real browser concurrent Run proof: %v", err)
	}
	defer d.Close()
	if err := d.BindTarget(); err != nil {
		t.Fatalf("BindTarget: %v", err)
	}

	op, cancel := d.WithOperationContext(context.Background(), time.Second)
	cancel()
	cancel()
	<-op.Context().Done()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var got int
			err := d.Evaluate(`21 * 2`, &got)
			if err == nil && got != 42 {
				err = fmt.Errorf("Evaluate got %d, want 42", got)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Run failed: %v", err)
		}
	}
}

func TestDriverRunNonCooperativeTimeoutMarksUnusable(t *testing.T) {
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithHeadless(true), WithTimeout(time.Second))
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so a browser is required: %v", err)
		}
		t.Skipf("skipping non-cooperative Run proof: %v", err)
	}
	defer d.Close()
	if err := d.BindTarget(); err != nil {
		t.Fatalf("BindTarget: %v", err)
	}
	d.timeout = time.Millisecond

	release := make(chan struct{})
	joined := make(chan struct{})
	start := time.Now()
	err = d.Run(chromedp.ActionFunc(func(context.Context) error {
		defer close(joined)
		<-release
		return nil
	}))
	if err == nil || !strings.Contains(err.Error(), "unusable") {
		t.Fatalf("non-cooperative Run error = %v, want unusable", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("non-cooperative Run took %s, want bounded return", elapsed)
	}
	if err := d.Run(); err == nil || !strings.Contains(err.Error(), "unusable") {
		t.Fatalf("future Run error = %v, want stable unusable", err)
	}
	if err := d.Context().Err(); err == nil {
		t.Fatal("base context stayed usable after non-cooperative timeout")
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close after unusable: %v", err)
	}
	close(release)
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("non-cooperative action did not join after release")
	}
}

func TestDriverRunSuccessfulOperationsReleaseWatchers(t *testing.T) {
	t.Setenv("CHROME_WS_URL", "")
	d, err := New(WithHeadless(true), WithTimeout(time.Minute))
	if err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so a browser is required: %v", err)
		}
		t.Skipf("skipping successful Run watcher proof: %v", err)
	}
	defer d.Close()
	if err := d.BindTarget(); err != nil {
		t.Fatalf("BindTarget: %v", err)
	}

	baseline := activeDriverOperationTimers.Load()
	for i := 0; i < 100; i++ {
		if err := d.Run(); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := activeDriverOperationTimers.Load(); got == baseline {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active operation timers = %d, want %d", activeDriverOperationTimers.Load(), baseline)
}

func assertNoRemoteSecretTokens(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{
		"user",
		"secret",
		"token",
		"debug",
		"devtools",
		"abc123",
		"abc%20123",
		"frag",
		"key",
		"value",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("remote text leaked %q in %q", forbidden, text)
		}
	}
}
