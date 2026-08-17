package ir

import (
	"fmt"
	"go/ast"
	"go/parser"
	"regexp"
	"strings"
	"time"
)

// Diagnostic represents a validation error or warning.
type Diagnostic struct {
	Span    Span
	Message string
	Hint    string

	// Code is an optional rule identifier, for example a consumer-defined
	// catalog code such as "EM001". Built-in gosx diagnostics leave Code
	// empty; it exists so a diagnostic sink shared with a third-party
	// checker (see strictcheck.Lint) can surface that checker's own rule
	// codes without a second, differently-shaped diagnostic type.
	Code string
}

func (d Diagnostic) String() string {
	message := d.Message
	if strings.TrimSpace(message) == "" {
		// gosx#185 n3: an empty Message would otherwise print as a bare
		// "line:col: " with nothing readable after it -- most likely from a
		// hand-built Diagnostic (a render profile's Validate hook, for
		// example) that forgot to set one, so say so instead of staying
		// silent about which diagnostic is the empty one.
		message = "(no message)"
	}
	s := ""
	if d.Span.File != "" {
		// gosx#186 B2: a multi-file check run (CheckPackage, CheckTree, or a
		// third-party strictcheck.Lint spanning several files) is otherwise
		// unattributable -- every diagnostic prints the same bare line:col
		// with no way to tell which file it came from. Built-in gosx and
		// strictcheck diagnostics leave Span.File empty today, so this only
		// changes output for a diagnostic that set it.
		s += d.Span.File + ":"
	}
	s += fmt.Sprintf("%d:%d: ", d.Span.StartLine, d.Span.StartCol)
	if d.Code != "" {
		s += d.Code + ": "
	}
	s += message
	if d.Hint != "" {
		s += " (" + d.Hint + ")"
	}
	return s
}

// Validate runs validation passes over the IR program.
// Returns diagnostics (errors and warnings). If any error is returned,
// the program should not be rendered.
func Validate(prog *Program) []Diagnostic {
	v := &validator{prog: prog}
	v.validate()
	return v.diags
}

type validator struct {
	prog  *Program
	diags []Diagnostic
}

func (v *validator) errorf(span Span, format string, args ...any) {
	v.diags = append(v.diags, Diagnostic{
		Span:    span,
		Message: fmt.Sprintf(format, args...),
	})
}

func (v *validator) validate() {
	componentNames := make(map[string]Span, len(v.prog.Components))
	// Validate each component
	for i := range v.prog.Components {
		component := &v.prog.Components[i]
		if first, exists := componentNames[component.Name]; exists {
			v.diags = append(v.diags, Diagnostic{
				Span:    component.Span,
				Message: fmt.Sprintf("duplicate component name %q", component.Name),
				Hint:    fmt.Sprintf("the first declaration is at %d:%d; component names must be unique within a .gsx file", first.StartLine, first.StartCol),
			})
		} else {
			componentNames[component.Name] = component.Span
		}
		v.validateComponent(component)
	}

	// Validate all nodes
	for i := range v.prog.Nodes {
		v.validateNode(&v.prog.Nodes[i])
	}
}

func (v *validator) validateComponent(comp *Component) {
	// Component names must start with uppercase
	if len(comp.Name) > 0 && (comp.Name[0] < 'A' || comp.Name[0] > 'Z') {
		v.errorf(comp.Span, "component %q must start with an uppercase letter", comp.Name)
	}

	// Root node must exist
	if int(comp.Root) >= len(v.prog.Nodes) {
		v.errorf(comp.Span, "component %q references invalid root node", comp.Name)
	}

	// For island components, validate expression subset
	if comp.IsIsland {
		v.diags = append(v.diags, validateIslandExprs(v.prog, comp)...)
	}

	// For engine surface components, run surface-specific validation.
	if comp.IsEngine && comp.EngineKind == "surface" {
		v.diags = append(v.diags, validateEngineSurface(v.prog, comp)...)
	}

	// Legacy (non-strict, non-island) components render through the
	// file-router's reflective interpreter (route/fileeval.go), which has no
	// static types for `any` data params. gosx#164: `.length` there resolves
	// to nil on every target — a slice has no such field or method — so
	// `cond={data.picks.length == 0}` compares nil to 0 and silently renders
	// neither branch. Strict and island components carry their own
	// type-checked or type-restricted expression paths and do not need this
	// rule.
	if comp.Syntax != ComponentSyntaxStrict && !comp.IsIsland {
		v.diags = append(v.diags, validateLegacyTemplateExprs(v.prog, comp)...)
	}
}

