package bundle

import (
	"strings"
	"testing"
)

// Drift guards for the depth only shadow shader.
//
// The native Go renderer fills its directional shadow map with
// render/bundle/lit.go shadowWGSL. The browser WebGPU renderer fills its own
// with client/js/bootstrap-src/16a-scene-webgpu.js WGSL_SHADOW_INSTANCED_VERTEX
// and WGSL_SHADOW_FRAGMENT. Nothing regenerates one copy from the other.
//
// A shadow pass has one job: write the depth a receiver later compares against.
// The invariant that matters is therefore not the text but the depth a vertex
// lands at. Three things decide that:
//
//  1. the multiplication order of the light view projection and the model
//     matrix,
//  2. the homogeneous w of the position, and
//  3. the fact that the pass writes depth only and no colour.
//
// Get any of those wrong on one backend and every shadow on that backend shifts
// or vanishes, while the receiving shader keeps using the same bias. These
// guards pin those three things.

const (
	shadowJSInstancedVertexName = "WGSL_SHADOW_INSTANCED_VERTEX"
	shadowJSPlainVertexName     = "WGSL_SHADOW_VERTEX"
	shadowJSFragmentName        = "WGSL_SHADOW_FRAGMENT"

	goShadowWhere = "render/bundle/lit.go shadowWGSL"
	jsShadowWhere = "client/js/bootstrap-src/16a-scene-webgpu.js WGSL_SHADOW_INSTANCED_VERTEX"
)

// shadowSharedTerms pins the depth producing contract of the shadow pass.
var shadowSharedTerms = []sharedTerm{
	{
		id:     "single-mat4-light-uniform",
		effect: "The shadow pass reads the wrong uniform offset and draws the map from the wrong viewpoint.",
		goPat:  `struct ShadowUniforms \{\nlightViewProj : mat4x4f,\n\};`,
		jsPat:  `struct ShadowFrameUniforms \{\nlightViewProjection: mat4x4f,\n\};`,
	},
	{
		id:     "light-uniform-at-group0-binding0",
		effect: "The pipeline layout stops matching the bind group, and pipeline creation fails.",
		goPat:  `@group\(0\) @binding\(0\) var<uniform> [A-Za-z]+ : ShadowUniforms;`,
		jsPat:  `@group\(0\) @binding\(0\) var<uniform> [A-Za-z]+: ShadowFrameUniforms;`,
	},
	{
		id:     "model-matrix-from-four-vec4-attributes",
		effect: "The instance transform is transposed or truncated, and every instance lands in the wrong place.",
		goPat:  `let model = mat4x4f\([A-Za-z0-9_.]+, [A-Za-z0-9_.]+, [A-Za-z0-9_.]+, [A-Za-z0-9_.]+\);`,
		jsPat:  `let model = mat4x4f\([A-Za-z0-9_.]+, [A-Za-z0-9_.]+, [A-Za-z0-9_.]+, [A-Za-z0-9_.]+\);`,
	},
	{
		id:     "transform-order-light-then-model-then-position",
		effect: "Shadow depth is computed in the wrong space, so shadows detach from their casters.",
		goPat:  `return [A-Za-z0-9_.]+ \* model \* vec4f\([A-Za-z0-9_.]+, (1\.0)\);`,
		jsPat:  `return [A-Za-z0-9_.]+ \* model \* vec4f\([A-Za-z0-9_.]+, (1\.0)\);`,
		want:   "1.0",
	},
	{
		id:     "vertex-entry-returns-only-clip-position",
		effect: "The pass starts producing an interpolant, which changes the pipeline layout.",
		goPat:  `\) -> @builtin\(position\) vec4f \{`,
		jsPat:  `\) -> @builtin\(position\) vec4f \{`,
	},
}

