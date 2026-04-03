package markdown

import (
	"regexp"
	"strings"
)

// postProcess walks the AST after parsing and applies Markdown++ transformations.
// Task lists and subscripts are handled during parsing (convertListItem and
// strikethrough detection respectively), so they are not processed here.
func postProcess(doc *Document) {
	if doc == nil || doc.Root == nil {
		return
	}
	processAdmonitions(doc.Root)
	footnoteDefs := processFootnotes(doc.Root)
	processInlineMath(doc.Root)
	processSuperscripts(doc.Root)

	// Append collected footnote definitions at the end of the document.
	if len(footnoteDefs) > 0 {
		doc.Root.Children = append(doc.Root.Children, footnoteDefs...)
	}
}

// --- Admonitions ---

var admonitionRawRe = regexp.MustCompile(`^\[!(NOTE|WARNING|TIP|IMPORTANT|CAUTION)\]$`)

// processAdmonitions converts blockquotes starting with [!TYPE] into admonition nodes.
// tree-sitter parses "[!NOTE]" as a shortcut_link with raw="[!NOTE]" and link text "!".
// The AST looks like: Blockquote > Paragraph > [Link(raw="[!NOTE]"), Text("\n> content")]
func processAdmonitions(root *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeBlockquote || len(n.Children) == 0 {
			return true
		}

		firstChild := n.Children[0]
		if firstChild.Type != NodeParagraph || len(firstChild.Children) == 0 {
			return true
		}

		// Look for a Link node with raw attr matching [!TYPE]
		firstNode := firstChild.Children[0]
		if firstNode.Type != NodeLink {
			return true
		}
		raw := firstNode.Attrs["raw"]
		if raw == "" {
			return true
		}
		match := admonitionRawRe.FindStringSubmatch(raw)
		if match == nil {
			return true
		}

		adType := strings.ToLower(match[1])

		// Build admonition node
		adm := &Node{
			Type:  NodeAdmonition,
			Attrs: map[string]string{"type": adType},
		}

		// Remove the Link node from the first paragraph's children
		firstChild.Children = firstChild.Children[1:]

		// The remaining text in the first paragraph may start with "\n> content".
		// Clean up the leading newline and "> " prefix from text nodes.
		if len(firstChild.Children) > 0 && firstChild.Children[0].Type == NodeText {
			text := firstChild.Children[0].Literal
			text = strings.TrimLeft(text, "\n")
			lines := strings.Split(text, "\n")
			for i, line := range lines {
				lines[i] = strings.TrimPrefix(line, "> ")
				lines[i] = strings.TrimPrefix(lines[i], ">")
			}
			text = strings.Join(lines, "\n")
			text = strings.TrimSpace(text)
			if text == "" {
				firstChild.Children = firstChild.Children[1:]
			} else {
				firstChild.Children[0].Literal = text
			}
		}

		// If first paragraph still has content, include it
		startIdx := 0
		if len(firstChild.Children) == 0 {
			startIdx = 1
		}

		adm.Children = make([]*Node, len(n.Children[startIdx:]))
		copy(adm.Children, n.Children[startIdx:])

		// Replace the blockquote with the admonition in the parent
		if parent != nil {
			parent.Children[index] = adm
		}

		return true
	})
}

// --- Footnotes ---

var footnoteRefRawRe = regexp.MustCompile(`^\[\^(\w+)\]$`)

// processFootnotes scans for footnote references and definitions.
// Definitions may be:
//  1. NodeFootnoteDef already created by the parser from link_reference_definition
//  2. Paragraphs containing Link(raw="[^id]") followed by Text(": content")
//
// References are Link nodes with raw="[^id]".
func processFootnotes(root *Node) []*Node {
	var defs []*Node
	var defIndices []int

	// First pass: collect footnote definitions from document children.
	for i, child := range root.Children {
		// Case 1: already a NodeFootnoteDef from parser
		if child.Type == NodeFootnoteDef {
			defs = append(defs, child)
			defIndices = append(defIndices, i)
			continue
		}

		// Case 2: paragraph containing Link(raw="[^id]") + Text(": content")
		if child.Type != NodeParagraph || len(child.Children) < 2 {
			continue
		}
		firstNode := child.Children[0]
		if firstNode.Type != NodeLink {
			continue
		}
		raw := firstNode.Attrs["raw"]
		if raw == "" {
			continue
		}
		match := footnoteRefRawRe.FindStringSubmatch(raw)
		if match == nil {
			continue
		}
		secondNode := child.Children[1]
		if secondNode.Type != NodeText || !strings.HasPrefix(secondNode.Literal, ": ") {
			continue
		}
		content := strings.TrimPrefix(secondNode.Literal, ": ")
		defNode := &Node{
			Type:     NodeFootnoteDef,
			Attrs:    map[string]string{"id": match[1]},
			Children: []*Node{textNode(content)},
		}
		defs = append(defs, defNode)
		defIndices = append(defIndices, i)
	}

	// Remove definition nodes from root (in reverse order to preserve indices).
	for j := len(defIndices) - 1; j >= 0; j-- {
		idx := defIndices[j]
		root.Children = append(root.Children[:idx], root.Children[idx+1:]...)
	}

	// Second pass: convert footnote references in all nodes.
	// Replace Link nodes with raw="[^id]" with FootnoteRef nodes.
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if len(n.Children) == 0 {
			return true
		}
		for i, child := range n.Children {
			if child.Type != NodeLink {
				continue
			}
			raw := child.Attrs["raw"]
			if raw == "" {
				continue
			}
			match := footnoteRefRawRe.FindStringSubmatch(raw)
			if match == nil {
				continue
			}
			n.Children[i] = &Node{
				Type:  NodeFootnoteRef,
				Attrs: map[string]string{"id": match[1]},
			}
		}
		return true
	})

	return defs
}

