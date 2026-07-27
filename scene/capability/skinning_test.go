package capability

import (
	"strings"
	"testing"
)

// These tests corroborate the skinning row against renderer source.
//
// lights_test.go states why source is the only oracle that settles a cell:
// drift_test.go compares Matrix against two hand-written manifests, and both
// sides of that comparison come from one person in one commit. Four cells have
// already been caught wrong with every manifest agreeing.
//
// readRenderer, webgpuRendererPath and webglRendererPath come from lights_test.go.

// TestSkinningImplementedOnBothGPUBackends ties both true cells to the shipped
// implementations. The two backends skin by different mechanisms, so each half
// names its own load-bearing symbols.
//
// WebGL2 skins per vertex in the vertex shader:
//   - SCENE_PBR_SKINNED_VERTEX_SOURCE: the skinned vertex program
//   - a_joints / a_weights:            the per-vertex joint indices and weights
//   - u_jointMatrices[64]:             the bone palette uniform
//   - createScenePBRSkinnedProgram:    compiles and links it
//
// WebGPU skins in a compute pass and feeds the result to the draw:
//   - SCENE_ELIO_SKIN_LBS_SOURCE:      the linear blend skinning kernel
//   - entryPoint: "skin":              the compute entry point
//   - updateElioSkinnedMeshes:         dispatches it once per frame
//   - webGPUBindElioSkinnedBuffers:    binds the output as vertex slot 0
//
// skinning sits in DefaultPolicy().Required, so a false cell EXCLUDES a backend.
// That makes an empty promise here expensive: it would send every skinned scene
// to one backend, which is the exact failure gpu-picking caused.
func TestSkinningImplementedOnBothGPUBackends(t *testing.T) {
	if !Matrix[FeatureSkinning][BackendWebGL] {
		t.Fatal("Matrix says WebGL2 cannot skin; corroborate the new false cell instead")
	}
	webgl := readRenderer(t, webglRendererPath)
	for _, symbol := range []string{
		"SCENE_PBR_SKINNED_VERTEX_SOURCE",
		`"in vec4 a_joints;",`,
		`"in vec4 a_weights;",`,
		`"uniform mat4 u_jointMatrices[64];",`,
		`"        pos = skinMatrix * pos;",`,
		"function createScenePBRSkinnedProgram(gl) {",
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("Matrix[skinning][webgl] is true but %q is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, webglRendererPath)
		}
	}

	if !Matrix[FeatureSkinning][BackendWebGPU] {
		t.Fatal("Matrix says WebGPU cannot skin; corroborate the new false cell instead")
	}
	webgpu := readRenderer(t, webgpuRendererPath)
	for _, symbol := range []string{
		"var SCENE_ELIO_SKIN_LBS_SOURCE = [",
		"@compute @workgroup_size(64)",
		`entryPoint: "skin"`,
		"function updateElioSkinnedMeshes(",
		"function webGPUObjectIsSkinned(",
		"function webGPUBindElioSkinnedBuffers(",
		"WGPU_PBR_VERTEX_LAYOUT",
	} {
		if !strings.Contains(webgpu, symbol) {
			t.Errorf("Matrix[skinning][webgpu] is true but %q is missing from %s; "+
				"flip the cell back or finish the implementation", symbol, webgpuRendererPath)
		}
	}
}

