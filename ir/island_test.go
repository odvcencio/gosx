package ir

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestLowerIslandSimpleElement(t *testing.T) {
	// Build an IR program with a simple island component
	prog := &Program{}

	// Node 0: <div class="counter">
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement,
		Tag:  "div",
		Attrs: []Attr{
			{Kind: AttrStatic, Name: "class", Value: "counter"},
		},
		Children: []NodeID{1, 2},
		IsStatic: false,
	})
	// Node 1: text "hello"
	prog.Nodes = append(prog.Nodes, Node{
		Kind:     NodeText,
		Text:     "hello",
		IsStatic: true,
	})
	// Node 2: expression {count}
	prog.Nodes = append(prog.Nodes, Node{
		Kind:     NodeExpr,
		Text:     "count",
		IsStatic: false,
	})

	prog.Components = append(prog.Components, Component{
		Name:     "Counter",
		Root:     0,
		IsIsland: true,
	})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}

	if island.Name != "Counter" {
		t.Fatalf("expected Counter, got %s", island.Name)
	}
	if len(island.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(island.Nodes))
	}
	if island.Nodes[0].Kind != program.NodeElement {
		t.Fatal("expected element")
	}
	if island.Nodes[0].Tag != "div" {
		t.Fatal("expected div")
	}
	if island.Nodes[1].Kind != program.NodeText {
		t.Fatal("expected text")
	}
	if island.Nodes[2].Kind != program.NodeExpr {
		t.Fatal("expected expr")
	}
}

func TestLowerIslandStaticMask(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{Kind: NodeElement, Tag: "div", Children: []NodeID{1}, IsStatic: false})
	prog.Nodes = append(prog.Nodes, Node{Kind: NodeText, Text: "static", IsStatic: true})
	prog.Components = append(prog.Components, Component{Name: "Test", Root: 0, IsIsland: true})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(island.StaticMask) != 2 {
		t.Fatalf("expected 2 mask entries, got %d", len(island.StaticMask))
	}
	if island.StaticMask[0] != false {
		t.Fatal("root should not be static")
	}
	if island.StaticMask[1] != true {
		t.Fatal("text should be static")
	}
}

func TestLowerIslandEventAttr(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement, Tag: "button",
		Attrs: []Attr{
			{Kind: AttrExpr, Name: "onClick", Expr: "handleClick", IsEvent: true},
		},
	})
	prog.Components = append(prog.Components, Component{Name: "Btn", Root: 0, IsIsland: true})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(island.Nodes[0].Attrs) != 1 {
		t.Fatal("expected 1 attr")
	}
	attr := island.Nodes[0].Attrs[0]
	if attr.Kind != program.AttrEvent {
		t.Fatalf("expected AttrEvent, got %d", attr.Kind)
	}
	if attr.Event != "handleClick" {
		t.Fatalf("expected handleClick, got %s", attr.Event)
	}
}

func TestLowerIslandSpreadReject(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement, Tag: "div",
		Attrs: []Attr{{Kind: AttrSpread, Expr: "props"}},
	})
	prog.Components = append(prog.Components, Component{Name: "Bad", Root: 0, IsIsland: true})

	_, err := LowerIsland(prog, 0)
	if err == nil {
		t.Fatal("expected error for spread attribute")
	}
}

func TestLowerIslandNotIsland(t *testing.T) {
	prog := &Program{}
	prog.Components = append(prog.Components, Component{Name: "Server", Root: 0, IsIsland: false})
	prog.Nodes = append(prog.Nodes, Node{Kind: NodeElement, Tag: "div"})

	_, err := LowerIsland(prog, 0)
	if err == nil {
		t.Fatal("expected error for non-island component")
	}
}

func TestLowerIslandOutOfRange(t *testing.T) {
	prog := &Program{}
	_, err := LowerIsland(prog, 5)
	if err == nil {
		t.Fatal("expected error for out of range")
	}
}

func TestValidateIslandValid(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement, Tag: "div",
		Attrs: []Attr{
			{Kind: AttrStatic, Name: "class", Value: "counter"},
			{Kind: AttrExpr, Name: "onClick", Expr: "handleClick", IsEvent: true},
		},
	})
	prog.Components = append(prog.Components, Component{Name: "Good", Root: 0, IsIsland: true})

	diags := Validate(prog)
	for _, d := range diags {
		if d.Message != "" {
			// Filter for island-specific errors only
			t.Logf("diagnostic: %s", d.Message)
		}
	}
}

func TestValidateIslandSpreadRejected(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement, Tag: "div",
		Attrs: []Attr{{Kind: AttrSpread, Expr: "props"}},
	})
	prog.Components = append(prog.Components, Component{Name: "Bad", Root: 0, IsIsland: true})

	diags := Validate(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "spread") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected diagnostic about spread attributes")
	}
}

