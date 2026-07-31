package capability

import (
	"testing"
)

// These tests corroborate the wireframe row against renderer source.
//
// readRenderer, webgpuRendererPath and webglRendererPath come from
// lights_test.go.

// TestWebGPUDrawsWireframe corroborates the true cell.
//
// A PASS PROVES: vertexMain writes a barycentric coordinate from
// vertex_index % 3, fragmentMain gates a discard on the flag lane
// (material.modelScaleSigns.w — see the WGSL_MATERIAL_STRUCT comment for why
// wireframe reuses that lane instead of adding one), and the discard test
// itself is present.
//
// A PASS DOES NOT PROVE: that WebGL2 draws the same feature. It does not; see
// TestWebGL2DrawsNoWireframe.
func TestWebGPUDrawsWireframe(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	evidence := evidenceFor(t, FeatureWireframe, BackendWebGPU).
		needs(webgpuRendererPath, webgpu,
			"vertexIndex % 3u",
			"material.modelScaleSigns.w > 0.5",
			"discard;",
		)
	evidence.assertAgrees("vertexMain derives a per-corner barycentric coordinate from vertex_index, and " +
		"fragmentMain discards fragments away from a triangle edge when the wireframe flag is set. No index " +
		"buffer and no second pipeline are needed because every mesh path draws non-indexed triangle soup.")
}

// TestWebGL2DrawsNoWireframe corroborates the false cell.
//
// WHEN THIS TEST FAILS because the renderer gained the feature: flip the
// WebGL2 cell to true, update 16-scene-webgl.capabilities.json in the same
// change, and rewrite this test to corroborate the new answer with the
// symbols that landed. Do not weaken the guard to keep it green.
func TestWebGL2DrawsNoWireframe(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)
	evidence := evidenceFor(t, FeatureWireframe, BackendWebGL).
		refutedBy(webglRendererPath, webgl, "wireframe")
	evidence.assertAgrees("\"wireframe\" appears nowhere in the WebGL2 renderer. gl_VertexID exists in GLSL ES " +
		"3.00, so the same barycentric trick WebGPU uses is available, but it is WebGL2 follow-on work per the " +
		"WebGPU-first constraint and is not implemented.")
}

// TestWireframeDroppableNotRequired pins that the row reports and does not
// gate, matching FeatureEnvironmentMap's reasoning
// (environmentmap_test.go's TestEnvironmentMapExcludesNoBackend).
func TestWireframeDroppableNotRequired(t *testing.T) {
	if DefaultPolicy().Required[FeatureWireframe] {
		t.Fatal("wireframe entered DefaultPolicy().Required. A required feature EXCLUDES every backend whose " +
			"cell is false, so an authored wireframe material would push every WebGL2-only viewer to a blank " +
			"canvas over a fill-versus-edge difference. Remove it, or state why a blank frame beats a filled one.")
	}
	caps := Verdict([]Feature{FeatureWireframe}, nil, DefaultPolicy())
	var sawWebGPU, degraded bool
	for _, backend := range caps.Capable {
		if backend == BackendWebGPU {
			sawWebGPU = true
		}
	}
	for _, feature := range caps.Degraded[BackendWebGL] {
		if feature == FeatureWireframe {
			degraded = true
		}
	}
	if !sawWebGPU {
		t.Fatalf("WebGPU dropped out of Capable for a scene whose only feature is wireframe; got %v", caps.Capable)
	}
	if !degraded {
		t.Fatalf("WebGL2 is capable but reports no wireframe degradation; the author gets no warning. Degraded[webgl]=%v",
			caps.Degraded[BackendWebGL])
	}
}

// The explicit-authoring guard — raising FeatureWireframe only for an
// explicit authored Wireframe on a solid mesh material, never from the
// legacy default-true flat-object path — lives in collectFeatures
// (scene/scene_ir.go), which imports this package; a test here would cycle.
// TestCollectFeaturesRaisesWireframeOnlyWhenAuthored in
// scene/scene_ir_test.go covers it, including the legacy-path trap R4 in the
// cluster spec names.
