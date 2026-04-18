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
	flattenDocumentNodes(doc.Root)
	processAdmonitions(doc.Root)
	processBlockquoteHeadings(doc.Root)
	footnoteDefs := processFootnotes(doc.Root)
	processInlineMath(doc.Root)
	processSuperscripts(doc.Root)
	processEmojiShortcodes(doc.Root)

	// Append referenced footnote definitions at the end of the document.
	for _, def := range footnoteDefs {
		if def != nil {
			doc.Root.Children = append(doc.Root.Children, def)
		}
	}
}

func flattenDocumentNodes(root *Node) {
	if root == nil {
		return
	}
	for i := 0; i < len(root.Children); i++ {
		child := root.Children[i]
		if child == nil {
			continue
		}
		if child.Type == NodeDocument {
			replacement := append([]*Node{}, child.Children...)
			root.Children = append(root.Children[:i], append(replacement, root.Children[i+1:]...)...)
			i--
			continue
		}
		flattenDocumentNodes(child)
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

		firstChild.Children = cleanAdmonitionLeadingChildren(firstChild.Children)

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

func cleanAdmonitionLeadingChildren(children []*Node) []*Node {
	cleaned := make([]*Node, 0, len(children))
	leading := true
	for _, child := range children {
		if child == nil {
			continue
		}
		if leading && (child.Type == NodeSoftBreak || child.Type == NodeHardBreak) {
			continue
		}
		if leading && child.Type == NodeText {
			clone := *child
			clone.Literal = cleanAdmonitionText(clone.Literal)
			if clone.Literal == "" {
				continue
			}
			cleaned = append(cleaned, &clone)
			leading = false
			continue
		}
		cleaned = append(cleaned, child)
		leading = false
	}
	return cleaned
}

func cleanAdmonitionText(text string) string {
	text = strings.TrimLeft(text, "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "> ")
		lines[i] = strings.TrimPrefix(lines[i], ">")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// --- Quote headings ---

var bracketedQuoteHeadingRe = regexp.MustCompile(`^\[[^\^!][\s\S]*\]$`)

func processBlockquoteHeadings(root *Node) {
	walkNodes(root, func(n *Node, parent *Node, index int) bool {
		if n.Type != NodeBlockquote || len(n.Children) == 0 {
			return true
		}
		firstChild := n.Children[0]
		if firstChild.Type != NodeParagraph || len(firstChild.Children) == 0 {
			return true
		}
		firstNode := firstChild.Children[0]
		if firstNode.Type != NodeLink || firstNode.Attrs["href"] != "" {
			return true
		}
		raw := firstNode.Attrs["raw"]
		if !bracketedQuoteHeadingRe.MatchString(raw) {
			return true
		}
		firstChild.Children[0] = newNode(NodeStrong, firstNode.Children...)
		return true
	})
}

// --- Footnotes ---

var footnoteRefRawRe = regexp.MustCompile(`^\[\^([A-Za-z0-9_-]+)\]$`)

// processFootnotes scans for footnote references and definitions.
// Definitions may be:
//  1. NodeFootnoteDef already created by the parser from link_reference_definition
//  2. Paragraphs containing Link(raw="[^id]") followed by Text(": content")
//
// References are Link nodes with raw="[^id]".
func processFootnotes(root *Node) []*Node {
	defs := make(map[string]*Node)
	var defOrder []string
	var defIndices []int

	// First pass: collect footnote definitions from document children.
	for i, child := range root.Children {
		// Case 1: already a NodeFootnoteDef from parser
		if child.Type == NodeFootnoteDef {
			id := child.Attrs["id"]
			if id != "" {
				if _, exists := defs[id]; !exists {
					defOrder = append(defOrder, id)
				}
				defs[id] = child
			}
			defIndices = append(defIndices, i)
			continue
		}

		// Case 2: paragraph containing Link(raw="[^id]") + Text(": content")
		if child.Type != NodeParagraph || len(child.Children) == 0 {
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
		defChildren, ok := footnoteDefinitionChildren(child.Children[1:])
		if !ok {
			continue
		}
		defNode := &Node{
			Type:     NodeFootnoteDef,
			Attrs:    map[string]string{"id": match[1]},
			Children: defChildren,
		}
		if _, exists := defs[match[1]]; !exists {
			defOrder = append(defOrder, match[1])
		}
		defs[match[1]] = defNode
		defIndices = append(defIndices, i)
	}

	// Remove definition nodes from root (in reverse order to preserve indices).
	for j := len(defIndices) - 1; j >= 0; j-- {
		idx := defIndices[j]
		root.Children = append(root.Children[:idx], root.Children[idx+1:]...)
	}

	// Second pass: convert footnote references in all nodes.
	// Replace Link nodes with raw="[^id]" with FootnoteRef nodes.
	var refOrder []string
	seenRefs := make(map[string]struct{})
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
			id := match[1]
			if _, seen := seenRefs[id]; !seen {
				refOrder = append(refOrder, id)
				seenRefs[id] = struct{}{}
			}
			n.Children[i] = &Node{
				Type:  NodeFootnoteRef,
				Attrs: map[string]string{"id": id},
			}
		}
		return true
	})

	ordered := make([]*Node, 0, len(refOrder))
	for _, id := range refOrder {
		if def := defs[id]; def != nil {
			ordered = append(ordered, def)
		}
	}
	if len(ordered) > 0 {
		return ordered
	}
	for _, id := range defOrder {
		if def := defs[id]; def != nil && len(def.Children) > 0 {
			ordered = append(ordered, def)
		}
	}
	return ordered
}

func footnoteDefinitionChildren(children []*Node) ([]*Node, bool) {
	if len(children) == 0 {
		return nil, false
	}
	first := children[0]
	if first.Type != NodeText || !strings.HasPrefix(first.Literal, ":") {
		return nil, false
	}

	out := make([]*Node, 0, len(children))
	firstText := strings.TrimPrefix(first.Literal, ":")
	firstText = strings.TrimPrefix(firstText, " ")
	firstText = strings.TrimPrefix(firstText, "\t")
	if firstText != "" {
		clone := *first
		clone.Literal = firstText
		out = append(out, &clone)
	}
	out = append(out, children[1:]...)
	return out, true
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
