# Scene3D CapsuleGeometry vertical slice

Implement one bounded, production-ready CapsuleGeometry slice in this GoSX checkout.

The patch must be TypeScript-first: edit the authored TypeScript runtime, never handwritten generated JavaScript. Keep the change entirely inside Scene3D. Do not touch Gridiron, gsxmail, CSS, GSX examples, dependency manifests, generated bundles, source maps, gzip/brotli artifacts, or unrelated framework surfaces.

## Required contract

- Add exported typed Go `scene.CapsuleGeometry` with exactly:
  - `Radius float64`
  - `Height float64`
  - `Segments int`
  - `RadialSegments int`
- Capsule orientation is the Y axis. `Height` is the straight cylindrical body length. Total Y extent is `Height + 2*Radius`.
- Defaults: radius 1, body height 1, hemisphere segments 4, radial segments 8.
- Clamp hemisphere `Segments` to 1..64 and `RadialSegments` to 3..256.
- Canonical kind is `capsule`; accept authored alias `capsuleGeometry` through the existing normalizers.
- Reuse existing `Radius`, `Height`, `Segments`, and `RadialSegments` transport fields. Do not add transport fields.
- Add one shared Go generator in `scene/geom`. It must produce a closed, outward-facing, non-indexed triangle mesh with positions and optional normals/UVs/colors according to the existing builder contract.
- The Go mesh must contain a cylindrical middle joined continuously to upper and lower hemispheres, with no flat end caps and no open seams. Normals are analytic outward capsule normals. UVs may use longitude around XZ and monotonic latitude/body V.
- Wire the generator through kind normalization, parameter normalization, cache key, bounding radius, build dispatch, vertex counts, the typed scene lowering path, the legacy adapter, `Tessellate`, native/headless primitive flow, and schema vocabulary.
- Browser production support belongs in `client/js/bootstrap-src/16c-scene-shared-pbr.ts`; route ordinary objects through the existing shared in-IIFE instanced generator in `12-scene-geometry.ts`. Add no handwritten production `.js`.
- Browser defaults and clamping must match Go exactly. Return the existing typed-array mesh shape.
- Add analytic picking for a finite Y-axis capsule. Test the cylindrical body plus the two spherical caps, return the nearest non-negative hit, and compute the correct outward normal. Set `Method: "analytic-capsule"` and kind `capsule`.
- Broadphase bound must never understate the geometry. Use `hypot(radius, height/2+radius)` for this origin-centered capsule.
- Add focused Go tests covering defaults/clamps, canonical alias, cache separation, vertex/triangle count consistency, finite mesh data, exact Y bounds, radial bound, unit normals, typed lowering/legacy transport, native/headless primitive generation, direct ray hit/miss and normal, and accelerated/direct picking parity.
- Extend the existing JavaScript harness tests to cover ordinary and instanced capsule routing, typed arrays, finite data, exact Y bound, radial bound, unit normals, default/clamped counts, and outward triangle winding.
- Keep the patch bounded. Do not redesign the geometry framework or introduce a second generator where an existing shared path applies.

## Output contract

Return only a unified git patch that applies to the exact source snapshot below.

- The first bytes must be `diff --git`.
- No prose, Markdown fence, preamble, summary, TODO, placeholder, ellipsis, or omitted hunk.
- Include every added/modified source and test file in the patch.
- Do not include this task packet or any `.buckley` file.
- Do not include generated `.js`, map, gzip, or brotli artifacts.
- Do not run, describe, or encode commits/pushes in the patch.
- Preserve exact surrounding source context shown below.

## Exact current source excerpts

### scene/scene.go

