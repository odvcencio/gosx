//go:build e2e

// Pixel-parity snapshot harness, built while investigating a restructure of
// the water surface shaders (surface.sel / surface-below.sel: hoisted,
// always-evaluated object/wall/sky shading -> gated behind the hit tests).
// That restructure was NOT shipped -- see the PERF INVESTIGATION note in
// water-surface.sel's header for why -- but this harness is kept as the
// reusable machinery for re-attempting that investigation (ideally on real
// GPU hardware/a real WebGPU backend rather than the software GLES2 path
// available in this sandbox; see the finding below).
//
// This test does NOT compare before/after itself -- it takes ONE deterministic
// screenshot of the water demo's canvas and writes it to
// GOSX_WATER_PARITY_OUT. Drive it by hand: swap the .sel source(s) under
// test on disk (each `go run ./cmd/gosx dev` picks up the current file
// content via go:embed, since it recompiles per invocation), invoke this
// test once per variant with a different GOSX_WATER_PARITY_OUT path, then
// diff the resulting PNGs (github.com/orisano/pixelmatch is a dependency;
// pixelDiffPNGBytes/pixelDiffImages in water_pixel_parity_diff_test.go do
// this, or write a small standalone comparator).
//
// Determinism knobs this test drives on its own (no page.gsx changes
// needed): it pauses the sim via the `paused` checkbox (freezes the
// height-field state buffer, so vertex displacement and every stateAt()
// hit-test in the fragment shader goes static) as early as possible after
// mount to minimize -- not eliminate -- the race against the first
// requestAnimationFrame-driven seed/settle step.
//
// Determinism knobs this test CANNOT drive itself (page.gsx must be
// temporarily edited for a fully reproducible run, then reverted):
//   - seedSalt: entry.seedSalt has no live control; without a fixed
//     `seedSalt={N}` prop on <WaterSystem>, each process launch seeds the
//     initial ripple pattern from Math.random(), so two separate `go run`
//     launches freeze a DIFFERENT height field even with pause clicked
//     immediately.
//   - caustics: caustics.sel's shimmer term is driven by a wall-clock `time`
//     uniform that keeps advancing every ~2 frames even while paused
//     (renderWaterCausticsPass runs unconditionally in
//     16a-scene-webgpu.js/16-scene-webgl.js), so causticTexture content
//     drifts between separate process launches regardless of pause. Setting
//     `caustics={false}` avoids the drift, though on the GLES2/WebGL2
//     backend it trades it for an uninitialized-texture read (texImage2D
//     with a null source is NOT zero-init per the WebGL/GL spec, unlike
//     WebGPU's mandatory zero-init) -- in practice this measured as SMALLER
//     noise than the caustics-on time drift in this sandbox, but it is its
//     own source of run-to-run variance, not a clean fix.
//
// FINDING (read before trusting a pixelmatch result from this harness): in
// this sandboxed environment, the ANGLE/SwiftShader software GLES2/WebGL2
// backend that gosx's own e2e CI exercises by default has substantial
// INHERENT run-to-run rendering noise -- diffing the SAME, byte-identical
// shader against itself across two separate `go run` launches (fixed
// seedSalt, paused from mount, caustics either on or off) measured max
// per-channel deltas from 2 up to 7-8 (of 255) and 8-20% of pixels with any
// delta, PURELY from environment variance (thread scheduling in
// SwiftShader's software rasterizer is the leading suspect;
// SWIFTSHADER_DISABLE_MULTITHREADING=1 reduced but did not eliminate it).
// That noise floor is comparable in magnitude to the signal a real shader
// change would produce, making this environment unsuitable for a rigorous
// "prove, don't assert" bit-parity verdict within a practical iteration
// budget. Any future use of this harness should either run on real hardware
// (a real GPU + a real browser, not a headless sandbox with a software
// rasterizer) or invest in averaging many runs / statistically
// characterizing the noise floor before trusting a single before/after
// diff.
package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
	"time"
)

