package markup_test

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/markup"
)

func ExampleRenderHTML() {
	n := markup.El("p", markup.Attrs(markup.Attr("class", "note")), markup.Text("Saved"))
	fmt.Println(markup.RenderHTML(n))
	// Output: <p class="note">Saved</p>
}

func TestRenderHTMLEscapesTextAndAttributes(t *testing.T) {
	n := markup.El("div", markup.Attrs(markup.Attr("title", `a"b<c`), markup.Attr("hidden", true)),
		markup.Text("<b>x</b>"), markup.RawHTML("<i>y</i>"), markup.El("br"))
	want := `<div title="a&#34;b&lt;c" hidden>&lt;b&gt;x&lt;/b&gt;<i>y</i><br /></div>`
	if got := markup.RenderHTML(n); got != want {
		t.Fatalf("RenderHTML = %q, want %q", got, want)
	}
}

func TestRenderHTMLDropsInvalidTagsAndZeroAttributes(t *testing.T) {
	if got := markup.RenderHTML(markup.El("x y", markup.Text("z"))); got != "" {
		t.Fatalf("invalid tag rendered %q, want empty", got)
	}
	n := markup.El("span", markup.Attrs(markup.Attribute{}, markup.Attr("id", "a")))
	if got, want := markup.RenderHTML(n), `<span id="a"></span>`; got != want {
		t.Fatalf("zero Attribute rendered %q, want %q", got, want)
	}
}

// RenderHTML expands the managed-form shorthand itself, so a form built in a
// browser build renders the same contract as one built on the server.
func TestRenderHTMLExpandsManagedFormShorthand(t *testing.T) {
	n := markup.El("form", markup.Attrs(markup.BoolAttr(markup.ManagedFormShorthandAttr)))
	want := fmt.Sprintf(`<form %s %s="idle" %s="form" %s="bootstrap" %s="native-form"></form>`,
		markup.ManagedFormAttr, markup.ManagedFormStateAttr, markup.EnhancementAttr,
		markup.EnhancementLayerAttr, markup.RuntimeFallbackAttr)
	if got := markup.RenderHTML(n); got != want {
		t.Fatalf("RenderHTML managed form = %q, want %q", got, want)
	}
	if got := markup.RenderAttrs(markup.ManagedFormAttrs(markup.ManagedFormOptions{Method: "POST"})); got != ` data-gosx-form data-gosx-form-mode="post" data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form"` {
		t.Fatalf("ManagedFormAttrs(POST) = %q", got)
	}
}

func TestPlainTextAndExpr(t *testing.T) {
	n := markup.Fragment(markup.Text("a"), markup.Expr(1), markup.RawHTML("<b>"), markup.Expr(markup.Text("c")),
		markup.Expr([]markup.Node{markup.Text("d")}))
	if got := markup.PlainText(n); got != "a1cd" {
		t.Fatalf("PlainText = %q, want a1cd", got)
	}
	if !(markup.Node{}).IsZero() || markup.Text("").IsZero() {
		t.Fatal("IsZero must hold only for the zero Node")
	}
}
