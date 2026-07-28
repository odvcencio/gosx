package bundle

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Drift guards for the two post-effect shaders that shape the presented image:
// the tone-map compose pass and the bloom bright pass.
//
// The native renderer compiles composePresentWGSL and brightPassWGSL from
// render/bundle/bloom.go. The browser WebGPU renderer compiles
// WGSL_POST_TONEMAPPING_FRAGMENT and WGSL_POST_BLOOM_BRIGHT_FRAGMENT from
// client/js/bootstrap-src/16a-scene-webgpu.js. Nothing generates one copy from
// the other, and both decide what a viewer sees.
//
// The audit of 2026-07-26 found two differences here. Both are closed, and both
// now sit in a shared table below. The tone-map mode table closed first. The
// bright-pass knee closed on 2026-07-27, when the browser adopted the soft knee,
// and brightDivergentTerms went empty.
//
// An EMPTY ledger is the intended resting state, not a disabled guard. Read the
// note above brightDivergentTerms before deleting it.

const (
	toneMapJSFragmentName = "WGSL_POST_TONEMAPPING_FRAGMENT"
	brightJSFragmentName  = "WGSL_POST_BLOOM_BRIGHT_FRAGMENT"
	toneMapJSFuncName     = "sceneWebGPUToneMapMode"

	goToneMapWhere = "render/bundle/bloom.go composePresentWGSL"
	jsToneMapWhere = "client/js/bootstrap-src/16a-scene-webgpu.js WGSL_POST_TONEMAPPING_FRAGMENT"
	goBrightWhere  = "render/bundle/bloom.go brightPassWGSL"
	jsBrightWhere  = "client/js/bootstrap-src/16a-scene-webgpu.js WGSL_POST_BLOOM_BRIGHT_FRAGMENT"
	goModeWhere    = "render/bundle/bloom.go toneMapModeCode"
	jsModeWhere    = "client/js/bootstrap-src/16a-scene-webgpu.js sceneWebGPUToneMapMode"
)

