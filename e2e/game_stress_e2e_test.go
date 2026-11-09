//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/profiler"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/examples/game-stress/stage"
	"m31labs.dev/gosx/examples/game-stress/stress"
	"m31labs.dev/gosx/examples/game-stress/web"
)

// The game-stress benchmark (examples/game-stress, docs/game-performance.md).
//
// TestGameStressSmoke is the CI version: small counts, both threads, loose
// budgets that hold on a software rasterizer, and a determinism check of the
// simulation against the native build. TestGameStressBenchmark is the full
// measurement; it runs only with GOSX_STRESS_BENCH=1, on the GPU runners:
//
//	GOSX_STRESS_BENCH=1 gpu-chrome.sh run -- go test -tags e2e -timeout 40m -run GameStressBenchmark -v ./e2e
//	GOSX_STRESS_BENCH=1 GPU_CHROME_EXCLUSIVE=1 mac-gpu-run <wt> go test -tags e2e -timeout 40m -run GameStressBenchmark -v ./e2e
//
// GOSX_STRESS_RUNS picks the runs (default all: webgpu and webgl, main and
// worker, each also at a 4x CPU slowdown of the page's main thread), GOSX_STRESS_SECONDS the measured
// seconds per run (30), GOSX_STRESS_OUT a directory for the JSON of each run,
// and GOSX_CICADA_FIXTURE a directory with Cicada's fixture.json and
// kernel.wasm for live music.

type stressSpec struct {
	Backend, Thread string
	Throttle        float64
	Players, Crowd  int
	Sections        int
	Width, Height   int
	Seconds         float64
	Warmup          float64
	Audio           string
	Probes          int
	NoCull          bool // turn off GPU frustum culling of the crowd (WebGPU)
}

func (s stressSpec) label() string {
	l := s.Backend + "/" + s.Thread
	if s.Throttle > 1 {
		l += fmt.Sprintf("@%gx", s.Throttle)
	}
	return l
}

func (s stressSpec) query() string {
	q := url.Values{}
	q.Set("backend", s.Backend)
	q.Set("thread", s.Thread)
	q.Set("players", strconv.Itoa(s.Players))
	q.Set("crowd", strconv.Itoa(s.Crowd))
	q.Set("sections", strconv.Itoa(s.Sections))
	q.Set("width", strconv.Itoa(s.Width))
	q.Set("height", strconv.Itoa(s.Height))
	q.Set("audio", s.Audio)
	if s.NoCull {
		q.Set("cull", "0")
	}
	return q.Encode()
}

var (
	stressBuildOnce sync.Once
	stressWASM      string
	stressBuildErr  error
)

