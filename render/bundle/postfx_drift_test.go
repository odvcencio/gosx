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
// The audit of 2026-07-26 found two differences here. One is closed and now has
// a shared table below. One stays open and has a ledger row that names the
// browser edit that closes it.

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

// TestToneMapModeTableMatchesJSWebGPU pins the string-to-number table that
// decides which branch of the shader runs.
//
// A PASS PROVES: toneMapModeCode in Go and sceneWebGPUToneMapMode in the browser
// return the same number for every name in toneMapNameTable, including the
// default that an unknown name and the empty string take.
//
// A PASS DOES NOT PROVE: that the browser WebGL path agrees. It has a third
// table, scenePostToneMapMode in 16-scene-webgl.js, whose post-chain numbers
// match this one, and a fourth, sceneToneMapMode, whose material-shader numbers
// do not. Those live outside the WebGPU pair this guard reads.
func TestToneMapModeTableMatchesJSWebGPU(t *testing.T) {
	for _, problem := range checkToneMapModeTable(jsToneMapModeTable(t, readJSWebGPURenderer(t))) {
		t.Error(problem)
	}
}

// checkToneMapModeTable returns one problem per name the two tables map
// differently. It takes the browser table as data so a self test can feed it a
// mutated copy and prove the guard fires.
func checkToneMapModeTable(jsTable map[string]int) []string {
	var problems []string
	for _, name := range toneMapNameTable {
		want, ok := jsTable[name]
		if !ok {
			want = jsTable[""]
		}
		if got := int(toneMapModeCode(name)); got != want {
			problems = append(problems, fmt.Sprintf("tone-map name %q: %s returns %d, %s returns %d.\n"+
				"Both tables serve one authored string, so change both or neither.",
				name, goModeWhere, got, jsModeWhere, want))
		}
	}
	return problems
}

// jsToneMapModeTable reads sceneWebGPUToneMapMode and returns the name-to-number
// mapping it implements. The empty-string key holds the default the function
// returns when no name matches.
//
// The function is small and regular: a run of `if (normalized === "x" ...)
// return N;` lines followed by one bare `return N;`. This reader fails instead
// of guessing when it finds neither.
func jsToneMapModeTable(t *testing.T, file string) map[string]int {
	t.Helper()
	body := jsFunctionBody(t, file, toneMapJSFuncName)
	table := map[string]int{}
	conditional := regexp.MustCompile(`if \(([^)]*)\) return (\d+);`)
	name := regexp.MustCompile(`"([a-zA-Z-]*)"`)
	for _, match := range conditional.FindAllStringSubmatch(body, -1) {
		code := 0
		if _, err := fmt.Sscanf(match[2], "%d", &code); err != nil {
			t.Fatalf("%s returns %q, which is not a number", jsModeWhere, match[2])
		}
		names := name.FindAllStringSubmatch(match[1], -1)
		if len(names) == 0 {
			t.Fatalf("%s has a branch this guard cannot read: %q", jsModeWhere, match[0])
		}
		for _, n := range names {
			table[n[1]] = code
		}
	}
	if len(table) == 0 {
		t.Fatalf("%s maps no name at all; the guard read the wrong function or the shape changed", jsModeWhere)
	}
	fallback := regexp.MustCompile(`\n\s*return (\d+);`)
	tail := fallback.FindAllStringSubmatch(body, -1)
	if len(tail) == 0 {
		t.Fatalf("%s has no default return; the guard cannot tell what an unknown name maps to", jsModeWhere)
	}
	last := 0
	if _, err := fmt.Sscanf(tail[len(tail)-1][1], "%d", &last); err != nil {
		t.Fatalf("%s default return %q is not a number", jsModeWhere, tail[len(tail)-1][1])
	}
	table[""] = last
	return table
}

// jsFunctionBody returns the text between the braces of `function NAME(`.
func jsFunctionBody(t *testing.T, file, name string) string {
	t.Helper()
	needle := "function " + name + "("
	start := strings.Index(file, needle)
	if start < 0 {
		t.Fatalf("function %s not found in %s. Was it renamed? Update this guard together with the rename.", name, jsWebGPURendererFile)
	}
	open := strings.IndexByte(file[start:], '{')
	if open < 0 {
		t.Fatalf("function %s in %s has no body", name, jsWebGPURendererFile)
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
	t.Fatalf("function %s in %s has no closing brace", name, jsWebGPURendererFile)
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
// A PASS DOES NOT PROVE: that the two passes select the same pixels. They do
// not. TestBrightPassKneeDivergesFromJSWebGPU records why.
func TestBrightPassMatchesJSWebGPU(t *testing.T) {
	goSrc, jsSrc := brightShaderCopies(t)
	for _, problem := range checkSharedTerms(brightSharedTerms, goBrightWhere, goSrc, jsBrightWhere, jsSrc) {
		t.Error(problem)
	}
}

// TestBrightPassKneeDivergesFromJSWebGPU pins the open difference in the bright
// pass.
//
// A PASS PROVES: the native copy still scales by a soft knee and the browser
// copy still cuts hard. An edit to either side fails here and forces a fresh
// decision.
//
// A PASS DOES NOT PROVE: that the difference is acceptable. Read the verdict on
// the row. It names the browser edit that closes it.
func TestBrightPassKneeDivergesFromJSWebGPU(t *testing.T) {
	goSrc, jsSrc := brightShaderCopies(t)
	for _, problem := range checkDivergentTerms(brightDivergentTerms, goBrightWhere, goSrc, jsBrightWhere, jsSrc) {
		t.Error(problem)
	}
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

// TestToneMapModeGuardDetectsMutation proves the mode-table guard can fail.
//
// The guard compares a Go function against a parsed JS function, so it cannot
// mutate shader text. It mutates the parsed browser table instead, one entry per
// case, and confirms the comparison names that entry.
func TestToneMapModeGuardDetectsMutation(t *testing.T) {
	shipped := jsToneMapModeTable(t, readJSWebGPURenderer(t))
	if len(shipped) < 5 {
		t.Fatalf("the parsed browser table holds %d entries; it must hold linear, none, reinhard, filmic and a default", len(shipped))
	}
	if problems := checkToneMapModeTable(shipped); len(problems) != 0 {
		t.Fatalf("the shipped tables must agree before the mutation check means anything; got:\n%s",
			strings.Join(problems, "\n"))
	}

	for _, mut := range []struct {
		name string
		key  string
		code int
	}{
		{"browser stops mapping none to the clamp", "none", 1},
		{"browser renumbers reinhard", "reinhard", 1},
		{"browser renumbers filmic", "filmic", 2},
		{"browser changes the default an unknown name takes", "", 0},
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
			problems := checkToneMapModeTable(mutated)
			if len(problems) == 0 {
				t.Fatalf("mutation %q produced no problem; the guard does not cover %q", mut.name, mut.key)
			}
		})
	}
}
