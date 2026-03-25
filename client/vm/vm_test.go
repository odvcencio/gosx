package vm

import (
	"testing"

	"github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

// helper to build a minimal program from expressions.
func progFromExprs(exprs []program.Expr) *program.Program {
	return &program.Program{
		Name:  "test",
		Exprs: exprs,
	}
}

// --- Literal evaluation ---

func TestVMLitInt(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(0)
	if v.Type != program.TypeInt {
		t.Fatalf("expected TypeInt, got %d", v.Type)
	}
	if v.Num != 42 {
		t.Fatalf("expected 42, got %f", v.Num)
	}
}

func TestVMLitString(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "hello", Type: program.TypeString},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(0)
	if v.Type != program.TypeString {
		t.Fatalf("expected TypeString, got %d", v.Type)
	}
	if v.Str != "hello" {
		t.Fatalf("expected 'hello', got %q", v.Str)
	}
}

func TestVMLitFloat(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitFloat, Value: "3.14", Type: program.TypeFloat},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(0)
	if v.Type != program.TypeFloat {
		t.Fatalf("expected TypeFloat, got %d", v.Type)
	}
	if v.Num != 3.14 {
		t.Fatalf("expected 3.14, got %f", v.Num)
	}
}

func TestVMLitBool(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
		{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)

	v := vm.Eval(0)
	if !v.Bool {
		t.Fatal("expected true")
	}

	v2 := vm.Eval(1)
	if v2.Bool {
		t.Fatal("expected false")
	}
}

// --- Arithmetic ---

func TestVMAdd(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpAdd, Operands: []program.ExprID{0, 1}, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Num != 13 {
		t.Fatalf("expected 13, got %f", v.Num)
	}
}

func TestVMSub(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpSub, Operands: []program.ExprID{0, 1}, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Num != 7 {
		t.Fatalf("expected 7, got %f", v.Num)
	}
}

func TestVMMul(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "4", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpMul, Operands: []program.ExprID{0, 1}, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Num != 20 {
		t.Fatalf("expected 20, got %f", v.Num)
	}
}

func TestVMDiv(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpDiv, Operands: []program.ExprID{0, 1}, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Num != 3 {
		t.Fatalf("expected 3 (integer division), got %f", v.Num)
	}
}

func TestVMMod(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpMod, Operands: []program.ExprID{0, 1}, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Num != 1 {
		t.Fatalf("expected 1, got %f", v.Num)
	}
}

func TestVMNeg(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpNeg, Operands: []program.ExprID{0}, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(1)
	if v.Num != -5 {
		t.Fatalf("expected -5, got %f", v.Num)
	}
}

// --- Comparisons ---

func TestVMEq(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpEq, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("5 == 5 should be true")
	}
}

func TestVMNeq(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "6", Type: program.TypeInt},
		{Op: program.OpNeq, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("5 != 6 should be true")
	}
}

func TestVMLt(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLt, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("3 < 5 should be true")
	}
}

func TestVMGt(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpGt, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("5 > 3 should be true")
	}
}

func TestVMLte(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLte, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("5 <= 5 should be true")
	}
}

func TestVMGte(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
		{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},
		{Op: program.OpGte, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("5 >= 3 should be true")
	}
}

// --- Boolean logic ---

func TestVMAnd(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
		{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},
		{Op: program.OpAnd, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Bool {
		t.Fatal("true && false should be false")
	}
}

func TestVMOr(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
		{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},
		{Op: program.OpOr, Operands: []program.ExprID{0, 1}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("true || false should be true")
	}
}

func TestVMNot(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
		{Op: program.OpNot, Operands: []program.ExprID{0}, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(1)
	if v.Bool {
		t.Fatal("!true should be false")
	}
}

// --- Prop access ---

func TestVMPropGet(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "title", Type: program.TypeString},
	})
	props := map[string]Value{
		"title": StringVal("Hello World"),
	}
	vm := NewVM(prog, props)
	v := vm.Eval(0)
	if v.Str != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", v.Str)
	}
}

