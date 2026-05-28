// Slice Y.G — ClosureVal value-kind tests.
//
// ClosureVal wraps a synthetic FuncDef name + caller-frame reference so
// OpIndirectCall can dispatch into a closure body with the enclosing
// scope's captured locals visible BY REFERENCE. The runtime evaluator
// lives in closure.go (Y.G.4); this file pins the Value-shape contract
// the lowerer and dispatcher both depend on.

package vm

import "testing"

// TestClosureValBasicShape pins ClosureVal's constructor: the returned
// Value reports as a closure via IsClosure, ClosureFuncName returns the
// supplied synthetic name, and non-closure Values fail both probes.
func TestClosureValBasicShape(t *testing.T) {
	f := newFrame()
	f.declare("y")
	f.set("y", IntVal(7))

	cv := ClosureVal("__y_g_funclit_42", []string{"y"}, f)
	if !IsClosure(cv) {
		t.Errorf("ClosureVal() should report IsClosure(true)")
	}
	if got := ClosureFuncName(cv); got != "__y_g_funclit_42" {
		t.Errorf("ClosureFuncName = %q, want %q", got, "__y_g_funclit_42")
	}

	// Scalar Values must NOT report as closures (a regression in the
	// closure detection would silently route every IntVal into
	// closure dispatch).
	if IsClosure(IntVal(5)) {
		t.Errorf("IntVal(5) reported as a closure")
	}
	if IsClosure(StringVal("hi")) {
		t.Errorf("StringVal reported as a closure")
	}
	if IsClosure(ObjectVal(map[string]Value{"x": IntVal(1)})) {
		t.Errorf("ObjectVal reported as a closure")
	}
}

// TestClosureValCaptureByReference verifies that mutating the captured
// frame AFTER the ClosureVal is built remains visible through the
// closure's internal frame pointer — the foundation of Go's
// variable-capture semantics.
func TestClosureValCaptureByReference(t *testing.T) {
	f := newFrame()
	f.declare("y")
	f.set("y", IntVal(10))

	cv := ClosureVal("__y_g_funclit_1", []string{"y"}, f)

	// Mutate via the original frame; the closure's frame pointer
	// must see the update.
	f.set("y", IntVal(20))

	got, ok := cv.closure.frame.get("y")
	if !ok {
		t.Fatalf("captured frame does not have y")
	}
	if int(got.Num) != 20 {
		t.Errorf("captured y = %v, want 20 (capture-by-reference broken)", got.Num)
	}
	if !cv.closure.captured["y"] {
		t.Errorf("captured set does not record y")
	}
}

// TestClosureValDoesNotMarshalThroughToAny pins that the unexported
// closure field is invisible to ToAny / JSON marshaling — closures
// never cross the wire, only their (lack of) JSON representation
// matters.
func TestClosureValDoesNotMarshalThroughToAny(t *testing.T) {
	cv := ClosureVal("__y_g_funclit_1", nil, newFrame())
	raw := cv.ToAny()
	// Default zero ToAny for a TypeAny with no Items/Fields is the
	// numeric zero — we just want to confirm no panic and no closure
	// internals leak.
	if _, isMap := raw.(map[string]any); isMap {
		t.Errorf("ClosureVal.ToAny() leaked the closure as a map: %v", raw)
	}
}