```go
	Values        []float64 // interleaved keyframe values (3 per time for translation/scale, 4 for rotation quaternions)
}

// Geometry describes one supported legacy primitive.
type Geometry interface {
	sceneGeometry()
	legacyGeometry() (string, map[string]any)
}

// AxesHelper renders colored XYZ axes as line geometry.
type AxesHelper struct {
	ID       string
	Size     float64

type CubeGeometry struct {
	Size float64
}

type BoxGeometry struct {
	Width  float64
	Height float64
	Depth  float64
}

type PlaneGeometry struct {
	Width  float64
	Height float64
}

type PyramidGeometry struct {
	Width  float64
	Height float64
	Depth  float64
}

type SphereGeometry struct {
	Radius   float64
	Segments int
}

type LinesGeometry struct {
	Points   []Vector3
	Segments [][2]int
	// Width is the stroke width in CSS pixels for line segments. Zero value
	// means the renderer's default (1.8px on the Canvas 2D fallback; hairline
	// on the legacy WebGL path until the thick-line shader ships). Non-zero
	// values flow through to renderers that honor per-line widths.
	Width float64
}

type CylinderGeometry struct {
	RadiusTop    float64
	RadiusBottom float64
	Height       float64
	Segments     int
}

type TorusGeometry struct {
	Radius          float64
	Tube            float64
	RadialSegments  int
	TubularSegments int
}

// TorusKnotGeometry is a (p=2, q=3) trefoil torus knot rendered as a swept
// tube along the parametric center curve used by the water shader SDF.
// TubularSegments controls path smoothness; RadialSegments controls cross-section.
type TorusKnotGeometry struct {
	Radius          float64
	Tube            float64
	RadialSegments  int
	TubularSegments int
func (BoxGeometry) sceneGeometry()       {}
func (PlaneGeometry) sceneGeometry()     {}
func (PyramidGeometry) sceneGeometry()   {}
func (SphereGeometry) sceneGeometry()    {}
func (LinesGeometry) sceneGeometry()     {}
func (CylinderGeometry) sceneGeometry()  {}
func (TorusGeometry) sceneGeometry()     {}
func (TorusKnotGeometry) sceneGeometry() {}

func (FlatMaterial) sceneMaterial()       {}
func (GhostMaterial) sceneMaterial()      {}
func (l *graphLowerer) lowerInstancedMesh(im InstancedMesh, parent worldTransform) {
	world := combineTransforms(parent, localTransform(Vector3{}, Euler{}))
	id := strings.TrimSpace(im.ID)
	if id == "" {
		l.nextInstancedID += 1
		id = "scene-instanced-" + intString(l.nextInstancedID)
	}
	kind, geometryProps := legacyGeometry(im.Geometry)
	materialProps := legacyMaterial(im.Material)

	record := InstancedMeshIR{
		ID:              id,
		Count:           im.Count,
		Kind:            kind,
		Pickable:        im.Pickable,
		CastShadow:      im.CastShadow,
		ReceiveShadow:   im.ReceiveShadow,
		Transition:      lowerTransition(im.Transition),
		InState:         im.InState.legacyProps(),
		OutState:        im.OutState.legacyProps(),
		Live:            normalizeLive(im.Live),
		Colors:          append([]string(nil), im.Colors...),
		Attributes:      cloneFloat64Slices(im.Attributes),
		CullKernelWGSL:  im.CullKernelWGSL,
		CullKernelEntry: im.CullKernelEntry,
		CullRadius:      im.CullRadius,
		CullBackend:     im.CullBackend,
	}
	// Apply geometry dimensions.
	if geometryProps != nil {
		record.Size = mapFloat64(geometryProps["size"])
		record.Width = mapFloat64(geometryProps["width"])
		record.Height = mapFloat64(geometryProps["height"])
		record.Depth = mapFloat64(geometryProps["depth"])
		record.Radius = mapFloat64(geometryProps["radius"])
		record.RadiusTop = mapFloat64(geometryProps["radiusTop"])
		record.RadiusBottom = mapFloat64(geometryProps["radiusBottom"])
		record.Tube = mapFloat64(geometryProps["tube"])
		record.Segments = mapInt(geometryProps["segments"])
		record.RadialSegments = mapInt(geometryProps["radialSegments"])
		record.TubularSegments = mapInt(geometryProps["tubularSegments"])
	}
	// Apply material kind.
//
// Adding a new concrete Geometry type? Add a case here with direct
// field assignments matching its legacyGeometry() implementation. If
// you forget, the fallback round-trips through the old map path.
func applyGeometryToObjectIR(record *ObjectIR, geometry Geometry) string {
	if geometry == nil {
		return "cube"
	}
	switch g := geometry.(type) {
	case CubeGeometry:
		if g.Size > 0 {
			record.Size = g.Size
		}
		return "cube"
	case BoxGeometry:
		record.Width = g.Width
		record.Height = g.Height
		record.Depth = g.Depth
		return "box"
	case PlaneGeometry:
		record.Width = g.Width
		record.Height = g.Height
		return "plane"
	case PyramidGeometry:
		record.Width = g.Width
		record.Height = g.Height
		record.Depth = g.Depth
		return "pyramid"
	case SphereGeometry:
		record.Radius = g.Radius
		if g.Segments > 0 {
			record.Segments = g.Segments
		}
		return "sphere"
	case LinesGeometry:
		record.Points = g.Points
		record.LineSegments = g.Segments
		if g.Width > 0 {
			record.LineWidth = g.Width
		}
		return "lines"
	case CylinderGeometry:
		record.RadiusTop = g.RadiusTop
		record.RadiusBottom = g.RadiusBottom
		record.Height = g.Height
		if g.Segments > 0 {
			record.Segments = g.Segments
		}
		return "cylinder"
	case TorusGeometry:
		record.Radius = g.Radius
		record.Tube = g.Tube
		if g.RadialSegments > 0 {
			record.RadialSegments = g.RadialSegments
		}
		if g.TubularSegments > 0 {
			record.TubularSegments = g.TubularSegments
		}
		return "torus"
	case TorusKnotGeometry:
		record.Radius = g.Radius
		record.Tube = g.Tube
		if g.RadialSegments > 0 {
			record.RadialSegments = g.RadialSegments
		}
		if g.TubularSegments > 0 {
			record.TubularSegments = g.TubularSegments
		}
		return "torusknot"
	case BufferGeometry:
		record.Vertices = bufferGeometryVertices(g)
		return "gltf-mesh"
	case *BufferGeometry:
		if g != nil {
			record.Vertices = bufferGeometryVertices(*g)
		}
		return "gltf-mesh"
	}
	// Fallback for any future geometry type that hasn't been type-switched
	// above yet — use the legacy map round-trip so correctness is
	// preserved even if perf isn't.
	kind, props := geometry.legacyGeometry()
	applyGeometryProps(record, props)
	return kind
}

func boxHelperGeometry(width, height, depth, lineWidth float64) LinesGeometry {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {

func (g LinesGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	if points := legacyLinePoints(g.Points); len(points) > 0 {
		out["points"] = points
	}
	if segments := legacyLineSegments(g.Segments); len(segments) > 0 {
		out["segments"] = segments
	}
	if g.Width > 0 {
		out["lineWidth"] = g.Width
	}
	if len(out) == 0 {
		return "lines", nil
	}
	return "lines", out
}

func (g CylinderGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "radiusTop", g.RadiusTop)
	setNumeric(out, "radiusBottom", g.RadiusBottom)
	setNumeric(out, "height", g.Height)
	if g.Segments > 0 {
		out["segments"] = g.Segments
	}
	if len(out) == 0 {
		return "cylinder", nil
	}
	return "cylinder", out
}

func (g TorusGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "radius", g.Radius)
	setNumeric(out, "tube", g.Tube)
	if g.RadialSegments > 0 {
		out["radialSegments"] = g.RadialSegments
	}
	if g.TubularSegments > 0 {
		out["tubularSegments"] = g.TubularSegments
	}
	if len(out) == 0 {
		return "torus", nil
	}
	return "torus", out
}

func (g TorusKnotGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "radius", g.Radius)
	setNumeric(out, "tube", g.Tube)
	if g.RadialSegments > 0 {
		out["radialSegments"] = g.RadialSegments
	}
	if g.TubularSegments > 0 {
		out["tubularSegments"] = g.TubularSegments
	}
	if len(out) == 0 {
		return "torusknot", nil
	}
	return "torusknot", out
}

func legacyMaterial(material Material) map[string]any {
	if material == nil {
		return nil
	}
```

### scene/geom/geom.go

