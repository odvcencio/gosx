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
// a uniform upward-facing (0, 1, 0) per vertex. UVs are omitted — callers
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

	indices := earcut.Triangulate(vertices, holeIndices, 2)
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
