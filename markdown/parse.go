package markdown

import (
	"strings"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Cached languages, initialised once.
var (
	mdLangOnce   sync.Once
	mdLang       *gotreesitter.Language
	mdInlineOnce sync.Once
	mdInlineLang *gotreesitter.Language

	mdEntry       *grammars.LangEntry
	mdInlineEntry *grammars.LangEntry
)

func blockLang() *gotreesitter.Language {
	mdLangOnce.Do(func() {
		mdLang = grammars.MarkdownLanguage()
		mdEntry = grammars.DetectLanguageByName("markdown")
	})
	return mdLang
}

func inlineLang() *gotreesitter.Language {
	mdInlineOnce.Do(func() {
		mdInlineLang = grammars.MarkdownInlineLanguage()
		mdInlineEntry = grammars.DetectLanguageByName("markdown_inline")
	})
	return mdInlineLang
}

// Parse parses Markdown source into a Document AST.
func Parse(source []byte) *Document {
	// tree-sitter markdown requires a trailing newline for correct parsing.
	if len(source) > 0 && source[len(source)-1] != '\n' {
		source = append(source, '\n')
	}

	lang := blockLang()
	if lang == nil {
		return &Document{Root: newNode(NodeDocument), Source: source}
	}

	parser := gotreesitter.NewParser(lang)
	var tree *gotreesitter.Tree
	var err error
	if mdEntry != nil && mdEntry.TokenSourceFactory != nil {
		ts := mdEntry.TokenSourceFactory(source, lang)
		tree, err = parser.ParseWithTokenSource(source, ts)
	} else {
		tree, err = parser.Parse(source)
	}
	if err != nil || tree == nil {
		return &Document{Root: newNode(NodeDocument), Source: source}
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)
	root := convertBlock(bt, bt.RootNode(), source)
	if root == nil {
		root = newNode(NodeDocument)
	}
	return &Document{Root: root, Source: source}
}

// convertBlock recursively converts a block-level tree-sitter node into an AST Node.
func convertBlock(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	if n == nil {
		return nil
	}
	typ := bt.NodeType(n)

	switch typ {
	case "document":
		doc := newNode(NodeDocument)
		for i := 0; i < n.ChildCount(); i++ {
			child := convertBlock(bt, n.Child(i), source)
			if child == nil {
				continue
			}
			// Flatten pseudo-document wrappers from section nodes
			if child.Type == NodeDocument {
				doc.Children = append(doc.Children, child.Children...)
			} else {
				doc.Children = append(doc.Children, child)
			}
		}
		return doc

	case "section":
		// section is a wrapper in tree-sitter-markdown; flatten its children.
		// For simple documents, tree-sitter may omit wrapper nodes and place
		// children (e.g. list_item, fenced_code_block_delimiter) directly
		// under section. We detect these patterns and synthesise the wrapper.
		if synth := synthesiseSectionContent(bt, n, source); synth != nil {
			return synth
		}
		var nodes []*Node
		for i := 0; i < n.ChildCount(); i++ {
			if child := convertBlock(bt, n.Child(i), source); child != nil {
				nodes = append(nodes, child)
			}
		}
		if len(nodes) == 1 {
			return nodes[0]
		}
		// Return a pseudo-document to hold multiple section children;
		// the caller will merge them.
		wrapper := newNode(NodeDocument)
		wrapper.Children = nodes
		return wrapper

	case "atx_heading", "setext_heading":
		heading := newNode(NodeHeading)
		level := headingLevel(bt, n)
		if heading.Attrs == nil {
			heading.Attrs = make(map[string]string)
		}
		heading.Attrs["level"] = levelStr(level)
		// Extract inline text from heading
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			childType := bt.NodeType(child)
			if childType == "inline" {
				heading.Children = append(heading.Children, parseInline(bt.NodeText(child), source)...)
			} else if childType == "paragraph" {
				// setext_heading uses paragraph for content
				for j := 0; j < child.ChildCount(); j++ {
					gc := child.Child(j)
					if bt.NodeType(gc) == "inline" {
						heading.Children = append(heading.Children, parseInline(bt.NodeText(gc), source)...)
					}
				}
			}
		}
		return heading

	case "paragraph":
		para := newNode(NodeParagraph)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if bt.NodeType(child) == "inline" {
				para.Children = append(para.Children, parseInline(bt.NodeText(child), source)...)
			}
		}
		return para

	case "fenced_code_block", "indented_code_block":
		cb := newNode(NodeCodeBlock)
		if cb.Attrs == nil {
			cb.Attrs = make(map[string]string)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			childType := bt.NodeType(child)
			switch childType {
			case "info_string":
				// info_string may contain a language child
				langNode := findChild(bt, child, "language")
				if langNode != nil {
					cb.Attrs["language"] = strings.TrimSpace(bt.NodeText(langNode))
				} else {
					cb.Attrs["language"] = strings.TrimSpace(bt.NodeText(child))
				}
			case "code_fence_content":
				cb.Literal = bt.NodeText(child)
			}
		}
		if typ == "indented_code_block" {
			cb.Literal = bt.NodeText(n)
		}
		return cb

	case "block_quote":
		bq := newNode(NodeBlockquote)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			childType := bt.NodeType(child)
			if childType == "block_quote_marker" || childType == "block_continuation" {
				continue
			}
			if converted := convertBlock(bt, child, source); converted != nil {
				bq.Children = append(bq.Children, converted)
			}
		}
		return bq

	case "list":
		list := newNode(NodeList)
		// Detect ordered vs unordered by checking first list item marker
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if bt.NodeType(child) == "list_item" {
				if converted := convertListItem(bt, child, source); converted != nil {
					list.Children = append(list.Children, converted)
				}
			}
		}
		return list

	case "thematic_break":
		return newNode(NodeThematicBreak)

	case "pipe_table":
		return convertTable(bt, n, source)

	case "html_block":
		block := newNode(NodeHTMLBlock)
		block.Literal = bt.NodeText(n)
		return block

	default:
		// Skip node types we don't map (block_continuation, markers, etc.)
		return nil
	}
}

