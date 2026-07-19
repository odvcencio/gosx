//go:build water_smoke

// water-visual-smoke renders the gosx water demo AND the evanw/webgl-water
// reference in the same Chrome on the same GPU (WSL2 -> Dozen Vulkan ->
// real hardware, WebGL path) and reports side-by-side FPS plus staged
// screenshots, so parity/efficiency claims are measured, not asserted.
//
//	# Terminal 1: docs server
//	PORT=8890 go run ./examples/gosx-docs
//	# Terminal 2:
//	WATER_SMOKE_SAVE_DIR=/tmp/water-smoke go run -tags water_smoke scripts/water-visual-smoke.go
//
// Env: WATER_SMOKE_URL (default http://127.0.0.1:8890/demos/water),
// WATER_SMOKE_REF_URL (default https://madebyevan.com/webgl-water/, empty to skip),
// WATER_SMOKE_SAVE_DIR (default /tmp/water-smoke),
// WATER_SMOKE_RUN_ID (default "1", tags output filenames for multi-run medians),
// WATER_SMOKE_STRESS (default "0"; "1" adds a stress phase after the idle/active
//
//	phases below: a bigger, higher-DPR viewport plus sustained interaction --
//	see the P3 water-parity-campaign shootout gate), WATER_SMOKE_STRESS_WIDTH /
//	WATER_SMOKE_STRESS_HEIGHT (default 2560x1440), WATER_SMOKE_STRESS_DPR
//	(default 2), WATER_SMOKE_STRESS_SECONDS (default 10).
//
// WATER_SMOKE_OBJECT (default "", meaning leave the demo on its boot default
// -- "Sphere"): when set to one of the fluid-object <select> option values
// ("None", "Sphere", "Cube", "TorusKnot", "Rubber Duck"), the harness drives
// the SAME settings-panel <select name="object"> a real user would use (see
// examples/gosx-docs/app/demos/water/page.gsx's
// data-gosx-scene3d-control-form="fluid-object" form and
// sceneManagedFluidObjectBindForm/sceneManagedFluidObjectReadControls in
// client/js/bootstrap-src/19b-scene-control-forms.js): it sets the select's
// value via the native value setter (bypassing React-style property
// shadowing) and dispatches bubbling "input"+"change" events, which is
// exactly what the form's own `form.addEventListener("change", schedule)`
// listens for. Selection is verified by re-reading the <select>'s resolved
// value AND polling the mount's data-gosx-scene3d-webgpu-water-object-* diag
// attributes (see gosxStateJS's webgpuAll dump) until the duck/mesh-target
// counters go nonzero, since object switch + first glTF-backed RTT render is
// async (form's rAF-scheduled apply, then a network fetch for Duck.gltf).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	cdppage "github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const dznICD = "/usr/share/vulkan/icd.d/dzn_icd.json"

const fpsSampleJS = `(function(seconds){
	return new Promise(function(resolve){
		var frames = 0, start = performance.now(), intervals = [];
		var last = start;
		function tick(ts){
			frames++;
			intervals.push(ts - last);
			last = ts;
			if (ts - start < seconds * 1000) { requestAnimationFrame(tick); return; }
			intervals.sort(function(a,b){return a-b;});
			var p = function(q){ return intervals[Math.min(intervals.length-1, Math.floor(q*intervals.length))] || 0; };
			resolve(JSON.stringify({
				fps: Math.round(frames / ((ts - start) / 1000) * 10) / 10,
				frames: frames,
				p50ms: Math.round(p(0.5)*100)/100,
				p95ms: Math.round(p(0.95)*100)/100,
				maxms: Math.round(intervals[intervals.length-1]*100)/100,
			}));
		}
		requestAnimationFrame(tick);
	});
})(%d)`

