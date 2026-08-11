package visual

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOuroborosPixelEvidenceBrowserSmoke(t *testing.T) {
	if os.Getenv("GOSX_OUROBOROS_PIXEL_BROWSER_SMOKE") != "1" {
		t.Skip("set GOSX_OUROBOROS_PIXEL_BROWSER_SMOKE=1 to run the browser smoke")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
<div id="scene-smoke"
  data-gosx-scene3d-backend="webgl"
  data-gosx-scene3d-renderer="webgl"
  data-gosx-scene3d-render-gpu="true"
  data-gosx-scene3d-render-implementation="webgl2"
  data-gosx-scene3d-render-backend-truth="{&quot;backend&quot;:&quot;webgl&quot;,&quot;gpu&quot;:true,&quot;fallbackReason&quot;:&quot;&quot;,&quot;implementation&quot;:&quot;webgl2&quot;,&quot;adapter&quot;:&quot;synthetic&quot;,&quot;adapterInfo&quot;:{&quot;vendor&quot;:&quot;synthetic&quot;},&quot;deviceLost&quot;:false,&quot;initError&quot;:&quot;&quot;,&quot;lastError&quot;:&quot;&quot;,&quot;shaderDiagnostics&quot;:{&quot;messages&quot;:1,&quot;errors&quot;:1}}"
  data-gosx-scene3d-webgl-frame-seq="40">
  <canvas data-gosx-scene3d-canvas width="96" height="64" style="width:96px;height:64px"></canvas>
</div>
<script>
const c = document.querySelector("canvas");
const ctx = c.getContext("2d");
const g = ctx.createLinearGradient(0, 0, c.width, c.height);
g.addColorStop(0, "#083344");
g.addColorStop(1, "#facc15");
ctx.fillStyle = g;
ctx.fillRect(0, 0, c.width, c.height);
setInterval(() => {
  const m = document.querySelector("#scene-smoke");
  m.setAttribute("data-gosx-scene3d-webgl-frame-seq", String(Number(m.getAttribute("data-gosx-scene3d-webgl-frame-seq") || 0) + 1));
}, 16);
</script>
</body>
</html>`))
	}))
	defer server.Close()

	manifest, err := CapturePixelEvidence(context.Background(), server.URL, PixelEvidenceOptions{
		RouteID:        "R08-smoke",
		ArtifactRoot:   filepath.Join(t.TempDir(), "pixels"),
		Source:         testPixelSource(),
		Backend:        RequireBackendWebGL,
		Samples:        3,
		SettledWait:    50 * time.Millisecond,
		WarmupFrames:   1,
		WaitSelector:   "canvas",
		CanvasSelector: "canvas[data-gosx-scene3d-canvas]",
		Timeout:        20 * time.Second,
	})
	if err == nil {
		t.Fatalf("CapturePixelEvidence certified a synthetic browser smoke")
	}
	if len(manifest.States) != 2 {
		t.Fatalf("states = %d, want 2", len(manifest.States))
	}
	for _, state := range manifest.States {
		if len(state.Captures) != 3 {
			t.Fatalf("%s captures = %d, want 3", state.State, len(state.Captures))
		}
		capture := state.Captures[0]
		if capture.Blank || capture.Placeholder {
			t.Fatalf("%s capture was rejected as blank", state.State)
		}
		if _, err := os.Stat(capture.Path); err != nil {
			t.Fatalf("stat %s: %v", capture.Path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(manifest.ArtifactRoot, "pixel-evidence.json")); err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	if len(manifest.Failures) == 0 {
		t.Fatalf("synthetic smoke produced no certification failure")
	}
}

func TestOuroborosBackendSelectionBrowserSmoke(t *testing.T) {
	if os.Getenv("GOSX_OUROBOROS_PIXEL_BROWSER_SMOKE") != "1" {
		t.Skip("set GOSX_OUROBOROS_PIXEL_BROWSER_SMOKE=1 to run the browser smoke")
	}

	t.Run("webgl pre navigation flag", func(t *testing.T) {
		server := newBackendSelectionSmokeServer(t, "webgl", true)
		defer server.Close()

		manifest, err := CapturePixelEvidence(context.Background(), server.URL, PixelEvidenceOptions{
			RouteID:        "R08-webgl-force",
			ArtifactRoot:   filepath.Join(t.TempDir(), "pixels"),
			Source:         testPixelSource(),
			Backend:        RequireBackendWebGL,
			ForceWebGL:     true,
			Samples:        3,
			SettledWait:    50 * time.Millisecond,
			WarmupFrames:   1,
			WaitSelector:   "canvas",
			CanvasSelector: "canvas[data-gosx-scene3d-canvas]",
			Timeout:        20 * time.Second,
		})
		if err == nil {
			t.Fatalf("synthetic WebGL backend selection certified")
		}
		assertBackendSelectionSmokeCaptured(t, manifest, "webgl", true)
		if manifest.BackendSelection.RuntimeObservedBackend != "webgl" {
			t.Fatalf("observed backend = %q, want webgl", manifest.BackendSelection.RuntimeObservedBackend)
		}
	})

	t.Run("webgpu clears force flag", func(t *testing.T) {
		server := newBackendSelectionSmokeServer(t, "webgpu", false)
		defer server.Close()

		manifest, err := CapturePixelEvidence(context.Background(), server.URL, PixelEvidenceOptions{
			RouteID:        "R10-webgpu-clear",
			ArtifactRoot:   filepath.Join(t.TempDir(), "pixels"),
			Source:         testPixelSource(),
			Backend:        RequireBackendWebGPU,
			Samples:        3,
			SettledWait:    50 * time.Millisecond,
			WarmupFrames:   1,
			WaitSelector:   "canvas",
			CanvasSelector: "canvas[data-gosx-scene3d-canvas]",
			Timeout:        20 * time.Second,
		})
		if err == nil {
			t.Fatalf("synthetic WebGPU backend selection certified")
		}
		assertBackendSelectionSmokeCaptured(t, manifest, "webgpu", false)
		if manifest.BackendSelection.RuntimeObservedBackend != "webgpu" {
			t.Fatalf("observed backend = %q, want webgpu", manifest.BackendSelection.RuntimeObservedBackend)
		}
	})

	t.Run("observed mismatch fails", func(t *testing.T) {
		server := newBackendSelectionSmokeServer(t, "webgpu", true)
		defer server.Close()

		manifest, err := CapturePixelEvidence(context.Background(), server.URL, PixelEvidenceOptions{
			RouteID:        "R08-mismatch",
			ArtifactRoot:   filepath.Join(t.TempDir(), "pixels"),
			Source:         testPixelSource(),
			Backend:        RequireBackendWebGL,
			ForceWebGL:     true,
			Samples:        3,
			SettledWait:    50 * time.Millisecond,
			WarmupFrames:   1,
			WaitSelector:   "canvas",
			CanvasSelector: "canvas[data-gosx-scene3d-canvas]",
			Timeout:        2 * time.Second,
		})
		if err == nil {
			t.Fatalf("backend mismatch passed")
		}
		if manifest.Certified {
			t.Fatalf("backend mismatch certified")
		}
		if manifest.BackendSelection.RuntimeObservedBackend != "webgpu" {
			t.Fatalf("observed backend = %q, want webgpu", manifest.BackendSelection.RuntimeObservedBackend)
		}
		if len(manifest.Failures) == 0 {
			t.Fatalf("backend mismatch did not record a failure")
		}
		if !containsFailure(manifest.Failures, "runtime observed backend=webgpu") {
			t.Fatalf("failures = %v, want direct backend mismatch", manifest.Failures)
		}
	})

	t.Run("sample time backend flip fails", func(t *testing.T) {
		server := newBackendFlipSmokeServer(t)
		defer server.Close()

		manifest, err := CapturePixelEvidence(context.Background(), server.URL, PixelEvidenceOptions{
			RouteID:        "R08-sample-flip",
			ArtifactRoot:   filepath.Join(t.TempDir(), "pixels"),
			Source:         testPixelSource(),
			Backend:        RequireBackendWebGL,
			ForceWebGL:     true,
			Samples:        3,
			SettledWait:    50 * time.Millisecond,
			WarmupFrames:   1,
			WaitSelector:   "canvas",
			CanvasSelector: "canvas[data-gosx-scene3d-canvas]",
			Timeout:        5 * time.Second,
		})
		if err == nil {
			t.Fatalf("sample-time backend flip passed")
		}
		if manifest.BackendSelection.RuntimeObservedBackend != "webgpu" {
			t.Fatalf("observed backend = %q, want newest webgpu", manifest.BackendSelection.RuntimeObservedBackend)
		}
		if !containsFailure(manifest.Failures, "initial sample 0: runtime observed backend=webgpu") {
			t.Fatalf("failures = %v, want sample-time backend mismatch", manifest.Failures)
		}
	})
}

func newBackendSelectionSmokeServer(t *testing.T, observedBackend string, expectForced bool) *httptest.Server {
	t.Helper()
	forceWant := "false"
	if expectForced {
		forceWant = "true"
	}
	frameAttr := "data-gosx-scene3d-webgl-frame-seq"
	implementation := "webgl2"
	if observedBackend == "webgpu" {
		frameAttr = "data-gosx-scene3d-webgpu-frame-seq"
		implementation = "dawn"
	}
	truth := fmt.Sprintf(`{"backend":%q,"renderer":%q,"gpu":true,"fallbackReason":"","implementation":%q,"adapter":"synthetic","adapterInfo":{"vendor":"synthetic"},"deviceLost":false,"initError":"","lastError":"","shaderDiagnostics":{"messages":1,"errors":1}}`, observedBackend, observedBackend, implementation)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head>
<script>window.__probeForceAtDocumentStart = window.__gosx_scene3d_force_webgl === true;</script>
</head>
<body>
<div id="scene-smoke" data-gosx-scene3d-backend="%s" data-gosx-scene3d-renderer="%s" data-gosx-scene3d-render-gpu="true" data-gosx-scene3d-render-implementation="%s">
  <canvas data-gosx-scene3d-canvas width="96" height="64" style="width:96px;height:64px"></canvas>
</div>
<script>
const expectedForce = %s;
const m = document.querySelector("#scene-smoke");
if (window.__probeForceAtDocumentStart === expectedForce) {
  m.setAttribute("data-gosx-scene3d-render-backend-truth", %q);
  m.setAttribute(%q, "40");
}
const c = document.querySelector("canvas");
const ctx = c.getContext("2d");
for (let y = 0; y < c.height; y++) {
  for (let x = 0; x < c.width; x++) {
    ctx.fillStyle = "rgb(" + ((x * 3) %% 255) + "," + ((y * 5) %% 255) + "," + ((x + y * 2) %% 255) + ")";
    ctx.fillRect(x, y, 1, 1);
  }
}
setInterval(() => {
  const attr = %q;
  if (m.hasAttribute(attr)) {
    m.setAttribute(attr, String(Number(m.getAttribute(attr) || 0) + 1));
  }
}, 16);
</script>
</body>
</html>`, observedBackend, observedBackend, implementation, forceWant, truth, frameAttr, frameAttr)
	}))
}

