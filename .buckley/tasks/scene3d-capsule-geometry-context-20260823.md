# Scene3D CapsuleGeometry source packet

Repository: `m31labs.dev/gosx`, MIT licensed at the repository root.
Exact task head: `d107361d9c453423b4ac05a621ff52a42ca53a85`.
Return only a complete valid unified diff beginning with `diff --git `.
Do not include prose or Markdown fences around the response.

This packet is the sole source snapshot for one bounded implementation attempt.
Every excerpt below is exact at the task head. Do not claim the repository is
unavailable and do not invent APIs outside these excerpts.

## Authoritative narrowing

Implement the original contract below with the existing transport fields:

- `CapsuleGeometry` has `Radius`, `Height`, `Segments`, and
  `RadialSegments`.
- `Height` is the straight cylindrical body length; total Y extent is
  `Height + 2*Radius`.
- `Segments` is the number of subdivisions per hemisphere.
- Defaults: radius 1, body height 1, cap segments 4, radial segments 8.
- Clamp cap segments to 1..64 and radial segments to 3..256.
- Lower directly into the already-existing `ObjectIR` /
  `InstancedMeshIR` fields `Radius`, `Height`, `Segments`, and
  `RadialSegments`; those fields already flow through `scene/ir.go`,
  `engine.RenderInstancedMesh`, preview, and native renderer parameter
  transfer. Do not add parallel transport fields.
- Canonical kind is `capsule`; accept `capsuleGeometry` in existing
  normalization seams.
- Add one shared Go generator in `scene/geom`. Browser production code stays
  TypeScript-first: add capsule generation to
  `client/js/bootstrap-src/16c-scene-shared-pbr.ts`, and route the ordinary
  object path in `12-scene-geometry.ts` through the shared in-IIFE instanced
  generator as cylinder/cone/pyramid already do.
- Picking must use a bounded analytic Y-axis capsule intersection (finite body
  plus two hemispherical caps), report `kind=capsule` and
  `method=analytic-capsule`, and use a broadphase radius that encloses radius
  X/Z and `Height/2 + Radius` Y.
- Tests may be added as new Go `*_test.go` files under the allowed packages.
  Extend the existing JavaScript test harness files shown below; do not add new
  handwritten production JavaScript.
- Do not edit generated JS/maps/compressed assets, CSS, GSX pages, examples, or
  unrelated files. Do not add dependencies.
- Keep the complete returned patch below 1,400 net new lines.

Required focused acceptance remains:

```sh
go test ./scene/... ./render/bundle/... -count=1
node --test client/js/12-scene-geometry.test.mjs client/js/12-scene-geometry-winding.test.mjs
go test ./cmd/buildbootstrap/... -count=1
go test ./... -count=1
go build ./...
git diff --check
```

## Exact source snapshot

### scene/scene.go (geometry API and lowering excerpts)

```go
}

// AnimationChannel describes one keyframe track targeting a single node property.
type AnimationChannel struct {
	TargetNode    int       // index of the target node in the scene node list
	Property      string    // "translation", "rotation", "scale"
	Interpolation string    // "LINEAR", "STEP" (default: "LINEAR")
	Times         []float64 // keyframe timestamps in seconds
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
	Position Vector3
	Rotation Euler
	Width    float64
}

// GridHelper renders an XZ-plane reference grid as line geometry.
type GridHelper struct {
	ID        string
	Size      float64
	Divisions int
	Color     string
	Position  Vector3
	Rotation  Euler
	Width     float64
}

// BoxHelper renders a wire box using Width/Height/Depth extents.
type BoxHelper struct {
	ID       string
	Width    float64
	Height   float64
	Depth    float64
	Color    string
	Position Vector3
	Rotation Euler
	WidthPx  float64
}

// BoundingBoxHelper renders a wire bounding box from min/max corners.
type BoundingBoxHelper struct {
	ID      string
	Min     Vector3
	Max     Vector3
	Color   string
	WidthPx float64
}

// SkeletonHelper renders a bone graph as line segments between joints.
type SkeletonHelper struct {
	ID       string
	Joints   []Vector3
	Bones    [][2]int
	Color    string
	Position Vector3
	Rotation Euler
	Width    float64
}

// TransformControls renders editor-style translate/rotate/scale handles.
// The first implementation is a visual helper surface; interactive pointer
// mutation is handled by the browser controls layer.
type TransformControls struct {
	ID       string
	Target   string
	Mode     string // "translate", "rotate", "scale"
	Size     float64
	Position Vector3
	Rotation Euler
	Width    float64
}

// Material describes one supported legacy material adapter.
type Material interface {
	sceneMaterial()
	legacyMaterial() map[string]any
}

type MaterialKind string

const (
	MaterialFlat  MaterialKind = "flat"
	MaterialGhost MaterialKind = "ghost"
	MaterialGlass MaterialKind = "glass"
	MaterialGlow  MaterialKind = "glow"
	MaterialMatte MaterialKind = "matte"
)

type MaterialBlendMode string

const (
	BlendOpaque   MaterialBlendMode = "opaque"
	BlendAlpha    MaterialBlendMode = "alpha"
	BlendAdditive MaterialBlendMode = "additive"
)

type PointStyle string

const (
	PointStyleSquare PointStyle = "square"
	PointStyleFocus  PointStyle = "focus"
	PointStyleGlow   PointStyle = "glow"
)

type MaterialRenderPass string

const (
	RenderOpaque   MaterialRenderPass = "opaque"
	RenderAlpha    MaterialRenderPass = "alpha"
	RenderAdditive MaterialRenderPass = "additive"
)

type MaterialStyle struct {
	Color      string
	Texture    string
	Opacity    *float64
	Emissive   *float64
	BlendMode  MaterialBlendMode
	RenderPass MaterialRenderPass
	Wireframe  *bool
}

// LineBasicMaterial styles line and helper geometry.
type LineBasicMaterial struct {
	MaterialStyle
	Width float64
}

// LineDashedMaterial styles line geometry with a repeating dash pattern.
type LineDashedMaterial struct {
	MaterialStyle
	Width    float64
	DashSize float64
	GapSize  float64
}

// CustomMaterial carries authored shader hooks and uniforms through Scene3D.
// WebGL custom shaders use GLSL ES snippets; WebGPU custom shaders use WGSL.
type CustomMaterial struct {
	StandardMaterial
	ShaderBackend     string
	ShaderLayout      map[string]any
	ShaderSource      string
	ShaderSourceFiles map[string]string
	VertexGLSL        string
	FragmentGLSL      string
	VertexWGSL        string
	FragmentWGSL      string
	Uniforms          map[string]any
}

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
}

type FlatMaterial MaterialStyle
type GhostMaterial MaterialStyle
type GlassMaterial MaterialStyle
type GlowMaterial MaterialStyle
type MatteMaterial MaterialStyle

// StandardMaterial is a PBR material using the roughness/metalness workflow.
type StandardMaterial struct {
	Color        string
	Texture      string
	Roughness    float64
	Metalness    float64
	Clearcoat    float64
	Sheen        float64
	Transmission float64
	Iridescence  float64
	Anisotropy   float64
	NormalMap    string
	RoughnessMap string
	MetalnessMap string
	OcclusionMap string
	EmissiveMap  string
	Emissive     float64
	Opacity      *float64
	BlendMode    MaterialBlendMode
	Wireframe    *bool
}

type quaternion struct {
	X float64
	Y float64
	Z float64
	W float64
}

type worldTransform struct {
	Position Vector3
	Rotation quaternion
}

type pendingLabel struct {
	label  Label
	parent worldTransform
}

type pendingSprite struct {
	sprite Sprite
	parent worldTransform
}

type pendingHTML struct {
	html   HTML
	parent worldTransform
}

type graphLowerer struct {
	objects            []ObjectIR
	models             []ModelIR
	points             []PointsIR
	instancedMeshes    []InstancedMeshIR
	instancedGLBMeshes []InstancedGLBMeshIR
	computeParticles   []ComputeParticlesIR
	waterSystems       []WaterSystemIR
	animations         []AnimationClipIR
	pending            []pendingLabel
	pendingSprites     []pendingSprite
	pendingHTML        []pendingHTML
	lights             []LightIR
	anchors            map[string]worldTransform
	nextObjectID       int
	nextLabelID        int
	nextSpriteID       int
	nextHTMLID         int
	nextLightID        int
	nextModelID        int
	nextPointsID       int
	nextInstancedID    int
	nextInstancedGLBID int
	nextParticlesID    int
	// spinTracks accumulates one GenSpin MotionIR Track per spinning node;
	// surfaced via SceneIR.SpinTracks (json:"-") as an in-memory facade.
	spinTracks []motion.Track
	// materialTracks accumulates one keyframe MotionIR Track per mesh
	// material-uniform animation (Target.Kind == TargetMaterial). These are
	// serialized into a SEPARATE wire program (SceneIR.MaterialMotionProgram)
	// so material packets route independently of transform motion.
	materialTracks []motion.Track
}

func (Group) sceneNode()             {}
func (Mesh) sceneNode()              {}
func (LODGroup) sceneNode()          {}
func (Decal) sceneNode()             {}
func (Points) sceneNode()            {}
func (InstancedMesh) sceneNode()     {}
func (InstancedGLBMesh) sceneNode()  {}
func (ComputeParticles) sceneNode()  {}
func (WaterSystem) sceneNode()       {}
func (Label) sceneNode()             {}
func (Sprite) sceneNode()            {}
func (HTML) sceneNode()              {}
func (HTMLSurface) sceneNode()       {}
func (Model) sceneNode()             {}
func (AmbientLight) sceneNode()      {}
func (DirectionalLight) sceneNode()  {}
func (PointLight) sceneNode()        {}
func (SpotLight) sceneNode()         {}
func (HemisphereLight) sceneNode()   {}
func (RectAreaLight) sceneNode()     {}
func (LightProbe) sceneNode()        {}
func (AnimationClip) sceneNode()     {}
func (AxesHelper) sceneNode()        {}
func (GridHelper) sceneNode()        {}
func (BoxHelper) sceneNode()         {}
func (BoundingBoxHelper) sceneNode() {}
func (SkeletonHelper) sceneNode()    {}
func (TransformControls) sceneNode() {}

func (CubeGeometry) sceneGeometry()      {}
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
func (GlassMaterial) sceneMaterial()      {}
func (GlowMaterial) sceneMaterial()       {}
func (MatteMaterial) sceneMaterial()      {}
func (StandardMaterial) sceneMaterial()   {}
func (LineBasicMaterial) sceneMaterial()  {}
func (LineDashedMaterial) sceneMaterial() {}
func (CustomMaterial) sceneMaterial()     {}

// Bool allocates a bool for opt-in Scene3D flags.
func Bool(value bool) *bool {
	return &value
}

// Float allocates a float64 for optional Scene3D numeric fields.
func Float(value float64) *float64 {
	return &value
}

// RequireWebGPU builds Scene3D requiredCapabilities for a WebGPU-only surface.
func RequireWebGPU(capabilities ...engine.Capability) []string {
		}
		record.Positions = flat
	}
	if len(pts.Sizes) > 0 && !useGenerator {
		record.Sizes = append([]float64(nil), pts.Sizes...)
	}
	if len(pts.Colors) > 0 {
		record.Colors = append([]string(nil), pts.Colors...)
	}
	if pts.Material != nil {
		applyMaterialToPointsIR(&record, *pts.Material)
	}
	l.points = append(l.points, record)
}

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
	if materialProps != nil {
		if mk, ok := mapStringValue(materialProps["materialKind"]); ok {
			record.MaterialKind = mk
		}
		if c, ok := materialProps["color"].(string); ok {
			record.Color = strings.TrimSpace(c)
		}
		if texture, ok := mapStringValue(materialProps["texture"]); ok {
			record.Texture = texture
		}
		if opacity, ok := mapFloat64OK(materialProps["opacity"]); ok {
			record.Opacity = Float(opacity)
		}
		if emissive, ok := mapFloat64OK(materialProps["emissive"]); ok {
			record.Emissive = Float(emissive)
		}
		if blendMode, ok := mapStringValue(materialProps["blendMode"]); ok {
			record.BlendMode = blendMode
		}
		if renderPass, ok := mapStringValue(materialProps["renderPass"]); ok {
			record.RenderPass = renderPass
		}
		if wireframe, ok := mapBool(materialProps["wireframe"]); ok {
func (l *graphLowerer) nextSceneHelperID(prefix, raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	l.nextObjectID += 1
	return prefix + "-" + intString(l.nextObjectID)
}

func applyGeometryProps(record *ObjectIR, props map[string]any) {
	if record == nil || len(props) == 0 {
		return
	}
	record.Size = mapFloat64(props["size"])
	record.Width = mapFloat64(props["width"])
	record.Height = mapFloat64(props["height"])
	record.Depth = mapFloat64(props["depth"])
	record.Radius = mapFloat64(props["radius"])
	record.Segments = mapInt(props["segments"])
	record.Points = mapVector3List(props["points"])
	record.LineSegments = mapSegmentPairs(props["segments"])
	record.LineWidth = mapFloat64(props["lineWidth"])
	record.RadiusTop = mapFloat64(props["radiusTop"])
	record.RadiusBottom = mapFloat64(props["radiusBottom"])
	record.Tube = mapFloat64(props["tube"])
	record.RadialSegments = mapInt(props["radialSegments"])
	record.TubularSegments = mapInt(props["tubularSegments"])
}

func applyMaterialProps(record *ObjectIR, props map[string]any) {
	if record == nil || len(props) == 0 {
		return
	}
	if kind, ok := mapStringValue(props["materialKind"]); ok {
		record.MaterialKind = kind
	}
	if color, ok := props["color"].(string); ok {
		record.Color = strings.TrimSpace(color)
	}
	if texture, ok := props["texture"].(string); ok {
		record.Texture = strings.TrimSpace(texture)
	}
	if opacity, ok := mapFloat64OK(props["opacity"]); ok {
		record.Opacity = Float(opacity)
	}
	if emissive, ok := mapFloat64OK(props["emissive"]); ok {
		record.Emissive = Float(emissive)
	}
	if blendMode, ok := mapStringValue(props["blendMode"]); ok {
		record.BlendMode = blendMode
	}
	if renderPass, ok := mapStringValue(props["renderPass"]); ok {
		record.RenderPass = renderPass
	}
	if wireframe, ok := mapBool(props["wireframe"]); ok {
		record.Wireframe = Bool(wireframe)
	}
	if lineDash, ok := mapBool(props["lineDash"]); ok {
		record.LineDash = Bool(lineDash)
	}
	if lineWidth, ok := mapFloat64OK(props["lineWidth"]); ok {
		record.LineWidth = lineWidth
	}
	record.DashSize = mapFloat64(props["dashSize"])
	record.GapSize = mapFloat64(props["gapSize"])
	if customVertex, ok := mapStringValue(props["customVertex"]); ok {
		record.CustomVertex = customVertex
	}
	if customFragment, ok := mapStringValue(props["customFragment"]); ok {
		record.CustomFragment = customFragment
	}
	if customVertexWGSL, ok := mapStringValue(props["customVertexWGSL"]); ok {
		record.CustomVertexWGSL = customVertexWGSL
	}
	if customFragmentWGSL, ok := mapStringValue(props["customFragmentWGSL"]); ok {
		record.CustomFragmentWGSL = customFragmentWGSL
	}
	if uniforms, ok := props["customUniforms"].(map[string]any); ok {
		record.CustomUniforms = cloneSceneAnyMap(uniforms)
	}
	if shaderBackend, ok := mapStringValue(props["shaderBackend"]); ok {
		record.ShaderBackend = shaderBackend
	}
	if shaderLayout, ok := props["shaderLayout"].(map[string]any); ok {
		record.ShaderLayout = cloneSceneAnyMap(shaderLayout)
	}
	if shaderSource, ok := mapStringValue(props["shaderSource"]); ok {
		record.ShaderSource = shaderSource
	}
	if shaderSourceFiles, ok := mapStringMapValue(props["shaderSourceFiles"]); ok {
		record.ShaderSourceFiles = shaderSourceFiles
	}
	record.Roughness = mapFloat64(props["roughness"])
	record.Metalness = mapFloat64(props["metalness"])
	record.Clearcoat = mapFloat64(props["clearcoat"])
	record.Sheen = mapFloat64(props["sheen"])
	record.Transmission = mapFloat64(props["transmission"])
	record.Iridescence = mapFloat64(props["iridescence"])
	record.Anisotropy = mapFloat64(props["anisotropy"])
	if normalMap, ok := mapStringValue(props["normalMap"]); ok {
		record.NormalMap = normalMap
	}
	if roughnessMap, ok := mapStringValue(props["roughnessMap"]); ok {
		record.RoughnessMap = roughnessMap
	}
	if metalnessMap, ok := mapStringValue(props["metalnessMap"]); ok {
		record.MetalnessMap = metalnessMap
	}
	if occlusionMap, ok := mapStringValue(props["occlusionMap"]); ok {
		record.OcclusionMap = occlusionMap
	}
	if emissiveMap, ok := mapStringValue(props["emissiveMap"]); ok {
		record.EmissiveMap = emissiveMap
	}
}

func legacyGeometry(geometry Geometry) (string, map[string]any) {
	if geometry == nil {
		return "cube", nil
	}
	return geometry.legacyGeometry()
}

// applyGeometryToObjectIR writes typed geometry fields directly onto the
// given ObjectIR record, returning the kind string. Replaces the older
// legacyGeometry + applyGeometryProps round-trip (typed Geometry → fresh
// map[string]any → read-back-into-record) which allocated one map per
// mesh even when a type switch could do the same work with zero
// allocations. Kept in parallel with legacyGeometry for the few
// non-hot-path callers that still want the map form.
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
		height = 1
	}
	if depth <= 0 {
		depth = 1
	}
	x := width / 2
	y := height / 2
	z := depth / 2
	return LinesGeometry{
		Points: []Vector3{
			{X: -x, Y: -y, Z: -z}, {X: x, Y: -y, Z: -z}, {X: x, Y: y, Z: -z}, {X: -x, Y: y, Z: -z},
			{X: -x, Y: -y, Z: z}, {X: x, Y: -y, Z: z}, {X: x, Y: y, Z: z}, {X: -x, Y: y, Z: z},
		},
		Segments: [][2]int{
			{0, 1}, {1, 2}, {2, 3}, {3, 0},
			{4, 5}, {5, 6}, {6, 7}, {7, 4},
			{0, 4}, {1, 5}, {2, 6}, {3, 7},
		},
		Width: lineWidth,
	}
}

func helperRingGeometry(radius float64, segments int, lineWidth float64) LinesGeometry {
	if radius <= 0 {
		radius = 1
	}
	if segments < 8 {
		segments = 32
	}
	points := make([]Vector3, 0, segments)
	links := make([][2]int, 0, segments)
	for i := 0; i < segments; i++ {
		angle := (float64(i) / float64(segments)) * math.Pi * 2
		points = append(points, Vector3{X: math.Cos(angle) * radius, Y: math.Sin(angle) * radius})
		links = append(links, [2]int{i, (i + 1) % segments})
	}
	return LinesGeometry{Points: points, Segments: links, Width: lineWidth}
}

func (g CubeGeometry) legacyGeometry() (string, map[string]any) {
	if g.Size <= 0 {
		return "cube", nil
	}
	return "cube", map[string]any{"size": g.Size}
}

func (g BoxGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "width", g.Width)
	setNumeric(out, "height", g.Height)
	setNumeric(out, "depth", g.Depth)
	if len(out) == 0 {
		return "box", nil
	}
	return "box", out
}

func (g PlaneGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "width", g.Width)
	setNumeric(out, "height", g.Height)
	if len(out) == 0 {
		return "plane", nil
	}
	return "plane", out
}

func (g PyramidGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "width", g.Width)
	setNumeric(out, "height", g.Height)
	setNumeric(out, "depth", g.Depth)
	if len(out) == 0 {
		return "pyramid", nil
	}
	return "pyramid", out
}

func (g SphereGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "radius", g.Radius)
	if g.Segments > 0 {
		out["segments"] = g.Segments
	}
	if len(out) == 0 {
		return "sphere", nil
	}
	return "sphere", out
}

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
	return material.legacyMaterial()
}

// applyMaterialToObjectIR writes typed material fields directly onto
// the given ObjectIR record. Parallel to applyGeometryToObjectIR —
// replaces the legacyMaterial → applyMaterialProps map round-trip
// with a zero-allocation type switch.
func applyMaterialToObjectIR(record *ObjectIR, material Material) {
```

### scene/scene_ir.go (existing transport fields)

```go
	ScaleY    float64 `json:"scaleY,omitempty"`
	ScaleZ    float64 `json:"scaleZ,omitempty"`
	RotationX float64 `json:"rotationX,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
	RotationZ float64 `json:"rotationZ,omitempty"`
}

// ObjectIR is the typed compatibility record for one lowered scene object.
type ObjectIR struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Size               float64  `json:"size,omitempty"`
	Width              float64  `json:"width,omitempty"`
	Height             float64  `json:"height,omitempty"`
	Depth              float64  `json:"depth,omitempty"`
	Radius             float64  `json:"radius,omitempty"`
	Segments           int      `json:"segments,omitempty"`
	LineSegments       [][2]int `json:"lineSegments,omitempty"`
	LineWidth          float64  `json:"lineWidth,omitempty"`
	RadiusTop          float64  `json:"radiusTop,omitempty"`
	RadiusBottom       float64  `json:"radiusBottom,omitempty"`
	Tube               float64  `json:"tube,omitempty"`
	RadialSegments     int      `json:"radialSegments,omitempty"`
	TubularSegments    int      `json:"tubularSegments,omitempty"`
	MaterialKind       string   `json:"materialKind,omitempty"`
	Color              string   `json:"color,omitempty"`
	Texture            string   `json:"texture,omitempty"`
	Opacity            *float64 `json:"opacity,omitempty"`
	Emissive           *float64 `json:"emissive,omitempty"`
	BlendMode          string   `json:"blendMode,omitempty"`
	RenderPass         string   `json:"renderPass,omitempty"`
	CustomFragmentWGSL    string            `json:"customFragmentWGSL,omitempty"`
	CustomVertexRef       string            `json:"customVertexRef,omitempty"`
	CustomFragmentRef     string            `json:"customFragmentRef,omitempty"`
	CustomVertexWGSLRef   string            `json:"customVertexWGSLRef,omitempty"`
	CustomFragmentWGSLRef string            `json:"customFragmentWGSLRef,omitempty"`
	CustomUniforms        map[string]any    `json:"customUniforms,omitempty"`
	ShaderBackend         string            `json:"shaderBackend,omitempty"`
	ShaderLayout          map[string]any    `json:"shaderLayout,omitempty"`
	ShaderSource          string            `json:"shaderSource,omitempty"`
	ShaderSourceFiles     map[string]string `json:"shaderSourceFiles,omitempty"`
	Transition            TransitionIR      `json:"transition,omitzero"`
	InState               map[string]any    `json:"inState,omitempty"`
	OutState              map[string]any    `json:"outState,omitempty"`
	Live                  []string          `json:"live,omitempty"`
	// QualityGroup: see scene.Points.QualityGroup and QualityRung.LayerGroups
	// (scene/quality_ladder.go). Empty means unconditionally visible.
	QualityGroup string `json:"qualityGroup,omitempty"`
}

