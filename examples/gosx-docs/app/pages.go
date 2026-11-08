package docs

import (
	"fmt"
	"html"
	"path/filepath"
	"runtime"
	"strings"

	"m31labs.dev/gosx"
	pages "m31labs.dev/gosx-pages"
	"m31labs.dev/gosx/server"
	"m31labs.dev/mdpp"
)

// LoadMarkdownDoc supplies the normal GoSX route loader with Pages content.
// The enclosing route still owns its scene, live demo, metadata, and layout.
func LoadMarkdownDoc(slug string) (map[string]any, error) {
	_, file, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(filepath.Join(filepath.Dir(file), "..", "main.go"))
	library, err := pages.Load(root, pages.Collection{Name: "docs", Dir: "content/docs", Prefix: "/docs"})
	if err != nil {
		return nil, err
	}
	doc, ok := library.BySlug("docs", slug)
	if !ok {
		return nil, fmt.Errorf("missing Pages document %q", slug)
	}
	node, toc, err := renderMarkdownDoc(doc)
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(doc.Metadata)+4)
	for key, value := range doc.Metadata {
		values[key] = value
	}
	values["title"] = doc.Title
	values["description"] = doc.Description
	values["content"] = node
	values["toc"] = toc
	return values, nil
}

func renderMarkdownDoc(doc pages.Document) (gosx.Node, []map[string]string, error) {
	anchors, _ := doc.Metadata["anchors"].(map[string]any)
	labels, _ := doc.Metadata["toc_labels"].(map[string]any)
	var toc []map[string]string
	ids := map[string]string{}
	for _, heading := range doc.Parsed.Headings() {
		id := heading.ID
		if custom, ok := anchors[heading.Text].(string); ok {
			id = custom
		}
		ids[heading.Text] = id
		if heading.Level == 2 {
			label := heading.Text
			if custom, ok := labels[id].(string); ok {
				label = custom
			}
			toc = append(toc, map[string]string{"href": "#" + id, "label": label})
		}
	}
	opts := mdpp.RenderOptions{HeadingIDs: true, NodeRenderers: map[mdpp.NodeType]mdpp.NodeRenderer{
		mdpp.NodeCodeBlock: func(_ *mdpp.Renderer, b *strings.Builder, n *mdpp.Node) bool {
			b.WriteString(gosx.RenderHTML(CodeBlock(n.Attrs["language"], strings.TrimSuffix(n.Literal, "\n"))))
			return true
		},
		mdpp.NodeCodeSpan: func(_ *mdpp.Renderer, b *strings.Builder, n *mdpp.Node) bool {
			b.WriteString(`<span class="inline-code">` + html.EscapeString(n.Literal) + `</span>`)
			return true
		},
		mdpp.NodeHeading: func(r *mdpp.Renderer, b *strings.Builder, n *mdpp.Node) bool {
			var label strings.Builder
			// Headings in these guides are plain text; mdpp owns their rendered
			// inline children, while Pages frontmatter preserves historical IDs.
			var text func(*mdpp.Node)
			text = func(node *mdpp.Node) {
				label.WriteString(node.Literal)
				for _, child := range node.Children {
					text(child)
				}
			}
			for _, child := range n.Children {
				text(child)
			}
			level := n.Attrs["level"]
			if level == "" {
				level = "2"
			}
			b.WriteString("<h" + level + ` id="` + html.EscapeString(ids[label.String()]) + `">`)
			r.RenderChildrenInto(b, n)
			b.WriteString("</h" + level + ">\n")
			return true
		},
	}}
	output, err := mdpp.Render(doc.Parsed, opts)
	if err != nil {
		return gosx.Node{}, nil, err
	}
	return gosx.RawHTML(string(output)), toc, nil
}
