package scene

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx/engine"
)

const DefaultEngineName = "GoSXScene3D"

var defaultCapabilities = []string{"canvas", "webgl", "animation"}

// Vector3 is a basic 3D point or direction.
type Vector3 struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// Euler stores XYZ rotation in radians.
type Euler struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// PerspectiveCamera describes the current Scene3D camera contract.
type PerspectiveCamera struct {
	Position     Vector3
	Rotation     Euler
	FOV          float64
	Near         float64
	Far          float64
	TransitionMS float64 // if > 0, client interpolates over this many milliseconds
}

// OrthographicCamera describes a non-perspective camera for CAD, editor, and
// 2D-in-3D views. Left/Right/Top/Bottom are world-space view bounds before
// Zoom. If all bounds are zero the runtime derives a 6-unit-tall view from the
// current viewport aspect ratio.
type OrthographicCamera struct {
	Position     Vector3
	Rotation     Euler
	Left         float64
	Right        float64
	Top          float64
	Bottom       float64
	Zoom         float64
	Near         float64
	Far          float64
	TransitionMS float64
}

// Environment describes scene-wide ambient, hemisphere, and image-based lighting.
type Environment struct {
	AmbientColor     string
	AmbientIntensity float64
	SkyColor         string
	SkyIntensity     float64
	GroundColor      string
	GroundIntensity  float64
	EnvironmentMap   string
	EnvIntensity     float64
	EnvRotation      float64
	Exposure         float64
	ToneMapping      string // "aces", "reinhard", "linear", "" (default = aces)
	FogColor         string
	FogDensity       float64 // for exponential fog (0 = no fog)
	Transition       Transition
	InState          *EnvironmentProps
	OutState         *EnvironmentProps
	Live             []string
}

// Props is the typed Go-side Scene3D surface. It lowers into the current
// Scene3D compatibility prop bag while preserving room for a real scene graph.
type Props struct {
	ProgramRef           string       `json:"-"`
	Capabilities         []string     `json:"-"`
	Width                int          `json:"width,omitempty"`
	Height               int          `json:"height,omitempty"`
	Label                string       `json:"label,omitempty"`
	AriaLabel            string       `json:"ariaLabel,omitempty"`
	Background           string       `json:"background,omitempty"`
	Controls             string       `json:"controls,omitempty"`
	AutoRotate           *bool        `json:"autoRotate,omitempty"`
	Responsive           *bool        `json:"responsive,omitempty"`
	FillHeight           *bool        `json:"fillHeight,omitempty"`
	PreferWebGL          *bool        `json:"preferWebGL,omitempty"`
	ForceWebGL           *bool        `json:"forceWebGL,omitempty"`
	RequireWebGL         *bool        `json:"requireWebGL,omitempty"`
	PreferCanvas         *bool        `json:"preferCanvas,omitempty"`
	UnsupportedMessage   string       `json:"unsupportedMessage,omitempty"`
	CanvasAlpha          *bool        `json:"canvasAlpha,omitempty"`
	DragToRotate         *bool        `json:"dragToRotate,omitempty"`
	DeferPostFX          *bool        `json:"deferPostFX,omitempty"`
	DeferPostFXDelayMS   int          `json:"deferPostFXDelayMS,omitempty"`
	DragSignalNamespace  string       `json:"dragSignalNamespace,omitempty"`
	PickSignalNamespace  string       `json:"pickSignalNamespace,omitempty"`
	EventSignalNamespace string       `json:"eventSignalNamespace,omitempty"`
	CapabilityTier       string       `json:"capabilityTier,omitempty"`
	Compression          *Compression `json:"compression,omitempty"`
	ControlTarget        Vector3
	ControlRotateSpeed   float64 `json:"controlRotateSpeed,omitempty"`
	ControlZoomSpeed     float64 `json:"controlZoomSpeed,omitempty"`
	ControlLookSpeed     float64 `json:"controlLookSpeed,omitempty"`
	ControlMoveSpeed     float64 `json:"controlMoveSpeed,omitempty"`
	ScrollCameraStart    float64 `json:"scrollCameraStart,omitempty"`
	ScrollCameraEnd      float64 `json:"scrollCameraEnd,omitempty"`
	MaxDevicePixelRatio  float64 `json:"maxDevicePixelRatio,omitempty"`
	Camera               PerspectiveCamera
	OrthographicCamera   *OrthographicCamera
	Stats                *bool `json:"stats,omitempty"`
	Environment          Environment
	PostFX               PostFX
	Shadows              Shadows
	Physics              PhysicsWorld
	Graph                Graph
}

// Compression configures TurboQuant compression for Scene3D vertex data.
// When non-nil with BitWidth > 0, IR lowering quantizes bulk float arrays.
type Compression struct {
	BitWidth int `json:"bitWidth"` // 1-8, bits per coordinate. 0 = no compression.

	// Progressive enables multi-resolution transport. When true, the scene
	// ships a fast preview at PreviewBitWidth (default 2) alongside the full
	// resolution at BitWidth. The client renders the preview immediately and
	// upgrades when the full data is ready.
	Progressive     bool `json:"progressive,omitempty"`
	PreviewBitWidth int  `json:"previewBitWidth,omitempty"` // default 2 if Progressive is true
	// ProgressiveDelayMS delays the full-quality client upgrade after first
	// paint. This lets the scene show a cheap preview, then spend upgrade CPU
	// during idle instead of competing with LCP and input setup.
	ProgressiveDelayMS int `json:"progressiveDelayMS,omitempty"`

	// LOD enables camera-distance-based level of detail. When true, the scene
	// ships both preview and full resolution, and the client selects which to
	// render based on each object's distance from the camera.
	LOD          bool    `json:"lod,omitempty"`
	LODThreshold float64 `json:"lodThreshold,omitempty"` // camera distance threshold; objects farther use preview. Default 20.
}

// ShadowMaxPixelsUnbounded opts out of the shadow map pixel cap entirely.
// Set Shadows.MaxPixels to this constant when you explicitly need the
// v0.14.0 behavior of allocating shadow maps at each light's requested
// shadowSize without a global cap.
//
// Value is 1<<30 (1,073,741,824) — effectively unbounded because the
// scaling factor clamps to 1.0 for any physically realistic shadow map.
const ShadowMaxPixelsUnbounded = 1 << 30

// Common Shadows.MaxPixels presets. Values correspond to the per-shadow-map
// pixel count (width * height), not total memory. Pick the one closest to
// the maximum shadow sharpness your scene needs.
const (
	ShadowMaxPixels512  = 512 * 512   //   262_144
	ShadowMaxPixels1024 = 1024 * 1024 // 1_048_576 (default)
	ShadowMaxPixels2048 = 2048 * 2048 // 4_194_304
	ShadowMaxPixels4096 = 4096 * 4096 // 16_777_216
)

// Shadows configures scene-wide shadow map allocation policy. Individual
// directional and spot lights still declare their own ShadowSize; this
// struct caps how many pixels each of those shadow maps may actually
// allocate.
type Shadows struct {
	// MaxPixels caps individual shadow maps at this many pixels
	// (width * height). When a light requests a shadow map bigger than
	// the cap, the pipeline scales it down uniformly. Below the cap,
	// the light's requested ShadowSize is honored as-is.
	//
	//   zero value (0): apply the safe default cap of 1024×1024
	//                   (1_048_576 pixels). Recommended for most scenes.
	//   positive:       explicit cap. Typically set via one of the
	//                   ShadowMaxPixels* constants.
	//   negative:       treated as zero (safe default). Not recommended;
	//                   use ShadowMaxPixelsUnbounded to opt out instead.
	//
	// Scaling formula (per shadow map):
	//     factor = min(1, sqrt(MaxPixels / (shadowSize * shadowSize)))
	//     scaledSize = max(1, floor(shadowSize * factor))
	//
	// Shadow maps are self-contained depth textures, so scaling only
	// affects shadow sharpness — no blit or upscale. A 4096-requesting
	// light with MaxPixels=1_048_576 gets a 1024 shadow map; memory
	// drops from 64 MB to 4 MB per light (depth24).
	MaxPixels int
}

// Graph is the typed scene graph lowered into the legacy Scene3D prop bag.
type Graph struct {
	Nodes []Node
}

// Node is a typed scene graph node.
type Node interface {
	sceneNode()
}

// Group applies a transform to descendant nodes.
type Group struct {
	ID       string
	Position Vector3
	Rotation Euler
	Children []Node
}

// Mesh lowers into one legacy scene object.
type Mesh struct {
	ID            string
	Geometry      Geometry
	Material      Material
	Position      Vector3
	Rotation      Euler
	Pickable      *bool
	Selected      bool
	OutlineColor  string
	OutlineWidth  float64
	CastShadow    bool
	ReceiveShadow bool
	DepthWrite    *bool // nil = default (true), false = no depth writes
	Spin          Euler
	Drift         Vector3
	DriftSpeed    float64
	DriftPhase    float64
	// RigidBody, when non-nil, attaches a physics body to this mesh. The
	// body's initial pose is taken from Position/Rotation above; after
	// simulation starts, the mesh's transform is driven by the physics
	// runner's broadcasts (keyed by Mesh.ID, which becomes the body ID).
	RigidBody  *RigidBody3D
	Transition Transition
	InState    *MeshProps
	OutState   *MeshProps
	Live       []string
	Children   []Node
}

// LODLevel describes one level inside a discrete LODGroup. Distance is the
// minimum camera distance at which Node becomes active; the next level's
// Distance becomes this level's maximum distance.
type LODLevel struct {
	Distance float64
	Node     Node
}

// LODGroup lowers conventional distance-threshold level-of-detail groups. It
// complements Compression.LOD: discrete groups swap authored meshes/models,
// while TurboQuant LOD swaps compressed vertex payload quality.
type LODGroup struct {
	ID       string
	Position Vector3
	Rotation Euler
	Levels   []LODLevel
}

// SpreadProps serializes a Mesh into the attribute map that the composable
// <Mesh {...props} /> element accepts. This lets typed Mesh values generated
// in Go flow into gsx templates via <Each> without round-tripping through
// the full scene IR.
func (m Mesh) SpreadProps() map[string]any {
	l := &graphLowerer{anchors: make(map[string]worldTransform)}
	l.lowerMesh(m, identityTransform())
	if len(l.objects) == 0 {
		return nil
	}
	return l.objects[0].legacyProps()
}

// Points renders a particle system using GL_POINTS.
type Points struct {
	ID          string
	Count       int       // number of particles
	Positions   []Vector3 // per-particle positions
	Sizes       []float64 // per-particle sizes (optional, default 1.0)
	Colors      []string  // per-particle hex colors (optional)
	Color       string    // uniform color if no per-vertex colors
	Style       PointStyle
	Size        float64 // uniform size if no per-vertex sizes
	Opacity     float64 // 0-1
	BlendMode   MaterialBlendMode
	DepthWrite  bool    // whether to write to depth buffer
	Attenuation bool    // size scales with distance
	Position    Vector3 // transform position
	Rotation    Euler   // transform rotation
	Spin        Euler   // procedural rotation animation
	Transition  Transition
	InState     *PointsProps
	OutState    *PointsProps
	Live        []string
}

