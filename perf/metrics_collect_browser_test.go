//go:build browser

package perf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func logBrowserVersion(t *testing.T, d *Driver) {
	t.Helper()
	var protocolVersion, product, revision, userAgent, jsVersion string
	if err := chromedp.Run(d.Context(), chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		protocolVersion, product, revision, userAgent, jsVersion, err = cdpbrowser.GetVersion().Do(ctx)
		return err
	})); err != nil {
		t.Logf("browser version unavailable: %v", err)
		return
	}
	t.Logf("browser version: product=%q protocol=%q revision=%q js=%q userAgent=%q", product, protocolVersion, revision, jsVersion, userAgent)
}

func TestCollectPageReportBusyMainThreadAttributesFirstQuery(t *testing.T) {
	d := requireDriver(t, 5*time.Second)
	busyStarted := make(chan struct{}, 1)
	releaseBusy := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseBusy)
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/busy-block" {
			select {
			case busyStarted <- struct{}{}:
			default:
			}
			<-releaseBusy
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
			window.blockMainThreadForPerfTest = function() {
				var xhr = new XMLHttpRequest();
				xhr.open('GET', '/busy-block', false);
				xhr.send();
			};
		</script></body></html>`)
	}))
	defer func() {
		release()
		srv.Close()
	}()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	blockErr := make(chan error, 1)
	go func() {
		var ignored any
		blockErr <- d.Evaluate(`window.blockMainThreadForPerfTest()`, &ignored)
	}()
	select {
	case <-busyStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("page did not enter the synchronized busy-main-thread section")
	}

	const queryTimeout = 150 * time.Millisecond
	routeCtx, routeCancel := context.WithTimeout(context.Background(), queryTimeout)
	defer routeCancel()
	routeD, driverCancel := d.WithOperationContext(routeCtx, 0)
	defer driverCancel()

	var diagnostics bytes.Buffer
	report, err := collectPageReportWithQueryRunner(
		routeD,
		srv.URL,
		diagnosticPageReportQueryRunner(&diagnostics, 0, 1, srv.URL, queryTimeout),
	)
	if report != nil {
		release()
		t.Fatalf("busy page returned a report: %+v", report)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		release()
		t.Fatalf("busy page error = %v, want context deadline exceeded", err)
	}
	release()
	if !strings.Contains(err.Error(), "collect/navigation-timing") {
		t.Fatalf("busy page error lacks exact first-query attribution: %v", err)
	}

	log := diagnostics.String()
	if !strings.Contains(log, "phase=collect/navigation-timing start required=true timeout=150ms") {
		t.Fatalf("missing attributed query start:\n%s", log)
	}
	if !strings.Contains(log, "phase=collect/navigation-timing failed") ||
		!strings.Contains(log, "required=true action=fail: context deadline exceeded") {
		t.Fatalf("missing attributed query failure:\n%s", log)
	}
	if strings.Contains(log, "phase=collect/heap-size start") {
		t.Fatalf("collector advanced after required query cancellation:\n%s", log)
	}
	if err := <-blockErr; err != nil {
		t.Fatalf("release synchronized busy-main-thread section: %v", err)
	}
}

func TestCollectPageReportBrowserMissingPerfHookMarksOptionalUnavailable(t *testing.T) {
	d := requireDriver(t, 10*time.Second)
	logBrowserVersion(t, d)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><p>plain server page</p></body></html>`)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	report, err := CollectPageReport(d, srv.URL)
	if err != nil {
		t.Fatalf("CollectPageReport: %v", err)
	}
	for _, metric := range []string{"lcp", "long_tasks", "signal_writes"} {
		if value, ok := ResolvePageMetric(metric, report); ok {
			t.Fatalf("%s resolved %.2f despite missing instrumentation hook", metric, value)
		}
	}
	if value, ok := ResolvePageMetric("network_kb", report); !ok || value < 0 {
		t.Fatalf("resource waterfall should be measured nonnegative, got %.2f found=%v", value, ok)
	}
}

