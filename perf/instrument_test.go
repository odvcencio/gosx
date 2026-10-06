//go:build browser

package perf

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// minimalGosxPage serves a minimal HTML page that mimics the GoSX runtime's
// readiness pattern: assigns __gosx_runtime_ready as a function, then calls
// it (as the WASM bridge would), then fires the gosx:ready CustomEvent.
const minimalGosxPage = `<!DOCTYPE html>
<html>
<head><title>GoSX Perf Test</title></head>
<body>
<div id="app">hello</div>
<script>
// Simulate the GoSX bootstrap assigning the ready callback
window.__gosx_runtime_ready = function() {
  // Original handler — sets a flag so we can verify it ran
  window.__gosx_original_ready_called = true;
};

// Simulate the WASM bridge calling it after exports are registered
window.__gosx_hydrate = function(id) { return "[]"; };
window.__gosx_action = function(id, handler, data) { return "[]"; };
window.__gosx_runtime_ready();

// Simulate gosx:ready CustomEvent (fired by bootstrap after full init)
document.dispatchEvent(new CustomEvent("gosx:ready"));
</script>
</body>
</html>`

func TestInjectCreatesReadyMark(t *testing.T) {
	d := requireDriver(t, 10*time.Second)

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

	// Give the async PerformanceObserver and event handlers a moment
	time.Sleep(200 * time.Millisecond)

	// Verify gosx:ready mark was emitted (bridged from CustomEvent)
	var readyCount int
	if err := d.Evaluate(`performance.getEntriesByName("gosx:ready").length`, &readyCount); err != nil {
		t.Fatalf("query gosx:ready: %v", err)
	}
	if readyCount == 0 {
		t.Fatal("expected gosx:ready performance mark, got 0 entries")
	}

	// Verify the original __gosx_runtime_ready was still called
	var originalCalled bool
	if err := d.Evaluate(`window.__gosx_original_ready_called === true`, &originalCalled); err != nil {
		t.Fatalf("query original ready: %v", err)
	}
	if !originalCalled {
		t.Fatal("original __gosx_runtime_ready was not called")
	}

	// Verify __gosx_perf namespace was created
	var perfReady bool
	if err := d.Evaluate(`window.__gosx_perf && window.__gosx_perf.ready === true`, &perfReady); err != nil {
		t.Fatalf("query perf ready: %v", err)
	}
	if !perfReady {
		t.Fatal("__gosx_perf.ready should be true after runtime init")
	}

	// Verify __gosx_scene3d_perf flag was enabled
	var scenePerfEnabled bool
	if err := d.Evaluate(`window.__gosx_scene3d_perf === true`, &scenePerfEnabled); err != nil {
		t.Fatalf("query scene perf: %v", err)
	}
	if !scenePerfEnabled {
		t.Fatal("__gosx_scene3d_perf should be true")
	}
}

func TestInjectWrapsHydrateWithMarks(t *testing.T) {
	d := requireDriver(t, 10*time.Second)

	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}

	// Page that registers __gosx_hydrate then calls __gosx_runtime_ready,
	// then calls __gosx_hydrate("test-island") so the wrapper fires.
	page := `<!DOCTYPE html><html><body><script>
window.__gosx_runtime_ready = function() {};
window.__gosx_hydrate = function(id) { return "[]"; };
window.__gosx_runtime_ready();
window.__gosx_hydrate("test-island-1");
</script></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Check that hydration measure was recorded
	var count int
	if err := d.Evaluate(`performance.getEntriesByName("gosx:island:hydrate:test-island-1").length`, &count); err != nil {
		t.Fatalf("query hydration: %v", err)
	}
	if count == 0 {
		t.Fatal("expected gosx:island:hydrate:test-island-1 measure, got 0")
	}

	// Check the hydration log
	var logLen int
	if err := d.Evaluate(`window.__gosx_perf.hydrationLog.length`, &logLen); err != nil {
		t.Fatalf("query hydration log: %v", err)
	}
	if logLen == 0 {
		t.Fatal("expected hydration log entries")
	}
}

func TestInjectWrapsActionWithMarks(t *testing.T) {
	d := requireDriver(t, 10*time.Second)

	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}

	page := `<!DOCTYPE html><html><body><script>
window.__gosx_runtime_ready = function() {};
window.__gosx_action = function(id, handler, data) { return "[]"; };
window.__gosx_runtime_ready();
window.__gosx_action("counter-1", "increment", "{}");
</script></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()

	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	var count int
	if err := d.Evaluate(`performance.getEntriesByName("gosx:dispatch:counter-1:increment").length`, &count); err != nil {
		t.Fatalf("query dispatch: %v", err)
	}
	if count == 0 {
		t.Fatal("expected dispatch measure for counter-1:increment")
	}

	var logLen int
	if err := d.Evaluate(`window.__gosx_perf.dispatchLog.length`, &logLen); err != nil {
		t.Fatalf("query dispatch log: %v", err)
	}
	if logLen == 0 {
		t.Fatal("expected dispatch log entries")
	}
}

func TestInjectReadyHandlerChaining(t *testing.T) {
	d := requireDriver(t, 10*time.Second)
	if err := InjectDriver(d); err != nil {
		t.Fatalf("InjectDriver: %v", err)
	}
	page := `<!DOCTYPE html><html><body><script>
window.readyCalls = [];
window.initialReadyMissing = window.__gosx_runtime_ready === undefined;
window.__gosx_runtime_ready = function(value) {
  readyCalls.push(this.label + ":original:" + value);
};
var priorReady = window.__gosx_runtime_ready;
window.__gosx_runtime_ready = function(value) {
  priorReady.call(this, value);
  readyCalls.push(this.label + ":bootstrap:" + value);
  return "complete";
};
var ready = window.__gosx_runtime_ready;
window.stableReadyWrapper = ready === window.__gosx_runtime_ready;
window.__gosx_runtime_ready = ready;
window.idempotentReadyWrapper = ready === window.__gosx_runtime_ready;
try {
  window.readyResult = window.__gosx_runtime_ready.call({label: "test"}, 7);
  window.__gosx_runtime_ready.call({label: "again"}, 8);
} catch (error) {
  window.readyError = String(error);
}
</script></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}))
	defer srv.Close()
	if err := d.Navigate(srv.URL); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	var result struct {
		InitialMissing bool     `json:"initialMissing"`
		Stable         bool     `json:"stable"`
		Idempotent     bool     `json:"idempotent"`
		Ready          bool     `json:"ready"`
		Marks          int      `json:"marks"`
		Calls          []string `json:"calls"`
		Value          string   `json:"value"`
		Error          string   `json:"error"`
	}
	if err := d.Evaluate(`({
initialMissing: window.initialReadyMissing,
stable: window.stableReadyWrapper,
idempotent: window.idempotentReadyWrapper,
ready: window.__gosx_perf.ready,
marks: performance.getEntriesByName("gosx:perf:ready").length,
calls: window.readyCalls,
value: window.readyResult || "",
error: window.readyError || ""
})`, &result); err != nil {
		t.Fatalf("query ready chain: %v", err)
	}
	if !result.InitialMissing || !result.Stable || !result.Idempotent || !result.Ready || result.Marks != 1 || result.Value != "complete" || result.Error != "" {
		t.Fatalf("ready chain: %+v", result)
	}
	want := "[test:original:7 test:bootstrap:7 again:original:8 again:bootstrap:8]"
	if fmt.Sprint(result.Calls) != want {
		t.Fatalf("calls = %v, want %s", result.Calls, want)
	}
}