func (v *validator) validateNode(node *Node) {
	switch node.Kind {
	case NodeElement:
		v.validateElement(node)
	case NodeComponent:
		v.validateComponentRef(node)
	case NodeExpr:
		v.validateExpr(node)
	}

	// Validate children references
	for _, childID := range node.Children {
		if int(childID) >= len(v.prog.Nodes) {
			v.errorf(node.Span, "node references invalid child %d", childID)
		}
	}
}

func (v *validator) validateElement(node *Node) {
	if node.Tag == "" {
		v.errorf(node.Span, "element node has empty tag name")
	}

	// Validate attributes
	for _, attr := range node.Attrs {
		v.validateAttr(node, &attr)
	}
}

func (v *validator) validateComponentRef(node *Node) {
	if node.Tag == "" {
		v.errorf(node.Span, "component reference has empty name")
	}

	// Event handlers on components should reference valid action names
	for _, attr := range node.Attrs {
		if attr.IsEvent && attr.Kind == AttrExpr && attr.Expr == "" {
			v.errorf(node.Span, "event handler %q has empty expression", attr.Name)
		}
		// gosx#178 review finding m14: a component reference can carry the
		// same data-gosx-countdown-* attributes an element can (for example
		// a builtin like <Form> or a component that forwards them onto its
		// own root element) — route static values through the same
		// countdown checks an element gets, so a component reference is not
		// a blind spot for the exact same bad-value class validateAttr
		// already catches on plain elements.
		if attr.Kind == AttrStatic {
			v.validateStaticCountdownAttr(node, &attr)
		}
	}
}

func (v *validator) validateExpr(node *Node) {
	if strings.TrimSpace(node.Text) == "" {
		v.errorf(node.Span, "expression hole is empty")
	}
}

func (v *validator) validateAttr(node *Node, attr *Attr) {
	switch attr.Kind {
	case AttrExpr:
		if strings.TrimSpace(attr.Expr) == "" {
			v.errorf(node.Span, "attribute %q has empty expression", attr.Name)
		}
	case AttrSpread:
		if strings.TrimSpace(attr.Expr) == "" {
			v.errorf(node.Span, "spread attribute has empty expression")
		}
	case AttrStatic:
		v.validateStaticCountdownAttr(node, attr)
	}
}

// The five data-gosx-countdown-* attributes with a fixed value vocabulary
// (gosx#178). These string values are pinned against server/navigation_contract.go
// and client/runtime/host/navigation.ts by server/navigation_contract_countdown_test.go
// (gosx#178 review finding m11).
const (
	countdownInstantAttr = "data-gosx-countdown"
	countdownFormatAttr  = "data-gosx-countdown-format"
	countdownSegmentAttr = "data-gosx-countdown-segment"
	countdownWarnAttr    = "data-gosx-countdown-warn"
	countdownThenAttr    = "data-gosx-countdown-then"
)

// countdownWarnIntegerPattern and countdownWarnDurationPattern mirror the
// small declarative duration subset parseCountdownWarnSeconds accepts in
// client/runtime/host/navigation.ts: a bare non-negative integer as whole
// seconds, or whole hour/minute/second components combined in one value
// (for example "30s" or "1m30s"). This is not a general Go duration parser
// — see parseRevalidateInterval's own comment in navigation.ts for the
// same small-subset rationale applied to data-gosx-revalidate-interval.
var (
	countdownWarnIntegerPattern  = regexp.MustCompile(`^[0-9]+$`)
	countdownWarnDurationPattern = regexp.MustCompile(`^(?:([0-9]+)h)?(?:([0-9]+)m)?(?:([0-9]+)s)?$`)
)

// isValidCountdownWarnValue reports whether value parses under the small
// declarative duration subset described above.
func isValidCountdownWarnValue(value string) bool {
	if countdownWarnIntegerPattern.MatchString(value) {
		return true
	}
	m := countdownWarnDurationPattern.FindStringSubmatch(value)
	return m != nil && (m[1] != "" || m[2] != "" || m[3] != "")
}