func startStressServer(t *testing.T) string {
	t.Helper()
	root := e2eRepoRoot(t)
	stressBuildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "game-stress-e2e")
		if err != nil {
			stressBuildErr = err
			return
		}
		stressWASM = filepath.Join(dir, "stress.wasm")
		cmd := exec.Command("go", "build", "-o", stressWASM, "./examples/game-stress/engine")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			stressBuildErr = fmt.Errorf("build the stress engine: %v\n%s", err, out)
		}
	})
	if stressBuildErr != nil {
		t.Fatal(stressBuildErr)
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	cicada := os.Getenv("GOSX_CICADA_FIXTURE")
	for _, name := range []string{"fixture.json", "kernel.wasm"} {
		if _, err := os.Stat(filepath.Join(cicada, name)); cicada != "" && err != nil {
			t.Logf("GOSX_CICADA_FIXTURE has no %s; music disabled", name)
			cicada = ""
		}
	}
	// GOSX_STRESS_CLIENT_ROOT serves the browser runtime from another
	// checkout, for example bundles built with GOSX_BUNDLE_DEBUG=1 (source
	// maps) for a readable CPU profile.
	clientRoot := root
	if alt := os.Getenv("GOSX_STRESS_CLIENT_ROOT"); alt != "" {
		clientRoot = alt
	}
	h, err := web.Handler(web.Options{Root: clientRoot, EngineWASM: stressWASM, CicadaDir: cicada,
		WASMExec: filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js")})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	if e2eWindowsRelay() {
		return e2eExposeToWindows(t, ts.URL)
	}
	return ts.URL
}

// stressRaw is what __stress.collect returns.
type stressRaw struct {
	Run struct {
		Run   web.Run `json:"run"`
		Audio string  `json:"audio"`
	} `json:"run"`
	UA    string `json:"ua"`
	Mount struct {
		Backend, Fallback string
		Mounted, Ready    bool
	} `json:"mount"`
	Frames []struct {
		TS, At, Raf, Msg, Gap, Bundle, Anim, Render float64
		N                                           int
	} `json:"frames"`
	Go  [][]float64 `json:"go"`
	GPU []struct {
		Renderer string `json:"renderer"`
		Timing   struct {
			Status, Source string
			GPUMS          *float64 `json:"gpuMS"`
			CPUSubmitMS    float64  `json:"cpuSubmitMS"`
		} `json:"timing"`
	} `json:"gpu"`
	Latency []struct {
		MS   float64 `json:"ms"`
		Sync bool    `json:"sync"`
	} `json:"latency"`
	Errors      []map[string]any `json:"errors"`
	Probes      []map[string]any `json:"probes"`
	Audio       map[string]any   `json:"audio"`
	PoseFrames  map[string]any   `json:"poseFrames"`
	TimedPoses  map[string]any   `json:"timedPoses"`
	Bundle      map[string]any   `json:"bundle"`
	WebGPUStats map[string]any   `json:"webgpuStats"`
	Now         float64          `json:"now"`
}

type dist struct {
	N                              int
	Mean, P50, P95, P99, Max, Over float64 // Over: share of frames above the stat's budget
}

func distOf(values []float64, budget float64) dist {
	if len(values) == 0 {
		return dist{}
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	at := func(p float64) float64 { return s[min(len(s)-1, int(math.Ceil(p*float64(len(s))))-1)] }
	var sum float64
	over := 0
	for _, v := range s {
		sum += v
		if budget > 0 && v > budget {
			over++
		}
	}
	return dist{N: len(s), Mean: sum / float64(len(s)), P50: at(0.5), P95: at(0.95), P99: at(0.99), Max: s[len(s)-1], Over: float64(over) / float64(len(s))}
}

// stressSummary is the per-run result written to JSON and to the table.
type stressSummary struct {
	Label, Backend, Thread, UA string
	Throttle                   float64
	FPS                        float64
	TicksPerSecond             float64
	Frames                     int
	MainThread                 dist // all main-thread frame work: rAF callbacks plus worker message handlers
	Gap                        dist // frame-to-frame interval
	Sim, AI, Anim, Render, GC  dist // budget lines, main thread only (zero for the parts a worker runs)
	JSAnim, Bundle, Msg        dist
	GoHook, GoGCPause          dist // the engine's own frame and GC pause, on whichever thread runs it
	WorkerSim, WorkerAI        dist // worker runs: the same lines on the worker thread
	WorkerAnim, WorkerGC       dist
	GPU                        dist
	GPUSource                  string
	LatencyMS                  dist
	Replayed, Mismatches       int
	Hashes                     map[string]string
	Allocs                     dist // KiB allocated per frame by Go
	AudioState                 string
	Errors                     []string
	// Fallback is the mount's renderer fallback reason; Console holds the
	// start of the page console when the backend is not the one asked for.
	Fallback, Console string
}

func summarize(spec stressSpec, raw stressRaw) stressSummary {
	s := stressSummary{Label: spec.label(), Backend: raw.Mount.Backend, Thread: spec.Thread, UA: raw.UA, Throttle: spec.Throttle,
		Hashes: map[string]string{}, Frames: len(raw.Frames), Fallback: raw.Mount.Fallback}
	var mainT, gap, render, bundle, jsAnim, msg []float64
	for i, f := range raw.Frames {
		mainT = append(mainT, f.Raf+f.Msg)
		if i > 0 {
			gap = append(gap, f.Gap)
		}
		render = append(render, f.Render+f.Bundle)
		bundle = append(bundle, f.Bundle)
		jsAnim = append(jsAnim, f.Anim)
		msg = append(msg, f.Msg)
	}
	if n := len(raw.Frames); n > 1 {
		s.FPS = float64(n-1) / ((raw.Frames[n-1].At - raw.Frames[0].At) / 1000)
	}
	from, to := 0.0, math.Inf(1)
	if len(raw.Frames) > 0 {
		from, to = raw.Frames[0].At, raw.Frames[len(raw.Frames)-1].At
	}
	var hook, sim, ai, anim, gcCPU, gcPause, alloc []float64
	var ticks float64
	var t0, t1 float64
	for _, r := range raw.Go {
		if len(r) < stage.GoRecordFloats {
			continue
		}
		if r[stage.RecHashTick] > 0 {
			h := uint64(r[stage.RecHashHi])<<32 | uint64(r[stage.RecHashLo])
			s.Hashes[strconv.Itoa(int(r[stage.RecHashTick]))] = fmt.Sprintf("%#x", h)
		}
		s.Mismatches += int(r[stage.RecMismatches])
		at := r[stage.RecTimeMS]
		// A worker's clock has its own time origin; use every worker record
		// after the warm-up instead of the page's window.
		if spec.Thread == "main" && (at < from || at > to) {
			continue
		}
		if spec.Thread == "worker" && r[stage.RecFrame] < float64(len(raw.Go))-spec.Seconds*60 {
			continue
		}
		if t0 == 0 {
			t0 = at
		}
		t1 = at
		ticks += r[stage.RecTicks]
		s.Replayed += int(r[stage.RecReplayed])
		hook = append(hook, r[stage.RecHookMS])
		sim = append(sim, r[stage.RecSimMS])
		ai = append(ai, r[stage.RecAIMS])
		anim = append(anim, r[stage.RecAnimMS])
		gcCPU = append(gcCPU, r[stage.RecGCCPUMS])
		gcPause = append(gcPause, r[stage.RecGCPauseMS])
		alloc = append(alloc, r[stage.RecAllocKB])
	}
	if t1 > t0 {
		s.TicksPerSecond = ticks / ((t1 - t0) / 1000)
	}
	s.MainThread = distOf(mainT, 1000.0/60)
	s.Gap = distOf(gap, 1000.0/60*1.5)
	s.Render = distOf(render, 4)
	s.Bundle = distOf(bundle, 0)
	s.JSAnim = distOf(jsAnim, 0)
	s.Msg = distOf(msg, 0)
	s.GoHook = distOf(hook, 0)
	s.GoGCPause = distOf(gcPause, 0)
	s.Allocs = distOf(alloc, 0)
	animTotal := append([]float64(nil), anim...)
	if spec.Thread == "main" {
		s.Sim, s.AI, s.GC = distOf(sim, 3), distOf(ai, 2), distOf(gcCPU, 1)
		// Animation on the main thread: the engine's pose work plus the
		// renderer's model-animation pass, frame by frame where both exist.
		for i := range animTotal {
			if i < len(jsAnim) {
				animTotal[i] += jsAnim[i]
			}
		}
		s.Anim = distOf(animTotal, 2)
	} else {
		s.WorkerSim, s.WorkerAI, s.WorkerAnim, s.WorkerGC = distOf(sim, 3), distOf(ai, 2), distOf(anim, 2), distOf(gcCPU, 1)
		s.Anim = distOf(jsAnim, 2)
	}
	var gpu []float64
	for _, g := range raw.GPU {
		if g.Timing.GPUMS != nil && g.Timing.Status == "measured" {
			gpu = append(gpu, *g.Timing.GPUMS)
			s.GPUSource = g.Timing.Source
		} else if s.GPUSource == "" {
			s.GPUSource = g.Timing.Status
		}
	}
	s.GPU = distOf(gpu, 0)
	var lat []float64
	for _, l := range raw.Latency {
		lat = append(lat, l.MS)
	}
	s.LatencyMS = distOf(lat, 0)
	if raw.Audio != nil {
		s.AudioState = fmt.Sprint(raw.Audio["mode"], ":", raw.Audio["state"])
	}
	for _, e := range raw.Errors {
		s.Errors = append(s.Errors, fmt.Sprint(e["name"], ": ", e["detail"]))
	}
	for _, p := range raw.Probes {
		if p["kind"] == "error" {
			s.Errors = append(s.Errors, fmt.Sprint("engine: ", p["error"]))
		}
	}
	return s
}

// runStress loads one configuration, waits for it to run, measures it, then
// probes input-to-display latency.
func runStress(t *testing.T, base string, spec stressSpec) (stressSummary, stressRaw) {
	t.Helper()
	chrome := e2eChromePath(t)
	timeout := time.Duration(spec.Warmup+spec.Seconds)*time.Second + 4*time.Minute
	browser := newBrowserPage(t, chrome, map[string]any{"enable-unsafe-webgpu": true, "ignore-gpu-blocklist": true,
		"autoplay-policy": "no-user-gesture-required"}, spec.Width+40, spec.Height+80, "", timeout)
	ctx := browser.ctx
	if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(spec.Width), int64(spec.Height), 1, false)); err != nil {
		t.Fatalf("viewport: %v", err)
	}
	browser.navigate(t, base+"/?"+spec.query())
	// Wait until the scene is ready and the engine records frames.
	var state struct {
		Ready  bool
		Frames int
		Errors []map[string]any
	}
	deadline := time.Now().Add(150 * time.Second)
	for {
		browser.eval(t, `window.__stress ? __stress.running() : {}`, &state)
		if state.Ready && state.Frames > 0 {
			break
		}
		if time.Now().After(deadline) {
			var raw string
			browser.eval(t, `__stress.collect(0)`, &raw)
			t.Fatalf("%s: the scene never ran: %+v\nstate: %.2000s\nconsole: %.4000s", spec.label(), state, raw, browser.Console())
		}
		time.Sleep(250 * time.Millisecond)
	}
	if spec.Audio != "off" {
		var audio string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`__stress.startAudio()`, &audio, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true).WithUserGesture(true)
		})); err != nil {
			t.Logf("start audio: %v", err)
		}
		t.Logf("%s: audio %s", spec.label(), audio)
	}
	if spec.Throttle > 1 {
		if err := chromedp.Run(ctx, emulation.SetCPUThrottlingRate(spec.Throttle)); err != nil {
			t.Fatalf("throttle: %v", err)
		}
	}
	time.Sleep(time.Duration(spec.Warmup * float64(time.Second)))
	var from float64
	browser.eval(t, `performance.now()`, &from)
	// GOSX_STRESS_PROFILE=1 records a main-thread CPU profile of the measured
	// window (the profiler adds its own cost, so profile in a separate run).
	profiling := os.Getenv("GOSX_STRESS_PROFILE") == "1" && os.Getenv("GOSX_STRESS_OUT") != ""
	if profiling {
		if err := chromedp.Run(ctx, profiler.Enable(), profiler.SetSamplingInterval(200), profiler.Start()); err != nil {
			t.Fatalf("start profiler: %v", err)
		}
	}
	time.Sleep(time.Duration(spec.Seconds * float64(time.Second)))
	if profiling {
		var prof *profiler.Profile
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
			var err error
			prof, err = profiler.Stop().Do(c)
			return err
		})); err != nil {
			t.Fatalf("stop profiler: %v", err)
		}
		data, _ := json.Marshal(prof)
		_ = os.MkdirAll(os.Getenv("GOSX_STRESS_OUT"), 0o755)
		name := "game-stress-" + strings.NewReplacer("/", "-", "@", "-").Replace(spec.label()) + ".cpuprofile"
		_ = os.WriteFile(filepath.Join(os.Getenv("GOSX_STRESS_OUT"), name), data, 0o644)
	}
	var raw string
	browser.eval(t, fmt.Sprintf(`__stress.collect(%f)`, from), &raw)
	if dir := os.Getenv("GOSX_STRESS_OUT"); dir != "" {
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&shot)); err == nil {
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(filepath.Join(dir, "game-stress-"+strings.NewReplacer("/", "-", "@", "-").Replace(spec.label())+".png"), shot, 0o644)
		}
	}
	// Latency probes after the measured window: hold KeyM until the marked
	// figure is drawn, release, repeat.
	for i := 0; i < spec.Probes; i++ {
		key := func(kind input.KeyType) error {
			return chromedp.Run(ctx, input.DispatchKeyEvent(kind).WithCode("KeyM").WithKey("m").WithWindowsVirtualKeyCode(77))
		}
		if err := key(input.KeyDown); err != nil {
			t.Fatalf("key down: %v", err)
		}
		time.Sleep(350 * time.Millisecond)
		if err := key(input.KeyUp); err != nil {
			t.Fatalf("key up: %v", err)
		}
		time.Sleep(time.Duration(250+37*i%90) * time.Millisecond)
	}
	if spec.Throttle > 1 {
		_ = chromedp.Run(ctx, emulation.SetCPUThrottlingRate(1))
	}
	var latency string
	browser.eval(t, `JSON.stringify(__stress.latency)`, &latency)
	var parsed stressRaw
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("decode collect: %v (%.300s)", err, raw)
	}
	_ = json.Unmarshal([]byte(latency), &parsed.Latency)
	if errs := browser.PageErrors(); len(errs) > 0 {
		for _, e := range errs {
			parsed.Errors = append(parsed.Errors, map[string]any{"name": "pageerror", "detail": e})
		}
	}
	sum := summarize(spec, parsed)
	if sum.Backend != spec.Backend {
		console := browser.Console()
		var diag string
		browser.eval(t, `JSON.stringify({fallback: document.getElementById("arena").getAttribute("data-gosx-scene3d-renderer-fallback"), attrs: Array.from(document.getElementById("arena").attributes).filter(a => /webgpu|fallback|backend/.test(a.name)).map(a => a.name + "=" + a.value)})`, &diag)
		sum.Console = diag + "\n" + console[:min(len(console), 3000)]
	}
	return sum, parsed
}

