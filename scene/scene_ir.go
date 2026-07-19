package scene

import (
	"encoding/json"
	"strings"

	"m31labs.dev/gosx/motion"
	"m31labs.dev/gosx/scene/capability"
)

// SceneIRSchema identifies the compatibility SceneIR payload shape when it is
// emitted outside a same-version GoSX server/runtime bundle.
const SceneIRSchema = "gosx.scene3d.ir.v1"

// CompressedArray holds a TurboQuant-compressed float array.
// The client checks for compressed fields first and falls back to raw arrays.
type CompressedArray struct {
	Packed   []byte  `json:"packed"`
	Norm     float32 `json:"norm"`   // min value (scalar quantization floor)
	MaxVal   float32 `json:"maxVal"` // max value (scalar quantization ceiling); MUST NOT be omitempty — an all-zero lane (e.g. a mat4 off-diagonal lane) has maxVal=0, and omitting it makes the JS dequantizer read undefined → step=NaN → NaN transforms.
	Dim      int     `json:"dim"`
	BitWidth int     `json:"bitWidth"`
	Count    int     `json:"count"` // number of original float64 values
}

// SceneIR is the typed lowered scene payload emitted from a Graph before it is
// serialized into the current Scene3D compatibility contract.
//
// Historically serialized via legacyProps → map[string]any → json.Marshal,
// which cost ~900 interface-boxing allocations per scene marshal. Since
// every field (including the interface-typed PostEffects — each concrete
// post-effect type implements json.Marshaler) now has proper json tags,
// reflection-based json.Marshal(sceneIR) produces the same wire shape
// directly, and Props.MarshalJSON takes that fast path.
//
// ShaderLib carries deduplicated large shader strings hoisted from
// computeParticles[].computeWGSL and objects[].custom*WGSL/customVertex/
// customFragment when the same source appears ≥2 times in the scene.
// The inline field is replaced with a sibling *Ref field (e.g.
// computeWGSLRef:"sl:..."). The JS hydrate path inflates refs back before
// downstream renderers see them.
type SceneIR struct {
	Schema             string               `json:"schema,omitempty"`
	Objects            []ObjectIR           `json:"objects,omitempty"`
	Models             []ModelIR            `json:"models,omitempty"`
	Points             []PointsIR           `json:"points,omitempty"`
	InstancedMeshes    []InstancedMeshIR    `json:"instancedMeshes,omitempty"`
	InstancedGLBMeshes []InstancedGLBMeshIR `json:"instancedGLBMeshes,omitempty"`
	ComputeParticles   []ComputeParticlesIR `json:"computeParticles,omitempty"`
	WaterSystems       []WaterSystemIR      `json:"waterSystems,omitempty"`
	Animations         []AnimationClipIR    `json:"animations,omitempty"`
	Labels             []LabelIR            `json:"labels,omitempty"`
	Sprites            []SpriteIR           `json:"sprites,omitempty"`
	HTML               []HTMLIR             `json:"html,omitempty"`
	Lights             []LightIR            `json:"lights,omitempty"`
	Environment        EnvironmentIR        `json:"environment,omitzero"`
	PostEffects        []PostEffectIR       `json:"postEffects,omitempty"`
	RenderGraph        *RenderGraphIR       `json:"renderGraph,omitempty"`
	PostFXMaxPixels    int                  `json:"postFXMaxPixels,omitempty"`
	ShadowMaxPixels    int                  `json:"shadowMaxPixels,omitempty"`
	// QualityLadder / QualityStartRung: see Props.QualityLadder and
	// Props.QualityStartRung (scene/quality_ladder.go). Omitted (nil/zero)
	// when no ladder is authored — the client governor's legacy dprCap-tier
	// adaptiveQuality behavior is unchanged in that case.
	QualityLadder    []QualityRungIR `json:"qualityLadder,omitempty"`
	QualityStartRung int             `json:"qualityStartRung,omitempty"`
	// PointQualityGroups: see Props.PointQualityGroups (scene/quality_ladder.go
	// doc / scene.go). Maps a runtime-extracted points-layer name (e.g. a
	// GLB-baked layer) to a QualityRung.LayerGroups tag, for point layers
	// that cannot carry PointsIR.QualityGroup directly.
	PointQualityGroups map[string]string `json:"pointQualityGroups,omitempty"`
	// BackendCaps is the honesty-gate verdict: which rendering backends can
	// faithfully render this scene, plus per-backend feature degradations. It
	// is computed once during Props.SceneIR() and ships to the JS runtime.
	BackendCaps *capability.BackendCaps `json:"backendCaps,omitempty"`
	// ShaderLib holds deduplicated shader sources hoisted from inline node
	// fields (computeWGSL, customVertex, customFragment, etc.) when the same
	// source appears ≥2 times. Keys are "sl:<16hex>" content hashes.
	// The JS hydrate path inflates refs back to inline fields before any
	// downstream renderer sees them. Native renderers receive kernels via
	// Config override and may ignore this field.
	ShaderLib map[string]string `json:"shaderLib,omitempty"`
	// SpinTracks holds one GenSpin MotionIR Track for every mesh/points node
	// that has a non-zero Spin. This field is populated at lowering time as an
	// in-memory facade over motion core; it is NOT serialised to the wire (JSON
	// serialisation of MotionIR tracks is deferred to Task 1.15+).
	SpinTracks []motion.Track `json:"-"`
	// MotionProgram carries the binary-encoded motion.Timeline for all spin
	// tracks in the scene. It is produced by Graph.SceneIR() from SpinTracks and
	// serialised as base64 in JSON (Go []byte → base64 automatically). Omitted
	// when there is no motion. The JS runtime can call motionLoad() with this
	// payload to drive object rotations.
	MotionProgram []byte `json:"motionProgram,omitempty"`
	// MaterialTracks holds one keyframe MotionIR Track per mesh material-uniform
	// animation (Target.Kind == TargetMaterial, Ref == mesh id). Populated at
	// lowering time as an in-memory facade; NOT serialised to the wire (the
	// encoded form ships via MaterialMotionProgram).
	MaterialTracks []motion.Track `json:"-"`
	// MaterialMotionProgram carries the binary-encoded motion.Timeline for all
	// material-uniform animation tracks in the scene. It is produced by
	// Graph.SceneIR() from MaterialTracks and serialised as base64 in JSON.
	// Omitted when there are no material animations. This is SEPARATE from
	// MotionProgram (transforms) so material packets route independently in the
	// JS runtime.
	MaterialMotionProgram []byte `json:"materialMotionProgram,omitempty"`
}

// RenderGraphIR is the backend-neutral retained render/resource schedule.
// Authoring tools may retain richer editing data, but runtime and headless
// consumers share this exact ordered plan.
type RenderGraphIR struct {
	Resources   []RenderResourceIR   `json:"resources"`
	Passes      []RenderGraphPassIR  `json:"passes"`
	Allocations []RenderAllocationIR `json:"allocations,omitempty"`
}

type RenderResourceIR struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Ownership string `json:"ownership"`
	Format    string `json:"format,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"`
}

type RenderGraphPassIR struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Reads   []string `json:"reads,omitempty"`
	Writes  []string `json:"writes,omitempty"`
	Depends []string `json:"depends,omitempty"`
}

type RenderAllocationIR struct {
	Resource string `json:"resource"`
	Slot     int    `json:"slot"`
	FirstUse int    `json:"firstUse"`
	LastUse  int    `json:"lastUse"`
}

// InteractionProfileIR declares a named client-side Scene3D interaction
// contract for runtime controls that need to coordinate picking, dragging,
// camera controls, and scene commands.
type InteractionProfileIR struct {
	Profile string `json:"profile,omitempty"`
	Target  string `json:"target,omitempty"`
	Object  string `json:"object,omitempty"`
}

// InstancedGLBMeshIR is the typed compatibility record for one GLB-backed
// instanced mesh batch — one wire node per (src, material) pair.
type InstancedGLBMeshIR struct {
	ID           string           `json:"id"`
	Src          string           `json:"src"`
	MaterialKind string           `json:"materialKind,omitempty"`
	Color        string           `json:"color,omitempty"`
	Texture      string           `json:"texture,omitempty"`
	Opacity      *float64         `json:"opacity,omitempty"`
	Emissive     *float64         `json:"emissive,omitempty"`
	BlendMode    string           `json:"blendMode,omitempty"`
	Roughness    float64          `json:"roughness,omitempty"`
	Metalness    float64          `json:"metalness,omitempty"`
	Instances    []MeshInstanceIR `json:"instances"`
	Pickable     *bool            `json:"pickable,omitempty"`
	Visible      *bool            `json:"visible,omitempty"`
	Static       *bool            `json:"static,omitempty"`
}

// MeshInstanceIR holds the per-instance transform data for InstancedGLBMeshIR.
type MeshInstanceIR struct {
	ID        string  `json:"id,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	ScaleX    float64 `json:"scaleX,omitempty"`
	ScaleY    float64 `json:"scaleY,omitempty"`
	ScaleZ    float64 `json:"scaleZ,omitempty"`
	RotationX float64 `json:"rotationX,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
	RotationZ float64 `json:"rotationZ,omitempty"`
}

// ObjectIR is the typed compatibility record for one lowered scene object.
type ObjectIR struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	Size               float64   `json:"size,omitempty"`
	Width              float64   `json:"width,omitempty"`
	Height             float64   `json:"height,omitempty"`
	Depth              float64   `json:"depth,omitempty"`
	Radius             float64   `json:"radius,omitempty"`
	Segments           int       `json:"segments,omitempty"`
	Points             []Vector3 `json:"points,omitempty"`
	LineSegments       [][2]int  `json:"lineSegments,omitempty"`
	LineWidth          float64   `json:"lineWidth,omitempty"`
	RadiusTop          float64   `json:"radiusTop,omitempty"`
	RadiusBottom       float64   `json:"radiusBottom,omitempty"`
	Tube               float64   `json:"tube,omitempty"`
	RadialSegments     int       `json:"radialSegments,omitempty"`
	TubularSegments    int       `json:"tubularSegments,omitempty"`
	MaterialKind       string    `json:"materialKind,omitempty"`
	Color              string    `json:"color,omitempty"`
	Texture            string    `json:"texture,omitempty"`
	Opacity            *float64  `json:"opacity,omitempty"`
	Emissive           *float64  `json:"emissive,omitempty"`
	BlendMode          string    `json:"blendMode,omitempty"`
	RenderPass         string    `json:"renderPass,omitempty"`
	Wireframe          *bool     `json:"wireframe,omitempty"`
	LineDash           *bool     `json:"lineDash,omitempty"`
	DashSize           float64   `json:"dashSize,omitempty"`
	GapSize            float64   `json:"gapSize,omitempty"`
	CustomVertex       string    `json:"customVertex,omitempty"`
	CustomFragment     string    `json:"customFragment,omitempty"`
	CustomVertexWGSL   string    `json:"customVertexWGSL,omitempty"`
	CustomFragmentWGSL string    `json:"customFragmentWGSL,omitempty"`
	// *Ref fields replace their counterparts when hoisted into SceneIR.ShaderLib.
	CustomVertexRef       string            `json:"customVertexRef,omitempty"`
	CustomFragmentRef     string            `json:"customFragmentRef,omitempty"`
	CustomVertexWGSLRef   string            `json:"customVertexWGSLRef,omitempty"`
	CustomFragmentWGSLRef string            `json:"customFragmentWGSLRef,omitempty"`
	CustomUniforms        map[string]any    `json:"customUniforms,omitempty"`
	ShaderBackend         string            `json:"shaderBackend,omitempty"`
	ShaderLayout          map[string]any    `json:"shaderLayout,omitempty"`
	ShaderSource          string            `json:"shaderSource,omitempty"`
	ShaderSourceFiles     map[string]string `json:"shaderSourceFiles,omitempty"`
	Pickable              *bool             `json:"pickable,omitempty"`
	Visible               *bool             `json:"visible,omitempty"`
	Selected              bool              `json:"selected,omitempty"`
	// GizmoRing marks a TransformControls rotate-mode ring helper mesh; see
	// scene.Mesh.GizmoRing and Props.GizmoInputSignal.
	GizmoRing bool `json:"gizmoRing,omitempty"`
	// GizmoHelper / GizmoFormMode: see scene.Mesh.GizmoHelper and
	// scene.Mesh.GizmoFormMode.
	GizmoHelper   bool   `json:"gizmoHelper,omitempty"`
	GizmoFormMode string `json:"gizmoFormMode,omitempty"`
	// QualityGroup: see scene.Mesh.QualityGroup and QualityRung.LayerGroups
	// (scene/quality_ladder.go). Empty means unconditionally visible.
	QualityGroup   string         `json:"qualityGroup,omitempty"`
	OutlineColor   string         `json:"outlineColor,omitempty"`
	OutlineWidth   float64        `json:"outlineWidth,omitempty"`
	CastShadow     bool           `json:"castShadow,omitempty"`
	ReceiveShadow  bool           `json:"receiveShadow,omitempty"`
	DepthWrite     *bool          `json:"depthWrite,omitempty"`
	Roughness      float64        `json:"roughness,omitempty"`
	Metalness      float64        `json:"metalness,omitempty"`
	Clearcoat      float64        `json:"clearcoat,omitempty"`
	Sheen          float64        `json:"sheen,omitempty"`
	Transmission   float64        `json:"transmission,omitempty"`
	Iridescence    float64        `json:"iridescence,omitempty"`
	Anisotropy     float64        `json:"anisotropy,omitempty"`
	NormalMap      string         `json:"normalMap,omitempty"`
	RoughnessMap   string         `json:"roughnessMap,omitempty"`
	MetalnessMap   string         `json:"metalnessMap,omitempty"`
	EmissiveMap    string         `json:"emissiveMap,omitempty"`
	LODGroup       string         `json:"lodGroup,omitempty"`
	LODLevel       int            `json:"lodLevel,omitempty"`
	LODMinDistance float64        `json:"lodMinDistance,omitempty"`
	LODMaxDistance float64        `json:"lodMaxDistance,omitempty"`
	X              float64        `json:"x,omitempty"`
	Y              float64        `json:"y,omitempty"`
	Z              float64        `json:"z,omitempty"`
	RotationX      float64        `json:"rotationX,omitempty"`
	RotationY      float64        `json:"rotationY,omitempty"`
	RotationZ      float64        `json:"rotationZ,omitempty"`
	ScaleX         float64        `json:"scaleX,omitempty"`
	ScaleY         float64        `json:"scaleY,omitempty"`
	ScaleZ         float64        `json:"scaleZ,omitempty"`
	SpinX          float64        `json:"spinX,omitempty"`
	SpinY          float64        `json:"spinY,omitempty"`
	SpinZ          float64        `json:"spinZ,omitempty"`
	ShiftX         float64        `json:"shiftX,omitempty"`
	ShiftY         float64        `json:"shiftY,omitempty"`
	ShiftZ         float64        `json:"shiftZ,omitempty"`
	DriftSpeed     float64        `json:"driftSpeed,omitempty"`
	DriftPhase     float64        `json:"driftPhase,omitempty"`
	Transition     TransitionIR   `json:"transition,omitzero"`
	InState        map[string]any `json:"inState,omitempty"`
	OutState       map[string]any `json:"outState,omitempty"`
	Live           []string       `json:"live,omitempty"`
	Vertices       *MeshVertices  `json:"vertices,omitempty"`
}

// MarshalJSON encodes ObjectIR via the standard reflection path but
// shadows the Points field so line-point coordinates always emit
// their x/y/z triple even when zero. The legacy map-based marshaling
// produced `{"x":0,"y":2,"z":0}` for a point like Vec3(0,2,0);
// Vector3's default omitempty tag would silently drop the zero
// coordinates and give `{"y":2}` instead, breaking any JS consumer
// that expects all three keys.
//
// The `type alias` trick sheds ObjectIR's MarshalJSON method so
// json.Marshal doesn't recurse infinitely, and the outer struct's
// Points field shadows the embedded alias's Points (shallower depth
// wins per encoding/json field resolution rules).
func (o ObjectIR) MarshalJSON() ([]byte, error) {
	type objectAlias ObjectIR
	return json.Marshal(struct {
		objectAlias
		Points []linePointWire `json:"points,omitempty"`
	}{
		objectAlias: objectAlias(o),
		Points:      toLinePointsWire(o.Points),
	})
}

