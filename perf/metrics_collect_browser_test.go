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
	"testing"
	"time"
)

func TestCollectPageReportBusyMainThreadAttributesFirstQuery(t *testing.T) {
	d := requireDriver(t, 5*time.Second)
	busyStarted := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/busy-started" {
			select {
			case busyStarted <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><script>
			setTimeout(function() {
				navigator.sendBeacon('/busy-started', 'busy');
				var until = performance.now() + 1500;
				while (performance.now() < until) {}
			}, 50);
		</script></body></html>`)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := d.WaitReady(); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
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
		t.Fatalf("busy page returned a report: %+v", report)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("busy page error = %v, want context deadline exceeded", err)
	}
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
}