// nativeStressHash is the hash the browser runs must report at tick t: the
// same workload stepped natively.
func nativeStressHash(players int, tick uint64) string {
	cfg := stage.EngineProps{Players: players}.SimConfig()
	s := stress.New(cfg)
	in := make([]byte, cfg.Seats)
	script := stress.ScriptedInput(7)
	for s.Tick() < tick {
		for seat := range in {
			in[seat] = script(s.Tick(), seat)
		}
		s.Step(in)
	}
	return fmt.Sprintf("%#x", s.Hash())
}

func stressTable(t *testing.T, runs []stressSummary) {
	t.Helper()
	f := func(d dist) string {
		if d.N == 0 {
			return "-"
		}
		return fmt.Sprintf("%.2f / %.2f / %.2f", d.P50, d.P95, d.P99)
	}
	t.Logf("p50 / p95 / p99 in ms per frame; budget lines are main-thread time")
	t.Logf("| run | backend | fps | main thread | sim (3) | AI (2) | anim (2) | render CPU (4) | Go GC (1) | worker sim+AI | GPU | input latency | ticks/s |")
	t.Logf("|---|---|---|---|---|---|---|---|---|---|---|---|---|")
	for _, s := range runs {
		worker := "-"
		if s.WorkerSim.N > 0 {
			worker = fmt.Sprintf("%.2f / %.2f", s.WorkerSim.P50+s.WorkerAI.P50, s.WorkerSim.P99+s.WorkerAI.P99)
		}
		t.Logf("| %s | %s | %.1f | %s | %s | %s | %s | %s | %s | %s | %s | %s | %.1f |", s.Label, s.Backend, s.FPS, f(s.MainThread), f(s.Sim), f(s.AI), f(s.Anim),
			f(s.Render), f(s.GC), worker, f(s.GPU), f(s.LatencyMS), s.TicksPerSecond)
	}
}

