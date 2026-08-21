// Package format provides a canonical formatter for GoSX source files.
//
// The formatter preserves normal Go formatting expectations while treating GSX
// markup as an ordered source stream. Markup whitespace is semantic: the
// shared lowerer normalizes line-wise JSX layout and preserves same-line
// separators, while expressions, attributes, comments, and raw bodies remain
// opaque. A formatter therefore only changes grammar-safe tag gaps and the
// layout around structural-only child runs.
package format

import (
	"fmt"
	"strings"
	"unicode/utf8"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/syntax"
)

// Source formats a GoSX source file.
func Source(source []byte) ([]byte, error) {
	if err := validateUTF8(source); err != nil {
		return nil, err
	}
	tree, lang, err := gosx.Parse(source)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()
	if root == nil {
		return nil, fmt.Errorf("parse produced no syntax tree")
	}
	// gosx.Parse intentionally returns a tree even when tree-sitter has
	// recovered an ERROR or MISSING node. Formatting recovered input is unsafe:
	// a reconstructed tag can make the invalid source look valid while dropping
	// the bytes that caused the diagnostic. Refuse it before any output is
	// produced and reuse GoSX's located diagnostic formatter.
	if err := gosx.DescribeParseError(root, source, lang); err != nil {
		return nil, err
	}
	if err := gosx.ValidateMarkupTree(root, source, lang); err != nil {
		return nil, err
	}
	f := &formatter{
		src:  source,
		lang: lang,
	}
	result := f.format(root, 0, "")
	return []byte(result), nil
}

type formatter struct {
	src  []byte
	lang *gotreesitter.Language
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

// format renders a node at its destination line indentation. The first
// formatter call is rooted at the Go source stream; after that, structural
// children receive the indentation selected by their parent. This distinction
// makes compact nested trees converge in one pass instead of letting each
// child reseed itself from its old source line.
func (f *formatter) format(n *gotreesitter.Node, depth int, lineIndent string) string {
	switch f.nodeType(n) {
	case "jsx_element":
		return f.formatElement(n, depth, lineIndent)
	case "jsx_raw_text_element":
		return f.formatRawTextElement(n, depth, lineIndent)
	case "jsx_self_closing_element":
		return f.formatSelfClosing(n, depth, lineIndent)
	case "jsx_fragment":
		return f.formatFragment(n, depth, lineIndent)
	case "jsx_expression_container":
		return f.formatExprContainer(n)
	case "jsx_text":
		return f.formatText(n)
	case "raw_string_literal", "interpreted_string_literal":
		return f.text(n)
	default:
		return f.formatDefault(n, depth, lineIndent)
	}
}

func (f *formatter) formatElement(n *gotreesitter.Node, depth int, lineIndent string) string {
	openNode := f.childByField(n, "open")
	closeNode := f.childByField(n, "close")
	if openNode == nil || closeNode == nil {
		return f.text(n)
	}

	// Keep each complete opening/closing tag under one gap model. Collecting
	// only named attributes loses comments and trivia between attributes, while
	// reconstructing an expression-valued attribute can change its source even
	// when its HTML result is unchanged. formatTag edits only grammar-safe gaps
	// around the already-parsed opaque spans.
	children := f.collectChildren(n)
	var b strings.Builder
	b.WriteString(f.formatTag(openNode, false, depth, lineIndent))
	if f.shouldBlock(children, openNode.EndByte(), closeNode.StartByte()) {
		b.WriteString(f.formatBlockChildren(children, depth, lineIndent))
	} else {
		b.WriteString(f.formatChildStream(n, openNode.EndByte(), closeNode.StartByte(), depth, lineIndent))
	}
	b.WriteString(f.text(closeNode))
	return b.String()
}

func (f *formatter) formatSelfClosing(n *gotreesitter.Node, depth int, lineIndent string) string {
	return f.formatTag(n, true, depth, lineIndent)
}

func (f *formatter) formatFragment(n *gotreesitter.Node, depth int, lineIndent string) string {
	// Fragment delimiters are fixed-width for a valid tree. Use the same
	// position-preserving child stream as an ordinary element so compact runs
	// stay compact and block-shaped runs retain one token per source separator.
	if n.EndByte() < n.StartByte()+5 {
		return f.text(n)
	}
	start := n.StartByte()
	end := n.EndByte()
	var b strings.Builder
	b.Write(f.src[start : start+2]) // <>
	children := f.collectChildren(n)
	if f.shouldBlock(children, start+2, end-3) {
		b.WriteString(f.formatBlockChildren(children, depth, lineIndent))
	} else {
		b.WriteString(f.formatChildStream(n, start+2, end-3, depth, lineIndent))
	}
	b.Write(f.src[end-3 : end]) // </>
	return b.String()
}

func (f *formatter) formatExprContainer(n *gotreesitter.Node) string {
	// Expressions are opaque source. Rewrapping a raw/interpreted string,
	// changing comment trivia, or rebuilding braces around a recovered child can
	// alter author data. Parse errors are rejected by Source before this method
	// is reached.
	return f.text(n)
}

func (f *formatter) formatText(n *gotreesitter.Node) string {
	// Text tokens are always source-opaque to the formatter. Whether an exact
	// token is layout-only is decided by the shared syntax.RenderText policy at
	// the one place that may discard it: structural block conversion.
	return f.text(n)
}

func (f *formatter) formatDefault(n *gotreesitter.Node, depth int, lineIndent string) string {
	if n.NamedChildCount() == 0 {
		return f.text(n)
	}

	var b strings.Builder
	lastEnd := n.StartByte()

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child == nil {
			continue
		}
		if child.StartByte() > lastEnd {
			b.Write(f.src[lastEnd:child.StartByte()])
		}

		if isGSXNodeType(f.nodeType(child)) {
			// GSX children own their source stream. Do not post-indent an
			// already-rendered string: that opaque rewrite was the source of
			// semantic whitespace drift and raw-string corruption.
			// This is the only place where source indentation seeds the GSX
			// destination. Once a child is inside a GSX block, its parent passes
			// the destination indentation recursively.
			b.WriteString(f.format(child, depth, f.sourceIndent(child.StartByte())))
		} else {
			b.WriteString(f.formatDefault(child, depth, lineIndent))
		}
		if child.EndByte() > lastEnd {
			lastEnd = child.EndByte()
		}
	}

	if lastEnd < n.EndByte() {
		b.Write(f.src[lastEnd:n.EndByte()])
	}
	return b.String()
}

