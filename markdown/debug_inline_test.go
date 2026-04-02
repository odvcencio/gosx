package markdown

import (
	"fmt"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestDebugInlineNodeTypes(t *testing.T) {
	src := []byte(`A paragraph with **bold** and *italic* text and [link text](https://example.com) and ![alt](img.png) and ` + "`code`" + `.`)

	lang := grammars.MarkdownInlineLanguage()
	if lang == nil {
		t.Fatal("MarkdownInlineLanguage returned nil")
	}
	parser := gotreesitter.NewParser(lang)

	entry := grammars.DetectLanguageByName("markdown_inline")
	var tree *gotreesitter.Tree
	var err error
	if entry != nil && entry.TokenSourceFactory != nil {
		ts := entry.TokenSourceFactory(src, lang)
		tree, err = parser.ParseWithTokenSource(src, ts)
	} else {
		tree, err = parser.Parse(src)
	}
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Release()

	bt := gotreesitter.Bind(tree)

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
		if len(text) > 80 {
			text = text[:80] + "..."
		}
		fmt.Printf("%s%s: %q\n", indent, bt.NodeType(n), text)
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i), depth+1)
		}
	}
	walk(bt.RootNode(), 0)
}
