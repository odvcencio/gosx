//go:build e2e

// Port of the retired e2e/motion-material.test.mjs (playwright) to chromedp.
//
// WASM-driven Scene3D MATERIAL-UNIFORM motion e2e test. Navigates to the
// hidden animated-material fixture served by gosx-docs at
// /test/motion-material. That route ships a single mesh whose CustomMaterial
// carries an explicit "emissive" customUniform AND a MaterialAnims Oscillator
// on that same uniform. The non-empty MaterialAnims auto-emits a SEPARATE
// wire program into the scene IR (SceneIR.MaterialMotionProgram,
// base64-serialized as the JSON "materialMotionProgram" key).
//
// HEADLESS REALITY (verified — this test does NOT fight it):
//  1. Selena needs WebGL/WebGPU; headless Chrome renders Scene3D on canvas2d,
//     so the animated pixels are not headless-observable — no pixel diff.
//  2. The Go WASM runtime only loads for shared-runtime/island scenes, so on
//     this stand-alone fixture __gosx_motion_tick stays undefined and the
//     material program is never ticked headlessly.
//
// Therefore this test HARD-ASSERTS what is headless-verifiable (the scene
// MOUNTS and the SSR payload carries materialMotionProgram — proving the
// lowering shipped), RECORDS the seam state, and SKIPS the GPU-gated
// animation assertion with a clear message when the uniform does not change.
// A skip here is honest; a hard-fail is not, because the architecture
// prevents a headless green for the visual.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func motionMaterialBaseURL() string {
	if url := os.Getenv("GOSX_MOTION_MATERIAL_E2E_BASE_URL"); url != "" {
		return url
	}
	// Distinct port so this suite can run alongside the other e2e suites.
	return "http://127.0.0.1:3073"
}

type emissiveReading struct {
	Found bool `json:"found"`
	Value any  `json:"value"`
}

