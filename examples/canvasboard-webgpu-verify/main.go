// Example canvasboard-webgpu-verify is a side-by-side visual parity instrument
// for the M1 WebGPU canvas board re-platform.
//
// It serves ONE page (GET /) showing TWO CanvasBoard instances rendering
// the IDENTICAL node set side by side:
//
//   LEFT  — no backend attribute → 26b1 painter path (the reference).
//   RIGHT — data-gosx-canvas-backend="webgpu" → the M1 GPU path.
//
// Neither board auto-interacts; both sit at the same initial camera so the
// rendering can be compared directly. A self-validation script on the page
// waits for both boards to hydrate, probes each board's labels layer, takes
// per-board screenshots, and POSTs a combined JSON report to /report.
//
// # Required chunks (file:line evidence)
//
//   - wasm_exec.js: Go toolchain shim (engine/surface/runtime_handler.go:34-93)
//   - bootstrap-runtime.js: the SELECTIVE bootstrap orchestrator (26-runtime-tail.js).
//     Reads the gosx-manifest, detects features via manifestFeatureNames(), loads WASM,
//     then dynamically loads bootstrap-feature-engines.js (26-runtime-tail.js:93-134).
//     Its __gosx_runtime_ready delegates to feature.runtimeReady() which calls
//     mountAllSurfaceKinds() — the critical path for data-gosx-surface-kind canvas2d.
//     The old monolithic bootstrap.js (30-tail.js) does NOT have mountAllSurfaceKinds.
//   - bootstrap-feature-engines.js: surface-kind mount logic + canvas2d painter +
//     DOM label overlay (26b-prefix / 26b1 / 26b2). Loaded dynamically at runtime.
//   - bootstrap-feature-scene3d-webgpu.js: publishes window.__gosx_scene3d_webgpu_api
//     .createRenderer — REQUIRED for canvas2d WebGPU path; without it
//     _canvasSurfaceWebGPUFactory() returns null and the board falls back
//     (client/js/bootstrap-src/26b-feature-engines-prefix.js:541-548)
//
// # Manifest structure
//
// The gosx-manifest JSON needs:
//   - engines[0].runtime = "shared" — triggers manifestNeedsSharedEngineRuntime
//     → loads the WASM (26-runtime-tail.js:170-177)
//   - engines[0] present — triggers "engines" feature load (26-runtime-tail.js:141-144)
//     which calls mountAllSurfaceKinds().
//   - engines[0].component = "CanvasSurfaceBoot" (not "CanvasBoard") — a stub
//     entry whose factory is pre-registered in the page's inline script so
//     mountAllEngines() completes without the "no engine factory registered" warn.
//     The actual canvas2d boards are mounted by mountAllSurfaceKinds() via the
//     data-gosx-surface-kind="canvas2d" placeholders, independent of this entry.
//   - runtime.path — WASM URL (26-runtime-tail.js:294-308)
//
// # Two-board coexistence
//
// mountAllSurfaceKinds() uses querySelectorAll("[data-gosx-surface-kind]:not([...])"),
// which discovers BOTH canvas elements and hydrates each independently.
// Each call to nextSurfaceID() auto-increments a module-level counter
// (26b-feature-engines-prefix.js:46-50), so the two boards get distinct ids
// ("gosx-engine-surface-1" and "gosx-engine-surface-2"). The WASM bridge keys
// all render/tick/event calls by that id, so the two boards are fully isolated.
// Both painter and WebGPU RAF loops key off their own closure-captured id.
//
// # WASM build
//
// The pre-built build/gosx-runtime.wasm (May 5) predates __gosx_canvas_set_backend.
// Build a fresh standard-Go WASM:
//
//	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" \
//	  -o /tmp/gosx-webgpu-verify/gosx-runtime.wasm \
//	  m31labs.dev/gosx/client/wasm
//
// (cmd/gosx/build.go:662-668: goWASMBuildArgs)
//
// # Run
//
//	cd /home/draco/work/gosx
//	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" \
//	  -o /tmp/gosx-webgpu-verify/gosx-runtime.wasm m31labs.dev/gosx/client/wasm
//	go run ./examples/canvasboard-webgpu-verify 2>&1
//
// Then open http://localhost:8765 in Windows Chrome (or curl the healthcheck).
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	listenAddr = "0.0.0.0:8765"
	wasmPath   = "/tmp/gosx-webgpu-verify/gosx-runtime.wasm"
	reportDir  = "/tmp/gosx-webgpu-verify"
)

