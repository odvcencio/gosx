package markdown

import (
	"fmt"
	"html"
	"strings"
	"unicode"
)

// renderNode recursively renders an AST node to HTML.
func renderNode(r *Renderer, n *Node) string {
	if n == nil {
		return ""
	}

	switch n.Type {
	case NodeDocument:
		return renderChildren(r, n)

	case NodeHeading:
		level := n.Attrs["level"]
		if level == "" {
			level = "1"
		}
		inner := renderChildren(r, n)
		if r.headingIDs {
			id := slugify(collectNodeText(n))
			return fmt.Sprintf("<h%s id=\"%s\">%s</h%s>\n", level, id, inner, level)
		}
		return fmt.Sprintf("<h%s>%s</h%s>\n", level, inner, level)

	case NodeParagraph:
		inner := renderChildren(r, n)
		return fmt.Sprintf("<p>%s</p>\n", inner)

	case NodeCodeBlock:
		lang := n.Attrs["language"]
		code := html.EscapeString(n.Literal)
		if lang != "" {
			return fmt.Sprintf("<pre><code class=\"language-%s\">%s</code></pre>\n", html.EscapeString(lang), code)
		}
		return fmt.Sprintf("<pre><code>%s</code></pre>\n", code)

	case NodeBlockquote:
		inner := renderChildren(r, n)
		return fmt.Sprintf("<blockquote>\n%s</blockquote>\n", inner)

	case NodeList:
		// Check if ordered
		tag := "ul"
		if n.Attrs != nil && n.Attrs["ordered"] == "true" {
			tag = "ol"
		}
		inner := renderChildren(r, n)
		return fmt.Sprintf("<%s>\n%s</%s>\n", tag, inner, tag)

	case NodeListItem:
		inner := renderChildren(r, n)
		// Trim trailing newline from inner content for cleaner output
		inner = strings.TrimRight(inner, "\n")
		return fmt.Sprintf("<li>%s</li>\n", inner)

	case NodeTable:
		return renderTable(r, n)

	case NodeThematicBreak:
		return "<hr />\n"

	case NodeLink:
		href := html.EscapeString(n.Attrs["href"])
		inner := renderChildren(r, n)
		title := n.Attrs["title"]
		if title != "" {
			return fmt.Sprintf("<a href=\"%s\" title=\"%s\">%s</a>", href, html.EscapeString(title), inner)
		}
		return fmt.Sprintf("<a href=\"%s\">%s</a>", href, inner)

	case NodeImage:
		src := n.Attrs["src"]
		if r.imageResolver != nil {
			src = r.imageResolver(src)
		}
		alt := html.EscapeString(n.Attrs["alt"])
		src = html.EscapeString(src)
		title := n.Attrs["title"]
		if title != "" {
			return fmt.Sprintf("<figure><img src=\"%s\" alt=\"%s\" /><figcaption>%s</figcaption></figure>", src, alt, html.EscapeString(title))
		}
		return fmt.Sprintf("<img src=\"%s\" alt=\"%s\" />", src, alt)

	case NodeEmphasis:
		return fmt.Sprintf("<em>%s</em>", renderChildren(r, n))

	case NodeStrong:
		return fmt.Sprintf("<strong>%s</strong>", renderChildren(r, n))

	case NodeStrikethrough:
		return fmt.Sprintf("<del>%s</del>", renderChildren(r, n))

	case NodeCodeSpan:
		return fmt.Sprintf("<code>%s</code>", html.EscapeString(n.Literal))

	case NodeText:
		return html.EscapeString(n.Literal)

	case NodeHTMLBlock:
		if r.unsafeHTML {
			return n.Literal
		}
		return html.EscapeString(n.Literal)

	case NodeHTMLInline:
		if r.unsafeHTML {
			return n.Literal
		}
		return html.EscapeString(n.Literal)

	case NodeSoftBreak:
		return "\n"

	case NodeHardBreak:
		return "<br />\n"

	default:
		// For any unhandled node type, render children
		return renderChildren(r, n)
	}
}

// renderChildren renders all children of a node and concatenates the results.
func renderChildren(r *Renderer, n *Node) string {
	var sb strings.Builder
	for _, child := range n.Children {
		sb.WriteString(renderNode(r, child))
	}
	return sb.String()
}

// renderTable renders a table node with thead/tbody structure.
func renderTable(r *Renderer, n *Node) string {
	var sb strings.Builder
	sb.WriteString("<table>\n")
	for i, row := range n.Children {
		if row.Type != NodeTableRow {
			continue
		}
		if i == 0 {
			sb.WriteString("<thead>\n<tr>")
			for _, cell := range row.Children {
				sb.WriteString("<th>")
				sb.WriteString(renderChildren(r, cell))
				sb.WriteString("</th>")
			}
			sb.WriteString("</tr>\n</thead>\n<tbody>\n")
		} else {
			sb.WriteString("<tr>")
			for _, cell := range row.Children {
				sb.WriteString("<td>")
				sb.WriteString(renderChildren(r, cell))
				sb.WriteString("</td>")
			}
			sb.WriteString("</tr>\n")
		}
	}
	sb.WriteString("</tbody>\n</table>\n")
	return sb.String()
}

// collectNodeText recursively extracts plain text from a node tree.
func collectNodeText(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Type == NodeText {
		return n.Literal
	}
	if n.Type == NodeCodeSpan {
		return n.Literal
	}
	var sb strings.Builder
	for _, c := range n.Children {
		sb.WriteString(collectNodeText(c))
	}
	return sb.String()
}

// slugify converts text into a URL-friendly ID string.
func slugify(s string) string {
	var sb strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			prevDash = false
		} else if r == ' ' || r == '-' || r == '_' {
			if !prevDash && sb.Len() > 0 {
				sb.WriteByte('-')
				prevDash = true
			}
		}
	}
	result := sb.String()
	result = strings.TrimRight(result, "-")
	return result
}
