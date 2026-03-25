package render

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx/ir"
)

func TestCompactIslandRendering(t *testing.T) {
	// Build a simple IR: <div class="counter"><span>hello</span><span>world</span></div>
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "div",
		Attrs:    []ir.Attr{{Kind: ir.AttrStatic, Name: "class", Value: "counter"}},
		Children: []ir.NodeID{1, 2},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "span",
		Children: []ir.NodeID{3},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "span",
		Children: []ir.NodeID{4},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "hello"})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "world"})

	prog.Components = append(prog.Components, ir.Component{
		Name: "Counter",
		Root: 0,
	})

	// Compact mode
	compact := RenderNode(prog, 0, Options{CompactIsland: true})

	// Should have NO whitespace between elements
	if strings.Contains(compact, "\n") {
		t.Fatalf("compact mode should have no newlines, got: %q", compact)
	}
	if strings.Contains(compact, "  ") {
		t.Fatalf("compact mode should have no indentation, got: %q", compact)
	}

	expected := `<div class="counter"><span>hello</span><span>world</span></div>`
	if compact != expected {
		t.Fatalf("expected %q, got %q", expected, compact)
	}
}

func TestCompactIslandOverridesIndent(t *testing.T) {
	// When both CompactIsland and Indent are set, CompactIsland wins.
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "div",
		Children: []ir.NodeID{1, 2},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "span",
		Children: []ir.NodeID{3},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "span",
		Children: []ir.NodeID{4},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "a"})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "b"})

	result := RenderNode(prog, 0, Options{Indent: "  ", CompactIsland: true})

	if strings.Contains(result, "\n") {
		t.Fatalf("compact mode should override indent, got: %q", result)
	}
	expected := `<div><span>a</span><span>b</span></div>`
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestIndentedRendering(t *testing.T) {
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "div",
		Children: []ir.NodeID{1},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "hello"})

	// Indented mode
	indented := RenderNode(prog, 0, Options{Indent: "  "})

	// Should be the same (single child inline is OK)
	if !strings.Contains(indented, "hello") {
		t.Fatal("missing text content")
	}
}

func TestRenderVoidElement(t *testing.T) {
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:  ir.NodeElement,
		Tag:   "img",
		Attrs: []ir.Attr{{Kind: ir.AttrStatic, Name: "src", Value: "test.png"}},
	})

	result := RenderNode(prog, 0, Options{})
	if !strings.Contains(result, "<img") {
		t.Fatal("missing img tag")
	}
	if strings.Contains(result, "</img>") {
		t.Fatal("void element should not have closing tag")
	}
}

func TestRenderFragment(t *testing.T) {
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:     ir.NodeFragment,
		Children: []ir.NodeID{1, 2},
	})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "a"})
	prog.Nodes = append(prog.Nodes, ir.Node{Kind: ir.NodeText, Text: "b"})

	result := RenderNode(prog, 0, Options{})
	if result != "ab" {
		t.Fatalf("expected 'ab', got %q", result)
	}
}

func TestRenderBoolAttr(t *testing.T) {
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind:  ir.NodeElement,
		Tag:   "input",
		Attrs: []ir.Attr{{Kind: ir.AttrBool, Name: "disabled"}},
	})

	result := RenderNode(prog, 0, Options{})
	if !strings.Contains(result, "disabled") {
		t.Fatal("missing boolean attribute")
	}
}

func TestRenderEscaping(t *testing.T) {
	prog := &ir.Program{}
	prog.Nodes = append(prog.Nodes, ir.Node{
		Kind: ir.NodeText,
		Text: "<script>alert('xss')</script>",
	})

	result := RenderNode(prog, 0, Options{})
	if strings.Contains(result, "<script>") {
		t.Fatal("text should be escaped")
	}
}
