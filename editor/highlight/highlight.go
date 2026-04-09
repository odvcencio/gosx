package highlight

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Language selects the grammar used by a Highlighter.
type Language int

const (
	LangMarkdown Language = iota
)

// Decoration is a highlight annotation for a byte range within a single line.
type Decoration struct {
	// Class is the CSS class name (e.g. "hl-heading").
	Class string
	// StartCol and EndCol are byte offsets within the line.
	StartCol int
	EndCol   int
}

// EditRange describes a source change for incremental re-highlighting.
// Row/column values are zero-based.
type EditRange struct {
	StartLine  int
	StartCol   int
	OldEndLine int
	OldEndCol  int
	NewEndLine int
	NewEndCol  int
}

// Highlighter holds a parser and the most recently parsed tree so that
// subsequent edits can reuse unchanged subtrees.
type Highlighter struct {
	lang   Language
	parser *gotreesitter.Parser
	entry  *grammars.LangEntry
	tree   *gotreesitter.Tree
	source []byte
}

// New creates a Highlighter configured for lang.
func New(lang Language) *Highlighter {
	var entry *grammars.LangEntry
	switch lang {
	case LangMarkdown:
		entry = grammars.DetectLanguageByName("markdown")
	}

	var parser *gotreesitter.Parser
	if entry != nil {
		l := entry.Language()
		if l != nil {
			parser = gotreesitter.NewParser(l)
		}
	}

	return &Highlighter{
		lang:   lang,
		parser: parser,
		entry:  entry,
	}
}

// Highlight performs a full parse of source and returns per-line decorations.
// The returned slice is indexed by line number; each element lists the
// decorations found on that line.
func (h *Highlighter) Highlight(source []byte) [][]Decoration {
	if h.parser == nil || h.entry == nil {
		return nil
	}

	lang := h.entry.Language()
	var tree *gotreesitter.Tree
	var err error
	if h.entry.TokenSourceFactory != nil {
		ts := h.entry.TokenSourceFactory(source, lang)
		tree, err = h.parser.ParseWithTokenSource(source, ts)
	} else {
		tree, err = h.parser.Parse(source)
	}
	if err != nil || tree == nil {
		return nil
	}

	// Release old tree after we have a new one.
	if h.tree != nil {
		h.tree.Release()
	}
	h.tree = tree
	h.source = source

	bt := gotreesitter.Bind(tree)
	root := bt.RootNode()
	if root == nil {
		return nil
	}

	return decorationsFromTree(bt, root, source)
}

// HighlightIncremental applies edit to the cached tree, re-parses only the
// affected region, and returns updated per-line decorations for the full
// (edited) source.
func (h *Highlighter) HighlightIncremental(source []byte, edit EditRange) [][]Decoration {
	if h.parser == nil || h.entry == nil || h.tree == nil {
		return h.Highlight(source)
	}

	// Convert line/col coordinates to byte offsets in the OLD source.
	startByte := lineColToByte(h.source, edit.StartLine, edit.StartCol)
	oldEndByte := lineColToByte(h.source, edit.OldEndLine, edit.OldEndCol)
	// newEndByte is in the NEW source.
	newEndByte := lineColToByte(source, edit.NewEndLine, edit.NewEndCol)

	inputEdit := gotreesitter.InputEdit{
		StartByte:  uint32(startByte),
		OldEndByte: uint32(oldEndByte),
		NewEndByte: uint32(newEndByte),
		StartPoint: gotreesitter.Point{
			Row:    uint32(edit.StartLine),
			Column: uint32(edit.StartCol),
		},
		OldEndPoint: gotreesitter.Point{
			Row:    uint32(edit.OldEndLine),
			Column: uint32(edit.OldEndCol),
		},
		NewEndPoint: gotreesitter.Point{
			Row:    uint32(edit.NewEndLine),
			Column: uint32(edit.NewEndCol),
		},
	}
	h.tree.Edit(inputEdit)

	lang := h.entry.Language()
	var newTree *gotreesitter.Tree
	var err error
	if h.entry.TokenSourceFactory != nil {
		ts := h.entry.TokenSourceFactory(source, lang)
		newTree, err = h.parser.ParseIncrementalWithTokenSource(source, h.tree, ts)
	} else {
		newTree, err = h.parser.ParseIncremental(source, h.tree)
	}
	if err != nil || newTree == nil {
		return h.Highlight(source)
	}

	if newTree != h.tree {
		h.tree.Release()
	}
	h.tree = newTree
	h.source = source

	bt := gotreesitter.Bind(newTree)
	root := bt.RootNode()
	if root == nil {
		return nil
	}

	return decorationsFromTree(bt, root, source)
}

