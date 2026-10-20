package strictcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// validateRequiredReachabilityContract is strictcheck's check-time
// required-control reachability heuristic (gosx#249, check 2). A form
// control carrying "required" that the project's own stylesheet visually
// hides makes the WHOLE form unsubmittable: a browser refuses to submit a
// form containing a required control it cannot focus, silently -- no
// request leaves the page, no console error, nothing. This is the signup
// form defect from gosx#249's premise table: the markup and the stylesheet
// were both in the repository the whole time.
//
// Warning severity, stated as a heuristic in every message this produces:
// CSS resolution without a browser is approximate by construction (see
// ruleMatchesElement's and hidingReason's doc comments for exactly what
// this check does and does not model). A false positive here costs a
// human one dismissed warning; a false negative here ships a form nobody
// can submit.
func validateRequiredReachabilityContract(files []transpile.PackageFile, root string, opts Options) {
	if opts.Warnings == nil {
		return
	}
	rules := loadPublicCSSHidingRules(root)
	if len(rules) == 0 {
		return
	}
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, comp := range file.Program.Components {
			for _, id := range collectImageContractNodeIDs(file.Program, comp.Root) {
				node := &file.Program.Nodes[id]
				if diag, ok := requiredReachabilityDiagnostic(file.Path, node, rules); ok {
					addWarnings(opts, []ir.Diagnostic{diag})
				}
			}
		}
	}
}

// requiredReachabilityDiagnostic reports whether node carries a static
// "required" attribute AND has a static class or id matching one of
// rules -- both class and id must be static (a dynamic {expr} class/id is
// outside what this check can resolve, so it abstains for that node
// rather than guess).
func requiredReachabilityDiagnostic(path string, node *ir.Node, rules []cssHidingRule) (ir.Diagnostic, bool) {
	if node.Kind != ir.NodeElement || !nodeHasStaticRequired(node) {
		return ir.Diagnostic{}, false
	}
	class, classOK := staticAttrValue(node, "class")
	id, idOK := staticAttrValue(node, "id")
	if !classOK && !idOK {
		return ir.Diagnostic{}, false
	}
	classes := strings.Fields(class)
	for _, rule := range rules {
		if !ruleMatchesElement(rule, node.Tag, classes, id) {
			continue
		}
		span := node.Span
		span.File = path
		return ir.Diagnostic{
			Span:     span,
			Severity: ir.SeverityWarning,
			Message:  fmt.Sprintf("gosx: heuristic: this required control matches CSS rule %q, which appears to hide it (%s)", rule.selector, rule.reason),
			Hint:     "a browser refuses to submit a form containing a required control it cannot focus, silently; this is a static, non-cascade-aware match against public/*.css, so verify by hand -- remove required, or make the control focusable another way",
		}, true
	}
	return ir.Diagnostic{}, false
}

func nodeHasStaticRequired(node *ir.Node) bool {
	for _, attr := range node.Attrs {
		if attr.Name != "required" {
			continue
		}
		switch attr.Kind {
		case ir.AttrBool:
			return true
		case ir.AttrStatic:
			return !strings.EqualFold(strings.TrimSpace(attr.Value), "false")
		}
	}
	return false
}

func staticAttrValue(node *ir.Node, name string) (string, bool) {
	for _, attr := range node.Attrs {
		if attr.Name == name && attr.Kind == ir.AttrStatic {
			return attr.Value, true
		}
	}
	return "", false
}

// cssHidingRule is one parsed CSS rule this check believes hides whatever
// it matches, plus the raw selector list and the human-readable reason for
// its own diagnostic message.
type cssHidingRule struct {
	selectors []simpleSelector
	selector  string // original selector list text, for the message
	reason    string
}

// simpleSelector is the rightmost compound selector of one comma-separated
// entry in a rule's selector list: a tag name, and/or one or more classes,
// and/or an id. ruleMatchesElement below matches ONLY this rightmost
// compound against the element's own tag/class/id -- see its doc comment
// for why ignoring ancestor combinators is a deliberate, documented
// simplification rather than an oversight.
type simpleSelector struct {
	tag     string
	classes []string
	id      string
}

// ruleMatchesElement reports whether any of rule's selectors' rightmost
// compound could match an element with tag/classes/id. This does not
// model combinators (descendant, child, sibling), pseudo-classes,
// attribute selectors, or specificity/cascade order at all -- it asks only
// "does the element's own tag/class/id satisfy this compound's own
// requirements", which is why gosx#249 ships this as a heuristic warning,
// not an error: a rule like ".card.archived .required-field" matches an
// element with class="required-field" by this rule's logic even outside
// an actual ".card.archived" ancestor, a false positive a human dismisses
// in seconds; the alternative -- silently missing an unsubmittable form --
// costs much more.
func ruleMatchesElement(rule cssHidingRule, tag string, classes []string, id string) bool {
	for _, sel := range rule.selectors {
		if sel.tag != "" && !strings.EqualFold(sel.tag, tag) {
			continue
		}
		if sel.id != "" && sel.id != id {
			continue
		}
		if !classSetContainsAll(classes, sel.classes) {
			continue
		}
		if sel.tag == "" && sel.id == "" && len(sel.classes) == 0 {
			continue // an empty compound (a bare combinator/universal) matches nothing usefully here
		}
		return true
	}
	return false
}

func classSetContainsAll(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// loadPublicCSSHidingRules reads every "*.css" file under root's public/
// directory (the same convention validateImageContract's publicImageDirFor
// already establishes) and returns every rule this check recognizes as
// hiding whatever it matches. A missing or absent public/ directory
// degrades to no rules found -- the same fail-quiet-not-fail-positive
// posture publicImageDirFor documents for its own local-source rule.
func loadPublicCSSHidingRules(root string) []cssHidingRule {
	dir := publicImageDirFor(root)
	if dir == "" {
		return nil
	}
	var rules []cssHidingRule
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".css") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rules = append(rules, parseCSSHidingRules(string(data))...)
		return nil
	})
	return rules
}
