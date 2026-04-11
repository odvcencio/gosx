package scene

import (
	"encoding/json"
	"math"
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
	Position Vector3
	Rotation Euler
	FOV      float64
	Near     float64
	Far      float64
}

// Environment describes scene-wide ambient and hemisphere lighting.
type Environment struct {
	AmbientColor     string
	AmbientIntensity float64
	SkyColor         string
	SkyIntensity     float64
	GroundColor      string
	GroundIntensity  float64
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
	PreferCanvas         *bool        `json:"preferCanvas,omitempty"`
	DragToRotate         *bool        `json:"dragToRotate,omitempty"`
	DragSignalNamespace  string       `json:"dragSignalNamespace,omitempty"`
	PickSignalNamespace  string       `json:"pickSignalNamespace,omitempty"`
	EventSignalNamespace string       `json:"eventSignalNamespace,omitempty"`
	CapabilityTier       string       `json:"capabilityTier,omitempty"`
	Compression          *Compression `json:"compression,omitempty"`
	ControlTarget        Vector3
	ControlRotateSpeed   float64 `json:"controlRotateSpeed,omitempty"`
	ControlZoomSpeed     float64 `json:"controlZoomSpeed,omitempty"`
	ScrollCameraStart    float64 `json:"scrollCameraStart,omitempty"`
	ScrollCameraEnd      float64 `json:"scrollCameraEnd,omitempty"`
	MaxDevicePixelRatio  float64 `json:"maxDevicePixelRatio,omitempty"`
	Camera               PerspectiveCamera
	Environment          Environment
	PostFX               PostFX
	Shadows              Shadows
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
	CastShadow    bool
	ReceiveShadow bool
	DepthWrite    *bool // nil = default (true), false = no depth writes
	Spin          Euler
	Drift         Vector3
	DriftSpeed    float64
	DriftPhase    float64
	Transition    Transition
	InState       *MeshProps
	OutState      *MeshProps
	Live          []string
	Children      []Node
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
	CastShadow    bool
	ReceiveShadow bool
	Transition    Transition
	InState       *InstancedMeshProps
	OutState      *InstancedMeshProps
	Live          []string
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
	ID         string
	Color      string
	Intensity  float64
	Direction  Vector3
	CastShadow bool
	ShadowBias float64
	ShadowSize int
	Transition Transition
	InState    *LightProps
	OutState   *LightProps
	Live       []string
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
	ID         string
	Color      string
	Intensity  float64
	Position   Vector3
	Direction  Vector3
	Angle      float64 // outer cone angle in radians
	Penumbra   float64 // 0 = hard edge, 1 = fully soft
	Range      float64
	Decay      float64
	CastShadow bool
	ShadowBias float64
	ShadowSize int
	Transition Transition
	InState    *LightProps
	OutState   *LightProps
	Live       []string
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
	objects          []ObjectIR
	models           []ModelIR
	points           []PointsIR
	instancedMeshes  []InstancedMeshIR
	computeParticles []ComputeParticlesIR
	animations       []AnimationClipIR
	pending          []pendingLabel
	pendingSprites   []pendingSprite
	lights           []LightIR
	anchors          map[string]worldTransform
	nextObjectID     int
	nextLabelID      int
	nextSpriteID     int
	nextLightID      int
	nextModelID      int
	nextPointsID     int
	nextInstancedID  int
	nextParticlesID  int
}

func (Group) sceneNode()            {}
func (Mesh) sceneNode()             {}
func (Points) sceneNode()           {}
func (InstancedMesh) sceneNode()    {}
func (ComputeParticles) sceneNode() {}
func (Label) sceneNode()            {}
func (Sprite) sceneNode()           {}
func (Model) sceneNode()            {}
func (AmbientLight) sceneNode()     {}
func (DirectionalLight) sceneNode() {}
func (PointLight) sceneNode()       {}
func (SpotLight) sceneNode()        {}
func (HemisphereLight) sceneNode()  {}
func (AnimationClip) sceneNode()    {}

func (CubeGeometry) sceneGeometry()     {}
func (BoxGeometry) sceneGeometry()      {}
func (PlaneGeometry) sceneGeometry()    {}
func (PyramidGeometry) sceneGeometry()  {}
func (SphereGeometry) sceneGeometry()   {}
func (LinesGeometry) sceneGeometry()    {}
func (CylinderGeometry) sceneGeometry() {}
func (TorusGeometry) sceneGeometry()    {}

func (FlatMaterial) sceneMaterial()     {}
func (GhostMaterial) sceneMaterial()    {}
func (GlassMaterial) sceneMaterial()    {}
func (GlowMaterial) sceneMaterial()     {}
func (MatteMaterial) sceneMaterial()    {}
func (StandardMaterial) sceneMaterial() {}

// Bool allocates a bool for opt-in Scene3D flags.
func Bool(value bool) *bool {
	return &value
}

