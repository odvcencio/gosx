package motion

import (
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCompileCSSScrollMapGolden(t *testing.T) {
	p := NewProgram("demo")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	mapped := p.Map("mapped", scroll, 0, 1, 0, 1)
	p.BindCSSVariable(mapped, "#card", "--progress", "")

	css, rest, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/csscompile-scroll.golden.css")
	if err != nil {
		t.Fatal(err)
	}
	if css != string(golden) {
		t.Fatalf("compiled CSS mismatch\nwant:\n%s\ngot:\n%s", golden, css)
	}
	if rest == nil || !reflect.DeepEqual(rest.CSSCompiled, []int{0}) || len(rest.Bindings) != 1 || len(rest.Signals) != 2 {
		t.Fatalf("the full program must stay, with its compiled binding listed: %#v", rest)
	}
}

func TestCompileCSSLeavesInteractiveSignalChainsUntouched(t *testing.T) {
	for name, build := range map[string]func() *Program{
		"spring": func() *Program {
			p := NewProgram("spring")
			spring := p.Spring("spring", 0, SpringOptions{To: 1})
			p.BindStyle(spring, "#card", "opacity", "")
			return p
		},
		"hover": func() *Program {
			p := NewProgram("hover")
			hover := p.Hover("hover", "#card")
			mapped := p.Map("mapped", hover, 0, 1, 0, 1)
			p.BindStyle(mapped, "#card", "opacity", "")
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := build()
			css, rest, err := CompileCSS(p)
			if err != nil {
				t.Fatal(err)
			}
			if css != "" || !reflect.DeepEqual(rest, p) {
				t.Fatalf("unsupported chain changed: css=%q rest=%#v", css, rest)
			}
		})
	}
}

func TestCompileCSSMixedProgramListsOnlyTheCompiledBindings(t *testing.T) {
	p := NewProgram("mixed")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	mapped := p.Map("mapped", scroll, 0, 1, 0, 1)
	p.BindCSSVariable(mapped, "#card", "--progress", "")
	spring := p.Spring("spring", 0, SpringOptions{To: 1})
	p.BindStyle(spring, "#card", "opacity", "")

	css, rest, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	if css == "" || rest == nil {
		t.Fatalf("mixed program was not compiled: css=%q rest=%#v", css, rest)
	}
	if !reflect.DeepEqual(rest.CSSCompiled, []int{0}) {
		t.Fatalf("CSSCompiled = %v, want only the scroll binding [0]", rest.CSSCompiled)
	}
	if len(rest.Bindings) != 2 || len(rest.Signals) != len(p.Signals) {
		t.Fatalf("the full program must stay so browsers without scroll timelines keep every binding: %#v", rest)
	}
	if len(p.CSSCompiled) != 0 {
		t.Fatal("CompileCSS must not modify its input")
	}
}

func TestCompileCSSOnlyCompilesPageScroll(t *testing.T) {
	for name, build := range map[string]func() *Program{
		"element visibility": func() *Program {
			p := NewProgram("vis")
			v := p.Visibility("v", "#card")
			p.BindStyle(p.Map("m", v, 0, 1, 0, 1), "#card", "opacity", "")
			return p
		},
		"container scroll": func() *Program {
			p := NewProgram("container")
			c := p.ScrollProgress("c", "#pane", AxisY)
			p.BindCSSVariable(p.Map("m", c, 0, 1, 0, 1), "#card", "--p", "")
			return p
		},
		"camera target": func() *Program {
			p := NewProgram("camera")
			s := p.ScrollProgress("s", "", AxisY)
			p.BindCamera(p.Map("m", s, 0, 1, 8, 5), "#scene", "position.z")
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			css, rest, err := CompileCSS(build())
			if err != nil {
				t.Fatal(err)
			}
			if css != "" || rest == nil || len(rest.CSSCompiled) != 0 {
				t.Fatalf("css=%q rest=%#v, want nothing compiled", css, rest)
			}
		})
	}
}

