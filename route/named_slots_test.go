package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	islandprogram "m31labs.dev/gosx/island/program"
)

// TestStrictComponentAcceptsANamedSlot proves the basic named-slot shape: a
// Go caller supplies a value through ProgramRenderEnv.Slots, keyed by slot
// name ("Title", not "slotTitle"), and the callee's {slotTitle} expression
// hole places it — exactly the way {children} already places a Go-supplied
// children value (gosx#246, gosx#248), but under its own name, beside
// children rather than instead of it.
func TestStrictComponentAcceptsANamedSlot(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div><h1>{slotTitle}</h1><main>{children}</main></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Layout", ProgramRenderEnv{
		Slots: map[string]gosx.Node{"Title": gosx.Text("Dashboard")},
	}, gosx.Text("body content"))
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><h1>Dashboard</h1><main>body content</main></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestNamedSlotDeclaredButNotSuppliedRendersEmpty proves the "declared but
// not supplied" half of the slot contract: an unsupplied {slotTitle} stays
// an unresolved scope identifier, the same as an unsupplied {children} does
// today (see entryChildrenNode's doc comment) — it renders empty, not an
// error.
func TestNamedSlotDeclaredButNotSuppliedRendersEmpty(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div><h1>[{slotTitle}]</h1></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Layout", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><h1>[]</h1></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestNamedSlotSuppliedButNotDeclaredFailsClosed proves the other half: a
// Slots key naming a slot the render entry's body never places is a
// caller error, following validateStrictCalleeChildren's arity-rule
// precedent (ir/lower.go) — supplying content a callee cannot place fails
// closed instead of silently discarding it.
func TestNamedSlotSuppliedButNotDeclaredFailsClosed(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div>{children}</div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = RenderProgramComponent(prog, "Layout", ProgramRenderEnv{
		Slots: map[string]gosx.Node{"Title": gosx.Text("Dashboard")},
	})
	if err == nil {
		t.Fatal("RenderProgramComponent: want an error for an undeclared slot, got nil")
	}
	if !strings.Contains(err.Error(), "Title") || !strings.Contains(err.Error(), "slotTitle") {
		t.Fatalf("error = %q, want it to name the slot and its binding identifier", err.Error())
	}
}

// TestMultipleNamedSlotsAddressDistinctInjectionPoints proves the reason
// named slots exist at all: three separate injection points, each with its
// own content, in one render — the case one repeatable {children} hole
// cannot express (TestStrictComponentRendersChildrenTwice pins every
// repeat to the SAME content).
func TestMultipleNamedSlotsAddressDistinctInjectionPoints(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <html><head><title>{slotTitle}</title>{slotPreload}</head><body>{children}{slotManifest}</body></html>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Layout", ProgramRenderEnv{
		Slots: map[string]gosx.Node{
			"Title":    gosx.Text("Dashboard"),
			"Preload":  gosx.RawHTML(`<link rel="preload">`),
			"Manifest": gosx.RawHTML(`<script id="m"></script>`),
		},
	}, gosx.Text("page content"))
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<html><head><title>Dashboard</title><link rel="preload"></head><body>page content<script id="m"></script></body></html>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// namedSlotIslandLayoutProgram hand-builds an ir.Program with a strict
// "Layout" component declaring {children} and the framework-filled
// {slotPageHead} hole, and a call node that supplies content through a
// registerIsland() legacy-expression child. It bypasses gosx.Compile
// entirely (mirroring childrenProgramPair in strict_children_test.go): this
// test exercises the RENDERER's own sequencing (writeLocalComponent /
// writeLocalComponentWithChildren), which is independent of whether a
// strict .gsx source may reference an island tag directly — a strict
// component cannot declare or call one today (a pre-existing, orthogonal
// restriction this feature does not touch; see the CHANGELOG entry for
// this change).
func namedSlotIslandLayoutProgram() (prog *ir.Program, callNode *ir.Node, comp *ir.Component) {
	prog = &ir.Program{Package: "app"}

	// The call site: <Layout>{registerIsland()}</Layout>. registerIsland is
	// a plain legacy-expression call (route/exprlower.go supports
	// *ast.CallExpr for the file renderer's general expression evaluator),
	// bound to a Go closure the test wires through env.funcs — its only job
	// is to register an island into a real *island.Renderer as a side
	// effect, standing in for whatever real content (a compiled island
	// child, in a real page) triggers island registration while children
	// render.
	childID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "registerIsland()"})
	callID := prog.AddNode(ir.Node{Kind: ir.NodeComponent, Tag: "Layout", Children: []ir.NodeID{childID}})
	callNode = prog.NodeAt(callID)

	// Layout's body: BEFORE{children}AFTER{slotPageHead} — the end-of-body
	// manifest placement the design brief names, expressed as plain
	// element children in document order. Nothing here computes
	// slotPageHead's value; the renderer binds it.
	beforeID := prog.AddNode(ir.Node{Kind: ir.NodeText, Text: "BEFORE"})
	childrenHoleID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "children"})
	afterID := prog.AddNode(ir.Node{Kind: ir.NodeText, Text: "AFTER"})
	pageHeadHoleID := prog.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "slotPageHead"})
	rootID := prog.AddNode(ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "div",
		Children: []ir.NodeID{beforeID, childrenHoleID, afterID, pageHeadHoleID},
	})

	prog.Components = append(prog.Components, ir.Component{
		Name:            "Layout",
		Syntax:          ir.ComponentSyntaxStrict,
		Root:            rootID,
		AcceptsChildren: true,
		AcceptsSlots:    []string{"PageHead"},
	})
	comp = &prog.Components[0]
	return prog, callNode, comp
}

