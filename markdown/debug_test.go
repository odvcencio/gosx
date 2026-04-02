package markdown

import (
	"fmt"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestDebugNodeTypes(t *testing.T) {
	src := []byte(`# Heading 1

A paragraph with **bold** and *italic* text.

- item 1
- item 2

> blockquote

` + "```go\nfmt.Println(\"hello\")\n```" + `

[link text](https://example.com)

![alt text](image.png)

---

| A | B |
|---|---|
| 1 | 2 |

` + "`inline code`" + `
`)

	bt, err := grammars.ParseFile("doc.md", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer bt.Release()

	var walk func(n *gotreesitter.Node, depth int)
	walk = func(n *gotreesitter.Node, depth int) {
		if n == nil {
			return
		}
		indent := ""
		for i := 0; i < depth; i++ {
			indent += "  "
		}
		text := bt.NodeText(n)
		if len(text) > 60 {
			text = text[:60] + "..."
		}
		fmt.Printf("%s%s: %q\n", indent, bt.NodeType(n), text)
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), depth+1)
		}
	}
	walk(bt.RootNode(), 0)
}