// ModelIR is the typed compatibility record for one scene model instance.
type ModelIR struct {
	ObjectIR
	Src                string   `json:"src,omitempty"`
	ScaleX             float64  `json:"scaleX,omitempty"`
	ScaleY             float64  `json:"scaleY,omitempty"`
	ScaleZ             float64  `json:"scaleZ,omitempty"`
	Bounds             float64  `json:"bounds,omitempty"`
	Fit                string   `json:"fit,omitempty"`
	FitAlign           string   `json:"fitAlign,omitempty"`
	Static             *bool    `json:"static,omitempty"`
	Animation          string   `json:"animation,omitempty"`
	AnimationSeq       string   `json:"animationSeq,omitempty"`
	AnimationSpeed     *float64 `json:"animationSpeed,omitempty"`
	AnimationWeight    *float64 `json:"animationWeight,omitempty"`
	AnimationFadeInMS  *int     `json:"animationFadeInMS,omitempty"`
	AnimationFadeOutMS *int     `json:"animationFadeOutMS,omitempty"`
	Loop               *bool    `json:"loop,omitempty"`
}

// MarshalJSON emits both the embedded ObjectIR fields AND the ModelIR
// local fields. Without this method, Go's method promotion would dispatch
// json.Marshal(modelIR) to ObjectIR.MarshalJSON — which only emits the
// ObjectIR half and silently drops src/scale/static/model animation fields.
// The symptom is a Scene3D typed-spread test that
// expects "src" in the runtime head missing it entirely.
//
// Uses the same type-alias trick as ObjectIR.MarshalJSON: alias ObjectIR
// to shed its MarshalJSON method, then flatten both sets of fields into
// one struct literal so field shadowing gives the outer Points field
// precedence (canonical wire shape for lines).
func (m ModelIR) MarshalJSON() ([]byte, error) {
	type objectAlias ObjectIR
	type modelWire struct {
		objectAlias
		Points             []linePointWire `json:"points,omitempty"`
		Src                string          `json:"src,omitempty"`
		ScaleX             float64         `json:"scaleX,omitempty"`
		ScaleY             float64         `json:"scaleY,omitempty"`
		ScaleZ             float64         `json:"scaleZ,omitempty"`
		Bounds             float64         `json:"bounds,omitempty"`
		Fit                string          `json:"fit,omitempty"`
		FitAlign           string          `json:"fitAlign,omitempty"`
		Static             *bool           `json:"static,omitempty"`
		Animation          string          `json:"animation,omitempty"`
		AnimationSeq       string          `json:"animationSeq,omitempty"`
		AnimationSpeed     *float64        `json:"animationSpeed,omitempty"`
		AnimationWeight    *float64        `json:"animationWeight,omitempty"`
		AnimationFadeInMS  *int            `json:"animationFadeInMS,omitempty"`
		AnimationFadeOutMS *int            `json:"animationFadeOutMS,omitempty"`
		Loop               *bool           `json:"loop,omitempty"`
	}
	return json.Marshal(modelWire{
		objectAlias:        objectAlias(m.ObjectIR),
		Points:             toLinePointsWire(m.Points),
		Src:                m.Src,
		ScaleX:             m.ScaleX,
		ScaleY:             m.ScaleY,
		ScaleZ:             m.ScaleZ,
		Bounds:             m.Bounds,
		Fit:                strings.TrimSpace(m.Fit),
		FitAlign:           strings.TrimSpace(m.FitAlign),
		Static:             m.Static,
		Animation:          m.Animation,
		AnimationSeq:       m.AnimationSeq,
		AnimationSpeed:     nonNegativeFloatPtr(m.AnimationSpeed),
		AnimationWeight:    nonNegativeFloatPtr(m.AnimationWeight),
		AnimationFadeInMS:  nonNegativeIntPtr(m.AnimationFadeInMS),
		AnimationFadeOutMS: nonNegativeIntPtr(m.AnimationFadeOutMS),
		Loop:               m.Loop,
	})
}

// LabelIR is the typed compatibility record for one lowered scene label.
type LabelIR struct {
	ID          string         `json:"id"`
	Text        string         `json:"text"`
	ClassName   string         `json:"className,omitempty"`
	X           float64        `json:"x,omitempty"`
	Y           float64        `json:"y,omitempty"`
	Z           float64        `json:"z,omitempty"`
	Priority    float64        `json:"priority,omitempty"`
	ShiftX      float64        `json:"shiftX,omitempty"`
	ShiftY      float64        `json:"shiftY,omitempty"`
	ShiftZ      float64        `json:"shiftZ,omitempty"`
	DriftSpeed  float64        `json:"driftSpeed,omitempty"`
	DriftPhase  float64        `json:"driftPhase,omitempty"`
	MaxWidth    float64        `json:"maxWidth,omitempty"`
	MaxLines    int            `json:"maxLines,omitempty"`
	Overflow    string         `json:"overflow,omitempty"`
	Font        string         `json:"font,omitempty"`
	LineHeight  float64        `json:"lineHeight,omitempty"`
	Color       string         `json:"color,omitempty"`
	Background  string         `json:"background,omitempty"`
	BorderColor string         `json:"borderColor,omitempty"`
	OffsetX     float64        `json:"offsetX,omitempty"`
	OffsetY     float64        `json:"offsetY,omitempty"`
	AnchorX     float64        `json:"anchorX,omitempty"`
	AnchorY     float64        `json:"anchorY,omitempty"`
	Collision   string         `json:"collision,omitempty"`
	Occlude     bool           `json:"occlude,omitempty"`
	WhiteSpace  string         `json:"whiteSpace,omitempty"`
	TextAlign   string         `json:"textAlign,omitempty"`
	Transition  TransitionIR   `json:"transition,omitzero"`
	InState     map[string]any `json:"inState,omitempty"`
	OutState    map[string]any `json:"outState,omitempty"`
	Live        []string       `json:"live,omitempty"`
}

// SpriteIR is the typed compatibility record for one projected sprite overlay.
type SpriteIR struct {
	ID         string         `json:"id"`
	Src        string         `json:"src"`
	ClassName  string         `json:"className,omitempty"`
	X          float64        `json:"x,omitempty"`
	Y          float64        `json:"y,omitempty"`
	Z          float64        `json:"z,omitempty"`
	Priority   float64        `json:"priority,omitempty"`
	ShiftX     float64        `json:"shiftX,omitempty"`
	ShiftY     float64        `json:"shiftY,omitempty"`
	ShiftZ     float64        `json:"shiftZ,omitempty"`
	DriftSpeed float64        `json:"driftSpeed,omitempty"`
	DriftPhase float64        `json:"driftPhase,omitempty"`
	Width      float64        `json:"width,omitempty"`
	Height     float64        `json:"height,omitempty"`
	Scale      float64        `json:"scale,omitempty"`
	Opacity    float64        `json:"opacity,omitempty"`
	OffsetX    float64        `json:"offsetX,omitempty"`
	OffsetY    float64        `json:"offsetY,omitempty"`
	AnchorX    float64        `json:"anchorX,omitempty"`
	AnchorY    float64        `json:"anchorY,omitempty"`
	Occlude    bool           `json:"occlude,omitempty"`
	Fit        string         `json:"fit,omitempty"`
	Transition TransitionIR   `json:"transition,omitzero"`
	InState    map[string]any `json:"inState,omitempty"`
	OutState   map[string]any `json:"outState,omitempty"`
	Live       []string       `json:"live,omitempty"`
}

// HTMLIR is the typed compatibility record for one DOM-backed scene overlay.
type HTMLIR struct {
	ID               string       `json:"id"`
	Target           string       `json:"target,omitempty"`
	Mode             string       `json:"mode,omitempty"`
	HTML             string       `json:"html"`
	ClassName        string       `json:"className,omitempty"`
	Fallback         string       `json:"fallback,omitempty"`
	FallbackReason   string       `json:"fallbackReason,omitempty"`
	TextureKey       string       `json:"textureKey,omitempty"`
	TextureWidth     int          `json:"textureWidth,omitempty"`
	TextureHeight    int          `json:"textureHeight,omitempty"`
	MaxTexturePixels int          `json:"maxTexturePixels,omitempty"`
	SurfaceWidth     float64      `json:"surfaceWidth,omitempty"`
	SurfaceHeight    float64      `json:"surfaceHeight,omitempty"`
	X                float64      `json:"x,omitempty"`
	Y                float64      `json:"y,omitempty"`
	Z                float64      `json:"z,omitempty"`
	Priority         float64      `json:"priority,omitempty"`
	ShiftX           float64      `json:"shiftX,omitempty"`
	ShiftY           float64      `json:"shiftY,omitempty"`
	ShiftZ           float64      `json:"shiftZ,omitempty"`
	DriftSpeed       float64      `json:"driftSpeed,omitempty"`
	DriftPhase       float64      `json:"driftPhase,omitempty"`
	Width            float64      `json:"width,omitempty"`
	Height           float64      `json:"height,omitempty"`
	Scale            float64      `json:"scale,omitempty"`
	Opacity          float64      `json:"opacity,omitempty"`
	OffsetX          float64      `json:"offsetX,omitempty"`
	OffsetY          float64      `json:"offsetY,omitempty"`
	AnchorX          float64      `json:"anchorX,omitempty"`
	AnchorY          float64      `json:"anchorY,omitempty"`
	Occlude          bool         `json:"occlude,omitempty"`
	PointerEvents    string       `json:"pointerEvents,omitempty"`
	Transition       TransitionIR `json:"transition,omitzero"`
	Live             []string     `json:"live,omitempty"`
}

// LightIR is the typed compatibility record for one lowered scene light.
type LightIR struct {
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	Color          string         `json:"color,omitempty"`
	GroundColor    string         `json:"groundColor,omitempty"`
	Intensity      float64        `json:"intensity,omitempty"`
	X              float64        `json:"x,omitempty"`
	Y              float64        `json:"y,omitempty"`
	Z              float64        `json:"z,omitempty"`
	DirectionX     float64        `json:"directionX,omitempty"`
	DirectionY     float64        `json:"directionY,omitempty"`
	DirectionZ     float64        `json:"directionZ,omitempty"`
	Angle          float64        `json:"angle,omitempty"`
	Penumbra       float64        `json:"penumbra,omitempty"`
	Range          float64        `json:"range,omitempty"`
	Decay          float64        `json:"decay,omitempty"`
	Width          float64        `json:"width,omitempty"`
	Height         float64        `json:"height,omitempty"`
	Coefficients   []Vector3      `json:"coefficients,omitempty"`
	CastShadow     bool           `json:"castShadow,omitempty"`
	ShadowBias     float64        `json:"shadowBias,omitempty"`
	ShadowSize     int            `json:"shadowSize,omitempty"`
	ShadowCascades int            `json:"shadowCascades,omitempty"`
	ShadowSoftness float64        `json:"shadowSoftness,omitempty"`
	Transition     TransitionIR   `json:"transition,omitzero"`
	InState        map[string]any `json:"inState,omitempty"`
	OutState       map[string]any `json:"outState,omitempty"`
	Live           []string       `json:"live,omitempty"`
}

// PointsIR is the typed compatibility record for one lowered particle system.
type PointsIR struct {
	ID                  string            `json:"id"`
	Count               int               `json:"count"`
	Positions           []float64         `json:"positions,omitempty"`
	Sizes               []float64         `json:"sizes,omitempty"`
	Colors              []string          `json:"colors,omitempty"`
	Color               string            `json:"color,omitempty"`
	Style               string            `json:"style,omitempty"`
	Size                float64           `json:"size,omitempty"`
	MinPixelSize        float64           `json:"minPixelSize,omitempty"`
	MaxPixelSize        float64           `json:"maxPixelSize,omitempty"`
	Opacity             float64           `json:"opacity,omitempty"`
	BlendMode           string            `json:"blendMode,omitempty"`
	DepthWrite          *bool             `json:"depthWrite,omitempty"`
	Attenuation         bool              `json:"attenuation,omitempty"`
	X                   float64           `json:"x,omitempty"`
	Y                   float64           `json:"y,omitempty"`
	Z                   float64           `json:"z,omitempty"`
	RotationX           float64           `json:"rotationX,omitempty"`
	RotationY           float64           `json:"rotationY,omitempty"`
	RotationZ           float64           `json:"rotationZ,omitempty"`
	SpinX               float64           `json:"spinX,omitempty"`
	SpinY               float64           `json:"spinY,omitempty"`
	SpinZ               float64           `json:"spinZ,omitempty"`
	CompressedPositions []CompressedArray `json:"compressedPositions,omitempty"`
	CompressedSizes     []CompressedArray `json:"compressedSizes,omitempty"`
	PreviewPositions    []CompressedArray `json:"previewPositions,omitempty"`
	PreviewSizes        []CompressedArray `json:"previewSizes,omitempty"`
	PositionStride      int               `json:"positionStride,omitempty"`
	// Authored shader material fields — same envelope as ObjectIR.
	CustomVertex          string            `json:"customVertex,omitempty"`
	CustomFragment        string            `json:"customFragment,omitempty"`
	CustomVertexWGSL      string            `json:"customVertexWGSL,omitempty"`
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
	ID                   string               `json:"id"`
	Count                int                  `json:"count"`
	Kind                 string               `json:"kind"`
	Size                 float64              `json:"size,omitempty"`
	Width                float64              `json:"width,omitempty"`
	Height               float64              `json:"height,omitempty"`
	Depth                float64              `json:"depth,omitempty"`
	Radius               float64              `json:"radius,omitempty"`
	RadiusTop            float64              `json:"radiusTop,omitempty"`
	RadiusBottom         float64              `json:"radiusBottom,omitempty"`
	Tube                 float64              `json:"tube,omitempty"`
	Segments             int                  `json:"segments,omitempty"`
	RadialSegments       int                  `json:"radialSegments,omitempty"`
	TubularSegments      int                  `json:"tubularSegments,omitempty"`
	MaterialKind         string               `json:"materialKind,omitempty"`
	Color                string               `json:"color,omitempty"`
	Texture              string               `json:"texture,omitempty"`
	Opacity              *float64             `json:"opacity,omitempty"`
	Emissive             *float64             `json:"emissive,omitempty"`
	BlendMode            string               `json:"blendMode,omitempty"`
	RenderPass           string               `json:"renderPass,omitempty"`
	Wireframe            *bool                `json:"wireframe,omitempty"`
	DepthWrite           *bool                `json:"depthWrite,omitempty"`
	Roughness            float64              `json:"roughness,omitempty"`
	Metalness            float64              `json:"metalness,omitempty"`
	Clearcoat            float64              `json:"clearcoat,omitempty"`
	Sheen                float64              `json:"sheen,omitempty"`
	Transmission         float64              `json:"transmission,omitempty"`
	Iridescence          float64              `json:"iridescence,omitempty"`
	Anisotropy           float64              `json:"anisotropy,omitempty"`
	NormalMap            string               `json:"normalMap,omitempty"`
	RoughnessMap         string               `json:"roughnessMap,omitempty"`
	MetalnessMap         string               `json:"metalnessMap,omitempty"`
	EmissiveMap          string               `json:"emissiveMap,omitempty"`
	CustomVertex         string               `json:"customVertex,omitempty"`
	CustomFragment       string               `json:"customFragment,omitempty"`
	CustomVertexWGSL     string               `json:"customVertexWGSL,omitempty"`
	CustomFragmentWGSL   string               `json:"customFragmentWGSL,omitempty"`
	CustomUniforms       map[string]any       `json:"customUniforms,omitempty"`
	ShaderBackend        string               `json:"shaderBackend,omitempty"`
	ShaderLayout         map[string]any       `json:"shaderLayout,omitempty"`
	ShaderSource         string               `json:"shaderSource,omitempty"`
	ShaderSourceFiles    map[string]string    `json:"shaderSourceFiles,omitempty"`
	Transforms           []float64            `json:"transforms"`
	Colors               []string             `json:"colors,omitempty"`
	Attributes           map[string][]float64 `json:"attributes,omitempty"`
	Pickable             *bool                `json:"pickable,omitempty"`
	CastShadow           bool                 `json:"castShadow,omitempty"`
	ReceiveShadow        bool                 `json:"receiveShadow,omitempty"`
	CompressedTransforms []CompressedArray    `json:"compressedTransforms,omitempty"`
	PreviewTransforms    []CompressedArray    `json:"previewTransforms,omitempty"`
	TransformStride      int                  `json:"transformStride,omitempty"`
	Transition           TransitionIR         `json:"transition,omitzero"`
	InState              map[string]any       `json:"inState,omitempty"`
	OutState             map[string]any       `json:"outState,omitempty"`
	Live                 []string             `json:"live,omitempty"`

	// Optional Elio GPU cull kernel carried through the scene payload.
	// CullKernelWGSL is the inline WGSL source; CullKernelWGSLRef replaces it
	// when the kernel has been hoisted into SceneIR.ShaderLib by applyShaderLib.
	// The JS hydrate path inflates CullKernelWGSLRef back to CullKernelWGSL
	// before downstream renderers see it.
	CullKernelWGSL    string  `json:"cullKernelWGSL,omitempty"`
	CullKernelWGSLRef string  `json:"cullKernelWGSLRef,omitempty"`
	CullKernelEntry   string  `json:"cullKernelEntry,omitempty"`
	CullRadius        float64 `json:"cullRadius,omitempty"`
	CullBackend       string  `json:"cullBackend,omitempty"`
}