// formatRawTextElement emits <script>/<style> exactly as written. Their
// bodies are script and stylesheet source, so reindentation can change a
// template literal or another whitespace-sensitive construct.
func (f *formatter) formatRawTextElement(n *gotreesitter.Node, depth int, lineIndent string) string {
	open := f.childByField(n, "open")
	if open == nil {
		return f.text(n)
	}
	var b strings.Builder
	b.WriteString(f.formatTag(open, false, depth, lineIndent))
	b.Write(f.src[open.EndByte():n.EndByte()])
	return b.String()
}

// formatTag canonicalizes only gaps between the tag name, complete parsed
// attribute nodes, and the closing delimiter. A complete attribute node is a
// byte-opaque span: spaces around '=', spread syntax, nested expressions,
// quoted values, Go raw strings, and comments are copied without scanning or
// rebuilding them. Comments or other non-whitespace trivia in an inter-node
// gap are likewise preserved exactly.
func (f *formatter) formatTag(n *gotreesitter.Node, selfClosing bool, depth int, lineIndent string) string {
	raw := f.text(n)
	if raw == "" {
		return raw
	}
	name := f.childByField(n, "name")
	if name == nil {
		return raw
	}
	attrs := f.collectAttrs(n)
	canonical, ok := f.canonicalTagGaps(n, name, attrs, selfClosing)
	if !ok {
		return raw
	}
	if f.shouldWrapTag(n, canonical, selfClosing, lineIndent) {
		return f.wrapTagAttributes(n, canonical, selfClosing, depth, lineIndent)
	}
	return canonical
}

const canonicalTagWidth = 100

func (f *formatter) shouldWrapTag(n *gotreesitter.Node, canonical string, selfClosing bool, lineIndent string) bool {
	if n == nil || len(f.collectAttrs(n)) < 2 {
		return false
	}
	// A comment in a gap is an ordered source token, not disposable trivia.
	// Keep the tag's gap stream intact rather than moving it into a wrapped
	// layout. Comments inside complete attribute nodes remain safe to wrap.
	if f.tagGapHasComment(n, selfClosing) {
		return false
	}
	return indentColumns(lineIndent)+len(canonical) > canonicalTagWidth
}