// InstancedMeshIR is the typed compatibility record for one instanced mesh.
type InstancedMeshIR struct {
	ID                   string                     `json:"id"`
	Count                int                        `json:"count"`
	Kind                 string                     `json:"kind"`
	Size                 float64                    `json:"size,omitempty"`
	Width                float64                    `json:"width,omitempty"`
	Height               float64                    `json:"height,omitempty"`
	Depth                float64                    `json:"depth,omitempty"`
	Radius               float64                    `json:"radius,omitempty"`
	RadiusTop            float64                    `json:"radiusTop,omitempty"`
	RadiusBottom         float64                    `json:"radiusBottom,omitempty"`
	Tube                 float64                    `json:"tube,omitempty"`
	Segments             int                        `json:"segments,omitempty"`
	RadialSegments       int                        `json:"radialSegments,omitempty"`
	TubularSegments      int                        `json:"tubularSegments,omitempty"`
	MaterialKind         string                     `json:"materialKind,omitempty"`
	Color                string                     `json:"color,omitempty"`
	Texture              string                     `json:"texture,omitempty"`
	Opacity              *float64                   `json:"opacity,omitempty"`
	Emissive             *float64                   `json:"emissive,omitempty"`
	BlendMode            string                     `json:"blendMode,omitempty"`
	RenderPass           string                     `json:"renderPass,omitempty"`
	Wireframe            *bool                      `json:"wireframe,omitempty"`
	DepthWrite           *bool                      `json:"depthWrite,omitempty"`
	Roughness            float64                    `json:"roughness,omitempty"`
	Metalness            float64                    `json:"metalness,omitempty"`
```

### scene/ir.go (SceneIR to render transport)

```go
		Roughness:          mesh.Roughness,
		Metalness:          mesh.Metalness,
		NormalMap:          mesh.NormalMap,
		RoughnessMap:       mesh.RoughnessMap,
		MetalnessMap:       mesh.MetalnessMap,
		OcclusionMap:       mesh.OcclusionMap,
		EmissiveMap:        mesh.EmissiveMap,
		TextureDescriptors: mesh.TextureDescriptors,
	}
}

func objectToIRNode(object ObjectIR, materialIndex int) IRNode {
	return IRNode{
		Kind:          "mesh",
		ID:            object.ID,
		MaterialIndex: materialIndex,
		Transform:     transformFromObjectIR(object),
		Mesh: &IRMeshNode{
			Kind:            object.Kind,
			Size:            object.Size,
			Width:           object.Width,
			Height:          object.Height,
			Depth:           object.Depth,
			Radius:          object.Radius,
			Segments:        object.Segments,
			Points:          vector3ListToIR(object.Points),
			LineSegments:    object.LineSegments,
			LineWidth:       object.LineWidth,
			RadiusTop:       object.RadiusTop,
			RadiusBottom:    object.RadiusBottom,
			Tube:            object.Tube,
			RadialSegments:  object.RadialSegments,
			TubularSegments: object.TubularSegments,
			CastShadow:      object.CastShadow,
			ReceiveShadow:   object.ReceiveShadow,
			Pickable:        object.Pickable,
			Visible:         object.Visible,
			LODGroup:        object.LODGroup,
			LODLevel:        object.LODLevel,
			LODMinDistance:  object.LODMinDistance,
			LODMaxDistance:  object.LODMaxDistance,
		},
	}
}

func modelToIRNode(model ModelIR, materialIndex int) IRNode {
	node := objectToIRNode(model.ObjectIR, materialIndex)
	node.ID = model.ID
	node.Transform.ScaleX = model.ScaleX
	node.Transform.ScaleY = model.ScaleY
	node.Transform.ScaleZ = model.ScaleZ
	node.Mesh.Src = model.Src
	node.Mesh.PreviewSrc = model.PreviewSrc
	node.Mesh.FullSrc = model.FullSrc
	node.Mesh.Progressive = model.Progressive
	node.Mesh.Bounds = model.Bounds
	node.Mesh.Fit = model.Fit
	node.Mesh.FitAlign = model.FitAlign
	node.Mesh.Static = model.Static
	node.Mesh.Animation = model.Animation
	node.Mesh.AnimationSeq = model.AnimationSeq
	node.Mesh.AnimationSpeed = nonNegativeFloatPtr(model.AnimationSpeed)
	node.Mesh.AnimationWeight = nonNegativeFloatPtr(model.AnimationWeight)
	node.Mesh.AnimationFadeInMS = nonNegativeIntPtr(model.AnimationFadeInMS)
	node.Mesh.AnimationFadeOutMS = nonNegativeIntPtr(model.AnimationFadeOutMS)
	node.Mesh.Loop = model.Loop
	return node
}

func pointsToIRNode(points PointsIR) IRNode {
	return IRNode{
		Kind:      "points",
		ID:        points.ID,
		Transform: transformFromPointsIR(points),
		Points: &IRPointsNode{
			Count:          points.Count,
			Positions:      append([]float64(nil), points.Positions...),
			Sizes:          append([]float64(nil), points.Sizes...),
			Colors:         append([]string(nil), points.Colors...),
			Color:          points.Color,
			Style:          points.Style,
			Size:           points.Size,
			MinPixelSize:   points.MinPixelSize,
			MaxPixelSize:   points.MaxPixelSize,
			Opacity:        points.Opacity,
			BlendMode:      points.BlendMode,
			DepthWrite:     points.DepthWrite,
			Attenuation:    points.Attenuation,
			PositionStride: points.PositionStride,
		},
	}
}

func instancedToIRNode(mesh InstancedMeshIR, materialIndex int) IRNode {
	return IRNode{
		Kind:          "instanced-mesh",
		ID:            mesh.ID,
		MaterialIndex: materialIndex,
		InstancedMesh: &IRInstancedMesh{
			Count:           mesh.Count,
			Kind:            mesh.Kind,
			Size:            mesh.Size,
			Width:           mesh.Width,
			Height:          mesh.Height,
			Depth:           mesh.Depth,
			Radius:          mesh.Radius,
			RadiusTop:       mesh.RadiusTop,
			RadiusBottom:    mesh.RadiusBottom,
			Tube:            mesh.Tube,
			Segments:        mesh.Segments,
			RadialSegments:  mesh.RadialSegments,
			TubularSegments: mesh.TubularSegments,
			Transforms:      append([]float64(nil), mesh.Transforms...),
			Colors:          append([]string(nil), mesh.Colors...),
			Attributes:      cloneFloat64Slices(mesh.Attributes),
			CastShadow:      mesh.CastShadow,
			ReceiveShadow:   mesh.ReceiveShadow,
		},
	}
}

func computeToIRNode(compute ComputeParticlesIR) IRNode {
	return IRNode{
		Kind:         "compute-particles",
		ID:           compute.ID,
		Capabilities: []string{"compute"},
		Compute: &IRComputeNode{
			Count:    compute.Count,
			Emitter:  emitterToIR(compute.Emitter),
			Forces:   forcesToIR(compute.Forces),
			Material: particleMaterialToIR(compute.Material),
```

### engine/render_bundle.go (render mesh transport)

```go
	FragmentGLSL  string         `json:"fragmentGLSL,omitempty"`
	VertexGLSL    string         `json:"vertexGLSL,omitempty"`
	ShaderBackend string         `json:"shaderBackend,omitempty"`
	ShaderLayout  map[string]any `json:"shaderLayout,omitempty"`
	Uniforms      map[string]any `json:"uniforms,omitempty"`
}

// RenderDiagnostic describes an explicit renderer/backend decision surfaced to
// development tools and headless tests.
type RenderDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Backend  string `json:"backend,omitempty"`
	Target   string `json:"target,omitempty"`
}

// RenderInstancedMesh is a GPU-ready instanced mesh entry for the render bundle.
type RenderInstancedMesh struct {
	ID              string               `json:"id,omitempty"`
	Kind            string               `json:"kind"`
	Size            float64              `json:"size,omitempty"`
	Width           float64              `json:"width,omitempty"`
	Height          float64              `json:"height,omitempty"`
	Depth           float64              `json:"depth,omitempty"`
	Radius          float64              `json:"radius,omitempty"`
	RadiusTop       float64              `json:"radiusTop,omitempty"`
	RadiusBottom    float64              `json:"radiusBottom,omitempty"`
	Tube            float64              `json:"tube,omitempty"`
	Segments        int                  `json:"segments,omitempty"`
	RadialSegments  int                  `json:"radialSegments,omitempty"`
	TubularSegments int                  `json:"tubularSegments,omitempty"`
	MaterialIndex   int                  `json:"materialIndex"`
	VertexCount     int                  `json:"vertexCount"`
	InstanceCount   int                  `json:"instanceCount"`
	Transforms      []float64            `json:"transforms"`
	Colors          []float64            `json:"colors,omitempty"`
	Attributes      map[string][]float64 `json:"attributes,omitempty"`
	SkinID          string               `json:"skinID,omitempty"`
	JointIndices    []uint32             `json:"jointIndices,omitempty"`
	Weights         []float64            `json:"weights,omitempty"`
	BindPose        []float64            `json:"bindPose,omitempty"`
	CastShadow      bool                 `json:"castShadow,omitempty"`
	ReceiveShadow   bool                 `json:"receiveShadow,omitempty"`
}
```

### render/bundle/renderer.go (primitive parameter transfer)

```go
	return nil, nil, false
}

// InstancedMeshKey is the bus key under which the instanced mesh im — at draw
// slot idx in the bundle — resolves its draw-source resources. An external
// compute pass (e.g. an Elio-generated cull) that publishes "<key>.instances"
// and "<key>.drawArgs" drives that mesh's draw in place of the built-in cull
// (see instanceDrawSource). Exposed so external pass authors can target a
// specific mesh's draw without replicating the key construction.
func InstancedMeshKey(idx int, im engine.RenderInstancedMesh) string {
	return instancedMeshKey(idx, im)
}

// instancedMeshKey returns the cull/skin-cache key for one InstancedMesh slot.
// Combines the bundle index with the full primitive key so entries with the
// same Kind but different authored geometry parameters do not share stale
// vertex-count-dependent resources.
//
// Frame does not call this per draw. prepareMeshStates computes the key once
// per slot and reuses it until the slot's geometry parameters change.
func instancedMeshKey(idx int, im engine.RenderInstancedMesh) string {
	return instancedMeshKeyForParams(idx, primitiveParamsForInstancedMesh(im))
}

func primitiveParamsForInstancedMesh(im engine.RenderInstancedMesh) primitiveParams {
	return primitiveParams{
		Kind:            im.Kind,
		Size:            im.Size,
		Width:           im.Width,
		Height:          im.Height,
		Depth:           im.Depth,
		Radius:          im.Radius,
		RadiusTop:       im.RadiusTop,
		RadiusBottom:    im.RadiusBottom,
		Tube:            im.Tube,
		Segments:        im.Segments,
		RadialSegments:  im.RadialSegments,
		TubularSegments: im.TubularSegments,
	}
}

// runExternalPasses records every registered ExternalComputePass whose phase
// matches, in registration order, onto enc. Each pass dispatches and may
// publish bus resources (instance/indirect buffers) into r.published for later
// passes to consume. WebGPU auto-synchronizes the compute writes against the
// render passes that follow within this encoder.
func (r *Renderer) runExternalPasses(enc gpu.CommandEncoder, phase compute.PassPhase) error {
	if len(r.externalPasses) == 0 {
		return nil
	}
	if r.published == nil {
		r.published = make(map[string]compute.GPUResource)
	}
	ctx := compute.PassContext{
		Device:  r.device,
		Encoder: enc,
```

### scene/geom/geom.go

```go
// Package geom generates the triangles of every built-in GoSX geometry.
//
// One generator serves three consumers:
//
//   - The exact raycaster in package scene, which tests the drawn triangles.
//   - The native renderer in package render/bundle, which uploads them.
//   - The headless PNG oracle, which draws through the native renderer.
//
// The package holds no dependency on package scene and no dependency on the
// renderer, so both sides import it without a cycle. Keep it that way. A second
// copy of a generator makes the browser, the desktop renderer and the picker
// disagree, and no test can see the difference.
//
// All arithmetic runs in float64. The renderer narrows to float32 at upload
// time, and the raycaster keeps the full precision.
package geom

import (
	"math"
	"strconv"
	"strings"
)

// Attribute selects the vertex streams a build fills. Positions are always
// filled. A consumer that reads only positions, such as the raycaster, asks for
// nothing else and pays for nothing else.
type Attribute uint8

const (
	// AttrNormals fills Mesh.Normals with one unit vector per vertex.
	AttrNormals Attribute = 1 << iota
	// AttrUVs fills Mesh.UVs with one texture coordinate pair per vertex.
	AttrUVs
	// AttrColors fills Mesh.Colors with one RGB triple per vertex.
	AttrColors
)

// AllAttributes fills every stream. The renderer asks for this set.
const AllAttributes = AttrNormals | AttrUVs | AttrColors

// PositionsOnly fills positions and indices only. The raycaster asks for this
// set, because a triangle test reads no other stream.
const PositionsOnly Attribute = 0

// Mesh is a triangle mesh in flat buffers. Positions, Normals and Colors hold
// three numbers per vertex. UVs holds two. Indices holds three vertex numbers
// per triangle; an empty Indices means the positions already run as a triangle
// list.
//
// A stream the caller did not request stays nil.
type Mesh struct {
	Positions []float64
	Normals   []float64
	UVs       []float64
	Colors    []float64
	Indices   []int
}

// VertexCount returns the number of vertices the mesh holds.
func (m *Mesh) VertexCount() int {
	if m == nil {
		return 0
	}
	return len(m.Positions) / 3
}

// TriangleCount returns the number of triangles the mesh draws.
func (m *Mesh) TriangleCount() int {
	if m == nil {
		return 0
	}
	if len(m.Indices) > 0 {
		return len(m.Indices) / 3
	}
	return len(m.Positions) / 9
}

// Expanded returns a non-indexed copy of the mesh. An already non-indexed mesh
// comes back unchanged. The native renderer draws non-indexed buffers, so it
// calls this before upload.
func (m *Mesh) Expanded() *Mesh {
	if m == nil {
		return nil
	}
	if len(m.Indices) == 0 {
		return m
	}
	out := &Mesh{}
	out.Positions = expandAttr(m.Positions, m.Indices, 3)
	out.Normals = expandAttr(m.Normals, m.Indices, 3)
	out.UVs = expandAttr(m.UVs, m.Indices, 2)
	out.Colors = expandAttr(m.Colors, m.Indices, 3)
	return out
}

// expandAttr repeats one indexed attribute per index. An index that reaches
// past the source is skipped, which matches how the lowerer treats malformed
// index data.
func expandAttr(src []float64, indices []int, stride int) []float64 {
	if len(src) == 0 || stride <= 0 {
		return nil
	}
	out := make([]float64, 0, len(indices)*stride)
	for _, index := range indices {
		base := index * stride
		if index < 0 || base+stride > len(src) {
			continue
		}
		out = append(out, src[base:base+stride]...)
	}
	return out
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
	}
	return 0
}

// PositiveOr returns value when it is a positive finite number, and fallback
// otherwise.
func PositiveOr(value, fallback float64) float64 {
	if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	return fallback
}

// FirstPositive returns the first positive finite value, or zero when there is
// none.
func FirstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value
		}
	}
	return 0
}

// ClampInt resolves an authored count. A value at or below zero selects the
// fallback, and the result stays inside [minValue, maxValue].
func ClampInt(value, fallback, minValue, maxValue int) int {
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

### scene/geom/builder.go

```go
package geom

import "math"

// vec3 is a point or a direction in local geometry space.
type vec3 struct{ X, Y, Z float64 }

// vec2 is a texture coordinate.
type vec2 struct{ U, V float64 }

// vertex carries one vertex of every stream. A builder drops the streams the
// caller did not ask for.
type vertex struct {
	position vec3
	normal   vec3
	uv       vec2
	color    vec3
}

// builder collects vertices into flat buffers. It fills only the streams named
// by want, so a raycaster pays for positions alone.
type builder struct {
	want Attribute
	mesh Mesh
}

func newBuilder(want Attribute, vertexCapacity int) *builder {
	if vertexCapacity < 0 {
		vertexCapacity = 0
	}
	b := &builder{want: want}
	b.mesh.Positions = make([]float64, 0, vertexCapacity*3)
	if want&AttrNormals != 0 {
		b.mesh.Normals = make([]float64, 0, vertexCapacity*3)
	}
	if want&AttrUVs != 0 {
		b.mesh.UVs = make([]float64, 0, vertexCapacity*2)
	}
	if want&AttrColors != 0 {
		b.mesh.Colors = make([]float64, 0, vertexCapacity*3)
	}
	return b
}

// emit appends one vertex and returns its vertex number. An indexed builder
// keeps the number; a non-indexed builder throws it away.
func (b *builder) emit(v vertex) int {
	index := len(b.mesh.Positions) / 3
	b.mesh.Positions = append(b.mesh.Positions, v.position.X, v.position.Y, v.position.Z)
	if b.want&AttrNormals != 0 {
		b.mesh.Normals = append(b.mesh.Normals, v.normal.X, v.normal.Y, v.normal.Z)
	}
	if b.want&AttrUVs != 0 {
		b.mesh.UVs = append(b.mesh.UVs, v.uv.U, v.uv.V)
	}
	if b.want&AttrColors != 0 {
		b.mesh.Colors = append(b.mesh.Colors, v.color.X, v.color.Y, v.color.Z)
	}
	return index
}

// tri appends three vertices as one non-indexed triangle.
func (b *builder) tri(a, c, d vertex) {
	b.emit(a)
	b.emit(c)
	b.emit(d)
}

// flatTri appends one triangle whose three vertices share the face normal. Use
// it where a hard edge must stay hard.
func (b *builder) flatTri(p0, p1, p2 vec3, color vec3, uv0, uv1, uv2 vec2) {
	n := triangleNormal(p0, p1, p2)
	b.tri(
		vertex{position: p0, normal: n, uv: uv0, color: color},
		vertex{position: p1, normal: n, uv: uv1, color: color},
		vertex{position: p2, normal: n, uv: uv2, color: color},
	)
}

// index appends three vertex numbers as one triangle.
func (b *builder) index(a, c, d int) {
	b.mesh.Indices = append(b.mesh.Indices, a, c, d)
}

func (b *builder) build() *Mesh {
	mesh := b.mesh
	return &mesh
}

// triangleNormal returns the unit normal of the triangle a, b, c, wound
// counter-clockwise as seen from the side the normal points to. A degenerate
// triangle returns +Y, which keeps the buffer finite.
func triangleNormal(a, b, c vec3) vec3 {
	ab := vec3{b.X - a.X, b.Y - a.Y, b.Z - a.Z}
	ac := vec3{c.X - a.X, c.Y - a.Y, c.Z - a.Z}
	return normalize(vec3{
		X: ab.Y*ac.Z - ab.Z*ac.Y,
		Y: ab.Z*ac.X - ab.X*ac.Z,
		Z: ab.X*ac.Y - ab.Y*ac.X,
	})
}

// normalize scales a vector to unit length. A zero or non-finite vector returns
// +Y, so no buffer ever carries a NaN normal.
func normalize(v vec3) vec3 {
	length := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if length <= 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return vec3{Y: 1}
	}
	inv := 1 / length
	return vec3{X: v.X * inv, Y: v.Y * inv, Z: v.Z * inv}
}

func addVec(a, b vec3) vec3 { return vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

func subVec(a, b vec3) vec3 { return vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }

func scaleVec(v vec3, s float64) vec3 { return vec3{v.X * s, v.Y * s, v.Z * s} }

func dotVec(a, b vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

func crossVec(a, b vec3) vec3 {
	return vec3{
		X: a.Y*b.Z - a.Z*b.Y,
		Y: a.Z*b.X - a.X*b.Z,
		Z: a.X*b.Y - a.Y*b.X,
	}
}

// radialUV maps a point on a cap circle onto the unit square.
func radialUV(cosTheta, sinTheta float64) vec2 {
	return vec2{U: 0.5 + cosTheta*0.5, V: 0.5 + sinTheta*0.5}
}
```

### scene/geom/primitives.go

```go
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
```

### scene/tessellate.go

```go
package scene

import "m31labs.dev/gosx/scene/geom"

// TriangleMesh is the tessellated surface of one geometry, in the geometry's
// own local space. Positions and Normals hold three numbers per vertex; UVs
// hold two. Indices holds three vertex numbers per triangle; an empty Indices
// means the positions already run as a triangle list.
//
// Normals and UVs are empty when the source geometry carries none.
type TriangleMesh struct {
	Positions []float64
	Normals   []float64
	UVs       []float64
	Indices   []int
}

// VertexCount returns the number of vertices the mesh holds.
func (m TriangleMesh) VertexCount() int { return len(m.Positions) / 3 }

// TriangleCount returns the number of triangles the mesh draws.
func (m TriangleMesh) TriangleCount() int {
	if len(m.Indices) > 0 {
		return len(m.Indices) / 3
	}
	return len(m.Positions) / 9
}

// Tessellate turns one geometry into the triangles a renderer draws and a ray
// tests. It answers for every geometry that owns a surface. It reports false
// for a geometry with no surface, such as LinesGeometry, and for a nil or
// unknown geometry.
//
// The triangles come from package scene/geom, the single generator the browser
// wire path, the native renderer and the exact raycaster all share. Ask this
// function instead of writing a second generator. A second copy makes the three
// consumers disagree, and no test can see the difference.
//
// A BufferGeometry is already triangles, so the returned mesh borrows the
// caller's slices and copies nothing. Do not write through the result.
func Tessellate(geometry Geometry) (TriangleMesh, bool) {
	return tessellate(geometry, geom.AllAttributes)
}

// tessellate is the shared body. want selects the vertex streams to fill; the
// raycaster asks for positions alone, because a triangle test reads no other
// stream.
func tessellate(geometry Geometry, want geom.Attribute) (TriangleMesh, bool) {
	switch g := geometry.(type) {
	case nil:
		return TriangleMesh{}, false
	case BufferGeometry:
		return bufferTriangleMesh(g)
	case *BufferGeometry:
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

### scene/raycast.go (bounds, dispatch, relevant analytic helpers)

```go
	return false
}

