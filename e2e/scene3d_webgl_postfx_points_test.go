//go:build e2e

// Regression coverage for the WebGL2 point-pass / post-FX interaction
// investigated as the "giant flat discs" report: m31labs.dev strips its
// entire post-FX chain whenever the runtime lands on WebGL because an
// earlier investigation believed the WebGL backend's postfx compositing
// corrupted point-sprite sizing (see app/galaxy.go's
// galaxySceneWebGLCompatProps comment upstream in m31labs.dev).
//
// Root-cause finding (2026-07): gosx's WebGL post-processing pipeline
// (createScenePostProcessor.begin/apply/applyCustomPost in
// 16-scene-webgl.js) does NOT corrupt point size. Isolated, pixel-level
// testing against the REAL production galaxy scene and the REAL compiled
// Selena post materials showed:
//   - A customPost pass with position-independent sampling (the production
//     "galaxy-stellar-bloom" material) leaves point footprints unchanged
//     (~1.05x baseline) across the scaled-FBO / viewport-uniform / GL-state
//     boundary that createScenePostProcessor manages.
//   - The dramatic "giant disc" appearance traces to the production
//     "galaxy-gravitational-lens" CUSTOM MATERIAL's own UV-displacement
//     math saturating to its configured maxBend across nearly the entire
//     frame (softening=0.02 is small relative to strength/maxBend) —
//     verified with a diagnostic heat-map fragment shader showing
//     bend/maxBend == 1.0 (fully saturated) everywhere except a small
//     center dead-zone. That saturation is identical in the WGSL Selena
//     emits for WebGPU, so it is a shader-content/tuning property, not a
//     WebGL-backend defect.
//
// This test locks in the part that IS gosx's responsibility: the shared
// post-processing plumbing (scaled scene FBO, u_viewportHeight, GL state
// restore across the pass boundary) must not alter point-sprite size when
// a customPost effect runs. It renders a single attenuated point through
// the real WebGL2 backend, once with no postEffects and once with a
// position-independent customPost effect active across several frames, and
// asserts the on-screen footprint stays within a tight bound of the
// no-postfx baseline. It then re-renders with postEffects cleared and
// asserts the footprint returns to baseline, catching any GL state that
// the post pass leaves dirty for the next frame.
package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const scene3DPostfxPointsHTML = `<!doctype html>
<html><head><meta charset="utf-8"></head>
<body style="margin:0;background:#000">
<canvas id="c" width="256" height="256"></canvas>
<script src="/bootstrap.js"></script>
<script>
// A customPost pass whose fragment sampling offsets are FIXED small
// constants, independent of screen position -- the shape of the
// production "galaxy-stellar-bloom" material (see app/galaxy_bloom.go),
// which the investigation showed does not disturb point sizing.
var FIXED_OFFSET_POST_VERTEX = [
  "attribute vec2 a_position;",
  "varying vec2 v_uv;",
  "void main() {",
  "  v_uv = a_position * 0.5 + 0.5;",
  "  gl_Position = vec4(a_position, 0.0, 1.0);",
  "}"
].join("\n");
var FIXED_OFFSET_POST_FRAGMENT = [
  "precision mediump float;",
  "varying vec2 v_uv;",
  "uniform sampler2D _sceneColor;",
  "uniform sampler2D _sceneDepth;",
  "void main() {",
  "  vec4 c0 = texture2D(_sceneColor, v_uv);",
  "  vec4 c1 = texture2D(_sceneColor, v_uv + vec2(0.002, 0.0));",
  "  vec4 c2 = texture2D(_sceneColor, v_uv - vec2(0.002, 0.0));",
  "  gl_FragColor = (c0 * 0.6) + (c1 * 0.2) + (c2 * 0.2);",
  "}"
].join("\n");

window.__renderAndMeasure = function(usePostFX) {
  var out = { ok: false, usePostFX: usePostFX };
  try {
    var api = window.__gosx_scene3d_api;
    if (!api) { out.error = "no __gosx_scene3d_api"; return out; }
    var canvas = document.getElementById("c");
    var backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
    if (!backend) { out.error = "no webgl backend selected"; return out; }
    out.backend = backend.kind || "unknown";
    if (out.backend === "canvas" || out.backend === "canvas2d") { out.error = "fell back to canvas renderer"; return out; }
    if (!window.__renderer) window.__renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
    var renderer = window.__renderer;

    var postEffects = usePostFX ? [{
      kind: "customPost", name: "fixed-offset-post",
      vertexGLSL: FIXED_OFFSET_POST_VERTEX,
      fragmentGLSL: FIXED_OFFSET_POST_FRAGMENT,
      uniforms: {},
      shaderLayout: { uniformBlock: { fields: [] } },
    }] : [];

    var bundle = {
      bundleVersion: 1,
      camera: { x: 0, y: 0, z: 5, fov: 40, near: 0.05, far: 128 },
      environment: { background: "#000000" }, background: "#000000",
      points: [{
        id: "p", count: 1,
        positions: new Float32Array([0, 0, 0]),
        sizes: new Float32Array([1.0]),
        color: "#ffffff", size: 1.0, attenuation: true, style: "circle",
      }],
      instancedMeshes: [], computeParticles: [], objects: [], meshObjects: [],
      materials: [], labels: [], sprites: [], lights: [],
      postEffects: postEffects,
      // Cap well below the canvas pixel count so the postfx path is
      // exercised at a SCALED render target, not a 1:1 passthrough --
      // this is the FBO-size/viewport-uniform boundary the investigation
      // targeted.
      postFXMaxPixels: 10000,
      positions: new Float32Array(0), colors: new Float32Array(0),
      worldPositions: new Float32Array(0), worldColors: new Float32Array(0),
      worldLineWidths: new Float32Array(0),
      worldMeshPositions: new Float32Array(0), worldMeshColors: new Float32Array(0),
      worldMeshNormals: new Float32Array(0), worldMeshUVs: new Float32Array(0),
      worldMeshTangents: new Float32Array(0),
      vertexCount: 0, worldVertexCount: 0,
    };
    var viewport = { cssWidth: 256, cssHeight: 256, pixelWidth: 256, pixelHeight: 256, pixelRatio: 1 };
    // Several frames: a state leak across the pass boundary would show up
    // as drift, not just a one-shot miscalculation.
    for (var f = 0; f < 5; f++) renderer.render(bundle, viewport);

    var gl = canvas.getContext("webgl2");
    if (!gl) { out.error = "no webgl2 context for readback"; return out; }
    var w = 256, h = 256;
    var px = new Uint8Array(w * h * 4);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    gl.readPixels(0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, px);
    var minX = w, maxX = -1, minY = h, maxY = -1, lit = 0;
    for (var y = 0; y < h; y++) {
      for (var x = 0; x < w; x++) {
        var idx = (y * w + x) * 4;
        if (px[idx] > 40 || px[idx + 1] > 40 || px[idx + 2] > 40) {
          lit++;
          if (x < minX) minX = x;
          if (x > maxX) maxX = x;
          if (y < minY) minY = y;
          if (y > maxY) maxY = y;
        }
      }
    }
    out.litPixelCount = lit;
    out.bboxW = lit > 0 ? (maxX - minX + 1) : 0;
    out.bboxH = lit > 0 ? (maxY - minY + 1) : 0;
    out.ok = true;
  } catch (e) {
    out.error = String((e && e.stack) || e);
  }
  return out;
};
</script>
</body></html>`