// toneMapSharedTerms pins the operator selection and the operator constants.
//
// The three curves already agreed term for term before this audit. The mode
// table did not: the Go table had no entry for "linear" or "none" and folded
// every unknown name onto ACES, so an author who asked for no curve got a full
// filmic curve on the server and a clamp in the browser. These rows keep the
// two tables together.
var toneMapSharedTerms = []sharedTerm{
	{
		id:     "tonemap-mode-read-as-integer",
		effect: "A fractional mode lane rounds one way natively and another way in the browser, so one scene picks two operators.",
		goPat:  `let mode = i32\(present\.params\.x\);`,
		jsPat:  `let mode = i32\(params\.toneMapMode\);`,
	},
	{
		id:     "tonemap-mode-zero-clamps",
		effect: `An author who writes ToneMapping "none" gets a filmic curve on one backend and no curve on the other.`,
		goPat:  `if \(mode == (0)\) \{\nreturn clamp\(exposed, vec3f\(0\.0\), vec3f\(1\.0\)\);`,
		jsPat:  `if \(mode == (0)\) \{\ncolor = clamp\(color, vec3f\(0\.0\), vec3f\(1\.0\)\);`,
		want:   "0",
	},
	{
		id:     "tonemap-mode-two-is-reinhard",
		effect: `An author who writes ToneMapping "reinhard" gets Reinhard on one backend and another curve on the other.`,
		goPat:  `mode == (2)\) \{\nreturn reinhardToneMap\(exposed\);`,
		jsPat:  `mode == (2)\) \{\ncolor = reinhard\(color\);`,
		want:   "2",
	},
	{
		id:     "tonemap-mode-three-is-filmic",
		effect: `An author who writes ToneMapping "filmic" gets the Hejl curve on one backend and another curve on the other.`,
		goPat:  `mode == (3)\) \{\nreturn filmicToneMap\(exposed\);`,
		jsPat:  `mode == (3)\) \{\ncolor = filmic\(color\);`,
		want:   "3",
	},
	{
		id:     "tonemap-default-is-aces",
		effect: "A scene with no authored tone map reads with one curve on one backend and another on the other.",
		goPat:  `\}\nreturn acesFilmic\(exposed\);`,
		jsPat:  `\} else \{\ncolor = aces\(color\);`,
	},
	{
		id:     "tonemap-exposure-applied-before-the-operator",
		effect: "Exposure lands after the curve on one backend, so a raised exposure clips instead of rolling off.",
		goPat:  `let exposed = max\(x \* max\(present\.params\.y, 0\.0\), vec3f\(0\.0\)\);`,
		jsPat:  `color = color \* params\.exposure;`,
	},
	{
		id:     "aces-shoulder-constant-a",
		effect: "The ACES curve changes shape, so every mid tone in the frame moves.",
		goPat:  `let a = ([0-9.]+);\nlet b = 0\.03;`,
		jsPat:  `let a = ([0-9.]+);\nlet b = 0\.03;`,
		want:   "2.51",
	},
	{
		id:     "aces-toe-constant-b",
		effect: "The ACES toe lifts or drops, so every shadow in the frame moves.",
		goPat:  `let b = ([0-9.]+);`,
		jsPat:  `let b = ([0-9.]+);`,
		want:   "0.03",
	},
	{
		id:     "aces-denominator-constants",
		effect: "The ACES roll-off changes, so a highlight clips on one backend and rolls off on the other.",
		goPat:  `let c = ([0-9.]+);\nlet d = ([0-9.]+);\nlet e = ([0-9.]+);`,
		jsPat:  `let c = ([0-9.]+);\nlet d = ([0-9.]+);\nlet e = ([0-9.]+);`,
		want:   "2.43|0.59|0.14",
	},
	{
		id:     "aces-clamped-to-unit-range",
		effect: "One backend writes above one into an eight-bit target and wraps instead of clipping.",
		goPat:  `return clamp\(\(x \* \(a \* x \+ b\)\) / \(x \* \(c \* x \+ d\) \+ e\),\nvec3f\(0\.0\), vec3f\(1\.0\)\);`,
		jsPat:  `return clamp\(\(x \* \(a \* x \+ b\)\) / \(x \* \(c \* x \+ d\) \+ e\), vec3f\(0\.0\), vec3f\(1\.0\)\);`,
	},
	{
		id:     "reinhard-is-x-over-one-plus-x",
		effect: "The Reinhard curve stops being the Reinhard curve on one backend.",
		goPat:  `return x / \(vec3f\(1\.0\) \+ x\);`,
		jsPat:  `return x / \(x \+ vec3f\(1\.0\)\);`,
	},
	{
		id:     "filmic-black-point",
		effect: "The Hejl curve crushes or lifts the black point on one backend.",
		goPat:  `max\(vec3f\(0\.0\), x - vec3f\((0\.004)\)\)`,
		jsPat:  `max\(vec3f\(0\.0\), x - vec3f\((0\.004)\)\)`,
		want:   "0.004",
	},
	{
		id:     "filmic-numerator-constants",
		effect: "The Hejl curve changes contrast on one backend.",
		goPat:  `\(c \* \(([0-9.]+) \* c \+ ([0-9.]+)\)\) /`,
		jsPat:  `\(y \* \(([0-9.]+) \* y \+ vec3f\(([0-9.]+)\)\)\) /`,
		want:   "6.2|0.5",
	},
	{
		id:     "filmic-denominator-constants",
		effect: "The Hejl curve changes its shoulder on one backend.",
		goPat:  `\(c \* \(6\.2 \* c \+ ([0-9.]+)\) \+ ([0-9.]+)\)`,
		jsPat:  `\(y \* \(6\.2 \* y \+ vec3f\(([0-9.]+)\)\) \+ vec3f\(([0-9.]+)\)\)`,
		want:   "1.7|0.06",
	},
}

// TestToneMapWGSLMatchesJSWebGPU pins the tone-map operators and the mode
// selection shared by composePresentWGSL and WGSL_POST_TONEMAPPING_FRAGMENT.
//
// A PASS PROVES: both copies read the mode as an integer, route 0 to a clamp, 2
// to Reinhard, 3 to the Hejl curve and everything else to ACES, apply exposure
// before the operator, and carry the same eleven curve constants.
//
// A PASS DOES NOT PROVE: that the two renderers feed the same value into the
// pass. The native renderer tone maps the whole frame including the background
// clear, because its main pass writes the background into the high dynamic
// range target. The browser applies the curve in the material shader when no
// post chain runs, which leaves its background clear untouched. That is an
// architecture difference, not a shader difference, and no row here covers it.
//
// TestPostFXDriftGuardsDetectMutation proves this test can fail.
func TestToneMapWGSLMatchesJSWebGPU(t *testing.T) {
	goSrc, jsSrc := toneMapShaderCopies(t)
	for _, problem := range checkSharedTerms(toneMapSharedTerms, goToneMapWhere, goSrc, jsToneMapWhere, jsSrc) {
		t.Error(problem)
	}
}