func TestCollectPageReportBrowserMeasuredZerosAndWebGLParentReuse(t *testing.T) {
	d := requireDriver(t, 30*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, minimalGosxPage)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	report, err := CollectPageReport(d, srv.URL)
	if err != nil {
		t.Fatalf("CollectPageReport: %v", err)
	}
	for _, metric := range []string{"cls", "long_tasks", "signal_writes"} {
		if value, ok := ResolvePageMetric(metric, report); !ok || value != 0 {
			t.Fatalf("%s = %.2f, %v; want measured zero", metric, value, ok)
		}
	}
	var answer int
	if err := d.Evaluate(`6 * 7`, &answer); err != nil {
		t.Fatalf("parent driver unusable after WebGL child probe: %v", err)
	}
	if answer != 42 {
		t.Fatalf("post-probe Evaluate returned %d", answer)
	}
}

func TestCollectPageReportBrowserUnsupportedObserverTypeUnavailable(t *testing.T) {
	d := requireDriver(t, 20*time.Second)
	addScript := page.AddScriptToEvaluateOnNewDocument(`
		Object.defineProperty(PerformanceObserver, "supportedEntryTypes", {
			value: ["layout-shift"],
			configurable: true
		});
	`)
	if err := chromedp.Run(d.Context(), chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := addScript.Do(ctx)
		return err
	})); err != nil {
		t.Fatalf("force supportedEntryTypes: %v", err)
	}
	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, minimalGosxPage)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	report, err := CollectPageReport(d, srv.URL)
	if err != nil {
		t.Fatalf("CollectPageReport: %v", err)
	}
	if status := report.measurementStatus("lcp"); status.State != MeasurementUnsupported {
		t.Fatalf("lcp status = %+v, want unsupported", status)
	}
	if status := report.measurementStatus("long-tasks"); status.State != MeasurementUnsupported {
		t.Fatalf("long-tasks status = %+v, want unsupported", status)
	}
	if _, ok := ResolvePageMetric("cls", report); !ok {
		t.Fatalf("layout-shift should remain measured with forced support list: %+v", report.Measurements)
	}
}

