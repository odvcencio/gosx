package scene

import (
	"fmt"
	"math"
)

// GenerateTangents derives area-weighted, orthonormal tangent frames from a
// triangle mesh's UVs. It supports indexed and flat triangle streams, preserves
// every existing stream and returns independently owned Tangents. Split vertices
// at UV/normal seams before calling it. Degenerate UVs use a finite perpendicular
// basis; malformed geometry is rejected without modifying the input.
func GenerateTangents(g BufferGeometry) (BufferGeometry, error) {
	n := len(g.Positions) / 3
	if len(g.Positions)%3 != 0 || len(g.Normals) != n*3 || len(g.UVs) != n*2 {
		return g, fmt.Errorf("tangents require complete position, normal and UV streams")
	}
	for _, stream := range [][]float64{g.Positions, g.Normals, g.UVs} {
		for _, v := range stream {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return g, fmt.Errorf("tangents require finite vertex attributes")
			}
		}
	}
	count := len(g.Indices)
	if count == 0 {
		count = n
	} else {
		for _, index := range g.Indices {
			if index < 0 || index >= n {
				return g, fmt.Errorf("tangent vertex index %d outside [0,%d)", index, n)
			}
		}
	}
	if count%3 != 0 {
		return g, fmt.Errorf("tangents require complete triangles")
	}
	tangent, bitangent := make([]float64, n*3), make([]float64, n*3)
	for i := 0; i < count; i += 3 {
		a, b, c := i, i+1, i+2
		if len(g.Indices) > 0 {
			a, b, c = g.Indices[i], g.Indices[i+1], g.Indices[i+2]
		}
		x1, y1, z1 := g.Positions[3*b]-g.Positions[3*a], g.Positions[3*b+1]-g.Positions[3*a+1], g.Positions[3*b+2]-g.Positions[3*a+2]
		x2, y2, z2 := g.Positions[3*c]-g.Positions[3*a], g.Positions[3*c+1]-g.Positions[3*a+1], g.Positions[3*c+2]-g.Positions[3*a+2]
		u1, v1 := g.UVs[2*b]-g.UVs[2*a], g.UVs[2*b+1]-g.UVs[2*a+1]
		u2, v2 := g.UVs[2*c]-g.UVs[2*a], g.UVs[2*c+1]-g.UVs[2*a+1]
		det := u1*v2 - v1*u2
		if math.Abs(det) < 1e-20 {
			continue
		}
		nx, ny, nz := y1*z2-z1*y2, z1*x2-x1*z2, x1*y2-y1*x2
		weight := math.Sqrt(nx*nx+ny*ny+nz*nz) / det
		tx, ty, tz := (x1*v2-x2*v1)*weight, (y1*v2-y2*v1)*weight, (z1*v2-z2*v1)*weight
		bx, by, bz := (x2*u1-x1*u2)*weight, (y2*u1-y1*u2)*weight, (z2*u1-z1*u2)*weight
		for _, index := range [3]int{a, b, c} {
			tangent[3*index] += tx
			tangent[3*index+1] += ty
			tangent[3*index+2] += tz
			bitangent[3*index] += bx
			bitangent[3*index+1] += by
			bitangent[3*index+2] += bz
		}
	}
	frames := make([]float64, n*4)
	for i := 0; i < n; i++ {
		nx, ny, nz := g.Normals[3*i], g.Normals[3*i+1], g.Normals[3*i+2]
		nlen := math.Sqrt(nx*nx + ny*ny + nz*nz)
		if nlen < 1e-15 || math.IsInf(nlen, 0) {
			return g, fmt.Errorf("tangent vertex %d has an unusable normal", i)
		}
		nx, ny, nz = nx/nlen, ny/nlen, nz/nlen
		tx, ty, tz := tangent[3*i], tangent[3*i+1], tangent[3*i+2]
		dot := nx*tx + ny*ty + nz*tz
		tx, ty, tz = tx-nx*dot, ty-ny*dot, tz-nz*dot
		length := math.Sqrt(tx*tx + ty*ty + tz*tz)
		if length < 1e-15 {
			if math.Abs(nx) < .8 {
				tx, ty, tz = 1-nx*nx, -nx*ny, -nx*nz
			} else {
				tx, ty, tz = -ny*nx, 1-ny*ny, -ny*nz
			}
			length = math.Sqrt(tx*tx + ty*ty + tz*tz)
		}
		tx, ty, tz = tx/length, ty/length, tz/length
		if math.IsNaN(tx) || math.IsNaN(ty) || math.IsNaN(tz) || math.IsInf(tx, 0) || math.IsInf(ty, 0) || math.IsInf(tz, 0) {
			return g, fmt.Errorf("tangent vertex %d exceeds finite frame range", i)
		}
		bx, by, bz := bitangent[3*i], bitangent[3*i+1], bitangent[3*i+2]
		w := 1.0
		if (ny*tz-nz*ty)*bx+(nz*tx-nx*tz)*by+(nx*ty-ny*tx)*bz < 0 {
			w = -1
		}
		frames[4*i], frames[4*i+1], frames[4*i+2], frames[4*i+3] = tx, ty, tz, w
	}
	g.Tangents = frames
	return g, nil
}
