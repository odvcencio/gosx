package capability

import (
	"strings"
	"testing"
)

// This file corroborates the water-simulation row in full.
//
// water_shadow_test.go already asserts that the cell stays true on WebGL2 and
// names three symbols. That was enough to stop the mesh-shadow correction from
// spreading by imitation, but it is not a corroboration: it never reads the
// WebGPU half, and it never shows that the WebGL2 path solves a height field
// rather than animating a canned normal map.
//
// water-simulation sits in DefaultPolicy().Required, so a false cell EXCLUDES a
// backend. Both cells therefore need real evidence.

// TestWaterSimWebGL2SolvesARealHeightField ties the true WebGL2 cell to the
// shipped simulation.
//
// A height-field water simulation needs three things, and WebGL2 has all three:
//
//  1. Float render targets. sceneWaterGLFloatCaps probes EXT_color_buffer_float
//     and the renderer builds RGBA32F targets, falling back to RGBA16F. An 8-bit
//     target cannot carry a height and a velocity.
//  2. Ping-pong state. Two targets, swapped per step, because a step reads the
//     previous state.
//  3. The five lowered stages. passSpecs names simulation, normal, seed, drop and
//     displacement, and each compiles the *FragmentGLES source the Go lowerer
//     produced. The renderer runs the author's shaders, not its own.
func TestWaterSimWebGL2SolvesARealHeightField(t *testing.T) {
	if !Matrix[FeatureWaterSim][BackendWebGL] {
		t.Fatal("Matrix says WebGL2 cannot simulate water; corroborate the new false cell instead")
	}
	webgl := readRenderer(t, webglRendererPath)

	for _, symbol := range []string{
		"function createSceneWaterSimWebGL(gl, entry) {",
		"function sceneWaterGLFloatCaps(gl) {",
		"if (!caps.colorBufferFloat && !caps.colorBufferHalfFloat) return null;",
		"formats.push({ internal: gl.RGBA32F, type: gl.FLOAT });",
		"formats.push({ internal: gl.RGBA16F, type: gl.HALF_FLOAT });",
		"states = [a, b];",
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("Matrix[water-simulation][webgl] is true but %q is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, webglRendererPath)
		}
	}

	// The five lowered stages. Each name must appear as a compiled pass, or the
	// WebGL2 path is animating something other than the authored simulation.
	for _, stage := range []string{"simulation", "normal", "seed", "drop", "displacement"} {
		want := stage + ": { vertex: entry."
		if !strings.Contains(webgl, want) {
			t.Errorf("the WebGL2 water sim must compile the %q pass from the lowered IR; %q is missing from %s",
				stage, want, webglRendererPath)
		}
	}

	// And the renderer must be reachable, or the definition is dead code and the
	// cell is a promise. The mount chunk resolves it.
	if !strings.Contains(webgl, "function createSceneWaterRendererWebGL(gl, canvas, entry) {") {
		t.Errorf("%s must define createSceneWaterRendererWebGL; the mount chunk resolves it by that name",
			webglRendererPath)
	}
	mountChunk := readRenderer(t, webglMountChunkPath)
	if !strings.Contains(mountChunk, "createSceneWaterRendererWebGL") {
		t.Errorf("%s must resolve createSceneWaterRendererWebGL, or the WebGL2 water renderer never runs",
			webglMountChunkPath)
	}
}

// TestWaterSimWebGPURunsTheSameFiveStages ties the true WebGPU cell to its own
// implementation, which is a compute pass rather than a fragment pass.
//
// The claim under test is parity of STAGES, not parity of mechanism. Both
// backends run simulation and normal every frame, and both run seed, drop and
// displacement on events. dispatchWaterComputeStage is the single dispatcher, so
// a lost stage shows up as a lost caller.
func TestWaterSimWebGPURunsTheSameFiveStages(t *testing.T) {
	if !Matrix[FeatureWaterSim][BackendWebGPU] {
		t.Fatal("Matrix says WebGPU cannot simulate water; corroborate the new false cell instead")
	}
	webgpu := readRenderer(t, webgpuRendererPath)
	for _, symbol := range []string{
		"function dispatchWaterComputeStage(encoder, system, entry, stage, fallbackPipeline, sharedPass) {",
		`encoder.beginComputePass({ label: "gosx-water-sim-normal-pass" })`,
		`dispatchWaterComputeStage(encoder, system, entry, "simulation", simulationCompute.pipeline, waterSimPass)`,
		`dispatchWaterComputeStage(encoder, system, entry, "normal", normalCompute.pipeline, waterSimPass)`,
	} {
		if !strings.Contains(webgpu, symbol) {
			t.Errorf("Matrix[water-simulation][webgpu] is true but %q is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, webgpuRendererPath)
		}
	}
	// The other three stages run on author events. Naming them keeps a silent
	// removal from passing as a refactor.
	for _, stage := range []string{"seedCompute", "dropCompute", "displacementCompute"} {
		if !strings.Contains(webgpu, stage) {
			t.Errorf("the WebGPU water sim must keep the %q stage; %s no longer mentions it",
				stage, webgpuRendererPath)
		}
	}
}

// TestWaterSimStaysRequired states the policy reasoning, which differs from every
// other water cell.
//
// The rule: a feature is required only when its absence produces a DIFFERENT
// scene, not a degraded image of the same scene. A pool with no simulation is a
// flat plane. Nothing about the authored scene survives — no ripple, no
// interaction, no caustic movement. That is a different scene, so it excludes.
//
// Compare the sibling cells. water-object-mesh-shadow-pass draws a shadow of the
// wrong shape, and ibl lights the same objects less accurately. Both keep the
// scene recognisable, so both degrade. Only the simulation itself excludes.
//
// The exclusion is affordable because both GPU backends implement it. Only
// Canvas2D falls out, and Canvas2D rasterizes no triangle.
func TestWaterSimStaysRequired(t *testing.T) {
	pol := DefaultPolicy()
	if !pol.Required[FeatureWaterSim] {
		t.Fatal("water-simulation must stay required: a pool with no simulation is a different scene")
	}
	if pol.Required[FeatureWaterObjectMeshShadowPass] {
		t.Fatal("the mesh-shadow pass must stay droppable; see water_shadow_test.go")
	}

	caps := Verdict([]Feature{FeatureWaterSim}, nil, pol)
	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	if !capable[BackendWebGPU] || !capable[BackendWebGL] {
		t.Errorf("both GPU backends implement the simulation and must stay capable; Capable=%v", caps.Capable)
	}
	if capable[BackendCanvas2D] {
		t.Errorf("Canvas2D rasterizes no triangle and must be excluded; Capable=%v", caps.Capable)
	}
	if !hasExcludeReason(caps.Reasons, FeatureWaterSim, BackendCanvas2D) {
		t.Errorf("the author must see WHY Canvas2D fell out; Reasons=%v", caps.Reasons)
	}
	// The required feature must never leave Capable empty. That is the failure the
	// mesh-shadow correction was written to avoid, so assert it here too.
	if len(caps.Capable) == 0 {
		t.Fatal("water-simulation left no capable backend; a required cell went false on every backend")
	}
}

// webglMountChunkPath resolves the WebGL2 water renderer at mount time. A
// definition nobody reaches is a promise, not an implementation, so the true
// WebGL2 cell rests on this file as much as on the renderer.
const webglMountChunkPath = "../../client/runtime/scene3d/mount-webgl.ts"
