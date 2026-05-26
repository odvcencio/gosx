// Slice X.C.7: lower three representative engine-surface handler
// functions of increasing complexity and verify they round-trip
// against a Go-side reference.
//
// These tests do NOT lower the hyphae graph_surface.go file directly
// (which uses maps, struct receivers on surface.Canvas, etc. — all out
// of the supported subset). Instead they exercise the lowerer over a
// small in-test fixture that mirrors the *kinds* of code engine-surface
// handlers contain: pure predicates, arithmetic loops over slices, and
// numeric reductions. This keeps the test independent of the hyphae
// checkout (the parity SSIM test in Slice X.E is the real cross-repo
// integration), while still proving the lowerer can handle the three
// complexity tiers the plan calls out.

package golower

import (
	"math"
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/island/program"
)

// fixture is the source snippet the three tests share. The three
// functions mirror graph_surface.go's complexity tiers:
//   IsGraftKind  — pure boolean (analogue of isGraftKind)
//   AccumulateAngle — for-loop with arithmetic (analogue of initPositions
//                     ring layout)
//   ForceFalloff — math.Sqrt + arithmetic (analogue of stepLayout's
//                  repulsion term in isolation)
const fixture = `package handlers

import "math"

func IsGraftKind(kind string) bool {
	return kind == "derived_from" || kind == "graft"
}

func AccumulateAngle(n int) float64 {
	total := 0.0
	for i := 0; i < n; i++ {
		total = total + float64(i) * 0.5
	}
	return total
}

func ForceFalloff(dx float64, dy float64) float64 {
	dist := math.Sqrt(dx*dx + dy*dy) + 1e-9
	return 4500.0 / (dist * dist)
}
`

func TestGraphSurfaceTier1IsGraftKind(t *testing.T) {
	prog, err := LowerFile([]byte(fixture))
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "IsGraftKind")
	cases := []struct {
		kind string
		want bool
	}{
		{"derived_from", true},
		{"graft", true},
		{"concept", false},
		{"", false},
	}
	for _, tc := range cases {
		machine := vm.NewVM(prog, map[string]vm.Value{"kind": vm.StringVal(tc.kind)})
		got := machine.EvalWithFrame(handler.Body[0])
		if got.Bool != tc.want {
			t.Errorf("IsGraftKind(%q) = %v, want %v", tc.kind, got.Bool, tc.want)
		}
	}
}

func TestGraphSurfaceTier2AccumulateAngle(t *testing.T) {
	prog, err := LowerFile([]byte(fixture))
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "AccumulateAngle")

	for _, n := range []int{0, 1, 5, 10} {
		machine := vm.NewVM(prog, map[string]vm.Value{"n": vm.IntVal(n)})
		got := machine.EvalWithFrame(handler.Body[0])
		want := refAccumulateAngle(n)
		if math.Abs(got.Num-want) > 1e-9 {
			t.Errorf("AccumulateAngle(%d) = %f, want %f", n, got.Num, want)
		}
	}
}

func TestGraphSurfaceTier3ForceFalloff(t *testing.T) {
	prog, err := LowerFile([]byte(fixture))
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "ForceFalloff")

	cases := []struct {
		dx, dy float64
	}{
		{1, 0},
		{0, 1},
		{3, 4},
		{0.5, 0.5},
	}
	for _, tc := range cases {
		machine := vm.NewVM(prog, map[string]vm.Value{
			"dx": vm.FloatVal(tc.dx),
			"dy": vm.FloatVal(tc.dy),
		})
		got := machine.EvalWithFrame(handler.Body[0])
		want := refForceFalloff(tc.dx, tc.dy)
		if math.Abs(got.Num-want) > 1e-3 {
			t.Errorf("ForceFalloff(%f, %f) = %f, want %f", tc.dx, tc.dy, got.Num, want)
		}
	}
}

// refAccumulateAngle is the Go reference for the AccumulateAngle
// handler — runtime cross-check that the lowered bytecode produces the
// same values the source would.
func refAccumulateAngle(n int) float64 {
	total := 0.0
	for i := 0; i < n; i++ {
		total = total + float64(i)*0.5
	}
	return total
}

// refForceFalloff mirrors ForceFalloff exactly.
func refForceFalloff(dx, dy float64) float64 {
	dist := math.Sqrt(dx*dx+dy*dy) + 1e-9
	return 4500.0 / (dist * dist)
}

// findHandler picks a Handler by name with a fatal on miss.
func findHandler(t *testing.T, handlers []program.Handler, name string) program.Handler {
	t.Helper()
	for _, h := range handlers {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("handler %q not found", name)
	return program.Handler{}
}
