package markdown

import (
	"strings"
	"testing"
)

func TestParseHeading(t *testing.T) {
	doc := Parse([]byte("# Hello"))
	root := doc.Root
	if root.Type != NodeDocument {
		t.Fatalf("expected NodeDocument, got %d", root.Type)
	}
	if len(root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	if h.Attrs["level"] != "1" {
		t.Fatalf("expected level 1, got %q", h.Attrs["level"])
	}
	if len(h.Children) == 0 {
		t.Fatal("expected heading to have text children")
	}
	text := collectText(h)
	if text != "Hello" {
		t.Fatalf("expected text %q, got %q", "Hello", text)
	}
}

func TestParseHeadingLevels(t *testing.T) {
	tests := []struct {
		src   string
		level string
		text  string
	}{
		{"## Second", "2", "Second"},
		{"### Third", "3", "Third"},
		{"#### Fourth", "4", "Fourth"},
		{"##### Fifth", "5", "Fifth"},
		{"###### Sixth", "6", "Sixth"},
	}
	for _, tt := range tests {
		doc := Parse([]byte(tt.src))
		if len(doc.Root.Children) == 0 {
			t.Fatalf("no children for %q", tt.src)
		}
		h := doc.Root.Children[0]
		if h.Type != NodeHeading {
			t.Fatalf("expected NodeHeading for %q, got %d", tt.src, h.Type)
		}
		if h.Attrs["level"] != tt.level {
			t.Errorf("expected level %q for %q, got %q", tt.level, tt.src, h.Attrs["level"])
		}
		text := collectText(h)
		if text != tt.text {
			t.Errorf("expected text %q for %q, got %q", tt.text, tt.src, text)
		}
	}
}

func TestParseHeadingWithExclamation(t *testing.T) {
	doc := Parse([]byte("# Hello World!"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := doc.Root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	text := collectText(h)
	if text != "Hello World!" {
		t.Fatalf("expected text %q, got %q", "Hello World!", text)
	}
}

func TestParseHeadingWithTerminalPunctuationInLongDocument(t *testing.T) {
	src := []byte(`# Hello World!

A matter of origin, this didn't really exist before February 19th, 2026. That was the evening I would conceive of gotreesitter[^1]. Less than a week or so later, I would foolishly declare it ported and solved to HackerNews[^2] to mixed-ish reviews. I think the general sentiment was that maybe this could be useful to *someone*, *somewhere*, *eventually*-- but that this was simply too immature, too vibe-coded, and too much maintenance burden for a single person.

[^1]: [gotreesitter](https://github.com/odvcencio/gotreesitter)
[^2]: [Disaster, but lucky still](https://link-to-article)
`)

	doc := Parse(src)
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := doc.Root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	if text := collectText(h); text != "Hello World!" {
		t.Fatalf("expected heading text %q, got %q", "Hello World!", text)
	}
}

func TestParseParagraph(t *testing.T) {
	doc := Parse([]byte("Just some plain text."))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	if p.Type != NodeParagraph {
		t.Fatalf("expected NodeParagraph, got %d", p.Type)
	}
	text := collectText(p)
	if text != "Just some plain text." {
		t.Fatalf("expected %q, got %q", "Just some plain text.", text)
	}
}

func TestParseCodeBlock(t *testing.T) {
	src := "```go\nfmt.Println(\"hello\")\n```"
	doc := Parse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	cb := doc.Root.Children[0]
	if cb.Type != NodeCodeBlock {
		t.Fatalf("expected NodeCodeBlock, got %d", cb.Type)
	}
	if cb.Attrs["language"] != "go" {
		t.Fatalf("expected language %q, got %q", "go", cb.Attrs["language"])
	}
	if cb.Literal == "" {
		t.Fatal("expected non-empty code literal")
	}
}

func TestParseDiagramFence(t *testing.T) {
	src := "```mermaid\nflowchart TD\n  A --> B\n```"
	doc := Parse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	diagram := doc.Root.Children[0]
	if diagram.Type != NodeDiagram {
		t.Fatalf("expected NodeDiagram, got %d", diagram.Type)
	}
	if diagram.Attrs["syntax"] != "mermaid" {
		t.Fatalf("expected syntax %q, got %q", "mermaid", diagram.Attrs["syntax"])
	}
	if diagram.Attrs["kind"] != "flowchart" {
		t.Fatalf("expected kind %q, got %q", "flowchart", diagram.Attrs["kind"])
	}
	if !strings.Contains(diagram.Literal, "A --> B") {
		t.Fatalf("expected diagram literal to contain edge, got %q", diagram.Literal)
	}
}

func TestParseDiagramAliasFence(t *testing.T) {
	doc := Parse([]byte("```erd\nUser ||--o{ Post : writes\n```"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	diagram := doc.Root.Children[0]
	if diagram.Type != NodeDiagram {
		t.Fatalf("expected NodeDiagram, got %d", diagram.Type)
	}
	if diagram.Attrs["kind"] != "erd" {
		t.Fatalf("expected kind %q, got %q", "erd", diagram.Attrs["kind"])
	}
}

func TestParseBoldItalic(t *testing.T) {
	doc := Parse([]byte("**bold** and *italic*"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	if p.Type != NodeParagraph {
		t.Fatalf("expected NodeParagraph, got %d", p.Type)
	}

	var foundStrong, foundEmphasis bool
	for _, c := range p.Children {
		if c.Type == NodeStrong {
			foundStrong = true
			text := collectText(c)
			if text != "bold" {
				t.Errorf("expected strong text %q, got %q", "bold", text)
			}
		}
		if c.Type == NodeEmphasis {
			foundEmphasis = true
			text := collectText(c)
			if text != "italic" {
				t.Errorf("expected emphasis text %q, got %q", "italic", text)
			}
		}
	}
	if !foundStrong {
		t.Error("expected NodeStrong child")
	}
	if !foundEmphasis {
		t.Error("expected NodeEmphasis child")
	}
}

func TestParseList(t *testing.T) {
	doc := Parse([]byte("- one\n- two"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	list := doc.Root.Children[0]
	if list.Type != NodeList {
		t.Fatalf("expected NodeList, got %d", list.Type)
	}
	if len(list.Children) < 2 {
		t.Fatalf("expected at least 2 list items, got %d", len(list.Children))
	}
	for _, item := range list.Children {
		if item.Type != NodeListItem {
			t.Errorf("expected NodeListItem, got %d", item.Type)
		}
	}
}

func TestParseBlockquote(t *testing.T) {
	doc := Parse([]byte("> quote"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	bq := doc.Root.Children[0]
	if bq.Type != NodeBlockquote {
		t.Fatalf("expected NodeBlockquote, got %d", bq.Type)
	}
	text := collectText(bq)
	if text != "quote" {
		t.Fatalf("expected %q, got %q", "quote", text)
	}
}

func TestParseLink(t *testing.T) {
	doc := Parse([]byte("[text](https://example.com)"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	var link *Node
	for _, c := range p.Children {
		if c.Type == NodeLink {
			link = c
			break
		}
	}
	if link == nil {
		t.Fatal("expected NodeLink child")
	}
	if link.Attrs["href"] != "https://example.com" {
		t.Errorf("expected href %q, got %q", "https://example.com", link.Attrs["href"])
	}
	text := collectText(link)
	if text != "text" {
		t.Errorf("expected link text %q, got %q", "text", text)
	}
}

func TestParseImage(t *testing.T) {
	doc := Parse([]byte(`![alt text](image.png "A title")`))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	var img *Node
	for _, c := range p.Children {
		if c.Type == NodeImage {
			img = c
			break
		}
	}
	if img == nil {
		t.Fatal("expected NodeImage child")
	}
	if img.Attrs["src"] != "image.png" {
		t.Errorf("expected src %q, got %q", "image.png", img.Attrs["src"])
	}
	if img.Attrs["alt"] != "alt text" {
		t.Errorf("expected alt %q, got %q", "alt text", img.Attrs["alt"])
	}
	if img.Attrs["title"] != "A title" {
		t.Errorf("expected title %q, got %q", "A title", img.Attrs["title"])
	}
}

func TestParseTable(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |"
	doc := Parse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	table := doc.Root.Children[0]
	if table.Type != NodeTable {
		t.Fatalf("expected NodeTable, got %d", table.Type)
	}
	if len(table.Children) < 2 {
		t.Fatalf("expected at least 2 rows (header + data), got %d", len(table.Children))
	}
	for _, row := range table.Children {
		if row.Type != NodeTableRow {
			t.Errorf("expected NodeTableRow, got %d", row.Type)
		}
		if len(row.Children) < 2 {
			t.Errorf("expected at least 2 cells, got %d", len(row.Children))
		}
		for _, cell := range row.Children {
			if cell.Type != NodeTableCell {
				t.Errorf("expected NodeTableCell, got %d", cell.Type)
			}
		}
	}
}

func TestParseComplexDocument(t *testing.T) {
	src := `# Title

A paragraph with **bold** and *italic*.

- item one
- item two

> a quote

` + "```js\nconsole.log(1)\n```" + `

[link](https://example.com)

| H1 | H2 |
|----|-----|
| a  | b   |
`

	doc := Parse([]byte(src))
	if doc.Root.Type != NodeDocument {
		t.Fatal("expected NodeDocument root")
	}

	types := map[NodeType]bool{}
	for _, c := range doc.Root.Children {
		types[c.Type] = true
	}

	expected := []NodeType{
		NodeHeading,
		NodeParagraph,
		NodeList,
		NodeBlockquote,
		NodeCodeBlock,
		NodeTable,
	}
	for _, e := range expected {
		if !types[e] {
			t.Errorf("expected node type %d in complex document", e)
		}
	}
}

// collectText recursively collects all text content from a node tree.
func collectText(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Type == NodeText {
		return n.Literal
	}
	var s string
	for _, c := range n.Children {
		s += collectText(c)
	}
	return s
}
