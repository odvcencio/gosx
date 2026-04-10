package scene

import "strings"

// CompressedArray holds a TurboQuant-compressed float array.
// The client checks for compressed fields first and falls back to raw arrays.
type CompressedArray struct {
	Packed   []byte  `json:"packed"`
	Norm     float32 `json:"norm"`             // min value (scalar quantization floor)
	MaxVal   float32 `json:"maxVal,omitempty"` // max value (scalar quantization ceiling)
	Dim      int     `json:"dim"`
	BitWidth int     `json:"bitWidth"`
	Count    int     `json:"count"` // number of original float64 values
}

// SceneIR is the typed lowered scene payload emitted from a Graph before it is
// serialized into the current Scene3D compatibility contract.
type SceneIR struct {
	Objects          []ObjectIR           `json:"objects,omitempty"`
	Models           []ModelIR            `json:"models,omitempty"`
	Points           []PointsIR           `json:"points,omitempty"`
	InstancedMeshes  []InstancedMeshIR    `json:"instancedMeshes,omitempty"`
	ComputeParticles []ComputeParticlesIR `json:"computeParticles,omitempty"`
	Animations       []AnimationClipIR    `json:"animations,omitempty"`
	Labels           []LabelIR            `json:"labels,omitempty"`
	Sprites          []SpriteIR           `json:"sprites,omitempty"`
	Lights           []LightIR            `json:"lights,omitempty"`
	Environment      EnvironmentIR        `json:"environment,omitempty"`
}

// ObjectIR is the typed compatibility record for one lowered scene object.
type ObjectIR struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	Size            float64        `json:"size,omitempty"`
	Width           float64        `json:"width,omitempty"`
	Height          float64        `json:"height,omitempty"`
	Depth           float64        `json:"depth,omitempty"`
	Radius          float64        `json:"radius,omitempty"`
	Segments        int            `json:"segments,omitempty"`
	Points          []Vector3      `json:"points,omitempty"`
	LineSegments    [][2]int       `json:"lineSegments,omitempty"`
	RadiusTop       float64        `json:"radiusTop,omitempty"`
	RadiusBottom    float64        `json:"radiusBottom,omitempty"`
	Tube            float64        `json:"tube,omitempty"`
	RadialSegments  int            `json:"radialSegments,omitempty"`
	TubularSegments int            `json:"tubularSegments,omitempty"`
	MaterialKind    string         `json:"materialKind,omitempty"`
	Color           string         `json:"color,omitempty"`
	Texture         string         `json:"texture,omitempty"`
	Opacity         *float64       `json:"opacity,omitempty"`
	Emissive        *float64       `json:"emissive,omitempty"`
	BlendMode       string         `json:"blendMode,omitempty"`
	RenderPass      string         `json:"renderPass,omitempty"`
	Wireframe       *bool          `json:"wireframe,omitempty"`
	Pickable        *bool          `json:"pickable,omitempty"`
	CastShadow      bool           `json:"castShadow,omitempty"`
	ReceiveShadow   bool           `json:"receiveShadow,omitempty"`
	DepthWrite      *bool          `json:"depthWrite,omitempty"`
	Roughness       float64        `json:"roughness,omitempty"`
	Metalness       float64        `json:"metalness,omitempty"`
	NormalMap       string         `json:"normalMap,omitempty"`
	RoughnessMap    string         `json:"roughnessMap,omitempty"`
	MetalnessMap    string         `json:"metalnessMap,omitempty"`
	EmissiveMap     string         `json:"emissiveMap,omitempty"`
	X               float64        `json:"x,omitempty"`
	Y               float64        `json:"y,omitempty"`
	Z               float64        `json:"z,omitempty"`
	RotationX       float64        `json:"rotationX,omitempty"`
	RotationY       float64        `json:"rotationY,omitempty"`
	RotationZ       float64        `json:"rotationZ,omitempty"`
	SpinX           float64        `json:"spinX,omitempty"`
	SpinY           float64        `json:"spinY,omitempty"`
	SpinZ           float64        `json:"spinZ,omitempty"`
	ShiftX          float64        `json:"shiftX,omitempty"`
	ShiftY          float64        `json:"shiftY,omitempty"`
	ShiftZ          float64        `json:"shiftZ,omitempty"`
	DriftSpeed      float64        `json:"driftSpeed,omitempty"`
	DriftPhase      float64        `json:"driftPhase,omitempty"`
	Transition      TransitionIR   `json:"transition,omitempty"`
	InState         map[string]any `json:"inState,omitempty"`
	OutState        map[string]any `json:"outState,omitempty"`
	Live            []string       `json:"live,omitempty"`
}

