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
		`Layout(gosx.El("div", gosx.Attrs(gosx.Attr("slot", "Title")), gosx.Text("Standings")), gosx.El("p", gosx.Text("content")))`,
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
		`Layout(LayoutProps{ID: "x"}, gosx.El("div", gosx.Attrs(gosx.Attr("slot", "Title")), gosx.Text("Standings")), gosx.El("p", gosx.Text("content")))`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTranspileComponentCallFillsUnsuppliedSlotWithZeroNode proves the
// "declared but not supplied" half at the transpile level: a call that
// supplies no slot at all still produces a call with a zero gosx.Node{}
// in the slot's parameter position, so the projection keeps compiling —
// the render-time equivalent of an unresolved {slotTitle} rendering
// empty (route/fileprogram.go).
func TestTranspileComponentCallFillsUnsuppliedSlotWithZeroNode(t *testing.T) {
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
	want := `Layout(gosx.Node{}, gosx.El("p", gosx.Text("content")))`
	if !strings.Contains(out, want) {
		t.Fatalf("missing %q in:\n%s", want, out)
	}
}
