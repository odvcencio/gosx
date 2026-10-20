package strictcheck

import "strings"

// This file is a small, purpose-built CSS reader for
// validateRequiredReachabilityContract (gosx#249, check 2) only -- it is
// not a general CSS parser, has no notion of specificity or the cascade,
// and does not attempt to resolve custom properties, imports, or nested
// CSS. It reads just enough structure (rule -> selector list +
// declarations, descending into @media/@supports/@layer/@container) to
// ask one narrow question per rule: "do this rule's own declarations look
// like they hide whatever they match", and if so, "what is the rightmost
// compound selector of each entry in its selector list".

// cssDecl is one "prop: value" declaration, both already lower-cased.
type cssDecl struct {
	prop  string
	value string
}

// parseCSSHidingRules extracts every rule in source whose declarations
// hidingReason recognizes as hiding whatever the rule matches.
func parseCSSHidingRules(source string) []cssHidingRule {
	var rules []cssHidingRule
	walkCSSRules(source, func(selectorList, body string) {
		reason, hides := hidingReason(parseCSSDeclarations(body))
		if !hides {
			return
		}
		rules = append(rules, cssHidingRule{
			selectors: parseSelectorList(selectorList),
			selector:  strings.TrimSpace(selectorList),
			reason:    reason,
		})
	})
	return rules
}

// walkCSSRules scans source for top-level style rules, calling visit with
// each rule's raw selector-list text and declaration-block text.
// @media/@supports/@layer/@container conditionally wrap ordinary rules, so
// this descends into their bodies; any other at-rule (@keyframes,
// @font-face, @page, and so on) has no element selectors to offer this
// check, so its body is skipped whole.
func walkCSSRules(source string, visit func(selector, body string)) {
	i := 0
	for {
		prelude, body, next, ok := nextCSSBlock(source, i)
		if !ok {
			return
		}
		i = next
		if strings.HasPrefix(prelude, "@") {
			switch atRuleKeyword(prelude) {
			case "media", "supports", "layer", "container":
				walkCSSRules(body, visit)
			}
			continue
		}
		if prelude == "" {
			continue
		}
		visit(prelude, body)
	}
}

func atRuleKeyword(prelude string) string {
	prelude = strings.TrimPrefix(prelude, "@")
	for i := 0; i < len(prelude); i++ {
		switch prelude[i] {
		case ' ', '\t', '\n', '\r', '(', '{':
			return strings.ToLower(prelude[:i])
		}
	}
	return strings.ToLower(prelude)
}

// nextCSSBlock finds the next "prelude { body }" block in source starting
// at from, respecting quoted strings and comments so a "{" or "}"
// appearing inside either is never mistaken for structure.
func nextCSSBlock(source string, from int) (prelude, body string, next int, ok bool) {
	n := len(source)
	i := from
	start := from
	for i < n {
		c := source[i]
		switch {
		case c == '/' && i+1 < n && source[i+1] == '*':
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				return "", "", n, false
			}
			i = i + 2 + end + 2
		case c == '"' || c == '\'':
			i = skipCSSString(source, i)
		case c == '{':
			prelude = strings.TrimSpace(source[start:i])
			blockEnd := matchCSSBrace(source, i)
			if blockEnd < 0 {
				return "", "", n, false
			}
			return prelude, source[i+1 : blockEnd], blockEnd + 1, true
		case c == '}':
			// An unmatched "}" this scan did not open (malformed CSS, or a
			// nesting shape this reader does not model): treat everything
			// up to here as consumed and keep scanning past it rather than
			// looping forever.
			i++
			start = i
		default:
			i++
		}
	}
	return "", "", n, false
}

// matchCSSBrace returns the index of the "}" matching the "{" at open.
func matchCSSBrace(source string, open int) int {
	n := len(source)
	depth := 0
	i := open
	for i < n {
		c := source[i]
		switch {
		case c == '/' && i+1 < n && source[i+1] == '*':
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				return -1
			}
			i = i + 2 + end + 2
		case c == '"' || c == '\'':
			i = skipCSSString(source, i)
		case c == '{':
			depth++
			i++
		case c == '}':
			depth--
			if depth == 0 {
				return i
			}
			i++
		default:
			i++
		}
	}
	return -1
}

