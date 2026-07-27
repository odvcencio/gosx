package capability

import (
	"strings"
	"testing"
)

// TestComputeParticlesRendererEvidence ties the true WebGPU cell to the shipped
// compute system.
//
// The named symbols are the load-bearing parts of the implementation:
//   - createSceneComputeParticleSystem: owns the state buffer, the params
//     uniform, the force buffer and the compute pipeline
//   - createComputePipelineAsync:       the compute stage itself. The async form
//     is deliberate: the synchronous call does not reject on a shader-validation
//     failure, so an authored kernel could fail silently
//   - SCENE_COMPUTE_PARTICLE_SOURCE / "simulate": the built-in kernel and its
//     entry point
//   - dispatchWorkgroups:               the per-frame simulation step
//   - GPUBufferUsage.VERTEX on the render buffer: the simulated state is drawn
//     directly, with no readback
func TestComputeParticlesRendererEvidence(t *testing.T) {
	if !Matrix[FeatureComputeParts][BackendWebGPU] {
		t.Fatal("Matrix says WebGPU cannot simulate particles on the GPU, but the compute system is " +
			"right there in 16b-scene-compute.js; corroborate the new false cell or flip it back")
	}
	compute := readRenderer(t, webgpuComputePath)
	for _, symbol := range []string{
		"function createSceneComputeParticleSystem(device, entry) {",
		`scopedDevice.createComputePipelineAsync(buildPipelineFromSource(SCENE_COMPUTE_PARTICLE_SOURCE, "simulate"))`,
		"GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX",
		"dispatchWorkgroups",
	} {
		if !strings.Contains(compute, symbol) {
			t.Errorf("16b-scene-compute.js must contain %q for compute-particles to be true on WebGPU", symbol)
		}
	}
	// An authored WGSL kernel is the whole point of the feature name. Losing it
	// would leave a built-in simulation, which the CPU mirror also provides.
	for _, symbol := range []string{
		"entry.computeWGSL",
		"entry.computeEntry",
	} {
		if !strings.Contains(compute, symbol) {
			t.Errorf("16b-scene-compute.js must read %q: an authored kernel is what separates "+
				"compute-particles from the CPU mirror", symbol)
		}
	}
	renderer := readRenderer(t, webgpuRendererPath)
	if !strings.Contains(renderer, "computeParticles") {
		t.Errorf("%s must consume bundle.computeParticles, or the compute system never runs", webgpuRendererPath)
	}
}

// TestComputeParticlesAbsentOnWebGL corroborates the FALSE half. A false cell is
// a claim too, and this one degrades a scene onto a backend that runs the
// simulation somewhere else.
//
// The WebGL2 renderer has no compute stage: createComputePipeline,
// beginComputePass and dispatchWorkgroups each appear zero times in
// 16-scene-webgl.js. gpucull_test.go asserts the same three absences for
// gpu-cull, and that shared absence is the reason both cells are false.
func TestComputeParticlesAbsentOnWebGL(t *testing.T) {
	if Matrix[FeatureComputeParts][BackendWebGL] {
		t.Fatal("the WebGL2 compute-particles cell must stay false: the WebGL2 renderer has no compute " +
			"stage, so it cannot run a compute kernel of any kind")
	}
	webgl := readRenderer(t, webglRendererPath)
	for _, symbol := range []string{
		"createComputePipeline",
		"beginComputePass",
		"dispatchWorkgroups",
	} {
		if strings.Contains(webgl, symbol) {
			t.Errorf("16-scene-webgl.js contains %q, so the false compute-particles cell is wrong", symbol)
		}
	}
}

// TestComputeParticlesWebGLSubstitutesACPUMirror records WHAT WebGL2 does
// instead, because that decides whether the false cell may degrade or must
// exclude.
//
// createSceneParticleSystem branches on the presence of a device:
//
//	if (device) { return createSceneComputeParticleSystem(device, entry); }
//	return createSceneCPUParticleSystem(entry);
//
// The CPU path mirrors the GPU path closely. Same 8-float per-particle layout,
// same deterministic hash RNG, same four emitter kinds. So particles still
// appear, still move and still fade.
//
// Two limits stop it from being the feature rather than a fallback:
//
//  1. It clamps the count to 10000. A larger system loses particles.
//  2. It cannot run an authored WGSL kernel, so custom motion falls back to the
//     built-in simulation.
//
// This test names both, so the next reader knows the degrade is measured rather
// than assumed.
func TestComputeParticlesWebGLSubstitutesACPUMirror(t *testing.T) {
	compute := readRenderer(t, webgpuComputePath)
	for _, symbol := range []string{
		"function createSceneParticleSystem(device, entry) {",
		"return createSceneCPUParticleSystem(entry);",
		"function createSceneCPUParticleSystem(entry) {",
		"var count = Math.min(entry.count || 0, 10000);",
		"// Particle state: same 8-float layout as GPU (posXYZ, velXYZ, age, lifetime).",
	} {
		if !strings.Contains(compute, symbol) {
			t.Errorf("expected %q in 16b-scene-compute.js: the CPU mirror is why the false WebGL2 cell "+
				"may degrade instead of exclude", symbol)
		}
	}
	// The WebGL2 renderer must actually drive the mirror, or the degrade claim is
	// empty and the cell would have to exclude.
	webgl := readRenderer(t, webglRendererPath)
	for _, symbol := range []string{
		"function syncComputeParticleSystems(entries) {",
		"system: createSceneParticleSystem(null, entry),",
		"buildComputePointsEntries(bundle.computeParticles, frameTimeSeconds)",
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("expected %q in %s: WebGL2 must run the CPU mirror, or compute-particles has to "+
				"exclude WebGL2 rather than degrade it", symbol, webglRendererPath)
		}
	}
}

// TestComputeParticlesDegradesRatherThanExcludes decides the policy half.
//
// The rule: a feature is required only when its absence produces a DIFFERENT
// scene, not a degraded image of the same scene. WebGL2 shows the same particle
// cloud, from the same emitter, with the same colour ramp, advanced by the same
// arithmetic on the CPU. That is the same scene at lower fidelity.
//
// The clamp at 10000 particles and the ignored authored kernel argue the other
// way for extreme scenes, and neither is visible at the granularity of one cell.
// Requiring the feature would resolve that ambiguity the expensive way: WebGPU is
// the only backend with the cell true, so every particle scene would exclude
// WebGL2, and a browser without WebGPU would get nothing where it can get a
// close approximation.
//
// So the cell degrades, and the author reads the gap under Degraded.
func TestComputeParticlesDegradesRatherThanExcludes(t *testing.T) {
	if DefaultPolicy().Required[FeatureComputeParts] {
		t.Fatal("compute-particles must not be required: it would exclude WebGL2 from every particle scene " +
			"that the CPU mirror draws")
	}
	caps := Verdict([]Feature{FeatureComputeParts}, nil, DefaultPolicy())
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
	if got := caps.Degraded[BackendWebGL]; len(got) != 1 || got[0] != FeatureComputeParts {
		t.Errorf("WebGL2 must be degraded by compute-particles alone; Degraded=%v", caps.Degraded)
	}
	if got := caps.Degraded[BackendWebGPU]; len(got) != 0 {
		t.Errorf("WebGPU must not be degraded; got %v", got)
	}
	for _, reason := range caps.Reasons {
		if reason.Excludes != "" {
			t.Errorf("compute-particles is droppable and must exclude nobody; got %+v", reason)
		}
	}
}
