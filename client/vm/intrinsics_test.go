// Registry-level tests for the stdlib intrinsic system (Slice X.B).
// The per-family tests (intrinsics_math_test.go etc.) cover individual
// function behavior; this file covers the registry contract and the
// OpCall dispatch fast-path.

package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestRegistryLookupRoundTrip(t *testing.T) {
	r := &IntrinsicRegistry{byName: map[string]Intrinsic{}}
	r.Register("test.Double", func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, nil
		}
		return IntVal(int(args[0].Num) * 2), nil
	})

	fn, ok := r.Lookup("test.Double")
	if !ok {
		t.Fatalf("expected test.Double to be registered")
	}
	v, err := fn([]Value{IntVal(7)})
	if err != nil {
		t.Fatalf("intrinsic err: %v", err)
	}
	if int(v.Num) != 14 {
		t.Fatalf("test.Double(7) = %f, want 14", v.Num)
	}
}

func TestRegistryLookupMissing(t *testing.T) {
	r := &IntrinsicRegistry{byName: map[string]Intrinsic{}}
	if _, ok := r.Lookup("not.registered"); ok {
		t.Fatalf("expected lookup miss")
	}
}

func TestOpCallDispatchesIntrinsic(t *testing.T) {
	// math.Sin(0) → 0.0 via OpCall.
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat},
		{Op: program.OpCall, Value: "math.Sin", Operands: []program.ExprID{0}},
	})
	vm := NewVM(prog, nil)
	got := vm.Eval(1)
	if got.Type != program.TypeFloat || got.Num != 0 {
		t.Fatalf("math.Sin(0) via OpCall = %+v, want FloatVal(0)", got)
	}
}

func TestOpCallUnknownCalleeStillReturnsZero(t *testing.T) {
	// Unknown callee: keeps the legacy zero-Value behavior so existing
	// programs that emit OpCall for placeholder reasons don't break.
	prog := progFromExprs([]program.Expr{
		{Op: program.OpCall, Value: "totally.NotARealFunc"},
	})
	vm := NewVM(prog, nil)
	got := vm.Eval(0)
	if got.Type != program.TypeAny {
		t.Fatalf("unknown OpCall = %+v, want zero-value Any", got)
	}
}

func TestOpCallIntrinsicErrorSurfacesAsDiagnostic(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "not-a-number", Type: program.TypeString},
		{Op: program.OpCall, Value: "strconv.Atoi", Operands: []program.ExprID{0}},
	})
	vm := NewVM(prog, nil)
	got, diags := vm.EvalWithDiagnostics(1)
	if got.Type != program.TypeAny {
		t.Fatalf("expected zero return on error, got %+v", got)
	}
	if len(diags) != 1 || diags[0].Code != "intrinsic_error" {
		t.Fatalf("expected intrinsic_error diagnostic, got %+v", diags)
	}
}

func TestIntrinsicNamesCoversAllFamilies(t *testing.T) {
	// Spot-check one function from each family to confirm init() ran.
	wantOneOfEach := []string{
		"math.Sin",
		"rand.Float64",
		"strings.Split",
		"strconv.Itoa",
		"sort.Slice",
	}
	names := IntrinsicNames()
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range wantOneOfEach {
		if !have[want] {
			t.Errorf("intrinsic %q not registered (have %v)", want, names)
		}
	}
}
