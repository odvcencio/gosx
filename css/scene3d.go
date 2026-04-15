package css

import (
	"strings"
	"unicode"
)

// Scene3DStylesheet is the compiler-owned CSS subset extracted from @scene3d.
// Browser CSS never sees these rules; route lowering applies them to Scene3D
// props before the runtime manifest is emitted.
type Scene3DStylesheet struct {
	Rules []Scene3DStyleRule
}

// Merge appends stylesheet rules while preserving cascade order.
func (s Scene3DStylesheet) Merge(next Scene3DStylesheet) Scene3DStylesheet {
	if len(next.Rules) == 0 {
		return s
	}
	out := Scene3DStylesheet{Rules: make([]Scene3DStyleRule, 0, len(s.Rules)+len(next.Rules))}
	out.Rules = append(out.Rules, s.Rules...)
	out.Rules = append(out.Rules, next.Rules...)
	return out
}

// Scene3DStyleRule is one selector/declaration block inside @scene3d.
type Scene3DStyleRule struct {
	Selector     string
	Declarations []Scene3DDeclaration
}

// Scene3DDeclaration is one property/value pair inside a Scene3D style rule.
type Scene3DDeclaration struct {
	Name  string
	Value string
}

// ExtractScene3DStyles removes @scene3d blocks from browser CSS and returns the
// parsed Scene3D stylesheet. The parser intentionally accepts a small CSS-like
// subset: selector blocks with declarations, plus nested grouping at-rules whose
// children are flattened for the compiler.
func ExtractScene3DStyles(source string) (string, Scene3DStylesheet) {
	var out strings.Builder
	var sheet Scene3DStylesheet
	for pos := 0; pos < len(source); {
		idx := findScene3DAtRule(source, pos)
		if idx < 0 {
			out.WriteString(source[pos:])
			break
		}
		brace := skipScene3DSpaces(source, idx+len("@scene3d"))
		if brace >= len(source) || source[brace] != '{' {
			out.WriteString(source[pos : idx+len("@scene3d")])
			pos = idx + len("@scene3d")
			continue
		}
		end := findScene3DMatchingBrace(source, brace)
		if end < 0 {
			out.WriteString(source[pos:])
			break
		}
		out.WriteString(source[pos:idx])
		sheet.Rules = append(sheet.Rules, parseScene3DRules(source[brace+1:end])...)
		pos = end + 1
	}
	return MirrorScene3DNativeProperties(out.String()), sheet
}

// MirrorScene3DNativeProperties keeps author-facing Scene3D CSS property names
// usable in browser CSS by lowering them to custom properties the runtime can
// read through getComputedStyle. Unknown non-custom properties are otherwise
// dropped by the browser cascade.
func MirrorScene3DNativeProperties(source string) string {
	var out strings.Builder
	for pos := 0; pos < len(source); {
		if strings.HasPrefix(source[pos:], "/*") {
			end := findScene3DCommentEnd(source, pos)
			out.WriteString(source[pos:end])
			pos = end
			continue
		}
		if source[pos] == '"' || source[pos] == '\'' {
			end := findScene3DStringEnd(source, pos)
			out.WriteString(source[pos:end])
			pos = end
			continue
		}
		if scene3DPropertyAt(source, pos, "scene-filter") {
			out.WriteString("--scene-filter")
			pos += len("scene-filter")
			continue
		}
		out.WriteByte(source[pos])
		pos++
	}
	return out.String()
}

func scene3DPropertyAt(source string, pos int, name string) bool {
	if pos+len(name) > len(source) || !strings.EqualFold(source[pos:pos+len(name)], name) {
		return false
	}
	if pos > 0 {
		prev := pos - 1
		for prev >= 0 && isScene3DSpace(rune(source[prev])) {
			prev--
		}
		if prev >= 0 && source[prev] != '{' && source[prev] != ';' {
			return false
		}
	}
	next := pos + len(name)
	for next < len(source) && isScene3DSpace(rune(source[next])) {
		next++
	}
	return next < len(source) && source[next] == ':'
}

func findScene3DAtRule(source string, start int) int {
	for pos := start; pos < len(source); pos++ {
		if strings.HasPrefix(source[pos:], "/*") {
			pos = findScene3DCommentEnd(source, pos) - 1
			continue
		}
		if source[pos] == '"' || source[pos] == '\'' {
			pos = findScene3DStringEnd(source, pos) - 1
			continue
		}
		if source[pos] != '@' || pos+len("@scene3d") > len(source) {
			continue
		}
		if !strings.EqualFold(source[pos:pos+len("@scene3d")], "@scene3d") {
			continue
		}
		next := pos + len("@scene3d")
		if next >= len(source) || source[next] == '{' || isScene3DSpace(rune(source[next])) {
			return pos
		}
	}
	return -1
}

