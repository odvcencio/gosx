package bundle

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Drift guard for the GPU frustum-cull kernel.
//
// The native renderer compiles cullWGSL from render/bundle/cull.go. The browser
// compiles SCENE_INSTANCED_CULL_BUILTIN_WGSL from
// client/js/bootstrap-src/16b-scene-compute.js. Both compact the visible
// instances of one InstancedMesh into a second buffer and bump one atomic
// counter that an indirect draw reads.
//
// The audit of 2026-07-26 found one difference. The browser bounded its threads
// on the live instance count and the native copy bounded on the buffer length,
// so the native copy compacted the zero-matrix records that sit past the live
// data in an over-allocated buffer. The browser recorded that in prose and
// called it "a documented divergence". The native copy now carries the same
// field and the same guard, and this file pins the agreement.

// jsSceneComputeFile is the browser compute-system source, relative to this
// package directory.
var jsSceneComputeFile = filepath.Join("..", "..", "client", "runtime", "scene3d", "compute.ts")

const (
	cullJSKernelName = "SCENE_INSTANCED_CULL_BUILTIN_WGSL"

	goCullWhere = "render/bundle/cull.go cullWGSL"
	jsCullWhere = "client/js/bootstrap-src/16b-scene-compute.js SCENE_INSTANCED_CULL_BUILTIN_WGSL"
)

// cullSharedTerms pins the contract both cull kernels must keep.
var cullSharedTerms = []sharedTerm{
	{
		id:     "cull-uniform-carries-the-live-instance-count",
		effect: "One kernel loses the live count and falls back to the buffer length, which compacts zero-matrix records.",
		goPat:  `radius : f32,\ninstanceCount : u32,`,
		jsPat:  `radius: f32,\ninstanceCount: u32,`,
	},
	{
		id: "cull-thread-guard-bounds-on-the-live-count",
		effect: "A thread past the live count reads an all-zero matrix. Its centre is the origin and its scale is zero, " +
			"so it passes the frustum test and compacts a degenerate instance. That instance draws nothing, " +
			"inflates the survivor count the telemetry reports, and costs vertex shading.",
		goPat: `if \(i >= min\(cull\.instanceCount, arrayLength\(&input\)\)\) \{ return; \}`,
		jsPat: `if \(index >= min\(cull\.instanceCount, arrayLength\(&src\)\)\) \{ return; \}`,
	},
	{
		id:     "cull-uniform-field-order",
		effect: "The 112-byte uniform block stops matching between the two copies, so every plane reads from the wrong offset.",
		goPat:  `planes : array<vec4f, 6>,\nvertexCount : u32,\nradius : f32,\ninstanceCount : u32,`,
		jsPat:  `planes: array<vec4f, 6>,\nvertexCount: u32,\nradius: f32,\ninstanceCount: u32,`,
	},
	{
		id:     "cull-radius-scaled-by-a-shear-safe-bound",
		effect: "A sheared instance is culled with an under-sized sphere and vanishes while it is plainly on screen.",
		goPat:  `let scale = sqrt\(dot\(m\[0\]\.xyz, m\[0\]\.xyz\) \+ dot\(m\[1\]\.xyz, m\[1\]\.xyz\) \+ dot\(m\[2\]\.xyz, m\[2\]\.xyz\)\);`,
		jsPat:  `let scale = sqrt\(dot\(m\[0\]\.xyz, m\[0\]\.xyz\) \+ dot\(m\[1\]\.xyz, m\[1\]\.xyz\) \+ dot\(m\[2\]\.xyz, m\[2\]\.xyz\)\);`,
	},
	{
		id:     "cull-zero-scale-keeps-the-base-radius",
		effect: "A degenerate transform collapses the bounding sphere to nothing on one backend and the instance disappears.",
		goPat:  `if \(scale > 0\.0\) \{\nradius = radius \* scale;`,
		jsPat:  `if \(scale > 0\.0\) \{ radius = radius \* scale; \}`,
	},
	{
		id:     "cull-plane-test-rejects-below-negative-radius",
		effect: "The frustum test flips sign on one backend, so it culls what it should keep.",
		goPat:  `let d = dot\(plane\.xyz, cent(?:er|re)\) \+ plane\.w;`,
		jsPat:  `let d = dot\(plane\.xyz, cent(?:er|re)\) \+ plane\.w;`,
	},
	{
		id:     "cull-survivor-slot-from-the-draw-args-counter",
		effect: "The survivors land in the wrong slots, or the indirect draw reads a count nothing wrote.",
		goPat:  `atomicAdd\(&drawArgs\[(1)\], 1u\)`,
		jsPat:  `atomicAdd\(&drawArgs\[(1)\], 1u\)`,
		want:   "1",
	},
	{
		id:     "cull-workgroup-size",
		effect: "The dispatch count the renderer computes stops matching the kernel, so the tail of a mesh never runs.",
		goPat:  `@compute @workgroup_size\((64)\)`,
		jsPat:  `@compute @workgroup_size\((64)\)`,
		want:   "64",
	},
}

