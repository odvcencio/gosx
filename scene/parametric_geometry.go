package scene

import "m31labs.dev/gosx/scene/geom"

// ParametricSurface samples one point of an arbitrary surface. u and v arrive
// normalized in [0, 1] and the return carries the XYZ position of that point,
// exactly as Three.js's ParametricGeometry addon feeds its callback.
type ParametricSurface func(u, v float64) Vector3

// ParametricGeometry samples surface over a (slices × stacks) grid and returns
// an indexed BufferGeometry with positions, normals, UVs, and indices. It is the
// generator-based counterpart of the named primitive kinds: the mesh lowers to
// inline vertices and raycasts through the exact triangle path with no browser
// runtime code of its own.
//
// A nil surface returns a zero-value BufferGeometry. If the surface reports a
// NaN or Inf coordinate anywhere on the grid, the geometry comes back empty
// rather than partial or non-finite.
//
// slices and stacks each fall back to 8 and stay inside [1, 512]. Edge rows and
// columns remain distinct even when a periodic surface maps them to identical
// positions, so UV seams stay honest.
func ParametricGeometry(surface ParametricSurface, slices, stacks int) BufferGeometry {
	if surface == nil {
		return BufferGeometry{}
	}
	mesh := geom.Parametric(func(u, v float64) (float64, float64, float64) {
		p := surface(u, v)
		return p.X, p.Y, p.Z
	}, slices, stacks, generatorAttributes)
	return bufferFromMesh(mesh)
}
