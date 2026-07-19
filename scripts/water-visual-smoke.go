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
		glRenderer: gl,
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
	State      json.RawMessage `json:"state,omitempty"`
	Idle       json.RawMessage `json:"idle,omitempty"`
	Active     json.RawMessage `json:"active,omitempty"`
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

func runGosxScenario(root context.Context, saveDir, gosxURL string, stress bool, stressW, stressH int, stressDPR, stressSeconds int) scenarioResult {
	res := scenarioResult{Name: "gosx"}
	fmt.Println("== gosx water (WebGL on Dozen) ==")
	ctx, cancel := newCtx(root)
	defer cancel()
	var state, fps1, fps2 string
	err := chromedp.Run(ctx,
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
	stress := getenv("WATER_SMOKE_STRESS", "0") == "1"
	stressW := getenvInt("WATER_SMOKE_STRESS_WIDTH", 2560)
	stressH := getenvInt("WATER_SMOKE_STRESS_HEIGHT", 1440)
	stressDPR := getenvInt("WATER_SMOKE_STRESS_DPR", 2)
	stressSeconds := getenvInt("WATER_SMOKE_STRESS_SECONDS", 10)
	_ = getenvFloat // silence unused if stress knobs above stay ints

	root, cancelRoot := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelRoot()

	gosx := runGosxScenario(root, saveDir, gosxURL, stress, stressW, stressH, stressDPR, stressSeconds)
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