// ComputeParticlesIR is the typed compatibility record for one GPU particle system.
type ComputeParticlesIR struct {
	ID         string             `json:"id"`
	Count      int                `json:"count"`
	Emitter    ParticleEmitterIR  `json:"emitter"`
	Forces     []ParticleForceIR  `json:"forces,omitempty"`
	Material   ParticleMaterialIR `json:"material"`
	Bounds     float64            `json:"bounds,omitempty"`
	Transition TransitionIR       `json:"transition,omitzero"`
	InState    map[string]any     `json:"inState,omitempty"`
	OutState   map[string]any     `json:"outState,omitempty"`
	Live       []string           `json:"live,omitempty"`

	// Optional Elio/custom kernel override carried through the scene payload.
	ComputeWGSL    string `json:"computeWGSL,omitempty"`
	ComputeEntry   string `json:"computeEntry,omitempty"`
	ComputeBackend string `json:"computeBackend,omitempty"`
	// ComputeWGSLRef replaces ComputeWGSL when the kernel source has been
	// hoisted into SceneIR.ShaderLib. The JS hydrate path inflates this back
	// to ComputeWGSL before downstream renderers see it.
	ComputeWGSLRef string `json:"computeWGSLRef,omitempty"`

	// Optional authored render-pass shader override — same envelope as ObjectIR.
	RenderVertex            string            `json:"renderVertex,omitempty"`
	RenderFragment          string            `json:"renderFragment,omitempty"`
	RenderVertexWGSL        string            `json:"renderVertexWGSL,omitempty"`
	RenderFragmentWGSL      string            `json:"renderFragmentWGSL,omitempty"`
	RenderVertexRef         string            `json:"renderVertexRef,omitempty"`
	RenderFragmentRef       string            `json:"renderFragmentRef,omitempty"`
	RenderVertexWGSLRef     string            `json:"renderVertexWGSLRef,omitempty"`
	RenderFragmentWGSLRef   string            `json:"renderFragmentWGSLRef,omitempty"`
	RenderUniforms          map[string]any    `json:"renderUniforms,omitempty"`
	RenderShaderBackend     string            `json:"renderShaderBackend,omitempty"`
	RenderShaderLayout      map[string]any    `json:"renderShaderLayout,omitempty"`
	RenderShaderSource      string            `json:"renderShaderSource,omitempty"`
	RenderShaderSourceFiles map[string]string `json:"renderShaderSourceFiles,omitempty"`
}

// WaterSystemIR declares a GPU heightfield water simulation and its optical
// render-pass inputs. It is intentionally shaped after jeantimex/threejs-water:
// a ping-pong heightmap, seeded drops, dynamic pool bounds, and caustic /
// reflection / refraction consumers.
type WaterSystemIR struct {
	ID                          string                           `json:"id"`
	InteractionProfile          string                           `json:"interactionProfile,omitempty"`
	InteractionTarget           string                           `json:"interactionTarget,omitempty"`
	InteractionObject           string                           `json:"interactionObject,omitempty"`
	Resolution                  int                              `json:"resolution,omitempty"`
	SurfaceResolution           int                              `json:"surfaceResolution,omitempty"`
	SurfaceMeshResolution       int                              `json:"surfaceMeshResolution,omitempty"`
	PoolShape                   string                           `json:"poolShape,omitempty"`
	PoolWidth                   float64                          `json:"poolWidth,omitempty"`
	PoolHeight                  float64                          `json:"poolHeight,omitempty"`
	PoolLength                  float64                          `json:"poolLength,omitempty"`
	CornerRadius                float64                          `json:"cornerRadius,omitempty"`
	WaveSpeed                   float64                          `json:"waveSpeed,omitempty"`
	Damping                     float64                          `json:"damping,omitempty"`
	NormalScale                 float64                          `json:"normalScale,omitempty"`
	SeedDrops                   int                              `json:"seedDrops,omitempty"`
	DropRadius                  float64                          `json:"dropRadius,omitempty"`
	DropStrength                float64                          `json:"dropStrength,omitempty"`
	DropEventID                 int                              `json:"dropEventID,omitempty"`
	DropX                       float64                          `json:"dropX,omitempty"`
	DropZ                       float64                          `json:"dropZ,omitempty"`
	DropEventRadius             float64                          `json:"dropEventRadius,omitempty"`
	DropEventStrength           float64                          `json:"dropEventStrength,omitempty"`
	TileTexture                 string                           `json:"tileTexture,omitempty"`
	CubeMap                     string                           `json:"cubeMap,omitempty"`
	ShallowColor                string                           `json:"shallowColor,omitempty"`
	DeepColor                   string                           `json:"deepColor,omitempty"`
	AboveWaterColorR            float64                          `json:"aboveWaterColorR,omitempty"`
	AboveWaterColorG            float64                          `json:"aboveWaterColorG,omitempty"`
	AboveWaterColorB            float64                          `json:"aboveWaterColorB,omitempty"`
	CausticsResolution          int                              `json:"causticsResolution,omitempty"`
	ObjectTextureResolution     int                              `json:"objectTextureResolution,omitempty"`
	ObjectTextureResolutionMode string                           `json:"objectTextureResolutionMode,omitempty"`
	ObjectTexturePixelBudget    int                              `json:"objectTexturePixelBudget,omitempty"`
	ObjectShadowResolution      int                              `json:"objectShadowResolution,omitempty"`
	Caustics                    bool                             `json:"caustics,omitempty"`
	Reflection                  bool                             `json:"reflection,omitempty"`
	Refraction                  bool                             `json:"refraction,omitempty"`
	Paused                      bool                             `json:"paused,omitempty"`
	FollowCamera                bool                             `json:"followCamera,omitempty"`
	LightDirectionX             float64                          `json:"lightDirectionX,omitempty"`
	LightDirectionY             float64                          `json:"lightDirectionY,omitempty"`
	LightDirectionZ             float64                          `json:"lightDirectionZ,omitempty"`
	ActiveObject                string                           `json:"activeObject,omitempty"`
	ObjectKind                  string                           `json:"objectKind,omitempty"`
	ObjectX                     float64                          `json:"objectX,omitempty"`
	ObjectY                     float64                          `json:"objectY,omitempty"`
	ObjectZ                     float64                          `json:"objectZ,omitempty"`
	ObjectPreviousSet           bool                             `json:"objectPreviousSet,omitempty"`
	ObjectPreviousX             float64                          `json:"objectPreviousX,omitempty"`
	ObjectPreviousY             float64                          `json:"objectPreviousY,omitempty"`
	ObjectPreviousZ             float64                          `json:"objectPreviousZ,omitempty"`
	ObjectRadius                float64                          `json:"objectRadius,omitempty"`
	ObjectHalfSizeX             float64                          `json:"objectHalfSizeX,omitempty"`
	ObjectHalfSizeY             float64                          `json:"objectHalfSizeY,omitempty"`
	ObjectHalfSizeZ             float64                          `json:"objectHalfSizeZ,omitempty"`
	ObjectDriftX                float64                          `json:"objectDriftX,omitempty"`
	ObjectDriftY                float64                          `json:"objectDriftY,omitempty"`
	ObjectDriftZ                float64                          `json:"objectDriftZ,omitempty"`
	ObjectBobAmplitude          float64                          `json:"objectBobAmplitude,omitempty"`
	ObjectBobSpeed              float64                          `json:"objectBobSpeed,omitempty"`
	ObjectDisplacementScale     float64                          `json:"objectDisplacementScale,omitempty"`
	ObjectDisplacementSpheres   []WaterDisplacementSphereIR      `json:"objectDisplacementSpheres,omitempty"`
	ObjectDisplacementEvents    []WaterObjectDisplacementEventIR `json:"objectDisplacementEvents,omitempty"`
	ComputeBackend              string                           `json:"computeBackend,omitempty"`
	MaterialBackend             string                           `json:"materialBackend,omitempty"`
	ComputeSource               string                           `json:"computeSource,omitempty"`
	MaterialSource              string                           `json:"materialSource,omitempty"`
	ComputeSourceFiles          map[string]string                `json:"computeSourceFiles,omitempty"`
	MaterialSourceFiles         map[string]string                `json:"materialSourceFiles,omitempty"`
	SeedWGSL                    string                           `json:"seedWGSL,omitempty"`
	DropWGSL                    string                           `json:"dropWGSL,omitempty"`
	DisplacementWGSL            string                           `json:"displacementWGSL,omitempty"`
	SimulationWGSL              string                           `json:"simulationWGSL,omitempty"`
	NormalWGSL                  string                           `json:"normalWGSL,omitempty"`
	CausticsWGSL                string                           `json:"causticsWGSL,omitempty"`
	PoolVertexWGSL              string                           `json:"poolVertexWGSL,omitempty"`
	PoolFragmentWGSL            string                           `json:"poolFragmentWGSL,omitempty"`
	// PoolSelenaWGSL is the Selena-emitted, single combined vertex+fragment
	// WGSL module for the pool render pass, compiled from the .selena source
	// and consumed by the generic descriptor-driven Selena WebGPU render path.
	// It is the sole primary WGSL source for this pass; PoolVertexWGSL /
	// PoolFragmentWGSL above are legacy JSON fields kept for wire-format
	// compatibility but are never populated (the hand-written WGSL trees they
	// once carried have been deleted) and are ignored by the WebGPU path. The
	// host binding descriptor for this module is the existing
	// ShaderDescriptors["pool"] entry (Selena's bindings.Layout is
	// backend-agnostic, so the descriptor compiled alongside the GLSL/GLES pool
	// shader already matches this WGSL's @group/@binding layout).
	PoolSelenaWGSL               string `json:"poolSelenaWGSL,omitempty"`
	SurfaceVertexWGSL            string `json:"surfaceVertexWGSL,omitempty"`
	SurfaceFragmentWGSL          string `json:"surfaceFragmentWGSL,omitempty"`
	SurfaceBelowFragmentWGSL     string `json:"surfaceBelowFragmentWGSL,omitempty"`
	ObjectShadowWGSL             string `json:"objectShadowWGSL,omitempty"`
	ObjectMeshShadowVertexWGSL   string `json:"objectMeshShadowVertexWGSL,omitempty"`
	ObjectMeshShadowFragmentWGSL string `json:"objectMeshShadowFragmentWGSL,omitempty"`
	// SurfaceSelenaWGSL..ObjectMeshShadowSelenaWGSL generalize PoolSelenaWGSL
	// above to the remaining migrated render passes (see scene.WaterSystem for
	// the full comment).
	SurfaceSelenaWGSL          string `json:"surfaceSelenaWGSL,omitempty"`
	SurfaceBelowSelenaWGSL     string `json:"surfaceBelowSelenaWGSL,omitempty"`
	CausticsSelenaWGSL         string `json:"causticsSelenaWGSL,omitempty"`
	ObjectShadowSelenaWGSL     string `json:"objectShadowSelenaWGSL,omitempty"`
	CompoundShadowSelenaWGSL   string `json:"compoundShadowSelenaWGSL,omitempty"`
	ObjectMeshShadowSelenaWGSL string `json:"objectMeshShadowSelenaWGSL,omitempty"`
	// SeedSelenaWGSL..NormalSelenaWGSL are the Selena-emitted single @compute
	// WGSL modules for the five feedback simulation kernels, compiled from the
	// .selena source and consumed by the generic descriptor-driven Selena
	// feedback-compute WebGPU path (getSelenaComputePipeline/
	// createSelenaComputeBindGroup in 16a-scene-webgpu.js). They are the sole
	// primary WGSL source for these kernels; SeedWGSL/DropWGSL/
	// DisplacementWGSL/SimulationWGSL/NormalWGSL above are legacy JSON fields
	// kept for wire-format compatibility but are never populated and are
	// ignored by the WebGPU path. The host binding descriptor for each is the
	// existing ShaderDescriptors[<key>] entry (compiled alongside the
	// GLSL/GLES kernel from the same .selena source; Selena's bindings.Layout
	// is backend-agnostic).
	SeedSelenaWGSL                  string `json:"seedSelenaWGSL,omitempty"`
	DropSelenaWGSL                  string `json:"dropSelenaWGSL,omitempty"`
	DisplacementSelenaWGSL          string `json:"displacementSelenaWGSL,omitempty"`
	SimulationSelenaWGSL            string `json:"simulationSelenaWGSL,omitempty"`
	NormalSelenaWGSL                string `json:"normalSelenaWGSL,omitempty"`
	DisplacementWGSLRef             string `json:"displacementWGSLRef,omitempty"`
	SeedWGSLRef                     string `json:"seedWGSLRef,omitempty"`
	DropWGSLRef                     string `json:"dropWGSLRef,omitempty"`
	SimulationWGSLRef               string `json:"simulationWGSLRef,omitempty"`
	NormalWGSLRef                   string `json:"normalWGSLRef,omitempty"`
	CausticsWGSLRef                 string `json:"causticsWGSLRef,omitempty"`
	PoolVertexWGSLRef               string `json:"poolVertexWGSLRef,omitempty"`
	PoolFragmentWGSLRef             string `json:"poolFragmentWGSLRef,omitempty"`
	SurfaceVertexWGSLRef            string `json:"surfaceVertexWGSLRef,omitempty"`
	SurfaceFragmentWGSLRef          string `json:"surfaceFragmentWGSLRef,omitempty"`
	SurfaceBelowFragmentWGSLRef     string `json:"surfaceBelowFragmentWGSLRef,omitempty"`
	ObjectShadowWGSLRef             string `json:"objectShadowWGSLRef,omitempty"`
	ObjectMeshShadowVertexWGSLRef   string `json:"objectMeshShadowVertexWGSLRef,omitempty"`
	ObjectMeshShadowFragmentWGSLRef string `json:"objectMeshShadowFragmentWGSLRef,omitempty"`

	// Selena-compiled GLSL/GLES slots. Each authored .selena material/kernel
	// compiles to GLSL (WebGL1) + GLES (WebGL2) + WGSL (WebGPU, the
	// *SelenaWGSL slots above) plus a backend-agnostic host binding descriptor
	// (ShaderDescriptors); these GLSL/GLES slots feed the WebGL/WebGL2 water
	// fallback and are ignored by the WebGPU path, which consumes the
	// *SelenaWGSL slots instead. The legacy hand-written *WGSL slots above
	// (PoolVertexWGSL, SeedWGSL, etc.) have been retired and are never
	// populated; if a *SelenaWGSL module is unexpectedly missing at runtime,
	// the JS client falls back to its builtin SCENE_WATER_*_SOURCE constants
	// (see 16a-scene-webgpu.js), not these slots.
	//
	// Each authored Selena material compiles to a vertex+fragment pair per
	// backend (unlike WGSL, which carries a single combined module): the GLSL
	// target emits GLSL ES 1.00 (WebGL1: `attribute`/`varying`) and the GLES
	// target emits GLSL ES 3.00 (WebGL2: `#version 300 es`, `in`/`out`). The
	// feedback simulation kernels (seed/drop/displacement/simulation/normal)
	// compile to a fullscreen vertex + a sim fragment for the ping-pong pass.
	SeedVertexGLSL           string `json:"seedVertexGLSL,omitempty"`
	SeedFragmentGLSL         string `json:"seedFragmentGLSL,omitempty"`
	SeedVertexGLES           string `json:"seedVertexGLES,omitempty"`
	SeedFragmentGLES         string `json:"seedFragmentGLES,omitempty"`
	DropVertexGLSL           string `json:"dropVertexGLSL,omitempty"`
	DropFragmentGLSL         string `json:"dropFragmentGLSL,omitempty"`
	DropVertexGLES           string `json:"dropVertexGLES,omitempty"`
	DropFragmentGLES         string `json:"dropFragmentGLES,omitempty"`
	DisplacementVertexGLSL   string `json:"displacementVertexGLSL,omitempty"`
	DisplacementFragmentGLSL string `json:"displacementFragmentGLSL,omitempty"`
	DisplacementVertexGLES   string `json:"displacementVertexGLES,omitempty"`
	DisplacementFragmentGLES string `json:"displacementFragmentGLES,omitempty"`
	SimulationVertexGLSL     string `json:"simulationVertexGLSL,omitempty"`
	SimulationFragmentGLSL   string `json:"simulationFragmentGLSL,omitempty"`
	SimulationVertexGLES     string `json:"simulationVertexGLES,omitempty"`
	SimulationFragmentGLES   string `json:"simulationFragmentGLES,omitempty"`
	NormalVertexGLSL         string `json:"normalVertexGLSL,omitempty"`
	NormalFragmentGLSL       string `json:"normalFragmentGLSL,omitempty"`
	NormalVertexGLES         string `json:"normalVertexGLES,omitempty"`
	NormalFragmentGLES       string `json:"normalFragmentGLES,omitempty"`
	CausticsVertexGLSL       string `json:"causticsVertexGLSL,omitempty"`
	CausticsFragmentGLSL     string `json:"causticsFragmentGLSL,omitempty"`
	CausticsVertexGLES       string `json:"causticsVertexGLES,omitempty"`
	CausticsFragmentGLES     string `json:"causticsFragmentGLES,omitempty"`
	PoolVertexGLSL           string `json:"poolVertexGLSL,omitempty"`
	PoolFragmentGLSL         string `json:"poolFragmentGLSL,omitempty"`
	PoolVertexGLES           string `json:"poolVertexGLES,omitempty"`
	PoolFragmentGLES         string `json:"poolFragmentGLES,omitempty"`
	SurfaceVertexGLSL        string `json:"surfaceVertexGLSL,omitempty"`
	SurfaceFragmentGLSL      string `json:"surfaceFragmentGLSL,omitempty"`
	SurfaceVertexGLES        string `json:"surfaceVertexGLES,omitempty"`
	SurfaceFragmentGLES      string `json:"surfaceFragmentGLES,omitempty"`
	SurfaceBelowVertexGLSL   string `json:"surfaceBelowVertexGLSL,omitempty"`
	SurfaceBelowFragmentGLSL string `json:"surfaceBelowFragmentGLSL,omitempty"`
	SurfaceBelowVertexGLES   string `json:"surfaceBelowVertexGLES,omitempty"`
	SurfaceBelowFragmentGLES string `json:"surfaceBelowFragmentGLES,omitempty"`
	ObjectShadowVertexGLSL   string `json:"objectShadowVertexGLSL,omitempty"`
	ObjectShadowFragmentGLSL string `json:"objectShadowFragmentGLSL,omitempty"`
	ObjectShadowVertexGLES   string `json:"objectShadowVertexGLES,omitempty"`
	ObjectShadowFragmentGLES string `json:"objectShadowFragmentGLES,omitempty"`
	// CompoundShadow* are the WebGL2-only compound-object (TorusKnot/Duck)
	// footprint shadow pass (compound-shadow.sel), an additive parallel that
	// handles the objectKind >= 2.5 case ObjectShadow* cannot express (a
	// fullscreen pass over up to 32 proxy displacement spheres). The WebGPU
	// path renders compound shadows via its own mesh-shadow RTT and ignores
	// these.
	CompoundShadowVertexGLSL     string `json:"compoundShadowVertexGLSL,omitempty"`
	CompoundShadowFragmentGLSL   string `json:"compoundShadowFragmentGLSL,omitempty"`
	CompoundShadowVertexGLES     string `json:"compoundShadowVertexGLES,omitempty"`
	CompoundShadowFragmentGLES   string `json:"compoundShadowFragmentGLES,omitempty"`
	ObjectMeshShadowVertexGLSL   string `json:"objectMeshShadowVertexGLSL,omitempty"`
	ObjectMeshShadowFragmentGLSL string `json:"objectMeshShadowFragmentGLSL,omitempty"`
	ObjectMeshShadowVertexGLES   string `json:"objectMeshShadowVertexGLES,omitempty"`
	ObjectMeshShadowFragmentGLES string `json:"objectMeshShadowFragmentGLES,omitempty"`
	ObjectMaterialVertexGLSL     string `json:"objectMaterialVertexGLSL,omitempty"`
	ObjectMaterialFragmentGLSL   string `json:"objectMaterialFragmentGLSL,omitempty"`
	ObjectMaterialVertexGLES     string `json:"objectMaterialVertexGLES,omitempty"`
	ObjectMaterialFragmentGLES   string `json:"objectMaterialFragmentGLES,omitempty"`
	DuckMaterialVertexGLSL       string `json:"duckMaterialVertexGLSL,omitempty"`
	DuckMaterialFragmentGLSL     string `json:"duckMaterialFragmentGLSL,omitempty"`
	DuckMaterialVertexGLES       string `json:"duckMaterialVertexGLES,omitempty"`
	DuckMaterialFragmentGLES     string `json:"duckMaterialFragmentGLES,omitempty"`

	// ShaderDescriptors carries the per-shader Selena host binding descriptor
	// (uniform block layout incl. std140 array stride, attributes, textures with
	// cube/2d dimension, feedback statefields, and the grid texelSize uniform),
	// keyed by the logical shader name (e.g. "surface", "pool", "seed"). The
	// WebGL2 runtime uses these to wire uniforms/textures/ping-pong state. Values
	// are the raw Selena bindings.Layout JSON.
	ShaderDescriptors map[string]json.RawMessage `json:"shaderDescriptors,omitempty"`
}