// lineColToByte returns the byte offset for a (row, col) position in source.
// If the position is out of range it clamps to the end of the source.
func lineColToByte(source []byte, row, col int) int {
	line := 0
	for i, b := range source {
		if line == row {
			return i + col
		}
		if b == '\n' {
			line++
		}
	}
	return len(source)
}

// decorationsFromTree walks the syntax tree and collects per-line decorations.
func decorationsFromTree(bt *gotreesitter.BoundTree, root *gotreesitter.Node, source []byte) [][]Decoration {
	// Count lines in source.
	lineCount := 1
	for _, b := range source {
		if b == '\n' {
			lineCount++
		}
	}

	result := make([][]Decoration, lineCount)
	walkNode(bt, root, source, result)
	return result
}

// walkNode recurses through the tree, emitting decorations for classifiable nodes.
func walkNode(bt *gotreesitter.BoundTree, n *gotreesitter.Node, source []byte, result [][]Decoration) {
	if n == nil {
		return
	}

	class := classifyMarkdownNode(bt, n)
	if class != "" && n.EndByte() > n.StartByte() {
		startLine := int(n.StartPoint().Row)
		endLine := int(n.EndPoint().Row)

		if startLine < len(result) {
			// For multi-line nodes, record on the start line only.
			startCol := int(n.StartPoint().Column)
			var endCol int
			if startLine == endLine {
				endCol = int(n.EndPoint().Column)
			} else {
				// Extend to end of start line.
				endCol = endOfLine(source, int(n.StartByte()))
			}
			result[startLine] = append(result[startLine], Decoration{
				Class:    class,
				StartCol: startCol,
				EndCol:   endCol,
			})
		}
		// Don't descend into classified nodes — avoid double decoration.
		return
	}

	for i := 0; i < n.ChildCount(); i++ {
		walkNode(bt, n.Child(i), source, result)
	}
}

// endOfLine returns the column offset of the last byte on the line that
// contains byteOffset.
func endOfLine(source []byte, byteOffset int) int {
	col := 0
	for i := byteOffset; i < len(source); i++ {
		if source[i] == '\n' {
			break
		}
		col++
	}
	// Find start of line to compute column.
	lineStart := byteOffset
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	end := byteOffset
	for end < len(source) && source[end] != '\n' {
		end++
	}
	return end - lineStart
}

// classifyMarkdownNode maps markdown tree-sitter node types to CSS classes.
func classifyMarkdownNode(bt *gotreesitter.BoundTree, n *gotreesitter.Node) string {
	nodeType := bt.NodeType(n)
	switch nodeType {
	case "atx_heading",
		"setext_heading",
		"atx_h1_marker", "atx_h2_marker", "atx_h3_marker",
		"atx_h4_marker", "atx_h5_marker", "atx_h6_marker",
		"setext_h1_underline", "setext_h2_underline":
		return "hl-heading"

	case "emphasis":
		return "hl-emphasis"

	case "strong_emphasis":
		return "hl-strong"

	case "code_span",
		"indented_code_block",
		"fenced_code_block",
		"code_fence_content",
		"fenced_code_block_delimiter":
		return "hl-code"

	case "link", "image":
		return "hl-link"

	case "link_destination":
		return "hl-link-dest"

	case "link_label":
		return "hl-link-label"

	case "block_quote":
		return "hl-blockquote"

	case "block_quote_marker", "block_continuation":
		return "hl-blockquote-marker"

	case "list_item":
		return "hl-list-item"

	case "list_marker_plus", "list_marker_minus", "list_marker_star",
		"list_marker_dot", "list_marker_parenthesis":
		return "hl-list-marker"

	case "thematic_break":
		return "hl-thematic-break"

	case "backslash_escape":
		return "hl-escape"
	}

	return ""
}
