package earcut

import (
	"testing"
	"time"
)

// TestIndices2D ports the "indices-2d" case from upstream test/test.js
// (mapbox/earcut v2.2.4).
func TestIndices2D(t *testing.T) {
	got := Triangulate([]float64{10, 0, 0, 50, 60, 60, 70, 10}, nil, 2)
	want := []int{1, 0, 3, 3, 2, 1}
	assertIntsEqual(t, got, want)
}

// TestIndices3D ports the "indices-3d" case from upstream test/test.js.
func TestIndices3D(t *testing.T) {
	got := Triangulate([]float64{10, 0, 0, 0, 50, 0, 60, 60, 0, 70, 10, 0}, nil, 3)
	want := []int{1, 0, 3, 3, 2, 1}
	assertIntsEqual(t, got, want)
}

// TestEmpty ports the "empty" case from upstream test/test.js.
func TestEmpty(t *testing.T) {
	got := Triangulate(nil, nil, 0)
	if len(got) != 0 {
		t.Fatalf("Triangulate(nil, nil, 0) = %v, want empty", got)
	}
}

// TestInfiniteLoop ports the "infinite-loop" regression case from upstream
// test/test.js: a self-intersecting/degenerate ring with a hole, which
// historically triggered a non-terminating loop in some ear-clipping
// implementations. We only assert that Triangulate returns.
func TestInfiniteLoop(t *testing.T) {
	done := make(chan struct{})
	go func() {
		Triangulate([]float64{1, 2, 2, 2, 1, 2, 1, 1, 1, 2, 4, 1, 5, 1, 3, 2, 4, 2, 4, 1}, []int{5}, 2)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Triangulate did not terminate (infinite-loop regression)")
	}
}

// TestDegenerateAllCollinear covers an all-collinear ring (no area): the
// triangulator must not panic and must produce no triangles.
func TestDegenerateAllCollinear(t *testing.T) {
	got := Triangulate([]float64{0, 0, 1, 0, 2, 0, 3, 0}, nil, 2)
	if len(got) != 0 {
		t.Fatalf("collinear ring produced %d triangles, want 0", len(got)/3)
	}
}

// TestSingleTriangle covers the minimal non-degenerate polygon.
func TestSingleTriangle(t *testing.T) {
	got := Triangulate([]float64{0, 0, 4, 0, 2, 4}, nil, 2)
	if len(got) != 3 {
		t.Fatalf("single triangle produced %d indices, want 3", len(got))
	}
	dev := Deviation([]float64{0, 0, 4, 0, 2, 4}, nil, 2, got)
	if dev > 1e-9 {
		t.Fatalf("single triangle deviation = %v, want ~0", dev)
	}
}

// TestTouchingHoles is a hand-built sanity check (not a fixture) covering
// two holes that touch each other and the outer ring, exercising
// findHoleBridge's tie-breaking path.
func TestTouchingHoles(t *testing.T) {
	// outer: 10x10 square; two small holes sharing an edge point
	vertices := []float64{
		0, 0, 10, 0, 10, 10, 0, 10, // outer
		2, 2, 5, 2, 5, 5, 2, 5, // hole 1
		5, 2, 8, 2, 8, 5, 5, 5, // hole 2 (touches hole 1 at (5,2)-(5,5))
	}
	holes := []int{4, 8}
	got := Triangulate(vertices, holes, 2)
	if len(got) == 0 {
		t.Fatal("expected a non-empty triangulation for touching holes")
	}
	dev := Deviation(vertices, holes, 2, got)
	if dev > 1e-9 {
		t.Fatalf("touching-holes deviation = %v, want ~0", dev)
	}
}

// TestSteinerPoint is a hand-built sanity check covering a single-vertex
// "hole" (a Steiner point) — a ring of length 1, which eliminateHoles marks
// via node.steiner so filterPoints doesn't discard it as degenerate.
func TestSteinerPoint(t *testing.T) {
	vertices := []float64{
		0, 0, 100, 0, 100, 100, 0, 100, // outer square
		50, 50, // steiner point
	}
	holes := []int{4}
	got := Triangulate(vertices, holes, 2)
	if len(got) == 0 {
		t.Fatal("expected a non-empty triangulation with a steiner point")
	}
	dev := Deviation(vertices, holes, 2, got)
	if dev > 1e-9 {
		t.Fatalf("steiner-point deviation = %v, want ~0", dev)
	}
}

func assertIntsEqual(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