```go
}

// Params names one parametric geometry. Kind selects the generator; the other
// fields carry the authored numbers. Normalize resolves the defaults and the
// limits, so a caller never has to.
type Params struct {
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

// Kind lists every generator this package answers for. Compare against these
// constants instead of writing the strings again.
const (
	KindCube      = "cube"
	KindBox       = "box"
	KindPlane     = "plane"
	KindPyramid   = "pyramid"
	KindSphere    = "sphere"
	KindCylinder  = "cylinder"
	KindCone      = "cone"
	KindTorus     = "torus"
	KindTorusKnot = "torusknot"
)

// NormalizeKind maps every authored spelling of a geometry name onto one
// canonical name. The public scene package uses Go type names such as
// BoxGeometry; the browser bridge uses lowercase names such as "box". An
// unknown name returns the empty string, and the caller must then treat the
// geometry as one this package cannot build.
func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "cube", "cubegeometry":
		return KindCube
	case "box", "boxgeometry":
		return KindBox
	case "plane", "planegeometry", "quad", "quadgeometry":
		return KindPlane
	case "pyramid", "pyramidgeometry":
		return KindPyramid
	case "sphere", "spheregeometry", "uvsphere", "uvspheregeometry":
		return KindSphere
	case "cylinder", "cylindergeometry":
		return KindCylinder
	case "cone", "conegeometry":
		return KindCone
	case "torus", "torusgeometry":
		return KindTorus
	case "torusknot", "torusknotgeometry", "torus-knot", "torusknotgeo":
		return KindTorusKnot
	}
	return ""
}

// Normalize resolves the kind name, the defaults and the segment limits. Call
// it before CacheKey, BoundingRadius or Build; each of those calls it again, so
// a second call is safe and changes nothing.
//
// The torus knot defaults match torusKnotTriangleMesh in the browser runtime.
// The other defaults match the renderer's own unit envelope.
func Normalize(params Params) Params {
	params.Kind = NormalizeKind(params.Kind)
	switch params.Kind {
	case KindCube:
		params.Size = PositiveOr(params.Size, 2)
	case KindBox:
		params.Width = PositiveOr(params.Width, 2)
		params.Height = PositiveOr(params.Height, 2)
		params.Depth = PositiveOr(params.Depth, 2)
	case KindPlane:
		params.Width = PositiveOr(params.Width, 2)
		params.Height = PositiveOr(FirstPositive(params.Height, params.Depth), 2)
	case KindPyramid:
		params.Width = PositiveOr(params.Width, 2)
		params.Height = PositiveOr(params.Height, 2)
		params.Depth = PositiveOr(params.Depth, 2)
	case KindSphere:
		params.Radius = PositiveOr(params.Radius, 1)
		params.Segments = ClampInt(params.Segments, 32, 3, 256)
	case KindCylinder:
		params.RadiusTop = PositiveOr(params.RadiusTop, 1)
		params.RadiusBottom = PositiveOr(params.RadiusBottom, 1)
		params.Height = PositiveOr(params.Height, 2)
		params.Segments = ClampInt(params.Segments, 32, 3, 256)
	case KindCone:
		params.RadiusBottom = PositiveOr(FirstPositive(params.RadiusBottom, params.Radius), 1)
		params.Height = PositiveOr(params.Height, 2)
		params.Segments = ClampInt(params.Segments, 32, 3, 256)
	case KindTorus:
		params.Radius = PositiveOr(params.Radius, 0.70)
		params.Tube = PositiveOr(params.Tube, 0.30)
		params.RadialSegments = ClampInt(params.RadialSegments, 32, 3, 256)
		params.TubularSegments = ClampInt(params.TubularSegments, 16, 3, 128)
	case KindTorusKnot:
		params.Radius = PositiveOr(params.Radius, 0.17)
		params.Tube = PositiveOr(params.Tube, 0.045)
		params.RadialSegments = ClampInt(params.RadialSegments, 16, 3, 64)
		params.TubularSegments = ClampInt(params.TubularSegments, 128, 8, 512)
	}
	return params
}

// CacheKey names one tessellation. Two parameter sets with the same key hold
// the same triangles, so a renderer or a picker can share one build between
// them. An unknown kind returns the empty string.
func CacheKey(params Params) string {
	params = Normalize(params)
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
	case KindCube:
		appendFloat("size", params.Size)
	case KindBox, KindPyramid:
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
		appendFloat("d", params.Depth)
	case KindPlane:
		appendFloat("w", params.Width)
		appendFloat("h", params.Height)
	case KindSphere:
		appendFloat("r", params.Radius)
		appendInt("seg", params.Segments)
	case KindCylinder:
		appendFloat("rt", params.RadiusTop)
		appendFloat("rb", params.RadiusBottom)
		appendFloat("h", params.Height)
		appendInt("seg", params.Segments)
	case KindCone:
		appendFloat("rb", params.RadiusBottom)
		appendFloat("h", params.Height)
		appendInt("seg", params.Segments)
	case KindTorus, KindTorusKnot:
		appendFloat("r", params.Radius)
		appendFloat("tube", params.Tube)
		appendInt("rad", params.RadialSegments)
		appendInt("tubeSeg", params.TubularSegments)
	}
	return strings.Join(parts, "|")
}

// BoundingRadius returns the radius of a sphere at the origin that holds the
// whole geometry. The value never understates the geometry, so a cull or a
// broad phase never drops a real hit. An unknown kind returns zero.
func BoundingRadius(params Params) float64 {
	params = Normalize(params)
	switch params.Kind {
	case KindCube:
		return math.Sqrt(3*params.Size*params.Size) * 0.5
	case KindBox, KindPyramid:
		return math.Sqrt(params.Width*params.Width+params.Height*params.Height+params.Depth*params.Depth) * 0.5
	case KindPlane:
		return math.Sqrt(params.Width*params.Width+params.Height*params.Height) * 0.5
	case KindSphere:
		return params.Radius
	case KindCylinder, KindCone:
		r := math.Max(params.RadiusTop, params.RadiusBottom)
		return math.Hypot(r, params.Height*0.5)
	case KindTorus:
		return params.Radius + params.Tube
	case KindTorusKnot:
		// The center curve reaches 1.5*radius from the origin, at the turn where
		// it leaves the Z axis, so the tube adds its whole radius.
		return params.Radius*1.5 + params.Tube
	}
	return 0
}

// Build tessellates one parametric geometry. want selects the vertex streams to
// fill. An unknown kind returns nil, and the caller must then report that it
// cannot draw the geometry instead of dropping the draw without a word.
func Build(params Params, want Attribute) *Mesh {
	params = Normalize(params)
	switch params.Kind {
	case KindCube:
		return buildBox(params.Size, params.Size, params.Size, want)
	case KindBox:
		return buildBox(params.Width, params.Height, params.Depth, want)
	case KindPlane:
		return buildPlane(params.Width, params.Height, want)
	case KindPyramid:
		return buildPyramid(params.Width, params.Height, params.Depth, want)
	case KindSphere:
		return buildSphere(params.Radius, params.Segments, maxInt(2, params.Segments/2), want)
	case KindCylinder:
		return buildCylinder(params.RadiusTop, params.RadiusBottom, params.Height, params.Segments, want)
	case KindCone:
		return buildCylinder(0, params.RadiusBottom, params.Height, params.Segments, want)
	case KindTorus:
		return buildTorus(params.Radius, params.Tube, params.RadialSegments, params.TubularSegments, want)
	case KindTorusKnot:
		return buildTorusKnot(params.Radius, params.Tube, params.RadialSegments, params.TubularSegments, want)
	}
	return nil
}

// DrawVertexCount returns how many vertices a renderer uploads and a wire
// payload ships, without building.
//
// This is the count after Expanded runs, so an indexed body such as the torus
// knot reports three vertices per triangle, not its smaller shared-vertex count.
// Memory reporting and size budgets read this number. An unknown kind returns
// zero.
func DrawVertexCount(params Params) int {
	params = Normalize(params)
	if params.Kind == KindTorusKnot {
		return params.RadialSegments * params.TubularSegments * 6
	}
	return VertexCount(params)
}

// VertexCount returns how many vertices Build emits, without building. An
// indexed body reports its shared-vertex count; call DrawVertexCount for the
// count a GPU upload or a wire payload carries. An unknown kind returns zero.
func VertexCount(params Params) int {
	params = Normalize(params)
	switch params.Kind {
	case KindCube, KindBox:
		return 36
	case KindPlane:
		return 6
	case KindPyramid:
		return 18
	case KindSphere:
		return params.Segments * maxInt(2, params.Segments/2) * 6
	case KindCylinder:
		return params.Segments * 12
	case KindCone:
		return params.Segments * 6
	case KindTorus:
		return params.RadialSegments * params.TubularSegments * 6
	case KindTorusKnot:
		// The knot grid keeps the wrap row and column separate.
		return (params.TubularSegments + 1) * (params.RadialSegments + 1)
```

