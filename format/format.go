// Package format provides a canonical formatter for GoSX source files.
//
// The formatter preserves normal Go formatting expectations while adding
// consistent formatting for GSX element/attribute/children syntax.
package format

import (
	"strings"

	"github.com/odvcencio/gosx"
	gotreesitter "github.com/odvcencio/gotreesitter"
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
	src      []byte
	lang     *gotreesitter.Language
	indent   string
	maxWidth int
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
	case "jsx_self_closing_element":
		return f.formatSelfClosing(n, depth)
	case "jsx_fragment":
		return f.formatFragment(n, depth)
	case "jsx_expression_container":
		return f.formatExprContainer(n)
	case "jsx_text":
		return f.formatText(n)
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
	attrs := f.collectAttrs(openNode)
	children := f.collectChildren(n)

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
			b.WriteString(strings.Repeat(f.indent, depth))
		} else {
			b.WriteString(attrStr)
		}
	}
	b.WriteByte('>')

	// Format children
	if len(children) == 0 {
		// Empty: <tag></tag>
	} else if len(children) == 1 && f.isInlineChild(children[0]) {
		// Single inline child: <tag>text</tag>
		b.WriteString(f.format(children[0], depth))
	} else {
		// Multi-line children
		for _, child := range children {
			childStr := f.format(child, depth+1)
			if strings.TrimSpace(childStr) == "" {
				continue
			}
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(f.indent, depth+1))
			b.WriteString(strings.TrimSpace(childStr))
		}
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(f.indent, depth))
	}

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
			b.WriteString(strings.Repeat(f.indent, depth))
		} else {
			b.WriteString(attrStr)
		}
	}

	b.WriteString(" />")
	return b.String()
}

func (f *formatter) formatFragment(n *gotreesitter.Node, depth int) string {
	children := f.collectChildren(n)

	var b strings.Builder
	b.WriteString("<>")

	for _, child := range children {
		childStr := f.format(child, depth+1)
		if strings.TrimSpace(childStr) == "" {
			continue
		}
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(f.indent, depth+1))
		b.WriteString(strings.TrimSpace(childStr))
	}

	b.WriteByte('\n')
	b.WriteString(strings.Repeat(f.indent, depth))
	b.WriteString("</>")
	return b.String()
}

func (f *formatter) formatExprContainer(n *gotreesitter.Node) string {
	exprNode := f.childByField(n, "expression")
	if exprNode == nil {
		return "{}"
	}
	expr := f.text(exprNode)
	if strings.Contains(expr, "\n") && f.containsStringLiteral(exprNode) {
		expr = f.normalizeMultilineExpr(expr)
	}
	return "{" + expr + "}"
}

func (f *formatter) formatText(n *gotreesitter.Node) string {
	text := f.text(n)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
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
		if childType == "jsx_element" || childType == "jsx_self_closing_element" || childType == "jsx_fragment" {
			childStr := f.format(child, depth)
			b.WriteString(f.indentEmbedded(childStr, f.lineLeadingWhitespace(child.StartByte())))
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

func (f *formatter) indentEmbedded(text string, prefix string) string {
	if prefix == "" || !strings.Contains(text, "\n") {
		return text
	}
	return strings.ReplaceAll(text, "\n", "\n"+prefix)
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

func (f *formatter) normalizeMultilineExpr(expr string) string {
	lines := strings.Split(expr, "\n")
	if len(lines) < 2 {
		return expr
	}

	changed := false
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, f.indent) {
			lines[i] = strings.TrimPrefix(line, f.indent)
			changed = true
		}
	}
	if !changed {
		return expr
	}
	return strings.Join(lines, "\n")
}

func (f *formatter) containsStringLiteral(n *gotreesitter.Node) bool {
	switch f.nodeType(n) {
	case "raw_string_literal", "interpreted_string_literal":
		return true
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if f.containsStringLiteral(n.NamedChild(i)) {
			return true
		}
	}
	return false
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

	if len(single) < maxWidth-depth*len(f.indent) {
		return single
	}

	// Multi-line: one attribute per line
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(strings.Repeat(f.indent, depth+1))
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
		if typ == "jsx_element" || typ == "jsx_self_closing_element" ||
			typ == "jsx_expression_container" || typ == "jsx_fragment" ||
			typ == "jsx_text" {
			children = append(children, child)
		}
	}
	return children
}

func (f *formatter) extractTagName(n *gotreesitter.Node) string {
	nameNode := f.childByField(n, "name")
	if nameNode == nil {
		return ""
	}
	return f.text(nameNode)
}

func (f *formatter) isInlineChild(n *gotreesitter.Node) bool {
	typ := f.nodeType(n)
	if typ == "jsx_text" {
		return len(strings.TrimSpace(f.text(n))) < 40
	}
	if typ == "jsx_expression_container" {
		return len(f.text(n)) < 40
	}
	return false
}