func TestCollectPageReportBrowserPendingWebGLTimesOutChildAndKeepsParent(t *testing.T) {
	d := requireDriver(t, 30*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}
	webglStarted := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/webgl-started" {
			select {
			case webglStarted <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
window.__gosx_runtime_ready = function() {};
window.__gosx_runtime_ready();
window.__gosx_perf_webgl_info = function() {
	fetch('/webgl-started', {method: 'POST', keepalive: true}).catch(function(){});
	return new Promise(function(){ });
};
document.dispatchEvent(new CustomEvent("gosx:ready"));
</script></body></html>`)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	start := time.Now()
	report, err := CollectPageReport(d, srv.URL)
	select {
	case <-webglStarted:
	default:
		t.Fatalf("pending WebGL probe did not signal that browser work started")
	}
	if err != nil {
		t.Fatalf("CollectPageReport: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 1500*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("pending WebGL probe should wait for bounded child timeout, got %s", elapsed)
	}
	if status := report.measurementStatus("webgl-info"); status.State != MeasurementFailed {
		t.Fatalf("webgl status = %+v, want failed child timeout", status)
	} else if !strings.Contains(status.Reason, "context deadline exceeded") {
		t.Fatalf("webgl failure should record child deadline cause, got %+v", status)
	}
	if _, ok := ResolvePageMetric("network_kb", report); !ok {
		t.Fatalf("waterfall should remain measured after WebGL child timeout: %+v", report.Measurements)
	}
	var answer int
	if err := d.Evaluate(`6 * 7`, &answer); err != nil {
		t.Fatalf("parent driver unusable after pending WebGL child timeout: %v", err)
	}
	if answer != 42 {
		t.Fatalf("post-timeout Evaluate returned %d", answer)
	}
}

func TestCollectPageReportBrowserOptionalParentDeadlineReturnsNilReport(t *testing.T) {
	d := requireDriver(t, 20*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}
	busyStarted := make(chan struct{}, 1)
	releaseBusy := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseBusy) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/busy-block" {
			select {
			case busyStarted <- struct{}{}:
			default:
			}
			<-releaseBusy
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
window.__gosx_runtime_ready = function() {};
window.__gosx_runtime_ready();
window.blockMainThreadForPerfTest = function() {
	var xhr = new XMLHttpRequest();
	xhr.open('GET', '/busy-block', false);
	xhr.send();
};
document.dispatchEvent(new CustomEvent("gosx:ready"));
</script></body></html>`)
	}))
	defer func() {
		release()
		srv.Close()
	}()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	routeCtx, routeCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer routeCancel()
	routeD, driverCancel := d.WithOperationContext(routeCtx, 0)
	defer driverCancel()
	runner := func(phase string, required bool, query func() error) error {
		if phase != "collect/resource-waterfall" {
			return query()
		}
		blockErr := make(chan error, 1)
		go func() {
			var ignored any
			blockErr <- d.Evaluate(`window.blockMainThreadForPerfTest()`, &ignored)
		}()
		select {
		case <-busyStarted:
		case <-time.After(2 * time.Second):
			release()
			t.Fatal("page did not enter busy section before resource waterfall query")
		}
		err := query()
		release()
		if blockErrErr := <-blockErr; blockErrErr != nil && !errors.Is(blockErrErr, context.Canceled) {
			t.Fatalf("release busy section: %v", blockErrErr)
		}
		return err
	}

	report, err := collectPageReportWithQueryRunner(routeD, srv.URL, runner)
	if err == nil || report != nil {
		t.Fatalf("parent optional deadline should return nil report and error, got report=%+v err=%v", report, err)
	}
	if !strings.Contains(err.Error(), "collect/resource-waterfall") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error should preserve optional phase and deadline cause: %v", err)
	}
	var answer int
	if err := d.Evaluate(`6 * 7`, &answer); err != nil {
		t.Fatalf("parent driver unusable after route deadline: %v", err)
	}
	if answer != 42 {
		t.Fatalf("post-deadline Evaluate returned %d", answer)
	}
}

func TestCollectPageReportBrowserParentCancelDuringPendingWebGLReturnsNilReport(t *testing.T) {
	d := requireDriver(t, 30*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}
	webglStarted := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/webgl-started" {
			select {
			case webglStarted <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
window.__gosx_runtime_ready = function() {};
window.__gosx_runtime_ready();
window.__gosx_perf_webgl_info = function() {
	fetch('/webgl-started', {method: 'POST', keepalive: true}).catch(function(){});
	return new Promise(function(){ });
};
document.dispatchEvent(new CustomEvent("gosx:ready"));
</script></body></html>`)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	routeCtx, routeCancel := context.WithCancel(context.Background())
	routeD, driverCancel := d.WithOperationContext(routeCtx, 0)
	defer driverCancel()

	done := make(chan struct {
		report *PageReport
		err    error
	}, 1)
	go func() {
		report, err := CollectPageReport(routeD, srv.URL)
		done <- struct {
			report *PageReport
			err    error
		}{report: report, err: err}
	}()
	select {
	case <-webglStarted:
	case <-time.After(5 * time.Second):
		routeCancel()
		t.Fatal("pending WebGL probe did not signal that browser work started")
	}
	routeCancel()

	var result struct {
		report *PageReport
		err    error
	}
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("collectPageReport did not return after route parent cancellation")
	}
	if result.err == nil || result.report != nil {
		t.Fatalf("route parent cancellation should return nil report and error, got report=%+v err=%v", result.report, result.err)
	}
	if !strings.Contains(result.err.Error(), "collect/webgl-info") || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("error should preserve WebGL phase and parent cancel cause: %v", result.err)
	}
	var answer int
	if err := d.Evaluate(`6 * 7`, &answer); err != nil {
		t.Fatalf("parent driver unusable after route cancellation: %v", err)
	}
	if answer != 42 {
		t.Fatalf("post-cancel Evaluate returned %d", answer)
	}
}
