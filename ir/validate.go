package ir

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"regexp"
	"strings"
	"time"
)

// Severity distinguishes a diagnostic that must block compilation from one
// that only advises the author. The zero value is SeverityError, so every
// Diagnostic literal written before Severity existed keeps failing exactly
// as it always did — this field is additive and changes no existing
// behavior on its own.
type Severity uint8

const (
	SeverityError Severity = iota
	SeverityWarning
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

	// Severity marks whether this diagnostic must block compilation
	// (SeverityError, the zero value) or only advises the author
	// (SeverityWarning). Validate's own diagnostics all leave this at the
	// zero value; ValidateWarnings is the one built-in source of
	// SeverityWarning diagnostics today.
	Severity Severity
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
	if d.Severity == SeverityWarning {
		s += "warning: "
	}
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

// ValidateWarnings runs advisory checks over the IR program and returns
// SeverityWarning diagnostics only. Unlike Validate, nothing here blocks
// compilation: gosx.Compile does not call this function, and a caller that
// wants these findings (gosx check, the language server) reports them
// itself and keeps going regardless of what it finds.
func ValidateWarnings(prog *Program) []Diagnostic {
	var diags []Diagnostic
	if prog == nil {
		return diags
	}
	for i := range prog.Components {
		diags = append(diags, untypedLegacyPropsWarning(&prog.Components[i])...)
	}
	// navigationAttrNameWarnings (ir/validate_warnings.go, gosx#249) is the
	// other built-in source of SeverityWarning diagnostics: a static
	// "data-gosx-*" attribute name close to, but not exactly, one of the
	// declarative navigation primitives.
	diags = append(diags, navigationAttrNameWarnings(prog)...)
	return diags
}

// untypedLegacyPropsWarning flags a component declared as untyped legacy:
// `func Name(props any) Node`. This is step one of retiring legacy
// component syntax — the goal is one component shape, `component
// Name(props: NameProps)` — and the untyped form is the one with no schema
// proof at all, so it is the first flagged.
//
// The check is deliberately narrow, matching only a props type of exactly
// "any": a legacy component that takes no props (PropsType == ""), or a
// TYPED legacy component whose props struct is declared in this file
// (Component.PropsTyped), is excluded — see the zero-props census in
// gosx's untyped-legacy-warning change for why zero-props legacy functions
// stay silent here. Engine and island components are also excluded by the
// current migration-warning policy. Strict islands are supported; extending
// this legacy retirement warning to them is a separate v1 migration change.
// Strict engines remain unsupported and automatic .gsx engine discovery is
// preview/post-v1.
func untypedLegacyPropsWarning(comp *Component) []Diagnostic {
	if comp.Syntax != ComponentSyntaxLegacy || comp.PropsTyped || comp.IsEngine || comp.IsIsland {
		return nil
	}
	if strings.TrimSpace(comp.PropsType) != "any" {
		return nil
	}
	return []Diagnostic{{
		Span:     comp.Span,
		Severity: SeverityWarning,
		Message: fmt.Sprintf(
			"component %s is declared as untyped legacy (func %s(%s any) Node); this form is deprecated and will be removed before v1.0",
			comp.Name, comp.Name, comp.PropsName,
		),
		Hint: fmt.Sprintf("declare component %s(props: %sProps) with the struct in this file instead", comp.Name, comp.Name),
	}}
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

// The data-gosx-countdown-* attributes with a fixed value vocabulary
// (gosx#178, extended by gosx#213). These string values are pinned against
// server/navigation_contract.go and client/runtime/host/navigation.ts by
// server/navigation_contract_countdown_test.go (gosx#178 review finding
// m11).
const (
	countdownInstantAttr = "data-gosx-countdown"
	countdownFormatAttr  = "data-gosx-countdown-format"
	countdownSegmentAttr = "data-gosx-countdown-segment"
	countdownWarnAttr    = "data-gosx-countdown-warn"
	countdownCueAttr     = "data-gosx-countdown-cue"
	countdownThenAttr    = "data-gosx-countdown-then"
)

// data-gosx-watch and its two companion attributes (gosx#214), pinned the
// same way against server/navigation_contract.go and
// client/runtime/host/navigation.ts by
// server/navigation_contract_countdown_test.go.
const (
	watchAttr       = "data-gosx-watch"
	watchEffectAttr = "data-gosx-watch-effect"
)

// data-gosx-live-* (gosx#217), pinned the same way against
// server/navigation_contract.go and client/runtime/host/navigation.ts, and
// data-gosx-region-interval (gosx#217), pinned against RegionIntervalAttr in
// runtime_contract.go and client/runtime/host/regions.ts. liveIntervalAttr
// and regionIntervalAttr are each the interval half of a "poll a
// same-origin source on an interval" pair, the same shape
// data-gosx-revalidate-interval declares; data-gosx-live-src and
// data-gosx-region-url are free-form same-origin URLs, checked only at run
// time (isSameOriginNavigation), the same as data-gosx-revalidate-src —
// neither has a const in this file, since neither is in the vocabulary
// this file checks statically. liveBindAttr is the one live-region value
// with a shape this file can usefully reject ahead of time: a JSON key (or
// dot-separated key chain), never empty and never containing whitespace.
const (
	liveIntervalAttr   = "data-gosx-live-interval"
	liveBindAttr       = "data-gosx-live-bind"
	liveFlashClassAttr = "data-gosx-live-flash-class"
	regionIntervalAttr = "data-gosx-region-interval"
	// regionModeAttr (gosx#217 extension) is the one data-gosx-region-*
	// growth-mode value this file can usefully reject ahead of time: see
	// isValidRegionModeValue below for why a typo here is worse than the
	// usual "silently falls back to a default" shape most other enumerated
	// attributes in this file get.
	regionModeAttr = "data-gosx-region-mode"
	// liveBindAttrAttr and liveBindClassAttr (gosx#217 extension) share
	// liveBindAttr's polled-or-event payload but each take a
	// comma-separated "target:key[,target:key...]" value instead of one
	// bare key: liveBindAttrAttr sets a named element attribute,
	// liveBindClassAttr toggles a named class from a boolean. Pinned
	// against NavigationLiveBindAttrAttr and NavigationLiveBindClassAttr
	// in server/navigation_contract.go and liveBindAttrTargetAllowed in
	// client/runtime/host/navigation.ts.
	liveBindAttrAttr  = "data-gosx-live-bind-attr"
	liveBindClassAttr = "data-gosx-live-bind-class"
)

// revalidateIntervalAttr, heartbeatIntervalAttr, and
// heartbeatHiddenIntervalAttr (gosx#216, gosx#217) share
// liveIntervalAttr and regionIntervalAttr's exact whole-seconds/
// whole-minutes grammar (see isValidPollIntervalValue below), pinned
// against NavigationRevalidateIntervalAttr,
// NavigationHeartbeatIntervalAttr, and NavigationHeartbeatHiddenIntervalAttr
// in server/navigation_contract.go and their shared parseRevalidateInterval
// parser in client/runtime/host/navigation.ts.
//
// linkCurrentPolicyAttr and prefetchAttr (gosx#210) are the two enumerated
// data-gosx-link companion values: NormalizeNavigationLinkCurrentPolicy in
// server/navigation_contract.go silently coerces anything outside
// {auto,page,ancestor,none} to "none", and NormalizeNavigationLinkPrefetch
// silently accepts anything at all once it sees a non-empty value, so
// neither one fails at run time on a bad value the way a missing key or a
// 404 does — this is the same "renders, does the wrong thing, says
// nothing" shape gosx#213's countdown pairs check was written for, applied
// to the link contract instead of the countdown one.
const (
	revalidateIntervalAttr      = "data-gosx-revalidate-interval"
	heartbeatIntervalAttr       = "data-gosx-heartbeat-interval"
	heartbeatHiddenIntervalAttr = "data-gosx-heartbeat-hidden-interval"
	linkCurrentPolicyAttr       = "data-gosx-link-current-policy"
	prefetchAttr                = "data-gosx-prefetch"
)

// countdownThresholdIntegerPattern and countdownThresholdDurationPattern
// mirror the small declarative duration subset
// parseCountdownThresholdSeconds accepts in client/runtime/host/navigation.ts:
// a bare non-negative integer as whole seconds, or whole hour/minute/second
// components combined in one value (for example "30s" or "1m30s"). This is
// not a general Go duration parser — see parseRevalidateInterval's own
// comment in navigation.ts for the same small-subset rationale applied to
// data-gosx-revalidate-interval. Shared by data-gosx-countdown-warn and
// data-gosx-countdown-cue (gosx#213): both attributes use this as the
// threshold half of a "threshold:token" pair.
var (
	countdownThresholdIntegerPattern  = regexp.MustCompile(`^[0-9]+$`)
	countdownThresholdDurationPattern = regexp.MustCompile(`^(?:([0-9]+)h)?(?:([0-9]+)m)?(?:([0-9]+)s)?$`)
	// countdownWarnClassTokenPattern rejects only what could never work as
	// one class in a space-joined class attribute: embedded whitespace.
	// This is deliberately not a full CSS identifier grammar — see
	// isValidCountdownWarnClassToken in navigation.ts for the identical
	// rule applied client-side.
	countdownWarnClassTokenPattern = regexp.MustCompile(`^\S+$`)
	// pollIntervalValuePattern mirrors parseRevalidateInterval's own small
	// declarative subset in navigation.ts exactly: a whole number of seconds
	// or minutes only ("4s", "90s", "2m"), not the wider hour/minute/second
	// combination countdownThresholdDurationPattern above accepts — this
	// file does not statically validate data-gosx-revalidate-interval
	// itself (see the const block's own comment above), but
	// data-gosx-live-interval and data-gosx-region-interval share its exact
	// grammar, so they share this pattern.
	pollIntervalValuePattern = regexp.MustCompile(`^[0-9]+(?:s|m)$`)
	// liveBindKeyPattern matches liveBindAttr's own shape check in
	// navigation.ts: one or more non-empty, whitespace-free segments joined
	// by ".".
	liveBindKeyPattern = regexp.MustCompile(`^[^\s.]+(?:\.[^\s.]+)*$`)
	// liveBindAttrTargetNamePattern rejects a data-gosx-live-bind-attr
	// target that could never work as an HTML attribute name at all —
	// embedded whitespace, a colon, or any other character outside this
	// shape — mirroring the identical guard LIVE_BIND_ATTR_NAME_PATTERN
	// applies in liveBindAttrTargetAllowed (client/runtime/host/navigation.ts).
	liveBindAttrTargetNamePattern = regexp.MustCompile(`^[A-Za-z_][-A-Za-z0-9_.]*$`)
)

// isValidCountdownThresholdValue reports whether value parses under the
// small declarative duration subset described above.
func isValidCountdownThresholdValue(value string) bool {
	if countdownThresholdIntegerPattern.MatchString(value) {
		return true
	}
	m := countdownThresholdDurationPattern.FindStringSubmatch(value)
	return m != nil && (m[1] != "" || m[2] != "" || m[3] != "")
}

// countdownCueNames is the fixed, tiny synthesized tone vocabulary
// data-gosx-countdown-cue and data-gosx-watch-effect's "cue:<name>" token
// both draw from (gosx#213 / gosx#214) — see "Shared synthesized audio
// cues" in client/runtime/host/navigation.ts for what each name actually
// sounds like.
var countdownCueNames = map[string]bool{"beep": true, "chime": true}

// isValidCountdownTierPairsValue reports whether value parses under the
// shared "threshold:token[,threshold:token]..." grammar
// data-gosx-countdown-warn and data-gosx-countdown-cue both use
// (gosx#213): a comma-separated list of pairs, each a valid threshold (see
// isValidCountdownThresholdValue) and a token isValidToken accepts. This
// mirrors parseCountdownTierPairs in navigation.ts exactly, including its
// fail-closed-as-a-whole behavior: a single malformed pair fails the
// entire value, not just that pair.
func isValidCountdownTierPairsValue(value string, isValidToken func(string) bool) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, rawPair := range strings.Split(trimmed, ",") {
		pair := strings.TrimSpace(rawPair)
		splitAt := strings.Index(pair, ":")
		if splitAt <= 0 || splitAt == len(pair)-1 {
			return false
		}
		threshold := pair[:splitAt]
		token := strings.TrimSpace(pair[splitAt+1:])
		if !isValidCountdownThresholdValue(threshold) || token == "" || !isValidToken(token) {
			return false
		}
	}
	return true
}

func isValidCountdownWarnClassToken(token string) bool {
	return countdownWarnClassTokenPattern.MatchString(token)
}

func isValidCountdownCueToken(token string) bool {
	return countdownCueNames[token]
}

// isValidWatchConditionValue reports whether value parses as a
// data-gosx-watch condition: "<attrName>=<valueRef>", split at the first
// "=", with a non-empty attrName. valueRef itself is not further validated
// statically — a literal is arbitrary author text, and a "@<selector>"
// or "@<selector>[<attrName>]" reference's selector is not something
// `gosx check` can usefully validate ahead of the DOM it will run against.
// This mirrors parseWatchCondition in navigation.ts's own top-level shape
// check.
func isValidWatchConditionValue(value string) bool {
	splitAt := strings.Index(value, "=")
	if splitAt <= 0 {
		return false
	}
	return strings.TrimSpace(value[:splitAt]) != ""
}

// watchEffectTokenPattern validates one data-gosx-watch-effect token:
// "title" bare, "class:<name>" optionally followed by "@<selector>", or
// "cue:<name>". Mirrors parseWatchEffects in navigation.ts's own token
// grammar. The named capture groups let isValidWatchEffectValue below
// single out a "cue:<name>" token to also check its name against
// countdownCueNames — the two other shapes accept any non-empty value for
// their own free-form parts (a class name, a selector).
var watchEffectTokenPattern = regexp.MustCompile(`^(?:title|class:[^\s@]+(?:@\S+)?|cue:(?P<cue>\S+))$`)

// isValidWatchEffectValue reports whether every comma-separated token in
// value matches watchEffectTokenPattern above, with a cue token's name
// additionally checked against the fixed tone vocabulary. Unlike
// data-gosx-countdown-warn/-cue's pairs, an unrecognized token here is not
// fail-closed-as-a-whole at RUN time (see parseWatchEffects' own doc
// comment in navigation.ts for why) — but `gosx check` still rejects the
// whole value at CHECK time, the same as it does for a countdown pair
// list: an author-visible diagnostic before the page ever serves beats a
// console.warn a real user session might silently drop one effect from.
func isValidWatchEffectValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, rawToken := range strings.Split(trimmed, ",") {
		token := strings.TrimSpace(rawToken)
		match := watchEffectTokenPattern.FindStringSubmatch(token)
		if match == nil {
			return false
		}
		cueIdx := watchEffectTokenPattern.SubexpIndex("cue")
		if cueName := match[cueIdx]; cueName != "" && !countdownCueNames[cueName] {
			return false
		}
	}
	return true
}

