package markdown

import (
	"html"
	"strings"
	"unicode"
)

// renderNode recursively renders an AST node to HTML. Returns a string
// for convenience at the top level, but internally delegates to
// renderNodeInto which writes directly into a shared strings.Builder —
// that avoids the per-node fmt.Sprintf + intermediate string allocation
// that the previous implementation paid for every AST node.
func renderNode(r *Renderer, n *Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	renderNodeInto(r, &b, n)
	return b.String()
}

// renderNodeInto writes the HTML for n into b. Internal recursive
// entry point — callers that already have a builder should use this
// directly to avoid an intermediate string alloc per subtree.
func renderNodeInto(r *Renderer, b *strings.Builder, n *Node) {
	if n == nil {
		return
	}

	switch n.Type {
	case NodeDocument:
		renderChildrenInto(r, b, n)

	case NodeHeading:
		level := n.Attrs["level"]
		if level == "" {
			level = "1"
		}
		b.WriteString("<h")
		b.WriteString(level)
		if r.headingIDs {
			id := slugify(collectNodeText(n))
			b.WriteString(` id="`)
			b.WriteString(id)
			b.WriteByte('"')
		}
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</h")
		b.WriteString(level)
		b.WriteString(">\n")

	case NodeParagraph:
		b.WriteString("<p>")
		renderChildrenInto(r, b, n)
		b.WriteString("</p>\n")

	case NodeCodeBlock:
		lang := n.Attrs["language"]
		if r.highlightCode && lang != "" {
			if highlighted, ok := highlightCode(lang, n.Literal); ok {
				b.WriteString(`<pre><code class="language-`)
				b.WriteString(html.EscapeString(lang))
				b.WriteString(`">`)
				b.WriteString(highlighted)
				b.WriteString("</code></pre>\n")
				return
			}
		}
		code := html.EscapeString(n.Literal)
		if lang != "" {
			b.WriteString(`<pre><code class="language-`)
			b.WriteString(html.EscapeString(lang))
			b.WriteString(`">`)
			b.WriteString(code)
			b.WriteString("</code></pre>\n")
			return
		}
		b.WriteString("<pre><code>")
		b.WriteString(code)
		b.WriteString("</code></pre>\n")

	case NodeBlockquote:
		b.WriteString("<blockquote>\n")
		renderChildrenInto(r, b, n)
		b.WriteString("</blockquote>\n")

	case NodeList:
		tag := "ul"
		if n.Attrs != nil && n.Attrs["ordered"] == "true" {
			tag = "ol"
		}
		b.WriteByte('<')
		b.WriteString(tag)
		b.WriteString(">\n")
		renderChildrenInto(r, b, n)
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">\n")

	case NodeListItem:
		b.WriteString("<li>")
		// Render children into a temporary builder so we can trim the
		// trailing newline cleanly. The child nodes are typically a
		// single paragraph so the temp cost is bounded.
		var inner strings.Builder
		renderChildrenInto(r, &inner, n)
		b.WriteString(strings.TrimRight(inner.String(), "\n"))
		b.WriteString("</li>\n")

	case NodeTable:
		renderTableInto(r, b, n)

	case NodeThematicBreak:
		b.WriteString("<hr />\n")

	case NodeLink:
		href := html.EscapeString(n.Attrs["href"])
		title := n.Attrs["title"]
		b.WriteString(`<a href="`)
		b.WriteString(href)
		b.WriteByte('"')
		if title != "" {
			b.WriteString(` title="`)
			b.WriteString(html.EscapeString(title))
			b.WriteByte('"')
		}
		b.WriteByte('>')
		renderChildrenInto(r, b, n)
		b.WriteString("</a>")

	case NodeImage:
		src := n.Attrs["src"]
		if r.imageResolver != nil {
			src = r.imageResolver(src)
		}
		alt := html.EscapeString(n.Attrs["alt"])
		src = html.EscapeString(src)
		title := n.Attrs["title"]
		if title != "" {
			b.WriteString(`<figure><img src="`)
			b.WriteString(src)
			b.WriteString(`" alt="`)
			b.WriteString(alt)
			b.WriteString(`" /><figcaption>`)
			b.WriteString(html.EscapeString(title))
			b.WriteString("</figcaption></figure>")
			return
		}
		b.WriteString(`<img src="`)
		b.WriteString(src)
		b.WriteString(`" alt="`)
		b.WriteString(alt)
		b.WriteString(`" />`)

	case NodeEmphasis:
		b.WriteString("<em>")
		renderChildrenInto(r, b, n)
		b.WriteString("</em>")

	case NodeStrong:
		b.WriteString("<strong>")
		renderChildrenInto(r, b, n)
		b.WriteString("</strong>")

	case NodeStrikethrough:
		b.WriteString("<del>")
		renderChildrenInto(r, b, n)
		b.WriteString("</del>")

	case NodeCodeSpan:
		b.WriteString("<code>")
		b.WriteString(html.EscapeString(n.Literal))
		b.WriteString("</code>")

	case NodeText:
		b.WriteString(html.EscapeString(n.Literal))

	case NodeHTMLBlock:
		if r.unsafeHTML {
			b.WriteString(n.Literal)
		} else {
			b.WriteString(html.EscapeString(n.Literal))
		}

	case NodeHTMLInline:
		if r.unsafeHTML {
			b.WriteString(n.Literal)
		} else {
			b.WriteString(html.EscapeString(n.Literal))
		}

	case NodeSoftBreak:
		if r.hardWraps {
			b.WriteString("<br />\n")
		} else {
			b.WriteByte('\n')
		}

	case NodeHardBreak:
		b.WriteString("<br />\n")

	case NodeAdmonition:
		adType := n.Attrs["type"]
		title := strings.ToUpper(adType)
		b.WriteString(`<div class="admonition admonition-`)
		b.WriteString(adType)
		b.WriteString(`"><p class="admonition-title">`)
		b.WriteString(title)
		b.WriteString("</p>")
		renderChildrenInto(r, b, n)
		b.WriteString("</div>\n")

	case NodeTaskListItem:
		b.WriteString(`<li class="task-list-item"><input type="checkbox" disabled`)
		if n.Attrs["checked"] == "true" {
			b.WriteString(" checked")
		}
		b.WriteString(" />")
		var inner strings.Builder
		renderChildrenInto(r, &inner, n)
		b.WriteString(strings.TrimRight(inner.String(), "\n"))
		b.WriteString("</li>\n")

	case NodeFootnoteRef:
		id := html.EscapeString(n.Attrs["id"])
		b.WriteString(`<sup><a class="footnote-ref" href="#fn-`)
		b.WriteString(id)
		b.WriteString(`" id="fnref-`)
		b.WriteString(id)
		b.WriteString(`">[`)
		b.WriteString(id)
		b.WriteString("]</a></sup>")

	case NodeFootnoteDef:
		id := html.EscapeString(n.Attrs["id"])
		b.WriteString(`<section class="footnotes"><ol><li id="fn-`)
		b.WriteString(id)
		b.WriteString(`">`)
		renderChildrenInto(r, b, n)
		b.WriteString(` <a href="#fnref-`)
		b.WriteString(id)
		b.WriteString(`">\u21a9</a></li></ol></section>`)
		b.WriteByte('\n')

	case NodeMathInline:
		b.WriteString(`<span class="math-inline">`)
		b.WriteString(renderLatexMath(n.Literal))
		b.WriteString("</span>")

	case NodeMathBlock:
		b.WriteString(`<div class="math-block">`)
		b.WriteString(renderLatexMath(n.Literal))
		b.WriteString("</div>\n")

	case NodeSuperscript:
		b.WriteString("<sup>")
		b.WriteString(html.EscapeString(n.Literal))
		b.WriteString("</sup>")

	case NodeSubscript:
		b.WriteString("<sub>")
		b.WriteString(html.EscapeString(n.Literal))
		b.WriteString("</sub>")

	case NodeEmoji:
		if r.wrapEmoji {
			code := n.Attrs["code"]
			b.WriteString(`<span class="emoji" role="img" aria-label="`)
			b.WriteString(html.EscapeString(code))
			b.WriteString(`">`)
			b.WriteString(n.Literal)
			b.WriteString("</span>")
		} else {
			b.WriteString(n.Literal)
		}

	default:
		renderChildrenInto(r, b, n)
	}
}

// renderChildrenInto writes the children of n into b without allocating
// an intermediate string per child.
func renderChildrenInto(r *Renderer, b *strings.Builder, n *Node) {
	for _, child := range n.Children {
		renderNodeInto(r, b, child)
	}
}

func renderTableInto(r *Renderer, b *strings.Builder, n *Node) {
	b.WriteString("<table>\n")
	for i, row := range n.Children {
		if row.Type != NodeTableRow {
			continue
		}
		if i == 0 {
			b.WriteString("<thead>\n<tr>")
			for _, cell := range row.Children {
				b.WriteString("<th>")
				renderChildrenInto(r, b, cell)
				b.WriteString("</th>")
			}
			b.WriteString("</tr>\n</thead>\n<tbody>\n")
		} else {
			b.WriteString("<tr>")
			for _, cell := range row.Children {
				b.WriteString("<td>")
				renderChildrenInto(r, b, cell)
				b.WriteString("</td>")
			}
			b.WriteString("</tr>\n")
		}
	}
	b.WriteString("</tbody>\n</table>\n")
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