type scene3DPostfxPointsResult struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error"`
	Backend       string `json:"backend"`
	UsePostFX     bool   `json:"usePostFX"`
	LitPixelCount int    `json:"litPixelCount"`
	BboxW         int    `json:"bboxW"`
	BboxH         int    `json:"bboxH"`
}

// TestWebGLPostFXDoesNotCorruptPointSize renders a single attenuated point
// sprite through the real WebGL2 backend with post-FX off, then on (a
// position-independent customPost pass) across several frames, then off
// again, and asserts the on-screen footprint never grows beyond a tight
// bound of the no-postfx baseline. This is the shared post-processing
// plumbing's contract (scaled scene FBO sizing, u_viewportHeight resolution,
// GL state restore across the customPost pass boundary) — see the file
// comment for the investigation that ruled this machinery in or out.
func TestWebGLPostFXDoesNotCorruptPointSize(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrapPath := filepath.Join(root, "client", "js", "bootstrap.js")

	mux := http.NewServeMux()
	mux.HandleFunc("/bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, bootstrapPath)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(scene3DPostfxPointsHTML))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	// Force ANGLE/SwiftShader so headless Chrome exercises the REAL WebGL2
	// path instead of silently degrading to Canvas2D (which would execute
	// no shaders and prove nothing about this pipeline).
	extraFlags := map[string]any{
		"use-gl":                    "angle",
		"use-angle":                 "swiftshader",
		"enable-unsafe-swiftshader": true,
		"enable-webgl":              true,
		"ignore-gpu-blocklist":      true,
	}
	page := newBrowserPage(t, chrome, extraFlags, 320, 320, "", 60*time.Second)

	if status := page.navigate(t, server.URL+"/"); status < 200 || status > 299 {
		t.Fatalf("test page returned status %d\n\nConsole:\n%s", status, page.Console())
	}
	page.waitFor(t, `!!document.getElementById("c")`, 10*time.Second, "canvas element")

	measure := func(usePostFX bool) scene3DPostfxPointsResult {
		var res scene3DPostfxPointsResult
		expr := "window.__renderAndMeasure(" + boolJS(usePostFX) + ")"
		page.eval(t, expr, &res)
		if !res.OK {
			t.Fatalf("render/measure (postfx=%v) failed: %s\n\nConsole:\n%s", usePostFX, res.Error, page.Console())
		}
		return res
	}

	baseline := measure(false)
	if baseline.Backend == "canvas" || baseline.Backend == "canvas2d" {
		t.Skipf("headless Chrome fell back to %s (no genuine WebGL2 available in this environment); this test proves nothing without a real WebGL2 backend", baseline.Backend)
	}
	if baseline.Backend != "webgl" {
		t.Fatalf("expected backend=webgl, got %q", baseline.Backend)
	}
	if baseline.BboxW == 0 || baseline.BboxH == 0 {
		t.Fatalf("baseline point never rendered (bboxW=%d bboxH=%d, lit=%d) — harness broken\n\nConsole:\n%s",
			baseline.BboxW, baseline.BboxH, baseline.LitPixelCount, page.Console())
	}

	withPostFX := measure(true)
	assertFootprintWithinBound(t, "postfx-on", baseline, withPostFX, 1.5)

	// Re-measure with postFX cleared: any GL state (blend, depth, viewport,
	// bound program/FBO) that applyCustomPost left dirty for the next frame
	// would show up here as a baseline that no longer matches the first one.
	afterPostFX := measure(false)
	assertFootprintWithinBound(t, "postfx-cleared-after", baseline, afterPostFX, 1.3)
}

func assertFootprintWithinBound(t *testing.T, label string, baseline, candidate scene3DPostfxPointsResult, maxRatio float64) {
	t.Helper()
	if candidate.BboxW == 0 || candidate.BboxH == 0 {
		blob, _ := json.Marshal(candidate)
		t.Fatalf("%s: point did not render at all: %s", label, blob)
	}
	widthRatio := float64(candidate.BboxW) / float64(baseline.BboxW)
	heightRatio := float64(candidate.BboxH) / float64(baseline.BboxH)
	if widthRatio > maxRatio || heightRatio > maxRatio {
		t.Fatalf("%s: point footprint grew beyond %.2fx baseline — baseline=%dx%d (%d lit px), %s=%dx%d (%d lit px), widthRatio=%.2f heightRatio=%.2f",
			label, maxRatio, baseline.BboxW, baseline.BboxH, baseline.LitPixelCount,
			label, candidate.BboxW, candidate.BboxH, candidate.LitPixelCount, widthRatio, heightRatio)
	}
}

func boolJS(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
