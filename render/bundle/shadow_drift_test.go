package bundle

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/gosx/render/gpu"
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

// ---------------------------------------------------------------------------
// WHICH FACE FILLS THE SHADOW MAP
// ---------------------------------------------------------------------------
//
// The guards above read shader text. Cull state is pipeline state, so none of
// them covers the one setting that decides WHICH SURFACE of a caster the shadow
// map records. The guards below cover it, on all three copies.
//
// THE TWO TECHNIQUES. A shadow pass may keep the face that points at the light
// or the face that points away from it.
//
//	first depth   keep the lit face. The map holds the near surface. A receiver
//	              compares against its own depth, so a curved surface shadows
//	              itself with acne unless a positive bias pushes the reference
//	              away from the light.
//	second depth  keep the unlit face. The map holds the far surface, already a
//	              caster thickness past the receiver, so acne cannot occur and
//	              the bias may fall to zero. The cost is that an OPEN caster has
//	              no far surface: a single-sided plane writes nothing and casts
//	              no shadow at all.
//
// WHAT EACH COPY DOES TODAY.
//
//	render/bundle/renderer.go        CullBack + FrontFaceCCW      first depth
//	16a-scene-webgpu.js gosx-shadow  cullMode "front", no
//	                                 frontFace, so WebGPU
//	                                 defaults to "ccw"            second depth
//	16-scene-webgl.js shadow pass    cullFace(gl.FRONT), no
//	                                 gl.frontFace call, so WebGL
//	                                 defaults to counter-clockwise second depth
//
// The browser pair reached second depth by accident, not by design. Both sites
// predate the winding change. 12-scene-geometry.js used to wind its solids
// CLOCKWISE as seen from outside, so a lit triangle projected clockwise from the
// light, read as BACK facing, and survived a front-face cull. The pass recorded
// the near surface and the mitigation those two sites were written for never
// ran. The winding change made the lit face front facing, so the cull started
// discarding it and both browser sites moved to second depth in one edit that
// named neither of them.
//
// VERDICT: the native path KEEPS first depth. Three measurements, not a
// preference.
//
//  1. The oracle reads no cull mode. scene/preview renders on
//     render/gpu/headless, and rasterizeTriangle there keeps a pixel whenever
//     the three edge functions share the sign of the triangle area, so BOTH
//     windings fill. TestBothWindingsFill in that package states it. The
//     shadow map on that device therefore holds whichever surface is nearer,
//     because the depth test is Less. Flipping this pipeline to CullFront
//     would not move one poster pixel. It would move only the Go/WASM browser
//     path through render/gpu/jsgpu, and move it AWAY from the poster it
//     exists to match.
//
//  2. The bias does not survive the move. litWGSL subtracts 0.003 + 0.003 per
//     cascade from the reference depth. Take the 1.6 unit cube of
//     render/gpu/headless shadowScene and measure how far its far face sits
//     behind its near face in each cascade's normalized depth:
//
//     cascade  depth shift  bias    shift / bias
//     0        0.036487     0.003   12.2
//     1        0.022388     0.006    3.7
//     2        0.010522     0.009    1.2
//
//     Recording the far surface therefore adds between 1.2 and 12 times the
//     WHOLE bias budget. Second depth needs that bias near zero, and even then
//     a receiver within one caster thickness loses its shadow. So the move is
//     not a pipeline edit; it is a pipeline edit plus a re-tuned bias plus new
//     fixtures.
//
//  3. Second depth drops open casters. engine.RenderInstancedMesh accepts
//     Kind "plane" with CastShadow true. A plane has one face. Under CullFront
//     the lit side is discarded and the plane writes no depth, so it casts
//     nothing. The poster, which culls nothing, still draws the shadow. That
//     is a new author-visible split, on the same path this change is meant to
//     unify.
//
// So the two browser sites and the native site disagree, on purpose, and the
// guards below hold each of them still. Change one and this file fails.

// shadowCullSite is one browser expression that decides which face fills a
// shadow map.
//
// pattern must match its source exactly once, and the capture must equal want.
// Exactly once matters: 16-scene-webgl.js calls gl.cullFace twice in the shadow
// pass, once to select the face and once to restore the default, and a guard
// that accepted either would pin nothing.
type shadowCullSite struct {
	id      string
	where   string
	effect  string
	source  string // "webgpu" for the whole WebGPU file, "webgl-shadow-pass" for one function body
	pattern string
	want    string
}