### scene/geom/primitives.go

```go
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
```

### scene/tessellate.go

```go
		if g == nil {
			return TriangleMesh{}, false
		}
		return bufferTriangleMesh(*g)
	case LinesGeometry:
		// A polyline owns no surface. Callers pick it with a threshold instead.
		return TriangleMesh{}, false
	}
	params, ok := geometryParams(geometry)
	if !ok {
		return TriangleMesh{}, false
	}
	mesh := geom.Build(params, want)
	if mesh == nil || mesh.VertexCount() == 0 {
		return TriangleMesh{}, false
	}
	return TriangleMesh{
		Positions: mesh.Positions,
		Normals:   mesh.Normals,
		UVs:       mesh.UVs,
		Indices:   mesh.Indices,
	}, true
}

func bufferTriangleMesh(g BufferGeometry) (TriangleMesh, bool) {
	if len(g.Positions) < 9 && len(g.Indices) < 3 {
		return TriangleMesh{}, false
	}
	return TriangleMesh{
		Positions: g.Positions,
		Normals:   g.Normals,
		UVs:       g.UVs,
		Indices:   g.Indices,
	}, true
}

// geometryParams maps one authored parametric geometry onto the generator
// parameters. It reports false for a geometry the generator does not name.
//
// Add a case here whenever a new parametric Geometry type lands, and add the
// generator to package scene/geom. Forgetting the generator makes Tessellate
// report false, which the caller must then surface. Forgetting this case does
// the same. Neither failure can drop a draw in silence.
func geometryParams(geometry Geometry) (geom.Params, bool) {
	switch g := geometry.(type) {
	case CubeGeometry:
		return geom.Params{Kind: geom.KindCube, Size: g.Size}, true
	case BoxGeometry:
		return geom.Params{Kind: geom.KindBox, Width: g.Width, Height: g.Height, Depth: g.Depth}, true
	case PlaneGeometry:
		return geom.Params{Kind: geom.KindPlane, Width: g.Width, Height: g.Height}, true
	case PyramidGeometry:
		return geom.Params{Kind: geom.KindPyramid, Width: g.Width, Height: g.Height, Depth: g.Depth}, true
	case SphereGeometry:
		return geom.Params{Kind: geom.KindSphere, Radius: g.Radius, Segments: g.Segments}, true
	case CylinderGeometry:
		if g.RadiusTop <= 0 {
			return geom.Params{
				Kind: geom.KindCone, RadiusBottom: g.RadiusBottom, Height: g.Height, Segments: g.Segments,
			}, true
		}
		return geom.Params{
			Kind: geom.KindCylinder, RadiusTop: g.RadiusTop, RadiusBottom: g.RadiusBottom,
			Height: g.Height, Segments: g.Segments,
		}, true
	case TorusGeometry:
		return geom.Params{
			Kind: geom.KindTorus, Radius: g.Radius, Tube: g.Tube,
			RadialSegments: g.RadialSegments, TubularSegments: g.TubularSegments,
		}, true
	case TorusKnotGeometry:
		return geom.Params{
			Kind: geom.KindTorusKnot, Radius: g.Radius, Tube: g.Tube,
			RadialSegments: g.RadialSegments, TubularSegments: g.TubularSegments,
		}, true
	}
	return geom.Params{}, false
}
```

### scene/raycast.go