// ModelIR is the typed compatibility record for one scene model instance.
type ModelIR struct {
	ObjectIR
	Src       string  `json:"src,omitempty"`
	ScaleX    float64 `json:"scaleX,omitempty"`
	ScaleY    float64 `json:"scaleY,omitempty"`
	ScaleZ    float64 `json:"scaleZ,omitempty"`
	Static    *bool   `json:"static,omitempty"`
	Animation string  `json:"animation,omitempty"`
	Loop      *bool   `json:"loop,omitempty"`
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
	Transition  TransitionIR   `json:"transition,omitempty"`
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
	Transition TransitionIR   `json:"transition,omitempty"`
	InState    map[string]any `json:"inState,omitempty"`
	OutState   map[string]any `json:"outState,omitempty"`
	Live       []string       `json:"live,omitempty"`
}

// LightIR is the typed compatibility record for one lowered scene light.
type LightIR struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Color       string         `json:"color,omitempty"`
	GroundColor string         `json:"groundColor,omitempty"`
	Intensity   float64        `json:"intensity,omitempty"`
	X           float64        `json:"x,omitempty"`
	Y           float64        `json:"y,omitempty"`
	Z           float64        `json:"z,omitempty"`
	DirectionX  float64        `json:"directionX,omitempty"`
	DirectionY  float64        `json:"directionY,omitempty"`
	DirectionZ  float64        `json:"directionZ,omitempty"`
	Angle       float64        `json:"angle,omitempty"`
	Penumbra    float64        `json:"penumbra,omitempty"`
	Range       float64        `json:"range,omitempty"`
	Decay       float64        `json:"decay,omitempty"`
	CastShadow  bool           `json:"castShadow,omitempty"`
	ShadowBias  float64        `json:"shadowBias,omitempty"`
	ShadowSize  int            `json:"shadowSize,omitempty"`
	Transition  TransitionIR   `json:"transition,omitempty"`
	InState     map[string]any `json:"inState,omitempty"`
	OutState    map[string]any `json:"outState,omitempty"`
	Live        []string       `json:"live,omitempty"`
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
	Transition          TransitionIR      `json:"transition,omitempty"`
	InState             map[string]any    `json:"inState,omitempty"`
	OutState            map[string]any    `json:"outState,omitempty"`
	Live                []string          `json:"live,omitempty"`
}

// InstancedMeshIR is the typed compatibility record for one instanced mesh.
type InstancedMeshIR struct {
	ID                   string            `json:"id"`
	Count                int               `json:"count"`
	Kind                 string            `json:"kind"`
	Width                float64           `json:"width,omitempty"`
	Height               float64           `json:"height,omitempty"`
	Depth                float64           `json:"depth,omitempty"`
	Radius               float64           `json:"radius,omitempty"`
	Segments             int               `json:"segments,omitempty"`
	MaterialKind         string            `json:"materialKind,omitempty"`
	Color                string            `json:"color,omitempty"`
	Roughness            float64           `json:"roughness,omitempty"`
	Metalness            float64           `json:"metalness,omitempty"`
	Transforms           []float64         `json:"transforms"`
	CastShadow           bool              `json:"castShadow,omitempty"`
	ReceiveShadow        bool              `json:"receiveShadow,omitempty"`
	CompressedTransforms []CompressedArray `json:"compressedTransforms,omitempty"`
	PreviewTransforms    []CompressedArray `json:"previewTransforms,omitempty"`
	Transition           TransitionIR      `json:"transition,omitempty"`
	InState              map[string]any    `json:"inState,omitempty"`
	OutState             map[string]any    `json:"outState,omitempty"`
	Live                 []string          `json:"live,omitempty"`
}

// ComputeParticlesIR is the typed compatibility record for one GPU particle system.
type ComputeParticlesIR struct {
	ID         string             `json:"id"`
	Count      int                `json:"count"`
	Emitter    ParticleEmitterIR  `json:"emitter"`
	Forces     []ParticleForceIR  `json:"forces,omitempty"`
	Material   ParticleMaterialIR `json:"material"`
	Bounds     float64            `json:"bounds,omitempty"`
	Transition TransitionIR       `json:"transition,omitempty"`
	InState    map[string]any     `json:"inState,omitempty"`
	OutState   map[string]any     `json:"outState,omitempty"`
	Live       []string           `json:"live,omitempty"`
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
	Exposure         float64        `json:"exposure,omitempty"`
	ToneMapping      string         `json:"toneMapping,omitempty"`
	FogColor         string         `json:"fogColor,omitempty"`
	FogDensity       float64        `json:"fogDensity,omitempty"`
	Transition       TransitionIR   `json:"transition,omitempty"`
	InState          map[string]any `json:"inState,omitempty"`
	OutState         map[string]any `json:"outState,omitempty"`
	Live             []string       `json:"live,omitempty"`
}

// SceneIR lowers typed scene props into a typed intermediate representation.
func (p Props) SceneIR() SceneIR {
	ir := p.Graph.SceneIR()
	ir.Environment = p.Environment.sceneIR()
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
	return ir
}