// InstancedMesh renders N copies of one geometry with per-instance transforms.
type InstancedMesh struct {
	ID            string
	Count         int
	Geometry      Geometry
	Material      Material
	Positions     []Vector3
	Rotations     []Euler
	Scales        []Vector3
	Colors        []string
	Attributes    map[string][]float64
	CastShadow    bool
	ReceiveShadow bool
	Transition    Transition
	InState       *InstancedMeshProps
	OutState      *InstancedMeshProps
	Live          []string
}

// InstancedGLBMesh renders N copies of a GLB model via a single instanced draw
// call. Each instance has its own position, scale, and rotation; all instances
// share the same GLB source and optional material override.
//
// The JS runtime loads the GLB once (via the existing GLTF loader), extracts
// the geometry, and renders all instances using the instanced draw path — one
// draw call per geometry/material pair instead of one per instance.
type InstancedGLBMesh struct {
	ID        string
	Src       string // GLB file URL
	Material  Material
	Instances []MeshInstance
	Pickable  *bool
	Static    *bool
}

// MeshInstance describes the transform for a single instance within an
// InstancedGLBMesh batch.
type MeshInstance struct {
	ID       string
	Position Vector3
	Scale    Vector3
	Rotation Euler
}

// ComputeParticles declares a GPU-computed particle system.
type ComputeParticles struct {
	ID         string
	Count      int
	Emitter    ParticleEmitter
	Forces     []ParticleForce
	Material   ParticleMaterial
	Bounds     float64
	Transition Transition
	InState    *ComputeParticlesProps
	OutState   *ComputeParticlesProps
	Live       []string
}

type ParticleEmitter struct {
	Kind     string // "point", "sphere", "disc", "spiral"
	Position Vector3
	Rotation Euler
	Spin     Euler // procedural rotation (radians per second)
	Radius   float64
	Rate     float64
	Lifetime float64
	Arms     int
	Wind     float64
	Scatter  float64
}

type ParticleForce struct {
	Kind      string // "gravity", "wind", "turbulence", "orbit", "drag"
	Strength  float64
	Direction Vector3
	Frequency float64
}

type ParticleMaterial struct {
	Color       string
	ColorEnd    string
	Style       PointStyle
	Size        float64
	SizeEnd     float64
	Opacity     float64
	OpacityEnd  float64
	BlendMode   MaterialBlendMode
	Attenuation bool
}

// Label lowers into one legacy scene label.
type Label struct {
	ID          string
	Target      string
	Text        string
	ClassName   string
	Position    Vector3
	Priority    float64
	Shift       Vector3
	DriftSpeed  float64
	DriftPhase  float64
	MaxWidth    float64
	MaxLines    int
	Overflow    string
	Font        string
	LineHeight  float64
	Color       string
	Background  string
	BorderColor string
	OffsetX     float64
	OffsetY     float64
	AnchorX     float64
	AnchorY     float64
	Collision   string
	Occlude     bool
	WhiteSpace  string
	TextAlign   string
	Transition  Transition
	InState     *LabelProps
	OutState    *LabelProps
	Live        []string
}

// Sprite lowers into one projected image billboard overlay.
type Sprite struct {
	ID         string
	Target     string
	Src        string
	ClassName  string
	Position   Vector3
	Priority   float64
	Shift      Vector3
	DriftSpeed float64
	DriftPhase float64
	Width      float64
	Height     float64
	Scale      float64
	Opacity    float64
	OffsetX    float64
	OffsetY    float64
	AnchorX    float64
	AnchorY    float64
	Occlude    bool
	Fit        string
	Transition Transition
	InState    *SpriteProps
	OutState   *SpriteProps
	Live       []string
}

// Model instances a framework-owned scene model asset with a transform and
// optional material/static overrides.
type Model struct {
	ID         string
	Src        string
	Position   Vector3
	Rotation   Euler
	Scale      Vector3
	Material   Material
	Pickable   *bool
	Static     *bool
	Animation  string
	Loop       *bool
	Transition Transition
	InState    *ModelProps
	OutState   *ModelProps
	Live       []string
}

// Decal projects a planar texture or color marker into the scene. The initial
// renderer surface is a thin alpha-blended plane so decals work across WebGL
// and canvas fallbacks without introducing a geometry clipping pass.
type Decal struct {
	ID         string
	Src        string
	Color      string
	Position   Vector3
	Rotation   Euler
	Width      float64
	Height     float64
	Opacity    float64
	Pickable   *bool
	DepthWrite *bool
}

// AmbientLight adds untargeted scene illumination.
type AmbientLight struct {
	ID         string
	Color      string
	Intensity  float64
	Transition Transition
	InState    *LightProps
	OutState   *LightProps
	Live       []string
}

// DirectionalLight adds a directional scene light.
type DirectionalLight struct {
	ID             string
	Color          string
	Intensity      float64
	Direction      Vector3
	CastShadow     bool
	ShadowBias     float64
	ShadowSize     int
	ShadowCascades int
	ShadowSoftness float64
	Transition     Transition
	InState        *LightProps
	OutState       *LightProps
	Live           []string
}

// PointLight adds a positioned scene light with optional range falloff.
type PointLight struct {
	ID         string
	Color      string
	Intensity  float64
	Position   Vector3
	Range      float64
	Decay      float64
	Transition Transition
	InState    *LightProps
	OutState   *LightProps
	Live       []string
}

// SpotLight adds a positioned cone light with falloff.
type SpotLight struct {
	ID             string
	Color          string
	Intensity      float64
	Position       Vector3
	Direction      Vector3
	Angle          float64 // outer cone angle in radians
	Penumbra       float64 // 0 = hard edge, 1 = fully soft
	Range          float64
	Decay          float64
	CastShadow     bool
	ShadowBias     float64
	ShadowSize     int
	ShadowSoftness float64
	Transition     Transition
	InState        *LightProps
	OutState       *LightProps
	Live           []string
}

// HemisphereLight adds sky/ground ambient lighting.
type HemisphereLight struct {
	ID          string
	SkyColor    string
	GroundColor string
	Intensity   float64
	Transition  Transition
	InState     *LightProps
	OutState    *LightProps
	Live        []string
}

// RectAreaLight approximates a rectangular emitter. WebGL treats it as a
// finite area-shaped point light today, preserving width/height in the IR so a
// later LTC implementation can use the same authoring surface.
type RectAreaLight struct {
	ID         string
	Color      string
	Intensity  float64
	Position   Vector3
	Direction  Vector3
	Width      float64
	Height     float64
	Range      float64
	Decay      float64
	Transition Transition
	InState    *LightProps
	OutState   *LightProps
	Live       []string
}

// LightProbe contributes ambient probe lighting. Coefficients are reserved for
// spherical-harmonics probes; current renderers use Color/Intensity as a
// first-order ambient probe.
type LightProbe struct {
	ID           string
	Color        string
	Intensity    float64
	Coefficients []Vector3
	Transition   Transition
	InState      *LightProps
	OutState     *LightProps
	Live         []string
}