// selectFluidObjectJS drives the water demo's settings-panel object <select>
// the way a real user's change event would: native-setter the value (so it
// survives even if some framework layer shadows the plain DOM property),
// then dispatch bubbling "input" and "change" so
// sceneManagedFluidObjectBindForm's `form.addEventListener("change",
// schedule)` (19b-scene-control-forms.js) picks it up on its next rAF and
// calls sceneManagedFluidObjectApply. Returns the select's resolved value
// (or an "error:..." string) so the caller can confirm the DOM accepted it
// before waiting on the async duck-attrs poll.
const selectFluidObjectJS = `(function(name){
	var root = document.querySelector('[data-gosx-scene3d-control-form="fluid-object"]');
	if (!root) return "error:no-form";
	var select = root.querySelector('select[name="object"]');
	if (!select) return "error:no-select";
	var setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, "value") ||
		Object.getOwnPropertyDescriptor(Object.getPrototypeOf(select), "value");
	if (setter && typeof setter.set === "function") {
		setter.set.call(select, name);
	} else {
		select.value = name;
	}
	select.dispatchEvent(new Event("input", { bubbles: true }));
	select.dispatchEvent(new Event("change", { bubbles: true }));
	return select.value;
})(%q)`

// waitForFluidObjectJS polls (via a short internal loop, resolved as a
// Promise so evalAwait's WithAwaitPromise(true) blocks on it) until the
// mount's data-gosx-scene3d-webgpu-water-object-texture-passes /
// water-object-shadow-passes counters have moved off zero at least once
// (cumulative counters, so "moved" = current > the baseline snapshotted
// right after dispatching the change event) or the timeout elapses -- proof
// the async object switch (rAF apply -> glTF mesh resolution for the duck ->
// first mesh-target RTT render) actually completed, not just that the
// <select>'s DOM value changed.
const waitForFluidObjectJS = `(function(baselinePasses, baselineShadow, timeoutMS){
	return new Promise(function(resolve){
		var start = performance.now();
		function poll(){
			var m = document.querySelector("[data-gosx-scene3d-renderer]");
			var passes = m ? Number(m.getAttribute("data-gosx-scene3d-webgpu-water-object-texture-passes") || 0) : 0;
			var shadow = m ? Number(m.getAttribute("data-gosx-scene3d-webgpu-water-object-shadow-passes") || 0) : 0;
			if (passes > baselinePasses || shadow > baselineShadow || performance.now() - start > timeoutMS) {
				resolve(JSON.stringify({ passes: passes, shadow: shadow, waitedMS: Math.round(performance.now() - start) }));
				return;
			}
			requestAnimationFrame(poll);
		}
		requestAnimationFrame(poll);
	});
})(%d, %d, %d)`

const gosxStateJS = `(function(){
	var m = document.querySelector("[data-gosx-scene3d-renderer]");
	var a = function(n){ return m ? (m.getAttribute("data-gosx-scene3d-" + n) || "") : ""; };
	var gl = (function(){
		try {
			var c = document.querySelector("canvas");
			var g = c && (c.getContext("webgl2") || c.getContext("webgl"));
			if (!g) return "";
			var e = g.getExtension("WEBGL_debug_renderer_info");
			return e ? g.getParameter(e.UNMASKED_RENDERER_WEBGL) : "";
		} catch (err) { return String(err); }
	})();
	// P4-M2 (water-parity-campaign): dump EVERY data-gosx-scene3d-webgpu-*
	// attribute the mount currently carries (not just a hand-picked subset) so
	// the gpu-ms/water pass-count/dedup counters are attributable frame cost
	// evidence rather than assertions. webgpuAll is keyed by the attribute's
	// suffix after "data-gosx-scene3d-webgpu-".
	var webgpuAll = {};
	if (m) {
		for (var i = 0; i < m.attributes.length; i++) {
			var attr = m.attributes[i];
			if (attr.name.indexOf("data-gosx-scene3d-webgpu-") === 0) {
				webgpuAll[attr.name.slice("data-gosx-scene3d-webgpu-".length)] = attr.value;
			}
		}
	}
	return JSON.stringify({
		renderer: a("renderer"),
		ready: a("ready"),
		watchdog: a("render-watchdog"),
		qualityTier: a("quality-tier"),
		// WebGPU-only M5 (water-parity-campaign) at-rest counters. This script
		// forces the WebGL2 backend (window.__gosx_scene3d_force_webgl) because
		// headless Chrome's WebGPU path hits the SwiftShader honesty-gate under
		// Dozen -- so these are expected to read "" here (the WebGL2 renderer
		// publishes no water-specific data-gosx-scene3d-webgl-water-* attributes
		// at all; confirmed by inspection of 16-scene-webgl.js). Left in place so
		// a future WebGPU-capable run of this harness picks them up for free.
		waterAtRest: a("webgpu-water-at-rest-systems"),
		restSkipped: a("webgpu-water-rest-substeps-skipped"),
		gpuMS: a("webgpu-gpu-ms"),
		gpuTiming: a("webgpu-gpu-timing"),
		glRenderer: gl,
		webgpuAll: webgpuAll,
	});
})()`

