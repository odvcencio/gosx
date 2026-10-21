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
	Frames  []map[string]any `json:"frames"`
	Signal  map[string]any   `json:"signal"`
	Shots   []shot           `json:"shots"`
}

type shot struct {
	Name   string   `json:"name"`
	Orange int      `json:"orange"`
	MeanX  *float64 `json:"meanX"`
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
window.__reports = []; window.__events = []; window.__frames = [];
document.addEventListener("gosx:engine:report", function(e) { window.__reports.push(e.detail.detail); });
["thread-fallback", "worker-error", "worker-frame-error"].forEach(function(n) {
  document.addEventListener("gosx:engine:" + n, function(e) { window.__events.push({ name: n, detail: e.detail }); });
});
var q = new URLSearchParams(location.search);
if (q.get("noworker")) window.__gosx_engine_worker_disable = true;
// Record binary frames at the page's Scene3D dispatch entry points: the same
// functions a main-thread engine calls.
window.addEventListener("load", function() {
  var api = window.__gosx && window.__gosx.scene3d; if (!api) return;
  ["dispatchPoseFrame", "dispatchMotionFrame", "dispatchClipFrame"].forEach(function(name) {
    var original = api[name];
    api[name] = function(target, bytes, options) {
      var b = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
      window.__frames.push({ method: name, target: String(target), length: b.length, magic: String.fromCharCode(b[0], b[1], b[2], b[3]) });
      return original.apply(this, arguments);
    };
  });
});
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
func drive(t *testing.T, url, waitExpr string, timeout time.Duration, shots ...string) browserState {
	return driveScript(t, url, waitExpr, timeout, "", shots...)
}

// driveScript is drive with a Playwright script (scripts/*.mjs) that acts on
// the page, for example to send input.
func driveScript(t *testing.T, url, waitExpr string, timeout time.Duration, script string, shots ...string) browserState {
	t.Helper()
	args := []string{"run.mjs", url, waitExpr, fmt.Sprint(timeout.Milliseconds()), "[]"}
	if len(shots) > 0 {
		args[4] = shots[0]
	}
	if script != "" {
		args = append(args, script)
	}
	cmd := exec.Command("node", args...)
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
		t.Fatalf("browser run failed: %s\npartial: %s\nlog: %v", state.Error, out, state.Log)
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

const sceneProps = `{"width":400,"height":300,"responsive":false,"maxDevicePixelRatio":1,"requireWebGL":true,"forceWebGL":true,"shaded":true,"controls":"none","background":"#08151f",
 "scene":{"camera":{"x":0,"y":0,"z":8,"fov":60},"instancedMeshes":[{"id":"crowd","count":1,"kind":"box","size":1,"color":"#ffbb33","transforms":[1,0,0,0,0,1,0,0,0,0,1,0,-3,0,0,1]}]}}`

func sceneEngine() engine.Config {
	return engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, MountID: "arena", Props: json.RawMessage(sceneProps),
		MountAttrs: map[string]any{"style": "width:400px;height:300px"}}
}

func sceneWorker(thread engine.Thread) engine.Config {
	cfg := workerEngine(`{"scenario":"scene","target":"arena"}`)
	cfg.Thread = thread
	return cfg
}

func scenePage(t *testing.T, thread engine.Thread) func(*http.Request) string {
	return func(*http.Request) string {
		return page(t, pageOptions{
			engines: []engine.Config{sceneEngine(), sceneWorker(thread)},
		})
	}
}

// A Go engine in a worker drives a Scene3D object on the page: GSXI frames move
// an instanced box, GSP2/GSP3/GSP4 reach the page's dispatch functions, and
// signal batches update a shared signal. The pixels of the page's canvas are
// the evidence that the object moved.
func TestWorkerDrivesAScene3DObjectThroughBinaryFrames(t *testing.T) {
	runSceneScenario(t, "/", "worker", false)
}

// The same engine on the main thread (the fallback path) must produce the same
// result, because the page's dispatch functions are the same ones.
func TestMainThreadFallbackDrivesTheSameScene(t *testing.T) {
	runSceneScenario(t, "/?noworker=1", "", true)
}

func runSceneScenario(t *testing.T, path, wantThread string, mainThread bool) {
	dir := assets(t)
	ts := serve(t, dir, scenePage(t, engine.ThreadWorker))
	shots := `[{"name":"early","selector":"#arena","when":"(window.__reports||[]).some(r => r.kind === 'scene-progress' && r.rev >= 30)"},
	           {"name":"late","selector":"#arena","when":"(window.__reports||[]).some(r => r.kind === 'scene-done')"}]`
	state := drive(t, ts.URL+path, `(window.__reports||[]).some(r => r.kind === 'scene-done' || r.kind === 'scene-error')`, 60*time.Second, shots)
	if e := state.find("scene-error"); e != nil {
		t.Fatalf("scene error: %v\nlog: %v", e, state.Log)
	}
	mounted := state.find("mounted")
	if mounted["thread"] != wantThread || (mounted["hasDocument"] == true) != mainThread {
		t.Fatalf("engine ran on the wrong thread (want %q): %v", wantThread, mounted)
	}
	done := state.find("scene-done")
	if done == nil || num(done, "applied") != 240 || num(done, "rejected") != 0 {
		t.Fatalf("scene-done %v\nlog: %v", done, state.Log)
	}
	if len(state.Shots) != 2 || state.Shots[0].MeanX == nil || state.Shots[1].MeanX == nil {
		t.Fatalf("no foreground pixels found in the canvas: %+v\nlog: %v", state.Shots, state.Log)
	}
	early, late := *state.Shots[0].MeanX, *state.Shots[1].MeanX
	t.Logf("instanced box foreground mean x: %.1f px at frame>=30, %.1f px at frame 240 (%d and %d foreground pixels)", early, late, state.Shots[0].Orange, state.Shots[1].Orange)
	if late-early < 80 {
		t.Fatalf("the box moved %.1f px, want at least 80: GSXI frames from the worker did not drive it", late-early)
	}
	magics := map[string]string{}
	for _, f := range state.Frames {
		magics[fmt.Sprint(f["method"])] = fmt.Sprint(f["magic"])
	}
	for method, magic := range map[string]string{"dispatchPoseFrame": "GSP2", "dispatchMotionFrame": "GSP3", "dispatchClipFrame": "GSP4"} {
		if magics[method] != magic {
			t.Errorf("%s received %q, want %q (frames seen: %v)", method, magics[method], magic, state.Frames)
		}
	}
	if state.Signal == nil || num(report(state.Signal), "rev") != 240 {
		t.Errorf("shared signal $sim = %v, want rev 240", state.Signal)
	}
	for _, ev := range state.Events {
		if (ev["name"] == "thread-fallback" && !mainThread) || ev["name"] == "worker-error" {
			t.Errorf("unexpected event %v", ev)
		}
	}
}

func inputEngine() engine.Config {
	cfg := workerEngine(`{"scenario":"input"}`)
	cfg.Capabilities = []engine.Capability{engine.CapKeyboard, engine.CapPointer, engine.CapGamepad}
	return cfg
}

// Page input reaches a worker with timestamps: keyboard (auto-repeat dropped),
// pointer, and a gamepad the page polls (navigator.getGamepads does not exist in a
// worker). The worker places each event on its own clock and measures the delay.
func TestWorkerReceivesTimestampedPageInput(t *testing.T) {
	dir := assets(t)
	ts := serve(t, dir, func(*http.Request) string { return page(t, pageOptions{engines: []engine.Config{inputEngine()}}) })
	state := driveScript(t, ts.URL+"/", `window.__done === true`, 45*time.Second, "scripts/input.mjs")
	if e := state.find("input-error"); e != nil {
		t.Fatalf("input error: %v", e)
	}
	var events []report
	for _, r := range state.Reports {
		if r["kind"] == "input" {
			events = append(events, r)
		}
	}
	want := func(pred func(report) bool, what string) report {
		t.Helper()
		for _, e := range events {
			if pred(e) {
				return e
			}
		}
		t.Fatalf("no %s event among %d forwarded events: %v", what, len(events), events)
		return nil
	}
	keyDown := want(func(e report) bool { return e["k"] == "key" && e["code"] == "KeyA" && e["down"] == true }, "KeyA down")
	if keyDown["keyA"] != true {
		t.Errorf("KeyDown(KeyA) was false while handling the key-down event: %v", keyDown)
	}
	want(func(e report) bool { return e["k"] == "key" && e["code"] == "KeyA" && e["down"] == false }, "KeyA up")
	downs := 0
	for _, e := range events {
		if e["k"] == "key" && e["code"] == "KeyA" && e["down"] == true {
			downs++
		}
	}
	if downs != 1 {
		t.Errorf("auto-repeat must not be forwarded: %d KeyA downs", downs)
	}
	want(func(e report) bool {
		return e["k"] == "pointer" && e["type"] == "move" && num(e, "x") == 60 && num(e, "y") == 70
	}, "pointer move to (60,70)")
	want(func(e report) bool { return e["k"] == "pointer" && e["type"] == "down" && num(e, "mask") == 1 }, "pointer down")
	want(func(e report) bool {
		return e["k"] == "pad" && e["connected"] == true && fmt.Sprint(e["buttons"]) == "[0 0 0 0]"
	}, "pad connect")
	want(func(e report) bool {
		return e["k"] == "pad" && e["connected"] == true && fmt.Sprint(e["buttons"]) == "[1 0 0 0]"
	}, "pad button press")
	want(func(e report) bool { return e["k"] == "pad" && fmt.Sprint(e["axes"]) == "[0.75 -0.5]" }, "pad stick")
	want(func(e report) bool { return e["k"] == "pad" && e["connected"] == false }, "pad disconnect")
	pads := 0
	for _, e := range events {
		if e["k"] == "pad" && e["connected"] == true {
			pads++
		}
	}
	if pads != 3 {
		t.Errorf("an unchanged pad must be sent once per change: %d pad states", pads)
	}
	// Timestamps: ordered on the worker clock, and the delay is small.
	var maxDelay, prevAt float64
	for _, e := range events {
		at, delay := num(e, "at"), num(e, "delay")
		if delay < 0 || delay > 250 {
			t.Errorf("event %v has delay %.1f ms", e, delay)
		}
		if delay > maxDelay {
			maxDelay = delay
		}
		if k := e["k"]; k == "key" || k == "pointer" {
			if at < prevAt-1 {
				t.Errorf("event timestamps went backwards: %.1f after %.1f", at, prevAt)
			}
			prevAt = at
		}
	}
	t.Logf("%d forwarded events, longest page-to-worker delay %.2f ms", len(events), maxDelay)
}
