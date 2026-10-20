// Package format provides a canonical formatter for GoSX source files.
//
// The formatter preserves normal Go formatting expectations while adding
// consistent formatting for GSX element/attribute/children syntax.
package format

import (
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx"
)

// Source formats a GoSX source file.
func Source(source []byte) ([]byte, error) {
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	f := &formatter{
		src:    source,
		lang:   lang,
		indent: "\t",
	}
	result := f.format(root, 0)
	return []byte(result), nil
}

// Options controls formatter behavior.
type Options struct {
	// IndentStr is the indentation string (default: "\t").
	IndentStr string
	// MaxLineWidth triggers wrapping for long attribute lists (default: 100).
	MaxLineWidth int
}

type formatter struct {
	src        []byte
	lang       *gotreesitter.Language
	indent     string
	maxWidth   int
	baseIndent string
}

func (f *formatter) text(n *gotreesitter.Node) string {
	return string(f.src[n.StartByte():n.EndByte()])
}

func (f *formatter) nodeType(n *gotreesitter.Node) string {
	return n.Type(f.lang)
}

func (f *formatter) childByField(n *gotreesitter.Node, name string) *gotreesitter.Node {
	return n.ChildByFieldName(name, f.lang)
}

func (f *formatter) format(n *gotreesitter.Node, depth int) string {
	switch f.nodeType(n) {
	case "jsx_element":
		return f.formatElement(n, depth)
	case "jsx_raw_text_element":
		return f.formatRawTextElement(n)
	case "jsx_self_closing_element":
		return f.formatSelfClosing(n, depth)
	case "jsx_fragment":
		return f.formatFragment(n, depth)
	case "jsx_expression_container":
		return f.formatExprContainer(n)
	case "jsx_text":
		return f.text(n)
	case "raw_string_literal", "interpreted_string_literal":
		return f.text(n)
	default:
		return f.formatDefault(n, depth)
	}
}

func (f *formatter) formatElement(n *gotreesitter.Node, depth int) string {
	openNode := f.childByField(n, "open")
	closeNode := f.childByField(n, "close")
	if openNode == nil || closeNode == nil {
		return f.text(n)
	}

	tag := f.extractTagName(openNode)
	// Preformatted descendants may contain markup, expressions, and literal
	// whitespace. Preserve the whole span, including their nested text.
	if tag == "pre" || tag == "textarea" {
		return f.text(n)
	}
	attrs := f.collectAttrs(openNode)

	var b strings.Builder

	// Opening tag
	b.WriteByte('<')
	b.WriteString(tag)

	// Format attributes
	if len(attrs) > 0 {
		attrStr := f.formatAttrs(attrs, depth)
		multiline := strings.Contains(attrStr, "\n")
		if multiline {
			b.WriteByte('\n')
			b.WriteString(attrStr)
			b.WriteByte('\n')
			b.WriteString(f.indentation(depth))
		} else {
			b.WriteString(attrStr)
		}
	}
	b.WriteByte('>')

	b.WriteString(f.formatChildren(n, openNode.EndByte(), closeNode.StartByte(), depth))

	// Closing tag
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')

	return b.String()
}

func (f *formatter) formatSelfClosing(n *gotreesitter.Node, depth int) string {
	tag := f.extractTagName(n)
	attrs := f.collectAttrs(n)

	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(tag)

	if len(attrs) > 0 {
		attrStr := f.formatAttrs(attrs, depth)
		multiline := strings.Contains(attrStr, "\n")
		if multiline {
			b.WriteByte('\n')
			b.WriteString(attrStr)
			b.WriteByte('\n')
			b.WriteString(f.indentation(depth))
		} else {
			b.WriteString(attrStr)
		}
	}

	b.WriteString(" />")
	return b.String()
}

func (f *formatter) formatFragment(n *gotreesitter.Node, depth int) string {
	return "<>" + f.formatChildren(n, n.StartByte()+2, n.EndByte()-3, depth) + "</>"
}

func (f *formatter) formatExprContainer(n *gotreesitter.Node) string {
	return f.text(n)
}

