//go:build e2e && drivespike

// Package drivespike is a throwaway experiment (spec-gosx-agent-driver v0.1,
// Phase 0): does a probe injected over DevTools give a page one virtual clock
// in real Chrome? Each test logs "RESULT" lines; a failed check fails the test
// but the test carries on so every check is reported.
//
//	mac-gpu-run <worktree> go test -tags 'e2e drivespike' -count=1 -run <Test> ./e2e/drivespike/
//	GOSX_E2E_CHROME=<chrome> local-heavy go test -tags 'e2e drivespike' -count=1 -run <Test> ./e2e/drivespike/
//	gpu-chrome.sh run -- go test -tags 'e2e drivespike' -count=1 -run <Test> ./e2e/drivespike/
package drivespike

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/internal/chrometest"
)

//go:embed probe.js
var probeJS string

const interval = 1000.0 / 60

// frameAt is the probe's frame grid: frame k starts at k*interval ms, rounded to 1 µs.
func frameAt(k int) float64 { return math.Round(float64(k)*interval*1000) / 1000 }

// probeSource returns the probe with its config object filled in.
func probeSource(cfg map[string]any) string {
	data, _ := json.Marshal(cfg)
	return strings.Replace(probeJS, "/*DRIVE_CONFIG*/{}", string(data), 1)
}

// ---- browser selection

func browserLabel() string {
	chrome := os.Getenv("GOSX_E2E_CHROME")
	switch {
	case filepath.Base(chrome) == "fake-chrome.sh":
		return "windows"
	case runtime.GOOS == "darwin":
		return "mac"
	default:
		return "linux"
	}
}

func windowsRelay() bool { return filepath.Base(os.Getenv("GOSX_E2E_CHROME")) == "fake-chrome.sh" }

func gpuRunner() bool { return windowsRelay() || os.Getenv("GPU_CHROME_ENDPOINT") != "" }

func chromePath(t *testing.T) string {
	t.Helper()
	for _, env := range []string{"GOSX_E2E_CHROME", "CHROME_PATH"} {
		if p := os.Getenv(env); p != "" {
			return p
		}
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no Chrome")
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// expose makes a loopback test server reachable from Windows Chrome through the relay.
func expose(t *testing.T, loopbackURL string) string {
	t.Helper()
	if !windowsRelay() {
		return loopbackURL
	}
	target := strings.TrimPrefix(loopbackURL, "http://")
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			in, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				out, err := net.Dial("tcp", target)
				if err != nil {
					_ = in.Close()
					return
				}
				go func() { _, _ = io.Copy(out, in); _ = out.Close() }()
				_, _ = io.Copy(in, out)
				_ = in.Close()
			}()
		}
	}()
	return fmt.Sprintf("http://localhost:%d", l.Addr().(*net.TCPAddr).Port)
}

func serve(t *testing.T, h http.Handler) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return expose(t, s.URL)
}

// ---- browser and tabs

type spikeBrowser struct {
	ctx     context.Context
	b       *chrometest.Browser
	Product string
	UA      string
}

// cpuGLFlags give headless Linux Chrome WebGL through SwiftShader. GPU runners ignore flags.
var cpuGLFlags = []string{"--use-gl=angle", "--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--enable-webgl", "--enable-unsafe-webgpu", "--ignore-gpu-blocklist"}

func startBrowser(t *testing.T, flags ...string) *spikeBrowser {
	t.Helper()
	args := append([]string{"--no-sandbox", "--mute-audio", "--window-size=1000,800", "--autoplay-policy=no-user-gesture-required"}, flags...)
	b, err := chrometest.Start(t.Context(), chromePath(t), args...)
	if err != nil {
		t.Fatalf("start Chrome: %v", err)
	}
	t.Cleanup(b.Close)
	sb := &spikeBrowser{ctx: b.Context, b: b}
	if err := chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
		_, product, _, ua, _, err := browser.GetVersion().Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		sb.Product, sb.UA = product, ua
		return err
	})); err != nil {
		t.Fatalf("version: %v", err)
	}
	t.Logf("CHROME browser=%s product=%q headlessUA=%v ua=%q", browserLabel(), sb.Product, strings.Contains(sb.UA, "Headless"), sb.UA)
	return sb
}

type tab struct {
	t        *testing.T
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	console  strings.Builder
	errors   []string
	inflight map[network.RequestID]string
}