// viewportStateJS reports what the page/canvas actually observes after a
// SetDeviceMetricsOverride call -- headless/Dozen can silently clamp a
// requested size, so callers should trust this over the requested numbers.
const viewportStateJS = `(function(){
	var c = document.querySelector("canvas");
	return JSON.stringify({
		innerWidth: window.innerWidth,
		innerHeight: window.innerHeight,
		dpr: window.devicePixelRatio,
		canvasW: c ? c.width : 0,
		canvasH: c ? c.height : 0,
	});
})()`

// objectTexturePassCountersJS reads just the two cumulative counters
// switchFluidObject needs as a wait baseline, cheaply (no JSON.stringify of
// the whole attribute set).
const objectTexturePassCountersJS = `(function(){
	var m = document.querySelector("[data-gosx-scene3d-renderer]");
	if (!m) return "0,0";
	var passes = m.getAttribute("data-gosx-scene3d-webgpu-water-object-texture-passes") || "0";
	var shadow = m.getAttribute("data-gosx-scene3d-webgpu-water-object-shadow-passes") || "0";
	return passes + "," + shadow;
})()`

// switchFluidObject drives the water demo's settings-panel object <select>
// to name (e.g. "Rubber Duck") and waits for evidence the switch actually
// took effect on the render side (not just in the DOM): the cumulative
// water-object-texture-passes/water-object-shadow-passes counters moving off
// their pre-switch baseline. See selectFluidObjectJS/waitForFluidObjectJS's
// doc comments for the full mechanism. Returns the select's resolved value,
// the wait-poll result JSON, and an error only for hard failures (missing
// form/select) -- a wait timeout is reported in the result JSON, not as an
// error, since some objects (e.g. "None") legitimately never touch those
// counters.
func switchFluidObject(ctx context.Context, name string, timeoutMS int) (selected, waitResult string, err error) {
	var baseline string
	if err = evalAwait(ctx, objectTexturePassCountersJS, &baseline); err != nil {
		return "", "", fmt.Errorf("read baseline counters: %w", err)
	}
	var basePasses, baseShadow int
	fmt.Sscanf(baseline, "%d,%d", &basePasses, &baseShadow)

	if err = evalAwait(ctx, fmt.Sprintf(selectFluidObjectJS, name), &selected); err != nil {
		return "", "", fmt.Errorf("dispatch select change: %w", err)
	}
	if len(selected) >= 6 && selected[:6] == "error:" {
		return selected, "", fmt.Errorf("fluid-object select not found: %s", selected)
	}

	if err = evalAwait(ctx, fmt.Sprintf(waitForFluidObjectJS, basePasses, baseShadow, timeoutMS), &waitResult); err != nil {
		return selected, "", fmt.Errorf("wait for object-texture counters: %w", err)
	}
	return selected, waitResult, nil
}

