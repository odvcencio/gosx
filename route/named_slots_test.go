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

// TestCallerSideSlotAttributeFillsANamedSlot is the caller-side counterpart
// to TestStrictComponentAcceptsANamedSlot: a static slot="Name" attribute
// on a direct child at a nested .gsx-to-.gsx call site — no Go glue
// anywhere in the path — partitions that child out of the default children
// group and into the named slot it names (gosx#249's caller-side supply,
// ir/lower.go's partitionCallSlots). The whole tagged element becomes the
// slot's value, matching the web-platform slot="" convention: the caller
// projects the element itself, not just its inner text.
func TestCallerSideSlotAttributeFillsANamedSlot(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div><h1>{slotTitle}</h1><main>{children}</main></div>
}
component Page() {
	return <Layout><div slot="Title">Standings</div><p>page content</p></Layout>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><h1><div>Standings</div></h1><main><p>page content</p></main></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestCallerSideSlotOrderIndependent proves the slot-tagged child binds by
// NAME, not by its position among its siblings: placing it after an
// ordinary child must produce the identical result placing it before one
// does (TestCallerSideSlotAttributeFillsANamedSlot) — a position-based
// implementation would swap which child fills the slot and which fills
// children.
func TestCallerSideSlotOrderIndependent(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div><h1>{slotTitle}</h1><main>{children}</main></div>
}
component Page() {
	return <Layout><p>page content</p><div slot="Title">Standings</div></Layout>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><h1><div>Standings</div></h1><main><p>page content</p></main></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestCallerSideMultipleSlotsAndChildrenCoexist proves several named
// slots and the default children group all resolve correctly from one
// call site, each holding only the content tagged for it.
func TestCallerSideMultipleSlotsAndChildrenCoexist(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div><h1>{slotTitle}</h1><nav>{slotNav}</nav><main>{children}</main></div>
}
component Page() {
	return <Layout><div slot="Title">Standings</div><i>middle</i><div slot="Nav">Links</div><b>end</b></Layout>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><h1><div>Standings</div></h1><nav><div>Links</div></nav><main><i>middle</i><b>end</b></main></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestCallerSideSlotMustBeAStaticStringLiteral proves the design brief's
// explicit restriction: a computed slot="{expr}" name fails to compile
// with a diagnostic naming the reason, rather than silently falling back
// to the default children group or evaluating the expression at an
// ordering-unsafe time.
func TestCallerSideSlotMustBeAStaticStringLiteral(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div>{slotTitle}{children}</div>
}
component Page(props: struct{ Name string }) {
	return <Layout>
		<div slot={props.Name}>x</div>
	</Layout>
}
`))
	if err == nil || !strings.Contains(err.Error(), "slot must be a static string literal") {
		t.Fatalf("Compile error = %v, want a static-literal diagnostic", err)
	}
}

// TestCallerSideSlotOnNonDirectDescendantFailsClosed proves a slot="Name"
// buried one level too deep — wrapped in a plain HTML element before it
// ever reaches the component call — is reported, not silently absorbed
// into the default children group: an author who mistypes the nesting
// deserves to hear about it.
func TestCallerSideSlotOnNonDirectDescendantFailsClosed(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div>{slotTitle}{children}</div>
}
component Page() {
	return <Layout>
		<div><span slot="Title">x</span></div>
	</Layout>
}
`))
	if err == nil || !strings.Contains(err.Error(), "only meaningful on a direct child of a component call") {
		t.Fatalf("Compile error = %v, want a not-a-direct-child diagnostic", err)
	}
}

// TestCallerSideSlotOnPlainHTMLElementFailsClosed is the same rule for a
// slot="Name" attribute with no enclosing component call anywhere: a
// plain HTML element's own direct child cannot address a slot, since
// nothing at that level ever reads a slots map.
func TestCallerSideSlotOnPlainHTMLElementFailsClosed(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
component Page() {
	return <div><span slot="Title">x</span></div>
}
`))
	if err == nil || !strings.Contains(err.Error(), "only meaningful on a direct child of a component call") {
		t.Fatalf("Compile error = %v, want a not-a-direct-child diagnostic", err)
	}
}

// TestCallerSideSlotSuppliedButNotDeclaredFailsClosed is the caller-side
// counterpart to TestNamedSlotSuppliedButNotDeclaredFailsClosed: a
// slot="Name" the callee's body never places is a caller error at COMPILE
// time here (ir.Lower has full static visibility into both sides of a
// same-program call), rather than the Go-entry path's runtime check.
func TestCallerSideSlotSuppliedButNotDeclaredFailsClosed(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div>{children}</div>
}
component Page() {
	return <Layout>
		<div slot="Title">x</div>
	</Layout>
}
`))
	if err == nil || !strings.Contains(err.Error(), `declares no slot named "Title"`) {
		t.Fatalf("Compile error = %v, want a declares-no-slot diagnostic", err)
	}
}

// TestCallerSideSlotDeclaredButNotSuppliedRendersEmpty is the caller-side
// counterpart to TestNamedSlotDeclaredButNotSuppliedRendersEmpty: a slot
// the callee declares but no call-site child tags stays an unresolved
// scope identifier and renders empty, the same as an unsupplied
// {children} does.
func TestCallerSideSlotDeclaredButNotSuppliedRendersEmpty(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Layout() {
	return <div><h1>[{slotTitle}]</h1><main>{children}</main></div>
}
component Page() {
	return <Layout><p>content</p></Layout>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><h1>[]</h1><main><p>content</p></main></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestExistingChildrenOnlyCallsStayByteIdentical proves the caller-side
// slot mechanism changes nothing for any call that never uses slot="Name"
// — the exact call shape every strict component call used before this
// change, still projected the same way (TestStrictComponentRendersChildrenTwice
// and the rest of strict_children_test.go already pin the byte-identical
// contract this test names for the record).
func TestExistingChildrenOnlyCallsStayByteIdentical(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Panel() {
	return <section>{children}</section>
}
component Page() {
	return <Panel><p>wrapped</p><b>markup</b></Panel>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section><p>wrapped</p><b>markup</b></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}
