package scene

import "m31labs.dev/gosx/scene/geom"

// TubeGeometryOptions names one tube sweep.
type TubeGeometryOptions struct {
	// Radius is the cross-section radius. Zero or negative falls back to 1,
	// matching the generator's own defaulting.
	Radius float64

	// RadialSegments is the number of steps around each ring. It falls back
	// to 8 and stays inside [3, 128]. Each ring carries RadialSegments+1
	// vertices so U=0 and U=1 stay distinct seam vertices.
	RadialSegments int

	// Closed connects the last path point back to the first and emits a
	// duplicated seam ring, exactly like Three.js TubeGeometry's closed flag.
	Closed bool
}

// TubeGeometry sweeps a circular cross section along an arbitrary 3D path and
// returns the tube surface as an indexed BufferGeometry.
//
// The result matches the generator contract of package scene/geom: stable
// parallel-transport frames along the centerline, outward-facing triangles,
// UV U running once around each ring and UV V following cumulative centerline
// distance. An invalid path (wrong length, non-finite coordinates, too few
// points, or consecutive duplicate points) yields a zero-value BufferGeometry.
func TubeGeometry(path []Vector3, opts TubeGeometryOptions) BufferGeometry {
	flat := make([]float64, 0, len(path)*3)
	for _, point := range path {
		flat = append(flat, point.X, point.Y, point.Z)
	}
	return bufferFromMesh(geom.Tube(flat, opts.Radius, opts.RadialSegments, opts.Closed, generatorAttributes))
}