// Float allocates a float64 for optional Scene3D numeric fields.
func Float(value float64) *float64 {
	return &value
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
func (p Props) GoSXSpreadProps() map[string]any {
	values := p.LegacyProps()
	if ref := strings.TrimSpace(p.ProgramRef); ref != "" {
		values["programRef"] = ref
	}
	values["capabilities"] = p.EngineCapabilities()
	return values
}

// LegacyProps lowers typed scene props into the current Scene3D prop bag.
func (p Props) LegacyProps() map[string]any {
	out := p.legacyBaseProps()
	if scene := p.SceneIR().legacyProps(); len(scene) > 0 {
		out["scene"] = scene
	}
	return out
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
	setBool(out, "preferCanvas", p.PreferCanvas)
	setBool(out, "dragToRotate", p.DragToRotate)
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
	setNumeric(out, "scrollCameraStart", p.ScrollCameraStart)
	setNumeric(out, "scrollCameraEnd", p.ScrollCameraEnd)
	setNumeric(out, "maxDevicePixelRatio", p.MaxDevicePixelRatio)
	if p.Compression != nil {
		comp := map[string]any{"bitWidth": p.Compression.BitWidth}
		if p.Compression.Progressive {
			comp["progressive"] = true
			bw := p.Compression.PreviewBitWidth
			if bw <= 0 {
				bw = 2
			}
			comp["previewBitWidth"] = bw
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
	if !p.Camera.isZero() {
		out["camera"] = p.Camera.legacyProps()
	}
	return out
}

// MarshalJSON encodes only runtime props. Engine transport fields such as
// ProgramRef and Capabilities stay outside the JSON payload.
func (p Props) MarshalJSON() ([]byte, error) {
	values := p.LegacyProps()
	if len(values) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(values)
}

// RawPropsJSON returns engine.Config-compatible runtime props.
func (p Props) RawPropsJSON() json.RawMessage {
	values := p.LegacyProps()
	if len(values) == 0 {
		return nil
	}
	data, err := json.Marshal(values)
	if err != nil {
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
	for _, capability := range p.Capabilities {
		appendCapability(capability)
	}
	return out
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
	return out
}

func (c PerspectiveCamera) isZero() bool {
	return c.Position == (Vector3{}) && c.Rotation == (Euler{}) && c.FOV == 0 && c.Near == 0 && c.Far == 0
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
	case AnimationClip:
		l.lowerAnimationClip(current)
	case *AnimationClip:
		if current != nil {
			l.lowerAnimationClip(*current)
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

func (l *graphLowerer) lowerMesh(mesh Mesh, parent worldTransform) {
	world := combineTransforms(parent, localTransform(mesh.Position, mesh.Rotation))
	id := strings.TrimSpace(mesh.ID)
	if id == "" {
		l.nextObjectID += 1
		id = "scene-object-" + intString(l.nextObjectID)
	}
	kind, geometryProps := legacyGeometry(mesh.Geometry)
	record := ObjectIR{
		ID:   id,
		Kind: kind,
	}
	applyGeometryProps(&record, geometryProps)
	applyMaterialProps(&record, legacyMaterial(mesh.Material))
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
		ID:         l.nextSceneLightID("directional-light", light.ID),
		Kind:       "directional",
		Color:      strings.TrimSpace(light.Color),
		Intensity:  light.Intensity,
		DirectionX: direction.X,
		DirectionY: direction.Y,
		DirectionZ: direction.Z,
		CastShadow: light.CastShadow,
		ShadowBias: light.ShadowBias,
		ShadowSize: light.ShadowSize,
		Transition: lowerTransition(light.Transition),
		InState:    light.InState.legacyProps(),
		OutState:   light.OutState.legacyProps(),
		Live:       normalizeLive(light.Live),
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
		ID:         l.nextSceneLightID("spot-light", light.ID),
		Kind:       "spot",
		Color:      strings.TrimSpace(light.Color),
		Intensity:  light.Intensity,
		X:          world.Position.X,
		Y:          world.Position.Y,
		Z:          world.Position.Z,
		DirectionX: direction.X,
		DirectionY: direction.Y,
		DirectionZ: direction.Z,
		Angle:      light.Angle,
		Penumbra:   light.Penumbra,
		Range:      light.Range,
		Decay:      light.Decay,
		CastShadow: light.CastShadow,
		ShadowBias: light.ShadowBias,
		ShadowSize: light.ShadowSize,
		Transition: lowerTransition(light.Transition),
		InState:    light.InState.legacyProps(),
		OutState:   light.OutState.legacyProps(),
		Live:       normalizeLive(light.Live),
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
	record.Roughness = mapFloat64(props["roughness"])
	record.Metalness = mapFloat64(props["metalness"])
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
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + (value % 10))}, digits...)
		value /= 10
	}
	return string(digits)
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
