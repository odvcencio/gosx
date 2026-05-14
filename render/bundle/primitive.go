package bundle

import (
	"math"
	"strconv"
	"strings"
)

// primitiveGeometry is the CPU-side geometry for a named primitive Kind.
// positions, colors, and normals are 3 floats per vertex; uvs are 2. UVs
// follow standard 0..1 face conventions: cube maps each face unit-square,
// plane matches the extent, sphere uses longitude/latitude, and generated
// curved primitives use wrapped cylindrical/parametric coordinates.
//
// The renderer intentionally keeps primitives non-indexed. That matches the
// current WebGPU vertex layout, allows flat and smooth normals to coexist
// without an index-split pass, and keeps every native primitive upload as four
// tightly-packed vertex buffers: positions, colors, normals, and uvs.
type primitiveGeometry struct {
	positions   []float32
	colors      []float32
	normals     []float32
	uvs         []float32
	vertexCount int
}

type primitiveParams struct {
	Kind            string
	Size            float64
	Width           float64
	Height          float64
	Depth           float64
	Radius          float64
	RadiusTop       float64
	RadiusBottom    float64
	Tube            float64
	Segments        int
	RadialSegments  int
	TubularSegments int
}

// primitiveForKind returns native WebGPU geometry for every Scene3D built-in
// mesh primitive kind. Unknown kinds return nil and the caller skips the draw.
//
// Kinds are intentionally named to match what scene/scene_ir.go and the
// browser bridge emit for RenderInstancedMesh.Kind. Keep aliases here broad:
// the public scene package uses Go type names such as BoxGeometry while the
// older JS bridge used lowercase compatibility names such as "box".
func primitiveForKind(kind string) *primitiveGeometry {
	return primitiveForParams(primitiveParams{Kind: kind})
}

func primitiveForParams(params primitiveParams) *primitiveGeometry {
	params = normalizePrimitiveParams(params)
	switch params.Kind {
	case "cube":
		return boxGeometry(params.Size, params.Size, params.Size)
	case "box":
		return boxGeometry(params.Width, params.Height, params.Depth)
	case "plane":
		return planeGeometry(params.Width, params.Height)
	case "pyramid":
		return pyramidGeometry(params.Width, params.Height, params.Depth)
	case "sphere":
		return sphereGeometry(params.Radius, params.Segments, max(2, params.Segments/2))
	case "cylinder":
		return cylinderGeometry(params.RadiusTop, params.RadiusBottom, params.Height, params.Segments)
	case "cone":
		return cylinderGeometry(0, params.RadiusBottom, params.Height, params.Segments)
	case "torus":
		return torusGeometry(params.Radius, params.Tube, params.RadialSegments, params.TubularSegments)
	}
	return nil
}

func normalizePrimitiveParams(params primitiveParams) primitiveParams {
	params.Kind = normalizePrimitiveKind(params.Kind)
	switch params.Kind {
	case "cube":
		params.Size = positiveOr(params.Size, 2)
	case "box":
		params.Width = positiveOr(params.Width, 2)
		params.Height = positiveOr(params.Height, 2)
		params.Depth = positiveOr(params.Depth, 2)
	case "plane":
		params.Width = positiveOr(params.Width, 2)
		params.Height = positiveOr(firstPositive(params.Height, params.Depth), 2)
	case "pyramid":
		params.Width = positiveOr(params.Width, 2)
		params.Height = positiveOr(params.Height, 2)
		params.Depth = positiveOr(params.Depth, 2)
	case "sphere":
		params.Radius = positiveOr(params.Radius, 1)
		params.Segments = clampInt(params.Segments, 32, 3, 256)
	case "cylinder":
		params.RadiusTop = positiveOr(params.RadiusTop, 1)
		params.RadiusBottom = positiveOr(params.RadiusBottom, 1)
		params.Height = positiveOr(params.Height, 2)
		params.Segments = clampInt(params.Segments, 32, 3, 256)
	case "cone":
		params.RadiusBottom = positiveOr(firstPositive(params.RadiusBottom, params.Radius), 1)
		params.Height = positiveOr(params.Height, 2)
		params.Segments = clampInt(params.Segments, 32, 3, 256)
	case "torus":
		params.Radius = positiveOr(params.Radius, 0.70)
		params.Tube = positiveOr(params.Tube, 0.30)
		params.RadialSegments = clampInt(params.RadialSegments, 32, 3, 256)
		params.TubularSegments = clampInt(params.TubularSegments, 16, 3, 128)
	}
	return params
}

func normalizePrimitiveKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "cube", "cubegeometry":
		return "cube"
	case "box", "boxgeometry":
		return "box"
	case "plane", "planegeometry", "quad", "quadgeometry":
		return "plane"
	case "pyramid", "pyramidgeometry":
		return "pyramid"
	case "sphere", "spheregeometry", "uvsphere", "uvspheregeometry":
		return "sphere"
	case "cylinder", "cylindergeometry":
		return "cylinder"
	case "cone", "conegeometry":
		return "cone"
	case "torus", "torusgeometry":
		return "torus"
	}
	return ""
}

func primitiveCacheKey(params primitiveParams) string {
	params = normalizePrimitiveParams(params)
	if params.Kind == "" {
		return ""
	}
	parts := []string{params.Kind}
	appendFloat := func(name string, value float64) {
		if value > 0 {
			parts = append(parts, name+"="+strconv.FormatFloat(value, 'g', -1, 64))
		}
	}
	appendInt := func(name string, value int) {
		if value > 0 {
			parts = append(parts, name+"="+strconv.Itoa(value))
		}
	}
	switch params.Kind {
	case "cube":
		appendFloat("size", params.Size)
	case "box":
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
		appendFloat("d", params.Depth)
	case "plane":
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
	case "pyramid":
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
		appendFloat("d", params.Depth)
	case "sphere":
		appendFloat("r", params.Radius)
		appendInt("seg", params.Segments)
	case "cylinder":
		appendFloat("rt", params.RadiusTop)
		appendFloat("rb", params.RadiusBottom)
		appendFloat("h", params.Height)
		appendInt("seg", params.Segments)
	case "cone":
		appendFloat("rb", params.RadiusBottom)
		appendFloat("h", params.Height)
		appendInt("seg", params.Segments)
	case "torus":
		appendFloat("r", params.Radius)
		appendFloat("tube", params.Tube)
		appendInt("rad", params.RadialSegments)
		appendInt("tubeSeg", params.TubularSegments)
	}
	return strings.Join(parts, "|")
}

func primitiveCullRadius(params primitiveParams) float32 {
	params = normalizePrimitiveParams(params)
	switch params.Kind {
	case "cube":
		return float32(math.Sqrt(3*params.Size*params.Size) * 0.5 * 1.05)
	case "box", "pyramid":
		return float32(math.Sqrt(params.Width*params.Width+params.Height*params.Height+params.Depth*params.Depth) * 0.5 * 1.05)
	case "plane":
		return float32(math.Sqrt(params.Width*params.Width+params.Height*params.Height) * 0.5 * 1.05)
	case "sphere":
		return float32(params.Radius * 1.05)
	case "cylinder", "cone":
		r := math.Max(params.RadiusTop, params.RadiusBottom)
		return float32(math.Sqrt(r*r+(params.Height*0.5)*(params.Height*0.5)) * 1.05)
	case "torus":
		return float32((params.Radius + params.Tube) * 1.05)
	}
	return 2
}

type primitiveBuilder struct {
	positions []float32
	colors    []float32
	normals   []float32
	uvs       []float32
}

type primitiveVertex struct {
	position [3]float32
	normal   [3]float32
	color    [3]float32
	uv       [2]float32
}

