package capability

import (
	"strings"
	"testing"
)

// webgpuComputePath is the compute module that owns the cull system.
// webgpuRendererPath and webglRendererPath come from lights_test.go, and
// readRenderer with them.
const webgpuComputePath = "../../client/js/bootstrap-src/16b-scene-compute.js"

// TestGPUCullRendererEvidence ties the gpu-cull Matrix cell to the shipped
// renderer source, the way TestGPUPickingRendererEvidence does for picking. The
// drift guard only proves the manifests agree with Matrix, and a manifest is one
// boolean anyone can edit. That exact failure already happened with gpu-picking:
// both cells were wrong and the drift guard stayed green.
//
// The named symbols are the load-bearing parts of the implementation:
//   - createSceneInstancedCullSystem: owns the buffers, the bind group and the
//     compute pipeline
//   - drawArgsBuf with INDIRECT usage: the indirect-draw argument buffer
//   - atomicAdd:                       compaction of survivors
//   - drawIndirect:                    the draw that reads the compacted count
//
// The test reads the renderer FIRST and then demands the cell match, so it fails
// whichever way the cell is wrong. It used to skip when the cell read false,
// which made flipping the cell to a lie also delete the check. See cellEvidence
// in evidence_test.go.
func TestGPUCullRendererEvidence(t *testing.T) {
	compute := readRenderer(t, webgpuComputePath)
	renderer := readRenderer(t, webgpuRendererPath)

	evidenceFor(t, FeatureGPUCull, BackendWebGPU).
		needs(webgpuComputePath, compute,
			"function createSceneInstancedCullSystem(",
			"GPUBufferUsage.INDIRECT",
			"atomicAdd(&drawArgs[1], 1u)",
			"beginComputePass()",
		).
		needs(webgpuRendererPath, renderer,
			"pass.drawIndirect(cullSys.drawArgsBuf, 0)",
			"updateInstancedCullSystems(",
		).
		assertAgrees("a GPU cull needs a compute pass that compacts survivors with atomicAdd, " +
			"an INDIRECT argument buffer, and a draw that reads the compacted count")
}

// TestGPUCullScalesRadiusPerInstance pins the correctness property that makes
// the gpu-cull cell honest rather than merely present.
//
// A cull that tests one constant radius against the frustum drops an instance
// whose transform scales it up, so the instance disappears while it is plainly on
// screen. The desktop renderer had the identical defect and fixed it in cullWGSL
// (render/bundle/cull.go) and instanceCullRadius (render/bundle/primitive.go).
// The browser kernel must carry the same three column lengths.
//
// Removing any of the three length() calls fails this test.
func TestGPUCullScalesRadiusPerInstance(t *testing.T) {
	compute := readRenderer(t, webgpuComputePath)

	for _, term := range []string{
		"length(m[0].xyz)",
		"length(m[1].xyz)",
		"length(m[2].xyz)",
		"radius = radius * scale",
		"if (d < -radius) { return; }",
	} {
		if !strings.Contains(compute, term) {
			t.Errorf("the built-in cull kernel must contain %q; without it a scaled instance vanishes", term)
		}
	}

	// The CPU oracle must exist too, so a test or a headless path can predict
	// what the kernel will do.
	for _, symbol := range []string{
		"function sceneInstanceColumnScale(",
		"function sceneInstanceCullRadius(",
		"function sceneInstancedMaxTransformScale(",
	} {
		if !strings.Contains(compute, symbol) {
			t.Errorf("16b-scene-compute.js must export %q as the CPU oracle for the kernel", symbol)
		}
	}

	// An AUTHORED kernel is the author's code and may ignore scale entirely, so
	// the JS side inflates the uniform radius by the largest instance scale
	// instead. Over-including is safe; dropping a visible instance is not.
	if !strings.Contains(compute, "maxInstanceScale") {
		t.Error("an authored cull kernel must receive the conservative radius bound")
	}
}

// TestGPUCullAbsentOnWebGL corroborates the FALSE half of the row. A false cell
// is a claim too, and it degrades a scene onto a backend that cannot cull. The
// WebGL2 renderer has no compute stage at all, so it must carry no compute cull.
//
// The three symbols are the ones a compute cull cannot do without. Their absence
// IS the evidence for the false cell, so any of them appearing must flip it. The
// test used to skip when the cell read true, which is exactly the case worth
// catching.
func TestGPUCullAbsentOnWebGL(t *testing.T) {
	webgl := readRenderer(t, webglRendererPath)

	evidenceFor(t, FeatureGPUCull, BackendWebGL).
		refutedBy(webglRendererPath, webgl,
			"beginComputePass",
			"drawIndirect",
			"createComputePipeline",
		).
		assertAgrees("WebGL2 has no compute stage, so a compute cull would need one of these three " +
			"WebGPU-only calls; their absence is why the cell reads false")
}

// TestGPUCullDegradesRatherThanExcludes keeps gpu-cull out of the required set.
// Every feature in DefaultPolicy().Required is WebGPU-true today, so no
// wire-detectable feature can exclude WebGPU. gpu-cull is a WebGPU-only
// optimisation: making it required would exclude WebGL2 from any scene that uses
// an instanced mesh, which is the opposite of an honest fallback.
func TestGPUCullDegradesRatherThanExcludes(t *testing.T) {
	if DefaultPolicy().Required[FeatureGPUCull] {
		t.Fatal("gpu-cull must not be required: it would exclude WebGL2 instead of degrading it")
	}
	caps := Verdict([]Feature{FeatureGPUCull}, nil, DefaultPolicy())
	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	if !capable[BackendWebGPU] {
		t.Errorf("WebGPU must stay capable; Capable=%v", caps.Capable)
	}
	if !capable[BackendWebGL] {
		t.Errorf("WebGL must stay capable and merely degraded; Capable=%v", caps.Capable)
	}
	if len(caps.Degraded[BackendWebGL]) != 1 || caps.Degraded[BackendWebGL][0] != FeatureGPUCull {
		t.Errorf("WebGL must be degraded by gpu-cull alone; Degraded=%v", caps.Degraded)
	}
	if len(caps.Degraded[BackendWebGPU]) != 0 {
		t.Errorf("WebGPU must not be degraded; Degraded=%v", caps.Degraded)
	}
}

// TestRenderBundlesAreNotACapabilityCell records a deliberate NON-decision, so a
// later reader does not add the cell and quietly degrade WebGL2.
//
// Render bundles, shader-f16 and timestamp queries are performance features. The
// Matrix records which backend produces a FAITHFUL IMAGE. WebGL2 draws the same
// image without any of the three, so giving them cells would mark WebGL2
// degraded for a scene it renders correctly, and the degrade list is author-
// facing. Keep them out of the Matrix and report them through the
// data-gosx-scene3d-webgpu-* diagnostics instead.
func TestRenderBundlesAreNotACapabilityCell(t *testing.T) {
	for _, name := range []string{"render-bundles", "shader-f16", "timestamp-query", "gpu-timing"} {
		if _, exists := Matrix[Feature(name)]; exists {
			t.Errorf("%q is a performance feature, not an image feature; it must not have a Matrix cell", name)
		}
	}
	// And the renderer must publish them, or a tool has no way to see them.
	renderer := readRenderer(t, webgpuRendererPath)
	for _, attr := range []string{
		"data-gosx-scene3d-webgpu-bundle-state",
		"data-gosx-scene3d-webgpu-post-precision",
		"data-gosx-scene3d-webgpu-gpu-pass-main-ms",
	} {
		if !strings.Contains(renderer, attr) {
			t.Errorf("the renderer must publish %q so tooling can observe the optimisation", attr)
		}
	}
}
