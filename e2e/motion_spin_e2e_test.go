//go:build e2e

// Port of the retired e2e/motion-spin.test.mjs (playwright) to chromedp.
//
// WASM-driven Scene3D motion e2e test. Navigates to the hidden spinning-box
// fixture served by gosx-docs at /test/motion-spin. That route ships a single
// mesh with a non-zero Spin, which the scene IR lowers into a motionProgram
// (a binary motion.Timeline). The test sets window.__gosx_motion_wasm = true
// BEFORE navigation (init script) so the JS runtime routes that motionProgram
// through the WASM exports __gosx_motion_load / __gosx_motion_tick.
//
// The test then verifies the scene actually animates: it screenshots the
// canvas, waits ~1.2s, screenshots again, and asserts the two frames differ
// (the spinning mesh moved, so pixels changed). This is the core
// visual-verify assertion and is renderer-agnostic.
//
// Backend reality under headless Chrome: the reported backend is "canvas"
// under headless CI, "webgl" on real-GPU hardware, and "webgpu" where
// navigator.gpu is present. The test records and accepts any of them.
//
// WASM motion seam: window.__gosx_motion_tick is only registered once the
// shared Go WASM runtime is loaded, which a stand-alone declarative Scene3D
// does not trigger — so on this fixture the spin is computed by the JS
// fall-through of the seam. The test records whether the WASM exports were
// present but does not hard-fail on their absence; the animation itself is
// the contract under verification.
package e2e

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func motionSpinBaseURL() string {
	if url := os.Getenv("GOSX_MOTION_E2E_BASE_URL"); url != "" {
		return url
	}
	// Distinct port so this suite can run alongside the other e2e suites.
	return "http://127.0.0.1:3072"
}

func TestMotionSpinAnimates(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, motionSpinBaseURL())
	// Route the scene's motionProgram through the WASM motion exports
	// (__gosx_motion_load / __gosx_motion_tick) instead of the inert JS
	// fall-through. Must run BEFORE any page script, hence the init script.
	page := newBrowserPage(t, chrome, nil, 1280, 800, "window.__gosx_motion_wasm = true;", 120*time.Second)

	fixturePath := "/test/motion-spin"
	if status := page.navigate(t, app.baseURL+fixturePath); status < 200 || status > 299 {
		t.Fatalf("fixture page returned %d\n\nLogs:\n%s", status, app.logs.String())
	}

	// Wait for the Scene3D mount to finish initialising.
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

	// Backend is environment-dependent: "canvas" under headless SwiftShader,
	// "webgl" on real-GPU hardware, "webgpu" where navigator.gpu exists.
	// Accept any real backend and record which one rendered.
	if !containsString([]string{"webgl", "webgpu", "canvas2d", "canvas"}, attrs.Backend) {
		t.Fatalf("expected backend in {webgl,webgpu,canvas2d,canvas}, got %q\n\nConsole:\n%s\n\nLogs:\n%s",
			attrs.Backend, page.Console(), app.logs.String())
	}
	t.Logf("[motion-spin] Scene3D backend = %s", attrs.Backend)

	// Exercise the WASM motion seam and record its state without hard-failing
	// on the WASM exports — the animation assertion below is the contract.
	var wasmFlag bool
	page.eval(t, `window.__gosx_motion_wasm === true`, &wasmFlag)
	if !wasmFlag {
		t.Fatalf("expected window.__gosx_motion_wasm === true (init script)\n\nConsole:\n%s", page.Console())
	}
	var tickType string
	page.eval(t, `typeof window.__gosx_motion_tick`, &tickType)
	if tickType == "function" {
		t.Logf("[motion-spin] WASM motion seam: __gosx_motion_tick=function (motion driven by motion.Eval in WASM)")
	} else {
		t.Logf("[motion-spin] WASM motion seam: __gosx_motion_tick=%s (motion computed by JS fall-through)", tickType)
	}

	canvasSelector := "canvas[data-gosx-scene3d-canvas]"
	page.waitFor(t, `(() => {
    const canvas = document.querySelector("canvas[data-gosx-scene3d-canvas]");
    if (!canvas) return false;
    const rect = canvas.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  })()`, 30*time.Second, "visible "+canvasSelector)

	// Pixel-diff over time: a spinning Responsive scene drives its own rAF
	// loop, so the box rotation should change pixels between two screenshots
	// taken ~1.2s apart.
	buf1 := page.screenshotElement(t, canvasSelector)
	time.Sleep(1200 * time.Millisecond)
	buf2 := page.screenshotElement(t, canvasSelector)

	if len(buf1) == 0 || len(buf2) == 0 {
		t.Fatalf("canvas screenshots were empty (buf1=%d buf2=%d)\n\nConsole:\n%s\n\nLogs:\n%s",
			len(buf1), len(buf2), page.Console(), app.logs.String())
	}
	if bytes.Equal(buf1, buf2) {
		t.Fatalf("expected canvas pixels to change between frames (spinning mesh should animate); "+
			"they were identical (buf1=%dB buf2=%dB, backend=%s)\n\nConsole:\n%s\n\nLogs:\n%s",
			len(buf1), len(buf2), attrs.Backend, page.Console(), app.logs.String())
	}
}
