package gosx

import (
	"strings"
	"testing"
)

// The gsx transpiler emits gosx.Expr(x) for every `{x}` interpolation. When x
// is a Node (a composed child or island, the standard embedding pattern) it
// must be inlined, not stringified into a struct dump and HTML-escaped.

func TestExprInlinesNodeChild(t *testing.T) {
	child := El("span", Attrs(Attr("class", "x")), Text("hi"))
	got := RenderHTML(El("div", Expr(child)))
	want := `<div><span class="x">hi</span></div>`
	if got != want {
		t.Fatalf("Expr(Node) = %q, want %q", got, want)
	}
	if strings.Contains(got, "{0 ") || strings.Contains(got, "[{class") {
		t.Fatalf("Expr(Node) produced a struct dump: %q", got)
	}
}

func TestExprInlinesNodeSlice(t *testing.T) {
	kids := []Node{El("li", Text("a")), El("li", Text("b"))}
	got := RenderHTML(El("ul", Expr(kids)))
	want := `<ul><li>a</li><li>b</li></ul>`
	if got != want {
		t.Fatalf("Expr([]Node) = %q, want %q", got, want)
	}
}

func TestExprStringifiesNonNode(t *testing.T) {
	if got := RenderHTML(Expr(42)); got != "42" {
		t.Fatalf("Expr(42) = %q, want %q", got, "42")
	}
	if got := RenderHTML(Expr("a<b")); got != "a&lt;b" {
		t.Fatalf("Expr string escaping = %q, want %q", got, "a&lt;b")
	}
}
