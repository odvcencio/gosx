//go:build browser

package perf

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunScenarioUsesFreshTimeoutForEachRealDriverRoute(t *testing.T) {
	requireChromeAvailableForScenario(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/first", "/second":
			time.Sleep(17 * time.Second)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body><h1>%s</h1></body></html>`, r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var diagnostics bytes.Buffer
	start := time.Now()
	report, err := RunScenario(&Scenario{
		URLs:        []string{srv.URL + "/first", srv.URL + "/second"},
		Timeout:     22 * time.Second,
		Headless:    true,
		diagnostics: &diagnostics,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunScenario should let each route use its own timeout, elapsed=%s err=%v\ndiagnostics:\n%s", elapsed, err, diagnostics.String())
	}
	if elapsed < 30*time.Second {
		t.Fatalf("test did not exceed the old default global 30s cap; elapsed=%s", elapsed)
	}
	if len(report.Pages) != 2 {
		t.Fatalf("expected reports for both routes, got %d", len(report.Pages))
	}
	if report.Pages[0].URL != srv.URL+"/first" || report.Pages[1].URL != srv.URL+"/second" {
		t.Fatalf("URL order changed: %#v", []string{report.Pages[0].URL, report.Pages[1].URL})
	}
	log := diagnostics.String()
	if !strings.Contains(log, "route 2/2 "+srv.URL+"/second phase=navigate start timeout=22s") ||
		!strings.Contains(log, "route 2/2 "+srv.URL+"/second phase=navigate done elapsed=") {
		t.Fatalf("missing route 2 timing diagnostics:\n%s", log)
	}
}

func TestRunScenarioAttributesScene3DFrameWaitTimeoutToScenePhase(t *testing.T) {
	requireChromeAvailableForScenario(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><div data-gosx-scene3d="true"></div></body></html>`)
	}))
	defer srv.Close()

	var diagnostics bytes.Buffer
	report, err := RunScenario(&Scenario{
		URLs:        []string{srv.URL},
		Frames:      999,
		Timeout:     600 * time.Millisecond,
		Headless:    true,
		diagnostics: &diagnostics,
	})
	if err == nil {
		t.Fatalf("RunScenario unexpectedly succeeded with report %#v", report)
	}
	text := err.Error()
	if !strings.Contains(text, "phase scene3d-wait") || !strings.Contains(text, "context deadline exceeded") {
		t.Fatalf("expected scene3d-wait deadline attribution, got %v\ndiagnostics:\n%s", err, diagnostics.String())
	}
	if strings.Contains(text, "phase collect") {
		t.Fatalf("scene3d wait timeout was misattributed to collect: %v", err)
	}
	if !strings.Contains(diagnostics.String(), "phase=scene3d-wait failed") {
		t.Fatalf("missing scene3d-wait failure diagnostics:\n%s", diagnostics.String())
	}
}

func requireChromeAvailableForScenario(t *testing.T) {
	t.Helper()
	if _, err := FindChrome(); err != nil {
		if os.Getenv(requireChromeEnv) != "" {
			t.Fatalf("%s is set, so Chrome is required for this scenario regression: %v", requireChromeEnv, err)
		}
		t.Skipf("skipping scenario regression: %v", err)
	}
}
