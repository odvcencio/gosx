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

func TestRunScenarioStaticWebGPUFallbackSampleWindowIsBestEffort(t *testing.T) {
	requireChromeAvailableForScenario(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
<div id="scene" data-gosx-scene3d="true" data-gosx-scene3d-mounted="true" data-gosx-scene3d-backend="webgl" data-gosx-scene3d-renderer="webgl" data-gosx-scene3d-renderer-fallback="webgpu-feature-gap">
<canvas data-gosx-scene3d-canvas="true" width="320" height="180"></canvas>
</div>
</body></html>`)
	}))
	defer srv.Close()

	var diagnostics bytes.Buffer
	report, err := RunScenario(&Scenario{
		URLs:                   []string{srv.URL},
		Timeout:                5 * time.Second,
		Headless:               true,
		diagnostics:            &diagnostics,
		sceneFrameSampleWindow: 50 * time.Millisecond,
		sceneFramePollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("static WebGPU-to-WebGL fallback should retain available metrics: %v\ndiagnostics:\n%s", err, diagnostics.String())
	}
	if len(report.Pages) != 1 || report.Pages[0].Scene == nil {
		t.Fatalf("expected one page with available Scene3D telemetry, got %#v", report.Pages)
	}
	scene := report.Pages[0].Scene
	if scene.FrameCount != 0 {
		t.Fatalf("static fallback unexpectedly reported rendered frames: %#v", scene)
	}
	if len(scene.Mounts) != 1 || scene.Mounts[0].Renderer != "webgl" || scene.Mounts[0].Fallback != "webgpu-feature-gap" {
		t.Fatalf("fallback telemetry was not preserved: %#v", scene.Mounts)
	}
	log := diagnostics.String()
	if strings.Count(log, "sample-window-exhausted") != 2 {
		t.Fatalf("expected one bounded exhaustion diagnostic for wait and rewait, got:\n%s", log)
	}
	for _, want := range []string{
		"phase=scene3d-wait sample-window-exhausted target=120 observed=0 window=50ms action=continue-with-partial-metrics",
		"phase=scene3d-rewait sample-window-exhausted target=120 observed=0 window=50ms action=continue-with-partial-metrics",
		"phase=collect done",
		"phase=scene3d-recollect done",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("missing %q in diagnostics:\n%s", want, log)
		}
	}
	if strings.Contains(log, "phase=scene3d-wait failed") || strings.Contains(log, "phase=scene3d-rewait failed") {
		t.Fatalf("local sample exhaustion was logged as a fatal phase:\n%s", log)
	}
}

func TestRunScenarioCollectsPartialScene3DSamplesAfterWindowExhaustion(t *testing.T) {
	requireChromeAvailableForScenario(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
<div data-gosx-scene3d="true" data-gosx-scene3d-mounted="true" data-gosx-scene3d-renderer="webgl"><canvas></canvas></div>
<script>
for (var i = 0; i < 3; i++) {
  var start = "scene-start-" + i;
  var end = "scene-end-" + i;
  performance.mark(start);
  performance.mark(end);
  performance.measure("scene3d-render", start, end);
}
</script>
</body></html>`)
	}))
	defer srv.Close()

	var diagnostics bytes.Buffer
	report, err := RunScenario(&Scenario{
		URLs:                   []string{srv.URL},
		Frames:                 10,
		Timeout:                5 * time.Second,
		Headless:               true,
		diagnostics:            &diagnostics,
		sceneFrameSampleWindow: 50 * time.Millisecond,
		sceneFramePollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("partial Scene3D samples should remain reportable: %v\ndiagnostics:\n%s", err, diagnostics.String())
	}
	if len(report.Pages) != 1 || report.Pages[0].Scene == nil {
		t.Fatalf("expected one page with partial Scene3D metrics, got %#v", report.Pages)
	}
	scene := report.Pages[0].Scene
	if scene.FrameCount != 3 || scene.FrameStats.Count != 3 {
		t.Fatalf("partial frame samples were not retained: %#v", scene)
	}
	log := diagnostics.String()
	if !strings.Contains(log, "phase=scene3d-wait sample-window-exhausted target=10 observed=3") ||
		!strings.Contains(log, "phase=scene3d-rewait sample-window-exhausted target=10 observed=3") {
		t.Fatalf("partial sample diagnostics were not explicit:\n%s", log)
	}
}

func TestRunScenarioAttributesScene3DRouteDeadlineToScenePhase(t *testing.T) {
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
	if strings.Contains(diagnostics.String(), "sample-window-exhausted") {
		t.Fatalf("route deadline was misclassified as local sample exhaustion:\n%s", diagnostics.String())
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
