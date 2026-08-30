package docs

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type chromeStartupState int

const (
	chromeStartupPending chromeStartupState = iota
	chromeStartupBound
	chromeStartupFailed
	chromeStartupTimedOut
)

type chromeStartupDeadline struct {
	mu              sync.Mutex
	state           chromeStartupState
	cancelBrowser   context.CancelFunc
	timer           *time.Timer
	timerDone       chan struct{}
	stopOnce        sync.Once
	beforeTimerWins func()
	beforeTimerJoin func()
}

func newChromeStartupDeadline(timeout time.Duration, cancelBrowser context.CancelFunc) *chromeStartupDeadline {
	return newChromeStartupDeadlineForTest(timeout, cancelBrowser, nil, nil)
}

func newChromeStartupDeadlineForTest(timeout time.Duration, cancelBrowser context.CancelFunc, beforeTimerWins func(), beforeTimerJoin func()) *chromeStartupDeadline {
	deadline := &chromeStartupDeadline{
		state:           chromeStartupPending,
		cancelBrowser:   cancelBrowser,
		timerDone:       make(chan struct{}),
		beforeTimerWins: beforeTimerWins,
		beforeTimerJoin: beforeTimerJoin,
	}
	deadline.timer = time.AfterFunc(timeout, func() {
		defer close(deadline.timerDone)
		if deadline.beforeTimerWins != nil {
			deadline.beforeTimerWins()
		}
		deadline.mu.Lock()
		defer deadline.mu.Unlock()
		if deadline.state != chromeStartupPending {
			return
		}
		deadline.state = chromeStartupTimedOut
		deadline.cancelBrowser()
	})
	return deadline
}

func (deadline *chromeStartupDeadline) finish(success bool) chromeStartupState {
	deadline.stopOnce.Do(func() {
		if !deadline.timer.Stop() {
			if deadline.beforeTimerJoin != nil {
				deadline.beforeTimerJoin()
			}
			<-deadline.timerDone
		}
	})
	deadline.mu.Lock()
	defer deadline.mu.Unlock()
	if deadline.state == chromeStartupPending {
		if success {
			deadline.state = chromeStartupBound
		} else {
			deadline.state = chromeStartupFailed
		}
	}
	return deadline.state
}

func requireChromeStartupTestSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for chrome startup test signal %q", name)
	}
}

func requireChromeStartupTestState(t *testing.T, ch <-chan chromeStartupState, name string) chromeStartupState {
	t.Helper()
	select {
	case state := <-ch:
		return state
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for chrome startup test state %q", name)
		return chromeStartupPending
	}
}

func TestChromeStartupDeadlineJoinsTimerBeforeSuccess(t *testing.T) {
	cancelled := make(chan struct{})
	cancelBrowser := func() {
		close(cancelled)
	}
	timerEntered := make(chan struct{})
	joinStarted := make(chan struct{})
	releaseTimer := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseTimer)
		})
	}
	defer release()
	deadline := newChromeStartupDeadlineForTest(0, cancelBrowser, func() {
		close(timerEntered)
		<-releaseTimer
	}, func() {
		close(joinStarted)
	})
	requireChromeStartupTestSignal(t, timerEntered, "timer callback entered")

	finished := make(chan chromeStartupState, 1)
	go func() {
		finished <- deadline.finish(true)
	}()

	requireChromeStartupTestSignal(t, joinStarted, "finish reached timer callback join")
	select {
	case state := <-finished:
		t.Fatalf("startup finish returned %v before joining in-flight timer callback", state)
	default:
	}

	release()
	if state := requireChromeStartupTestState(t, finished, "finish after timer release"); state != chromeStartupTimedOut {
		t.Fatalf("startup boundary did not fail closed after timer won; state=%v", state)
	}
	requireChromeStartupTestSignal(t, cancelled, "browser canceled by startup timeout")
	if state := deadline.finish(true); state != chromeStartupTimedOut {
		t.Fatalf("startup timeout state was not terminal; got %v", state)
	}
}

func TestChromeStartupDeadlineSuccessIsTerminal(t *testing.T) {
	cancelBrowser := func() {
		t.Fatal("startup timer canceled browser after successful bind")
	}
	deadline := newChromeStartupDeadline(time.Hour, cancelBrowser)
	if state := deadline.finish(true); state != chromeStartupBound {
		t.Fatalf("startup success was not recorded; state=%v", state)
	}
	if state := deadline.finish(false); state != chromeStartupBound {
		t.Fatalf("startup success state was not terminal; got %v", state)
	}
}

func waterDiagSource(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "public", "water-diag.js"))
	if err != nil {
		t.Fatalf("read water-diag.js: %v", err)
	}
	return source
}

