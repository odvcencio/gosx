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
	case "jsx_raw_text_element":
		return f.formatRawTextElement(n)
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
	} else {
		b.WriteString(f.formatChildren(children, depth))
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
	if len(children) > 0 {
		b.WriteString(f.formatChildren(children, depth))
	}
	b.WriteString("</>")
	return b.String()
}

func (f *formatter) formatExprContainer(n *gotreesitter.Node) string {
	exprNode := f.childByField(n, "expression")
	if exprNode == nil {
		// The grammar requires an expression, but a comment-only container
		// can still reach the formatter before Compile reports the missing
		// expression. Keep the source intact instead of deleting its comment.
		return f.text(n)
	}

	// Go comments are extras rather than part of the expression node span.
	// Preserve a leading/trailing comment together with its surrounding
	// container; rebuilding from only exprNode would silently erase it.
	innerStart := int(n.StartByte()) + 1
	innerEnd := int(n.EndByte()) - 1
	exprStart := int(exprNode.StartByte())
	exprEnd := int(exprNode.EndByte())
	if exprStart >= innerStart && exprEnd <= innerEnd {
		prefix := string(f.src[innerStart:exprStart])
		suffix := string(f.src[exprEnd:innerEnd])
		if strings.Contains(prefix, "//") || strings.Contains(prefix, "/*") ||
			strings.Contains(suffix, "//") || strings.Contains(suffix, "/*") {
			return f.text(n)
		}
	}
	expr := f.text(exprNode)
	if strings.Contains(expr, "\n") && f.containsStringLiteral(exprNode) {
		expr = f.normalizeMultilineExpr(expr)
	}
	return "{" + expr + "}"
}

func (f *formatter) formatText(n *gotreesitter.Node) string {
	text := f.text(n)
	if strings.TrimSpace(text) == "" {
		// The compiler lowers every whitespace-only jsx_text node to one
		// semantic space. Keep that canonical spelling when the node stays
		// inline; formatChildren may replace it with a source-approved line
		// break when the surrounding child stream is multiline.
		return " "
	}
	// Non-empty text is rendered verbatim by the compiler and renderer. In
	// particular, a line break or a run of spaces inside prose is not merely
	// formatter indentation: it belongs to the output text node. Returning it
	// unchanged keeps source, formatted source, and repeated formatting on the
	// same rendered HTML.
	return text
}

// formatChildren formats the direct GSX child stream without inventing a
// separator between adjacent children. A newline in a formatted GSX body is
// itself a jsx_text child and therefore renders as a space; inserting one
// around every child changes `(<expr>)` into `( <expr> )` and joins such as
// `<b>A</b><i>B</i>` into a spaced sequence. The only safe place to break is
// a standalone whitespace-only child that was already present in the source.
// That child is represented by the formatter's line break and indentation,
// preserving the compiler's one-space rendering contract.
func (f *formatter) formatChildren(children []*gotreesitter.Node, depth int) string {
	if len(children) == 0 {
		return ""
	}

	type childInfo struct {
		node           *gotreesitter.Node
		whitespaceOnly bool
	}
	infos := make([]childInfo, 0, len(children))
	nonWhitespace := 0
	hasWhitespaceBoundary := false
	for _, child := range children {
		info := childInfo{node: child}
		if f.nodeType(child) == "jsx_text" {
			info.whitespaceOnly = strings.TrimSpace(f.text(child)) == ""
		}
		if info.whitespaceOnly {
			hasWhitespaceBoundary = true
		}
		if !info.whitespaceOnly {
			nonWhitespace++
		}
		infos = append(infos, info)
	}

	// A whitespace-only child is the source-level permission to make a
	// structural line break. Keep a body made solely of whitespace inline
	// (`<p> </p>`); there is no useful wrapping to do and the inline spelling
	// is the least surprising canonical result.
	multiline := hasWhitespaceBoundary && nonWhitespace > 0
	if !multiline {
		var b strings.Builder
		for _, info := range infos {
			b.WriteString(f.format(info.node, depth))
		}
		return b.String()
	}

	var b strings.Builder
	pendingBreak := false
	for _, info := range infos {
		if info.whitespaceOnly {
			// The whitespace-only child is emitted either as a literal space
			// (when the run remains inline) or as this pending source-approved
			// line break. Consecutive whitespace-only nodes coalesce here
			// without quadratic concatenation.
			pendingBreak = true
			continue
		}

		childDepth := depth
		if pendingBreak {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(f.indent, depth+1))
			childDepth = depth + 1
			pendingBreak = false
		}
		b.WriteString(f.format(info.node, childDepth))
	}
	if pendingBreak {
		// A trailing whitespace-only child is retained as a source-approved
		// newline. Align the closing tag with this element/fragment's depth.
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(f.indent, depth))
	}
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
		if childType == "jsx_element" || childType == "jsx_raw_text_element" ||
			childType == "jsx_self_closing_element" || childType == "jsx_fragment" {
			// Format the embedded GSX node at the next structural depth so
			// its own child lines have the right code indentation. Do not
			// prefix every rendered line afterwards: a multiline jsx_text node
			// is output verbatim, and adding Go indentation to it changes the
			// rendered prose (and compounds on every formatter pass).
			b.WriteString(f.format(child, depth+1))
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
		if strings.TrimSpace(line) == "" {
			lines[i] = ""
			changed = true
			continue
		}
		// One leading indent belongs to the Go expression that contains
		// this multiline literal. Leave the literal's own first indent in
		// place so a second formatter pass does not keep shifting its
		// contents left.
		if strings.HasPrefix(line, f.indent+f.indent) {
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
