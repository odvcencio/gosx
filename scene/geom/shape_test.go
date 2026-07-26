package geom

import (
	"math"
	"testing"
)

func squareContour(half float64) Contour {
	return Contour{Outline: []float64{-half, -half, half, -half, half, half, -half, half}}
}

func squareWithHole() Contour {
	return Contour{
		Outline: []float64{-2, -2, 2, -2, 2, 2, -2, 2},
		Holes:   [][]float64{{-1, -1, 1, -1, 1, 1, -1, 1}},
	}
}

// TestShapeCapCountsAndWinding proves the flat cap triangulates the whole
// outline, faces +Y, and keeps its declared normals.
func TestShapeCapCountsAndWinding(t *testing.T) {
	cases := map[string]struct {
		contour  Contour
		vertices int
	}{
		"square":         {squareContour(1), 4},
		"square-in-hole": {squareWithHole(), 8},
	}
	for name, testCase := range cases {
		mesh := Shape(testCase.contour, 0.5, AllAttributes)
		if mesh == nil {
			t.Fatalf("%s: nil mesh", name)
		}
		if got := mesh.VertexCount(); got != testCase.vertices {
			t.Fatalf("%s: %d vertices, want %d", name, got, testCase.vertices)
		}
		assertFiniteUnitNormals(t, name, mesh)
		assertWindingMatchesNormals(t, name, mesh, 0)
		for i := 0; i+3 <= len(mesh.Positions); i += 3 {
			if y := mesh.Positions[i+1]; y != 0.5 {
				t.Fatalf("%s: vertex %d sits at y=%v, want the authored elevation", name, i/3, y)
			}
		}
		for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
			p0, p1, p2 := meshTriangle(mesh, triangle)
			if got := triangleNormal(p0, p1, p2); math.Abs(got.Y-1) > 1e-9 {
				t.Fatalf("%s: triangle %d faces %v, want +Y", name, triangle, got)
			}
		}
	}
}

// TestShapeWindingIgnoresTheAuthoredRingDirection proves the cap faces +Y for a
// clockwise outline and a counter-clockwise one alike.
//
// Package scene/earcut always emits counter-clockwise triangles in the 2D input
// plane, whichever way the author wound the ring, and a counter-clockwise loop
// in (x, z) faces -Y. Without the repair pass every cap would face down.
func TestShapeWindingIgnoresTheAuthoredRingDirection(t *testing.T) {
	clockwise := Contour{Outline: []float64{-1, -1, -1, 1, 1, 1, 1, -1}}
	counterClockwise := Contour{Outline: []float64{-1, -1, 1, -1, 1, 1, -1, 1}}
	for name, contour := range map[string]Contour{"cw": clockwise, "ccw": counterClockwise} {
		mesh := Shape(contour, 0, AllAttributes)
		assertWindingMatchesNormals(t, name, mesh, 0)
		for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
			p0, p1, p2 := meshTriangle(mesh, triangle)
			if got := triangleNormal(p0, p1, p2); got.Y < 0.99 {
				t.Fatalf("%s outline: triangle %d faces %v, want +Y", name, triangle, got)
			}
		}
	}
}

// TestShapeRejectsDegenerateContours proves a shape with no area refuses.
func TestShapeRejectsDegenerateContours(t *testing.T) {
	for name, contour := range map[string]Contour{
		"empty":     {},
		"two-point": {Outline: []float64{0, 0, 1, 1}},
		"collinear": {Outline: []float64{0, 0, 1, 0, 2, 0}},
	} {
		if got := Shape(contour, 0, AllAttributes); got != nil {
			t.Fatalf("%s: Shape produced a mesh, want nil", name)
		}
	}
}