// convertListItem converts a list_item node into a NodeListItem.
func convertListItem(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	item := newNode(NodeListItem)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childType := bt.NodeType(child)
		// Skip markers and continuations
		if strings.HasPrefix(childType, "list_marker") || childType == "block_continuation" {
			// Check for task list markers
			if childType == "list_marker_minus" || childType == "list_marker_plus" || childType == "list_marker_star" {
				// task list detection handled below
			}
			continue
		}
		if converted := convertBlock(bt, child, source); converted != nil {
			item.Children = append(item.Children, converted)
		}
	}
	return item
}

// convertTable converts a pipe_table node into a NodeTable with rows and cells.
func convertTable(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	table := newNode(NodeTable)
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childType := bt.NodeType(child)
		switch childType {
		case "pipe_table_header", "pipe_table_row":
			row := newNode(NodeTableRow)
			for j := 0; j < child.ChildCount(); j++ {
				cell := child.Child(j)
				if bt.NodeType(cell) == "pipe_table_cell" {
					c := newNode(NodeTableCell)
					text := strings.TrimSpace(bt.NodeText(cell))
					if text != "" {
						c.Children = append(c.Children, textNode(text))
					}
					row.Children = append(row.Children, c)
				}
			}
			table.Children = append(table.Children, row)
		case "pipe_table_delimiter_row":
			// skip delimiter row
		}
	}
	return table
}

// parseInline parses inline markdown text using the markdown_inline grammar.
func parseInline(text string, source []byte) []*Node {
	lang := inlineLang()
	if lang == nil {
		return []*Node{textNode(text)}
	}

	parser := gotreesitter.NewParser(lang)
	src := []byte(text)
	var tree *gotreesitter.Tree
	var err error
	if mdInlineEntry != nil && mdInlineEntry.TokenSourceFactory != nil {
		ts := mdInlineEntry.TokenSourceFactory(src, lang)
		tree, err = parser.ParseWithTokenSource(src, ts)
	} else {
		tree, err = parser.Parse(src)
	}
	if err != nil || tree == nil {
		return []*Node{textNode(text)}
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)
	return convertInlineChildren(bt, bt.RootNode(), src)
}