// WaterDisplacementSphereIR is one sphere in a compound water displacement
// volume. Offsets are relative to the active object center.
type WaterDisplacementSphereIR struct {
	OffsetX float64 `json:"offsetX,omitempty"`
	OffsetY float64 `json:"offsetY,omitempty"`
	OffsetZ float64 `json:"offsetZ,omitempty"`
	Radius  float64 `json:"radius,omitempty"`
}

// WaterObjectDisplacementEventIR describes a one-shot object displacement that
// should be applied even when the active object is no longer present. It mirrors
// upstream setEnabled moves such as object removal to inactive Y.
type WaterObjectDisplacementEventIR struct {
	ID                        int                         `json:"id,omitempty"`
	ActiveObject              string                      `json:"activeObject,omitempty"`
	ObjectKind                string                      `json:"objectKind,omitempty"`
	ObjectSubtype             string                      `json:"objectSubtype,omitempty"`
	ObjectX                   float64                     `json:"objectX,omitempty"`
	ObjectY                   float64                     `json:"objectY,omitempty"`
	ObjectZ                   float64                     `json:"objectZ,omitempty"`
	ObjectPreviousSet         bool                        `json:"objectPreviousSet,omitempty"`
	ObjectPreviousX           float64                     `json:"objectPreviousX,omitempty"`
	ObjectPreviousY           float64                     `json:"objectPreviousY,omitempty"`
	ObjectPreviousZ           float64                     `json:"objectPreviousZ,omitempty"`
	ObjectRadius              float64                     `json:"objectRadius,omitempty"`
	ObjectHalfSizeX           float64                     `json:"objectHalfSizeX,omitempty"`
	ObjectHalfSizeY           float64                     `json:"objectHalfSizeY,omitempty"`
	ObjectHalfSizeZ           float64                     `json:"objectHalfSizeZ,omitempty"`
	ObjectDisplacementScale   float64                     `json:"objectDisplacementScale,omitempty"`
	ObjectDisplacementSpheres []WaterDisplacementSphereIR `json:"objectDisplacementSpheres,omitempty"`
}

// ParticleEmitterIR describes the emitter configuration for a GPU particle system.
type ParticleEmitterIR struct {
	Kind      string  `json:"kind"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	RotationX float64 `json:"rotationX,omitempty"`
	RotationY float64 `json:"rotationY,omitempty"`
	RotationZ float64 `json:"rotationZ,omitempty"`
	SpinX     float64 `json:"spinX,omitempty"`
	SpinY     float64 `json:"spinY,omitempty"`
	SpinZ     float64 `json:"spinZ,omitempty"`
	Radius    float64 `json:"radius,omitempty"`
	Rate      float64 `json:"rate,omitempty"`
	Lifetime  float64 `json:"lifetime,omitempty"`
	Arms      int     `json:"arms,omitempty"`
	Wind      float64 `json:"wind,omitempty"`
	Scatter   float64 `json:"scatter,omitempty"`
	Once      bool    `json:"once,omitempty"`
}

// ParticleForceIR describes one force acting on a GPU particle system.
type ParticleForceIR struct {
	Kind      string  `json:"kind"`
	Strength  float64 `json:"strength,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	Z         float64 `json:"z,omitempty"`
	Frequency float64 `json:"frequency,omitempty"`
}

// ParticleMaterialIR describes the material for a GPU particle system.
type ParticleMaterialIR struct {
	Color       string  `json:"color,omitempty"`
	ColorEnd    string  `json:"colorEnd,omitempty"`
	Style       string  `json:"style,omitempty"`
	Size        float64 `json:"size,omitempty"`
	SizeEnd     float64 `json:"sizeEnd,omitempty"`
	Opacity     float64 `json:"opacity,omitempty"`
	OpacityEnd  float64 `json:"opacityEnd,omitempty"`
	BlendMode   string  `json:"blendMode,omitempty"`
	Attenuation bool    `json:"attenuation,omitempty"`
}

// AnimationClipIR is the typed compatibility record for one procedural
// animation clip with per-channel keyframe data.
type AnimationClipIR struct {
	Name     string               `json:"name"`
	Duration float64              `json:"duration"`
	Channels []AnimationChannelIR `json:"channels"`
}

// AnimationChannelIR is one keyframe track targeting a single node property.
// Times and Values are the raw float arrays; CompressedTimes/CompressedValues
// replace them when TurboQuant compression is enabled.
type AnimationChannelIR struct {
	TargetNode    int    `json:"targetNode"`
	Property      string `json:"property"`
	Interpolation string `json:"interpolation,omitempty"`

	Times  []float64 `json:"times,omitempty"`
	Values []float64 `json:"values,omitempty"`

	CompressedTimes  []CompressedArray `json:"compressedTimes,omitempty"`
	CompressedValues []CompressedArray `json:"compressedValues,omitempty"`
	PreviewTimes     []CompressedArray `json:"previewTimes,omitempty"`
	PreviewValues    []CompressedArray `json:"previewValues,omitempty"`
}

// EnvironmentIR is the typed compatibility record for scene-wide lighting.
type EnvironmentIR struct {
	AmbientColor     string         `json:"ambientColor,omitempty"`
	AmbientIntensity float64        `json:"ambientIntensity,omitempty"`
	SkyColor         string         `json:"skyColor,omitempty"`
	SkyIntensity     float64        `json:"skyIntensity,omitempty"`
	GroundColor      string         `json:"groundColor,omitempty"`
	GroundIntensity  float64        `json:"groundIntensity,omitempty"`
	EnvMap           string         `json:"envMap,omitempty"`
	EnvIntensity     float64        `json:"envIntensity,omitempty"`
	EnvRotation      float64        `json:"envRotation,omitempty"`
	Exposure         float64        `json:"exposure,omitempty"`
	ToneMapping      string         `json:"toneMapping,omitempty"`
	FogColor         string         `json:"fogColor,omitempty"`
	FogDensity       float64        `json:"fogDensity,omitempty"`
	Transition       TransitionIR   `json:"transition,omitzero"`
	InState          map[string]any `json:"inState,omitempty"`
	OutState         map[string]any `json:"outState,omitempty"`
	Live             []string       `json:"live,omitempty"`
}

// SceneIR lowers typed scene props into a typed intermediate representation.
func (p Props) SceneIR() SceneIR {
	ir := p.Graph.SceneIR()
	ir.Environment = p.Environment.sceneIR()
	ir.PostEffects = p.PostFX.sceneIR()
	if synthesized := migrateEnvironmentTonemap(p.Environment, ir.PostEffects); synthesized != nil {
		// Prepend so user-declared effects still run after the synthesized
		// tonemap. (Order doesn't matter much for tonemap-only chains, but
		// keeping it first matches the order users typically write.)
		ir.PostEffects = append([]PostEffectIR{synthesized}, ir.PostEffects...)
	}
	ir.PostFXMaxPixels = p.PostFX.resolveMaxPixels()
	ir.ShadowMaxPixels = p.Shadows.resolveMaxPixels()
	ir.QualityLadder = qualityLadderSceneIR(p.QualityLadder)
	if len(ir.QualityLadder) > 0 {
		ir.QualityStartRung = resolveQualityStartRung(p.QualityLadder, p.QualityStartRung)
	}
	if len(p.PointQualityGroups) > 0 {
		groups := make(map[string]string, len(p.PointQualityGroups))
		for name, group := range p.PointQualityGroups {
			name = strings.TrimSpace(name)
			group = strings.TrimSpace(group)
			if name == "" || group == "" {
				continue
			}
			groups[name] = group
		}
		if len(groups) > 0 {
			ir.PointQualityGroups = groups
		}
	}
	if p.Compression != nil && p.Compression.BitWidth > 0 {
		previewBW := 0
		if p.Compression.Progressive || p.Compression.LOD {
			previewBW = p.Compression.PreviewBitWidth
			if previewBW <= 0 {
				previewBW = 2 // default preview at 2-bit
			}
		}
		compressSceneIR(&ir, p.Compression.BitWidth, previewBW)
	}
	// Compute the honesty-gate verdict now that the wire records are assembled
	// AND the author-side gates (RequireWebGL / RequiredCapabilities) are in
	// scope. The verdict ships on the SceneIR so the JS runtime can obey it.
	ir.BackendCaps = ptrTo(capability.Verdict(collectFeatures(ir), requiredBackends(p), capability.DefaultPolicy()))

	// Post-filter: remove backends that no custom-shader material can serve.
	// This is a post-filter (not fed into Verdict) because custom-shader
	// exclusion is per-material, not a flat feature-matrix entry.
	for _, b := range customShaderUnservedBackends(ir, defaultShaderResolver) {
		caps := ir.BackendCaps
		newCapable := caps.Capable[:0:len(caps.Capable)]
		for _, c := range caps.Capable {
			if c != b {
				newCapable = append(newCapable, c)
			}
		}
		caps.Capable = newCapable
		caps.Reasons = append(caps.Reasons, capability.CapReason{
			Feature:  capability.FeatureCustomShader,
			Excludes: b,
		})
	}

	return ir
}

// defaultShaderResolver is the package-level ShaderResolver used by
// Props.SceneIR(). A future task can swap this to a transpiling resolver
// without changing any callers.
var defaultShaderResolver capability.ShaderResolver = capability.PresenceResolver{}

// SkinLookup is a build-time probe that reports whether a given GLB/glTF asset
// source URL is skinned. collectFeatures consults it so that Model and
// InstancedGLBMesh records whose GLB was probed at build time get
// FeatureSkinning tagged without needing to load the binary at runtime.
// A nil skinLookup means no build-time probe is available; the runtime
// backstop (later JS task) covers that path.
type SkinLookup interface {
	Skinned(src string) bool
}

// skinLookup is the package-level SkinLookup. Nil by default.
var skinLookup SkinLookup

// SetSkinLookup replaces the package-level SkinLookup. Pass nil to disable.
// Intended for use by assetpipe integration and tests.
func SetSkinLookup(l SkinLookup) { skinLookup = l }