func newCtx(parent context.Context) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("enable-unsafe-webgpu", true),
		chromedp.Flag("ignore-gpu-blocklist", true),
		chromedp.Flag("enable-webgl", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.WindowSize(1280, 800),
	)
	if runtime.GOOS != "windows" {
		// Linux/WSL: route Chrome's Vulkan onto the real GPU via Mesa Dozen.
		// On Windows, Dawn uses D3D12 natively — no flags needed, and real
		// WebGPU (not SwiftShader) is available in headless.
		opts = append(opts,
			chromedp.Flag("use-vulkan", "native"),
			chromedp.Flag("enable-features", "Vulkan"),
			chromedp.Env("VK_DRIVER_FILES="+dznICD),
			chromedp.Env("VK_ICD_FILENAMES="+dznICD),
		)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parent, opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	return ctx, func() { cancelCtx(); cancelAlloc() }
}

// startConsoleCapture listens for browser-side console.error/warn calls and
// uncaught exceptions for the lifetime of ctx and prints them immediately
// (prefixed so they're easy to grep out of the harness's own log). This is
// the harness's error-visibility gap closer: a silently-thrown/caught
// exception inside the WebGPU render loop (e.g. a bad bind group in the
// duck's mesh-target RTT path) would otherwise show up ONLY as an attribute
// that mysteriously never increments, with zero clue why -- see the duck
// investigation in the water-parity/p5-duck PR description for a case that
// actually mattered.
func startConsoleCapture(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *cdpruntime.EventExceptionThrown:
			if e.ExceptionDetails != nil {
				fmt.Println("  [console] exception:", e.ExceptionDetails.Error())
			}
		case *cdpruntime.EventConsoleAPICalled:
			if e.Type != "error" && e.Type != "warning" {
				return
			}
			parts := make([]string, 0, len(e.Args))
			for _, a := range e.Args {
				if a == nil {
					continue
				}
				if a.Value != nil {
					parts = append(parts, string(a.Value))
				} else if a.Description != "" {
					parts = append(parts, a.Description)
				}
			}
			fmt.Println("  [console]", e.Type+":", fmt.Sprint(parts))
		}
	})
}

func evalAwait(ctx context.Context, js string, out *string) error {
	return chromedp.Run(ctx, chromedp.Evaluate(js, out, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	}))
}

func shot(ctx context.Context, dir, name string) {
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		buf, err = cdppage.CaptureScreenshot().Do(ctx)
		return err
	})); err != nil {
		fmt.Println("  screenshot", name, "failed:", err)
		return
	}
	path := filepath.Join(dir, name+".png")
	_ = os.WriteFile(path, buf, 0o644)
	fmt.Println("  saved", path)
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getenvInt(k string, d int) int {
	v := getenv(k, "")
	if v == "" {
		return d
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return d
	}
	return n
}

func getenvFloat(k string, d float64) float64 {
	v := getenv(k, "")
	if v == "" {
		return d
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err != nil {
		return d
	}
	return f
}

// clickLoop issues one MouseClickXY every intervalMS for durationMS, centered
// at (x,y) with a small jitter so repeated clicks land at slightly different
// pool coordinates (closer to a real "poke the water repeatedly" session than
// hammering one exact pixel). Runs on its own goroutine against the SAME
// chromedp ctx as the concurrently-running fps sampler -- chromedp's Target
// executor multiplexes CDP requests by message id over one websocket, so
// concurrent Run() calls against one ctx are safe (a well-established
// chromedp pattern for "interact while a long-running Evaluate is pending").
func clickLoop(ctx context.Context, x, y int, intervalMS, durationMS int) int {
	clicks := 0
	deadline := time.Now().Add(time.Duration(durationMS) * time.Millisecond)
	dx := 0
	for time.Now().Before(deadline) {
		jx := x + dx
		dx = (dx+17)%80 - 40 // small deterministic wobble across the pool
		_ = chromedp.Run(ctx, chromedp.MouseClickXY(float64(jx), float64(y)))
		clicks++
		time.Sleep(time.Duration(intervalMS) * time.Millisecond)
	}
	return clicks
}

// sampleUnderLoad runs the fps sampler and the sustained click loop
// concurrently for the same wall-clock window and returns the fps sample
// JSON plus how many clicks landed during it.
func sampleUnderLoad(ctx context.Context, x, y, seconds, clickIntervalMS int) (fpsJSON string, clicks int) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = evalAwait(ctx, fmt.Sprintf(fpsSampleJS, seconds), &fpsJSON)
	}()
	go func() {
		defer wg.Done()
		clicks = clickLoop(ctx, x, y, clickIntervalMS, seconds*1000)
	}()
	wg.Wait()
	return fpsJSON, clicks
}