// TestExtrudeCountsAndWinding proves the solid closes and every face points out.
func TestExtrudeCountsAndWinding(t *testing.T) {
	cases := map[string]struct {
		contour Contour
		opts    ExtrudeOptions
	}{
		"square":         {squareContour(1), ExtrudeOptions{Depth: 2}},
		"square-hole":    {squareWithHole(), ExtrudeOptions{Depth: 1}},
		"beveled":        {squareContour(1), ExtrudeOptions{Depth: 2, Bevel: true, BevelSize: 0.2, BevelThickness: 0.2, BevelSegments: 3}},
		"beveled-hole":   {squareWithHole(), ExtrudeOptions{Depth: 2, Bevel: true, BevelSize: 0.2, BevelThickness: 0.2, BevelSegments: 2}},
		"bevel-defaults": {squareContour(1), ExtrudeOptions{Depth: 2, Bevel: true}},
	}
	for name, testCase := range cases {
		mesh := Extrude(testCase.contour, testCase.opts, AllAttributes)
		if mesh == nil {
			t.Fatalf("%s: nil mesh", name)
		}
		assertFiniteUnitNormals(t, name, mesh)
		assertWindingMatchesNormals(t, name, mesh, 0)
	}
}

// TestExtrudeBoundsFollowTheDepth proves the solid runs from y=0 to y=Depth and
// never passes the authored outline.
func TestExtrudeBoundsFollowTheDepth(t *testing.T) {
	const depth = 2.5
	mesh := Extrude(squareContour(1), ExtrudeOptions{Depth: depth}, AllAttributes)
	lo, hi := meshBounds(mesh)
	if lo.Y != 0 || math.Abs(hi.Y-depth) > 1e-12 {
		t.Fatalf("y bounds [%v, %v], want [0, %v]", lo.Y, hi.Y, depth)
	}
	for _, value := range []float64{lo.X, lo.Z} {
		if math.Abs(value+1) > 1e-12 {
			t.Fatalf("a horizontal bound reached %v, want -1", value)
		}
	}
	for _, value := range []float64{hi.X, hi.Z} {
		if math.Abs(value-1) > 1e-12 {
			t.Fatalf("a horizontal bound reached %v, want 1", value)
		}
	}
	// A depth at or below zero falls back to 1.
	fallback := Extrude(squareContour(1), ExtrudeOptions{}, PositionsOnly)
	_, high := meshBounds(fallback)
	if math.Abs(high.Y-1) > 1e-12 {
		t.Fatalf("a zero depth gave a height of %v, want the fallback 1", high.Y)
	}
}

// TestExtrudeCapsFaceAwayFromTheSolid proves the bottom cap faces -Y and the top
// cap faces +Y. A cap wound the other way turns the solid inside out under
// back-face culling, which the native renderer uses.
func TestExtrudeCapsFaceAwayFromTheSolid(t *testing.T) {
	const depth = 2.0
	mesh := Extrude(squareWithHole(), ExtrudeOptions{Depth: depth}, AllAttributes)
	bottom, top := 0, 0
	for triangle := 0; triangle < mesh.TriangleCount(); triangle++ {
		p0, p1, p2 := meshTriangle(mesh, triangle)
		normal := triangleNormal(p0, p1, p2)
		height := (p0.Y + p1.Y + p2.Y) / 3
		switch {
		case math.Abs(height) < 1e-9:
			if normal.Y > -0.99 {
				t.Fatalf("triangle %d sits on the bottom cap but faces %v", triangle, normal)
			}
			bottom++
		case math.Abs(height-depth) < 1e-9:
			if normal.Y < 0.99 {
				t.Fatalf("triangle %d sits on the top cap but faces %v", triangle, normal)
			}
			top++
		default:
			if math.Abs(normal.Y) > 1e-9 {
				t.Fatalf("triangle %d is a wall but its normal leans %v along Y", triangle, normal.Y)
			}
		}
	}
	if bottom == 0 || top == 0 || bottom != top {
		t.Fatalf("cap triangle counts bottom %d top %d; both must exist and match", bottom, top)
	}
}