// browserShadowCullSites pins the face each browser shadow pass keeps.
var browserShadowCullSites = []shadowCullSite{
	{
		id:      "webgpu-plain-shadow-pipeline-culls-front",
		where:   "16a-scene-webgpu.js wgpuCreateShadowPipeline",
		effect:  "Every WebGPU shadow under a non-instanced draw switches between the near and the far surface of its caster, so peter-panning appears or self-shadowing acne appears.",
		source:  "webgpu",
		pattern: `label: "gosx-shadow",[\s\S]{0,800}?cullMode: "([a-z]+)"`,
		want:    "front",
	},
	{
		id:      "webgpu-instanced-shadow-pipeline-culls-front",
		where:   "16a-scene-webgpu.js wgpuCreateShadowInstancedPipeline",
		effect:  "The instanced shadow draw stops agreeing with the plain one, so two copies of one authored mesh cast two different shadows.",
		source:  "webgpu",
		pattern: `label: "gosx-shadow-instanced",[\s\S]{0,800}?cullMode: "([a-z]+)"`,
		want:    "front",
	},
	{
		id:     "webgl-shadow-pass-culls-front",
		where:  "16-scene-webgl.js renderSceneShadowPass",
		effect: "The WebGL2 shadow map switches between the near and the far surface of its caster, so the two browser backends stop drawing the same shadow.",
		source: "webgl-shadow-pass",
		// Scoped to the shadow-pass function body on purpose. The same two calls
		// appear again in the water pool pass, which culls front faces for an
		// unrelated reason: the pool tiles line the INSIDE of a box, so dropping
		// the near walls lets the camera look into the water. A whole-file
		// pattern would match both and pin neither.
		pattern: `gl\.enable\(gl\.CULL_FACE\);[\s\S]{0,800}?gl\.cullFace\(gl\.([A-Z_]+)\);`,
		want:    "FRONT",
	},
}

// webglShadowPassFunc is the WebGL2 function whose body carries the shadow cull
// call. checkBrowserShadowCullSites reads only that body.
const webglShadowPassFunc = "renderSceneShadowPass"

// checkBrowserShadowCullSites returns one problem per site whose expression
// moved. It takes the two sources as arguments so a self test can feed it a
// mutated copy and prove the guard fires. webglShadowPass is the body of
// renderSceneShadowPass, not the whole WebGL2 file.
func checkBrowserShadowCullSites(sites []shadowCullSite, webgpu, webglShadowPass string) []string {
	var problems []string
	seen := map[string]bool{}
	for _, site := range sites {
		if seen[site.id] {
			problems = append(problems, fmt.Sprintf("the shadow cull table lists id %q twice", site.id))
			continue
		}
		seen[site.id] = true
		src := webgpu
		if site.source == "webgl-shadow-pass" {
			src = webglShadowPass
		}
		re, err := regexp.Compile(site.pattern)
		if err != nil {
			problems = append(problems, fmt.Sprintf("site %q: pattern %s does not compile: %v", site.id, site.pattern, err))
			continue
		}
		matches := re.FindAllStringSubmatch(src, -1)
		if len(matches) != 1 {
			problems = append(problems, fmt.Sprintf("site %q: %s matched %d times, the guard needs exactly one.\nEffect: %s\nThe expression was renamed, reshaped, deleted or duplicated. Update the pattern together with the renderer change.",
				site.id, site.where, len(matches), site.effect))
			continue
		}
		if got := matches[0][1]; got != site.want {
			problems = append(problems, fmt.Sprintf("site %q: %s now culls %q, pinned value is %q.\nEffect: %s\nRead the verdict above this table: the browser keeps the unlit face and the native path keeps the lit one, on purpose.",
				site.id, site.where, got, site.want, site.effect))
		}
	}
	return problems
}

// browserFrontFaceSetting matches an expression that would override the default
// front face on either browser renderer. Both files must contain none, because
// the cull mode above only selects the intended face while the default holds.
//
// The pattern demands the punctuation of a real call or a real descriptor key,
// so the word "frontFace" inside a comment does not trip it. Comments in both
// files discuss the default on purpose.
var browserFrontFaceSetting = regexp.MustCompile(`frontFace\s*:|gl\.frontFace\(`)

// checkBrowserFrontFaceDefaults returns a problem when either browser renderer
// starts setting a front face.
func checkBrowserFrontFaceDefaults(webgpu, webgl string) []string {
	var problems []string
	for _, side := range []struct{ where, src string }{
		{jsWebGPURendererFile, webgpu},
		{jsWebGLRendererFile, webgl},
	} {
		if hits := browserFrontFaceSetting.FindAllString(side.src, -1); len(hits) != 0 {
			problems = append(problems, fmt.Sprintf("%s now sets a front face (%d occurrence(s), first %q).\n"+
				"Both browser renderers relied on the counter-clockwise default, and the shadow cull mode above was chosen against it. "+
				"Say which face the shadow pass keeps now, and update browserShadowCullSites in the same change.",
				side.where, len(hits), hits[0]))
		}
	}
	return problems
}