// TestShadowWGSLDepthContractMatchesJSWebGPU pins the depth producing contract
// shared by render/bundle/lit.go shadowWGSL and 16a-scene-webgpu.js
// WGSL_SHADOW_INSTANCED_VERTEX.
//
// A PASS PROVES: both copies still read one mat4x4 light view projection from
// group 0 binding 0, still build the model matrix from four vec4 attributes,
// still multiply light view projection by model by position with w equal to 1.0,
// and still return only a clip position. An edit that reorders that
// multiplication, drops the model matrix, or moves the uniform fails here.
//
// A PASS DOES NOT PROVE: that the two shadow maps hold the same depth values.
// They do not have to. Each renderer chooses its own map resolution, its own
// cascade count, and its own depth format. It also proves nothing about the
// receiving side; the receiver bias and filter differ, and
// TestShadowWGSLKnownDivergenceFromJSWebGPU records that.
//
// TestShadowDriftGuardsDetectMutation proves this test can fail.
func TestShadowWGSLDepthContractMatchesJSWebGPU(t *testing.T) {
	goSrc, jsSrc, _ := shadowShaderCopies(t)
	for _, problem := range checkSharedTerms(shadowSharedTerms, goShadowWhere, goSrc, jsShadowWhere, jsSrc) {
		t.Error(problem)
	}
}

// TestShadowWGSLDepthOnlyPass pins that neither copy writes colour.
//
// A PASS PROVES: the Go shadow module declares no fragment entry point, and the
// browser fragment entry point has an empty body and no return type. A colour
// write added to either copy fails here.
//
// A PASS DOES NOT PROVE: that the render pass attaches no colour target. That is
// a pipeline setting, not shader text.
func TestShadowWGSLDepthOnlyPass(t *testing.T) {
	if strings.Contains(shadowWGSL, "@fragment") {
		t.Errorf("%s now declares a fragment entry point. The shadow pass writes depth only; a colour write there costs bandwidth and changes the pipeline layout.", goShadowWhere)
	}
	jsFragment := normalizeWGSLSyntax(jsShaderSource(t, readJSWebGPURenderer(t), shadowJSFragmentName))
	const want = "@fragment fn fragmentMain() {}"
	if strings.TrimSpace(jsFragment) != want {
		t.Errorf("%s %s is now %q, pinned form is %q. The browser shadow pass must stay depth only.",
			jsWebGPURendererFile, shadowJSFragmentName, jsFragment, want)
	}
}

// shadowDivergentTerms records where the two shadow paths already differ.
var shadowDivergentTerms = []divergentTerm{
	{
		id:      "instance-matrix-attribute-locations",
		effect:  "None today. Each renderer builds its own vertex buffer layout, so the locations only have to agree with that renderer.",
		verdict: "Benign, but it blocks a shared Selena source. One generated shader would have to pick one location set, so one of the two vertex layouts would have to move.",
		goLine:  "@location(1) m0 : vec4f,",
		jsLine:  "@location(4) instanceMatrix0: vec4f,",
	},
	{
		id:      "non-instanced-variant",
		effect:  "None today. The Go renderer always draws shadow casters instanced, so it needs no plain variant.",
		verdict: "Record only. If the native renderer ever draws a single caster it needs the plain form, and the browser already has it.",
		goLine:  "let model = mat4x4f(m0, m1, m2, m3);",
		jsLine:  "return shadowFrame.lightViewProjection * vec4f(position, 1.0);",
	},
}

// TestShadowWGSLKnownDivergenceFromJSWebGPU pins the recorded differences
// between the two shadow paths.
//
// A PASS PROVES: both recorded differences are still present as written. An edit
// to either copy fails here and forces a fresh decision.
//
// A PASS DOES NOT PROVE: that the differences are harmless. Read the verdict on
// each row. It also covers no difference in the receiving shader. The Go
// receiver takes one hardware compare tap over a three layer depth array with a
// constant bias of 0.003 plus 0.003 per cascade. The browser receiver takes four
// Poisson disk taps over two single layer maps with a per light bias uniform.
// Those terms live in litWGSL and WGSL_PBR_FRAGMENT, not here.
//
// TestShadowDriftGuardsDetectMutation proves this test can fail.
func TestShadowWGSLKnownDivergenceFromJSWebGPU(t *testing.T) {
	goSrc, jsInstanced, jsPlain := shadowShaderCopies(t)
	jsSrc := jsInstanced + "\n" + jsPlain
	for _, problem := range checkDivergentTerms(shadowDivergentTerms, goShadowWhere, goSrc, jsShadowWhere, jsSrc) {
		t.Error(problem)
	}
}

