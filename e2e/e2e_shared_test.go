//go:build e2e

// Shared harness for the browser e2e suites (ported from the retired
// playwright-based e2e/*.test.mjs files). Each suite boots the gosx-docs
// example app with `go run ./cmd/gosx dev ./examples/gosx-docs`, drives a
// locally installed Chrome/Chromium through chromedp, and tears both down.
//
// Run with: go test -tags e2e ./e2e
package e2e

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// e2eChromePath resolves the browser binary: GOSX_E2E_CHROME first,
// PLAYWRIGHT_CHROMIUM_EXECUTABLE for backwards compatibility with the old
// harness, CHROME_PATH, then well-known names on PATH. Skips when absent.
func e2eChromePath(t *testing.T) string {
	t.Helper()
	for _, env := range []string{"GOSX_E2E_CHROME", "PLAYWRIGHT_CHROMIUM_EXECUTABLE", "CHROME_PATH"} {
		if path := os.Getenv(env); path != "" {
			return path
		}
	}
	return findChrome(t)
}

func e2eRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// docsApp is a running `gosx dev ./examples/gosx-docs` process.
type docsApp struct {
	baseURL string
	logs    *logBuffer
}

type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// startDocsApp launches the gosx-docs dev server on the given base URL and
// waits for /readyz. The process group is killed on test cleanup.
func startDocsApp(t *testing.T, baseURL string) *docsApp {
	t.Helper()
	root := e2eRepoRoot(t)
	port := baseURL[strings.LastIndex(baseURL, ":")+1:]

	logs := &logBuffer{}
	cmd := exec.Command("go", "run", "./cmd/gosx", "dev", "./examples/gosx-docs")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PORT="+port,
		"SESSION_SECRET=gosx-e2e-session-secret",
	)
	cmd.Stdout = logs
	cmd.Stderr = logs
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gosx dev: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			<-done
		}
	})

	app := &docsApp{baseURL: baseURL, logs: logs}
	if err := waitForHealthy(baseURL+"/readyz", 45*time.Second); err != nil {
		t.Fatalf("%v\n\nLogs:\n%s", err, logs.String())
	}
	return app
}

func waitForHealthy(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastError := ""
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			status := resp.StatusCode
			resp.Body.Close()
			if status < 500 {
				return nil
			}
			lastError = fmt.Sprintf("status %d", status)
		} else {
			lastError = err.Error()
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s: %s", url, lastError)
}

func freeE2EPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func netListen(t *testing.T) (net.Listener, error) {
	t.Helper()
	return net.Listen("tcp", "127.0.0.1:0")
}

// browserPage is a chromedp tab with console/pageerror/request capture.
type browserPage struct {
	ctx        context.Context
	mu         sync.Mutex
	console    strings.Builder
	pageErrors []string
	requests   []string
}

func (p *browserPage) Console() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.console.String()
}

func (p *browserPage) PageErrors() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pageErrors...)
}

func (p *browserPage) Requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requests...)
}

func (p *browserPage) anyRequest(match func(string) bool) bool {
	for _, url := range p.Requests() {
		if match(url) {
			return true
		}
	}
	return false
}

// newBrowserPage starts a fresh headless browser with the given extra flags
// and returns an event-instrumented tab. initScript, when non-empty, runs
// before every document in the tab (playwright addInitScript equivalent).
func newBrowserPage(t *testing.T, chrome string, extraFlags map[string]any, width, height int, initScript string, timeout time.Duration) *browserPage {
	t.Helper()
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(width, height),
	)
	for name, value := range extraFlags {
		allocOpts = append(allocOpts, chromedp.Flag(name, value))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	ctx, timeoutCancel := context.WithTimeout(browserCtx, timeout)
	t.Cleanup(func() {
		timeoutCancel()
		browserCancel()
		allocCancel()
	})

	page := &browserPage{ctx: ctx}
	attachPageListeners(ctx, page)

	actions := []chromedp.Action{network.Enable()}
	if initScript != "" {
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := cdppage.AddScriptToEvaluateOnNewDocument(initScript).Do(ctx)
			return err
		}))
	}
	if err := chromedp.Run(ctx, actions...); err != nil {
		if browserUnavailable(err) {
			t.Skipf("chrome could not start in this environment: %v", err)
		}
		t.Fatalf("initialize browser: %v", err)
	}
	return page
}

