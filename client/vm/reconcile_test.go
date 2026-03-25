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

// === Keyed Reconciliation Tests ===

func TestReconcileKeyedReorder(t *testing.T) {
	// Prev: A, B, C  →  Next: C, A, B
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2, 3}},
		{Tag: "li", Key: "a", Text: "Apple"},
		{Tag: "li", Key: "b", Text: "Banana"},
		{Tag: "li", Key: "c", Text: "Cherry"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2, 3}},
		{Tag: "li", Key: "c", Text: "Cherry"},
		{Tag: "li", Key: "a", Text: "Apple"},
		{Tag: "li", Key: "b", Text: "Banana"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false, false})

	// Should emit Reorder, NOT 3 SetText ops
	foundReorder := false
	for _, op := range ops {
		if op.Kind == PatchReorder {
			foundReorder = true
		}
	}
	if !foundReorder {
		t.Fatalf("expected Reorder op for keyed reorder, got %d ops: %+v", len(ops), ops)
	}
}

func TestReconcileKeyedInsert(t *testing.T) {
	// Prev: A, B  →  Next: A, X, B (insert in middle)
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2}},
		{Tag: "li", Key: "a", Text: "A"},
		{Tag: "li", Key: "b", Text: "B"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2, 3}},
		{Tag: "li", Key: "a", Text: "A"},
		{Tag: "li", Key: "x", Text: "X"},
		{Tag: "li", Key: "b", Text: "B"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false, false})

	foundCreate := false
	for _, op := range ops {
		if op.Kind == PatchCreateElement {
			foundCreate = true
			if op.Tag != "li" {
				t.Fatalf("expected li create, got %s", op.Tag)
			}
		}
	}
	if !foundCreate {
		t.Fatal("expected CreateElement for new keyed item")
	}
}

func TestReconcileKeyedRemove(t *testing.T) {
	// Prev: A, B, C  →  Next: A, C (B removed)
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2, 3}},
		{Tag: "li", Key: "a", Text: "A"},
		{Tag: "li", Key: "b", Text: "B"},
		{Tag: "li", Key: "c", Text: "C"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2}},
		{Tag: "li", Key: "a", Text: "A"},
		{Tag: "li", Key: "c", Text: "C"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false, false})

	foundRemove := false
	for _, op := range ops {
		if op.Kind == PatchRemoveElement {
			foundRemove = true
		}
	}
	if !foundRemove {
		t.Fatal("expected RemoveElement for removed keyed item")
	}
}

func TestReconcileKeyedStableUpdate(t *testing.T) {
	// Same keys, text changes — should emit SetText, NOT recreate
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2}},
		{Tag: "li", Key: "a", Text: "old-a"},
		{Tag: "li", Key: "b", Text: "old-b"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "ul", Children: []int{1, 2}},
		{Tag: "li", Key: "a", Text: "new-a"},
		{Tag: "li", Key: "b", Text: "new-b"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false, false})

	if len(ops) != 2 {
		t.Fatalf("expected 2 SetText ops, got %d: %+v", len(ops), ops)
	}
	for _, op := range ops {
		if op.Kind != PatchSetText {
			t.Fatalf("expected SetText, got kind %d", op.Kind)
		}
	}
}

func TestReconcileNoKeysUnchanged(t *testing.T) {
	// Positional (no keys) — existing behavior should be preserved
	prev := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1}},
		{Tag: "span", Text: "same"},
	}}
	next := &ResolvedTree{Nodes: []ResolvedNode{
		{Tag: "div", Children: []int{1}},
		{Tag: "span", Text: "same"},
	}}
	ops := ReconcileTrees(prev, next, []bool{false, false})
	if len(ops) != 0 {
		t.Fatalf("expected 0 ops for unchanged tree, got %d", len(ops))
	}
}