// --- Math ---

var mathBlockRe = regexp.MustCompile(`^\$\$([\s\S]+?)\$\$$`)
var mathInlineRe = regexp.MustCompile(`\$([^\$\n]+?)\$`)

// processInlineMath converts $...$ to inline math and $$...$$ paragraphs to block math.
func processInlineMath(root *Node) {
	// First: handle block math — paragraphs that are entirely $$...$$
	for i, child := range root.Children {
		if child.Type != NodeParagraph {
			continue
		}
		text := collectNodeText(child)
		text = strings.TrimSpace(text)
		match := mathBlockRe.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		root.Children[i] = &Node{
			Type:    NodeMathBlock,
			Literal: strings.TrimSpace(match[1]),
		}
	}

	// Second: handle inline math $...$
	processInlinePattern(root, mathInlineRe, func(match []string) *Node {
		return &Node{
			Type:    NodeMathInline,
			Literal: match[1],
		}
	})
}

// --- Superscript ---

var superscriptRe = regexp.MustCompile(`\^([^\^\s]+?)\^`)

// processSuperscripts converts ^text^ patterns in text nodes to superscript nodes.
func processSuperscripts(root *Node) {
	processInlinePattern(root, superscriptRe, func(match []string) *Node {
		return &Node{
			Type:    NodeSuperscript,
			Literal: match[1],
		}
	})
}

// --- Helpers ---

// walkNodes performs a depth-first walk of the AST, calling fn for each node.
// fn receives the node, its parent, and the index within the parent's children.
// If fn returns false, children of that node are not visited.
func walkNodes(root *Node, fn func(n *Node, parent *Node, index int) bool) {
	walkNodesRecursive(root, nil, 0, fn)
}

func walkNodesRecursive(n *Node, parent *Node, index int, fn func(*Node, *Node, int) bool) {
	if n == nil {
		return
	}
	if !fn(n, parent, index) {
		return
	}
	for i := 0; i < len(n.Children); i++ {
		walkNodesRecursive(n.Children[i], n, i, fn)
	}
}

// processInlinePattern scans all text nodes in the AST for a regex pattern,
// splitting text nodes to insert new nodes created by the factory function.
func processInlinePattern(root *Node, re *regexp.Regexp, factory func(match []string) *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if len(n.Children) == 0 {
			return true
		}

		newChildren := make([]*Node, 0, len(n.Children))
		changed := false
		for _, child := range n.Children {
			if child.Type != NodeText {
				newChildren = append(newChildren, child)
				continue
			}
			split := splitTextByPattern(child.Literal, re, factory)
			if len(split) == 1 && split[0].Type == NodeText {
				newChildren = append(newChildren, child)
				continue
			}
			newChildren = append(newChildren, split...)
			changed = true
		}

		if changed {
			n.Children = newChildren
		}

		return true
	})
}

// splitTextByPattern splits a text string into alternating text and newly
// created nodes based on a regex pattern.
func splitTextByPattern(text string, re *regexp.Regexp, factory func(match []string) *Node) []*Node {
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return []*Node{textNode(text)}
	}

	var nodes []*Node
	cursor := 0

	for _, loc := range locs {
		if loc[0] > cursor {
			nodes = append(nodes, textNode(text[cursor:loc[0]]))
		}

		match := make([]string, len(loc)/2)
		for i := 0; i < len(loc)/2; i++ {
			if loc[i*2] >= 0 && loc[i*2+1] >= 0 {
				match[i] = text[loc[i*2]:loc[i*2+1]]
			}
		}

		nodes = append(nodes, factory(match))
		cursor = loc[1]
	}

	if cursor < len(text) {
		nodes = append(nodes, textNode(text[cursor:]))
	}

	return nodes
}