func (b *spikeBrowser) newTab(t *testing.T, timeout time.Duration) *tab {
	t.Helper()
	ctx, cancel := chromedp.NewContext(b.ctx)
	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	tb := &tab{t: t, ctx: ctx, cancel: func() { cancelTimeout(); cancel() }, inflight: map[network.RequestID]string{}}
	chromedp.ListenTarget(ctx, func(ev any) {
		tb.mu.Lock()
		defer tb.mu.Unlock()
		switch ev := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			var parts []string
			for _, a := range ev.Args {
				if a.Value != nil {
					parts = append(parts, string(a.Value))
				} else {
					parts = append(parts, a.Description)
				}
			}
			fmt.Fprintf(&tb.console, "[%s] %s\n", ev.Type, strings.Join(parts, " "))
		case *cdpruntime.EventExceptionThrown:
			msg := ev.ExceptionDetails.Error()
			if ev.ExceptionDetails.Exception != nil && ev.ExceptionDetails.Exception.Description != "" {
				msg = ev.ExceptionDetails.Exception.Description
			}
			tb.errors = append(tb.errors, msg)
			fmt.Fprintf(&tb.console, "[pageerror] %s\n", msg)
		case *network.EventRequestWillBeSent:
			if ev.Type != network.ResourceTypeEventSource {
				tb.inflight[ev.RequestID] = ev.Request.URL
			}
		case *network.EventLoadingFinished:
			delete(tb.inflight, ev.RequestID)
		case *network.EventLoadingFailed:
			delete(tb.inflight, ev.RequestID)
		}
	})
	if err := chromedp.Run(ctx, network.Enable()); err != nil {
		t.Fatalf("new tab: %v", err)
	}
	// A background tab gets no real frames: bring this one to the front.
	if c := chromedp.FromContext(ctx); c != nil && c.Target != nil {
		_ = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return target.ActivateTarget(c.Target.TargetID).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		}))
	}
	t.Cleanup(tb.cancel)
	return tb
}

func (tb *tab) Console() string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	s := tb.console.String()
	if len(s) > 4000 {
		s = s[len(s)-4000:]
	}
	return s
}

func (tb *tab) Errors() []string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return append([]string(nil), tb.errors...)
}

func (tb *tab) Inflight() []string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	var out []string
	for _, u := range tb.inflight {
		out = append(out, u)
	}
	return out
}

// waitNetIdle waits until no request the page started is in flight (settle rule 1).
func (tb *tab) waitNetIdle(bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if len(tb.Inflight()) == 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (tb *tab) addProbe(cfg map[string]any) {
	tb.t.Helper()
	if cfg == nil {
		cfg = map[string]any{}
	}
	if _, ok := cfg["wall"]; !ok {
		cfg["wall"] = float64(time.Now().UnixMilli())
	}
	if err := chromedp.Run(tb.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(probeSource(cfg)).Do(ctx)
		return err
	})); err != nil {
		tb.t.Fatalf("add probe: %v", err)
	}
}

func (tb *tab) navigate(url string) {
	tb.t.Helper()
	err := chromedp.Run(tb.ctx, chromedp.Navigate(url))
	for i := 0; err != nil && windowsRelay() && strings.Contains(err.Error(), "ERR_CONNECTION_REFUSED") && i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		err = chromedp.Run(tb.ctx, chromedp.Navigate(url))
	}
	if err != nil {
		tb.t.Fatalf("navigate %s: %v", url, err)
	}
}

func evalIn(ctx context.Context, expr string, out any) error {
	opt := func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }
	if out == nil {
		var ignored any
		return chromedp.Run(ctx, chromedp.Evaluate(expr, &ignored, opt))
	}
	return chromedp.Run(ctx, chromedp.Evaluate(expr, out, opt))
}

func (tb *tab) tryEval(expr string, out any) error { return evalIn(tb.ctx, expr, out) }

func (tb *tab) eval(expr string, out any) {
	tb.t.Helper()
	if err := tb.tryEval(expr, out); err != nil {
		tb.t.Fatalf("eval %.120q: %v\nconsole:\n%s", expr, err, tb.Console())
	}
}