func main() {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		log.Fatalf("create report dir: %v", err)
	}

	// Resolve GOROOT for wasm_exec.js.
	goroot := runtime.GOROOT()

	// Resolve repo root so we can serve client/js/*.js.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})

	// Serve the parity page (two boards side by side).
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(buildPage()))
	})

	// Serve pre-built test PNG (64×64 colorful).
	mux.HandleFunc("GET /test.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if err := png.Encode(w, makeTestPNG()); err != nil {
			log.Printf("encode test.png: %v", err)
		}
	})

	// Serve WASM runtime.
	mux.HandleFunc("GET /gosx/runtime.wasm", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, wasmPath)
	})

	// Serve wasm_exec.js from GOROOT.
	mux.HandleFunc("GET /gosx/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) {
		candidates := []string{
			filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
			filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				w.Header().Set("Content-Type", "application/javascript")
				w.Header().Set("Cache-Control", "public, max-age=300")
				http.ServeFile(w, r, p)
				return
			}
		}
		http.Error(w, "wasm_exec.js not found in GOROOT "+goroot, http.StatusServiceUnavailable)
	})

	// Serve the split-bundle runtime stack from client/js/:
	//
	//   bootstrap-runtime.js   — the selective bootstrap orchestrator (26-runtime-tail.js).
	//                            Reads the gosx-manifest, detects feature names via
	//                            manifestFeatureNames(), loads WASM, then dynamically loads
	//                            bootstrap-feature-engines.js when the manifest has engines
	//                            entries (26-runtime-tail.js:93-134).  This is the ONLY
	//                            bundle that calls mountAllSurfaceKinds() — the old monolithic
	//                            bootstrap.js / 30-tail.js does NOT have that path.
	//
	//   bootstrap-feature-engines.js — installs the engines feature (26b/26b1/26b2):
	//                            mountAllEngines, mountAllSurfaceKinds, the canvas2d RAF
	//                            paint loop, __gosx_paint_canvas_bundle painter, and the
	//                            DOM label overlay (26b1/26b2).  Loaded dynamically by
	//                            bootstrap-runtime.js at runtime-ready time.
	//
	//   bootstrap-feature-scene3d-webgpu.js — publishes
	//                            window.__gosx_scene3d_webgpu_api.createRenderer, required
	//                            for the WebGPU canvas backend path.  Loaded conditionally
	//                            by the inline script when navigator.gpu is present.
	//
	// We do NOT serve bootstrap.js: the monolithic bundle embeds 30-tail.js whose
	// __gosx_runtime_ready calls mountAllEngines then hydrateAllIslands — it never calls
	// mountAllSurfaceKinds(), so a surface-kind canvas2d placeholder is never hydrated.
	// bootstrap-runtime.js's __gosx_runtime_ready delegates to feature.runtimeReady()
	// which calls mountAllSurfaceKinds() alongside mountAllEngines().
	mux.HandleFunc("GET /gosx/bootstrap-runtime.js", func(w http.ResponseWriter, r *http.Request) {
		serveClientJS(w, r, filepath.Join(repoRoot, "client", "js", "bootstrap-runtime.js"))
	})
	mux.HandleFunc("GET /gosx/bootstrap-feature-engines.js", func(w http.ResponseWriter, r *http.Request) {
		serveClientJS(w, r, filepath.Join(repoRoot, "client", "js", "bootstrap-feature-engines.js"))
	})
	mux.HandleFunc("GET /gosx/bootstrap-feature-scene3d-webgpu.js", func(w http.ResponseWriter, r *http.Request) {
		serveClientJS(w, r, filepath.Join(repoRoot, "client", "js", "bootstrap-feature-scene3d-webgpu.js"))
	})
	// The scene3d chunk publishes window.__gosx_scene3d_api (10-runtime-scene-core's
	// sceneApi export), which 26e-feature-scene3d-webgpu-prefix.js imports from. The
	// webgpu chunk is NOT self-sufficient — bootstrap-runtime.js carries only the
	// runtime-utils extract of scene-core, not the full api. Chain-load this first.
	mux.HandleFunc("GET /gosx/bootstrap-feature-scene3d.js", func(w http.ResponseWriter, r *http.Request) {
		serveClientJS(w, r, filepath.Join(repoRoot, "client", "js", "bootstrap-feature-scene3d.js"))
	})

	// Absorb telemetry pings from 04-telemetry.js (/gosx/client-events).
	// Without this route the browser logs a 405 console error which is noise.
	mux.HandleFunc("POST /_gosx/client-events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	// Receive the self-report from the browser (parity-v2: per-board fields).
	mux.HandleFunc("POST /report", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Parse top-level fields.
		var parsed map[string]json.RawMessage
		_ = json.Unmarshal(raw, &parsed)

		// Write report JSON.
		reportPath := filepath.Join(reportDir, "report.json")
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		if err := os.WriteFile(reportPath, pretty, 0o644); err != nil {
			log.Printf("write report: %v", err)
		}

		// Decode per-board screenshots if present.
		for _, key := range []string{"painterScreenshotDataURL", "webgpuScreenshotDataURL"} {
			if dataURL, ok := parsed[key]; ok {
				var urlStr string
				if err := json.Unmarshal(dataURL, &urlStr); err == nil && strings.HasPrefix(urlStr, "data:image/png;base64,") {
					b64 := strings.TrimPrefix(urlStr, "data:image/png;base64,")
					imgBytes, err := base64.StdEncoding.DecodeString(b64)
					if err == nil {
						name := strings.TrimSuffix(strings.TrimSuffix(key, "DataURL"), "Screenshot") + ".png"
						imgPath := filepath.Join(reportDir, name)
						if err := os.WriteFile(imgPath, imgBytes, 0o644); err != nil {
							log.Printf("write %s: %v", name, err)
						} else {
							log.Printf("[report] screenshot saved: %s", imgPath)
						}
					} else {
						log.Printf("[report] screenshot decode error (%s): %v", key, err)
					}
				}
			}
		}
		// Legacy single-board screenshot field (backward compat).
		if dataURL, ok := parsed["screenshotDataURL"]; ok {
			var urlStr string
			if err := json.Unmarshal(dataURL, &urlStr); err == nil && strings.HasPrefix(urlStr, "data:image/png;base64,") {
				b64 := strings.TrimPrefix(urlStr, "data:image/png;base64,")
				imgBytes, err := base64.StdEncoding.DecodeString(b64)
				if err == nil {
					imgPath := filepath.Join(reportDir, "board.png")
					if err := os.WriteFile(imgPath, imgBytes, 0o644); err != nil {
						log.Printf("write board.png: %v", err)
					}
				}
			}
		}

		log.Printf("[report] received: %s", reportPath)

		// Print a parity summary.
		printParityReport(parsed)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	log.Printf("canvasboard-webgpu-verify (parity-v2) listening at http://localhost:8765")
	log.Printf("  parity page:  http://localhost:8765/")
	log.Printf("  healthcheck:  http://localhost:8765/healthz")
	log.Printf("  report:       %s/report.json", reportDir)
	log.Printf("  painter png:  %s/painter.png", reportDir)
	log.Printf("  webgpu png:   %s/webgpu.png", reportDir)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}

// printParityReport logs a concise parity summary from the parsed report fields.
func printParityReport(parsed map[string]json.RawMessage) {
	extract := func(key string) string {
		if v, ok := parsed[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				return s
			}
			return string(v)
		}
		return "<missing>"
	}
	extractBool := func(key string) bool {
		if v, ok := parsed[key]; ok {
			var b bool
			_ = json.Unmarshal(v, &b)
			return b
		}
		return false
	}

	painterPainted := extractBool("painterPaintedPixels")
	webgpuPainted := extractBool("webgpuPaintedPixels")
	webgpuRoute := extractBool("webgpuRoute")

	log.Printf("[report/parity] painterPaintedPixels=%v webgpuPaintedPixels=%v webgpuRoute=%v",
		painterPainted, webgpuPainted, webgpuRoute)

	// Per-board labels info.
	if v, ok := parsed["painterBoard"]; ok {
		log.Printf("[report/painter] %s", string(v))
	}
	if v, ok := parsed["webgpuBoard"]; ok {
		log.Printf("[report/webgpu]  %s", string(v))
	}

	// Surface IDs.
	log.Printf("[report] painterSurfaceId=%s webgpuSurfaceId=%s",
		extract("painterSurfaceId"), extract("webgpuSurfaceId"))
}

