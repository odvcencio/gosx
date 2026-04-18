package markdown

import (
	"strings"
	"testing"
)

func TestMarkdownPPParsingRegressionMatrix(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		typ     NodeType
		attrs   map[string]string
		literal string
	}{
		{
			name:    "code fence",
			source:  "```go\nfmt.Println(\"hello\")\n```",
			typ:     NodeCodeBlock,
			attrs:   map[string]string{"language": "go"},
			literal: "fmt.Println",
		},
		{
			name:    "mermaid diagram fence",
			source:  "```mermaid\nflowchart TD\n  A --> B\n```",
			typ:     NodeDiagram,
			attrs:   map[string]string{"syntax": "mermaid", "kind": "flowchart"},
			literal: "A --> B",
		},
		{
			name:    "erd diagram alias fence",
			source:  "```erd\nUser ||--o{ Post : writes\n```",
			typ:     NodeDiagram,
			attrs:   map[string]string{"syntax": "mermaid", "kind": "erd"},
			literal: "User",
		},
		{
			name:    "math fence",
			source:  "$$\nE = mc^{2}\n$$",
			typ:     NodeMathBlock,
			literal: "E = mc^{2}",
		},
		{
			name:    "inline math",
			source:  "Energy is $E = mc^{2}$.",
			typ:     NodeMathInline,
			literal: "E = mc^{2}",
		},
		{
			name:   "admonition",
			source: "> [!WARNING]\n> Careful.",
			typ:    NodeAdmonition,
			attrs:  map[string]string{"type": "warning"},
		},
		{
			name:   "footnote reference",
			source: "Text[^one]\n\n[^one]: Footnote.",
			typ:    NodeFootnoteRef,
			attrs:  map[string]string{"id": "one"},
		},
		{
			name:    "footnote definition",
			source:  "Text[^one]\n\n[^one]: Footnote.",
			typ:     NodeFootnoteDef,
			attrs:   map[string]string{"id": "one"},
			literal: "Footnote.",
		},
		{
			name:    "emoji shortcode",
			source:  "Nice :sweat_smile:",
			typ:     NodeEmoji,
			literal: "😅",
		},
		{
			name:    "task list",
			source:  "- [x] Done",
			typ:     NodeTaskListItem,
			attrs:   map[string]string{"checked": "true"},
			literal: "Done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse([]byte(tt.source))
			node := findFirstNodeOfType(doc.Root, tt.typ)
			if node == nil {
				t.Fatalf("expected node type %d in %#v", tt.typ, doc.Root)
			}
			for key, want := range tt.attrs {
				if got := node.Attrs[key]; got != want {
					t.Fatalf("expected attr %s=%q, got %q", key, want, got)
				}
			}
			if tt.literal != "" && !containsNodeLiteral(node, tt.literal) {
				t.Fatalf("expected node literal/text to contain %q, got literal=%q text=%q", tt.literal, node.Literal, collectNodeText(node))
			}
		})
	}
}

func TestMarkdownPPRenderingRegressionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		contains    []string
		notContains []string
	}{
		{
			name:     "code fence",
			source:   "```go\nfmt.Println(\"hello\")\n```",
			contains: []string{`<pre><code class="language-go">`, `fmt.Println`},
		},
		{
			name:     "mermaid diagram fence",
			source:   "```mermaid\nflowchart TD\n  A --> B\n```",
			contains: []string{`class="mdpp-diagram mdpp-diagram-mermaid mdpp-diagram-flowchart"`, `data-diagram-kind="flowchart"`, `A --&gt; B`},
		},
		{
			name:     "erd diagram alias fence",
			source:   "```erd\nUser ||--o{ Post : writes\n```",
			contains: []string{`mdpp-diagram-erd`, `<code class="language-erd">`, `User ||--o{ Post : writes`},
		},
		{
			name:     "math fence",
			source:   "$$\nE = mc^{2}\n$$",
			contains: []string{`class="math-block"`, `E=mc<sup>2</sup>`},
		},
		{
			name:     "inline math",
			source:   "Energy is $E = mc^{2}$.",
			contains: []string{`class="math-inline"`, `E=mc<sup>2</sup>`},
		},
		{
			name:        "diagram content is not processed as math",
			source:      "```mermaid\nflowchart TD\n  A[$E = mc^2$] --> B\n```",
			contains:    []string{`mdpp-diagram-flowchart`, `$E = mc^2$`},
			notContains: []string{`math-inline`},
		},
		{
			name:     "admonition",
			source:   "> [!NOTE]\n> Body.",
			contains: []string{`class="admonition admonition-note"`, `<p class="admonition-title">NOTE</p>`, `Body.`},
		},
		{
			name:     "footnote",
			source:   "Text[^one]\n\n[^one]: Footnote.",
			contains: []string{`class="footnote-ref"`, `href="#fn-one"`, `class="footnotes"`, `Footnote.`},
		},
		{
			name:     "emoji shortcode",
			source:   "Nice :sweat_smile:",
			contains: []string{`😅`},
		},
		{
			name:     "task list",
			source:   "- [x] Done",
			contains: []string{`class="task-list-item"`, `checked`, `Done`},
		},
	}

	renderer := NewRenderer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := renderer.RenderString(tt.source)
			for _, want := range tt.contains {
				assertContains(t, html, want)
			}
			for _, unwanted := range tt.notContains {
				assertNotContains(t, html, unwanted)
			}
		})
	}
}

func findFirstNodeOfType(node *Node, typ NodeType) *Node {
	if node == nil {
		return nil
	}
	if node.Type == typ {
		return node
	}
	for _, child := range node.Children {
		if found := findFirstNodeOfType(child, typ); found != nil {
			return found
		}
	}
	return nil
}

func containsNodeLiteral(node *Node, want string) bool {
	if node == nil {
		return false
	}
	if strings.Contains(node.Literal, want) || strings.Contains(collectNodeText(node), want) {
		return true
	}
	for _, child := range node.Children {
		if containsNodeLiteral(child, want) {
			return true
		}
	}
	return false
}