// toneMapNameTable is every authored tone-map name whose mapping both sides
// must agree on, plus the names that must fall through to the default.
var toneMapNameTable = []string{"linear", "none", "reinhard", "filmic", "aces", "", "not-a-mode"}

// FOUR TABLES SERVE ONE AUTHORED STRING.
//
// Environment.ToneMapping is one field, and four functions turn it into the
// integer a shader branches on:
//
//	toneMapModeCode          render/bundle/bloom.go        native present pass
//	sceneWebGPUToneMapMode   16a-scene-webgpu.js           WebGPU post chain
//	scenePostToneMapMode     16-scene-webgl.js             WebGL2 post chain
//	sceneToneMapMode         16-scene-webgl.js             WebGL2 material shader
//
// The fourth was the odd one until 2026-07-27. It mapped neither "none" nor
// "filmic", so both fell through to ACES, and it skipped the trim the other
// three apply. scenePBRUploadExposure feeds it only when the page runs NO post
// chain, so one authored name produced two images on one backend, and which
// image an author saw depended on whether the page happened to carry a post
// effect. postfx_drift_test.go recorded that in prose and called it out of
// scope. It is in scope now, and toneMapModeTables below pins all four.

// toneMapModeTableSite names one table and where to read it.
type toneMapModeTableSite struct {
	id       string
	where    string
	webgl    bool // read 16-scene-webgl.js instead of 16a-scene-webgpu.js
	function string
}

// toneMapModeTables lists every browser table that must agree with Go.
var toneMapModeTables = []toneMapModeTableSite{
	{
		id:       "webgpu-post-chain",
		where:    "client/js/bootstrap-src/16a-scene-webgpu.js sceneWebGPUToneMapMode",
		function: toneMapJSFuncName,
	},
	{
		id:       "webgl-post-chain",
		where:    "client/js/bootstrap-src/16-scene-webgl.js scenePostToneMapMode",
		webgl:    true,
		function: "scenePostToneMapMode",
	},
	{
		id:       "webgl-material-shader",
		where:    "client/js/bootstrap-src/16-scene-webgl.js sceneToneMapMode",
		webgl:    true,
		function: "sceneToneMapMode",
	},
}

// readToneMapModeTables parses every browser table into name-to-number maps,
// keyed by site id.
func readToneMapModeTables(t *testing.T) map[string]map[string]int {
	t.Helper()
	webgpu, webgl := readJSWebGPURenderer(t), readJSWebGLRenderer(t)
	out := make(map[string]map[string]int, len(toneMapModeTables))
	for _, site := range toneMapModeTables {
		file, where := webgpu, jsWebGPURendererFile
		if site.webgl {
			file, where = webgl, jsWebGLRendererFile
		}
		out[site.id] = jsToneMapModeTableIn(t, where, file, site.function)
	}
	return out
}

// TestToneMapModeTablesAgreeAcrossAllFourCopies pins the string-to-number table
// that decides which branch of the shader runs, on every copy.
//
// A PASS PROVES: toneMapModeCode in Go and all three browser tables return the
// same number for every name in toneMapNameTable, including the default that an
// unknown name and the empty string take. So "none" reaches the same operator on
// the native present pass, on the WebGPU post chain, on the WebGL2 post chain
// and in the WebGL2 material shader, and so does "filmic".
//
// A PASS DOES NOT PROVE: that the four shaders then DRAW the same curve for that
// number. The tables and the branches are separate,
// TestWebGL2ShadersImplementEveryModeTheirTablesEmit covers the WebGL2 branches,
// and toneMapSharedTerms covers the native-to-WebGPU pair.
//
// TestToneMapModeGuardDetectsMutation proves this test can fail.
func TestToneMapModeTablesAgreeAcrossAllFourCopies(t *testing.T) {
	tables := readToneMapModeTables(t)
	for _, site := range toneMapModeTables {
		for _, problem := range checkToneMapModeTable(site.where, tables[site.id]) {
			t.Error(problem)
		}
	}
}

