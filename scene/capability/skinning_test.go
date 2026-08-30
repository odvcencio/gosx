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
//   - webGPUBindElioSkinnedBuffers:    binds the packed output regions as
//     vertex slots 0, 1 and 3
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
		`"mat4 model=u_modelMatrix*skinMatrix;vec4 worldPos=model*vec4(a_position,1.0);",`,
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

// TestSkinnedNormalsAndTangentsReachTheDraw is the positive contract that
// replaced the old rest-pose-normals gap sentinel: WebGPU now joint-skins
// normals and tangents alongside positions, matching WebGL2's shading-vector
// attribute coverage, so no cell or diagnostic needs to record that gap.
//
// The contract has three links. If any one breaks, WebGPU lights a deformed
// limb from wrong shading vectors again:
//
//  1. The WGSL kernel blends positions AND shading vectors with the joint
//     palette and writes three packed regions per vertex: positions at float
//     i*3, inverse-transpose normals at paddedCount*3, and orthogonalized
//     tangents (w corrected for reflection) at paddedCount*6.
//  2. webGPUBindElioSkinnedBuffers binds those regions to vertex slots 1 and
//     3 of the SAME output buffer that feeds slot 0, at byte offsets
//     paddedCount*12 and paddedCount*24.
//  3. WebGL2 composes model and skin first, then applies the same
//     inverse-transpose/orthogonalized tangent-frame rule in its vertex shader.
func TestSkinnedNormalsAndTangentsReachTheDraw(t *testing.T) {
	webgpu := readRenderer(t, webgpuRendererPath)
	kernel := webgpuElioSkinKernel(t, webgpu)

	// Link 1: the kernel skins and writes all three vectors. Slicing the
	// kernel out keeps each claim about the KERNEL, not about the whole ~900 KB
	// renderer, where "normal" appears everywhere for unskinned draws.
	for _, write := range []string{
		// positions
		"out[posBase] = skinned.x;",
		"out[posBase + 1u] = skinned.y;",
		"out[posBase + 2u] = skinned.z;",
		// normals: inverse-transpose the joint blend, write at paddedCount*3
		"let an = gosxAffineNormal(m, vec3f(v.nx, v.ny, v.nz));",
		"let rn = an.xyz;",
		"let normBase = (paddedCount * 3u) + posBase;",
		"out[normBase] = sn.x;",
		"out[normBase + 1u] = sn.y;",
		"out[normBase + 2u] = sn.z;",
		// tangents: joint-blend, orthogonalize, correct reflected w, write at paddedCount*6
		"let rt = (m * vec4f(v.tx, v.ty, v.tz, 0.0)).xyz;",
		"let ot = rt - sn * dot(sn, rt);",
		"let tanBase = (paddedCount * 6u) + (i * 4u);",
		"out[tanBase] = st.x;",
		"out[tanBase + 1u] = st.y;",
		"out[tanBase + 2u] = st.z;",
		"out[tanBase + 3u] = v.tw * an.w;",
	} {
		if !strings.Contains(kernel, write) {
			t.Errorf("the WebGPU skin kernel lost %q; re-read what it writes now", write)
		}
	}

	// Link 2: the draw consumes those regions from slots 1 and 3. The bind
	// helper must keep binding them from the packed output buffer at the byte
	// offsets the kernel wrote — not from model-transformed attribute scratch.
	for _, bind := range []string{
		"pass.setVertexBuffer(0, outputBuffer, 0, vec3Bytes);",
		"pass.setVertexBuffer(1, outputBuffer, paddedCount * 12, vec3Bytes);",
		"pass.setVertexBuffer(3, outputBuffer, paddedCount * 24, Math.max(4, count * 4 * 4));",
	} {
		if !strings.Contains(webgpu, bind) {
			t.Errorf("expected %q in %s: the skinned draw must feed slots 0/1/3 "+
				"from the compute output buffer's packed regions", bind, webgpuRendererPath)
		}
	}

	// Link 3: WebGL2 remains the coverage reference — it composes skinning with
	// the object model, inverse-transposes normals, orthogonalizes tangents, and
	// carries determinant sign into tangent handedness.
	webgl := readRenderer(t, webglRendererPath)
	for _, symbol := range []string{
		`"mat4 model=u_modelMatrix*skinMatrix;vec4 worldPos=model*vec4(a_position,1.0);",`,
		`"mat3 m=mat3(model);vec4 q=gosxAffineNormal(m,a_normal);",`,
		`"vec3 t=m*a_tangent.xyz;vec3 N=v_normal;vec3 T=normalize(t-N*dot(N,t));",`,
		`"vec3 B=cross(N,T)*a_tangent.w*q.w;",`,
	} {
		if !strings.Contains(webgl, symbol) {
			t.Errorf("expected %q in %s: WebGL2 skinning normals and tangents "+
				"is half of this coverage contract", symbol, webglRendererPath)
		}
	}

	// Full attribute coverage needs no new capability row; if one ever appears
	// it must carry keys in BOTH renderer manifests and its own corroboration
	// test.
	if _, exists := Matrix[Feature("skinned-normals")]; exists {
		t.Error("a skinned-normals row appeared; both GPU backends now skin all " +
			"three vectors, so a flat row would merely restate skinning")
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
// kernel out keeps each write claim about the KERNEL, not about the whole
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