// scenarioResult is one demo's full report for one run: idle, light-active,
// and (if requested) stress-phase fps samples plus renderer identity.
type scenarioResult struct {
	Name       string          `json:"name"`
	GLRenderer string          `json:"glRenderer,omitempty"`
	ObjectReq  string          `json:"objectRequested,omitempty"`
	ObjectGot  string          `json:"objectSelected,omitempty"`
	ObjectWait json.RawMessage `json:"objectSwitchWait,omitempty"`
	State      json.RawMessage `json:"state,omitempty"`
	Idle       json.RawMessage `json:"idle,omitempty"`
	Active     json.RawMessage `json:"active,omitempty"`
	RestState  json.RawMessage `json:"restState,omitempty"`
	Rest       json.RawMessage `json:"rest,omitempty"`
	Stress     json.RawMessage `json:"stress,omitempty"`
	StressView json.RawMessage `json:"stressViewport,omitempty"`
	StressReq  string          `json:"stressRequested,omitempty"`
	Clicks     int             `json:"stressClicks,omitempty"`
	Err        string          `json:"err,omitempty"`
}

func rawOrNil(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	return json.RawMessage(s)
}

func runGosxScenario(root context.Context, saveDir, gosxURL, object string, stress bool, stressW, stressH int, stressDPR, stressSeconds int) scenarioResult {
	res := scenarioResult{Name: "gosx"}
	fmt.Println("== gosx water (WebGL on Dozen) ==")
	ctx, cancel := newCtx(root)
	defer cancel()
	startConsoleCapture(ctx)
	var state, fps1, fps2 string
	err := chromedp.Run(ctx,
		cdpruntime.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			forceWebGL := getenv("WATER_SMOKE_FORCE_WEBGL", map[bool]string{true: "1", false: "0"}[runtime.GOOS != "windows"])
			if forceWebGL != "1" {
				return nil
			}
			_, err := cdppage.AddScriptToEvaluateOnNewDocument("window.__gosx_scene3d_force_webgl = true;").Do(ctx)
			return err
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetDeviceMetricsOverride(1280, 800, 1.0, false).Do(ctx)
		}),
		chromedp.Navigate(gosxURL),
		chromedp.Sleep(12*time.Second), // boot + selena compile + settle window
	)
	if err != nil {
		fmt.Println("gosx nav error:", err)
		res.Err = err.Error()
		return res
	}
	if object != "" {
		res.ObjectReq = object
		selected, waitJSON, switchErr := switchFluidObject(ctx, object, 8000)
		res.ObjectGot = selected
		res.ObjectWait = rawOrNil(waitJSON)
		if switchErr != nil {
			fmt.Println("  object switch error:", switchErr)
			res.Err = switchErr.Error()
		} else {
			fmt.Println("  object switched to:", selected, " wait:", waitJSON)
		}
		// glTF-backed objects (duck) fetch+parse a mesh asynchronously; give the
		// buoyancy sim a couple seconds to settle onto the water surface before
		// the idle sample so "idle" isn't secretly sampling a falling-in splash.
		_ = chromedp.Run(ctx, chromedp.Sleep(3*time.Second))
	}
	_ = evalAwait(ctx, gosxStateJS, &state)
	fmt.Println("  state:", state)
	res.State = rawOrNil(state)
	shot(ctx, saveDir, "gosx-settled")
	if err := evalAwait(ctx, fmt.Sprintf(fpsSampleJS, 4), &fps1); err == nil {
		fmt.Println("  fps(idle):", fps1)
		res.Idle = rawOrNil(fps1)
	}
	// poke the water: click canvas center for drops, then sample active fps
	_ = chromedp.Run(ctx,
		chromedp.MouseClickXY(640, 400),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.MouseClickXY(600, 380),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.MouseClickXY(680, 420),
	)
	if err := evalAwait(ctx, fmt.Sprintf(fpsSampleJS, 4), &fps2); err == nil {
		fmt.Println("  fps(active):", fps2)
		res.Active = rawOrNil(fps2)
	}
	shot(ctx, saveDir, "gosx-active")
	_ = evalAwait(ctx, gosxStateJS, &state)
	fmt.Println("  state(after):", state)

	// M1 rest verification (water-parity-campaign): the at-rest gate needs
	// the CPU-only energy proxy to decay below WATER_REST_ENERGY_EPSILON AND
	// quietMS to clear WATER_REST_MIN_QUIET_MS since the LAST disturbance --
	// with the demo's default damping=0.995 that is ~11.5s of undisturbed sim
	// time (see WATER_REST_ENERGY_EPSILON's comment in 16a-scene-webgpu.js).
	// The active-phase clicks just above reset that timer, so wait clearly
	// past the decay window for a clean, unambiguously-rested sample instead
	// of racing the crossover the way the original 12s settle + 4s idle
	// sample did (that window sampled right AT the crossover, ~0.001 vs the
	// 0.001 threshold -- see the P4-M1 report).
	var restState, restFPS string
	if err := chromedp.Run(ctx, chromedp.Sleep(13*time.Second)); err == nil {
		_ = evalAwait(ctx, gosxStateJS, &restState)
		fmt.Println("  state(rest):", restState)
		res.RestState = rawOrNil(restState)
		shot(ctx, saveDir, "gosx-rested")
		if err := evalAwait(ctx, fmt.Sprintf(fpsSampleJS, 4), &restFPS); err == nil {
			fmt.Println("  fps(rest):", restFPS)
			res.Rest = rawOrNil(restFPS)
		}
	}

	if stress {
		res.StressReq = fmt.Sprintf("%dx%d@%dx", stressW, stressH, stressDPR)
		fmt.Println("  == stress phase:", res.StressReq, fmt.Sprintf("for %ds ==", stressSeconds))
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetDeviceMetricsOverride(int64(stressW), int64(stressH), float64(stressDPR), false).Do(ctx)
		})); err != nil {
			fmt.Println("  stress resize error:", err)
			res.Err = err.Error()
		} else {
			time.Sleep(1200 * time.Millisecond) // let the resize settle / re-tile
			var viewJSON string
			_ = evalAwait(ctx, viewportStateJS, &viewJSON)
			fmt.Println("  stress viewport observed:", viewJSON)
			res.StressView = rawOrNil(viewJSON)
			stressFPS, clicks := sampleUnderLoad(ctx, stressW/2, stressH/2, stressSeconds, 500)
			fmt.Println("  fps(stress):", stressFPS, " clicks:", clicks)
			res.Stress = rawOrNil(stressFPS)
			res.Clicks = clicks
			shot(ctx, saveDir, "gosx-stress")
		}
	}
	return res
}

