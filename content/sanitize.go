package content

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Sanitize in MDPPOptions selects inert Markdown HTML. Keep this policy here:
// the mdpp dependency currently reserves, but does not implement, that option.
// Active embeds, forms, SVG and MathML are excluded. Applications needing those
// trusted surfaces can render them separately without opting into this policy.
func sanitizeMarkdownHTML(source []byte) ([]byte, error) {
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(bytes.NewReader(source), context)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	for _, node := range nodes {
		for _, safe := range sanitizeMarkdownNode(node) {
			if err := html.Render(&output, safe); err != nil {
				return nil, err
			}
		}
	}
	return output.Bytes(), nil
}

func sanitizeMarkdownNode(node *html.Node) []*html.Node {
	if node.Type == html.TextNode {
		return []*html.Node{{Type: html.TextNode, Data: node.Data}}
	}
	if node.Type != html.ElementNode || node.Namespace != "" {
		return nil
	}
	switch node.Data {
	case "script", "style", "iframe", "object", "embed", "template", "svg", "math", "form", "button", "textarea", "select":
		return nil
	}
	var children []*html.Node
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		children = append(children, sanitizeMarkdownNode(child)...)
	}
	switch node.Data {
	case "a", "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "blockquote", "code", "pre", "em", "strong", "del", "s", "sup", "sub", "hr", "br", "table", "thead", "tbody", "tfoot", "tr", "th", "td", "img", "span", "div", "section", "details", "summary", "kbd", "mark", "input":
	default:
		return children
	}
	safe := &html.Node{Type: html.ElementNode, Data: node.Data, DataAtom: node.DataAtom}
	for _, attr := range node.Attr {
		if attr.Namespace != "" {
			continue
		}
		keep := false
		switch attr.Key {
		case "id", "class", "title", "lang", "dir", "aria-label", "aria-hidden", "role",
			"data-mdpp-source-start", "data-mdpp-source-end", "data-mdpp-source-line", "data-mdpp-source-col", "data-mdpp-source-end-line", "data-mdpp-source-end-col":
			keep = true
		case "href":
			keep = node.Data == "a" && safeMarkdownURL(attr.Val, true)
		case "src":
			keep = node.Data == "img" && safeMarkdownURL(attr.Val, false)
		case "alt", "width", "height":
			keep = node.Data == "img"
		case "colspan", "rowspan", "scope", "align":
			keep = node.Data == "th" || node.Data == "td"
		case "start", "reversed":
			keep = node.Data == "ol"
		case "checked":
			keep = node.Data == "input"
		case "type":
			keep = node.Data == "input" && attr.Val == "checkbox"
		}
		if keep {
			safe.Attr = append(safe.Attr, attr)
		}
	}
	if node.Data == "input" {
		checkbox := false
		for _, attr := range safe.Attr {
			if attr.Key == "type" && attr.Val == "checkbox" {
				checkbox = true
			}
		}
		if !checkbox {
			return nil
		}
		safe.Attr = append(safe.Attr, html.Attribute{Key: "disabled"})
	}
	if node.Data == "a" {
		safe.Attr = append(safe.Attr, html.Attribute{Key: "rel", Val: "noopener noreferrer"})
	}
	for _, child := range children {
		safe.AppendChild(child)
	}
	return []*html.Node{safe}
}

func safeMarkdownURL(value string, link bool) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "", "http", "https":
		return true
	case "mailto":
		return link
	default:
		return false
	}
}