// TestCullWGSLMatchesJSCompute pins the shared contract of the two cull kernels.
//
// A PASS PROVES: both declare the live instance count in the same uniform lane,
// both bound their threads on min(that count, the buffer length), both scale the
// bounding radius by the largest transform column, both keep the base radius at
// zero scale, and both compact through the same atomic counter at the same
// workgroup size.
//
// A PASS DOES NOT PROVE: that the two renderers cull the same instances. Each
// builds its own frustum planes from its own camera. It also says nothing about
// the CPU oracle in render/gpu/headless; runCullFrustum re-implements this
// kernel and TestCullDropsRecordsPastTheLiveInstanceCount covers that.
//
// TestCullDriftGuardsDetectMutation proves this test can fail.
func TestCullWGSLMatchesJSCompute(t *testing.T) {
	goSrc, jsSrc := cullShaderCopies(t)
	for _, problem := range checkSharedTerms(cullSharedTerms, goCullWhere, goSrc, jsCullWhere, jsSrc) {
		t.Error(problem)
	}
}

func cullShaderCopies(t *testing.T) (goSrc, jsSrc string) {
	t.Helper()
	data, err := os.ReadFile(jsSceneComputeFile)
	if err != nil {
		t.Fatalf("read %s: %v\nThe cull drift guard needs this file. Fix the path; do not skip the guard.", jsSceneComputeFile, err)
	}
	file := string(data)
	if !strings.Contains(file, cullJSKernelName) {
		t.Fatalf("%s no longer declares %s. Was the kernel renamed or moved? Update this guard together with the move.",
			jsSceneComputeFile, cullJSKernelName)
	}
	return normalizeWGSLSyntax(cullWGSL), normalizeWGSLSyntax(jsShaderSource(t, file, cullJSKernelName))
}

// cullGuardMutations are edits the guard must catch.
var cullGuardMutations = []litGuardMutation{
	{
		name:    "native renderer bounds threads on the buffer length again",
		side:    "go",
		from:    "if (i >= min(cull.instanceCount, arrayLength(&input))) { return; }",
		to:      "if (i >= arrayLength(&input)) { return; }",
		wantRow: "cull-thread-guard-bounds-on-the-live-count",
	},
	{
		name:    "browser bounds threads on the buffer length",
		side:    "js",
		from:    "if (index >= min(cull.instanceCount, arrayLength(&src))) { return; }",
		to:      "if (index >= arrayLength(&src)) { return; }",
		wantRow: "cull-thread-guard-bounds-on-the-live-count",
	},
	{
		name:    "native renderer turns the count lane back into padding",
		side:    "go",
		from:    "radius : f32,\ninstanceCount : u32,",
		to:      "radius : f32,\n_pad0 : vec2f,",
		wantRow: "cull-uniform-carries-the-live-instance-count",
	},
	{
		name:    "browser drops the per-instance scale term",
		side:    "js",
		from:    "let scale = sqrt(dot(m[0].xyz, m[0].xyz) + dot(m[1].xyz, m[1].xyz) + dot(m[2].xyz, m[2].xyz));",
		to:      "let scale = 1.0;",
		wantRow: "cull-radius-scaled-by-a-shear-safe-bound",
	},
	{
		name:    "native renderer bumps a different draw-args lane",
		side:    "go",
		from:    "atomicAdd(&drawArgs[1], 1u)",
		to:      "atomicAdd(&drawArgs[2], 1u)",
		wantRow: "cull-survivor-slot-from-the-draw-args-counter",
	},
	{
		name:    "browser halves the workgroup size",
		side:    "js",
		from:    "@compute @workgroup_size(64)",
		to:      "@compute @workgroup_size(32)",
		wantRow: "cull-workgroup-size",
	},
}

