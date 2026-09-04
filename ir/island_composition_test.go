package ir

import (
	"strings"
	"testing"
)

func TestLowerIslandPureViewCompositionBoundaryErrors(t *testing.T) {
	tests := map[string]struct {
		mutate func(*Program)
		want   string
	}{
		"nested island": {
			mutate: func(prog *Program) { prog.Components[1].IsIsland = true },
			want:   "nested island <Badge>",
		},
		"legacy callee": {
			mutate: func(prog *Program) { prog.Components[1].Syntax = ComponentSyntaxLegacy },
			want:   "uses legacy component syntax",
		},
		"callee state": {
			mutate: func(prog *Program) {
				prog.Components[1].Scope = &ComponentScope{Signals: []SignalInfo{{Name: "local"}}}
			},
			want: "owns signals, computed values, handlers, or effects",
		},
		"non scalar prop": {
			mutate: func(prog *Program) { prog.Components[1].PropsFields["Label"] = "LabelModel" },
			want:   "reads non-scalar prop Label",
		},
		"handler valued prop": {
			mutate: func(prog *Program) {
				prog.Nodes[1].Attrs[0] = Attr{Kind: AttrExpr, Name: "OnTap", Expr: "increment", IsEvent: true}
			},
			want: "passes handler-valued prop \"OnTap\"",
		},
		"handler reference as ordinary prop": {
			mutate: func(prog *Program) {
				prog.Components[0].Scope = &ComponentScope{
					Handlers: []HandlerInfo{{Name: "increment"}},
					Locals:   map[string]string{"increment": "handler"},
				}
				prog.Nodes[1].Attrs[0] = Attr{Kind: AttrExpr, Name: "Callback", Expr: "increment"}
			},
			want: "passes handler-valued prop \"Callback\"",
		},
		"spread call": {
			mutate: func(prog *Program) {
				prog.Nodes[1].Attrs = []Attr{{Kind: AttrSpread, Expr: "props"}}
			},
			want: "uses a spread",
		},
		"callee handler": {
			mutate: func(prog *Program) {
				prog.Nodes[2].Attrs = []Attr{{Kind: AttrExpr, Name: "onClick", Expr: "tap", IsEvent: true}}
			},
			want: "contains handler attribute \"onClick\"",
		},
		"callee spread": {
			mutate: func(prog *Program) {
				prog.Nodes[2].Attrs = []Attr{{Kind: AttrSpread, Expr: "props.Extra"}}
			},
			want: "contains a spread attribute",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			prog := basicComposedIslandProgram()
			tc.mutate(prog)
			_, err := LowerIsland(prog, 0)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LowerIsland error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateIslandPureViewCompositionBoundaryErrors(t *testing.T) {
	prog := basicComposedIslandProgram()
	prog.Components[1].IsIsland = true
	diags := Validate(prog)
	if !diagnosticsContain(diags, "nested island <Badge>") {
		t.Fatalf("Validate diagnostics = %#v, want nested-island boundary", diags)
	}
}

func TestLowerIslandRejectsImportedComposition(t *testing.T) {
	prog := basicComposedIslandProgram()
	prog.Nodes[1].Tag = "ui.Badge"
	prog.Components = prog.Components[:1]
	_, err := LowerIsland(prog, 0)
	if err == nil || !strings.Contains(err.Error(), "imported component <ui.Badge>") {
		t.Fatalf("LowerIsland error = %v, want imported-component boundary", err)
	}
}

func TestLowerIslandRejectsMissingScalarProp(t *testing.T) {
	prog := basicComposedIslandProgram()
	prog.Nodes[1].Attrs = nil
	_, err := LowerIsland(prog, 0)
	if err == nil || !strings.Contains(err.Error(), "requires scalar prop Label") {
		t.Fatalf("LowerIsland error = %v, want missing-prop boundary", err)
	}
}

func TestLowerIslandRejectsCompositionCycle(t *testing.T) {
	prog := &Program{
		Nodes: []Node{
			{Kind: NodeComponent, Tag: "Child"},
			{Kind: NodeComponent, Tag: "Root"},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "Child", Syntax: ComponentSyntaxStrict, Root: 1},
		},
	}
	_, err := LowerIsland(prog, 0)
	if err == nil || !strings.Contains(err.Error(), "Root -> Child -> Root") {
		t.Fatalf("LowerIsland error = %v, want explicit cycle path", err)
	}
}

func TestLowerIslandRejectsCompositionDepthOverflow(t *testing.T) {
	const componentCount = maxIslandCompositionDepth + 1
	prog := &Program{}
	for i := 0; i < componentCount; i++ {
		name := "C" + decimal(i)
		node := Node{Kind: NodeElement, Tag: "span"}
		if i+1 < componentCount {
			node = Node{Kind: NodeComponent, Tag: "C" + decimal(i+1)}
		}
		prog.Nodes = append(prog.Nodes, node)
		prog.Components = append(prog.Components, Component{
			Name:     name,
			Syntax:   ComponentSyntaxStrict,
			Root:     NodeID(i),
			IsIsland: i == 0,
		})
	}
	_, err := LowerIsland(prog, 0)
	if err == nil || !strings.Contains(err.Error(), "32-component depth limit") {
		t.Fatalf("LowerIsland error = %v, want depth boundary", err)
	}
}

func TestLowerIslandRejectsExpandedNodeOverflow(t *testing.T) {
	const callCount = 65535
	prog := &Program{
		Nodes: []Node{
			{Kind: NodeFragment, Children: make([]NodeID, 0, callCount)},
			{Kind: NodeElement, Tag: "span", IsStatic: true},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "Leaf", Syntax: ComponentSyntaxStrict, Root: 1},
		},
	}
	for i := 0; i < callCount; i++ {
		id := prog.AddNode(Node{Kind: NodeComponent, Tag: "Leaf"})
		prog.Nodes[0].Children = append(prog.Nodes[0].Children, id)
	}
	_, err := LowerIsland(prog, 0)
	if err == nil || !strings.Contains(err.Error(), "65,535 expanded-node limit") {
		t.Fatalf("LowerIsland error = %v, want expanded-node boundary", err)
	}
}

func TestLowerIslandAllowsOmittedProjectedChildrenAndSlots(t *testing.T) {
	prog := basicComposedIslandProgram()
	prog.Nodes[2] = Node{Kind: NodeElement, Tag: "article", Children: []NodeID{3, 4}}
	prog.Nodes[3] = Node{Kind: NodeExpr, Text: "children"}
	prog.Nodes = append(prog.Nodes, Node{Kind: NodeExpr, Text: "slotTitle"})
	prog.Components[1].AcceptsChildren = true
	prog.Components[1].AcceptsSlots = []string{"Title"}
	prog.Components[1].PropsFields = nil
	prog.Nodes[1].Attrs = nil

	lowered, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("LowerIsland: %v", err)
	}
	if len(lowered.Nodes) != 2 || len(lowered.Nodes[1].Children) != 0 {
		t.Fatalf("lowered nodes = %#v, want empty article projection", lowered.Nodes)
	}
}