// convertInlineChildren walks an inline tree-sitter node and converts
// its children into AST nodes, collecting text runs from unnamed/leaf nodes.
func convertInlineChildren(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) []*Node {
	if n == nil {
		return nil
	}
	var nodes []*Node
	typ := bt.NodeType(n)

	switch typ {
	case "inline":
		// Root inline node — process children, collecting text spans
		nodes = collectInlineChildren(bt, n, source)

	case "strong_emphasis":
		strong := newNode(NodeStrong)
		strong.Children = collectInlineTextOnly(bt, n, source)
		nodes = append(nodes, strong)

	case "emphasis":
		em := newNode(NodeEmphasis)
		em.Children = collectInlineTextOnly(bt, n, source)
		nodes = append(nodes, em)

	case "strikethrough":
		s := newNode(NodeStrikethrough)
		s.Children = collectInlineTextOnly(bt, n, source)
		nodes = append(nodes, s)

	case "inline_link":
		link := newNode(NodeLink)
		if link.Attrs == nil {
			link.Attrs = make(map[string]string)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "link_text":
				link.Children = append(link.Children, textNode(bt.NodeText(child)))
			case "link_destination":
				link.Attrs["href"] = bt.NodeText(child)
			case "link_title":
				link.Attrs["title"] = stripQuotes(bt.NodeText(child))
			}
		}
		nodes = append(nodes, link)

	case "full_reference_link", "collapsed_reference_link", "shortcut_link":
		link := newNode(NodeLink)
		if link.Attrs == nil {
			link.Attrs = make(map[string]string)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "link_text":
				link.Children = append(link.Children, textNode(bt.NodeText(child)))
			case "link_label":
				link.Attrs["ref"] = bt.NodeText(child)
			}
		}
		nodes = append(nodes, link)

	case "image":
		img := newNode(NodeImage)
		if img.Attrs == nil {
			img.Attrs = make(map[string]string)
		}
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := bt.NodeType(child)
			switch ct {
			case "image_description":
				img.Attrs["alt"] = bt.NodeText(child)
			case "link_destination":
				img.Attrs["src"] = bt.NodeText(child)
			case "link_title":
				img.Attrs["title"] = stripQuotes(bt.NodeText(child))
			}
		}
		nodes = append(nodes, img)

	case "code_span":
		cs := newNode(NodeCodeSpan)
		// Extract text between delimiters
		cs.Literal = extractCodeSpanText(bt, n)
		nodes = append(nodes, cs)

	case "hard_line_break":
		nodes = append(nodes, newNode(NodeHardBreak))

	case "backslash_escape":
		text := bt.NodeText(n)
		if len(text) > 1 {
			nodes = append(nodes, textNode(text[1:]))
		}

	case "html_tag":
		hi := newNode(NodeHTMLInline)
		hi.Literal = bt.NodeText(n)
		nodes = append(nodes, hi)

	default:
		// Leaf text or unnamed punctuation — handled by parent's collector
		text := bt.NodeText(n)
		if text != "" {
			nodes = append(nodes, textNode(text))
		}
	}

	return nodes
}

// collectInlineChildren processes the children of an inline-level node,
// extracting gap text between children and recursing into structural nodes.
// tree-sitter markdown_inline does not create child nodes for plain text;
// text that falls between (or around) named children must be recovered
// from the source using byte offsets.
func collectInlineChildren(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) []*Node {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		// Leaf inline — all text
		if len(src) > 0 {
			return []*Node{textNode(string(src))}
		}
		return nil
	}

	var nodes []*Node
	cursor := uint32(0) // relative to nodeStart

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		// Gap text before this child
		if childStart > cursor {
			gap := string(src[cursor:childStart])
			if gap != "" {
				appendText(&nodes, gap)
			}
		}

		ct := bt.NodeType(child)
		if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source)...)
		} else {
			// Non-structural child (punctuation, etc.) — include its text
			text := bt.NodeText(child)
			if text != "" {
				appendText(&nodes, text)
			}
		}
		cursor = childEnd
	}

	// Trailing gap text
	if cursor < uint32(len(src)) {
		gap := string(src[cursor:])
		if gap != "" {
			appendText(&nodes, gap)
		}
	}

	return nodes
}

