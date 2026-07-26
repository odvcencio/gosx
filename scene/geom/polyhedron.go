package geom

import "math"

// This file builds the five platonic-family solids. All four named solids share
// one subdivider: Polyhedron takes a base hull of triangles, splits each face
// into (detail+1)^2 sub-triangles, and pushes every point onto the sphere of the
// given radius. That is the same construction the three.js PolyhedronGeometry
// family uses, so an authored radius and detail carry the same meaning here.
//
// The meshes are non-indexed. A detail of zero must keep hard edges, and a hard
// edge needs one normal per face, not one normal per shared vertex.

// Polyhedron subdivides a base hull and projects it onto a sphere.
//
// vertices holds the base hull points as flat xyz triples. indices holds three
// base vertex numbers per base face, wound counter-clockwise as seen from
// outside. radius is the sphere the result sits on. detail is the number of
// times each edge is split; zero keeps the base faces.
//
// A detail of zero gives flat normals, so each face reads as a facet. A detail
// above zero gives smooth normals, because every point already sits on the
// sphere and the outward direction is the point itself.
//
// A degenerate input, such as fewer than three vertices or no whole triangle,
// returns nil.
func Polyhedron(vertices []float64, indices []int, radius float64, detail int, want Attribute) *Mesh {
	if len(vertices) < 9 || len(indices) < 3 {
		return nil
	}
	radius = PositiveOr(radius, 1)
	if detail < 0 {
		detail = 0
	}
	if detail > 5 {
		// Each step multiplies the triangle count by (detail+1)^2. Five steps on
		// an icosahedron already give 720 faces. Stop there so a typo cannot ask
		// for a gigabyte of vertices.
		detail = 5
	}

	faces := len(indices) / 3
	cols := detail + 1
	b := newBuilder(want, faces*cols*cols*3)

	pointAt := func(index int) vec3 {
		base := index * 3
		if index < 0 || base+3 > len(vertices) {
			return vec3{}
		}
		return vec3{X: vertices[base], Y: vertices[base+1], Z: vertices[base+2]}
	}
	onSphere := func(v vec3) vec3 { return scaleVec(normalize(v), radius) }

	for face := 0; face < faces; face++ {
		a := pointAt(indices[face*3])
		c := pointAt(indices[face*3+1])
		d := pointAt(indices[face*3+2])
		subdivideFace(b, a, c, d, cols, onSphere, detail == 0)
	}

	mesh := b.build()
	if want&AttrUVs != 0 {
		correctPolyhedronSeam(mesh)
		correctPolyhedronPoles(mesh)
	}
	if want&AttrColors != 0 {
		fillSphericalColors(mesh, radius)
	}
	return mesh
}

// subdivideFace splits one base triangle into cols^2 sub-triangles and emits
// them. It walks the face row by row from corner a toward the edge c-d. Row i
// starts on the edge a-c and ends on the edge a-d, so every sub-triangle keeps
// the winding of the base face a, c, d.
func subdivideFace(b *builder, a, c, d vec3, cols int, onSphere func(vec3) vec3, flat bool) {
	// grid[i] holds the points along the row i steps down from a.
	grid := make([][]vec3, cols+1)
	for i := 0; i <= cols; i++ {
		startEdge := lerpVec(a, c, float64(i)/float64(cols))
		endEdge := lerpVec(a, d, float64(i)/float64(cols))
		row := make([]vec3, i+1)
		for j := 0; j <= i; j++ {
			if i == 0 {
				row[j] = onSphere(a)
				continue
			}
			row[j] = onSphere(lerpVec(startEdge, endEdge, float64(j)/float64(i)))
		}
		grid[i] = row
	}

	emitTriangle := func(p0, p1, p2 vec3) {
		if flat {
			b.flatTri(p0, p1, p2, vec3{}, sphericalUV(p0), sphericalUV(p1), sphericalUV(p2))
			return
		}
		b.tri(
			vertex{position: p0, normal: normalize(p0), uv: sphericalUV(p0)},
			vertex{position: p1, normal: normalize(p1), uv: sphericalUV(p1)},
			vertex{position: p2, normal: normalize(p2), uv: sphericalUV(p2)},
		)
	}

	for i := 0; i < cols; i++ {
		for j := 0; j < i+1; j++ {
			emitTriangle(grid[i][j], grid[i+1][j], grid[i+1][j+1])
			if j < i {
				emitTriangle(grid[i][j], grid[i+1][j+1], grid[i][j+1])
			}
		}
	}
}

