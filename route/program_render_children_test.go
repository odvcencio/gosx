package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestRenderProgramComponentAcceptsEntryChildren proves the render-entry half
// of gosx#226's remaining gap: a Go caller can place one or more Go-computed
// gosx.Node values wherever a .gsx-authored render entry writes {children},
// the same hole a nested <Component>...</Component> call fills, without
// hand-building the surrounding chrome with gosx.El.
func TestRenderProgramComponentAcceptsEntryChildren(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Card() {
	return <div class="card">{children}</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Card", ProgramRenderEnv{},
		gosx.El("h3", gosx.Text("Header")),
		gosx.RawHTML("<span>island</span>"),
	)
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div class="card"><h3>Header</h3><span>island</span></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestRenderProgramComponentNoChildrenStaysByteIdentical pins that adding the
// children ...gosx.Node parameter changed nothing for a call that supplies
// none: an unresolved "children" identifier still fails soft to empty, the
// same as every call before gosx#246 gave RenderProgramComponent a children
// parameter to omit.
func TestRenderProgramComponentNoChildrenStaysByteIdentical(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Card() {
	return <div class="card">{children}</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Card", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if want := `<div class="card"></div>`; html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestRenderProgramComponentNodeReturnsAComposableNode proves
// RenderProgramComponentNode's result composes into a further gosx.El(...)
// tree with no double escaping and no double rendering: the Node it returns
// wraps the rendered HTML exactly once, via gosx.RawHTML, so splicing it into
// a parent element reproduces the same bytes RenderProgramComponent's string
// plus a hand-written gosx.RawHTML would have produced.
func TestRenderProgramComponentNodeReturnsAComposableNode(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Greeting() {
	return <p>Hello, {children}!</p>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	node, err := RenderProgramComponentNode(prog, "Greeting", ProgramRenderEnv{}, gosx.Text(`<world>`))
	if err != nil {
		t.Fatalf("RenderProgramComponentNode: %v", err)
	}
	composed := gosx.El("section", node)
	want := `<section><p>Hello, &lt;world&gt;!</p></section>`
	if got := gosx.RenderHTML(composed); got != want {
		t.Fatalf("composed html = %q, want %q", got, want)
	}
}

// TestRenderProgramComponentNodeReturnsZeroNodeOnError proves the error
// contract matches RenderProgramComponent's own: no partial HTML, and here,
// no partial Node either.
func TestRenderProgramComponentNodeReturnsZeroNodeOnError(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Card() {
	return <div class="card">{children}</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	node, err := RenderProgramComponentNode(prog, "DoesNotExist", ProgramRenderEnv{})
	if err == nil {
		t.Fatal("expected an error for an unknown component")
	}
	if !node.IsZero() {
		t.Fatalf("node = %#v, want the zero Node on error", node)
	}
}

// TestRenderProgramComponentChildrenAgainstALegacyEntryFailsClosed proves
// EntryChildren's legacy guard: a legacy render entry has no {children} hole
// for writeLocalComponent's binding to reach, so a caller-supplied child node
// against one fails closed with a clear error instead of silently dropping
// the caller's markup.
func TestRenderProgramComponentChildrenAgainstALegacyEntryFailsClosed(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
func Legacy() Node {
	return <div>legacy</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = RenderProgramComponent(prog, "Legacy", ProgramRenderEnv{}, gosx.Text("dropped?"))
	if err == nil {
		t.Fatal("expected an error for children against a legacy render entry")
	}
	if !strings.Contains(err.Error(), "not a strict component") {
		t.Fatalf("err = %v, want it to name the legacy-entry refusal", err)
	}
}

// TestRenderProgramComponentEntryChildrenNeverOverwriteAProvedPropsField is
// the entry-render-path sibling of TestChildrenNeverOverwriteAProvedPropsField:
// the render entry proves Props through strictSpreadProps directly against
// the caller's struct, never through a props map EntryChildren could write
// into, so a declared `Children` props field and Go-supplied entry children
// coexist exactly as they do at a nested call site.
func TestRenderProgramComponentEntryChildrenNeverOverwriteAProvedPropsField(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type CardProps struct {
	Children string
}
component Card(props: CardProps) {
	return <section><b>{props.Children}</b><div>{children}</div></section>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Card", ProgramRenderEnv{
		Props: struct{ Children string }{Children: "proved string"},
	}, gosx.El("i", gosx.Text("caller")))
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section><b>proved string</b><div><i>caller</i></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}
