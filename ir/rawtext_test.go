package ir_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	. "m31labs.dev/gosx/ir"
)

func lowerSource(t *testing.T, src string) *Program {
	t.Helper()
	tree, lang, err := gosx.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	prog, err := Lower(tree.RootNode(), []byte(src), lang)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	return prog
}

// findNode returns the first node matching pred.
func findNode(prog *Program, pred func(Node) bool) (Node, bool) {
	for _, n := range prog.Nodes {
		if pred(n) {
			return n, true
		}
	}
	return Node{}, false
}

// TestLowerRawTextElementProducesRawHTML pins the IR half of raw-text support.
// Without an explicit case, jsx_raw_text_element fell through to the default
// branch and became an expression hole, which breaks island compilation.
func TestLowerRawTextElementProducesRawHTML(t *testing.T) {
	t.Parallel()

	js := `if (a < b) { go({x: 1}); }`
	prog := lowerSource(t, "package main\n\nfunc Page() Node {\n\treturn <div><script>"+js+"</script></div>\n}\n")

	script, ok := findNode(prog, func(n Node) bool { return n.Kind == NodeElement && n.Tag == "script" })
	if !ok {
		t.Fatal("no <script> element in lowered IR")
	}
	if len(script.Children) != 1 {
		t.Fatalf("want 1 script child, got %d", len(script.Children))
	}

	body := prog.Nodes[script.Children[0]]
	if body.Kind != NodeRawHTML {
		t.Errorf("script body kind = %v, want NodeRawHTML (escaping breaks && and <)", body.Kind)
	}
	if body.Text != js {
		t.Errorf("script body altered\n want: %q\n  got: %q", js, body.Text)
	}
	if strings.Contains(body.Text, "</script>") {
		t.Error("script body still carries its closing tag")
	}
}

// TestLowerRawTextElementKeepsAttributes checks attributes survive, since the
// opening tag lexes `<script` as a single token.
func TestLowerRawTextElementKeepsAttributes(t *testing.T) {
	t.Parallel()

	prog := lowerSource(t, "package main\n\nfunc Page() Node {\n\treturn <div><script src=\"/a.js\" defer></script></div>\n}\n")

	script, ok := findNode(prog, func(n Node) bool { return n.Kind == NodeElement && n.Tag == "script" })
	if !ok {
		t.Fatal("no <script> element in lowered IR")
	}
	if len(script.Attrs) == 0 {
		t.Fatal("script element lost its attributes")
	}
}

// TestLowerStyleElement covers the other raw-text tag.
func TestLowerStyleElement(t *testing.T) {
	t.Parallel()

	css := `.a > .b { color: red; }`
	prog := lowerSource(t, "package main\n\nfunc Page() Node {\n\treturn <div><style>"+css+"</style></div>\n}\n")

	style, ok := findNode(prog, func(n Node) bool { return n.Kind == NodeElement && n.Tag == "style" })
	if !ok {
		t.Fatal("no <style> element in lowered IR")
	}
	if len(style.Children) != 1 {
		t.Fatalf("want 1 style child, got %d", len(style.Children))
	}
	if body := prog.Nodes[style.Children[0]]; body.Text != css {
		t.Errorf("style body altered\n want: %q\n  got: %q", css, body.Text)
	}
}