func TestCompileCSSRejectsOutOfRangeCompiledIndexes(t *testing.T) {
	p := NewProgram("bad")
	p.Time("t")
	p.CSSCompiled = []int{3}
	if _, err := p.Marshal(); err == nil {
		t.Fatal("Marshal must reject a cssCompiled index outside the bindings")
	}
}

func TestCompileCSSUnitsReducedMotionAndVisibleFallback(t *testing.T) {
	p := NewProgram("units")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	mapped := p.Map("mapped", scroll, 0, 1, 0, 100)
	p.BindCSSVariable(mapped, "#card", "--offset", "px")
	p.BindStyle(mapped, "#card", "width", "px")
	css, _, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"@media (prefers-reduced-motion: no-preference)", "@supports (animation-timeline: scroll())", "width: 0px", "width: 100px", "--offset: 0px", "--offset: 100px", "@property --offset", "syntax: '<length>'"} {
		if !strings.Contains(css, expected) {
			t.Fatalf("expected %q in CSS:\n%s", expected, css)
		}
	}
	if strings.Contains(css, "opacity: 0") || strings.Contains(css, "visibility: hidden") {
		t.Fatalf("compiler generated a hidden base state:\n%s", css)
	}
}

func TestCompileCSSKeyframesMatchMotionEvaluationAtElevenPoints(t *testing.T) {
	const from, to = -2.5, 7.25
	p := NewProgram("samples")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	mapped := p.Map("mapped", scroll, 0, 1, from, to)
	p.BindCSSVariable(mapped, "#card", "--sample", "")
	css, _, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}

	// A two-key linear motion track is the Go evaluator equivalent of Map's
	// affine interpolation over source progress [0,1].
	track := &Timeline{Children: []Positioned{{Track: &Track{
		Keys: []Key{
			{T: 0, Value: ScalarV(from)},
			{T: 1, Value: ScalarV(to)},
		},
	}}}}
	for i := 0; i <= 10; i++ {
		progress := float64(i) / 10
		var out WriteBuf
		Eval(track, progress, Policy{}, &out)
		if len(out.Writes()) < 4 {
			t.Fatalf("motion evaluator emitted no scalar at progress %.1f", progress)
		}
		want := out.Writes()[3]
		got, ok := cssKeyframeValue(css, progress)
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Fatalf("progress %.1f: compiled=%v (found %v), motion eval=%.12g", progress, got, ok, want)
		}
	}
}

func cssKeyframeValue(css string, progress float64) (float64, bool) {
	start := strings.Index(css, "@keyframes ")
	if start < 0 {
		return 0, false
	}
	body := css[start:]
	var first, last float64
	startStop := strings.Index(body, "0% { --sample: ")
	if startStop < 0 {
		return 0, false
	}
	if _, err := fmt.Sscanf(body[startStop:], "0%% { --sample: %f; }", &first); err != nil {
		return 0, false
	}
	endStop := strings.Index(body, "100% { --sample: ")
	if endStop < 0 {
		return 0, false
	}
	if _, err := fmt.Sscanf(body[endStop:], "100%% { --sample: %f; }", &last); err != nil {
		return 0, false
	}
	return first + (last-first)*progress, true
}

// A CSS variable animated without a registered syntax flips at the midpoint of
// the animation instead of interpolating, so every compiled variable must be
// registered with @property, and a unit that has no syntax must stay in JS.
func TestCompileCSSRegistersCustomPropertiesSoTheyInterpolate(t *testing.T) {
	for _, tc := range []struct{ unit, syntax string }{
		{"", "<number>"}, {"px", "<length>"}, {"rem", "<length>"}, {"%", "<percentage>"}, {"deg", "<angle>"}, {"ms", "<time>"},
	} {
		p := NewProgram("props")
		scroll := p.ScrollProgress("scroll", "", AxisY)
		p.BindCSSVariable(p.Map("m", scroll, 0, 1, 2, 9), "#card", "--v", tc.unit)
		css, rest, err := CompileCSS(p)
		if err != nil {
			t.Fatal(err)
		}
		want := "@property --v {\n    syntax: '" + tc.syntax + "';\n    inherits: true;\n    initial-value: 2" + tc.unit + ";\n  }"
		if !strings.Contains(css, want) || len(rest.CSSCompiled) != 1 {
			t.Fatalf("unit %q: want %q registered; css=\n%s", tc.unit, want, css)
		}
		if strings.Count(css, "@property --v") != 1 {
			t.Fatalf("unit %q: the property must be registered once", tc.unit)
		}
	}
}

