package motion

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
)

// CompileCSS compiles fixed page-scroll-driven style and CSS-variable bindings
// into progressive CSS: a Map or Clamp chain rooted at the page scroll position
// (empty selector) becomes @keyframes driven by animation-timeline: scroll().
// Compiled rules are scoped to the element carrying
// data-gosx-motion-scope=<Program.ID>; callers must mark the rendered scope
// element with that attribute.
//
// The returned Program is the full program with Program.CSSCompiled listing the
// bindings that now have CSS. The browser runtime skips those bindings, and the
// signals only they used, when the browser supports scroll timelines, and keeps
// them as the JavaScript fallback when it does not, so authors get the same
// motion everywhere. Element visibility, container scroll, pointer, hover,
// spring, tween, time and every scene, camera and material target stay in
// JavaScript.
func CompileCSS(p *Program) (css string, rest *Program, err error) {
	if p == nil {
		return "", nil, errors.New("motion: cannot compile a nil program")
	}
	if _, err := p.Marshal(); err != nil {
		return "", nil, err
	}
	if len(p.Bindings) == 0 {
		if len(p.Signals) == 0 && len(p.Pins) == 0 {
			return "", nil, nil
		}
		copy := cloneMotionProgram(p)
		return "", &copy, nil
	}

	byID := make(map[SignalRef]SignalSpec, len(p.Signals))
	for _, signal := range p.Signals {
		byID[SignalRef(signal.ID)] = signal
	}
	compiled := make(map[int]compiledMotionBinding)
	for i, binding := range p.Bindings {
		if binding.Target != BindingStyle && binding.Target != BindingCSSVariable {
			continue
		}
		if !validCSSSelector(binding.Selector) {
			continue
		}
		chain, source, ok := cssSignalChain(binding.Signal, byID)
		if !ok || source.Source == nil || source.Kind != SignalScroll || source.Source.Selector != "" {
			continue
		}
		if len(chain) == 0 {
			continue
		}
		stops, ok := compileSignalStops(chain)
		if !ok {
			continue
		}
		// An unregistered custom property animates discretely (it flips at the
		// midpoint), so a CSS variable only compiles when its unit maps to a
		// registerable syntax.
		if binding.Target == BindingCSSVariable {
			if _, known := cssPropertySyntax(binding.Unit); !known {
				continue
			}
		}
		compiled[i] = compiledMotionBinding{binding: binding, source: source, stops: stops}
	}
	// Two compiled bindings that animate the same CSS property of the same
	// element would overwrite each other (the later animation wins), while
	// JavaScript combines them (for example transform.x and transform.y both
	// become the translate property). Leave every binding in such a group to
	// JavaScript so no effect is lost.
	propertyKey := func(b Binding) string {
		property, _, _ := cssDeclaration(b, 0)
		return b.Selector + "\x00" + property
	}
	propertyCount := make(map[string]int, len(compiled))
	for _, item := range compiled {
		propertyCount[propertyKey(item.binding)]++
	}
	for i, item := range compiled {
		if propertyCount[propertyKey(item.binding)] > 1 {
			delete(compiled, i)
		}
	}
	if len(compiled) == 0 {
		copy := cloneMotionProgram(p)
		return "", &copy, nil
	}

	var body strings.Builder
	type ruleItem struct{ name, timeline string }
	ruleOrder := make([]string, 0, len(compiled))
	ruleItems := make(map[string][]ruleItem, len(compiled))
	var registered strings.Builder
	registeredNames := make(map[string]bool)
	for i := range p.Bindings {
		item, ok := compiled[i]
		if !ok {
			continue
		}
		if item.binding.Target == BindingCSSVariable && !registeredNames[item.binding.Property] {
			registeredNames[item.binding.Property] = true
			syntax, _ := cssPropertySyntax(item.binding.Unit)
			fmt.Fprintf(&registered, "  @property %s {\n    syntax: '%s';\n    inherits: true;\n    initial-value: %s%s;\n  }\n",
				item.binding.Property, syntax, formatCSSFloat(item.stops[0].value), item.binding.Unit)
		}
		name := cssAnimationName(p.ID, i)
		fmt.Fprintf(&body, "    @keyframes %s {\n", name)
		for _, stop := range item.stops {
			property, value, valid := cssDeclaration(item.binding, stop.value)
			if !valid {
				continue
			}
			fmt.Fprintf(&body, "      %s%% { %s: %s; }\n", formatCSSFloat(stop.at*100), property, value)
		}
		body.WriteString("    }\n")
		selector := scopedCSSSelector(p.ID, item.binding.Selector)
		if _, seen := ruleItems[selector]; !seen {
			ruleOrder = append(ruleOrder, selector)
		}
		ruleItems[selector] = append(ruleItems[selector], ruleItem{name: name, timeline: "scroll(root " + cssAxis(item.source.Source.Axis) + ")"})
	}
	// One rule per element lists every animation, so several fixed effects on
	// one element all apply instead of the last declaration winning.
	for _, selector := range ruleOrder {
		items := ruleItems[selector]
		names := make([]string, len(items))
		timelines := make([]string, len(items))
		durations := make([]string, len(items))
		easings := make([]string, len(items))
		fills := make([]string, len(items))
		for i, item := range items {
			names[i], timelines[i], durations[i], easings[i], fills[i] = item.name, item.timeline, "1ms", "linear", "both"
		}
		fmt.Fprintf(&body, "    %s {\n", selector)
		fmt.Fprintf(&body, "      animation-name: %s;\n", strings.Join(names, ", "))
		fmt.Fprintf(&body, "      animation-timeline: %s;\n", strings.Join(timelines, ", "))
		fmt.Fprintf(&body, "      animation-duration: %s;\n", strings.Join(durations, ", "))
		fmt.Fprintf(&body, "      animation-timing-function: %s;\n", strings.Join(easings, ", "))
		fmt.Fprintf(&body, "      animation-fill-mode: %s;\n", strings.Join(fills, ", "))
		body.WriteString("    }\n")
	}
	if body.Len() == 0 {
		copy := cloneMotionProgram(p)
		return "", &copy, nil
	}
	css = "@supports (animation-timeline: scroll()) {\n" + registered.String() + "  @media (prefers-reduced-motion: no-preference) {\n" + body.String() + "  }\n}\n"

	// The CSS is embedded verbatim in a style element, so it must never be able
	// to close it or open a comment, whatever the program ID or selectors hold.
	// (The @property syntax strings such as '<length>' are safe: only "</" and
	// "<!" matter inside a style element.)
	if lower := strings.ToLower(css); strings.Contains(lower, "</") || strings.Contains(lower, "<!") {
		copy := cloneMotionProgram(p)
		return "", &copy, nil
	}
	indexes := make([]int, 0, len(compiled))
	for i := range p.Bindings {
		if _, ok := compiled[i]; ok {
			indexes = append(indexes, i)
		}
	}
	copy := cloneMotionProgram(p)
	copy.CSSCompiled = indexes
	return css, &copy, nil
}

