// Package css provides lightweight CSS scoping for GoSX components.
package css

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ScopeID generates a short scope identifier from the component or file name.
func ScopeID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:2])
}

// ScopeCSS rewrites selectors so they stay inside the provided scope while
// still matching the scoped root element itself.
func ScopeCSS(source string, scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if source == "" || scopeID == "" {
		return source
	}
	ctx := scopeContext{
		attrSelector: `[data-gosx-s="` + scopeID + `"]`,
		scopeAnchor:  `:where([data-gosx-s="` + scopeID + `"])`,
	}
	return scopeRuleList(source, ctx)
}

// ScopeAttr returns the HTML attribute to add to a scoped root.
func ScopeAttr(scopeID string) string {
	return `data-gosx-s="` + scopeID + `"`
}

type scopeContext struct {
	attrSelector string
	scopeAnchor  string
}

func scopeRuleList(source string, ctx scopeContext) string {
	var out strings.Builder
	for pos := 0; pos < len(source); {
		if strings.HasPrefix(source[pos:], "/*") {
			end := findCSSCommentEnd(source, pos)
			out.WriteString(source[pos:end])
			pos = end
			continue
		}

		boundary, kind := findCSSBoundary(source, pos)
		if boundary < 0 {
			out.WriteString(source[pos:])
			break
		}

		if kind == ';' {
			out.WriteString(source[pos : boundary+1])
			pos = boundary + 1
			continue
		}

		blockEnd := findMatchingBrace(source, boundary)
		if blockEnd < 0 {
			out.WriteString(source[pos:])
			break
		}

		prelude := source[pos:boundary]
		body := source[boundary+1 : blockEnd]
		trimmed := strings.TrimSpace(prelude)

		switch {
		case trimmed == "":
			out.WriteString(prelude)
			out.WriteByte('{')
			out.WriteString(scopeRuleList(body, ctx))
			out.WriteByte('}')
		case strings.HasPrefix(trimmed, "@"):
			lowered := strings.ToLower(trimmed)
			out.WriteString(prelude)
			out.WriteByte('{')
			if cssAtRuleScopesChildren(lowered) {
				out.WriteString(scopeRuleList(body, ctx))
			} else {
				out.WriteString(body)
			}
			out.WriteByte('}')
		default:
			out.WriteString(scopeRulePrelude(prelude, ctx))
			out.WriteByte('{')
			out.WriteString(body)
			out.WriteByte('}')
		}

		pos = blockEnd + 1
	}
	return out.String()
}

func scopeRulePrelude(prelude string, ctx scopeContext) string {
	leading, trimmed, trailing := splitOuterWhitespace(prelude)
	if trimmed == "" {
		return prelude
	}
	return leading + scopeSelectorList(trimmed, ctx) + trailing
}

func splitOuterWhitespace(value string) (string, string, string) {
	start := 0
	for start < len(value) && isCSSSpace(value[start]) {
		start++
	}
	end := len(value)
	for end > start && isCSSSpace(value[end-1]) {
		end--
	}
	return value[:start], value[start:end], value[end:]
}

func scopeSelectorList(selectorList string, ctx scopeContext) string {
	parts := splitSelectorList(selectorList)
	scoped := make([]string, 0, len(parts))
	for _, part := range parts {
		leading, trimmed, trailing := splitOuterWhitespace(part)
		if trimmed == "" {
			continue
		}
		scoped = append(scoped, leading+scopeSingleSelector(trimmed, ctx)+trailing)
	}
	return strings.Join(scoped, ", ")
}

func scopeSingleSelector(selector string, ctx scopeContext) string {
	if global, ok := unwrapGlobalSelector(selector); ok {
		return global
	}
	if isInherentlyGlobalSelector(selector) {
		return selector
	}

	cleaned := replaceGlobalWrappers(selector)
	ancestor := ctx.scopeAnchor + " " + cleaned
	lead, rest := splitLeadingCompound(cleaned)
	if lead == "" {
		return ancestor
	}
	root := scopeLeadingCompound(lead, ctx) + rest
	if root == ancestor {
		return ancestor
	}
	return ancestor + ", " + root
}

// isInherentlyGlobalSelector returns true for selectors that target
// document-level elements or universal contexts outside the component tree.
// Scoping these to :where([data-gosx-s="..."]) would make them ineffective.
func isInherentlyGlobalSelector(selector string) bool {
	s := strings.TrimSpace(selector)
	switch s {
	case ":root", "html", "body", "head",
		"*", "*::before", "*::after",
		"::selection", "::placeholder":
		return true
	}
	return false
}

