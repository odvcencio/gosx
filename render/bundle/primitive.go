package bundle

import "math"

// primitiveGeometry is the CPU-side geometry for a named primitive Kind.
// positions and colors are interleaved-safe (each 3 floats per vertex).
// vertexCount is the triangle-list vertex count: positions is vertexCount*3
// floats long. R1 uses non-indexed draws; indices come with R2.
type primitiveGeometry struct {
	positions   []float32
	colors      []float32
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
// on each axis. Each of the 6 faces gets a solid color so the instance
// transforms are visually legible without needing lighting.
func cubeGeometry() *primitiveGeometry {
	faces := []struct {
		corners [4][3]float32
		color   [3]float32
	}{
		{[4][3]float32{{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1}}, [3]float32{1, 0.3, 0.2}},      // +Z
		{[4][3]float32{{1, -1, -1}, {-1, -1, -1}, {-1, 1, -1}, {1, 1, -1}}, [3]float32{0.2, 0.8, 0.3}}, // -Z
		{[4][3]float32{{-1, 1, 1}, {1, 1, 1}, {1, 1, -1}, {-1, 1, -1}}, [3]float32{0.3, 0.5, 1}},       // +Y
		{[4][3]float32{{-1, -1, -1}, {1, -1, -1}, {1, -1, 1}, {-1, -1, 1}}, [3]float32{1, 0.9, 0.2}},   // -Y
		{[4][3]float32{{1, -1, 1}, {1, -1, -1}, {1, 1, -1}, {1, 1, 1}}, [3]float32{0.9, 0.2, 0.8}},     // +X
		{[4][3]float32{{-1, -1, -1}, {-1, -1, 1}, {-1, 1, 1}, {-1, 1, -1}}, [3]float32{0.2, 0.9, 0.9}}, // -X
	}

	pos := make([]float32, 0, 6*6*3)
	col := make([]float32, 0, 6*6*3)
	tris := [][3]int{{0, 1, 2}, {0, 2, 3}}
	for _, face := range faces {
		for _, tri := range tris {
			for _, idx := range tri {
				c := face.corners[idx]
				pos = append(pos, c[0], c[1], c[2])
				col = append(col, face.color[0], face.color[1], face.color[2])
			}
		}
	}
	return &primitiveGeometry{positions: pos, colors: col, vertexCount: len(pos) / 3}
}

// planeGeometry produces a unit XZ-plane at y=0 with extent [-1,1] on x and z.
// Two triangles, shaded a soft neutral so instance transforms read clearly.
func planeGeometry() *primitiveGeometry {
	corners := [4][3]float32{{-1, 0, -1}, {1, 0, -1}, {1, 0, 1}, {-1, 0, 1}}
	color := [3]float32{0.7, 0.72, 0.75}
	tris := [][3]int{{0, 2, 1}, {0, 3, 2}}
	pos := make([]float32, 0, 6*3)
	col := make([]float32, 0, 6*3)
	for _, tri := range tris {
		for _, idx := range tri {
			c := corners[idx]
			pos = append(pos, c[0], c[1], c[2])
			col = append(col, color[0], color[1], color[2])
		}
	}
	return &primitiveGeometry{positions: pos, colors: col, vertexCount: len(pos) / 3}
}

// sphereGeometry produces a UV-sphere with the given latitude/longitude
// subdivisions. Colored by a simple gradient from pole-to-pole so the instance
// orientation is visible without normals/lighting.
func sphereGeometry(longitudes, latitudes int) *primitiveGeometry {
	if longitudes < 3 {
		longitudes = 3
	}
	if latitudes < 2 {
		latitudes = 2
	}
	type vert struct{ x, y, z, r, g, b float32 }
	verts := make([][]vert, latitudes+1)
	for lat := 0; lat <= latitudes; lat++ {
		theta := float64(lat) * math.Pi / float64(latitudes)
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		row := make([]vert, longitudes+1)
		for lon := 0; lon <= longitudes; lon++ {
			phi := float64(lon) * 2 * math.Pi / float64(longitudes)
			sinP, cosP := math.Sin(phi), math.Cos(phi)
			t := float32(lat) / float32(latitudes)
			row[lon] = vert{
				x: float32(cosP * sinT),
				y: float32(cosT),
				z: float32(sinP * sinT),
				r: 0.9 - 0.6*t,
				g: 0.3 + 0.5*t,
				b: 0.4 + 0.5*(1-t),
			}
		}
		verts[lat] = row
	}
	pos := make([]float32, 0, latitudes*longitudes*6*3)
	col := make([]float32, 0, latitudes*longitudes*6*3)
	for lat := 0; lat < latitudes; lat++ {
		for lon := 0; lon < longitudes; lon++ {
			a := verts[lat][lon]
			b := verts[lat][lon+1]
			c := verts[lat+1][lon+1]
			d := verts[lat+1][lon]
			// Two triangles per quad, CCW winding against outward normal.
			for _, v := range []vert{a, b, c, a, c, d} {
				pos = append(pos, v.x, v.y, v.z)
				col = append(col, v.r, v.g, v.b)
			}
		}
	}
	return &primitiveGeometry{positions: pos, colors: col, vertexCount: len(pos) / 3}
}