// SceneIR lowers a typed graph into a typed intermediate representation.
func (g Graph) SceneIR() SceneIR {
	if len(g.Nodes) == 0 {
		return SceneIR{}
	}

	lowerer := &graphLowerer{
		anchors: make(map[string]worldTransform),
	}
	for _, node := range g.Nodes {
		lowerer.lowerNode(node, identityTransform())
	}
	return SceneIR{
		Objects:          append([]ObjectIR(nil), lowerer.objects...),
		Models:           append([]ModelIR(nil), lowerer.models...),
		Points:           append([]PointsIR(nil), lowerer.points...),
		InstancedMeshes:  append([]InstancedMeshIR(nil), lowerer.instancedMeshes...),
		ComputeParticles: append([]ComputeParticlesIR(nil), lowerer.computeParticles...),
		Animations:       append([]AnimationClipIR(nil), lowerer.animations...),
		Labels:           lowerer.resolveLabels(),
		Sprites:          lowerer.resolveSprites(),
		Lights:           append([]LightIR(nil), lowerer.lights...),
	}
}

func (ir SceneIR) isZero() bool {
	return len(ir.Objects) == 0 && len(ir.Models) == 0 && len(ir.Points) == 0 && len(ir.InstancedMeshes) == 0 && len(ir.ComputeParticles) == 0 && len(ir.Animations) == 0 && len(ir.Labels) == 0 && len(ir.Sprites) == 0 && len(ir.Lights) == 0 && ir.Environment.isZero()
}

func (ir SceneIR) legacyProps() map[string]any {
	if ir.isZero() {
		return nil
	}
	out := map[string]any{}
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
	if computeParticles := legacyComputeParticles(ir.ComputeParticles); len(computeParticles) > 0 {
		out["computeParticles"] = computeParticles
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
	if lights := legacyLights(ir.Lights); len(lights) > 0 {
		out["lights"] = lights
	}
	if environment := ir.Environment.legacyProps(); len(environment) > 0 {
		out["environment"] = environment
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
	if item.Pickable != nil {
		record["pickable"] = *item.Pickable
	}
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
	setString(record, "normalMap", item.NormalMap)
	setString(record, "roughnessMap", item.RoughnessMap)
	setString(record, "metalnessMap", item.MetalnessMap)
	setString(record, "emissiveMap", item.EmissiveMap)
	setNumeric(record, "x", item.X)
	setNumeric(record, "y", item.Y)
	setNumeric(record, "z", item.Z)
	setNumeric(record, "rotationX", item.RotationX)
	setNumeric(record, "rotationY", item.RotationY)
	setNumeric(record, "rotationZ", item.RotationZ)
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
	setString(record, "animation", item.Animation)
	if item.Loop != nil {
		record["loop"] = *item.Loop
	}
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
	setNumeric(record, "width", item.Width)
	setNumeric(record, "height", item.Height)
	setNumeric(record, "depth", item.Depth)
	setNumeric(record, "radius", item.Radius)
	setInt(record, "segments", item.Segments)
	setString(record, "materialKind", item.MaterialKind)
	setString(record, "color", item.Color)
	setNumeric(record, "roughness", item.Roughness)
	setNumeric(record, "metalness", item.Metalness)
	if len(item.CompressedTransforms) > 0 {
		record["compressedTransforms"] = item.CompressedTransforms
	} else if len(item.Transforms) > 0 {
		record["transforms"] = item.Transforms
	}
	if len(item.PreviewTransforms) > 0 {
		record["previewTransforms"] = item.PreviewTransforms
	}
	if item.CastShadow {
		record["castShadow"] = true
	}
	if item.ReceiveShadow {
		record["receiveShadow"] = true
	}
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
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
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
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
	if item.CastShadow {
		record["castShadow"] = true
	}
	setNumeric(record, "shadowBias", item.ShadowBias)
	setInt(record, "shadowSize", item.ShadowSize)
	applySceneLifecycleRecord(record, item.Transition, item.InState, item.OutState, item.Live)
	return record
}

func (item EnvironmentIR) isZero() bool {
	return item.AmbientColor == "" &&
		item.AmbientIntensity == 0 &&
		item.SkyColor == "" &&
		item.SkyIntensity == 0 &&
		item.GroundColor == "" &&
		item.GroundIntensity == 0 &&
		item.Exposure == 0 &&
		item.ToneMapping == "" &&
		item.FogColor == "" &&
		item.FogDensity == 0 &&
		item.Transition.isZero() &&
		len(item.InState) == 0 &&
		len(item.OutState) == 0 &&
		len(item.Live) == 0
}

func (item EnvironmentIR) legacyProps() map[string]any {
	if item.isZero() {
		return nil
	}
	record := map[string]any{}
	setString(record, "ambientColor", item.AmbientColor)
	setNumeric(record, "ambientIntensity", item.AmbientIntensity)
	setString(record, "skyColor", item.SkyColor)
	setNumeric(record, "skyIntensity", item.SkyIntensity)
	setString(record, "groundColor", item.GroundColor)
	setNumeric(record, "groundIntensity", item.GroundIntensity)
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
