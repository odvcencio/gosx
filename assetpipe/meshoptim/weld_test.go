package meshoptim

import (
	"math"
	"testing"
)

func TestWeldMergesOnlyIdenticalVertices(t *testing.T) {
	// Four vertices, where 0 and 2 match on every stream and 1 and 3 differ in
	// the second stream only. Welding must merge the first pair and keep the
	// second pair apart, because a seam lives in the second stream.
	positions := []float64{
		0, 0, 0,
		1, 0, 0,
		0, 0, 0,
		1, 0, 0,
	}
	uvs := []float64{
		0, 0,
		0.5, 0.5,
		0, 0,
		0.75, 0.5,
	}
	remap, unique := Weld(4, []Stream{
		{Values: positions, Components: 3},
		{Values: uvs, Components: 2},
	})
	if unique != 3 {
		t.Fatalf("unique vertices = %d, want 3", unique)
	}
	if remap[0] != remap[2] {
		t.Fatalf("identical vertices did not merge: %v", remap)
	}
	if remap[1] == remap[3] {
		t.Fatalf("a texture coordinate seam merged: %v", remap)
	}
}

func TestWeldKeepsTheRenderedResultUnchanged(t *testing.T) {
	// Expand a grid to a triangle soup, weld it back, and check that the mesh
	// draws the same triangles with the same corner positions.
	indices, positions, vertexCount := gridMesh(12)
	soupPositions := make([]float64, 0, len(indices)*3)
	soupIndices := make([]uint32, len(indices))
	for i, index := range indices {
		soupIndices[i] = uint32(i)
		base := int(index) * 3
		soupPositions = append(soupPositions,
			float64(positions[base]), float64(positions[base+1]), float64(positions[base+2]))
	}

	remap, unique := Weld(len(indices), []Stream{{Values: soupPositions, Components: 3}})
	if unique != vertexCount {
		t.Fatalf("welded to %d vertices, want the original %d", unique, vertexCount)
	}
	welded := ApplyWeld(soupIndices, remap)
	collapsed := CollapseWeld(soupPositions, 3, remap, unique)

	if len(welded)/3 != len(indices)/3 {
		t.Fatalf("triangle count changed: %d, want %d", len(welded)/3, len(indices)/3)
	}
	if kept := DropDegenerate(welded); len(kept) != len(welded) {
		t.Fatalf("welding produced %d degenerate triangles", (len(welded)-len(kept))/3)
	}
	for i := range welded {
		want := soupPositions[i*3 : i*3+3]
		got := collapsed[int(welded[i])*3 : int(welded[i])*3+3]
		for axis := 0; axis < 3; axis++ {
			if got[axis] != want[axis] {
				t.Fatalf("corner %d axis %d moved from %g to %g", i, axis, want[axis], got[axis])
			}
		}
	}

	lowSource, highSource := boundsOf(soupPositions)
	lowWelded, highWelded := boundsOf(collapsed)
	if lowSource != lowWelded || highSource != highWelded {
		t.Fatalf("bounds changed: %v %v, want %v %v", lowWelded, highWelded, lowSource, highSource)
	}
}

func boundsOf(values []float64) ([3]float64, [3]float64) {
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i+2 < len(values); i += 3 {
		for axis := 0; axis < 3; axis++ {
			low[axis] = math.Min(low[axis], values[i+axis])
			high[axis] = math.Max(high[axis], values[i+axis])
		}
	}
	return low, high
}

func TestWeldQuantumSnapsToAGrid(t *testing.T) {
	positions := []float64{0, 0, 0, 0.0001, 0, 0}
	exact, uniqueExact := Weld(2, []Stream{{Values: positions, Components: 3}})
	if uniqueExact != 2 {
		t.Fatalf("an exact weld must keep both vertices: %v", exact)
	}
	snapped, uniqueSnapped := Weld(2, []Stream{{Values: positions, Components: 3, Quantum: 0.001}})
	if uniqueSnapped != 1 {
		t.Fatalf("a quantum of 0.001 must merge both vertices: %v", snapped)
	}
}

func TestWeldTreatsBothZerosAsOne(t *testing.T) {
	positions := []float64{0, 0, 0, math.Copysign(0, -1), 0, 0}
	_, unique := Weld(2, []Stream{{Values: positions, Components: 3}})
	if unique != 1 {
		t.Fatalf("negative zero must merge with zero, got %d vertices", unique)
	}
}

func TestDropDegenerateRemovesCollapsedTriangles(t *testing.T) {
	indices := []uint32{0, 1, 2, 3, 3, 4, 5, 6, 7, 8, 9, 8}
	kept := DropDegenerate(indices)
	if len(kept) != 6 {
		t.Fatalf("kept %d indices, want 6: %v", len(kept), kept)
	}
}
