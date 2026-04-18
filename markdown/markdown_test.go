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

func TestCompatibilityPreservesLongParagraphTail(t *testing.T) {
	html := RenderString(strings.Join([]string{
		"It was an alerting to several signals:",
		"",
		"1. **Even when confronted with mounting evidence to the contrary, many folks are still firmly head-in-sand about LLM-assisted approaches to software**",
		"",
		"There's a definitive stigma on using AI to help you do things. It was almost last year when I was myself a convert to the AI-assisted workflow and it was almost difficult for me to do so. I had stigmatized the LLMs \"wasteful\", or \"too good at producing everything I ask for and nothing I want.\". Adapting to them and adopting them is continuous struggle- it was until I realized that this is a software system like any other that I realized there is a real \"superstitious\" or \"magical\" attribution or sentiment applied towards these technologies. In other words, **they're not well understood so they're feared.**",
	}, "\n"))

	for _, want := range []string{
		`real &#34;superstitious&#34; or &#34;magical&#34; attribution`,
		`they&#39;re not well understood so they&#39;re feared`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("RenderString() missing %q in:\n%s", want, html)
		}
	}
}
