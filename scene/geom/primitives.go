package geom

import "math"

// This file holds the parametric bodies. Every generator is centered on the
// origin and matches the browser runtime's generator in 12-scene-geometry.ts and
// 16c-scene-shared-pbr.ts. UVs follow the standard face conventions: a box maps
// each face to the unit square, a plane matches its extent, and a curved body
// uses wrapped cylindrical or parametric coordinates.
//
// The bodies stay non-indexed. That matches the renderer's vertex layout, lets
// flat and smooth normals live side by side without an index split, and keeps
// each upload as four tightly packed buffers.

// buildBox produces an axis-aligned box centered on the origin. Each face has a
// constant normal and a face color, so flat shading reads cleanly.
func buildBox(width, height, depth float64, want Attribute) *Mesh {
	hx := PositiveOr(width, 2) * 0.5
	hy := PositiveOr(height, 2) * 0.5
	hz := PositiveOr(depth, 2) * 0.5
	faces := []struct {
		corners [4]vec3
		normal  vec3
		color   vec3
	}{
		{[4]vec3{{-hx, -hy, hz}, {hx, -hy, hz}, {hx, hy, hz}, {-hx, hy, hz}}, vec3{0, 0, 1}, vec3{1, 0.3, 0.2}},        // +Z
		{[4]vec3{{hx, -hy, -hz}, {-hx, -hy, -hz}, {-hx, hy, -hz}, {hx, hy, -hz}}, vec3{0, 0, -1}, vec3{0.2, 0.8, 0.3}}, // -Z
		{[4]vec3{{-hx, hy, hz}, {hx, hy, hz}, {hx, hy, -hz}, {-hx, hy, -hz}}, vec3{0, 1, 0}, vec3{0.3, 0.5, 1}},        // +Y
		{[4]vec3{{-hx, -hy, -hz}, {hx, -hy, -hz}, {hx, -hy, hz}, {-hx, -hy, hz}}, vec3{0, -1, 0}, vec3{1, 0.9, 0.2}},   // -Y
		{[4]vec3{{hx, -hy, hz}, {hx, -hy, -hz}, {hx, hy, -hz}, {hx, hy, hz}}, vec3{1, 0, 0}, vec3{0.9, 0.2, 0.8}},      // +X
		{[4]vec3{{-hx, -hy, -hz}, {-hx, -hy, hz}, {-hx, hy, hz}, {-hx, hy, -hz}}, vec3{-1, 0, 0}, vec3{0.2, 0.9, 0.9}}, // -X
	}

	cornerUVs := [4]vec2{{0, 1}, {1, 1}, {1, 0}, {0, 0}}
	b := newBuilder(want, 6*6)
	tris := [][3]int{{0, 1, 2}, {0, 2, 3}}
	for _, face := range faces {
		for _, tri := range tris {
			for _, idx := range tri {
				b.emit(vertex{
					position: face.corners[idx],
					normal:   face.normal,
					uv:       cornerUVs[idx],
					color:    face.color,
				})
			}
		}
	}
	return b.build()
}