```go
// the switch once and reuse it for every instance.
func geometryBounds(geometry Geometry) (radius float64, strokes float64) {
	switch g := geometry.(type) {
	case SphereGeometry:
		return positiveOr(g.Radius, 1), 0
	case TorusGeometry:
		return torusRadius(g) + torusTube(g), 0
	case TorusKnotGeometry:
		// The knot center curve reaches 1.5*radius from the origin, at the turn
		// where the curve leaves the Z axis, so the tube adds its whole radius.
		return torusKnotRadius(g)*1.5 + torusKnotTube(g), 0
	case LinesGeometry:
		min, max := lineBounds(g)
		return boxCornerRadius(min, max), 1
	case CubeGeometry:
		size := positiveOr(g.Size, 1)
		return size / 2 * math.Sqrt(3), 0
	case BoxGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		return boxCornerRadius(min, max), 0
	case PlaneGeometry:
		width := positiveOr(g.Width, 1)
		height := positiveOr(g.Height, 1)
		return math.Hypot(width, height) / 2, 0
	case PyramidGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		return boxCornerRadius(min, max), 0
	case CylinderGeometry:
		radiusTop, radiusBottom := cylinderRadii(g.RadiusTop, g.RadiusBottom)
		return math.Hypot(math.Max(radiusTop, radiusBottom), positiveOr(g.Height, 1)/2), 0
	case BufferGeometry:
		return bufferBoundingRadius(g), 0
	case *BufferGeometry:
		if g == nil {
			return 0, 0
		}
		return bufferBoundingRadius(*g), 0
	default:
		// raycastGeometry falls back to a unit cube for unknown geometries.
		return math.Sqrt(3) / 2, 0
	}
}

// bufferBoundingRadius returns the farthest vertex distance from the origin. A
// triangle mesh carries its own extent, so the bound has to read the vertices.
func bufferBoundingRadius(geometry BufferGeometry) float64 {
	longest := 0.0
	positions := geometry.Positions
	for index := 0; index+3 <= len(positions); index += 3 {
		x, y, z := positions[index], positions[index+1], positions[index+2]
		if squared := x*x + y*y + z*z; squared > longest {
//
// Every other surface goes through Tessellate, the shared generator in package
// scene/geom. The torus knot is the one built-in body with no closed form, so its
// soup comes from that call; see torusKnotSoup. Adding a parametric geometry with
// no closed form means adding a generator to scene/geom and a case to
// geometryParams, not a second tessellator here. A second copy is what let the
// native renderer draw a different scene than the browser.
func raycastGeometry(geometry Geometry, ray Ray, localThreshold float64) (RayHit, string, bool) {
	switch g := geometry.(type) {
	case SphereGeometry:
		radius := positiveOr(g.Radius, 1)
		hit, ok := intersectSphere(ray, radius)
		hit.Method = "analytic-sphere"
		return hit, "sphere", ok
	case TorusGeometry:
		hit, ok := intersectTorus(ray, torusRadius(g), torusTube(g))
		hit.Method = "analytic-torus"
		return hit, "torus", ok
	case TorusKnotGeometry:
		// The knot has no closed form, so the test runs against the triangles the
		// renderer draws. torusKnotSoup builds them through Tessellate and caches
		// the hierarchy, so the picker and the renderer read one tessellation.
		soup := torusKnotSoup(g)
		hit, ok := soup.intersect(ray)
		hit.Method = "tessellated-triangle"
		return hit, "torusknot", ok
	case LinesGeometry:
		hit, ok := intersectLines(ray, g, localThreshold)
		hit.Method = "line-threshold"
		return hit, "lines", ok
	case BufferGeometry:
		soup := soupOfBuffer(g)
		hit, ok := soup.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *BufferGeometry:
		if g == nil {
			return RayHit{}, "gltf-mesh", false
		}
		soup := soupOfBuffer(*g)
		hit, ok := soup.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *triangleSoup:
		// SceneAccelerator swaps a triangle mesh for its prebuilt hierarchy. The
		// triangles are the same, so the hit is the same as the walk reports.
		hit, ok := g.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *segmentSoup:
		// The same swap for a polyline. The strokes are the same, and both paths
		// keep the nearest one with the same tie order.
		hit, ok := g.intersect(ray, localThreshold)
		hit.Method = "line-threshold"
		return hit, "lines", ok
	case CubeGeometry:
		size := positiveOr(g.Size, 1)
		hit, ok := intersectAABB(ray, Vector3{X: -size / 2, Y: -size / 2, Z: -size / 2}, Vector3{X: size / 2, Y: size / 2, Z: size / 2})
		hit.Method = "analytic-aabb"
		return hit, "cube", ok
	case BoxGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		hit, ok := intersectAABB(ray, min, max)
		hit.Method = "analytic-aabb"
		return hit, "box", ok
	case PlaneGeometry:
		hit, ok := intersectPlane(ray, positiveOr(g.Width, 1), positiveOr(g.Height, 1))
		hit.Method = "analytic-plane"
		return hit, "plane", ok
	case PyramidGeometry:
		hit, ok := intersectPyramid(ray, positiveOr(g.Width, 1), positiveOr(g.Height, 1), positiveOr(g.Depth, 1))
		hit.Method = "analytic-pyramid"
		return hit, "pyramid", ok
	case CylinderGeometry:
		radiusTop, radiusBottom := cylinderRadii(g.RadiusTop, g.RadiusBottom)
		hit, ok := intersectCylinder(ray, radiusTop, radiusBottom, positiveOr(g.Height, 1))
		hit.Method = "analytic-frustum"
		return hit, "cylinder", ok
	default:
		hit, ok := intersectAABB(ray, Vector3{X: -0.5, Y: -0.5, Z: -0.5}, Vector3{X: 0.5, Y: 0.5, Z: 0.5})
		hit.Method = "fallback-aabb"
		return hit, "cube", ok
	}
}

func intersectSphere(ray Ray, radius float64) (RayHit, bool) {
	oc := ray.Origin
	b := dotVector(oc, ray.Direction)
	c := dotVector(oc, oc) - radius*radius
	discriminant := b*b - c
	if discriminant < 0 {
		return RayHit{}, false
	}
	root := math.Sqrt(discriminant)
	t := -b - root
	if t < 0 {
		t = -b + root
	}
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	return RayHit{Distance: t, Point: point, Normal: normalizeVector(point)}, true
}

func intersectPlane(ray Ray, width, height float64) (RayHit, bool) {
	const epsilon = 1e-9
	if math.Abs(ray.Direction.Z) < epsilon {
		return RayHit{}, false
	}
	t := -ray.Origin.Z / ray.Direction.Z
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	if math.Abs(point.X) > width/2 || math.Abs(point.Y) > height/2 {
		return RayHit{}, false
	}
	normal := Vector3{Z: 1}
	if ray.Direction.Z > 0 {
		normal.Z = -1
	}
	return RayHit{Distance: t, Point: point, Normal: normal}, true
}

// intersectCylinder solves a finite Y-axis cylinder or truncated cone,
	min = subVectors(min, Vector3{X: padding, Y: padding, Z: padding})
	max = addVectors(max, Vector3{X: padding, Y: padding, Z: padding})
	return min, max
}

func positiveOr(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func cylinderRadii(top, bottom float64) (float64, float64) {
	if top <= 0 && bottom <= 0 {
		return 0.5, 0.5
	}
	return math.Max(0, top), math.Max(0, bottom)
}

func subVectors(left, right Vector3) Vector3 {
	return Vector3{X: left.X - right.X, Y: left.Y - right.Y, Z: left.Z - right.Z}
}

func scaleVector(value Vector3, scalar float64) Vector3 {
	return Vector3{X: value.X * scalar, Y: value.Y * scalar, Z: value.Z * scalar}
}
```