func lerpVec(a, b vec3, t float64) vec3 {
	return vec3{
		X: a.X + (b.X-a.X)*t,
		Y: a.Y + (b.Y-a.Y)*t,
		Z: a.Z + (b.Z-a.Z)*t,
	}
}

// sphericalUV maps a direction onto the unit square by azimuth and inclination.
// The seam at azimuth 0 and the two poles need a repair pass; see
// correctPolyhedronSeam and correctPolyhedronPoles.
func sphericalUV(v vec3) vec2 {
	azimuth := math.Atan2(v.Z, -v.X)
	inclination := math.Atan2(-v.Y, math.Hypot(v.X, v.Z))
	return vec2{
		U: azimuth/(2*math.Pi) + 0.5,
		V: 1 - (inclination/math.Pi + 0.5),
	}
}

// correctPolyhedronSeam repairs the triangles that cross the azimuth wrap. One
// corner reads a u near 1 while the others read a u near 0, which stretches the
// texture across the whole sphere. Lifting the small u values by one keeps the
// triangle continuous.
func correctPolyhedronSeam(mesh *Mesh) {
	uvs := mesh.UVs
	for i := 0; i+6 <= len(uvs); i += 6 {
		u0, u1, u2 := uvs[i], uvs[i+2], uvs[i+4]
		high := math.Max(u0, math.Max(u1, u2))
		low := math.Min(u0, math.Min(u1, u2))
		if high <= 0.9 || low >= 0.1 {
			continue
		}
		for k := 0; k < 3; k++ {
			if uvs[i+k*2] < 0.2 {
				uvs[i+k*2] += 1
			}
		}
	}
}

// correctPolyhedronPoles repairs the triangles that touch a pole. The azimuth of
// a pole is undefined, so its u is meaningless. Take the average of the other
// two corners instead.
func correctPolyhedronPoles(mesh *Mesh) {
	positions, uvs := mesh.Positions, mesh.UVs
	for i := 0; i+9 <= len(positions) && i/3*2+6 <= len(uvs); i += 9 {
		base := i / 3 * 2
		for k := 0; k < 3; k++ {
			x, z := positions[i+k*3], positions[i+k*3+2]
			if math.Hypot(x, z) > 1e-9 {
				continue
			}
			other0 := uvs[base+((k+1)%3)*2]
			other1 := uvs[base+((k+2)%3)*2]
			uvs[base+k*2] = (other0 + other1) / 2
		}
	}
}

// fillSphericalColors gives the unlit fallback path a visible cue. The color
// follows height, the same way the UV sphere colors its rows.
func fillSphericalColors(mesh *Mesh, radius float64) {
	if radius <= 0 {
		radius = 1
	}
	mesh.Colors = make([]float64, 0, len(mesh.Positions))
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		t := 0.5 - mesh.Positions[i+1]/(2*radius)
		if t < 0 {
			t = 0
		}
		if t > 1 {
			t = 1
		}
		mesh.Colors = append(mesh.Colors, 0.9-0.6*t, 0.3+0.5*t, 0.4+0.5*(1-t))
	}
}

// The four base hulls below match the three.js hulls point for point and face
// for face, so an authored radius and detail give the same solid in GoSX and in
// a three.js reference. Every face is wound counter-clockwise as seen from
// outside; TestBaseHullsFaceOutward proves it.

// TetrahedronHull returns the base hull of a regular tetrahedron.
func TetrahedronHull() ([]float64, []int) {
	return []float64{
			1, 1, 1,
			-1, -1, 1,
			-1, 1, -1,
			1, -1, -1,
		}, []int{
			2, 1, 0,
			0, 3, 2,
			1, 3, 0,
			2, 3, 1,
		}
}

// OctahedronHull returns the base hull of a regular octahedron.
func OctahedronHull() ([]float64, []int) {
	return []float64{
			1, 0, 0,
			-1, 0, 0,
			0, 1, 0,
			0, -1, 0,
			0, 0, 1,
			0, 0, -1,
		}, []int{
			0, 2, 4,
			0, 4, 3,
			0, 3, 5,
			0, 5, 2,
			1, 2, 5,
			1, 5, 3,
			1, 3, 4,
			1, 4, 2,
		}
}

