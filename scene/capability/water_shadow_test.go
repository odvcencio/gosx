package capability

import (
	"strings"
	"testing"
)

// webglWaterShadowPath and webgpuWaterShadowPath name the two renderers that
// own the water shadow passes. webglRendererPath and webgpuRendererPath come
// from lights_test.go, and readRenderer reads either one.
const (
	webglWaterShadowPath  = webglRendererPath
	webgpuWaterShadowPath = webgpuRendererPath
)

// TestWaterObjectMeshShadowPassIsWebGPUOnly corroborates BOTH halves of the
// water-object-mesh-shadow-pass row against renderer source.
//
// WHY this test exists. The cell read {webgpu:true, webgl:true} and no WebGL2
// code backed the WebGL2 half. Three oracles agreed with each other and all
// three were wrong:
//
//   - Matrix said true.
//   - 16-scene-webgl.capabilities.json said true.
//   - runtime.test.js asserted the manifest said true.
//   - drift_test.go asserted Matrix equals the manifest.
//
// The drift guard cannot catch this, because both sides of its comparison are
// hand-written by the same person in the same commit. Only renderer source
// settles the question. lights_test.go states the same limitation, and
// gpupicking_test.go exists because that exact failure already happened once.
//
// The distinction the feature name draws is real. A mesh shadow rasterizes the
// caster's own geometry. An analytic primitive shadow evaluates a closed form
// over a full-screen draw. The two produce different images, so a backend that
// does only the second must not claim the first.
func TestWaterObjectMeshShadowPassIsWebGPUOnly(t *testing.T) {
	webgl := readRenderer(t, webglWaterShadowPath)
	webgpu := readRenderer(t, webgpuWaterShadowPath)

	// The TRUE half. WebGPU must rasterize real geometry.
	if !Matrix[FeatureWaterObjectMeshShadowPass][BackendWebGPU] {
		t.Fatal("Matrix must keep the WebGPU mesh-shadow cell true; this test corroborates it")
	}
	for _, symbol := range []string{
		"renderWaterObjectMeshShadowPass",
		"WGPU_PBR_VERTEX_LAYOUT",
	} {
		if !strings.Contains(webgpu, symbol) {
			t.Errorf("16a-scene-webgpu.js must contain %q for the WebGPU mesh-shadow cell to be true", symbol)
		}
	}

	// The FALSE half. A false cell is a claim too, so corroborate it.
	if Matrix[FeatureWaterObjectMeshShadowPass][BackendWebGL] {
		t.Fatal("the WebGL2 mesh-shadow cell must stay false: the WebGL2 water renderer rasterizes no caster geometry")
	}
	if strings.Contains(webgl, "objectMeshShadow") {
		t.Error("16-scene-webgl.js now mentions objectMeshShadow; if it rasterizes caster geometry, flip the cell and rewrite this test")
	}

	// And pin WHY it is false, so a reader does not have to rediscover it. Both
	// WebGL2 shadow programs draw one full-screen triangle over an empty vertex
	// array object. A mesh pass would bind vertex buffers instead.
	for _, marker := range []string{
		"bindVertexArray(emptyVAO)",
		"drawArrays(gl.TRIANGLES, 0, 3)",
	} {
		if !strings.Contains(webgl, marker) {
			t.Errorf("expected %q in 16-scene-webgl.js: the WebGL2 shadow passes are full-screen analytic draws, and this test documents that", marker)
		}
	}
}

// TestWaterObjectMeshShadowPassDegradesRatherThanExcludes pins the policy half
// of the correction.
//
// The feature used to sit in DefaultPolicy().Required. Once the WebGL2 cell went
// false, leaving it required would exclude WebGL2 from every scene that uses the
// pass, and Verdict returns an EMPTY Capable list when a required feature
// excludes every candidate backend. A browser without WebGPU would then have no
// backend at all for a scene WebGL2 draws.
//
// WebGL2 runs the simulation, the caustics and the object texture pass, and it
// shades the object shadow from an analytic primitive. Only the shadow shape is
// wrong. A differently shaped shadow beats a blank canvas.
func TestWaterObjectMeshShadowPassDegradesRatherThanExcludes(t *testing.T) {
	if DefaultPolicy().Required[FeatureWaterObjectMeshShadowPass] {
		t.Fatal("water-object-mesh-shadow-pass must not be required: it would exclude WebGL2 and leave a non-WebGPU browser with no backend")
	}

	caps := Verdict([]Feature{FeatureWaterObjectMeshShadowPass}, nil, DefaultPolicy())

	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	if !capable[BackendWebGPU] {
		t.Errorf("WebGPU must stay capable; Capable=%v", caps.Capable)
	}
	if !capable[BackendWebGL] {
		t.Errorf("WebGL2 must stay capable and merely degraded; Capable=%v", caps.Capable)
	}
	if got := caps.Degraded[BackendWebGL]; len(got) != 1 || got[0] != FeatureWaterObjectMeshShadowPass {
		t.Errorf("WebGL2 must be degraded by the mesh-shadow pass alone; Degraded=%v", caps.Degraded)
	}
	if got := caps.Degraded[BackendWebGPU]; len(got) != 0 {
		t.Errorf("WebGPU must not be degraded; Degraded=%v", got)
	}
}

