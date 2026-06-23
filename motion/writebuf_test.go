package motion

import (
	"testing"
)

// TestWriteBufPacking verifies the packed layout: [targetID, propID, arity, v.F...].
func TestWriteBufPacking(t *testing.T) {
	w := NewWriteBuf(64)
	w.Reset()
	w.Push(5, 2, Value{Arity: ArityVec3, F: []float64{1, 2, 3}})
	w.Push(7, 0, Value{Arity: ArityScalar, F: []float64{9}})

	want := []float64{5, 2, 2, 1, 2, 3, 7, 0, 0, 9}
	got := w.Writes()

	if len(got) != len(want) {
		t.Fatalf("Writes() length = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Writes()[%d] = %v, want %v", i, got[i], v)
		}
	}
}

// TestWriteBufResetReuse confirms Reset rewinds the cursor without freeing.
func TestWriteBufResetReuse(t *testing.T) {
	w := NewWriteBuf(64)
	w.Push(1, 1, Value{Arity: ArityScalar, F: []float64{42}})
	if w.Len() == 0 {
		t.Fatal("expected Len > 0 after first Push")
	}

	w.Reset()
	if w.Len() != 0 {
		t.Fatalf("Len() after Reset = %d, want 0", w.Len())
	}

	// Subsequent push should start at index 0.
	w.Push(3, 4, Value{Arity: ArityScalar, F: []float64{7}})
	got := w.Writes()
	want := []float64{3, 4, 0, 7}
	if len(got) != len(want) {
		t.Fatalf("Writes() after Reset+Push length = %d, want %d", len(got), len(want))
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Writes()[%d] = %v, want %v", i, got[i], v)
		}
	}
}

// TestWriteBufZeroAlloc verifies the hot path does not allocate when capacity suffices.
func TestWriteBufZeroAlloc(t *testing.T) {
	// Allocate Value slices OUTSIDE the closure so they are not re-created per iteration.
	vec3val := Value{Arity: ArityVec3, F: []float64{1, 2, 3}}
	scalarval := Value{Arity: ArityScalar, F: []float64{9}}

	w := NewWriteBuf(64)

	allocs := testing.AllocsPerRun(1000, func() {
		w.Reset()
		w.Push(5, 2, vec3val)
		w.Push(7, 0, scalarval)
	})

	if allocs != 0 {
		t.Errorf("AllocsPerRun = %v, want 0 (pre-sized buffer must not allocate)", allocs)
	}
}

// TestWriteBufGrowth verifies correctness when the buffer must grow.
func TestWriteBufGrowth(t *testing.T) {
	// Capacity of 2 is too small for any single push (need at least 3 header + values).
	w := NewWriteBuf(2)

	w.Push(1, 1, Value{Arity: ArityVec3, F: []float64{10, 20, 30}})
	w.Push(2, 3, Value{Arity: ArityVec2, F: []float64{4, 5}})
	w.Push(9, 7, Value{Arity: ArityScalar, F: []float64{99}})

	want := []float64{
		1, 1, 2, 10, 20, 30, // vec3
		2, 3, 1, 4, 5,       // vec2
		9, 7, 0, 99,         // scalar
	}
	got := w.Writes()
	if len(got) != len(want) {
		t.Fatalf("Writes() length = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("Writes()[%d] = %v, want %v", i, got[i], v)
		}
	}
}

// TestWriteBufLen checks that Len matches the number of packed floats.
func TestWriteBufLen(t *testing.T) {
	w := NewWriteBuf(64)
	if w.Len() != 0 {
		t.Fatalf("initial Len = %d, want 0", w.Len())
	}
	// Push a scalar: 3 header + 1 value = 4 floats.
	w.Push(0, 0, Value{Arity: ArityScalar, F: []float64{1}})
	if w.Len() != 4 {
		t.Fatalf("Len after scalar push = %d, want 4", w.Len())
	}
	// Push a vec4: 3 header + 4 values = 7 more floats → total 11.
	w.Push(0, 0, Value{Arity: ArityVec4, F: []float64{1, 2, 3, 4}})
	if w.Len() != 11 {
		t.Fatalf("Len after vec4 push = %d, want 11", w.Len())
	}
}

// TestWritesIsView ensures Writes() returns a slice view, not a copy.
func TestWritesIsView(t *testing.T) {
	w := NewWriteBuf(64)
	w.Push(1, 2, Value{Arity: ArityScalar, F: []float64{3}})
	got := w.Writes()
	// Mutate through the view and verify the backing buffer reflects the change.
	got[0] = 999
	if w.F[0] != 999 {
		t.Error("Writes() returned a copy; expected a view into w.F")
	}
}
