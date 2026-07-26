package capability

import (
	"os"
	"strings"
	"testing"
)

// TestGPUPickingCapableOnBothGPUBackends pins the gpu-picking verdict.
//
// gpu-picking is a REQUIRED feature in DefaultPolicy, so an unsupported cell
// EXCLUDES a backend instead of degrading it. While the WebGPU cell was false,
// one Pickable object removed WebGPU from Capable and forced every interactive
// scene onto WebGL2. Both GPU backends now implement picking, so the verdict
// must keep both and drop only Canvas2D.
func TestGPUPickingCapableOnBothGPUBackends(t *testing.T) {
	caps := Verdict([]Feature{FeatureGPUPicking}, nil, DefaultPolicy())

	got := map[Backend]bool{}
	for _, b := range caps.Capable {
		got[b] = true
	}
	if !got[BackendWebGPU] {
		t.Errorf("gpu-picking must keep WebGPU capable; Capable=%v", caps.Capable)
	}
	if !got[BackendWebGL] {
		t.Errorf("gpu-picking must keep WebGL capable; Capable=%v", caps.Capable)
	}
	if got[BackendCanvas2D] {
		t.Errorf("Canvas2D cannot pick and must be excluded; Capable=%v", caps.Capable)
	}
	if len(caps.Degraded[BackendWebGPU]) != 0 {
		t.Errorf("WebGPU must be capable outright, not degraded; Degraded=%v", caps.Degraded)
	}
	for _, reason := range caps.Reasons {
		if reason.Feature == FeatureGPUPicking && reason.Excludes == BackendWebGPU {
			t.Errorf("no reason may exclude WebGPU for gpu-picking; Reasons=%v", caps.Reasons)
		}
	}
}

// TestGPUPickingRendererEvidence ties the Matrix cell to the shipped renderer
// source. The drift guard already compares Matrix against the capability
// manifests, but a manifest is one boolean that anyone can edit. This test
// demands the WebGPU renderer actually carry the pick implementation, so the
// cell cannot go true on an empty promise.
//
// The named symbols are the load-bearing parts of the implementation:
//   - createSceneWebGPUPicker: owns the pick textures, pipelines, and readback
//   - r32uint:                 the integer ID attachment format
//   - copyTextureToBuffer:     the 1x1 pixel copy
//   - mapAsync:                the non-blocking readback
//   - sceneWebGPUPickResolve:  maps an ID back to the shared hit record
func TestGPUPickingRendererEvidence(t *testing.T) {
	if !Matrix[FeatureGPUPicking][BackendWebGPU] {
		t.Skip("Matrix says WebGPU cannot pick; nothing to corroborate")
	}
	const rendererPath = "../../client/js/bootstrap-src/16a-scene-webgpu.js"
	data, err := os.ReadFile(rendererPath)
	if err != nil {
		t.Fatalf("read WebGPU renderer at %s: %v", rendererPath, err)
	}
	source := string(data)
	for _, symbol := range []string{
		"createSceneWebGPUPicker",
		"sceneWebGPUPickIDPlan",
		"sceneWebGPUPickResolve",
		"r32uint",
		"copyTextureToBuffer",
		"mapAsync",
	} {
		if !strings.Contains(source, symbol) {
			t.Errorf("Matrix[gpu-picking][webgpu] is true but %s is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, rendererPath)
		}
	}
}

// TestGPUPickingWebGLEvidence records why the WebGL cell is true. Picking on
// WebGL2 is not an ID-buffer read: setupScenePickInteractions in
// 17-scene-input.js raycasts the scene bundle on the CPU. That function takes no
// renderer argument, so it serves BOTH GPU backends identically. This test fails
// if that shared, backend-neutral entry point disappears, because the WebGL cell
// and the byte-for-byte parity claim both rest on it.
func TestGPUPickingWebGLEvidence(t *testing.T) {
	if !Matrix[FeatureGPUPicking][BackendWebGL] {
		t.Skip("Matrix says WebGL cannot pick; nothing to corroborate")
	}
	const inputPath = "../../client/js/bootstrap-src/17-scene-input.js"
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read scene input at %s: %v", inputPath, err)
	}
	source := string(data)
	for _, symbol := range []string{
		"function setupScenePickInteractions(canvas, props, readViewport, readSceneBundle, emitInteraction)",
		"function sceneRaycastPick(",
		"window.__gosx_scene3d_api.sceneRaycastPickGroup = sceneRaycastPickGroup",
		"window.__gosx_scene3d_api.sceneRaycastPickInstancedMeshes = sceneRaycastPickInstancedMeshes",
	} {
		if !strings.Contains(source, symbol) {
			t.Errorf("the shared pick contract changed: %q is missing from %s; "+
				"re-check both gpu-picking cells and the WebGPU picker's refinement path", symbol, inputPath)
		}
	}
}