// TestFrameworkFilledSlotReflectsIslandsRegisteredByChildren is the direct
// proof for the coordinator's demanded case: a pure same-program
// .gsx-to-.gsx nested call (<Layout>{content}</Layout>, no Go glue between
// caller and callee) places an end-of-body island manifest that reflects
// every island its children registered, with the value computed by the
// RENDERER — never by a strict .gsx expression, which still cannot express
// "compute this after that sibling renders" and is not asked to here.
//
// The manifest value is asserted to change between "before children
// render" and "after children render, before Layout's own body walks" —
// the exact window writeLocalComponentWithChildren computes it in — so a
// regression that moved the computation earlier (losing the ordering
// guarantee) would be caught by content, not just by a passing render.
func TestFrameworkFilledSlotReflectsIslandsRegisteredByChildren(t *testing.T) {
	prog, callNode, comp := namedSlotIslandLayoutProgram()

	renderer := island.NewRenderer("test")
	renderer.SetBundle("test", "/gosx/runtime.wasm")
	renderer.SetRuntime("/gosx/runtime.wasm", "", 0)

	// Before any content renders, the manifest is empty — proving the
	// "before" snapshot really is different from the "after" one below,
	// not coincidentally identical regardless of ordering.
	before := gosx.RenderHTML(renderer.PageHeadWithNonce(""))
	if strings.Contains(before, "Counter") {
		t.Fatalf("manifest already names Counter before any island rendered: %q", before)
	}

	var registerCalled bool
	env := fileRenderEnv{
		funcs: map[string]any{
			"registerIsland": func() gosx.Node {
				registerCalled = true
				return renderer.RenderIslandFromProgram(islandprogram.CounterProgram(), map[string]any{"initial": 0})
			},
		},
		islandPageHead: func() gosx.Node { return renderer.PageHeadWithNonce("") },
	}

	r := newFileProgramRenderer(prog, fileRenderOptions{})
	var b strings.Builder
	r.writeLocalComponent(&b, comp, callNode, env)
	if r.err != nil {
		t.Fatalf("writeLocalComponent: %v", r.err)
	}
	html := b.String()

	if !registerCalled {
		t.Fatal("registerIsland() was never called; children did not render")
	}

	beforeIdx := strings.Index(html, "BEFORE")
	islandIdx := strings.Index(html, `data-gosx-island="Counter"`)
	afterIdx := strings.Index(html, "AFTER")
	manifestIdx := strings.Index(html, `id="gosx-manifest"`)
	if beforeIdx < 0 || islandIdx < 0 || afterIdx < 0 || manifestIdx < 0 {
		t.Fatalf("html missing an expected marker: %q", html)
	}
	if !(beforeIdx < islandIdx && islandIdx < afterIdx && afterIdx < manifestIdx) {
		t.Fatalf("html = %q, want BEFORE, then the island, then AFTER, then the manifest script, in that order", html)
	}

	// The manifest script placed at the END of the body must name the
	// island the children registered — proving slotPageHead's value was
	// computed AFTER children rendered, not before (the "before" snapshot
	// above proves the two would differ).
	manifestScript := html[manifestIdx:]
	if !strings.Contains(manifestScript, "Counter") {
		t.Fatalf("end-of-body manifest does not name the Counter island: %q", manifestScript)
	}
}