// validateStaticCountdownAttr flags a static data-gosx-countdown-* value
// outside its documented vocabulary: an instant that is not valid RFC3339,
// a format outside the two render modes the countdown runtime supports
// ("dhms" and "mm:ss"), a segment name outside the four the runtime fills
// (days|hours|minutes|seconds), a warn duration outside the small
// declarative subset above, or a then action other than "revalidate". This
// follows the same fail-closed principle as the ".length" rule above: a
// bad value here renders a silently inert (or silently ignored) countdown
// today, with nothing at the terminal to explain why, so Validate now
// catches it at check time instead.
//
// A dynamic expression value ({...}) is exempt — attr.Kind is AttrExpr for
// those, and this method only runs from the AttrStatic case in validateAttr
// and from validateComponentRef. Its value is known only at render or run
// time, and the browser runtime already fails inert (leaves the element or
// segment untouched) on a bad value it discovers there.
func (v *validator) validateStaticCountdownAttr(node *Node, attr *Attr) {
	switch attr.Name {
	case countdownInstantAttr:
		if _, err := time.Parse(time.RFC3339, attr.Value); err != nil {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: not a valid RFC3339 instant", countdownInstantAttr, attr.Value),
				Hint:    `use an RFC3339 instant such as "2026-08-22T16:00:00-04:00", or move the value into an expression ({...}) to compute it at render time`,
			})
		}
	case countdownFormatAttr:
		if attr.Value != "dhms" && attr.Value != "mm:ss" {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"dhms\" or \"mm:ss\"", countdownFormatAttr, attr.Value),
				Hint:    `"dhms" renders day/hour/minute/second text; "mm:ss" renders a minutes:seconds clock`,
			})
		}
	case countdownSegmentAttr:
		switch attr.Value {
		case "days", "hours", "minutes", "seconds":
		default:
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"days\", \"hours\", \"minutes\", or \"seconds\"", countdownSegmentAttr, attr.Value),
				Hint:    `mark each descendant the countdown should fill with one of these four segment names`,
			})
		}
	case countdownWarnAttr:
		if !isValidCountdownWarnValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a bare integer number of seconds, or whole h/m/s components such as \"30s\" or \"1m30s\"", countdownWarnAttr, attr.Value),
				Hint:    `this is a small declarative duration subset, not a general Go duration parser`,
			})
		}
	case countdownThenAttr:
		if attr.Value != "revalidate" {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"revalidate\"", countdownThenAttr, attr.Value),
				Hint:    `"revalidate" fires one revalidation of the page's revalidate root the first time the countdown reaches zero`,
			})
		}
	}
}

// validateLegacyTemplateExprs flags the well-known JS mistake of reading
// .length on a slice-valued expression (see gosx#164). The legacy file-router
// renderer resolves member access reflectively with no static types, so a
// slice's .length silently evaluates to nil instead of failing to compile —
// and nil compared to 0 is always false, so `<If cond={x.length == 0}>`
// renders neither branch with no error anywhere.
//
// gosx has no type information for legacy `any` data at check time, so this
// cannot distinguish a slice's .length from a map value legitimately keyed
// "length" (m[string]any{"length": n} resolves that reflectively too, and
// correctly). It flags ".length" selectors rooted at the identifier "data"
// rather than staying silent — the honest trade a checker with no types can
// make: a rare, working `data["length"]`-shaped access is rejected alongside
// the far more common accidental one, and check-time failure with a
// diagnosis beats silent divergence between check and render.
//
// gosx#174: the rule used to flag ".length" anywhere in a legacy component's
// expression holes, regardless of which identifier it was read from. That
// rejected valid Go: a legacy component can declare a typed parameter other
// than "data" (e.g. `func Page(r *ruler) Node` where `type ruler struct{
// length int }`), and `r.length` there is an ordinary, statically-checked
// struct field read — real Go code that compiles fine. It is "data" alone
// that route/fileeval.go binds to the reflective, untyped route payload
// (see fileRenderEnv / newFileRenderEnv: `env.values["data"] = ctx.Data`) —
// that binding exists under the literal name "data" no matter what the
// component's own function-parameter is named, because the file router
// never reads the source parameter name back. Only a selector chain whose
// root identifier is that literal "data" binding (`data.picks.length`,
// `data.picks[0].length`, ...) can hit the reflective-nil gotcha this rule
// exists for, so only those are flagged now.
func validateLegacyTemplateExprs(prog *Program, comp *Component) []Diagnostic {
	if int(comp.Root) >= len(prog.Nodes) {
		return nil
	}

	var diags []Diagnostic
	for _, id := range collectComponentNodeIDs(prog, comp.Root) {
		node := &prog.Nodes[id]
		if node.Kind == NodeExpr {
			diags = append(diags, lengthSelectorDiagnostics(node.Span, node.Text)...)
		}
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case AttrExpr, AttrSpread:
				diags = append(diags, lengthSelectorDiagnostics(node.Span, attr.Expr)...)
			}
		}
	}
	return diags
}