func TestValidateIslandGoroutineRejected(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeExpr, Text: "go func(){}()",
	})
	prog.Components = append(prog.Components, Component{Name: "Bad", Root: 0, IsIsland: true})

	diags := Validate(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "goroutine") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected diagnostic about goroutine")
	}
}

func TestValidateIslandChannelRejected(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeExpr, Text: "<-ch",
	})
	prog.Components = append(prog.Components, Component{Name: "Bad", Root: 0, IsIsland: true})

	diags := Validate(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "channel") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected diagnostic about channels")
	}
}

func TestValidateIslandConditionalComponentRefsAccepted(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind:     NodeComponent,
		Tag:      "If",
		Attrs:    []Attr{{Kind: AttrExpr, Name: "when", Expr: "visible"}},
		Children: []NodeID{1},
	})
	prog.Nodes = append(prog.Nodes, Node{Kind: NodeText, Text: "visible"})
	prog.Components = append(prog.Components, Component{Name: "Good", Root: 0, IsIsland: true})

	diags := Validate(prog)
	for _, d := range diags {
		if strings.Contains(d.Message, "not supported inside island components") {
			t.Fatalf("conditional component should validate inside islands, got %q", d.Message)
		}
	}
}

func TestValidateIslandElementAliasComponentRefsAccepted(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeComponent,
		Tag:  "Link",
		Attrs: []Attr{
			{Kind: AttrStatic, Name: "href", Value: "/docs"},
		},
	})
	prog.Components = append(prog.Components, Component{Name: "Good", Root: 0, IsIsland: true})

	diags := Validate(prog)
	for _, d := range diags {
		if strings.Contains(d.Message, "not supported inside island components") {
			t.Fatalf("element alias component should validate inside islands, got %q", d.Message)
		}
	}
}

func TestValidateIslandUnsupportedComponentRefRejected(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeComponent,
		Tag:  "Scene3D",
	})
	prog.Components = append(prog.Components, Component{Name: "Bad", Root: 0, IsIsland: true})

	diags := Validate(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "not supported inside island components") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected diagnostic about unsupported component refs inside islands")
	}
}

func TestValidateIslandAcceptsSignalAliasExprsFromComponentScope(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeExpr,
		Text: "count.Get()",
	})
	prog.Components = append(prog.Components, Component{
		Name:     "Counter",
		Root:     0,
		IsIsland: true,
		Scope: &ComponentScope{
			Signals: []SignalInfo{{Name: "$count", Local: "count", InitExpr: "0", TypeHint: "int"}},
		},
	})

	diags := Validate(prog)
	for _, d := range diags {
		if strings.Contains(d.Message, "island expression error") {
			t.Fatalf("expected alias signal expression to validate, got %q", d.Message)
		}
	}
}

func TestValidateIslandRejectsChannelCreationInAttrExpr(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeElement,
		Tag:  "div",
		Attrs: []Attr{
			{Kind: AttrExpr, Name: "data-bad", Expr: "make(chan int)"},
		},
	})
	prog.Components = append(prog.Components, Component{Name: "Bad", Root: 0, IsIsland: true})

	diags := Validate(prog)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "channel creation") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected diagnostic about channel creation in attribute expression")
	}
}

func TestLowerIslandEachComponent(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes,
		Node{
			Kind:     NodeComponent,
			Tag:      "Each",
			Attrs:    []Attr{{Kind: AttrExpr, Name: "of", Expr: "items"}, {Kind: AttrStatic, Name: "as", Value: "item"}, {Kind: AttrStatic, Name: "index", Value: "i"}},
			Children: []NodeID{1},
		},
		Node{
			Kind:     NodeElement,
			Tag:      "li",
			Children: []NodeID{2, 3, 4},
		},
		Node{Kind: NodeExpr, Text: "i"},
		Node{Kind: NodeText, Text: ":"},
		Node{Kind: NodeExpr, Text: "item"},
	)
	prog.Components = append(prog.Components, Component{Name: "List", Root: 0, IsIsland: true})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if len(island.Nodes) != 5 {
		t.Fatalf("expected 5 lowered nodes, got %d", len(island.Nodes))
	}
	if island.Nodes[0].Kind != program.NodeForEach {
		t.Fatalf("expected NodeForEach, got %s", island.Nodes[0].Kind)
	}
	if got := forEachStaticAttr(island.Nodes[0].Attrs, "as"); got != "item" {
		t.Fatalf("expected each alias item, got %q", got)
	}
	if got := forEachStaticAttr(island.Nodes[0].Attrs, "index"); got != "i" {
		t.Fatalf("expected each index alias i, got %q", got)
	}
	if island.Nodes[1].Kind != program.NodeElement || island.Nodes[1].Tag != "li" {
		t.Fatalf("expected li child element, got %+v", island.Nodes[1])
	}
	if island.Nodes[2].Kind != program.NodeExpr || island.Nodes[3].Kind != program.NodeText || island.Nodes[4].Kind != program.NodeExpr {
		t.Fatalf("expected li children to lower through each scope, got %+v %+v %+v", island.Nodes[2], island.Nodes[3], island.Nodes[4])
	}
}

