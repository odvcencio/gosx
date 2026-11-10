//go:build e2e && drivespike

package drivespike

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"m31labs.dev/gosx/examples/game-stress/stage"
	"m31labs.dev/gosx/examples/game-stress/stress"
	"m31labs.dev/gosx/examples/game-stress/web"
)

type workerSession struct {
	ID       target.ID
	URL      string
	ctx      context.Context
	v0       float64
	waiting  bool
	err      error
	attachMS float64
	ready    chan struct{}
}

type workerHub struct {
	tb      *tab
	cfg     map[string]any
	mu      sync.Mutex
	workers []*workerSession
	added   chan *workerSession
}

type probeStats struct {
	V                   float64
	FrameIndex          int
	WorkerPosted        int
	WorkerReceived      int
	PostedToWorkers     int
	ReceivedFromWorkers int
	TimersRun           int
	Gpu                 bool
}

// autoAttachWorkers makes Chrome hold each new worker before its first script
// (Target.setAutoAttach with waitForDebuggerOnStart). For each worker it opens
// a session, installs the probe with the page's current virtual time as the
// worker's origin, then resumes it.
func (tb *tab) autoAttachWorkers(t *testing.T, cfg map[string]any) *workerHub {
	h := &workerHub{tb: tb, cfg: cfg, added: make(chan *workerSession, 16)}
	chromedp.ListenTarget(tb.ctx, func(ev any) {
		e, ok := ev.(*target.EventAttachedToTarget)
		if !ok || e.TargetInfo == nil || e.TargetInfo.Type != "worker" {
			return
		}
		ws := &workerSession{ID: e.TargetInfo.TargetID, URL: e.TargetInfo.URL, waiting: e.WaitingForDebugger, ready: make(chan struct{})}
		h.mu.Lock()
		h.workers = append(h.workers, ws)
		h.mu.Unlock()
		go h.install(ws)
	})
	if err := chromedp.Run(tb.ctx, target.SetAutoAttach(true, true).WithFlatten(true)); err != nil {
		t.Fatalf("Target.setAutoAttach: %v", err)
	}
	return h
}

func (h *workerHub) install(ws *workerSession) {
	defer close(ws.ready)
	defer func() { h.added <- ws }()
	start := time.Now()
	_ = h.tb.tryEval(`__drive.now()`, &ws.v0)
	cfg := map[string]any{}
	for k, v := range h.cfg {
		cfg[k] = v
	}
	cfg["v0"] = ws.v0
	ctx, _ := chromedp.NewContext(h.tb.ctx, chromedp.WithTargetID(ws.ID))
	ws.ctx = ctx
	if err := chromedp.Run(ctx, chromedp.Evaluate(probeSource(cfg), nil)); err != nil {
		ws.err = fmt.Errorf("install probe in worker: %w", err)
	}
	if err := chromedp.Run(ctx, cdpruntime.RunIfWaitingForDebugger()); err != nil && ws.err == nil {
		ws.err = fmt.Errorf("resume worker: %w", err)
	}
	ws.attachMS = float64(time.Since(start).Microseconds()) / 1000
}

func (h *workerHub) list() []*workerSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]*workerSession(nil), h.workers...)
}

func (h *workerHub) waitWorker(bound time.Duration) *workerSession {
	select {
	case ws := <-h.added:
		return ws
	case <-time.After(bound):
		return nil
	}
}

func (ws *workerSession) stats() (probeStats, error) {
	var s probeStats
	err := evalIn(ws.ctx, `__drive.stats()`, &s)
	return s, err
}

func (ws *workerSession) stepTo(target float64) (stepResult, error) {
	var r stepResult
	err := evalIn(ws.ctx, fmt.Sprintf(`__drive.step(Math.max(0, %v - __drive.now()))`, target), &r)
	return r, err
}

type lockstepCost struct {
	Frames     int
	WallMS     float64
	DrainWaits int
	Timeouts   int
}

