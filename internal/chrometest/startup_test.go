package chrometest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gorilla/websocket"
)

func TestStartDoesNotRetryEarlyProcessExitAndRedactsDiagnostics(t *testing.T) {
	requirePOSIXShell(t)
	logPath := filepath.Join(t.TempDir(), "attempts.log")
	t.Setenv("GOSX_FAKE_LOG", logPath)
	executable := writeFakeChrome(t, `
printf 'attempt\n' >> "$GOSX_FAKE_LOG"
printf 'fatal token=hunter2 url=https://user:password@example.test/path?q=secret\n' >&2
exit 23
`)
	started := time.Now()
	_, err := startWithPolicy(context.Background(), executable, fastPolicy(3))
	if err == nil {
		t.Fatal("expected early process exit")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("early exit took %s", elapsed)
	}
	if got := countLogLines(t, logPath); got != 1 {
		t.Fatalf("early exit attempts = %d, want 1", got)
	}
	message := err.Error()
	for _, secret := range []string{"hunter2", "user:password", "q=secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("startup error leaked %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "token=<redacted>") || !strings.Contains(message, "exit status 23") {
		t.Fatalf("startup error lacks actionable redaction: %s", message)
	}
}

func TestStartRetriesOnlyEndpointTimeoutWithFreshProfilesAndCleanup(t *testing.T) {
	requirePOSIXShell(t)
	tempRoot := t.TempDir()
	logPath := filepath.Join(tempRoot, "attempts.log")
	t.Setenv("GOSX_FAKE_LOG", logPath)
	executable := writeFakeChrome(t, `
printf 'pid=%s' "$$" >> "$GOSX_FAKE_LOG"
for arg in "$@"; do printf ' %s' "$arg" >> "$GOSX_FAKE_LOG"; done
printf '\nstartup secret=private-attempt\n' >> "$GOSX_FAKE_LOG"
printf 'startup secret=private-attempt\n' >&2
exec tail -f /dev/null
`)
	policy := fastPolicy(2)
	_, err := startWithPolicy(context.Background(), executable, policy)
	if err == nil {
		t.Fatal("expected bounded endpoint timeout")
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, logPath)), "\n")
	var profiles []string
	var attempts int
	for _, line := range lines {
		if !strings.HasPrefix(line, "pid=") {
			continue
		}
		attempts++
		fields := strings.Fields(line)
		for _, field := range fields {
			if strings.HasPrefix(field, "--user-data-dir=") {
				profiles = append(profiles, strings.TrimPrefix(field, "--user-data-dir="))
			}
		}
		if !strings.Contains(line, "--remote-debugging-port=0") {
			t.Fatalf("attempt did not use an ephemeral debugging port: %s", line)
		}
	}
	if attempts != 2 || len(profiles) != 2 || profiles[0] == profiles[1] {
		t.Fatalf("attempts=%d profiles=%v, want two fresh profiles", attempts, profiles)
	}
	for _, profile := range profiles {
		if _, statErr := os.Stat(profile); !os.IsNotExist(statErr) {
			t.Fatalf("profile was not removed after failed attempt: %q err=%v", profile, statErr)
		}
	}
	if strings.Contains(err.Error(), "private-attempt") || !strings.Contains(err.Error(), "secret=<redacted>") {
		t.Fatalf("retry diagnostics were not redacted: %v", err)
	}
}

func TestStartContinuouslyDrainsLargeProcessOutput(t *testing.T) {
	requirePOSIXShell(t)
	executable := writeFakeChrome(t, `
i=0
while [ "$i" -lt 12000 ]; do
  printf 'pipe-noise-%s password=do-not-print\n' "$i" >&2
  i=$((i + 1))
done
printf 'FLOOD_COMPLETE\n' >&2
exit 27
`)
	policy := fastPolicy(1)
	policy.attemptTimeout = 5 * time.Second
	policy.overallTimeout = 10 * time.Second
	started := time.Now()
	_, err := startWithPolicy(context.Background(), executable, policy)
	if err == nil {
		t.Fatal("expected process exit after pipe flood")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("large pipe output blocked startup cleanup for %s", elapsed)
	}
	if strings.Contains(err.Error(), "do-not-print") {
		t.Fatalf("pipe diagnostics leaked secret: %v", err)
	}
	if !strings.Contains(err.Error(), "earlier output truncated") || len(err.Error()) > diagnosticsLimit+1024 {
		t.Fatalf("pipe diagnostics were not bounded: bytes=%d error=%v", len(err.Error()), err)
	}
	if !strings.Contains(err.Error(), "FLOOD_COMPLETE") || !strings.Contains(err.Error(), "exit status 27") {
		t.Fatalf("process did not finish writing the full pipe flood: %v", err)
	}
}