func TestLowerIslandConditionalComponent(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes,
		Node{
			Kind:     NodeComponent,
			Tag:      "Show",
			Attrs:    []Attr{{Kind: AttrExpr, Name: "when", Expr: "visible"}, {Kind: AttrStatic, Name: "fallback", Value: "hidden"}},
			Children: []NodeID{1},
		},
		Node{Kind: NodeText, Text: "visible"},
	)
	prog.Components = append(prog.Components, Component{Name: "Conditional", Root: 0, IsIsland: true})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("LowerIsland failed: %v", err)
	}
	root := island.Nodes[island.Root]
	if root.Kind != program.NodeConditional {
		t.Fatalf("expected conditional root, got %s", root.Kind)
	}
	if len(root.Children) != 1 {
		t.Fatalf("expected one conditional child, got %#v", root.Children)
	}
	if len(root.Attrs) != 1 || root.Attrs[0].Name != "fallback" {
		t.Fatalf("expected fallback attr, got %#v", root.Attrs)
	}
}

func TestLowerIslandElementAliasComponents(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes,
		Node{
			Kind: NodeComponent,
			Tag:  "Link",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "href", Value: "/docs"},
			},
			Children: []NodeID{1},
		},
		Node{Kind: NodeText, Text: "Docs"},
		Node{
			Kind: NodeComponent,
			Tag:  "Image",
			Attrs: []Attr{
				{Kind: AttrStatic, Name: "src", Value: "/hero.png"},
				{Kind: AttrStatic, Name: "alt", Value: "Hero"},
			},
		},
	)
	prog.Nodes[0].Children = append(prog.Nodes[0].Children, 2)
	prog.Components = append(prog.Components, Component{Name: "Aliases", Root: 0, IsIsland: true})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("LowerIsland failed: %v", err)
	}
	root := island.Nodes[island.Root]
	if root.Kind != program.NodeElement || root.Tag != "a" {
		t.Fatalf("expected Link to lower to <a>, got kind=%s tag=%q", root.Kind, root.Tag)
	}
	image := island.Nodes[root.Children[1]]
	if image.Kind != program.NodeElement || image.Tag != "img" {
		t.Fatalf("expected Image to lower to <img>, got kind=%s tag=%q", image.Kind, image.Tag)
	}
}

func TestLowerIslandEmitsComponentScopeDefs(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes,
		Node{
			Kind: NodeElement,
			Tag:  "button",
			Attrs: []Attr{
				{Kind: AttrExpr, Name: "onInput", Expr: "sync", IsEvent: true},
			},
			Children: []NodeID{1},
		},
		Node{Kind: NodeExpr, Text: "labelUpper"},
	)
	prog.Components = append(prog.Components, Component{
		Name:     "Editor",
		Root:     0,
		IsIsland: true,
		Scope: &ComponentScope{
			Signals: []SignalInfo{
				{Name: "label", Local: "label", InitExpr: `"draft"`, TypeHint: "string"},
			},
			Computeds: []ComputedInfo{
				{Name: "labelUpper", BodyExpr: "label.Get()"},
			},
			Handlers: []HandlerInfo{
				{Name: "sync", Statements: []string{"label.Set(value)"}},
			},
			Locals: map[string]string{
				"label":      "signal",
				"labelUpper": "computed",
				"sync":       "handler",
			},
		},
	})

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if len(island.Signals) != 1 || island.Signals[0].Name != "label" {
		t.Fatalf("expected emitted signal def for label, got %+v", island.Signals)
	}
	if len(island.Computeds) != 1 || island.Computeds[0].Name != "labelUpper" {
		t.Fatalf("expected emitted computed def for labelUpper, got %+v", island.Computeds)
	}
	if len(island.Handlers) != 1 || island.Handlers[0].Name != "sync" || len(island.Handlers[0].Body) != 1 {
		t.Fatalf("expected emitted sync handler body, got %+v", island.Handlers)
	}

	foundEventGet := false
	for _, expr := range island.Exprs {
		if expr.Op == program.OpEventGet && expr.Value == "value" {
			foundEventGet = true
			break
		}
	}
	if !foundEventGet {
		t.Fatalf("expected handler lowering to expose event value in expr table, got %+v", island.Exprs)
	}
}

