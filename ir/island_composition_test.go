package ir

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
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

func TestIslandSameFileComponentsShadowBuiltinsAndAliases(t *testing.T) {
	for _, tag := range []string{"If", "Each", "For", "Show", "When", "Link", "Image"} {
		t.Run(tag, func(t *testing.T) {
			prog := shadowedIslandComponentProgram(tag)
			lowered, err := LowerIsland(prog, 0)
			if err != nil {
				t.Fatalf("LowerIsland pure shadow: %v", err)
			}
			if len(lowered.Nodes) != 1 || lowered.Nodes[lowered.Root].Tag != "span" {
				t.Fatalf("lowered %s shadow = %#v, want composed span", tag, lowered.Nodes)
			}
			if diags := Validate(prog); len(diags) != 0 {
				t.Fatalf("Validate pure %s shadow = %#v", tag, diags)
			}

			prog = shadowedIslandComponentProgram(tag)
			prog.Components[1].Scope = &ComponentScope{Signals: []SignalInfo{{Name: "owned", InitExpr: "0"}}}
			_, err = LowerIsland(prog, 0)
			if err == nil || !strings.Contains(err.Error(), "owns signals, computed values, handlers, or effects") {
				t.Fatalf("LowerIsland impure %s shadow = %v, want callee-purity error", tag, err)
			}
			if diags := Validate(prog); !diagnosticsContain(diags, "owns signals, computed values, handlers, or effects") {
				t.Fatalf("Validate impure %s shadow = %#v, want callee-purity diagnostic", tag, diags)
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

func TestIslandCompositionPhysicalDepthThroughProjectedChildren(t *testing.T) {
	allowed := nestedWrapperProjectionProgram(maxIslandCompositionDepth - 1)
	if _, err := LowerIsland(allowed, 0); err != nil {
		t.Fatalf("LowerIsland at physical depth limit: %v", err)
	}
	if diags := Validate(allowed); diagnosticsContain(diags, "depth limit") {
		t.Fatalf("Validate at physical depth limit = %#v", diags)
	}

	overflow := nestedWrapperProjectionProgram(maxIslandCompositionDepth)
	_, err := LowerIsland(overflow, 0)
	if err == nil || !strings.Contains(err.Error(), "32-component depth limit") {
		t.Fatalf("LowerIsland projected depth overflow = %v", err)
	}
	if diags := Validate(overflow); !diagnosticsContain(diags, "32-component depth limit") {
		t.Fatalf("Validate projected depth overflow = %#v", diags)
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

func TestIslandCompositionNodeLimitMatchesExactEmittedTree(t *testing.T) {
	tests := []struct {
		name string
		prog func(int) *Program
	}{
		{name: "leaf calls", prog: leafCallIslandProgram},
		{name: "repeated children holes", prog: repeatedChildrenHoleIslandProgram},
	}
	for _, tc := range tests {
		t.Run(tc.name+" at limit", func(t *testing.T) {
			prog := tc.prog(maxIslandProgramEntries - 1)
			lowered, err := LowerIsland(prog, 0)
			if err != nil {
				t.Fatalf("LowerIsland: %v", err)
			}
			if got := len(lowered.Nodes); got != maxIslandProgramEntries {
				t.Fatalf("emitted nodes = %d, want %d", got, maxIslandProgramEntries)
			}
			if diags := Validate(prog); diagnosticsContain(diags, "expanded-node limit") {
				t.Fatalf("Validate rejected exact emitted-node limit: %#v", diags)
			}
		})
		t.Run(tc.name+" overflow", func(t *testing.T) {
			prog := tc.prog(maxIslandProgramEntries)
			_, err := LowerIsland(prog, 0)
			if err == nil || !strings.Contains(err.Error(), "65,535 expanded-node limit") {
				t.Fatalf("LowerIsland error = %v", err)
			}
			if diags := Validate(prog); !diagnosticsContain(diags, "65,535 expanded-node limit") {
				t.Fatalf("Validate overflow diagnostics = %#v", diags)
			}
		})
	}
}

func TestLowerIslandRejectsScopeExpressionOverflowAfterNodeExpressions(t *testing.T) {
	exprs, _, err := ParseExpr("props.Value", nil)
	if err != nil {
		t.Fatal(err)
	}
	signalCount := maxIslandProgramEntries - len(exprs) + 1
	signals := make([]SignalInfo, signalCount)
	for i := range signals {
		signals[i] = SignalInfo{Name: "filled", InitExpr: "0", TypeHint: "int"}
	}
	prog := &Program{
		Nodes: []Node{{Kind: NodeExpr, Text: "props.Value"}},
		Components: []Component{{
			Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true,
			Scope: &ComponentScope{Signals: signals},
		}},
	}
	_, err = LowerIsland(prog, 0)
	if err == nil || !strings.Contains(err.Error(), "65,535 expression limit") {
		t.Fatalf("LowerIsland expression overflow = %v", err)
	}
	if diags := Validate(prog); !diagnosticsContain(diags, "65,535 expression limit") {
		t.Fatalf("Validate expression overflow = %#v", diags)
	}
}

func TestIslandExpressionAllocationsFailClosedAtCapacity(t *testing.T) {
	newFullLowerer := func() *islandLowerer {
		l := newIslandLowerer(&Program{}, "Root", cloneExprScope(nil))
		l.dst.Exprs = make([]program.Expr, maxIslandProgramEntries)
		return l
	}
	tests := map[string]func(*islandLowerer) error{
		"signal": func(l *islandLowerer) error {
			return l.emitSignalDefs([]SignalInfo{{Name: "value", InitExpr: "0"}})
		},
		"computed": func(l *islandLowerer) error {
			return l.emitComputedDefs([]ComputedInfo{{Name: "value", BodyExpr: "0"}})
		},
		"handler": func(l *islandLowerer) error {
			return l.emitHandlerDefs([]HandlerInfo{{Name: "tap", Statements: []string{"0"}}})
		},
		"inline event": func(l *islandLowerer) error {
			_, err := l.lowerInlineEvent("click", "0")
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(newFullLowerer()); err == nil || !strings.Contains(err.Error(), "65,535 expression limit") {
				t.Fatalf("allocation error = %v", err)
			}
		})
	}
}

func TestIslandProgramIntegrityRejectsOverflowingExpressionCount(t *testing.T) {
	l := newIslandLowerer(&Program{}, "Root", cloneExprScope(nil))
	l.dst.Nodes = []program.Node{{Kind: program.NodeText}}
	l.dst.StaticMask = []bool{true}
	l.dst.Exprs = make([]program.Expr, maxIslandProgramEntries+1)
	if err := l.validateProgramIntegrity(); err == nil || !strings.Contains(err.Error(), "65536 expressions; the program limit is 65,535") {
		t.Fatalf("program integrity error = %v", err)
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

func TestIslandCompositionSlotDiagnosticOrderIsDeterministic(t *testing.T) {
	purityProgram := &Program{
		Nodes: []Node{
			{Kind: NodeComponent, Tag: "View"},
			{Kind: NodeElement, Tag: "div", Slots: map[string]NodeID{"Zulu": 2, "Alpha": 3}},
			{Kind: NodeElement, Tag: "button", Attrs: []Attr{{Kind: AttrStatic, Name: "data-on-zulu", Value: "tap"}}},
			{Kind: NodeElement, Tag: "button", Attrs: []Attr{{Kind: AttrStatic, Name: "data-on-alpha", Value: "tap"}}},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "View", Syntax: ComponentSyntaxStrict, Root: 1},
		},
	}
	validatorProgram := &Program{
		Nodes: []Node{
			{Kind: NodeElement, Tag: "div", Slots: map[string]NodeID{"Zulu": 1, "Alpha": 2}},
			{Kind: NodeExpr, Text: "zulu()"},
			{Kind: NodeExpr, Text: "alpha()"},
		},
		Components: []Component{{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true}},
	}
	var first string
	for i := 0; i < 100; i++ {
		_, err := LowerIsland(purityProgram, 0)
		if err == nil || !strings.Contains(err.Error(), `handler attribute "data-on-alpha"`) {
			t.Fatalf("LowerIsland run %d = %v, want alphabetically first slot diagnostic", i, err)
		}
		diags := Validate(validatorProgram)
		var messages []string
		for _, diag := range diags {
			messages = append(messages, diag.Message)
		}
		got := strings.Join(messages, "\n")
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatalf("Validate diagnostic order changed on run %d:\nfirst=%q\n got=%q", i, first, got)
		}
		alpha := strings.Index(got, `"alpha"`)
		zulu := strings.Index(got, `"zulu"`)
		if alpha < 0 || zulu < 0 || alpha >= zulu {
			t.Fatalf("Validate run %d = %q, want Alpha diagnostic before Zulu", i, got)
		}
	}
}

func shadowedIslandComponentProgram(tag string) *Program {
	return &Program{
		Nodes: []Node{
			{Kind: NodeComponent, Tag: tag},
			{Kind: NodeElement, Tag: "span", IsStatic: true},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: tag, Syntax: ComponentSyntaxStrict, Root: 1},
		},
	}
}

func nestedWrapperProjectionProgram(callCount int) *Program {
	nodes := make([]Node, callCount)
	for i := range nodes {
		nodes[i] = Node{Kind: NodeComponent, Tag: "Wrapper"}
		if i+1 < callCount {
			nodes[i].Children = []NodeID{NodeID(i + 1)}
		}
	}
	leafID := NodeID(len(nodes))
	nodes = append(nodes, Node{Kind: NodeElement, Tag: "span", IsStatic: true})
	if callCount > 0 {
		nodes[callCount-1].Children = []NodeID{leafID}
	}
	wrapperRoot := NodeID(len(nodes))
	holeID := wrapperRoot + 1
	nodes = append(nodes,
		Node{Kind: NodeElement, Tag: "div", Children: []NodeID{holeID}},
		Node{Kind: NodeExpr, Text: "children"},
	)
	return &Program{
		Nodes: nodes,
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "Wrapper", Syntax: ComponentSyntaxStrict, Root: wrapperRoot, AcceptsChildren: true},
		},
	}
}

func leafCallIslandProgram(callCount int) *Program {
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
	return prog
}

func repeatedChildrenHoleIslandProgram(holeCount int) *Program {
	holes := make([]NodeID, holeCount)
	for i := range holes {
		holes[i] = 4
	}
	return &Program{
		Nodes: []Node{
			{Kind: NodeComponent, Tag: "Repeater", Children: []NodeID{1}},
			{Kind: NodeComponent, Tag: "Leaf"},
			{Kind: NodeElement, Tag: "span", IsStatic: true},
			{Kind: NodeFragment, Children: holes},
			{Kind: NodeExpr, Text: "children"},
		},
		Components: []Component{
			{Name: "Root", Syntax: ComponentSyntaxStrict, Root: 0, IsIsland: true},
			{Name: "Repeater", Syntax: ComponentSyntaxStrict, Root: 3, AcceptsChildren: true},
			{Name: "Leaf", Syntax: ComponentSyntaxStrict, Root: 2},
		},
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