// customShaderUnservedBackends returns the de-duped set of backends that are
// unserved by at least one custom-material object in the SceneIR. A backend
// is "unserved" by a material if the resolver says the material cannot serve
// it (i.e. the required shading language is absent).
func customShaderUnservedBackends(ir SceneIR, r capability.ShaderResolver) []capability.Backend {
	seen := map[capability.Backend]bool{}
	for _, obj := range ir.Objects {
		if obj.CustomVertex == "" && obj.CustomFragment == "" &&
			obj.CustomVertexWGSL == "" && obj.CustomFragmentWGSL == "" {
			continue
		}
		src := capability.CustomMaterialSources{
			GLSL: obj.CustomVertex != "" || obj.CustomFragment != "",
			WGSL: obj.CustomVertexWGSL != "" || obj.CustomFragmentWGSL != "",
		}
		served := r.Serves(src)
		for b, ok := range served {
			if !ok {
				seen[b] = true
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]capability.Backend, 0, len(seen))
	for b := range seen {
		out = append(out, b)
	}
	return out
}

// ptrTo returns the address of v. Used to store capability.Verdict's value
// result as the *capability.BackendCaps the wire format carries.
func ptrTo[T any](v T) *T { return &v }

// requiredBackends maps a Props' author-declared hard gates onto the backend
// set the verdict must restrict itself to. An empty result means no hard gate
// (PreferWebGPU/PreferWebGL only reorder at runtime — they are not gates).
func requiredBackends(p Props) []capability.Backend {
	if p.RequireWebGL != nil && *p.RequireWebGL {
		return []capability.Backend{capability.BackendWebGL}
	}
	for _, capName := range p.EngineRequiredCapabilities() {
		if strings.EqualFold(strings.TrimSpace(capName), string(capability.BackendWebGPU)) {
			return []capability.Backend{capability.BackendWebGPU}
		}
	}
	return nil
}

// SceneIR lowers a typed graph into a typed intermediate representation.
//
// The returned slices alias the graphLowerer's internal accumulators
// directly — no defensive copy — because SceneIR's only caller is
// Props.SceneIR → legacyProps → MarshalJSON, none of which mutate the
// slices. Skipping the defensive copies cuts ~7 allocations and ~7
// backing-array copies per page render on any non-trivial scene, which
// on a 20-mesh fixture drops SceneIR() from ~22µs / 292 allocs to
// noticeably less. If a future caller needs to mutate, they can take
// a copy themselves — the cost of the copy belongs at the mutation
// site, not at the lowering site.
func (g Graph) SceneIR() SceneIR {
	if len(g.Nodes) == 0 {
		return SceneIR{}
	}

	// Pre-size the top-level slices and the anchors map to len(g.Nodes)
	// — each node is likely to resolve to one object/light/label/etc.,
	// so starting with that capacity avoids the 3-5 append doublings
	// most typical scenes would otherwise trigger. Over-estimates waste
	// a few slice headers but under-estimates are absorbed by the
	// append grow path.
	nodeCount := len(g.Nodes)
	lowerer := &graphLowerer{
		anchors: make(map[string]worldTransform, nodeCount),
		objects: make([]ObjectIR, 0, nodeCount),
		lights:  make([]LightIR, 0, nodeCount),
	}
	for _, node := range g.Nodes {
		lowerer.lowerNode(node, identityTransform())
	}
	ir := SceneIR{
		Objects:            lowerer.objects,
		Models:             lowerer.models,
		Points:             lowerer.points,
		InstancedMeshes:    lowerer.instancedMeshes,
		InstancedGLBMeshes: lowerer.instancedGLBMeshes,
		ComputeParticles:   lowerer.computeParticles,
		WaterSystems:       lowerer.waterSystems,
		Animations:         lowerer.animations,
		Labels:             lowerer.resolveLabels(),
		Sprites:            lowerer.resolveSprites(),
		HTML:               lowerer.resolveHTML(),
		Lights:             lowerer.lights,
		SpinTracks:         lowerer.spinTracks,
		MaterialTracks:     lowerer.materialTracks,
	}

	// Serialize spin tracks into MotionProgram so the browser can motionLoad()
	// them. Build a scene-level Timeline from SpinTracks, intern target/prop
	// refs, then encode to a flat binary blob (base64 on the wire).
	if len(ir.SpinTracks) > 0 {
		tl := ir.SpinMotionTimeline()
		targetIn := motion.NewInterner()
		propIn := motion.NewInterner()
		motion.PrepareTracks(tl, targetIn, propIn)
		ir.MotionProgram = motion.EncodeProgram(tl, targetIn.Refs(), propIn.Refs())
	}

	// Serialize material-uniform tracks into a SEPARATE wire program so material
	// packets route independently of transform motion in the JS runtime. Fresh
	// interners keep the target/prop ref tables scoped to this program.
	if len(ir.MaterialTracks) > 0 {
		tl := ir.MaterialMotionTimeline()
		targetIn := motion.NewInterner()
		propIn := motion.NewInterner()
		motion.PrepareTracks(tl, targetIn, propIn)
		ir.MaterialMotionProgram = motion.EncodeProgram(tl, targetIn.Refs(), propIn.Refs())
	}

	return ir
}

// MarshalJSON serializes SceneIR to its wire JSON form. When any qualifying
// shader string (computeWGSL, customVertexWGSL, etc.) appears ≥2 times across
// the scene's nodes, MarshalJSON hoists the duplicates into a top-level
// "shaderLib" map and replaces the inline fields with "*Ref" fields. This
// is transparent to all downstream consumers: the JS hydrate path inflates
// the refs back before renderers see them.
//
// Implementation note: we produce the map form via legacyProps() so that the
// applyShaderLib pass can mutate map values in place — the struct-tag reflection
// path doesn't give us mutable access to nested object fields at low cost.
func (ir SceneIR) MarshalJSON() ([]byte, error) {
	m := ir.legacyProps()
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	applyShaderLib(m)
	return json.Marshal(m)
}

// UnmarshalJSON deserializes SceneIR from its wire JSON form, inflating any
// shaderLib refs back to inline fields before populating the struct.
func (ir *SceneIR) UnmarshalJSON(data []byte) error {
	// Use a type alias to avoid infinite recursion.
	type sceneIRAlias SceneIR
	var alias sceneIRAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	// Inflate shaderLib refs on the raw map so that the *Ref struct fields
	// are populated — then re-unmarshal into the alias. This is a two-pass
	// approach that handles both the struct Ref fields and the inline fields.
	//
	// Simpler path: inflate on the map, re-marshal, re-unmarshal.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	inflateShaderLib(raw)
	inflated, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var final sceneIRAlias
	if err := json.Unmarshal(inflated, &final); err != nil {
		return err
	}
	*ir = SceneIR(final)
	return nil
}

func (ir SceneIR) isZero() bool {
	return strings.TrimSpace(ir.Schema) == "" && len(ir.Objects) == 0 && len(ir.Models) == 0 && len(ir.Points) == 0 && len(ir.InstancedMeshes) == 0 && len(ir.InstancedGLBMeshes) == 0 && len(ir.ComputeParticles) == 0 && len(ir.WaterSystems) == 0 && len(ir.Animations) == 0 && len(ir.Labels) == 0 && len(ir.Sprites) == 0 && len(ir.HTML) == 0 && len(ir.Lights) == 0 && ir.Environment.IsZero() && len(ir.PostEffects) == 0 && len(ir.QualityLadder) == 0 && len(ir.PointQualityGroups) == 0
}

func (ir SceneIR) legacyProps() map[string]any {
	if ir.isZero() {
		return nil
	}
	// Pre-sized map header — the final scene wire format rarely exceeds
	// 12 top-level keys (objects/models/points/instancedMeshes/
	// computeParticles/animations/labels/sprites/lights/environment/
	// postEffects/shadowMaxPixels/postFXMaxPixels). Starting with that
	// capacity skips the 1-2 bucket grows the default-sized literal path
	// incurred on every marshal.
	out := make(map[string]any, 16)
	if strings.TrimSpace(ir.Schema) != "" {
		out["schema"] = strings.TrimSpace(ir.Schema)
	}
	if objects := legacyObjects(ir.Objects); len(objects) > 0 {
		out["objects"] = objects
	}
	if models := legacyModels(ir.Models); len(models) > 0 {
		out["models"] = models
	}
	if points := legacyPointsList(ir.Points); len(points) > 0 {
		out["points"] = points
	}
	if instancedMeshes := legacyInstancedMeshes(ir.InstancedMeshes); len(instancedMeshes) > 0 {
		out["instancedMeshes"] = instancedMeshes
	}
	if instancedGLBMeshes := legacyInstancedGLBMeshes(ir.InstancedGLBMeshes); len(instancedGLBMeshes) > 0 {
		out["instancedGLBMeshes"] = instancedGLBMeshes
	}
	if computeParticles := legacyComputeParticles(ir.ComputeParticles); len(computeParticles) > 0 {
		out["computeParticles"] = computeParticles
	}
	if waterSystems := legacyWaterSystems(ir.WaterSystems); len(waterSystems) > 0 {
		out["waterSystems"] = waterSystems
	}
	if animations := legacyAnimations(ir.Animations); len(animations) > 0 {
		out["animations"] = animations
	}
	if labels := legacyLabels(ir.Labels); len(labels) > 0 {
		out["labels"] = labels
	}
	if sprites := legacySprites(ir.Sprites); len(sprites) > 0 {
		out["sprites"] = sprites
	}
	if html := legacyHTML(ir.HTML); len(html) > 0 {
		out["html"] = html
	}
	if lights := legacyLights(ir.Lights); len(lights) > 0 {
		out["lights"] = lights
	}
	if environment := ir.Environment.legacyProps(); len(environment) > 0 {
		out["environment"] = environment
	}
	if len(ir.PostEffects) > 0 {
		effects := make([]map[string]any, 0, len(ir.PostEffects))
		for _, e := range ir.PostEffects {
			effects = append(effects, e.legacyProps())
		}
		out["postEffects"] = effects
		if ir.PostFXMaxPixels != 0 {
			out["postFXMaxPixels"] = ir.PostFXMaxPixels
		}
	}
	if ir.ShadowMaxPixels != 0 {
		out["shadowMaxPixels"] = ir.ShadowMaxPixels
	}
	if len(ir.QualityLadder) > 0 {
		rungs := make([]map[string]any, 0, len(ir.QualityLadder))
		for _, r := range ir.QualityLadder {
			rungs = append(rungs, r.legacyProps())
		}
		out["qualityLadder"] = rungs
		if ir.QualityStartRung != 0 {
			out["qualityStartRung"] = ir.QualityStartRung
		}
	}
	if len(ir.PointQualityGroups) > 0 {
		groups := make(map[string]string, len(ir.PointQualityGroups))
		for name, group := range ir.PointQualityGroups {
			groups[name] = group
		}
		out["pointQualityGroups"] = groups
	}
	if ir.BackendCaps != nil {
		out["backendCaps"] = ir.BackendCaps
	}
	if len(ir.ShaderLib) > 0 {
		lib := make(map[string]string, len(ir.ShaderLib))
		for key, source := range ir.ShaderLib {
			lib[key] = source
		}
		out["shaderLib"] = lib
	}
	if len(ir.MotionProgram) > 0 {
		out["motionProgram"] = ir.MotionProgram
	}
	if len(ir.MaterialMotionProgram) > 0 {
		out["materialMotionProgram"] = ir.MaterialMotionProgram
	}
	return out
}

func legacyObjects(items []ObjectIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item ObjectIR) legacyProps() map[string]any {
	record := map[string]any{
		"id":   item.ID,
		"kind": item.Kind,
	}
	setNumeric(record, "size", item.Size)
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	setNumeric(record, "depth", item.Depth)
	setNumeric(record, "radius", item.Radius)
	setInt(record, "segments", item.Segments)
	if points := legacyLinePoints(item.Points); len(points) > 0 {
		record["points"] = points
	}
	if segments := legacyLineSegments(item.LineSegments); len(segments) > 0 {
		record["lineSegments"] = segments
	}
	if v := item.Vertices; v != nil && len(v.Positions) > 0 {
		vert := map[string]any{
			"positions": append([]float64(nil), v.Positions...),
			"count":     v.Count,
		}
		if len(v.Normals) > 0 {
			vert["normals"] = append([]float64(nil), v.Normals...)
		}
		if len(v.UVs) > 0 {
			vert["uvs"] = append([]float64(nil), v.UVs...)
		}
		record["vertices"] = vert
	}
	setNumeric(record, "lineWidth", item.LineWidth)
	setNumeric(record, "radiusTop", item.RadiusTop)
	setNumeric(record, "radiusBottom", item.RadiusBottom)
	setNumeric(record, "tube", item.Tube)
	setInt(record, "radialSegments", item.RadialSegments)
	setInt(record, "tubularSegments", item.TubularSegments)
	setString(record, "materialKind", item.MaterialKind)
	setString(record, "color", item.Color)
	setString(record, "texture", item.Texture)
	setNumericPtr(record, "opacity", item.Opacity)
	setNumericPtr(record, "emissive", item.Emissive)
	setString(record, "blendMode", item.BlendMode)
	setString(record, "renderPass", item.RenderPass)
	if item.Wireframe != nil {
		record["wireframe"] = *item.Wireframe
	}
	if item.LineDash != nil {
		record["lineDash"] = *item.LineDash
	}
	setNumeric(record, "dashSize", item.DashSize)
	setNumeric(record, "gapSize", item.GapSize)
	setString(record, "customVertex", item.CustomVertex)
	setString(record, "customFragment", item.CustomFragment)
	setString(record, "customVertexWGSL", item.CustomVertexWGSL)
	setString(record, "customFragmentWGSL", item.CustomFragmentWGSL)
	if len(item.CustomUniforms) > 0 {
		record["customUniforms"] = cloneSceneAnyMap(item.CustomUniforms)
	}
	setString(record, "shaderBackend", item.ShaderBackend)
	if len(item.ShaderLayout) > 0 {
		record["shaderLayout"] = cloneSceneAnyMap(item.ShaderLayout)
	}
	setString(record, "shaderSource", item.ShaderSource)
	if len(item.ShaderSourceFiles) > 0 {
		record["shaderSourceFiles"] = cloneSceneStringMap(item.ShaderSourceFiles)
	}
	if item.Pickable != nil {
		record["pickable"] = *item.Pickable
	}
	if item.Visible != nil {
		record["visible"] = *item.Visible
	}
	if item.Selected {
		record["selected"] = true
	}
	if item.GizmoRing {
		record["gizmoRing"] = true
	}
	if item.GizmoHelper {
		record["gizmoHelper"] = true
	}
	setString(record, "gizmoFormMode", item.GizmoFormMode)
	setString(record, "qualityGroup", item.QualityGroup)
	setString(record, "outlineColor", item.OutlineColor)
	setNumeric(record, "outlineWidth", item.OutlineWidth)
	if item.CastShadow {
		record["castShadow"] = true
	}
	if item.ReceiveShadow {
		record["receiveShadow"] = true
	}
	if item.DepthWrite != nil {
		record["depthWrite"] = *item.DepthWrite
	}
	setNumeric(record, "roughness", item.Roughness)
	setNumeric(record, "metalness", item.Metalness)
	setNumeric(record, "clearcoat", item.Clearcoat)
	setNumeric(record, "sheen", item.Sheen)
	setNumeric(record, "transmission", item.Transmission)
	setNumeric(record, "iridescence", item.Iridescence)
	setNumeric(record, "anisotropy", item.Anisotropy)
	setString(record, "normalMap", item.NormalMap)
	setString(record, "roughnessMap", item.RoughnessMap)
	setString(record, "metalnessMap", item.MetalnessMap)
	setString(record, "emissiveMap", item.EmissiveMap)
	setString(record, "lodGroup", item.LODGroup)
	setInt(record, "lodLevel", item.LODLevel)
	setNumeric(record, "lodMinDistance", item.LODMinDistance)
	setNumeric(record, "lodMaxDistance", item.LODMaxDistance)
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "rotationX", item.RotationX)
	setNumeric(record, "rotationY", item.RotationY)
	setNumeric(record, "rotationZ", item.RotationZ)
	setNumeric(record, "scaleX", item.ScaleX)
	setNumeric(record, "scaleY", item.ScaleY)
	setNumeric(record, "scaleZ", item.ScaleZ)
	setNumeric(record, "spinX", item.SpinX)
	setNumeric(record, "spinY", item.SpinY)
	setNumeric(record, "spinZ", item.SpinZ)
	setNumeric(record, "shiftX", item.ShiftX)
	setNumeric(record, "shiftY", item.ShiftY)
	setNumeric(record, "shiftZ", item.ShiftZ)
	setNumeric(record, "driftSpeed", item.DriftSpeed)
	setNumeric(record, "driftPhase", item.DriftPhase)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func legacyModels(items []ModelIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record := item.legacyProps(); record != nil {
			out = append(out, record)
		}
	}
	return out
}