func TestVMPropGetMissing(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "missing", Type: program.TypeString},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(0)
	if v.Type != program.TypeString {
		t.Fatalf("expected TypeString zero value, got type %d", v.Type)
	}
	if v.Str != "" {
		t.Fatalf("expected empty string, got %q", v.Str)
	}
}

// --- Signal access ---

func TestVMSignalGet(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	sig := signal.New[Value](IntVal(5))
	vm.SetSignal("count", sig)

	v := vm.Eval(0)
	if v.Num != 5 {
		t.Fatalf("expected 5, got %f", v.Num)
	}
}

func TestVMSignalSet(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},
		{Op: program.OpSignalSet, Operands: []program.ExprID{0}, Value: "count", Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)
	sig := signal.New[Value](IntVal(0))
	vm.SetSignal("count", sig)

	vm.Eval(1) // execute the set
	v := sig.Get()
	if v.Num != 42 {
		t.Fatalf("expected signal to be 42, got %f", v.Num)
	}
}

// --- Conditional ---

func TestVMCondTrue(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
		{Op: program.OpLitString, Value: "yes", Type: program.TypeString},
		{Op: program.OpLitString, Value: "no", Type: program.TypeString},
		{Op: program.OpCond, Operands: []program.ExprID{0, 1, 2}, Type: program.TypeString},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(3)
	if v.Str != "yes" {
		t.Fatalf("expected 'yes', got %q", v.Str)
	}
}

func TestVMCondFalse(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitBool, Value: "false", Type: program.TypeBool},
		{Op: program.OpLitString, Value: "yes", Type: program.TypeString},
		{Op: program.OpLitString, Value: "no", Type: program.TypeString},
		{Op: program.OpCond, Operands: []program.ExprID{0, 1, 2}, Type: program.TypeString},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(3)
	if v.Str != "no" {
		t.Fatalf("expected 'no', got %q", v.Str)
	}
}

// --- String ops ---

func TestVMConcat(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "hello", Type: program.TypeString},
		{Op: program.OpLitString, Value: " world", Type: program.TypeString},
		{Op: program.OpConcat, Operands: []program.ExprID{0, 1}, Type: program.TypeString},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Str != "hello world" {
		t.Fatalf("expected 'hello world', got %q", v.Str)
	}
}

// --- Counter program evaluation ---

func TestVMCounterProgram(t *testing.T) {
	prog := program.CounterProgram()
	vm := NewVM(prog, nil)

	// Create and register a signal for "count" with initial value 5.
	countSig := signal.New[Value](IntVal(5))
	vm.SetSignal("count", countSig)

	// Evaluate expr[0] (SignalGet "count") — should return 5.
	v := vm.Eval(0)
	if v.Num != 5 {
		t.Fatalf("expected count=5, got %f", v.Num)
	}

	// Dispatch decrement: evaluate expr[2] (SignalSet "count" <- expr[4])
	// expr[4] = Sub(expr[6], expr[7]) = Sub(SignalGet("count"), LitInt("1")) = 5 - 1 = 4
	vm.Eval(2)
	v = countSig.Get()
	if v.Num != 4 {
		t.Fatalf("expected count=4 after decrement, got %f", v.Num)
	}

	// Dispatch increment: evaluate expr[3] (SignalSet "count" <- expr[5])
	// expr[5] = Add(expr[8], expr[9]) = Add(SignalGet("count"), LitInt("1")) = 4 + 1 = 5
	vm.Eval(3)
	v = countSig.Get()
	if v.Num != 5 {
		t.Fatalf("expected count=5 after increment, got %f", v.Num)
	}
}

// --- EvalTree ---