// wrapTagAttributes emits one complete opaque attribute span per line once a
// tag would exceed the canonical width. Only inter-node gaps become newlines;
// no byte inside an attribute span is reconstructed.
func (f *formatter) wrapTagAttributes(n *gotreesitter.Node, canonical string, selfClosing bool, depth int, lineIndent string) string {
	attrs := f.collectAttrs(n)
	if len(attrs) < 2 {
		return canonical
	}
	name := f.childByField(n, "name")
	if name == nil {
		return canonical
	}
	parts := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		part := f.text(attr)
		if part == "" || strings.ContainsAny(part, "\r\n") {
			return canonical
		}
		parts = append(parts, part)
	}
	// The destination indentation is passed by the parent. It is canonicalized
	// from display columns once, so a raw tab is never appended to a prefix
	// ending in spaces and the second pass sees the same continuation width.
	openIndent := canonicalIndent(lineIndent)
	attributeIndent := canonicalIndentColumns(indentColumns(openIndent) + formatterTabWidth)
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(f.text(name))
	for _, part := range parts {
		b.WriteByte('\n')
		b.WriteString(attributeIndent)
		b.WriteString(part)
	}
	b.WriteByte('\n')
	b.WriteString(openIndent)
	if selfClosing {
		b.WriteString(" />")
	} else {
		b.WriteByte('>')
	}
	return b.String()
}

// canonicalTagGaps returns a tag with only inter-node gaps rewritten. The
// boolean is false when the node spans are not a complete, ordered tag shape;
// preserving the whole source is safer than guessing at recovered syntax.
func (f *formatter) canonicalTagGaps(n, name *gotreesitter.Node, attrs []*gotreesitter.Node, selfClosing bool) (string, bool) {
	raw := f.text(n)
	if len(raw) < 3 || name.StartByte() < n.StartByte() || name.EndByte() > n.EndByte() {
		return raw, false
	}
	closeStart := tagClosingDelimiterStart(raw, selfClosing)
	if closeStart < 0 {
		return raw, false
	}
	rel := func(pos uint32) int { return int(pos - n.StartByte()) }
	nameStart, nameEnd := rel(name.StartByte()), rel(name.EndByte())
	if nameStart < 0 || nameEnd < nameStart || nameEnd > closeStart {
		return raw, false
	}
	var b strings.Builder
	b.WriteString(raw[:nameStart])
	b.WriteString(raw[nameStart:nameEnd])
	cursor := nameEnd
	for _, attr := range attrs {
		if attr == nil || attr.StartByte() < n.StartByte() || attr.EndByte() > n.EndByte() {
			return raw, false
		}
		attrStart, attrEnd := rel(attr.StartByte()), rel(attr.EndByte())
		if attrStart < cursor || attrEnd < attrStart || attrEnd > closeStart {
			return raw, false
		}
		b.WriteString(canonicalInterNodeGap(raw[cursor:attrStart], false, false))
		b.WriteString(raw[attrStart:attrEnd])
		cursor = attrEnd
	}
	if cursor > closeStart {
		return raw, false
	}
	b.WriteString(canonicalInterNodeGap(raw[cursor:closeStart], true, selfClosing))
	b.WriteString(raw[closeStart:])
	return b.String(), true
}

func canonicalInterNodeGap(gap string, beforeClose, selfClosing bool) string {
	if gap != "" {
		for i := 0; i < len(gap); i++ {
			switch gap[i] {
			case ' ', '\t', '\r', '\n':
			default:
				return gap
			}
		}
	}
	if beforeClose {
		if selfClosing {
			return " "
		}
		return ""
	}
	return " "
}

func tagClosingDelimiterStart(raw string, selfClosing bool) int {
	if len(raw) == 0 || raw[len(raw)-1] != '>' {
		return -1
	}
	if selfClosing && len(raw) >= 2 && raw[len(raw)-2] == '/' {
		return len(raw) - 2
	}
	return len(raw) - 1
}

func (f *formatter) tagGapHasComment(n *gotreesitter.Node, selfClosing bool) bool {
	name := f.childByField(n, "name")
	if name == nil {
		return false
	}
	attrs := f.collectAttrs(n)
	raw := f.text(n)
	closeStart := tagClosingDelimiterStart(raw, selfClosing)
	if closeStart < 0 {
		return true
	}
	rel := func(pos uint32) int { return int(pos - n.StartByte()) }
	cursor := rel(name.EndByte())
	for _, attr := range attrs {
		start, end := rel(attr.StartByte()), rel(attr.EndByte())
		if start < cursor || end > closeStart {
			return true
		}
		if strings.Contains(raw[cursor:start], "/*") || strings.Contains(raw[cursor:start], "//") {
			return true
		}
		cursor = end
	}
	return strings.Contains(raw[cursor:closeStart], "/*") || strings.Contains(raw[cursor:closeStart], "//")
}

