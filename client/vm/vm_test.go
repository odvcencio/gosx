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

func TestVMAddConcatsStrings(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "badge ", Type: program.TypeString},
		{Op: program.OpLitString, Value: "tone-success", Type: program.TypeString},
		{Op: program.OpAdd, Operands: []program.ExprID{0, 1}, Type: program.TypeAny},
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if v.Type != program.TypeString {
		t.Fatalf("expected TypeString, got %d", v.Type)
	}
	if v.Str != "badge tone-success" {
		t.Fatalf("expected concatenated string, got %q", v.Str)
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

func TestVMEvalTreeCachesDOMAttrsForEvents(t *testing.T) {
	prog := &program.Program{
		Name: "Events",
		Nodes: []program.Node{
			{
				Kind: program.NodeElement,
				Tag:  "button",
				Attrs: []program.Attr{
					{Kind: program.AttrEvent, Name: "click", Event: "increment"},
				},
				Children: []program.NodeID{1},
			},
			{Kind: program.NodeText, Text: "+"},
		},
		Root: 0,
	}

	vm := NewVM(prog, nil)
	tree := vm.EvalTree()
	if len(tree.Nodes) == 0 {
		t.Fatal("expected resolved tree nodes")
	}

	node := tree.Nodes[0]
	if len(node.DOMAttrs) != 2 {
		t.Fatalf("expected 2 DOM attrs for click handler, got %d", len(node.DOMAttrs))
	}
	if node.DOMAttrs[0].Name != "data-gosx-on-click" || node.DOMAttrs[0].Value != "increment" {
		t.Fatalf("unexpected delegated handler attr: %+v", node.DOMAttrs[0])
	}
	if node.DOMAttrs[1].Name != "data-gosx-handler" || node.DOMAttrs[1].Value != "increment" {
		t.Fatalf("unexpected click shorthand attr: %+v", node.DOMAttrs[1])
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

	// First child: button (decrement) — event attrs should be excluded
	n1 := tree.Nodes[n0.Children[0]]
	if n1.Tag != "button" {
		t.Fatalf("first child tag: expected 'button', got %q", n1.Tag)
	}
	if len(n1.Attrs) != 0 {
		t.Fatalf("first child attrs: expected 0 (event attrs excluded), got %d", len(n1.Attrs))
	}

	// Middle child: expr node showing count — should resolve to "7"
	n2 := tree.Nodes[n0.Children[1]]
	if n2.Text != "7" {
		t.Fatalf("middle child text: expected '7', got %q", n2.Text)
	}

	// First button child: text "-"
	n4 := tree.Nodes[n1.Children[0]]
	if n4.Text != "-" {
		t.Fatalf("first button text: expected '-', got %q", n4.Text)
	}

	// Second button child: text "+"
	n3 := tree.Nodes[n0.Children[2]]
	n5 := tree.Nodes[n3.Children[0]]
	if n5.Text != "+" {
		t.Fatalf("second button text: expected '+', got %q", n5.Text)
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

// --- OpMap ---

func TestVMOpMap(t *testing.T) {
	// Build an array [1, 2, 3] via props, then map: _item * 2
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "nums", Type: program.TypeAny},                // 0: the array
		{Op: program.OpPropGet, Value: "_item", Type: program.TypeInt},               // 1: current item
		{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},                    // 2: literal 2
		{Op: program.OpMul, Operands: []program.ExprID{1, 2}, Type: program.TypeInt}, // 3: _item * 2
		{Op: program.OpMap, Operands: []program.ExprID{0, 3}, Type: program.TypeAny}, // 4: map(nums, _item*2)
	})
	props := map[string]Value{
		"nums": ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3)}),
	}
	vm := NewVM(prog, props)
	v := vm.Eval(4)
	if len(v.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(v.Items))
	}
	if v.Items[0].Num != 2 || v.Items[1].Num != 4 || v.Items[2].Num != 6 {
		t.Fatalf("expected [2, 4, 6], got [%f, %f, %f]", v.Items[0].Num, v.Items[1].Num, v.Items[2].Num)
	}
}

// --- OpFilter ---

func TestVMOpFilter(t *testing.T) {
	// Filter [1, 2, 3, 4] keeping even: _item % 2 == 0
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "nums", Type: program.TypeAny},                   // 0: array
		{Op: program.OpPropGet, Value: "_item", Type: program.TypeInt},                  // 1: _item
		{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},                       // 2: literal 2
		{Op: program.OpMod, Operands: []program.ExprID{1, 2}, Type: program.TypeInt},    // 3: _item % 2
		{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},                       // 4: literal 0
		{Op: program.OpEq, Operands: []program.ExprID{3, 4}, Type: program.TypeBool},    // 5: _item%2 == 0
		{Op: program.OpFilter, Operands: []program.ExprID{0, 5}, Type: program.TypeAny}, // 6: filter
	})
	props := map[string]Value{
		"nums": ArrayVal([]Value{IntVal(1), IntVal(2), IntVal(3), IntVal(4)}),
	}
	vm := NewVM(prog, props)
	v := vm.Eval(6)
	if len(v.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(v.Items))
	}
	if v.Items[0].Num != 2 || v.Items[1].Num != 4 {
		t.Fatalf("expected [2, 4], got [%f, %f]", v.Items[0].Num, v.Items[1].Num)
	}
}