// checkToneMapModeTable returns one problem per name the two tables map
// differently. It takes the browser table as data so a self test can feed it a
// mutated copy and prove the guard fires.
func checkToneMapModeTable(where string, jsTable map[string]int) []string {
	var problems []string
	for _, name := range toneMapNameTable {
		want, ok := jsTable[name]
		if !ok {
			want = jsTable[""]
		}
		if got := int(toneMapModeCode(name)); got != want {
			problems = append(problems, fmt.Sprintf("tone-map name %q: %s returns %d, %s returns %d.\n"+
				"Four tables serve one authored Environment.ToneMapping string, so change all four or none.",
				name, goModeWhere, got, where, want))
		}
	}
	return problems
}

// TestBrowserToneMapTablesNormalizeTheAuthoredName pins the normalization every
// table must apply before it compares.
//
// A PASS PROVES: all three browser tables trim the authored string and lower its
// case, so they answer the same way Go does. toneMapModeCode calls
// strings.ToLower(strings.TrimSpace(mode)), and a table that skips the trim
// sends " filmic " to the default while Go sends it to the Hejl curve.
//
// A PASS DOES NOT PROVE: that the trim is reachable. Each table guards it behind
// a typeof check, and this guard reads text rather than running the function.
//
// WHY A SEPARATE TEST. jsToneMapModeTableIn parses the literal names out of the
// if-conditions. It models no normalization at all, so a missing trim leaves the
// parsed map identical and TestToneMapModeTablesAgreeAcrossAllFourCopies stays
// green. sceneToneMapMode shipped without the trim until 2026-07-27 for exactly
// that reason.
func TestBrowserToneMapTablesNormalizeTheAuthoredName(t *testing.T) {
	webgpu, webgl := readJSWebGPURenderer(t), readJSWebGLRenderer(t)
	for _, site := range toneMapModeTables {
		file, where := webgpu, jsWebGPURendererFile
		if site.webgl {
			file, where = webgl, jsWebGLRendererFile
		}
		body := jsFunctionBody(t, where, file, site.function)
		for _, call := range []string{".trim()", ".toLowerCase()"} {
			if strings.Contains(body, call) {
				continue
			}
			t.Errorf("%s does not call %s.\n"+
				"render/bundle/bloom.go toneMapModeCode calls strings.ToLower(strings.TrimSpace(mode)), so an "+
				"authored name with a stray space or a capital reaches a different operator on this copy. "+
				"Four tables serve one authored string.", site.where, call)
		}
	}
}

// webglToneMapShaders names each WebGL2 shader constant and the mode numbers its
// own table can send it. A number with no branch falls through to the shader's
// else, which is a DIFFERENT wrong curve from the one the table fixed.
var webglToneMapShaders = []struct {
	constant string
	fedBy    string
	modes    []int
	fallsTo  string
}{
	{
		constant: "SCENE_PBR_FRAGMENT_SOURCE",
		fedBy:    "sceneToneMapMode",
		modes:    []int{1, 2, 3},
		fallsTo:  "mode 0, which applies no curve",
	},
	{
		constant: "SCENE_POST_TONEMAPPING_SOURCE",
		fedBy:    "scenePostToneMapMode",
		modes:    []int{0, 2, 3},
		fallsTo:  "mode 1, which applies ACES",
	},
}

// TestWebGL2ShadersImplementEveryModeTheirTablesEmit pins the branches behind
// the numbers.
//
// A PASS PROVES: each WebGL2 tone-map shader carries an explicit branch for
// every mode its own table emits, except the one mode that is meant to be the
// else. Adding "filmic" to a table without adding the branch fails here.
//
// A PASS DOES NOT PROVE: that the branch computes the right curve. That is
// toneMapSharedTerms for the native-to-WebGPU pair, and no guard reads the GLSL
// constants term by term today.
//
// WHY THIS TEST EXISTS. The material shader had no mode-3 branch when the mode-3
// name was added to its table. The table fix alone would have moved "filmic"
// from ACES to no curve at all, which is a different wrong answer, not a fix.
func TestWebGL2ShadersImplementEveryModeTheirTablesEmit(t *testing.T) {
	webgl := readJSWebGLRenderer(t)
	for _, shader := range webglToneMapShaders {
		body := jsConstArrayBody(t, jsWebGLRendererFile, webgl, shader.constant)
		for _, mode := range shader.modes {
			needle := fmt.Sprintf("u_toneMapMode == %d", mode)
			if strings.Contains(body, needle) {
				continue
			}
			t.Errorf("%s in %s carries no %q branch, but %s can emit %d.\n"+
				"An emitted mode with no branch falls through to the else, which is %s. "+
				"Add the branch and the table entry together.",
				shader.constant, jsWebGLRendererFile, needle, shader.fedBy, mode, shader.fallsTo)
		}
	}
}