// AnimationClip defines a procedural animation clip with keyframe channels.
// Animation keyframes are high-dimensional vectors: a 64-joint skeleton with
// 60 keyframes produces 64 * 60 * 7 floats (pos xyz + quat xyzw) = 26,880
// floats. TurboQuant compression at 2-bit reduces this from ~107KB to ~6.7KB.
type AnimationClip struct {
	Name     string             // clip name (e.g. "idle", "walk")
	Duration float64            // clip duration in seconds
	Channels []AnimationChannel // per-target keyframe tracks
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
	VertexGLSL   string
	FragmentGLSL string
	VertexWGSL   string
	FragmentWGSL string
	Uniforms     map[string]any
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

type FlatMaterial MaterialStyle
type GhostMaterial MaterialStyle
type GlassMaterial MaterialStyle
type GlowMaterial MaterialStyle
type MatteMaterial MaterialStyle

// StandardMaterial is a PBR material using the roughness/metalness workflow.
type StandardMaterial struct {
	Color        string
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

type graphLowerer struct {
	objects            []ObjectIR
	models             []ModelIR
	points             []PointsIR
	instancedMeshes    []InstancedMeshIR
	instancedGLBMeshes []InstancedGLBMeshIR
	computeParticles   []ComputeParticlesIR
	animations         []AnimationClipIR
	pending            []pendingLabel
	pendingSprites     []pendingSprite
	lights             []LightIR
	anchors            map[string]worldTransform
	nextObjectID       int
	nextLabelID        int
	nextSpriteID       int
	nextLightID        int
	nextModelID        int
	nextPointsID       int
	nextInstancedID    int
	nextInstancedGLBID int
	nextParticlesID    int
}

func (Group) sceneNode()             {}
func (Mesh) sceneNode()              {}
func (LODGroup) sceneNode()          {}
func (Decal) sceneNode()             {}
func (Points) sceneNode()            {}
func (InstancedMesh) sceneNode()     {}
func (InstancedGLBMesh) sceneNode()  {}
func (ComputeParticles) sceneNode()  {}
func (Label) sceneNode()             {}
func (Sprite) sceneNode()            {}
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

func (CubeGeometry) sceneGeometry()     {}
func (BoxGeometry) sceneGeometry()      {}
func (PlaneGeometry) sceneGeometry()    {}
func (PyramidGeometry) sceneGeometry()  {}
func (SphereGeometry) sceneGeometry()   {}
func (LinesGeometry) sceneGeometry()    {}
func (CylinderGeometry) sceneGeometry() {}
func (TorusGeometry) sceneGeometry()    {}

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

func cloneSceneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneFloat64Slices(values map[string][]float64) map[string][]float64 {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string][]float64, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = append([]float64(nil), value...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Vec3 builds a Vector3 without forcing struct literals at callsites.
func Vec3(x, y, z float64) Vector3 {
	return Vector3{X: x, Y: y, Z: z}
}

// Rotate builds an Euler rotation in radians.
func Rotate(x, y, z float64) Euler {
	return Euler{X: x, Y: y, Z: z}
}

// NewGraph builds a scene graph from typed nodes.
func NewGraph(nodes ...Node) Graph {
	return Graph{Nodes: append([]Node(nil), nodes...)}
}

// GoSXSpreadProps allows file-route spreads to lower typed scene props into
// the current Scene3D component contract.
//
// Uses the fast spread path (same SceneIR → json.RawMessage trick MarshalJSON
// uses) so the hot SSR rendering path that spreads galaxy-sized scenes into
// a <Scene3D> component doesn't pay for the deep legacyProps() map tree.
// On a 20-mesh mixed scene the fast path is ~128 allocs where the legacy
// path is 378.
//
// The downstream consumer chain (route/fileprogram.go::renderEngineComponent
// → canonicalizeEnginePropsMap → json.Marshal) treats the json.RawMessage
// "scene" value as pre-serialized bytes and splices it into the final
// engine.Config.Props without re-marshaling — as long as
// canonicalizeEnginePropValue recognizes json.RawMessage and passes it
// through (see fileprogram.go for the pass-through).
func (p Props) GoSXSpreadProps() map[string]any {
	values := p.spreadPropsFast()
	if ref := strings.TrimSpace(p.ProgramRef); ref != "" {
		values["programRef"] = ref
	}
	values["capabilities"] = p.EngineCapabilities()
	return values
}

// LegacyProps lowers typed scene props into the current Scene3D prop bag.
//
// Preserved as-is because exported tests and any external consumers assert
// that legacy["scene"] is a map[string]any tree they can inspect. The fast
// SSR path (GoSXSpreadProps, MarshalJSON, RawPropsJSON) uses spreadPropsFast
// instead — see that method for the optimized flow.
func (p Props) LegacyProps() map[string]any {
	out := p.legacyBaseProps()
	if scene := p.SceneIR().legacyProps(); len(scene) > 0 {
		out["scene"] = scene
	}
	return out
}

// spreadPropsFast builds the same map shape as LegacyProps but uses the
// direct SceneIR marshal path for the "scene" key — the result is a
// json.RawMessage wrapped as a map value instead of a deep map tree.
//
// This is the shared hot-path helper for GoSXSpreadProps, MarshalJSON,
// and RawPropsJSON. Collapsing those three methods onto one internal
// spread builder means:
//
//  1. Only one code path to optimize as the scene prop shape evolves.
//  2. The expensive SceneIR lowering + json marshal runs exactly once per
//     Props → SSR output regardless of which entry point is used.
//  3. Tests that drive Props through the public MarshalJSON API pick up
//     the same fast path as callers using GoSXSpreadProps.
func (p Props) spreadPropsFast() map[string]any {
	base := p.legacyBaseProps()
	sceneIR := p.SceneIR()
	if !sceneIR.isZero() {
		sceneBytes, err := json.Marshal(sceneIR)
		if err == nil {
			base["scene"] = json.RawMessage(sceneBytes)
		}
	}
	return base
}

func (p Props) legacyBaseProps() map[string]any {
	out := map[string]any{}
	setInt(out, "width", p.Width)
	setInt(out, "height", p.Height)
	setString(out, "label", p.Label)
	setString(out, "ariaLabel", p.AriaLabel)
	setString(out, "background", p.Background)
	setString(out, "controls", p.Controls)
	setBool(out, "autoRotate", p.AutoRotate)
	setBool(out, "responsive", p.Responsive)
	setBool(out, "fillHeight", p.FillHeight)
	setBool(out, "preferWebGL", p.PreferWebGL)
	setBool(out, "forceWebGL", p.ForceWebGL)
	setBool(out, "requireWebGL", p.RequireWebGL)
	setBool(out, "preferCanvas", p.PreferCanvas)
	setString(out, "unsupportedMessage", p.UnsupportedMessage)
	setBool(out, "canvasAlpha", p.CanvasAlpha)
	setBool(out, "dragToRotate", p.DragToRotate)
	setBool(out, "deferPostFX", p.DeferPostFX)
	setInt(out, "deferPostFXDelayMS", p.DeferPostFXDelayMS)
	setString(out, "dragSignalNamespace", p.DragSignalNamespace)
	setString(out, "pickSignalNamespace", p.PickSignalNamespace)
	setString(out, "eventSignalNamespace", p.EventSignalNamespace)
	setString(out, "capabilityTier", p.CapabilityTier)
	if p.ControlTarget != (Vector3{}) {
		out["controlTarget"] = map[string]any{
			"x": p.ControlTarget.X,
			"y": p.ControlTarget.Y,
			"z": p.ControlTarget.Z,
		}
	}
	setNumeric(out, "controlRotateSpeed", p.ControlRotateSpeed)
	setNumeric(out, "controlZoomSpeed", p.ControlZoomSpeed)
	setNumeric(out, "controlLookSpeed", p.ControlLookSpeed)
	setNumeric(out, "controlMoveSpeed", p.ControlMoveSpeed)
	setNumeric(out, "scrollCameraStart", p.ScrollCameraStart)
	setNumeric(out, "scrollCameraEnd", p.ScrollCameraEnd)
	setNumeric(out, "maxDevicePixelRatio", p.MaxDevicePixelRatio)
	setBool(out, "stats", p.Stats)
	if p.Compression != nil {
		comp := map[string]any{"bitWidth": p.Compression.BitWidth}
		if p.Compression.Progressive {
			comp["progressive"] = true
			bw := p.Compression.PreviewBitWidth
			if bw <= 0 {
				bw = 2
			}
			comp["previewBitWidth"] = bw
			if p.Compression.ProgressiveDelayMS > 0 {
				comp["progressiveDelayMS"] = p.Compression.ProgressiveDelayMS
			}
		}
		if p.Compression.LOD {
			comp["lod"] = true
			thresh := p.Compression.LODThreshold
			if thresh <= 0 {
				thresh = 20
			}
			comp["lodThreshold"] = thresh
		}
		out["compression"] = comp
	}
	if camera := p.cameraLegacyProps(); len(camera) > 0 {
		out["camera"] = camera
	}
	return out
}

// MarshalJSON encodes only runtime props. Engine transport fields such as
// ProgramRef and Capabilities stay outside the JSON payload.
//
// The hot path builds the base props map (width/height/camera/compression/
// capabilities/etc. — Props has too many optional fields to express
// reliably via struct tags) and marshals SceneIR directly via reflection
// over its tagged struct fields, wiring the result in as a json.RawMessage
// under the "scene" key. This skips the big sceneIR.legacyProps()
// map tree that used to dominate Props allocations on every render —
// hundreds of interface{} boxings for numeric setters, nested
// map[string]any allocations per object/light/etc.
func (p Props) MarshalJSON() ([]byte, error) {
	base := p.spreadPropsFast()
	if len(base) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(base)
}

// RawPropsJSON returns engine.Config-compatible runtime props as bytes
// suitable for embedding directly in an engine manifest. Uses the fast
// spread path (same as MarshalJSON) so Scene3D engine serialization
// during SSR pays ~128 allocs instead of the legacy 378.
func (p Props) RawPropsJSON() json.RawMessage {
	data, err := p.MarshalJSON()
	if err != nil || len(data) == 0 || string(data) == "{}" {
		return nil
	}
	return data
}

// EngineCapabilities returns the Scene3D capability set with built-in defaults.
func (p Props) EngineCapabilities() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(defaultCapabilities)+len(p.Capabilities))
	appendCapability := func(raw string) {
		raw = strings.TrimSpace(strings.ToLower(raw))
		if raw == "" {
			return
		}
		if _, exists := seen[raw]; exists {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	for _, capability := range defaultCapabilities {
		appendCapability(capability)
	}
	if p.Graph.requiresComputeCapability() {
		appendCapability("webgpu")
	}
	for _, capability := range p.Capabilities {
		appendCapability(capability)
	}
	return out
}

func (g Graph) requiresComputeCapability() bool {
	return sceneNodesRequireComputeCapability(g.Nodes)
}

func sceneNodesRequireComputeCapability(nodes []Node) bool {
	for _, node := range nodes {
		if sceneNodeRequiresComputeCapability(node) {
			return true
		}
	}
	return false
}

func sceneNodeRequiresComputeCapability(node Node) bool {
	switch current := node.(type) {
	case ComputeParticles:
		return true
	case *ComputeParticles:
		return current != nil
	case LODGroup:
		for _, level := range current.Levels {
			if sceneNodeRequiresComputeCapability(level.Node) {
				return true
			}
		}
		return false
	case *LODGroup:
		if current == nil {
			return false
		}
		for _, level := range current.Levels {
			if sceneNodeRequiresComputeCapability(level.Node) {
				return true
			}
		}
		return false
	case Group:
		return sceneNodesRequireComputeCapability(current.Children)
	case *Group:
		return current != nil && sceneNodesRequireComputeCapability(current.Children)
	default:
		return false
	}
}

// EngineConfig builds the built-in Scene3D engine.Config for pure Go callsites.
func (p Props) EngineConfig() engine.Config {
	capabilities := p.EngineCapabilities()
	cfg := engine.Config{
		Name:       DefaultEngineName,
		Kind:       engine.KindSurface,
		WASMPath:   strings.TrimSpace(p.ProgramRef),
		Props:      p.RawPropsJSON(),
		MountAttrs: map[string]any{"data-gosx-scene3d": true},
	}
	if len(capabilities) > 0 {
		cfg.Capabilities = make([]engine.Capability, 0, len(capabilities))
		for _, capability := range capabilities {
			cfg.Capabilities = append(cfg.Capabilities, engine.Capability(capability))
		}
	}
	if p.RequireWebGL != nil && *p.RequireWebGL {
		cfg.RequiredCapabilities = []engine.Capability{engine.CapCanvas, engine.CapWebGL}
	}
	if cfg.WASMPath != "" {
		cfg.Runtime = engine.RuntimeShared
	}
	return cfg
}

func (c PerspectiveCamera) legacyProps() map[string]any {
	out := map[string]any{}
	if c.Position.X != 0 {
		out["x"] = c.Position.X
	}
	if c.Position.Y != 0 {
		out["y"] = c.Position.Y
	}
	if c.Position.Z != 0 {
		out["z"] = c.Position.Z
	}
	if c.Rotation.X != 0 {
		out["rotationX"] = c.Rotation.X
	}
	if c.Rotation.Y != 0 {
		out["rotationY"] = c.Rotation.Y
	}
	if c.Rotation.Z != 0 {
		out["rotationZ"] = c.Rotation.Z
	}
	if c.FOV != 0 {
		out["fov"] = c.FOV
	}
	if c.Near != 0 {
		out["near"] = c.Near
	}
	if c.Far != 0 {
		out["far"] = c.Far
	}
	if c.TransitionMS > 0 {
		out["transitionMS"] = c.TransitionMS
	}
	return out
}

func (c PerspectiveCamera) isZero() bool {
	return c.Position == (Vector3{}) && c.Rotation == (Euler{}) && c.FOV == 0 && c.Near == 0 && c.Far == 0 && c.TransitionMS == 0
}

func (c OrthographicCamera) legacyProps() map[string]any {
	out := map[string]any{"kind": "orthographic"}
	if c.Position.X != 0 {
		out["x"] = c.Position.X
	}
	if c.Position.Y != 0 {
		out["y"] = c.Position.Y
	}
	if c.Position.Z != 0 {
		out["z"] = c.Position.Z
	}
	if c.Rotation.X != 0 {
		out["rotationX"] = c.Rotation.X
	}
	if c.Rotation.Y != 0 {
		out["rotationY"] = c.Rotation.Y
	}
	if c.Rotation.Z != 0 {
		out["rotationZ"] = c.Rotation.Z
	}
	if c.Left != 0 {
		out["left"] = c.Left
	}
	if c.Right != 0 {
		out["right"] = c.Right
	}
	if c.Top != 0 {
		out["top"] = c.Top
	}
	if c.Bottom != 0 {
		out["bottom"] = c.Bottom
	}
	if c.Zoom != 0 {
		out["zoom"] = c.Zoom
	}
	if c.Near != 0 {
		out["near"] = c.Near
	}
	if c.Far != 0 {
		out["far"] = c.Far
	}
	if c.TransitionMS > 0 {
		out["transitionMS"] = c.TransitionMS
	}
	return out
}

func (c OrthographicCamera) isZero() bool {
	return c.Position == (Vector3{}) && c.Rotation == (Euler{}) && c.Left == 0 && c.Right == 0 && c.Top == 0 && c.Bottom == 0 && c.Zoom == 0 && c.Near == 0 && c.Far == 0 && c.TransitionMS == 0
}

func (p Props) cameraLegacyProps() map[string]any {
	if p.OrthographicCamera != nil && !p.OrthographicCamera.isZero() {
		return p.OrthographicCamera.legacyProps()
	}
	if !p.Camera.isZero() {
		return p.Camera.legacyProps()
	}
	return nil
}

func (l *graphLowerer) lowerNode(node Node, parent worldTransform) {
	switch current := node.(type) {
	case Group:
		l.lowerGroup(current, parent)
	case *Group:
		if current != nil {
			l.lowerGroup(*current, parent)
		}
	case Mesh:
		l.lowerMesh(current, parent)
	case *Mesh:
		if current != nil {
			l.lowerMesh(*current, parent)
		}
	case LODGroup:
		l.lowerLODGroup(current, parent)
	case *LODGroup:
		if current != nil {
			l.lowerLODGroup(*current, parent)
		}
	case Decal:
		l.lowerDecal(current, parent)
	case *Decal:
		if current != nil {
			l.lowerDecal(*current, parent)
		}
	case Points:
		l.lowerPoints(current, parent)
	case *Points:
		if current != nil {
			l.lowerPoints(*current, parent)
		}
	case InstancedMesh:
		l.lowerInstancedMesh(current, parent)
	case *InstancedMesh:
		if current != nil {
			l.lowerInstancedMesh(*current, parent)
		}
	case InstancedGLBMesh:
		l.lowerInstancedGLBMesh(current, parent)
	case *InstancedGLBMesh:
		if current != nil {
			l.lowerInstancedGLBMesh(*current, parent)
		}
	case ComputeParticles:
		l.lowerComputeParticles(current, parent)
	case *ComputeParticles:
		if current != nil {
			l.lowerComputeParticles(*current, parent)
		}
	case Label:
		l.pending = append(l.pending, pendingLabel{label: current, parent: parent})
	case *Label:
		if current != nil {
			l.pending = append(l.pending, pendingLabel{label: *current, parent: parent})
		}
	case Sprite:
		l.pendingSprites = append(l.pendingSprites, pendingSprite{sprite: current, parent: parent})
	case *Sprite:
		if current != nil {
			l.pendingSprites = append(l.pendingSprites, pendingSprite{sprite: *current, parent: parent})
		}
	case Model:
		l.lowerModel(current, parent)
	case *Model:
		if current != nil {
			l.lowerModel(*current, parent)
		}
	case AmbientLight:
		l.lowerAmbientLight(current)
	case *AmbientLight:
		if current != nil {
			l.lowerAmbientLight(*current)
		}
	case DirectionalLight:
		l.lowerDirectionalLight(current, parent)
	case *DirectionalLight:
		if current != nil {
			l.lowerDirectionalLight(*current, parent)
		}
	case PointLight:
		l.lowerPointLight(current, parent)
	case *PointLight:
		if current != nil {
			l.lowerPointLight(*current, parent)
		}
	case SpotLight:
		l.lowerSpotLight(current, parent)
	case *SpotLight:
		if current != nil {
			l.lowerSpotLight(*current, parent)
		}
	case HemisphereLight:
		l.lowerHemisphereLight(current)
	case *HemisphereLight:
		if current != nil {
			l.lowerHemisphereLight(*current)
		}
	case RectAreaLight:
		l.lowerRectAreaLight(current, parent)
	case *RectAreaLight:
		if current != nil {
			l.lowerRectAreaLight(*current, parent)
		}
	case LightProbe:
		l.lowerLightProbe(current)
	case *LightProbe:
		if current != nil {
			l.lowerLightProbe(*current)
		}
	case AnimationClip:
		l.lowerAnimationClip(current)
	case *AnimationClip:
		if current != nil {
			l.lowerAnimationClip(*current)
		}
	case AxesHelper:
		l.lowerAxesHelper(current, parent)
	case *AxesHelper:
		if current != nil {
			l.lowerAxesHelper(*current, parent)
		}
	case GridHelper:
		l.lowerGridHelper(current, parent)
	case *GridHelper:
		if current != nil {
			l.lowerGridHelper(*current, parent)
		}
	case BoxHelper:
		l.lowerBoxHelper(current, parent)
	case *BoxHelper:
		if current != nil {
			l.lowerBoxHelper(*current, parent)
		}
	case BoundingBoxHelper:
		l.lowerBoundingBoxHelper(current, parent)
	case *BoundingBoxHelper:
		if current != nil {
			l.lowerBoundingBoxHelper(*current, parent)
		}
	case SkeletonHelper:
		l.lowerSkeletonHelper(current, parent)
	case *SkeletonHelper:
		if current != nil {
			l.lowerSkeletonHelper(*current, parent)
		}
	case TransformControls:
		l.lowerTransformControls(current, parent)
	case *TransformControls:
		if current != nil {
			l.lowerTransformControls(*current, parent)
		}
	}
}

func (l *graphLowerer) lowerGroup(group Group, parent worldTransform) {
	world := combineTransforms(parent, localTransform(group.Position, group.Rotation))
	if id := strings.TrimSpace(group.ID); id != "" {
		l.anchors[id] = world
	}
	for _, child := range group.Children {
		l.lowerNode(child, world)
	}
}

func (l *graphLowerer) lowerLODGroup(group LODGroup, parent worldTransform) {
	if len(group.Levels) == 0 {
		return
	}
	world := combineTransforms(parent, localTransform(group.Position, group.Rotation))
	id := strings.TrimSpace(group.ID)
	if id == "" {
		l.nextObjectID++
		id = "scene-lod-group-" + intString(l.nextObjectID)
	}
	l.anchors[id] = world
	levels := append([]LODLevel(nil), group.Levels...)
	sort.SliceStable(levels, func(i, j int) bool {
		return levels[i].Distance < levels[j].Distance
	})
	for index, level := range levels {
		if level.Node == nil {
			continue
		}
		startObjects := len(l.objects)
		startModels := len(l.models)
		l.lowerNode(level.Node, world)
		minDistance := level.Distance
		if minDistance < 0 {
			minDistance = 0
		}
		maxDistance := 0.0
		if index+1 < len(levels) {
			maxDistance = levels[index+1].Distance
			if maxDistance < minDistance {
				maxDistance = 0
			}
		}
		l.annotateLODRecords(startObjects, startModels, id, index, minDistance, maxDistance)
	}
}

func (l *graphLowerer) annotateLODRecords(startObjects, startModels int, group string, level int, minDistance, maxDistance float64) {
	for i := startObjects; i < len(l.objects); i++ {
		l.objects[i].LODGroup = group
		l.objects[i].LODLevel = level
		l.objects[i].LODMinDistance = minDistance
		l.objects[i].LODMaxDistance = maxDistance
	}
	for i := startModels; i < len(l.models); i++ {
		l.models[i].LODGroup = group
		l.models[i].LODLevel = level
		l.models[i].LODMinDistance = minDistance
		l.models[i].LODMaxDistance = maxDistance
	}
}

func (l *graphLowerer) lowerDecal(decal Decal, parent worldTransform) {
	width := decal.Width
	if width <= 0 {
		width = 1
	}
	height := decal.Height
	if height <= 0 {
		height = width
	}
	opacity := decal.Opacity
	if opacity <= 0 {
		opacity = 1
	}
	color := strings.TrimSpace(decal.Color)
	if color == "" {
		color = "#ffffff"
	}
	depthWrite := decal.DepthWrite
	if depthWrite == nil {
		depthWrite = Bool(false)
	}
	material := FlatMaterial{
		Color:      color,
		Texture:    strings.TrimSpace(decal.Src),
		Opacity:    Float(opacity),
		BlendMode:  BlendAlpha,
		RenderPass: RenderAlpha,
	}
	l.lowerMesh(Mesh{
		ID:         decal.ID,
		Geometry:   PlaneGeometry{Width: width, Height: height},
		Material:   material,
		Position:   decal.Position,
		Rotation:   decal.Rotation,
		Pickable:   decal.Pickable,
		DepthWrite: depthWrite,
	}, parent)
}

func (l *graphLowerer) lowerAxesHelper(helper AxesHelper, parent worldTransform) {
	size := helper.Size
	if size <= 0 {
		size = 1
	}
	width := helper.Width
	if width <= 0 {
		width = 1.5
	}
	base := strings.TrimSpace(helper.ID)
	if base == "" {
		l.nextObjectID++
		base = "scene-axes-helper-" + intString(l.nextObjectID)
	}
	lowerLineHelper := func(suffix, color string, points []Vector3) {
		l.lowerMesh(Mesh{
			ID:       base + "-" + suffix,
			Geometry: LinesGeometry{Points: points, Segments: [][2]int{{0, 1}}, Width: width},
			Material: LineBasicMaterial{MaterialStyle: MaterialStyle{Color: color}, Width: width},
			Position: helper.Position,
			Rotation: helper.Rotation,
		}, parent)
	}
	lowerLineHelper("x", "#ef4444", []Vector3{{}, {X: size}})
	lowerLineHelper("y", "#22c55e", []Vector3{{}, {Y: size}})
	lowerLineHelper("z", "#3b82f6", []Vector3{{}, {Z: size}})
}

func (l *graphLowerer) lowerGridHelper(helper GridHelper, parent worldTransform) {
	size := helper.Size
	if size <= 0 {
		size = 10
	}
	divisions := helper.Divisions
	if divisions <= 0 {
		divisions = 10
	}
	half := size / 2
	step := size / float64(divisions)
	points := make([]Vector3, 0, (divisions+1)*4)
	segments := make([][2]int, 0, (divisions+1)*2)
	for i := 0; i <= divisions; i++ {
		offset := -half + float64(i)*step
		base := len(points)
		points = append(points,
			Vector3{X: -half, Z: offset},
			Vector3{X: half, Z: offset},
			Vector3{X: offset, Z: -half},
			Vector3{X: offset, Z: half},
		)
		segments = append(segments, [2]int{base, base + 1}, [2]int{base + 2, base + 3})
	}
	color := strings.TrimSpace(helper.Color)
	if color == "" {
		color = "#9ca3af"
	}
	width := helper.Width
	if width <= 0 {
		width = 1
	}
	id := l.nextSceneHelperID("scene-grid-helper", helper.ID)
	l.lowerMesh(Mesh{
		ID:       id,
		Geometry: LinesGeometry{Points: points, Segments: segments, Width: width},
		Material: LineBasicMaterial{MaterialStyle: MaterialStyle{Color: color, Opacity: Float(0.72), BlendMode: BlendAlpha}, Width: width},
		Position: helper.Position,
		Rotation: helper.Rotation,
	}, parent)
}

func (l *graphLowerer) lowerBoxHelper(helper BoxHelper, parent worldTransform) {
	width := helper.Width
	if width <= 0 {
		width = 1
	}
	height := helper.Height
	if height <= 0 {
		height = width
	}
	depth := helper.Depth
	if depth <= 0 {
		depth = width
	}
	id := l.nextSceneHelperID("scene-box-helper", helper.ID)
	l.lowerMesh(Mesh{
		ID:       id,
		Geometry: boxHelperGeometry(width, height, depth, helper.WidthPx),
		Material: LineBasicMaterial{MaterialStyle: MaterialStyle{Color: firstNonEmptySceneString(helper.Color, "#f59e0b")}, Width: helper.WidthPx},
		Position: helper.Position,
		Rotation: helper.Rotation,
	}, parent)
}

func (l *graphLowerer) lowerBoundingBoxHelper(helper BoundingBoxHelper, parent worldTransform) {
	min := helper.Min
	max := helper.Max
	center := Vector3{
		X: (min.X + max.X) / 2,
		Y: (min.Y + max.Y) / 2,
		Z: (min.Z + max.Z) / 2,
	}
	id := l.nextSceneHelperID("scene-bounds-helper", helper.ID)
	l.lowerMesh(Mesh{
		ID:       id,
		Geometry: boxHelperGeometry(math.Abs(max.X-min.X), math.Abs(max.Y-min.Y), math.Abs(max.Z-min.Z), helper.WidthPx),
		Material: LineBasicMaterial{MaterialStyle: MaterialStyle{Color: firstNonEmptySceneString(helper.Color, "#f59e0b")}, Width: helper.WidthPx},
		Position: center,
	}, parent)
}

func (l *graphLowerer) lowerSkeletonHelper(helper SkeletonHelper, parent worldTransform) {
	width := helper.Width
	if width <= 0 {
		width = 1.25
	}
	id := l.nextSceneHelperID("scene-skeleton-helper", helper.ID)
	l.lowerMesh(Mesh{
		ID:       id,
		Geometry: LinesGeometry{Points: append([]Vector3(nil), helper.Joints...), Segments: append([][2]int(nil), helper.Bones...), Width: width},
		Material: LineBasicMaterial{MaterialStyle: MaterialStyle{Color: firstNonEmptySceneString(helper.Color, "#e879f9")}, Width: width},
		Position: helper.Position,
		Rotation: helper.Rotation,
	}, parent)
}

func (l *graphLowerer) lowerTransformControls(control TransformControls, parent worldTransform) {
	size := control.Size
	if size <= 0 {
		size = 1.25
	}
	width := control.Width
	if width <= 0 {
		width = 2
	}
	position := control.Position
	if target := strings.TrimSpace(control.Target); target != "" {
		if anchor, ok := l.anchors[target]; ok {
			position = anchor.Position
		}
	}
	id := l.nextSceneHelperID("scene-transform-controls", control.ID)
	l.lowerAxesHelper(AxesHelper{
		ID:       id,
		Size:     size,
		Position: position,
		Rotation: control.Rotation,
		Width:    width,
	}, parent)
	if strings.EqualFold(strings.TrimSpace(control.Mode), "rotate") {
		l.lowerMesh(Mesh{
			ID:       id + "-ring",
			Geometry: helperRingGeometry(size, 48, width),
			Material: LineBasicMaterial{MaterialStyle: MaterialStyle{Color: "#facc15"}, Width: width},
			Position: position,
			Rotation: control.Rotation,
		}, parent)
	}
}

func (l *graphLowerer) lowerMesh(mesh Mesh, parent worldTransform) {
	world := combineTransforms(parent, localTransform(mesh.Position, mesh.Rotation))
	id := strings.TrimSpace(mesh.ID)
	if id == "" {
		l.nextObjectID += 1
		id = "scene-object-" + intString(l.nextObjectID)
	}
	record := ObjectIR{
		ID: id,
	}
	record.Kind = applyGeometryToObjectIR(&record, mesh.Geometry)
	applyMaterialToObjectIR(&record, mesh.Material)
	record.X = world.Position.X
	record.Y = world.Position.Y
	record.Z = world.Position.Z
	rotation := eulerFromQuaternion(world.Rotation)
	record.RotationX = rotation.X
	record.RotationY = rotation.Y
	record.RotationZ = rotation.Z
	record.SpinX = mesh.Spin.X
	record.SpinY = mesh.Spin.Y
	record.SpinZ = mesh.Spin.Z
	record.Pickable = mesh.Pickable
	record.Selected = mesh.Selected
	record.OutlineColor = strings.TrimSpace(mesh.OutlineColor)
	record.OutlineWidth = mesh.OutlineWidth
	record.CastShadow = mesh.CastShadow
	record.ReceiveShadow = mesh.ReceiveShadow
	record.DepthWrite = mesh.DepthWrite
	record.ShiftX = mesh.Drift.X
	record.ShiftY = mesh.Drift.Y
	record.ShiftZ = mesh.Drift.Z
	record.DriftSpeed = mesh.DriftSpeed
	record.DriftPhase = mesh.DriftPhase
	record.Transition = lowerTransition(mesh.Transition)
	record.InState = mesh.InState.legacyProps()
	record.OutState = mesh.OutState.legacyProps()
	record.Live = normalizeLive(mesh.Live)
	l.objects = append(l.objects, record)
	l.anchors[id] = world
	for _, child := range mesh.Children {
		l.lowerNode(child, world)
	}
}

func (l *graphLowerer) lowerPoints(pts Points, parent worldTransform) {
	world := combineTransforms(parent, localTransform(pts.Position, pts.Rotation))
	id := strings.TrimSpace(pts.ID)
	if id == "" {
		l.nextPointsID += 1
		id = "scene-points-" + intString(l.nextPointsID)
	}
	record := PointsIR{
		ID:          id,
		Count:       pts.Count,
		Color:       strings.TrimSpace(pts.Color),
		Style:       strings.TrimSpace(string(pts.Style)),
		Size:        pts.Size,
		Opacity:     pts.Opacity,
		BlendMode:   strings.TrimSpace(string(pts.BlendMode)),
		DepthWrite:  Bool(pts.DepthWrite),
		Attenuation: pts.Attenuation,
		X:           world.Position.X,
		Y:           world.Position.Y,
		Z:           world.Position.Z,
		Transition:  lowerTransition(pts.Transition),
		InState:     pts.InState.legacyProps(),
		OutState:    pts.OutState.legacyProps(),
		Live:        normalizeLive(pts.Live),
	}
	rotation := eulerFromQuaternion(world.Rotation)
	record.RotationX = rotation.X
	record.RotationY = rotation.Y
	record.RotationZ = rotation.Z
	record.SpinX = pts.Spin.X
	record.SpinY = pts.Spin.Y
	record.SpinZ = pts.Spin.Z
	// Flatten positions to [x,y,z, x,y,z, ...].
	if len(pts.Positions) > 0 {
		flat := make([]float64, 0, len(pts.Positions)*3)
		for _, p := range pts.Positions {
			flat = append(flat, p.X, p.Y, p.Z)
		}
		record.Positions = flat
	}
	if len(pts.Sizes) > 0 {
		record.Sizes = append([]float64(nil), pts.Sizes...)
	}
	if len(pts.Colors) > 0 {
		record.Colors = append([]string(nil), pts.Colors...)
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
		ID:            id,
		Count:         im.Count,
		Kind:          kind,
		CastShadow:    im.CastShadow,
		ReceiveShadow: im.ReceiveShadow,
		Transition:    lowerTransition(im.Transition),
		InState:       im.InState.legacyProps(),
		OutState:      im.OutState.legacyProps(),
		Live:          normalizeLive(im.Live),
		Colors:        append([]string(nil), im.Colors...),
		Attributes:    cloneFloat64Slices(im.Attributes),
	}
	// Apply geometry dimensions.
	if geometryProps != nil {
		record.Width = mapFloat64(geometryProps["width"])
		record.Height = mapFloat64(geometryProps["height"])
		record.Depth = mapFloat64(geometryProps["depth"])
		record.Radius = mapFloat64(geometryProps["radius"])
		record.Segments = mapInt(geometryProps["segments"])
	}
	// Apply material kind.
	if materialProps != nil {
		if mk, ok := mapStringValue(materialProps["materialKind"]); ok {
			record.MaterialKind = mk
		}
		if c, ok := materialProps["color"].(string); ok {
			record.Color = strings.TrimSpace(c)
		}
		record.Roughness = mapFloat64(materialProps["roughness"])
		record.Metalness = mapFloat64(materialProps["metalness"])
	}

	// Pre-compute per-instance column-major 4x4 transforms.
	count := im.Count
	transforms := make([]float64, 0, count*16)
	for i := 0; i < count; i++ {
		var pos Vector3
		if i < len(im.Positions) {
			pos = im.Positions[i]
		}
		var rot Euler
		if i < len(im.Rotations) {
			rot = im.Rotations[i]
		}
		var scl Vector3
		if i < len(im.Scales) {
			scl = im.Scales[i]
		} else {
			scl = Vector3{X: 1, Y: 1, Z: 1}
		}
		// Apply parent world transform to instance position.
		worldPos := addVectors(world.Position, world.Rotation.rotate(pos))
		instanceRot := world.Rotation.mul(quaternionFromEuler(rot)).normalized()
		transforms = append(transforms, mat4FromTRS(worldPos, instanceRot, scl)...)
	}
	record.Transforms = transforms
	l.instancedMeshes = append(l.instancedMeshes, record)
}

func (l *graphLowerer) lowerComputeParticles(cp ComputeParticles, parent worldTransform) {
	world := combineTransforms(parent, localTransform(cp.Emitter.Position, cp.Emitter.Rotation))
	id := strings.TrimSpace(cp.ID)
	if id == "" {
		l.nextParticlesID += 1
		id = "scene-particles-" + intString(l.nextParticlesID)
	}

	forces := make([]ParticleForceIR, 0, len(cp.Forces))
	for _, f := range cp.Forces {
		forces = append(forces, ParticleForceIR{
			Kind:      strings.TrimSpace(f.Kind),
			Strength:  f.Strength,
			X:         f.Direction.X,
			Y:         f.Direction.Y,
			Z:         f.Direction.Z,
			Frequency: f.Frequency,
		})
	}

	record := ComputeParticlesIR{
		ID:    id,
		Count: cp.Count,
		Emitter: ParticleEmitterIR{
			Kind:      strings.TrimSpace(cp.Emitter.Kind),
			X:         world.Position.X,
			Y:         world.Position.Y,
			Z:         world.Position.Z,
			RotationX: eulerFromQuaternion(world.Rotation).X,
			RotationY: eulerFromQuaternion(world.Rotation).Y,
			RotationZ: eulerFromQuaternion(world.Rotation).Z,
			SpinX:     cp.Emitter.Spin.X,
			SpinY:     cp.Emitter.Spin.Y,
			SpinZ:     cp.Emitter.Spin.Z,
			Radius:    cp.Emitter.Radius,
			Rate:      cp.Emitter.Rate,
			Lifetime:  cp.Emitter.Lifetime,
			Arms:      cp.Emitter.Arms,
			Wind:      cp.Emitter.Wind,
			Scatter:   cp.Emitter.Scatter,
		},
		Forces: forces,
		Material: ParticleMaterialIR{
			Color:       strings.TrimSpace(cp.Material.Color),
			ColorEnd:    strings.TrimSpace(cp.Material.ColorEnd),
			Style:       strings.TrimSpace(string(cp.Material.Style)),
			Size:        cp.Material.Size,
			SizeEnd:     cp.Material.SizeEnd,
			Opacity:     cp.Material.Opacity,
			OpacityEnd:  cp.Material.OpacityEnd,
			BlendMode:   strings.TrimSpace(string(cp.Material.BlendMode)),
			Attenuation: cp.Material.Attenuation,
		},
		Bounds:     cp.Bounds,
		Transition: lowerTransition(cp.Transition),
		InState:    cp.InState.legacyProps(),
		OutState:   cp.OutState.legacyProps(),
		Live:       normalizeLive(cp.Live),
	}
	l.computeParticles = append(l.computeParticles, record)
}

// mat4FromTRS builds a column-major 4x4 matrix from translation, rotation (quaternion), and scale.
func mat4FromTRS(t Vector3, q quaternion, s Vector3) []float64 {
	// Rotation matrix from quaternion.
	xx := q.X * q.X
	yy := q.Y * q.Y
	zz := q.Z * q.Z
	xy := q.X * q.Y
	xz := q.X * q.Z
	yz := q.Y * q.Z
	wx := q.W * q.X
	wy := q.W * q.Y
	wz := q.W * q.Z

	// Column-major order: m[col*4 + row]
	return []float64{
		// Column 0
		(1 - 2*(yy+zz)) * s.X,
		(2 * (xy + wz)) * s.X,
		(2 * (xz - wy)) * s.X,
		0,
		// Column 1
		(2 * (xy - wz)) * s.Y,
		(1 - 2*(xx+zz)) * s.Y,
		(2 * (yz + wx)) * s.Y,
		0,
		// Column 2
		(2 * (xz + wy)) * s.Z,
		(2 * (yz - wx)) * s.Z,
		(1 - 2*(xx+yy)) * s.Z,
		0,
		// Column 3
		t.X,
		t.Y,
		t.Z,
		1,
	}
}

func (l *graphLowerer) lowerModel(model Model, parent worldTransform) {
	src := strings.TrimSpace(model.Src)
	if src == "" {
		return
	}
	world := combineTransforms(parent, localTransform(model.Position, model.Rotation))
	id := l.nextSceneModelID(model.ID)
	record := ModelIR{
		ObjectIR: ObjectIR{
			ID:         id,
			X:          world.Position.X,
			Y:          world.Position.Y,
			Z:          world.Position.Z,
			Transition: lowerTransition(model.Transition),
			InState:    model.InState.legacyProps(),
			OutState:   model.OutState.legacyProps(),
			Live:       normalizeLive(model.Live),
		},
		Src:    src,
		ScaleX: model.Scale.X,
		ScaleY: model.Scale.Y,
		ScaleZ: model.Scale.Z,
	}
	rotation := eulerFromQuaternion(world.Rotation)
	record.RotationX = rotation.X
	record.RotationY = rotation.Y
	record.RotationZ = rotation.Z
	applyMaterialProps(&record.ObjectIR, legacyMaterial(model.Material))
	record.Static = model.Static
	record.Pickable = model.Pickable
	record.Animation = strings.TrimSpace(model.Animation)
	record.Loop = model.Loop
	l.models = append(l.models, record)
	l.anchors[id] = world
}

func (l *graphLowerer) lowerInstancedGLBMesh(igm InstancedGLBMesh, parent worldTransform) {
	src := strings.TrimSpace(igm.Src)
	if src == "" || len(igm.Instances) == 0 {
		return
	}
	id := strings.TrimSpace(igm.ID)
	if id == "" {
		l.nextInstancedGLBID += 1
		id = "scene-instanced-glb-" + intString(l.nextInstancedGLBID)
	}
	mat := legacyMaterial(igm.Material)
	instances := make([]MeshInstanceIR, 0, len(igm.Instances))
	for _, inst := range igm.Instances {
		world := combineTransforms(parent, localTransform(inst.Position, inst.Rotation))
		rotation := eulerFromQuaternion(world.Rotation)
		instances = append(instances, MeshInstanceIR{
			ID:        strings.TrimSpace(inst.ID),
			X:         world.Position.X,
			Y:         world.Position.Y,
			Z:         world.Position.Z,
			ScaleX:    inst.Scale.X,
			ScaleY:    inst.Scale.Y,
			ScaleZ:    inst.Scale.Z,
			RotationX: rotation.X,
			RotationY: rotation.Y,
			RotationZ: rotation.Z,
		})
	}
	record := InstancedGLBMeshIR{
		ID:        id,
		Src:       src,
		Pickable:  igm.Pickable,
		Static:    igm.Static,
		Instances: instances,
	}
	if mat != nil {
		if s, ok := mapStringValue(mat["materialKind"]); ok {
			record.MaterialKind = s
		}
		if s, ok := mapStringValue(mat["color"]); ok {
			record.Color = s
		}
		if s, ok := mapStringValue(mat["texture"]); ok {
			record.Texture = s
		}
		if s, ok := mapStringValue(mat["blendMode"]); ok {
			record.BlendMode = s
		}
		record.Roughness = mapFloat64(mat["roughness"])
		record.Metalness = mapFloat64(mat["metalness"])
		if v, ok := mat["opacity"]; ok {
			if f, ok2 := toFloat64(v); ok2 {
				record.Opacity = &f
			}
		}
		if v, ok := mat["emissive"]; ok {
			if f, ok2 := toFloat64(v); ok2 {
				record.Emissive = &f
			}
		}
	}
	l.instancedGLBMeshes = append(l.instancedGLBMeshes, record)
}

func toFloat64(v any) (float64, bool) {
	switch f := v.(type) {
	case float64:
		return f, true
	case float32:
		return float64(f), true
	case int:
		return float64(f), true
	default:
		return 0, false
	}
}

func (l *graphLowerer) resolveLabels() []LabelIR {
	if len(l.pending) == 0 {
		return nil
	}
	out := make([]LabelIR, 0, len(l.pending))
	for _, item := range l.pending {
		if record, ok := l.resolveLabel(item); ok {
			out = append(out, record)
		}
	}
	return out
}

func (l *graphLowerer) resolveSprites() []SpriteIR {
	if len(l.pendingSprites) == 0 {
		return nil
	}
	out := make([]SpriteIR, 0, len(l.pendingSprites))
	for _, item := range l.pendingSprites {
		if record, ok := l.resolveSprite(item); ok {
			out = append(out, record)
		}
	}
	return out
}

func (l *graphLowerer) lowerAmbientLight(light AmbientLight) {
	l.lights = append(l.lights, LightIR{
		ID:         l.nextSceneLightID("ambient-light", light.ID),
		Kind:       "ambient",
		Color:      strings.TrimSpace(light.Color),
		Intensity:  light.Intensity,
		Transition: lowerTransition(light.Transition),
		InState:    light.InState.legacyProps(),
		OutState:   light.OutState.legacyProps(),
		Live:       normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerDirectionalLight(light DirectionalLight, parent worldTransform) {
	direction := parent.Rotation.rotate(light.Direction)
	l.lights = append(l.lights, LightIR{
		ID:             l.nextSceneLightID("directional-light", light.ID),
		Kind:           "directional",
		Color:          strings.TrimSpace(light.Color),
		Intensity:      light.Intensity,
		DirectionX:     direction.X,
		DirectionY:     direction.Y,
		DirectionZ:     direction.Z,
		CastShadow:     light.CastShadow,
		ShadowBias:     light.ShadowBias,
		ShadowSize:     light.ShadowSize,
		ShadowCascades: normalizeShadowCascades(light.ShadowCascades),
		ShadowSoftness: normalizeShadowSoftness(light.ShadowSoftness),
		Transition:     lowerTransition(light.Transition),
		InState:        light.InState.legacyProps(),
		OutState:       light.OutState.legacyProps(),
		Live:           normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerPointLight(light PointLight, parent worldTransform) {
	world := combineTransforms(parent, localTransform(light.Position, Euler{}))
	l.lights = append(l.lights, LightIR{
		ID:         l.nextSceneLightID("point-light", light.ID),
		Kind:       "point",
		Color:      strings.TrimSpace(light.Color),
		Intensity:  light.Intensity,
		X:          world.Position.X,
		Y:          world.Position.Y,
		Z:          world.Position.Z,
		Range:      light.Range,
		Decay:      light.Decay,
		Transition: lowerTransition(light.Transition),
		InState:    light.InState.legacyProps(),
		OutState:   light.OutState.legacyProps(),
		Live:       normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerSpotLight(light SpotLight, parent worldTransform) {
	world := combineTransforms(parent, localTransform(light.Position, Euler{}))
	direction := parent.Rotation.rotate(light.Direction)
	l.lights = append(l.lights, LightIR{
		ID:             l.nextSceneLightID("spot-light", light.ID),
		Kind:           "spot",
		Color:          strings.TrimSpace(light.Color),
		Intensity:      light.Intensity,
		X:              world.Position.X,
		Y:              world.Position.Y,
		Z:              world.Position.Z,
		DirectionX:     direction.X,
		DirectionY:     direction.Y,
		DirectionZ:     direction.Z,
		Angle:          light.Angle,
		Penumbra:       light.Penumbra,
		Range:          light.Range,
		Decay:          light.Decay,
		CastShadow:     light.CastShadow,
		ShadowBias:     light.ShadowBias,
		ShadowSize:     light.ShadowSize,
		ShadowSoftness: normalizeShadowSoftness(light.ShadowSoftness),
		Transition:     lowerTransition(light.Transition),
		InState:        light.InState.legacyProps(),
		OutState:       light.OutState.legacyProps(),
		Live:           normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerHemisphereLight(light HemisphereLight) {
	l.lights = append(l.lights, LightIR{
		ID:          l.nextSceneLightID("hemisphere-light", light.ID),
		Kind:        "hemisphere",
		Color:       strings.TrimSpace(light.SkyColor),
		GroundColor: strings.TrimSpace(light.GroundColor),
		Intensity:   light.Intensity,
		Transition:  lowerTransition(light.Transition),
		InState:     light.InState.legacyProps(),
		OutState:    light.OutState.legacyProps(),
		Live:        normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerRectAreaLight(light RectAreaLight, parent worldTransform) {
	world := combineTransforms(parent, localTransform(light.Position, Euler{}))
	direction := parent.Rotation.rotate(light.Direction)
	l.lights = append(l.lights, LightIR{
		ID:         l.nextSceneLightID("rect-area-light", light.ID),
		Kind:       "rect-area",
		Color:      strings.TrimSpace(light.Color),
		Intensity:  light.Intensity,
		X:          world.Position.X,
		Y:          world.Position.Y,
		Z:          world.Position.Z,
		DirectionX: direction.X,
		DirectionY: direction.Y,
		DirectionZ: direction.Z,
		Width:      light.Width,
		Height:     light.Height,
		Range:      light.Range,
		Decay:      light.Decay,
		Transition: lowerTransition(light.Transition),
		InState:    light.InState.legacyProps(),
		OutState:   light.OutState.legacyProps(),
		Live:       normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerLightProbe(light LightProbe) {
	l.lights = append(l.lights, LightIR{
		ID:           l.nextSceneLightID("light-probe", light.ID),
		Kind:         "light-probe",
		Color:        strings.TrimSpace(light.Color),
		Intensity:    light.Intensity,
		Coefficients: append([]Vector3(nil), light.Coefficients...),
		Transition:   lowerTransition(light.Transition),
		InState:      light.InState.legacyProps(),
		OutState:     light.OutState.legacyProps(),
		Live:         normalizeLive(light.Live),
	})
}

func (l *graphLowerer) lowerAnimationClip(clip AnimationClip) {
	name := strings.TrimSpace(clip.Name)
	if name == "" || len(clip.Channels) == 0 {
		return
	}
	channels := make([]AnimationChannelIR, 0, len(clip.Channels))
	for _, ch := range clip.Channels {
		prop := strings.TrimSpace(ch.Property)
		if prop == "" || len(ch.Times) == 0 || len(ch.Values) == 0 {
			continue
		}
		interp := strings.TrimSpace(ch.Interpolation)
		if interp == "" {
			interp = "LINEAR"
		}
		channels = append(channels, AnimationChannelIR{
			TargetNode:    ch.TargetNode,
			Property:      prop,
			Interpolation: interp,
			Times:         append([]float64(nil), ch.Times...),
			Values:        append([]float64(nil), ch.Values...),
		})
	}
	if len(channels) == 0 {
		return
	}
	l.animations = append(l.animations, AnimationClipIR{
		Name:     name,
		Duration: clip.Duration,
		Channels: channels,
	})
}

func (l *graphLowerer) resolveLabel(item pendingLabel) (LabelIR, bool) {
	text := strings.TrimSpace(item.label.Text)
	if text == "" {
		return LabelIR{}, false
	}
	position := l.resolveLabelPosition(item)
	return LabelIR{
		ID:          l.nextSceneLabelID(item.label.ID),
		Text:        text,
		ClassName:   strings.TrimSpace(item.label.ClassName),
		X:           position.X,
		Y:           position.Y,
		Z:           position.Z,
		Priority:    item.label.Priority,
		ShiftX:      item.label.Shift.X,
		ShiftY:      item.label.Shift.Y,
		ShiftZ:      item.label.Shift.Z,
		DriftSpeed:  item.label.DriftSpeed,
		DriftPhase:  item.label.DriftPhase,
		MaxWidth:    item.label.MaxWidth,
		MaxLines:    item.label.MaxLines,
		Overflow:    strings.TrimSpace(item.label.Overflow),
		Font:        strings.TrimSpace(item.label.Font),
		LineHeight:  item.label.LineHeight,
		Color:       strings.TrimSpace(item.label.Color),
		Background:  strings.TrimSpace(item.label.Background),
		BorderColor: strings.TrimSpace(item.label.BorderColor),
		OffsetX:     item.label.OffsetX,
		OffsetY:     item.label.OffsetY,
		AnchorX:     item.label.AnchorX,
		AnchorY:     item.label.AnchorY,
		Collision:   strings.TrimSpace(item.label.Collision),
		Occlude:     item.label.Occlude,
		WhiteSpace:  strings.TrimSpace(item.label.WhiteSpace),
		TextAlign:   strings.TrimSpace(item.label.TextAlign),
		Transition:  lowerTransition(item.label.Transition),
		InState:     item.label.InState.legacyProps(),
		OutState:    item.label.OutState.legacyProps(),
		Live:        normalizeLive(item.label.Live),
	}, true
}

func (l *graphLowerer) resolveSprite(item pendingSprite) (SpriteIR, bool) {
	src := strings.TrimSpace(item.sprite.Src)
	if src == "" {
		return SpriteIR{}, false
	}
	position := l.resolveAnchoredPosition(item.parent, item.sprite.Target, item.sprite.Position)
	return SpriteIR{
		ID:         l.nextSceneSpriteID(item.sprite.ID),
		Src:        src,
		ClassName:  strings.TrimSpace(item.sprite.ClassName),
		X:          position.X,
		Y:          position.Y,
		Z:          position.Z,
		Priority:   item.sprite.Priority,
		ShiftX:     item.sprite.Shift.X,
		ShiftY:     item.sprite.Shift.Y,
		ShiftZ:     item.sprite.Shift.Z,
		DriftSpeed: item.sprite.DriftSpeed,
		DriftPhase: item.sprite.DriftPhase,
		Width:      item.sprite.Width,
		Height:     item.sprite.Height,
		Scale:      item.sprite.Scale,
		Opacity:    item.sprite.Opacity,
		OffsetX:    item.sprite.OffsetX,
		OffsetY:    item.sprite.OffsetY,
		AnchorX:    item.sprite.AnchorX,
		AnchorY:    item.sprite.AnchorY,
		Occlude:    item.sprite.Occlude,
		Fit:        strings.TrimSpace(item.sprite.Fit),
		Transition: lowerTransition(item.sprite.Transition),
		InState:    item.sprite.InState.legacyProps(),
		OutState:   item.sprite.OutState.legacyProps(),
		Live:       normalizeLive(item.sprite.Live),
	}, true
}

func (l *graphLowerer) resolveLabelPosition(item pendingLabel) Vector3 {
	return l.resolveAnchoredPosition(item.parent, item.label.Target, item.label.Position)
}

func (l *graphLowerer) resolveAnchoredPosition(parent worldTransform, rawTarget string, localPosition Vector3) Vector3 {
	position := combineTransforms(parent, localTransform(localPosition, Euler{})).Position
	target := strings.TrimSpace(rawTarget)
	if target == "" {
		return position
	}
	anchor, ok := l.anchors[target]
	if !ok {
		return position
	}
	return addVectors(anchor.Position, anchor.Rotation.rotate(localPosition))
}

func (l *graphLowerer) nextSceneLabelID(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	l.nextLabelID += 1
	return "scene-label-" + intString(l.nextLabelID)
}

func (l *graphLowerer) nextSceneSpriteID(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	l.nextSpriteID += 1
	return "scene-sprite-" + intString(l.nextSpriteID)
}

func (l *graphLowerer) nextSceneLightID(prefix, raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	l.nextLightID += 1
	return prefix + "-" + intString(l.nextLightID)
}

func (l *graphLowerer) nextSceneModelID(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	l.nextModelID += 1
	return "scene-model-" + intString(l.nextModelID)
}

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
	if material == nil {
		return
	}
	switch m := material.(type) {
	case FlatMaterial:
		applyMaterialStyleToObjectIR(record, MaterialFlat, MaterialStyle(m))
	case GhostMaterial:
		applyMaterialStyleToObjectIR(record, MaterialGhost, MaterialStyle(m))
	case GlassMaterial:
		applyMaterialStyleToObjectIR(record, MaterialGlass, MaterialStyle(m))
	case GlowMaterial:
		applyMaterialStyleToObjectIR(record, MaterialGlow, MaterialStyle(m))
	case MatteMaterial:
		applyMaterialStyleToObjectIR(record, MaterialMatte, MaterialStyle(m))
	case StandardMaterial:
		record.MaterialKind = "standard"
		record.Color = strings.TrimSpace(m.Color)
		record.Roughness = m.Roughness
		record.Metalness = m.Metalness
		record.Clearcoat = m.Clearcoat
		record.Sheen = m.Sheen
		record.Transmission = m.Transmission
		record.Iridescence = m.Iridescence
		record.Anisotropy = m.Anisotropy
		record.NormalMap = strings.TrimSpace(m.NormalMap)
		record.RoughnessMap = strings.TrimSpace(m.RoughnessMap)
		record.MetalnessMap = strings.TrimSpace(m.MetalnessMap)
		record.EmissiveMap = strings.TrimSpace(m.EmissiveMap)
		if m.Emissive != 0 {
			record.Emissive = Float(m.Emissive)
		}
		if m.Opacity != nil {
			record.Opacity = m.Opacity
		}
		if m.BlendMode != "" {
			record.BlendMode = string(m.BlendMode)
		}
		if m.Wireframe != nil {
			record.Wireframe = m.Wireframe
		}
	case LineBasicMaterial:
		applyMaterialStyleToObjectIR(record, "line-basic", m.MaterialStyle)
		if m.Width > 0 {
			record.LineWidth = m.Width
		}
	case LineDashedMaterial:
		applyMaterialStyleToObjectIR(record, "line-dashed", m.MaterialStyle)
		record.LineDash = Bool(true)
		if m.Width > 0 {
			record.LineWidth = m.Width
		}
		record.DashSize = m.DashSize
		record.GapSize = m.GapSize
	case CustomMaterial:
		record.MaterialKind = "custom"
		record.Color = strings.TrimSpace(m.Color)
		record.Roughness = m.Roughness
		record.Metalness = m.Metalness
		record.Clearcoat = m.Clearcoat
		record.Sheen = m.Sheen
		record.Transmission = m.Transmission
		record.Iridescence = m.Iridescence
		record.Anisotropy = m.Anisotropy
		record.NormalMap = strings.TrimSpace(m.NormalMap)
		record.RoughnessMap = strings.TrimSpace(m.RoughnessMap)
		record.MetalnessMap = strings.TrimSpace(m.MetalnessMap)
		record.EmissiveMap = strings.TrimSpace(m.EmissiveMap)
		if m.Emissive != 0 {
			record.Emissive = Float(m.Emissive)
		}
		if m.Opacity != nil {
			record.Opacity = m.Opacity
		}
		if m.BlendMode != "" {
			record.BlendMode = string(m.BlendMode)
		}
		if m.Wireframe != nil {
			record.Wireframe = m.Wireframe
		}
		record.CustomVertex = strings.TrimSpace(m.VertexGLSL)
		record.CustomFragment = strings.TrimSpace(m.FragmentGLSL)
		record.CustomVertexWGSL = strings.TrimSpace(m.VertexWGSL)
		record.CustomFragmentWGSL = strings.TrimSpace(m.FragmentWGSL)
		record.CustomUniforms = cloneSceneAnyMap(m.Uniforms)
	default:
		// Fallback for any Material implementation not enumerated above —
		// use the legacy map round-trip so correctness is preserved.
		applyMaterialProps(record, material.legacyMaterial())
	}
}

func applyMaterialStyleToObjectIR(record *ObjectIR, kind MaterialKind, style MaterialStyle) {
	record.MaterialKind = string(kind)
	if style.Color != "" {
		record.Color = strings.TrimSpace(style.Color)
	}
	if style.Texture != "" {
		record.Texture = strings.TrimSpace(style.Texture)
	}
	if style.Opacity != nil {
		record.Opacity = style.Opacity
	}
	if style.Emissive != nil {
		record.Emissive = style.Emissive
	}
	if style.BlendMode != "" {
		record.BlendMode = string(style.BlendMode)
	}
	if style.RenderPass != "" {
		record.RenderPass = string(style.RenderPass)
	}
	if style.Wireframe != nil {
		record.Wireframe = style.Wireframe
	}
}

func (m FlatMaterial) legacyMaterial() map[string]any {
	return legacySceneMaterial(MaterialFlat, MaterialStyle(m))
}

func (m GhostMaterial) legacyMaterial() map[string]any {
	return legacySceneMaterial(MaterialGhost, MaterialStyle(m))
}

func (m GlassMaterial) legacyMaterial() map[string]any {
	return legacySceneMaterial(MaterialGlass, MaterialStyle(m))
}

func (m GlowMaterial) legacyMaterial() map[string]any {
	return legacySceneMaterial(MaterialGlow, MaterialStyle(m))
}

func (m MatteMaterial) legacyMaterial() map[string]any {
	return legacySceneMaterial(MaterialMatte, MaterialStyle(m))
}

func (m StandardMaterial) legacyMaterial() map[string]any {
	out := map[string]any{}
	setString(out, "materialKind", "standard")
	setString(out, "color", m.Color)
	setNumeric(out, "roughness", m.Roughness)
	setNumeric(out, "metalness", m.Metalness)
	setNumeric(out, "clearcoat", m.Clearcoat)
	setNumeric(out, "sheen", m.Sheen)
	setNumeric(out, "transmission", m.Transmission)
	setNumeric(out, "iridescence", m.Iridescence)
	setNumeric(out, "anisotropy", m.Anisotropy)
	setString(out, "normalMap", m.NormalMap)
	setString(out, "roughnessMap", m.RoughnessMap)
	setString(out, "metalnessMap", m.MetalnessMap)
	setString(out, "emissiveMap", m.EmissiveMap)
	setNumeric(out, "emissive", m.Emissive)
	setNumericPtr(out, "opacity", m.Opacity)
	setString(out, "blendMode", string(m.BlendMode))
	setBool(out, "wireframe", m.Wireframe)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m LineBasicMaterial) legacyMaterial() map[string]any {
	out := legacySceneMaterial("line-basic", m.MaterialStyle)
	if out == nil {
		out = map[string]any{}
	}
	setNumeric(out, "lineWidth", m.Width)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m LineDashedMaterial) legacyMaterial() map[string]any {
	out := legacySceneMaterial("line-dashed", m.MaterialStyle)
	if out == nil {
		out = map[string]any{}
	}
	out["lineDash"] = true
	setNumeric(out, "lineWidth", m.Width)
	setNumeric(out, "dashSize", m.DashSize)
	setNumeric(out, "gapSize", m.GapSize)
	return out
}

func (m CustomMaterial) legacyMaterial() map[string]any {
	out := StandardMaterial(m.StandardMaterial).legacyMaterial()
	if out == nil {
		out = map[string]any{}
	}
	out["materialKind"] = "custom"
	setString(out, "customVertex", m.VertexGLSL)
	setString(out, "customFragment", m.FragmentGLSL)
	setString(out, "customVertexWGSL", m.VertexWGSL)
	setString(out, "customFragmentWGSL", m.FragmentWGSL)
	if len(m.Uniforms) > 0 {
		out["customUniforms"] = cloneSceneAnyMap(m.Uniforms)
	}
	return out
}

func legacySceneMaterial(kind MaterialKind, style MaterialStyle) map[string]any {
	out := map[string]any{}
	setString(out, "materialKind", string(kind))
	setString(out, "color", style.Color)
	setString(out, "texture", style.Texture)
	setNumericPtr(out, "opacity", style.Opacity)
	setNumericPtr(out, "emissive", style.Emissive)
	setString(out, "blendMode", string(style.BlendMode))
	setString(out, "renderPass", string(style.RenderPass))
	setBool(out, "wireframe", style.Wireframe)
	if len(out) == 0 {
		return nil
	}
	return out
}

func localTransform(position Vector3, rotation Euler) worldTransform {
	return worldTransform{
		Position: position,
		Rotation: quaternionFromEuler(rotation),
	}
}

func identityTransform() worldTransform {
	return worldTransform{
		Rotation: quaternion{W: 1},
	}
}

func combineTransforms(parent, local worldTransform) worldTransform {
	return worldTransform{
		Position: addVectors(parent.Position, parent.Rotation.rotate(local.Position)),
		Rotation: parent.Rotation.mul(local.Rotation).normalized(),
	}
}

func addVectors(left, right Vector3) Vector3 {
	return Vector3{
		X: left.X + right.X,
		Y: left.Y + right.Y,
		Z: left.Z + right.Z,
	}
}

func quaternionFromEuler(rotation Euler) quaternion {
	qx := axisAngleQuaternion(Vector3{X: 1}, rotation.X)
	qy := axisAngleQuaternion(Vector3{Y: 1}, rotation.Y)
	qz := axisAngleQuaternion(Vector3{Z: 1}, rotation.Z)
	return qz.mul(qy).mul(qx).normalized()
}

func axisAngleQuaternion(axis Vector3, angle float64) quaternion {
	if angle == 0 {
		return quaternion{W: 1}
	}
	half := angle / 2
	sine := math.Sin(half)
	return quaternion{
		X: axis.X * sine,
		Y: axis.Y * sine,
		Z: axis.Z * sine,
		W: math.Cos(half),
	}
}

func (q quaternion) normalized() quaternion {
	length := math.Sqrt((q.X * q.X) + (q.Y * q.Y) + (q.Z * q.Z) + (q.W * q.W))
	if length == 0 {
		return quaternion{W: 1}
	}
	return quaternion{
		X: q.X / length,
		Y: q.Y / length,
		Z: q.Z / length,
		W: q.W / length,
	}
}

func (q quaternion) conjugate() quaternion {
	return quaternion{X: -q.X, Y: -q.Y, Z: -q.Z, W: q.W}
}

func (q quaternion) mul(other quaternion) quaternion {
	return quaternion{
		X: (q.W * other.X) + (q.X * other.W) + (q.Y * other.Z) - (q.Z * other.Y),
		Y: (q.W * other.Y) - (q.X * other.Z) + (q.Y * other.W) + (q.Z * other.X),
		Z: (q.W * other.Z) + (q.X * other.Y) - (q.Y * other.X) + (q.Z * other.W),
		W: (q.W * other.W) - (q.X * other.X) - (q.Y * other.Y) - (q.Z * other.Z),
	}
}

func (q quaternion) rotate(vector Vector3) Vector3 {
	point := quaternion{X: vector.X, Y: vector.Y, Z: vector.Z}
	result := q.mul(point).mul(q.conjugate())
	return Vector3{X: result.X, Y: result.Y, Z: result.Z}
}

func eulerFromQuaternion(q quaternion) Euler {
	q = q.normalized()
	m00 := 1 - (2 * ((q.Y * q.Y) + (q.Z * q.Z)))
	m10 := 2 * ((q.X * q.Y) + (q.Z * q.W))
	m20 := 2 * ((q.X * q.Z) - (q.Y * q.W))
	m21 := 2 * ((q.Y * q.Z) + (q.X * q.W))
	m22 := 1 - (2 * ((q.X * q.X) + (q.Y * q.Y)))
	m01 := 2 * ((q.X * q.Y) - (q.Z * q.W))
	m11 := 1 - (2 * ((q.X * q.X) + (q.Z * q.Z)))

	y := math.Asin(clamp(-m20, -1, 1))
	cosY := math.Cos(y)
	if math.Abs(cosY) > 1e-9 {
		return Euler{
			X: math.Atan2(m21, m22),
			Y: y,
			Z: math.Atan2(m10, m00),
		}
	}
	return Euler{
		X: 0,
		Y: y,
		Z: math.Atan2(-m01, m11),
	}
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func setString(target map[string]any, name, value string) {
	value = strings.TrimSpace(value)
	if target == nil || name == "" || value == "" {
		return
	}
	target[name] = value
}

func setInt(target map[string]any, name string, value int) {
	if target == nil || name == "" || value == 0 {
		return
	}
	target[name] = value
}

func setBool(target map[string]any, name string, value *bool) {
	if target == nil || name == "" || value == nil {
		return
	}
	target[name] = *value
}

func setNumeric(target map[string]any, name string, value float64) {
	if target == nil || name == "" || value == 0 {
		return
	}
	target[name] = value
}

func setNumericPtr(target map[string]any, name string, value *float64) {
	if target == nil || name == "" || value == nil {
		return
	}
	target[name] = *value
}

func mapFloat64(value any) float64 {
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	case int64:
		return float64(current)
	case int32:
		return float64(current)
	default:
		return 0
	}
}

func mapFloat64OK(value any) (float64, bool) {
	switch current := value.(type) {
	case float64:
		return current, true
	case float32:
		return float64(current), true
	case int:
		return float64(current), true
	case int64:
		return float64(current), true
	case int32:
		return float64(current), true
	default:
		return 0, false
	}
}

func mapStringValue(value any) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func mapBool(value any) (bool, bool) {
	current, ok := value.(bool)
	if !ok {
		return false, false
	}
	return current, true
}

func mapInt(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case int32:
		return int(current)
	case float64:
		return int(current)
	case float32:
		return int(current)
	default:
		return 0
	}
}

func legacyLinePoints(points []Vector3) []map[string]any {
	if len(points) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(points))
	for _, point := range points {
		out = append(out, map[string]any{
			"x": point.X,
			"y": point.Y,
			"z": point.Z,
		})
	}
	return out
}

func legacyLineSegments(segments [][2]int) [][2]int {
	if len(segments) == 0 {
		return nil
	}
	out := make([][2]int, 0, len(segments))
	for _, segment := range segments {
		if segment[0] < 0 || segment[1] < 0 || segment[0] == segment[1] {
			continue
		}
		out = append(out, segment)
	}
	return out
}

func mapVector3List(value any) []Vector3 {
	switch current := value.(type) {
	case []Vector3:
		if len(current) == 0 {
			return nil
		}
		return append([]Vector3(nil), current...)
	case []map[string]any:
		out := make([]Vector3, 0, len(current))
		for _, item := range current {
			out = append(out, Vector3{
				X: mapFloat64(item["x"]),
				Y: mapFloat64(item["y"]),
				Z: mapFloat64(item["z"]),
			})
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]Vector3, 0, len(current))
		for _, item := range current {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, Vector3{
				X: mapFloat64(entry["x"]),
				Y: mapFloat64(entry["y"]),
				Z: mapFloat64(entry["z"]),
			})
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func mapSegmentPairs(value any) [][2]int {
	switch current := value.(type) {
	case [][2]int:
		if len(current) == 0 {
			return nil
		}
		return append([][2]int(nil), current...)
	case []any:
		out := make([][2]int, 0, len(current))
		for _, item := range current {
			pair, ok := item.([]any)
			if ok && len(pair) >= 2 {
				left := mapInt(pair[0])
				right := mapInt(pair[1])
				if left >= 0 && right >= 0 && left != right {
					out = append(out, [2]int{left, right})
				}
				continue
			}
			typed, ok := item.([2]int)
			if ok && typed[0] >= 0 && typed[1] >= 0 && typed[0] != typed[1] {
				out = append(out, typed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func intString(value int) string {
	// Delegate to strconv.Itoa: the hand-rolled digit builder this used
	// to be allocated a new []byte per digit (via `append([]byte{...},
	// digits...)` which is a fresh slice header + grow each iteration).
	// strconv.Itoa uses a stack-allocated 20-byte scratch buffer and
	// returns a single string — zero heap allocations for non-negative
	// values.
	return strconv.Itoa(value)
}

// resolveMaxPixels normalizes the shadow cap for IR emission. Zero or
// negative values become the default 1024×1024 cap; positive values pass
// through.
func (s Shadows) resolveMaxPixels() int {
	if s.MaxPixels <= 0 {
		return ShadowMaxPixels1024
	}
	return s.MaxPixels
}

func normalizeShadowCascades(cascades int) int {
	if cascades == 0 {
		return 0
	}
	if cascades < 1 {
		return 1
	}
	if cascades > 4 {
		return 4
	}
	return cascades
}

func normalizeShadowSoftness(softness float64) float64 {
	if softness <= 0 || math.IsNaN(softness) || math.IsInf(softness, 0) {
		return 0
	}
	return softness
}
