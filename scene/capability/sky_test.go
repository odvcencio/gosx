package capability

import (
	"testing"
)

// These tests corroborate the sky-environment row against renderer source.
//
// The row tracks only Sky.Mode == "environment": a ray-sampled cube or
// equirect background. Gradient sky (the other mode) draws on every backend,
// including Canvas2D's blanket exclusion (a genuine ctx2d linear-gradient
// fill), so it earns no Matrix row at all — an absent feature is supported
// everywhere, per the Matrix contract capability.go states at the top of
// Matrix's doc comment.
//
// Both cells start false: PR-3 (this commit) wires the authoring surface and
// the wire IR only. No renderer draws a sky yet. WHEN A DRAW LANDS: flip the
// cell, add the pipeline symbols to the needs() list below, update the
// manifest in the same commit, and change refutedBy to needs for that
// backend. TestDriftGuard forces the cell and manifest to travel together.

// TestNoBackendDrawsSkyYet corroborates both false cells with the absence of
// the pipeline symbols the sky draw will introduce, per the design in
// specs/scene3d-parity/cluster-a-environment.md section 3.1.3 (WebGPU) and
// 3.1.5 (WebGL2): a dedicated pipeline/program with its own bind group or
// uniform set, not a branch inside the existing PBR fragment shader.
func TestNoBackendDrawsSkyYet(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	webgl := readRenderer(t, webglRendererPath)

	evidenceFor(t, FeatureSkyEnvironment, BackendWebGPU).
		refutedBy(webgpuRendererPath, webgpu, "WGSL_SKY_FRAGMENT", "wgpuCreateSkyPipeline").
		assertAgrees("no WebGPU sky pipeline exists yet; PR-3 wires only the authoring surface and the wire IR")

	evidenceFor(t, FeatureSkyEnvironment, BackendWebGL).
		refutedBy(webglRendererPath, webgl, "SCENE_SKY_FRAGMENT_SOURCE", "sceneCreateSkyProgram").
		assertAgrees("no WebGL2 sky program exists yet; PR-3 wires only the authoring surface and the wire IR")
}

// TestSkyEnvironmentExcludesNoBackend pins that the row, once it exists, will
// report and not gate: a missing sky degrades the image (the gradient/flat
// degrade target), it never blanks the canvas.
func TestSkyEnvironmentExcludesNoBackend(t *testing.T) {
	if DefaultPolicy().Required[FeatureSkyEnvironment] {
		t.Fatal("sky-environment entered DefaultPolicy().Required. A required feature EXCLUDES every " +
			"backend whose cell is false, so an authored environment sky would push every viewer to a " +
			"blank canvas over a background term. Remove it, or state why a blank frame beats a degraded one.")
	}
}

// TestSkyEnvironmentHasNoCanvas2DRow pins the blanket-exclusion reasoning: the
// row carries no BackendCanvas2D key, so Canvas2D reads unsupported by the
// zero value, matching every other feature that needs more than the two
// primitives Canvas2D draws (see capability.go's CANVAS2D BLANKET RULE).
func TestSkyEnvironmentHasNoCanvas2DRow(t *testing.T) {
	if Supports(BackendCanvas2D, FeatureSkyEnvironment) {
		t.Fatal("Canvas2D must not claim sky-environment: it rasterizes no triangle and cannot ray-sample " +
			"a cube or an equirect texture. Environment-mode sky degrades to a flat HorizonColor fill there.")
	}
}