// jsConstArrayBody returns the text of a `const NAME = [ ... ].join(` array
// literal. The WebGL2 renderer keeps its GLSL that way, and jsShaderSource reads
// only the `var NAME = [` spelling the WebGPU renderer uses.
func jsConstArrayBody(t *testing.T, where, file, name string) string {
	t.Helper()
	needle := "const " + name + " = ["
	start := strings.Index(file, needle)
	if start < 0 {
		t.Fatalf("shader constant %s not found in %s as `const %s = [`. Was it renamed, or is it no longer an array of lines? Update this guard together with the rename.",
			name, where, name)
	}
	end := strings.Index(file[start:], `].join(`)
	if end < 0 {
		t.Fatalf("shader constant %s in %s has no `].join(` terminator; the guard cannot tell where it ends", name, where)
	}
	return file[start : start+end]
}

// jsToneMapModeTableIn reads one name-to-number tone-map function and returns the
// mapping it implements. The empty-string key holds the default the function
// returns when no name matches.
//
// All three browser functions share one small, regular shape: a run of
// `if (normalized === "x" ...) return N;` lines followed by one bare
// `return N;`. This reader fails instead of guessing when it finds neither.
func jsToneMapModeTableIn(t *testing.T, where, file, function string) map[string]int {
	t.Helper()
	body := jsFunctionBody(t, where, file, function)
	table := map[string]int{}
	conditional := regexp.MustCompile(`if \(([^)]*)\) return (\d+);`)
	name := regexp.MustCompile(`"([a-zA-Z-]*)"`)
	for _, match := range conditional.FindAllStringSubmatch(body, -1) {
		code := 0
		if _, err := fmt.Sscanf(match[2], "%d", &code); err != nil {
			t.Fatalf("%s returns %q, which is not a number", where, match[2])
		}
		names := name.FindAllStringSubmatch(match[1], -1)
		if len(names) == 0 {
			t.Fatalf("%s has a branch this guard cannot read: %q", where, match[0])
		}
		for _, n := range names {
			table[n[1]] = code
		}
	}
	if len(table) == 0 {
		t.Fatalf("%s maps no name at all; the guard read the wrong function or the shape changed", where)
	}
	fallback := regexp.MustCompile(`\n\s*return (\d+);`)
	tail := fallback.FindAllStringSubmatch(body, -1)
	if len(tail) == 0 {
		t.Fatalf("%s has no default return; the guard cannot tell what an unknown name maps to", where)
	}
	last := 0
	if _, err := fmt.Sscanf(tail[len(tail)-1][1], "%d", &last); err != nil {
		t.Fatalf("%s default return %q is not a number", where, tail[len(tail)-1][1])
	}
	table[""] = last
	return table
}

// jsFunctionBody returns the text between the braces of `function NAME(`.
//
// where names the file in every failure. Two callers read two different browser
// renderers, so a hard-coded file name in the message sends the reader to the
// wrong file.
func jsFunctionBody(t *testing.T, where, file, name string) string {
	t.Helper()
	needle := "function " + name + "("
	start := strings.Index(file, needle)
	if start < 0 {
		t.Fatalf("function %s not found in %s. Was it renamed? Update this guard together with the rename.", name, where)
	}
	open := strings.IndexByte(file[start:], '{')
	if open < 0 {
		t.Fatalf("function %s in %s has no body", name, where)
	}
	open += start
	depth := 0
	for i := open; i < len(file); i++ {
		switch file[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return file[open+1 : i]
			}
		}
	}
	t.Fatalf("function %s in %s has no closing brace", name, where)
	return ""
}

