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
// THE SKIP USED TO BE THE FAILURE PATH. The test ended with:
//
//	if emissiveChanged { return }
//	t.Skipf("material-uniform animation not observable headless: ...")
//
// So a real regression in the WebAssembly motion pipeline and "headless cannot
// observe this" produced the SAME green skip. The one condition the test exists
// to check had no failing outcome at all.
//
// The two conditions are now separate, and each has its own failing outcome:
//
//  1. Observability. The test reads the seam and decides whether this harness CAN
//     see a uniform animate. Exactly one reason may make it unobservable: the
//     documented absence of the WASM motion tick on a stand-alone fixture. Any
//     other reason — a missing scene-state handle, a missing uniform record, a
//     GPU backend that ticks and still shows nothing — is a regression and FAILS.
//  2. Animation. When the harness CAN observe, the uniform MUST change, and the
//     test fails if it does not.
//
// So the unobservable branch now asserts its own precondition instead of assuming
// it, and it fails the moment the reason changes.
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

	// (b) Read the WASM motion seam state. __gosx_motion_tick is expected absent
	// on a stand-alone declarative Scene3D, and step (c) decides what that means.
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

	// (c) OBSERVABILITY FIRST. Decide whether this harness can see the uniform
	// change at all, and fail if it cannot for any reason other than the one
	// documented at the top of this file.
	//
	// The scene-state handle is NOT part of that limitation. The mount publishes
	// __gosxScene3DState for every backend, canvas2d included, and the seam writes
	// customUniforms through it. So a missing handle means the mount stopped
	// exposing scene state, which is a regression whatever the backend is.
	if !stateHandlePresent {
		t.Fatalf("the scene-state handle or the \"glow-cube\" record is missing at t1=%v t2=%v.\n"+
			"__gosxScene3DState.objects is how the motion seam reaches customUniforms, and the mount "+
			"publishes it on every backend including canvas2d. Its absence is a mount regression, "+
			"NOT the documented headless GPU limitation.\n\nbackend=%s tick=%s\n\nConsole:\n%s\n\nLogs:\n%s",
			t1.Found, t2.Found, attrs.Backend, tickType, page.Console(), app.logs.String())
	}

	// The tick export is the one thing a stand-alone fixture is allowed to lack.
	tickPresent := tickType == "function"
	if !tickPresent && tickType != "undefined" {
		t.Fatalf("__gosx_motion_tick is %q. It must be either \"function\" (the WASM runtime loaded, "+
			"so the animation is observable and asserted below) or \"undefined\" (the documented "+
			"stand-alone case). Any other value means the seam is half-installed.\n\nConsole:\n%s",
			tickType, page.Console())
	}

	// (d) ANIMATION. With the tick installed the pipeline is fully observable, so
	// a uniform that does not move is a failure, not a skip.
	if tickPresent {
		if !emissiveChanged {
			t.Fatalf("__gosx_motion_tick is installed and the scene-state handle is present, so the "+
				"material-uniform animation IS observable here — but customUniforms.emissive did not "+
				"change over 1s (t1=%s t2=%s).\n"+
				"The WASM motion pipeline ran and produced no movement. That is a regression in "+
				"motion.Eval, in the materialMotionProgram lowering, or in the seam that writes the "+
				"uniform back.\n\nbackend=%s\n\nConsole:\n%s\n\nLogs:\n%s",
				t1JSON, t2JSON, attrs.Backend, page.Console(), app.logs.String())
		}
		t.Logf("[motion-material] PASS: customUniforms.emissive animated (WASM motion pipeline ran end-to-end).")
		return
	}

	// (e) The documented unobservable case, now with its precondition ASSERTED
	// rather than assumed. The uniform must ALSO be unchanged: a value that moved
	// with no tick installed would mean something else is writing it, and the
	// reasoning above would be wrong.
	if emissiveChanged {
		t.Fatalf("customUniforms.emissive changed from %s to %s with __gosx_motion_tick undefined.\n"+
			"Nothing should drive the material program without the WASM tick, so either the runtime "+
			"now loads on a stand-alone fixture — in which case delete this branch and let the "+
			"assertion above cover it — or a second writer is touching the uniform.",
			t1JSON, t2JSON)
	}
	t.Logf("[motion-material] the WASM motion tick is absent on this stand-alone fixture "+
		"(tick=%s, backend=%s), which is the documented case: the runtime loads only for "+
		"shared-runtime/island scenes. Every headless-observable claim above was asserted, and "+
		"the uniform correctly did not move. Run a shared-runtime Scene3D fixture on a GPU host "+
		"to exercise the visual pulse.", tickType, attrs.Backend)
}
