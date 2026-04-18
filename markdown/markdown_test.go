package markdown

import (
	"strings"
	"testing"
)

func TestCompatibilityRenderString(t *testing.T) {
	html := RenderString(`# Hello

> [!NOTE]
> It works.

` + "```mermaid" + `
flowchart TD
  A --> B
` + "```" + `
`)
	for _, want := range []string{
		"<h1>Hello</h1>",
		`class="admonition admonition-note"`,
		`class="mdpp-diagram mdpp-diagram-mermaid mdpp-diagram-flowchart"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("RenderString() missing %q in:\n%s", want, html)
		}
	}
}

func TestCompatibilityParseAliasesTypes(t *testing.T) {
	doc := Parse([]byte("# Title"))
	if doc.Root.Type != NodeDocument {
		t.Fatalf("root type = %v, want %v", doc.Root.Type, NodeDocument)
	}
	if doc.Root.Children[0].Type != NodeHeading {
		t.Fatalf("first child type = %v, want %v", doc.Root.Children[0].Type, NodeHeading)
	}
}
