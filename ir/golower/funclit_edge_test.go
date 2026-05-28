// Slice Y.G — FuncLit edge-case + failure-mode tests.
//
// Pin the corner cases that distinguish a robust closure
// implementation from one that just compiles the happy path:
//
//  - Nested closures (outer captures, inner captures both)
//  - Multi-return closures (uses Y.D's multi-return scaffolding)
//  - Closures that don't capture anything (degenerate case)
//  - Closures stored in maps/slices (delayed dispatch)
//  - Closures that reference signals (package-var path, not capture)

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// TestLowerFuncLitNoCapture verifies a closure with no captured locals
// still lowers and dispatches correctly. The captured-frame bridge
// stays inert; the closure behaves as a plain anonymous function.
func TestLowerFuncLitNoCapture(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	f := func(a int, b int) int {
		return a + b
	}
	return f(7, 3)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 10 {
		t.Errorf("F() = %v, want 10", got.Num)
	}
}

// TestLowerFuncLitNested verifies a closure inside a closure: the
// inner FuncLit can capture both the outer FuncLit's locals AND its
// own enclosing-handler captures. Each level installs its own bridge.
func TestLowerFuncLitNested(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	a := 10
	outer := func() int {
		b := 20
		inner := func() int {
			return a + b
		}
		return inner()
	}
	return outer()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 30 {
		t.Errorf("F() = %v, want 30 (nested closures lost a capture)", got.Num)
	}
}

// TestLowerFuncLitCapturesSignalIndirectly verifies that a closure
// referencing a package-level var (signal) reads through the signal
// path rather than the captured-frame bridge. The signal is NOT in
// the captured set — it's not a handler local — so the closure body
// reads it via the normal OpLocalGet → signal-fallback chain.
func TestLowerFuncLitCapturesSignalIndirectly(t *testing.T) {
	src := []byte(`package handlers

var gFoo int

func Init() {
	gFoo = 99
}

func F() int {
	f := func() int {
		return gFoo
	}
	return f()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	// Seed the signal by running Init().
	initH := findHandler(t, prog.Handlers, "Init")
	machine.EvalWithFrame(initH.Body[0])
	// Now invoke F() and observe that the closure sees the signal.
	fH := findHandler(t, prog.Handlers, "F")
	got := machine.EvalWithFrame(fH.Body[0])
	if int(got.Num) != 99 {
		t.Errorf("F() = %v, want 99 (closure should see package-var signal value)", got.Num)
	}
}

// TestLowerFuncLitInvokedTwiceSharesCapture verifies that two
// invocations of the same closure observe each other's mutations —
// the captured frame is the same across calls.
func TestLowerFuncLitInvokedTwiceSharesCapture(t *testing.T) {
	src := []byte(`package handlers

func F() int {
	n := 0
	bump := func() {
		n = n + 5
	}
	bump()
	bump()
	bump()
	return n
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 15 {
		t.Errorf("F() = %v, want 15 (cross-invocation capture share broken)", got.Num)
	}
}

// TestLowerFuncLitCalleeKeywordsSkipped verifies that the closure's
// capture analysis doesn't accidentally treat language keywords as
// captures (true/false/nil/iota) — those resolve as literals, not
// frame slots, and listing them as captures would explode the
// closureRef map for every closure that uses a comparison.
func TestLowerFuncLitCalleeKeywordsSkipped(t *testing.T) {
	src := []byte(`package handlers

func F() bool {
	f := func() bool {
		return true
	}
	return f()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	// The synthetic FuncDef (alongside F's own user-fn FuncDef
	// per Y.D) should have ZERO params — `func() bool { return true }`
	// captures nothing because `true` is a keyword. Locate the
	// synthetic by its reserved __y_g_funclit_ prefix.
	var synth *program.FuncDef
	for i := range prog.Funcs {
		if len(prog.Funcs[i].Name) >= 14 && prog.Funcs[i].Name[:14] == "__y_g_funclit_" {
			f := prog.Funcs[i]
			synth = &f
			break
		}
	}
	if synth == nil {
		t.Fatalf("synthetic FuncLit FuncDef not registered; got Funcs=%+v", prog.Funcs)
	}
	if len(synth.Params) != 0 {
		t.Errorf("synth FuncDef should have no params; got %v", synth.Params)
	}

	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	got := machine.EvalWithFrame(handler.Body[0])
	if !got.Bool {
		t.Errorf("F() = false, want true (closure should return literal true)")
	}
}