func parseScene3DRules(body string) []Scene3DStyleRule {
	rules := []Scene3DStyleRule{}
	for pos := 0; pos < len(body); {
		pos = skipScene3DTrivia(body, pos)
		if pos >= len(body) {
			break
		}
		boundary := findScene3DBlockBoundary(body, pos)
		if boundary < 0 {
			break
		}
		if body[boundary] == ';' {
			pos = boundary + 1
			continue
		}
		end := findScene3DMatchingBrace(body, boundary)
		if end < 0 {
			break
		}
		selector := strings.TrimSpace(body[pos:boundary])
		block := body[boundary+1 : end]
		if strings.HasPrefix(strings.TrimSpace(selector), "@") {
			rules = append(rules, parseScene3DRules(block)...)
		} else if declarations := parseScene3DDeclarations(block); selector != "" && len(declarations) > 0 {
			rules = append(rules, Scene3DStyleRule{
				Selector:     selector,
				Declarations: declarations,
			})
		}
		pos = end + 1
	}
	return rules
}

func parseScene3DDeclarations(block string) []Scene3DDeclaration {
	out := []Scene3DDeclaration{}
	start := 0
	for pos := 0; pos <= len(block); pos++ {
		if pos < len(block) {
			if strings.HasPrefix(block[pos:], "/*") {
				pos = findScene3DCommentEnd(block, pos) - 1
				continue
			}
			if block[pos] == '"' || block[pos] == '\'' {
				pos = findScene3DStringEnd(block, pos) - 1
				continue
			}
			if block[pos] != ';' {
				continue
			}
		}
		if declaration, ok := parseScene3DDeclaration(block[start:pos]); ok {
			out = append(out, declaration)
		}
		start = pos + 1
	}
	return out
}

func parseScene3DDeclaration(source string) (Scene3DDeclaration, bool) {
	colon := findScene3DDeclarationColon(source)
	if colon < 0 {
		return Scene3DDeclaration{}, false
	}
	name := strings.ToLower(strings.TrimSpace(source[:colon]))
	value := strings.TrimSpace(source[colon+1:])
	if strings.HasSuffix(strings.ToLower(value), "!important") {
		value = strings.TrimSpace(value[:len(value)-len("!important")])
	}
	if name == "" || value == "" {
		return Scene3DDeclaration{}, false
	}
	return Scene3DDeclaration{Name: name, Value: value}, true
}

func findScene3DDeclarationColon(source string) int {
	for pos := 0; pos < len(source); pos++ {
		if strings.HasPrefix(source[pos:], "/*") {
			pos = findScene3DCommentEnd(source, pos) - 1
			continue
		}
		if source[pos] == '"' || source[pos] == '\'' {
			pos = findScene3DStringEnd(source, pos) - 1
			continue
		}
		if source[pos] == ':' {
			return pos
		}
	}
	return -1
}

func findScene3DBlockBoundary(source string, start int) int {
	for pos := start; pos < len(source); pos++ {
		if strings.HasPrefix(source[pos:], "/*") {
			pos = findScene3DCommentEnd(source, pos) - 1
			continue
		}
		if source[pos] == '"' || source[pos] == '\'' {
			pos = findScene3DStringEnd(source, pos) - 1
			continue
		}
		if source[pos] == '{' || source[pos] == ';' {
			return pos
		}
	}
	return -1
}

func findScene3DMatchingBrace(source string, open int) int {
	if open < 0 || open >= len(source) || source[open] != '{' {
		return -1
	}
	depth := 0
	for pos := open; pos < len(source); pos++ {
		if strings.HasPrefix(source[pos:], "/*") {
			pos = findScene3DCommentEnd(source, pos) - 1
			continue
		}
		if source[pos] == '"' || source[pos] == '\'' {
			pos = findScene3DStringEnd(source, pos) - 1
			continue
		}
		switch source[pos] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return pos
			}
		}
	}
	return -1
}

func skipScene3DTrivia(source string, pos int) int {
	for pos < len(source) {
		if isScene3DSpace(rune(source[pos])) {
			pos++
			continue
		}
		if strings.HasPrefix(source[pos:], "/*") {
			pos = findScene3DCommentEnd(source, pos)
			continue
		}
		return pos
	}
	return pos
}

func skipScene3DSpaces(source string, pos int) int {
	for pos < len(source) && isScene3DSpace(rune(source[pos])) {
		pos++
	}
	return pos
}

func isScene3DSpace(r rune) bool {
	return unicode.IsSpace(r)
}

func findScene3DCommentEnd(source string, start int) int {
	if end := strings.Index(source[start+2:], "*/"); end >= 0 {
		return start + 2 + end + 2
	}
	return len(source)
}

func findScene3DStringEnd(source string, start int) int {
	quote := source[start]
	for pos := start + 1; pos < len(source); pos++ {
		if source[pos] == '\\' {
			pos++
			continue
		}
		if source[pos] == quote {
			return pos + 1
		}
	}
	return len(source)
}
