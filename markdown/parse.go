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
		// section is a wrapper in tree-sitter-markdown; flatten its children
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
				link.Attrs["title"] = bt.NodeText(child)
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
				img.Attrs["title"] = bt.NodeText(child)
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
// merging adjacent unnamed text spans and recursing into named nodes.
func collectInlineChildren(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) []*Node {
	var nodes []*Node
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source)...)
		} else {
			// unnamed/punctuation/text nodes — take their text
			text := bt.NodeText(child)
			if text != "" {
				// Merge with previous text node if possible
				if len(nodes) > 0 && nodes[len(nodes)-1].Type == NodeText {
					nodes[len(nodes)-1].Literal += text
				} else {
					nodes = append(nodes, textNode(text))
				}
			}
		}
	}
	return nodes
}

// collectInlineTextOnly extracts text from an inline node, skipping
// delimiter tokens (emphasis_delimiter, etc.) and recursing into nested
// inline structures.
func collectInlineTextOnly(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte) []*Node {
	var nodes []*Node
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		ct := bt.NodeType(child)
		if isDelimiter(ct) {
			continue
		}
		if isInlineStructural(ct) {
			nodes = append(nodes, convertInlineChildren(bt, child, source)...)
		} else {
			text := bt.NodeText(child)
			if text != "" {
				if len(nodes) > 0 && nodes[len(nodes)-1].Type == NodeText {
					nodes[len(nodes)-1].Literal += text
				} else {
					nodes = append(nodes, textNode(text))
				}
			}
		}
	}
	return nodes
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
func extractCodeSpanText(bt *gotreesitter.BoundTree, n *gotreesitter.Node) string {
	var parts []string
	for i := 0; i < n.ChildCount(); i++ {
		child := n.Child(i)
		if bt.NodeType(child) != "code_span_delimiter" {
			parts = append(parts, bt.NodeText(child))
		}
	}
	return strings.Join(parts, "")
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