type compiledMotionBinding struct {
	binding Binding
	source  SignalSpec
	stops   []cssStop
}

type cssStop struct {
	at    float64
	value float64
}

// cssSignalChain follows only derived fixed-function signals back to a source.
// Its result is ordered source-first.
func cssSignalChain(ref SignalRef, signals map[SignalRef]SignalSpec) ([]SignalSpec, SignalSpec, bool) {
	var reverse []SignalSpec
	seen := make(map[SignalRef]bool)
	for {
		if seen[ref] {
			return nil, SignalSpec{}, false
		}
		seen[ref] = true
		signal, ok := signals[ref]
		if !ok {
			return nil, SignalSpec{}, false
		}
		switch signal.Kind {
		case SignalScroll, SignalVisibility:
			reverseChain(reverse)
			return reverse, signal, true
		case SignalMap, SignalClamp:
			reverse = append(reverse, signal)
			if signal.Input == "" {
				return nil, SignalSpec{}, false
			}
			ref = signal.Input
		default:
			return nil, SignalSpec{}, false
		}
	}
}

func reverseChain(chain []SignalSpec) {
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
}

func compileSignalStops(chain []SignalSpec) ([]cssStop, bool) {
	points := []float64{0, 1}
	processed := make([]SignalSpec, 0, len(chain))
	for _, signal := range chain {
		if signal.Kind == SignalClamp {
			var extra []float64
			for i := 0; i+1 < len(points); i++ {
				x0, x1 := points[i], points[i+1]
				y0 := evalSignalOps(x0, processed)
				y1 := evalSignalOps(x1, processed)
				for _, threshold := range []float64{signal.Min, signal.Max} {
					if (threshold > math.Min(y0, y1)) && (threshold < math.Max(y0, y1)) && y0 != y1 {
						extra = append(extra, x0+(threshold-y0)*(x1-x0)/(y1-y0))
					}
				}
			}
			points = append(points, extra...)
			sort.Float64s(points)
			points = uniqueCSSPoints(points)
		}
		processed = append(processed, signal)
	}
	stops := make([]cssStop, 0, len(points))
	for _, point := range points {
		value := evalSignalOps(point, processed)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, false
		}
		stops = append(stops, cssStop{at: point, value: value})
	}
	return stops, true
}