func (item ModelIR) legacyProps() map[string]any {
	src := strings.TrimSpace(item.Src)
	if src == "" {
		return nil
	}
	record := map[string]any{
		"id":  item.ID,
		"src": src,
	}
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "rotationX", item.RotationX)
	setNumeric(record, "rotationY", item.RotationY)
	setNumeric(record, "rotationZ", item.RotationZ)
	setNumeric(record, "scaleX", item.ScaleX)
	setNumeric(record, "scaleY", item.ScaleY)
	setNumeric(record, "scaleZ", item.ScaleZ)
	setNumeric(record, "bounds", item.Bounds)
	setString(record, "fit", item.Fit)
	setString(record, "fitAlign", item.FitAlign)
	if item.Static != nil {
		record["static"] = *item.Static
	}
	if item.MaterialKind != "" {
		record["materialKind"] = item.MaterialKind
	}
	if item.Color != "" {
		record["color"] = item.Color
	}
	if item.Texture != "" {
		record["texture"] = item.Texture
	}
	if item.Opacity != nil {
		record["opacity"] = *item.Opacity
	}
	if item.Emissive != nil {
		record["emissive"] = *item.Emissive
	}
	setNumeric(record, "roughness", item.Roughness)
	setNumeric(record, "metalness", item.Metalness)
	setNumeric(record, "clearcoat", item.Clearcoat)
	setNumeric(record, "sheen", item.Sheen)
	setNumeric(record, "transmission", item.Transmission)
	setNumeric(record, "iridescence", item.Iridescence)
	setNumeric(record, "anisotropy", item.Anisotropy)
	setString(record, "normalMap", item.NormalMap)
	setString(record, "roughnessMap", item.RoughnessMap)
	setString(record, "metalnessMap", item.MetalnessMap)
	setString(record, "emissiveMap", item.EmissiveMap)
	if item.BlendMode != "" {
		record["blendMode"] = item.BlendMode
	}
	if item.RenderPass != "" {
		record["renderPass"] = item.RenderPass
	}
	if item.Wireframe != nil {
		record["wireframe"] = *item.Wireframe
	}
	if item.DepthWrite != nil {
		record["depthWrite"] = *item.DepthWrite
	}
	if item.Pickable != nil {
		record["pickable"] = *item.Pickable
	}
	if item.Visible != nil {
		record["visible"] = *item.Visible
	}
	setString(record, "animation", item.Animation)
	setString(record, "animationSeq", item.AnimationSeq)
	setNumericPtr(record, "animationSpeed", nonNegativeFloatPtr(item.AnimationSpeed))
	setNumericPtr(record, "animationWeight", nonNegativeFloatPtr(item.AnimationWeight))
	setIntPtr(record, "animationFadeInMS", nonNegativeIntPtr(item.AnimationFadeInMS))
	setIntPtr(record, "animationFadeOutMS", nonNegativeIntPtr(item.AnimationFadeOutMS))
	if item.Loop != nil {
		record["loop"] = *item.Loop
	}
	setString(record, "lodGroup", item.LODGroup)
	setInt(record, "lodLevel", item.LODLevel)
	setNumeric(record, "lodMinDistance", item.LODMinDistance)
	setNumeric(record, "lodMaxDistance", item.LODMaxDistance)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func legacyPointsList(items []PointsIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item PointsIR) legacyProps() map[string]any {
	record := map[string]any{
		"id":    item.ID,
		"count": item.Count,
	}
	if len(item.CompressedPositions) > 0 {
		record["compressedPositions"] = item.CompressedPositions
	} else if len(item.Positions) > 0 {
		record["positions"] = item.Positions
	}
	if len(item.PreviewPositions) > 0 {
		record["previewPositions"] = item.PreviewPositions
	}
	if len(item.CompressedSizes) > 0 {
		record["compressedSizes"] = item.CompressedSizes
	} else if len(item.Sizes) > 0 {
		record["sizes"] = item.Sizes
	}
	if len(item.PreviewSizes) > 0 {
		record["previewSizes"] = item.PreviewSizes
	}
	if len(item.Colors) > 0 {
		record["colors"] = item.Colors
	}
	setString(record, "color", item.Color)
	setString(record, "style", item.Style)
	setNumeric(record, "size", item.Size)
	setNumeric(record, "minPixelSize", item.MinPixelSize)
	setNumeric(record, "maxPixelSize", item.MaxPixelSize)
	setNumeric(record, "opacity", item.Opacity)
	setString(record, "blendMode", item.BlendMode)
	if item.DepthWrite != nil {
		record["depthWrite"] = *item.DepthWrite
	}
	if item.Attenuation {
		record["attenuation"] = true
	}
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "rotationX", item.RotationX)
	setNumeric(record, "rotationY", item.RotationY)
	setNumeric(record, "rotationZ", item.RotationZ)
	setNumeric(record, "spinX", item.SpinX)
	setNumeric(record, "spinY", item.SpinY)
	setNumeric(record, "spinZ", item.SpinZ)
	setInt(record, "positionStride", item.PositionStride)
	// Authored shader fields (nil → absent → builtin pipeline used).
	setString(record, "customVertex", item.CustomVertex)
	setString(record, "customFragment", item.CustomFragment)
	setString(record, "customVertexWGSL", item.CustomVertexWGSL)
	setString(record, "customFragmentWGSL", item.CustomFragmentWGSL)
	setString(record, "customVertexRef", item.CustomVertexRef)
	setString(record, "customFragmentRef", item.CustomFragmentRef)
	setString(record, "customVertexWGSLRef", item.CustomVertexWGSLRef)
	setString(record, "customFragmentWGSLRef", item.CustomFragmentWGSLRef)
	if len(item.CustomUniforms) > 0 {
		record["customUniforms"] = item.CustomUniforms
	}
	setString(record, "shaderBackend", item.ShaderBackend)
	if len(item.ShaderLayout) > 0 {
		record["shaderLayout"] = item.ShaderLayout
	}
	setString(record, "shaderSource", item.ShaderSource)
	if len(item.ShaderSourceFiles) > 0 {
		record["shaderSourceFiles"] = cloneSceneStringMap(item.ShaderSourceFiles)
	}
	setString(record, "qualityGroup", item.QualityGroup)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func legacyInstancedMeshes(items []InstancedMeshIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item InstancedMeshIR) legacyProps() map[string]any {
	record := map[string]any{
		"id":    item.ID,
		"count": item.Count,
		"kind":  item.Kind,
	}
	setNumeric(record, "size", item.Size)
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	setNumeric(record, "depth", item.Depth)
	setNumeric(record, "radius", item.Radius)
	setNumeric(record, "radiusTop", item.RadiusTop)
	setNumeric(record, "radiusBottom", item.RadiusBottom)
	setNumeric(record, "tube", item.Tube)
	setInt(record, "segments", item.Segments)
	setInt(record, "radialSegments", item.RadialSegments)
	setInt(record, "tubularSegments", item.TubularSegments)
	setString(record, "materialKind", item.MaterialKind)
	setString(record, "color", item.Color)
	setString(record, "texture", item.Texture)
	setNumericPtr(record, "opacity", item.Opacity)
	setNumericPtr(record, "emissive", item.Emissive)
	setString(record, "blendMode", item.BlendMode)
	setString(record, "renderPass", item.RenderPass)
	setBool(record, "wireframe", item.Wireframe)
	setBool(record, "depthWrite", item.DepthWrite)
	setNumeric(record, "roughness", item.Roughness)
	setNumeric(record, "metalness", item.Metalness)
	setNumeric(record, "clearcoat", item.Clearcoat)
	setNumeric(record, "sheen", item.Sheen)
	setNumeric(record, "transmission", item.Transmission)
	setNumeric(record, "iridescence", item.Iridescence)
	setNumeric(record, "anisotropy", item.Anisotropy)
	setString(record, "normalMap", item.NormalMap)
	setString(record, "roughnessMap", item.RoughnessMap)
	setString(record, "metalnessMap", item.MetalnessMap)
	setString(record, "emissiveMap", item.EmissiveMap)
	if len(item.CompressedTransforms) > 0 {
		record["compressedTransforms"] = item.CompressedTransforms
	} else if len(item.Transforms) > 0 {
		record["transforms"] = item.Transforms
	}
	if len(item.Colors) > 0 {
		record["colors"] = item.Colors
	}
	if len(item.Attributes) > 0 {
		record["attributes"] = item.Attributes
	}
	if len(item.PreviewTransforms) > 0 {
		record["previewTransforms"] = item.PreviewTransforms
	}
	setInt(record, "transformStride", item.TransformStride)
	setBool(record, "pickable", item.Pickable)
	if item.CastShadow {
		record["castShadow"] = true
	}
	if item.ReceiveShadow {
		record["receiveShadow"] = true
	}
	// GPU cull kernel fields (additive — only emitted when set).
	if item.CullKernelWGSL != "" {
		record["cullKernelWGSL"] = item.CullKernelWGSL
	}
	if item.CullKernelWGSLRef != "" {
		record["cullKernelWGSLRef"] = item.CullKernelWGSLRef
	}
	setString(record, "cullKernelEntry", item.CullKernelEntry)
	setNumeric(record, "cullRadius", item.CullRadius)
	setString(record, "cullBackend", item.CullBackend)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func legacyInstancedGLBMeshes(items []InstancedGLBMeshIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item InstancedGLBMeshIR) legacyProps() map[string]any {
	src := strings.TrimSpace(item.Src)
	if src == "" {
		return nil
	}
	record := map[string]any{
		"id":  item.ID,
		"src": src,
	}
	setString(record, "materialKind", item.MaterialKind)
	setString(record, "color", item.Color)
	setString(record, "texture", item.Texture)
	setNumericPtr(record, "opacity", item.Opacity)
	setNumericPtr(record, "emissive", item.Emissive)
	setString(record, "blendMode", item.BlendMode)
	setNumeric(record, "roughness", item.Roughness)
	setNumeric(record, "metalness", item.Metalness)
	if item.Pickable != nil {
		record["pickable"] = *item.Pickable
	}
	if item.Visible != nil {
		record["visible"] = *item.Visible
	}
	if item.Static != nil {
		record["static"] = *item.Static
	}
	if len(item.Instances) > 0 {
		instances := make([]map[string]any, 0, len(item.Instances))
		for _, inst := range item.Instances {
			instRecord := map[string]any{}
			if inst.ID != "" {
				instRecord["id"] = inst.ID
			}
			setNumeric(instRecord, "x", inst.X)
			setNumeric(instRecord, "y", inst.Y)
			setNumeric(instRecord, "z", inst.Z)
			setNumeric(instRecord, "scaleX", inst.ScaleX)
			setNumeric(instRecord, "scaleY", inst.ScaleY)
			setNumeric(instRecord, "scaleZ", inst.ScaleZ)
			setNumeric(instRecord, "rotationX", inst.RotationX)
			setNumeric(instRecord, "rotationY", inst.RotationY)
			setNumeric(instRecord, "rotationZ", inst.RotationZ)
			instances = append(instances, instRecord)
		}
		record["instances"] = instances
	}
	return record
}

func legacyComputeParticles(items []ComputeParticlesIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item ComputeParticlesIR) legacyProps() map[string]any {
	record := map[string]any{
		"id":    item.ID,
		"count": item.Count,
	}
	emitter := map[string]any{}
	setString(emitter, "kind", item.Emitter.Kind)
	setNumeric(emitter, "x", item.Emitter.X)
	setNumeric(emitter, "y", item.Emitter.Y)
	setNumeric(emitter, "z", item.Emitter.Z)
	setNumeric(emitter, "radius", item.Emitter.Radius)
	setNumeric(emitter, "rate", item.Emitter.Rate)
	setNumeric(emitter, "lifetime", item.Emitter.Lifetime)
	setInt(emitter, "arms", item.Emitter.Arms)
	setNumeric(emitter, "wind", item.Emitter.Wind)
	setNumeric(emitter, "scatter", item.Emitter.Scatter)
	setNumeric(emitter, "rotationX", item.Emitter.RotationX)
	setNumeric(emitter, "rotationY", item.Emitter.RotationY)
	setNumeric(emitter, "rotationZ", item.Emitter.RotationZ)
	setNumeric(emitter, "spinX", item.Emitter.SpinX)
	setNumeric(emitter, "spinY", item.Emitter.SpinY)
	setNumeric(emitter, "spinZ", item.Emitter.SpinZ)
	if item.Emitter.Once {
		emitter["once"] = true
	}
	record["emitter"] = emitter
	if len(item.Forces) > 0 {
		forces := make([]map[string]any, 0, len(item.Forces))
		for _, f := range item.Forces {
			force := map[string]any{}
			setString(force, "kind", f.Kind)
			setNumeric(force, "strength", f.Strength)
			setNumeric(force, "x", f.X)
			setNumeric(force, "y", f.Y)
			setNumeric(force, "z", f.Z)
			setNumeric(force, "frequency", f.Frequency)
			forces = append(forces, force)
		}
		record["forces"] = forces
	}
	material := map[string]any{}
	setString(material, "color", item.Material.Color)
	setString(material, "colorEnd", item.Material.ColorEnd)
	setString(material, "style", item.Material.Style)
	setNumeric(material, "size", item.Material.Size)
	setNumeric(material, "sizeEnd", item.Material.SizeEnd)
	setNumeric(material, "opacity", item.Material.Opacity)
	setNumeric(material, "opacityEnd", item.Material.OpacityEnd)
	setString(material, "blendMode", item.Material.BlendMode)
	if item.Material.Attenuation {
		material["attenuation"] = true
	}
	record["material"] = material
	setNumeric(record, "bounds", item.Bounds)
	// computeWGSL is stored verbatim: do not TrimSpace (callers own the source).
	if item.ComputeWGSL != "" {
		record["computeWGSL"] = item.ComputeWGSL
	}
	setString(record, "computeEntry", item.ComputeEntry)
	setString(record, "computeBackend", item.ComputeBackend)
	// Render-pass authored shader fields.
	setString(record, "renderVertex", item.RenderVertex)
	setString(record, "renderFragment", item.RenderFragment)
	setString(record, "renderVertexWGSL", item.RenderVertexWGSL)
	setString(record, "renderFragmentWGSL", item.RenderFragmentWGSL)
	setString(record, "renderVertexRef", item.RenderVertexRef)
	setString(record, "renderFragmentRef", item.RenderFragmentRef)
	setString(record, "renderVertexWGSLRef", item.RenderVertexWGSLRef)
	setString(record, "renderFragmentWGSLRef", item.RenderFragmentWGSLRef)
	if len(item.RenderUniforms) > 0 {
		record["renderUniforms"] = item.RenderUniforms
	}
	setString(record, "renderShaderBackend", item.RenderShaderBackend)
	if len(item.RenderShaderLayout) > 0 {
		record["renderShaderLayout"] = item.RenderShaderLayout
	}
	setString(record, "renderShaderSource", item.RenderShaderSource)
	if len(item.RenderShaderSourceFiles) > 0 {
		record["renderShaderSourceFiles"] = cloneSceneStringMap(item.RenderShaderSourceFiles)
	}
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func legacyWaterSystems(items []WaterSystemIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item WaterSystemIR) legacyProps() map[string]any {
	record := map[string]any{
		"id": item.ID,
	}
	setString(record, "interactionProfile", item.InteractionProfile)
	setString(record, "interactionTarget", item.InteractionTarget)
	setString(record, "interactionObject", item.InteractionObject)
	setInt(record, "resolution", item.Resolution)
	setInt(record, "surfaceResolution", item.SurfaceResolution)
	setInt(record, "surfaceMeshResolution", item.SurfaceMeshResolution)
	setString(record, "poolShape", item.PoolShape)
	setNumeric(record, "poolWidth", item.PoolWidth)
	setNumeric(record, "poolHeight", item.PoolHeight)
	setNumeric(record, "poolLength", item.PoolLength)
	setNumeric(record, "cornerRadius", item.CornerRadius)
	setNumeric(record, "waveSpeed", item.WaveSpeed)
	setNumeric(record, "damping", item.Damping)
	setNumeric(record, "normalScale", item.NormalScale)
	setInt(record, "seedDrops", item.SeedDrops)
	setNumeric(record, "dropRadius", item.DropRadius)
	setNumeric(record, "dropStrength", item.DropStrength)
	setInt(record, "dropEventID", item.DropEventID)
	setNumeric(record, "dropX", item.DropX)
	setNumeric(record, "dropZ", item.DropZ)
	setNumeric(record, "dropEventRadius", item.DropEventRadius)
	setNumeric(record, "dropEventStrength", item.DropEventStrength)
	setString(record, "tileTexture", item.TileTexture)
	setString(record, "cubeMap", item.CubeMap)
	setString(record, "shallowColor", item.ShallowColor)
	setString(record, "deepColor", item.DeepColor)
	setNumeric(record, "aboveWaterColorR", item.AboveWaterColorR)
	setNumeric(record, "aboveWaterColorG", item.AboveWaterColorG)
	setNumeric(record, "aboveWaterColorB", item.AboveWaterColorB)
	setInt(record, "causticsResolution", item.CausticsResolution)
	setInt(record, "objectTextureResolution", item.ObjectTextureResolution)
	setString(record, "objectTextureResolutionMode", item.ObjectTextureResolutionMode)
	setInt(record, "objectTexturePixelBudget", item.ObjectTexturePixelBudget)
	setInt(record, "objectShadowResolution", item.ObjectShadowResolution)
	if item.Caustics {
		record["caustics"] = true
	}
	if item.Reflection {
		record["reflection"] = true
	}
	if item.Refraction {
		record["refraction"] = true
	}
	if item.Paused {
		record["paused"] = true
	}
	if item.FollowCamera {
		record["followCamera"] = true
	}
	setNumeric(record, "lightDirectionX", item.LightDirectionX)
	setNumeric(record, "lightDirectionY", item.LightDirectionY)
	setNumeric(record, "lightDirectionZ", item.LightDirectionZ)
	setString(record, "activeObject", item.ActiveObject)
	setString(record, "objectKind", item.ObjectKind)
	setNumeric(record, "objectX", item.ObjectX)
	setNumeric(record, "objectY", item.ObjectY)
	setNumeric(record, "objectZ", item.ObjectZ)
	if item.ObjectPreviousSet {
		record["objectPreviousSet"] = true
	}
	setNumeric(record, "objectPreviousX", item.ObjectPreviousX)
	setNumeric(record, "objectPreviousY", item.ObjectPreviousY)
	setNumeric(record, "objectPreviousZ", item.ObjectPreviousZ)
	setNumeric(record, "objectRadius", item.ObjectRadius)
	setNumeric(record, "objectHalfSizeX", item.ObjectHalfSizeX)
	setNumeric(record, "objectHalfSizeY", item.ObjectHalfSizeY)
	setNumeric(record, "objectHalfSizeZ", item.ObjectHalfSizeZ)
	setNumeric(record, "objectDriftX", item.ObjectDriftX)
	setNumeric(record, "objectDriftY", item.ObjectDriftY)
	setNumeric(record, "objectDriftZ", item.ObjectDriftZ)
	setNumeric(record, "objectBobAmplitude", item.ObjectBobAmplitude)
	setNumeric(record, "objectBobSpeed", item.ObjectBobSpeed)
	setNumeric(record, "objectDisplacementScale", item.ObjectDisplacementScale)
	if len(item.ObjectDisplacementSpheres) > 0 {
		spheres := make([]map[string]any, 0, len(item.ObjectDisplacementSpheres))
		for _, sphere := range item.ObjectDisplacementSpheres {
			child := make(map[string]any)
			setNumeric(child, "offsetX", sphere.OffsetX)
			setNumeric(child, "offsetY", sphere.OffsetY)
			setNumeric(child, "offsetZ", sphere.OffsetZ)
			setNumeric(child, "radius", sphere.Radius)
			spheres = append(spheres, child)
		}
		record["objectDisplacementSpheres"] = spheres
	}
	if len(item.ObjectDisplacementEvents) > 0 {
		events := make([]map[string]any, 0, len(item.ObjectDisplacementEvents))
		for _, event := range item.ObjectDisplacementEvents {
			child := make(map[string]any)
			if event.ID > 0 {
				child["id"] = event.ID
			}
			setString(child, "activeObject", event.ActiveObject)
			setString(child, "objectKind", event.ObjectKind)
			setString(child, "objectSubtype", event.ObjectSubtype)
			setNumeric(child, "objectX", event.ObjectX)
			setNumeric(child, "objectY", event.ObjectY)
			setNumeric(child, "objectZ", event.ObjectZ)
			if event.ObjectPreviousSet {
				child["objectPreviousSet"] = true
			}
			setNumeric(child, "objectPreviousX", event.ObjectPreviousX)
			setNumeric(child, "objectPreviousY", event.ObjectPreviousY)
			setNumeric(child, "objectPreviousZ", event.ObjectPreviousZ)
			setNumeric(child, "objectRadius", event.ObjectRadius)
			setNumeric(child, "objectHalfSizeX", event.ObjectHalfSizeX)
			setNumeric(child, "objectHalfSizeY", event.ObjectHalfSizeY)
			setNumeric(child, "objectHalfSizeZ", event.ObjectHalfSizeZ)
			setNumeric(child, "objectDisplacementScale", event.ObjectDisplacementScale)
			if len(event.ObjectDisplacementSpheres) > 0 {
				spheres := make([]map[string]any, 0, len(event.ObjectDisplacementSpheres))
				for _, sphere := range event.ObjectDisplacementSpheres {
					grandchild := make(map[string]any)
					setNumeric(grandchild, "offsetX", sphere.OffsetX)
					setNumeric(grandchild, "offsetY", sphere.OffsetY)
					setNumeric(grandchild, "offsetZ", sphere.OffsetZ)
					setNumeric(grandchild, "radius", sphere.Radius)
					spheres = append(spheres, grandchild)
				}
				child["objectDisplacementSpheres"] = spheres
			}
			events = append(events, child)
		}
		record["objectDisplacementEvents"] = events
	}
	setString(record, "computeBackend", item.ComputeBackend)
	setString(record, "materialBackend", item.MaterialBackend)
	setString(record, "computeSource", item.ComputeSource)
	setString(record, "materialSource", item.MaterialSource)
	if len(item.ComputeSourceFiles) > 0 {
		record["computeSourceFiles"] = cloneSceneStringMap(item.ComputeSourceFiles)
	}
	if len(item.MaterialSourceFiles) > 0 {
		record["materialSourceFiles"] = cloneSceneStringMap(item.MaterialSourceFiles)
	}
	setString(record, "seedWGSL", item.SeedWGSL)
	setString(record, "dropWGSL", item.DropWGSL)
	setString(record, "displacementWGSL", item.DisplacementWGSL)
	setString(record, "simulationWGSL", item.SimulationWGSL)
	setString(record, "normalWGSL", item.NormalWGSL)
	setString(record, "causticsWGSL", item.CausticsWGSL)
	setString(record, "poolVertexWGSL", item.PoolVertexWGSL)
	setString(record, "poolFragmentWGSL", item.PoolFragmentWGSL)
	setString(record, "poolSelenaWGSL", item.PoolSelenaWGSL)
	setString(record, "surfaceVertexWGSL", item.SurfaceVertexWGSL)
	setString(record, "surfaceFragmentWGSL", item.SurfaceFragmentWGSL)
	setString(record, "surfaceBelowFragmentWGSL", item.SurfaceBelowFragmentWGSL)
	setString(record, "objectShadowWGSL", item.ObjectShadowWGSL)
	setString(record, "objectMeshShadowVertexWGSL", item.ObjectMeshShadowVertexWGSL)
	setString(record, "objectMeshShadowFragmentWGSL", item.ObjectMeshShadowFragmentWGSL)
	setString(record, "surfaceSelenaWGSL", item.SurfaceSelenaWGSL)
	setString(record, "surfaceBelowSelenaWGSL", item.SurfaceBelowSelenaWGSL)
	setString(record, "causticsSelenaWGSL", item.CausticsSelenaWGSL)
	setString(record, "objectShadowSelenaWGSL", item.ObjectShadowSelenaWGSL)
	setString(record, "compoundShadowSelenaWGSL", item.CompoundShadowSelenaWGSL)
	setString(record, "objectMeshShadowSelenaWGSL", item.ObjectMeshShadowSelenaWGSL)
	setString(record, "seedSelenaWGSL", item.SeedSelenaWGSL)
	setString(record, "dropSelenaWGSL", item.DropSelenaWGSL)
	setString(record, "displacementSelenaWGSL", item.DisplacementSelenaWGSL)
	setString(record, "simulationSelenaWGSL", item.SimulationSelenaWGSL)
	setString(record, "normalSelenaWGSL", item.NormalSelenaWGSL)
	setString(record, "displacementWGSLRef", item.DisplacementWGSLRef)
	setString(record, "seedWGSLRef", item.SeedWGSLRef)
	setString(record, "dropWGSLRef", item.DropWGSLRef)
	setString(record, "simulationWGSLRef", item.SimulationWGSLRef)
	setString(record, "normalWGSLRef", item.NormalWGSLRef)
	setString(record, "causticsWGSLRef", item.CausticsWGSLRef)
	setString(record, "poolVertexWGSLRef", item.PoolVertexWGSLRef)
	setString(record, "poolFragmentWGSLRef", item.PoolFragmentWGSLRef)
	setString(record, "surfaceVertexWGSLRef", item.SurfaceVertexWGSLRef)
	setString(record, "surfaceFragmentWGSLRef", item.SurfaceFragmentWGSLRef)
	setString(record, "surfaceBelowFragmentWGSLRef", item.SurfaceBelowFragmentWGSLRef)
	setString(record, "objectShadowWGSLRef", item.ObjectShadowWGSLRef)
	setString(record, "objectMeshShadowVertexWGSLRef", item.ObjectMeshShadowVertexWGSLRef)
	setString(record, "objectMeshShadowFragmentWGSLRef", item.ObjectMeshShadowFragmentWGSLRef)

	// Selena-compiled GLSL/GLES render + sim slots. They feed the WebGL/WebGL2
	// water fallback (the A1 sim driver + A2 render passes) and are ignored by
	// the WebGPU path, which consumes the *SelenaWGSL slots above instead.
	// Emitted inline (not hoisted into shaderLib) so the runtime reads them
	// directly.
	setString(record, "seedVertexGLSL", item.SeedVertexGLSL)
	setString(record, "seedFragmentGLSL", item.SeedFragmentGLSL)
	setString(record, "seedVertexGLES", item.SeedVertexGLES)
	setString(record, "seedFragmentGLES", item.SeedFragmentGLES)
	setString(record, "dropVertexGLSL", item.DropVertexGLSL)
	setString(record, "dropFragmentGLSL", item.DropFragmentGLSL)
	setString(record, "dropVertexGLES", item.DropVertexGLES)
	setString(record, "dropFragmentGLES", item.DropFragmentGLES)
	setString(record, "displacementVertexGLSL", item.DisplacementVertexGLSL)
	setString(record, "displacementFragmentGLSL", item.DisplacementFragmentGLSL)
	setString(record, "displacementVertexGLES", item.DisplacementVertexGLES)
	setString(record, "displacementFragmentGLES", item.DisplacementFragmentGLES)
	setString(record, "simulationVertexGLSL", item.SimulationVertexGLSL)
	setString(record, "simulationFragmentGLSL", item.SimulationFragmentGLSL)
	setString(record, "simulationVertexGLES", item.SimulationVertexGLES)
	setString(record, "simulationFragmentGLES", item.SimulationFragmentGLES)
	setString(record, "normalVertexGLSL", item.NormalVertexGLSL)
	setString(record, "normalFragmentGLSL", item.NormalFragmentGLSL)
	setString(record, "normalVertexGLES", item.NormalVertexGLES)
	setString(record, "normalFragmentGLES", item.NormalFragmentGLES)
	setString(record, "causticsVertexGLSL", item.CausticsVertexGLSL)
	setString(record, "causticsFragmentGLSL", item.CausticsFragmentGLSL)
	setString(record, "causticsVertexGLES", item.CausticsVertexGLES)
	setString(record, "causticsFragmentGLES", item.CausticsFragmentGLES)
	setString(record, "poolVertexGLSL", item.PoolVertexGLSL)
	setString(record, "poolFragmentGLSL", item.PoolFragmentGLSL)
	setString(record, "poolVertexGLES", item.PoolVertexGLES)
	setString(record, "poolFragmentGLES", item.PoolFragmentGLES)
	setString(record, "surfaceVertexGLSL", item.SurfaceVertexGLSL)
	setString(record, "surfaceFragmentGLSL", item.SurfaceFragmentGLSL)
	setString(record, "surfaceVertexGLES", item.SurfaceVertexGLES)
	setString(record, "surfaceFragmentGLES", item.SurfaceFragmentGLES)
	setString(record, "surfaceBelowVertexGLSL", item.SurfaceBelowVertexGLSL)
	setString(record, "surfaceBelowFragmentGLSL", item.SurfaceBelowFragmentGLSL)
	setString(record, "surfaceBelowVertexGLES", item.SurfaceBelowVertexGLES)
	setString(record, "surfaceBelowFragmentGLES", item.SurfaceBelowFragmentGLES)
	setString(record, "objectShadowVertexGLSL", item.ObjectShadowVertexGLSL)
	setString(record, "objectShadowFragmentGLSL", item.ObjectShadowFragmentGLSL)
	setString(record, "objectShadowVertexGLES", item.ObjectShadowVertexGLES)
	setString(record, "objectShadowFragmentGLES", item.ObjectShadowFragmentGLES)
	setString(record, "compoundShadowVertexGLSL", item.CompoundShadowVertexGLSL)
	setString(record, "compoundShadowFragmentGLSL", item.CompoundShadowFragmentGLSL)
	setString(record, "compoundShadowVertexGLES", item.CompoundShadowVertexGLES)
	setString(record, "compoundShadowFragmentGLES", item.CompoundShadowFragmentGLES)
	setString(record, "objectMeshShadowVertexGLSL", item.ObjectMeshShadowVertexGLSL)
	setString(record, "objectMeshShadowFragmentGLSL", item.ObjectMeshShadowFragmentGLSL)
	setString(record, "objectMeshShadowVertexGLES", item.ObjectMeshShadowVertexGLES)
	setString(record, "objectMeshShadowFragmentGLES", item.ObjectMeshShadowFragmentGLES)
	setString(record, "objectMaterialVertexGLSL", item.ObjectMaterialVertexGLSL)
	setString(record, "objectMaterialFragmentGLSL", item.ObjectMaterialFragmentGLSL)
	setString(record, "objectMaterialVertexGLES", item.ObjectMaterialVertexGLES)
	setString(record, "objectMaterialFragmentGLES", item.ObjectMaterialFragmentGLES)
	setString(record, "duckMaterialVertexGLSL", item.DuckMaterialVertexGLSL)
	setString(record, "duckMaterialFragmentGLSL", item.DuckMaterialFragmentGLSL)
	setString(record, "duckMaterialVertexGLES", item.DuckMaterialVertexGLES)
	setString(record, "duckMaterialFragmentGLES", item.DuckMaterialFragmentGLES)
	if len(item.ShaderDescriptors) > 0 {
		descriptors := make(map[string]any, len(item.ShaderDescriptors))
		for k, v := range item.ShaderDescriptors {
			clone := make(json.RawMessage, len(v))
			copy(clone, v)
			descriptors[k] = clone
		}
		record["shaderDescriptors"] = descriptors
	}
	return record
}

func legacyAnimations(items []AnimationClipIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.legacyProps())
	}
	return out
}