func writeStressJSON(t *testing.T, dir string, s stressSummary, raw stressRaw) {
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	name := strings.NewReplacer("/", "-", "@", "-").Replace(s.Label)
	data, _ := json.MarshalIndent(map[string]any{"summary": s, "raw": raw}, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "game-stress-"+name+".json"), data, 0o644); err != nil {
		t.Errorf("write %s: %v", name, err)
	}
}

// TestGameStressSmoke runs the stress scene with small counts on both
// threads and checks loose budgets that hold on a software rasterizer, plus
// determinism: the browser simulation, in a worker and on the main thread,
// reaches the hash the native build reaches at tick 300.
func TestGameStressSmoke(t *testing.T) {
	base := startStressServer(t)
	want := nativeStressHash(22, 300)
	backend := "webgl"
	if gpuRunnerAvailable() {
		backend = "webgpu"
	}
	var runs []stressSummary
	for _, thread := range []string{"main", "worker"} {
		// NoCull: GPU crowd culling loses the Metal device on Apple GPUs
		// (docs/game-performance.md); this test covers the worker path.
		spec := stressSpec{Backend: backend, Thread: thread, Players: 22, Crowd: 800, Sections: 4, Width: 480, Height: 270,
			Seconds: 6, Warmup: 6, Audio: "worklet", Probes: 3, NoCull: true}
		s, raw := runStress(t, base, spec)
		runs = append(runs, s)
		writeStressJSON(t, os.Getenv("GOSX_STRESS_OUT"), s, raw)
		if len(s.Errors) > 0 {
			t.Errorf("%s: errors: %v", s.Label, s.Errors)
		}
		if s.Hashes["300"] != want {
			t.Errorf("%s: state hash at tick 300 = %q, native build = %q: the simulation is not deterministic across threads", s.Label, s.Hashes["300"], want)
		}
		if s.Mismatches != 0 {
			t.Errorf("%s: %d replays changed the state", s.Label, s.Mismatches)
		}
		if s.TicksPerSecond < 24 || s.TicksPerSecond > 36 {
			t.Errorf("%s: %.1f simulation ticks per second, want about 30", s.Label, s.TicksPerSecond)
		}
		// A software rasterizer on a busy CI host draws a few frames per
		// second; the floor only proves the page keeps drawing.
		if s.Frames < 15 {
			t.Errorf("%s: only %d frames in %gs", s.Label, s.Frames, spec.Seconds)
		}
		if s.GoHook.P95 > 40 {
			t.Errorf("%s: engine frame p95 %.1f ms, want under 40", s.Label, s.GoHook.P95)
		}
		if s.LatencyMS.N == 0 {
			t.Errorf("%s: no input reached the screen in %d presses", s.Label, spec.Probes)
		}
		if thread == "worker" {
			if s.TimedPosesApplied(raw) == 0 {
				t.Errorf("%s: the page applied no interpolated pose: %v", s.Label, raw.TimedPoses)
			}
		} else if raw.PoseFrames == nil || raw.PoseFrames["accepted"] == nil {
			t.Errorf("%s: no pose frame reached the renderer: %v", s.Label, raw.PoseFrames)
		}
	}
	stressTable(t, runs)
}

