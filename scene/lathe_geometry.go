package scene

import "m31labs.dev/gosx/scene/geom"

// LathePoint is one authored sample of a lathe profile: a radius at a
// given height Y. The profile is revolved around the Y axis.
type LathePoint struct {
	Radius float64
	Y      float64
}

// LatheGeometry revolves the authored profile around the Y axis and returns
// an indexed BufferGeometry with positions, normals, UVs, and indices.
func LatheGeometry(profile []LathePoint, segments int, phiStart, phiLength float64) BufferGeometry {
	flat := make([]float64, 0, len(profile)*2)
	for _, p := range profile {
		flat = append(flat, p.Radius, p.Y)
	}
	mesh := geom.Lathe(flat, segments, phiStart, phiLength, generatorAttributes)
	return bufferFromMesh(mesh)
}
