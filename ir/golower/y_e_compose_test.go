// Slice Y.E compositional tests — exercise Y.A through Y.E features
// together to catch interaction bugs that single-feature tests miss.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_E_TypeColorPlusSetFillStyle reproduces the canonical
// graph_surface.go composition: `c.SetFillStyle(typeColor(n.Type))`.
// Exercises Y.E.3 (switch — typeColor uses one), Y.D (user-fn call),
// and Y.E.2 (host call on the result).
func TestY_E_TypeColorPlusSetFillStyle(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

func typeColor(t string) string {
	switch t {
	case "concept":
		return "#7b5c3a"
	case "decision":
		return "#5a7b3a"
	default:
		return "#9a8870"
	}
}

func Paint(c *surface.Canvas, kind string) {
	c.SetFillStyle(typeColor(kind))
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Paint")
	rec := vm.NewHostRecorder()

	cases := map[string]string{
		"concept":  "#7b5c3a",
		"decision": "#5a7b3a",
		"unknown":  "#9a8870",
	}
	for kind, want := range cases {
		rec.Reset()
		machine := vm.NewVM(prog, map[string]vm.Value{
			"kind": vm.StringVal(kind),
		})
		machine.BindHost("c", rec)
		machine.EvalWithFrame(handler.Body[0])
		if len(rec.Calls) != 1 {
			t.Errorf("kind=%q: got %d calls; want 1", kind, len(rec.Calls))
			continue
		}
		if rec.Calls[0].Method != "SetFillStyle" {
			t.Errorf("kind=%q: method = %q, want SetFillStyle", kind, rec.Calls[0].Method)
		}
		if rec.Calls[0].Args[0].Str != want {
			t.Errorf("kind=%q: arg = %q, want %q", kind, rec.Calls[0].Args[0].Str, want)
		}
	}
}

// TestY_E_LabelTruncation reproduces graph_surface.go's
// label-truncation idiom: `string([]rune(label)[:N]) + "…"`. Uses
// rune-array semantics so multibyte characters count correctly.
func TestY_E_LabelTruncation(t *testing.T) {
	src := []byte(`package handlers

func Trunc(label string, n int) string {
	if len([]rune(label)) > n {
		return string([]rune(label)[:n-2]) + "…"
	}
	return label
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Trunc")

	cases := []struct {
		label string
		n     int
		want  string
	}{
		{"hi", 5, "hi"},
		{"héllo world", 6, "héll…"},
		{"short", 10, "short"},
	}
	for _, tc := range cases {
		machine := vm.NewVM(prog, map[string]vm.Value{
			"label": vm.StringVal(tc.label),
			"n":     vm.IntVal(tc.n),
		})
		got := machine.EvalWithFrame(handler.Body[0])
		if got.Str != tc.want {
			t.Errorf("Trunc(%q, %d) = %q, want %q", tc.label, tc.n, got.Str, tc.want)
		}
	}
}

// TestY_E_MakeMapInUserFn verifies a user function can call make()
// internally and the caller observes the materialized map.
func TestY_E_MakeMapInUserFn(t *testing.T) {
	src := []byte(`package handlers

func buildForces(n int) map[string]float64 {
	m := make(map[string]float64, n)
	m["a"] = 1.0
	m["b"] = 2.0
	return m
}

func F(n int) float64 {
	fx := buildForces(n)
	return fx["a"] + fx["b"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, map[string]vm.Value{
		"n": vm.IntVal(4),
	})
	got := machine.EvalWithFrame(handler.Body[0])
	if got.Num != 3.0 {
		t.Errorf("F(4) = %f, want 3.0 (1.0 + 2.0)", got.Num)
	}
}

// TestY_E_StepLayoutKernel is the Y.E analog of Y.C's Tier-4 kernel
// test: a stepwise distillation of graph_surface.go's stepLayout
// integration loop. Exercises Y.A composite literals, Y.B comma-ok
// map lookups, Y.C LHS index/field set, Y.D user-fn dispatch, AND
// Y.E make() + host call.
func TestY_E_StepLayoutKernel(t *testing.T) {
	src := []byte(`package handlers

import "m31labs.dev/gosx/engine/surface"

type vec2 struct{ X, Y float64 }

func updateVel(v vec2, fx float64, fy float64) vec2 {
	v.X = (v.X + fx) * 0.82
	v.Y = (v.Y + fy) * 0.82
	return v
}

func Step(c *surface.Canvas, n int) int {
	gPos := make(map[string]vec2, n)
	gVel := make(map[string]vec2, n)
	gPos["root"] = vec2{X: 100, Y: 200}
	gVel["root"] = vec2{}

	fx := make(map[string]float64, n)
	fy := make(map[string]float64, n)
	fx["root"] = 1.5
	fy["root"] = -0.5

	v := gVel["root"]
	v = updateVel(v, fx["root"], fy["root"])
	gVel["root"] = v

	p := gPos["root"]
	p.X = p.X + v.X
	p.Y = p.Y + v.Y
	gPos["root"] = p

	c.SetFillStyle("debug")
	return len(gPos)
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "Step")
	rec := vm.NewHostRecorder()
	machine := vm.NewVM(prog, map[string]vm.Value{
		"n": vm.IntVal(4),
	})
	machine.BindHost("c", rec)
	got := machine.EvalWithFrame(handler.Body[0])
	if int(got.Num) != 1 {
		t.Errorf("Step returned %d, want 1 (one node)", int(got.Num))
	}
	if len(rec.Calls) != 1 || rec.Calls[0].Method != "SetFillStyle" {
		t.Errorf("expected one SetFillStyle host call; got %+v", rec.Calls)
	}
}
