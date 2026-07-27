package vm

import (
	"fmt"
	"testing"
)

// TestAttrIndexScanAndMapAgree pins that the two lookup strategies answer
// alike. The scan path runs for a short list and the map path for a wide one,
// so a divergence would appear only on elements above attrScanLimit — exactly
// the shape no hand-written reconcile test happens to build.
func TestAttrIndexScanAndMapAgree(t *testing.T) {
	attrs := make([]ResolvedAttr, 0, attrScanLimit*2)
	for i := 0; i < attrScanLimit*2; i++ {
		attrs = append(attrs, ResolvedAttr{Name: fmt.Sprintf("a%d", i), Value: fmt.Sprintf("v%d", i)})
	}

	short := newAttrIndex(attrs[:attrScanLimit])
	wide := newAttrIndex(attrs)
	if short.byName != nil {
		t.Fatalf("a list of %d attributes must use the scan path", attrScanLimit)
	}
	if wide.byName == nil {
		t.Fatalf("a list of %d attributes must use the map path", len(attrs))
	}

	for i := range attrs {
		name := attrs[i].Name
		wantValue := attrs[i].Value
		gotValue, gotOK := wide.lookup(name)
		if !gotOK || gotValue != wantValue {
			t.Errorf("wide.lookup(%q) = %q, %v; want %q, true", name, gotValue, gotOK, wantValue)
		}
		if !wide.has(name) {
			t.Errorf("wide.has(%q) = false, want true", name)
		}
		inShort := i < attrScanLimit
		if got := short.has(name); got != inShort {
			t.Errorf("short.has(%q) = %v, want %v", name, got, inShort)
		}
	}

	if _, ok := wide.lookup("absent"); ok {
		t.Error("wide.lookup of a missing name reported present")
	}
	if _, ok := short.lookup("absent"); ok {
		t.Error("short.lookup of a missing name reported present")
	}
}

// TestAttrIndexDuplicateNameTakesTheLastValue pins the semantics the map build
// had. A map keeps the last value written for a repeated key, so the scan must
// search backwards. Searching forwards would answer with the first value and
// suppress a real attribute patch.
func TestAttrIndexDuplicateNameTakesTheLastValue(t *testing.T) {
	short := []ResolvedAttr{
		{Name: "class", Value: "first"},
		{Name: "id", Value: "x"},
		{Name: "class", Value: "last"},
	}
	if got, ok := newAttrIndex(short).lookup("class"); !ok || got != "last" {
		t.Errorf("scan lookup of a duplicated name = %q, %v; want %q, true", got, ok, "last")
	}

	wide := append([]ResolvedAttr{}, short...)
	for i := 0; i < attrScanLimit; i++ {
		wide = append(wide, ResolvedAttr{Name: fmt.Sprintf("pad%d", i)})
	}
	index := newAttrIndex(wide)
	if index.byName == nil {
		t.Fatal("the padded list must use the map path")
	}
	if got, ok := index.lookup("class"); !ok || got != "last" {
		t.Errorf("map lookup of a duplicated name = %q, %v; want %q, true", got, ok, "last")
	}
}

// TestReconcileWideAttrListEmitsOneSetOp drives the map path through the real
// reconciler, so the crossover is covered end to end and not only by the unit
// test above.
func TestReconcileWideAttrListEmitsOneSetOp(t *testing.T) {
	build := func(changed string) *ResolvedTree {
		attrs := make([]ResolvedAttr, 0, attrScanLimit+2)
		attrs = append(attrs, ResolvedAttr{Name: "class", Value: changed})
		for i := 0; i < attrScanLimit+1; i++ {
			attrs = append(attrs, ResolvedAttr{Name: fmt.Sprintf("data-%d", i), Value: "same"})
		}
		return &ResolvedTree{Nodes: []ResolvedNode{{Tag: "div", Attrs: attrs}}}
	}

	ops := ReconcileTrees(build("old"), build("new"), []bool{false})
	if len(ops) != 1 {
		t.Fatalf("ops = %#v, want exactly one SetAttr for the changed attribute", ops)
	}
	if ops[0].Kind != PatchSetAttr || ops[0].AttrName != "class" || ops[0].Text != "new" {
		t.Fatalf("ops[0] = %#v, want SetAttr class=new", ops[0])
	}
}

// TestAttrsAliasSkipsOnlyIdenticalArrays pins the fast path reconcileAttrs
// takes for a reused element. Two lists that merely hold equal values must not
// alias, or the check would be claiming more than the copy guarantees.
func TestAttrsAliasSkipsOnlyIdenticalArrays(t *testing.T) {
	shared := []ResolvedAttr{{Name: "class", Value: "a"}}
	copied := []ResolvedAttr{{Name: "class", Value: "a"}}

	if !attrsAlias(shared, shared) {
		t.Error("a list must alias itself")
	}
	if attrsAlias(shared, copied) {
		t.Error("two distinct arrays with equal contents must not report as aliased")
	}
	if !attrsAlias(nil, nil) {
		t.Error("two empty lists must report as aliased: neither carries an attribute")
	}
	if attrsAlias(shared, shared[:0]) {
		t.Error("lists of different lengths must not report as aliased")
	}
}

// TestReconcileAliasedAttrsEmitNoOps pins that the fast path produces the same
// answer as the slow one for a node whose attribute array was copied whole,
// which is what subtree reuse does.
func TestReconcileAliasedAttrsEmitNoOps(t *testing.T) {
	shared := []ResolvedAttr{{Name: "class", Value: "a"}, {Name: "id", Value: "b"}}
	prev := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "div", Attrs: shared}}}
	next := &ResolvedTree{Nodes: []ResolvedNode{{Tag: "div", Attrs: shared}}}

	if ops := ReconcileTrees(prev, next, []bool{false}); len(ops) != 0 {
		t.Fatalf("ops = %#v, want none for an element whose attribute array was copied", ops)
	}
}
