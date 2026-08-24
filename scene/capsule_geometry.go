package scene

import "m31labs.dev/gosx/scene/geom"

// CapsuleGeometryOptions names the capsule CapsuleGeometry builds. Length is
// the straight cylindrical body length along Y, matching Three.js
// CapsuleGeometry terminology; the caps of Radius sit on both ends. Every zero
// or invalid field takes a default: Radius and Length fall back to 1,
// CapSegments to 4 inside [1, 64], and RadialSegments to 8 inside [3, 128].
type CapsuleGeometryOptions struct {
	Radius         float64
	Length         float64
	CapSegments    int
	RadialSegments int
}

// CapsuleGeometry builds a Y-axis capsule centered at the origin as an indexed
// BufferGeometry with positions, normals, UVs, and indices. The total Y extent
// is Length + 2*Radius.
//
// Like every generator in this file it costs no browser runtime bytes: the mesh
// lowers to inline vertices that both the renderer and the exact raycaster read.
func CapsuleGeometry(opts CapsuleGeometryOptions) BufferGeometry {
	return bufferFromMesh(geom.Capsule(opts.Radius, opts.Length, opts.CapSegments, opts.RadialSegments, generatorAttributes))
}