### scene/schema/vocabulary.go

```go
package schema

import (
	"fmt"
	"sort"
	"strings"
)

// This file closes the widest authoring gap in the native loop: a misspelled
// kind used to pass every check. The browser runtime coerces an unknown
// geometry kind to a cube and an unknown material kind to flat, and the CPU
// preview draws nothing at all. The author saw a green exit code either way.
//
// Every check below names the record, quotes the value the author wrote, and
// suggests the nearest legal spelling when one exists.

// geometryKinds is the closed set of mesh kinds the Scene3D runtimes accept.
// Unlike materials, geometry has no runtime registry, so anything outside this
// set is a mistake rather than an extension.
var geometryKinds = []string{
	"box",
	"cone",
	"cube",
	"cylinder",
	"gltf-mesh",
	"lines",
	"plane",
	"pyramid",
	"sphere",
	"torus",
	"torusknot",
}

// geometryKindAliases maps accepted spellings onto their canonical kind.
var geometryKindAliases = map[string]string{
	"torus-knot": "torusknot",
}

// materialKinds is the built-in material vocabulary. A project may add more
// through registerSceneMaterialProfile, so an unknown value is a warning by
// default and an error only under strict validation.
var materialKinds = []string{
	"custom",
	"flat",
	"ghost",
	"glass",
	"glow",
	"line-basic",
	"line-dashed",
	"matte",
	"standard",
}

// lightKinds is the closed set of light kinds the Scene3D IR carries.
var lightKinds = []string{
	"ambient",
	"directional",
	"hemisphere",
	"light-probe",
	"point",
	"rect-area",
	"spot",
}

// blendModes is the closed set of material blend modes.
var blendModes = []string{
	"additive",
	"alpha",
	"opaque",
}

// GeometryKinds returns the accepted mesh kinds in sorted order.
func GeometryKinds() []string { return sortedCopy(geometryKinds) }

// MaterialKinds returns the built-in material kinds in sorted order.
```

### client/js/bootstrap-src/12-scene-geometry.ts

```typescript
  // IIFE and function declarations hoist, so the call resolves lexically.
  //
  // The two families return the same shape and differ only in the count key:
  // this one uses `count`, the instanced one uses `vertexCount`. Tangents come
  // through as generated, which is better than the empty array
  // sceneFinalizePrimitiveMesh hands back.
  function sceneInstancedTriangleMesh(kind, object) {
    if (typeof generateInstancedGeometry !== "function") return null;
    const mesh = generateInstancedGeometry(kind, object || {});
    if (!mesh || !(mesh.vertexCount > 2)) return null;
    return {
      positions: mesh.positions,
      normals: mesh.normals,
      uvs: mesh.uvs,
      tangents: mesh.tangents || new Float32Array(0),
      count: mesh.vertexCount,
      immutable: true,
      revision: 0,
      dynamic: false,
    };
  }

  function scenePrimitiveTriangleMesh(object) {
    switch (object && object.kind) {
      case "box":
      case "cube":
        return boxTriangleMesh(object);
      case "plane":
        return planeTriangleMesh(object);
      case "sphere":
        return sphereTriangleMesh(object);
      case "torus":
        return torusTriangleMesh(object);
      case "torusknot":
        return torusKnotTriangleMesh(object);
      // cylinder, cone and pyramid had no case here, so
      // 10-runtime-scene-core.js never set vertices for them,
      // appendSceneObjectToBundle fell through to sceneObjectSegments, and
      // 15-scene-draw-plan.js kept the object on the line pass. Three
      // documented primitive kinds drew as wireframes when the author asked
      // for a solid mesh. The solid generators already existed in 16c.
      case "cylinder":
      case "cone":
      case "pyramid":
        return sceneInstancedTriangleMesh(object.kind, object);
      default:
        return null;
    }
  }

  function lineSegments(object) {
    const points = Array.isArray(object && object.points) ? object.points : [];
    const segments = Array.isArray(object && object.lineSegments) ? object.lineSegments : [];
    const out = [];
    for (const pair of segments) {
      if (!Array.isArray(pair) || pair.length < 2) {
        continue;
      }
      const from = points[pair[0]];
      const to = points[pair[1]];
```

### client/js/bootstrap-src/16c-scene-shared-pbr.ts