func attachPageListeners(ctx context.Context, page *browserPage) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			var parts []string
			for _, arg := range ev.Args {
				if arg.Value != nil {
					parts = append(parts, string(arg.Value))
				} else if arg.Description != "" {
					parts = append(parts, arg.Description)
				}
			}
			page.mu.Lock()
			fmt.Fprintf(&page.console, "[console.%s] %s\n", ev.Type, strings.Join(parts, " "))
			page.mu.Unlock()
		case *cdpruntime.EventExceptionThrown:
			msg := ""
			if ev.ExceptionDetails != nil {
				msg = ev.ExceptionDetails.Error()
			}
			page.mu.Lock()
			page.pageErrors = append(page.pageErrors, msg)
			fmt.Fprintf(&page.console, "[pageerror] %s\n", msg)
			page.mu.Unlock()
		case *network.EventRequestWillBeSent:
			page.mu.Lock()
			page.requests = append(page.requests, ev.Request.URL)
			page.mu.Unlock()
		}
	})
}

// navigate loads a URL and returns the HTTP status of the main document.
func (p *browserPage) navigate(t *testing.T, url string) int {
	t.Helper()
	var status int64
	var statusMu sync.Mutex
	listenCtx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	chromedp.ListenTarget(listenCtx, func(ev any) {
		if resp, ok := ev.(*network.EventResponseReceived); ok {
			if resp.Type == network.ResourceTypeDocument {
				statusMu.Lock()
				status = resp.Response.Status
				statusMu.Unlock()
			}
		}
	})
	if err := chromedp.Run(p.ctx, chromedp.Navigate(url)); err != nil {
		t.Fatalf("navigate %s: %v", url, err)
	}
	statusMu.Lock()
	defer statusMu.Unlock()
	return int(status)
}

// eval evaluates a JS expression, decoding the result into out (pass nil to
// ignore the result). Promise results are awaited.
func (p *browserPage) eval(t *testing.T, expr string, out any) {
	t.Helper()
	if err := p.tryEval(expr, out); err != nil {
		t.Fatalf("evaluate %q: %v", truncateForLog(expr), err)
	}
}

func (p *browserPage) tryEval(expr string, out any) error {
	opts := []chromedp.EvaluateOption{
		func(ep *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return ep.WithAwaitPromise(true)
		},
	}
	if out == nil {
		var ignored any
		return chromedp.Run(p.ctx, chromedp.Evaluate(expr, &ignored, opts...))
	}
	return chromedp.Run(p.ctx, chromedp.Evaluate(expr, out, opts...))
}

// waitFor polls a JS boolean expression until it is true or the timeout
// elapses (playwright waitForFunction / waitForSelector equivalent).
func (p *browserPage) waitFor(t *testing.T, expr string, timeout time.Duration, what string) {
	t.Helper()
	if !p.pollFor(expr, timeout) {
		t.Fatalf("timed out after %s waiting for %s\n\nConsole:\n%s", timeout, what, p.Console())
	}
}

func (p *browserPage) pollFor(expr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var ok bool
		if err := p.tryEval(expr, &ok); err == nil && ok {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// screenshotElement captures a PNG of the first element matching the CSS
// selector.
func (p *browserPage) screenshotElement(t *testing.T, selector string) []byte {
	t.Helper()
	var buf []byte
	if err := chromedp.Run(p.ctx, chromedp.Screenshot(selector, &buf, chromedp.ByQuery)); err != nil {
		t.Fatalf("screenshot %s: %v", selector, err)
	}
	return buf
}

func truncateForLog(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