func serveClientJS(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}

// makeTestPNG generates a colorful 64×64 test image with 4 quadrant colors.
func makeTestPNG() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	colors := [4]color.RGBA{
		{R: 255, G: 128, B: 0, A: 255},  // top-left: orange
		{R: 0, G: 200, B: 255, A: 255},  // top-right: cyan
		{R: 180, G: 0, B: 255, A: 255},  // bottom-left: purple
		{R: 64, G: 255, B: 64, A: 255},  // bottom-right: green
	}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			qi := 0
			if x >= 32 {
				qi++
			}
			if y >= 32 {
				qi += 2
			}
			img.SetRGBA(x, y, colors[qi])
		}
	}
	return img
}

// boardNodes returns the shared node list used by both boards:
// 100 rects (20×5), 3 line nodes, 2 sprite nodes, 3 label nodes.
// Factored out so both boards receive an IDENTICAL node set.
func boardNodes() []map[string]interface{} {
	palette := []string{"#ff8866", "#88ddff", "#ffd866", "#a0ff88", "#ff88dd"}
	nodes := make([]map[string]interface{}, 0, 110)

	// 100 rects in a 20×5 grid.
	for row := 0; row < 5; row++ {
		for col := 0; col < 20; col++ {
			x := float64(col-10) * 60
			y := float64(row-2) * 50
			nodes = append(nodes, map[string]interface{}{
				"id":     "rect-" + strconv.Itoa(row) + "-" + strconv.Itoa(col),
				"kind":   "rect",
				"x":      x,
				"y":      y,
				"width":  44.0,
				"height": 32.0,
				"color":  palette[(row+col)%len(palette)],
			})
		}
	}

	// 3 line nodes.
	nodes = append(nodes,
		map[string]interface{}{"id": "line-0", "kind": "line", "x1": -300.0, "y1": -150.0, "x2": 300.0, "y2": 150.0, "color": "#ff4444"},
		map[string]interface{}{"id": "line-1", "kind": "line", "x1": -300.0, "y1": 150.0, "x2": 300.0, "y2": -150.0, "color": "#44ff44"},
		map[string]interface{}{"id": "line-2", "kind": "line", "x1": 0.0, "y1": -200.0, "x2": 0.0, "y2": 200.0, "color": "#4444ff"},
	)

	// 2 sprite nodes pointing at the test PNG.
	nodes = append(nodes,
		map[string]interface{}{"id": "sprite-0", "kind": "image", "x": -380.0, "y": 80.0, "width": 64.0, "height": 64.0, "src": "/test.png"},
		map[string]interface{}{"id": "sprite-1", "kind": "image", "x": 320.0, "y": -120.0, "width": 64.0, "height": 64.0, "src": "/test.png"},
	)

	// 3 label nodes.
	nodes = append(nodes,
		map[string]interface{}{"id": "label-0", "kind": "label", "x": 0.0, "y": 160.0, "text": "GoSX Board"},
		map[string]interface{}{"id": "label-1", "kind": "label", "x": -300.0, "y": -160.0, "text": "M1 verify"},
		map[string]interface{}{"id": "label-2", "kind": "label", "x": 300.0, "y": -160.0, "text": "GoSX 16a"},
	)

	return nodes
}