// nativeShadowPrimitive is the pinned primitive state of the native shadow
// pipeline. Read the verdict above browserShadowCullSites before changing it.
var nativeShadowPrimitive = gpu.PrimitiveState{
	Topology:  gpu.TopologyTriangleList,
	CullMode:  gpu.CullBack,
	FrontFace: gpu.FrontFaceCCW,
}

// cullModeName, frontFaceName and topologyName spell a gpu enum the way the
// source spells it. The enums are plain ints with no String method, so a raw %v
// prints a number and the reader has to open render/gpu/types.go to decode it.
func cullModeName(m gpu.CullMode) string {
	switch m {
	case gpu.CullNone:
		return "gpu.CullNone"
	case gpu.CullFront:
		return "gpu.CullFront"
	case gpu.CullBack:
		return "gpu.CullBack"
	}
	return fmt.Sprintf("gpu.CullMode(%d)", int(m))
}

func frontFaceName(f gpu.FrontFace) string {
	switch f {
	case gpu.FrontFaceCCW:
		return "gpu.FrontFaceCCW"
	case gpu.FrontFaceCW:
		return "gpu.FrontFaceCW"
	}
	return fmt.Sprintf("gpu.FrontFace(%d)", int(f))
}

func topologyName(top gpu.PrimitiveTopology) string {
	if top == gpu.TopologyTriangleList {
		return "gpu.TopologyTriangleList"
	}
	return fmt.Sprintf("gpu.PrimitiveTopology(%d)", int(top))
}

// checkNativeShadowPrimitive returns one problem per field that moved. It takes
// the state as an argument so a self test can feed it a mutated copy.
func checkNativeShadowPrimitive(got gpu.PrimitiveState) []string {
	var problems []string
	if got.CullMode != nativeShadowPrimitive.CullMode {
		problems = append(problems, fmt.Sprintf("cull-mode: the bundle.shadow pipeline now culls %s, pinned value is %s.\n"+
			"CullBack keeps the lit face, so the native map holds the near surface and the 0.003 plus 0.003 per cascade bias in litWGSL fits it. "+
			"CullFront would hold the far surface, which is between 1.2 and 12 times that whole bias away. Re-tune the bias and regenerate the shadow fixtures in the same change, or revert.",
			cullModeName(got.CullMode), cullModeName(nativeShadowPrimitive.CullMode)))
	}
	if got.FrontFace != nativeShadowPrimitive.FrontFace {
		problems = append(problems, fmt.Sprintf("front-face: the bundle.shadow pipeline now treats %s as front facing, pinned value is %s.\n"+
			"scene/geom winds every solid counter-clockwise as seen from outside. Flipping this inverts which face the cull mode above discards, so the shadow map records the opposite surface without the cull mode moving.",
			frontFaceName(got.FrontFace), frontFaceName(nativeShadowPrimitive.FrontFace)))
	}
	if got.Topology != nativeShadowPrimitive.Topology {
		problems = append(problems, fmt.Sprintf("topology: the bundle.shadow pipeline now draws %s, pinned value is %s.\n"+
			"Cull mode has no meaning outside a triangle topology, so the two settings above stop guarding anything.",
			topologyName(got.Topology), topologyName(nativeShadowPrimitive.Topology)))
	}
	return problems
}

// nativeShadowPipelinePrimitive builds a renderer on the recording fake device
// and returns the primitive state the renderer asked for on the depth-only
// shadow pipeline.
//
// It reads the descriptor the renderer built rather than the text of
// renderer.go. A text guard passes when the source is rewritten to reach the
// same wrong value through a variable.
func nativeShadowPipelinePrimitive(t *testing.T) gpu.PrimitiveState {
	t.Helper()
	device := newFakeDevice()
	renderer, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer renderer.Destroy()
	for _, pipeline := range device.pipelines {
		if pipeline.desc.Label == "bundle.shadow" {
			return pipeline.desc.Primitive
		}
	}
	t.Fatalf("no pipeline labelled %q was created; this guard reads the shadow pipeline by label, so a rename hides it. Update the label here together with the rename.", "bundle.shadow")
	return gpu.PrimitiveState{}
}

