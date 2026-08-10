//go:build browser

package visual

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cdpRuntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type visualTargetRAFProbe struct {
	Visibility string `json:"visibility"`
	Hidden     bool   `json:"hidden"`
	Before     int    `json:"before"`
	After      int    `json:"after"`
	Fired      bool   `json:"fired"`
}

func TestActivateVisualTargetRestoresVisibilityAndRAF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head><title>visual target %s</title></head>
<body>
<main id="ready">target %s</main>
<script>
window.__visualFrames = 0;
window.__visualLastFrame = 0;
function tick(ts) {
  window.__visualFrames++;
  window.__visualLastFrame = ts;
  requestAnimationFrame(tick);
}
requestAnimationFrame(tick);
</script>
</body>
</html>`, r.URL.Query().Get("tab"), r.URL.Query().Get("tab"))
	}))
	defer server.Close()

	allocCtx, allocCancel, err := newAllocator(context.Background())
	if err != nil {
		t.Skipf("browser allocator unavailable: %v", err)
	}
	defer allocCancel()

	firstCtx, firstCancel := chromedp.NewContext(allocCtx)
	defer firstCancel()
	firstRunCtx, firstRunCancel := context.WithTimeout(firstCtx, 10*time.Second)
	defer firstRunCancel()
	if err := chromedp.Run(firstRunCtx,
		activateVisualTargetAction(),
		chromedp.EmulateViewport(800, 600),
		chromedp.Navigate(server.URL+"?tab=first"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("first target setup: %v", err)
	}
	firstProbe, err := probeFreshRAF(firstRunCtx, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("first target rAF probe: %v", err)
	}
	if firstProbe.Visibility != "visible" || !firstProbe.Fired {
		t.Skipf("foreground target does not deliver fresh rAF in this Chrome: %+v", firstProbe)
	}

	secondCtx, secondCancel := chromedp.NewContext(firstCtx)
	defer secondCancel()
	secondRunCtx, secondRunCancel := context.WithTimeout(secondCtx, 10*time.Second)
	defer secondRunCancel()
	if err := chromedp.Run(secondRunCtx,
		chromedp.EmulateViewport(800, 600),
		chromedp.Navigate(server.URL+"?tab=second"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("second target setup without activation: %v", err)
	}
	if err := chromedp.Run(firstRunCtx,
		activateVisualTargetAction(),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatalf("reactivate first target: %v", err)
	}

	beforeActivation, err := probeFreshRAF(secondRunCtx, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("second target pre-activation rAF probe: %v", err)
	}
	if beforeActivation.Visibility != "hidden" || !beforeActivation.Hidden || beforeActivation.Fired {
		t.Skipf("two-tab hidden branch not reproduced by this Chrome: %+v", beforeActivation)
	}

	if err := chromedp.Run(secondRunCtx, activateVisualTargetAction()); err != nil {
		t.Fatalf("activate second target: %v", err)
	}
	afterActivation, err := waitForVisibleRAF(secondRunCtx, 2*time.Second)
	if err != nil {
		t.Fatalf("second target after activation: %v; before activation was %+v", err, beforeActivation)
	}
	if afterActivation.Visibility != "visible" || afterActivation.Hidden || !afterActivation.Fired {
		t.Fatalf("second target after activation = %+v, want visible target with fresh rAF", afterActivation)
	}
}

func waitForVisibleRAF(ctx context.Context, timeout time.Duration) (visualTargetRAFProbe, error) {
	deadline := time.Now().Add(timeout)
	var last visualTargetRAFProbe
	var lastErr error
	for time.Now().Before(deadline) {
		probe, err := probeFreshRAF(ctx, 300*time.Millisecond)
		if err == nil && probe.Visibility == "visible" && !probe.Hidden && probe.Fired {
			return probe, nil
		}
		last = probe
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		return last, lastErr
	}
	return last, fmt.Errorf("target did not become visible with live rAF: %+v", last)
}

func probeFreshRAF(ctx context.Context, timeout time.Duration) (visualTargetRAFProbe, error) {
	ms := timeout.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	script := fmt.Sprintf(`(async function() {
  const before = window.__visualFrames || 0;
  const fired = await Promise.race([
    new Promise((resolve) => requestAnimationFrame(() => resolve(true))),
    new Promise((resolve) => setTimeout(() => resolve(false), %d))
  ]);
  return {
    visibility: document.visibilityState,
    hidden: document.hidden,
    before: before,
    after: window.__visualFrames || 0,
    fired: fired
  };
})()`, ms)
	var probe visualTargetRAFProbe
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &probe, func(p *cdpRuntime.EvaluateParams) *cdpRuntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		return visualTargetRAFProbe{}, err
	}
	return probe, nil
}