func newPrimitiveBuilder(vertexCapacity int) *primitiveBuilder {
	if vertexCapacity < 0 {
		vertexCapacity = 0
	}
	return &primitiveBuilder{
		positions: make([]float32, 0, vertexCapacity*3),
		colors:    make([]float32, 0, vertexCapacity*3),
		normals:   make([]float32, 0, vertexCapacity*3),
		uvs:       make([]float32, 0, vertexCapacity*2),
	}
}

func (b *primitiveBuilder) emit(v primitiveVertex) {
	b.positions = append(b.positions, v.position[0], v.position[1], v.position[2])
	b.colors = append(b.colors, v.color[0], v.color[1], v.color[2])
	b.normals = append(b.normals, v.normal[0], v.normal[1], v.normal[2])
	b.uvs = append(b.uvs, v.uv[0], v.uv[1])
}

func (b *primitiveBuilder) tri(a, c, d primitiveVertex) {
	b.emit(a)
	b.emit(c)
	b.emit(d)
}

func (b *primitiveBuilder) flatTri(p0, p1, p2 [3]float32, color [3]float32, uv0, uv1, uv2 [2]float32) {
	n := triangleNormal(p0, p1, p2)
	b.tri(
		primitiveVertex{position: p0, normal: n, color: color, uv: uv0},
		primitiveVertex{position: p1, normal: n, color: color, uv: uv1},
		primitiveVertex{position: p2, normal: n, color: color, uv: uv2},
	)
}

func (b *primitiveBuilder) build() *primitiveGeometry {
	return &primitiveGeometry{
		positions:   b.positions,
		colors:      b.colors,
		normals:     b.normals,
		uvs:         b.uvs,
		vertexCount: len(b.positions) / 3,
	}
}

// cubeGeometry produces a unit cube centered on the origin with extent [-1,1]
// on each axis. Each face has a constant normal and a face-indexed color;
// vertices are duplicated per face so flat shading reads cleanly.
func cubeGeometry() *primitiveGeometry {
	return boxGeometry(2, 2, 2)
}

func boxGeometry(width, height, depth float64) *primitiveGeometry {
	hx := float32(positiveOr(width, 2) * 0.5)
	hy := float32(positiveOr(height, 2) * 0.5)
	hz := float32(positiveOr(depth, 2) * 0.5)
	faces := []struct {
		corners [4][3]float32
		normal  [3]float32
		color   [3]float32
	}{
		{[4][3]float32{{-hx, -hy, hz}, {hx, -hy, hz}, {hx, hy, hz}, {-hx, hy, hz}}, [3]float32{0, 0, 1}, [3]float32{1, 0.3, 0.2}},        // +Z
		{[4][3]float32{{hx, -hy, -hz}, {-hx, -hy, -hz}, {-hx, hy, -hz}, {hx, hy, -hz}}, [3]float32{0, 0, -1}, [3]float32{0.2, 0.8, 0.3}}, // -Z
		{[4][3]float32{{-hx, hy, hz}, {hx, hy, hz}, {hx, hy, -hz}, {-hx, hy, -hz}}, [3]float32{0, 1, 0}, [3]float32{0.3, 0.5, 1}},        // +Y
		{[4][3]float32{{-hx, -hy, -hz}, {hx, -hy, -hz}, {hx, -hy, hz}, {-hx, -hy, hz}}, [3]float32{0, -1, 0}, [3]float32{1, 0.9, 0.2}},   // -Y
		{[4][3]float32{{hx, -hy, hz}, {hx, -hy, -hz}, {hx, hy, -hz}, {hx, hy, hz}}, [3]float32{1, 0, 0}, [3]float32{0.9, 0.2, 0.8}},      // +X
		{[4][3]float32{{-hx, -hy, -hz}, {-hx, -hy, hz}, {-hx, hy, hz}, {-hx, hy, -hz}}, [3]float32{-1, 0, 0}, [3]float32{0.2, 0.9, 0.9}}, // -X
	}

	cornerUVs := [4][2]float32{{0, 1}, {1, 1}, {1, 0}, {0, 0}}
	builder := newPrimitiveBuilder(6 * 6)
	tris := [][3]int{{0, 1, 2}, {0, 2, 3}}
	for _, face := range faces {
		for _, tri := range tris {
			for _, idx := range tri {
				builder.emit(primitiveVertex{
					position: face.corners[idx],
					normal:   face.normal,
					color:    face.color,
					uv:       cornerUVs[idx],
				})
			}
		}
	}
	return builder.build()
}

