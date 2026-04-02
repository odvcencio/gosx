//go:build !js || !wasm

package markdown

import (
	"html"
	"sort"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// highlightCodeImpl parses source with the gotreesitter grammar for language,
// walks the tree, and emits <span class="hl-{category}"> wrapped tokens.
func highlightCodeImpl(language, source string) (string, bool) {
	if source == "" || language == "" {
		return "", false
	}

	entry := grammars.DetectLanguageByName(language)
	if entry == nil {
		return "", false
	}

	lang := entry.Language()
	if lang == nil {
		return "", false
	}

	parser := gotreesitter.NewParser(lang)
	src := []byte(source)

	var tree *gotreesitter.Tree
	var err error
	if entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(src, lang)
		tree, err = parser.ParseWithTokenSource(src, ts)
	} else {
		tree, err = parser.Parse(src)
	}
	if err != nil || tree == nil {
		return "", false
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)
	root := bt.RootNode()
	if root == nil {
		return "", false
	}

	var spans []hlSpan
	collectHighlightSpans(bt, root, &spans)

	if len(spans) == 0 {
		return "", false
	}

	return renderHighlightedHTML(source, spans), true
}

// hlSpan marks a byte range in the source with a highlight class.
type hlSpan struct {
	start uint32
	end   uint32
	class string
}

// collectHighlightSpans walks the tree recursively, classifying leaf nodes
// into highlight categories and recording their spans.
func collectHighlightSpans(bt *gotreesitter.BoundTree, n *gotreesitter.Node, spans *[]hlSpan) {
	if n == nil {
		return
	}
	if class := classifyNode(bt, n); class != "" && n.EndByte() > n.StartByte() {
		*spans = append(*spans, hlSpan{
			start: n.StartByte(),
			end:   n.EndByte(),
			class: class,
		})
		return
	}
	for i := 0; i < n.ChildCount(); i++ {
		collectHighlightSpans(bt, n.Child(i), spans)
	}
}

// classifyNode maps a tree-sitter node to a highlight category class name.
func classifyNode(bt *gotreesitter.BoundTree, n *gotreesitter.Node) string {
	nodeType := bt.NodeType(n)
	text := bt.NodeText(n)

	// Comments
	if strings.Contains(nodeType, "comment") {
		return "hl-comment"
	}

	// Strings
	if strings.Contains(nodeType, "string") {
		return "hl-string"
	}
	switch nodeType {
	case "interpreted_string_literal", "raw_string_literal", "string_literal",
		"template_string", "string_content", "escape_sequence":
		return "hl-string"
	}

	// Numbers
	switch nodeType {
	case "int_literal", "float_literal", "imaginary_literal", "rune_literal",
		"number", "integer", "float":
		return "hl-number"
	}

	// Types
	switch nodeType {
	case "type_identifier", "type_conversion_expression":
		return "hl-type"
	}

	// Functions — identifier under call expression or function declaration
	if nodeType == "identifier" || nodeType == "field_identifier" || nodeType == "property_identifier" {
		if parent := n.Parent(); parent != nil {
			parentType := bt.NodeType(parent)
			if parentType == "call_expression" || parentType == "function_declaration" ||
				parentType == "method_declaration" {
				// First child of call_expression is typically the function name
				if parent.Child(0) == n {
					return "hl-function"
				}
			}
		}
	}

	// Boolean and nil literals
	switch text {
	case "true", "false", "True", "False":
		return "hl-keyword"
	case "nil", "null", "undefined", "None":
		return "hl-keyword"
	}

	// Keywords
	if hlKeywords[nodeType] || hlKeywords[text] {
		return "hl-keyword"
	}

	// Identifiers that look like types (start with uppercase) — Go convention
	if nodeType == "identifier" && len(text) > 0 && text[0] >= 'A' && text[0] <= 'Z' {
		return "hl-type"
	}

	// Operators
	if isHLOperator(nodeType) || isHLOperator(text) {
		return "hl-operator"
	}

	// Punctuation
	if isHLPunctuation(nodeType) {
		return "hl-punctuation"
	}

	return ""
}

// renderHighlightedHTML renders source with highlight spans applied.
func renderHighlightedHTML(source string, spans []hlSpan) string {
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})

	src := []byte(source)
	pos := uint32(0)
	end := uint32(len(src))

	var b strings.Builder
	b.Grow(len(source) * 2)

	for _, span := range spans {
		if span.start < pos || span.end > end || span.end <= pos {
			continue
		}
		if span.start > pos {
			b.WriteString(html.EscapeString(string(src[pos:span.start])))
		}
		b.WriteString(`<span class="`)
		b.WriteString(span.class)
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(string(src[span.start:span.end])))
		b.WriteString(`</span>`)
		pos = span.end
	}
	if pos < end {
		b.WriteString(html.EscapeString(string(src[pos:end])))
	}
	return b.String()
}

// hlKeywords contains common keyword tokens across languages.
var hlKeywords = map[string]bool{
	// Node type names
	"keyword": true,

	// Common keyword texts
	"async": true, "await": true, "break": true, "case": true, "chan": true,
	"class": true, "const": true, "continue": true, "default": true, "def": true,
	"defer": true, "del": true, "elif": true, "else": true, "except": true,
	"export": true, "extends": true, "fallthrough": true, "finally": true,
	"for": true, "from": true, "func": true, "function": true, "go": true,
	"goto": true, "if": true, "import": true, "in": true, "interface": true,
	"is": true, "lambda": true, "let": true, "map": true, "new": true,
	"not": true, "or": true, "and": true, "package": true, "pass": true,
	"raise": true, "range": true, "return": true, "select": true,
	"struct": true, "switch": true, "try": true, "type": true, "var": true,
	"while": true, "with": true, "yield": true,
}

func isHLOperator(token string) bool {
	switch token {
	case "+", "-", "*", "/", "%", "=", "==", "!=", "<", ">", "<=", ">=",
		":=", "&&", "||", "!", "...", "**", "//", "+=", "-=", "*=", "/=",
		"&", "|", "^", "~", "<<", ">>":
		return true
	default:
		return false
	}
}

func isHLPunctuation(token string) bool {
	switch token {
	case "(", ")", "{", "}", "[", "]", ",", ".", ":", ";":
		return true
	default:
		return false
	}
}
