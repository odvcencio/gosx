package vm

import (
	"testing"

	"github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

func TestIslandCreation(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{"initial": 5}`)

	if island == nil {
		t.Fatal("island should not be nil")
	}
	if island.prev == nil {
		t.Fatal("prev tree should be initialized")
	}
	if len(island.handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(island.handlers))
	}
}

func TestIslandSignalInit(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{}`)

	// The count signal should be initialized to 0 (LitInt "0" at expr[1])
	tree := island.vm.EvalTree()
	// Node 2 is the expr node displaying count
	countDisplay := tree.Nodes[2].Text
	if countDisplay != "0" {
		t.Fatalf("expected count display '0', got %q", countDisplay)
	}
}

func TestIslandDispatchIncrement(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{}`)

	// Dispatch increment handler
	patches := island.Dispatch("increment", "{}")

	// Count should now be 1
	tree := island.vm.EvalTree()
	countDisplay := tree.Nodes[2].Text
	if countDisplay != "1" {
		t.Fatalf("after increment: expected '1', got %q", countDisplay)
	}

	// Should have produced at least one patch
	if len(patches) == 0 {
		t.Fatal("expected patches from increment")
	}
}

func TestIslandDispatchDecrement(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{}`)

	// Dispatch decrement
	island.Dispatch("decrement", "{}")

	// Count should be -1
	tree := island.vm.EvalTree()
	countDisplay := tree.Nodes[2].Text
	if countDisplay != "-1" {
		t.Fatalf("after decrement: expected '-1', got %q", countDisplay)
	}
}

func TestIslandBatching(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{}`)

	// Multiple dispatches
	island.Dispatch("increment", "{}")
	island.Dispatch("increment", "{}")
	island.Dispatch("increment", "{}")

	tree := island.vm.EvalTree()
	countDisplay := tree.Nodes[2].Text
	if countDisplay != "3" {
		t.Fatalf("after 3 increments: expected '3', got %q", countDisplay)
	}
}

func TestIslandUnknownHandler(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{}`)

	// Unknown handler should be a no-op
	patches := island.Dispatch("nonexistent", "{}")
	if patches != nil {
		t.Fatal("expected nil patches for unknown handler")
	}
}

func TestIslandDispose(t *testing.T) {
	prog := program.CounterProgram()
	island := NewIsland(prog, `{}`)
	island.Dispose()
	if island.prev != nil {
		t.Fatal("prev should be nil after dispose")
	}
}

func TestReentrantBatch(t *testing.T) {
	// Spec section 5.3: verify no deadlock on re-entrant Batch calls
	// This validates WASM single-thread safety
	s := signal.New(IntVal(0))

	// Nested batch should not deadlock
	signal.Batch(func() {
		s.Set(IntVal(1))
		signal.Batch(func() {
			s.Set(IntVal(2))
		})
	})

	if s.Get() != (IntVal(2)) {
		t.Fatalf("expected 2 after nested batch, got %v", s.Get())
	}
}