func TestLowerIslandComputedDefinitionsUseSequentialScope(t *testing.T) {
	baseProgram := func(computeds []ComputedInfo) *Program {
		return &Program{
			Nodes: []Node{{Kind: NodeExpr, Text: computeds[len(computeds)-1].Name}},
			Components: []Component{{
				Name:     "ComputedScope",
				Root:     0,
				IsIsland: true,
				Scope: &ComponentScope{
					Signals:   []SignalInfo{{Name: "count", Local: "count", InitExpr: "1", TypeHint: "int"}},
					Computeds: computeds,
				},
			}},
		}
	}

	valid, err := LowerIsland(baseProgram([]ComputedInfo{
		{Name: "doubled", BodyExpr: "count.Get() * 2"},
		{Name: "label", BodyExpr: "doubled.Get() + 1"},
	}), 0)
	if err != nil {
		t.Fatalf("valid source-order chain: %v", err)
	}
	if len(valid.Computeds) != 2 {
		t.Fatalf("valid computed chain = %+v", valid.Computeds)
	}

	tests := []struct {
		name      string
		computeds []ComputedInfo
		want      string
	}{
		{
			name: "forward reference",
			computeds: []ComputedInfo{
				{Name: "first", BodyExpr: "later.Get()"},
				{Name: "later", BodyExpr: "count.Get()"},
			},
			want: "parse computed first",
		},
		{
			name:      "self reference",
			computeds: []ComputedInfo{{Name: "loop", BodyExpr: "loop.Get()"}},
			want:      "parse computed loop",
		},
		{
			name:      "missing body",
			computeds: []ComputedInfo{{Name: "empty", BodyExpr: ""}},
			want:      "exactly one return expression",
		},
		{
			name:      "multi statement body",
			computeds: []ComputedInfo{{Name: "many", BodyExpr: "count.Get(); count.Get()"}},
			want:      "parse computed many",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LowerIsland(baseProgram(test.computeds), 0)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LowerIsland error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLowerIslandHandlerBrowserHostAndExpandedEventFields(t *testing.T) {
	prog := &Program{
		Nodes: []Node{{
			Kind:  NodeElement,
			Tag:   "button",
			Attrs: []Attr{{Kind: AttrExpr, Name: "onPointerDown", Expr: "activate", IsEvent: true}},
		}},
		Components: []Component{{
			Name:     "BrowserControls",
			Root:     0,
			IsIsland: true,
			Scope: &ComponentScope{Handlers: []HandlerInfo{{
				Name: "activate",
				Statements: []string{
					`browser.PreventDefault(ctrlKey)`,
					`browser.Focus(data["focusTarget"])`,
					`browser.Submit(eventData != "" , "#move-form")`,
				},
			}}},
		}},
	}

	island, err := LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("LowerIsland: %v", err)
	}
	wantEvents := map[string]program.ExprType{
		"ctrlKey":   program.TypeBool,
		"data":      program.TypeAny,
		"eventData": program.TypeString,
	}
	foundEvents := make(map[string]bool, len(wantEvents))
	hostCalls := 0
	for _, expr := range island.Exprs {
		if expr.Op == program.OpEventGet {
			if wantType, ok := wantEvents[expr.Value]; ok {
				foundEvents[expr.Value] = true
				if expr.Type != wantType {
					t.Errorf("event field %q type = %v, want %v", expr.Value, expr.Type, wantType)
				}
			}
		}
		if expr.Op == program.OpHostCall && len(expr.Value) > len("browser.") && expr.Value[:len("browser.")] == "browser." {
			hostCalls++
		}
	}
	if hostCalls != 3 {
		t.Fatalf("browser host calls = %d, want 3; exprs=%+v", hostCalls, island.Exprs)
	}
	for field := range wantEvents {
		if !foundEvents[field] {
			t.Errorf("event field %q did not lower to OpEventGet", field)
		}
	}
}

func TestHandlerEventFieldsDoNotShadowAuthoredBindings(t *testing.T) {
	scope := &ExprScope{
		Signals:       map[string]bool{"code": true},
		SignalAliases: map[string]string{"data": "$data"},
		Props:         map[string]bool{"key": true},
		Handlers:      map[string]bool{"repeat": true},
		EventFields:   map[string]bool{},
	}
	handlerScope := handlerExprScope(scope)
	for _, name := range []string{"code", "data", "key", "repeat"} {
		if handlerScope.EventFields[name] {
			t.Fatalf("event field %q shadows authored handler binding", name)
		}
	}
	if !handlerScope.EventFields["value"] || !handlerScope.EventFields["timeStamp"] {
		t.Fatalf("non-colliding event fields were not injected: %+v", handlerScope.EventFields)
	}
}
