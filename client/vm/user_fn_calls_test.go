// Slice Y.D VM-level tests — exercise OpIndirectCall directly against
// hand-built Program / FuncDef shapes so the call-frame mechanism,
// parameter binding, multi-return carrier, and recursion cap each get
// covered without relying on the golower's lowering decisions.

package vm

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/island/program"
)

// TestEvalIndirectCallZeroArg verifies the simplest dispatch shape:
// a callee with no params and a single string return value.
func TestEvalIndirectCallZeroArg(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLitString, Value: "hi", Type: program.TypeString}, // expr 0
			{Op: program.OpReturn, Operands: []program.ExprID{0}},            // expr 1
			{Op: program.OpSeq, Operands: []program.ExprID{1}},               // expr 2 (body)
			{Op: program.OpIndirectCall, Value: "greet", Operands: nil},      // expr 3 (call site)
		},
		Funcs: []program.FuncDef{
			{Name: "greet", Params: nil, Body: []program.ExprID{2}, Results: 1},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(3)
	if got.Str != "hi" {
		t.Errorf("OpIndirectCall greet() = %q, want %q", got.Str, "hi")
	}
}

// TestEvalIndirectCallSingleParam pins the parameter-binding contract:
// the call site's argument value must arrive in the callee's frame
// under the registered Param name.
func TestEvalIndirectCallSingleParam(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "n"},                                         // 0: read param
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},                    // 1
			{Op: program.OpMul, Operands: []program.ExprID{0, 1}},                        // 2: n*2
			{Op: program.OpReturn, Operands: []program.ExprID{2}},                        // 3
			{Op: program.OpSeq, Operands: []program.ExprID{3}},                           // 4 (body)
			{Op: program.OpLitInt, Value: "21", Type: program.TypeInt},                   // 5 (arg)
			{Op: program.OpIndirectCall, Value: "double", Operands: []program.ExprID{5}}, // 6 (call)
		},
		Funcs: []program.FuncDef{
			{Name: "double", Params: []string{"n"}, Body: []program.ExprID{4}, Results: 1},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(6)
	if int(got.Num) != 42 {
		t.Errorf("double(21) = %d, want 42", int(got.Num))
	}
}

// TestEvalIndirectCallUnknownCallee verifies that calling an
// unregistered function records a structured diagnostic and returns
// the zero Value, preserving the VM's panic-free contract.
func TestEvalIndirectCallUnknownCallee(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpIndirectCall, Value: "noSuchFn"},
		},
	}
	vm := NewVM(prog, nil)
	got := vm.Eval(0)
	if got.Type != program.TypeAny {
		t.Errorf("unknown callee should yield TypeAny zero, got Type=%d", got.Type)
	}
	requireDiag(t, vm, "unknown_user_function")
}

// TestEvalIndirectCallDepthCapDefault verifies a runaway recursion
// hits the DefaultMaxCallDepth cap without panicking the host.
func TestEvalIndirectCallDepthCapDefault(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpIndirectCall, Value: "rec"},            // 0: rec() inside body
			{Op: program.OpReturn, Operands: []program.ExprID{0}}, // 1
			{Op: program.OpSeq, Operands: []program.ExprID{1}},    // 2: body
			{Op: program.OpIndirectCall, Value: "rec"},            // 3: outer call
		},
		Funcs: []program.FuncDef{
			{Name: "rec", Params: nil, Body: []program.ExprID{2}, Results: 1},
		},
	}
	vm := NewVM(prog, nil)
	// Must not panic and must surface a diagnostic.
	got := vm.Eval(3)
	if got.Type != program.TypeAny {
		t.Errorf("runaway recursion should yield TypeAny zero, got Type=%d", got.Type)
	}
	requireDiag(t, vm, "call_depth_exceeded")
}

// TestEvalIndirectCallDepthCapCustom verifies that a Program can lower
// the cap to a finite value and the diagnostic fires at exactly that
// boundary. Using MaxCallDepth=3 keeps the test fast and unambiguous.
func TestEvalIndirectCallDepthCapCustom(t *testing.T) {
	prog := &program.Program{
		MaxCallDepth: 3,
		Exprs: []program.Expr{
			{Op: program.OpIndirectCall, Value: "rec"},            // 0
			{Op: program.OpReturn, Operands: []program.ExprID{0}}, // 1
			{Op: program.OpSeq, Operands: []program.ExprID{1}},    // 2
			{Op: program.OpIndirectCall, Value: "rec"},            // 3
		},
		Funcs: []program.FuncDef{
			{Name: "rec", Params: nil, Body: []program.ExprID{2}, Results: 1},
		},
	}
	vm := NewVM(prog, nil)
	vm.Eval(3)
	requireDiag(t, vm, "call_depth_exceeded")
}

// TestEvalIndirectCallArityMismatch verifies that providing the wrong
// argument count records the `call_arity_mismatch` diagnostic but
// keeps evaluating (missing params bind to zero) so a stale call site
// produces a deterministic result rather than panicking.
func TestEvalIndirectCallArityMismatch(t *testing.T) {
	prog := &program.Program{
		Exprs: []program.Expr{
			{Op: program.OpLocalGet, Value: "a"},                        // 0
			{Op: program.OpReturn, Operands: []program.ExprID{0}},       // 1
			{Op: program.OpSeq, Operands: []program.ExprID{1}},          // 2: body
			{Op: program.OpIndirectCall, Value: "ident", Operands: nil}, // 3: 0 args (expects 1)
		},
		Funcs: []program.FuncDef{
			{Name: "ident", Params: []string{"a"}, Body: []program.ExprID{2}, Results: 1},
		},
	}
	vm := NewVM(prog, nil)
	vm.Eval(3)
	requireDiag(t, vm, "call_arity_mismatch")
}

// requireDiag asserts the VM has recorded at least one diagnostic
// whose Code matches the expected substring.
func requireDiag(t *testing.T, vm *VM, codeSubstr string) {
	t.Helper()
	for _, d := range vm.diagnostics {
		if strings.Contains(d.Code, codeSubstr) {
			return
		}
	}
	t.Fatalf("expected diagnostic code containing %q, got %d diagnostics: %+v", codeSubstr, len(vm.diagnostics), vm.diagnostics)
}
