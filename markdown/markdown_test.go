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

func TestCompatibilityOrderedListAndAdmonitionTitle(t *testing.T) {
	html := RenderString(`1) first
2) second

> [!WARNING] Deployment caveat
> Be careful.
`)
	for _, want := range []string{
		"<ol>",
		"<li><p>first</p></li>",
		`class="admonition admonition-warning"`,
		`<p class="admonition-title">Deployment caveat</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("RenderString() missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<ul>") {
		t.Fatalf("RenderString() rendered ordered list as unordered:\n%s", html)
	}
}

func TestCompatibilityAdmonitionTitleEmoji(t *testing.T) {
	html := NewRenderer(WithWrapEmoji(true)).RenderString(`> [!NOTE] Being Defensive on HN... :sweat_smile:
> Let's just say it was a wake-up call
`)
	for _, want := range []string{
		`<p class="admonition-title">Being Defensive on HN... <span class="emoji" role="img" aria-label="sweat_smile">😅</span></p>`,
		"wake-up call",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("RenderString() missing %q in:\n%s", want, html)
		}
	}
}