func TestWaterPixelParitySnapshot(t *testing.T) {
	outPath := os.Getenv("GOSX_WATER_PARITY_OUT")
	if outPath == "" {
		t.Skip("GOSX_WATER_PARITY_OUT not set; this test is driven by the parity comparison script, not `go test` directly")
	}

	chrome := e2eChromePath(t)
	app := startDocsApp(t, waterBaseURL(t))
	page := newBrowserPage(t, chrome, waterBrowserFlags(), 1280, 800, "", 120*time.Second)

	if status := page.navigate(t, app.baseURL+"/demos/water"); status < 200 || status > 299 {
		t.Fatalf("/demos/water returned %d\n\nLogs:\n%s", status, app.logs.String())
	}
	page.waitFor(t, `!!document.querySelector("[data-gosx-scene3d-mounted][data-gosx-scene3d-water-renderer]")`,
		45*time.Second, "[data-gosx-scene3d-mounted][data-gosx-scene3d-water-renderer]")
	canvasSelector := "canvas[data-gosx-scene3d-canvas]"
	page.waitFor(t, `(() => {
    const canvas = document.querySelector("canvas[data-gosx-scene3d-canvas]");
    if (!canvas) return false;
    const rect = canvas.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  })()`, 30*time.Second, "visible "+canvasSelector)

	initial := takeWaterSnapshot(t, page)
	if initial.Renderer != "active" {
		t.Fatal(waterDiagnostics(page, app, "water renderer did not become active", initial))
	}

	// Pause as early as possible after mount to minimize (not eliminate) the
	// race against the sim's first seed/settle step -- see the file header:
	// without also pinning page.gsx's seedSalt prop, this alone is NOT
	// sufficient for cross-process determinism, only a best effort.
	setChecked(t, page, `input[name="paused"]`, true)
	page.waitFor(t, `document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-paused") === "true"`,
		15*time.Second, "paused attribute")

	// Settle: let any in-flight frame drain before capturing. The canvas's
	// own CSS layout also settles asynchronously after mount on an
	// unpredictable timescale (observed passing through more than one
	// transient rect -- e.g. a loading-state height before the container
	// corrects to its steady-state square aspect -- anywhere from under a
	// second to several seconds later), so rather than guess a wait budget,
	// the loop below directly verifies what it captured: take two
	// screenshots, and only accept them if they report the SAME dimensions
	// AND are pixel-identical (still paused, nothing should be moving).
	// Anything else (size drift, or a real per-frame difference) means the
	// page had not settled yet, and it retries.
	time.Sleep(2 * time.Second)
	var shot1, shot2 []byte
	var lastErr string
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		shot1 = page.screenshotElement(t, canvasSelector)
		time.Sleep(400 * time.Millisecond)
		shot2 = page.screenshotElement(t, canvasSelector)
		img1, err1 := decodePNGDims(shot1)
		img2, err2 := decodePNGDims(shot2)
		if err1 != nil || err2 != nil {
			lastErr = fmt.Sprintf("decode: %v / %v", err1, err2)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if img1 != img2 {
			lastErr = fmt.Sprintf("size drift %v -> %v (canvas layout still settling)", img1, img2)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		diff := pixelDiffPNGBytes(t, shot1, shot2)
		if diff.differing != 0 {
			lastErr = fmt.Sprintf("pixels changed while paused (differing=%d maxDelta=%d)", diff.differing, diff.maxDelta)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastErr = ""
		break
	}
	if lastErr != "" {
		t.Fatalf("harness never reached a stable, self-consistent frame (last attempt: %s)\n\n%s",
			lastErr, waterDiagnostics(page, app, "self-consistency", takeWaterSnapshot(t, page)))
	}

	if err := os.WriteFile(outPath, shot2, 0o644); err != nil {
		t.Fatalf("write snapshot %s: %v", outPath, err)
	}
	t.Logf("wrote %s (%d bytes), backend=%s", outPath, len(shot2), initial.Backend)
}

func decodePNGDims(b []byte) (image.Point, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return image.Point{}, err
	}
	return image.Point{X: cfg.Width, Y: cfg.Height}, nil
}