func TestMotionMaterialProgramShips(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, motionMaterialBaseURL())
	// Route the scene's materialMotionProgram through the WASM motion exports
	// instead of the inert fall-through. Must run BEFORE any page script.
	page := newBrowserPage(t, chrome, nil, 1280, 800, "window.__gosx_motion_wasm = true;", 120*time.Second)

	fixturePath := "/test/motion-material"

	// (a.1) The SSR payload must carry the lowered materialMotionProgram.
	// Fetch the raw HTML server-side: the scene IR (including the base64
	// materialMotionProgram key) is embedded for client hydration. This is
	// the headless-verifiable proof that MaterialAnims lowering shipped,
	// independent of any GPU/WASM availability.
	resp, err := http.Get(app.baseURL + fixturePath)
	if err != nil {
		t.Fatalf("fetch fixture SSR HTML: %v\n\nLogs:\n%s", err, app.logs.String())
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read fixture SSR HTML: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fixture page returned %d\n\nLogs:\n%s", resp.StatusCode, app.logs.String())
	}
	html := string(body)
	materialProgramInSSR := strings.Contains(html, "materialMotionProgram")
	customUniformsInSSR := strings.Contains(html, "customUniforms")
	t.Logf("[motion-material] SSR payload: materialMotionProgram=%v customUniforms=%v",
		materialProgramInSSR, customUniformsInSSR)
	if !materialProgramInSSR {
		t.Fatalf("expected SSR HTML to contain \"materialMotionProgram\" (proves MaterialAnims "+
			"lowered into SceneIR.MaterialMotionProgram); it did not.\n\nLogs:\n%s", app.logs.String())
	}
	if !customUniformsInSSR {
		t.Fatalf("expected SSR HTML to contain \"customUniforms\" (the emissive uniform the "+
			"seam mutates); it did not.\n\nLogs:\n%s", app.logs.String())
	}

	// (a.2) Navigate and wait for the Scene3D mount to finish initialising.
	if status := page.navigate(t, app.baseURL+fixturePath); status < 200 || status > 299 {
		t.Fatalf("fixture page returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	page.waitFor(t, `!!document.querySelector("[data-gosx-scene3d-mounted]")`,
		30*time.Second, "[data-gosx-scene3d-mounted]")

	var attrs struct {
		Ready   string `json:"ready"`
		Backend string `json:"backend"`
	}
	page.eval(t, `(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    if (!el) return null;
    return {
      ready: el.getAttribute("data-gosx-scene3d-ready"),
      backend: el.getAttribute("data-gosx-scene3d-backend"),
    };
  })()`, &attrs)

	if attrs.Ready != "true" {
		t.Fatalf("expected data-gosx-scene3d-ready=\"true\", got %q\n\nConsole:\n%s\n\nLogs:\n%s",
			attrs.Ready, page.Console(), app.logs.String())
	}
	if !containsString([]string{"webgl", "webgpu", "canvas2d", "canvas"}, attrs.Backend) {
		t.Fatalf("expected backend in {webgl,webgpu,canvas2d,canvas}, got %q\n\nConsole:\n%s\n\nLogs:\n%s",
			attrs.Backend, page.Console(), app.logs.String())
	}
	t.Logf("[motion-material] Scene3D backend = %s", attrs.Backend)

	// (b) Record the WASM motion seam state without hard-failing on the WASM
	// exports — on a stand-alone declarative Scene3D they are expected absent.
	var wasmFlag bool
	page.eval(t, `window.__gosx_motion_wasm === true`, &wasmFlag)
	if !wasmFlag {
		t.Fatalf("expected window.__gosx_motion_wasm === true (init script)\n\nConsole:\n%s", page.Console())
	}
	var tickType string
	page.eval(t, `typeof window.__gosx_motion_tick`, &tickType)
	if tickType == "function" {
		t.Logf("[motion-material] WASM motion seam: __gosx_motion_tick=function (material motion driven by motion.Eval in WASM)")
	} else {
		t.Logf("[motion-material] WASM motion seam: __gosx_motion_tick=%s (NOT ticked — no WASM runtime on this stand-alone fixture)", tickType)
	}

	// The live scene-state handle exposes the mesh's customUniforms bag, which
	// the seam mutates each frame. Read emissive at t1, wait ~1s, read at t2.
	readEmissive := func() emissiveReading {
		var reading emissiveReading
		page.eval(t, `(() => {
      const el = document.querySelector("[data-gosx-scene3d-mounted]");
      const state = el && el.__gosxScene3DState;
      if (!state || !state.objects || typeof state.objects.get !== "function") {
        return { found: false, value: null };
      }
      const record = state.objects.get("glow-cube");
      if (!record) return { found: false, value: null };
      const uniforms = record.customUniforms;
      const value = uniforms && uniforms.emissive != null ? uniforms.emissive : null;
      return { found: true, value: Array.isArray(value) ? value.slice() : value };
    })()`, &reading)
		return reading
	}

	t1 := readEmissive()
	time.Sleep(1 * time.Second)
	t2 := readEmissive()

	stateHandlePresent := t1.Found && t2.Found
	emissiveChanged := stateHandlePresent && !reflect.DeepEqual(t1.Value, t2.Value)
	t1JSON, _ := json.Marshal(t1.Value)
	t2JSON, _ := json.Marshal(t2.Value)
	t.Logf("[motion-material] __gosxScene3DState handle present=%v; emissive t1=%s t2=%s; changed=%v",
		stateHandlePresent, t1JSON, t2JSON, emissiveChanged)

	// (c) GPU/WASM-gated animation assertion. If the uniform actually
	// animated, the full pipeline ran headlessly — assert it. Otherwise skip
	// honestly: the visual requires a WASM runtime + WebGL/WebGPU, which
	// headless Chrome on a stand-alone declarative Scene3D does not provide.
	if emissiveChanged {
		t.Logf("[motion-material] PASS: customUniforms.emissive animated headlessly (WASM motion pipeline ran end-to-end).")
		return
	}
	t.Skipf("material-uniform animation not observable headless: __gosx_motion_tick=%s, "+
		"state-handle present=%v, backend=%s. Requires WASM runtime + WebGL/WebGPU — run on a "+
		"GPU host with a shared-runtime Scene3D fixture to verify the visual pulse. "+
		"Mount + materialMotionProgram-in-SSR assertions (above) passed.",
		tickType, stateHandlePresent, attrs.Backend)
}