// TestNativeShadowPipelineKeepsTheLitFace pins the native shadow cull state.
//
// A PASS PROVES: the pipeline the renderer builds for the depth-only shadow pass
// still draws triangles, still treats counter-clockwise as front facing, and
// still culls back faces, so the native shadow map still records the surface
// nearest the light.
//
// A PASS DOES NOT PROVE: that the poster changes when this moves. It does not.
// render/gpu/headless culls nothing, so this setting reaches only the Go/WASM
// browser path through render/gpu/jsgpu. Read measurement 1 in the verdict
// above.
//
// TestShadowCullGuardsDetectMutation proves this test can fail.
func TestNativeShadowPipelineKeepsTheLitFace(t *testing.T) {
	for _, problem := range checkNativeShadowPrimitive(nativeShadowPipelinePrimitive(t)) {
		t.Error(problem)
	}
}

// TestBrowserShadowPassesKeepTheUnlitFace pins both browser shadow cull sites.
//
// A PASS PROVES: the two WebGPU shadow pipelines still set cullMode "front", the
// WebGL2 shadow pass still calls gl.cullFace(gl.FRONT), and neither browser
// renderer overrides the counter-clockwise front-face default those three
// settings were chosen against.
//
// A PASS DOES NOT PROVE: that the browser draws the shadow a viewer wants. Read
// measurement 3 in the verdict above: an open caster writes no depth under this
// setting, so a single-sided plane casts no browser shadow while it casts a
// native one.
//
// TestShadowCullGuardsDetectMutation proves this test can fail.
func TestBrowserShadowPassesKeepTheUnlitFace(t *testing.T) {
	webgpu, webgl := readJSWebGPURenderer(t), readJSWebGLRenderer(t)
	for _, problem := range checkBrowserShadowCullSites(browserShadowCullSites, webgpu, jsFunctionBody(t, jsWebGLRendererFile, webgl, webglShadowPassFunc)) {
		t.Error(problem)
	}
	for _, problem := range checkBrowserFrontFaceDefaults(webgpu, webgl) {
		t.Error(problem)
	}
}

// shadowCullMutation is one edit the cull guards must catch.
type shadowCullMutation struct {
	name    string
	side    string // "webgpu", "webgl" or "native"
	from    string
	to      string
	wantRow string // the text the failure must name
}

// shadowCullGuardMutations are edits that move a pinned cull setting.
var shadowCullGuardMutations = []shadowCullMutation{
	{
		name:    "browser WebGPU moves the plain shadow pipeline to back-face culling",
		side:    "webgpu",
		from:    "label: \"gosx-shadow\",\n      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),",
		to:      "label: \"gosx-shadow\",\n      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),\n      primitive: { topology: \"triangle-list\", cullMode: \"back\" },",
		wantRow: "webgpu-plain-shadow-pipeline-culls-front",
	},
	{
		name:    "browser WebGPU stops culling on the instanced shadow pipeline",
		side:    "webgpu",
		from:    "label: \"gosx-shadow-instanced\",\n      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),",
		to:      "label: \"gosx-shadow-instanced\",\n      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),\n      primitive: { topology: \"triangle-list\", cullMode: \"none\" },",
		wantRow: "webgpu-instanced-shadow-pipeline-culls-front",
	},
	{
		name:    "browser WebGL2 restores the pre-winding-change cull face in the shadow pass",
		side:    "webgl",
		from:    "gl.enable(gl.CULL_FACE);\n    gl.cullFace(gl.FRONT);",
		to:      "gl.enable(gl.CULL_FACE);\n    gl.cullFace(gl.BACK);",
		wantRow: "webgl-shadow-pass-culls-front",
	},
	{
		name:    "browser WebGL2 declares a clockwise front face",
		side:    "webgl",
		from:    "gl.enable(gl.CULL_FACE);\n    gl.cullFace(gl.FRONT);",
		to:      "gl.enable(gl.CULL_FACE);\n    gl.frontFace(gl.CW);\n    gl.cullFace(gl.FRONT);",
		wantRow: "now sets a front face",
	},
	{
		name:    "browser WebGPU declares a clockwise front face on the shadow pipeline",
		side:    "webgpu",
		from:    "primitive: { topology: \"triangle-list\", cullMode: \"front\" },",
		to:      "primitive: { topology: \"triangle-list\", cullMode: \"front\", frontFace: \"cw\" },",
		wantRow: "now sets a front face",
	},
}

