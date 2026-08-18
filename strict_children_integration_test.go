package gosx_test

import (
	"testing"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

// childrenPanelFixtureSource is WP0's acceptance shape: a same-file strict
// component that wraps arbitrary caller-authored .gsx markup. It uses no
// import, no shared directory, and no file boundary — children are a
// same-file gap that single-file pages have today.
const childrenPanelFixtureSource = `package app

type PanelProps struct {
	Title string
	Tone  string
}

component Panel(props: PanelProps) {
	return <section class={"panel tone-" + props.Tone}><h2 class="panel__title">{props.Title}</h2><div class="panel__body">{children}</div></section>
}

component Page(props: PanelProps) {
	return <Panel Title={props.Title} Tone={props.Tone}><p class="lead">wrapped</p><b>markup</b></Panel>
}
`

// panelPropsTwin mirrors the fixture's PanelProps struct for the render
// entry and for the generated-Go twin below.
type panelPropsTwin struct {
	Title string
	Tone  string
}

// panelTwin is the exact declaration emitTypedComponentCall and
// emitStrictComponent project for the fixture's Panel, transcribed by hand:
//
//	func Panel(props PanelProps, children ...gosx.Node) gosx.Node
//
// Writing it out is the point. If the projected signature ever stops taking
// children, this file stops compiling.
func panelTwin(props panelPropsTwin, children ...gosx.Node) gosx.Node {
	return gosx.El("section", gosx.Attrs(gosx.Attr("class", "panel tone-"+props.Tone)),
		gosx.El("h2", gosx.Attrs(gosx.Attr("class", "panel__title")), gosx.Expr(props.Title)),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "panel__body")), gosx.Expr(children)),
	)
}

// pageTwin is the projection of the fixture's Page: a typed props literal
// followed by the child arguments emitTypedComponentCall appends.
func pageTwin(props panelPropsTwin, children ...gosx.Node) gosx.Node {
	return panelTwin(panelPropsTwin{Title: props.Title, Tone: props.Tone},
		gosx.El("p", gosx.Attrs(gosx.Attr("class", "lead")), gosx.Text("wrapped")),
		gosx.El("b", gosx.Text("markup")),
	)
}

// TestStrictChildrenFixtureCompilesChecksAndRendersAtParity is WP0's exit
// gate, in three steps.
//
//  1. Compiles: the lowerer admits the {children} hole and the call site.
//  2. Checks: the real Go-compiler-backed pipeline accepts the projection,
//     which proves the always-variadic signature type-checks in a real
//     module.
//  3. Renders at parity: the file renderer emits byte-identical output to
//     the generated-Go twin. Parity between the two renderers is the
//     invariant the whole strict design rests on, so children must not be
//     the one shape where they diverge.
func TestStrictChildrenFixtureCompilesChecksAndRendersAtParity(t *testing.T) {
	prog, err := gosx.Compile([]byte(childrenPanelFixtureSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	checkFixtureInRealModule(t, "children", "panel.gsx", childrenPanelFixtureSource)

	props := panelPropsTwin{Title: "Standings", Tone: "home"}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{Props: props})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := gosx.RenderHTML(pageTwin(props))
	if html != want {
		t.Fatalf("file render = %q, generated-Go render = %q", html, want)
	}
}

// TestStrictChildrenEmptyCallRendersAtParity proves the variadic parameter's
// zero-argument case. A component that places {children} and receives none
// emits nothing there, in both renderers, so the always-variadic decision
// costs no existing call site.
func TestStrictChildrenEmptyCallRendersAtParity(t *testing.T) {
	const source = `package app

type PanelProps struct {
	Title string
	Tone  string
}

component Panel(props: PanelProps) {
	return <section class={"panel tone-" + props.Tone}><h2 class="panel__title">{props.Title}</h2><div class="panel__body">{children}</div></section>
}

component Page(props: PanelProps) {
	return <Panel Title={props.Title} Tone={props.Tone}></Panel>
}
`
	prog, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	props := panelPropsTwin{Title: "Standings", Tone: "home"}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{Props: props})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := gosx.RenderHTML(panelTwin(props))
	if html != want {
		t.Fatalf("file render = %q, generated-Go render = %q", html, want)
	}
}