// brightSharedTerms pins what the two bright passes already share.
var brightSharedTerms = []sharedTerm{
	{
		id:     "bright-pass-rec709-luma-weights",
		effect: "The same colour counts as bright on one backend and dark on the other, so bloom picks different pixels.",
		goPat:  `dot\(c, vec3f\((0\.2126), (0\.7152), (0\.0722)\)\)`,
		jsPat:  `dot\(color, vec3f\((0\.2126), (0\.7152), (0\.0722)\)\)`,
		want:   "0.2126|0.7152|0.0722",
	},
	{
		id:     "bright-pass-threshold-from-a-uniform",
		effect: "One backend stops reading the authored bloom threshold and bakes a constant.",
		goPat:  `bloom\.params\.x`,
		jsPat:  `params\.threshold`,
	},
	{
		// RETIRED FROM THE DIVERGENCE LEDGER on 2026-07-27. The browser adopted
		// the soft knee, so both copies now scale by excess/(excess+1) and the
		// term crossed from a recorded difference into a pinned agreement.
		//
		// The knee matters because a hard cut is discontinuous at the authored
		// dial: a pixel one part in a thousand below the threshold contributes
		// nothing and the same pixel one part above contributes its whole
		// colour, so a slow camera move snaps a highlight on. The knee's scale
		// factor falls to zero as the luminance falls to the threshold, so the
		// same move fades the highlight in.
		id:     "bright-pass-soft-knee",
		effect: "a highlight snaps on instead of fading in when one copy uses a hard cut",
		goPat:  `\(thresholdedLum \+ 1\.0\)`,
		jsPat:  `\(excess \+ 1\.0\)`,
	},
}

// brightDivergentTerms is EMPTY, and that is the intended resting state.
//
// It held one row: the native bright pass used a continuous soft knee while the
// browser used a hard cut. The browser adopted the knee on 2026-07-27, the row's
// own guard failed and named the fix, and the term moved into brightSharedTerms.
// That is the mechanism working — a ledger row cannot outlive the difference it
// records.
//
// Keep the slice and its test rather than deleting them, so the next divergence
// has somewhere to live for the length of one commit instead of going unrecorded.
var brightDivergentTerms = []divergentTerm{}

// TestBrightPassMatchesJSWebGPU pins what the two bright passes share.
//
// A PASS PROVES: both weight luminance with the same Rec.709 vector and both
// read the threshold from a uniform rather than a constant.
//
// It also proves both copies scale by the same soft knee, because the
// bright-pass-soft-knee row moved into this table when the browser adopted it.
// Both passes therefore select the same pixels from the same input.
//
// A PASS DOES NOT PROVE: that the two passes receive the same input. They do
// not. The native bright pass reads a high dynamic range target that already
// carries the background clear; the browser reads its own target. A pixel that
// crosses the threshold on one backend can sit under it on the other, and no row
// here covers that.
func TestBrightPassMatchesJSWebGPU(t *testing.T) {
	goSrc, jsSrc := brightShaderCopies(t)
	for _, problem := range checkSharedTerms(brightSharedTerms, goBrightWhere, goSrc, jsBrightWhere, jsSrc) {
		t.Error(problem)
	}
}

// TestBrightPassDivergenceLedgerIsEmptyAndTheKneeIsShared pins the resting state
// of the bright-pass ledger.
//
// The name changed on 2026-07-27, and so did the body. The old name was
// TestBrightPassKneeDivergesFromJSWebGPU, and it iterated an empty slice: it
// passed whatever either copy said, so it proved nothing while its own doc
// comment claimed a live divergence. A test that cannot fail is worse than no
// test, because the reader counts it.
//
// A PASS PROVES three things:
//
//  1. every row still recorded in the ledger is still present on both sides;
//  2. the ledger is EMPTY, so no row was added without a decision recorded
//     beside it;
//  3. the knee that used to be the one row is now pinned as an agreement in
//     brightSharedTerms, so emptying the ledger did not drop the term.
//
// A PASS DOES NOT PROVE: that the two bright passes agree on everything. They
// agree on the terms brightSharedTerms names and on nothing else.
//
// WHEN A NEW DIVERGENCE APPEARS: add its row to brightDivergentTerms with an
// effect and a verdict, and relax check 2 to name the row you added. Do not
// delete check 2; it is what stops a row from being parked here forever.
func TestBrightPassDivergenceLedgerIsEmptyAndTheKneeIsShared(t *testing.T) {
	goSrc, jsSrc := brightShaderCopies(t)
	for _, problem := range checkDivergentTerms(brightDivergentTerms, goBrightWhere, goSrc, jsBrightWhere, jsSrc) {
		t.Error(problem)
	}
	if len(brightDivergentTerms) != 0 {
		ids := make([]string, 0, len(brightDivergentTerms))
		for _, row := range brightDivergentTerms {
			ids = append(ids, row.id)
		}
		t.Errorf("brightDivergentTerms is no longer empty; it now holds %s.\n"+
			"That is allowed, and the ledger exists for it. Update this test to name the row, "+
			"and update the file comment at the top, which still says both audit differences are closed.",
			strings.Join(ids, ", "))
	}
	if !sharedTermsCover(brightSharedTerms, "bright-pass-soft-knee") {
		t.Error("brightSharedTerms no longer carries the bright-pass-soft-knee row.\n" +
			"That row is where the knee went when it stopped being a divergence. With the ledger empty and the " +
			"shared row gone, nothing pins the knee on either copy, and either side could revert to a hard cut in silence.")
	}
}