func TestStartRetriesBeforeActionsAndSuccessStopsStartupTimer(t *testing.T) {
	requirePOSIXShell(t)
	endpoint, calls, _ := fakeCDP(t, false)
	t.Setenv("GOSX_FAKE_ENDPOINT", endpoint)
	logPath := filepath.Join(t.TempDir(), "attempts.log")
	t.Setenv("GOSX_FAKE_LOG", logPath)
	executable := writeFakeChrome(t, `
if [ ! -e "$GOSX_FAKE_LOG" ]; then
  printf 'first\n' > "$GOSX_FAKE_LOG"
  exec tail -f /dev/null
fi
printf 'second\n' >> "$GOSX_FAKE_LOG"
printf 'DevTools listening on %s\n' "$GOSX_FAKE_ENDPOINT" >&2
exec tail -f /dev/null
`)
	policy := fastPolicy(2)
	policy.attemptTimeout = 500 * time.Millisecond
	browser, err := startWithPolicy(context.Background(), executable, policy)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	if got := countLogLines(t, logPath); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
	select {
	case <-calls:
	default:
		t.Fatal("successful startup did not bind a CDP target")
	}
	// A recovered browser must outlive its startup timer and remain usable.
	time.Sleep(2 * policy.attemptTimeout)
	var got int
	if err := chromedp.Run(browser.Context, chromedp.Evaluate(`1 + 1`, &got)); err != nil || got != 2 {
		t.Fatalf("bound browser did not survive startup deadline: got=%d err=%v", got, err)
	}
	browser.Close()
	browser.Close()
	if _, err := os.Stat(browser.process.profileDir); !os.IsNotExist(err) {
		t.Fatalf("successful browser profile not removed: %v", err)
	}
}