func skipCSSString(source string, i int) int {
	n := len(source)
	quote := source[i]
	i++
	for i < n {
		if source[i] == '\\' && i+1 < n {
			i += 2
			continue
		}
		if source[i] == quote {
			return i + 1
		}
		i++
	}
	return n
}

// parseCSSDeclarations splits body on top-level ";" (respecting quoted
// strings, comments, and parenthesized values such as rect(...) or
// url(...)) into individual declarations.
func parseCSSDeclarations(body string) []cssDecl {
	var decls []cssDecl
	n := len(body)
	i := 0
	start := 0
	depth := 0
	for i < n {
		c := body[i]
		switch {
		case c == '/' && i+1 < n && body[i+1] == '*':
			end := strings.Index(body[i+2:], "*/")
			if end < 0 {
				i = n
				continue
			}
			i = i + 2 + end + 2
			continue
		case c == '"' || c == '\'':
			i = skipCSSString(body, i)
			continue
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ';':
			if depth == 0 {
				if d, ok := parseOneCSSDeclaration(body[start:i]); ok {
					decls = append(decls, d)
				}
				start = i + 1
			}
		}
		i++
	}
	if d, ok := parseOneCSSDeclaration(body[start:]); ok {
		decls = append(decls, d)
	}
	return decls
}

func parseOneCSSDeclaration(text string) (cssDecl, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return cssDecl{}, false
	}
	idx := strings.Index(text, ":")
	if idx < 0 {
		return cssDecl{}, false
	}
	prop := strings.ToLower(strings.TrimSpace(text[:idx]))
	value := strings.ToLower(strings.TrimSpace(text[idx+1:]))
	value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
	if prop == "" || value == "" {
		return cssDecl{}, false
	}
	return cssDecl{prop: prop, value: value}, true
}

// hidingReason inspects one rule's declarations for the small set of
// shapes this check recognizes as hiding whatever the rule matches, and
// returns a human-readable reason for the diagnostic message. See
// ruleMatchesElement's doc comment in requiredreach.go for the companion
// selector-matching simplification; this function is the declaration-side
// half of the same documented heuristic.
func hidingReason(decls []cssDecl) (string, bool) {
	var display, visibility, opacity, position, width, height, clip, clipPath, left, top string
	for _, d := range decls {
		switch d.prop {
		case "display":
			display = d.value
		case "visibility":
			visibility = d.value
		case "opacity":
			opacity = d.value
		case "position":
			position = d.value
		case "width":
			width = d.value
		case "height":
			height = d.value
		case "clip":
			clip = d.value
		case "clip-path":
			clipPath = d.value
		case "left":
			left = d.value
		case "top":
			top = d.value
		}
	}
	switch {
	case display == "none":
		return `"display: none"`, true
	case visibility == "hidden":
		return `"visibility: hidden"`, true
	case isZeroCSSNumber(opacity):
		return `"opacity: 0"`, true
	}
	if position != "absolute" {
		return "", false
	}
	if isOnePixelOrZero(width) || isOnePixelOrZero(height) || isZeroSizeClipRect(clip) || isCollapsingClipPath(clipPath) {
		return `"position: absolute" combined with a zero/1px clip box, the common visually-hidden/sr-only pattern`, true
	}
	if isFarOffscreenOffset(left) || isFarOffscreenOffset(top) {
		return `"position: absolute" combined with a large off-screen offset`, true
	}
	return "", false
}

func isZeroCSSNumber(v string) bool {
	switch strings.TrimSpace(v) {
	case "0", "0.0", "0%", ".0", "0.00":
		return true
	default:
		return false
	}
}

func isOnePixelOrZero(v string) bool {
	switch strings.TrimSpace(v) {
	case "1px", "0", "0px":
		return true
	default:
		return false
	}
}

func isZeroSizeClipRect(v string) bool {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "rect(") || !strings.HasSuffix(v, ")") {
		return false
	}
	inner := v[len("rect(") : len(v)-1]
	parts := strings.FieldsFunc(inner, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		switch p {
		case "0", "0px", "0%":
		default:
			return false
		}
	}
	return true
}

