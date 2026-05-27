// Slice Y.E.3 — failing-first tests for the three syntax residuals
// Y.D's retrospective handed off: SwitchStmt, SliceExpr, and the
// `[]T(x)` ArrayType cast.
//
// Per Y.D's handoff note these are NOT in Y.E's plan-defined scope
// (the plan parked them under "residuals" and left a Y.G micro-slice
// open). Y.E folds them in because we're already touching expr.go
// (for OpHostCall) and stmt.go (potentially) — adding the three
// handlers in the same slice avoids a tiny follow-up slice.
//
// Patterns pinned here:
//
//   1. switch: typeColor's string switch (graph_surface.go:303-330)
//   2. switch with default fall-through: graph_surface.go uses default
//   3. slice expr: dotted-line `[]float64{4, 4}` substring `s[i:j]`
//   4. ArrayType cast: `[]rune(s)[:n]` rune conversion + slice
//   5. ArrayType cast + len: `len([]rune(label)) > 24` in graph_surface.go
//
// At Y.E.3.1 each lowering call still fails. Y.E.3.2-4 add handlers
// for each; Y.E.3.5 marks PASS.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestLowerSwitchStmt verifies that a string switch with multiple
// cases (typeColor's shape) lowers to a chain of OpCond branches.
func TestLowerSwitchStmt(t *testing.T) {
	src := []byte(`package handlers

func F(t string) string {
	switch t {
	case "concept":
		return "#7b5c3a"
	case "decision":
		return "#5a7b3a"
	case "spec":
		return "#5c7b3a"
	default:
		return "#9a8870"
	}
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")

	cases := []struct {
		in   string
		want string
	}{
		{"concept", "#7b5c3a"},
		{"decision", "#5a7b3a"},
		{"spec", "#5c7b3a"},
		{"unknown", "#9a8870"},
		{"", "#9a8870"},
	}
	for _, tc := range cases {
		machine := vm.NewVM(prog, map[string]vm.Value{
			"t": vm.StringVal(tc.in),
		})
		got := machine.EvalWithFrame(handler.Body[0])
		if got.Str != tc.want {
			t.Errorf("F(%q) = %q, want %q", tc.in, got.Str, tc.want)
		}
	}
}

// TestLowerSwitchStmtMultipleCaseValues verifies the `case "a", "b":`
// form where one case lists multiple match values.
func TestLowerSwitchStmtMultipleCaseValues(t *testing.T) {
	src := []byte(`package handlers

func F(t string) int {
	switch t {
	case "a", "b", "c":
		return 1
	case "d":
		return 2
	default:
		return 0
	}
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	for in, want := range map[string]int{"a": 1, "b": 1, "c": 1, "d": 2, "x": 0} {
		machine := vm.NewVM(prog, map[string]vm.Value{
			"t": vm.StringVal(in),
		})
		got := machine.EvalWithFrame(handler.Body[0])
		if int(got.Num) != want {
			t.Errorf("F(%q) = %d, want %d", in, int(got.Num), want)
		}
	}
}

// TestLowerSliceExpr verifies `s[i:j]` lowers through OpSlice (already
// exists for the X.B intrinsic family). graph_surface.go's draw handler
// uses `string([]rune(label)[:22])` — the slice expression is the
// `[:22]` part.
func TestLowerSliceExpr(t *testing.T) {
	src := []byte(`package handlers

func F(s string) string {
	return s[1:4]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"s": vm.StringVal("hello"),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "ell" {
		t.Errorf("F(\"hello\")[1:4] = %q, want \"ell\"", got.Str)
	}
}

// TestLowerArrayTypeCastForLen verifies `len([]rune(s))` lowers — the
// `[]rune(s)` is an ArrayType "call" in Go's AST. graph_surface.go
// uses this on line 404 (`if len([]rune(label)) > 24`).
func TestLowerArrayTypeCastForLen(t *testing.T) {
	src := []byte(`package handlers

func F(s string) int {
	return len([]rune(s))
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"s": vm.StringVal("héllo"),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	// "héllo" is 5 runes; the VM treats len(string) as byte length
	// historically, but len([]rune(...)) must use rune count.
	if int(got.Num) != 5 {
		t.Errorf("len([]rune(\"héllo\")) = %d, want 5", int(got.Num))
	}
}

// TestLowerArrayTypeCastThenSliceThenString verifies the full
// graph_surface.go pattern: `string([]rune(label)[:22]) + "…"`.
func TestLowerArrayTypeCastThenSliceThenString(t *testing.T) {
	src := []byte(`package handlers

func F(label string) string {
	return string([]rune(label)[:3]) + "…"
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"label": vm.StringVal("héllo world"),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Str != "hél…" {
		t.Errorf("F(\"héllo world\") = %q, want %q", got.Str, "hél…")
	}
}