// lockstep moves the page and every attached worker to the next n frame
// boundaries together. Per frame: wait until each worker received what the
// page posted, step the workers, wait until the page received what the workers
// posted, step the page.
func (h *workerHub) lockstep(t *testing.T, n int) lockstepCost {
	tb := h.tb
	c := lockstepCost{Frames: n}
	start := time.Now()
	for i := 0; i < n; i++ {
		var ps probeStats
		tb.eval(`__drive.stats()`, &ps)
		target := frameAt(ps.FrameIndex + 1)
		posted := 0
		for _, ws := range h.list() {
			<-ws.ready
			if ws.err != nil {
				continue
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				s, err := ws.stats()
				if err != nil || s.WorkerReceived >= ps.PostedToWorkers || time.Now().After(deadline) {
					if time.Now().After(deadline) {
						c.Timeouts++
					}
					break
				}
				c.DrainWaits++
				time.Sleep(time.Millisecond)
			}
			if _, err := ws.stepTo(target); err != nil {
				t.Logf("worker step: %v", err)
			}
			if s, err := ws.stats(); err == nil {
				posted += s.WorkerPosted
			}
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			tb.eval(`__drive.stats()`, &ps)
			if ps.ReceivedFromWorkers >= posted {
				break
			}
			if time.Now().After(deadline) {
				c.Timeouts++
				break
			}
			c.DrainWaits++
			time.Sleep(time.Millisecond)
		}
		tb.stepTo(target)
	}
	c.WallMS = float64(time.Since(start).Microseconds()) / 1000
	return c
}

type wmsg struct {
	Kind     string
	PageV    float64
	PagePerf float64
	PageDate float64
	Perf     float64
	Date     float64
	Origin   float64
	V        *float64
	N        int
	First    *firstScript
}