func scopeLeadingCompound(compound string, ctx scopeContext) string {
	if compound == "" {
		return ctx.scopeAnchor
	}
	if compound == ":root" {
		return ctx.scopeAnchor
	}
	if global, ok := unwrapGlobalSelector(compound); ok {
		return global
	}
	insertAt := findPseudoElementStart(compound)
	if insertAt < 0 {
		return compound + ctx.scopeAnchor
	}
	return compound[:insertAt] + ctx.scopeAnchor + compound[insertAt:]
}

func splitLeadingCompound(selector string) (string, string) {
	for i := 0; i < len(selector); i++ {
		ch := selector[i]
		switch ch {
		case '"', '\'':
			i = advanceCSSString(selector, i)
		case '[':
			i = advanceCSSBracket(selector, i, '[', ']')
		case '(':
			i = advanceCSSBracket(selector, i, '(', ')')
		case '>', '+', '~':
			return selector[:i], selector[i:]
		default:
			if isCSSSpace(ch) {
				return selector[:i], selector[i:]
			}
		}
	}
	return selector, ""
}

func splitSelectorList(selectorList string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(selectorList); i++ {
		switch selectorList[i] {
		case '"', '\'':
			i = advanceCSSString(selectorList, i)
		case '[':
			i = advanceCSSBracket(selectorList, i, '[', ']')
		case '(':
			i = advanceCSSBracket(selectorList, i, '(', ')')
		case ',':
			parts = append(parts, selectorList[start:i])
			start = i + 1
		}
	}
	parts = append(parts, selectorList[start:])
	return parts
}

func replaceGlobalWrappers(selector string) string {
	var out strings.Builder
	for i := 0; i < len(selector); i++ {
		if strings.HasPrefix(selector[i:], ":global(") {
			start := i + len(":global")
			end := advanceCSSBracket(selector, start, '(', ')')
			content := selector[start+1 : end]
			out.WriteString(content)
			i = end
			continue
		}
		out.WriteByte(selector[i])
	}
	return out.String()
}

func unwrapGlobalSelector(selector string) (string, bool) {
	selector = strings.TrimSpace(selector)
	if !strings.HasPrefix(selector, ":global(") || !strings.HasSuffix(selector, ")") {
		return "", false
	}
	end := advanceCSSBracket(selector, len(":global"), '(', ')')
	if end != len(selector)-1 {
		return "", false
	}
	return selector[len(":global")+1 : end], true
}

func findPseudoElementStart(compound string) int {
	for i := 0; i < len(compound); i++ {
		switch compound[i] {
		case '"', '\'':
			i = advanceCSSString(compound, i)
		case '[':
			i = advanceCSSBracket(compound, i, '[', ']')
		case '(':
			i = advanceCSSBracket(compound, i, '(', ')')
		case ':':
			if i+1 < len(compound) && compound[i+1] == ':' {
				return i
			}
		}
	}
	return -1
}

func cssAtRuleScopesChildren(rule string) bool {
	switch {
	case strings.HasPrefix(rule, "@media"),
		strings.HasPrefix(rule, "@supports"),
		strings.HasPrefix(rule, "@layer"),
		strings.HasPrefix(rule, "@container"),
		strings.HasPrefix(rule, "@scope"),
		strings.HasPrefix(rule, "@document"):
		return true
	default:
		return false
	}
}

func findCSSBoundary(source string, start int) (int, byte) {
	for i := start; i < len(source); i++ {
		switch source[i] {
		case '"', '\'':
			i = advanceCSSString(source, i)
		case '[':
			i = advanceCSSBracket(source, i, '[', ']')
		case '(':
			i = advanceCSSBracket(source, i, '(', ')')
		case '/':
			if i+1 < len(source) && source[i+1] == '*' {
				i = findCSSCommentEnd(source, i) - 1
			}
		case '{', ';':
			return i, source[i]
		}
	}
	return -1, 0
}

func findMatchingBrace(source string, open int) int {
	depth := 0
	for i := open; i < len(source); i++ {
		switch source[i] {
		case '"', '\'':
			i = advanceCSSString(source, i)
		case '/':
			if i+1 < len(source) && source[i+1] == '*' {
				i = findCSSCommentEnd(source, i) - 1
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func findCSSCommentEnd(source string, start int) int {
	end := strings.Index(source[start+2:], "*/")
	if end < 0 {
		return len(source)
	}
	return start + 2 + end + 2
}

func advanceCSSString(source string, start int) int {
	quote := source[start]
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		if source[i] == quote {
			return i
		}
	}
	return len(source) - 1
}

func advanceCSSBracket(source string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(source); i++ {
		switch source[i] {
		case '"', '\'':
			i = advanceCSSString(source, i)
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(source) - 1
}

func isCSSSpace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' || ch == '\f'
}
