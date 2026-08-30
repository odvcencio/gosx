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
)

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