// isValidPollIntervalValue reports whether value parses under
// pollIntervalValuePattern above (gosx#217): the same "whole seconds or
// whole minutes only" subset parseRevalidateInterval accepts in
// navigation.ts, shared by data-gosx-live-interval and
// data-gosx-region-interval.
func isValidPollIntervalValue(value string) bool {
	return pollIntervalValuePattern.MatchString(strings.TrimSpace(value))
}

// isValidLiveBindKeyValue reports whether value parses as a
// data-gosx-live-bind key (gosx#217): one or more non-empty,
// whitespace-free segments joined by "." — a top-level key, or a chain of
// nested-object keys, into the region's polled JSON object. There is no
// array-index or selector syntax here; see NavigationLiveBindAttr's own
// doc comment in server/navigation_contract.go for why the grammar stays
// this small.
func isValidLiveBindKeyValue(value string) bool {
	return liveBindKeyPattern.MatchString(strings.TrimSpace(value))
}

// isValidLiveBindPairsValue parses value under liveBindAttrAttr and
// liveBindClassAttr's shared "target:key[,target:key...]" grammar,
// mirroring parseLiveBindPairs in navigation.ts, and applies checkTarget to
// each pair's target. Malformed syntax (no ":", an empty target, or an
// empty key) fails the whole value, the same fail-closed-as-a-whole
// contract isValidCountdownTierPairsValue documents for the
// countdown-warn/-cue pair grammar above.
func isValidLiveBindPairsValue(value string, checkTarget func(string) bool) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, rawPair := range strings.Split(trimmed, ",") {
		pair := strings.TrimSpace(rawPair)
		splitAt := strings.Index(pair, ":")
		if splitAt <= 0 || splitAt == len(pair)-1 {
			return false
		}
		target := strings.TrimSpace(pair[:splitAt])
		key := strings.TrimSpace(pair[splitAt+1:])
		if target == "" || !isValidLiveBindKeyValue(key) || !checkTarget(target) {
			return false
		}
	}
	return true
}

