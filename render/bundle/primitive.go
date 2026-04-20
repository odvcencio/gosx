package bundle

import "math"

// primitiveGeometry is the CPU-side geometry for a named primitive Kind.
// positions, colors, and normals are 3 floats per vertex; uvs are 2. UVs
// follow standard 0..1 face conventions: cube maps each face unit-square,
// plane matches the extent, sphere uses longitude/latitude.
// R1/R2 use non-indexed draws; indices come with R3.
type primitiveGeometry struct {
	positions   []float32
	colors      []float32
	normals     []float32
	uvs         []float32
	vertexCount int
}

// primitiveForKind returns the geometry for one of the supported primitive
// mesh kinds. Unknown kinds return nil and the caller skips the draw.
//
// Kinds are intentionally named to match what scene/scene_ir.go emits for
// RenderInstancedMesh.Kind. The set is deliberately small for R1; R2 adds
// cylinder/cone/torus and loader-backed custom meshes.
func primitiveForKind(kind string) *primitiveGeometry {
	switch kind {
	case "cube", "box", "boxGeometry":
		return cubeGeometry()
	case "plane", "planeGeometry":
		return planeGeometry()
	case "sphere", "sphereGeometry":
		return sphereGeometry(16, 12)
	}
	return nil
}

// cubeGeometry produces a unit cube centered on the origin with extent [-1,1]
// on each axis. Each face has a constant normal and a face-indexed color;
// vertices are duplicated per face so flat shading reads cleanly.
func cubeGeometry() *primitiveGeometry {
	faces := []struct {
		corners [4][3]float32
		normal  [3]float32
		color   [3]float32
	}{
		{[4][3]float32{{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1}}, [3]float32{0, 0, 1}, [3]float32{1, 0.3, 0.2}},       // +Z
		{[4][3]float32{{1, -1, -1}, {-1, -1, -1}, {-1, 1, -1}, {1, 1, -1}}, [3]float32{0, 0, -1}, [3]float32{0.2, 0.8, 0.3}}, // -Z
		{[4][3]float32{{-1, 1, 1}, {1, 1, 1}, {1, 1, -1}, {-1, 1, -1}}, [3]float32{0, 1, 0}, [3]float32{0.3, 0.5, 1}},        // +Y
		{[4][3]float32{{-1, -1, -1}, {1, -1, -1}, {1, -1, 1}, {-1, -1, 1}}, [3]float32{0, -1, 0}, [3]float32{1, 0.9, 0.2}},   // -Y
		{[4][3]float32{{1, -1, 1}, {1, -1, -1}, {1, 1, -1}, {1, 1, 1}}, [3]float32{1, 0, 0}, [3]float32{0.9, 0.2, 0.8}},      // +X
		{[4][3]float32{{-1, -1, -1}, {-1, -1, 1}, {-1, 1, 1}, {-1, 1, -1}}, [3]float32{-1, 0, 0}, [3]float32{0.2, 0.9, 0.9}}, // -X
	}

	// Standard quad UVs matching the corner order [bl, br, tr, tl].
	cornerUVs := [4][2]float32{{0, 1}, {1, 1}, {1, 0}, {0, 0}}
	pos := make([]float32, 0, 6*6*3)
	col := make([]float32, 0, 6*6*3)
	nrm := make([]float32, 0, 6*6*3)
	uvs := make([]float32, 0, 6*6*2)
	tris := [][3]int{{0, 1, 2}, {0, 2, 3}}
	for _, face := range faces {
		for _, tri := range tris {
			for _, idx := range tri {
				c := face.corners[idx]
				uv := cornerUVs[idx]
				pos = append(pos, c[0], c[1], c[2])
				col = append(col, face.color[0], face.color[1], face.color[2])
				nrm = append(nrm, face.normal[0], face.normal[1], face.normal[2])
				uvs = append(uvs, uv[0], uv[1])
			}
		}
	}
	return &primitiveGeometry{
		positions:   pos,
		colors:      col,
		normals:     nrm,
		uvs:         uvs,
		vertexCount: len(pos) / 3,
	}
}

