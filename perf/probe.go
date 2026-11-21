package perf

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	cdpperf "github.com/chromedp/cdproto/performance"
	"github.com/chromedp/cdproto/profiler"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The probes below measure a page from a check that drives its own tab with
// chromedp (for example through package browsertest): they take the tab's
// context, not a Driver.

// FrameProbe is an init script (page.AddScriptToEvaluateOnNewDocument) that
// measures the frames a page draws. It wraps requestAnimationFrame so it can
// add up the work every callback does in one frame, records the interval
// between drawn frames and long tasks while it is on, and notes named marks:
// the page time of the first frame on which a test registered with when()
// holds, or the time mark() was called.
//
//	window.__gosx_frame_probe.when("ready", () => document.documentElement.dataset.engine === "ready")
//	window.__gosx_frame_probe.mark("start")
//
// StartFrameProbe and StopFrameProbe turn it on and off.
const FrameProbe = `(() => {
  if (window.__gosx_frame_probe) return;
  const P = window.__gosx_frame_probe = {on: false, frames: [], work: new Map(), tasks: [], marks: {}, tests: {}};
  P.when = (name, test) => { P.tests[name] = test; };
  P.mark = (name) => { if (!P.marks[name]) P.marks[name] = performance.now(); };
  try { new PerformanceObserver((l) => { for (const e of l.getEntries()) P.tasks.push([e.startTime, e.duration]); }).observe({type: "longtask", buffered: true}); } catch (err) {}
  const raf = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = (cb) => raf((t) => {
    const s = performance.now();
    try { cb(t); } finally { if (P.on) P.work.set(t, (P.work.get(t) || 0) + performance.now() - s); }
  });
  let last = 0;
  const tick = (t) => {
    for (const name in P.tests) {
      if (!P.marks[name]) { try { if (P.tests[name]()) P.marks[name] = performance.now(); } catch (err) {} }
    }
    if (P.on && last) P.frames.push(t - last);
    last = t;
    raf(tick);
  };
  raf(tick);
})();`

// FrameProbeData is what FrameProbe recorded while it was on.
type FrameProbeData struct {
	// Frames are the intervals between drawn frames, in milliseconds.
	Frames []float64 `json:"frames"`
	// Work is each frame's requestAnimationFrame callback time, in ms.
	Work []float64 `json:"work"`
	// Tasks are the long tasks since the page loaded: start and duration.
	Tasks [][2]float64 `json:"tasks"`
	// Marks are the page times (ms since navigation) of the named marks.
	Marks map[string]float64 `json:"marks"`
}

// StartFrameProbe clears the recorded frames and work and starts recording.
// It returns the page time it started at.
func StartFrameProbe(ctx context.Context) (float64, error) {
	var t0 float64
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const P = window.__gosx_frame_probe; P.frames = []; P.work = new Map(); P.on = true; return performance.now(); })()`, &t0))
	return t0, err
}

// StopFrameProbe stops recording and returns what was recorded.
func StopFrameProbe(ctx context.Context) (FrameProbeData, error) {
	var d FrameProbeData
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const P = window.__gosx_frame_probe; P.on = false;
	  return {frames: P.frames, work: [...P.work.values()], tasks: P.tasks, marks: P.marks}; })()`, &d))
	return d, err
}

// Spread summarises a series of milliseconds. Percentiles take the value at
// floor(n*p) of the sorted series. Late counts values over 1.5 times the
// median: frames that missed the browser's frame rate whatever that rate is
// (at a 60 Hz cap, timer jitter alone puts many intervals at 16.8 ms).
type Spread struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	Max    float64 `json:"max"`
	Over16 int     `json:"over16_7"`
	Over33 int     `json:"over33_3"`
	Late   int     `json:"over_1_5x_median"`
}

