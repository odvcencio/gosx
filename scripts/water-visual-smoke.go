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
// WATER_SMOKE_SAVE_DIR (default /tmp/water-smoke).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		waterAtRest: a("webgpu-water-at-rest-systems"),
		restSkipped: a("webgpu-water-rest-substeps-skipped"),
		glRenderer: gl,
	});
})()`

func newCtx(parent context.Context) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("use-vulkan", "native"),
		chromedp.Flag("enable-features", "Vulkan"),
		chromedp.Flag("ignore-gpu-blocklist", true),
		chromedp.Flag("enable-webgl", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Env("VK_DRIVER_FILES="+dznICD),
		chromedp.Env("VK_ICD_FILENAMES="+dznICD),
		chromedp.WindowSize(1280, 800),
	)
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

func main() {
	saveDir := getenv("WATER_SMOKE_SAVE_DIR", "/tmp/water-smoke")
	_ = os.MkdirAll(saveDir, 0o755)
	gosxURL := getenv("WATER_SMOKE_URL", "http://127.0.0.1:8890/demos/water")
	refURL := getenv("WATER_SMOKE_REF_URL", "https://madebyevan.com/webgl-water/")

	root, cancelRoot := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelRoot()

	// ---- gosx water (forced WebGL so Dozen carries it on real GPU) ----
	fmt.Println("== gosx water (WebGL on Dozen) ==")
	ctx, cancel := newCtx(root)
	defer cancel()
	var state, fps1, fps2 string
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
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
	} else {
		_ = evalAwait(ctx, gosxStateJS, &state)
		fmt.Println("  state:", state)
		shot(ctx, saveDir, "gosx-settled")
		if err := evalAwait(ctx, fmt.Sprintf(fpsSampleJS, 4), &fps1); err == nil {
			fmt.Println("  fps(idle):", fps1)
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
		}
		shot(ctx, saveDir, "gosx-active")
		_ = evalAwait(ctx, gosxStateJS, &state)
		fmt.Println("  state(after):", state)
	}
	cancel()

	// ---- reference ----
	if refURL != "" {
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
		} else {
			_ = evalAwait(rctx, `(function(){try{var c=document.querySelector("canvas");var g=c&&(c.getContext("webgl")||c.getContext("experimental-webgl"));if(!g)return "no gl";var e=g.getExtension("WEBGL_debug_renderer_info");return e?g.getParameter(e.UNMASKED_RENDERER_WEBGL):"no ext";}catch(err){return String(err);}})()`, &rgl)
			fmt.Println("  glRenderer:", rgl)
			shot(rctx, saveDir, "reference-settled")
			if err := evalAwait(rctx, fmt.Sprintf(fpsSampleJS, 4), &rfps); err == nil {
				fmt.Println("  fps:", rfps)
			}
			_ = chromedp.Run(rctx, chromedp.MouseClickXY(640, 400))
			if err := evalAwait(rctx, fmt.Sprintf(fpsSampleJS, 4), &rfps); err == nil {
				fmt.Println("  fps(active):", rfps)
			}
			shot(rctx, saveDir, "reference-active")
		}
	}
	fmt.Println("done")
}