// lengthSelectorDiagnostics parses one Go expression hole and reports every
// ".length" member access rooted at the "data" identifier it contains. A
// source that fails to parse here is not this check's job — the render path
// already tolerates unparseable expressions by evaluating them to nil, and
// normal validation elsewhere covers empty/malformed expressions.
func lengthSelectorDiagnostics(span Span, source string) []Diagnostic {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil
	}
	expr, err := parser.ParseExpr(source)
	if err != nil {
		return nil
	}

	var diags []Diagnostic
	ast.Inspect(expr, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "length" {
			return true
		}
		// Only the reflective "data" binding resolves .length to a silent nil
		// (see the comment above validateLegacyTemplateExprs). A selector
		// rooted at any other identifier — including a legacy component's own
		// typed, non-"data" parameter — is either a real Go struct/map access
		// the compiler already checks, or a value the file router never binds
		// reflectively, so it is out of scope for this rule.
		root, ok := selectorRootIdent(sel.X)
		if !ok || root != "data" {
			return true
		}
		diags = append(diags, Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("unsupported member \".length\" in expression %q", source),
			Hint:    "Go has no automatic .length; pass a precomputed count from a DataLoader (e.g. \"picksEmpty\": len(picks) == 0), or add a typed component that calls len(...) directly",
		})
		return true
	})
	return diags
}

// selectorRootIdent walks down the left-hand side of a selector/index/paren
// chain (data.picks[0].length -> data.picks[0] -> data.picks -> data) to find
// the identifier the chain is rooted at. It reports ok=false for a chain
// rooted at anything other than a bare identifier (a call result, a
// composite literal, and so on), since those can never be the "data" binding.
func selectorRootIdent(expr ast.Expr) (string, bool) {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name, true
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		default:
			return "", false
		}
	}
}

// validateIslandExprs validates that all expressions in an island component
// are within the allowed island expression subset.
func validateIslandExprs(prog *Program, comp *Component) []Diagnostic {
	if int(comp.Root) >= len(prog.Nodes) {
		return nil
	}

	var diags []Diagnostic
	nodeIDs := collectComponentNodeIDs(prog, comp.Root)
	scope := mergedIslandScope(prog, *comp)
	for _, id := range nodeIDs {
		node := &prog.Nodes[id]
		diags = append(diags, validateIslandNode(node, scope)...)
	}

	return diags
}

func collectComponentNodeIDs(prog *Program, root NodeID) []NodeID {
	var nodeIDs []NodeID
	var collect func(id NodeID)
	collect = func(id NodeID) {
		if int(id) >= len(prog.Nodes) {
			return
		}
		nodeIDs = append(nodeIDs, id)
		for _, child := range prog.Nodes[id].Children {
			collect(child)
		}
	}
	collect(root)
	return nodeIDs
}

func validateIslandNode(node *Node, scope *ExprScope) []Diagnostic {
	if node == nil {
		return nil
	}
	if diag, ok := unsupportedIslandComponentDiagnostic(node); ok {
		return []Diagnostic{diag}
	}
	var diags []Diagnostic
	if node.Kind == NodeExpr {
		if diag, ok := validateIslandExprSource(node.Span, node.Text, scope); ok {
			diags = append(diags, diag)
		}
	}
	for _, attr := range node.Attrs {
		if diag, ok := validateIslandAttr(node.Span, attr, scope); ok {
			diags = append(diags, diag)
		}
	}
	return diags
}

func unsupportedIslandComponentDiagnostic(node *Node) (Diagnostic, bool) {
	if node == nil || node.Kind != NodeComponent {
		return Diagnostic{}, false
	}
	// <Image> gets its own message rather than falling through to the
	// generic one below (gosx#201): an island re-renders client-side from
	// its own program, which cannot rebuild the manifest-driven <picture>
	// markup <Image> emits on the server (route/fileprogram.go) without
	// shipping the whole buildmanifest.Manifest.Images bucket to the
	// client -- out of scope for this release. One tag name must not mean
	// two contracts, so <Image> is rejected inside an island outright, not
	// silently downgraded to a plain <img> the way it used to be lowered
	// (see islandElementAlias in ir/island.go, which no longer aliases it).
	if node.Tag == "Image" {
		return Diagnostic{
			Span:    node.Span,
			Message: "<Image> is not supported inside island components",
			Hint:    "an island cannot rebuild <Image>'s server-rendered <picture> markup on the client; use a plain <img> element inside the island instead, and set width and height explicitly to avoid layout shift",
		}, true
	}
	if !isUnsupportedIslandComponentRef(node.Tag) {
		return Diagnostic{}, false
	}
	return Diagnostic{
		Span:    node.Span,
		Message: fmt.Sprintf("component <%s> is not supported inside island components yet", node.Tag),
		Hint:    "Use plain elements inside the island or move the component outside the hydrated subtree.",
	}, true
}