// planeGeometry produces a unit XZ-plane at y=0 with extent [-1,1] on x and z,
// normal +Y. Two triangles, shaded neutral so lighting + shadow read clearly.
// UVs tile once over the quad.
func planeGeometry(width, height float64) *primitiveGeometry {
	hx := float32(positiveOr(width, 2) * 0.5)
	hz := float32(positiveOr(height, 2) * 0.5)
	corners := [4][3]float32{{-hx, 0, -hz}, {hx, 0, -hz}, {hx, 0, hz}, {-hx, 0, hz}}
	cornerUVs := [4][2]float32{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	normal := [3]float32{0, 1, 0}
	color := [3]float32{0.7, 0.72, 0.75}
	tris := [][3]int{{0, 2, 1}, {0, 3, 2}}
	builder := newPrimitiveBuilder(6)
	for _, tri := range tris {
		for _, idx := range tri {
			builder.emit(primitiveVertex{
				position: corners[idx],
				normal:   normal,
				color:    color,
				uv:       cornerUVs[idx],
			})
		}
	}
	return builder.build()
}

// pyramidGeometry produces a square pyramid centered on the origin. The base
// sits at y=-1 and the apex at y=1, matching the cube/sphere unit envelope.
func pyramidGeometry(width, height, depth float64) *primitiveGeometry {
	hx := float32(positiveOr(width, 2) * 0.5)
	hy := float32(positiveOr(height, 2) * 0.5)
	hz := float32(positiveOr(depth, 2) * 0.5)
	base := [4][3]float32{{-hx, -hy, -hz}, {hx, -hy, -hz}, {hx, -hy, hz}, {-hx, -hy, hz}}
	apex := [3]float32{0, hy, 0}
	builder := newPrimitiveBuilder(18)

	// Bottom face, wound for outward -Y normal.
	baseColor := [3]float32{0.72, 0.68, 0.58}
	builder.flatTri(base[0], base[1], base[2], baseColor, [2]float32{0, 0}, [2]float32{1, 0}, [2]float32{1, 1})
	builder.flatTri(base[0], base[2], base[3], baseColor, [2]float32{0, 0}, [2]float32{1, 1}, [2]float32{0, 1})

	// Side faces. The order is base[i], apex, base[next] to produce outward
	// normals around the perimeter.
	sideColors := [4][3]float32{
		{0.95, 0.48, 0.28},
		{0.35, 0.66, 0.94},
		{0.44, 0.83, 0.48},
		{0.86, 0.42, 0.85},
	}
	for i := 0; i < 4; i++ {
		next := (i + 1) % 4
		builder.flatTri(base[i], apex, base[next], sideColors[i], [2]float32{0, 1}, [2]float32{0.5, 0}, [2]float32{1, 1})
	}
	return builder.build()
}

// sphereGeometry produces a UV-sphere with the given latitude/longitude
// subdivisions. Normals are the outward unit vector at each vertex — on a
// unit sphere, position and normal are the same. A soft gradient color gives
// the unlit fallback path a visible cue.
func sphereGeometry(radius float64, longitudes, latitudes int) *primitiveGeometry {
	if radius <= 0 {
		radius = 1
	}
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
				x: x * float32(radius), y: y * float32(radius), z: z * float32(radius),
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
	builder := newPrimitiveBuilder(latitudes * longitudes * 6)
	for lat := 0; lat < latitudes; lat++ {
		for lon := 0; lon < longitudes; lon++ {
			a := verts[lat][lon]
			c := verts[lat][lon+1]
			d := verts[lat+1][lon+1]
			e := verts[lat+1][lon]
			for _, vx := range []vert{a, c, d, a, d, e} {
				builder.emit(primitiveVertex{
					position: [3]float32{vx.x, vx.y, vx.z},
					normal:   [3]float32{vx.nx, vx.ny, vx.nz},
					color:    [3]float32{vx.r, vx.g, vx.b},
					uv:       [2]float32{vx.u, vx.v},
				})
			}
		}
	}
	return builder.build()
}

// cylinderGeometry produces a smooth frustum/cylinder/cone along the Y axis.
// RadiusTop/RadiusBottom are centered around y=±height/2. Caps are emitted
// with flat normals; sides use analytic smooth normals.
func cylinderGeometry(radiusTop, radiusBottom, height float64, segments int) *primitiveGeometry {
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
	rt := float32(radiusTop)
	rb := float32(radiusBottom)
	halfH := float32(height / 2)
	slopeY := float32((radiusBottom - radiusTop) / height)
	builder := newPrimitiveBuilder(segments * 12)
	sideColor := [3]float32{0.62, 0.75, 0.95}
	topColor := [3]float32{0.86, 0.88, 0.92}
	bottomColor := [3]float32{0.48, 0.52, 0.58}

	for i := 0; i < segments; i++ {
		u0 := float32(i) / float32(segments)
		u1 := float32(i+1) / float32(segments)
		th0 := float64(i) * 2 * math.Pi / float64(segments)
		th1 := float64(i+1) * 2 * math.Pi / float64(segments)
		c0, s0 := float32(math.Cos(th0)), float32(math.Sin(th0))
		c1, s1 := float32(math.Cos(th1)), float32(math.Sin(th1))
		n0 := normalize3(c0, slopeY, s0)
		n1 := normalize3(c1, slopeY, s1)

		b0 := primitiveVertex{position: [3]float32{rb * c0, -halfH, rb * s0}, normal: n0, color: sideColor, uv: [2]float32{u0, 1}}
		b1 := primitiveVertex{position: [3]float32{rb * c1, -halfH, rb * s1}, normal: n1, color: sideColor, uv: [2]float32{u1, 1}}
		t0 := primitiveVertex{position: [3]float32{rt * c0, halfH, rt * s0}, normal: n0, color: sideColor, uv: [2]float32{u0, 0}}
		t1 := primitiveVertex{position: [3]float32{rt * c1, halfH, rt * s1}, normal: n1, color: sideColor, uv: [2]float32{u1, 0}}

		if radiusBottom > 0 && radiusTop > 0 {
			builder.tri(b0, t1, b1)
			builder.tri(b0, t0, t1)
		} else if radiusTop == 0 {
			builder.tri(b0, t1, b1)
		} else {
			builder.tri(b0, t0, t1)
		}

		if radiusBottom > 0 {
			center := [3]float32{0, -halfH, 0}
			p0 := [3]float32{rb * c0, -halfH, rb * s0}
			p1 := [3]float32{rb * c1, -halfH, rb * s1}
			builder.tri(
				primitiveVertex{position: center, normal: [3]float32{0, -1, 0}, color: bottomColor, uv: [2]float32{0.5, 0.5}},
				primitiveVertex{position: p0, normal: [3]float32{0, -1, 0}, color: bottomColor, uv: radialUV(c0, s0)},
				primitiveVertex{position: p1, normal: [3]float32{0, -1, 0}, color: bottomColor, uv: radialUV(c1, s1)},
			)
		}
		if radiusTop > 0 {
			center := [3]float32{0, halfH, 0}
			p0 := [3]float32{rt * c0, halfH, rt * s0}
			p1 := [3]float32{rt * c1, halfH, rt * s1}
			builder.tri(
				primitiveVertex{position: center, normal: [3]float32{0, 1, 0}, color: topColor, uv: [2]float32{0.5, 0.5}},
				primitiveVertex{position: p1, normal: [3]float32{0, 1, 0}, color: topColor, uv: radialUV(c1, s1)},
				primitiveVertex{position: p0, normal: [3]float32{0, 1, 0}, color: topColor, uv: radialUV(c0, s0)},
			)
		}
	}
	return builder.build()
}

// torusGeometry produces a smooth torus centered on the origin around the Y
// axis. The generated mesh stays within roughly [-1,1] for the default
// primitiveForKind parameters so transforms behave like the other primitives.
func torusGeometry(majorRadius, tubeRadius float64, radialSegments, tubularSegments int) *primitiveGeometry {
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

	type torusVertex struct {
		position [3]float32
		normal   [3]float32
		uv       [2]float32
		color    [3]float32
	}
	vertexAt := func(i, j int) torusVertex {
		u := float64(i) * 2 * math.Pi / float64(radialSegments)
		v := float64(j) * 2 * math.Pi / float64(tubularSegments)
		cu, su := math.Cos(u), math.Sin(u)
		cv, sv := math.Cos(v), math.Sin(v)
		radius := majorRadius + tubeRadius*cv
		n := normalize3(float32(cv*cu), float32(sv), float32(cv*su))
		t := float32(j) / float32(tubularSegments)
		return torusVertex{
			position: [3]float32{float32(radius * cu), float32(tubeRadius * sv), float32(radius * su)},
			normal:   n,
			uv:       [2]float32{float32(i) / float32(radialSegments), t},
			color:    [3]float32{0.45 + 0.35*t, 0.78 - 0.30*t, 0.92},
		}
	}

	builder := newPrimitiveBuilder(radialSegments * tubularSegments * 6)
	for i := 0; i < radialSegments; i++ {
		for j := 0; j < tubularSegments; j++ {
			a := vertexAt(i, j)
			c := vertexAt(i, j+1)
			d := vertexAt(i+1, j)
			e := vertexAt(i+1, j+1)
			builder.tri(
				primitiveVertex{position: a.position, normal: a.normal, color: a.color, uv: a.uv},
				primitiveVertex{position: c.position, normal: c.normal, color: c.color, uv: c.uv},
				primitiveVertex{position: d.position, normal: d.normal, color: d.color, uv: d.uv},
			)
			builder.tri(
				primitiveVertex{position: d.position, normal: d.normal, color: d.color, uv: d.uv},
				primitiveVertex{position: c.position, normal: c.normal, color: c.color, uv: c.uv},
				primitiveVertex{position: e.position, normal: e.normal, color: e.color, uv: e.uv},
			)
		}
	}
	return builder.build()
}

func radialUV(cosTheta, sinTheta float32) [2]float32 {
	return [2]float32{0.5 + cosTheta*0.5, 0.5 + sinTheta*0.5}
}

func triangleNormal(a, b, c [3]float32) [3]float32 {
	ab := [3]float32{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	ac := [3]float32{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	return normalize3(
		ab[1]*ac[2]-ab[2]*ac[1],
		ab[2]*ac[0]-ab[0]*ac[2],
		ab[0]*ac[1]-ab[1]*ac[0],
	)
}

func normalize3(x, y, z float32) [3]float32 {
	length := math.Sqrt(float64(x*x + y*y + z*z))
	if length <= 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return [3]float32{0, 1, 0}
	}
	inv := float32(1 / length)
	return [3]float32{x * inv, y * inv, z * inv}
}

func positiveOr(value, fallback float64) float64 {
	if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	return fallback
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value
		}
	}
	return 0
}

func clampInt(value, fallback, minValue, maxValue int) int {
	if value <= 0 {
		value = fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