// sharedTermsCover reports whether a table carries a row with the given id. It
// lets a test assert that a term did not vanish from BOTH tables at once.
func sharedTermsCover(terms []sharedTerm, id string) bool {
	for _, term := range terms {
		if term.id == id {
			return true
		}
	}
	return false
}

func toneMapShaderCopies(t *testing.T) (goSrc, jsSrc string) {
	t.Helper()
	return normalizeWGSLSyntax(composePresentWGSL),
		normalizeWGSLSyntax(jsShaderSource(t, readJSWebGPURenderer(t), toneMapJSFragmentName))
}

func brightShaderCopies(t *testing.T) (goSrc, jsSrc string) {
	t.Helper()
	return normalizeWGSLSyntax(brightPassWGSL),
		normalizeWGSLSyntax(jsShaderSource(t, readJSWebGPURenderer(t), brightJSFragmentName))
}

// toneMapSharedGuardMutations are edits that break the tone-map agreement.
var toneMapSharedGuardMutations = []litGuardMutation{
	{
		name:    "native renderer folds the clamp mode back onto ACES",
		side:    "go",
		from:    "if (mode == 0) {\nreturn clamp(exposed, vec3f(0.0), vec3f(1.0));",
		to:      "if (mode == 9) {\nreturn clamp(exposed, vec3f(0.0), vec3f(1.0));",
		wantRow: "tonemap-mode-zero-clamps",
	},
	{
		name:    "browser swaps the Reinhard and filmic mode numbers",
		side:    "js",
		from:    "} else if (mode == 2) {\ncolor = reinhard(color);",
		to:      "} else if (mode == 3) {\ncolor = reinhard(color);",
		wantRow: "tonemap-mode-two-is-reinhard",
	},
	{
		name:    "native renderer moves the ACES shoulder",
		side:    "go",
		from:    "let a = 2.51;",
		to:      "let a = 2.61;",
		wantRow: "aces-shoulder-constant-a",
	},
	{
		name:    "browser moves the Hejl black point",
		side:    "js",
		from:    "x - vec3f(0.004)",
		to:      "x - vec3f(0.008)",
		wantRow: "filmic-black-point",
	},
	{
		name:    "native renderer applies exposure after the operator",
		side:    "go",
		from:    "let exposed = max(x * max(present.params.y, 0.0), vec3f(0.0));",
		to:      "let exposed = max(x, vec3f(0.0));",
		wantRow: "tonemap-exposure-applied-before-the-operator",
	},
	{
		name:    "browser reads the mode as a float comparison",
		side:    "js",
		from:    "let mode = i32(params.toneMapMode);",
		to:      "let mode = params.toneMapMode;",
		wantRow: "tonemap-mode-read-as-integer",
	},
}

// brightGuardMutations are edits that change the bright-pass rows.
var brightGuardMutations = []litGuardMutation{
	{
		name:    "native renderer changes the luma weights",
		side:    "go",
		from:    "vec3f(0.2126, 0.7152, 0.0722)",
		to:      "vec3f(0.3333, 0.3333, 0.3334)",
		wantRow: "bright-pass-rec709-luma-weights",
	},
	// Both knee mutations now name the SHARED row, not the divergence row. The
	// browser adopted the soft knee, so the difference these two recorded no
	// longer exists and its ledger entry retired into brightSharedTerms. Either
	// side reverting to a hard cut must break the agreement.
	{
		name:    "native renderer reverts to a hard cut",
		side:    "go",
		from:    "let soft = thresholdedLum / (thresholdedLum + 1.0);",
		to:      "let soft = step(0.0001, thresholdedLum);",
		wantRow: "bright-pass-soft-knee",
	},
	{
		name: "browser reverts to a hard cut",
		side: "js",
		// Mutate the RETURN, not the excess line. The shared row pins
		// "(excess + 1.0)", which is the knee's denominator and the thing that
		// makes the curve continuous. Changing only how excess is computed
		// leaves that denominator in place and the guard would not fire, which
		// would make this mutation prove nothing.
		from:    "return vec4f(color * (excess / (excess + 1.0)), 1.0);",
		to:      "return vec4f(color * step(0.0001, excess), 1.0);",
		wantRow: "bright-pass-soft-knee",
	},
}

