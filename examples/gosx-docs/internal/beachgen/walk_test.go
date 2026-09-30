package beachgen

import (
	"math"
	"testing"
)

func TestWalkHeightfieldMatchesTerrain(t *testing.T) {
	g := WalkHeightfield(7)
	if len(g.Heights) != g.Cols*g.Rows {
		t.Fatalf("heights = %d, want %d", len(g.Heights), g.Cols*g.Rows)
	}
	n := newNoise(7)
	for _, idx := range []int{0, g.Cols - 1, len(g.Heights) / 2, len(g.Heights) - 1} {
		row, col := idx/g.Cols, idx%g.Cols
		x := g.MinX + g.SizeX*float64(col)/float64(g.Cols-1)
		z := g.MinZ + g.SizeZ*float64(row)/float64(g.Rows-1)
		if want := terrainHeight(n, x, z); math.Abs(g.Heights[idx]-want) > 1e-9 {
			t.Fatalf("height at (%g, %g) = %g, want %g", x, z, g.Heights[idx], want)
		}
	}
}

func TestWalkCollidersCoverMonolithAndStacks(t *testing.T) {
	shapes := WalkColliders(7)
	stacks, boulders, monolith := 0, 0, false
	for _, s := range shapes {
		switch {
		case s.X == MonolithX && s.Z == MonolithZ:
			monolith = true
		case s.Kind == "cylinder":
			stacks++
		case s.Kind == "sphere":
			boulders++
		}
		if s.Radius <= 0 {
			t.Fatalf("collider %+v has no radius", s)
		}
	}
	if !monolith || stacks != len(stackSpecs(7)) || boulders != len(boulderSpecs(7)) {
		t.Fatalf("monolith=%v stacks=%d boulders=%d", monolith, stacks, boulders)
	}
}