func (item AnimationClipIR) legacyProps() map[string]any {
	record := map[string]any{
		"name":     item.Name,
		"duration": item.Duration,
	}
	if len(item.Channels) > 0 {
		channels := make([]map[string]any, 0, len(item.Channels))
		for _, ch := range item.Channels {
			channels = append(channels, ch.legacyProps())
		}
		record["channels"] = channels
	}
	return record
}

func (ch AnimationChannelIR) legacyProps() map[string]any {
	record := map[string]any{
		"targetNode": ch.TargetNode,
		"property":   ch.Property,
	}
	setString(record, "interpolation", ch.Interpolation)
	if len(ch.CompressedTimes) > 0 {
		record["compressedTimes"] = ch.CompressedTimes
	} else if len(ch.Times) > 0 {
		record["times"] = ch.Times
	}
	if len(ch.PreviewTimes) > 0 {
		record["previewTimes"] = ch.PreviewTimes
	}
	if len(ch.CompressedValues) > 0 {
		record["compressedValues"] = ch.CompressedValues
	} else if len(ch.Values) > 0 {
		record["values"] = ch.Values
	}
	if len(ch.PreviewValues) > 0 {
		record["previewValues"] = ch.PreviewValues
	}
	return record
}

func legacyLabels(items []LabelIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record := item.legacyProps(); record != nil {
			out = append(out, record)
		}
	}
	return out
}

