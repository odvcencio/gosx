package ir

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx/island/program"
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

func TestValidateIslandComponentRefRejected(t *testing.T) {
	prog := &Program{}
	prog.Nodes = append(prog.Nodes, Node{
		Kind: NodeComponent,
		Tag:  "If",
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
		t.Fatal("expected diagnostic about component refs inside islands")
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