// TimedPosesApplied reads the page interpolator's applied count.
func (s stressSummary) TimedPosesApplied(raw stressRaw) float64 {
	v, _ := raw.TimedPoses["applied"].(float64)
	return v
}

// TestGameStressBenchmark is the full measurement at the jam budget's counts.
func TestGameStressBenchmark(t *testing.T) {
	if os.Getenv("GOSX_STRESS_BENCH") != "1" {
		t.Skip("set GOSX_STRESS_BENCH=1 (on a GPU runner) to run the full benchmark")
	}
	base := startStressServer(t)
	seconds := 30.0
	if v, err := strconv.ParseFloat(os.Getenv("GOSX_STRESS_SECONDS"), 64); err == nil && v > 0 {
		seconds = v
	}
	crowd := 40000
	if v, err := strconv.Atoi(os.Getenv("GOSX_STRESS_CROWD")); err == nil && v >= 0 {
		crowd = v
	}
	audio := "worklet"
	if os.Getenv("GOSX_CICADA_FIXTURE") != "" {
		audio = "cicada"
	}
	names := os.Getenv("GOSX_STRESS_RUNS")
	if names == "" {
		names = "webgpu/main,webgpu/worker,webgl/main,webgl/worker,webgpu/main@4,webgpu/worker@4,webgl/main@4,webgl/worker@4"
	}
	want := nativeStressHash(22, 300)
	var runs []stressSummary
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		parts := strings.SplitN(name, "@", 2)
		bt := strings.SplitN(parts[0], "/", 2)
		if len(bt) != 2 {
			t.Fatalf("bad run %q: want backend/thread[@throttle]", name)
		}
		spec := stressSpec{Backend: bt[0], Thread: bt[1], Throttle: 1, Players: 22, Crowd: crowd, Sections: 8,
			Width: 1920, Height: 1080, Seconds: seconds, Warmup: 8, Audio: audio, Probes: 10}
		if len(parts) == 2 {
			spec.Throttle, _ = strconv.ParseFloat(parts[1], 64)
		}
		spec.NoCull = os.Getenv("GOSX_STRESS_CULL") == "0"
		s, raw := runStress(t, base, spec)
		writeStressJSON(t, os.Getenv("GOSX_STRESS_OUT"), s, raw)
		if s.Hashes["300"] != "" && s.Hashes["300"] != want {
			t.Errorf("%s: state hash at tick 300 = %s, native %s", s.Label, s.Hashes["300"], want)
		}
		js, _ := json.Marshal(s)
		t.Logf("%s summary: %s", s.Label, js)
		runs = append(runs, s)
	}
	stressTable(t, runs)
}