func runReferenceScenario(root context.Context, saveDir, refURL string, stress bool, stressW, stressH int, stressDPR, stressSeconds int) scenarioResult {
	res := scenarioResult{Name: "reference"}
	if refURL == "" {
		return res
	}
	fmt.Println("== reference (evanw/webgl-water) ==")
	rctx, rcancel := newCtx(root)
	defer rcancel()
	var rfps, rgl string
	err := chromedp.Run(rctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetDeviceMetricsOverride(1280, 800, 1.0, false).Do(ctx)
		}),
		chromedp.Navigate(refURL),
		chromedp.Sleep(6*time.Second),
	)
	if err != nil {
		fmt.Println("ref nav error:", err)
		res.Err = err.Error()
		return res
	}
	_ = evalAwait(rctx, `(function(){try{var c=document.querySelector("canvas");var g=c&&(c.getContext("webgl")||c.getContext("experimental-webgl"));if(!g)return "no gl";var e=g.getExtension("WEBGL_debug_renderer_info");return e?g.getParameter(e.UNMASKED_RENDERER_WEBGL):"no ext";}catch(err){return String(err);}})()`, &rgl)
	fmt.Println("  glRenderer:", rgl)
	res.GLRenderer = rgl
	shot(rctx, saveDir, "reference-settled")
	if err := evalAwait(rctx, fmt.Sprintf(fpsSampleJS, 4), &rfps); err == nil {
		fmt.Println("  fps:", rfps)
		res.Idle = rawOrNil(rfps)
	}
	_ = chromedp.Run(rctx, chromedp.MouseClickXY(640, 400))
	if err := evalAwait(rctx, fmt.Sprintf(fpsSampleJS, 4), &rfps); err == nil {
		fmt.Println("  fps(active):", rfps)
		res.Active = rawOrNil(rfps)
	}
	shot(rctx, saveDir, "reference-active")

	if stress {
		res.StressReq = fmt.Sprintf("%dx%d@%dx", stressW, stressH, stressDPR)
		fmt.Println("  == stress phase:", res.StressReq, fmt.Sprintf("for %ds ==", stressSeconds))
		if err := chromedp.Run(rctx, chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetDeviceMetricsOverride(int64(stressW), int64(stressH), float64(stressDPR), false).Do(ctx)
		})); err != nil {
			fmt.Println("  stress resize error:", err)
			res.Err = err.Error()
		} else {
			time.Sleep(1200 * time.Millisecond)
			var viewJSON string
			_ = evalAwait(rctx, viewportStateJS, &viewJSON)
			fmt.Println("  stress viewport observed:", viewJSON)
			res.StressView = rawOrNil(viewJSON)
			stressFPS, clicks := sampleUnderLoad(rctx, stressW/2, stressH/2, stressSeconds, 500)
			fmt.Println("  fps(stress):", stressFPS, " clicks:", clicks)
			res.Stress = rawOrNil(stressFPS)
			res.Clicks = clicks
			shot(rctx, saveDir, "reference-stress")
		}
	}
	return res
}