// formatChildStream formats direct named children without losing the source
// gaps between them. Positions are measured in the original source, so a
// child that changes length cannot shift which gap belongs to its neighbor.
// This is deliberately a stream operation, not a parent-wide inline/block
// heuristic.
func (f *formatter) formatChildStream(parent *gotreesitter.Node, start, end uint32, depth int, lineIndent string) string {
	var b strings.Builder
	lastEnd := start
	for i := 0; i < parent.NamedChildCount(); i++ {
		child := parent.NamedChild(i)
		if child == nil || child.StartByte() < start || child.EndByte() > end {
			continue
		}
		if child.StartByte() > lastEnd {
			gap := f.src[lastEnd:child.StartByte()]
			b.Write(gap)
		}
		if child.StartByte() < lastEnd {
			// A recovered tree with overlapping children is rejected above for
			// normal input. If a future parser exposes one without an error node,
			// retain the child's source rather than silently dropping it.
			b.WriteString(f.text(child))
		} else {
			childIndent := lineIndent
			if child.StartByte() > lastEnd {
				childIndent = destinationIndentFromGap(string(f.src[lastEnd:child.StartByte()]), lineIndent)
			}
			b.WriteString(f.format(child, depth+1, childIndent))
		}
		if child.EndByte() > lastEnd {
			lastEnd = child.EndByte()
		}
	}
	if lastEnd < end {
		b.Write(f.src[lastEnd:end])
	}
	return b.String()
}

func (f *formatter) collectChildren(parent *gotreesitter.Node) []*gotreesitter.Node {
	children := make([]*gotreesitter.Node, 0, parent.NamedChildCount())
	for i := 0; i < parent.NamedChildCount(); i++ {
		child := parent.NamedChild(i)
		if child == nil {
			continue
		}
		typ := f.nodeType(child)
		if typ == "jsx_opening_element" || typ == "jsx_closing_element" ||
			typ == "jsx_script_raw_opening_element" || typ == "jsx_style_raw_opening_element" {
			continue
		}
		if isGSXNodeType(typ) {
			children = append(children, child)
		}
	}
	return children
}

func (f *formatter) collectAttrs(parent *gotreesitter.Node) []*gotreesitter.Node {
	attrs := make([]*gotreesitter.Node, 0, parent.NamedChildCount())
	for i := 0; i < parent.NamedChildCount(); i++ {
		child := parent.NamedChild(i)
		if child == nil {
			continue
		}
		switch f.nodeType(child) {
		case "jsx_attribute", "jsx_spread_attribute":
			attrs = append(attrs, child)
		}
	}
	return attrs
}

func (f *formatter) shouldBlock(children []*gotreesitter.Node, start, end uint32) bool {
	if len(children) == 0 || f.directGapHasComment(children, start, end) {
		return false
	}
	structural := false
	meaningfulText := false
	for _, child := range children {
		switch f.nodeType(child) {
		case "jsx_element", "jsx_raw_text_element", "jsx_self_closing_element", "jsx_fragment":
			structural = true
		case "jsx_text":
			if syntax.RenderText(f.text(child)) != "" {
				meaningfulText = true
			}
		}
	}
	// Structural-only child streams are canonicalized into an indented block.
	// Since the shared semantic layer discards whitespace-only layout tokens,
	// this insertion cannot change rendered HTML. Text-bearing streams remain
	// source-opaque, including punctuation adjacency and authored wrapping.
	return structural && !meaningfulText
}

func (f *formatter) formatBlockChildren(children []*gotreesitter.Node, depth int, baseIndent string) string {
	baseIndent = canonicalIndent(baseIndent)
	childIndent := canonicalIndentColumns(indentColumns(baseIndent) + formatterTabWidth)
	var b strings.Builder
	for _, child := range children {
		if f.nodeType(child) == "jsx_text" && syntax.RenderText(f.text(child)) == "" {
			continue
		}
		b.WriteByte('\n')
		b.WriteString(childIndent)
		b.WriteString(f.format(child, depth+1, childIndent))
	}
	b.WriteByte('\n')
	b.WriteString(canonicalIndent(baseIndent))
	return b.String()
}