// TestShadowCullGuardsDetectMutation proves the cull guards can fail.
//
// It confirms every guard passes on the shipped state, then applies one edit at
// a time to a copy held in memory and confirms the matching guard reports a
// problem that names the affected site. No file changes.
//
// The native side cannot be mutated as text, because its guard reads a pipeline
// descriptor rather than source. It mutates the descriptor instead, which is the
// same shape TestToneMapModeGuardDetectsMutation uses for the parsed browser
// table.
func TestShadowCullGuardsDetectMutation(t *testing.T) {
	webgpu, webgl := readJSWebGPURenderer(t), readJSWebGLRenderer(t)
	if problems := checkBrowserShadowCullSites(browserShadowCullSites, webgpu, jsFunctionBody(t, jsWebGLRendererFile, webgl, webglShadowPassFunc)); len(problems) != 0 {
		t.Fatalf("the browser shadow cull table must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	if problems := checkBrowserFrontFaceDefaults(webgpu, webgl); len(problems) != 0 {
		t.Fatalf("both browser renderers must set no front face before the mutation check means anything; got:\n%s",
			strings.Join(problems, "\n"))
	}
	shipped := nativeShadowPipelinePrimitive(t)
	if problems := checkNativeShadowPrimitive(shipped); len(problems) != 0 {
		t.Fatalf("the native shadow pipeline must match the pinned state before the mutation check means anything; got:\n%s",
			strings.Join(problems, "\n"))
	}

	t.Run("browser", func(t *testing.T) {
		for _, mut := range shadowCullGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGPU, mutGL := webgpu, webgl
				switch mut.side {
				case "webgpu":
					if !strings.Contains(mutGPU, mut.from) {
						t.Fatalf("mutation %q cannot apply: %s does not contain\n%s\nA mutation that changes nothing proves nothing.", mut.name, jsWebGPURendererFile, mut.from)
					}
					mutGPU = strings.Replace(mutGPU, mut.from, mut.to, 1)
				case "webgl":
					if !strings.Contains(mutGL, mut.from) {
						t.Fatalf("mutation %q cannot apply: %s does not contain\n%s\nA mutation that changes nothing proves nothing.", mut.name, jsWebGLRendererFile, mut.from)
					}
					mutGL = strings.Replace(mutGL, mut.from, mut.to, 1)
				default:
					t.Fatalf("mutation %q names side %q; the browser cases accept only webgpu and webgl", mut.name, mut.side)
				}
				problems := checkBrowserShadowCullSites(browserShadowCullSites, mutGPU, jsFunctionBody(t, jsWebGLRendererFile, mutGL, webglShadowPassFunc))
				problems = append(problems, checkBrowserFrontFaceDefaults(mutGPU, mutGL)...)
				assertShadowCullProblemNames(t, problems, mut)
			})
		}
	})

	t.Run("native", func(t *testing.T) {
		for _, mut := range []struct {
			name  string
			state gpu.PrimitiveState
			names string
		}{
			{
				name:  "native renderer adopts the browser front-face cull",
				state: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullFront, FrontFace: gpu.FrontFaceCCW},
				names: "cull-mode",
			},
			{
				name:  "native renderer stops culling in the shadow pass",
				state: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone, FrontFace: gpu.FrontFaceCCW},
				names: "cull-mode",
			},
			{
				name:  "native renderer declares a clockwise front face",
				state: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullBack, FrontFace: gpu.FrontFaceCW},
				names: "front-face",
			},
			{
				name:  "native renderer draws the shadow pass as a line list",
				state: gpu.PrimitiveState{Topology: gpu.TopologyLineList, CullMode: gpu.CullBack, FrontFace: gpu.FrontFaceCCW},
				names: "topology",
			},
		} {
			t.Run(mut.name, func(t *testing.T) {
				if mut.state == shipped {
					t.Fatalf("mutation %q sets the state the pipeline already carries, so it proves nothing", mut.name)
				}
				problems := checkNativeShadowPrimitive(mut.state)
				if len(problems) == 0 {
					t.Fatalf("mutation %q produced no problem; the guard does not cover it", mut.name)
				}
				if !strings.Contains(strings.Join(problems, "\n"), mut.names) {
					t.Fatalf("mutation %q fired a guard that does not name %q:\n%s", mut.name, mut.names, strings.Join(problems, "\n"))
				}
			})
		}
	})
}

// assertShadowCullProblemNames fails unless one reported problem names the site
// the mutation was meant to break. A mutation that fires the wrong guard proves
// nothing about the guard it was written for.
func assertShadowCullProblemNames(t *testing.T, problems []string, mut shadowCullMutation) {
	t.Helper()
	if len(problems) == 0 {
		t.Fatalf("mutation %q produced no problem; the guard does not cover it", mut.name)
	}
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, mut.wantRow) {
		t.Fatalf("mutation %q fired a guard that does not name %q:\n%s", mut.name, mut.wantRow, joined)
	}
}