// collectInlineTextOnly extracts text from an inline node, skipping
// delimiter tokens (emphasis_delimiter, etc.) and recursing into nested
// inline structures. Uses gap-based extraction for text between children.
func collectInlineTextOnly(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) []*Node {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		if len(src) > 0 {
			return []*Node{textNode(string(src))}
		}
		return nil
	}

	var nodes []*Node
	cursor := uint32(0)

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		// Gap text before this child (content text between delimiters/children)
		if childStart > cursor {
			gap := string(src[cursor:childStart])
			if gap != "" {
				appendText(&nodes, gap)
			}
		}

		if isDelimiter(ct) {
			// Skip delimiter token itself (don't emit its text)
			cursor = childEnd
			continue
		}

		if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source)...)
		} else {
			text := bt.NodeText(child)
			if text != "" {
				appendText(&nodes, text)
			}
		}
		cursor = childEnd
	}

	// Trailing gap text
	if cursor < uint32(len(src)) {
		gap := string(src[cursor:])
		if gap != "" {
			appendText(&nodes, gap)
		}
	}

	return nodes
}

// appendText merges text into the last node if it's a text node,
// or appends a new text node.
func appendText(nodes *[]*Node, text string) {
	if len(*nodes) > 0 && (*nodes)[len(*nodes)-1].Type == NodeText {
		(*nodes)[len(*nodes)-1].Literal += text
	} else {
		*nodes = append(*nodes, textNode(text))
	}
}

// synthesiseSectionContent checks whether a section node contains
// unwrapped children that belong in a wrapper node and, if so,
// synthesises the wrapper.  Returns nil if no special handling applies.
func synthesiseSectionContent(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) *Node {
	if n.ChildCount() == 0 {
		return nil
	}

	// Collect child types for pattern matching.
	childTypes := make([]string, n.ChildCount())
	for i := 0; i < n.ChildCount(); i++ {
		childTypes[i] = bt.NodeType(n.Child(i))
	}

	// Pattern: block_quote_marker + paragraph/... = blockquote
	if childTypes[0] == "block_quote_marker" {
		bq := newNode(NodeBlockquote)
		for i := 1; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := childTypes[i]
			if ct == "block_continuation" {
				continue
			}
			if converted := convertBlock(bt, child, source); converted != nil {
				bq.Children = append(bq.Children, converted)
			}
		}
		return bq
	}

	// Pattern: list_item children = list
	hasListItem := false
	for _, ct := range childTypes {
		if ct == "list_item" {
			hasListItem = true
			break
		}
	}
	if hasListItem {
		list := newNode(NodeList)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			if bt.NodeType(child) == "list_item" {
				if converted := convertListItem(bt, child, source); converted != nil {
					list.Children = append(list.Children, converted)
				}
			}
		}
		return list
	}

	// Pattern: fenced_code_block_delimiter + info_string + code_fence_content + ... = code block
	hasFenceDelim := false
	for _, ct := range childTypes {
		if ct == "fenced_code_block_delimiter" {
			hasFenceDelim = true
			break
		}
	}
	if hasFenceDelim {
		cb := newNode(NodeCodeBlock)
		cb.Attrs = make(map[string]string)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := childTypes[i]
			switch ct {
			case "info_string":
				langNode := findChild(bt, child, "language")
				if langNode != nil {
					cb.Attrs["language"] = strings.TrimSpace(bt.NodeText(langNode))
				} else {
					cb.Attrs["language"] = strings.TrimSpace(bt.NodeText(child))
				}
			case "code_fence_content":
				cb.Literal = bt.NodeText(child)
			}
		}
		return cb
	}

	// Pattern: pipe_table_header + pipe_table_delimiter_row + pipe_table_row = table
	hasPipeTableHeader := false
	for _, ct := range childTypes {
		if ct == "pipe_table_header" {
			hasPipeTableHeader = true
			break
		}
	}
	if hasPipeTableHeader {
		table := newNode(NodeTable)
		for i := 0; i < n.ChildCount(); i++ {
			child := n.Child(i)
			ct := childTypes[i]
			switch ct {
			case "pipe_table_header", "pipe_table_row":
				row := newNode(NodeTableRow)
				for j := 0; j < child.ChildCount(); j++ {
					cell := child.Child(j)
					if bt.NodeType(cell) == "pipe_table_cell" {
						c := newNode(NodeTableCell)
						text := strings.TrimSpace(bt.NodeText(cell))
						if text != "" {
							c.Children = append(c.Children, textNode(text))
						}
						row.Children = append(row.Children, c)
					}
				}
				table.Children = append(table.Children, row)
			case "pipe_table_delimiter_row":
				// skip
			}
		}
		return table
	}

	// Pattern: section with only "inline" child(ren) and no structural wrappers = paragraph.
	// In single-element documents, the block parser's "inline" children may only
	// cover punctuation. Use the full section text instead.
	allInlineOrSkip := true
	hasInline := false
	for _, ct := range childTypes {
		if ct == "inline" {
			hasInline = true
		} else if ct != "block_continuation" && ct != "_whitespace" {
			allInlineOrSkip = false
			break
		}
	}
	if allInlineOrSkip && hasInline {
		para := newNode(NodeParagraph)
		sectionText := strings.TrimRight(bt.NodeText(n), "\n")
		para.Children = append(para.Children, parseInline(sectionText, source)...)
		return para
	}

	return nil
}

