package ir_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

func componentByName(t *testing.T, prog *ir.Program, name string) *ir.Component {
	t.Helper()
	for i := range prog.Components {
		if prog.Components[i].Name == name {
			return &prog.Components[i]
		}
	}
	t.Fatalf("component %s not found in %d components", name, len(prog.Components))
	return nil
}

// TestAcceptsChildrenRecordsTheChildrenHole proves the IR field's whole
// definition: a body that places {children} accepts children, and a body
// that does not, does not.
func TestAcceptsChildrenRecordsTheChildrenHole(t *testing.T) {
	prog, err := parse(t, []byte(`package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2><div>{children}</div></section>
}

component Leaf(props: PanelProps) {
	return <span>{props.Title}</span>
}
`))
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	if !componentByName(t, prog, "Panel").AcceptsChildren {
		t.Fatal("Panel.AcceptsChildren = false, want true")
	}
	if componentByName(t, prog, "Leaf").AcceptsChildren {
		t.Fatal("Leaf.AcceptsChildren = true, want false")
	}
}

// TestAcceptsChildrenIsIndependentOfDeclarationOrder proves the flag is
// computed for the whole file before any body is lowered. A caller declared
// BEFORE its callee must see the same answer as one declared after, or a
// legal call would fail on file layout alone — the order-dependence class
// collectStrictSchemas' two-pass split already fixed once for strictNames.
func TestAcceptsChildrenIsIndependentOfDeclarationOrder(t *testing.T) {
	const callerFirst = `package app

type PanelProps struct {
	Title string
}

component Page(props: PanelProps) {
	return <Panel Title={props.Title}><b>content</b></Panel>
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}
`
	const calleeFirst = `package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}

component Page(props: PanelProps) {
	return <Panel Title={props.Title}><b>content</b></Panel>
}
`
	for name, source := range map[string]string{"caller first": callerFirst, "callee first": calleeFirst} {
		t.Run(name, func(t *testing.T) {
			prog, err := parse(t, []byte(source))
			if err != nil {
				t.Fatalf("Lower: %v", err)
			}
			if !componentByName(t, prog, "Panel").AcceptsChildren {
				t.Fatal("Panel.AcceptsChildren = false, want true")
			}
		})
	}
}

// TestChildrenInAnAttributeDoesNotDeclareAcceptance proves the attribute
// walk exclusion in componentRendersChildren. An attribute value can itself
// be an expression container, and class={children} is refused precisely
// because rendered markup cannot go inside an attribute value. Counting it
// would declare a component to accept children it can never place.
func TestChildrenInAnAttributeDoesNotDeclareAcceptance(t *testing.T) {
	_, err := parse(t, []byte(`package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section class={children}>{props.Title}</section>
}
`))
	if err == nil {
		t.Fatal("Lower accepted children in an attribute value")
	}
	if !strings.Contains(err.Error(), "children renders as element content, not as an attribute value") {
		t.Fatalf("error = %v, want the attribute-position refusal", err)
	}
}

// TestStrictCallSitesAcceptChildrenForAnAcceptingCallee covers all three
// call shapes that reach a strict callee: a strict caller's named
// attributes, a strict caller's single spread, and a legacy caller's single
// spread.
func TestStrictCallSitesAcceptChildrenForAnAcceptingCallee(t *testing.T) {
	const preamble = `package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}
`
	tests := map[string]string{
		"strict named attributes": `
component Page(props: PanelProps) {
	return <Panel Title={props.Title}><b>x</b></Panel>
}
`,
		"strict single spread": `
component Page(props: PanelProps) {
	return <Panel {...props}><b>x</b></Panel>
}
`,
		"legacy single spread": `
func Page(data PageData) Node {
	return <Panel {...data.Panel}><b>x</b></Panel>
}

type PageData struct {
	Panel PanelProps
}
`,
	}
	for name, caller := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, []byte(preamble+caller)); err != nil {
				t.Fatalf("Lower rejected children at an accepting callee: %v", err)
			}
		})
	}
}

// TestStrictCallSitesRejectChildrenForANonAcceptingCallee proves the
// unexpected-children diagnostic. Silent truncation is the failure this
// replaces: without the rule, a caller writes child content and the callee
// drops it with no signal at all.
func TestStrictCallSitesRejectChildrenForANonAcceptingCallee(t *testing.T) {
	const preamble = `package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2></section>
}
`
	tests := map[string]string{
		"strict named attributes": `
component Page(props: PanelProps) {
	return <Panel Title={props.Title}><b>x</b></Panel>
}
`,
		"strict single spread": `
component Page(props: PanelProps) {
	return <Panel {...props}><b>x</b></Panel>
}
`,
		"legacy single spread": `
type PageData struct {
	Panel PanelProps
}

func Page(data PageData) Node {
	return <Panel {...data.Panel}><b>x</b></Panel>
}
`,
	}
	const want = "strict component Panel renders no children; remove the child content or render {children} in Panel's body"
	for name, caller := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, []byte(preamble+caller))
			if err == nil {
				t.Fatal("Lower accepted children at a callee that renders none")
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want it to contain %q", err, want)
			}
		})
	}
}

// TestChildrenRenderedTwiceIsAccepted pins the documented semantics: each
// {children} hole emits the children again, matching gosx.Expr(children) in
// the Go projection. It is a feature of the emission model, not a defect.
func TestChildrenRenderedTwiceIsAccepted(t *testing.T) {
	prog, err := parse(t, []byte(`package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><div>{children}</div><div>{children}</div></section>
}
`))
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	if !componentByName(t, prog, "Panel").AcceptsChildren {
		t.Fatal("Panel.AcceptsChildren = false, want true")
	}
}

// TestChildrenIsNotAProp proves the boundary claim. Children never enter the
// props schema, so no proof reads them and the explicit-supply rule does not
// cover them.
func TestChildrenIsNotAProp(t *testing.T) {
	prog, err := parse(t, []byte(`package app

type PanelProps struct {
	Title string
}

component Panel(props: PanelProps) {
	return <section><h2>{props.Title}</h2>{children}</section>
}
`))
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	panel := componentByName(t, prog, "Panel")
	for field := range panel.PropsFields {
		if strings.EqualFold(field, "children") {
			t.Fatalf("PropsFields carries %q; children must stay outside the props schema", field)
		}
	}
	for path := range panel.PropsPaths {
		if strings.HasPrefix(strings.ToLower(path), "children") {
			t.Fatalf("PropsPaths carries %q; children must stay outside the props schema", path)
		}
	}
	if _, ok := panel.PropsSlices["children"]; ok {
		t.Fatal("PropsSlices carries children; children must stay outside the props schema")
	}
}

// TestEachBindingNamedChildrenStaysReserved keeps W7 intact: the codebase
// reserved the name against an <Each> binding before the feature existed,
// and the reservation is what stops a loop binding from shadowing the one
// identifier a body uses to place its caller's markup.
func TestEachBindingNamedChildrenStaysReserved(t *testing.T) {
	_, err := parse(t, []byte(`package app

type Row struct {
	Label string
}

type PanelProps struct {
	Rows []Row
}

component Panel(props: PanelProps) {
	return <section><Each of={props.Rows} as="children"><b>x</b></Each></section>
}
`))
	if err == nil {
		t.Fatal("Lower accepted an <Each> binding named children")
	}
	if !strings.Contains(err.Error(), `strict <Each> binding "children" is reserved`) {
		t.Fatalf("error = %v, want the reserved-name refusal", err)
	}
}