// Experiment 5a: a dedicated worker held at start, given the probe, then resumed.
func TestExp5Worker(t *testing.T) {
	res := newResults(t, "5")
	defer res.summary()
	url := testdataServer(t)
	b := startBrowser(t)
	tb := b.newTab(t, 2*time.Minute)
	wall := 1.7e12
	tb.addProbe(map[string]any{"wall": wall})
	hub := tb.autoAttachWorkers(t, map[string]any{"wall": wall})
	tb.navigate(url + "/worker.html")
	tb.step(500)
	tb.eval(`startWorker()`, nil)
	ws := hub.waitWorker(10 * time.Second)
	if ws == nil {
		res.check("worker attached and held at start", false, "no Target.attachedToTarget for a worker in 10 s")
		return
	}
	res.check("worker attached and held at start (waitingForDebugger)", ws.waiting && ws.err == nil, "waiting=%v err=%v url=%s attach+install+resume=%.1fms", ws.waiting, ws.err, ws.URL, ws.attachMS)
	if !tb.waitFor(`wlog.some(m => m.kind === "first")`, 10*time.Second) {
		res.check("worker ran after resume", false, "no first message\n%s", tb.Console())
		return
	}
	var log []wmsg
	tb.eval(`wlog`, &log)
	f := log[0].First
	res.check("probe installed before the worker's first script", f != nil && f.Probe && f.Patched, "first=%s", jsonString(f))
	res.check("worker clock starts at the page's virtual time (origin = wall + 500)", f != nil && f.Perf == 0 && near(f.Origin, wall+500, 1e-6) && f.Date == wall+500,
		"first=%s", jsonString(f))

	time.Sleep(700 * time.Millisecond)
	tb.eval(`wlog`, &log)
	res.check("worker timers do not run in 700 ms of real time without a step", len(log) == 1, "log=%s", jsonString(log))

	// Step only the page: the worker's timers must not move.
	tb.step(300)
	time.Sleep(100 * time.Millisecond)
	tb.eval(`wlog`, &log)
	res.check("stepping the page alone does not move the worker clock", len(log) == 1, "log=%s", jsonString(log))

	// Step only the worker to the page's time: 100, 250 and the 50 ms interval run, in order.
	var pv float64
	tb.eval(`__drive.now()`, &pv)
	r, err := ws.stepTo(pv)
	if err != nil {
		t.Fatalf("worker step: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // worker -> page messages are real tasks
	tb.eval(`wlog`, &log)
	var kinds []string
	var perfs []float64
	for _, m := range log[1:] {
		kinds = append(kinds, m.Kind)
		perfs = append(perfs, m.Perf)
	}
	want := "iv,t100,iv,iv,iv,t250,iv"
	res.check("worker step runs its due timers in order at their due times", strings.Join(kinds, ",") == want && fmt.Sprint(perfs) == "[50 100 100 150 200 250 250]",
		"kinds=%v perfs=%v step=%s", kinds, perfs, jsonString(r))
	// Keeping the clocks in step: page and worker report the same wall time.
	tb.eval(`ask()`, nil)
	time.Sleep(100 * time.Millisecond)
	tb.eval(`wlog`, &log)
	last := log[len(log)-1]
	var page struct{ V, Date, Perf, Origin float64 }
	tb.eval(`({v: __drive.now(), date: Date.now(), perf: performance.now(), origin: performance.timeOrigin})`, &page)
	res.check("after stepping both to the same instant they agree on Date and V", last.Kind == "now" && last.V != nil && *last.V == page.V && last.Date == page.Date && near(last.Origin+last.Perf, page.Origin+page.Perf, 1e-6),
		"worker=%s page=%+v", jsonString(last), page)

	// Lockstep 60 frames and compare.
	cost := hub.lockstep(t, 60)
	var wsS probeStats
	wsS, _ = ws.stats()
	var psS probeStats
	tb.eval(`__drive.stats()`, &psS)
	res.check("60 lockstep frames: page and worker clocks equal", wsS.V == psS.V && psS.V == frameAt(int(math.Round((800)/interval))+60), "page V=%v worker V=%v", psS.V, wsS.V)
	t.Logf("lockstep cost: %d frames in %.1f ms (%.2f ms/frame), drain waits %d, timeouts %d", cost.Frames, cost.WallMS, cost.WallMS/float64(cost.Frames), cost.DrainWaits, cost.Timeouts)
}

// Experiment 5b: GoSX's engine worker (game-stress, thread=worker) under the probe.
func TestExp5EngineWorker(t *testing.T) {
	res := newResults(t, "5/engine")
	defer res.summary()
	root := repoRoot(t)
	dir := t.TempDir()
	engineWASM := filepath.Join(dir, "stress.wasm")
	cmd := exec.Command("go", "build", "-o", engineWASM, "./examples/game-stress/engine")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "GOWORK=off", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the stress engine: %v\n%s", err, out)
	}
	goroot, _ := exec.Command("go", "env", "GOROOT").Output()
	h, err := web.Handler(web.Options{Root: root, EngineWASM: engineWASM, WASMExec: filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js")})
	if err != nil {
		t.Fatal(err)
	}
	base := serve(t, h)
	backend := "webgl"
	if gpuRunner() {
		backend = "webgpu"
	}
	b := startBrowser(t, cpuGLFlags...)
	tb := b.newTab(t, 6*time.Minute)
	wall := 1.7e12
	tb.addProbe(map[string]any{"wall": wall})
	hub := tb.autoAttachWorkers(t, map[string]any{"wall": wall})
	tb.navigate(base + "/?backend=" + backend + "&thread=worker&players=22&crowd=0&sections=2&width=320&height=180&audio=off")

	// Boot: lockstep frames, never stepping while a request is in flight, until the scene runs.
	bootStart := time.Now()
	bootFrames := 0
	var run struct {
		Ready  bool
		Frames int
		Errors []map[string]any
	}
	idleTimeouts := 0
	stuck := map[string]int{}
	for bootFrames < 3000 && time.Since(bootStart) < 3*time.Minute {
		if !tb.waitNetIdle(5 * time.Second) {
			idleTimeouts++
			for _, u := range tb.Inflight() {
				stuck[u]++
			}
		}
		hub.lockstep(t, 1)
		bootFrames++
		if bootFrames%10 == 0 {
			tb.eval(`window.__stress ? __stress.running() : {}`, &run)
			if run.Ready && run.Frames > 0 {
				break
			}
		}
	}
	workers := hub.list()
	res.check("the engine worker was attached, given the probe and resumed", len(workers) >= 1 && workers[0].err == nil, "workers=%d err=%v", len(workers), errOf(workers))
	res.check("the scene became ready and the engine posted frames", run.Ready && run.Frames > 0, "run=%s bootFrames=%d boot=%s\n%s", jsonString(run), bootFrames, time.Since(bootStart).Round(time.Millisecond), tb.Console())
	if !run.Ready {
		return
	}
	t.Logf("boot: %d lockstep frames, %s real; network-idle waits that hit the 5 s bound: %d; requests seen in flight then: %s", bootFrames, time.Since(bootStart).Round(time.Millisecond), idleTimeouts, jsonString(stuck))
	type rec struct{ Frame, TimeMS, SimTick, HashTick float64 }
	records := func() []rec {
		var raw [][]float64
		tb.eval(`(__stress.running(), __stress.go)`, &raw)
		var out []rec
		for _, r := range raw {
			if len(r) >= stage.GoRecordFloats {
				out = append(out, rec{r[stage.RecFrame], r[stage.RecTimeMS], r[stage.RecSimTick], r[stage.RecHashTick]})
			}
		}
		return out
	}
	before := records()
	lastBefore := before[len(before)-1]
	n := 690
	cost := hub.lockstep(t, n)
	after := records()
	var fresh []rec
	for _, r := range after {
		if r.Frame > lastBefore.Frame {
			fresh = append(fresh, r)
		}
	}
	// Records arrive in batches of 30 frames.
	exactGap, exactTick := true, true
	prev := lastBefore
	for _, r := range fresh {
		if !near(r.TimeMS-prev.TimeMS, (r.Frame-prev.Frame)*interval, 0.002) {
			exactGap = false
		}
		prev = r
	}
	if len(fresh) > 1 {
		f0, f1 := fresh[0], fresh[len(fresh)-1]
		exactTick = near(f1.SimTick-f0.SimTick, (f1.Frame-f0.Frame)/2, 1)
	}
	got := 0.0
	if len(fresh) > 0 {
		got = fresh[len(fresh)-1].Frame - lastBefore.Frame
	}
	res.check("engine frame clock fired once per stepped frame", len(fresh) > 0 && math.Abs(got-float64(n)) <= 30, "stepped=%d engineFrames=%v (records post every 30)", n, got)
	res.check("engine frame times are exactly 16.667 ms apart in virtual time", exactGap && len(fresh) > 0, "records=%d first=%+v last=%+v", len(fresh), firstRec(fresh), lastRec(fresh))
	res.check("simulation ticks at 30 Hz of virtual time", exactTick, "first=%+v last=%+v", firstRec(fresh), lastRec(fresh))
	var raw string
	tb.eval(`__stress.collect(0)`, &raw)
	hashes := stressHashes(raw)
	want := nativeHash(22, 300)
	if h, ok := hashes["300"]; ok {
		res.check("state hash at tick 300 equals the native build's", h == want, "browser=%s native=%s", h, want)
	} else {
		t.Logf("RESULT exp=5/engine browser=%s check=%q NOTRUN tick 300 not reached (hashes=%v)", browserLabel(), "state hash at tick 300", hashes)
	}
	t.Logf("lockstep cost: %d frames in %.0f ms = %.2f ms/frame; drain waits %d; timeouts %d", cost.Frames, cost.WallMS, cost.WallMS/float64(cost.Frames), cost.DrainWaits, cost.Timeouts)
	if errs := tb.Errors(); len(errs) > 0 {
		t.Logf("page errors: %v", errs)
	}
}

func errOf(ws []*workerSession) error {
	for _, w := range ws {
		if w.err != nil {
			return w.err
		}
	}
	return nil
}

func firstRec[T any](v []T) any {
	if len(v) == 0 {
		return nil
	}
	return v[0]
}

func lastRec[T any](v []T) any {
	if len(v) == 0 {
		return nil
	}
	return v[len(v)-1]
}

func stressHashes(raw string) map[string]string {
	var parsed struct{ Go [][]float64 }
	_ = json.Unmarshal([]byte(raw), &parsed)
	out := map[string]string{}
	for _, r := range parsed.Go {
		if len(r) >= stage.GoRecordFloats && r[stage.RecHashTick] > 0 {
			h := uint64(r[stage.RecHashHi])<<32 | uint64(r[stage.RecHashLo])
			out[strconv.Itoa(int(r[stage.RecHashTick]))] = fmt.Sprintf("%#x", h)
		}
	}
	return out
}

func nativeHash(players int, tick uint64) string {
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