func validateIslandAttr(span Span, attr Attr, scope *ExprScope) (Diagnostic, bool) {
	switch attr.Kind {
	case AttrSpread:
		return Diagnostic{
			Span:    span,
			Message: "spread attributes not allowed in island components",
		}, true
	case AttrExpr:
		if attr.IsEvent {
			if strings.TrimSpace(attr.Expr) == "" {
				return Diagnostic{
					Span:    span,
					Message: fmt.Sprintf("event handler %q has empty handler name in island component", attr.Name),
				}, true
			}
			return Diagnostic{}, false
		}
		return validateIslandExprSource(span, attr.Expr, scope)
	default:
		return Diagnostic{}, false
	}
}

func validateIslandExprSource(span Span, source string, scope *ExprScope) (Diagnostic, bool) {
	text := strings.TrimSpace(source)
	if text == "" {
		return Diagnostic{}, false
	}
	if err := islandExprRestrictionError(text); err != nil {
		return Diagnostic{
			Span:    span,
			Message: islandValidationMessage(err, text),
		}, true
	}
	if _, _, err := ParseExpr(text, scope); err != nil {
		return Diagnostic{
			Span:    span,
			Message: fmt.Sprintf("island expression error: %v", err),
		}, true
	}
	return Diagnostic{}, false
}

func islandValidationMessage(err error, source string) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "goroutine launch"):
		return fmt.Sprintf("goroutine launch not allowed in island components: %q", source)
	case strings.Contains(text, "channel creation"):
		return fmt.Sprintf("channel creation not allowed in island components: %q", source)
	case strings.Contains(text, "channel operations"):
		return fmt.Sprintf("channel operations not allowed in island components: %q", source)
	default:
		return fmt.Sprintf("island expression error: %v", err)
	}
}

func isUnsupportedIslandComponentRef(tag string) bool {
	switch strings.TrimSpace(tag) {
	case "TextBlock", "Stylesheet", "Surface", "Worker", "Scene3D":
		return true
	default:
		return false
	}
}

// validateEngineSurface performs validation specific to engine surface
// components that goes beyond what the lowering pass checks. It produces
// informational diagnostics that are appropriate for IDE integration.
func validateEngineSurface(prog *Program, comp *Component) []Diagnostic {
	var diags []Diagnostic

	if int(comp.Root) >= len(prog.Nodes) {
		return diags
	}
	root := &prog.Nodes[comp.Root]

	// Root must be <canvas>. (The lowering pass already rejects this, but
	// Validate is a separate pass that may run on programs not produced by
	// Lower, so we check here too.)
	if root.Kind != NodeElement || root.Tag != "canvas" {
		tag := root.Tag
		if root.Kind == NodeFragment {
			tag = "(fragment)"
		}
		diags = append(diags, Diagnostic{
			Span:    root.Span,
			Message: fmt.Sprintf("engine surface root must be <canvas>; got <%s>", tag),
			Hint:    "An engine surface component must return a single <canvas> element.",
		})
		return diags
	}

	// Validate each SurfaceHandlerRef: function name must be a non-empty valid
	// Go identifier. (Existence in the package is deferred to the build pipeline.)
	for _, ref := range comp.SurfaceHandlers {
		if strings.TrimSpace(ref.FunctionName) == "" {
			diags = append(diags, Diagnostic{
				Span:    root.Span,
				Message: fmt.Sprintf("engine surface handler %q has empty function name", ref.EventName),
			})
			continue
		}
		if !isValidGoIdent(ref.FunctionName) {
			diags = append(diags, Diagnostic{
				Span:    root.Span,
				Message: fmt.Sprintf("engine surface handler %q references %q which is not a valid Go identifier", ref.EventName, ref.FunctionName),
				Hint:    "The handler must be the name of a top-level function in the same package.",
			})
		}
	}

	return diags
}

// VoidElements are HTML elements that cannot have children.
var VoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true,
	"embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "param": true, "source": true,
	"track": true, "wbr": true,
}