// IcosahedronHull returns the base hull of a regular icosahedron. The points are
// the twelve corners of three orthogonal golden rectangles.
func IcosahedronHull() ([]float64, []int) {
	t := (1 + math.Sqrt(5)) / 2
	return []float64{
			-1, t, 0, 1, t, 0, -1, -t, 0, 1, -t, 0,
			0, -1, t, 0, 1, t, 0, -1, -t, 0, 1, -t,
			t, 0, -1, t, 0, 1, -t, 0, -1, -t, 0, 1,
		}, []int{
			0, 11, 5, 0, 5, 1, 0, 1, 7, 0, 7, 10, 0, 10, 11,
			1, 5, 9, 5, 11, 4, 11, 10, 2, 10, 7, 6, 7, 1, 8,
			3, 9, 4, 3, 4, 2, 3, 2, 6, 3, 6, 8, 3, 8, 9,
			4, 9, 5, 2, 4, 11, 6, 2, 10, 8, 6, 7, 9, 8, 1,
		}
}

// DodecahedronHull returns the base hull of a regular dodecahedron. Each of the
// twelve pentagon faces is already split into three triangles, so the hull holds
// 36 faces.
func DodecahedronHull() ([]float64, []int) {
	t := (1 + math.Sqrt(5)) / 2
	r := 1 / t
	return []float64{
			// The eight corners of a cube, at (+/-1, +/-1, +/-1).
			-1, -1, -1, -1, -1, 1,
			-1, 1, -1, -1, 1, 1,
			1, -1, -1, 1, -1, 1,
			1, 1, -1, 1, 1, 1,

			// The rectangle in the YZ plane, at (0, +/-1/phi, +/-phi).
			0, -r, -t, 0, -r, t,
			0, r, -t, 0, r, t,

			// The rectangle in the XY plane, at (+/-1/phi, +/-phi, 0).
			-r, -t, 0, -r, t, 0,
			r, -t, 0, r, t, 0,

			// The rectangle in the XZ plane, at (+/-phi, 0, +/-1/phi).
			-t, 0, -r, t, 0, -r,
			-t, 0, r, t, 0, r,
		}, []int{
			3, 11, 7, 3, 7, 15, 3, 15, 13,
			7, 19, 17, 7, 17, 6, 7, 6, 15,
			17, 4, 8, 17, 8, 10, 17, 10, 6,
			8, 0, 16, 8, 16, 2, 8, 2, 10,
			0, 12, 1, 0, 1, 18, 0, 18, 16,
			6, 10, 2, 6, 2, 13, 6, 13, 15,
			2, 16, 18, 2, 18, 3, 2, 3, 13,
			18, 1, 9, 18, 9, 11, 18, 11, 3,
			4, 14, 12, 4, 12, 0, 4, 0, 8,
			11, 9, 5, 11, 5, 19, 11, 19, 7,
			19, 5, 14, 19, 14, 4, 19, 4, 17,
			1, 12, 14, 1, 14, 5, 1, 5, 9,
		}
}

// Tetrahedron builds a regular tetrahedron on the sphere of the given radius.
func Tetrahedron(radius float64, detail int, want Attribute) *Mesh {
	vertices, indices := TetrahedronHull()
	return Polyhedron(vertices, indices, radius, detail, want)
}

// Octahedron builds a regular octahedron on the sphere of the given radius.
func Octahedron(radius float64, detail int, want Attribute) *Mesh {
	vertices, indices := OctahedronHull()
	return Polyhedron(vertices, indices, radius, detail, want)
}

// Icosahedron builds a regular icosahedron on the sphere of the given radius.
func Icosahedron(radius float64, detail int, want Attribute) *Mesh {
	vertices, indices := IcosahedronHull()
	return Polyhedron(vertices, indices, radius, detail, want)
}

// Dodecahedron builds a regular dodecahedron on the sphere of the given radius.
func Dodecahedron(radius float64, detail int, want Attribute) *Mesh {
	vertices, indices := DodecahedronHull()
	return Polyhedron(vertices, indices, radius, detail, want)
}

// PolyhedronVertexCount returns how many vertices Polyhedron emits for a hull of
// faceCount base faces at the given detail. Memory reporting reads this.
func PolyhedronVertexCount(faceCount, detail int) int {
	if faceCount <= 0 {
		return 0
	}
	if detail < 0 {
		detail = 0
	}
	if detail > 5 {
		detail = 5
	}
	cols := detail + 1
	return faceCount * cols * cols * 3
}
