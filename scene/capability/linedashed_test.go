package capability

import (
	"strings"
	"testing"
)

// canvasRendererPath is the Canvas 2D fallback renderer. It is the only backend
// that draws a dash pattern, which is why the line-dashed row needed correcting.
const canvasRendererPath = "../../client/js/bootstrap-src/18-scene-canvas.ts"

// mountBackendPath owns the runtime backend choice. It matters here because the
// runtime already knew that dashed lines need WebGL2 or Canvas2D, and the wrong
// Matrix cell overrode that knowledge.
const mountBackendPath = "../../client/js/bootstrap-src/20a-scene-mount-backend.js"

// TestLineDashedWasTrueOnTheWrongBackend corroborates all three cells of the
// corrected row.
//
// WHY this test exists. The row read {webgpu:false, webgl:true} and no WebGL2
// code backed the WebGL2 half. Four oracles agreed with each other and all four
// were wrong:
//
//   - Matrix said webgl:true.
//   - 16-scene-webgl.capabilities.json said "line-dashed": true.
//   - drift_test.go asserted Matrix equals the manifest.
//   - scene/capability_features_test.go raised the feature and never asked which
//     backend could draw it.
//
// The counts settle it. A dash pattern needs an arc-length test in a fragment
// shader or a setLineDash call on a 2D context. Neither exists in the WebGL2
// renderer:
//
//	16-scene-webgl.js       "dash" appears 0 times, case-insensitive
//	16a-scene-webgpu.js     "lineDash" appears 3 times, all inside
//	                        webGPUUnsupportedLineStyles, which REFUSES the draw
//	18-scene-canvas.ts      "dash" appears 16 times, and one of them is
//	                        ctx2d.setLineDash([dashSize, gapSize])
//
// So the true cell belonged to Canvas2D, and it sat on WebGL2 instead.
func TestLineDashedWasTrueOnTheWrongBackend(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)
	webgpu := readRenderer(t, webgpuRendererPath)
	canvas := readRenderer(t, canvasRendererPath)

	// The corrected WebGL2 half. WebGL2 draws a dashed line as a solid line.
	if Matrix[FeatureLineDashed][BackendWebGL] {
		t.Fatal("the WebGL2 line-dashed cell must stay false: the WebGL2 renderer contains no dash code at all")
	}
	if strings.Contains(strings.ToLower(webgl), "dash") {
		t.Error("16-scene-webgl.js now mentions dash; if WebGL2 gained a dash pattern, " +
			"flip the cell, fix the manifest and rewrite this test")
	}

	// The WebGPU half was already false, and for a stronger reason: WebGPU does
	// not draw the line at all. webGPUUnsupportedLineStyles gates hasWorldLineData,
	// so a dashed segment contributes no geometry.
	if Matrix[FeatureLineDashed][BackendWebGPU] {
		t.Fatal("the WebGPU line-dashed cell must stay false: the renderer refuses the draw")
	}
	for _, symbol := range []string{
		"function webGPUUnsupportedLineStyles(bundle) {",
		"if (line.lineDash) return true;",
		"!webGPUUnsupportedLineStyles(bundle) &&",
	} {
		if !strings.Contains(webgpu, symbol) {
			t.Errorf("expected %q in %s: WebGPU refusing dashed lines is why its cell is false",
				symbol, webgpuRendererPath)
		}
	}

	// The one true cell. Canvas2D draws the dashes.
	if !Matrix[FeatureLineDashed][BackendCanvas2D] {
		t.Fatal("the Canvas2D line-dashed cell must stay true: 18-scene-canvas.ts calls setLineDash")
	}
	for _, symbol := range []string{
		"function createSceneCanvasLineBatch(ctx2d) {",
		"ctx2d.setLineDash(dashed ? [dashSize, gapSize] : []);",
		"const dashes = bundle && bundle.worldLineDashes;",
		"const dashSizes = bundle && bundle.worldLineDashSizes;",
		"const gapSizes = bundle && bundle.worldLineGapSizes;",
	} {
		if !strings.Contains(canvas, symbol) {
			t.Errorf("Matrix[line-dashed][canvas2d] is true but %q is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, canvasRendererPath)
		}
	}
}