```typescript
    if (!isFinite(minX)) {
      return { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
    }
    return { minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ };
  }

  // Generate PBR vertex data (positions, normals, UVs, tangents) for a geometry
  // kind. Returns { positions, normals, uvs, tangents, vertexCount } where each
  // array is a Float32Array ready for GPU upload.
  function generateInstancedGeometry(kind, dims) {
    kind = normalizeInstancedGeometryKind(kind);
    var w = sceneNumber(dims && dims.width, 1);
    var h = sceneNumber(dims && dims.height, 1);
    var d = sceneNumber(dims && dims.depth, 1);
    var size = sceneNumber(dims && dims.size, 0);
    if (kind === "cube" && size > 0) {
      w = size;
      h = size;
      d = size;
    }

    if (kind === "sphere") {
      return generateInstancedSphereGeometry(
        sceneNumber(dims && dims.radius, 0.5),
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "plane") {
      return generateInstancedPlaneGeometry(w, d);
    }
    if (kind === "pyramid") {
      return generateInstancedPyramidGeometry(w, h, d);
    }
    if (kind === "cylinder") {
      return generateInstancedCylinderGeometry(
        sceneNumber(dims && dims.radiusTop, sceneNumber(dims && dims.radius, 0.5)),
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "cone") {
      return generateInstancedCylinderGeometry(
        0,
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "torus") {
      return generateInstancedTorusGeometry(
        sceneNumber(dims && dims.radius, 0.7),
        sceneNumber(dims && dims.tube, 0.3),
        sceneNumber(dims && dims.radialSegments, 32),
        sceneNumber(dims && dims.tubularSegments, 16)
      );
    }

    // Default: box geometry.
    return generateInstancedBoxGeometry(w, h, d);
  }

  function normalizeInstancedGeometryKind(kind) {
    if (typeof normalizeSceneKind === "function") {
      return normalizeSceneKind(kind);
    }
    var text = typeof kind === "string" ? kind.trim().toLowerCase() : "";
    switch (text) {
      case "cubegeometry":
        return "cube";
      case "boxgeometry":
        return "box";
      case "planegeometry":
      case "quad":
      case "quadgeometry":
        return "plane";
      case "pyramidgeometry":
        return "pyramid";
      case "spheregeometry":
      case "uvsphere":
      case "uvspheregeometry":
        return "sphere";
      case "cylindergeometry":
        return "cylinder";
      case "conegeometry":
        return "cone";
      case "torusgeometry":
        return "torus";
      case "torusknotgeometry":
      case "torus-knot":
        return "torusknot";
    var length = Math.sqrt(x * x + y * y + z * z);
    if (!Number.isFinite(length) || length <= 0.000001) {
      return [0, 1, 0];
    }
    return [x / length, y / length, z / length];
  }

  function instancedTriangleNormal(a, b, c) {
    var abx = b[0] - a[0];
    var aby = b[1] - a[1];
    var abz = b[2] - a[2];
    var acx = c[0] - a[0];
    var acy = c[1] - a[1];
    var acz = c[2] - a[2];
    return instancedNormalize3(
      aby * acz - abz * acy,
      abz * acx - abx * acz,
      abx * acy - aby * acx
    );
  }

  function createInstancedGeometryWriter(vertexCount) {
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;
    function push(position, normal, uv, tangent) {
      positions[vi * 3] = position[0];
      positions[vi * 3 + 1] = position[1];
      positions[vi * 3 + 2] = position[2];
      normals[vi * 3] = normal[0];
      normals[vi * 3 + 1] = normal[1];
      normals[vi * 3 + 2] = normal[2];
      uvs[vi * 2] = uv[0];
      uvs[vi * 2 + 1] = uv[1];
      tangents[vi * 4] = tangent[0];
      tangents[vi * 4 + 1] = tangent[1];
      tangents[vi * 4 + 2] = tangent[2];
      tangents[vi * 4 + 3] = tangent[3];
      vi++;
    }
    function build() {
      return {
        positions: vi * 3 === positions.length ? positions : positions.subarray(0, vi * 3),
        normals: vi * 3 === normals.length ? normals : normals.subarray(0, vi * 3),
        uvs: vi * 2 === uvs.length ? uvs : uvs.subarray(0, vi * 2),
        tangents: vi * 4 === tangents.length ? tangents : tangents.subarray(0, vi * 4),
        vertexCount: vi,
      };
    }
    return { push: push, build: build };
  }

  function pushInstancedFlatTri(writer, p0, p1, p2, uv0, uv1, uv2) {
    var normal = instancedTriangleNormal(p0, p1, p2);
    var tangent3 = instancedNormalize3(p1[0] - p0[0], p1[1] - p0[1], p1[2] - p0[2]);
    var tangent = [tangent3[0], tangent3[1], tangent3[2], 1];
    writer.push(p0, normal, uv0, tangent);
    writer.push(p1, normal, uv1, tangent);
    writer.push(p2, normal, uv2, tangent);
  }

  function generateInstancedPyramidGeometry(w, h, d) {
    var hw = instancedPositiveNumber(w, 1) * 0.5;
    var hh = instancedPositiveNumber(h, 1) * 0.5;
    var hd = instancedPositiveNumber(d, 1) * 0.5;
    var base = [[-hw, -hh, -hd], [hw, -hh, -hd], [hw, -hh, hd], [-hw, -hh, hd]];
    var apex = [0, hh, 0];
    var writer = createInstancedGeometryWriter(18);

    pushInstancedFlatTri(writer, base[0], base[1], base[2], [0, 0], [1, 0], [1, 1]);
    pushInstancedFlatTri(writer, base[0], base[2], base[3], [0, 0], [1, 1], [0, 1]);
    for (var i = 0; i < 4; i++) {
      pushInstancedFlatTri(writer, base[i], apex, base[(i + 1) % 4], [0, 1], [0.5, 0], [1, 1]);
    }
    return writer.build();
  }

  function generateInstancedCylinderGeometry(radiusTop, radiusBottom, height, segments) {
    var rt = Math.max(0, sceneNumber(radiusTop, 0.5));
    var rb = Math.max(0, sceneNumber(radiusBottom, 0.5));
    var h = instancedPositiveNumber(height, 1);
    if (rt === 0 && rb === 0) {
      rb = 0.5;
    }
    var count = instancedSegmentCount(segments, 32, 3, 256);
    var vertsPerSegment = (rt > 0 && rb > 0 ? 6 : 3) + (rb > 0 ? 3 : 0) + (rt > 0 ? 3 : 0);
    var writer = createInstancedGeometryWriter(count * vertsPerSegment);
    var halfH = h * 0.5;
    var slopeY = (rb - rt) / h;

    for (var i = 0; i < count; i++) {
      var u0 = i / count;
      var u1 = (i + 1) / count;
      var th0 = (Math.PI * 2 * i) / count;
      var th1 = (Math.PI * 2 * (i + 1)) / count;
      var c0 = Math.cos(th0);
      var s0 = Math.sin(th0);
      var c1 = Math.cos(th1);
      var s1 = Math.sin(th1);
      var n0 = instancedNormalize3(c0, slopeY, s0);
      var n1 = instancedNormalize3(c1, slopeY, s1);
      var t0 = [-s0, 0, c0, 1];
      var t1 = [-s1, 0, c1, 1];
      var b0 = [rb * c0, -halfH, rb * s0];
      var b1 = [rb * c1, -halfH, rb * s1];
      var top0 = [rt * c0, halfH, rt * s0];
      var top1 = [rt * c1, halfH, rt * s1];

      if (rb > 0 && rt > 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      } else if (rt === 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
      } else {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      }

      if (rb > 0) {
        writer.push([0, -halfH, 0], [0, -1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(b0, [0, -1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
        writer.push(b1, [0, -1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
      }
      if (rt > 0) {
        writer.push([0, halfH, 0], [0, 1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(top1, [0, 1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
        writer.push(top0, [0, 1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
      }
    }
    return writer.build();
  }

  function generateInstancedTorusGeometry(radius, tube, radialSegments, tubularSegments) {
    var major = instancedPositiveNumber(radius, 0.7);
```