// SpreadOf summarises v; an empty series gives the zero Spread.
func SpreadOf(v []float64) Spread {
	if len(v) == 0 {
		return Spread{}
	}
	s := slices.Sorted(slices.Values(v))
	q := func(p float64) float64 { return s[min(len(s)-1, int(float64(len(s))*p))] }
	r := Spread{N: len(s), P50: q(.5), P95: q(.95), P99: q(.99), Max: s[len(s)-1]}
	for _, x := range s {
		r.Mean += x / float64(len(s))
		if x > 16.7 {
			r.Over16++
		}
		if x > 33.3 {
			r.Over33++
		}
		if x > 1.5*r.P50 {
			r.Late++
		}
	}
	return r
}

// Environment names the browser, the screen and the GPU a measurement ran on.
type Environment struct {
	UA      string  `json:"ua"`
	Version string  `json:"version"`
	Backend string  `json:"backend"`
	WebGL   string  `json:"webgl"`
	WebGPU  string  `json:"webgpu"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	DPR     float64 `json:"dpr"`
	Load    float64 `json:"load"`
}

const environmentJS = `(async () => {
  const e = document.querySelector("[data-gosx-scene3d-backend]");
  const out = {ua: navigator.userAgent, width: innerWidth, height: innerHeight, dpr: devicePixelRatio,
    backend: e ? e.getAttribute("data-gosx-scene3d-backend") : "", webgl: "", webgpu: "", version: "", load: 0};
  const nav = performance.getEntriesByType("navigation")[0];
  out.load = nav ? nav.loadEventEnd : 0;
  try {
    const h = await navigator.userAgentData.getHighEntropyValues(["fullVersionList", "platformVersion", "architecture"]);
    out.version = h.fullVersionList.filter(b => /chrom/i.test(b.brand)).map(b => b.brand + " " + b.version).join(", ") +
      "; " + h.platform + " " + h.platformVersion + " " + h.architecture;
  } catch (err) { out.version = ""; }
  try {
    const gl = document.createElement("canvas").getContext("webgl");
    const ext = gl && gl.getExtension("WEBGL_debug_renderer_info");
    out.webgl = gl ? String(gl.getParameter(ext ? ext.UNMASKED_RENDERER_WEBGL : gl.RENDERER)) : "no WebGL";
    const lose = gl && gl.getExtension("WEBGL_lose_context");
    if (lose) lose.loseContext();
  } catch (err) { out.webgl = "error: " + err; }
  try {
    const a = navigator.gpu && await navigator.gpu.requestAdapter();
    const i = a && a.info;
    out.webgpu = !navigator.gpu ? "no navigator.gpu" : !a ? "no adapter" :
      [i.vendor, i.architecture, i.device, i.description].filter(Boolean).join(" ") || "adapter without info";
  } catch (err) { out.webgpu = "error: " + err; }
  return out;
})()`

// ReadEnvironment names the browser, screen and GPU. Run it after a
// measurement: it opens a WebGL context and asks for a WebGPU adapter of its
// own.
func ReadEnvironment(ctx context.Context) (Environment, error) {
	var env Environment
	err := chromedp.Run(ctx, chromedp.Evaluate(environmentJS, &env, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
	return env, err
}

// ReadSceneCounters reads the Scene3D renderer's running counters
// (window.__gosx_scene3d_counters): planner plans, cache hits, rebuilds,
// CSS record hashes, full vertex hash scans, material signatures and WebGPU
// buffer writes. Divide the difference of two reads by the frames between.
func ReadSceneCounters(ctx context.Context) (map[string]float64, error) {
	var out map[string]float64
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const c = window.__gosx_scene3d_counters || {}, p = c.planner || {};
  return {plans: p.plans || 0, planCacheHits: p.cacheHits || 0, planRebuilds: p.rebuilds || 0,
    cssRecordHashes: p.cssRecordHashes || 0, fullVertexHashScans: p.fullVertexHashScans || 0,
    materialSignatures: c.materialSignatures || 0, writeBuffers: c.writeBuffers || 0};
})()`, &out))
	return out, err
}