// --- OpContains (string) ---

func TestVMOpContainsString(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "hello world", Type: program.TypeString},          // 0
		{Op: program.OpLitString, Value: "world", Type: program.TypeString},                // 1
		{Op: program.OpContains, Operands: []program.ExprID{0, 1}, Type: program.TypeBool}, // 2
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(2)
	if !v.Bool {
		t.Fatal("expected 'hello world' to contain 'world'")
	}
}

// --- OpToUpper ---

func TestVMOpToUpper(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "hello", Type: program.TypeString},              // 0
		{Op: program.OpToUpper, Operands: []program.ExprID{0}, Type: program.TypeString}, // 1
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(1)
	if v.Str != "HELLO" {
		t.Fatalf("expected 'HELLO', got %q", v.Str)
	}
}

func TestVMOpIndexObjectField(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "user", Type: program.TypeAny},                  // 0
		{Op: program.OpLitString, Value: "Name", Type: program.TypeString},             // 1
		{Op: program.OpIndex, Operands: []program.ExprID{0, 1}, Type: program.TypeAny}, // 2
	})
	vm := NewVM(prog, map[string]Value{
		"user": ObjectVal(map[string]Value{"Name": StringVal("Ada")}),
	})

	v := vm.Eval(2)
	if v.Str != "Ada" {
		t.Fatalf("expected Ada, got %q", v.Str)
	}
}

func TestVMOpIndexArray(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpPropGet, Value: "items", Type: program.TypeAny},                 // 0
		{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},                      // 1
		{Op: program.OpIndex, Operands: []program.ExprID{0, 1}, Type: program.TypeAny}, // 2
	})
	vm := NewVM(prog, map[string]Value{
		"items": ArrayVal([]Value{StringVal("a"), StringVal("b")}),
	})

	v := vm.Eval(2)
	if v.Str != "b" {
		t.Fatalf("expected b, got %q", v.Str)
	}
}

// --- OpSplit / OpJoin round-trip ---

func TestVMOpSplitJoinRoundTrip(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "a,b,c", Type: program.TypeString},                       // 0
		{Op: program.OpSplit, Operands: []program.ExprID{0}, Value: ",", Type: program.TypeAny},   // 1: split by ","
		{Op: program.OpJoin, Operands: []program.ExprID{1}, Value: ",", Type: program.TypeString}, // 2: join by ","
	})
	vm := NewVM(prog, nil)

	// Check split
	split := vm.Eval(1)
	if len(split.Items) != 3 {
		t.Fatalf("expected 3 items from split, got %d", len(split.Items))
	}

	// Check round-trip
	joined := vm.Eval(2)
	if joined.Str != "a,b,c" {
		t.Fatalf("expected 'a,b,c' after round-trip, got %q", joined.Str)
	}
}

func TestVMOpSplitJoinDynamicSeparator(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitString, Value: "a|b|c", Type: program.TypeString},              // 0
		{Op: program.OpLitString, Value: "|", Type: program.TypeString},                  // 1
		{Op: program.OpSplit, Operands: []program.ExprID{0, 1}, Type: program.TypeAny},   // 2
		{Op: program.OpLitString, Value: "::", Type: program.TypeString},                 // 3
		{Op: program.OpJoin, Operands: []program.ExprID{2, 3}, Type: program.TypeString}, // 4
	})
	vm := NewVM(prog, nil)

	if split := vm.Eval(2); len(split.Items) != 3 {
		t.Fatalf("expected 3 items from split, got %d", len(split.Items))
	}
	if joined := vm.Eval(4); joined.Str != "a::b::c" {
		t.Fatalf("expected a::b::c, got %q", joined.Str)
	}
}

// --- OpToString on int ---

func TestVMOpToStringInt(t *testing.T) {
	prog := progFromExprs([]program.Expr{
		{Op: program.OpLitInt, Value: "42", Type: program.TypeInt},                        // 0
		{Op: program.OpToString, Operands: []program.ExprID{0}, Type: program.TypeString}, // 1
	})
	vm := NewVM(prog, nil)
	v := vm.Eval(1)
	if v.Type != program.TypeString {
		t.Fatalf("expected TypeString, got %d", v.Type)
	}
	if v.Str != "42" {
		t.Fatalf("expected '42', got %q", v.Str)
	}
}

// --- Missing operand resilience for new opcodes ---

func TestVMNewOpcodesMissingOperands(t *testing.T) {
	// All new opcodes should return zero values when operands are missing.
	ops := []program.OpCode{
		program.OpMap, program.OpFilter, program.OpFind, program.OpSlice,
		program.OpAppend, program.OpContains,
		program.OpToUpper, program.OpToLower, program.OpTrim,
		program.OpSplit, program.OpJoin,
		program.OpReplace, program.OpSubstring,
		program.OpStartsWith, program.OpEndsWith,
		program.OpToString, program.OpToInt, program.OpToFloat,
	}
	for _, op := range ops {
		prog := progFromExprs([]program.Expr{
			{Op: op, Operands: nil, Type: program.TypeAny},
		})
		vm := NewVM(prog, nil)
		// Should not panic
		vm.Eval(0)
	}
}