// boardPropsJSON builds the CanvasBoard props JSON using the shared node list.
// The camera is identical for both boards: zoom=1, pan=(0,0).
func boardPropsJSON() string {
	props := map[string]interface{}{
		"board": map[string]interface{}{
			"pan":  map[string]interface{}{"x": 0.0, "y": 0.0},
			"zoom": 1.0,
		},
		"background": "#0f1720",
		"nodes":      boardNodes(),
	}
	b, _ := json.Marshal(props)
	return string(b)
}

// buildPage returns the full HTML page showing two CanvasBoard instances side
// by side: LEFT = 26b1 painter (reference), RIGHT = WebGPU (M1 GPU path).
// Neither board auto-interacts; both render their initial camera and sit still.
func buildPage() string {
	props := boardPropsJSON()

	// Inline gosx-manifest: one sentinel engine entry with runtime:"shared" to trigger:
	//   1. manifestNeedsSharedEngineRuntime → WASM load
	//      (26-runtime-tail.js:170-177, 166-168)
	//   2. manifestFeatureNames pushes "engines" → dynamic load of
	//      /gosx/bootstrap-feature-engines.js → mountAllSurfaceKinds()
	//      (26-runtime-tail.js:141-144)
	//
	// The component name "CanvasSurfaceBoot" is used instead of "CanvasBoard"
	// because mountAllEngines() (30-tail.js:3374) iterates manifest.engines and
	// calls mountEngine() → resolveMountedEngineFactory() for every entry. If the
	// component has no registered factory, mountEngine emits:
	//   "[gosx] no engine factory registered for <component>"
	// (30-tail.js:3262). A stub no-op factory for "CanvasSurfaceBoot" is
	// pre-registered in the inline script below so mountEngine completes cleanly.
	//
	// The canvas2d boards themselves are NOT mounted via this engines entry — they
	// are mounted by mountAllSurfaceKinds() in the engines feature
	// (bootstrap-feature-engines.js) which discovers ALL <canvas data-gosx-surface-kind="canvas2d">
	// placeholders via querySelectorAll and calls __gosx_hydrate for each.
	// Each board gets a unique id from nextSurfaceID() (auto-increments:
	// "gosx-engine-surface-1", "gosx-engine-surface-2").
	manifest := `{
  "version":"1",
  "engines":[{"id":"gosx-canvas-surface-0","component":"CanvasSurfaceBoot","kind":"canvas2d","runtime":"shared","props":{}}],
  "bundles":{},
  "runtime":{"path":"/gosx/runtime.wasm","hash":"dev","size":0}
}`

	ts := time.Now().UnixMilli()

	return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>GoSX Canvas Parity — 26b1 Painter vs 16a WebGPU</title>
<style>
  * { box-sizing: border-box; }
  body { margin:0; background:#1a1f2b; color:#e6edf3; font-family:system-ui,sans-serif; }
  h1 { text-align:center; font-size:22px; margin:20px 0 8px; letter-spacing:0.02em; }
  #parity-row {
    display:flex; flex-direction:row; gap:24px;
    justify-content:center; align-items:flex-start;
    padding:0 24px 24px;
  }
  .board-col { display:flex; flex-direction:column; align-items:center; gap:10px; }
  .board-caption {
    font-size:16px; font-weight:700; letter-spacing:0.03em;
    padding:6px 18px; border-radius:6px; text-align:center;
  }
  .caption-painter { background:#1a3040; color:#88ddff; border:1px solid #3a6080; }
  .caption-webgpu  { background:#1a2840; color:#ffdd88; border:1px solid #806030; }
  .board-host {
    position:relative;
    width:620px; height:400px;
    border:2px solid #2a3550; border-radius:4px; overflow:hidden;
  }
  .board-canvas { display:block; width:620px; height:400px; }
  #verdict { position:fixed; top:16px; right:16px; padding:12px 20px; border-radius:8px;
             font-size:18px; font-weight:bold; z-index:9999; opacity:0; transition:opacity 0.3s; }
  #verdict.show { opacity:1; }
  #verdict.pass { background:#1a5c1a; color:#4dff4d; border:2px solid #4dff4d; }
  #verdict.info { background:#1a2a5c; color:#4d9dff; border:2px solid #4d9dff; }
  #status { position:fixed; bottom:16px; left:16px; font-size:12px; color:#7a8a9a; max-width:700px; }
</style>
</head>
<body>

<h1>GoSX Canvas Parity — 26b1 Painter (reference) vs 16a WebGPU</h1>

<div id="parity-row">

  <!-- LEFT: painter board (no backend attr → 26b1 painter reference) -->
  <div class="board-col">
    <div class="board-caption caption-painter">26b1 painter (reference)</div>
    <div id="painter-host" class="board-host">
      <!-- CanvasBoard placeholder WITHOUT backend attr — stays on 26b1 painter.
           data-gosx-surface-kind="canvas2d" is picked up by mountAllSurfaceKinds()
           (26b-feature-engines-prefix.js:1054-1063). -->
      <canvas id="painter-board"
        class="board-canvas"
        width="620" height="400"
        data-gosx-surface-kind="canvas2d"
        data-gosx-engine-component="CanvasBoard"
        data-gosx-engine-props='` + props + `'
        data-gosx-engine-caps="canvas"
        data-gosx-canvas2d="1">
      </canvas>
    </div>
  </div>

  <!-- RIGHT: WebGPU board (data-gosx-canvas-backend="webgpu" → 16a GPU path) -->
  <div class="board-col">
    <div class="board-caption caption-webgpu">16a WebGPU</div>
    <div id="webgpu-host" class="board-host">
      <!-- CanvasBoard placeholder WITH data-gosx-canvas-backend="webgpu" opts into
           the 16a renderer (_canvasSurfaceWantsWebGPU).
           data-gosx-surface-kind="canvas2d" is picked up by mountAllSurfaceKinds()
           (26b-feature-engines-prefix.js:437, 1054-1063). -->
      <canvas id="webgpu-board"
        class="board-canvas"
        width="620" height="400"
        data-gosx-surface-kind="canvas2d"
        data-gosx-engine-component="CanvasBoard"
        data-gosx-canvas-backend="webgpu"
        data-gosx-engine-props='` + props + `'
        data-gosx-engine-caps="canvas,webgpu"
        data-gosx-canvas2d="1">
      </canvas>
    </div>
  </div>

</div>

<div id="verdict"></div>
<div id="status">Waiting for WASM hydration…</div>

<!-- Inline manifest: engines entry with runtime:"shared" triggers WASM load
     and "engines" feature (26-runtime-tail.js:141-144,170-177).
     component:"CanvasSurfaceBoot" is a stub entry whose factory is pre-registered
     below so mountAllEngines() completes cleanly without warnings. -->
<script id="gosx-manifest" type="application/json">` + manifest + `</script>

<!-- Step 1: set __gosx_scene3d_perf BEFORE bootstrap-runtime.js so 16a picks it up
     (16a-scene-webgpu.js:5855).
     Also pre-register a no-op engine factory for "CanvasSurfaceBoot" so
     mountAllEngines() (bootstrap-feature-engines.js) completes without the
     "no engine factory registered" warn.
     bootstrap-runtime.js reads window.__gosx_engine_factories at init time, so the
     registration must happen before bootstrap-runtime.js runs (it is defer'd,
     so this inline script runs first). -->
<script>
window.__gosx_scene3d_perf = true;

// Pre-register a stub factory for the sentinel engine entry.
(function() {
  var reg = window.__gosx_register_engine_factory;
  if (typeof reg === 'function') {
    reg('CanvasSurfaceBoot', function() { return {}; });
  } else {
    var factories = window.__gosx_engine_factories;
    if (!factories) {
      factories = Object.create(null);
      window.__gosx_engine_factories = factories;
    }
    factories['CanvasSurfaceBoot'] = function() { return {}; };
  }
})();

// Capture console.warn / console.error before bootstrap loads.
var _captured_warns = [];
var _captured_errors = [];
var _captured_logs = [];
var _orig_warn = console.warn.bind(console);
var _orig_error = console.error.bind(console);
var _orig_log = console.log.bind(console);
console.warn = function() {
  var msg = Array.from(arguments).join(' ');
  _captured_warns.push(msg);
  _orig_warn.apply(console, arguments);
};
console.error = function() {
  var msg = Array.from(arguments).join(' ');
  _captured_errors.push(msg);
  _orig_error.apply(console, arguments);
};
console.log = function() {
  var msg = Array.from(arguments).join(' ');
  _captured_logs.push(msg);
  _orig_log.apply(console, arguments);
};

// MutationObserver on both canvases to track data-gosx-surface-id assignment.
var _painterSurfaceId = null;
var _webgpuSurfaceId = null;
function watchCanvas(canvasId, setter) {
  var el = document.getElementById(canvasId);
  if (el && typeof MutationObserver !== 'undefined') {
    new MutationObserver(function(muts) {
      muts.forEach(function(m) {
        if (m.type === 'attributes' && m.attributeName === 'data-gosx-surface-id') {
          setter(el.getAttribute('data-gosx-surface-id'));
        }
      });
    }).observe(el, { attributes: true, attributeFilter: ['data-gosx-surface-id'] });
  }
}
watchCanvas('painter-board', function(id) { _painterSurfaceId = id; });
watchCanvas('webgpu-board',  function(id) { _webgpuSurfaceId = id; });

// Wrap __gosx_hydrate when available to trace calls.
var _hydrateCallLog = [];
var _hydrateWatchInterval = setInterval(function() {
  if (typeof window.__gosx_hydrate === 'function' && !window.__gosx_hydrate._traced) {
    var orig = window.__gosx_hydrate;
    window.__gosx_hydrate = function() {
      var args = Array.from(arguments).map(function(a) {
        return typeof a === 'string' ? (a.length > 80 ? a.slice(0,80)+'...' : a) : String(a);
      });
      var entry = {args: args, t: Date.now()};
      _hydrateCallLog.push(entry);
      _orig_log('[verify] __gosx_hydrate called:', args[0], args[1], args[2]);
      var result = orig.apply(this, arguments);
      entry.result = typeof result === 'string' ? result : (result && result.toString ? result.toString() : 'null');
      return result;
    };
    window.__gosx_hydrate._traced = true;
    clearInterval(_hydrateWatchInterval);
  }
}, 50);

window.__gosx_verify_start = ` + strconv.FormatInt(ts, 10) + `;
</script>

<!-- Step 2: wasm_exec.js — must precede bootstrap-runtime.js -->
<script defer src="/gosx/wasm_exec.js"></script>

<!-- Step 3: bootstrap-runtime.js — the SELECTIVE bootstrap orchestrator.
     Contains 26-runtime-tail.js: sets window.__gosx_register_bootstrap_feature,
     reads the gosx-manifest, detects feature names via manifestFeatureNames(),
     loads WASM, then dynamically loads /gosx/bootstrap-feature-engines.js whose
     runtimeReady() calls mountAllSurfaceKinds().
     mountAllSurfaceKinds() discovers BOTH canvas placeholders via querySelectorAll
     and hydrates each independently with a unique auto-incremented id. -->
<script defer src="/gosx/bootstrap-runtime.js"></script>

<!-- Step 4: scene3d + scene3d-webgpu chunks as STATIC defer tags, in order.
     M1 slice-4 contract: the scene3d-webgpu chunk must be script-tag-present
     at mount time so window.__gosx_scene3d_webgpu_api.createRenderer is available.
     defer scripts execute in document order before DCL: scene3d publishes
     window.__gosx_scene3d_api, then webgpu chunk publishes __gosx_scene3d_webgpu_api.
     On no-GPU environments the chunks parse harmlessly and the WebGPU board
     falls back to the painter with a single console.warn. -->
<script defer src="/gosx/bootstrap-feature-scene3d.js"></script>
<script defer src="/gosx/bootstrap-feature-scene3d-webgpu.js"></script>

<!-- Step 5: PerformanceObserver for scene3d render measures per board.
     We capture all "scene3d-render" measures; they are not split per-board
     (the PerformanceObserver does not expose which board triggered a measure)
     but the aggregate count / p95 is still useful as a system-wide signal. -->
<script>
var _perf_entries = [];
if (typeof PerformanceObserver !== 'undefined') {
  try {
    var _perfObs = new PerformanceObserver(function(list) {
      list.getEntries().forEach(function(entry) {
        if (entry.entryType === 'measure' && entry.name === 'scene3d-render') {
          _perf_entries.push(entry.duration);
        }
      });
    });
    _perfObs.observe({ entryTypes: ['measure'] });
  } catch(e) {}
}
</script>

<!-- Step 6: self-validation script — runs after all resources load.
     Waits for BOTH boards to hydrate (both get data-gosx-surface-id), then
     probes labels layers and screenshots each board, then POSTs a combined
     parity report. NO auto-interaction: both boards render their initial camera
     and sit still. -->
<script>
(function() {
  'use strict';

  var MAX_WAIT_MS = 20000;
  var statusEl  = document.getElementById('status');
  var verdictEl = document.getElementById('verdict');

  var painterCanvas = document.getElementById('painter-board');
  var webgpuCanvas  = document.getElementById('webgpu-board');
  var painterHost   = document.getElementById('painter-host');
  var webgpuHost    = document.getElementById('webgpu-host');

  function setStatus(msg) { if (statusEl) statusEl.textContent = msg; }

  function showVerdict(cls, msg) {
    if (!verdictEl) return;
    verdictEl.className = 'show ' + cls;
    verdictEl.textContent = msg;
  }

  // Wait for a canvas element to receive data-gosx-surface-id.
  function waitForSurfaceId(canvas, deadlineMs) {
    return new Promise(function(resolve) {
      function check() {
        var id = canvas ? canvas.getAttribute('data-gosx-surface-id') : null;
        if (id || Date.now() > deadlineMs) { resolve(id); return; }
        setTimeout(check, 100);
      }
      check();
    });
  }

  // Wait for the WASM runtime ready signal.
  function waitForRuntime(deadlineMs) {
    return new Promise(function(resolve) {
      function check() {
        if ((window.__gosx && window.__gosx.ready) || Date.now() > deadlineMs) {
          resolve(); return;
        }
        setTimeout(check, 100);
      }
      check();
    });
  }

  // probeLabelsLayer locates the label overlay div on the given board host and
  // counts its <span> children.
  //
  // The overlay is created by ensureLabelLayer() in 26b2-canvas-board-labels.js:
  //   layer.style.cssText = "position:absolute;inset:0;overflow:hidden;pointer-events:none;"
  // (26b2-canvas-board-labels.js:146)
  //
  // It is also cached on host.__gosxBoardLabelLayer (line 153), but that private
  // property is not accessible cross-script. We identify it as the first direct
  // child DIV of the host whose inline style contains "pointer-events:none"
  // (the only div the runtime ever adds as a direct child of the board host).
  //
  // WSL headless: both boards fall back to the painter path (no GPU) so no label
  // overlay is ever created; both will return labelsLayerFound:false. The probe
  // code must handle that honestly without crashing.
  function probeLabelsLayer(host) {
    var result = { labelsLayerFound: false, labelSpanCount: 0, sampleLabelTexts: [] };
    if (!host) return result;
    try {
      var children = host.children;
      for (var i = 0; i < children.length; i++) {
        var child = children[i];
        if (child.tagName !== 'DIV') continue;
        var style = child.style;
        // Identify layer: pointer-events:none is set only on the label overlay div.
        // (26b2-canvas-board-labels.js:146: "...pointer-events:none;")
        if (style && style.pointerEvents === 'none') {
          result.labelsLayerFound = true;
          var spans = child.getElementsByTagName('span');
          result.labelSpanCount = spans.length;
          var samples = [];
          for (var j = 0; j < Math.min(3, spans.length); j++) {
            samples.push(spans[j].textContent || '');
          }
          result.sampleLabelTexts = samples;
          break;
        }
      }
    } catch(e) {
      result.error = String(e && e.message ? e.message : e);
    }
    return result;
  }

  // takeScreenshot copies the board canvas to an offscreen canvas and returns
  // { dataURL, paintedPixels }. Also scans for any non-background pixel.
  function takeScreenshot(canvas) {
    var result = { dataURL: '', paintedPixels: false };
    if (!canvas) return result;
    try {
      var offscreen = document.createElement('canvas');
      offscreen.width  = canvas.width  || 620;
      offscreen.height = canvas.height || 400;
      var ctx2d = offscreen.getContext('2d');
      if (ctx2d) {
        ctx2d.drawImage(canvas, 0, 0);
        try {
          result.dataURL = offscreen.toDataURL('image/png');
        } catch(e) {
          result.dataURL = 'error:' + String(e && e.message ? e.message : e);
        }
        // Scan for any non-background pixel. #0f1720 = rgb(15,23,32).
        try {
          var imgData = ctx2d.getImageData(0, 0, offscreen.width, offscreen.height);
          var bg = [15, 23, 32];
          var d = imgData.data;
          for (var pi = 0; pi < d.length; pi += 4 * 8) {
            if (Math.abs(d[pi]-bg[0]) > 8 || Math.abs(d[pi+1]-bg[1]) > 8 || Math.abs(d[pi+2]-bg[2]) > 8) {
              result.paintedPixels = true;
              break;
            }
          }
        } catch(e2) { /* tolerate cross-origin canvas taint */ }
      }
    } catch(e) {
      result.dataURL = 'error:' + String(e && e.message ? e.message : e);
    }
    return result;
  }

  // collectMountAttrs gathers all data-gosx-* attributes from a canvas and its host.
  function collectMountAttrs(canvas, host) {
    var attrs = {};
    [canvas, host].forEach(function(el) {
      if (!el) return;
      for (var i = 0; i < el.attributes.length; i++) {
        var attr = el.attributes[i];
        if (attr.name.startsWith('data-gosx-')) {
          attrs[attr.name] = attr.value;
        }
      }
    });
    return attrs;
  }

  async function run() {
    setStatus('Waiting for gosx runtime ready…');
    var deadline = Date.now() + MAX_WAIT_MS;

    await waitForRuntime(deadline);
    setStatus('Runtime ready. Waiting for both boards to hydrate…');

    // Wait for both boards to receive data-gosx-surface-id (set by mountSurfaceKind
    // after __gosx_hydrate succeeds — 26b-feature-engines-prefix.js:431).
    var painterSurfaceId = await waitForSurfaceId(painterCanvas, deadline);
    var webgpuSurfaceId  = await waitForSurfaceId(webgpuCanvas,  deadline);

    setStatus('Both boards hydrated (or timed out). Waiting two rAF frames before probing…');

    // Wait two rAF cycles to ensure at least one paint frame has completed.
    await new Promise(function(r) {
      requestAnimationFrame(function() { requestAnimationFrame(r); });
    });

    setStatus('Collecting parity data…');

    // ---- Per-board probes ----

    // Labels layer probe (26b2 overlay; only present on WebGPU-routed boards).
    var painterLabels = probeLabelsLayer(painterHost);
    var webgpuLabels  = probeLabelsLayer(webgpuHost);

    // Screenshots.
    var painterShot = takeScreenshot(painterCanvas);
    var webgpuShot  = takeScreenshot(webgpuCanvas);

    // Mount attributes.
    var painterAttrs = collectMountAttrs(painterCanvas, painterHost);
    var webgpuAttrs  = collectMountAttrs(webgpuCanvas,  webgpuHost);

    // WebGPU-route detection for the webgpu board:
    // a fallback warn ("WebGPU backend unavailable") indicates painter fallback.
    var hasFallbackWarn = _captured_warns.some(function(w) {
      return w.indexOf('WebGPU backend unavailable') !== -1 ||
             w.indexOf('falling back to the 2D painter') !== -1;
    });
    var webgpuMeshObjects = -1;
    var meshAttr = webgpuHost ? webgpuHost.getAttribute('data-gosx-scene3d-webgpu-mesh-objects') : null;
    if (meshAttr !== null) webgpuMeshObjects = parseInt(meshAttr, 10);
    var webgpuRoute = !hasFallbackWarn && webgpuMeshObjects > 0;

    // Canvas context probe on the WebGPU board (webgpu context taints 2d).
    var webgpuContextProbe = 'unknown';
    try {
      if (webgpuCanvas) {
        var testCtx = webgpuCanvas.getContext('2d');
        webgpuContextProbe = testCtx === null ? 'webgpu-tainted-2d-null' : '2d-available';
      }
    } catch(e) { webgpuContextProbe = 'error:' + e.message; }

    // GPU adapter info.
    var adapterInfo = null;
    try {
      if (navigator.gpu) {
        var adapter = await navigator.gpu.requestAdapter();
        if (adapter && adapter.info) {
          adapterInfo = {
            vendor: adapter.info.vendor || '',
            architecture: adapter.info.architecture || '',
            device: adapter.info.device || '',
            description: adapter.info.description || ''
          };
        }
      }
    } catch(e) { adapterInfo = { error: String(e && e.message ? e.message : e) }; }

    // Perf stats.
    var perfStats = { count: 0, p50: 0, p95: 0, max: 0 };
    if (_perf_entries.length > 0) {
      var sorted = _perf_entries.slice().sort(function(a,b){return a-b;});
      perfStats.count  = sorted.length;
      perfStats.p50    = sorted[Math.floor(sorted.length * 0.5)] || 0;
      perfStats.p95    = sorted[Math.floor(sorted.length * 0.95)] || 0;
      perfStats.max    = sorted[sorted.length - 1] || 0;
    }

    // ---- Build report ----
    var report = {
      harnessVersion: 'parity-v2',

      // Per-board summaries.
      painterBoard: {
        surfaceId: painterSurfaceId || null,
        paintedPixels: painterShot.paintedPixels,
        mountAttrs: painterAttrs,
        labelsLayerFound: painterLabels.labelsLayerFound,
        labelSpanCount: painterLabels.labelSpanCount,
        sampleLabelTexts: painterLabels.sampleLabelTexts
      },
      webgpuBoard: {
        surfaceId: webgpuSurfaceId || null,
        paintedPixels: webgpuShot.paintedPixels,
        mountAttrs: webgpuAttrs,
        webgpuRoute: webgpuRoute,
        webgpuContextProbe: webgpuContextProbe,
        meshObjectsAttr: meshAttr,
        labelsLayerFound: webgpuLabels.labelsLayerFound,
        labelSpanCount: webgpuLabels.labelSpanCount,
        sampleLabelTexts: webgpuLabels.sampleLabelTexts
      },

      // Top-level fields for backward compat / server summary.
      webgpuRoute: webgpuRoute,
      painterSurfaceId: painterSurfaceId || null,
      webgpuSurfaceId: webgpuSurfaceId || null,
      painterPaintedPixels: painterShot.paintedPixels,
      webgpuPaintedPixels: webgpuShot.paintedPixels,

      navigatorGPU: !!navigator.gpu,
      adapterInfo: adapterInfo,
      warns: _captured_warns,
      errors: _captured_errors,
      logs: (_captured_logs || []).filter(function(l) { return l.indexOf('[verify]') !== -1; }),
      perf: perfStats,
      hydrateCallLog: typeof _hydrateCallLog !== 'undefined' ? _hydrateCallLog : [],
      gosxReadyAtMount: !!(window.__gosx && window.__gosx.ready),
      gosxHydratePresent: typeof window.__gosx_hydrate === 'function',
      gosxRenderCanvasPresent: typeof window.__gosx_render_canvas === 'function',
      gosxPaintCanvasBundlePresent: typeof window.__gosx_paint_canvas_bundle === 'function',
      gosxRuntimeReadyFnPresent: typeof window.__gosx_runtime_ready === 'function',
      mountAllSurfaceKindsPresent: typeof window.__gosx_mount_all_surface_kinds === 'function',

      // Per-board screenshots.
      painterScreenshotDataURL: painterShot.dataURL,
      webgpuScreenshotDataURL: webgpuShot.dataURL,

      timestamp: Date.now()
    };

    // POST report.
    setStatus('POSTing parity report…');
    try {
      var resp = await fetch('/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(report)
      });
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
    } catch(e) {
      setStatus('Report POST failed: ' + e.message);
      return;
    }

    // Show verdict.
    var painterOK = painterShot.paintedPixels;
    var webgpuOK  = webgpuShot.paintedPixels;
    if (painterOK && webgpuOK) {
      showVerdict('pass', 'Both boards painted — open Windows Chrome for GPU verdict');
      setStatus('PASS (painter painted, webgpu painted' + (webgpuRoute ? ', GPU route' : ', painter fallback') + ')');
    } else if (painterOK) {
      showVerdict('info', 'Painter OK — WebGPU board not painted (headless / no GPU)');
      setStatus('painter painted; webgpu board empty (expected in headless). Report: /tmp/gosx-webgpu-verify/report.json');
    } else {
      showVerdict('info', 'Waiting for paint — check server log');
      setStatus('painter not painted yet — check report.json for details');
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', run);
  } else {
    run();
  }
})();
</script>

</body>
</html>`
}

// handleBoardPick is referenced for documentation purposes; the pick callback
// lives on the client side (WASM bridge).
var _ = fmt.Sprintf // keep fmt import used