func TestLowerIslandAllowsFiniteNestedInvocationThroughChildren(t *testing.T) {
	prog := &Program{
		Nodes: []Node{
			{Kind: NodeComponent, Tag: "Wrapper", Children: []NodeID{1}},
			{Kind: NodeComponent, Tag: "Wrapper", Children: []NodeID{2}},
			{Kind: NodeElement, Tag: "span", IsStatic: true},
			{Kind: NodeElement, Tag: "div", Children: []NodeID{4}},
			{Kind: NodeExpr, Text: "children"},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "Wrapper", Syntax: ComponentSyntaxStrict, Root: 3, AcceptsChildren: true},
		},
	}

	lowered, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("LowerIsland: %v", err)
	}
	if len(lowered.Nodes) != 3 {
		t.Fatalf("lowered nodes = %#v, want two div invocations and one span", lowered.Nodes)
	}
	outer := lowered.Nodes[lowered.Root]
	inner := lowered.Nodes[outer.Children[0]]
	if outer.Tag != "div" || inner.Tag != "div" || lowered.Nodes[inner.Children[0]].Tag != "span" {
		t.Fatalf("lowered nested projection = %#v", lowered.Nodes)
	}
}

func TestLowerIslandRejectsRootCallSiteChildrenAndSlots(t *testing.T) {
	for _, mutate := range []func(*Component){
		func(comp *Component) { comp.AcceptsChildren = true },
		func(comp *Component) { comp.AcceptsSlots = []string{"Title"} },
	} {
		prog := basicComposedIslandProgram()
		mutate(&prog.Components[0])
		_, err := LowerIsland(prog, 0)
		if err == nil || !strings.Contains(err.Error(), "root island call-site content") {
			t.Fatalf("LowerIsland error = %v, want root projection boundary", err)
		}
	}
}

func TestIslandProjectionExpression(t *testing.T) {
	tests := map[string]struct {
		name string
		ok   bool
	}{
		"children":       {ok: true},
		"((children))":   {ok: true},
		"slotTitle":      {name: "Title", ok: true},
		"(slotTrailing)": {name: "Trailing", ok: true},
		"slotlower":      {},
		"props.children": {},
		"children + 1":   {},
		"(children":      {},
	}
	for source, want := range tests {
		name, ok := islandProjectionExpression(source)
		if name != want.name || ok != want.ok {
			t.Errorf("islandProjectionExpression(%q) = (%q, %v), want (%q, %v)", source, name, ok, want.name, want.ok)
		}
	}
}

func basicComposedIslandProgram() *Program {
	return &Program{
		Nodes: []Node{
			{Kind: NodeElement, Tag: "main", Children: []NodeID{1}},
			{Kind: NodeComponent, Tag: "Badge", Attrs: []Attr{{Kind: AttrExpr, Name: "Label", Expr: "props.Label"}}},
			{Kind: NodeElement, Tag: "span", Children: []NodeID{3}},
			{Kind: NodeExpr, Text: "props.Label"},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "Badge", Syntax: ComponentSyntaxStrict, Root: 2, PropsFields: map[string]string{"Label": "string"}},
		},
	}
}

func diagnosticsContain(diags []Diagnostic, want string) bool {
	for _, diag := range diags {
		if strings.Contains(diag.Message, want) {
			return true
		}
	}
	return false
}

func decimal(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