// maxAbsComponent returns the largest absolute component of a scale vector. A
// bounding sphere grows by this factor under any non-uniform scale.
func maxAbsComponent(scale Vector3) float64 {
	return math.Max(math.Abs(scale.X), math.Max(math.Abs(scale.Y), math.Abs(scale.Z)))
}

// geometryBounds returns the local-space bounding sphere radius of a geometry
// centered on the node origin, plus how many pick radii that geometry needs on
// top of it. Every builtin geometry is origin centered, so a single radius
// bounds it. The radius must never understate the geometry or the broadphase
// would drop real hits.
//
// Lines strokes are the only geometry that reports a stroke count. A polyline
// has no cross-section, so its pick volume grows by one pick radius around every
// segment. Callers combine the two numbers as
// radius*maxScale + strokes*worldThreshold, which bounds the swept volume
// exactly. Returning both from one type switch lets an instanced mesh resolve
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
			longest = squared
		}
	}
	return math.Sqrt(longest)
}

// scaleFactor returns the factor that converts a local length into the longest
// world length the scale can produce. It never returns zero, so callers can
// divide a world threshold by it.
func scaleFactor(scale Vector3) float64 {
	factor := maxAbsComponent(scale)
	if factor <= 0 {
		return 1
	}
	return factor
}

// boxCornerRadius returns the radius of the sphere that encloses a box.
func boxCornerRadius(min, max Vector3) float64 {
	x := math.Max(math.Abs(min.X), math.Abs(max.X))
	y := math.Max(math.Abs(min.Y), math.Abs(max.Y))
	z := math.Max(math.Abs(min.Z), math.Abs(max.Z))
	return math.Sqrt(x*x + y*y + z*z)
}

// raycastTransformedGeometry runs the exact geometry test in local space.
// localThreshold is the pick radius in local units, which only Lines strokes
// read.
func raycastTransformedGeometry(geometry Geometry, world worldTransform, scale Vector3, ray Ray, localThreshold float64) (RayHit, bool) {
	localRay := localRayFor(world, scale, ray)
	localHit, kind, ok := raycastGeometry(geometry, localRay, localThreshold)
	if !ok {
		return RayHit{}, false
	}
	return worldHitFor(localHit, kind, world, scale, ray), true
}

// localRayFor moves a world ray into the local space of one node.
func localRayFor(world worldTransform, scale Vector3, ray Ray) Ray {
	inv := world.Rotation.conjugate().normalized()
	return Ray{
		Origin:    divideVector(inv.rotate(subVectors(ray.Origin, world.Position)), scale),
		Direction: normalizeVector(divideVector(inv.rotate(ray.Direction), scale)),
	}
}

// worldHitFor lifts a local-space hit back into world space. The distance comes
// from the world point, so a non-uniform scale reports the true world distance.
func worldHitFor(localHit RayHit, kind string, world worldTransform, scale Vector3, ray Ray) RayHit {
	point := addVectors(world.Position, world.Rotation.rotate(multiplyVector(localHit.Point, scale)))
	normal := normalizeVector(world.Rotation.rotate(divideVector(localHit.Normal, scale)))
	return RayHit{
		Kind:     kind,
		Distance: vectorLength(subVectors(point, ray.Origin)),
		Point:    point,
		Normal:   normal,
		Method:   localHit.Method,
	}
}

func appendTraceHit(trace *RayTrace, hit RayHit, opts RaycastOptions) {
	if opts.MaxDistance <= 0 || hit.Distance <= opts.MaxDistance {
		trace.Hits = append(trace.Hits, hit)
	}
}

// raycastGeometry runs the exact intersection for one geometry in its own local
// space. localThreshold is the pick radius in local units for geometries that
// have no cross-section. The returned Method names the routine that ran, so a
// trace stays honest about how exact the answer is.
//
// A geometry with a closed-form solution keeps it. An analytic sphere, box,
// plane, frustum, pyramid or torus answers for the ideal surface, which is more
// exact than any tessellation of it.
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
// including both end caps. This rejects the large false-positive corner
// regions produced by the former bounding-box approximation.
func intersectCylinder(ray Ray, radiusTop, radiusBottom, height float64) (RayHit, bool) {
	const epsilon = 1e-9
	half := height / 2
	slope := (radiusTop - radiusBottom) / height
	radiusAtOrigin := radiusBottom + slope*(ray.Origin.Y+half)
	a := ray.Direction.X*ray.Direction.X + ray.Direction.Z*ray.Direction.Z - slope*slope*ray.Direction.Y*ray.Direction.Y
	b := 2 * (ray.Origin.X*ray.Direction.X + ray.Origin.Z*ray.Direction.Z - radiusAtOrigin*slope*ray.Direction.Y)
	c := ray.Origin.X*ray.Origin.X + ray.Origin.Z*ray.Origin.Z - radiusAtOrigin*radiusAtOrigin

	bestT := math.Inf(1)
	bestNormal := Vector3{}
	considerSide := func(t float64) {
		if t < 0 || t >= bestT {
			return
		}
		point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
		if point.Y < -half-epsilon || point.Y > half+epsilon {
			return
		}
		radius := radiusBottom + slope*(point.Y+half)
		bestT = t
		bestNormal = normalizeVector(Vector3{X: point.X, Y: -radius * slope, Z: point.Z})
	}
	if math.Abs(a) < epsilon {
		if math.Abs(b) >= epsilon {
			considerSide(-c / b)
		}
	} else if discriminant := b*b - 4*a*c; discriminant >= 0 {
		root := math.Sqrt(discriminant)
		t0, t1 := (-b-root)/(2*a), (-b+root)/(2*a)
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		considerSide(t0)
		considerSide(t1)
	}

	considerCap := func(y, radius float64, normal Vector3) {
		if math.Abs(ray.Direction.Y) < epsilon {
			return
		}
		t := (y - ray.Origin.Y) / ray.Direction.Y
		if t < 0 || t >= bestT {
			return
		}
		point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
		if point.X*point.X+point.Z*point.Z <= radius*radius+epsilon {
			bestT, bestNormal = t, normal
		}
	}
	considerCap(-half, radiusBottom, Vector3{Y: -1})
	considerCap(half, radiusTop, Vector3{Y: 1})
	if math.IsInf(bestT, 1) {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, bestT))
	return RayHit{Distance: bestT, Point: point, Normal: bestNormal}, true
}

// torusRadius and torusTube resolve the authored torus size. The defaults match
// the renderer, which draws a zero field as radius 0.7 and tube 0.3. An exact
// test against the wrong size would not be exact, so the two paths must agree.
func torusRadius(geometry TorusGeometry) float64 { return positiveOr(geometry.Radius, 0.7) }

func torusTube(geometry TorusGeometry) float64 { return positiveOr(geometry.Tube, 0.3) }

// intersectTorus solves the exact ray/torus quartic. The torus rings around the
// Y axis and lies in the XZ plane, which is the surface the renderer builds:
//
//	P(u,v) = ((R + t*cos v)*cos u, t*sin v, (R + t*cos v)*sin u)
//
// The same surface in implicit form is
//
//	(x² + y² + z² + R² - t²)² = 4R²(x² + z²)
//
// Substituting the ray gives a quartic in the ray parameter. The solver first
// slides the parameter origin to the point of the ray nearest the torus center.
// That drops the cubic term, keeps every coefficient small, and bounds the
func lineBounds(g LinesGeometry) (Vector3, Vector3) {
	if len(g.Points) == 0 {
		return Vector3{X: -0.5, Y: -0.5, Z: -0.5}, Vector3{X: 0.5, Y: 0.5, Z: 0.5}
	}
	min := g.Points[0]
	max := g.Points[0]
	for _, point := range g.Points[1:] {
		min.X = math.Min(min.X, point.X)
		min.Y = math.Min(min.Y, point.Y)
		min.Z = math.Min(min.Z, point.Z)
		max.X = math.Max(max.X, point.X)
		max.Y = math.Max(max.Y, point.Y)
		max.Z = math.Max(max.Z, point.Z)
	}
	padding := math.Max(0.01, positiveOr(g.Width, 1)*0.01)
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

func dotVector(left, right Vector3) float64 {
	return left.X*right.X + left.Y*right.Y + left.Z*right.Z
}

func crossVector(left, right Vector3) Vector3 {
	return Vector3{
		X: left.Y*right.Z - left.Z*right.Y,
		Y: left.Z*right.X - left.X*right.Z,
		Z: left.X*right.Y - left.Y*right.X,
	}
}

func vectorLength(value Vector3) float64 {
	return math.Sqrt(dotVector(value, value))
```

### render/bundle/primitive.go

```go
package bundle

import (
	"math"

	"m31labs.dev/gosx/scene/geom"
)

// This file is the GPU-side adapter over package scene/geom. It holds no
// generator of its own.
//
// A second copy of a generator is what produced the torusknot defect: the
// browser drew a knot, this file did not know the name, normalizePrimitiveKind
// returned the empty string, primitiveCacheKey returned the empty string,
// ensurePrimitive returned nil, and the draw disappeared with no diagnostic.
// The desktop renderer and the headless PNG oracle drew a different scene than
// the browser. Add a new kind in scene/geom, never here.

// primitiveGeometry is the CPU-side geometry for a named primitive Kind.
// positions, colors, and normals hold three floats per vertex; uvs hold two.
//
// The renderer keeps primitives non-indexed. That matches the current WebGPU
// vertex layout, lets flat and smooth normals live side by side without an
// index-split pass, and keeps every native primitive upload as four tightly
// packed vertex buffers: positions, colors, normals, and uvs.
type primitiveGeometry struct {
	positions   []float32
	colors      []float32
	normals     []float32
	uvs         []float32
	vertexCount int
}

// primitiveParams mirrors the authored numbers on engine.RenderInstancedMesh.
// It is the bundle-side spelling of geom.Params.
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

func (p primitiveParams) geom() geom.Params {
	return geom.Params{
		Kind:            p.Kind,
		Size:            p.Size,
		Width:           p.Width,
		Height:          p.Height,
		Depth:           p.Depth,
		Radius:          p.Radius,
		RadiusTop:       p.RadiusTop,
		RadiusBottom:    p.RadiusBottom,
		Tube:            p.Tube,
		Segments:        p.Segments,
		RadialSegments:  p.RadialSegments,
		TubularSegments: p.TubularSegments,
	}
}

func primitiveParamsFromGeom(p geom.Params) primitiveParams {
	return primitiveParams{
		Kind:            p.Kind,
		Size:            p.Size,
		Width:           p.Width,
		Height:          p.Height,
		Depth:           p.Depth,
		Radius:          p.Radius,
		RadiusTop:       p.RadiusTop,
		RadiusBottom:    p.RadiusBottom,
		Tube:            p.Tube,
		Segments:        p.Segments,
		RadialSegments:  p.RadialSegments,
		TubularSegments: p.TubularSegments,
	}
}

// primitiveForKind returns native geometry for one Scene3D built-in mesh
// primitive kind, at that kind's default size. An unknown kind returns nil and
// the caller skips the draw.
func primitiveForKind(kind string) *primitiveGeometry {
	return primitiveForParams(primitiveParams{Kind: kind})
}

// primitiveForParams tessellates one primitive and narrows it to the float32
// buffers the vertex layout wants. An indexed mesh, such as the torus knot, is
// expanded to a flat triangle list first.
func primitiveForParams(params primitiveParams) *primitiveGeometry {
	mesh := geom.Build(params.geom(), geom.AllAttributes)
	if mesh == nil {
		return nil
	}
	return narrowMesh(mesh.Expanded())
}

// narrowMesh converts a geom.Mesh to the renderer's float32 buffers.
func narrowMesh(mesh *geom.Mesh) *primitiveGeometry {
	count := mesh.VertexCount()
	if count == 0 {
		return nil
	}
	return &primitiveGeometry{
		positions:   narrowFloats(mesh.Positions),
		colors:      narrowFloats(mesh.Colors),
		normals:     narrowFloats(mesh.Normals),
		uvs:         narrowFloats(mesh.UVs),
		vertexCount: count,
	}
}

func narrowFloats(src []float64) []float32 {
	if len(src) == 0 {
		return nil
	}
	out := make([]float32, len(src))
	for i, v := range src {
		out[i] = float32(v)
	}
	return out
}

func normalizePrimitiveParams(params primitiveParams) primitiveParams {
	return primitiveParamsFromGeom(geom.Normalize(params.geom()))
}

func normalizePrimitiveKind(kind string) string {
	return geom.NormalizeKind(kind)
}

func primitiveCacheKey(params primitiveParams) string {
	return geom.CacheKey(params.geom())
}

// sphereGeometry, cylinderGeometry and torusGeometry name the three curved
// bodies by their own parameters. They exist so a caller that already knows the
// shape does not have to fill a params struct.
func sphereGeometry(radius float64, longitudes, latitudes int) *primitiveGeometry {
	_ = latitudes // geom derives the latitude count from the longitude count.
	return primitiveForParams(primitiveParams{Kind: geom.KindSphere, Radius: radius, Segments: longitudes})
}

func cylinderGeometry(radiusTop, radiusBottom, height float64, segments int) *primitiveGeometry {
	if radiusTop <= 0 {
		return primitiveForParams(primitiveParams{
			Kind: geom.KindCone, RadiusBottom: radiusBottom, Height: height, Segments: segments,
		})
	}
	return primitiveForParams(primitiveParams{
		Kind: geom.KindCylinder, RadiusTop: radiusTop, RadiusBottom: radiusBottom, Height: height, Segments: segments,
	})
}

func torusGeometry(majorRadius, tubeRadius float64, radialSegments, tubularSegments int) *primitiveGeometry {
	return primitiveForParams(primitiveParams{
		Kind: geom.KindTorus, Radius: majorRadius, Tube: tubeRadius,
		RadialSegments: radialSegments, TubularSegments: tubularSegments,
	})
}

// instanceCullRadius scales a primitive's bounding radius by the scale baked
// into one instance transform. The renderer stores unscaled radii per
// primitive, so an instance scaled up 10x needs a radius 10x larger or the cull
// drops it while it is still on screen.
//
// The largest of the three column lengths is the conservative choice for
// non-uniform scale: it never under-estimates the sphere, so an instance is
// never wrongly culled. Skew from a sheared matrix inflates the radius, which
// is safe.
//
// cullWGSL runs the same calculation per thread on the GPU. Keep the two in
// step: this function is the CPU oracle the headless backend and the pick
// bounding test share.
func instanceCullRadius(baseRadius float32, model mat4) float32 {
	sx := columnLength(model[0], model[1], model[2])
	sy := columnLength(model[4], model[5], model[6])
	sz := columnLength(model[8], model[9], model[10])
	scale := sx
	if sy > scale {
		scale = sy
	}
	if sz > scale {
		scale = sz
	}
	if scale <= 0 {
		return baseRadius
	}
	return baseRadius * scale
}

func columnLength(x, y, z float32) float32 {
	return float32(math.Sqrt(float64(x*x + y*y + z*z)))
}

// primitiveCullRadiusMargin pads the tight bounding sphere. The pad covers the
// difference between the ideal surface and the chorded tessellation, plus the
// float32 rounding at upload.
const primitiveCullRadiusMargin = 1.05

// primitiveCullRadius returns the cull sphere of one primitive at unit scale.
// An unknown kind returns 2, which holds every default-sized built-in body, so
// an unrecognized draw is never culled by mistake.
func primitiveCullRadius(params primitiveParams) float32 {
	radius := geom.BoundingRadius(params.geom())
	if radius <= 0 {
		return 2
	}
	return float32(radius * primitiveCullRadiusMargin)
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
func MaterialKinds() []string { return sortedCopy(materialKinds) }

// LightKinds returns the accepted light kinds in sorted order.
func LightKinds() []string { return sortedCopy(lightKinds) }

// validateGeometryKind reports a mesh kind that no Scene3D runtime can draw.
func validateGeometryKind(report *Report, kind, id, path string) {
	normalized := canonicalKind(kind, geometryKindAliases)
	if normalized == "" || contains(geometryKinds, normalized) {
		return
	}
	report.add(Error, "scene.geometry.unknown_kind",
		unknownKindMessage("Geometry kind", kind, normalized, geometryKinds,
			"no Scene3D runtime can draw it; the browser runtime substitutes a cube and the native preview draws nothing"),
		path+".kind", id, kindData(kind, normalized, geometryKinds))
}

// validateMaterialKind reports a material kind outside the built-in set. It
// stays a warning by default because a project may register extra profiles.
func validateMaterialKind(report *Report, kind, id, path string, strict bool) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" || contains(materialKinds, normalized) {
		return
	}
	severity := Warn
	if strict {
		severity = Error
	}
	report.add(severity, "scene.material.unknown_kind",
		unknownKindMessage("Material kind", kind, normalized, materialKinds,
			"the browser runtime falls back to flat unless a material profile with this name is registered"),
		path+".materialKind", id, kindData(kind, normalized, materialKinds))
}

// validateLightKind reports a light kind that no Scene3D runtime lights with.
func validateLightKind(report *Report, kind, id, path string) {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if normalized == "" || contains(lightKinds, normalized) {
		return
	}
	report.add(Error, "scene.light.unknown_kind",
		unknownKindMessage("Light kind", kind, normalized, lightKinds,
			"no Scene3D runtime lights the scene with it"),
		path+".kind", id, kindData(kind, normalized, lightKinds))
}

// validateBlendMode reports a blend mode outside the closed set.
func validateBlendMode(report *Report, mode, id, path string) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" || contains(blendModes, normalized) {
		return
	}
	report.add(Error, "scene.material.unknown_blend_mode",
		unknownKindMessage("Blend mode", mode, normalized, blendModes, "it is not one of the supported modes"),
		path+".blendMode", id, kindData(mode, normalized, blendModes))
}

func unknownKindMessage(label, written, normalized string, vocabulary []string, consequence string) string {
	message := fmt.Sprintf("%s %q is not recognized; %s", label, written, consequence)
	if suggestion := nearestValue(normalized, vocabulary); suggestion != "" {
		return message + fmt.Sprintf(". Did you mean %q?", suggestion)
	}
	return message + ". Accepted values are " + strings.Join(sortedCopy(vocabulary), ", ")
}

func kindData(written, normalized string, vocabulary []string) map[string]any {
	data := map[string]any{"value": written, "accepted": sortedCopy(vocabulary)}
	if suggestion := nearestValue(normalized, vocabulary); suggestion != "" {
		data["suggestion"] = suggestion
	}
	return data
}

// canonicalKind lowercases a kind and resolves any accepted alias.
func canonicalKind(kind string, aliases map[string]string) string {
	normalized := strings.ToLower(strings.TrimSpace(kind))
	if canonical, ok := aliases[normalized]; ok {
		return canonical
	}
	return normalized
}

