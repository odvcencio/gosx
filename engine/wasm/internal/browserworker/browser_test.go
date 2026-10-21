//go:build !js || !wasm

// Package browserworker holds the real-browser tests for Thread: worker
// engines. They build a Go WASM engine, serve a page made with the real
// island.Renderer and the real runtime bundles from client/js, and drive it in
// Chromium with Playwright (run.mjs). They run only with ENGINE_WORKER_BROWSER=1
// (make test-engine-worker-browser); ENGINE_WORKER_CHROME selects the browser.
package browserworker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
)

type report map[string]any

type browserState struct {
	Reports []report         `json:"reports"`
	Events  []map[string]any `json:"events"`
	Issues  []map[string]any `json:"issues"`
	UA      string           `json:"ua"`
	Error   string           `json:"error"`
	Log     []string         `json:"log"`
	Extra   map[string]any   `json:"extra"`
}

func (s browserState) find(kind string) report {
	for _, r := range s.Reports {
		if r["kind"] == kind {
			return r
		}
	}
	return nil
}

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// assets builds the fixture engine once and stages wasm_exec.js.
func assets(t *testing.T) string {
	t.Helper()
	if os.Getenv("ENGINE_WORKER_BROWSER") == "" {
		t.Skip("set ENGINE_WORKER_BROWSER=1 to run the real-browser worker tests")
	}
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "engine-worker-browser")
		if buildErr != nil {
			return
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(buildDir, "fixture.wasm"), "./engine/wasm/internal/browserworker/fixture")
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("building the fixture engine: %v\n%s", err, out)
			return
		}
		goroot, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			buildErr = err
			return
		}
		src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"))
		if err != nil {
			buildErr = err
			return
		}
		// The same isolation wrapper `gosx build` applies (cmd/gosx wrapStandardGoWASMExec).
		wrapped := "(function(global){\n\"use strict\";\nvar hadGo = Object.prototype.hasOwnProperty.call(global, \"Go\");\nvar previousGo = global.Go;\nvar standardGo;\ntry {\n" +
			string(src) +
			"\nstandardGo = global.Go;\n} finally {\nif (hadGo) global.Go = previousGo; else delete global.Go;\n}\nif (typeof standardGo !== \"function\") throw new Error(\"standard-Go wasm_exec.js did not publish Go\");\nObject.defineProperty(global, \"__gosx_standard_go_wasm_ctor\", {value: standardGo, writable: false, configurable: true});\n})(globalThis);\n"
		buildErr = os.WriteFile(filepath.Join(buildDir, "standard-go-wasm_exec.js"), []byte(wrapped), 0o644)
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildDir
}

// harness script: collects what the engines report and what the runtime raises.
const harness = `<script>
window.__reports = []; window.__events = [];
document.addEventListener("gosx:engine:report", function(e) { window.__reports.push(e.detail.detail); });
["thread-fallback", "worker-error", "worker-frame-error"].forEach(function(n) {
  document.addEventListener("gosx:engine:" + n, function(e) { window.__events.push({ name: n, detail: e.detail }); });
});
var q = new URLSearchParams(location.search);
if (q.get("noworker")) window.__gosx_engine_worker_disable = true;
// A main-thread timer the tests compare with the worker clock.
window.__mainTicks = 0; setInterval(function() { window.__mainTicks++; }, 16);
// Main-thread jank: a render spike of ~120 ms every 200 ms.
if (q.get("jank")) setInterval(function() { var t = performance.now(); while (performance.now() - t < 120); }, 200);
</script>`

type pageOptions struct {
	engines []engine.Config
	body    string
}

func page(t *testing.T, opts pageOptions) string {
	t.Helper()
	r := island.NewRenderer("main")
	r.SetClientAssetPaths("/gosx/wasm_exec.js", "/gosx/patch.js", "/gosx/bootstrap.js")
	r.SetStandardGoWASMExecPath("/gosx/standard-go-wasm_exec.js")
	var body strings.Builder
	body.WriteString(opts.body)
	for _, cfg := range opts.engines {
		body.WriteString(gosx.RenderHTML(r.RenderEngine(cfg, gosx.Node{})))
	}
	return "<!doctype html><html><head><meta charset=\"utf-8\"><title>engine worker</title>" + harness +
		gosx.RenderHTML(r.PageHead()) + "</head><body>" + body.String() + "</body></html>"
}

