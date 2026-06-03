package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// progA: signal "count" init 3, expr 0 reads count, handler "bump" sets
// count = count + 1 (Add of count and lit 1).
func swapProgA() *program.Program {
	return &program.Program{
		Name: "A",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},        // 0
			{Op: program.OpLitInt, Value: "3", Type: program.TypeInt},               // 1 (init)
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},               // 2
			{Op: program.OpAdd, Operands: []program.ExprID{0, 2}, Type: program.TypeInt}, // 3
			{Op: program.OpSignalSet, Operands: []program.ExprID{3}, Value: "count", Type: program.TypeInt}, // 4
		},
		Nodes: []program.Node{
			{Kind: program.NodeExpr, Expr: program.ExprID(0)}, // 0: shows count
		},
		Root: 0,
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: program.ExprID(1)},
		},
		Handlers: []program.Handler{
			{Name: "bump", Body: []program.ExprID{4}},
		},
		StaticMask: []bool{false},
	}
}

// progB: SAME signal name "count" (init 99 — must be ignored on merge), but
// a NEW handler "bump" that sets count = count + 10. Init expr differs to
// prove merge-by-name keeps the live value rather than re-evaluating init.
func swapProgB() *program.Program {
	return &program.Program{
		Name: "B",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "count", Type: program.TypeInt},        // 0
			{Op: program.OpLitInt, Value: "99", Type: program.TypeInt},              // 1 (init — ignored on merge)
			{Op: program.OpLitInt, Value: "10", Type: program.TypeInt},              // 2 (new step)
			{Op: program.OpAdd, Operands: []program.ExprID{0, 2}, Type: program.TypeInt}, // 3
			{Op: program.OpSignalSet, Operands: []program.ExprID{3}, Value: "count", Type: program.TypeInt}, // 4
		},
		Nodes: []program.Node{
			{Kind: program.NodeExpr, Expr: program.ExprID(0)},
		},
		Root: 0,
		Signals: []program.SignalDef{
			{Name: "count", Type: program.TypeInt, Init: program.ExprID(1)},
		},
		Handlers: []program.Handler{
			{Name: "bump", Body: []program.ExprID{4}},
		},
		StaticMask: []bool{false},
	}
}

// progC: RENAMED signal "tally" init 0. The old "count" is gone, so swapping
// to C must clean re-init — no stale 3/value carried under the new name.
func swapProgC() *program.Program {
	return &program.Program{
		Name: "C",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "tally", Type: program.TypeInt}, // 0
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},        // 1 (init)
		},
		Nodes: []program.Node{
			{Kind: program.NodeExpr, Expr: program.ExprID(0)},
		},
		Root: 0,
		Signals: []program.SignalDef{
			{Name: "tally", Type: program.TypeInt, Init: program.ExprID(1)},
		},
		StaticMask: []bool{false},
	}
}

// TestVMSwapProgramMergesSignalByName builds a VM on program A, mutates the
// live signal, then swaps to B with the same signal name. The live value must
// survive (merge-by-name), the stale init must be ignored, and the new
// program's expressions/handler body must be the ones now evaluated.
func TestVMSwapProgramMergesSignalByName(t *testing.T) {
	vm := NewVM(swapProgA(), nil)
	InitSignals(vm, swapProgA())

	// count starts at 3 (A's init).
	if got := vm.Eval(0); got.Num != 3 {
		t.Fatalf("initial count = %v, want 3", got.Num)
	}
	// Run A's handler once: count 3 -> 4.
	vm.EvalWithFrame(4)
	if got := vm.signals["count"].Get(); got.Num != 4 {
		t.Fatalf("after A bump, count = %v, want 4", got.Num)
	}

	// Swap to B. Same signal name => live value 4 is kept, init 99 ignored.
	vm.SwapProgram(swapProgB())
	if got := vm.signals["count"].Get(); got.Num != 4 {
		t.Fatalf("after swap to B, count = %v, want 4 (merge-by-name kept live value)", got.Num)
	}
	// B's handler is now active: count = count + 10 => 14.
	vm.EvalWithFrame(4)
	if got := vm.signals["count"].Get(); got.Num != 14 {
		t.Fatalf("after B bump, count = %v, want 14 (new handler active)", got.Num)
	}
	// The new program is installed (exprs swapped).
	if vm.program.Name != "B" {
		t.Fatalf("program name = %q, want B", vm.program.Name)
	}
}

// TestIslandSwapProgramKeepsStateNewHandlerReconciles drives the swap through
// the Island the bridge owns: it must preserve signal state by name, activate
// the new program's handler, rebuild the handler map, and reconcile the DOM so
// the previous tree reflects the new program. This is the end-to-end seam the
// reload route depends on.
func TestIslandSwapProgramKeepsStateNewHandlerReconciles(t *testing.T) {
	island := NewIsland(swapProgA(), `{}`)
	island.Dispatch("bump", "{}") // count 3 -> 4 under A
	if got := island.vm.signals["count"].Get(); got.Num != 4 {
		t.Fatalf("pre-swap count = %v, want 4", got.Num)
	}

	island.SwapProgram(swapProgB())

	// Merge-by-name kept the live value across the swap.
	if got := island.vm.signals["count"].Get(); got.Num != 4 {
		t.Fatalf("post-swap count = %v, want 4 (state preserved)", got.Num)
	}
	// The handler map was rebuilt against B; the rendered tree reflects the
	// reconcile (display node shows the live value 4).
	if got := counterFirstNodeText(island.prev); got != "4" {
		t.Fatalf("reconciled display = %q, want \"4\"", got)
	}
	// B's handler body is the active one: count = count + 10 => 14.
	island.Dispatch("bump", "{}")
	if got := island.vm.signals["count"].Get(); got.Num != 14 {
		t.Fatalf("post-swap bump count = %v, want 14 (new handler active)", got.Num)
	}
	if got := counterFirstNodeText(island.prev); got != "14" {
		t.Fatalf("post-bump display = %q, want \"14\"", got)
	}
}

// counterFirstNodeText returns the text of the first resolved node — the
// single expr node in the swap fixtures that displays the live signal.
func counterFirstNodeText(tree *ResolvedTree) string {
	if tree == nil || len(tree.Nodes) == 0 {
		return ""
	}
	return tree.Nodes[0].Text
}

// TestVMSwapProgramRenamedSignalCleanReinit swaps from A (signal "count"=3)
// to C (signal "tally"). The renamed signal must initialize fresh from C's
// init expr; the old "count" signal must be gone (no stale carryover).
func TestVMSwapProgramRenamedSignalCleanReinit(t *testing.T) {
	vm := NewVM(swapProgA(), nil)
	InitSignals(vm, swapProgA())
	vm.EvalWithFrame(4) // count -> 4, give it a non-init value to detect leakage

	vm.SwapProgram(swapProgC())

	// New signal initialized fresh to 0.
	tally, ok := vm.signals["tally"]
	if !ok {
		t.Fatal("tally signal missing after swap to C")
	}
	if got := tally.Get(); got.Num != 0 {
		t.Fatalf("tally = %v, want 0 (clean re-init)", got.Num)
	}
	// Old signal removed — no stale "count" lingering.
	if _, ok := vm.signals["count"]; ok {
		t.Fatal("stale count signal survived swap to C (removed signals must be dropped)")
	}
}
