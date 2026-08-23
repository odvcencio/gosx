package ir

import (
	"fmt"
	"strings"
)

// navigationAttrNameWarnings is one of two built-in sources
// ValidateWarnings (ir/validate.go) combines -- the other is
// untypedLegacyPropsWarning -- so this file only exports the one function
// ValidateWarnings' own body calls, matching untypedLegacyPropsWarning's
// own standalone shape rather than adding a second, competing entry point.
// Both no-I/O passes run over the same *Program Validate already saw and
// are safe to run on every keystroke.
func navigationAttrNameWarnings(prog *Program) []Diagnostic {
	if prog == nil {
		return nil
	}
	var diags []Diagnostic
	for i := range prog.Components {
		diags = append(diags, navigationAttrNameWarningsForComponent(prog, &prog.Components[i])...)
	}
	return diags
}

func navigationAttrNameWarningsForComponent(prog *Program, comp *Component) []Diagnostic {
	if int(comp.Root) >= len(prog.Nodes) {
		return nil
	}
	var diags []Diagnostic
	for _, id := range collectComponentNodeIDs(prog, comp.Root) {
		node := &prog.Nodes[id]
		for _, attr := range node.Attrs {
			if diag, ok := navigationAttrNameWarning(node, attr); ok {
				diags = append(diags, diag)
			}
		}
	}
	return diags
}

// navigationPrimitiveAttrs is the closed set of declarative client
// primitives the bootstrap navigation runtime
// (client/runtime/host/navigation.ts and regions.ts) reads by exact
// attribute name (gosx#212, #213, #214, #215, #216, #217, #227, #228, #236).
// This is deliberately NOT every "data-gosx-*" attribute the framework
// defines anywhere -- scene3d, motion, text-layout, stripe, engine, and
// island each own a separate, actively evolving attribute contract of
// their own, most of it runtime-written telemetry rather than
// author-typed markup, and folding all of it into one "closed set" here
// would make an ordinary, correct use of one of those surfaces look like a
// misspelled navigation primitive. Scoping this list to the one family
// gosx#212-#236 actually describes keeps a false positive rare: a name has
// to be a near-miss of one of THESE names, not merely absent from a much
// larger list, before nearestNavigationPrimitiveAttr below suggests it.
var navigationPrimitiveAttrs = []string{
	"data-gosx-enhance",
	"data-gosx-enhance-layer",
	"data-gosx-fallback",
	"data-gosx-managed",
	"data-gosx-form",
	"data-gosx-form-mode",
	"data-gosx-form-state",
	"data-gosx-form-project",
	"data-gosx-toast-host",
	"data-gosx-action",
	"data-gosx-reset",
	"data-gosx-submit-on",
	"data-gosx-action-event",
	"data-gosx-action-signal",
	"data-gosx-action-target",
	"data-gosx-disclosure",
	"data-gosx-disclosure-target",
	"data-gosx-disclosure-close",
	"data-gosx-disclosure-backdrop",
	"data-gosx-disclosure-initial-focus",
	"data-gosx-disclosure-modal",
	"data-gosx-region",
	"data-gosx-region-url",
	"data-gosx-region-signal",
	"data-gosx-region-on",
	"data-gosx-region-field",
	"data-gosx-region-allow-empty",
	"data-gosx-region-interval",
	"data-gosx-runtime-surface",
	"data-gosx-runtime-surface-version",
	"data-gosx-link",
	"data-gosx-link-state",
	"data-gosx-link-current",
	"data-gosx-link-current-policy",
	"data-gosx-prefetch",
	"data-gosx-prefetch-state",
	"data-gosx-aria-current-managed",
	"data-gosx-revalidate-interval",
	"data-gosx-revalidate-src",
	countdownInstantAttr,
	countdownFormatAttr,
	countdownSegmentAttr,
	countdownWarnAttr,
	countdownCueAttr,
	countdownThenAttr,
	watchAttr,
	watchEffectAttr,
	"data-gosx-watch-title",
	"data-gosx-reorder",
	"data-gosx-reorder-action",
	"data-gosx-reorder-item",
	"data-gosx-reorder-handle",
	"data-gosx-reorder-item-field",
	"data-gosx-reorder-index-field",
	"data-gosx-reorder-placeholder",
	"data-gosx-live-src",
	liveIntervalAttr,
	liveBindAttr,
	liveFlashClassAttr,
	"data-gosx-live-signal",
	"data-gosx-live-on",
	"data-gosx-filter",
	"data-gosx-filter-text",
	"data-gosx-filter-announce",
	"data-gosx-heartbeat",
	"data-gosx-heartbeat-interval",
	heartbeatHiddenIntervalAttr,
	regionIntervalAttr,
}