// buildPlane produces a quad in the XZ plane at y=0 with a +Y normal. UVs tile
// once over the quad. The winding is clockwise about +Y, which every other
// generator in GoSX uses for a ground-facing surface.
func buildPlane(width, height float64, want Attribute) *Mesh {
	hx := PositiveOr(width, 2) * 0.5
	hz := PositiveOr(height, 2) * 0.5
	corners := [4]vec3{{-hx, 0, -hz}, {hx, 0, -hz}, {hx, 0, hz}, {-hx, 0, hz}}
	cornerUVs := [4]vec2{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	normal := vec3{0, 1, 0}
	color := vec3{0.7, 0.72, 0.75}
	tris := [][3]int{{0, 2, 1}, {0, 3, 2}}
	b := newBuilder(want, 6)
	for _, tri := range tris {
		for _, idx := range tri {
			b.emit(vertex{position: corners[idx], normal: normal, uv: cornerUVs[idx], color: color})
		}
	}
	return b.build()
}

// buildPyramid produces a square pyramid centered on the origin. The base sits
// at -height/2 and the apex at +height/2, which matches the unit envelope of
// the box and the sphere.
func buildPyramid(width, height, depth float64, want Attribute) *Mesh {
	hx := PositiveOr(width, 2) * 0.5
	hy := PositiveOr(height, 2) * 0.5
	hz := PositiveOr(depth, 2) * 0.5
	base := [4]vec3{{-hx, -hy, -hz}, {hx, -hy, -hz}, {hx, -hy, hz}, {-hx, -hy, hz}}
	apex := vec3{0, hy, 0}
	b := newBuilder(want, 18)

	// Bottom face, wound for an outward -Y normal.
	baseColor := vec3{0.72, 0.68, 0.58}
	b.flatTri(base[0], base[1], base[2], baseColor, vec2{0, 0}, vec2{1, 0}, vec2{1, 1})
	b.flatTri(base[0], base[2], base[3], baseColor, vec2{0, 0}, vec2{1, 1}, vec2{0, 1})

	// Side faces. The order base[i], apex, base[next] gives outward normals
	// around the perimeter.
	sideColors := [4]vec3{
		{0.95, 0.48, 0.28},
		{0.35, 0.66, 0.94},
		{0.44, 0.83, 0.48},
		{0.86, 0.42, 0.85},
	}
	for i := 0; i < 4; i++ {
		next := (i + 1) % 4
		b.flatTri(base[i], apex, base[next], sideColors[i], vec2{0, 1}, vec2{0.5, 0}, vec2{1, 1})
	}
	return b.build()
}

// buildSphere produces a UV sphere with the given longitude and latitude counts.
// The normal at each vertex is the outward unit direction, so position and
// normal agree on a unit sphere. A soft gradient color gives the unlit fallback
// path a visible cue.
func buildSphere(radius float64, longitudes, latitudes int, want Attribute) *Mesh {
	if radius <= 0 {
		radius = 1
	}
	if longitudes < 3 {
		longitudes = 3
	}
	if latitudes < 2 {
		latitudes = 2
	}
	rows := make([][]vertex, latitudes+1)
	for lat := 0; lat <= latitudes; lat++ {
		theta := float64(lat) * math.Pi / float64(latitudes)
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		row := make([]vertex, longitudes+1)
		for lon := 0; lon <= longitudes; lon++ {
			phi := float64(lon) * 2 * math.Pi / float64(longitudes)
			sinP, cosP := math.Sin(phi), math.Cos(phi)
			t := float64(lat) / float64(latitudes)
			normal := vec3{X: cosP * sinT, Y: cosT, Z: sinP * sinT}
			row[lon] = vertex{
				position: scaleVec(normal, radius),
				normal:   normal,
				uv:       vec2{U: float64(lon) / float64(longitudes), V: t},
				color:    vec3{X: 0.9 - 0.6*t, Y: 0.3 + 0.5*t, Z: 0.4 + 0.5*(1-t)},
			}
		}
		rows[lat] = row
	}
	b := newBuilder(want, latitudes*longitudes*6)
	for lat := 0; lat < latitudes; lat++ {
		for lon := 0; lon < longitudes; lon++ {
			a := rows[lat][lon]
			c := rows[lat][lon+1]
			d := rows[lat+1][lon+1]
			e := rows[lat+1][lon]
			b.tri(a, c, d)
			b.tri(a, d, e)
		}
	}
	return b.build()
}

// buildCylinder produces a smooth frustum, cylinder or cone along the Y axis.
// The two radii sit at y=-height/2 and y=+height/2. Caps carry flat normals; the
// side carries analytic smooth normals. A zero top radius makes a cone.
func buildCylinder(radiusTop, radiusBottom, height float64, segments int, want Attribute) *Mesh {
	if segments < 3 {
		segments = 3
	}
	if height <= 0 {
		height = 2
	}
	if radiusTop < 0 {
		radiusTop = 0
	}
	if radiusBottom < 0 {
		radiusBottom = 0
	}
	if radiusTop == 0 && radiusBottom == 0 {
		radiusBottom = 1
	}
	halfH := height / 2
	slopeY := (radiusBottom - radiusTop) / height
	b := newBuilder(want, segments*12)
	sideColor := vec3{0.62, 0.75, 0.95}
	topColor := vec3{0.86, 0.88, 0.92}
	bottomColor := vec3{0.48, 0.52, 0.58}

	for i := 0; i < segments; i++ {
		u0 := float64(i) / float64(segments)
		u1 := float64(i+1) / float64(segments)
		th0 := float64(i) * 2 * math.Pi / float64(segments)
		th1 := float64(i+1) * 2 * math.Pi / float64(segments)
		c0, s0 := math.Cos(th0), math.Sin(th0)
		c1, s1 := math.Cos(th1), math.Sin(th1)
		n0 := normalize(vec3{c0, slopeY, s0})
		n1 := normalize(vec3{c1, slopeY, s1})

		b0 := vertex{position: vec3{radiusBottom * c0, -halfH, radiusBottom * s0}, normal: n0, uv: vec2{u0, 1}, color: sideColor}
		b1 := vertex{position: vec3{radiusBottom * c1, -halfH, radiusBottom * s1}, normal: n1, uv: vec2{u1, 1}, color: sideColor}
		t0 := vertex{position: vec3{radiusTop * c0, halfH, radiusTop * s0}, normal: n0, uv: vec2{u0, 0}, color: sideColor}
		t1 := vertex{position: vec3{radiusTop * c1, halfH, radiusTop * s1}, normal: n1, uv: vec2{u1, 0}, color: sideColor}

		if radiusBottom > 0 && radiusTop > 0 {
			b.tri(b0, t1, b1)
			b.tri(b0, t0, t1)
		} else if radiusTop == 0 {
			b.tri(b0, t1, b1)
		} else {
			b.tri(b0, t0, t1)
		}

		if radiusBottom > 0 {
			down := vec3{0, -1, 0}
			center := vertex{position: vec3{0, -halfH, 0}, normal: down, uv: vec2{0.5, 0.5}, color: bottomColor}
			p0 := vertex{position: vec3{radiusBottom * c0, -halfH, radiusBottom * s0}, normal: down, uv: radialUV(c0, s0), color: bottomColor}
			p1 := vertex{position: vec3{radiusBottom * c1, -halfH, radiusBottom * s1}, normal: down, uv: radialUV(c1, s1), color: bottomColor}
			b.tri(center, p0, p1)
		}
		if radiusTop > 0 {
			up := vec3{0, 1, 0}
			center := vertex{position: vec3{0, halfH, 0}, normal: up, uv: vec2{0.5, 0.5}, color: topColor}
			p0 := vertex{position: vec3{radiusTop * c0, halfH, radiusTop * s0}, normal: up, uv: radialUV(c0, s0), color: topColor}
			p1 := vertex{position: vec3{radiusTop * c1, halfH, radiusTop * s1}, normal: up, uv: radialUV(c1, s1), color: topColor}
			b.tri(center, p1, p0)
		}
	}
	return b.build()
}

// buildTorus produces a smooth torus centered on the origin around the Y axis.
func buildTorus(majorRadius, tubeRadius float64, radialSegments, tubularSegments int, want Attribute) *Mesh {
	if radialSegments < 3 {
		radialSegments = 3
	}
	if tubularSegments < 3 {
		tubularSegments = 3
	}
	if majorRadius <= 0 {
		majorRadius = 0.70
	}
	if tubeRadius <= 0 {
		tubeRadius = 0.30
	}

	vertexAt := func(i, j int) vertex {
		u := float64(i) * 2 * math.Pi / float64(radialSegments)
		v := float64(j) * 2 * math.Pi / float64(tubularSegments)
		cu, su := math.Cos(u), math.Sin(u)
		cv, sv := math.Cos(v), math.Sin(v)
		radius := majorRadius + tubeRadius*cv
		t := float64(j) / float64(tubularSegments)
		return vertex{
			position: vec3{X: radius * cu, Y: tubeRadius * sv, Z: radius * su},
			normal:   normalize(vec3{X: cv * cu, Y: sv, Z: cv * su}),
			uv:       vec2{U: float64(i) / float64(radialSegments), V: t},
			color:    vec3{X: 0.45 + 0.35*t, Y: 0.78 - 0.30*t, Z: 0.92},
		}
	}

	b := newBuilder(want, radialSegments*tubularSegments*6)
	for i := 0; i < radialSegments; i++ {
		for j := 0; j < tubularSegments; j++ {
			a := vertexAt(i, j)
			c := vertexAt(i, j+1)
			d := vertexAt(i+1, j)
			e := vertexAt(i+1, j+1)
			b.tri(a, c, d)
			b.tri(d, c, e)
		}
	}
	return b.build()
}

// buildTorusKnot tessellates a (p=2, q=3) trefoil knot exactly as
// torusKnotTriangleMesh does in the browser runtime. It sweeps a circular cross
// section along the knot curve on rotation-minimizing frames, then closes the
// tube with a linear twist correction at the seam.
//
// The vertex grid keeps the wrap row and the wrap column separate, as the
// runtime does, so the Go triangles match the drawn triangles to the last bit.
// The mesh is indexed, because the picker walks a hierarchy over shared
// vertices and the renderer expands the indices at upload time.
func buildTorusKnot(radius, tube float64, radialSegments, tubularSegments int, want Attribute) *Mesh {
	const (
		windings = 2.0
		lobes    = 3.0
	)
	if radialSegments < 3 {
		radialSegments = 3
	}
	if tubularSegments < 3 {
		tubularSegments = 3
	}
	if radius <= 0 {
		radius = 0.17
	}
	if tube <= 0 {
		tube = 0.045
	}
	radial, tubular := radialSegments, tubularSegments

	curveAt := func(theta float64) vec3 {
		sweep := radius * (2.0 + math.Cos(lobes*theta)) * 0.5
		return vec3{
			X: sweep * math.Cos(windings*theta),
			Y: sweep * math.Sin(windings*theta),
			Z: radius * math.Sin(lobes*theta) * 0.5,
		}
	}
	tangentAt := func(theta float64) vec3 {
		const step = 0.0001
		return normalize(subVec(curveAt(theta+step), curveAt(theta-step)))
	}

	centers := make([]vec3, tubular+1)
	tangents := make([]vec3, tubular+1)
	normals := make([]vec3, tubular+1)
	binormals := make([]vec3, tubular+1)

	centers[0] = curveAt(0)
	tangents[0] = tangentAt(0)
	normals[0] = normalize(leastParallelNormal(tangents[0]))
	binormals[0] = crossVec(tangents[0], normals[0])
	for i := 1; i <= tubular; i++ {
		theta := 2 * math.Pi * float64(i) / float64(tubular)
		tangent := tangentAt(theta)
		// Parallel transport: drop the part of the last normal that runs along the
		// new tangent. This stops the cross-section from spinning, which a Frenet
		// frame would do at every inflection.
		previous := normals[i-1]
		along := dotVec(previous, tangent)
		normal := normalize(subVec(previous, scaleVec(tangent, along)))
		centers[i] = curveAt(theta)
		tangents[i] = tangent
		normals[i] = normal
		binormals[i] = crossVec(tangent, normal)
	}
	// Seam correction: measure the angle between the last frame and the first,
	// then spread it over the whole sweep so the tube closes without a visible
	// twist.
	last, first := normals[tubular], normals[0]
	turn := math.Atan2(dotVec(crossVec(last, first), tangents[tubular]), dotVec(last, first))
	for i := 1; i <= tubular; i++ {
		angle := turn * float64(i) / float64(tubular)
		cos, sin := math.Cos(angle), math.Sin(angle)
		normal, binormal := normals[i], binormals[i]
		normals[i] = addVec(scaleVec(normal, cos), scaleVec(binormal, sin))
		binormals[i] = subVec(scaleVec(binormal, cos), scaleVec(normal, sin))
	}

	stride := radial + 1
	b := newBuilder(want, (tubular+1)*stride)
	for i := 0; i <= tubular; i++ {
		t := float64(i) / float64(tubular)
		for j := 0; j < stride; j++ {
			phi := 2 * math.Pi * float64(j) / float64(radial)
			cos, sin := math.Cos(phi), math.Sin(phi)
			out := addVec(scaleVec(normals[i], cos), scaleVec(binormals[i], sin))
			b.emit(vertex{
				position: addVec(centers[i], scaleVec(out, tube)),
				normal:   out,
				uv:       vec2{U: t, V: float64(j) / float64(radial)},
				color:    vec3{X: 0.45 + 0.35*t, Y: 0.78 - 0.30*t, Z: 0.92},
			})
		}
	}
	// Wind each quad counter-clockwise as seen from outside the tube.
	//
	// torusKnotTriangleMesh in the browser runtime winds these quads the other
	// way, against the outward normals it stores on the same vertices. Nothing
	// caught it: the browser main pass calls gl.disable(gl.CULL_FACE) and the
	// WebGPU path sets cullMode "none", the ray tester accepts both faces, and
	// the native renderer skipped the knot entirely. The native renderer culls
	// back faces with FrontFaceCCW, so it needs the correct winding to draw the
	// near wall of the tube instead of the far one.
	//
	// Only the order inside each triangle changes. The triangle count, the
	// triangle order and every vertex stay the same, so a pick reports the same
	// triangle as before.
	b.mesh.Indices = make([]int, 0, tubular*radial*6)
	for i := 0; i < tubular; i++ {
		for j := 0; j < radial; j++ {
			near := i*stride + j
			far := (i+1)*stride + j
			b.index(near, far+1, far)
			b.index(near, near+1, far+1)
		}
	}
	return b.build()
}

// leastParallelNormal returns a vector across the tangent, taken from the axis
// that lines up with it least. That keeps the first frame well conditioned.
func leastParallelNormal(tangent vec3) vec3 {
	x, y, z := math.Abs(tangent.X), math.Abs(tangent.Y), math.Abs(tangent.Z)
	if x <= y && x <= z {
		return vec3{Y: -tangent.Z, Z: tangent.Y}
	}
	if y <= z {
		return vec3{X: -tangent.Z, Z: tangent.X}
	}
	return vec3{X: -tangent.Y, Y: tangent.X}
}