// isInlineStructural returns true for node types that are meaningful inline
// structures (not raw text/punctuation).
func isInlineStructural(nodeType string) bool {
	switch nodeType {
	case "strong_emphasis", "emphasis", "strikethrough",
		"inline_link", "full_reference_link", "collapsed_reference_link", "shortcut_link",
		"image", "code_span", "hard_line_break", "html_tag", "backslash_escape":
		return true
	default:
		return false
	}
}

// isDelimiter returns true for delimiter node types that should be stripped.
func isDelimiter(nodeType string) bool {
	switch nodeType {
	case "emphasis_delimiter", "code_span_delimiter":
		return true
	default:
		return false
	}
}

// extractCodeSpanText gets the text inside a code_span, stripping delimiters.
// Uses gap-based extraction since tree-sitter may not represent all text as children.
func extractCodeSpanText(bt *gotreesitter.BoundTree, n *gotreesitter.Node) string {
	nodeText := bt.NodeText(n)
	src := []byte(nodeText)
	nodeStart := n.StartByte()

	if n.ChildCount() == 0 {
		return nodeText
	}

	var sb strings.Builder
	cursor := uint32(0)

	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		childStart := child.StartByte() - nodeStart
		childEnd := child.EndByte() - nodeStart

		// Gap text before this child
		if childStart > cursor {
			sb.Write(src[cursor:childStart])
		}

		if bt.NodeType(child) != "code_span_delimiter" {
			sb.WriteString(bt.NodeText(child))
		}
		cursor = childEnd
	}

	// Trailing gap text
	if cursor < uint32(len(src)) {
		sb.Write(src[cursor:])
	}

	return sb.String()
}

// headingLevel extracts the heading level (1-6) from an atx_heading or setext_heading node.
func headingLevel(bt *gotreesitter.BoundTree, n *gotreesitter.Node) int {
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		switch ct {
		case "atx_h1_marker":
			return 1
		case "atx_h2_marker":
			return 2
		case "atx_h3_marker":
			return 3
		case "atx_h4_marker":
			return 4
		case "atx_h5_marker":
			return 5
		case "atx_h6_marker":
			return 6
		case "setext_h1_underline":
			return 1
		case "setext_h2_underline":
			return 2
		}
	}
	return 1
}

// levelStr converts a heading level int to its string representation.
func levelStr(level int) string {
	switch level {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	case 6:
		return "6"
	default:
		return "1"
	}
}

// stripQuotes removes surrounding quote characters from a string.
// tree-sitter link_title nodes include their surrounding quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '(' && last == ')') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// findChild finds the first child of n with the given node type.
func findChild(bt *gotreesitter.BoundTree, n *gotreesitter.Node, nodeType string) *gotreesitter.Node {
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if bt.NodeType(child) == nodeType {
			return child
		}
	}
	return nil
}
