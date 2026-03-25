package vm

import "testing"

func TestReconcileTextChange(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "span", Text: "old"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "span", Text: "new"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetText {
		t.Fatal("expected SetText")
	}
	if ops[0].Text != "new" {
		t.Fatalf("expected 'new', got %q", ops[0].Text)
	}
}

func TestReconcileStaticSkip(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "span", Text: "old"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "span", Text: "new"},
	}}
	ops := ReconcileTrees(prev, next, []bool{true}) // static!
	if len(ops) != 0 {
		t.Fatalf("expected 0 ops for static node, got %d", len(ops))
	}
}

func TestReconcileAttrChange(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Attrs: []ResolvedAttr{{Name: "class", Value: "old"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Attrs: []ResolvedAttr{{Name: "class", Value: "new"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetAttr {
		t.Fatal("expected SetAttr")
	}
	if ops[0].AttrName != "class" {
		t.Fatal("expected class attr")
	}
	if ops[0].Text != "new" {
		t.Fatal("expected new value")
	}
}

func TestReconcileAttrRemoval(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Attrs: []ResolvedAttr{{Name: "class", Value: "old"}, {Name: "id", Value: "x"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Attrs: []ResolvedAttr{{Name: "class", Value: "old"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchRemoveAttr {
		t.Fatal("expected RemoveAttr")
	}
	if ops[0].AttrName != "id" {
		t.Fatal("expected id attr removed")
	}
}

func TestReconcileNoChange(t *testing.T) {
	tree := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Text: "same", Attrs: []ResolvedAttr{{Name: "class", Value: "x"}}},
	}}
	ops := ReconcileTrees(tree, tree, []bool{false})
	if len(ops) != 0 {
		t.Fatalf("expected 0 ops for unchanged tree, got %d", len(ops))
	}
}

func TestReconcileInputValueNoOp(t *testing.T) {
	// Spec section 13.2: same value on input should NOT emit PatchSetValue
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "input", Attrs: []ResolvedAttr{{Name: "value", Value: "hello"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "input", Attrs: []ResolvedAttr{{Name: "value", Value: "hello"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 0 {
		t.Fatalf("expected 0 ops for same input value, got %d", len(ops))
	}
}

func TestReconcileInputValueChange(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "input", Attrs: []ResolvedAttr{{Name: "value", Value: "old"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "input", Attrs: []ResolvedAttr{{Name: "value", Value: "new"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetValue {
		t.Fatal("expected PatchSetValue for input")
	}
}

func TestReconcileNilTrees(t *testing.T) {
	ops := ReconcileTrees(nil, nil, nil)
	if ops != nil {
		t.Fatal("expected nil for nil trees")
	}
}

func TestReconcileTextareaValueChange(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "textarea", Attrs: []ResolvedAttr{{Name: "value", Value: "old"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "textarea", Attrs: []ResolvedAttr{{Name: "value", Value: "new"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetValue {
		t.Fatal("expected PatchSetValue for textarea")
	}
}

func TestReconcileSelectValueChange(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "select", Attrs: []ResolvedAttr{{Name: "value", Value: "a"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "select", Attrs: []ResolvedAttr{{Name: "value", Value: "b"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetValue {
		t.Fatal("expected PatchSetValue for select")
	}
}

func TestReconcileChildPath(t *testing.T) {
	// Root div with two children: a span (text "old"->"new") and a static span
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1, 2}},    // node 0: root
		{Tag: "span", Text: "old"},               // node 1: dynamic child
		{Tag: "span", Text: "static"},             // node 2: static child
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1, 2}},    // node 0: root
		{Tag: "span", Text: "new"},               // node 1: changed
		{Tag: "span", Text: "static"},             // node 2: unchanged
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, true})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Path != "0" {
		t.Fatalf("expected path '0', got %q", ops[0].Path)
	}
	if ops[0].Kind != PatchSetText {
		t.Fatal("expected SetText")
	}
	if ops[0].Text != "new" {
		t.Fatalf("expected 'new', got %q", ops[0].Text)
	}
}

func TestReconcileNestedPath(t *testing.T) {
	// Root -> child0 -> grandchild0 (text change)
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1}},      // node 0: root
		{Tag: "div", Children: []int{2}},      // node 1: child
		{Tag: "span", Text: "old"},             // node 2: grandchild
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1}},      // node 0: root
		{Tag: "div", Children: []int{2}},      // node 1: child
		{Tag: "span", Text: "new"},             // node 2: grandchild
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Path != "0/0" {
		t.Fatalf("expected path '0/0', got %q", ops[0].Path)
	}
}

func TestReconcileNewChildren(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1}},
		{Tag: "span", Text: "first"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1, 2}},
		{Tag: "span", Text: "first"},
		{Tag: "span", Text: "second"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false})
	// Should have a PatchCreateElement for the new child
	found := false
	for _, op := range ops {
		if op.Kind == PatchCreateElement {
			found = true
			if op.Path != "" {
				t.Fatalf("expected path '' (root), got %q", op.Path)
			}
			if op.Tag != "span" {
				t.Fatalf("expected tag 'span', got %q", op.Tag)
			}
		}
	}
	if !found {
		t.Fatal("expected PatchCreateElement op")
	}
}

func TestReconcileRemovedChildren(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1, 2}},
		{Tag: "span", Text: "first"},
		{Tag: "span", Text: "second"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1}},
		{Tag: "span", Text: "first"},
		{Tag: "span", Text: "second"}, // still in nodes array, just not a child
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false})
	found := false
	for _, op := range ops {
		if op.Kind == PatchRemoveElement {
			found = true
			if op.Path != "1" {
				t.Fatalf("expected path '1' for removed child, got %q", op.Path)
			}
		}
	}
	if !found {
		t.Fatal("expected PatchRemoveElement op")
	}
}

func TestReconcileAttrAdded(t *testing.T) {
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Attrs: []ResolvedAttr{}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Attrs: []ResolvedAttr{{Name: "class", Value: "new"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetAttr {
		t.Fatal("expected SetAttr for new attribute")
	}
	if ops[0].AttrName != "class" {
		t.Fatal("expected class attr")
	}
}

func TestReconcileInputNonValueAttr(t *testing.T) {
	// Non-value attributes on input should still use PatchSetAttr
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "input", Attrs: []ResolvedAttr{{Name: "class", Value: "old"}}},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "input", Attrs: []ResolvedAttr{{Name: "class", Value: "new"}}},
	}}
	ops := ReconcileTrees(prev, next, []bool{false})
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	if ops[0].Kind != PatchSetAttr {
		t.Fatal("expected PatchSetAttr for non-value attr on input")
	}
}
