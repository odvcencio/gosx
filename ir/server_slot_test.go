package ir

import (
	"testing"
)

func TestServerSlotLowering(t *testing.T) {
	p := &Program{Nodes: []Node{
		{Kind: NodeElement, Tag: "div", Children: []NodeID{1}},
		{Kind: NodeComponent, Tag: "ServerSlot", Attrs: []Attr{{Kind: AttrStatic, Name: "name", Value: "Profile"}}},
	}, Components: []Component{{Name: "Panels", IsIsland: true, Root: 0}}}
	got, err := LowerIsland(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 2 || got.Nodes[1].Tag != "div" || got.Nodes[1].Text != "Profile" {
		t.Fatalf("missing opaque Profile outlet: %+v", got.Nodes)
	}
}