// isValidLiveBindAttrTargetValue reports whether target is a permitted
// data-gosx-live-bind-attr target at check time, mirroring
// liveBindAttrTargetAllowed's run-time POSITIVE allowlist in
// client/runtime/host/navigation.ts by name: a data-* attribute other
// than a runtime-owned data-gosx-* attribute or the runtime-read
// data-csrf-token/data-csrf pair, an aria-* attribute,
// title/value/datetime/disabled/hidden, href, or data-gosx-countdown
// itself. A target whose shape could never work as an HTML attribute
// name at all (embedded whitespace, a colon, and so on) fails first,
// under the same liveBindAttrTargetNamePattern the run-time allowlist
// checks too. This check-time pass does not guarantee a run-time pass —
// an href target's scheme, and a data-gosx-countdown target's node-level
// -then refusal, are both known only once a payload value (or the node)
// is in hand — but a check-time failure here always means a guaranteed
// run-time refusal too, so a bind that checks clean is never silently
// rejected once live.
func isValidLiveBindAttrTargetValue(target string) bool {
	name := strings.ToLower(strings.TrimSpace(target))
	if name == "" || !liveBindAttrTargetNamePattern.MatchString(name) {
		return false
	}
	if name == countdownInstantAttr {
		return true
	}
	if strings.HasPrefix(name, "data-gosx-") {
		return false
	}
	if name == "data-csrf-token" || name == "data-csrf" {
		return false
	}
	if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "aria-") {
		return true
	}
	switch name {
	case "title", "value", "datetime", "disabled", "hidden", "href":
		return true
	}
	return false
}