func legacySprites(items []SpriteIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record := item.legacyProps(); record != nil {
			out = append(out, record)
		}
	}
	return out
}

func legacyHTML(items []HTMLIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record := item.legacyProps(); record != nil {
			out = append(out, record)
		}
	}
	return out
}

func (item LabelIR) legacyProps() map[string]any {
	text := strings.TrimSpace(item.Text)
	if text == "" {
		return nil
	}
	record := map[string]any{
		"id":   item.ID,
		"text": text,
	}
	if className := strings.TrimSpace(item.ClassName); className != "" {
		record["className"] = className
	}
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "priority", item.Priority)
	setNumeric(record, "shiftX", item.ShiftX)
	setNumeric(record, "shiftY", item.ShiftY)
	setNumeric(record, "shiftZ", item.ShiftZ)
	setNumeric(record, "driftSpeed", item.DriftSpeed)
	setNumeric(record, "driftPhase", item.DriftPhase)
	setNumeric(record, "maxWidth", item.MaxWidth)
	setInt(record, "maxLines", item.MaxLines)
	setString(record, "overflow", item.Overflow)
	setString(record, "font", item.Font)
	setNumeric(record, "lineHeight", item.LineHeight)
	setString(record, "color", item.Color)
	setString(record, "background", item.Background)
	setString(record, "borderColor", item.BorderColor)
	setNumeric(record, "offsetX", item.OffsetX)
	setNumeric(record, "offsetY", item.OffsetY)
	setNumeric(record, "anchorX", item.AnchorX)
	setNumeric(record, "anchorY", item.AnchorY)
	setString(record, "collision", item.Collision)
	if item.Occlude {
		record["occlude"] = true
	}
	setString(record, "whiteSpace", item.WhiteSpace)
	setString(record, "textAlign", item.TextAlign)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func (item SpriteIR) legacyProps() map[string]any {
	src := strings.TrimSpace(item.Src)
	if src == "" {
		return nil
	}
	record := map[string]any{
		"id":  item.ID,
		"src": src,
	}
	setString(record, "className", item.ClassName)
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "priority", item.Priority)
	setNumeric(record, "shiftX", item.ShiftX)
	setNumeric(record, "shiftY", item.ShiftY)
	setNumeric(record, "shiftZ", item.ShiftZ)
	setNumeric(record, "driftSpeed", item.DriftSpeed)
	setNumeric(record, "driftPhase", item.DriftPhase)
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	setNumeric(record, "scale", item.Scale)
	setNumeric(record, "opacity", item.Opacity)
	setNumeric(record, "offsetX", item.OffsetX)
	setNumeric(record, "offsetY", item.OffsetY)
	setNumeric(record, "anchorX", item.AnchorX)
	setNumeric(record, "anchorY", item.AnchorY)
	if item.Occlude {
		record["occlude"] = true
	}
	setString(record, "fit", item.Fit)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func (item HTMLIR) legacyProps() map[string]any {
	html := strings.TrimSpace(item.HTML)
	if html == "" {
		return nil
	}
	record := map[string]any{
		"id":   item.ID,
		"html": html,
	}
	setString(record, "target", item.Target)
	setString(record, "mode", item.Mode)
	setString(record, "className", item.ClassName)
	setString(record, "fallback", item.Fallback)
	setString(record, "fallbackReason", item.FallbackReason)
	setString(record, "textureKey", item.TextureKey)
	setInt(record, "textureWidth", item.TextureWidth)
	setInt(record, "textureHeight", item.TextureHeight)
	setInt(record, "maxTexturePixels", item.MaxTexturePixels)
	setNumeric(record, "surfaceWidth", item.SurfaceWidth)
	setNumeric(record, "surfaceHeight", item.SurfaceHeight)
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "priority", item.Priority)
	setNumeric(record, "shiftX", item.ShiftX)
	setNumeric(record, "shiftY", item.ShiftY)
	setNumeric(record, "shiftZ", item.ShiftZ)
	setNumeric(record, "driftSpeed", item.DriftSpeed)
	setNumeric(record, "driftPhase", item.DriftPhase)
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	setNumeric(record, "scale", item.Scale)
	setNumeric(record, "opacity", item.Opacity)
	setNumeric(record, "offsetX", item.OffsetX)
	setNumeric(record, "offsetY", item.OffsetY)
	setNumeric(record, "anchorX", item.AnchorX)
	setNumeric(record, "anchorY", item.AnchorY)
	if item.Occlude {
		record["occlude"] = true
	}
	setString(record, "pointerEvents", item.PointerEvents)
	applySceneLifecycleRecord(record, item.Transition, nil, nil, item.Live)
	return record
}

func legacyLights(items []LightIR) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record := item.legacyProps(); record != nil {
			out = append(out, record)
		}
	}
	return out
}

func (item LightIR) legacyProps() map[string]any {
	kind := strings.TrimSpace(item.Kind)
	if kind == "" {
		return nil
	}
	record := map[string]any{
		"id":   item.ID,
		"kind": kind,
	}
	setString(record, "color", item.Color)
	setString(record, "groundColor", item.GroundColor)
	setNumeric(record, "intensity", item.Intensity)
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "directionX", item.DirectionX)
	setNumeric(record, "directionY", item.DirectionY)
	setNumeric(record, "directionZ", item.DirectionZ)
	setNumeric(record, "angle", item.Angle)
	setNumeric(record, "penumbra", item.Penumbra)
	setNumeric(record, "range", item.Range)
	setNumeric(record, "decay", item.Decay)
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	if coefficients := legacyLinePoints(item.Coefficients); len(coefficients) > 0 {
		record["coefficients"] = coefficients
	}
	if item.CastShadow {
		record["castShadow"] = true
	}
	setNumeric(record, "shadowBias", item.ShadowBias)
	setInt(record, "shadowSize", item.ShadowSize)
	setInt(record, "shadowCascades", item.ShadowCascades)
	setNumeric(record, "shadowSoftness", item.ShadowSoftness)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

// IsZero is required for the `json:"environment,omitzero"` tag on SceneIR
// to omit zero-valued environments. Must be exported so encoding/json's
// struct-tag reflection can find it.
func (item EnvironmentIR) IsZero() bool {
	return item.AmbientColor == "" &&
		item.AmbientIntensity == 0 &&
		item.SkyColor == "" &&
		item.SkyIntensity == 0 &&
		item.GroundColor == "" &&
		item.GroundIntensity == 0 &&
		item.EnvMap == "" &&
		item.EnvIntensity == 0 &&
		item.EnvRotation == 0 &&
		item.Exposure == 0 &&
		item.ToneMapping == "" &&
		item.FogColor == "" &&
		item.FogDensity == 0 &&
		item.Transition.IsZero() &&
		len(item.InState) == 0 &&
		len(item.OutState) == 0 &&
		len(item.Live) == 0
}

func (item EnvironmentIR) legacyProps() map[string]any {
	if item.IsZero() {
		return nil
	}
	record := map[string]any{}
	setString(record, "ambientColor", item.AmbientColor)
	setNumeric(record, "ambientIntensity", item.AmbientIntensity)
	setString(record, "skyColor", item.SkyColor)
	setNumeric(record, "skyIntensity", item.SkyIntensity)
	setString(record, "groundColor", item.GroundColor)
	setNumeric(record, "groundIntensity", item.GroundIntensity)
	setString(record, "envMap", item.EnvMap)
	setNumeric(record, "envIntensity", item.EnvIntensity)
	setNumeric(record, "envRotation", item.EnvRotation)
	setNumeric(record, "exposure", item.Exposure)
	setString(record, "toneMapping", item.ToneMapping)
	setString(record, "fogColor", item.FogColor)
	setNumeric(record, "fogDensity", item.FogDensity)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func (environment Environment) sceneIR() EnvironmentIR {
	return EnvironmentIR{
		AmbientColor:     strings.TrimSpace(environment.AmbientColor),
		AmbientIntensity: environment.AmbientIntensity,
		SkyColor:         strings.TrimSpace(environment.SkyColor),
		SkyIntensity:     environment.SkyIntensity,
		GroundColor:      strings.TrimSpace(environment.GroundColor),
		GroundIntensity:  environment.GroundIntensity,
		EnvMap:           strings.TrimSpace(environment.EnvironmentMap),
		EnvIntensity:     environment.EnvIntensity,
		EnvRotation:      environment.EnvRotation,
		Exposure:         environment.Exposure,
		ToneMapping:      strings.TrimSpace(environment.ToneMapping),
		FogColor:         strings.TrimSpace(environment.FogColor),
		FogDensity:       environment.FogDensity,
		Transition:       lowerTransition(environment.Transition),
		InState:          environment.InState.legacyProps(),
		OutState:         environment.OutState.legacyProps(),
		Live:             normalizeLive(environment.Live),
	}
}

func applySceneLifecycleRecord(record map[string]any, transition TransitionIR, inState, outState map[string]any, live []string) {
	if record == nil {
		return
	}
	if current := transition.legacyProps(); len(current) > 0 {
		record["transition"] = current
	}
	if len(inState) > 0 {
		record["inState"] = inState
	}
	if len(outState) > 0 {
		record["outState"] = outState
	}
	if len(live) > 0 {
		record["live"] = append([]string(nil), live...)
	}
}

var collectFeatureOrder = []capability.Feature{
	capability.FeatureWaterObjectMeshShadowPass,
	capability.FeatureWaterObjectTexturePass,
	capability.FeatureWaterSim,
	capability.FeatureIBL,
	capability.FeatureGPUPicking,
	capability.FeatureLineDashed,
	capability.FeatureSkinning,
}

func waterSystemUsesObjectTexturePass(w WaterSystemIR) bool {
	if w.ObjectTextureResolution > 0 || strings.TrimSpace(w.ObjectTextureResolutionMode) != "" || w.ObjectTexturePixelBudget > 0 || w.ObjectShadowResolution > 0 {
		return true
	}
	if waterObjectKindUsesObjectTexturePass(w.ObjectKind) || waterObjectKindUsesObjectTexturePass(w.ActiveObject) || waterObjectKindUsesObjectTexturePass(w.InteractionObject) {
		return true
	}
	if len(w.ObjectDisplacementSpheres) > 0 {
		return true
	}
	for _, event := range w.ObjectDisplacementEvents {
		if len(event.ObjectDisplacementSpheres) > 0 || waterObjectKindUsesObjectTexturePass(event.ObjectKind) || waterObjectKindUsesObjectTexturePass(event.ActiveObject) {
			return true
		}
	}
	return false
}

func waterObjectKindUsesObjectTexturePass(value string) bool {
	return waterObjectKindUsesMeshProjectedPass(value)
}

func waterSystemUsesObjectMeshShadowPass(w WaterSystemIR) bool {
	if strings.TrimSpace(w.ObjectMeshShadowVertexWGSL) != "" ||
		strings.TrimSpace(w.ObjectMeshShadowFragmentWGSL) != "" ||
		strings.TrimSpace(w.ObjectMeshShadowVertexWGSLRef) != "" ||
		strings.TrimSpace(w.ObjectMeshShadowFragmentWGSLRef) != "" {
		return true
	}
	if waterObjectKindUsesMeshProjectedPass(w.ObjectKind) ||
		waterObjectKindUsesMeshProjectedPass(w.ActiveObject) ||
		waterObjectKindUsesMeshProjectedPass(w.InteractionObject) {
		return true
	}
	for _, event := range w.ObjectDisplacementEvents {
		if waterObjectKindUsesMeshProjectedPass(event.ObjectKind) || waterObjectKindUsesMeshProjectedPass(event.ActiveObject) {
			return true
		}
	}
	return false
}

func waterObjectKindUsesMeshProjectedPass(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" || text == "none" || text == "off" || text == "disabled" || text == "no_object" {
		return false
	}
	return strings.Contains(text, "compound") ||
		strings.Contains(text, "mesh") ||
		strings.Contains(text, "torus") ||
		strings.Contains(text, "duck") ||
		strings.Contains(text, "gltf") ||
		strings.Contains(text, "glb")
}

// collectFeatures inspects the wire SceneIR and returns the de-duplicated set
// of backend-constrained features the scene uses. Only features detectable
// from the wire record are returned; caller-side features (skinning, custom
// shader) are handled by later tasks.
func collectFeatures(ir SceneIR) []capability.Feature {
	seen := map[capability.Feature]bool{}

	// ibl: environment has a non-empty env-map.
	if strings.TrimSpace(ir.Environment.EnvMap) != "" {
		seen[capability.FeatureIBL] = true
	}

	// gpu-picking: any ObjectIR or InstancedGLBMeshIR is explicitly pickable.
	for i := range ir.Objects {
		if ir.Objects[i].Pickable != nil && *ir.Objects[i].Pickable {
			seen[capability.FeatureGPUPicking] = true
			break
		}
	}
	if !seen[capability.FeatureGPUPicking] {
		for i := range ir.Models {
			if ir.Models[i].Pickable != nil && *ir.Models[i].Pickable {
				seen[capability.FeatureGPUPicking] = true
				break
			}
		}
	}
	if !seen[capability.FeatureGPUPicking] {
		for i := range ir.InstancedGLBMeshes {
			if ir.InstancedGLBMeshes[i].Pickable != nil && *ir.InstancedGLBMeshes[i].Pickable {
				seen[capability.FeatureGPUPicking] = true
				break
			}
		}
	}

	// line-dashed: any ObjectIR has LineDash set to true (set by LineDashedMaterial lowering).
	for i := range ir.Objects {
		if ir.Objects[i].LineDash != nil && *ir.Objects[i].LineDash {
			seen[capability.FeatureLineDashed] = true
			break
		}
	}
	if !seen[capability.FeatureLineDashed] {
		for i := range ir.Models {
			if ir.Models[i].LineDash != nil && *ir.Models[i].LineDash {
				seen[capability.FeatureLineDashed] = true
				break
			}
		}
	}

	if len(ir.WaterSystems) > 0 {
		seen[capability.FeatureWaterSim] = true
		for i := range ir.WaterSystems {
			if waterSystemUsesObjectTexturePass(ir.WaterSystems[i]) {
				seen[capability.FeatureWaterObjectTexturePass] = true
			}
			if waterSystemUsesObjectMeshShadowPass(ir.WaterSystems[i]) {
				seen[capability.FeatureWaterObjectMeshShadowPass] = true
			}
			if seen[capability.FeatureWaterObjectTexturePass] && seen[capability.FeatureWaterObjectMeshShadowPass] {
				break
			}
		}
	}

	// skinning: check build-time probe for each Model and InstancedGLBMesh.
	if skinLookup != nil && !seen[capability.FeatureSkinning] {
		for i := range ir.Models {
			if skinLookup.Skinned(ir.Models[i].Src) {
				seen[capability.FeatureSkinning] = true
				break
			}
		}
	}
	if skinLookup != nil && !seen[capability.FeatureSkinning] {
		for i := range ir.InstancedGLBMeshes {
			if skinLookup.Skinned(ir.InstancedGLBMeshes[i].Src) {
				seen[capability.FeatureSkinning] = true
				break
			}
		}
	}

	features := make([]capability.Feature, 0, len(seen))
	for _, f := range collectFeatureOrder {
		if seen[f] {
			features = append(features, f)
			delete(seen, f)
		}
	}
	for f := range seen {
		features = append(features, f)
	}
	return features
}
