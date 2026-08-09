package gosx

import "testing"

func TestNodeAttributeElement(t *testing.T) {
	node := El("canvas", Attrs(Attr("data-gosx-surface-kind", "canvas2d")))
	got, ok := node.Attribute("data-gosx-surface-kind")
	if !ok || got != "canvas2d" {
		t.Fatalf("Attribute = %q, %v; want canvas2d, true", got, ok)
	}
}

func TestNodeAttributeNonElement(t *testing.T) {
	for _, node := range []Node{Text("x"), RawHTML("<b>x</b>"), Fragment(Text("x"))} {
		if got, ok := node.Attribute("id"); ok || got != "" {
			t.Fatalf("Attribute on non-element = %q, %v; want empty, false", got, ok)
		}
	}
}

func TestNodeAttributeMissingAndBlankName(t *testing.T) {
	node := El("div", Attrs(Attr("id", "x")))
	if got, ok := node.Attribute("class"); ok || got != "" {
		t.Fatalf("missing Attribute = %q, %v; want empty, false", got, ok)
	}
	if got, ok := node.Attribute(" "); ok || got != "" {
		t.Fatalf("blank Attribute = %q, %v; want empty, false", got, ok)
	}
}

func TestNodeAttributeDuplicateUsesLastValue(t *testing.T) {
	node := El("div", Attrs(Attr("data-mode", "old"), Attr("data-mode", "new")))
	got, ok := node.Attribute("data-mode")
	if !ok || got != "new" {
		t.Fatalf("duplicate Attribute = %q, %v; want new, true", got, ok)
	}
}