// TestExtrudeHoleWallsFaceIntoTheHole proves a hole is cut, not filled. The wall
// of a hole must face the hole's own axis, because the solid lies outside it.
func TestExtrudeHoleWallsFaceIntoTheHole(t *testing.T) {
	mesh := Extrude(squareWithHole(), ExtrudeOptions{Depth: 1}, AllAttributes)
	checked := 0
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		x, z := mesh.Positions[i], mesh.Positions[i+2]
		if math.Max(math.Abs(x), math.Abs(z)) > 1.5 {
			continue // The outer wall or an outer cap corner.
		}
		normal := vec3{mesh.Normals[i], 0, mesh.Normals[i+2]}
		if math.Hypot(normal.X, normal.Z) < 1e-9 {
			continue // A cap vertex; its normal runs along Y.
		}
		// A normal facing into the hole points back toward the hole's axis.
		if dotVec(normal, vec3{X: x, Z: z}) >= 0 {
			t.Fatalf("hole wall vertex %d at (%v, %v) carries the normal %v, which points out of the hole",
				i/3, x, z, normal)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no hole wall vertex was checked, so this test proved nothing")
	}
}

// TestExtrudeBevelPullsTheCapsIn proves the bevel does something. Without it the
// caps would sit at the authored outline, which a bounds test alone would miss.
func TestExtrudeBevelPullsTheCapsIn(t *testing.T) {
	const (
		depth = 2.0
		size  = 0.25
	)
	mesh := Extrude(squareContour(1), ExtrudeOptions{
		Depth: depth, Bevel: true, BevelSize: size, BevelThickness: 0.25, BevelSegments: 2,
	}, AllAttributes)
	widestAt := map[float64]float64{}
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		y := math.Round(mesh.Positions[i+1]*1e6) / 1e6
		reach := math.Max(math.Abs(mesh.Positions[i]), math.Abs(mesh.Positions[i+2]))
		if reach > widestAt[y] {
			widestAt[y] = reach
		}
	}
	if got := widestAt[0]; math.Abs(got-(1-size)) > 1e-9 {
		t.Fatalf("the bottom cap reaches %v, want the outline pulled in to %v", got, 1-size)
	}
	if got := widestAt[depth]; math.Abs(got-(1-size)) > 1e-9 {
		t.Fatalf("the top cap reaches %v, want the outline pulled in to %v", got, 1-size)
	}
	// The two shoulders, where each lip meets the straight wall, must sit on the
	// authored outline. Everything between them is one quad with no vertex.
	const thickness = 0.25
	for _, shoulder := range []float64{thickness, depth - thickness} {
		reach, ok := widestAt[shoulder]
		if !ok {
			t.Fatalf("no wall level at y=%v; the bevel did not build a shoulder", shoulder)
		}
		if math.Abs(reach-1) > 1e-9 {
			t.Fatalf("the shoulder at y=%v reaches %v, want the authored outline 1", shoulder, reach)
		}
	}
	// A bevel thicker than half the depth must not fold the wall inside out.
	thick := Extrude(squareContour(1), ExtrudeOptions{
		Depth: 1, Bevel: true, BevelSize: 0.2, BevelThickness: 5, BevelSegments: 2,
	}, AllAttributes)
	assertWindingMatchesNormals(t, "thick bevel", thick, 0)
	lo, hi := meshBounds(thick)
	if lo.Y != 0 || math.Abs(hi.Y-1) > 1e-12 {
		t.Fatalf("a runaway bevel gave y bounds [%v, %v], want [0, 1]", lo.Y, hi.Y)
	}
}

// TestExtrudeRejectsDegenerateContours proves a shape with no area refuses.
func TestExtrudeRejectsDegenerateContours(t *testing.T) {
	for name, contour := range map[string]Contour{
		"empty":     {},
		"two-point": {Outline: []float64{0, 0, 1, 1}},
		"collinear": {Outline: []float64{0, 0, 1, 0, 2, 0}},
	} {
		if got := Extrude(contour, ExtrudeOptions{Depth: 1}, AllAttributes); got != nil {
			t.Fatalf("%s: Extrude produced a mesh, want nil", name)
		}
	}
}