func TestWaterDiagAvoidsHTMLParsingSinks(t *testing.T) {
	source := string(waterDiagSource(t))
	for _, sink := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write"} {
		if strings.Contains(source, sink) {
			t.Fatalf("water-diag.js uses HTML parsing sink %q", sink)
		}
	}
	for _, textAssignment := range []string{
		"name.textContent = String(label)",
		"result.textContent = String(value)",
		"element.textContent = String(text)",
	} {
		if !strings.Contains(source, textAssignment) {
			t.Fatalf("water-diag.js lost text-only DOM assignment %q", textAssignment)
		}
	}
}

func TestWaterDiagRendersQueryAndAttributeValuesAsText(t *testing.T) {
	const (
		chromeStartupTimeout = 30 * time.Second
		assertionTimeout     = 15 * time.Second
	)

	chromePath := ""
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			chromePath = path
			break
		}
	}
	if chromePath == "" {
		t.Skip("Chrome/Chromium is unavailable; text-sink source contract still ran")
	}

	source := waterDiagSource(t)
	queryAttack := `<img data-water-query-attack src=x onerror="window.__waterQueryAttack=true">QUERY_ATTACK_MARKER`
	attributeAttack := `<svg data-water-attribute-attack onload="window.__waterAttributeAttack=true">ATTRIBUTE_ATTACK_MARKER</svg>`

	mux := http.NewServeMux()
	mux.HandleFunc("/water-diag.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(source)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html><body>
<script>window.__waterQueryAttack=false;window.__waterAttributeAttack=false;</script>
<div data-gosx-scene3d data-gosx-scene3d-webgpu-adapter="` + html.EscapeString(attributeAttack) + `">
  <canvas width="8" height="8"></canvas>
</div>
<script src="/water-diag.js"></script>
</body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	allocOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.Headless,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
	)
	allocContext, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOptions...)
	defer cancelAlloc()
	browserContext, cancelBrowser := chromedp.NewContext(allocContext)
	defer cancelBrowser()

	// Bind the browser under a separate startup deadline. The deadline protocol
	// joins any in-flight timeout callback before the test proceeds, so startup
	// success cannot be canceled later by a timer that already began firing. The
	// security assertion below keeps its own fixed deadline, so a slow CI Chrome
	// launch cannot spend the entire payload-rendering/XSS assertion budget
	// before navigation starts.
	startupDeadline := newChromeStartupDeadline(chromeStartupTimeout, cancelBrowser)
	if err := chromedp.Run(browserContext); err != nil {
		if startupDeadline.finish(false) == chromeStartupTimedOut {
			t.Fatalf("start Chrome for water diagnostics within %s: %v", chromeStartupTimeout, err)
		}
		t.Fatalf("start Chrome for water diagnostics: %v", err)
	}
	if startupDeadline.finish(true) == chromeStartupTimedOut {
		t.Fatalf("start Chrome for water diagnostics within %s: startup timer fired while binding browser", chromeStartupTimeout)
	}

	ctx, cancelAssertion := context.WithTimeout(browserContext, assertionTimeout)
	defer cancelAssertion()

	var got struct {
		Text            string `json:"text"`
		QueryNode       bool   `json:"queryNode"`
		AttributeNode   bool   `json:"attributeNode"`
		QueryExecuted   bool   `json:"queryExecuted"`
		AttributeRan    bool   `json:"attributeRan"`
		UnexpectedNodes int    `json:"unexpectedNodes"`
	}
	target := server.URL + "/?diag=1&meshRes=" + url.QueryEscape(queryAttack)
	err := chromedp.Run(ctx,
		chromedp.Navigate(target),
		chromedp.WaitReady(`[data-water-diag]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const overlay = document.querySelector("[data-water-diag]");
			return {
				text: overlay.textContent,
				queryNode: Boolean(overlay.querySelector("[data-water-query-attack]")),
				attributeNode: Boolean(overlay.querySelector("[data-water-attribute-attack]")),
				queryExecuted: Boolean(window.__waterQueryAttack),
				attributeRan: Boolean(window.__waterAttributeAttack),
				unexpectedNodes: overlay.querySelectorAll("img,svg,script").length,
			};
		})()`, &got),
	)
	if err != nil {
		t.Fatalf("run water diagnostics in Chrome: %v", err)
	}
	if !strings.Contains(got.Text, queryAttack) {
		t.Fatalf("query payload was not preserved as text; overlay text = %q", got.Text)
	}
	if !strings.Contains(got.Text, attributeAttack) {
		t.Fatalf("attribute payload was not preserved as text; overlay text = %q", got.Text)
	}
	if got.QueryNode || got.AttributeNode || got.QueryExecuted || got.AttributeRan || got.UnexpectedNodes != 0 {
		t.Fatalf("attacker-controlled values reached executable markup: %+v", got)
	}
}