// shadowShaderCopies returns the normalized Go shadow source, the browser
// instanced vertex source, and the browser plain vertex source.
func shadowShaderCopies(t *testing.T) (goSrc, jsInstanced, jsPlain string) {
	t.Helper()
	file := readJSWebGPURenderer(t)
	return normalizeWGSLSyntax(shadowWGSL),
		normalizeWGSLSyntax(jsShaderSource(t, file, shadowJSInstancedVertexName)),
		normalizeWGSLSyntax(jsShaderSource(t, file, shadowJSPlainVertexName))
}

// shadowSharedGuardMutations are edits that break the depth producing contract.
var shadowSharedGuardMutations = []litGuardMutation{
	{
		name:    "native renderer reverses the light and model multiplication",
		side:    "go",
		from:    "return shadowU.lightViewProj * model * vec4f(pos, 1.0);",
		to:      "return model * shadowU.lightViewProj * vec4f(pos, 1.0);",
		wantRow: "transform-order-light-then-model-then-position",
	},
	{
		name:    "browser drops the homogeneous w",
		side:    "js",
		from:    "* model * vec4f(in.position, 1.0);",
		to:      "* model * vec4f(in.position, 0.0);",
		wantRow: "transform-order-light-then-model-then-position",
	},
	{
		name:    "browser moves the light uniform off binding 0",
		side:    "js",
		from:    "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
		to:      "@group(0) @binding(1) var<uniform> shadowFrame: ShadowFrameUniforms;",
		wantRow: "light-uniform-at-group0-binding0",
	},
	{
		name:    "native renderer adds a second uniform field",
		side:    "go",
		from:    "struct ShadowUniforms {\nlightViewProj : mat4x4f,\n};",
		to:      "struct ShadowUniforms {\nlightViewProj : mat4x4f,\ncascadeIndex : f32,\n};",
		wantRow: "single-mat4-light-uniform",
	},
}

// shadowDivergentGuardMutations are edits that change a recorded divergence.
var shadowDivergentGuardMutations = []litGuardMutation{
	{
		name:    "native renderer renumbers the instance matrix attributes to match the browser",
		side:    "go",
		from:    "@location(1) m0 : vec4f,",
		to:      "@location(4) m0 : vec4f,",
		wantRow: "instance-matrix-attribute-locations",
	},
	{
		name:    "browser deletes the plain shadow vertex variant",
		side:    "js",
		from:    "return shadowFrame.lightViewProjection * vec4f(position, 1.0);",
		to:      "return shadowFrame.lightViewProjection * vec4f(position, 1.0) * 1.0;",
		wantRow: "non-instanced-variant",
	},
}

// TestShadowDriftGuardsDetectMutation proves the shadow guards can fail.
//
// It first confirms both guards pass on the shipped sources. It then applies one
// edit at a time to a copy held in memory and confirms the matching guard
// reports a problem that names the affected row. No file changes.
func TestShadowDriftGuardsDetectMutation(t *testing.T) {
	goSrc, jsInstanced, jsPlain := shadowShaderCopies(t)
	jsLedgerSrc := jsInstanced + "\n" + jsPlain

	if problems := checkSharedTerms(shadowSharedTerms, goShadowWhere, goSrc, jsShadowWhere, jsInstanced); len(problems) != 0 {
		t.Fatalf("the shadow contract table must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	if problems := checkDivergentTerms(shadowDivergentTerms, goShadowWhere, goSrc, jsShadowWhere, jsLedgerSrc); len(problems) != 0 {
		t.Fatalf("the shadow divergence ledger must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}

	t.Run("contract", func(t *testing.T) {
		for _, mut := range shadowSharedGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGo, mutJS := applyLitMutation(t, mut, goSrc, jsInstanced)
				assertProblemNamesRow(t, checkSharedTerms(shadowSharedTerms, goShadowWhere, mutGo, jsShadowWhere, mutJS), mut)
			})
		}
	})

	t.Run("divergent", func(t *testing.T) {
		for _, mut := range shadowDivergentGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGo, mutJS := applyLitMutation(t, mut, goSrc, jsLedgerSrc)
				assertProblemNamesRow(t, checkDivergentTerms(shadowDivergentTerms, goShadowWhere, mutGo, jsShadowWhere, mutJS), mut)
			})
		}
	})
}
