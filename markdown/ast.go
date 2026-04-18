package markdown

import "strings"

// NodeType classifies an AST node.
type NodeType int

const (
	NodeDocument NodeType = iota
	NodeHeading
	NodeParagraph
	NodeCodeBlock
	NodeBlockquote
	NodeList
	NodeListItem
	NodeTable
	NodeTableRow
	NodeTableCell
	NodeThematicBreak
	NodeLink
	NodeImage
	NodeEmphasis
	NodeStrong
	NodeStrikethrough
	NodeCodeSpan
	NodeText
	NodeSoftBreak
	NodeHardBreak
	NodeHTMLBlock
	NodeHTMLInline
	NodeFootnoteRef
	NodeFootnoteDef
	NodeMathInline
	NodeMathBlock
	NodeAdmonition
	NodeDefinitionList
	NodeDefinitionTerm
	NodeDefinitionDesc
	NodeSuperscript
	NodeSubscript
	NodeTaskListItem
	NodeFrontmatter
	NodeTableOfContents
	NodeAutoEmbed
	NodeEmoji
	NodeDiagram
)

// Node is a single element in the Markdown AST.
type Node struct {
	Type     NodeType
	Children []*Node
	Literal  string
	Attrs    map[string]string
}

// Document is the top-level result of parsing a Markdown source.
type Document struct {
	Root            *Node
	Source          []byte
	frontmatterData map[string]any
}

// Frontmatter returns the parsed frontmatter metadata, if any.
func (d *Document) Frontmatter() map[string]any {
	return d.frontmatterData
}

// TOCEntry represents a single table-of-contents heading.
type TOCEntry struct {
	Level int
	ID    string
	Text  string
}

// collectNodesText extracts plain text from a slice of nodes.
func collectNodesText(nodes []*Node) string {
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString(collectNodeText(n))
	}
	return sb.String()
}

// textNode creates a leaf text node.
func textNode(text string) *Node {
	return &Node{Type: NodeText, Literal: text}
}

// newNode creates a node of the given type with optional children.
func newNode(typ NodeType, children ...*Node) *Node {
	return &Node{Type: typ, Children: children}
}