// planeGeometry produces a unit XZ-plane at y=0 with extent [-1,1] on x and z,
// normal +Y. Two triangles, shaded neutral so lighting + shadow read clearly.
// UVs tile once over the quad.
func planeGeometry() *primitiveGeometry {
	corners := [4][3]float32{{-1, 0, -1}, {1, 0, -1}, {1, 0, 1}, {-1, 0, 1}}
	cornerUVs := [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	normal := [3]float32{0, 1, 0}
	color := [3]float32{0.7, 0.72, 0.75}
	tris := [][3]int{{0, 2, 1}, {0, 3, 2}}
	pos := make([]float32, 0, 6*3)
	col := make([]float32, 0, 6*3)
	nrm := make([]float32, 0, 6*3)
	uvs := make([]float32, 0, 6*2)
	for _, tri := range tris {
		for _, idx := range tri {
			c := corners[idx]
			uv := cornerUVs[idx]
			pos = append(pos, c[0], c[1], c[2])
			col = append(col, color[0], color[1], color[2])
			nrm = append(nrm, normal[0], normal[1], normal[2])
			uvs = append(uvs, uv[0], uv[1])
		}
	}
	return &primitiveGeometry{
		positions:   pos,
		colors:      col,
		normals:     nrm,
		uvs:         uvs,
		vertexCount: len(pos) / 3,
	}
}

// sphereGeometry produces a UV-sphere with the given latitude/longitude
// subdivisions. Normals are the outward unit vector at each vertex — on a
// unit sphere, position and normal are the same. A soft gradient color gives
// the unlit fallback path a visible cue.
func sphereGeometry(longitudes, latitudes int) *primitiveGeometry {
	if longitudes < 3 {
		longitudes = 3
	}
	if latitudes < 2 {
		latitudes = 2
	}
	type vert struct{ x, y, z, r, g, b, nx, ny, nz, u, v float32 }
	verts := make([][]vert, latitudes+1)
	for lat := 0; lat <= latitudes; lat++ {
		theta := float64(lat) * math.Pi / float64(latitudes)
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		row := make([]vert, longitudes+1)
		for lon := 0; lon <= longitudes; lon++ {
			phi := float64(lon) * 2 * math.Pi / float64(longitudes)
			sinP, cosP := math.Sin(phi), math.Cos(phi)
			t := float32(lat) / float32(latitudes)
			x := float32(cosP * sinT)
			y := float32(cosT)
			z := float32(sinP * sinT)
			row[lon] = vert{
				x: x, y: y, z: z,
				r:  0.9 - 0.6*t,
				g:  0.3 + 0.5*t,
				b:  0.4 + 0.5*(1-t),
				nx: x, ny: y, nz: z,
				u: float32(lon) / float32(longitudes),
				v: t,
			}
		}
		verts[lat] = row
	}
	pos := make([]float32, 0, latitudes*longitudes*6*3)
	col := make([]float32, 0, latitudes*longitudes*6*3)
	nrm := make([]float32, 0, latitudes*longitudes*6*3)
	uvs := make([]float32, 0, latitudes*longitudes*6*2)
	for lat := 0; lat < latitudes; lat++ {
		for lon := 0; lon < longitudes; lon++ {
			a := verts[lat][lon]
			b := verts[lat][lon+1]
			c := verts[lat+1][lon+1]
			d := verts[lat+1][lon]
			for _, vx := range []vert{a, b, c, a, c, d} {
				pos = append(pos, vx.x, vx.y, vx.z)
				col = append(col, vx.r, vx.g, vx.b)
				nrm = append(nrm, vx.nx, vx.ny, vx.nz)
				uvs = append(uvs, vx.u, vx.v)
			}
		}
	}
	return &primitiveGeometry{
		positions:   pos,
		colors:      col,
		normals:     nrm,
		uvs:         uvs,
		vertexCount: len(pos) / 3,
	}
}