// TestCullDriftGuardsDetectMutation proves the cull guard can fail.
func TestCullDriftGuardsDetectMutation(t *testing.T) {
	goSrc, jsSrc := cullShaderCopies(t)
	if problems := checkSharedTerms(cullSharedTerms, goCullWhere, goSrc, jsCullWhere, jsSrc); len(problems) != 0 {
		t.Fatalf("the cull table must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	for _, mut := range cullGuardMutations {
		t.Run(mut.name, func(t *testing.T) {
			mutGo, mutJS := applyLitMutation(t, mut, goSrc, jsSrc)
			assertProblemNamesRow(t, checkSharedTerms(cullSharedTerms, goCullWhere, mutGo, jsCullWhere, mutJS), mut)
		})
	}
}

// TestCullUniformCarriesLiveInstanceCount pins the packing of the count lane.
//
// A PASS PROVES: a caller that supplies the live count writes it at the offset
// the shader reads, and a caller that omits it writes the all-ones sentinel that
// makes min() select the buffer length. The second half is what keeps the shader
// safe while recordCullPass in renderer.go still calls the three-argument form.
//
// A PASS DOES NOT PROVE: that the renderer supplies the count. It does not yet.
// TestCullUniformFromRecordCullPassStillOmitsTheCount records that.
func TestCullUniformCarriesLiveInstanceCount(t *testing.T) {
	r := &Renderer{}
	planes := [6][4]float32{}
	for p := range planes {
		planes[p] = [4]float32{float32(p), 0, 0, 1}
	}

	supplied := append([]byte(nil), r.cullUniformBytes(planes, 36, 2.5, 7)...)
	if len(supplied) != cullUniformSize {
		t.Fatalf("cullUniformBytes returned %d bytes, want %d; the shader binds a fixed size", len(supplied), cullUniformSize)
	}
	if got := binary.LittleEndian.Uint32(supplied[96:100]); got != 36 {
		t.Errorf("vertexCount lane = %d, want 36", got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(supplied[100:104])); got != 2.5 {
		t.Errorf("radius lane = %v, want 2.5", got)
	}
	if got := binary.LittleEndian.Uint32(supplied[cullUniformInstanceCountOffset : cullUniformInstanceCountOffset+4]); got != 7 {
		t.Errorf("instanceCount lane = %d, want 7; the shader would then cull every instance past that count", got)
	}
	if got := binary.LittleEndian.Uint32(supplied[108:112]); got != 0 {
		t.Errorf("the trailing pad lane = %d, want 0", got)
	}

	omitted := r.cullUniformBytes(planes, 36, 2.5)
	if got := binary.LittleEndian.Uint32(omitted[cullUniformInstanceCountOffset : cullUniformInstanceCountOffset+4]); got != cullUnboundedInstanceCount {
		t.Errorf("a call that omits the count wrote %d in the count lane, want the sentinel %d.\n"+
			"Any smaller value culls live instances, because the shader takes min(instanceCount, arrayLength).",
			got, cullUnboundedInstanceCount)
	}
}

// TestCullUniformFromRecordCullPassStillOmitsTheCount records the half of the
// cull fix that is not applied yet.
//
// recordCullPass lives in render/bundle/renderer.go, which another change owns.
// Until it passes st.instanceCount, the count lane carries the sentinel and the
// kernel still compacts the zero records past the live data on a real device.
// The one-line edit is on cullUniformBytes.
//
// A PASS PROVES: the caller still omits the count. Delete this test when the
// renderer edit lands, and add an assertion that the recorded uniform carries
// the mesh instance count instead.
func TestCullUniformFromRecordCullPassStillOmitsTheCount(t *testing.T) {
	source, err := os.ReadFile("renderer.go")
	if err != nil {
		t.Fatalf("read renderer.go: %v", err)
	}
	const pending = "r.cullUniformBytes(frustum, uint32(st.vertexCount), st.radius)"
	if !strings.Contains(string(source), pending) {
		t.Skipf("recordCullPass no longer calls the three-argument form. If it now passes the live count, "+
			"delete this test and assert the count instead. Looked for:\n  %s", pending)
	}
}