func TestCompileCSSRegistersAVariableOnceForTwoBindings(t *testing.T) {
	p := NewProgram("dup")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	m := p.Map("m", scroll, 0, 1, 0, 10)
	p.BindCSSVariable(m, "#a", "--v", "px")
	p.BindCSSVariable(m, "#b", "--v", "px")
	css, rest, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(css, "@property --v") != 1 || len(rest.CSSCompiled) != 2 {
		t.Fatalf("want one @property and two compiled bindings; compiled=%v css=\n%s", rest.CSSCompiled, css)
	}
}

// Several fixed effects on one element must all apply. Each compiled effect is
// its own animation, so they go in one rule as lists; separate rules would let
// the last animation-name declaration win and silently drop the others, while
// the runtime skips every compiled binding.
func TestCompileCSSListsEveryAnimationOfOneElementInOneRule(t *testing.T) {
	p := NewProgram("multi")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	fade := p.Map("fade", scroll, 0, 1, 1, 0)
	grow := p.Map("grow", scroll, 0, 1, 100, 300)
	p.BindStyle(fade, "#card", "opacity", "")
	p.BindStyle(grow, "#card", "width", "px")
	p.BindStyle(fade, "#other", "opacity", "")
	css, rest, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.CSSCompiled) != 3 {
		t.Fatalf("CSSCompiled = %v, want all three bindings", rest.CSSCompiled)
	}
	card := ":where([data-gosx-motion-scope='multi']) :is(#card) {"
	if strings.Count(css, card) != 1 {
		t.Fatalf("want exactly one rule for #card:\n%s", css)
	}
	if strings.Count(css, "animation-name:") != 2 {
		t.Fatalf("want one animation-name declaration per element (two elements):\n%s", css)
	}
	rule := css[strings.Index(css, card):]
	rule = rule[:strings.Index(rule, "}")]
	for _, want := range []string{"animation-name: gosx-motion-multi-0-", "animation-timeline: scroll(root block), scroll(root block)", "animation-duration: 1ms, 1ms", "animation-fill-mode: both, both"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("rule for #card lacks %q:\n%s", want, rule)
		}
	}
	if !strings.Contains(rule, ", gosx-motion-multi-1-") {
		t.Fatalf("rule for #card must list both animations:\n%s", rule)
	}
}

// Bindings that would animate the same property of the same element cannot
// both be CSS animations, and JavaScript combines them (transform.x and
// transform.y both become one translate), so all of them stay in JavaScript.
func TestCompileCSSLeavesSamePropertyConflictsToJavaScript(t *testing.T) {
	p := NewProgram("conflict")
	scroll := p.ScrollProgress("scroll", "", AxisY)
	x := p.Map("x", scroll, 0, 1, 0, 40)
	y := p.Map("y", scroll, 0, 1, 0, 80)
	p.BindStyle(x, "#card", "transform.x", "px")
	p.BindStyle(y, "#card", "transform.y", "px")
	p.BindCSSVariable(x, "#card", "--p", "px")
	p.BindCSSVariable(y, "#card", "--p", "px")
	css, rest, err := CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	if css != "" || len(rest.CSSCompiled) != 0 {
		t.Fatalf("conflicting bindings must stay in JavaScript: css=%q compiled=%v", css, rest.CSSCompiled)
	}

	// A conflict on one element does not stop other elements from compiling.
	p.BindStyle(x, "#free", "opacity", "")
	css, rest, err = CompileCSS(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rest.CSSCompiled, []int{4}) || !strings.Contains(css, "#free") || strings.Contains(css, "#card") {
		t.Fatalf("only the conflict-free binding should compile: compiled=%v css=\n%s", rest.CSSCompiled, css)
	}
}