func main() {
	saveDir := getenv("WATER_SMOKE_SAVE_DIR", "/tmp/water-smoke")
	_ = os.MkdirAll(saveDir, 0o755)
	gosxURL := getenv("WATER_SMOKE_URL", "http://127.0.0.1:8890/demos/water")
	refURL := getenv("WATER_SMOKE_REF_URL", "https://madebyevan.com/webgl-water/")
	runID := getenv("WATER_SMOKE_RUN_ID", "1")
	object := getenv("WATER_SMOKE_OBJECT", "")
	stress := getenv("WATER_SMOKE_STRESS", "0") == "1"
	stressW := getenvInt("WATER_SMOKE_STRESS_WIDTH", 2560)
	stressH := getenvInt("WATER_SMOKE_STRESS_HEIGHT", 1440)
	stressDPR := getenvInt("WATER_SMOKE_STRESS_DPR", 2)
	stressSeconds := getenvInt("WATER_SMOKE_STRESS_SECONDS", 10)
	_ = getenvFloat // silence unused if stress knobs above stay ints

	root, cancelRoot := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelRoot()

	gosx := runGosxScenario(root, saveDir, gosxURL, object, stress, stressW, stressH, stressDPR, stressSeconds)
	ref := runReferenceScenario(root, saveDir, refURL, stress, stressW, stressH, stressDPR, stressSeconds)

	summary := struct {
		RunID string         `json:"runID"`
		Gosx  scenarioResult `json:"gosx"`
		Ref   scenarioResult `json:"reference"`
	}{RunID: runID, Gosx: gosx, Ref: ref}
	out, _ := json.MarshalIndent(summary, "", "  ")
	summaryPath := filepath.Join(saveDir, "summary-"+runID+".json")
	_ = os.WriteFile(summaryPath, out, 0o644)
	fmt.Println("RESULT_JSON_FILE:", summaryPath)
	fmt.Println("done")
}