func (tb *tab) waitFor(expr string, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		var ok bool
		if err := tb.tryEval(expr, &ok); err == nil && ok {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

type stepResult struct {
	From            float64 `json:"from"`
	To              float64 `json:"to"`
	Advanced        float64 `json:"advanced"`
	Navigated       bool    `json:"navigated"`
	RealMS          float64 `json:"realMS"`
	Boundaries      int     `json:"boundaries"`
	Frames          int     `json:"frames"`
	FrameCallbacks  int     `json:"frameCallbacks"`
	TimersRun       int     `json:"timersRun"`
	Delivered       int     `json:"delivered"`
	RealFrames      int     `json:"realFrames"`
	PresentTimeouts int     `json:"presentTimeouts"`
	GPUSubmits      int     `json:"gpuSubmits"`
	Navigations     int     `json:"-"`
	WallMS          float64 `json:"-"`
}

func (s *stepResult) add(r stepResult) {
	s.To = r.To
	s.Boundaries += r.Boundaries
	s.Frames += r.Frames
	s.FrameCallbacks += r.FrameCallbacks
	s.TimersRun += r.TimersRun
	s.Delivered += r.Delivered
	s.RealFrames += r.RealFrames
	s.PresentTimeouts += r.PresentTimeouts
	s.GPUSubmits += r.GPUSubmits
	s.RealMS += r.RealMS
}

func navigationError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, m := range []string{"Execution context was destroyed", "Inspected target navigated", "Cannot find context", "context with specified id", "uniqueContextId", "Promise was collected"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// step advances the tab's clock by ms. When the page navigates during the step
// (a reload from a timer), it waits for the new document's probe and finishes
// the remaining time there.
func (tb *tab) step(ms float64) stepResult {
	tb.t.Helper()
	var now float64
	tb.eval(`__drive.now()`, &now)
	return tb.stepTo(now + ms)
}

func (tb *tab) frames(n int) stepResult {
	tb.t.Helper()
	var st struct{ FrameIndex int }
	tb.eval(`__drive.stats()`, &st)
	return tb.stepTo(frameAt(st.FrameIndex + n))
}

func (tb *tab) stepTo(target float64) stepResult {
	tb.t.Helper()
	start := time.Now()
	total := stepResult{}
	first := true
	for attempt := 0; attempt < 8; attempt++ {
		var now float64
		var doc string
		tb.eval(`__drive.now()`, &now)
		tb.eval(`__drive.docId`, &doc)
		if first {
			total.From, first = now, false
		}
		if target-now < 1e-9 {
			total.To = now
			break
		}
		var r stepResult
		err := tb.tryEval(fmt.Sprintf(`__drive.step(%v)`, target-now), &r)
		if err != nil && !navigationError(err) {
			tb.t.Fatalf("step: %v\nconsole:\n%s", err, tb.Console())
		}
		total.add(r)
		if err == nil && !r.Navigated {
			break
		}
		total.Navigations++
		if !tb.waitFor(fmt.Sprintf(`!!window.__drive && __drive.docId !== %q && document.readyState !== "loading"`, doc), 10*time.Second) {
			tb.t.Fatalf("no new document after a navigation during a step\nconsole:\n%s", tb.Console())
		}
	}
	total.Advanced = total.To - total.From
	total.WallMS = float64(time.Since(start).Microseconds()) / 1000
	return total
}

// ---- results

type results struct {
	t   *testing.T
	exp string
	mu  sync.Mutex
	n   map[string][2]int
}

func newResults(t *testing.T, exp string) *results {
	return &results{t: t, exp: exp, n: map[string][2]int{}}
}

// check logs one RESULT line and fails the test (without stopping it) when ok is false.
func (r *results) check(name string, ok bool, format string, args ...any) bool {
	r.t.Helper()
	status := "PASS"
	if !ok {
		status = "FAIL"
	}
	r.mu.Lock()
	c := r.n[name]
	if ok {
		c[0]++
	} else {
		c[1]++
	}
	r.n[name] = c
	r.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	r.t.Logf("RESULT exp=%s browser=%s check=%q %s %s", r.exp, browserLabel(), name, status, msg)
	if !ok {
		r.t.Errorf("exp %s: %s: %s", r.exp, name, msg)
	}
	return ok
}

// quiet records a check without logging a line (for runs repeated many times).
func (r *results) quiet(name string, ok bool, format string, args ...any) bool {
	r.t.Helper()
	r.mu.Lock()
	c := r.n[name]
	if ok {
		c[0]++
	} else {
		c[1]++
	}
	r.n[name] = c
	r.mu.Unlock()
	if !ok {
		msg := fmt.Sprintf(format, args...)
		r.t.Logf("RESULT exp=%s browser=%s check=%q FAIL %s", r.exp, browserLabel(), name, msg)
		r.t.Errorf("exp %s: %s: %s", r.exp, name, msg)
	}
	return ok
}

func (r *results) summary() {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.n))
	for k := range r.n {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		c := r.n[k]
		r.t.Logf("SUMMARY exp=%s browser=%s check=%q pass=%d fail=%d", r.exp, browserLabel(), k, c[0], c[1])
	}
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 1500 {
		s = s[:1500] + "..."
	}
	return s
}

var errSkip = errors.New("skip")