// ReadSceneAttributes reads the renderer's own counts off the Scene3D mount:
// the data-gosx-scene3d-* attributes about draws, meshes, vertices, shadows,
// skinning, instances, passes, materials, pipelines, quality, bytes and the
// GPU, without the prefix.
func ReadSceneAttributes(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
  const e = document.querySelector("[data-gosx-scene3d-backend]");
  const out = {};
  if (!e) return out;
  for (const a of e.attributes) {
    if (/^data-gosx-scene3d-/.test(a.name) && /draw|mesh|vert|tri|shadow|skin|instanc|object|pass|material|pipeline|quality|bytes|tier|pixel|gpu|rung/.test(a.name)) {
      out[a.name.replace("data-gosx-scene3d-", "")] = a.value;
    }
  }
  return out;
})()`, &out))
	return out, err
}

// HeapMB reads the tab's JavaScript heap in use and the ArrayBuffer backing
// store, in megabytes.
func HeapMB(ctx context.Context) (heap, buffers float64, err error) {
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		used, _, _, backing, err := runtime.GetHeapUsage().Do(ctx)
		heap, buffers = used/1e6, backing/1e6
		return err
	}))
	return heap, buffers, err
}

// MainThreadTimes reads Chrome's running performance metrics for the tab
// (TaskDuration, ScriptDuration and the rest, in seconds).
func MainThreadTimes(ctx context.Context) (map[string]float64, error) {
	m := map[string]float64{}
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		metrics, err := cdpperf.GetMetrics().Do(ctx)
		for _, x := range metrics {
			m[x.Name] = x.Value
		}
		return err
	}))
	return m, err
}

// HotFunctions names the functions with the most self time in a CPU profile:
// first the self time by script (to tell page code, the renderer, the Go
// engine and the browser apart; script 0 is native), then the top n functions
// with up to four callers and the first characters of their source. source
// returns a script's text by id, or "".
func HotFunctions(p *profiler.Profile, n int, source func(scriptID string) string) []string {
	byID := map[int64]*profiler.ProfileNode{}
	parent := map[int64]int64{}
	for _, node := range p.Nodes {
		byID[node.ID] = node
		for _, c := range node.Children {
			parent[c] = node.ID
		}
	}
	type frame struct {
		name, script string
		line, col    int64
	}
	frameOf := func(node *profiler.ProfileNode) frame {
		f := frame{name: node.CallFrame.FunctionName, script: string(node.CallFrame.ScriptID), line: node.CallFrame.LineNumber, col: node.CallFrame.ColumnNumber}
		if f.name == "" {
			f.name = "(anonymous)"
		}
		return f
	}
	self := map[frame]float64{}
	sample := map[frame]int64{}
	var total float64
	for i, id := range p.Samples {
		node := byID[id]
		if node == nil || i >= len(p.TimeDeltas) {
			continue
		}
		dt := float64(p.TimeDeltas[i]) / 1000
		f := frameOf(node)
		self[f] += dt
		sample[f] = id
		total += dt
	}
	keys := slices.SortedFunc(maps.Keys(self), func(a, b frame) int {
		if self[a] > self[b] {
			return -1
		}
		return 1
	})
	var out []string
	byScript := map[string]float64{}
	for k, v := range self {
		byScript[k.script] += v
	}
	for _, id := range slices.SortedFunc(maps.Keys(byScript), func(a, b string) int {
		if byScript[a] > byScript[b] {
			return -1
		}
		return 1
	}) {
		out = append(out, fmt.Sprintf("%7.1f ms %4.1f%%  script %s in all", byScript[id], 100*byScript[id]/total, id))
	}
	for _, k := range keys[:min(n, len(keys))] {
		chain := ""
		for id, d := parent[sample[k]], 0; id != 0 && d < 4; id, d = parent[id], d+1 {
			chain += " < " + frameOf(byID[id]).name
		}
		snippet := ""
		if src := source(k.script); src != "" {
			lines := strings.Split(src, "\n")
			if int(k.line) < len(lines) && int(k.col) < len(lines[k.line]) {
				l := lines[k.line][k.col:]
				snippet = " | " + l[:min(120, len(l))]
			}
		}
		out = append(out, fmt.Sprintf("%7.1f ms %4.1f%%  %s [script %s %d:%d]%s%s", self[k], 100*self[k]/total, k.name, k.script, k.line, k.col, chain, snippet))
	}
	return out
}