// TestPostFXDriftGuardsDetectMutation proves the guards above can fail.
//
// It first confirms every table passes on the shipped sources, then applies one
// edit at a time to a copy held in memory and confirms the matching guard
// reports a problem that names the affected row. No file changes.
func TestPostFXDriftGuardsDetectMutation(t *testing.T) {
	toneGo, toneJS := toneMapShaderCopies(t)
	brightGo, brightJS := brightShaderCopies(t)

	if problems := checkSharedTerms(toneMapSharedTerms, goToneMapWhere, toneGo, jsToneMapWhere, toneJS); len(problems) != 0 {
		t.Fatalf("the tone-map table must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	if problems := checkSharedTerms(brightSharedTerms, goBrightWhere, brightGo, jsBrightWhere, brightJS); len(problems) != 0 {
		t.Fatalf("the bright-pass table must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	if problems := checkDivergentTerms(brightDivergentTerms, goBrightWhere, brightGo, jsBrightWhere, brightJS); len(problems) != 0 {
		t.Fatalf("the bright-pass ledger must pass on the shipped sources before the mutation check means anything; got %d problems:\n%s",
			len(problems), strings.Join(problems, "\n"))
	}

	t.Run("tonemap", func(t *testing.T) {
		for _, mut := range toneMapSharedGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGo, mutJS := applyLitMutation(t, mut, toneGo, toneJS)
				assertProblemNamesRow(t, checkSharedTerms(toneMapSharedTerms, goToneMapWhere, mutGo, jsToneMapWhere, mutJS), mut)
			})
		}
	})

	t.Run("bright", func(t *testing.T) {
		for _, mut := range brightGuardMutations {
			t.Run(mut.name, func(t *testing.T) {
				mutGo, mutJS := applyLitMutation(t, mut, brightGo, brightJS)
				problems := checkSharedTerms(brightSharedTerms, goBrightWhere, mutGo, jsBrightWhere, mutJS)
				problems = append(problems, checkDivergentTerms(brightDivergentTerms, goBrightWhere, mutGo, jsBrightWhere, mutJS)...)
				assertProblemNamesRow(t, problems, mut)
			})
		}
	})
}

// TestToneMapModeGuardDetectsMutation proves the mode-table guard can fail, on
// every one of the three browser tables.
//
// The guard compares a Go function against a parsed JS function, so it cannot
// mutate shader text. It mutates the parsed browser table instead, one entry per
// case, and confirms the comparison names that entry. Every case runs against
// every table, because a guard that only fires on the WebGPU copy is exactly the
// gap this change closed.
func TestToneMapModeGuardDetectsMutation(t *testing.T) {
	tables := readToneMapModeTables(t)

	for _, site := range toneMapModeTables {
		shipped := tables[site.id]
		if len(shipped) < 5 {
			t.Fatalf("%s parses to %d entries; it must hold linear, none, reinhard, filmic and a default", site.where, len(shipped))
		}
		if problems := checkToneMapModeTable(site.where, shipped); len(problems) != 0 {
			t.Fatalf("every shipped table must agree before the mutation check means anything; %s gave:\n%s",
				site.where, strings.Join(problems, "\n"))
		}
	}

	for _, site := range toneMapModeTables {
		shipped := tables[site.id]
		t.Run(site.id, func(t *testing.T) {
			for _, mut := range []struct {
				name string
				key  string
				code int
			}{
				{"stops mapping none to the clamp", "none", 1},
				{"stops mapping filmic to the Hejl curve", "filmic", 1},
				{"renumbers reinhard", "reinhard", 1},
				{"renumbers linear", "linear", 3},
				{"changes the default an unknown name takes", "", 0},
			} {
				t.Run(mut.name, func(t *testing.T) {
					mutated := map[string]int{}
					for k, v := range shipped {
						mutated[k] = v
					}
					if mutated[mut.key] == mut.code {
						t.Fatalf("mutation %q sets %q to the value it already holds, so it proves nothing", mut.name, mut.key)
					}
					mutated[mut.key] = mut.code
					problems := checkToneMapModeTable(site.where, mutated)
					if len(problems) == 0 {
						t.Fatalf("mutation %q produced no problem on %s; the guard does not cover %q", mut.name, site.where, mut.key)
					}
				})
			}
		})
	}
}