// navigationPrimitiveAttrSet is navigationPrimitiveAttrs as a lookup set,
// built once at package init.
var navigationPrimitiveAttrSet = func() map[string]bool {
	set := make(map[string]bool, len(navigationPrimitiveAttrs))
	for _, name := range navigationPrimitiveAttrs {
		set[name] = true
	}
	return set
}()

// navigationPrimitiveTypoDistance is the maximum Levenshtein distance a
// name may sit from a known primitive before warnDataGosxAttrName treats it
// as an unrelated attribute rather than a likely typo. 1 catches the
// single dropped/doubled/transposed/substituted-character typos this check
// exists for ("data-gosx-hearbeat" is distance 1 from
// "data-gosx-heartbeat"). 2 was tried first and rejected: it also matched
// "data-gosx-motion" (the motion package's own, unrelated attribute) to
// "data-gosx-action" (distance 2, one substitution short of "motion" and
// "action" sharing their last four letters "tion"), a real false positive
// this check caught running across this repository's own examples before
// shipping -- see the CHANGELOG entry for gosx#249.
const navigationPrimitiveTypoDistance = 1

// navigationAttrNameWarning flags a static "data-gosx-*" attribute name
// that is close to, but not exactly, one of the declarative navigation
// primitives (gosx#212-#236) -- a likely misspelling that will render and
// silently no-op, with nothing at the terminal to explain why (see the
// closed-set requirement in the check's own design notes). An attribute
// already in the closed set is exempt; its VALUE is checked elsewhere
// (validateStaticCountdownAttr and its siblings), at error severity, not
// here. An attribute whose name is not close to any known primitive is
// silently out of scope -- it may belong to a different subsystem's own
// attribute contract (scene3d, motion, text-layout, stripe, engine,
// island) or be an application's own data attribute, and this check has no
// way to tell those apart from a typo of a name it has never heard of.
func navigationAttrNameWarning(node *Node, attr Attr) (Diagnostic, bool) {
	name := attr.Name
	if !strings.HasPrefix(name, "data-gosx-") || navigationPrimitiveAttrSet[name] {
		return Diagnostic{}, false
	}
	match, ok := nearestNavigationPrimitiveAttr(name)
	if !ok {
		return Diagnostic{}, false
	}
	return Diagnostic{
		Span:     node.Span,
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("%q is not a recognized gosx navigation attribute; did you mean %q?", name, match),
	}, true
}

// nearestNavigationPrimitiveAttr returns the closed-set name nearest to
// name, if any sit within navigationPrimitiveTypoDistance.
func nearestNavigationPrimitiveAttr(name string) (string, bool) {
	best := ""
	bestDistance := navigationPrimitiveTypoDistance + 1
	for _, candidate := range navigationPrimitiveAttrs {
		distance := attrNameEditDistance(name, candidate)
		if distance < bestDistance {
			bestDistance = distance
			best = candidate
		}
	}
	if bestDistance > navigationPrimitiveTypoDistance {
		return "", false
	}
	return best, true
}

// attrNameEditDistance is the Levenshtein distance between two short
// strings, an unexported copy of the same small algorithm used elsewhere
// in this module for an unrelated closed-set nearest-match search (see
// scene/schema/vocabulary.go's editDistance) -- there is no shared
// internal package either could import from without a new dependency
// edge, so each caller keeps its own copy, matching that precedent.
func attrNameEditDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = minInt(previous[j]+1, minInt(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