// nearestValue returns the closest vocabulary entry when the author probably
// misspelled one. It returns an empty string when nothing is close, so an
// unrelated value never receives a misleading suggestion.
func nearestValue(value string, vocabulary []string) string {
	best, bestDistance := "", 0
	for _, candidate := range sortedCopy(vocabulary) {
		distance := editDistance(value, candidate)
		if best == "" || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	limit := len(value) / 3
	if limit < 1 {
		limit = 1
	}
	if bestDistance > limit {
		return ""
	}
	return best
}

// editDistance returns the Levenshtein distance between two short strings.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = minOf(previous[j]+1, minOf(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func minOf(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

### scene/schema/schema.json (object primitive fields)

```json
        "clearcoat": { "type": "number" },
        "sheen": { "type": "number" },
        "transmission": { "type": "number" },
        "iridescence": { "type": "number" },
        "anisotropy": { "type": "number" },
        "normalMap": { "type": "string" },
        "roughnessMap": { "type": "string" },
        "metalnessMap": { "type": "string" },
        "emissiveMap": { "type": "string" }
      }
    },
    "primitiveParams": {
      "type": "object",
      "additionalProperties": true,
      "properties": {
        "kind": { "$ref": "#/$defs/id" },
        "size": {
          "type": "number",
          "minimum": 0
        },
        "width": {
          "type": "number",
          "minimum": 0
        },
        "height": {
          "type": "number",
          "minimum": 0
        },
        "depth": {
          "type": "number",
          "minimum": 0
        },
        "radius": {
          "type": "number",
          "minimum": 0
        },
        "radiusTop": {
          "type": "number",
          "minimum": 0
        },
        "radiusBottom": {
          "type": "number",
          "minimum": 0
        },
        "tube": {
          "type": "number",
          "minimum": 0
        },
        "segments": {
          "type": "integer",
          "minimum": 0
        },
        "radialSegments": {
          "type": "integer",
          "minimum": 0
        },
        "tubularSegments": {
          "type": "integer",
          "minimum": 0
        }
      }
    },
    "object": {
      "allOf": [
        { "$ref": "#/$defs/primitiveParams" },
        { "$ref": "#/$defs/materialFields" },
        { "$ref": "#/$defs/transformFields" },
        { "$ref": "#/$defs/lifecycleFields" },
        {
          "type": "object",
          "required": ["id", "kind"],
          "additionalProperties": true,
          "properties": {
            "id": { "$ref": "#/$defs/id" },
            "points": {
              "type": "array",
              "items": { "$ref": "#/$defs/vector3" }
            },
            "lineSegments": {
              "type": "array",
              "items": {
                "type": "array",
                "prefixItems": [
                  {
                    "type": "integer",
                    "minimum": 0
                  },
                  {
                    "type": "integer",
                    "minimum": 0
                  }
```

### client/js/bootstrap-src/12-scene-geometry.ts

```typescript
  // Scene geometry — vertex generation for wireframe primitives.

  function sceneSegmentResolution(value) {
    const segments = Math.round(sceneNumber(value, 12));
    return Math.max(6, Math.min(24, segments));
  }

  function scenePrimitiveSegmentResolution(value, fallback, minValue, maxValue) {
    const segments = Math.round(sceneNumber(value, fallback));
    return Math.max(minValue, Math.min(maxValue, segments));
  }

  function scenePositiveNumber(value, fallback) {
    const number = sceneNumber(value, fallback);
    return number > 0 ? number : fallback;
  }

  function boxVertices(width, height, depth) {
    const halfWidth = width / 2;
    const halfHeight = height / 2;
    const halfDepth = depth / 2;
    return [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: halfHeight, z: -halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: halfWidth, y: halfHeight, z: halfDepth },
      { x: -halfWidth, y: halfHeight, z: halfDepth },
    ];
  }

  const boxEdgePairs = [
    [0, 1], [1, 2], [2, 3], [3, 0],
    [4, 5], [5, 6], [6, 7], [7, 4],
    [0, 4], [1, 5], [2, 6], [3, 7],
  ];

  function indexSegments(points, edgePairs) {
    return edgePairs.map(function(edge) {
      return [points[edge[0]], points[edge[1]]];
    });
  }

  function boxSegments(object) {
    return indexSegments(boxVertices(object.width, object.height, object.depth), boxEdgePairs);
  }

  // boxVertices packs the -Z face first, so at height zero slice(0, 4)
  // collapses to one edge. These indices walk the actual XZ corner ring.
  const planeQuadIndices = [0, 1, 5, 4];

  function planeQuadVertices(width, depth) {
    const vertices = boxVertices(width, 0, depth);
    return [
      vertices[planeQuadIndices[0]],
      vertices[planeQuadIndices[1]],
      vertices[planeQuadIndices[2]],
      vertices[planeQuadIndices[3]],
    ];
  }

  function planeSegments(object) {
    return indexSegments(planeQuadVertices(object.width, object.depth), [
      [0, 1], [1, 2], [2, 3], [3, 0],
    ]);
  }

  function pyramidSegments(object) {
    const halfWidth = object.width / 2;
    const halfDepth = object.depth / 2;
    const halfHeight = object.height / 2;
    const vertices = [
      { x: -halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: -halfDepth },
      { x: halfWidth, y: -halfHeight, z: halfDepth },
      { x: -halfWidth, y: -halfHeight, z: halfDepth },
      { x: 0, y: halfHeight, z: 0 },
    ];
    return indexSegments(vertices, [
      [0, 1], [1, 2], [2, 3], [3, 0],
      [0, 4], [1, 4], [2, 4], [3, 4],
    ]);
  }

  function circleSegments(radius, axis, segments) {
    const points = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      points.push(circlePoint(radius, axis, angle));
    }
    const out = [];
    for (let i = 0; i < points.length; i += 1) {
      out.push([points[i], points[(i + 1) % points.length]]);
    }
    return out;
  }

  function circlePoint(radius, axis, angle) {
    const sin = Math.sin(angle) * radius;
    const cos = Math.cos(angle) * radius;
    switch (axis) {
      case "xy":
        return { x: cos, y: sin, z: 0 };
      case "yz":
        return { x: 0, y: cos, z: sin };
      default:
        return { x: cos, y: 0, z: sin };
    }
  }

  function sphereSegments(object) {
    return []
      .concat(circleSegments(object.radius, "xy", object.segments))
      .concat(circleSegments(object.radius, "xz", object.segments))
      .concat(circleSegments(object.radius, "yz", object.segments));
  }

  function cylinderSegments(object) {
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 3, 256);
    const radiusTop = scenePositiveNumber(object && object.radiusTop, scenePositiveNumber(object && object.radius, 0.5));
    const radiusBottom = scenePositiveNumber(object && object.radiusBottom, scenePositiveNumber(object && object.radius, 0.5));
    const halfHeight = scenePositiveNumber(object && object.height, 1) * 0.5;
    const bottom = [];
    const top = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      const cos = Math.cos(angle);
      const sin = Math.sin(angle);
      bottom.push({ x: radiusBottom * cos, y: -halfHeight, z: radiusBottom * sin });
      top.push({ x: radiusTop * cos, y: halfHeight, z: radiusTop * sin });
    }
    const out = [];
    for (let i = 0; i < segments; i += 1) {
      const next = (i + 1) % segments;
      out.push([bottom[i], bottom[next]]);
      out.push([top[i], top[next]]);
      out.push([bottom[i], top[i]]);
    }
    return out;
  }

  function coneSegments(object) {
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 3, 256);
    const radius = scenePositiveNumber(object && object.radiusBottom, scenePositiveNumber(object && object.radius, 0.5));
    const halfHeight = scenePositiveNumber(object && object.height, 1) * 0.5;
    const apex = { x: 0, y: halfHeight, z: 0 };
    const base = [];
    for (let i = 0; i < segments; i += 1) {
      const angle = (Math.PI * 2 * i) / segments;
      base.push({ x: radius * Math.cos(angle), y: -halfHeight, z: radius * Math.sin(angle) });
    }
    const out = [];
    for (let i = 0; i < segments; i += 1) {
      const next = (i + 1) % segments;
      out.push([base[i], base[next]]);
      out.push([base[i], apex]);
    }
    return out;
  }

  function torusSegments(object) {
    const radialSegments = scenePrimitiveSegmentResolution(object && object.radialSegments, 32, 3, 256);
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 16, 3, 128);
    const radius = scenePositiveNumber(object && object.radius, 0.7);
    const tube = scenePositiveNumber(object && object.tube, 0.3);
    function point(i, j) {
      const u = (Math.PI * 2 * i) / radialSegments;
      const v = (Math.PI * 2 * j) / tubularSegments;
      const cu = Math.cos(u);
      const su = Math.sin(u);
      const cv = Math.cos(v);
      const r = radius + tube * cv;
      return { x: r * cu, y: tube * Math.sin(v), z: r * su };
    }
    const out = [];
    for (let i = 0; i < radialSegments; i += 1) {
      const next = (i + 1) % radialSegments;
      out.push([point(i, 0), point(next, 0)]);
      out.push([point(i, Math.floor(tubularSegments / 2)), point(next, Math.floor(tubularSegments / 2))]);
    }
    const radialStride = Math.max(1, Math.floor(radialSegments / 8));
    for (let i = 0; i < radialSegments; i += radialStride) {
      for (let j = 0; j < tubularSegments; j += 1) {
        out.push([point(i, j), point(i, (j + 1) % tubularSegments)]);
      }
    }
    return out;
  }

  function scenePushMeshVertex(out, position, normal, uv) {
    out.positions.push(position.x, position.y, position.z);
    out.normals.push(normal.x, normal.y, normal.z);
    out.uvs.push(uv.x, uv.y);
    out.count += 1;
  }

  function scenePushMeshTriangle(out, a, b, c, normal, uva, uvb, uvc) {
    scenePushMeshVertex(out, a, normal, uva || { x: 0, y: 0 });
    scenePushMeshVertex(out, b, normal, uvb || { x: 1, y: 0 });
    scenePushMeshVertex(out, c, normal, uvc || { x: 1, y: 1 });
  }

  function sceneFinalizePrimitiveMesh(out) {
    if (!out || out.count < 3) return null;
    return {
      positions: new Float32Array(out.positions),
      normals: new Float32Array(out.normals),
      uvs: new Float32Array(out.uvs),
      tangents: new Float32Array(0),
      count: out.count,
      immutable: true,
      revision: 0,
      dynamic: false,
    };
  }

  function scenePrimitiveMeshBuilder() {
    return { positions: [], normals: [], uvs: [], count: 0 };
  }

  // Winding convention for every solid mesh below.
  //
  // Wind each triangle counter-clockwise as seen from outside the surface. The
  // geometric normal that the right-hand rule gives then agrees with the outward
  // normals the three vertices carry.
  //
  // Three producers build the same primitive kinds, and one authored shape can
  // reach the screen through any of them:
  //   - this file, when the renderer draws the object on its own;
  //   - generateInstancedGeometry in 16c-scene-shared-pbr.js, when the renderer
  //     instances the object;
  //   - scene/geom in Go, for the native renderer and the headless oracle.
  //
  // box, plane, sphere and torus were wound the other way here. They measured
  // -1.000000, -1.000000, -0.999170 and -0.997526 against their own normals,
  // while 16c and scene/geom measured the same three figures positive. One
  // authored box therefore had opposite winding depending only on whether the
  // renderer instanced it.
  //
  // Four permissive defaults hid the split in the MAIN colour pass:
  //   - the WebGL main pass calls gl.disable(gl.CULL_FACE);
  //   - the WebGPU PBR pipeline sets cullMode "none";
  //   - sceneRayIntersectsTriangle reports a hit on both faces;
  //   - the native Go renderer reads scene/geom and never reads this file.
  //
  // FOUR browser draw paths DO cull. Read every one before you touch this file.
  //   - the WebGL shadow pass enables CULL_FACE and calls cullFace(gl.FRONT);
  //   - the WebGPU gosx-shadow pipeline sets cullMode "front";
  //   - the WebGPU gosx-shadow-instanced pipeline sets cullMode "front";
  //   - drawPBRObjects in 16a-scene-webgpu.js leaves a mesh object on
  //     getSelenaPipeline's cullMode "back" default whenever the object carries
  //     a Selena custom shader and doubleSided stays false.
  //
  // The three shadow sites keep the faces that point AWAY from the light, which
  // is the standard mitigation for peter-panning. So the winding below decides
  // which surface a browser shadow map records. render/bundle/renderer.go keeps
  // the opposite face natively, and render/bundle/shadow_drift_test.go pins all
  // three settings and states the verdict.
  //
  // render/bundle/renderer.go draws scene/geom with CullBack plus FrontFaceCCW,
  // and render/gpu/jsgpu/encode.go maps that pair to WebGPU cullMode "back" plus
  // frontFace "ccw" with no inversion, so the winding below is the winding that
  // pair expects.
  //
  // Only the vertex order inside each triangle changed. Every vertex, the
  // triangle count and the triangle order stay the same, so a pick still reports
  // the same triangle index and every raycast test keeps its answer.
  //
  // 12-scene-geometry-winding.test.mjs measures the dot product per generator and
  // fails on a reversed face. It also builds one shape through both browser paths
  // and compares the two signs directly.
  function boxTriangleMesh(object) {
    const vertices = boxVertices(object.width, object.height, object.depth);
    const out = scenePrimitiveMeshBuilder();
    const uv0 = { x: 0, y: 0 };
    const uv1 = { x: 1, y: 0 };
    const uv2 = { x: 1, y: 1 };
    const uv3 = { x: 0, y: 1 };
    const faces = [
      { normal: { x: 0, y: 0, z: -1 }, indices: [0, 1, 2, 3] },
      { normal: { x: 0, y: 0, z: 1 }, indices: [5, 4, 7, 6] },
      { normal: { x: -1, y: 0, z: 0 }, indices: [4, 0, 3, 7] },
      { normal: { x: 1, y: 0, z: 0 }, indices: [1, 5, 6, 2] },
      { normal: { x: 0, y: 1, z: 0 }, indices: [3, 2, 6, 7] },
      { normal: { x: 0, y: -1, z: 0 }, indices: [4, 5, 1, 0] },
    ];
    for (let i = 0; i < faces.length; i += 1) {
      const face = faces[i];
      const a = vertices[face.indices[0]];
      const b = vertices[face.indices[1]];
      const c = vertices[face.indices[2]];
      const d = vertices[face.indices[3]];
      // Each face lists its four corners clockwise about its own outward normal,
      // so the quad fan runs a, c, b and a, d, c. Each UV travels with its corner.
      scenePushMeshTriangle(out, a, c, b, face.normal, uv0, uv2, uv1);
      scenePushMeshTriangle(out, a, d, c, face.normal, uv0, uv3, uv2);
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  function planeTriangleMesh(object) {
    // Take the four corners of the y-plane, not the first four boxVertices.
    // boxVertices lists the -z face first (indices 0..3), so slice(0, 4) with
    // height 0 gave four points that all share z = -depth/2: a zero-area strip
    // instead of a plane. Indices 0, 1, 5 and 4 are the corners that span x
    // and z.
    //
    // The four corners run clockwise about the +y normal, so the fan runs 0, 2, 1
    // and 0, 3, 2. That winds both triangles counter-clockwise seen from above,
    // which is where the +y normal points. generateInstancedPlaneGeometry in
    // 16c-scene-shared-pbr.js measures +1.000000 for the same quad.
    const vertices = planeQuadVertices(object.width, object.depth);
    const out = scenePrimitiveMeshBuilder();
    const normal = { x: 0, y: 1, z: 0 };
    scenePushMeshTriangle(out, vertices[0], vertices[2], vertices[1], normal, { x: 0, y: 1 }, { x: 1, y: 0 }, { x: 1, y: 1 });
    scenePushMeshTriangle(out, vertices[0], vertices[3], vertices[2], normal, { x: 0, y: 1 }, { x: 0, y: 0 }, { x: 1, y: 0 });
    return sceneFinalizePrimitiveMesh(out);
  }

  function sphereTriangleMesh(object) {
    const radius = scenePositiveNumber(object && object.radius, 0.5);
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 6, 128);
    const rings = Math.max(3, Math.floor(segments / 2));
    const out = scenePrimitiveMeshBuilder();
    function point(lat, lon) {
      const theta = Math.PI * lat / rings;
      const phi = Math.PI * 2 * lon / segments;
      const sinTheta = Math.sin(theta);
      const normal = {
        x: Math.cos(phi) * sinTheta,
        y: Math.cos(theta),
        z: Math.sin(phi) * sinTheta,
      };
      return {
        position: { x: normal.x * radius, y: normal.y * radius, z: normal.z * radius },
        normal,
        uv: { x: lon / segments, y: lat / rings },
      };
    }
    for (let lat = 0; lat < rings; lat += 1) {
      for (let lon = 0; lon < segments; lon += 1) {
        const nextLon = lon + 1;
        const a = point(lat, lon);
        const b = point(lat + 1, lon);
        const c = point(lat + 1, nextLon);
        const d = point(lat, nextLon);
        // a and d sit on ring lat, b and c sit on ring lat + 1. Latitude grows
        // downward from the north pole, so a, d, b and d, c, b wind
        // counter-clockwise seen from outside the ball. The top and the bottom row
        // each drop one triangle, because a pole quad collapses to a sliver.
        if (lat > 0) {
          scenePushMeshVertex(out, a.position, a.normal, a.uv);
          scenePushMeshVertex(out, d.position, d.normal, d.uv);
          scenePushMeshVertex(out, b.position, b.normal, b.uv);
        }
        if (lat < rings - 1) {
          scenePushMeshVertex(out, d.position, d.normal, d.uv);
          scenePushMeshVertex(out, c.position, c.normal, c.uv);
          scenePushMeshVertex(out, b.position, b.normal, b.uv);
        }
      }
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  function torusTriangleMesh(object) {
    const radialSegments = scenePrimitiveSegmentResolution(object && object.radialSegments, 32, 3, 128);
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 16, 3, 64);
    const radius = scenePositiveNumber(object && object.radius, 0.7);
    const tube = scenePositiveNumber(object && object.tube, 0.3);
    const out = scenePrimitiveMeshBuilder();
    function point(i, j) {
      const u = Math.PI * 2 * i / radialSegments;
      const v = Math.PI * 2 * j / tubularSegments;
      const cu = Math.cos(u);
      const su = Math.sin(u);
      const cv = Math.cos(v);
      const sv = Math.sin(v);
      const r = radius + tube * cv;
      const normal = { x: cu * cv, y: sv, z: su * cv };
      return {
        position: { x: r * cu, y: tube * sv, z: r * su },
        normal,
        uv: { x: i / radialSegments, y: j / tubularSegments },
      };
    }
    for (let i = 0; i < radialSegments; i += 1) {
      for (let j = 0; j < tubularSegments; j += 1) {
        const a = point(i, j);
        const b = point(i + 1, j);
        const c = point(i + 1, j + 1);
        const d = point(i, j + 1);
        // i sweeps the major ring and j sweeps the tube cross-section, so the quad
        // a, b, c, d reads clockwise from outside the tube. Fan it a, c, b and
        // a, d, c to wind both triangles with the outward normals.
        // generateInstancedTorusGeometry in 16c-scene-shared-pbr.js measures
        // +0.997526 for the same default torus.
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
        scenePushMeshVertex(out, b.position, b.normal, b.uv);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, d.position, d.normal, d.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
      }
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  // Torus-knot mesh generator — (p=2, q=3) trefoil knot.
  //
  // Parameter conventions (match THREE.js TorusKnotGeometry, opposite of GoSX torus):
  //   tubularSegments — steps along the knot PATH (default 128; page.gsx uses 64)
  //   radialSegments  — steps around the tube CROSS-SECTION (default 16; page.gsx uses 8)
  //
  // Local-space orientation: primary loop in XY plane, Z oscillation.
  // The scene's rotationX={π/2} maps local→world via (x, -z, y), yielding the
  // world-space layout the water shader's analytic SDF uses:
  //   SDF: C = (rad·cos(2θ), −r·sin(3θ)/2, rad·sin(2θ))  [world, XZ-primary]
  //   local: C = (rad·cos(2θ), rad·sin(2θ), r·sin(3θ)/2) [XY-primary]
  //   After rotX(π/2): world.x=local.x ✓  world.y=−local.z ✓  world.z=local.y ✓
  function torusKnotTriangleMesh(object) {
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 128, 8, 512);
    const radialSegments = scenePrimitiveSegmentResolution(object && object.radialSegments, 16, 3, 64);
    const radius = scenePositiveNumber(object && object.radius, 0.17);
    const tube = scenePositiveNumber(object && object.tube, 0.045);
    const p = 2, q = 3;
    function knotCurve(theta) {
      const rad = radius * (2.0 + Math.cos(q * theta)) * 0.5;
      return { x: rad * Math.cos(p * theta), y: rad * Math.sin(p * theta), z: radius * Math.sin(q * theta) * 0.5 };
    }
    function knotTangent(theta) {
      const h = 0.0001;
      const a = knotCurve(theta - h), b = knotCurve(theta + h);
      const dx = b.x - a.x, dy = b.y - a.y, dz = b.z - a.z;
      const len = Math.sqrt(dx * dx + dy * dy + dz * dz) || 1;
      return { x: dx / len, y: dy / len, z: dz / len };
    }
    // Build rotation-minimizing frames (parallel transport) to orient the tube
    // cross-sections stably without Frenet-frame flipping.
    const C_arr = [], T_arr = [], N_arr = [], B_arr = [];
    {
      const t0 = knotTangent(0);
      // Initial normal: axis least-parallel to T₀
      const ax = Math.abs(t0.x), ay = Math.abs(t0.y), az = Math.abs(t0.z);
      let n0;
      if (ax <= ay && ax <= az)        { n0 = { x: 0, y: -t0.z, z: t0.y }; }
      else if (ay <= az)               { n0 = { x: -t0.z, y: 0, z: t0.x }; }
      else                             { n0 = { x: -t0.y, y: t0.x, z: 0 }; }
      const nl = Math.sqrt(n0.x * n0.x + n0.y * n0.y + n0.z * n0.z) || 1;
      n0 = { x: n0.x / nl, y: n0.y / nl, z: n0.z / nl };
      const b0 = { x: t0.y * n0.z - t0.z * n0.y, y: t0.z * n0.x - t0.x * n0.z, z: t0.x * n0.y - t0.y * n0.x };
      C_arr.push(knotCurve(0)); T_arr.push(t0); N_arr.push(n0); B_arr.push(b0);
    }
    for (let i = 1; i <= tubularSegments; i++) {
      const theta = (Math.PI * 2 * i) / tubularSegments;
      const t = knotTangent(theta);
      const pn = N_arr[i - 1];
      const dot = pn.x * t.x + pn.y * t.y + pn.z * t.z;
      let nx = pn.x - dot * t.x, ny = pn.y - dot * t.y, nz = pn.z - dot * t.z;
      const nl = Math.sqrt(nx * nx + ny * ny + nz * nz) || 1;
      nx /= nl; ny /= nl; nz /= nl;
      const bx = t.y * nz - t.z * ny, by = t.z * nx - t.x * nz, bz = t.x * ny - t.y * nx;
      C_arr.push(knotCurve(theta));
      T_arr.push(t);
      N_arr.push({ x: nx, y: ny, z: nz });
      B_arr.push({ x: bx, y: by, z: bz });
    }
    // Seam correction: measure angular gap between frame[N] and frame[0],
    // distribute it linearly so the tube closes without a twist seam.
    {
      const nEnd = N_arr[tubularSegments], n0 = N_arr[0], tEnd = T_arr[tubularSegments];
      const dot = nEnd.x * n0.x + nEnd.y * n0.y + nEnd.z * n0.z;
      const cx = nEnd.y * n0.z - nEnd.z * n0.y;
      const cy = nEnd.z * n0.x - nEnd.x * n0.z;
      const cz = nEnd.x * n0.y - nEnd.y * n0.x;
      const sinA = cx * tEnd.x + cy * tEnd.y + cz * tEnd.z;
      const totalAngle = Math.atan2(sinA, dot);
      for (let i = 1; i <= tubularSegments; i++) {
        const angle = totalAngle * i / tubularSegments;
        const cos = Math.cos(angle), sin = Math.sin(angle);
        const n = N_arr[i], b = B_arr[i];
        N_arr[i] = { x: cos * n.x + sin * b.x, y: cos * n.y + sin * b.y, z: cos * n.z + sin * b.z };
        B_arr[i] = { x: cos * b.x - sin * n.x, y: cos * b.y - sin * n.y, z: cos * b.z - sin * n.z };
      }
    }
    function knotVertex(iSeg, jRad) {
      const phi = Math.PI * 2 * jRad / radialSegments;
      const cp = Math.cos(phi), sp = Math.sin(phi);
      const n = N_arr[iSeg], b = B_arr[iSeg], c = C_arr[iSeg];
      const nx = cp * n.x + sp * b.x, ny = cp * n.y + sp * b.y, nz = cp * n.z + sp * b.z;
      return {
        position: { x: c.x + tube * nx, y: c.y + tube * ny, z: c.z + tube * nz },
        normal: { x: nx, y: ny, z: nz },
        uv: { x: iSeg / tubularSegments, y: jRad / radialSegments },
      };
    }
    const out = scenePrimitiveMeshBuilder();
    // Wind each quad counter-clockwise as seen from outside the tube, so the
    // geometric normal of every triangle agrees with the outward normals its own
    // three vertices carry.
    //
    // The old order (a, b, c) and (a, c, d) opposed those normals at a dot
    // product of -0.998. Four permissive defaults hid it: the WebGL main pass
    // calls gl.disable(gl.CULL_FACE), the WebGPU pipeline sets cullMode "none",
    // sceneRayIntersectsTriangle accepts both faces, and the native renderer
    // skipped the shape. The native renderer culls back faces with a
    // counter-clockwise front face, so it needs this order to draw the near wall
    // of the tube instead of the far one.
    //
    // buildTorusKnot in scene/geom/primitives.go now emits the same two
    // triangles in the same order. Only the vertex order inside each triangle
    // changed here, so the triangle count, the triangle order and every vertex
    // stay the same and a pick still reports the same triangle index.
    for (let i = 0; i < tubularSegments; i++) {
      for (let j = 0; j < radialSegments; j++) {
        const a = knotVertex(i, j);
        const b = knotVertex(i + 1, j);
        const c = knotVertex(i + 1, j + 1);
        const d = knotVertex(i, j + 1);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
        scenePushMeshVertex(out, b.position, b.normal, b.uv);
        scenePushMeshVertex(out, a.position, a.normal, a.uv);
        scenePushMeshVertex(out, d.position, d.normal, d.uv);
        scenePushMeshVertex(out, c.position, c.normal, c.uv);
      }
    }
    return sceneFinalizePrimitiveMesh(out);
  }

  // sceneInstancedTriangleMesh borrows a solid mesh from the instanced
  // geometry generators in 16c-scene-shared-pbr.js. Both files sit in the same
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
      if (!from || !to) {
        continue;
      }
      out.push([from, to]);
    }
    return out;
  }

  function torusKnotSegments(object) {
    const tubularSegments = scenePrimitiveSegmentResolution(object && object.tubularSegments, 128, 8, 512);
    const radius = scenePositiveNumber(object && object.radius, 0.17);
    const p = 2, q = 3;
    function knotCurve(theta) {
      const rad = radius * (2.0 + Math.cos(q * theta)) * 0.5;
      return { x: rad * Math.cos(p * theta), y: rad * Math.sin(p * theta), z: radius * Math.sin(q * theta) * 0.5 };
    }
    const out = [];
    for (let i = 0; i < tubularSegments; i++) {
      const t0 = (Math.PI * 2 * i) / tubularSegments;
      const t1 = (Math.PI * 2 * (i + 1)) / tubularSegments;
      out.push([knotCurve(t0), knotCurve(t1)]);
    }
    return out;
  }

  function sceneObjectSegments(object) {
    switch (object.kind) {
      case "box":
      case "cube":
        return boxSegments(object);
      case "lines":
        return lineSegments(object);
      case "plane":
        return planeSegments(object);
      case "pyramid":
        return pyramidSegments(object);
      case "sphere":
        return sphereSegments(object);
      case "cylinder":
        return cylinderSegments(object);
      case "cone":
        return coneSegments(object);
      case "torus":
        return torusSegments(object);
      case "torusknot":
        return torusKnotSegments(object);
      default:
        return boxSegments(object);
    }
  }

  function scenePlaneLocalCorners(object) {
    return planeQuadVertices(
      sceneNumber(object && object.width, 1),
      sceneNumber(object && object.depth, sceneNumber(object && object.height, 1)),
    );
  }

  // Module-level scratch for scenePlaneSurfaceCorners. Four stable corner
  // objects wrapped in a stable array — the two callers in
  // 10-runtime-scene-core.js (appendSceneObjectToBundle bounds expansion
  // and appendSceneSurfaceToBundle positions serialization) consume the
  // returned corners immediately inside a for loop without retaining the
  // individual refs, so it's safe to share. Previously each call
  // allocated a 4-element array of fresh {x,y,z} objects through
  // translateScenePoint — 5 allocations per plane per frame.
  const _scenePlaneSurfaceCornersScratch = [
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
    { x: 0, y: 0, z: 0 },
  ];

  function scenePlaneSurfaceCorners(object, timeSeconds) {
    const local = scenePlaneLocalCorners(object);
    const out = _scenePlaneSurfaceCornersScratch;
    for (let i = 0; i < 4; i += 1) {
      const p = local[i];
      translateScenePointInto(out[i], p && p.x, p && p.y, p && p.z, object, timeSeconds);
    }
    return out;
  }

  function scenePlaneSurfacePositions(corners) {
    if (!Array.isArray(corners) || corners.length < 4) {
      return [];
    }
    return [
      corners[0].x, corners[0].y, corners[0].z,
      corners[1].x, corners[1].y, corners[1].z,
      corners[2].x, corners[2].y, corners[2].z,
      corners[0].x, corners[0].y, corners[0].z,
      corners[2].x, corners[2].y, corners[2].z,
      corners[3].x, corners[3].y, corners[3].z,
    ];
  }

  function scenePlaneSurfaceUVs() {
    return [
      0, 1,
      1, 1,
      1, 0,
      0, 1,
      1, 0,
      0, 0,
    ];
  }
```

### client/js/bootstrap-src/16c-scene-shared-pbr.ts

```typescript
  // Backend-agnostic Scene3D PBR helpers. This file stays in the base
  // scene3d chunk. 16-scene-webgl.js became a lazily fetched chunk
  // (bootstrap-feature-scene3d-webgl.js) because a WebGPU-capable browser
  // never runs it. These helpers are the part of the old WebGL file that the
  // base chunk and the WebGPU chunk still need, so they must stay eager.
  //
  // Three groups of callers depend on them:
  //   1. 15b-scene-planner.js sorts and classifies draw passes with
  //      scenePBRObjectRenderPass and scenePBRDepthSort.
  //   2. 10-runtime-scene-core.js and 20-scene-mount.js keep light and
  //      environment dirty hashes with hashLightContent and
  //      hashEnvironmentContent.
  //   3. 10-runtime-scene-core.js publishes the camera matrices, the shadow
  //      bounds and the instanced geometry generators on
  //      window.__gosx_scene3d_api. The WebGPU chunk reads them there through
  //      26e-feature-scene3d-webgpu-prefix.js.
  //
  // Every function here is pure math or plain object work. None of them
  // touches a WebGLRenderingContext. Keep it that way: a `gl.` call in this
  // file means the WebGL split leaked back into the eager path.
  //
  // The 37 declarations below are a closed dependency set. Adding a caller
  // here that reaches back into 16-scene-webgl.js breaks a WebGPU-only page
  // with a ReferenceError, which no test in this repo can catch without a
  // real GPU. Re-derive the closure before you move anything in or out.

  // Post-effect kind constants.
  var SCENE_POST_TONE_MAPPING = "toneMapping";

  var SCENE_POST_BLOOM = "bloom";

  var SCENE_POST_VIGNETTE = "vignette";

  var SCENE_POST_COLOR_GRADE = "colorGrade";

  var SCENE_POST_SSAO = "ssao";

  var SCENE_POST_DOF = "dof";

  var SCENE_POST_CUSTOM_POST = "customPost";

  var SCENE_POST_FXAA = "fxaa";

  // --- Camera matrices ---

  // Build a 4x4 view matrix from camera position and Euler rotation.
  //
  // The GoSX camera convention: the camera has position (x, y, z) and Euler
  // angles (rotationX, rotationY, rotationZ). The shared Scene3D contract
  // shifts world points by (-camX, -camY, -camZ) then applies inverse
  // rotation. Positive forward depth is -viewZ.
  //
  // To produce a standard 4x4 view matrix we construct:
  //   V = inverseRotation * translation(-camX, -camY, -camZ)
  //
  // The inverse rotation is computed by applying -rotZ, -rotY, -rotX
  // (reverse order, negative angles) — matching sceneInverseRotatePoint.
  // Build a 4x4 view matrix into `out` (or a new Float32Array if omitted).
  function scenePBRViewMatrix(camera, out) {
    const cam = sceneRenderCamera(camera);
    const tx = -cam.x;
    const ty = -cam.y;
    const tz = -cam.z;

    // Inverse Euler: apply -rotZ, then -rotY, then -rotX.
    const sx = Math.sin(-cam.rotationX);
    const cx = Math.cos(-cam.rotationX);
    const sy = Math.sin(-cam.rotationY);
    const cy = Math.cos(-cam.rotationY);
    const sz = Math.sin(-cam.rotationZ);
    const cz = Math.cos(-cam.rotationZ);

    // Rotation matrix = Rx(-rx) * Ry(-ry) * Rz(-rz), matching
    // sceneInverseRotatePoint's scalar sequence exactly.
    // Column-major order for WebGL.
    const r00 = cy * cz;
    const r01 = -cy * sz;
    const r02 = sy;

    const r10 = cx * sz + sx * sy * cz;
    const r11 = cx * cz - sx * sy * sz;
    const r12 = -sx * cy;

    const r20 = sx * sz - cx * sy * cz;
    const r21 = sx * cz + cx * sy * sz;
    const r22 = cx * cy;

    // Translation part: R * t
    const d0 = r00 * tx + r01 * ty + r02 * tz;
    const d1 = r10 * tx + r11 * ty + r12 * tz;
    const d2 = r20 * tx + r21 * ty + r22 * tz;

    // Column-major 4x4 matrix as Float32Array.
    var m = out || new Float32Array(16);
    m[0] = r00; m[1] = r10; m[2] = r20; m[3] = 0;
    m[4] = r01; m[5] = r11; m[6] = r21; m[7] = 0;
    m[8] = r02; m[9] = r12; m[10] = r22; m[11] = 0;
    m[12] = d0; m[13] = d1; m[14] = d2; m[15] = 1;
    return m;
  }

  // Build a perspective projection matrix into `out` (or a new Float32Array).
  // fov is in degrees, matching sceneRenderCamera output.
  function scenePBRProjectionMatrix(fov, aspect, near, far, out) {
    const fovRad = (fov * Math.PI) / 180;
    const f = 1 / Math.tan(fovRad * 0.5);
    const rangeInv = 1 / (near - far);

    // Column-major.
    var m = out || new Float32Array(16);
    m[0] = f / aspect; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = f; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = (near + far) * rangeInv; m[11] = -1;
    m[12] = 0; m[13] = 0; m[14] = 2 * near * far * rangeInv; m[15] = 0;
    return m;
  }

  function scenePBROrthographicProjectionMatrix(left, right, top, bottom, near, far, out) {
    var m = out || new Float32Array(16);
    const width = Math.max(0.000001, right - left);
    const height = Math.max(0.000001, top - bottom);
    const depth = Math.max(0.000001, far - near);
    m[0] = 2 / width; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = 2 / height; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = -2 / depth; m[11] = 0;
    m[12] = -(right + left) / width; m[13] = -(top + bottom) / height; m[14] = -(far + near) / depth; m[15] = 1;
    return m;
  }

  function scenePBRProjectionMatrixForCamera(camera, aspect, out) {
    const cam = sceneRenderCamera(camera);
    if (cam.kind === "orthographic") {
      const bounds = sceneOrthographicBounds(cam, Math.max(1, aspect * 1000), 1000);
      return scenePBROrthographicProjectionMatrix(bounds.left, bounds.right, bounds.top, bounds.bottom, cam.near, cam.far, out);
    }
    return scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, out);
  }

  // --- Shadow Map Infrastructure ---

  // Compute an orthographic light-space matrix for a directional light.
  // sceneBounds is { minX, minY, minZ, maxX, maxY, maxZ }.
  function sceneShadowLightSpaceMatrix(light, sceneBounds) {
    // Light direction (normalized).
    var dx = sceneNumber(light.directionX, 0);
    var dy = sceneNumber(light.directionY, -1);
    var dz = sceneNumber(light.directionZ, 0);
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) {
      dx = 0; dy = -1; dz = 0; len = 1;
    }
    dx /= len; dy /= len; dz /= len;

    // Scene center and radius from AABB.
    var cx = (sceneBounds.minX + sceneBounds.maxX) * 0.5;
    var cy = (sceneBounds.minY + sceneBounds.maxY) * 0.5;
    var cz = (sceneBounds.minZ + sceneBounds.maxZ) * 0.5;
    var ex = (sceneBounds.maxX - sceneBounds.minX) * 0.5;
    var ey = (sceneBounds.maxY - sceneBounds.minY) * 0.5;
    var ez = (sceneBounds.maxZ - sceneBounds.minZ) * 0.5;
    var radius = Math.sqrt(ex * ex + ey * ey + ez * ez);
    if (radius < 0.01) radius = 10;

    // Position the light camera behind the scene center along the light direction.
    var eyeX = cx - dx * radius * 2;
    var eyeY = cy - dy * radius * 2;
    var eyeZ = cz - dz * radius * 2;

    // Build a lookAt view matrix (light looking at scene center).
    // Forward = normalize(center - eye) = (dx, dy, dz).
    var fx = dx, fy = dy, fz = dz;

    // Choose an up vector not parallel to forward.
    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }

    // Right = normalize(forward x up).
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;

    // Recompute up = right x forward.
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    // View matrix (column-major).
    var tx = -(rx * eyeX + ry * eyeY + rz * eyeZ);
    var ty = -(upX * eyeX + upY * eyeY + upZ * eyeZ);
    var tz = -(fx * eyeX + fy * eyeY + fz * eyeZ);

    // Note: forward is positive — we look along +forward, so no negation.
    var view = new Float32Array([
      rx,  upX, fx,  0,
      ry,  upY, fy,  0,
      rz,  upZ, fz,  0,
      tx,  ty,  tz,  1,
    ]);

    // Orthographic projection matrix (column-major).
    // Maps [-radius, radius] in all axes to [-1, 1] clip space.
    var near = 0.01;
    var far = radius * 4;
    var l = -radius, rr = radius, b = -radius, t = radius;
    var proj = new Float32Array([
      2 / (rr - l),     0,              0,                    0,
      0,                2 / (t - b),    0,                    0,
      0,                0,              -2 / (far - near),    0,
      -(rr + l) / (rr - l), -(t + b) / (t - b), -(far + near) / (far - near), 1,
    ]);

    // Multiply proj * view (column-major).
    return sceneMat4Multiply(proj, view);
  }

  // Compute the AABB of all objects in the bundle.
  function sceneShadowComputeBounds(bundle) {
    var minX = Infinity, minY = Infinity, minZ = Infinity;
    var maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
    var positions = bundle.worldMeshPositions;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (obj.directVertices) continue;
      var offset = obj.vertexOffset;
      var count = obj.vertexCount;
      if (!Number.isFinite(offset) || !Number.isFinite(count) || count <= 0) continue;

      for (var v = 0; v < count; v++) {
        var idx = (offset + v) * 3;
        var px = positions[idx];
        var py = positions[idx + 1];
        var pz = positions[idx + 2];
        if (px < minX) minX = px;
        if (py < minY) minY = py;
        if (pz < minZ) minZ = pz;
        if (px > maxX) maxX = px;
        if (py > maxY) maxY = py;
        if (pz > maxZ) maxZ = pz;
      }
    }

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
      default:
        return text || "box";
    }
  }

  // Generate a unit box with the given dimensions. 36 vertices (12 triangles).
  // Each face has outward normals, [0,1] UVs, and MikkTSpace-compatible tangents.
  function generateInstancedBoxGeometry(w, h, d) {
    var hw = w * 0.5, hh = h * 0.5, hd = d * 0.5;

    // 6 faces × 2 triangles × 3 vertices = 36 vertices.
    // Each face: normal, tangent(vec4), 4 corners → 2 triangles.
    var faces = [
      // +Z face (front)
      { n: [0, 0, 1], t: [1, 0, 0, 1], v: [[-hw,-hh,hd],[hw,-hh,hd],[hw,hh,hd],[-hw,hh,hd]] },
      // -Z face (back)
      { n: [0, 0,-1], t: [-1, 0, 0, 1], v: [[hw,-hh,-hd],[-hw,-hh,-hd],[-hw,hh,-hd],[hw,hh,-hd]] },
      // +X face (right)
      { n: [1, 0, 0], t: [0, 0,-1, 1], v: [[hw,-hh,hd],[hw,-hh,-hd],[hw,hh,-hd],[hw,hh,hd]] },
      // -X face (left)
      { n: [-1, 0, 0], t: [0, 0, 1, 1], v: [[-hw,-hh,-hd],[-hw,-hh,hd],[-hw,hh,hd],[-hw,hh,-hd]] },
      // +Y face (top)
      { n: [0, 1, 0], t: [1, 0, 0, 1], v: [[-hw,hh,hd],[hw,hh,hd],[hw,hh,-hd],[-hw,hh,-hd]] },
      // -Y face (bottom)
      { n: [0,-1, 0], t: [1, 0, 0, 1], v: [[-hw,-hh,-hd],[hw,-hh,-hd],[hw,-hh,hd],[-hw,-hh,hd]] },
    ];

    var quadUVs = [[0,0],[1,0],[1,1],[0,1]];
    var triIndices = [0,1,2, 0,2,3];

    var vertexCount = 36;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var vi = 0;
    for (var fi = 0; fi < 6; fi++) {
      var face = faces[fi];
      for (var ti = 0; ti < 6; ti++) {
        var ci = triIndices[ti];
        var p = face.v[ci];
        positions[vi * 3]     = p[0];
        positions[vi * 3 + 1] = p[1];
        positions[vi * 3 + 2] = p[2];
        normals[vi * 3]     = face.n[0];
        normals[vi * 3 + 1] = face.n[1];
        normals[vi * 3 + 2] = face.n[2];
        uvs[vi * 2]     = quadUVs[ci][0];
        uvs[vi * 2 + 1] = quadUVs[ci][1];
        tangents[vi * 4]     = face.t[0];
        tangents[vi * 4 + 1] = face.t[1];
        tangents[vi * 4 + 2] = face.t[2];
        tangents[vi * 4 + 3] = face.t[3];
        vi++;
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a plane (quad) with the given width and depth, lying in the XZ plane.
  // 6 vertices (2 triangles), face normal pointing up (+Y).
  function generateInstancedPlaneGeometry(w, d) {
    var hw = w * 0.5, hd = d * 0.5;
    var vertexCount = 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var corners = [[-hw, 0, hd], [hw, 0, hd], [hw, 0, -hd], [-hw, 0, -hd]];
    var cornerUVs = [[0, 0], [1, 0], [1, 1], [0, 1]];
    var triIndices = [0, 1, 2, 0, 2, 3];

    for (var i = 0; i < 6; i++) {
      var ci = triIndices[i];
      var p = corners[ci];
      positions[i * 3] = p[0]; positions[i * 3 + 1] = p[1]; positions[i * 3 + 2] = p[2];
      normals[i * 3] = 0; normals[i * 3 + 1] = 1; normals[i * 3 + 2] = 0;
      uvs[i * 2] = cornerUVs[ci][0]; uvs[i * 2 + 1] = cornerUVs[ci][1];
      tangents[i * 4] = 1; tangents[i * 4 + 1] = 0; tangents[i * 4 + 2] = 0; tangents[i * 4 + 3] = 1;
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a UV sphere with the given radius and segment count.
  function generateInstancedSphereGeometry(radius, segments) {
    var slices = instancedSegmentCount(segments, 32, 3, 256);
    var rings = Math.max(2, Math.floor(slices / 2));

    // Count: each ring-slice quad = 2 triangles = 6 vertices,
    // except the top and bottom caps which are single triangles.
    var vertexCount = rings * slices * 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;

    function spherePoint(ring, slice) {
      var phi = (ring / rings) * Math.PI;
      var theta = (slice / slices) * Math.PI * 2;
      var sp = Math.sin(phi);
      var nx = sp * Math.cos(theta);
      var ny = Math.cos(phi);
      var nz = sp * Math.sin(theta);
      return {
        px: nx * radius, py: ny * radius, pz: nz * radius,
        nx: nx, ny: ny, nz: nz,
        u: slice / slices, v: ring / rings,
        tx: -Math.sin(theta), ty: 0, tz: Math.cos(theta),
      };
    }

    function pushVert(pt) {
      positions[vi * 3] = pt.px; positions[vi * 3 + 1] = pt.py; positions[vi * 3 + 2] = pt.pz;
      normals[vi * 3] = pt.nx; normals[vi * 3 + 1] = pt.ny; normals[vi * 3 + 2] = pt.nz;
      uvs[vi * 2] = pt.u; uvs[vi * 2 + 1] = pt.v;
      tangents[vi * 4] = pt.tx; tangents[vi * 4 + 1] = pt.ty; tangents[vi * 4 + 2] = pt.tz; tangents[vi * 4 + 3] = 1;
      vi++;
    }

    for (var r = 0; r < rings; r++) {
      for (var s = 0; s < slices; s++) {
        var a = spherePoint(r, s);
        var b = spherePoint(r, s + 1);
        var c = spherePoint(r + 1, s + 1);
        var dd = spherePoint(r + 1, s);
        pushVert(a); pushVert(b); pushVert(c);
        pushVert(a); pushVert(c); pushVert(dd);
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vi };
  }

  function instancedSegmentCount(value, fallback, minValue, maxValue) {
    var count = Math.round(sceneNumber(value, fallback));
    return Math.max(minValue, Math.min(maxValue, count));
  }

  function instancedPositiveNumber(value, fallback) {
    var number = sceneNumber(value, fallback);
    return number > 0 ? number : fallback;
  }

  function instancedNormalize3(x, y, z) {
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
    var minor = instancedPositiveNumber(tube, 0.3);
    var radial = instancedSegmentCount(radialSegments, 32, 3, 256);
    var tubular = instancedSegmentCount(tubularSegments, 16, 3, 128);
    var writer = createInstancedGeometryWriter(radial * tubular * 6);

    function vertexAt(i, j) {
      var u = (Math.PI * 2 * i) / radial;
      var v = (Math.PI * 2 * j) / tubular;
      var cu = Math.cos(u);
      var su = Math.sin(u);
      var cv = Math.cos(v);
      var sv = Math.sin(v);
      var r = major + minor * cv;
      return {
        position: [r * cu, minor * sv, r * su],
        normal: instancedNormalize3(cv * cu, sv, cv * su),
        uv: [i / radial, j / tubular],
        tangent: [-su, 0, cu, 1],
      };
    }

    function pushTorusVertex(v) {
      writer.push(v.position, v.normal, v.uv, v.tangent);
    }

    for (var i = 0; i < radial; i++) {
      for (var j = 0; j < tubular; j++) {
        var a = vertexAt(i, j);
        var b = vertexAt(i, j + 1);
        var c = vertexAt(i + 1, j);
        var dd = vertexAt(i + 1, j + 1);
        pushTorusVertex(a);
        pushTorusVertex(b);
        pushTorusVertex(c);
        pushTorusVertex(c);
        pushTorusVertex(b);
        pushTorusVertex(dd);
      }
    }
    return writer.build();
  }

  // Shared scratch for number → u32 bit reinterpretation used by the light
  // hash. Allocated once at module level; safe because the hash function
  // is called synchronously per upload and never recursively.
  var _scenePBRLightsHashBuf = new ArrayBuffer(4);

  var _scenePBRLightsHashFloat = new Float32Array(_scenePBRLightsHashBuf);

  var _scenePBRLightsHashInt = new Uint32Array(_scenePBRLightsHashBuf);

  function scenePBRLightsHashNumber(h, n) {
    _scenePBRLightsHashFloat[0] = (typeof n === "number" && n === n) ? n : 0;
    return Math.imul((h ^ _scenePBRLightsHashInt[0]) >>> 0, 16777619) >>> 0;
  }

  function scenePBRLightsHashString(h, s) {
    var str = (typeof s === "string") ? s : "";
    var len = str.length;
    for (var i = 0; i < len; i++) {
      h = Math.imul((h ^ str.charCodeAt(i)) >>> 0, 16777619) >>> 0;
    }
    // Length-delimit to distinguish "ab" + "c" from "a" + "bc".
    return Math.imul((h ^ (len + 1)) >>> 0, 16777619) >>> 0;
  }

  // hashLightContent computes the per-light sub-hash the frame-level
  // scenePBRLightsHash combines. Called from normalizeSceneLight (in
  // 10-runtime-scene-core.js) whenever a light is created or patched,
  // so the expensive string/number walk runs at mutation time — rare —
  // instead of per-frame. The result is stamped onto the light object
  // as `_lightHash` and read by scenePBRLightsHash without rehashing.
  //
  // Kept in 16-scene-webgl.js alongside scenePBRLightsHash so the two
  // must agree on what fields contribute to the hash; moving either
  // without the other is a correctness bug.
  function hashLightContent(l) {
    if (!l) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, l.kind);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.x, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.y, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.z, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionX, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionY, -1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionZ, 0));
    h = scenePBRLightsHashString(h, l.color);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.intensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.range, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.decay, 2));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.angle, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.penumbra, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.width, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.height, 0));
    h = scenePBRLightsHashString(h, l.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowBias, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSize, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowCascades, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSoftness, 0));
    return h;
  }

  // hashEnvironmentContent is the env-side counterpart to hashLightContent.
  // Called from normalizeSceneEnvironment and sceneResolveLightingEnvironment
  // whenever the environment is normalized so the cached sub-hash travels
  // with the environment object downstream.
  function hashEnvironmentContent(env) {
    if (!env) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, env.ambientColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.ambientIntensity, 0));
    h = scenePBRLightsHashString(h, env.skyColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.skyIntensity, 0));
    h = scenePBRLightsHashString(h, env.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.groundIntensity, 0));
    h = scenePBRLightsHashString(h, env.envMap);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envIntensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envRotation, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.fogDensity, 0));
    h = scenePBRLightsHashString(h, env.fogColor);
    return h;
  }

  // Determine the render pass for an object given its material.
  function scenePBRObjectRenderPass(obj, material) {
    if (obj && typeof obj.renderPass === "string" && obj.renderPass) {
      const pass = obj.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    if (material && typeof material.renderPass === "string" && material.renderPass) {
      const pass = material.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    // If material opacity < 1, default to alpha pass.
    if (material && sceneNumber(material.opacity, 1) < 1) {
      return "alpha";
    }
    return "opaque";
  }

  // Depth-based sort comparator for translucent objects (back-to-front).
  function scenePBRDepthSort(a, b) {
    const da = sceneNumber(a && a.depthCenter, 0);
    const db = sceneNumber(b && b.depthCenter, 0);
    if (da !== db) {
      return db - da;
    }
    return String(a && a.id || "").localeCompare(String(b && b.id || ""));
  }
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
			t.Fatalf("%s: triangle %d is wound against its own normals (dot %.4f); corners %v %v %v",
				label, triangle, dot, p0, p1, p2)
		}
	}
	if degenerate > allowedDegenerate {
		t.Fatalf("%s: %d degenerate triangles, want at most %d", label, degenerate, allowedDegenerate)
	}
}

// assertFiniteUnitNormals checks that every normal is finite and unit length.
func assertFiniteUnitNormals(t *testing.T, label string, m *Mesh) {
	t.Helper()
	for i := 0; i+3 <= len(m.Normals); i += 3 {
		x, y, z := m.Normals[i], m.Normals[i+1], m.Normals[i+2]
		length := math.Sqrt(x*x + y*y + z*z)
		if math.IsNaN(length) || math.IsInf(length, 0) || length < 0.99 || length > 1.01 {
			t.Fatalf("%s: normal %d has length %v, want unit", label, i/3, length)
		}
	}
	for i, v := range m.Positions {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s: position %d is %v", label, i, v)
		}
	}
	for i, v := range m.UVs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s: uv %d is %v", label, i, v)
		}
	}
}

// meshBounds returns the axis-aligned box that holds the mesh.
func meshBounds(m *Mesh) (lo, hi vec3) {
	lo = vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i+3 <= len(m.Positions); i += 3 {
		lo.X = math.Min(lo.X, m.Positions[i])
		lo.Y = math.Min(lo.Y, m.Positions[i+1])
		lo.Z = math.Min(lo.Z, m.Positions[i+2])
		hi.X = math.Max(hi.X, m.Positions[i])
		hi.Y = math.Max(hi.Y, m.Positions[i+1])
		hi.Z = math.Max(hi.Z, m.Positions[i+2])
	}
	return lo, hi
}

// TestBuildVertexCountsMatchTheDeclaredCounts pins every parametric generator to
// the count VertexCount promises. Memory reporting and the GPU upload both read
// that promise, so a drift understates or overstates real memory.
func TestBuildVertexCountsMatchTheDeclaredCounts(t *testing.T) {
	cases := []Params{
		{Kind: "cube"},
		{Kind: "box", Width: 4, Height: 2, Depth: 1},
		{Kind: "plane"},
		{Kind: "pyramid"},
		{Kind: "sphere"},
		{Kind: "sphere", Radius: 2, Segments: 12},
		{Kind: "cylinder"},
		{Kind: "cylinder", Segments: 7},
		{Kind: "cone"},
		{Kind: "torus"},
		{Kind: "torus", RadialSegments: 20, TubularSegments: 10},
		{Kind: "torusknot"},
		{Kind: "torusknot", RadialSegments: 8, TubularSegments: 32},
	}
	for _, params := range cases {
		mesh := Build(params, AllAttributes)
		if mesh == nil {
			t.Fatalf("%s: Build returned nil", params.Kind)
		}
		if got, want := mesh.VertexCount(), VertexCount(params); got != want {
			t.Fatalf("%s: vertex count %d, want the declared %d", params.Kind, got, want)
		}
		if got, want := len(mesh.Normals), mesh.VertexCount()*3; got != want {
			t.Fatalf("%s: normals length %d, want %d", params.Kind, got, want)
		}
		if got, want := len(mesh.UVs), mesh.VertexCount()*2; got != want {
			t.Fatalf("%s: uvs length %d, want %d", params.Kind, got, want)
		}
		if got, want := len(mesh.Colors), mesh.VertexCount()*3; got != want {
			t.Fatalf("%s: colors length %d, want %d", params.Kind, got, want)
		}
		assertFiniteUnitNormals(t, params.Kind, mesh)
	}
}

// TestDrawVertexCountMatchesTheUpload pins the count a GPU upload and a wire
// payload really carry. An indexed body must report its expanded size here, or a
// memory report understates it by the sharing factor.
func TestDrawVertexCountMatchesTheUpload(t *testing.T) {
	cases := []Params{
		{Kind: "cube"},
		{Kind: "plane"},
		{Kind: "sphere", Segments: 12},
		{Kind: "cylinder", Segments: 7},
		{Kind: "cone"},
		{Kind: "torus", RadialSegments: 20, TubularSegments: 10},
		{Kind: "torusknot"},
		{Kind: "torusknot", RadialSegments: 4, TubularSegments: 16},
	}
	for _, params := range cases {
		mesh := Build(params, AllAttributes)
		flat := mesh.Expanded()
		if got, want := flat.VertexCount(), DrawVertexCount(params); got != want {
			t.Fatalf("%s: the expanded mesh holds %d vertices, DrawVertexCount says %d",
				CacheKey(params), got, want)
		}
		if got, want := flat.TriangleCount(), DrawVertexCount(params)/3; got != want {
			t.Fatalf("%s: %d triangles, want %d", CacheKey(params), got, want)
		}
	}
	if got := DrawVertexCount(Params{Kind: "nosuchkind"}); got != 0 {
		t.Fatalf("DrawVertexCount of an unknown kind = %d, want 0", got)
	}
}

// TestBuildWindingAgreesWithNormals covers every parametric body.
func TestBuildWindingAgreesWithNormals(t *testing.T) {
	for _, kind := range []string{"cube", "box", "plane", "pyramid", "sphere", "cylinder", "cone", "torus", "torusknot"} {
		mesh := Build(Params{Kind: kind}, AllAttributes)
		if mesh == nil {
			t.Fatalf("%s: Build returned nil", kind)
		}
		// A UV sphere collapses its two pole rows into slivers, and a cone
		// collapses its apex row, so those rows carry degenerate triangles.
		allowed := 0
		switch kind {
		case "sphere":
			allowed = 64
		}
		assertWindingMatchesNormals(t, kind, mesh, allowed)
	}
}

// TestBuildBoundsStayInsideTheDeclaredRadius proves BoundingRadius never
// understates a body. A broad phase that understates drops real hits, and a cull
// that understates makes an object blink out while it is still on screen.
func TestBuildBoundsStayInsideTheDeclaredRadius(t *testing.T) {
	cases := []Params{
		{Kind: "cube"},
		{Kind: "cube", Size: 3},
		{Kind: "box", Width: 4, Height: 2, Depth: 1},
		{Kind: "plane", Width: 6, Height: 2},
		{Kind: "pyramid"},
		{Kind: "sphere", Radius: 2.5, Segments: 24},
		{Kind: "cylinder", RadiusTop: 0.5, RadiusBottom: 2, Height: 3},
		{Kind: "cone", RadiusBottom: 2, Height: 3},
		{Kind: "torus", Radius: 1.25, Tube: 0.25},
		{Kind: "torusknot", Radius: 1, Tube: 0.3},
	}
	for _, params := range cases {
		mesh := Build(params, AllAttributes)
		declared := BoundingRadius(params)
		if declared <= 0 {
			t.Fatalf("%s: BoundingRadius returned %v", params.Kind, declared)
		}
		widest := 0.0
		for i := 0; i+3 <= len(mesh.Positions); i += 3 {
			x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
			widest = math.Max(widest, math.Sqrt(x*x+y*y+z*z))
		}
		if widest > declared+1e-9 {
			t.Fatalf("%s: a vertex sits %v from the origin, past the declared radius %v",
				params.Kind, widest, declared)
		}
		// A bound far larger than the body would pass the check above and still
		// waste every cull test, so hold it close.
		if widest < declared*0.5 {
			t.Fatalf("%s: declared radius %v is more than twice the real extent %v",
				params.Kind, declared, widest)
		}
	}
}

// TestTorusKnotIsNamedByEverySpelling is the regression test for the defect this
// package was built to close.
//
// The native renderer used to know eight kinds and stop at "torus". A torusknot
// returned the empty kind, the cache key came back empty, the primitive came
// back nil, and the renderer dropped the draw with no diagnostic. The browser
// drew a knot; the desktop renderer and the headless PNG oracle drew nothing.
func TestTorusKnotIsNamedByEverySpelling(t *testing.T) {
	for _, spelling := range []string{"torusknot", "torusKnot", "TorusKnotGeometry", " TORUSKNOT ", "torus-knot"} {
		if got := NormalizeKind(spelling); got != KindTorusKnot {
			t.Fatalf("NormalizeKind(%q) = %q, want %q", spelling, got, KindTorusKnot)
		}
		if key := CacheKey(Params{Kind: spelling}); key == "" {
			t.Fatalf("CacheKey(%q) is empty, so a renderer would skip the draw", spelling)
		}
		mesh := Build(Params{Kind: spelling}, AllAttributes)
		if mesh == nil || mesh.TriangleCount() == 0 {
			t.Fatalf("Build(%q) produced no triangles", spelling)
		}
		if radius := BoundingRadius(Params{Kind: spelling}); radius <= 0 {
			t.Fatalf("BoundingRadius(%q) = %v", spelling, radius)
		}
	}
}

// TestUnknownKindIsReportedNotDropped proves an unknown name answers with a
// refusal a caller can see, instead of an empty mesh that looks like a draw.
func TestUnknownKindIsReportedNotDropped(t *testing.T) {
	for _, kind := range []string{"", "nosuchkind", "sphre"} {
		if got := NormalizeKind(kind); got != "" {
			t.Fatalf("NormalizeKind(%q) = %q, want the empty string", kind, got)
		}
		if got := CacheKey(Params{Kind: kind}); got != "" {
			t.Fatalf("CacheKey(%q) = %q, want the empty string", kind, got)
		}
		if got := Build(Params{Kind: kind}, AllAttributes); got != nil {
			t.Fatalf("Build(%q) returned a mesh, want nil", kind)
		}
		if got := VertexCount(Params{Kind: kind}); got != 0 {
			t.Fatalf("VertexCount(%q) = %d, want 0", kind, got)
		}
	}
}

// TestCacheKeysSeparateDifferentGeometry proves two different bodies never share
// one upload.
func TestCacheKeysSeparateDifferentGeometry(t *testing.T) {
	pairs := [][2]Params{
		{{Kind: "sphere", Radius: 1, Segments: 12}, {Kind: "sphere", Radius: 2, Segments: 12}},
		{{Kind: "sphere", Radius: 1, Segments: 12}, {Kind: "sphere", Radius: 1, Segments: 24}},
		{{Kind: "torusknot", Radius: 1}, {Kind: "torusknot", Radius: 2}},
		{{Kind: "torusknot", TubularSegments: 64}, {Kind: "torusknot", TubularSegments: 128}},
		{{Kind: "torus", Radius: 1}, {Kind: "torusknot", Radius: 1}},
		{{Kind: "cylinder", RadiusTop: 1}, {Kind: "cone", RadiusBottom: 1}},
	}
	for _, pair := range pairs {
		first, second := CacheKey(pair[0]), CacheKey(pair[1])
		if first == "" || second == "" {
			t.Fatalf("empty key for %v or %v", pair[0], pair[1])
		}
		if first == second {
			t.Fatalf("%v and %v share the key %q", pair[0], pair[1], first)
		}
	}
	// The same numbers must always give the same key, or the cache never hits.
	if CacheKey(Params{Kind: "sphere", Segments: 12}) != CacheKey(Params{Kind: "sphereGeometry", Segments: 12}) {
		t.Fatal("two spellings of one sphere produced different keys")
	}
}

// TestAttributeSelectionSkipsUnwantedStreams proves the positions-only path
// really costs nothing. The raycaster asks for it on every knot.
func TestAttributeSelectionSkipsUnwantedStreams(t *testing.T) {
	full := Build(Params{Kind: "torusknot"}, AllAttributes)
	lean := Build(Params{Kind: "torusknot"}, PositionsOnly)
	if len(lean.Normals) != 0 || len(lean.UVs) != 0 || len(lean.Colors) != 0 {
		t.Fatal("PositionsOnly filled a stream nobody asked for")
	}
	if len(lean.Positions) != len(full.Positions) {
		t.Fatalf("positions length %d, want %d", len(lean.Positions), len(full.Positions))
	}
	for i := range lean.Positions {
		if lean.Positions[i] != full.Positions[i] {
			t.Fatalf("position %d differs between the two attribute sets", i)
		}
	}
	if len(lean.Indices) != len(full.Indices) {
		t.Fatal("the two attribute sets produced different index counts")
	}
}

// TestExpandedMatchesTheIndexedMesh proves the renderer's flat upload draws the
// same triangles the picker tests.
func TestExpandedMatchesTheIndexedMesh(t *testing.T) {
	indexed := Build(Params{Kind: "torusknot", RadialSegments: 4, TubularSegments: 8}, AllAttributes)
	flat := indexed.Expanded()
	if flat.TriangleCount() != indexed.TriangleCount() {
		t.Fatalf("expanded triangle count %d, want %d", flat.TriangleCount(), indexed.TriangleCount())
	}
	if len(flat.Indices) != 0 {
		t.Fatal("the expanded mesh still carries indices")
	}
	for triangle := 0; triangle < indexed.TriangleCount(); triangle++ {
		want0, want1, want2 := meshTriangle(indexed, triangle)
		got0, got1, got2 := meshTriangle(flat, triangle)
		if want0 != got0 || want1 != got1 || want2 != got2 {
			t.Fatalf("triangle %d differs after expansion", triangle)
		}
	}
	// A non-indexed mesh must come back unchanged rather than being copied.
	plain := Build(Params{Kind: "cube"}, AllAttributes)
	if plain.Expanded() != plain {
		t.Fatal("Expanded copied a mesh that was already flat")
	}
}

// TestSphereVerticesSitOnTheSphere is a shape test that a count test cannot
// replace: a generator can emit the right number of wrong vertices.
func TestSphereVerticesSitOnTheSphere(t *testing.T) {
	const radius = 2.5
	mesh := Build(Params{Kind: "sphere", Radius: radius, Segments: 16}, AllAttributes)
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
		if got := math.Sqrt(x*x + y*y + z*z); math.Abs(got-radius) > 1e-9 {
			t.Fatalf("vertex %d sits %v from the origin, want %v", i/3, got, radius)
		}
		nx, ny, nz := mesh.Normals[i], mesh.Normals[i+1], mesh.Normals[i+2]
		if math.Abs(nx*radius-x) > 1e-9 || math.Abs(ny*radius-y) > 1e-9 || math.Abs(nz*radius-z) > 1e-9 {
			t.Fatalf("vertex %d carries a normal that does not point out of the origin", i/3)
		}
	}
	lo, hi := meshBounds(mesh)
	if math.Abs(lo.X+radius) > 1e-9 || math.Abs(hi.X-radius) > 1e-9 {
		t.Fatalf("bounds x = [%v, %v], want [%v, %v]", lo.X, hi.X, -radius, radius)
	}
}

// TestTorusKnotStaysOnItsTube proves the swept tube keeps its radius. A broken
// frame transport shows up as a vertex off the tube, which a count test misses.
func TestTorusKnotStaysOnItsTube(t *testing.T) {
	const (
		radius = 1.0
		tube   = 0.25
	)
	mesh := Build(Params{Kind: "torusknot", Radius: radius, Tube: tube, RadialSegments: 8, TubularSegments: 64}, PositionsOnly)
	// Every vertex sits one tube radius from the center curve, so its distance
	// from the origin stays between the curve's own reach minus and plus a tube.
	for i := 0; i+3 <= len(mesh.Positions); i += 3 {
		x, y, z := mesh.Positions[i], mesh.Positions[i+1], mesh.Positions[i+2]
		distance := math.Sqrt(x*x + y*y + z*z)
		if distance > radius*1.5+tube+1e-9 {
			t.Fatalf("vertex %d reaches %v, past the declared bound", i/3, distance)
		}
		if distance < radius*0.5-tube-1e-9 {
			t.Fatalf("vertex %d collapsed to %v from the origin", i/3, distance)
		}
	}
}
```

### render/bundle/primitive_native_test.go

```go
package bundle

import (
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestPrimitiveForKnownKinds(t *testing.T) {
	cases := []struct {
		kind        string
		wantVertexN int
	}{
		{"cube", 36},
		{"cubeGeometry", 36},
		{"box", 36},
		{"boxGeometry", 36},
		{"plane", 6},
		{"planeGeometry", 6},
		{"pyramid", 18},
		{"pyramidGeometry", 18},
		{"sphere", 32 * 16 * 6},
		{"sphereGeometry", 32 * 16 * 6},
		{"cylinder", 32 * 12},
		{"cylinderGeometry", 32 * 12},
		{"cone", 32 * 6},
		{"coneGeometry", 32 * 6},
		{"torus", 32 * 16 * 6},
		{"torusGeometry", 32 * 16 * 6},
	}

	for _, tc := range cases {
		geo := primitiveForKind(tc.kind)
		if geo == nil {
			t.Fatalf("%s: primitive should be non-nil", tc.kind)
		}
		if geo.vertexCount != tc.wantVertexN {
			t.Fatalf("%s: vertexCount %d, want %d", tc.kind, geo.vertexCount, tc.wantVertexN)
		}
		assertPrimitiveBuffers(t, tc.kind, geo)
	}

	if got := primitiveForKind("nosuchkind"); got != nil {
		t.Error("unknown kind should return nil")
	}
}

func assertPrimitiveBuffers(t *testing.T, kind string, geo *primitiveGeometry) {
	t.Helper()
	if geo.vertexCount == 0 {
		t.Fatalf("%s: vertexCount is 0", kind)
	}
	if len(geo.positions) != geo.vertexCount*3 {
		t.Fatalf("%s: positions len %d, want %d", kind, len(geo.positions), geo.vertexCount*3)
	}
	if len(geo.colors) != geo.vertexCount*3 {
		t.Fatalf("%s: colors len %d, want %d", kind, len(geo.colors), geo.vertexCount*3)
	}
	if len(geo.normals) != geo.vertexCount*3 {
		t.Fatalf("%s: normals len %d, want %d", kind, len(geo.normals), geo.vertexCount*3)
	}
	if len(geo.uvs) != geo.vertexCount*2 {
		t.Fatalf("%s: uvs len %d, want %d", kind, len(geo.uvs), geo.vertexCount*2)
	}

	for i, v := range geo.positions {
		if !isFinite32(v) {
			t.Fatalf("%s: non-finite position[%d]=%v", kind, i, v)
		}
	}
	for i, v := range geo.colors {
		if !isFinite32(v) || v < 0 || v > 1 {
			t.Fatalf("%s: invalid color[%d]=%v", kind, i, v)
		}
	}
	for i := 0; i < len(geo.normals); i += 3 {
		x, y, z := geo.normals[i], geo.normals[i+1], geo.normals[i+2]
		length := math.Sqrt(float64(x*x + y*y + z*z))
		if length < 0.99 || length > 1.01 {
			t.Fatalf("%s: normal %d length %f, want unit", kind, i/3, length)
		}
	}
	for i, v := range geo.uvs {
		if !isFinite32(v) {
			t.Fatalf("%s: non-finite uv[%d]=%v", kind, i, v)
		}
	}
}

func TestPrimitiveGenerationClampsSegments(t *testing.T) {
	for name, geo := range map[string]*primitiveGeometry{
		"sphere":   sphereGeometry(1, 0, 0),
		"cylinder": cylinderGeometry(1, 1, 2, 0),
		"cone":     cylinderGeometry(0, 1, 2, 0),
		"torus":    torusGeometry(1, 0.25, 0, 0),
	} {
		if geo == nil || geo.vertexCount == 0 {
			t.Fatalf("%s: expected generated geometry", name)
		}
		assertPrimitiveBuffers(t, name, geo)
	}
}

func TestPrimitiveParameterizedGeometry(t *testing.T) {
	sphere := primitiveForParams(primitiveParams{Kind: "sphere", Radius: 2, Segments: 12})
	if sphere == nil {
		t.Fatal("sphere: expected geometry")
	}
	if sphere.vertexCount != 12*6*6 {
		t.Fatalf("sphere vertexCount %d, want %d", sphere.vertexCount, 12*6*6)
	}
	assertPositionExtents(t, "sphere", sphere, [3]float32{-2, -2, -2}, [3]float32{2, 2, 2}, 0.02)

	box := primitiveForParams(primitiveParams{Kind: "box", Width: 4, Height: 2, Depth: 1})
	if box == nil {
		t.Fatal("box: expected geometry")
	}
	assertPositionExtents(t, "box", box, [3]float32{-2, -1, -0.5}, [3]float32{2, 1, 0.5}, 0)

	torus := primitiveForParams(primitiveParams{Kind: "torus", Radius: 1.25, Tube: 0.25, RadialSegments: 20, TubularSegments: 10})
	if torus == nil {
		t.Fatal("torus: expected geometry")
	}
	if torus.vertexCount != 20*10*6 {
		t.Fatalf("torus vertexCount %d, want %d", torus.vertexCount, 20*10*6)
	}

	a := primitiveCacheKey(primitiveParams{Kind: "sphere", Radius: 1, Segments: 12})
	b := primitiveCacheKey(primitiveParams{Kind: "sphere", Radius: 2, Segments: 12})
	if a == "" || b == "" || a == b {
		t.Fatalf("parameterized cache keys should be non-empty and distinct: %q %q", a, b)
	}
}

func TestPrimitiveCacheUsesGeometryParameters(t *testing.T) {
	d := newFakeDevice()
	r, err := New(Config{Device: d, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Destroy()

	before := len(d.buffers)
	first := engine.RenderInstancedMesh{Kind: "sphere", Radius: 1, Segments: 12}
	second := engine.RenderInstancedMesh{Kind: "sphere", Radius: 2, Segments: 12}
	if _, err := r.ensurePrimitiveForMesh(first); err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	if got := len(d.buffers) - before; got != 4 {
		t.Fatalf("first primitive upload created %d buffers, want 4", got)
	}
	if _, err := r.ensurePrimitiveForMesh(first); err != nil {
		t.Fatalf("ensure first again: %v", err)
	}
	if got := len(d.buffers) - before; got != 4 {
		t.Fatalf("same primitive parameters should reuse cache, created %d buffers", got)
	}
	if _, err := r.ensurePrimitiveForMesh(second); err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if got := len(d.buffers) - before; got != 8 {
		t.Fatalf("distinct primitive parameters should upload another 4 buffers, created %d", got)
	}
}

func assertPositionExtents(t *testing.T, kind string, geo *primitiveGeometry, wantMin, wantMax [3]float32, tolerance float64) {
	t.Helper()
	mins := [3]float32{geo.positions[0], geo.positions[1], geo.positions[2]}
	maxs := mins
	for i := 0; i < len(geo.positions); i += 3 {
		for axis := 0; axis < 3; axis++ {
			value := geo.positions[i+axis]
			if value < mins[axis] {
				mins[axis] = value
			}
			if value > maxs[axis] {
				maxs[axis] = value
			}
		}
	}
	for axis := 0; axis < 3; axis++ {
		if math.Abs(float64(mins[axis]-wantMin[axis])) > tolerance {
			t.Fatalf("%s min[%d]=%v, want %v", kind, axis, mins[axis], wantMin[axis])
		}
		if math.Abs(float64(maxs[axis]-wantMax[axis])) > tolerance {
			t.Fatalf("%s max[%d]=%v, want %v", kind, axis, maxs[axis], wantMax[axis])
		}
	}
}

func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}
```

### client/js/12-scene-geometry.test.mjs

```javascript
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const sourceDir = path.join(__dirname, "bootstrap-src");

function extractFunction(source, name) {
  const marker = "function " + name + "(";
  const start = source.indexOf(marker);
  assert.ok(start >= 0, name + " not found");
  let depth = 0;
  let sawBrace = false;
  for (let index = start; index < source.length; index += 1) {
    if (source[index] === "{") {
      depth += 1;
      sawBrace = true;
    } else if (source[index] === "}") {
      depth -= 1;
      if (sawBrace && depth === 0) {
        return source.slice(start, index + 1);
      }
    }
  }
  throw new Error("unbalanced function " + name);
}

function loadGeometry() {
  const geometrySource = fs.readFileSync(path.join(sourceDir, "12-scene-geometry.ts"), "utf8");
  const coreSource = fs.readFileSync(path.join(sourceDir, "10-runtime-scene-core.ts"), "utf8");
  const utilsSource = fs.readFileSync(path.join(sourceDir, "10-runtime-scene-utils.ts"), "utf8");
  const script = [
    "globalThis.__geometry = (function() {",
    extractFunction(utilsSource, "sceneNumber"),
    extractFunction(coreSource, "translateScenePointInto"),
    geometrySource,
    "return { boxVertices, planeQuadVertices, planeSegments, planeTriangleMesh,",
    "scenePrimitiveTriangleMesh, scenePlaneLocalCorners, scenePlaneSurfaceCorners,",
    "scenePlaneSurfacePositions, scenePlaneSurfaceUVs };",
    "})();",
  ].join("\n");
  const context = { console };
  vm.createContext(context);
  vm.runInContext(script, context);
  return context.__geometry;
}

const geometry = loadGeometry();

function triangleArea(a, b, c) {
  const ux = b.x - a.x;
  const uy = b.y - a.y;
  const uz = b.z - a.z;
  const vx = c.x - a.x;
  const vy = c.y - a.y;
  const vz = c.z - a.z;
  const cx = uy * vz - uz * vy;
  const cy = uz * vx - ux * vz;
  const cz = ux * vy - uy * vx;
  return Math.sqrt(cx * cx + cy * cy + cz * cz) / 2;
}

function distinctPointCount(points) {
  return new Set(points.map((point) => [point.x, point.y, point.z].join("|"))).size;
}

function flatTriangleArea(positions) {
  let area = 0;
  for (let index = 0; index < positions.length; index += 9) {
    area += triangleArea(
      { x: positions[index], y: positions[index + 1], z: positions[index + 2] },
      { x: positions[index + 3], y: positions[index + 4], z: positions[index + 5] },
      { x: positions[index + 6], y: positions[index + 7], z: positions[index + 8] },
    );
  }
  return area;
}

test("height-zero box prefix documents the plane degeneracy trap", () => {
  const prefix = geometry.boxVertices(2, 0, 2).slice(0, 4);
  assert.equal(distinctPointCount(prefix), 2);
  assert.equal(triangleArea(prefix[0], prefix[1], prefix[2]), 0);
});

test("planeQuadVertices walks four corners of the XZ ring", () => {
  const quad = geometry.planeQuadVertices(4, 2);
  assert.equal(distinctPointCount(quad), 4);
  assert.deepEqual(
    Array.from(quad, (point) => [point.x, point.y + 0, point.z]),
    [[-2, 0, -1], [2, 0, -1], [2, 0, 1], [-2, 0, 1]],
  );
});

test("plane wireframe and solid generators preserve requested area", () => {
  const segments = geometry.planeSegments({ width: 4, depth: 2 });
  assert.equal(segments.length, 4);
  let perimeter = 0;
  for (const [a, b] of segments) {
    const length = Math.hypot(b.x - a.x, b.y - a.y, b.z - a.z);
    assert.ok(length > 0);
    perimeter += length;
  }
  assert.equal(perimeter, 12);

  const direct = geometry.planeTriangleMesh({ width: 3, depth: 2 });
  assert.equal(direct.positions.length, 18);
  assert.ok(Math.abs(flatTriangleArea(direct.positions) - 6) < 1e-9);

  const dispatched = geometry.scenePrimitiveTriangleMesh({ kind: "plane", width: 5, depth: 4 });
  assert.ok(Math.abs(flatTriangleArea(dispatched.positions) - 20) < 1e-9);
});

test("HTML texture surface geometry remains nondegenerate before and after rotation", () => {
  const base = {
    width: 2.4,
    depth: 1.2,
    height: 0,
    x: 0,
    y: 0,
    z: 0,
    scaleX: 1,
    scaleY: 1,
    scaleZ: 1,
    rotationX: 0,
    rotationY: 0,
    rotationZ: 0,
    spinX: 0,
    spinY: 0,
    spinZ: 0,
    shiftX: 0,
    shiftY: 0,
    shiftZ: 0,
    driftSpeed: 0,
    driftPhase: 0,
  };
  const flat = geometry.scenePlaneSurfaceCorners(base, 0).map((point) => ({ ...point }));
  assert.equal(distinctPointCount(flat), 4);
  assert.ok(Math.abs(flatTriangleArea(geometry.scenePlaneSurfacePositions(flat)) - 2.88) < 1e-9);

  const upright = geometry.scenePlaneSurfaceCorners(
    { ...base, width: 2, depth: 1, rotationX: -Math.PI / 2 },
    0,
  ).map((point) => ({ ...point }));
  const spanY = Math.max(...upright.map((point) => point.y)) - Math.min(...upright.map((point) => point.y));
  const spanZ = Math.max(...upright.map((point) => point.z)) - Math.min(...upright.map((point) => point.z));
  assert.ok(Math.abs(spanY - 1) < 1e-9);
  assert.ok(spanZ < 1e-9);
  assert.deepEqual(Array.from(geometry.scenePlaneSurfaceUVs()), [0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0]);
});
```

### client/js/12-scene-geometry-winding.test.mjs

```javascript
// Winding gate for the browser solid-mesh generators in 12-scene-geometry.ts.
//
// The MAIN colour pass reads no winding: the WebGL main pass calls
// gl.disable(gl.CULL_FACE), the WebGPU PBR pipeline sets cullMode "none", and
// sceneRayIntersectsTriangle accepts both faces. Three permissive defaults
// therefore hide a reversed face in every colour image, and every existing test
// passes with the mesh inside out. The only property that can fail there is
// numeric: the geometric normal of each triangle must agree with the shaded
// normals its own three vertices carry.
//
// FOUR browser draw paths do read the winding, so a reversed face is not free.
// The WebGL shadow pass calls cullFace(gl.FRONT); the gosx-shadow and
// gosx-shadow-instanced WebGPU pipelines set cullMode "front"; and
// drawPBRObjects leaves a single-sided Selena mesh on cullMode "back". The
// three shadow sites keep the faces that point away from the light, so the
// winding below decides which surface a browser shadow map records.
// render/bundle/shadow_drift_test.go pins all three settings.
//
// This file is the JavaScript half of assertWindingMatchesNormals in
// scene/geom/geom_test.go. Both sides run the same formula on the same shapes, so
// a divergence fails on whichever side drifted. The native Go renderer culls back
// faces with a counter-clockwise front face, so it draws the far wall of a
// reversed tube — which is how the torus knot defect surfaced at all.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

// loadGeometryModule loads 12-scene-geometry.ts on its own, with
// generateInstancedGeometry stubbed out. Use it to measure a generator this file
// owns. Pass true to load 16c-scene-shared-pbr.ts first, which makes the real
// instanced generators reachable and lets one test compare the two paths.
function loadGeometryModule(withSharedPBR) {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    Float32Array,
    isFinite,
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  const context = vm.createContext(sandbox);
  // The helpers earlier bundle files declare. Copied from 10-runtime-scene-core.js
  // so the generators run exactly as they do in a page.
  vm.runInContext(
    `function sceneNumber(value, fallback) {
       var n = Number(value);
       return Number.isFinite(n) ? n : fallback;
     }
     function normalizeSceneKind(value) {
       return typeof value === "string" ? value.trim().toLowerCase() : "box";
     }
     function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
     ${withSharedPBR ? "" : "function generateInstancedGeometry() { return null; }"}`,
    context,
    { filename: "prelude.js" },
  );
  if (withSharedPBR) {
    vm.runInContext(fs.readFileSync(path.join(srcDir, "16c-scene-shared-pbr.ts"), "utf8"), context, {
      filename: "16c-scene-shared-pbr.ts",
    });
  }
  vm.runInContext(fs.readFileSync(path.join(srcDir, "12-scene-geometry.ts"), "utf8"), context, {
    filename: "12-scene-geometry.ts",
  });
  return context;
}

function buildMesh(context, generator, object) {
  return vm.runInContext(`${generator}(${JSON.stringify(object)})`, context);
}

// measureWinding returns the dot product between each triangle's geometric normal
// and the average of the shaded normals its own vertices carry. It mirrors
// assertWindingMatchesNormals in scene/geom/geom_test.go term for term.
function measureWinding(mesh) {
  const positions = mesh.positions;
  const normals = mesh.normals;
  let worst = Infinity;
  let total = 0;
  let triangles = 0;
  let degenerate = 0;
  let reversed = 0;
  for (let base = 0; base + 8 < positions.length; base += 9) {
    const e0 = [
      positions[base + 3] - positions[base],
      positions[base + 4] - positions[base + 1],
      positions[base + 5] - positions[base + 2],
    ];
    const e1 = [
      positions[base + 6] - positions[base],
      positions[base + 7] - positions[base + 1],
      positions[base + 8] - positions[base + 2],
    ];
    const geometric = [
      e0[1] * e1[2] - e0[2] * e1[1],
      e0[2] * e1[0] - e0[0] * e1[2],
      e0[0] * e1[1] - e0[1] * e1[0],
    ];
    const length = Math.hypot(geometric[0], geometric[1], geometric[2]);
    if (length < 1e-12) {
      degenerate += 1;
      continue;
    }
    let shaded = [0, 0, 0];
    for (let corner = 0; corner < 3; corner += 1) {
      shaded[0] += normals[base + corner * 3];
      shaded[1] += normals[base + corner * 3 + 1];
      shaded[2] += normals[base + corner * 3 + 2];
    }
    const shadedLength = Math.hypot(shaded[0], shaded[1], shaded[2]) || 1;
    const dot =
      (geometric[0] * shaded[0] + geometric[1] * shaded[1] + geometric[2] * shaded[2]) /
      (length * shadedLength);
    worst = Math.min(worst, dot);
    total += dot;
    triangles += 1;
    if (dot <= 0) {
      reversed += 1;
    }
  }
  return { triangles, degenerate, reversed, worst, mean: total / triangles };
}

// Every solid-mesh generator 12-scene-geometry.ts declares, with the parameters
// to build it and the figures the three producers now report.
//
// mean and worst are the measured dot products, rounded to six places. Go reports
// the same six places for the same parameters, because the formula is identical
// and the shapes are identical. The client stores positions as float32 and Go
// keeps float64, so each assertion allows 1e-4 — far below the 2.0 gap a sign
// flip opens.
//
// A negative mean here says the generator winds against its own normals.
const generatorCases = [
  {
    generator: "boxTriangleMesh",
    object: { width: 1, height: 1, depth: 1 },
    triangles: 12,
    degenerate: 0,
    mean: 1.0,
    worst: 1.0,
    // Was -1.000000 before the flip. Six flat quads, so the shaded normal of a
    // triangle equals its geometric normal exactly.
  },
  {
    generator: "planeTriangleMesh",
    object: { width: 2, depth: 3 },
    triangles: 2,
    degenerate: 0,
    mean: 1.0,
    worst: 1.0,
    // Was -1.000000 before the flip. One flat quad about the +y normal.
  },
  {
    generator: "sphereTriangleMesh",
    object: { radius: 1 },
    triangles: 960,
    degenerate: 0,
    mean: 0.99917,
    worst: 0.998844,
    // Was -0.999170 mean and -0.999529 worst before the flip. The generator drops
    // one triangle from the top row and one from the bottom row, so no pole quad
    // collapses and the degenerate count stays at zero. scene/geom keeps its pole
    // slivers and reports 64 degenerate triangles for the same shape, with the
    // same mean and the same worst.
  },
  {
    generator: "sphereTriangleMesh",
    object: { radius: 1, segments: 12 },
    triangles: 120,
    degenerate: 0,
    mean: 0.99369,
    worst: 0.989379,
    // Was -0.993690 mean before the flip. A coarse ball moves the worst dot
    // product, so pin a second resolution and catch a per-ring mistake.
  },
  {
    generator: "torusTriangleMesh",
    object: { radius: 0.7, tube: 0.3 },
    triangles: 1024,
    degenerate: 0,
    mean: 0.997526,
    worst: 0.997024,
    // Was -0.997526 mean and -0.998020 worst before the flip.
  },
  {
    generator: "torusKnotTriangleMesh",
    object: { radius: 0.17, tube: 0.045, tubularSegments: 64, radialSegments: 8 },
    triangles: 1024,
    degenerate: 0,
    mean: 0.991716,
    worst: 0.98749,
    // The torus knot was corrected first, because it also contradicted its own
    // stored normals at -0.998. buildTorusKnot in scene/geom/primitives.go reports
    // the same two figures.
  },
  {
    generator: "torusKnotTriangleMesh",
    object: {},
    triangles: 4096,
    degenerate: 0,
    mean: 0.998066,
    worst: 0.997281,
    // The default resolution, 128 path steps by 16 cross-section steps.
  },
];

test("every solid-mesh generator winds with its own stored normals", () => {
  const context = loadGeometryModule(false);
  for (const testCase of generatorCases) {
    const label = `${testCase.generator}(${JSON.stringify(testCase.object)})`;
    const mesh = buildMesh(context, testCase.generator, testCase.object);
    assert.ok(mesh, `${label} returned no mesh`);
    const measured = measureWinding(mesh);
    assert.equal(measured.triangles, testCase.triangles, `${label} triangle count`);
    assert.equal(measured.degenerate, testCase.degenerate, `${label} degenerate count`);
    assert.equal(
      measured.reversed,
      0,
      `${label}: ${measured.reversed} of ${measured.triangles} triangles oppose their own normals (worst dot ${measured.worst.toFixed(6)})`,
    );
    assert.ok(
      Math.abs(measured.mean - testCase.mean) < 1e-4,
      `${label} mean dot ${measured.mean.toFixed(6)}, want the recorded ${testCase.mean}`,
    );
    assert.ok(
      Math.abs(measured.worst - testCase.worst) < 1e-4,
      `${label} worst dot ${measured.worst.toFixed(6)}, want the recorded ${testCase.worst}`,
    );
  }
});

// The divergence is closed. box, plane, sphere and torus used to wind the other
// way in this file, at -1.000000, -1.000000, -0.999170 and -0.997526 against
// their own normals, while generateInstancedGeometry in 16c-scene-shared-pbr.ts
// and scene/geom in Go reported the same figures positive. One authored box
// therefore had opposite winding depending only on whether the renderer instanced
// it. This test now asserts the opposite of what it once pinned: no generator in
// 12-scene-geometry.ts may wind negatively.
//
// The test also refuses to pass when someone adds a generator and skips the
// table. It reads the function declarations out of the source and requires a case
// for each one, so a new shape cannot enter the file unmeasured.
//
// Two names stay out of the table on purpose:
//   - scenePrimitiveTriangleMesh only dispatches on object.kind;
//   - sceneInstancedTriangleMesh only forwards to 16c-scene-shared-pbr.ts, which
//     the cross-file test below measures directly.
//
// scenePlaneSurfacePositions stays out too. It emits a textured-surface quad with
// no normals, so this formula cannot read it. The test below it pins its sign.
test("no generator in 12-scene-geometry.ts winds against its own normals", () => {
  const source = fs.readFileSync(path.join(srcDir, "12-scene-geometry.ts"), "utf8");
  const declared = new Set(
    Array.from(source.matchAll(/function\s+(\w*TriangleMesh)\s*\(/g), (match) => match[1]),
  );
  declared.delete("scenePrimitiveTriangleMesh");
  declared.delete("sceneInstancedTriangleMesh");
  const covered = new Set(generatorCases.map((testCase) => testCase.generator));
  for (const name of declared) {
    assert.ok(covered.has(name), `${name} has no case in generatorCases; add one and record its dot product`);
  }
  for (const name of covered) {
    assert.ok(declared.has(name), `generatorCases names ${name}, which 12-scene-geometry.ts no longer declares`);
  }

  const context = loadGeometryModule(false);
  for (const testCase of generatorCases) {
    const measured = measureWinding(buildMesh(context, testCase.generator, testCase.object));
    assert.ok(
      measured.mean > 0,
      `${testCase.generator} mean dot ${measured.mean.toFixed(6)} is not positive; the two-convention split is back`,
    );
    // The mean cancels when a mutation reverses only half the triangles, so gate
    // on the reversed count and on the worst triangle as well. A mesh with one
    // reversed face out of a thousand still fails both of these.
    assert.equal(measured.reversed, 0, `${testCase.generator} has ${measured.reversed} reversed triangles`);
    assert.ok(
      measured.worst > 0,
      `${testCase.generator} worst dot ${measured.worst.toFixed(6)} is not positive`,
    );
  }
});

// scenePlaneSurfacePositions is the one triangle emitter in the file that stays
// unflipped. Three reasons hold it back:
//   - it writes positions only, so no stored normal exists to measure against;
//   - both callers are double-sided passes, which never cull either face;
//   - scenePlaneSurfacePositions in client/vm/scene_render_bundle.go is its Go
//     twin and emits the identical order, so the two already agree.
//
// Pin the sign anyway. Four corners listed counter-clockwise about +y give a
// geometric normal of -y here. A future change that enables culling on the
// surface passes must flip both halves together, not one.
test("scenePlaneSurfacePositions keeps its recorded triangle order", () => {
  const context = loadGeometryModule(false);
  const positions = vm.runInContext(
    "scenePlaneSurfacePositions([{x:-1,y:0,z:-1},{x:1,y:0,z:-1},{x:1,y:0,z:1},{x:-1,y:0,z:1}])",
    context,
  );
  assert.equal(positions.length, 18, "two triangles");
  for (let base = 0; base + 8 < positions.length; base += 9) {
    const e0 = [
      positions[base + 3] - positions[base],
      positions[base + 4] - positions[base + 1],
      positions[base + 5] - positions[base + 2],
    ];
    const e1 = [
      positions[base + 6] - positions[base],
      positions[base + 7] - positions[base + 1],
      positions[base + 8] - positions[base + 2],
    ];
    const geometricY = e0[2] * e1[0] - e0[0] * e1[2];
    assert.ok(
      geometricY < 0,
      `surface triangle ${base / 9} geometric normal y ${geometricY}, want the recorded negative value`,
    );
  }
});

// The invariant the two-convention split broke: one authored shape must wind the
// same way whichever browser path builds it.
//
// 12-scene-geometry.ts builds a non-instanced object. generateInstancedGeometry
// in 16c-scene-shared-pbr.ts builds an instanced one. A reader cannot infer this
// from two separate per-file tests, because each file can be self-consistent and
// still disagree with the other. That is exactly the state this closed. So build
// both here and compare the signs directly.
//
// The two files lay their triangles out differently, and that is allowed. The
// sphere is the clear case: this file drops the two pole triangles, while 16c
// keeps them as slivers and reports 24 degenerate triangles for a 12-segment
// ball. Compare the count of real triangles and the mean dot product, never the
// buffer contents.
//
// torusknot stays out of the list. generateInstancedGeometry has no torusknot
// case and returns a box, so a comparison there would measure a box against a
// knot. scene/geom/geom_test.go covers the Go torus knot, and the generator table
// above covers the browser one.
test("both browser geometry paths wind the same shape the same way", () => {
  const context = loadGeometryModule(true);
  const dims = { radius: 0.5, width: 1, height: 1, depth: 1, size: 1, tube: 0.3, segments: 12 };
  // Go reports these means for the same shapes. cylinder, cone and pyramid come
  // out of 16c through sceneInstancedTriangleMesh, so both browser figures are
  // one number by construction; Go builds them from its own code and lands on a
  // different segment count, so it is left out of the comparison for those three.
  const goMean = { box: 1.0, cube: 1.0, plane: 1.0, sphere: 0.99369, torus: 0.997526 };
  for (const kind of ["box", "cube", "plane", "sphere", "torus", "cylinder", "cone", "pyramid"]) {
    const object = Object.assign({ kind }, dims);
    const direct = vm.runInContext(`scenePrimitiveTriangleMesh(${JSON.stringify(object)})`, context);
    const instanced = vm.runInContext(
      `generateInstancedGeometry(${JSON.stringify(kind)}, ${JSON.stringify(dims)})`,
      context,
    );
    assert.ok(direct, `${kind}: 12-scene-geometry.ts built no mesh`);
    assert.ok(instanced, `${kind}: generateInstancedGeometry built no mesh`);

    const directMeasured = measureWinding(direct);
    const instancedMeasured = measureWinding(instanced);
    assert.ok(directMeasured.mean > 0, `${kind}: the non-instanced path winds at ${directMeasured.mean.toFixed(6)}`);
    assert.ok(instancedMeasured.mean > 0, `${kind}: the instanced path winds at ${instancedMeasured.mean.toFixed(6)}`);
    assert.equal(
      Math.sign(directMeasured.mean),
      Math.sign(instancedMeasured.mean),
      `${kind}: the two browser paths wind opposite ways (${directMeasured.mean.toFixed(6)} against ${instancedMeasured.mean.toFixed(6)})`,
    );
    assert.equal(
      directMeasured.triangles,
      instancedMeasured.triangles,
      `${kind}: the two paths build a different number of real triangles`,
    );
    assert.ok(
      Math.abs(directMeasured.mean - instancedMeasured.mean) < 1e-4,
      `${kind}: mean dot ${directMeasured.mean.toFixed(6)} against ${instancedMeasured.mean.toFixed(6)}`,
    );
    if (goMean[kind] !== undefined) {
      assert.ok(
        Math.abs(directMeasured.mean - goMean[kind]) < 1e-4,
        `${kind}: mean dot ${directMeasured.mean.toFixed(6)}, want the Go figure ${goMean[kind]}`,
      );
    }
  }
});
```

### scene/scene_test.go (lowering test pattern)

```go
func TestPropsSceneIRLowersCylinderAndTorusGeometry(t *testing.T) {
	props := Props{
		Graph: NewGraph(
			Mesh{
				ID: "cyl",
				Geometry: CylinderGeometry{
					RadiusTop:    0.5,
					RadiusBottom: 1.0,
					Height:       2.0,
					Segments:     16,
				},
				Material: FlatMaterial{Color: "#ff0000"},
				Position: Vec3(0, 0, 0),
			},
			Mesh{
				ID: "tor",
				Geometry: TorusGeometry{
					Radius:          1.5,
					Tube:            0.4,
					RadialSegments:  16,
					TubularSegments: 48,
				},
				Material: FlatMaterial{Color: "#00ff00"},
				Position: Vec3(3, 0, 0),
			},
		),
	}

	ir := props.SceneIR()
	if len(ir.Objects) != 2 {
		t.Fatalf("expected two lowered objects, got %d", len(ir.Objects))
	}

	cyl := ir.Objects[0]
	if cyl.Kind != "cylinder" {
		t.Fatalf("expected cylinder kind, got %q", cyl.Kind)
	}
	if cyl.RadiusTop != 0.5 {
		t.Fatalf("expected radiusTop 0.5, got %v", cyl.RadiusTop)
	}
	if cyl.RadiusBottom != 1.0 {
		t.Fatalf("expected radiusBottom 1.0, got %v", cyl.RadiusBottom)
	}
	if cyl.Height != 2.0 {
		t.Fatalf("expected height 2.0, got %v", cyl.Height)
	}
	if cyl.Segments != 16 {
		t.Fatalf("expected segments 16, got %v", cyl.Segments)
	}

	tor := ir.Objects[1]
	if tor.Kind != "torus" {
		t.Fatalf("expected torus kind, got %q", tor.Kind)
	}
	if tor.Radius != 1.5 {
		t.Fatalf("expected radius 1.5, got %v", tor.Radius)
	}
	if tor.Tube != 0.4 {
		t.Fatalf("expected tube 0.4, got %v", tor.Tube)
	}
	if tor.RadialSegments != 16 {
		t.Fatalf("expected radialSegments 16, got %v", tor.RadialSegments)
	}
	if tor.TubularSegments != 48 {
		t.Fatalf("expected tubularSegments 48, got %v", tor.TubularSegments)
	}

	legacy := props.LegacyProps()
	sceneValue, ok := legacy["scene"].(map[string]any)
	if !ok {
		t.Fatalf("expected scene map, got %#v", legacy["scene"])
	}
	objects, ok := sceneValue["objects"].([]map[string]any)
	if !ok || len(objects) != 2 {
		t.Fatalf("expected two objects in legacy props, got %#v", sceneValue["objects"])
	}
	if got := objects[0]["kind"]; got != "cylinder" {
		t.Fatalf("expected cylinder kind in legacy props, got %#v", got)
	}
	if got := objects[0]["radiusTop"]; got != 0.5 {
		t.Fatalf("expected radiusTop in legacy props, got %#v", got)
	}
	if got := objects[1]["kind"]; got != "torus" {
		t.Fatalf("expected torus kind in legacy props, got %#v", got)
	}
	if got := objects[1]["tube"]; got != 0.4 {
		t.Fatalf("expected tube in legacy props, got %#v", got)
	}
}

func TestPropsSceneIRLowersShadowFields(t *testing.T) {
```

### scene/raycast_test.go (analytic test pattern)

```go
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)}, PickableOnly())
	if trace.Closest != nil {
		t.Fatalf("expected non-pickable instances to be filtered, got %#v", trace.Closest)
	}
	if trace.FilteredPrimitives != 2 || trace.PrimitivesTested != 0 || trace.InstancesTested != 0 {
		t.Fatalf("unexpected filtered instance telemetry: %#v", trace)
	}
}

func TestTraceGraphReportsSortedHitsAndWork(t *testing.T) {
	graph := NewGraph(InstancedMesh{
		ID:        "stack",
		Count:     2,
		Geometry:  SphereGeometry{Radius: 0.5},
		Positions: []Vector3{Vec3(0, 0, -2), Vec3(0, 0, -5)},
	})
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 2), Direction: Vec3(0, 0, -1)})
	if trace.NodesVisited != 1 || trace.PrimitivesTested != 2 || trace.InstancesTested != 2 {
		t.Fatalf("unexpected traversal telemetry: %#v", trace)
	}
	if len(trace.Hits) != 2 || trace.Closest == nil || *trace.Closest.InstanceIndex != 0 {
		t.Fatalf("expected two sorted instance hits, got %#v", trace)
	}
	if trace.Hits[0].Distance >= trace.Hits[1].Distance {
		t.Fatalf("hits are not nearest-first: %#v", trace.Hits)
	}
}

func TestCylinderRaycastRejectsBoundingBoxCorner(t *testing.T) {
	graph := NewGraph(Mesh{
		ID:       "round-board",
		Geometry: CylinderGeometry{RadiusTop: 1, RadiusBottom: 1, Height: 0.5},
	})
	// This ray crosses the old AABB top face but is outside the circular cap.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.9, 2, 0.9), Direction: Vec3(0, -1, 0)}); ok {
		t.Fatalf("expected cylinder corner miss, got %#v", hit)
	}
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.5, 2, 0.5), Direction: Vec3(0, -1, 0)})
	if !ok || hit.Method != "analytic-frustum" || math.Abs(hit.Point.Y-0.25) > 1e-9 {
		t.Fatalf("expected exact cap hit, got %#v ok=%v", hit, ok)
	}
}

func TestCylinderRaycastPreservesZeroRadiusConeTip(t *testing.T) {
	graph := NewGraph(Mesh{ID: "cone", Geometry: CylinderGeometry{RadiusTop: 0, RadiusBottom: 1, Height: 1}})
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.2, 0.49, 2), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("ray above cone envelope should miss, got %#v", hit)
	}
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0.49, 2), Direction: Vec3(0, 0, -1)})
	if !ok || hit.Method != "analytic-frustum" {
		t.Fatalf("ray through cone axis should hit, got %#v ok=%v", hit, ok)
	}
}

func TestSceneIRPropagatesDeepHierarchyTransforms(t *testing.T) {
	var node Node = Mesh{
		ID:       "leaf",
		Geometry: CubeGeometry{Size: 1},
		Position: Vec3(1, 0, -3),
	}
	for i := 0; i < 1000; i++ {
		node = Group{
			Position: Vec3(0.001, 0, 0),
			Children: []Node{node},
		}
	}

	ir := NewGraph(node).SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.ID != "leaf" {
		t.Fatalf("object ID = %q, want leaf", obj.ID)
	}
	if math.Abs(obj.X-2) > 1e-9 || math.Abs(obj.Z+3) > 1e-9 {
		t.Fatalf("deep hierarchy world transform = (%v,%v,%v), want near (2,0,-3)", obj.X, obj.Y, obj.Z)
	}
}

func TestRaycastMeshHonorsLeafScale(t *testing.T) {
	// A unit box at origin misses a ray at x=1.5; scaled 2x it must hit.
	ray := Ray{Origin: Vector3{X: 1.5, Y: 0, Z: 6}, Direction: Vector3{Z: -1}}
	unit := Mesh{ID: "unit", Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2}}
	scaled := Mesh{ID: "scaled", Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2}, Scale: Vector3{X: 2, Y: 2, Z: 2}}
	if trace := TraceGraph(Graph{Nodes: []Node{unit}}, ray); trace.Closest != nil {
```

### scene/raycast_bvh_test.go (accelerator parity pattern)

```go
package scene

import (
	"math"
	"math/rand"
	"testing"
)

// parityGraph mixes every raycastable node kind and every geometry that owns an
// exact narrow phase, so the accelerator and the graph walk must agree over the
// full coverage surface.
func parityGraph(seed int64, count int) Graph {
	random := rand.New(rand.NewSource(seed))
	position := func() Vector3 {
		return Vec3(random.Float64()*20-10, random.Float64()*20-10, random.Float64()*20-10)
	}
	notPickable := false
	nodes := []Node{}
	for i := 0; i < count; i++ {
		switch i % 9 {
		case 6:
			// Torus and pyramid: exact quartic and exact triangles.
			nodes = append(nodes,
				Mesh{
					ID:       "ring",
					Geometry: TorusGeometry{Radius: 0.9, Tube: 0.3},
					Position: position(),
					Rotation: Euler{X: random.Float64(), Z: random.Float64()},
				},
				Mesh{
					ID:       "spire",
					Geometry: PyramidGeometry{Width: 1.2, Height: 1.8, Depth: 0.9},
					Position: position(),
					Rotation: Euler{Y: random.Float64()},
					Scale:    Vec3(1, 1+random.Float64(), 1),
				},
			)
			continue
		case 7:
			// A polyline picks through its stroke threshold, so it also exercises
			// the pick radius the accelerator pads its boxes with.
			nodes = append(nodes, Mesh{
				ID: "wire",
				Geometry: LinesGeometry{
					Points:   []Vector3{Vec3(-1, 0, 0), Vec3(1, 0.4, 0.2), Vec3(0.5, 1.2, -0.8), Vec3(-0.8, 0.3, 0.9)},
					Segments: [][2]int{{0, 1}, {1, 2}, {2, 3}},
				},
				Position: position(),
				Rotation: Euler{Y: random.Float64()},
				Scale:    Vec3(2, 2, 2),
			})
			continue
		case 8:
			// A tessellated knot and an authored triangle mesh. The accelerator
			// answers the triangle mesh through a per-geometry hierarchy and the walk
			// answers it in triangle order, so this pins that they agree.
			nodes = append(nodes,
				Mesh{
					ID:       "knot",
					Geometry: TorusKnotGeometry{Radius: 1.1, Tube: 0.25, RadialSegments: 6, TubularSegments: 24},
					Position: position(),
					Rotation: Euler{X: random.Float64(), Y: random.Float64()},
				},
				Mesh{
					ID:       "panel",
					Geometry: parityBuffer,
					Position: position(),
					Rotation: Euler{Z: random.Float64()},
				},
				Mesh{
					ID:       "deck",
					Geometry: parityPolygon,
					Position: position(),
				},
			)
			continue
		}
		switch i % 6 {
		case 0:
			nodes = append(nodes, Mesh{ID: "sphere", Geometry: SphereGeometry{Radius: 0.5 + random.Float64()}, Position: position()})
		case 1:
			nodes = append(nodes, Mesh{
				ID:       "box",
				Geometry: BoxGeometry{Width: 1, Height: 2, Depth: 0.5},
				Position: position(),
				Rotation: Euler{X: random.Float64(), Y: random.Float64(), Z: random.Float64()},
				Scale:    Vec3(1+random.Float64(), 1, 1),
			})
		case 2:
			nodes = append(nodes, Group{
				Position: position(),
				Rotation: Euler{Y: random.Float64()},
				Children: []Node{
					Mesh{ID: "child", Geometry: CylinderGeometry{RadiusTop: 0.4, RadiusBottom: 0.6, Height: 1.5}, Position: position()},
					Mesh{ID: "blocked", Geometry: CubeGeometry{Size: 1}, Position: position(), Pickable: &notPickable},
				},
			})
		case 3:
			positions := make([]Vector3, 8)
			for p := range positions {
				positions[p] = position()
			}
			nodes = append(nodes, Points{ID: "cloud", Count: len(positions), Positions: positions, Position: position()})
		case 4:
			instancePositions := make([]Vector3, 12)
			instanceScales := make([]Vector3, 12)
			instanceRotations := make([]Euler, 12)
			for p := range instancePositions {
				instancePositions[p] = position()
				instanceScales[p] = Vec3(0.5+random.Float64(), 0.5+random.Float64(), 0.5+random.Float64())
				instanceRotations[p] = Euler{Z: random.Float64()}
			}
			nodes = append(nodes, InstancedMesh{
				ID:        "instances",
				Count:     len(instancePositions),
				Geometry:  SphereGeometry{Radius: 0.4},
				Positions: instancePositions,
				Rotations: instanceRotations,
				Scales:    instanceScales,
			})
		default:
			nodes = append(nodes,
				Sprite{ID: "badge", Position: position(), Scale: 1 + random.Float64()},
				Decal{ID: "mark", Width: 2, Height: 2, Position: position(), Rotation: Euler{X: random.Float64()}},
				Model{ID: "prop", Src: "/prop.glb", Bounds: 1 + random.Float64(), Position: position()},
			)
		}
	}
	return Graph{Nodes: nodes}
}

// parityBuffer is a folded strip of authored triangles. Two of them share an
// edge, so a ray along that edge has to break its tie the same way in both paths.
var parityBuffer = BufferGeometry{
	Positions: []float64{
		-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0,
		-1, 1, 0, 1, 1, 0, 1, 1, -1.5, -1, 1, -1.5,
	},
	Indices: []int{0, 1, 2, 0, 2, 3, 4, 5, 6, 4, 6, 7},
}

// parityPolygon is an earcut triangulation with a hole, which is the case
// PolygonGeometry produces.
var parityPolygon = PolygonGeometry(
	[]float64{-1.5, -1.5, 1.5, -1.5, 1.5, 1.5, -1.5, 1.5},
	[][]float64{{-0.4, -0.4, -0.4, 0.4, 0.4, 0.4, 0.4, -0.4}},
	0,
)

func TestSceneAcceleratorMatchesGraphWalk(t *testing.T) {
	graph := parityGraph(7, 90)
	accel := NewSceneAccelerator(graph)
	if accel.PrimitiveCount() == 0 {
		t.Fatal("accelerator holds no primitives")
	}
	random := rand.New(rand.NewSource(11))
	seen := map[string]int{}
	for i := 0; i < 1500; i++ {
		ray := Ray{
			Origin:    Vec3(random.Float64()*40-20, random.Float64()*40-20, random.Float64()*40-20),
			Direction: Vec3(random.Float64()*2-1, random.Float64()*2-1, random.Float64()*2-1),
		}
		if normalizeVector(ray.Direction) == (Vector3{}) {
			continue
		}
		options := []RaycastOption{}
		if i%3 == 0 {
			options = append(options, PickableOnly())
		}
		if i%5 == 0 {
			options = append(options, MaxDistance(15))
		}
		want := TraceGraph(graph, ray, options...)
		got := accel.Trace(ray, options...)
		if len(want.Hits) != len(got.Hits) {
			t.Fatalf("ray %d: hit count = %d, want %d", i, len(got.Hits), len(want.Hits))
		}
		for h := range want.Hits {
			assertHitsEqual(t, i, h, got.Hits[h], want.Hits[h])
			seen[want.Hits[h].Kind+"/"+want.Hits[h].Method]++
		}
		if (want.Closest == nil) != (got.Closest == nil) {
			t.Fatalf("ray %d: closest presence mismatch", i)
		}
		if want.Closest != nil {
```

### docs/scene3d-native-webgpu-spec.md (primitive roadmap excerpt)

```markdown
- Compute pipelines for particle simulation, culling, skinning precompute, and future meshlet/LOD transforms.
- Post-FX pipelines for bloom, tonemap, FXAA, DOF, SSAO, color grade, vignette.
- Picking/object-ID pipeline or second color attachment.

### 6.3 Primitive vertex layout

Native generated mesh primitives use four buffers:

- `@location(0)` position: `float32x3`.
- `@location(1)` color: `float32x3`.
- `@location(2)` normal: `float32x3`.
- `@location(3)` uv: `float32x2`.

Instanced transforms use matrix columns through the existing instance-rate buffer. Future per-instance material/color overrides must use a dedicated instance attribute buffer or storage buffer, not mutate shared geometry buffers.

### 6.4 Native primitive catalog

This patch upgrades the WebGPU mesh primitive catalog from a partial set to the current built-in mesh primitive set plus compatibility aliases:

| Primitive | Aliases | Geometry | Normals | UVs | Notes |
|---|---|---:|---|---|---|
| Cube/Box | `cube`, `cubeGeometry`, `box`, `boxGeometry` | 36 verts | flat | face | unit envelope `[-1,1]` |
| Plane | `plane`, `planeGeometry`, `quad`, `quadGeometry` | 6 verts | flat +Y | quad | XZ plane at y=0 |
| Pyramid | `pyramid`, `pyramidGeometry` | 18 verts | flat | face | square base, apex at y=1 |
| Sphere | `sphere`, `sphereGeometry`, `uvSphere`, `uvSphereGeometry` | generated | smooth | lat/long | default 32x16 for native path |
| Cylinder | `cylinder`, `cylinderGeometry` | generated | smooth sides, flat caps | wrapped/caps | default 32 segments |
| Cone | `cone`, `coneGeometry` | generated | smooth sides, flat cap | wrapped/cap | compatibility alias via frustum generator |
| Torus | `torus`, `torusGeometry` | generated | smooth | parametric | default 32x16 |

The renderer must not silently skip any primitive emitted by Scene3D’s built-in mesh geometry path.

### 6.5 Future primitive catalog

The next primitives should be added only when their full path is specified:

- Capsule.
- Rounded box.
- Beveled text mesh.
- Extruded SVG/path mesh.
- Terrain grid.
- Volume cube/3D texture slice proxy.
- Meshlet-backed arbitrary geometry.
- Spline/tube curves.
- Decal projection mesh.
- Thick world lines and dashed world lines.

Each must include vertex layout, bounds, picking behavior, material compatibility, shadow behavior, LOD behavior, and fallback behavior.

## 7. HTML-in-canvas flows

### 7.1 DOM overlay mode
```
