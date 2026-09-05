package transpile

import (
	"strings"
	"testing"
)

// TestTranspileEmitComponentCallRoutesSlotByName is the regression test for
// a bug emitComponentCall (the propless-strict / untyped-legacy call
// shape) had before slot-aware child partitioning existed: every child —
// slot-tagged or not — sat in one positional list, so a slot="Name" child
// filled whichever slot parameter its DOCUMENT ORDER happened to line up
// with, not the one it was tagged for. This fixture places the slot-tagged
// child SECOND, after an ordinary child, specifically to catch that: a
// position-based projection would put the ordinary child in slotTitle's
// argument position instead.
func TestTranspileEmitComponentCallRoutesSlotByName(t *testing.T) {
	source := []byte(`package app

component Layout() {
	return <div>{slotTitle}{children}</div>
}

component Page() {
	return <Layout>
		<p>content</p>
		<div slot="Title">Standings</div>
	</Layout>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	for _, want := range []string{
		`func Layout(slotTitle gosx.Node, children ...gosx.Node) gosx.Node`,
		// The slot-tagged child's own projection must be the FIRST argument
		// (slotTitle's position), even though it is the SECOND child in
		// source order.
		`Layout(gosx.El("div", gosx.Text("Standings")), gosx.El("p", gosx.Text("content")))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTranspileTypedComponentCallRoutesSlotByName is
// TestTranspileEmitComponentCallRoutesSlotByName for emitTypedComponentCall
// — the call shape a strict component WITH a declared props type takes
// (emitGSXElement routes it through typedPropsType, a separate code path
// from emitComponentCall with its own, independently broken positional
// append before this fix).
func TestTranspileTypedComponentCallRoutesSlotByName(t *testing.T) {
	source := []byte(`package app

type LayoutProps struct {
	ID string
}

component Layout(props: LayoutProps) {
	return <div id={props.ID}>{slotTitle}{children}</div>
}

component Page() {
	return <Layout ID="x">
		<p>content</p>
		<div slot="Title">Standings</div>
	</Layout>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	for _, want := range []string{
		`func Layout(props LayoutProps, slotTitle gosx.Node, children ...gosx.Node) gosx.Node`,
		`Layout(LayoutProps{ID: "x"}, gosx.El("div", gosx.Text("Standings")), gosx.El("p", gosx.Text("content")))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTranspileComponentCallFillsUnsuppliedSlotWithEmptyNode proves the
// "declared but not supplied" half at the transpile level: a call that
// supplies no slot at all still produces an empty gosx.Node value in the
// slot's parameter position, so the projection keeps compiling —
// the render-time equivalent of an unresolved {slotTitle} rendering
// empty (route/fileprogram.go).
func TestTranspileComponentCallFillsUnsuppliedSlotWithEmptyNode(t *testing.T) {
	source := []byte(`package app

component Layout() {
	return <div>{slotTitle}{children}</div>
}

component Page() {
	return <Layout><p>content</p></Layout>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	want := `Layout(gosx.Fragment(), gosx.El("p", gosx.Text("content")))`
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}

func TestTranspileExpressionWrappedSlotChildRoutesByName(t *testing.T) {
	source := []byte(`package app

component Layout() {
	return <div>{slotTitle}{children}</div>
}

component Page() {
	return <Layout>{<h1 slot="Title">Title</h1>}<p>body</p></Layout>
}
`)
	out, err := Transpile(source, Options{SourceFile: "page.gsx"})
	if err != nil {
		t.Fatalf("Transpile: %v", err)
	}
	want := `Layout(gosx.El("h1", gosx.Text("Title")), gosx.El("p", gosx.Text("body")))`
	if !strings.Contains(out, want) {
		t.Fatalf("expression-wrapped slot child was not routed by name; want %q in:\n%s", want, out)
	}
	if strings.Contains(out, `Attr("slot"`) {
		t.Fatalf("slot routing attribute leaked into expression-wrapped generated Go:\n%s", out)
	}
}

func TestTranspileIgnoresWhitespaceAndCommentOnlySameFileLeafBody(t *testing.T) {
	for name, body := range map[string]string{
		"whitespace":  "\n\t\t\n",
		"commentOnly": "\n\t\t// staging note\n\t\t\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := []byte(`package app

component Badge() {
	return <span>badge</span>
}

component Page() {
	return <Badge>` + body + `</Badge>
}
`)
			out, err := Transpile(source, Options{SourceFile: "page.gsx"})
			if err != nil {
				t.Fatalf("Transpile: %v", err)
			}
			if want := `Badge()`; !strings.Contains(out, want) {
				t.Fatalf("formatting-only leaf body projected unexpected children; want %q in:\n%s", want, out)
			}
		})
	}
}

func TestTranspileRejectsEncodedWhitespaceAndEmptyFragmentSameFileLeafBody(t *testing.T) {
	for name, body := range map[string]string{
		"nbsp":          "&nbsp;",
		"encodedSpace":  "&#32;",
		"encodedTab":    "&#9;",
		"emptyFragment": "<></>",
	} {
		t.Run(name, func(t *testing.T) {
			source := []byte(`package app

component Badge() {
	return <span>badge</span>
}

component Page() {
	return <Badge>` + body + `</Badge>
}
`)
			_, err := Transpile(source, Options{SourceFile: "page.gsx"})
			if err == nil || !strings.Contains(err.Error(), "renders no children") {
				t.Fatalf("Transpile error = %v, want meaningful child rejection", err)
			}
		})
	}
}
