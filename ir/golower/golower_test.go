package golower

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// TestLowerTrivialFunction is the spec test from X.C.1: lowering
// `func F() int { return 42 }` produces a Program whose single Handler
// evaluates to IntVal(42).
func TestLowerTrivialFunction(t *testing.T) {
	src := []byte(`package handlers

func F() int { return 42 }`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	if len(prog.Handlers) != 1 {
		t.Fatalf("handlers = %d, want 1", len(prog.Handlers))
	}
	if prog.Handlers[0].Name != "F" {
		t.Errorf("handler name = %q, want F", prog.Handlers[0].Name)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if got.Type != program.TypeInt || int(got.Num) != 42 {
		t.Errorf("F() = %+v, want IntVal(42)", got)
	}
}

// TestLowerArithmeticReturn covers binary ops + literals.
func TestLowerArithmeticReturn(t *testing.T) {
	src := []byte(`package handlers

func F() int { return 2 * 3 + 1 }`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if int(got.Num) != 7 {
		t.Errorf("F() = %f, want 7", got.Num)
	}
}

// TestLowerLocalAndAssign verifies `:=` then `=` then return.
func TestLowerLocalAndAssign(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	x := 5
	x = x + 10
	return x
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if int(got.Num) != 15 {
		t.Errorf("F() = %f, want 15", got.Num)
	}
}

// TestLowerIfElse exercises the OpCond chain.
func TestLowerIfElse(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	x := 10
	if x > 5 {
		return 1
	}
	return 0
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if int(got.Num) != 1 {
		t.Errorf("F() = %f, want 1", got.Num)
	}
}

// TestLowerForLoop verifies a 3-clause for loop accumulates correctly.
func TestLowerForLoop(t *testing.T) {
	src := []byte(`package handlers

func Sum() int {
	s := 0
	for i := 0; i < 5; i = i + 1 {
		s = s + i
	}
	return s
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if int(got.Num) != 10 { // 0+1+2+3+4
		t.Errorf("Sum() = %f, want 10", got.Num)
	}
}

// TestLowerIncDec verifies that `i++` and `i--` work.
func TestLowerIncDec(t *testing.T) {
	src := []byte(`package handlers

func Count() int {
	n := 0
	for i := 0; i < 3; i++ {
		n++
	}
	return n
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if int(got.Num) != 3 {
		t.Errorf("Count() = %f, want 3", got.Num)
	}
}

// TestLowerIntrinsicCall confirms math.Sqrt(16) lowers to OpCall and
// returns 4.
func TestLowerIntrinsicCall(t *testing.T) {
	src := []byte(`package handlers

import "math"

func F() float64 { return math.Sqrt(16) }`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if got.Num != 4 {
		t.Errorf("F() = %f, want 4", got.Num)
	}
}

// TestLowerMathPi confirms the constant-intrinsic special-case fires.
func TestLowerMathPi(t *testing.T) {
	src := []byte(`package handlers

import "math"

func F() float64 { return math.Pi }`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if got.Num < 3.14 || got.Num > 3.15 {
		t.Errorf("math.Pi = %f, want ~3.14159", got.Num)
	}
}

// TestLowerPackageVarBecomesSignal — `var n = 42` at the package level
// shows up as a SignalDef.
func TestLowerPackageVarBecomesSignal(t *testing.T) {
	src := []byte(`package handlers

var counter = 42`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	if len(prog.Signals) != 1 {
		t.Fatalf("signals = %d, want 1", len(prog.Signals))
	}
	if prog.Signals[0].Name != "counter" {
		t.Errorf("signal name = %q, want counter", prog.Signals[0].Name)
	}
}

// TestLowerUnsupportedStatementIssue ensures unsupported constructs
// surface with the escape-hatch suggestion.
func TestLowerUnsupportedStatementIssue(t *testing.T) {
	src := []byte(`package handlers

func F() { go doStuff() }`)
	_, err := LowerFile(src)
	if err == nil {
		t.Fatal("expected unsupported-statement error")
	}
	if !strings.Contains(err.Error(), "ADR 0006") {
		t.Errorf("error should mention ADR 0006: %v", err)
	}
}

// TestLowerRangeOverArray verifies a range loop sums element values.
func TestLowerRangeOverArray(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	xs := items
	sum := 0
	for _, v := range xs {
		sum = sum + v
	}
	return sum
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, map[string]vm.Value{
		"items": vm.ArrayVal([]vm.Value{vm.IntVal(10), vm.IntVal(20), vm.IntVal(30)}),
	})
	got := machine.EvalWithFrame(prog.Handlers[0].Body[0])
	if int(got.Num) != 60 {
		t.Errorf("F() = %f, want 60", got.Num)
	}
}
