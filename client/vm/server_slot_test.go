package vm

import (
	"fmt"
	"m31labs.dev/gosx/island/program"
	"testing"
)

func TestServerSlotOrdinaryTransitionAllocations(t *testing.T) {
	tree := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "div", Children: []int{1}}, {Tag: "p", Text: "ordinary"}}}
	if n := testing.AllocsPerRun(100, func() {
		if err := ValidateServerSlotTransition(tree, tree); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatalf("ordinary ownership allocations=%v", n)
	}
	next := &ResolvedTree{Nodes: []ResolvedNode{tree.Nodes[0], {Tag: "div", ServerSlot: "Profile"}}}
	if err := ValidateServerSlotTransition(tree, next); err == nil {
		t.Fatal("ordinary to opaque mutation skipped")
	}
}

func TestServerSlotRejectedTransitionKeepsBaseline(t *testing.T) {
	for _, beforeRefusal := range []bool{false, true} {
		t.Run(fmt.Sprint(beforeRefusal), func(t *testing.T) { testServerSlotRejectedTransition(t, beforeRefusal) })
	}
}

func testServerSlotRejectedTransition(t *testing.T, beforeRefusal bool) {
	p := &program.Program{Name: "Panels", Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}}, {Kind: program.NodeServerSlot, Tag: "div", Text: "Profile"}}}
	p.Exprs = []program.Expr{{Op: program.OpLitString, Value: "before", Type: program.TypeString}, {Op: program.OpSignalGet, Value: "class", Type: program.TypeString}}
	p.Signals = []program.SignalDef{{Name: "class", Init: 0, Type: program.TypeString}}
	p.Nodes[0].Attrs = []program.Attr{{Kind: program.AttrExpr, Name: "class", Expr: 1}}
	i := NewIsland(p, `{}`)
	defer i.Dispose()
	before := i.CurrentTree()
	if beforeRefusal {
		i.vm.signals["class"].Set(StringVal("repaired"))
	}
	p.Nodes[1].Text = "Security"
	if ops := i.Reconcile(); len(ops) != 0 || i.CurrentTree() != before {
		t.Fatal("refusal advanced baseline", ops)
	}
	if ds := i.vm.Diagnostics(); len(ds) == 0 || ds[len(ds)-1].Code != "server_slot_transition" {
		t.Fatal("missing refusal diagnostic", ds)
	}
	p.Nodes[1].Text = "Profile"
	if !beforeRefusal {
		i.vm.signals["class"].Set(StringVal("repaired"))
	}
	if ops := i.Reconcile(); len(ops) != 1 || ops[0].AttrName != "class" {
		t.Fatal("repair diff lost baseline", ops)
	}
}

func TestServerSlotTransitionRefusesClearing(t *testing.T) {
	base := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "div", Children: []int{1}}, {Tag: "div", ServerSlot: "Profile"}}}
	for _, tc := range []struct {
		name string
		node ResolvedNode
	}{
		{"text", ResolvedNode{Tag: "div", ServerSlot: "Profile", Text: "clear"}},
		{"identity", ResolvedNode{Tag: "div", ServerSlot: "Security"}},
		{"ordinary", ResolvedNode{Tag: "div"}},
		{"children", ResolvedNode{Tag: "div", ServerSlot: "Profile", Children: []int{0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := &ResolvedTree{Nodes: []ResolvedNode{base.Nodes[0], tc.node}}
			if err := ValidateServerSlotTransition(base, next); err == nil {
				t.Fatal("unsafe transition admitted")
			}
			if ops := ReconcileTrees(base, next, nil); len(ops) != 0 {
				t.Fatalf("unsafe patches %+v", ops)
			}
		})
	}
}