// TestWaterSimAndTexturePassStayTrueOnWebGL guards the two water cells that ARE
// honest, so the correction above does not spread by imitation.
//
// I verified these against the same renderer before changing anything:
// createSceneWaterSimWebGL exists, the caustics program exists, and the object
// texture and material paths carry many references. Only the mesh-shadow claim
// was empty.
func TestWaterSimAndTexturePassStayTrueOnWebGL(t *testing.T) {
	webgl := readRenderer(t, webglWaterShadowPath)

	if !Matrix[FeatureWaterSim][BackendWebGL] {
		t.Error("water-simulation must stay true on WebGL2: createSceneWaterSimWebGL implements it")
	}
	if !Matrix[FeatureWaterObjectTexturePass][BackendWebGL] {
		t.Error("water-object-texture-pass must stay true on WebGL2")
	}
	for _, symbol := range []string{
		"createSceneWaterSimWebGL",
		"createSceneWaterRendererWebGL",
		"causticsProgram",
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("16-scene-webgl.js must contain %q; the two true water cells rest on it", symbol)
		}
	}

	// Both remain required. They exclude rather than degrade, because a scene
	// that asks for a water simulation and gets none is not a degraded image of
	// that scene; it is a different scene.
	pol := DefaultPolicy()
	if !pol.Required[FeatureWaterSim] || !pol.Required[FeatureWaterObjectTexturePass] {
		t.Error("water-simulation and water-object-texture-pass must stay required")
	}
}

// TestIBLIsFalseOnBothBackends pins the intentionally staged capability cell.
//
// Both renderers now consume assetpipe's radiance cube, irradiance cube and
// split-sum BRDF LUT. The matrix remains false until that path is universally
// available on both backends: WebGL2 must compile a bounded legacy variant on
// the spec-minimum 16-fragment-sampler devices because the full material + CSM
// + IBL layout needs 18. Claiming the feature at the matrix level would hide
// that deterministic degradation.
func TestIBLIsFalseOnBothBackends(t *testing.T) {
	for _, b := range []Backend{BackendWebGPU, BackendWebGL} {
		if Matrix[FeatureIBL][b] {
			t.Errorf("the ibl cell for %s must be false until a renderer consumes the prefiltered products", b)
		}
	}

	webgl := readRenderer(t, webglWaterShadowPath)
	webgpu := readRenderer(t, webgpuWaterShadowPath)

	// Real split-sum consumption exists on both backends. These are positive
	// evidence, not a reason to flip the unconditional capability cell.
	for _, marker := range []string{
		"u_iblIrradiance",
		"u_iblRadiance",
		"u_iblBRDFLUT",
		"textureLod(u_iblRadiance",
		"prefiltered * (F0 * brdf.x + brdf.y)",
		"scenePBRLinearHDRPixels",
	} {
		if !strings.Contains(webgl, marker) {
			t.Errorf("expected real WebGL2 IBL marker %q in 16-scene-webgl.js", marker)
		}
	}
	for _, marker := range []string{
		"iblIrradiance: texture_cube<f32>",
		"iblRadiance: texture_cube<f32>",
		"iblBRDFLUT: texture_2d<f32>",
		"textureSampleLevel(iblRadiance",
		"prefiltered * (F0 * brdf.x + brdf.y)",
	} {
		if !strings.Contains(webgpu, marker) {
			t.Errorf("expected real WebGPU IBL marker %q in 16a-scene-webgpu.js", marker)
		}
	}

	// This explicit resource gate is why FeatureIBL stays false despite the real
	// capable-device path above. Keep the matrix and diagnostic coupled.
	for _, marker := range []string{"maxUnits >= 18", "fragment-texture-units<18"} {
		if !strings.Contains(webgl, marker) {
			t.Errorf("expected staged WebGL2 IBL resource gate %q", marker)
		}
	}

	// ibl stays droppable. A scene with an environment map still renders without
	// it; the author sees the gap under Degraded.
	if DefaultPolicy().Required[FeatureIBL] {
		t.Error("ibl must not be required: a scene without IBL still renders, so it degrades")
	}
}
