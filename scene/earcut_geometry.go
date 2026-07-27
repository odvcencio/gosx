package scene

import "m31labs.dev/gosx/scene/earcut"

// PolygonGeometry triangulates a 2D polygon — an outer ring plus optional
// hole rings — via package scene/earcut (a pure-Go port of mapbox/earcut) and
// lifts the result into a BufferGeometry lying flat in the XZ plane at the
// given Y elevation. X/Z as the horizontal axes and Y as up matches the
// engine's ground-plane convention (see GridHelper, which lays its lines out
// as Vector3{X, Z}).
//
// polygon is the outer ring's flat 2D coordinates (x0, z0, x1, z1, ...).
// holes, if non-empty, are additional closed rings cut out of the polygon,
// each given the same flat 2D form; a ring with a single point is treated as
// a Steiner point rather than a hole (matching earcut's convention).
//
// The returned BufferGeometry is indexed (Positions + Indices); Normals are
// a uniform upward-facing (0, 1, 0) per vertex, and orientPolygonUp winds every
// triangle to agree with that normal. UVs are omitted — callers
// that need texture coordinates can derive them from Positions. Returns a
// zero-value BufferGeometry (no vertices) for a degenerate polygon (fewer
// than 3 outer-ring points, or a triangulation that yields no triangles).
func PolygonGeometry(polygon []float64, holes [][]float64, y float64) BufferGeometry {
	vertices := append([]float64(nil), polygon...)
	holeIndices := make([]int, 0, len(holes))
	for _, hole := range holes {
		holeIndices = append(holeIndices, len(vertices)/2)
		vertices = append(vertices, hole...)
	}

	indices := orientPolygonUp(vertices, earcut.Triangulate(vertices, holeIndices, 2))
	if len(indices) == 0 {
		return BufferGeometry{}
	}

	vertexCount := len(vertices) / 2
	positions := make([]float64, 0, vertexCount*3)
	normals := make([]float64, 0, vertexCount*3)
	for i := 0; i < vertexCount; i++ {
		x, z := vertices[i*2], vertices[i*2+1]
		positions = append(positions, x, y, z)
		normals = append(normals, 0, 1, 0)
	}

	return BufferGeometry{
		Positions: positions,
		Normals:   normals,
		Indices:   indices,
	}
}

// orientPolygonUp returns the triangulation wound so every triangle agrees with
// the +Y normal PolygonGeometry declares.
//
// Package scene/earcut always emits counter-clockwise triangles in the flat
// input plane, whichever direction the author wound the ring: both directions of
// the same square measure a signed area of +8. A counter-clockwise loop in
// (x, z) has a -Y face normal, so the raw output faces down while every vertex
// claims (0, 1, 0). Measure the total signed area and reverse every triangle
// when it faces the wrong way.
//
// Do not drop this step. A raycaster tests both sides of a triangle and the
// browser runtime culls no back faces today, so a reversed cap passes every
// other test here and turns invisible the day back-face culling arrives.
// scene/geom/shape.go guards its own earcut caps with the same measurement.
func orientPolygonUp(points []float64, indices []int) []int {
	if len(indices) < 3 {
		return nil
	}
	out := append([]int(nil), indices...)
	total := 0.0
	for i := 0; i+3 <= len(out); i += 3 {
		ax, az := points[out[i]*2], points[out[i]*2+1]
		bx, bz := points[out[i+1]*2], points[out[i+1]*2+1]
		cx, cz := points[out[i+2]*2], points[out[i+2]*2+1]
		total += (bx-ax)*(cz-az) - (bz-az)*(cx-ax)
	}
	if total > 0 {
		for i := 0; i+3 <= len(out); i += 3 {
			out[i+1], out[i+2] = out[i+2], out[i+1]
		}
	}
	return out
}