// TestLineDashedRuntimeAlreadyKnew records the second half of the defect, which
// is worse than one wrong boolean.
//
// sceneWebGPUFeatureGap in 20a-scene-mount-backend.js returns "line-styles" for a
// dashed scene, and sceneNeedsWebGLForWebGPUCoverage turns that into a WebGL2
// route. So the runtime carried the right answer. But that function returns EARLY
// when the scene ships a backendCaps verdict:
//
//	if (bc && Array.isArray(bc.capable)) { return ...webgpu present ? "" : ... }
//
// The wrong cell kept WebGPU in Capable, chooseSceneBackend prefers WebGPU first,
// and the runtime's own dash detection never ran. So a page authored before the
// honesty gate drew dashes and the same page after it drew nothing.
//
// The corrected row removes WebGPU's claim and gives Canvas2D the real one, so
// the verdict and the runtime now agree.
func TestLineDashedRuntimeAlreadyKnew(t *testing.T) {
	mount := readRenderer(t, mountBackendPath)
	for _, symbol := range []string{
		"function sceneWebGPUUnsupportedLineStyle(entry) {",
		`materialKind === "line-dashed" ||`,
		`if (sceneWebGPUUnsupportedLineBundle(root)) { return "line-styles"; }`,
		"function sceneNeedsWebGLForWebGPUCoverage(source) {",
	} {
		if !strings.Contains(mount, symbol) {
			t.Errorf("expected %q in %s: the runtime's own dash detection is why the wrong cell mattered",
				symbol, mountBackendPath)
		}
	}
	// The early return that let the wrong verdict win. If it disappears, the two
	// paths no longer overlap and the reasoning above needs re-reading.
	if !strings.Contains(mount, `? "" : "backendcaps-excluded";`) {
		t.Error("sceneWebGPUFeatureGap no longer short-circuits on a present verdict; " +
			"re-read how a dashed scene now picks its backend")
	}
}

// TestLineDashedDegradesRatherThanExcludes decides the policy half of the
// correction.
//
// The rule: a feature is required only when its absence produces a DIFFERENT
// scene, not a degraded image of the same scene. A solid line occupies the same
// pixels along the same path as the dashed line the author asked for. The
// geometry is present and correctly placed; only the stroke pattern is wrong. So
// it degrades.
//
// This matters more than usual now. The cell went false on BOTH GPU backends. Had
// line-dashed been required, Verdict would exclude WebGPU and WebGL2 and leave
// Capable holding Canvas2D alone — so one dashed helper line would drop a whole
// 3D scene to a 2D line drawing. Requiring it would have been the worst possible
// reading of the correction.
func TestLineDashedDegradesRatherThanExcludes(t *testing.T) {
	if DefaultPolicy().Required[FeatureLineDashed] {
		t.Fatal("line-dashed must not be required: it is false on both GPU backends, so requiring it " +
			"would drop every dashed scene to Canvas2D")
	}

	caps := Verdict([]Feature{FeatureLineDashed}, nil, DefaultPolicy())
	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	for _, b := range []Backend{BackendWebGPU, BackendWebGL, BackendCanvas2D} {
		if !capable[b] {
			t.Errorf("%s must stay capable for a droppable feature; Capable=%v", b, caps.Capable)
		}
	}
	for _, b := range []Backend{BackendWebGPU, BackendWebGL} {
		if got := caps.Degraded[b]; len(got) != 1 || got[0] != FeatureLineDashed {
			t.Errorf("%s must be degraded by line-dashed alone; Degraded=%v", b, caps.Degraded)
		}
	}
	// Canvas2D draws the dashes, so it must NOT appear under Degraded. That is the
	// visible payoff of giving it a real row instead of a blanket exclusion.
	if got, ok := caps.Degraded[BackendCanvas2D]; ok {
		t.Errorf("Canvas2D draws the dashes and must not be listed as degraded; got %v", got)
	}
	for _, reason := range caps.Reasons {
		if reason.Excludes != "" {
			t.Errorf("line-dashed is droppable and must exclude nobody; got %+v", reason)
		}
	}
}