// Child boundaries are content: introducing a newline between adjacent
// elements adds a text node, and reflowing nonempty text changes its value.
// Only existing whitespace-only line breaks can be reindented safely:
// IR collapses those nodes to a space and generated Go omits them.
func (f *formatter) formatChildren(n *gotreesitter.Node, start, end uint32, depth int) string {
	var b strings.Builder
	lastEnd := start
	for _, child := range f.collectChildren(n) {
		b.Write(f.src[lastEnd:child.StartByte()])
		text := f.text(child)
		if f.nodeType(child) == "jsx_text" && strings.TrimSpace(text) == "" && strings.Contains(text, "\n") {
			childDepth := depth + 1
			if child.EndByte() == end {
				childDepth = depth
			}
			b.WriteByte('\n')
			b.WriteString(f.indentation(childDepth))
		} else {
			b.WriteString(f.format(child, depth+1))
		}
		lastEnd = child.EndByte()
	}
	b.Write(f.src[lastEnd:end])
	return b.String()
}

func (f *formatter) formatDefault(n *gotreesitter.Node, depth int) string {
	if n.NamedChildCount() == 0 {
		return f.text(n)
	}

	var b strings.Builder
	lastEnd := n.StartByte()

	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)

		if child.StartByte() > lastEnd {
			b.Write(f.src[lastEnd:child.StartByte()])
		}

		childType := f.nodeType(child)
		if childType == "jsx_element" || childType == "jsx_raw_text_element" || childType == "jsx_self_closing_element" || childType == "jsx_fragment" {
			f.baseIndent = f.lineLeadingWhitespace(child.StartByte())
			b.WriteString(f.format(child, depth))
		} else {
			b.WriteString(f.formatDefault(child, depth))
		}

		lastEnd = child.EndByte()
	}

	if lastEnd < n.EndByte() {
		b.Write(f.src[lastEnd:n.EndByte()])
	}

	return b.String()
}

func (f *formatter) indentation(depth int) string {
	return f.baseIndent + strings.Repeat(f.indent, depth)
}

func (f *formatter) lineLeadingWhitespace(pos uint32) string {
	if pos == 0 || len(f.src) == 0 {
		return ""
	}
	idx := int(pos)
	if idx > len(f.src) {
		idx = len(f.src)
	}
	lineStart := idx - 1
	for lineStart >= 0 && f.src[lineStart] != '\n' {
		lineStart--
	}
	lineStart++
	lineEnd := lineStart
	for lineEnd < idx {
		if f.src[lineEnd] != ' ' && f.src[lineEnd] != '\t' {
			break
		}
		lineEnd++
	}
	return string(f.src[lineStart:lineEnd])
}

func (f *formatter) formatAttrs(attrs []*gotreesitter.Node, depth int) string {
	// Try single-line first
	var parts []string
	for _, attr := range attrs {
		parts = append(parts, f.text(attr))
	}
	single := " " + strings.Join(parts, " ")

	maxWidth := f.maxWidth
	if maxWidth == 0 {
		maxWidth = 100
	}

	if !strings.Contains(single, "\n") && len(single) < maxWidth-depth*len(f.indent) {
		return single
	}

	// Multi-line: one attribute per line
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(f.indentation(depth + 1))
		b.WriteString(part)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func (f *formatter) collectAttrs(n *gotreesitter.Node) []*gotreesitter.Node {
	var attrs []*gotreesitter.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := f.nodeType(child)
		if typ == "jsx_attribute" || typ == "jsx_spread_attribute" {
			attrs = append(attrs, child)
		}
	}
	return attrs
}

func (f *formatter) collectChildren(n *gotreesitter.Node) []*gotreesitter.Node {
	var children []*gotreesitter.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		child := n.NamedChild(i)
		typ := f.nodeType(child)
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" {
			continue
		}
		if typ == "jsx_element" || typ == "jsx_raw_text_element" ||
			typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			children = append(children, child)
		}
	}
	return children
}

// formatRawTextElement emits <script>/<style> exactly as written. Their bodies
// are script and stylesheet source, so the formatter must not reindent or
// reflow them: re-wrapping a line inside a JS template literal changes the
// string it produces. Returning the original span also keeps `gosx fmt`
// idempotent over these elements.
func (f *formatter) formatRawTextElement(n *gotreesitter.Node) string {
	return f.text(n)
}

func (f *formatter) extractTagName(n *gotreesitter.Node) string {
	nameNode := f.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	return f.text(nameNode)
}