// isValidLinkCurrentPolicyValue reports whether value is one of the four
// policies NormalizeNavigationLinkCurrentPolicy recognizes by name
// (case-insensitively, surrounding whitespace ignored) rather than
// silently folding into its "none" default.
func isValidLinkCurrentPolicyValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "page", "ancestor", "none":
		return true
	default:
		return false
	}
}

// isValidPrefetchValue reports whether value is one of the four prefetch
// policies NormalizeNavigationLinkPrefetch's own switch names
// (case-insensitively, surrounding whitespace ignored), rather than
// falling into that function's pass-through default branch.
func isValidPrefetchValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "intent", "render", "force":
		return true
	default:
		return false
	}
}

// isValidRegionModeValue reports whether value is one of the three
// data-gosx-region-mode values RegionModeAttr's own doc comment in
// runtime_contract.go documents: "replace" (the default), "append", or
// "prepend" — matched byte for byte against the run-time's own exact,
// UNTRIMMED comparison (record.mode === "append" || record.mode ===
// "prepend", where record.mode itself is read straight off
// el.getAttribute with no trim at all). This function deliberately does
// not call strings.TrimSpace either: "append " or " prepend" is exactly
// as wrong as "Append" here, since the run-time's === would reject all
// three the same way. Unlike isValidLinkCurrentPolicyValue or
// isValidPrefetchValue above, an unrecognized region mode is not a
// harmless "falls back to a sane default and moves on" value: the
// run-time treats anything other than "append"/"prepend" as "replace",
// so a typo (a stray space, "Append", "perpend") would silently turn an
// intended growth mode into a destructive full-region swap that wipes
// every row the region already rendered — this must fail `gosx check`,
// never reach a browser.
func isValidRegionModeValue(value string) bool {
	switch value {
	case "replace", "append", "prepend":
		return true
	default:
		return false
	}
}

