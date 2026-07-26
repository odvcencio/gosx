//go:build forcedred

// forced-red-custompost is the pixel-level gate for the "customPost is never
// dispatched" bug. It serves the built client/js/bootstrap.js, drives the REAL
// scene3d author path (createSceneState -> bundle -> backend.render) in headless
// Chrome on SwiftShader, and reads back an actual pixel.
//
// The custom post pass outputs OPAQUE RED unconditionally. So:
//
//	pass dispatched     -> canvas is red   (255, 0, 0)
//	pass never entered  -> canvas is black (the scene background)
//
// Health attributes are NOT trusted here: the bug kept backend/postfx/effect
// count all reading healthy while the pass produced zero pixels.
//
//	go run -tags forcedred scripts/forced-red-custompost.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"
)

const pageHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>forced red</title>
<style>html,body{margin:0;background:#000}canvas{display:block}</style></head>
<body>
<canvas id="c" width="320" height="180"></canvas>
<script src="/bootstrap.js"></script>
<script>
window.__forcedRed = function() {
  var out = { ok: false };
  try {
    var api = window.__gosx_scene3d_api;
    if (!api) { out.error = "no __gosx_scene3d_api"; return out; }

    var canvas = document.getElementById("c");
    var registry = api.sceneBackendRegistry;
    var backend = registry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
    if (!backend) { out.error = "no webgl backend selected"; return out; }
    out.backend = backend.id || backend.name || "unknown";
    if (out.backend === "canvas" || out.backend === "canvas2d") { out.error = "fell back to canvas renderer"; return out; }

    var renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
    if (!renderer) { out.error = "renderer create failed"; return out; }

    // FORCED RED: this pass ignores its input entirely.
    var vert = "attribute vec2 a_position; void main() { gl_Position = vec4(a_position, 0.0, 1.0); }";
    var frag = "precision mediump float; void main() { gl_FragColor = vec4(1.0, 0.0, 0.0, 1.0); }";

    // The REAL author path: props -> createSceneState -> normalizeScenePostEffect.
    var state = api.createSceneState({
      scene: { postEffects: [{ kind: "customPost", name: "forced-red", vertexGLSL: vert, fragmentGLSL: frag }] }
    });
    out.stateKind = state.postEffects && state.postEffects[0] ? state.postEffects[0].kind : "(none)";
    out.canonicalKind = api.SCENE_POST_CUSTOM_POST;

    var bundle = {
      bundleVersion: 1,
      camera: { x: 0, y: 0, z: 5, fov: 72, near: 0.05, far: 128 },
      environment: {},
      points: [{ id: "p", count: 1, positions: new Float32Array([0, 0, 0]), color: "#ffffff" }],
      instancedMeshes: [], computeParticles: [], objects: [], meshObjects: [],
      materials: [], labels: [], sprites: [], lights: [],
      postEffects: state.postEffects,
      positions: new Float32Array(0), colors: new Float32Array(0),
      worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
      worldLineWidths: new Float32Array(0),
      worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
      worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
      worldMeshTangents: new Float32Array(0),
      vertexCount: 0, worldVertexCount: 0
    };

    renderer.render(bundle, { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 });

    var gl = canvas.getContext("webgl2");
    if (!gl) { out.error = "no webgl2 context for readback"; return out; }
    var px = new Uint8Array(4);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    gl.readPixels(160, 90, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, px);
    out.pixel = [px[0], px[1], px[2], px[3]];
    out.red = (px[0] > 200 && px[1] < 60 && px[2] < 60);
    out.ok = true;
  } catch (e) {
    out.error = String(e && e.stack || e);
  }
  return out;
};
</script>
</body></html>`

type result struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error"`
	Backend       string `json:"backend"`
	StateKind     string `json:"stateKind"`
	CanonicalKind string `json:"canonicalKind"`
	Pixel         []int  `json:"pixel"`
	Red           bool   `json:"red"`
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	bootstrap := filepath.Join(root, "client", "js", "bootstrap.js")
	if _, err := os.Stat(bootstrap); err != nil {
		fail(fmt.Errorf("bootstrap.js not found at %s: %w", bootstrap, err))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, bootstrap)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, pageHTML)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("use-gl", "angle"),
		chromedp.Flag("use-angle", "swiftshader"),
		chromedp.Flag("enable-unsafe-swiftshader", true),
		chromedp.Flag("enable-webgl", true),
		chromedp.Flag("ignore-gpu-blocklist", true),
		chromedp.WindowSize(400, 300),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	ctx, cancelTimeout := context.WithTimeout(browserCtx, 90*time.Second)
	defer cancelTimeout()

	var res result
	err = chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitReady("#c", chromedp.ByQuery),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`window.__forcedRed()`, &res),
	)
	if err != nil {
		fail(err)
	}

	blob, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(blob))

	if !res.OK {
		fmt.Println("RESULT: HARNESS ERROR")
		os.Exit(2)
	}
	if res.Red {
		fmt.Printf("RESULT: RED (%v) -- customPost DISPATCHED\n", res.Pixel)
		return
	}
	fmt.Printf("RESULT: NOT RED (%v) -- customPost NEVER DISPATCHED\n", res.Pixel)
	os.Exit(1)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(2)
}