func TestStartCancellationDuringCDPBindJoinsConnection(t *testing.T) {
	requirePOSIXShell(t)
	endpoint, calls, closed := fakeCDP(t, true)
	t.Setenv("GOSX_FAKE_ENDPOINT", endpoint)
	executable := writeFakeChrome(t, `
printf 'DevTools listening on %s\n' "$GOSX_FAKE_ENDPOINT" >&2
exec tail -f /dev/null
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Start(ctx, executable)
		result <- err
	}()
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("did not reach stalled CDP bind")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("startup cancellation = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked in CDP bind")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("CDP connection survived canceled startup")
	}
}

func TestStartOverallBudgetBoundsRetries(t *testing.T) {
	requirePOSIXShell(t)
	logPath := filepath.Join(t.TempDir(), "attempts.log")
	t.Setenv("GOSX_FAKE_LOG", logPath)
	executable := writeFakeChrome(t, `
printf 'attempt\n' >> "$GOSX_FAKE_LOG"
exec tail -f /dev/null
`)
	policy := fastPolicy(3)
	policy.attemptTimeout = time.Second
	policy.overallTimeout = cleanupAllowance + 50*time.Millisecond
	started := time.Now()
	_, err := startWithPolicy(context.Background(), executable, policy)
	if err == nil || !strings.Contains(err.Error(), "overall budget") {
		t.Fatalf("overall startup deadline = %v", err)
	}
	if time.Since(started) >= time.Second || countLogLines(t, logPath) != 1 {
		t.Fatal("overall budget allowed an additional attempt")
	}
}

func TestEndpointParsingAndDiagnosticsAreBounded(t *testing.T) {
	for _, invalid := range []string{
		"ws://example.test:1234/devtools/browser/id", "ws://127.0.0.1:0/devtools/browser/id",
		"ws://user@127.0.0.1:1234/devtools/browser/id", "ws://127.0.0.1:1234/devtools/browser/id?",
		"ws://127.0.0.1:1234/devtools/browser/", "wss://127.0.0.1:1234/devtools/browser/id",
	} {
		if err := validateDevToolsEndpoint(invalid); err == nil {
			t.Errorf("accepted invalid endpoint %q", invalid)
		}
	}
	for _, endpoint := range []string{"ws://127.0.0.1:1234/devtools/browser/id", "ws://[::1]:1234/devtools/browser/id"} {
		if err := validateDevToolsEndpoint(endpoint); err != nil {
			t.Errorf("rejected loopback endpoint: %v", err)
		}
	}
	tail := &tailWriter{limit: 256}
	endpoint := make(chan string, 1)
	writer := &endpointWriter{diagnostics: tail, endpoint: endpoint, endpointOnce: new(sync.Once)}
	for _, chunk := range []string{"noise\r\nDevTools listen", "ing on ws://127.0.0.1:1234/devtools/", "browser/id\r\n"} {
		_, _ = writer.Write([]byte(chunk))
	}
	select {
	case got := <-endpoint:
		if got != "ws://127.0.0.1:1234/devtools/browser/id" {
			t.Fatalf("chunked endpoint = %q", got)
		}
	default:
		t.Fatal("chunked CRLF endpoint was not parsed")
	}
	_, _ = tail.Write([]byte("token=" + strings.Repeat("private", 300) + "\nuseful error\n"))
	if got := redactDiagnostics(tail.String()); strings.Contains(got, "private") || !strings.Contains(got, "useful error") {
		t.Fatalf("tail cut leaked a partial secret or lost complete lines: %q", got)
	}
	for _, input := range []string{"authorization: Bearer private", `password="two words"`, "https://user:pass@host/path?q=private", "\x1b[31msecret=private", "secret: private\rnext line"} {
		if got := redactDiagnostics(input); strings.Contains(got, "private") || strings.Contains(got, "\x1b") || strings.Contains(got, "two words") {
			t.Errorf("diagnostic was not redacted: %q", got)
		}
	}
}

// This small CDP peer only supplies the responses needed to bind an empty tab.
// Tests observe real websocket lifetimes; they do not stub chromedp.Run or cleanup.
func fakeCDP(t *testing.T, stall bool) (string, <-chan struct{}, <-chan struct{}) {
	return fakeCDPBeforeUpgrade(t, stall, nil)
}

func fakeCDPBeforeUpgrade(t *testing.T, stall bool, beforeUpgrade func(http.ResponseWriter, *http.Request) bool) (string, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	calls := make(chan struct{}, 1)
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if beforeUpgrade != nil && !beforeUpgrade(w, r) {
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		defer close(closed)
		for {
			var request struct {
				ID        int             `json:"id"`
				Method    string          `json:"method"`
				SessionID string          `json:"sessionId,omitempty"`
				Params    json.RawMessage `json:"params"`
			}
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			select {
			case calls <- struct{}{}:
			default:
			}
			if stall {
				continue
			}
			result := any(map[string]any{})
			switch request.Method {
			case "Target.createTarget":
				result = map[string]any{"targetId": "page"}
			case "Target.attachToTarget":
				result = map[string]any{"sessionId": "session"}
			case "Runtime.evaluate":
				if strings.Contains(string(request.Params), "1 + 1") {
					result = map[string]any{"result": map[string]any{"type": "number", "value": 2}}
				} else {
					result = map[string]any{"result": map[string]any{"type": "object", "className": "Window"}}
				}
			}
			if err := conn.WriteJSON(map[string]any{"id": request.ID, "sessionId": request.SessionID, "result": result}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/test", calls, closed
}

func TestStartAllowsDelayedWebSocketHandshakeWithinAttempt(t *testing.T) {
	requirePOSIXShell(t)
	endpoint, _, _ := fakeCDPBeforeUpgrade(t, false, func(w http.ResponseWriter, r *http.Request) bool {
		select {
		case <-time.After(150 * time.Millisecond):
			return true
		case <-r.Context().Done():
			return false
		}
	})
	t.Setenv("GOSX_FAKE_ENDPOINT", endpoint)
	executable := writeFakeChrome(t, `
printf 'DevTools listening on %s\n' "$GOSX_FAKE_ENDPOINT" >&2
exec tail -f /dev/null
`)
	policy := fastPolicy(1)
	policy.attemptTimeout = time.Second
	browser, err := startWithPolicy(t.Context(), executable, policy)
	if err != nil {
		t.Fatalf("handshake within the startup budget failed: %v", err)
	}
	defer browser.Close()
}

func TestStartCancellationIsImmediateAndDoesNotRetry(t *testing.T) {
	requirePOSIXShell(t)
	tempRoot := t.TempDir()
	logPath := filepath.Join(tempRoot, "attempts.log")
	readyPath := filepath.Join(tempRoot, "ready")
	t.Setenv("GOSX_FAKE_LOG", logPath)
	t.Setenv("GOSX_FAKE_READY", readyPath)
	executable := writeFakeChrome(t, `
printf 'attempt\n' >> "$GOSX_FAKE_LOG"
: > "$GOSX_FAKE_READY"
exec tail -f /dev/null
`)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := startWithPolicy(ctx, executable, startupPolicy{
			attempts:       3,
			attemptTimeout: 2 * time.Second,
			overallTimeout: 10 * time.Second,
			retryDelay:     time.Millisecond,
		})
		result <- err
	}()
	waitForFile(t, readyPath)
	started := time.Now()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start() error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup did not stop promptly after cancellation")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancellation cleanup took %s", elapsed)
	}
	if got := countLogLines(t, logPath); got != 1 {
		t.Fatalf("canceled startup attempts = %d, want 1", got)
	}
}

func TestStartParsesStandardDevToolsEndpointWithoutWaitingForTimeout(t *testing.T) {
	requirePOSIXShell(t)
	executable := writeFakeChrome(t, `
printf 'DevTools listening on ws://127.0.0.1:1/devtools/browser/private-id\n' >&2
exec tail -f /dev/null
`)
	started := time.Now()
	_, err := startWithPolicy(context.Background(), executable, startupPolicy{
		attempts:       1,
		attemptTimeout: time.Second,
		overallTimeout: 5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected fake CDP dial failure")
	}
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("standard endpoint was not parsed promptly: %s", elapsed)
	}
	if strings.Contains(err.Error(), "private-id") || strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("endpoint diagnostic was not redacted: %v", err)
	}
}

func TestStartChromeSmoke(t *testing.T) {
	chrome := strings.TrimSpace(os.Getenv("GOSX_CHROME_BIN"))
	if chrome == "" {
		t.Skip("set GOSX_CHROME_BIN to run the bounded Chrome startup smoke")
	}
	for attempt := 0; attempt < 3; attempt++ {
		browser, err := Start(t.Context(), chrome, "--no-sandbox", "--disable-gpu")
		if err != nil {
			t.Fatalf("start Chrome iteration %d: %v", attempt+1, err)
		}
		var value int
		var visibility string
		var painted bool
		ctx, cancel := context.WithTimeout(browser.Context, 10*time.Second)
		runErr := chromedp.Run(ctx,
			chromedp.Navigate("data:text/html,<!doctype html><body>Chrome startup smoke</body>"),
			chromedp.Evaluate(`1 + 1`, &value),
			chromedp.Evaluate(`document.visibilityState`, &visibility),
			chromedp.Evaluate(`window.painted = false; requestAnimationFrame(() => { window.painted = true; })`, nil),
			chromedp.Poll(`window.painted`, &painted, chromedp.WithPollingTimeout(5*time.Second)),
		)
		cancel()
		browser.Close()
		if runErr != nil || value != 2 || visibility != "visible" || !painted {
			t.Fatalf("Chrome iteration %d: value=%d visibility=%s painted=%v err=%v diagnostics=%s", attempt+1, value, visibility, painted, runErr, browser.Diagnostics())
		}
		if _, err := os.Stat(browser.process.profileDir); !os.IsNotExist(err) {
			t.Fatalf("Chrome iteration %d left its profile: %v", attempt+1, err)
		}
	}
}

func fastPolicy(attempts int) startupPolicy {
	return startupPolicy{
		attempts:       attempts,
		attemptTimeout: 250 * time.Millisecond,
		overallTimeout: 5 * time.Second,
		retryDelay:     time.Millisecond,
	}
}

func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake Chrome process tests require a POSIX shell")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("fake Chrome process tests require /bin/sh")
	}
}

func writeFakeChrome(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-chrome")
	script := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake Chrome: %v", err)
	}
	return path
}

func countLogLines(t *testing.T, path string) int {
	t.Helper()
	content := strings.TrimSpace(readFile(t, path))
	if content == "" {
		return 0
	}
	return len(strings.Split(content, "\n"))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("timed out waiting for %s", path))
}
