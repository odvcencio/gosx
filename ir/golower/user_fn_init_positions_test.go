// Slice Y.D — `initPositions` shape regression test. Closest analogue
// in graph_surface.go: a handler invocation seeds the package-level
// position/velocity maps by calling a sibling helper, which in turn
// loops over the node list and writes into the maps via OpIndexSet.
//
// This is the test that proves the Y.A + Y.C + Y.D stack supports
// the exact shape graph_surface.go's Mount → initPositions flow uses.

package golower

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestY_D_InitPositionsShape lowers a fixture that mirrors
// graph_surface.go's Mount → initPositions → seedNode cascade. The
// handler dispatches to a helper that walks an input node-id list and
// stores a fresh vec2 in a package-level map for each id. The final
// reduction over the map confirms every write landed in the shared
// storage.
func TestY_D_InitPositionsShape(t *testing.T) {
	src := []byte(`package handlers

type vec2 struct {
	X float64
	Y float64
}

var gPos = map[string]vec2{}

func placeNode(id string, x float64, y float64) {
	gPos[id] = vec2{X: x, Y: y}
}

func initPositions() {
	placeNode("n0", 1.0, 0.0)
	placeNode("n1", 0.0, 2.0)
	placeNode("n2", 3.0, 4.0)
}

func F() float64 {
	initPositions()
	a := gPos["n0"]
	b := gPos["n1"]
	c := gPos["n2"]
	return a.X + b.Y + c.X + c.Y
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	// 1.0 + 2.0 + 3.0 + 4.0 = 10.0
	if got.Num != 10.0 {
		t.Errorf("F() = %f, want 10.0 (initPositions cascade must populate gPos through nested user-fn calls)", got.Num)
	}
}

// TestY_D_RecursiveHelperOverPackageState combines Y.D's recursion
// with Y.C's package-state mutation: a helper writes one entry then
// recurses to seed the next. Bounded by an explicit base case to
// stay well inside the MaxCallDepth cap.
func TestY_D_RecursiveHelperOverPackageState(t *testing.T) {
	src := []byte(`package handlers

var acc = map[string]int{}

func seed(i int, n int) {
	if i >= n {
		return
	}
	acc["k"] = acc["k"] + i
	seed(i+1, n)
}

func F() int {
	seed(0, 5)
	return acc["k"]
}`)
	prog, err := LowerFile(src)
	if err != nil {
		t.Fatalf("LowerFile: %v", err)
	}
	handler := findHandler(t, prog.Handlers, "F")
	machine := vm.NewVM(prog, nil)
	vm.InitSignals(machine, prog)
	got := machine.EvalWithFrame(handler.Body[0])
	// 0+1+2+3+4 = 10
	if int(got.Num) != 10 {
		t.Errorf("F() = %d, want 10 (recursive seeding over package state must accumulate)", int(got.Num))
	}
}