### scene/geom/geom_test.go

```go
package geom

import (
	"math"
	"testing"
)

// meshTriangle returns the three corners of one triangle of a mesh, indexed or
// not. Every test in this package reads triangles through it, so an indexed mesh
// and a flat one are checked the same way.
func meshTriangle(m *Mesh, triangle int) (vec3, vec3, vec3) {
	corner := func(vertex int) vec3 {
		base := vertex * 3
		return vec3{m.Positions[base], m.Positions[base+1], m.Positions[base+2]}
	}
	if len(m.Indices) > 0 {
		base := triangle * 3
		return corner(m.Indices[base]), corner(m.Indices[base+1]), corner(m.Indices[base+2])
	}
	base := triangle * 3
	return corner(base), corner(base + 1), corner(base + 2)
}

func meshNormals(m *Mesh, triangle int) (vec3, vec3, vec3) {
	normal := func(vertex int) vec3 {
		base := vertex * 3
		return vec3{m.Normals[base], m.Normals[base+1], m.Normals[base+2]}
	}
	if len(m.Indices) > 0 {
		base := triangle * 3
		return normal(m.Indices[base]), normal(m.Indices[base+1]), normal(m.Indices[base+2])
	}
	base := triangle * 3
	return normal(base), normal(base + 1), normal(base + 2)
}

// assertWindingMatchesNormals checks that every triangle's geometric normal
// agrees with the shaded normals its own vertices carry.
//
// This is the winding test. Nothing else catches a reversed face: the ray tester
// hits both sides of a triangle, and the browser runtime does not cull back
// faces today. A reversed face would therefore pass every other test in the
// repository and turn invisible the day back-face culling arrives.
//
// A degenerate triangle carries no direction, so it is skipped and counted. The
// caller states how many the mesh is allowed to hold.
func assertWindingMatchesNormals(t *testing.T, label string, m *Mesh, allowedDegenerate int) {
	t.Helper()
	if len(m.Normals) == 0 {
		t.Fatalf("%s: the mesh carries no normals, so winding cannot be checked", label)
	}
	degenerate := 0
	for triangle := 0; triangle < m.TriangleCount(); triangle++ {
		p0, p1, p2 := meshTriangle(m, triangle)
		edge0 := subVec(p1, p0)
		edge1 := subVec(p2, p0)
		raw := crossVec(edge0, edge1)
		if math.Sqrt(dotVec(raw, raw)) < 1e-12 {
			degenerate++
			continue
		}
		geometric := normalize(raw)
		n0, n1, n2 := meshNormals(m, triangle)
		shaded := normalize(addVec(addVec(n0, n1), n2))
		if dot := dotVec(geometric, shaded); dot <= 0 {
```

### scene/raycast_test.go

```go
package scene

import (
	"math"
	"testing"
)

func TestRaycastGraphReturnsClosestMesh(t *testing.T) {
	graph := NewGraph(
		Mesh{
			ID:       "far",
			Geometry: SphereGeometry{Radius: 1},
			Position: Vec3(0, 0, -6),
		},
		Mesh{
			ID:       "near",
			Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2},
			Position: Vec3(0, 0, -2),
		},
	)
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected ray hit")
	}
	if hit.ID != "near" || hit.Kind != "box" {
		t.Fatalf("expected near box hit, got %#v", hit)
	}
	if math.Abs(hit.Distance-5) > 1e-9 {
		t.Fatalf("expected distance 5, got %v", hit.Distance)
	}
}

func TestRaycastGraphPickableOnlyAndMaxDistance(t *testing.T) {
	notPickable := false
	graph := NewGraph(
		Mesh{
			ID:       "shield",
			Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2},
			Position: Vec3(0, 0, -2),
			Pickable: &notPickable,
		},
		Mesh{
			ID:       "target",
			Geometry: SphereGeometry{Radius: 1},
			Position: Vec3(0, 0, -6),
		},
	)
	hit, ok := RaycastGraph(
		graph,
		Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)},
		PickableOnly(),
	)
	if !ok || hit.ID != "target" {
		t.Fatalf("expected pickable target, got %#v ok=%v", hit, ok)
	}
	if _, ok := RaycastGraph(
		graph,
		Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)},
		PickableOnly(),
		MaxDistance(4),
	); ok {
		t.Fatal("expected max distance to reject hit")
	}
}

func TestRaycastPickableChildSurvivesNonPickableParent(t *testing.T) {
	notPickable := false
	graph := NewGraph(Mesh{
		ID: "decoration", Geometry: BoxGeometry{Width: 4, Height: 4, Depth: 0.2}, Pickable: &notPickable,
		Children: []Node{Mesh{ID: "control", Geometry: SphereGeometry{Radius: 0.5}, Position: Vec3(0, 0, -1)}},
	})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)}, PickableOnly())
	if !ok || hit.ID != "control" {
		t.Fatalf("pickable child was hidden by non-pickable parent: %#v ok=%v", hit, ok)
	}
```