func evalSignalOps(value float64, ops []SignalSpec) float64 {
	for _, signal := range ops {
		switch signal.Kind {
		case SignalMap:
			if signal.To == signal.From {
				return signal.Min
			}
			value = signal.Min + (value-signal.From)*(signal.Max-signal.Min)/(signal.To-signal.From)
		case SignalClamp:
			value = math.Max(signal.Min, math.Min(signal.Max, value))
		}
	}
	return value
}

func uniqueCSSPoints(points []float64) []float64 {
	unique := points[:0]
	for _, point := range points {
		if len(unique) == 0 || math.Abs(unique[len(unique)-1]-point) > 1e-12 {
			unique = append(unique, point)
		}
	}
	return unique
}

func cssDeclaration(binding Binding, value float64) (property, rendered string, ok bool) {
	property = binding.Property
	unit := binding.Unit
	if binding.Target == BindingStyle {
		switch property {
		case "backgroundColor":
			property = "background-color"
		case "minWidth":
			property = "min-width"
		case "maxWidth":
			property = "max-width"
		case "minHeight":
			property = "min-height"
		case "maxHeight":
			property = "max-height"
		case "marginTop":
			property = "margin-top"
		case "marginRight":
			property = "margin-right"
		case "marginBottom":
			property = "margin-bottom"
		case "marginLeft":
			property = "margin-left"
		case "paddingTop":
			property = "padding-top"
		case "paddingRight":
			property = "padding-right"
		case "paddingBottom":
			property = "padding-bottom"
		case "paddingLeft":
			property = "padding-left"
		case "transform.x":
			property = "translate"
			return property, formatCSSFloat(value) + unit + " 0 0", true
		case "transform.y":
			property = "translate"
			return property, "0 " + formatCSSFloat(value) + unit + " 0", true
		case "transform.z":
			property = "translate"
			return property, "0 0 " + formatCSSFloat(value) + unit, true
		case "transform.scale":
			property = "scale"
		case "transform.rotate", "transform.rotation":
			property = "rotate"
		}
	}
	if binding.Target == BindingStyle && !strings.Contains(property, ".") {
		property = cssKebabCase(property)
	}
	return property, formatCSSFloat(value) + unit, true
}

func cssKebabCase(property string) string {
	var out strings.Builder
	for _, r := range property {
		if r >= 'A' && r <= 'Z' {
			out.WriteByte('-')
			r += 'a' - 'A'
		}
		out.WriteRune(r)
	}
	return out.String()
}

func scopedCSSSelector(programID, selector string) string {
	return ":where([data-gosx-motion-scope='" + cssStringEscape(programID) + "']) :is(" + selector + ")"
}

func validCSSSelector(selector string) bool {
	return strings.TrimSpace(selector) != "" && strings.TrimSpace(selector) == selector && !strings.ContainsAny(selector, "{}")
}

func cssStringEscape(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch r {
		case '\\', '\'':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\n':
			out.WriteString(`\a `)
		case '\r':
			out.WriteString(`\d `)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func cssSlug(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			out.WriteRune(r)
		} else if out.Len() == 0 || !strings.HasSuffix(out.String(), "-") {
			out.WriteByte('-')
		}
	}
	if out.Len() == 0 {
		return "program"
	}
	return strings.Trim(out.String(), "-")
}

func cssAnimationName(programID string, binding int) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(programID))
	return fmt.Sprintf("gosx-motion-%s-%d-%x", cssSlug(programID), binding, hash.Sum32())
}

func cssAxis(axis MotionAxis) string {
	if axis == AxisX {
		return "inline"
	}
	return "block"
}

func formatCSSFloat(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func cloneMotionProgram(p *Program) Program {
	copy := *p
	copy.Signals = append([]SignalSpec(nil), p.Signals...)
	copy.Bindings = append([]Binding(nil), p.Bindings...)
	copy.Pins = append([]PinBinding(nil), p.Pins...)
	return copy
}

// cssPropertySyntax maps a binding unit to the @property syntax that lets the
// browser interpolate the variable.
func cssPropertySyntax(unit string) (string, bool) {
	switch unit {
	case "":
		return "<number>", true
	case "px", "em", "rem", "vh", "vw", "vmin", "vmax":
		return "<length>", true
	case "%":
		return "<percentage>", true
	case "deg", "rad", "turn":
		return "<angle>", true
	case "ms", "s":
		return "<time>", true
	}
	return "", false
}