func TestVMEvalTree(t *testing.T) {
	prog := program.CounterProgram()
	vm := NewVM(prog, nil)

	countSig := signal.New[Value](IntVal(7))
	vm.SetSignal("count", countSig)

	tree := vm.EvalTree()

	if len(tree.Nodes) != len(prog.Nodes) {
		t.Fatalf("expected %d nodes, got %d", len(prog.Nodes), len(tree.Nodes))
	}

	// Node 0: div with class="counter"
	n0 := tree.Nodes[0]
	if n0.Tag != "div" {
		t.Fatalf("node 0 tag: expected 'div', got %q", n0.Tag)
	}
	if len(n0.Attrs) != 1 || n0.Attrs[0].Name != "class" || n0.Attrs[0].Value != "counter" {
		t.Fatalf("node 0 attrs: expected class=counter, got %+v", n0.Attrs)
	}
	if len(n0.Children) != 3 {
		t.Fatalf("node 0 children: expected 3, got %d", len(n0.Children))
	}

	// Node 1: button (decrement) — event attrs should be excluded
	n1 := tree.Nodes[1]
	if n1.Tag != "button" {
		t.Fatalf("node 1 tag: expected 'button', got %q", n1.Tag)
	}
	if len(n1.Attrs) != 0 {
		t.Fatalf("node 1 attrs: expected 0 (event attrs excluded), got %d", len(n1.Attrs))
	}

	// Node 2: expr node showing count — should resolve to "7"
	n2 := tree.Nodes[2]
	if n2.Text != "7" {
		t.Fatalf("node 2 text: expected '7', got %q", n2.Text)
	}

	// Node 4: text "-"
	n4 := tree.Nodes[4]
	if n4.Text != "-" {
		t.Fatalf("node 4 text: expected '-', got %q", n4.Text)
	}

	// Node 5: text "+"
	n5 := tree.Nodes[5]
	if n5.Text != "+" {
		t.Fatalf("node 5 text: expected '+', got %q", n5.Text)
	}
}

// --- Error resilience ---

func TestVMOutOfBoundsExprID(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)

	// Evaluate an expr ID that is beyond the expression table.
	v := vm.Eval(999)
	if v.Type != program.TypeAny {
		t.Fatalf("expected TypeAny zero value for OOB, got type %d", v.Type)
	}
}

func TestVMMissingSignalReturnsZero(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpSignalGet, Value: "missing", Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)

	v := vm.Eval(0)
	if v.Type != program.TypeInt {
		t.Fatalf("expected TypeInt zero value, got type %d", v.Type)
	}
	if v.Num != 0 {
		t.Fatalf("expected 0, got %f", v.Num)
	}
}

func TestVMBinaryWithMissingOperands(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpAdd, Operands: nil, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)

	// Should return zero value, not panic.
	v := vm.Eval(0)
	if v.Type != program.TypeAny {
		t.Fatalf("expected TypeAny zero value, got type %d", v.Type)
	}
}

func TestVMNegWithMissingOperand(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpNeg, Operands: nil, Type: program.TypeInt},
	})
	vm := NewVM(prog, nil)

	v := vm.Eval(0)
	if v.Type != program.TypeInt {
		t.Fatalf("expected TypeInt zero value, got type %d", v.Type)
	}
}

func TestVMNotWithMissingOperand(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpNot, Operands: nil, Type: program.TypeBool},
	})
	vm := NewVM(prog, nil)

	v := vm.Eval(0)
	if v.Bool {
		t.Fatal("expected false for Not with no operand")
	}
}

func TestVMCondWithInsufficientOperands(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpCond, Operands: []program.ExprID{}, Type: program.TypeAny},
	})
	vm := NewVM(prog, nil)

	v := vm.Eval(0)
	if v.Type != program.TypeAny {
		t.Fatalf("expected TypeAny zero value, got type %d", v.Type)
	}
}

func TestVMSignalSetMissing(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
		{Op: program.OpSignalSet, Operands: []program.ExprID{0}, Value: "missing", Type: program.TypeAny},
	})
	vm := NewVM(prog, nil)

	// Should not panic when signal is missing.
	v := vm.Eval(1)
	if v.Type != program.TypeAny {
		t.Fatalf("expected TypeAny zero value, got type %d", v.Type)
	}
}
