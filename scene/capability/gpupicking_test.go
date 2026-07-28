package capability

import (
	"os"
	"testing"
)

// TestGPUPickingVerdictTracksMatrix pins the required-feature behavior without
// claiming a renderer implementation before its owning runtime slice lands.
func TestGPUPickingVerdictTracksMatrix(t *testing.T) {
	caps := Verdict([]Feature{FeatureGPUPicking}, nil, DefaultPolicy())

	got := map[Backend]bool{}
	for _, b := range caps.Capable {
		got[b] = true
	}
	wantWebGPU := Matrix[FeatureGPUPicking][BackendWebGPU]
	if got[BackendWebGPU] != wantWebGPU {
		t.Errorf("WebGPU capable=%v, want Matrix cell %v; Capable=%v", got[BackendWebGPU], wantWebGPU, caps.Capable)
	}
	if !got[BackendWebGL] {
		t.Errorf("gpu-picking must keep WebGL capable; Capable=%v", caps.Capable)
	}
	if got[BackendCanvas2D] {
		t.Errorf("Canvas2D cannot pick and must be excluded; Capable=%v", caps.Capable)
	}
	if wantWebGPU && len(caps.Degraded[BackendWebGPU]) != 0 {
		t.Errorf("supported WebGPU picking must not be degraded; Degraded=%v", caps.Degraded)
	}
	for _, reason := range caps.Reasons {
		if reason.Feature == FeatureGPUPicking && reason.Excludes == BackendWebGPU && wantWebGPU {
			t.Errorf("a true WebGPU cell must not be excluded; Reasons=%v", caps.Reasons)
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
//
// The test reads the renderer FIRST and then demands the cell match, so it fails
// whichever way the cell is wrong. It used to skip when the cell read false, so
// re-introducing the original defect — a false cell over a working picker —
// would have deleted this check instead of failing it.
func TestGPUPickingRendererEvidence(t *testing.T) {
	const rendererPath = "../../client/js/bootstrap-src/16a-scene-webgpu.js"
	data, err := os.ReadFile(rendererPath)
	if err != nil {
		t.Fatalf("read WebGPU renderer at %s: %v", rendererPath, err)
	}
	source := string(data)

	evidenceFor(t, FeatureGPUPicking, BackendWebGPU).
		needs(rendererPath, source,
			"createSceneWebGPUPicker",
			"sceneWebGPUPickIDPlan",
			"sceneWebGPUPickResolve",
			"r32uint",
			"copyTextureToBuffer",
			"mapAsync",
		).
		assertAgrees("an ID-buffer pick needs the picker, an r32uint attachment, a 1x1 " +
			"copyTextureToBuffer and a mapAsync readback, plus the resolve back to the hit record")
}

// TestGPUPickingWebGLEvidence records why the WebGL cell is true. Picking on
// WebGL2 is not an ID-buffer read: setupScenePickInteractions in
// 17-scene-input.js raycasts the scene bundle on the CPU. That function takes no
// renderer argument, so it serves BOTH GPU backends identically. This test fails
// if that shared, backend-neutral entry point disappears, because the WebGL cell
// and the byte-for-byte parity claim both rest on it.
//
// The test reads the input module FIRST and then demands the cell match, in
// whichever direction. The skip it used to open with meant that a WebGL cell
// flipped to false — which would exclude WebGL2 from every interactive scene,
// because gpu-picking is REQUIRED — silently removed this check.
func TestGPUPickingWebGLEvidence(t *testing.T) {
	const inputPath = "../../client/js/bootstrap-src/17-scene-input.js"
	data, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatalf("read scene input at %s: %v", inputPath, err)
	}
	source := string(data)

	evidenceFor(t, FeatureGPUPicking, BackendWebGL).
		needs(inputPath, source,
			"function setupScenePickInteractions(canvas, props, readViewport, readSceneBundle, emitInteraction)",
			"function sceneRaycastPick(",
			"window.__gosx_scene3d_api.sceneRaycastPickGroup = sceneRaycastPickGroup",
			"window.__gosx_scene3d_api.sceneRaycastPickInstancedMeshes = sceneRaycastPickInstancedMeshes",
		).
		assertAgrees("WebGL2 picks by raycasting the scene bundle on the CPU; setupScenePickInteractions " +
			"takes no renderer argument, so the same entry point serves both GPU backends")
}