func newBackendFlipSmokeServer(t *testing.T) *httptest.Server {
	t.Helper()
	webglTruth := `{"backend":"webgl","renderer":"webgl","gpu":true,"fallbackReason":"","implementation":"webgl2","adapter":"synthetic","adapterInfo":{"vendor":"synthetic"},"deviceLost":false,"initError":"","lastError":"","shaderDiagnostics":{"messages":1,"errors":1}}`
	webgpuTruth := `{"backend":"webgpu","renderer":"webgpu","gpu":true,"fallbackReason":"","implementation":"dawn","adapter":"synthetic","adapterInfo":{"vendor":"synthetic"},"deviceLost":false,"initError":"","lastError":"","shaderDiagnostics":{"messages":1,"errors":1}}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<body>
<div id="scene-smoke" data-gosx-scene3d-backend="webgl" data-gosx-scene3d-renderer="webgl" data-gosx-scene3d-webgl-frame-seq="40">
  <canvas data-gosx-scene3d-canvas width="96" height="64" style="width:96px;height:64px"></canvas>
</div>
<script>
const webglTruth = %q;
const webgpuTruth = %q;
let truthReads = 0;
const nativeGetAttribute = Element.prototype.getAttribute;
Element.prototype.getAttribute = function(name) {
  if (this.id === "scene-smoke" && name === "data-gosx-scene3d-render-backend-truth") {
    truthReads++;
    return truthReads <= 2 ? webglTruth : webgpuTruth;
  }
  return nativeGetAttribute.call(this, name);
};
const c = document.querySelector("canvas");
const ctx = c.getContext("2d");
for (let y = 0; y < c.height; y++) {
  for (let x = 0; x < c.width; x++) {
    ctx.fillStyle = "rgb(" + ((x * 3) %% 255) + "," + ((y * 7) %% 255) + "," + ((x + y) %% 255) + ")";
    ctx.fillRect(x, y, 1, 1);
  }
}
</script>
</body>
</html>`, webglTruth, webgpuTruth)
	}))
}

func assertBackendSelectionSmokeCaptured(t *testing.T, manifest PixelEvidenceManifest, backend string, forced bool) {
	t.Helper()
	if manifest.BackendSelection.RequestedBackend != backend {
		t.Fatalf("requested backend = %q, want %s", manifest.BackendSelection.RequestedBackend, backend)
	}
	if manifest.BackendSelection.ForceWebGL != forced {
		t.Fatalf("forceWebGL = %v, want %v", manifest.BackendSelection.ForceWebGL, forced)
	}
	if len(manifest.States) != 2 {
		t.Fatalf("states = %d, want 2", len(manifest.States))
	}
	for _, state := range manifest.States {
		if len(state.Captures) != 3 {
			t.Fatalf("%s captures = %d, want 3", state.State, len(state.Captures))
		}
		if state.Captures[0].Backend != backend {
			t.Fatalf("%s backend = %q, want %s", state.State, state.Captures[0].Backend, backend)
		}
	}
}