func isCollapsingClipPath(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "inset(50%") || strings.Contains(v, "inset(100%") || strings.HasPrefix(v, "polygon(0")
}

// isFarOffscreenOffset reports whether v is a negative length with at
// least 4 digits of magnitude ("-9999px" and similar), the common
// off-screen-positioning idiom -- an ordinary small negative offset used
// for everyday layout ("-8px", "-20px") never matches.
func isFarOffscreenOffset(v string) bool {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "-") {
		return false
	}
	digits := 0
	for _, r := range v {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 4
}

// parseSelectorList splits a CSS selector list on top-level commas and
// reduces each entry to its rightmost compound selector.
func parseSelectorList(selectorList string) []simpleSelector {
	var out []simpleSelector
	for _, raw := range splitTopLevelComma(selectorList) {
		sel := strings.TrimSpace(raw)
		if sel == "" {
			continue
		}
		out = append(out, rightmostCompoundSelector(sel))
	}
	return out
}

func splitTopLevelComma(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// rightmostCompoundSelector returns the last combinator-separated compound
// in sel -- see ruleMatchesElement's doc comment for why only the
// rightmost compound is matched.
func rightmostCompoundSelector(sel string) simpleSelector {
	segments := splitCSSCombinators(sel)
	if len(segments) == 0 {
		return simpleSelector{}
	}
	return parseCompoundSelector(segments[len(segments)-1])
}

func splitCSSCombinators(sel string) []string {
	var segments []string
	depth := 0
	var current strings.Builder
	flush := func() {
		s := strings.TrimSpace(current.String())
		if s != "" {
			segments = append(segments, s)
		}
		current.Reset()
	}
	for i := 0; i < len(sel); i++ {
		c := sel[i]
		switch {
		case c == '[' || c == '(':
			depth++
			current.WriteByte(c)
		case c == ']' || c == ')':
			if depth > 0 {
				depth--
			}
			current.WriteByte(c)
		case depth == 0 && (c == '>' || c == '+' || c == '~'):
			flush()
		case depth == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			flush()
		default:
			current.WriteByte(c)
		}
	}
	flush()
	return segments
}

// parseCompoundSelector reads one combinator-free compound selector
// ("input.sr-only#field:focus[type=text]") into its tag, class list, and
// id, discarding pseudo-classes/elements and attribute selectors -- this
// check has no per-state or per-attribute information about an element to
// match those against, so it deliberately widens the match rather than
// silently mis-parsing one into a wrong tag/class/id.
func parseCompoundSelector(s string) simpleSelector {
	var sel simpleSelector
	n := len(s)
	i := 0
	for i < n {
		c := s[i]
		switch {
		case c == '.':
			j := i + 1
			for j < n && isCSSIdentChar(s[j]) {
				j++
			}
			sel.classes = append(sel.classes, s[i+1:j])
			i = j
		case c == '#':
			j := i + 1
			for j < n && isCSSIdentChar(s[j]) {
				j++
			}
			sel.id = s[i+1 : j]
			i = j
		case c == ':':
			j := i + 1
			if j < n && s[j] == ':' {
				j++
			}
			for j < n && isCSSIdentChar(s[j]) {
				j++
			}
			if j < n && s[j] == '(' {
				depth := 1
				j++
				for j < n && depth > 0 {
					if s[j] == '(' {
						depth++
					}
					if s[j] == ')' {
						depth--
					}
					j++
				}
			}
			i = j
		case c == '[':
			depth := 1
			j := i + 1
			for j < n && depth > 0 {
				if s[j] == '[' {
					depth++
				}
				if s[j] == ']' {
					depth--
				}
				j++
			}
			i = j
		case c == '*':
			i++
		case isCSSIdentChar(c):
			j := i
			for j < n && isCSSIdentChar(s[j]) {
				j++
			}
			sel.tag = s[i:j]
			i = j
		default:
			i++
		}
	}
	return sel
}

func isCSSIdentChar(c byte) bool {
	return c == '-' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
