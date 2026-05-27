// Slice Y.E.2.1 — failing-first tests for method calls on non-package
// receivers (the engine-surface Canvas + Context host bindings).
//
// graph_surface.go's handlers receive `c *surface.Canvas` and
// `ctx *surface.Context` parameters and dispatch through them like:
//
//   c.MoveTo(x, y)        // host call: canvas dispatch with "MoveTo"
//   c.SetFillStyle("...") // host call: canvas dispatch with "SetFillStyle"
//   ctx.PropsInto(&props) // host call: context dispatch with "PropsInto"
//
// Pre-Y.E the lowerer treats every selector-call as a stdlib intrinsic
// candidate and rejects unknown ones with "call to <X> is not in the
// supported intrinsic set." Y.E.2 introduces OpHostCall, which lets the
// VM dispatch into a runtime-bound receiver (the actual *surface.Canvas
// instance threaded in by the surface bootstrap).
//
// The patterns pinned here cover the three shapes graph_surface.go uses:
//
//   1. Zero-arg host method:        c.BeginPath()
//   2. Numeric-args host method:    c.MoveTo(x, y)
//   3. String-arg host method:      c.SetFillStyle(color)
//   4. Context-receiver host call:  ctx.PropsInto(&props)
//   5. Mixed-arg host method:       c.FillText(label, p.X, p.Y+r+12)
//
// At Y.E.2.1 each lowering call still fails. Y.E.2.2 adds OpHostCall,
// Y.E.2.3 the VM evaluator + BindHost, Y.E.2.4 the lowerer dispatch
// (import-pre-pass + receiver name classification), Y.E.2.5 marks PASS.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerHostCallZeroArg verifies the simplest case: a method call on
// a non-package receiver with no arguments. The VM dispatches the call
// into a bound host-receiver and records the method name for the test
// to inspect.
func TestLowerHostCallZeroArg(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func F(c *surface.Canvas) {
	c.BeginPath()
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, nil)
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])
	if len(rec.Calls) != 1 || rec.Calls[0].Method != "BeginPath" {
		t.Errorf("expected one canvas.BeginPath call; got %+v", rec.Calls)
	}
}

// TestLowerHostCallTwoFloatArgs verifies that float args are evaluated
// and passed through in source order. Mirrors `c.MoveTo(x, y)` from
// graph_surface.go's draw handler.
func TestLowerHostCallTwoFloatArgs(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func F(c *surface.Canvas, x float64, y float64) {
	c.MoveTo(x, y)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"x": vm.FloatVal(3.5),
		"y": vm.FloatVal(7.25),
	})
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])
	if len(rec.Calls) != 1 {
		t.Fatalf("expected one canvas.MoveTo call; got %+v", rec.Calls)
	}
	if rec.Calls[0].Method != "MoveTo" {
		t.Errorf("call.Method = %q, want MoveTo", rec.Calls[0].Method)
	}
	if len(rec.Calls[0].Args) != 2 {
		t.Fatalf("call.Args len = %d, want 2", len(rec.Calls[0].Args))
	}
	if rec.Calls[0].Args[0].Num != 3.5 {
		t.Errorf("args[0] = %f, want 3.5", rec.Calls[0].Args[0].Num)
	}
	if rec.Calls[0].Args[1].Num != 7.25 {
		t.Errorf("args[1] = %f, want 7.25", rec.Calls[0].Args[1].Num)
	}
}

// TestLowerHostCallStringArg verifies that a string-typed argument
// flows through unchanged. Mirrors `c.SetFillStyle("#7b5c3a")` from
// graph_surface.go's draw handler.
func TestLowerHostCallStringArg(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func F(c *surface.Canvas, color string) {
	c.SetFillStyle(color)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"color": vm.StringVal("#7b5c3a"),
	})
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])
	if len(rec.Calls) != 1 || rec.Calls[0].Method != "SetFillStyle" {
		t.Fatalf("expected one canvas.SetFillStyle call; got %+v", rec.Calls)
	}
	if rec.Calls[0].Args[0].Str != "#7b5c3a" {
		t.Errorf("args[0] = %q, want %q", rec.Calls[0].Args[0].Str, "#7b5c3a")
	}
}

// TestLowerHostCallContextReceiver verifies that a second host receiver
// (`ctx`) is dispatched independently of the canvas receiver. Mirrors
// `ctx.PropsInto(&props)` from graph_surface.go's Mount handler.
//
// Note: the `&props` argument is `*ast.UnaryExpr{Op: AND, X: Ident}`.
// The host-dispatch path doesn't need a real address — it just needs
// the operand to lower. We pass a primitive props value here so the
// argument count is what matters; PropsInto's actual JSON unmarshaling
// stays a host-side concern of the bound receiver.
func TestLowerHostCallContextReceiver(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func F(ctx *surface.Context, slot int) {
	ctx.RegisterSlot(slot)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"slot": vm.IntVal(42),
	})
	machine.BindHost("ctx", rec)
	machine.EvalWithFrame(handler.Body[0])
	if len(rec.Calls) != 1 || rec.Calls[0].Method != "RegisterSlot" {
		t.Fatalf("expected one ctx.RegisterSlot call; got %+v", rec.Calls)
	}
	if int(rec.Calls[0].Args[0].Num) != 42 {
		t.Errorf("args[0] = %d, want 42", int(rec.Calls[0].Args[0].Num))
	}
}

// TestLowerHostCallMixedArgs verifies a multi-arg call with mixed
// string and float types. Mirrors `c.FillText(label, p.X, p.Y+12)`.
func TestLowerHostCallMixedArgs(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func F(c *surface.Canvas, label string, x float64, y float64) {
	c.FillText(label, x, y + 12)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"label": vm.StringVal("hello"),
		"x":     vm.FloatVal(10),
		"y":     vm.FloatVal(20),
	})
	machine.BindHost("c", rec)
	machine.EvalWithFrame(handler.Body[0])
	if len(rec.Calls) != 1 {
		t.Fatalf("expected one canvas.FillText call; got %+v", rec.Calls)
	}
	if rec.Calls[0].Method != "FillText" {
		t.Errorf("call.Method = %q, want FillText", rec.Calls[0].Method)
	}
	if len(rec.Calls[0].Args) != 3 {
		t.Fatalf("call.Args len = %d, want 3", len(rec.Calls[0].Args))
	}
	if rec.Calls[0].Args[0].Str != "hello" {
		t.Errorf("args[0] = %q, want \"hello\"", rec.Calls[0].Args[0].Str)
	}
	if rec.Calls[0].Args[1].Num != 10 {
		t.Errorf("args[1] = %f, want 10", rec.Calls[0].Args[1].Num)
	}
	if rec.Calls[0].Args[2].Num != 32 {
		t.Errorf("args[2] = %f, want 32 (y + 12)", rec.Calls[0].Args[2].Num)
	}
}

// TestLowerHostCallStdlibIntrinsicStillRoutes verifies that stdlib
// package calls (math.Sin, strings.Join, etc.) STILL route through the
// intrinsic path even though the host-call dispatch is now wired. This
// is the discrimination test: pkg.X where pkg is imported → intrinsic;
// receiver.X where receiver is not imported → host call.
func TestLowerHostCallStdlibIntrinsicStillRoutes(t *testing.T) {
	src := []byte(`package handlers

import "math"

func F(x float64) float64 {
	return math.Sin(x)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"x": vm.FloatVal(0),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 0 {
		t.Errorf("sin(0) = %f, want 0", got.Num)
	}
}