// TestSkinnedNormalGapIsRecordedNotClaimed is the finding this file exists for.
//
// Both skinning cells are true, and a skinned mesh does deform on both backends.
// The two images still differ, because only WebGL2 skins the NORMALS.
//
// The WGSL kernel writes three floats per vertex and stops. Its whole body
// contains the strings "normal" and "tangent" zero times, and its last write is
// out[((i * 3u) + 2u)] = skinned.z. Meanwhile webGPUBindElioSkinnedBuffers binds
// slots 1 and 3 from webGPUTransformVec3Attribute and
// webGPUTransformTangentAttribute, and both of those apply the object's MODEL
// matrix, not a joint matrix. So WebGPU lights a deformed limb from rest-pose
// normals.
//
// WebGL2 skins all three vectors in one matrix multiply.
//
// No cell and no diagnostic record this. The WebGPU renderer publishes four
// issue codes (light-cap, spot-empty-cone, rect-area-specular, light-probe-sh)
// and none of them mentions skinning.
//
// RECOMMENDED FOLLOW-UP: a skinned-normals feature, WebGPU false and WebGL2
// true, droppable. Wrong normals shade the same silhouette in the same place, so
// they degrade the image rather than produce a different scene. That row needs a
// key in BOTH renderer manifests, so this test records the gap instead of adding
// a row the drift guard would reject.
//
// The test fails the moment either side changes, which is the moment the row
// becomes worth adding.
func TestSkinnedNormalGapIsRecordedNotClaimed(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	kernel := webgpuElioSkinKernel(t, webgpu)

	// The kernel writes position only. Three writes, and no fourth.
	for _, write := range []string{
		"out[((i * 3u) + 0u)] = skinned.x;",
		"out[((i * 3u) + 1u)] = skinned.y;",
		"out[((i * 3u) + 2u)] = skinned.z;",
	} {
		if !strings.Contains(kernel, write) {
			t.Errorf("the WebGPU skin kernel lost %q; re-read what it writes now", write)
		}
	}
	for _, absent := range []string{"normal", "tangent", "Normal", "Tangent"} {
		if strings.Contains(kernel, absent) {
			t.Errorf("the WebGPU skin kernel now mentions %q; if it skins normals, "+
				"delete this test and check whether WebGPU still needs a skinned-normals gap", absent)
		}
	}

	// And the draw binds unskinned normals and tangents. Both helpers apply the
	// model matrix only, which is why the gap survives the kernel.
	for _, symbol := range []string{
		`webGPUTransformVec3Attribute(obj, "normals", count, [0, 0, 1], "normals")`,
		"webGPUTransformTangentAttribute(obj, count)",
	} {
		if !strings.Contains(webgpu, symbol) {
			t.Errorf("expected %q in %s: the skinned draw binds model-transformed normals, "+
				"and this test documents that", symbol, webgpuRendererPath)
		}
	}

	// WebGL2 is the contrast that makes the gap a gap rather than a limitation
	// of skinning itself.
	webgl := readRenderer(t, webglRendererPath)
	for _, symbol := range []string{
		`"        norm = mat3(skinMatrix) * norm;",`,
		`"        tang = mat3(skinMatrix) * tang;",`,
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("expected %q in %s: WebGL2 skinning normals is the contrast this gap rests on",
				symbol, webglRendererPath)
		}
	}

	// No cell claims the gap yet, and no cell may claim it without manifest keys.
	if _, exists := Matrix[Feature("skinned-normals")]; exists {
		t.Error("a skinned-normals row appeared; move the reasoning above into its own corroboration test " +
			"and confirm both renderer manifests carry the key")
	}
}

// TestSkinningStaysRequired keeps the policy half explicit.
//
// The rule: a feature is required only when its absence produces a DIFFERENT
// scene, not a degraded image of the same scene. A mesh that does not deform is
// a different scene — an author who animates a walk cycle and gets a T-pose is
// not looking at a rougher version of the walk. So skinning excludes.
//
// The exclusion is safe today because both GPU backends implement it, so only
// Canvas2D falls out, and Canvas2D rasterizes no triangle anyway.
func TestSkinningStaysRequired(t *testing.T) {
	if !DefaultPolicy().Required[FeatureSkinning] {
		t.Fatal("skinning must stay required: an undeformed mesh is a different scene, not a degraded one")
	}
	caps := Verdict([]Feature{FeatureSkinning}, nil, DefaultPolicy())
	capable := map[Backend]bool{}
	for _, b := range caps.Capable {
		capable[b] = true
	}
	if !capable[BackendWebGPU] || !capable[BackendWebGL] {
		t.Errorf("both GPU backends must stay capable; Capable=%v", caps.Capable)
	}
	if capable[BackendCanvas2D] {
		t.Errorf("Canvas2D rasterizes no triangle and must be excluded; Capable=%v", caps.Capable)
	}
	if len(caps.Degraded) != 0 {
		t.Errorf("a required feature excludes rather than degrades; Degraded=%v", caps.Degraded)
	}
	if !hasExcludeReason(caps.Reasons, FeatureSkinning, BackendCanvas2D) {
		t.Errorf("the author must see WHY Canvas2D fell out; Reasons=%v", caps.Reasons)
	}
}

// webgpuElioSkinKernel returns the WGSL source of the skin kernel. Slicing the
// kernel out keeps the "no normals" claim about the KERNEL, not about the whole
// 842 KB renderer, where "normal" appears everywhere for unskinned draws.
func webgpuElioSkinKernel(t *testing.T, source string) string {
	t.Helper()
	const anchor = "var SCENE_ELIO_SKIN_LBS_SOURCE = ["
	start := strings.Index(source, anchor)
	if start < 0 {
		t.Fatalf("the WebGPU skin kernel moved; re-check Matrix[skinning][webgpu]")
	}
	rest := source[start+len(anchor):]
	end := strings.Index(rest, `].join("\n");`)
	if end < 0 {
		t.Fatal("the WebGPU skin kernel literal is unterminated; re-read the source")
	}
	return rest[:end]
}
