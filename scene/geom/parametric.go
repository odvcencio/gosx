package geom

import "math"

// This file generates arbitrary surfaces from an authored callback, the shape
// Three.js builds with its ParametricGeometry addon. The callback answers one
// XYZ position per normalized (u, v) sample and the generator turns those
// samples into the same indexed shared-vertex mesh every other generator here
// emits, so the raycaster and the native renderer see nothing special.
//
// Edge rows and columns stay distinct even when a periodic surface maps them to
// identical positions, which keeps UV seams honest.

const (
	parametricDefaultSegments = 8
	parametricMinSegments     = 1
	parametricMaxSegments     = 512
)

// SurfaceFunc samples one point of a parametric surface. u and v arrive
// normalized in [0, 1] and the return carries the XYZ position of that grid
// point. Returning any NaN or Inf coordinate rejects the whole build.
type SurfaceFunc func(u, v float64) (x, y, z float64)

// ParametricVertexCount reports how many shared vertices Parametric emits for
// the given resolution, without building. slices and stacks resolve through the
// same default and clamp Parametric applies.
func ParametricVertexCount(slices, stacks int) int {
	slices, stacks = parametricGrid(slices, stacks)
	return (slices + 1) * (stacks + 1)
}

// Parametric samples surface over a (slices × stacks) grid and returns the
// indexed surface mesh, or nil for input it cannot honor.
//
// A nil callback returns nil. If the callback reports a NaN or Inf coordinate at
// any sample, Parametric returns nil instead of emitting a partial or
// non-finite mesh.
//
// The callback runs exactly once per grid point, in stable row-major order from
// (0, 0) through (1, 1), so callback cost and determinism are auditable. The
// mesh keeps every edge row and column as distinct shared vertices even when a
// periodic surface maps them to the same position, so U=0 and U=1 stay
// separable across a seam.
//
// Normals come from finite differences over the sampled positions rather than
// extra callback calls: centered differences inside the grid and one-sided
// differences at the borders, oriented along du × dv to match the index
// winding. A singular or degenerate sample falls back to +Y, so no buffer ever
// carries NaN or Inf.
//
// slices and stacks each fall back to 8 and clamp into [1, 512]. want selects
// which streams fill; PositionsOnly leaves Normals and UVs nil.
func Parametric(surface SurfaceFunc, slices, stacks int, want Attribute) *Mesh {
	if surface == nil {
		return nil
	}
	slices, stacks = parametricGrid(slices, stacks)
	cols := slices + 1 // vertices per row, along u
	rows := stacks + 1 // vertices per column, along v

	gridU := make([]float64, cols)
	for i := range gridU {
		gridU[i] = float64(i) / float64(slices)
	}
	gridV := make([]float64, rows)
	for j := range gridV {
		gridV[j] = float64(j) / float64(stacks)
	}

	// One callback call per grid point, row-major from (0, 0) through (1, 1).
	// A temporary position grid is expected here; it is sized from the
	// resolved vertex count and lets normals reuse the samples.
	points := make([]vec3, rows*cols)
	valid := true
	for j := 0; j < rows; j++ {
		for i := 0; i < cols; i++ {
			x, y, z := surface(gridU[i], gridV[j])
			if !finiteXYZ(x, y, z) {
				valid = false
			}
			points[j*cols+i] = vec3{X: x, Y: y, Z: z}
		}
	}
	if !valid {
		return nil
	}

	var normals []vec3
	if want&AttrNormals != 0 {
		normals = parametricNormals(points, cols, rows)
	}

	b := newBuilder(want, rows*cols)
	k := 0
	for j := 0; j < rows; j++ {
		for i := 0; i < cols; i++ {
			v := vertex{position: points[k], uv: vec2{U: gridU[i], V: gridV[j]}}
			if normals != nil {
				v.normal = normals[k]
			}
			b.emit(v)
			k++
		}
	}
	for j := 0; j < stacks; j++ {
		for i := 0; i < slices; i++ {
			a := j*cols + i
			right := a + 1   // one step along du
			down := a + cols // one step along dv
			far := down + 1  // both steps
			b.index(a, right, far)
			b.index(a, far, down)
		}
	}
	return b.build()
}

// parametricNormals derives one unit normal per grid vertex from finite
// differences of the sampled positions, oriented along du × dv. Centered
// differences run inside the grid; the borders take one-sided differences. A
// singular or degenerate sample normalizes to the deterministic +Y fallback,
// which keeps every emitted normal finite.
func parametricNormals(points []vec3, cols, rows int) []vec3 {
	at := func(i, j int) vec3 { return points[j*cols+i] }
	normals := make([]vec3, len(points))
	for j := 0; j < rows; j++ {
		for i := 0; i < cols; i++ {
			var du, dv vec3
			switch i {
			case 0:
				du = subVec(at(1, j), at(0, j))
			case cols - 1:
				du = subVec(at(i, j), at(i-1, j))
			default:
				du = subVec(at(i+1, j), at(i-1, j))
			}
			switch j {
			case 0:
				dv = subVec(at(i, 1), at(i, 0))
			case rows - 1:
				dv = subVec(at(i, j), at(i, j-1))
			default:
				dv = subVec(at(i, j+1), at(i, j-1))
			}
			normals[j*cols+i] = normalize(crossVec(du, dv))
		}
	}
	return normals
}

// parametricGrid resolves the authored counts through the shared default and
// clamp.
func parametricGrid(slices, stacks int) (int, int) {
	slices = ClampInt(slices, parametricDefaultSegments, parametricMinSegments, parametricMaxSegments)
	stacks = ClampInt(stacks, parametricDefaultSegments, parametricMinSegments, parametricMaxSegments)
	return slices, stacks
}

// finiteXYZ reports whether all three coordinates are finite.
func finiteXYZ(x, y, z float64) bool {
	return !(math.IsNaN(x) || math.IsInf(x, 0) ||
		math.IsNaN(y) || math.IsInf(y, 0) ||
		math.IsNaN(z) || math.IsInf(z, 0))
}