// destinationIndentFromGap keeps an already-existing child line's display
// indentation when an inline stream contains a source newline. A compact
// stream inherits its parent's destination indentation; generated block lines
// are always supplied directly by formatBlockChildren.
func destinationIndentFromGap(gap, fallback string) string {
	lastNewline := strings.LastIndexAny(gap, "\r\n")
	if lastNewline < 0 {
		return fallback
	}
	suffix := gap[lastNewline+1:]
	for i := 0; i < len(suffix); i++ {
		if suffix[i] != ' ' && suffix[i] != '\t' {
			return fallback
		}
	}
	return canonicalIndent(suffix)
}

// sourceIndent returns only the leading horizontal whitespace on the line
// containing start. It deliberately ignores the rest of a Go statement (for
// example, the `return ` before a fragment), so a markup block nested inside a
// return keeps the enclosing Go indentation while adding one tab per child.
func (f *formatter) sourceIndent(start uint32) string {
	prefix := f.sourceLinePrefix(start)
	i := 0
	for i < len(prefix) && (prefix[i] == ' ' || prefix[i] == '\t') {
		i++
	}
	return canonicalIndent(prefix[:i])
}

const formatterTabWidth = 4

func canonicalIndent(raw string) string {
	return canonicalIndentColumns(indentColumns(raw))
}

// indentColumns measures indentation using the same four-column tab stops as
// the docs code-block and editor surfaces. A tab advances to the next stop,
// rather than contributing a fixed number of columns. This matters for mixed
// prefixes such as " \t" and "\t   \t": the formatter must preserve their
// display width while emitting a canonical sequence with tabs first and any
// remainder spaces after them.
func indentColumns(raw string) int {
	columns := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\t' {
			columns += formatterTabWidth - columns%formatterTabWidth
			continue
		}
		columns++
	}
	return columns
}

func canonicalIndentColumns(columns int) string {
	if columns <= 0 {
		return ""
	}
	return strings.Repeat("\t", columns/formatterTabWidth) + strings.Repeat(" ", columns%formatterTabWidth)
}

func (f *formatter) sourceLinePrefix(start uint32) string {
	if int(start) > len(f.src) {
		return ""
	}
	lineStart := 0
	for i := int(start) - 1; i >= 0; i-- {
		if f.src[i] == '\n' {
			lineStart = i + 1
			break
		}
	}
	return string(f.src[lineStart:int(start)])
}

// directGapHasComment scans only the source gaps between this parent's direct
// child spans. Descendant attributes, expression strings, and raw-text bodies
// are opaque child-owned bytes; looking for comment markers across the whole
// parent range would mistake those bytes for direct trivia and suppress a
// valid structural block conversion. A comment that actually lives in a gap
// remains visible and therefore keeps that source stream intact.
func (f *formatter) directGapHasComment(children []*gotreesitter.Node, start, end uint32) bool {
	if start >= end || int(end) > len(f.src) {
		return false
	}
	lastEnd := start
	for _, child := range children {
		if child == nil {
			continue
		}
		childStart, childEnd := child.StartByte(), child.EndByte()
		if childStart < start || childEnd < childStart || childEnd > end {
			// The caller supplied a recovered or inconsistent span. Preserve
			// source ownership rather than scanning bytes that may belong to a
			// descendant or outside this parent's child stream.
			return true
		}
		if childStart > lastEnd && f.containsComment(lastEnd, childStart) {
			return true
		}
		if childEnd > lastEnd {
			lastEnd = childEnd
		}
	}
	return lastEnd < end && f.containsComment(lastEnd, end)
}

func (f *formatter) containsComment(start, end uint32) bool {
	if start >= end || int(end) > len(f.src) {
		return false
	}
	raw := string(f.src[start:end])
	return strings.Contains(raw, "/*") || strings.Contains(raw, "//")
}

func isGSXNodeType(typ string) bool {
	switch typ {
	case "jsx_element", "jsx_raw_text_element", "jsx_self_closing_element",
		"jsx_fragment", "jsx_expression_container", "jsx_text":
		return true
	default:
		return false
	}
}

func validateUTF8(source []byte) error {
	for offset := 0; offset < len(source); {
		r, size := utf8.DecodeRune(source[offset:])
		if r == utf8.RuneError && size == 1 {
			line, column := 1, 1
			for i := 0; i < offset; i++ {
				if source[i] == '\n' {
					line++
					column = 1
					continue
				}
				column++
			}
			return fmt.Errorf("%d:%d: invalid UTF-8", line, column)
		}
		offset += size
	}
	return nil
}