// validateStaticCountdownAttr flags a static data-gosx-countdown-*,
// data-gosx-watch, data-gosx-watch-effect, data-gosx-live-*, or
// data-gosx-region-interval value outside its documented vocabulary: an instant
// that is not valid RFC3339, a format outside the two render modes the
// countdown runtime supports ("dhms" and "mm:ss"), a segment name outside
// the four the runtime fills (days|hours|minutes|seconds), a warn or cue
// value outside the shared
// threshold:token pairs grammar (gosx#213), a then action other than
// "revalidate", a watch condition with no "=", a watch effect list with an
// unrecognized token (gosx#214), a live or region interval outside the
// whole-seconds/whole-minutes subset data-gosx-revalidate-interval uses, a
// live bind key with an empty or whitespace-containing segment, a live
// flash class with embedded whitespace (gosx#217), a revalidate or
// heartbeat interval outside that same whole-seconds/whole-minutes subset,
// a link current-policy outside {auto,page,ancestor,none}, or a prefetch
// policy outside {off,intent,render,force}. This follows the same
// fail-closed principle as the ".length" rule above: a bad value here
// renders a silently inert (or silently mis-normalized) countdown,
// watcher, live/region poll, or link today, with nothing at the terminal
// to explain why, so Validate now catches it at check time instead.
//
// A dynamic expression value ({...}) is exempt — attr.Kind is AttrExpr for
// those, and this method only runs from the AttrStatic case in validateAttr
// and from validateComponentRef. Its value is known only at render or run
// time, and the browser runtime already fails inert (leaves the element or
// segment untouched, or the watcher/countdown disabled) on a bad value it
// discovers there.
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
		if !isValidCountdownTierPairsValue(attr.Value, isValidCountdownWarnClassToken) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a comma-separated list of threshold:class pairs", countdownWarnAttr, attr.Value),
				Hint:    `for example "30s:is-warn,10s:is-critical"; each threshold is a bare integer number of seconds, or whole h/m/s components such as "30s" or "1m30s"`,
			})
		}
	case countdownCueAttr:
		if !isValidCountdownTierPairsValue(attr.Value, isValidCountdownCueToken) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a comma-separated list of threshold:cue pairs using \"beep\" or \"chime\"", countdownCueAttr, attr.Value),
				Hint:    `for example "10s:beep"; each threshold is a bare integer number of seconds, or whole h/m/s components such as "30s" or "1m30s"`,
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
	case watchAttr:
		if !isValidWatchConditionValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"<attrName>=<value>\"", watchAttr, attr.Value),
				Hint:    `compare against a literal ("data-on-clock=true") or another element ("data-seat=@#viewer[data-seat-id]")`,
			})
		}
	case watchEffectAttr:
		if !isValidWatchEffectValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a comma-separated list of \"class:<name>\", \"title\", or \"cue:<name>\" tokens", watchEffectAttr, attr.Value),
				Hint:    `a "cue:<name>" token's name must be "beep" or "chime"; a "class:<name>" token may add "@<selector>" to target another element`,
			})
		}
	case liveIntervalAttr, regionIntervalAttr, revalidateIntervalAttr, heartbeatIntervalAttr, heartbeatHiddenIntervalAttr:
		if !isValidPollIntervalValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a whole number of seconds or minutes", attr.Name, attr.Value),
				Hint:    `for example "4s" or "2m" — the same subset data-gosx-revalidate-interval accepts`,
			})
		}
	case regionModeAttr:
		if !isValidRegionModeValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"replace\", \"append\", or \"prepend\"", regionModeAttr, attr.Value),
				Hint:    `an unrecognized value falls back to "replace" at run time, silently turning an intended growth mode into a destructive full-region swap`,
			})
		}
	case linkCurrentPolicyAttr:
		if !isValidLinkCurrentPolicyValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"auto\", \"page\", \"ancestor\", or \"none\"", linkCurrentPolicyAttr, attr.Value),
				Hint:    `NormalizeNavigationLinkCurrentPolicy silently treats any other value as "none"; write one of the four recognized values instead`,
			})
		}
	case prefetchAttr:
		if !isValidPrefetchValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be \"off\", \"intent\", \"render\", or \"force\"", prefetchAttr, attr.Value),
				Hint:    `NormalizeNavigationLinkPrefetch does not reject an unrecognized value at run time, so a typo here silently disables prefetch instead of failing loudly`,
			})
		}
	case liveBindAttr:
		if !isValidLiveBindKeyValue(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a \".\"-separated chain of non-empty keys", liveBindAttr, attr.Value),
				Hint:    `for example "score:t42" or "status.mode" — no embedded whitespace, no array index`,
			})
		}
	case liveFlashClassAttr:
		if !isValidCountdownWarnClassToken(attr.Value) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be one class name with no embedded whitespace", liveFlashClassAttr, attr.Value),
			})
		}
	case liveBindAttrAttr:
		if !isValidLiveBindPairsValue(attr.Value, isValidLiveBindAttrTargetValue) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a comma-separated list of target:key pairs", liveBindAttrAttr, attr.Value),
				Hint:    `for example "data-gosx-countdown:clock.deadline,href:link"; a target must be a data-* attribute (other than data-gosx-* or data-csrf-token/data-csrf), an aria-* attribute, title/value/datetime/disabled/hidden, href, or data-gosx-countdown`,
			})
		}
	case liveBindClassAttr:
		if !isValidLiveBindPairsValue(attr.Value, isValidCountdownWarnClassToken) {
			v.diags = append(v.diags, Diagnostic{
				Span:    node.Span,
				Message: fmt.Sprintf("invalid %s value %q: must be a comma-separated list of class:key pairs", liveBindClassAttr, attr.Value),
				Hint:    `for example "pick-clock--paused:clock.paused"; each class name may not contain embedded whitespace`,
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
	scope := mergedIslandScope(prog, *comp)
	checker := newIslandLowerer(prog, comp.Name, scope)
	if comp.AcceptsChildren || len(comp.AcceptsSlots) > 0 {
		diags = append(diags, Diagnostic{
			Span:    comp.Span,
			Message: fmt.Sprintf("island component %s cannot declare caller children or named slots", comp.Name),
			Hint:    "root island call-site content is outside the per-component .gxi program; compose children and slots through a same-file pure-view component inside the island instead",
		})
	}

	var walk func(NodeID, []string)
	walk = func(id NodeID, stack []string) {
		if int(id) >= len(prog.Nodes) {
			return
		}

		node := &prog.Nodes[id]
		if node.Kind != NodeComponent || node.IsSyntheticConditional() {
			diags = append(diags, validateIslandNode(node, scope)...)
			for _, child := range node.Children {
				walk(child, stack)
			}
			for _, name := range sortedNodeSlotNames(node.Slots) {
				walk(node.Slots[name], stack)
			}
			return
		}

		targetIdx, local := checker.componentIndex[node.Tag]
		if !local {
			if isEachComponent(node.Tag) || isConditionalComponent(node.Tag) || node.Tag == "Link" || node.Tag == "Image" {
				diags = append(diags, validateIslandNode(node, scope)...)
				for _, child := range node.Children {
					walk(child, stack)
				}
				for _, name := range sortedNodeSlotNames(node.Slots) {
					walk(node.Slots[name], stack)
				}
				return
			}
			if diag, unsupported := unsupportedIslandComponentDiagnostic(node); unsupported {
				diags = append(diags, diag)
				for _, child := range node.Children {
					walk(child, stack)
				}
				for _, name := range sortedNodeSlotNames(node.Slots) {
					walk(node.Slots[name], stack)
				}
				return
			}
			message := fmt.Sprintf("component <%s> is not supported inside island components", node.Tag)
			hint := "use a same-file strict pure-view component or move the component outside the hydrated subtree"
			if strings.Contains(node.Tag, ".") {
				message = fmt.Sprintf("imported component <%s> cannot be composed inside island %s in v1", node.Tag, comp.Name)
				hint = "use a same-file strict pure-view component; imported island callees require a future package-aware composition contract"
			}
			diags = append(diags, Diagnostic{Span: node.Span, Message: message, Hint: hint})
			for _, child := range node.Children {
				walk(child, stack)
			}
			for _, name := range sortedNodeSlotNames(node.Slots) {
				walk(node.Slots[name], stack)
			}
			return
		}

		callInvalid := false
		for _, attr := range node.Attrs {
			switch {
			case attr.Kind == AttrSpread:
				callInvalid = true
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: fmt.Sprintf("component <%s> uses a spread inside island %s", node.Tag, comp.Name),
					Hint:    "v1 pure-view composition requires explicit typed scalar props",
				})
			case attr.IsEvent || (attr.Kind == AttrExpr && scope.Handlers[strings.TrimSpace(attr.Expr)]):
				callInvalid = true
				diags = append(diags, Diagnostic{
					Span:    node.Span,
					Message: fmt.Sprintf("component <%s> passes handler-valued prop %q inside island %s", node.Tag, attr.Name, comp.Name),
					Hint:    "keep behavior in the parent island and pass only typed scalar view data",
				})
			case attr.Kind == AttrExpr:
				if diag, ok := validateIslandExprSource(node.Span, attr.Expr, scope); ok {
					callInvalid = true
					diags = append(diags, diag)
				}
			}
		}

		target := &prog.Components[targetIdx]
		for _, name := range stack {
			if name == target.Name {
				path := append(append([]string(nil), stack...), target.Name)
				diags = append(diags, Diagnostic{Span: node.Span, Message: "island component composition cycle: " + strings.Join(path, " -> ")})
				return
			}
		}
		if len(stack) >= maxIslandCompositionDepth {
			// Physical composition depth can differ from definition ancestry
			// when caller projections pass through multiple wrappers. The
			// actual lowerer below is the sole exact authority for the cap.
			return
		}
		if err := checker.composableCalleeError(target); err != nil {
			callInvalid = true
			diags = append(diags, Diagnostic{Span: node.Span, Message: err.Error()})
		}

		// Caller-owned projections retain the parent island scope. Validate
		// them even when the callee boundary itself is invalid so diagnostics
		// never hide an independently malformed child expression.
		for _, child := range node.Children {
			walk(child, stack)
		}
		for _, name := range sortedNodeSlotNames(node.Slots) {
			walk(node.Slots[name], stack)
		}
		if !callInvalid {
			walk(target.Root, append(stack, target.Name))
		}
	}
	walk(comp.Root, []string{comp.Name})
	if compIdx, ok := checker.componentIndex[comp.Name]; ok {
		if _, err := LowerIsland(prog, compIdx); err != nil {
			var expansionErr *islandExpansionError
			if errors.As(err, &expansionErr) {
				diags = append(diags, Diagnostic{Span: comp.Span, Message: expansionErr.Error()})
			}
		}
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
