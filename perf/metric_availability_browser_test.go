//go:build browser

package perf

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestDriverNavigateActivatesProfilingTarget(t *testing.T) {
	d := requireDriver(t, 30*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatal(err)
	}
	other, cancel := chromedp.NewContext(d.Context())
	defer cancel()
	if err := chromedp.Run(other, chromedp.Navigate("about:blank"), page.BringToFront()); err != nil {
		t.Fatal(err)
	}
	var hidden bool
	if err := d.Evaluate(`document.visibilityState === "hidden"`, &hidden); err != nil {
		t.Fatal(err)
	}
	if !hidden {
		t.Fatal("fixture did not background the profiling target")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><h1>Foreground performance fixture</h1><script>requestAnimationFrame(function tick(){performance.measure('scene3d-render',{start:Math.max(0,performance.now()-1)});requestAnimationFrame(tick)});</script>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(d.Context(), chromedp.Poll(`document.visibilityState === 'visible' && window.__gosx_perf.availability.lcp && window.__gosx_perf.frameCount >= 2`, nil, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("foreground paint/frame samples: %v", err)
	}
	p, err := CollectPageReport(d, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lcp", "scene_p95", "frame_count"} {
		if value, found := ResolvePageMetric(name, p); !found || value <= 0 {
			t.Errorf("%s should have actual samples, got %v,%v", name, value, found)
		}
	}
	if !p.MetricAvailability.LongTasks {
		t.Fatal("long-task observer was not available in the foreground")
	}
}

func TestCollectPageReportUnsupportedObserversAreUnavailable(t *testing.T) {
	d := requireDriver(t, 30*time.Second)
	if err := chromedp.Run(d.Context(), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(`window.PerformanceObserver = undefined;`).Do(ctx)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	if err := InjectDriver(d); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><h1>Unsupported observer fixture</h1>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatal(err)
	}
	p, err := CollectPageReport(d, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lcp", "cls", "fid", "long_tasks", "tbt"} {
		if _, found := ResolvePageMetric(name, p); found {
			t.Errorf("%s is unavailable without PerformanceObserver", name)
		}
	}
	if _, found := ResolvePageMetric("network_kb", p); !found {
		t.Fatal("resource timing should remain available independently of observers")
	}
}

func TestInstrumentationFlushesPendingSceneMeasures(t *testing.T) {
	d := requireDriver(t, 10*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><h1>Pending observer fixture</h1>`)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatal(err)
	}
	var captured bool
	if err := d.Evaluate(`(function(){var p=window.__gosx_perf;var before=p.frameCount;performance.measure('scene3d-render',{start:Math.max(0,performance.now()-1)});p.flushObservers();return p.frameCount === before + 1;})()`, &captured); err != nil {
		t.Fatal(err)
	}
	if !captured {
		t.Fatal("pending scene measure was lost before the observer callback ran")
	}
}
