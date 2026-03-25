package ir

import (
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
