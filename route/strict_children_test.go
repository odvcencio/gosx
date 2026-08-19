package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// TestStrictComponentRendersCallerAuthoredChildren is the WP0 acceptance
// render: a strict component wraps arbitrary caller-authored .gsx markup and
// emits it in place.
func TestStrictComponentRendersCallerAuthoredChildren(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
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
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{},
		Props: struct {
			Title string
			Tone  string
		}{Title: "Standings", Tone: "home"},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section class="panel tone-home"><h2 class="panel__title">Standings</h2><div class="panel__body"><p class="lead">wrapped</p><b>markup</b></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestStrictComponentRendersChildrenTwice pins the documented semantics of
// two holes: each one emits the children again, matching gosx.Expr(children)
// in the Go projection.
func TestStrictComponentRendersChildrenTwice(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type PanelProps struct { Title string }
component Panel(props: PanelProps) {
	return <section><div class="a">{children}</div><div class="b">{children}</div></section>
}
component Page(props: PanelProps) {
	return <Panel Title={props.Title}><i>x</i></Panel>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Props: struct{ Title string }{Title: "t"},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section><div class="a"><i>x</i></div><div class="b"><i>x</i></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestStrictComponentChildrenAreEscapedByTheCaller proves the children reach
// the callee as already-rendered, already-escaped markup. Text authored at
// the call site is escaped by the caller's own renderer before the callee
// ever sees it, so the callee adds no second escape pass and strips none.
func TestStrictComponentChildrenAreEscapedByTheCaller(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type PanelProps struct { Title string }
component Panel(props: PanelProps) {
	return <section>{children}</section>
}
component Page(props: PanelProps) {
	return <Panel Title={props.Title}><p>{props.Title}</p></Panel>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Props: struct{ Title string }{Title: `<script>alert(1)</script>`},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("children were not escaped by the caller: %q", html)
	}
	want := `<section><p>&lt;script&gt;alert(1)&lt;/script&gt;</p></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// Row must carry this exact name: the strict boundary proves a loop source
// by its element struct's DECLARED name (requireStrictSliceValue), so the Go
// type the test supplies has to match the .gsx fixture's element struct.
type Row struct{ Label string }

type childrenEachPanelProps struct{ Rows []Row }

// TestStrictComponentRendersChildrenInsideAnEachBody proves the children
// binding survives into a loop scope: renderEach derives each item scope from
// the enclosing frame, so the binding the component frame set is still in the
// chain. Each iteration emits the children again, which is the same "one hole,
// one emission" rule two holes follow.
func TestStrictComponentRendersChildrenInsideAnEachBody(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type Row struct { Label string }
type PanelProps struct { Rows []Row }
type PageProps struct { Panel PanelProps }
component Panel(props: PanelProps) {
	return <ul><Each of={props.Rows} as="row"><li>{row.Label}{children}</li></Each></ul>
}
component Page(props: PageProps) {
	return <Panel {...props.Panel}><b>C</b></Panel>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Props: struct{ Panel childrenEachPanelProps }{
			Panel: childrenEachPanelProps{Rows: []Row{{Label: "a"}, {Label: "b"}}},
		},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<ul><li>a<b>C</b></li><li>b<b>C</b></li></ul>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestChildrenNeverOverwriteAProvedPropsField proves the boundary claim at
// run time: children are not a prop, so they must not replace one.
//
// A callee that declares and reads `Children string` has that field proved
// against its own schema. The children node is bound in the render scope
// beside props, so both are readable and neither shadows the other.
func TestChildrenNeverOverwriteAProvedPropsField(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type PanelProps struct {
	Children string
}
component Panel(props: PanelProps) {
	return <section><b>{props.Children}</b><div>{children}</div></section>
}
component Page(props: PanelProps) {
	return <Panel Children={props.Children}><i>caller</i></Panel>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Props: struct{ Children string }{Children: "proved string"},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section><b>proved string</b><div><i>caller</i></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestSpreadChildrenNeverOverwriteAProvedPropsField is the same claim for
// the single-spread call shape, which builds its props map through a
// different function (strictSpreadProps).
func TestSpreadChildrenNeverOverwriteAProvedPropsField(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type PanelProps struct {
	Children string
}
type PageProps struct {
	Panel PanelProps
}
component Panel(props: PanelProps) {
	return <section><b>{props.Children}</b><div>{children}</div></section>
}
component Page(props: PageProps) {
	return <Panel {...props.Panel}><i>caller</i></Panel>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	type panelProps struct{ Children string }
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Props: struct{ Panel panelProps }{Panel: panelProps{Children: "proved string"}},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section><b>proved string</b><div><i>caller</i></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// childrenProgramPair builds two deliberately mismatched programs for the
// cross-program regression test below.
//
// caller holds pad nodes so its children node IDs are large. callee is built
// so that the SAME ID means something else entirely, which is what makes the
// wrong-program bug observable rather than accidentally harmless.
func childrenProgramPair(calleeFillers int) (caller *ir.Program, callee *ir.Program, callNode *ir.Node, comp *ir.Component) {
	caller = &ir.Program{Package: "app"}
	caller.AddNode(ir.Node{Kind: ir.NodeText, Text: "caller pad 0"})
	caller.AddNode(ir.Node{Kind: ir.NodeText, Text: "caller pad 1"})
	caller.AddNode(ir.Node{Kind: ir.NodeText, Text: "caller pad 2"})
	childID := caller.AddNode(ir.Node{Kind: ir.NodeText, Text: "CALLER OWNS THIS"})
	callID := caller.AddNode(ir.Node{
		Kind:     ir.NodeComponent,
		Tag:      "Panel",
		Children: []ir.NodeID{childID},
	})
	callNode = caller.NodeAt(callID)

	callee = &ir.Program{Package: "ui"}
	for i := 0; i < calleeFillers; i++ {
		callee.AddNode(ir.Node{Kind: ir.NodeText, Text: "CALLEE INTERNAL"})
	}
	holeID := callee.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "children"})
	rootID := callee.AddNode(ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "section",
		Children: []ir.NodeID{holeID},
	})
	callee.Components = append(callee.Components, ir.Component{
		Name:            "Panel",
		Syntax:          ir.ComponentSyntaxStrict,
		Root:            rootID,
		AcceptsChildren: true,
	})
	comp = &callee.Components[0]
	return caller, callee, callNode, comp
}

// TestChildrenResolveAgainstTheCallersProgram is the regression test for the
// defect section 9.1.5 names. node.Children are ir.NodeID values, and a node
// ID is an index into ONE program's Nodes slice. At a call site whose node
// belongs to one program and whose callee body belongs to another, rendering
// the children on the callee's renderer indexes the wrong slice.
//
// The two programs are built with deliberately different node counts, so the
// bug cannot pass silently. Each sub-case first proves the WRONG path is
// observably wrong, then proves the split renders correctly.
func TestChildrenResolveAgainstTheCallersProgram(t *testing.T) {
	t.Run("callee program is larger: wrong path renders unrelated markup", func(t *testing.T) {
		caller, callee, callNode, comp := childrenProgramPair(6)
		parent := newFileProgramRenderer(caller, fileRenderOptions{})
		child := newFileProgramRenderer(callee, fileRenderOptions{})
		env := fileRenderEnv{}

		// The wrong path: hand the whole call node to the callee's renderer.
		var wrong strings.Builder
		child.writeLocalComponent(&wrong, comp, callNode, env)
		if strings.Contains(wrong.String(), "CALLER OWNS THIS") {
			t.Fatalf("the wrong-program path accidentally rendered the caller's markup: %q", wrong.String())
		}
		if !strings.Contains(wrong.String(), "CALLEE INTERNAL") {
			t.Fatalf("the wrong-program path did not index the callee's slice, so this test proves nothing: %q", wrong.String())
		}

		// The required path: render children on the renderer that owns the
		// call node, then hand the finished node across.
		var right strings.Builder
		childrenNode := gosx.RawHTML(parent.renderChildren(callNode.Children, env))
		child.writeLocalComponentWithChildren(&right, comp, callNode, env, childrenNode, nil)
		want := "<section>CALLER OWNS THIS</section>"
		if right.String() != want {
			t.Fatalf("html = %q, want %q", right.String(), want)
		}
	})

	t.Run("callee program is smaller: wrong path panics out of range", func(t *testing.T) {
		caller, callee, callNode, comp := childrenProgramPair(0)
		parent := newFileProgramRenderer(caller, fileRenderOptions{})
		child := newFileProgramRenderer(callee, fileRenderOptions{})
		env := fileRenderEnv{}

		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("the wrong-program path did not panic, so this test proves nothing")
				}
			}()
			var wrong strings.Builder
			child.writeLocalComponent(&wrong, comp, callNode, env)
		}()

		var right strings.Builder
		childrenNode := gosx.RawHTML(parent.renderChildren(callNode.Children, env))
		child.writeLocalComponentWithChildren(&right, comp, callNode, env, childrenNode, nil)
		want := "<section>CALLER OWNS THIS</section>"
		if right.String() != want {
			t.Fatalf("html = %q, want %q", right.String(), want)
		}
	})
}

// TestSameFileChildrenPathStaysByteIdentical proves the split changed no
// behavior for the only call kind that exists today: writeLocalComponent
// renders the children itself, on its own renderer, exactly as before.
func TestSameFileChildrenPathStaysByteIdentical(t *testing.T) {
	caller, _, callNode, _ := childrenProgramPair(0)
	comp := ir.Component{
		Name:            "Panel",
		Syntax:          ir.ComponentSyntaxStrict,
		AcceptsChildren: true,
	}
	holeID := caller.AddNode(ir.Node{Kind: ir.NodeExpr, Text: "children"})
	comp.Root = caller.AddNode(ir.Node{
		Kind:     ir.NodeElement,
		Tag:      "section",
		Children: []ir.NodeID{holeID},
	})
	caller.Components = append(caller.Components, comp)
	r := newFileProgramRenderer(caller, fileRenderOptions{})
	env := fileRenderEnv{}

	var single strings.Builder
	r.writeLocalComponent(&single, &caller.Components[0], callNode, env)

	var split strings.Builder
	childrenNode := gosx.RawHTML(r.renderChildren(callNode.Children, env))
	r.writeLocalComponentWithChildren(&split, &caller.Components[0], callNode, env, childrenNode, nil)

	if single.String() != split.String() {
		t.Fatalf("writeLocalComponent = %q, writeLocalComponentWithChildren = %q, want identical", single.String(), split.String())
	}
	if single.String() != "<section>CALLER OWNS THIS</section>" {
		t.Fatalf("html = %q, want <section>CALLER OWNS THIS</section>", single.String())
	}
}

// TestStrictBodyRendersATypedLegacyCalleesChildren is the render half of the
// gosx#240 interaction: the lowerer admits the call, and the file renderer
// executes it, because writeLocalComponent binds the children scope value for
// every same-file callee regardless of category.
func TestStrictBodyRendersATypedLegacyCalleesChildren(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type CardProps struct { Title string }
func Card(props CardProps) Node {
	return <section><h3>{props.Title}</h3><div>{children}</div></section>
}
component Page(props: CardProps) {
	return <Card Title={props.Title}><i>CALLER</i></Card>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Props: struct{ Title string }{Title: "T"},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<section><h3>T</h3><div><i>CALLER</i></div></section>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// TestTypedLegacyCalleeKeepsTheLegacyChildrenChannel pins the boundary of
// setStrictComponentChildren. It guards STRICT props maps only.
//
// For a LEGACY callee, props.Children IS the children, by a contract older
// than this feature that gosx#240 kept on purpose. componentProps injects the
// key for the named-attribute shape, so guarding the spread shape alone would
// make one legacy call shape disagree with the other. Both shapes are
// asserted here so a future guard cannot be widened onto this path by
// accident.
func TestTypedLegacyCalleeKeepsTheLegacyChildrenChannel(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type CardProps struct {
	Title    string
	Children string
}
func Card(props CardProps) Node {
	return <section><h3>{props.Title}</h3><b>{props.Children}</b></section>
}
func Page() Node {
	return <div><Card {...data.card}><i>SPREAD</i></Card><Card Title="N" Children="ignored"><i>NAMED</i></Card></div>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !componentByName(t, prog, "Card").PropsTyped {
		t.Fatal("Card.PropsTyped = false, want true")
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{
			"data": map[string]any{"card": map[string]any{"Title": "T", "Children": "declared string"}},
		},
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	want := `<div><section><h3>T</h3><b><i>SPREAD</i></b></section><section><h3>N</h3><b><i>NAMED</i></b></section></div>`
	if html != want {
		t.Fatalf("html = %q, want %q", html, want)
	}
}

// componentByName returns the named component of prog, or fails.
func componentByName(t *testing.T, prog *ir.Program, name string) ir.Component {
	t.Helper()
	for _, comp := range prog.Components {
		if comp.Name == name {
			return comp
		}
	}
	t.Fatalf("component %s not found in %#v", name, prog.Components)
	return ir.Component{}
}

// TestSetStrictComponentChildrenSkipsAProvedField covers the guard directly,
// at every strict props-map builder that routes through it — including
// gosx#240's typed-frame branch, whose own observable failure happens
// upstream (the enclosing legacy frame's map already carries the children
// node under Children, so the boundary proof refuses it before this write is
// reached). Testing the function rather than only that one path keeps the
// guard covered no matter which builder a future change sends here.
func TestSetStrictComponentChildrenSkipsAProvedField(t *testing.T) {
	children := gosx.RawHTML("<i>x</i>")

	proved := &ir.Component{
		Name:        "Panel",
		Syntax:      ir.ComponentSyntaxStrict,
		PropsFields: map[string]string{"Children": "string", "Title": "string"},
	}
	props := map[string]any{"Children": "proved string", "Title": "T"}
	setStrictComponentChildren(proved, props, children)
	if props["Children"] != "proved string" {
		t.Fatalf("props[Children] = %#v, want the proved string untouched", props["Children"])
	}

	unproved := &ir.Component{
		Name:        "Panel",
		Syntax:      ir.ComponentSyntaxStrict,
		PropsFields: map[string]string{"Title": "string"},
	}
	props = map[string]any{"Title": "T"}
	setStrictComponentChildren(unproved, props, children)
	for _, key := range []string{"Children", "children"} {
		node, ok := props[key].(gosx.Node)
		if !ok || gosx.RenderHTML(node) != "<i>x</i>" {
			t.Fatalf("props[%s] = %#v, want the children node when the schema proves neither spelling", key, props[key])
		}
	}

	// A zero node writes nothing at all, whatever the schema says.
	props = map[string]any{"Title": "T"}
	setStrictComponentChildren(unproved, props, gosx.Node{})
	if _, ok := props["Children"]; ok {
		t.Fatalf("props = %#v, want no children keys for a zero node", props)
	}
}
