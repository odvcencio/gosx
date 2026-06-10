// Example canvasboard-webgpu-verify is a throwaway browser validation harness
// for the M1 WebGPU canvas board re-platform.
//
// It serves a CanvasBoard page with data-gosx-canvas-backend="webgpu", full
// WASM hydration, and all required JS chunks (runtime, scene3d-webgpu). The
// page self-validates, exercises primitives, and POSTs a JSON report back.
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
//     The actual canvas2d board is mounted by mountAllSurfaceKinds() via the
//     data-gosx-surface-kind="canvas2d" placeholder, independent of this entry.
//   - runtime.path — WASM URL (26-runtime-tail.js:294-308)
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

	// Serve the board page.
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

	// Receive the self-report from the browser.
	mux.HandleFunc("POST /report", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Parse for screenshot extraction.
		var parsed map[string]json.RawMessage
		_ = json.Unmarshal(raw, &parsed)

		// Write report JSON.
		reportPath := filepath.Join(reportDir, "report.json")
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		if err := os.WriteFile(reportPath, pretty, 0o644); err != nil {
			log.Printf("write report: %v", err)
		}

		// Decode screenshot if present.
		if dataURL, ok := parsed["screenshotDataURL"]; ok {
			var urlStr string
			if err := json.Unmarshal(dataURL, &urlStr); err == nil && strings.HasPrefix(urlStr, "data:image/png;base64,") {
				b64 := strings.TrimPrefix(urlStr, "data:image/png;base64,")
				imgBytes, err := base64.StdEncoding.DecodeString(b64)
				if err == nil {
					imgPath := filepath.Join(reportDir, "board.png")
					if err := os.WriteFile(imgPath, imgBytes, 0o644); err != nil {
						log.Printf("write board.png: %v", err)
					} else {
						log.Printf("[report] screenshot saved: %s", imgPath)
					}
				} else {
					log.Printf("[report] screenshot decode error: %v", err)
				}
			}
		}

		log.Printf("[report] received: %s", reportPath)

		// Print a one-line summary.
		if webgpuRoute, ok := parsed["webgpuRoute"]; ok {
			var route bool
			_ = json.Unmarshal(webgpuRoute, &route)
			var painted bool
			if pp, ok2 := parsed["paintedPixels"]; ok2 {
				_ = json.Unmarshal(pp, &painted)
			}
			var labelsN int
			if lc, ok2 := parsed["labelsCount"]; ok2 {
				_ = json.Unmarshal(lc, &labelsN)
			}
			if route {
				log.Printf("[report] PASS — webgpuRoute=true paintedPixels=%v labelsCount=%d", painted, labelsN)
			} else {
				log.Printf("[report] FALLBACK — webgpuRoute=false paintedPixels=%v labelsCount=%d (see report for warns)", painted, labelsN)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	log.Printf("canvasboard-webgpu-verify listening at http://localhost:8765")
	log.Printf("  board page:   http://localhost:8765/")
	log.Printf("  healthcheck:  http://localhost:8765/healthz")
	log.Printf("  report:       %s/report.json", reportDir)
	log.Printf("  screenshot:   %s/board.png", reportDir)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
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

// boardJSON builds the CanvasBoard props JSON:
// 100 rects (20×5), 3 line nodes, 2 sprite nodes, 3 label nodes.
func boardJSON() string {
	type node struct {
		ID     string  `json:"id,omitempty"`
		Kind   string  `json:"kind"`
		X      float64 `json:"x,omitempty"`
		Y      float64 `json:"y,omitempty"`
		Width  float64 `json:"width,omitempty"`
		Height float64 `json:"height,omitempty"`
		X1     float64 `json:"x1,omitempty"`
		Y1     float64 `json:"y1,omitempty"`
		X2     float64 `json:"x2,omitempty"`
		Y2     float64 `json:"y2,omitempty"`
		Color  string  `json:"color,omitempty"`
		Text   string  `json:"text,omitempty"`
		Src    string  `json:"src,omitempty"`
	}

	palette := []string{"#ff8866", "#88ddff", "#ffd866", "#a0ff88", "#ff88dd"}
	nodes := make([]node, 0, 110)

	// 100 rects in a 20×5 grid.
	for row := 0; row < 5; row++ {
		for col := 0; col < 20; col++ {
			x := float64(col-10) * 60
			y := float64(row-2) * 50
			nodes = append(nodes, node{
				ID:     "rect-" + strconv.Itoa(row) + "-" + strconv.Itoa(col),
				Kind:   "rect",
				X:      x,
				Y:      y,
				Width:  44,
				Height: 32,
				Color:  palette[(row+col)%len(palette)],
			})
		}
	}

	// 3 line nodes.
	nodes = append(nodes,
		node{ID: "line-0", Kind: "line", X1: -300, Y1: -150, X2: 300, Y2: 150, Color: "#ff4444"},
		node{ID: "line-1", Kind: "line", X1: -300, Y1: 150, X2: 300, Y2: -150, Color: "#44ff44"},
		node{ID: "line-2", Kind: "line", X1: 0, Y1: -200, X2: 0, Y2: 200, Color: "#4444ff"},
	)

	// 2 sprite nodes pointing at the test PNG.
	nodes = append(nodes,
		node{ID: "sprite-0", Kind: "image", X: -380, Y: 80, Width: 64, Height: 64, Src: "/test.png"},
		node{ID: "sprite-1", Kind: "image", X: 320, Y: -120, Width: 64, Height: 64, Src: "/test.png"},
	)

	// 3 label nodes.
	nodes = append(nodes,
		node{ID: "label-0", Kind: "label", X: 0, Y: 160, Text: "WebGPU Board"},
		node{ID: "label-1", Kind: "label", X: -300, Y: -160, Text: "M1 verify"},
		node{ID: "label-2", Kind: "label", X: 300, Y: -160, Text: "GoSX 16a"},
	)

	type panXY struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	type props struct {
		Board struct {
			Pan  panXY   `json:"pan"`
			Zoom float64 `json:"zoom"`
		} `json:"board"`
		Background string `json:"background"`
		Nodes      []node `json:"nodes"`
	}
	var p props
	p.Board.Zoom = 1.0
	p.Background = "#0f1720"
	p.Nodes = nodes

	b, _ := json.Marshal(p)
	return string(b)
}

// buildPage returns the full HTML page for the board.
func buildPage() string {
	props := boardJSON()

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
	// The canvas2d board itself is NOT mounted via this engines entry — it is
	// mounted by mountAllSurfaceKinds() in the engines feature (bootstrap-feature-
	// engines.js) which discovers the <canvas data-gosx-surface-kind="canvas2d">
	// placeholder via querySelectorAll and calls __gosx_hydrate directly.
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
<title>GoSX WebGPU Board Verify</title>
<style>
  body { margin:0; background:#0f1720; color:#e6edf3; font-family:system-ui,sans-serif; }
  #board-host { position:relative; width:1280px; height:720px; margin:0 auto; }
  #board { display:block; }
  #verdict { position:fixed; top:16px; right:16px; padding:12px 20px; border-radius:8px;
             font-size:20px; font-weight:bold; z-index:9999; opacity:0; transition:opacity 0.3s; }
  #verdict.show { opacity:1; }
  #verdict.pass { background:#1a5c1a; color:#4dff4d; border:2px solid #4dff4d; }
  #verdict.fail { background:#5c1a1a; color:#ff4d4d; border:2px solid #ff4d4d; }
  #status { position:fixed; bottom:16px; left:16px; font-size:12px; color:#7a8a9a; max-width:600px; }
</style>
</head>
<body>

<div id="board-host">
  <!-- CanvasBoard placeholder: data-gosx-canvas-backend="webgpu" opts into 16a renderer
       (26b-feature-engines-prefix.js:437, _canvasSurfaceWantsWebGPU).
       data-gosx-surface-kind="canvas2d" is picked up by mountAllSurfaceKinds()
       (26b-feature-engines-prefix.js:1054-1055). -->
  <canvas id="board"
    width="1280" height="720"
    data-gosx-surface-kind="canvas2d"
    data-gosx-engine-component="CanvasBoard"
    data-gosx-canvas-backend="webgpu"
    data-gosx-engine-props='` + props + `'
    data-gosx-engine-caps="canvas,webgpu"
    data-gosx-canvas2d="1"
    data-gosx-onpick="handleBoardPick">
  </canvas>
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

// Pre-register a stub factory for the sentinel engine entry so mountAllEngines
// completes silently. The factory immediately returns an empty handle — the
// canvas2d board is mounted independently by mountAllSurfaceKinds() in the
// engines feature, not by this engine entry.
(function() {
  var reg = window.__gosx_register_engine_factory;
  if (typeof reg === 'function') {
    reg('CanvasSurfaceBoot', function() { return {}; });
  } else {
    // bootstrap.js has not run yet — populate the shared registry directly.
    // bootstrap.js picks this up at: const engineFactories = window.__gosx_engine_factories || Object.create(null)
    var factories = window.__gosx_engine_factories;
    if (!factories) {
      factories = Object.create(null);
      window.__gosx_engine_factories = factories;
    }
    factories['CanvasSurfaceBoot'] = function() { return {}; };
  }
})();

// Capture all console.warn and console.error calls before bootstrap loads.
// The WebGPU fallback emits exactly one warn (bootstrap-feature-engines.js).
// console.error captures let the self-report expose hydration failures that
// would otherwise be invisible in the headless run (errors are not visible
// in report.json without this capture).
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

// MutationObserver on the canvas to watch for data-gosx-surface-id.
var _surfaceIdObserved = null;
var _canvasEl = document.getElementById('board');
if (_canvasEl && typeof MutationObserver !== 'undefined') {
  var _surfaceObs = new MutationObserver(function(muts) {
    muts.forEach(function(m) {
      if (m.type === 'attributes' && m.attributeName === 'data-gosx-surface-id') {
        _surfaceIdObserved = _canvasEl.getAttribute('data-gosx-surface-id');
        console.log('[verify] data-gosx-surface-id set:', _surfaceIdObserved);
      }
    });
  });
  _surfaceObs.observe(_canvasEl, { attributes: true, attributeFilter: ['data-gosx-surface-id', 'width', 'height'] });
}

// Wrap __gosx_hydrate when it becomes available to trace calls.
var _hydrateCallLog = [];
var _hydrateWatchInterval = setInterval(function() {
  if (typeof window.__gosx_hydrate === 'function' && !window.__gosx_hydrate._traced) {
    var orig = window.__gosx_hydrate;
    window.__gosx_hydrate = function() {
      var args = Array.from(arguments).map(function(a) {
        if (typeof a === 'string') return a.length > 80 ? a.slice(0,80)+'...' : a;
        return String(a);
      });
      var entry = {args: args, t: Date.now()};
      _hydrateCallLog.push(entry);
      console.log('[verify] __gosx_hydrate called:', args[0], args[1], args[2]);
      var result = orig.apply(this, arguments);
      entry.result = typeof result === 'string' ? result : (result && result.toString ? result.toString() : 'null');
      console.log('[verify] __gosx_hydrate returned:', entry.result);
      return result;
    };
    window.__gosx_hydrate._traced = true;
    clearInterval(_hydrateWatchInterval);
  }
}, 50);

// Timestamp for build-fresh verification.
window.__gosx_verify_start = ` + strconv.FormatInt(ts, 10) + `;
</script>

<!-- Step 2: wasm_exec.js — must precede bootstrap-runtime.js (bootstrap loads Go
     WASM via new Go() constructor; 10-runtime-scene-core.js:49-50) -->
<script defer src="/gosx/wasm_exec.js"></script>

<!-- Step 3: bootstrap-runtime.js — the SELECTIVE bootstrap orchestrator.
     Contains 26-runtime-tail.js: sets window.__gosx_register_bootstrap_feature,
     reads the gosx-manifest, detects feature names via manifestFeatureNames(),
     loads WASM, then dynamically loads /gosx/bootstrap-feature-engines.js whose
     runtimeReady() calls mountAllSurfaceKinds() for the canvas2d board.
     The old monolithic bootstrap.js (30-tail.js path) does NOT have
     mountAllSurfaceKinds() — it calls mountAllEngines + hydrateAllIslands only. -->
<script defer src="/gosx/bootstrap-runtime.js"></script>

<!-- Step 4: scene3d + scene3d-webgpu chunks as STATIC defer tags, in order.
     This is the M1 slice-4 contract verbatim: "the scene3d-webgpu chunk must be
     script-tag-present" — the routed mount checks window.__gosx_scene3d_webgpu_api
     synchronously at mount time (26b-feature-engines-prefix.js) and the runtime
     CLOSES engine-factory registration after init, so dynamically-injected
     after-DCL chunks both miss the mount-time check AND get their factory
     registration rejected ("registration is closed after init" — observed).
     defer scripts execute in document order before DCL: scene3d publishes
     window.__gosx_scene3d_api (sceneApi), then the webgpu chunk imports from it
     and publishes __gosx_scene3d_webgpu_api.createRenderer — both present before
     hydration mounts the surface. On no-GPU environments the chunks parse
     harmlessly (the 16z probe gates actual device use) and the board falls back
     to the painter with the documented single warn. -->
<script defer src="/gosx/bootstrap-feature-scene3d.js"></script>
<script defer src="/gosx/bootstrap-feature-scene3d-webgpu.js"></script>

<!-- Step 5: PerformanceObserver for scene3d render measures.
     Mark names: "scene3d-render-start", "scene3d-render-end"
     Measure name: "scene3d-render"
     (16a-scene-webgpu.js:5857, 5119, 5120) -->
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
  } catch(e) {
    // tolerate environments without PerformanceObserver
  }
}
</script>

<!-- Step 6: self-validation script — runs after all resources load -->
<script>
(function() {
  'use strict';

  var MAX_WAIT_MS = 15000;
  var INTERACT_MS = 5000;
  var statusEl = document.getElementById('status');
  var verdictEl = document.getElementById('verdict');
  var canvas = document.getElementById('board');
  var boardHost = document.getElementById('board-host');

  function setStatus(msg) {
    if (statusEl) statusEl.textContent = msg;
  }

  function showVerdict(pass, msg) {
    if (!verdictEl) return;
    verdictEl.className = 'show ' + (pass ? 'pass' : 'fail');
    verdictEl.textContent = (pass ? 'PASS' : 'FAIL') + ' — ' + msg;
  }

  function wait(ms) {
    return new Promise(function(r) { setTimeout(r, ms); });
  }

  // Dispatch realistic pointer events on the canvas to drive frames.
  function driveInteraction() {
    if (!canvas) return;
    var rect = canvas.getBoundingClientRect();
    var cx = rect.left + rect.width / 2;
    var cy = rect.top + rect.height / 2;

    function makePointerEvent(type, x, y, buttons) {
      return new PointerEvent(type, {
        bubbles: true, cancelable: true,
        clientX: x, clientY: y,
        button: 0, buttons: buttons || 0,
        pointerId: 1, pointerType: 'mouse'
      });
    }
    function makeWheelEvent(dy) {
      return new WheelEvent('wheel', {
        bubbles: true, cancelable: true,
        clientX: cx, clientY: cy,
        deltaY: dy, deltaMode: 0
      });
    }

    // Drag pan: down, move 200px right, move 200px down, up.
    canvas.dispatchEvent(makePointerEvent('pointerdown', cx, cy, 1));
    canvas.dispatchEvent(makePointerEvent('pointermove', cx + 50, cy, 1));
    canvas.dispatchEvent(makePointerEvent('pointermove', cx + 100, cy + 50, 1));
    canvas.dispatchEvent(makePointerEvent('pointermove', cx + 150, cy + 100, 1));
    canvas.dispatchEvent(makePointerEvent('pointermove', cx + 200, cy + 200, 1));
    canvas.dispatchEvent(makePointerEvent('pointerup', cx + 200, cy + 200, 0));

    // Wheel zoom in.
    canvas.dispatchEvent(makeWheelEvent(-120));
    canvas.dispatchEvent(makeWheelEvent(-120));

    // Drag pan back.
    canvas.dispatchEvent(makePointerEvent('pointerdown', cx + 200, cy + 200, 1));
    canvas.dispatchEvent(makePointerEvent('pointermove', cx, cy, 1));
    canvas.dispatchEvent(makePointerEvent('pointerup', cx, cy, 0));

    // Wheel zoom out.
    canvas.dispatchEvent(makeWheelEvent(120));
  }

  // Poll for WebGPU-route attributes on the canvas element.
  // The 16a renderer publishes data-gosx-scene3d-webgpu-mesh-objects on the
  // canvas's PARENT (the host) after the first render frame.
  // (16a-scene-webgpu.js:5640 sets mount.setAttribute on the mount element,
  // which is canvas.parentNode in the WebGPU canvas board path — the
  // _startCanvasSurfaceWebGPURAF stores instance.webgpuHost = canvas.parentNode)
  function pollForWebGPUAttrs(deadline) {
    return new Promise(function(resolve) {
      var interval = setInterval(function() {
        var mount = canvas && (boardHost || canvas.parentNode);
        var meshObj = mount ? mount.getAttribute('data-gosx-scene3d-webgpu-mesh-objects') : null;
        var surfaceId = canvas ? canvas.getAttribute('data-gosx-surface-id') : null;
        if ((meshObj !== null && parseInt(meshObj, 10) > 0) || Date.now() > deadline) {
          clearInterval(interval);
          resolve({ meshObjects: meshObj, surfaceId: surfaceId });
        }
      }, 200);
    });
  }

  async function run() {
    setStatus('Waiting for gosx runtime ready…');
    var deadline = Date.now() + MAX_WAIT_MS;

    // Wait for WASM runtime + feature loading.
    await new Promise(function(resolve) {
      function check() {
        if ((window.__gosx && window.__gosx.ready) || Date.now() > deadline) {
          resolve();
          return;
        }
        setTimeout(check, 100);
      }
      check();
    });

    setStatus('Runtime ready. Waiting for WebGPU hydration…');

    // Wait for canvas2d board hydration (surface-id attribute set by
    // mountSurfaceKind after __gosx_hydrate succeeds —
    // bootstrap-feature-engines.js sn() function line: try{o.setAttribute(...)}).
    // The polling timeout is MAX_WAIT_MS - time already spent waiting for WASM.
    await new Promise(function(resolve) {
      function checkHydrated() {
        var surfaceId = canvas ? canvas.getAttribute('data-gosx-surface-id') : null;
        if (surfaceId || Date.now() > deadline) { resolve(); return; }
        setTimeout(checkHydrated, 100);
      }
      checkHydrated();
    });

    // Diagnostic: emit current surface-id and __gosx_ready state.
    var surfaceIdAtMount = canvas ? canvas.getAttribute('data-gosx-surface-id') : null;
    var gosxReadyAtMount = window.__gosx && window.__gosx.ready;

    // Capture a pixel snapshot BEFORE interaction so the check is not affected
    // by camera pan/zoom moving content out of frame.
    var _prePaintedPixels = false;
    try {
      // Wait one rAF to ensure at least one paint frame has completed.
      await new Promise(function(r) { requestAnimationFrame(function() { requestAnimationFrame(r); }); });
      var preOffscreen = document.createElement('canvas');
      preOffscreen.width = canvas.width || 1280;
      preOffscreen.height = canvas.height || 720;
      var preCtx = preOffscreen.getContext('2d');
      if (preCtx) {
        preCtx.drawImage(canvas, 0, 0);
        try {
          var preImg = preCtx.getImageData(0, 0, preOffscreen.width, preOffscreen.height);
          var preBg = [15, 23, 32];
          var preD = preImg.data;
          for (var prePi = 0; prePi < preD.length; prePi += 4 * 8) {
            if (Math.abs(preD[prePi] - preBg[0]) > 8 || Math.abs(preD[prePi+1] - preBg[1]) > 8 || Math.abs(preD[prePi+2] - preBg[2]) > 8) {
              _prePaintedPixels = true;
              break;
            }
          }
        } catch(_e) { /* tolerate */ }
      }
    } catch(_e) { /* tolerate */ }

    setStatus('Board hydrated. Driving interaction for ' + (INTERACT_MS/1000) + 's…');

    // Drive interaction over INTERACT_MS to collect perf measures.
    var interactEnd = Date.now() + INTERACT_MS;
    while (Date.now() < interactEnd) {
      driveInteraction();
      await wait(200);
    }

    setStatus('Collecting results…');

    // Poll for WebGPU mesh-object attrs (may appear on canvas's parent).
    var attrResult = await pollForWebGPUAttrs(Date.now() + 3000);

    // Collect all data-gosx-* attrs from canvas and its parent.
    var mountAttrs = {};
    [canvas, boardHost].forEach(function(el) {
      if (!el) return;
      for (var i = 0; i < el.attributes.length; i++) {
        var attr = el.attributes[i];
        if (attr.name.startsWith('data-gosx-')) {
          mountAttrs[attr.name] = attr.value;
        }
      }
    });

    // Determine WebGPU route: no fallback warn AND mesh-objects > 0.
    var hasFallbackWarn = _captured_warns.some(function(w) {
      return w.indexOf('WebGPU backend unavailable') !== -1 ||
             w.indexOf('falling back to the 2D painter') !== -1;
    });
    var meshObjects = attrResult && attrResult.meshObjects !== null
      ? parseInt(attrResult.meshObjects, 10) : -1;
    var webgpuRoute = !hasFallbackWarn && meshObjects > 0;

    // Canvas context probe: on a WebGPU canvas, getContext('2d') returns null
    // (the context was already taken by 'webgpu').
    var canvasContextProbe = 'unknown';
    try {
      var testCtx = canvas.getContext('2d');
      canvasContextProbe = testCtx === null ? 'webgpu-tainted-2d-null' : '2d-available';
    } catch(e) {
      canvasContextProbe = 'error:' + e.message;
    }

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
    } catch(e) {
      adapterInfo = { error: String(e.message || e) };
    }

    // Perf stats from PerformanceObserver.
    var perfStats = { count: 0, p50: 0, p95: 0, max: 0 };
    if (_perf_entries.length > 0) {
      var sorted = _perf_entries.slice().sort(function(a,b){return a-b;});
      perfStats.count = sorted.length;
      perfStats.p50 = sorted[Math.floor(sorted.length * 0.5)] || 0;
      perfStats.p95 = sorted[Math.floor(sorted.length * 0.95)] || 0;
      perfStats.max = sorted[sorted.length - 1] || 0;
    }

    // Count label spans in the label overlay.
    var labelsLayer = boardHost ? boardHost.querySelectorAll('[data-gosx-canvas-label]') : [];
    var labelsCount = labelsLayer.length;

    // Screenshot: draw the board canvas into a 2D offscreen canvas via drawImage.
    // Also check for painted pixels (any non-background pixel proves the board painted).
    //
    // Scanning strategy: sample every 8th pixel across the FULL canvas (strided grid).
    // A 256×256 crop is NOT enough — after interaction the camera may have panned such
    // that colored rects only appear in the center or right half of the canvas.
    var screenshotDataURL = '';
    var paintedPixels = false;
    try {
      var offscreen = document.createElement('canvas');
      offscreen.width = canvas.width;
      offscreen.height = canvas.height;
      var ctx2d = offscreen.getContext('2d');
      if (ctx2d) {
        ctx2d.drawImage(canvas, 0, 0);
        screenshotDataURL = offscreen.toDataURL('image/png');
        // Strided scan of the full canvas for any non-background pixel.
        // #0f1720 = rgb(15,23,32). Threshold >8 tolerates subpixel AA bleed.
        try {
          var imgData = ctx2d.getImageData(0, 0, offscreen.width, offscreen.height);
          var bg = [15, 23, 32]; // #0f1720
          var d = imgData.data;
          var stride = 8; // sample every 8th pixel for performance
          for (var pi = 0; pi < d.length; pi += 4 * stride) {
            if (Math.abs(d[pi] - bg[0]) > 8 || Math.abs(d[pi+1] - bg[1]) > 8 || Math.abs(d[pi+2] - bg[2]) > 8) {
              paintedPixels = true;
              break;
            }
          }
        } catch(e2) { /* tolerate cross-origin canvas taint */ }
      }
    } catch(e) {
      screenshotDataURL = 'error:' + String(e.message || e);
    }

    var report = {
      webgpuRoute: webgpuRoute,
      navigatorGPU: !!navigator.gpu,
      adapterInfo: adapterInfo,
      canvasContextProbe: canvasContextProbe,
      mountAttrs: mountAttrs,
      warns: _captured_warns,
      errors: _captured_errors,
      logs: (_captured_logs || []).filter(function(l) { return l.indexOf('[verify]') !== -1; }),
      perf: perfStats,
      labelsCount: labelsCount,
      paintedPixels: paintedPixels || _prePaintedPixels,
      surfaceIdAtMount: surfaceIdAtMount,
      surfaceIdObserved: typeof _surfaceIdObserved !== 'undefined' ? _surfaceIdObserved : null,
      hydrateCallLog: typeof _hydrateCallLog !== 'undefined' ? _hydrateCallLog : [],
      gosxReadyAtMount: !!gosxReadyAtMount,
      gosxHydratePresent: typeof window.__gosx_hydrate === 'function',
      gosxEngineFactoriesPresent: typeof window.__gosx_engine_factories === 'object' && !!window.__gosx_engine_factories,
      gosxRenderCanvasPresent: typeof window.__gosx_render_canvas === 'function',
      gosxPaintCanvasBundlePresent: typeof window.__gosx_paint_canvas_bundle === 'function',
      gosxRuntimeReadyFnPresent: typeof window.__gosx_runtime_ready === 'function',
      mountAllSurfaceKindsPresent: typeof window.__gosx_mount_all_surface_kinds === 'function',
      screenshotDataURL: screenshotDataURL,
      meshObjectsAttr: attrResult ? attrResult.meshObjects : null,
      timestamp: Date.now()
    };

    // POST report.
    setStatus('POSTing report…');
    try {
      var resp = await fetch('/report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(report)
      });
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
    } catch(e) {
      setStatus('Report POST failed: ' + e.message);
    }

    // Show verdict banner.
    if (webgpuRoute) {
      showVerdict(true, 'WebGPU route confirmed (mesh-objects=' + meshObjects + ')');
      setStatus('PASS — WebGPU board rendered. Report saved to ' + '/tmp/gosx-webgpu-verify/report.json');
    } else {
      var reason = hasFallbackWarn ? 'fallback warn detected' : 'mesh-objects=' + meshObjects;
      showVerdict(false, 'Fell back to 2D painter (' + reason + ')');
      setStatus('FALLBACK — ' + reason + '. Report: /tmp/gosx-webgpu-verify/report.json');
    }
  }

  // Start after page load.
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

// handleBoardPick is referenced in the page's onpick attribute but lives on
// the client side (WASM bridge). It's named here to document the contract.
var _ = fmt.Sprintf // keep fmt import used