func serve(t *testing.T, dir string, html func(r *http.Request) string) *httptest.Server {
	t.Helper()
	root := repoRoot(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/gosx/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		path := filepath.Join(root, "client", "js", name)
		if name == "standard-go-wasm_exec.js" {
			path = filepath.Join(dir, name)
		}
		if _, err := os.Stat(path); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	})
	mux.HandleFunc("/engines/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(r.URL.Path)))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html(r)))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// drive opens url in Chromium and returns the page's state once waitExpr is truthy.
func drive(t *testing.T, url, waitExpr string, timeout time.Duration) browserState {
	t.Helper()
	cmd := exec.Command("node", "run.mjs", url, waitExpr, fmt.Sprint(timeout.Milliseconds()))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if len(out) == 0 {
		t.Fatalf("browser run produced no output: %v %s", err, stderr.String())
	}
	var state browserState
	if jerr := json.Unmarshal(out, &state); jerr != nil {
		t.Fatalf("browser output %q: %v", out, jerr)
	}
	if state.Error != "" {
		t.Fatalf("browser run failed: %s\nlog: %v", state.Error, state.Log)
	}
	return state
}

func (s browserState) clockReports() []report {
	var out []report
	for _, r := range s.Reports {
		if r["kind"] == "clock" {
			out = append(out, r)
		}
	}
	return out
}

func num(r report, key string) float64 {
	v, _ := r[key].(float64)
	return v
}

// clockSummary condenses the fixture's clock reports: frames per second over
// the run and the longest gap between two frames the engine saw.
type clockSummary struct {
	thread       string
	fps, stepsPS float64
	gapMaxMs     float64
	reports      int
}

func summarize(rs []report) clockSummary {
	first, last := rs[0], rs[len(rs)-1]
	span := (num(last, "elapsedMs") - num(first, "elapsedMs")) / 1000
	sum := clockSummary{thread: fmt.Sprint(last["thread"]), reports: len(rs)}
	if span > 0 {
		sum.fps = (num(last, "frames") - num(first, "frames")) / span
		sum.stepsPS = (num(last, "steps") - num(first, "steps")) / span
	}
	for _, r := range rs[1:] {
		if g := num(r, "gapMaxMs"); g > sum.gapMaxMs {
			sum.gapMaxMs = g
		}
	}
	return sum
}

func clockEngine(runHidden bool) engine.Config {
	return workerEngine(fmt.Sprintf(`{"scenario":"clock","runHidden":%t}`, runHidden))
}

const clockDone = `(window.__reports||[]).filter(r => r.kind === "clock" && r.elapsedMs > 3000).length > 0`

func workerEngine(props string) engine.Config {
	return engine.Config{
		Name: "WorkerFixture", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, Thread: engine.ThreadWorker,
		WASMPath: "/engines/fixture.wasm", Props: json.RawMessage(props),
	}
}

func TestWorkerEngineBootsInARealWorker(t *testing.T) {
	dir := assets(t)
	ts := serve(t, dir, func(*http.Request) string {
		return page(t, pageOptions{engines: []engine.Config{workerEngine(`{"scenario":"boot"}`)}})
	})
	state := drive(t, ts.URL+"/", `(window.__reports||[]).some(r => r.kind === "mounted")`, 30*time.Second)
	mounted := state.find("mounted")
	if mounted["thread"] != "worker" || mounted["hasDocument"] != false || mounted["hasPort"] != true || mounted["fallback"] != "" {
		t.Fatalf("engine did not run in a worker: %v\nlog: %v", mounted, state.Log)
	}
	if len(state.Events) != 0 {
		t.Fatalf("unexpected fallback events: %v", state.Events)
	}
	t.Logf("worker engine mounted: %v (%s)", mounted, state.UA)
}

func TestWorkerEngineFallsBackToTheMainThreadAndReports(t *testing.T) {
	dir := assets(t)
	ts := serve(t, dir, func(*http.Request) string {
		return page(t, pageOptions{engines: []engine.Config{workerEngine(`{"scenario":"fallback"}`)}})
	})
	state := drive(t, ts.URL+"/?noworker=1", `(window.__reports||[]).some(r => r.kind === "mounted")`, 30*time.Second)
	mounted := state.find("mounted")
	if mounted["thread"] != "" || mounted["hasDocument"] != true || mounted["hasPort"] != false || mounted["fallback"] != "worker-disabled" {
		t.Fatalf("engine did not fall back: %v\nlog: %v", mounted, state.Log)
	}
	if len(state.Events) != 1 || state.Events[0]["name"] != "thread-fallback" {
		t.Fatalf("want one thread-fallback event, got %v", state.Events)
	}
	found := false
	for _, issue := range state.Issues {
		if issue["type"] == "worker" && issue["fallback"] == "main-thread" && strings.Contains(fmt.Sprint(issue["message"]), "disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("fallback was not reported as a runtime issue: %v", state.Issues)
	}
}

// The simulation must hold its rate when the page's main thread is busy. The
// page blocks its main thread for 120 ms out of every 200 ms; the same engine
// runs once in a worker and once on the main thread (the fallback path).
func TestWorkerClockHoldsRateWhileTheMainThreadIsBusy(t *testing.T) {
	dir := assets(t)
	ts := serve(t, dir, func(*http.Request) string {
		return page(t, pageOptions{engines: []engine.Config{clockEngine(false)}})
	})
	worker := summarize(drive(t, ts.URL+"/?jank=1", clockDone, 40*time.Second).clockReports())
	main := summarize(drive(t, ts.URL+"/?jank=1&noworker=1", clockDone, 40*time.Second).clockReports())
	t.Logf("main thread busy 60%%: worker fps=%.1f steps/s=%.1f longest gap=%.1f ms | main-thread engine fps=%.1f steps/s=%.1f longest gap=%.1f ms",
		worker.fps, worker.stepsPS, worker.gapMaxMs, main.fps, main.stepsPS, main.gapMaxMs)
	if worker.thread != "worker" || main.thread != "" {
		t.Fatalf("threads: worker run %q, main run %q", worker.thread, main.thread)
	}
	if worker.fps < 55 || worker.fps > 65 {
		t.Fatalf("worker frame rate %.1f, want about 60", worker.fps)
	}
	if worker.stepsPS < 55 || worker.stepsPS > 65 {
		t.Fatalf("worker fixed steps per second %.1f, want about 60", worker.stepsPS)
	}
	if worker.gapMaxMs > 50 {
		t.Fatalf("worker longest frame gap %.1f ms: main-thread work leaked into the worker", worker.gapMaxMs)
	}
	if main.gapMaxMs < 100 {
		t.Fatalf("control run gap %.1f ms: the page was not busy, so the comparison proves nothing", main.gapMaxMs)
	}
}

type hiddenSample struct {
	Label      string             `json:"label"`
	Wall       float64            `json:"wall"`
	Vis        string             `json:"vis"`
	MainTicks  float64            `json:"mainTicks"`
	Last       map[string]report  `json:"last"`
	TickGapMax map[string]float64 `json:"tickGapMax"`
	Reports    int                `json:"reports"`
}

// TestHiddenTabTimers measures a worker clock, a paused worker driver and a
// main-thread engine while the page is really hidden. A second tab takes the
// foreground (raw CDP; Playwright would keep the page "visible"). It needs a
// headed Chrome and a display, so it runs only when ENGINE_WORKER_HEADFUL_CHROME
// names a Chrome binary and DISPLAY is set.
func TestHiddenTabTimers(t *testing.T) {
	chrome := os.Getenv("ENGINE_WORKER_HEADFUL_CHROME")
	if chrome == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("set ENGINE_WORKER_HEADFUL_CHROME to a Chrome binary and DISPLAY to run the hidden-tab measurement")
	}
	dir := assets(t)
	mainEngine := engine.Config{Name: "WorkerFixture", Kind: engine.KindWorker, Runtime: engine.RuntimeGoWASM, WASMPath: "/engines/fixture.wasm",
		Props: json.RawMessage(`{"scenario":"clock","tag":"main-run","runHidden":true}`)}
	workerRun := workerEngine(`{"scenario":"clock","tag":"worker-run","runHidden":true}`)
	workerPause := workerEngine(`{"scenario":"clock","tag":"worker-pause","runHidden":false}`)
	ts := serve(t, dir, func(*http.Request) string {
		return page(t, pageOptions{engines: []engine.Config{workerRun, workerPause, mainEngine}})
	})
	cmd := exec.Command("node", "run-hidden.mjs", chrome, ts.URL+"/", "25", "9444")
	out, err := cmd.Output()
	if len(out) == 0 {
		t.Fatalf("hidden run produced no output: %v", err)
	}
	var res struct {
		UA      string         `json:"ua"`
		Samples []hiddenSample `json:"samples"`
		Error   string         `json:"error"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil || res.Error != "" {
		t.Fatalf("hidden run: %v %s", jerr, out)
	}
	t.Logf("browser: %s", res.UA)
	rate := func(a, b hiddenSample, tag, key string) float64 {
		secs := (b.Wall - a.Wall) / 1000
		if tag == "" {
			return (b.MainTicks - a.MainTicks) / secs
		}
		// Use the engine's own clock between its two reports, so the sampling
		// phase (a report is up to 0.5 s old) does not add error. No new report
		// means the engine counted nothing in the interval.
		engineSecs := (num(b.Last[tag], "elapsedMs") - num(a.Last[tag], "elapsedMs")) / 1000
		if engineSecs <= 0 {
			return 0
		}
		return (num(b.Last[tag], key) - num(a.Last[tag], key)) / engineSecs
	}
	t.Logf("%-12s %-8s %12s %12s %12s %12s %12s", "interval", "page", "worker-run", "worker-run", "worker-pause", "worker-pause", "main timer")
	t.Logf("%-12s %-8s %12s %12s %12s %12s %12s", "", "", "clock/s", "steps/s", "clock/s", "steps/s", "16ms ticks/s")
	for i := 1; i < len(res.Samples); i++ {
		a, b := res.Samples[i-1], res.Samples[i]
		t.Logf("%-12s %-8s %12.1f %12.1f %12.1f %12.1f %12.1f   (main-thread rAF engine ticks/s %.1f; longest tick gap in worker-run %.1f ms, reports %d)",
			a.Label+">"+b.Label, b.Vis, rate(a, b, "worker-run", "ticks"), rate(a, b, "worker-run", "steps"),
			rate(a, b, "worker-pause", "ticks"), rate(a, b, "worker-pause", "steps"), rate(a, b, "", ""), rate(a, b, "main-run", "ticks"), b.TickGapMax["worker-run"], b.Reports-a.Reports)
	}
	var hiddenIdx []int
	for i, s := range res.Samples {
		if s.Vis == "hidden" {
			hiddenIdx = append(hiddenIdx, i)
		}
	}
	if len(hiddenIdx) < 3 {
		t.Fatalf("the page never became hidden (visibility per sample: %v)", func() (v []string) {
			for _, s := range res.Samples {
				v = append(v, s.Vis)
			}
			return
		}())
	}
	// Judge the last hidden interval: the browser has had >= 20 s to throttle.
	a, b := res.Samples[hiddenIdx[len(hiddenIdx)-2]], res.Samples[hiddenIdx[len(hiddenIdx)-1]]
	if got := rate(a, b, "worker-run", "ticks"); got < 50 {
		t.Errorf("worker clock ran at %.1f/s in a hidden tab, want about 60", got)
	}
	if got := rate(a, b, "worker-run", "steps"); got < 50 {
		t.Errorf("worker simulation stepped %.1f/s in a hidden tab with RunWhileHidden, want about 60", got)
	}
	if got := rate(a, b, "worker-pause", "ticks"); got < 50 {
		t.Errorf("paused worker driver's clock ticked %.1f/s, want about 60 (it keeps polling for visibility)", got)
	}
	if got := rate(a, b, "worker-pause", "steps"); got != 0 {
		t.Errorf("worker driver without RunWhileHidden stepped %.1f/s while Hidden() was true", got)
	}
	if got := rate(a, b, "main-run", "ticks"); got != 0 {
		t.Logf("main-thread requestAnimationFrame still ticked %.1f/s while hidden", got)
	}
	if got := rate(a, b, "", ""); got > 20 {
		t.Errorf("main-thread 16 ms timer ran %.1f/s while hidden; the control shows no throttling, so the comparison proves nothing", got)
	}
}
