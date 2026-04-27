package scene

import (
	"strings"
	"time"
)

// Easing selects the interpolation curve used for scene transitions.
type Easing string

const (
	Linear    Easing = "linear"
	EaseIn    Easing = "ease-in"
	EaseOut   Easing = "ease-out"
	EaseInOut Easing = "ease-in-out"
)

// Transition controls how a node enters, exits, and responds to live updates.
type Transition struct {
	In     TransitionTiming
	Out    TransitionTiming
	Update TransitionTiming
}

// TransitionTiming controls duration and easing for one transition phase.
type TransitionTiming struct {
	Duration time.Duration
	Easing   Easing
}

// Int allocates an int for optional scene numeric fields.
func Int(value int) *int {
	return &value
}

// String allocates a string for optional scene string fields.
func String(value string) *string {
	return &value
}

// Live declares the hub events a node should listen to for live updates.
func Live(events ...string) []string {
	if len(events) == 0 {
		return nil
	}
	out := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		out = append(out, event)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeLive(events []string) []string {
	if len(events) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(events))
	out := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event == "" {
			continue
		}
		if _, ok := seen[event]; ok {
			continue
		}
		seen[event] = struct{}{}
		out = append(out, event)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type transitionState interface {
	legacyProps() map[string]any
}

// MeshProps is a partial mesh prop bag used for transition enter/exit states.
type MeshProps struct {
	Kind          *string
	Size          *float64
	Width         *float64
	Height        *float64
	Depth         *float64
	Radius        *float64
	Color         *string
	Texture       *string
	Opacity       *float64
	Emissive      *float64
	BlendMode     *MaterialBlendMode
	RenderPass    *MaterialRenderPass
	Wireframe     *bool
	Pickable      *bool
	CastShadow    *bool
	ReceiveShadow *bool
	DepthWrite    *bool
	Position      *Vector3
	Rotation      *Euler
	Scale         *Vector3
	Spin          *Euler
	Drift         *Vector3
	DriftSpeed    *float64
	DriftPhase    *float64
}

// PointsProps is a partial points prop bag used for transition states.
type PointsProps struct {
	Count       *int
	Color       *string
	Style       *PointStyle
	Size        *float64
	Opacity     *float64
	BlendMode   *MaterialBlendMode
	DepthWrite  *bool
	Attenuation *bool
	Position    *Vector3
	Rotation    *Euler
	Spin        *Euler
}

// InstancedMeshProps is a partial instanced-mesh prop bag used for transition states.
type InstancedMeshProps struct {
	Count         *int
	Color         *string
	Colors        []string
	Attributes    map[string][]float64
	CastShadow    *bool
	ReceiveShadow *bool
}

// ComputeParticlesProps is a partial compute-particles prop bag used for transition states.
type ComputeParticlesProps struct {
	Count    *int
	Emitter  *ParticleEmitter
	Forces   []ParticleForce
	Material *ParticleMaterial
	Bounds   *float64
}

// LabelProps is a partial label prop bag used for transition states.
type LabelProps struct {
	Text        *string
	ClassName   *string
	Position    *Vector3
	Priority    *float64
	Shift       *Vector3
	DriftSpeed  *float64
	DriftPhase  *float64
	MaxWidth    *float64
	MaxLines    *int
	Overflow    *string
	Font        *string
	LineHeight  *float64
	Color       *string
	Background  *string
	BorderColor *string
	OffsetX     *float64
	OffsetY     *float64
	AnchorX     *float64
	AnchorY     *float64
	Collision   *string
	Occlude     *bool
	WhiteSpace  *string
	TextAlign   *string
}

// SpriteProps is a partial sprite prop bag used for transition states.
type SpriteProps struct {
	Src        *string
	ClassName  *string
	Position   *Vector3
	Priority   *float64
	Shift      *Vector3
	DriftSpeed *float64
	DriftPhase *float64
	Width      *float64
	Height     *float64
	Scale      *float64
	Opacity    *float64
	OffsetX    *float64
	OffsetY    *float64
	AnchorX    *float64
	AnchorY    *float64
	Occlude    *bool
	Fit        *string
}

// ModelProps is a partial model prop bag used for transition states.
type ModelProps struct {
	Src        *string
	Position   *Vector3
	Rotation   *Euler
	Scale      *Vector3
	Color      *string
	Texture    *string
	Opacity    *float64
	Emissive   *float64
	BlendMode  *MaterialBlendMode
	RenderPass *MaterialRenderPass
	Wireframe  *bool
	Pickable   *bool
	Static     *bool
	Animation  *string
	Loop       *bool
}

// LightProps is a partial light prop bag used for transition states.
type LightProps struct {
	Color          *string
	GroundColor    *string
	Intensity      *float64
	Position       *Vector3
	Direction      *Vector3
	Range          *float64
	Decay          *float64
	Angle          *float64
	Penumbra       *float64
	CastShadow     *bool
	ShadowBias     *float64
	ShadowSize     *int
	ShadowSoftness *float64
}

// EnvironmentProps is a partial environment prop bag used for transition states.
type EnvironmentProps struct {
	AmbientColor     *string
	AmbientIntensity *float64
	SkyColor         *string
	SkyIntensity     *float64
	GroundColor      *string
	GroundIntensity  *float64
	EnvMap           *string
	EnvIntensity     *float64
	EnvRotation      *float64
	Exposure         *float64
	ToneMapping      *string
	FogColor         *string
	FogDensity       *float64
}

func (props *MeshProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setStringPtr(record, "kind", props.Kind)
	setNumericPtr(record, "size", props.Size)
	setNumericPtr(record, "width", props.Width)
	setNumericPtr(record, "height", props.Height)
	setNumericPtr(record, "depth", props.Depth)
	setNumericPtr(record, "radius", props.Radius)
	setStringPtr(record, "color", props.Color)
	setStringPtr(record, "texture", props.Texture)
	setNumericPtr(record, "opacity", props.Opacity)
	setNumericPtr(record, "emissive", props.Emissive)
	setEnumPtr(record, "blendMode", props.BlendMode)
	setEnumPtr(record, "renderPass", props.RenderPass)
	setBoolPtr(record, "wireframe", props.Wireframe)
	setBoolPtr(record, "pickable", props.Pickable)
	setBoolPtr(record, "castShadow", props.CastShadow)
	setBoolPtr(record, "receiveShadow", props.ReceiveShadow)
	setBoolPtr(record, "depthWrite", props.DepthWrite)
	setVector3Ptr(record, "", props.Position)
	setEulerPtr(record, "", props.Rotation)
	setScalePtr(record, props.Scale)
	setEulerPtr(record, "spin", props.Spin)
	setVector3Ptr(record, "shift", props.Drift)
	setNumericPtr(record, "driftSpeed", props.DriftSpeed)
	setNumericPtr(record, "driftPhase", props.DriftPhase)
	return trimEmptyRecord(record)
}

func (props *PointsProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setIntPtr(record, "count", props.Count)
	setStringPtr(record, "color", props.Color)
	setEnumPtr(record, "style", props.Style)
	setNumericPtr(record, "size", props.Size)
	setNumericPtr(record, "opacity", props.Opacity)
	setEnumPtr(record, "blendMode", props.BlendMode)
	setBoolPtr(record, "depthWrite", props.DepthWrite)
	setBoolPtr(record, "attenuation", props.Attenuation)
	setVector3Ptr(record, "", props.Position)
	setEulerPtr(record, "", props.Rotation)
	setEulerPtr(record, "spin", props.Spin)
	return trimEmptyRecord(record)
}

func (props *InstancedMeshProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setIntPtr(record, "count", props.Count)
	setStringPtr(record, "color", props.Color)
	if len(props.Colors) > 0 {
		record["colors"] = append([]string(nil), props.Colors...)
	}
	if len(props.Attributes) > 0 {
		record["attributes"] = cloneFloat64Slices(props.Attributes)
	}
	setBoolPtr(record, "castShadow", props.CastShadow)
	setBoolPtr(record, "receiveShadow", props.ReceiveShadow)
	return trimEmptyRecord(record)
}

func (props *ComputeParticlesProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setIntPtr(record, "count", props.Count)
	if props.Emitter != nil {
		emitter := map[string]any{}
		setString(emitter, "kind", strings.TrimSpace(props.Emitter.Kind))
		setVector3Ptr(emitter, "", &props.Emitter.Position)
		setNumeric(emitter, "radius", props.Emitter.Radius)
		setNumeric(emitter, "rate", props.Emitter.Rate)
		setNumeric(emitter, "lifetime", props.Emitter.Lifetime)
		setInt(emitter, "arms", props.Emitter.Arms)
		setNumeric(emitter, "wind", props.Emitter.Wind)
		setNumeric(emitter, "scatter", props.Emitter.Scatter)
		record["emitter"] = trimEmptyRecord(emitter)
	}
	if len(props.Forces) > 0 {
		forces := make([]map[string]any, 0, len(props.Forces))
		for _, force := range props.Forces {
			recordForce := map[string]any{}
			setString(recordForce, "kind", strings.TrimSpace(force.Kind))
			setNumeric(recordForce, "strength", force.Strength)
			setNumeric(recordForce, "frequency", force.Frequency)
			setVector3Ptr(recordForce, "", &force.Direction)
			forces = append(forces, trimEmptyRecord(recordForce))
		}
		record["forces"] = forces
	}
	if props.Material != nil {
		material := map[string]any{}
		setString(material, "color", strings.TrimSpace(props.Material.Color))
		setString(material, "colorEnd", strings.TrimSpace(props.Material.ColorEnd))
		setString(material, "style", strings.TrimSpace(string(props.Material.Style)))
		setNumeric(material, "size", props.Material.Size)
		setNumeric(material, "sizeEnd", props.Material.SizeEnd)
		setNumeric(material, "opacity", props.Material.Opacity)
		setNumeric(material, "opacityEnd", props.Material.OpacityEnd)
		setString(material, "blendMode", strings.TrimSpace(string(props.Material.BlendMode)))
		if props.Material.Attenuation {
			material["attenuation"] = true
		}
		record["material"] = trimEmptyRecord(material)
	}
	setNumericPtr(record, "bounds", props.Bounds)
	return trimEmptyRecord(record)
}

func (props *LabelProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setStringPtr(record, "text", props.Text)
	setStringPtr(record, "className", props.ClassName)
	setVector3Ptr(record, "", props.Position)
	setNumericPtr(record, "priority", props.Priority)
	setVector3Ptr(record, "shift", props.Shift)
	setNumericPtr(record, "driftSpeed", props.DriftSpeed)
	setNumericPtr(record, "driftPhase", props.DriftPhase)
	setNumericPtr(record, "maxWidth", props.MaxWidth)
	setIntPtr(record, "maxLines", props.MaxLines)
	setStringPtr(record, "overflow", props.Overflow)
	setStringPtr(record, "font", props.Font)
	setNumericPtr(record, "lineHeight", props.LineHeight)
	setStringPtr(record, "color", props.Color)
	setStringPtr(record, "background", props.Background)
	setStringPtr(record, "borderColor", props.BorderColor)
	setNumericPtr(record, "offsetX", props.OffsetX)
	setNumericPtr(record, "offsetY", props.OffsetY)
	setNumericPtr(record, "anchorX", props.AnchorX)
	setNumericPtr(record, "anchorY", props.AnchorY)
	setStringPtr(record, "collision", props.Collision)
	setBoolPtr(record, "occlude", props.Occlude)
	setStringPtr(record, "whiteSpace", props.WhiteSpace)
	setStringPtr(record, "textAlign", props.TextAlign)
	return trimEmptyRecord(record)
}

func (props *SpriteProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setStringPtr(record, "src", props.Src)
	setStringPtr(record, "className", props.ClassName)
	setVector3Ptr(record, "", props.Position)
	setNumericPtr(record, "priority", props.Priority)
	setVector3Ptr(record, "shift", props.Shift)
	setNumericPtr(record, "driftSpeed", props.DriftSpeed)
	setNumericPtr(record, "driftPhase", props.DriftPhase)
	setNumericPtr(record, "width", props.Width)
	setNumericPtr(record, "height", props.Height)
	setNumericPtr(record, "scale", props.Scale)
	setNumericPtr(record, "opacity", props.Opacity)
	setNumericPtr(record, "offsetX", props.OffsetX)
	setNumericPtr(record, "offsetY", props.OffsetY)
	setNumericPtr(record, "anchorX", props.AnchorX)
	setNumericPtr(record, "anchorY", props.AnchorY)
	setBoolPtr(record, "occlude", props.Occlude)
	setStringPtr(record, "fit", props.Fit)
	return trimEmptyRecord(record)
}

func (props *ModelProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setStringPtr(record, "src", props.Src)
	setVector3Ptr(record, "", props.Position)
	setEulerPtr(record, "", props.Rotation)
	setScalePtr(record, props.Scale)
	setStringPtr(record, "color", props.Color)
	setStringPtr(record, "texture", props.Texture)
	setNumericPtr(record, "opacity", props.Opacity)
	setNumericPtr(record, "emissive", props.Emissive)
	setEnumPtr(record, "blendMode", props.BlendMode)
	setEnumPtr(record, "renderPass", props.RenderPass)
	setBoolPtr(record, "wireframe", props.Wireframe)
	setBoolPtr(record, "pickable", props.Pickable)
	setBoolPtr(record, "static", props.Static)
	setStringPtr(record, "animation", props.Animation)
	setBoolPtr(record, "loop", props.Loop)
	return trimEmptyRecord(record)
}

func (props *LightProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setStringPtr(record, "color", props.Color)
	setStringPtr(record, "groundColor", props.GroundColor)
	setNumericPtr(record, "intensity", props.Intensity)
	setVector3Ptr(record, "", props.Position)
	setVector3Ptr(record, "direction", props.Direction)
	setNumericPtr(record, "range", props.Range)
	setNumericPtr(record, "decay", props.Decay)
	setNumericPtr(record, "angle", props.Angle)
	setNumericPtr(record, "penumbra", props.Penumbra)
	setBoolPtr(record, "castShadow", props.CastShadow)
	setNumericPtr(record, "shadowBias", props.ShadowBias)
	setIntPtr(record, "shadowSize", props.ShadowSize)
	if props.ShadowSoftness != nil {
		softness := normalizeShadowSoftness(*props.ShadowSoftness)
		setNumericPtr(record, "shadowSoftness", &softness)
	}
	return trimEmptyRecord(record)
}

func (props *EnvironmentProps) legacyProps() map[string]any {
	if props == nil {
		return nil
	}
	record := map[string]any{}
	setStringPtr(record, "ambientColor", props.AmbientColor)
	setNumericPtr(record, "ambientIntensity", props.AmbientIntensity)
	setStringPtr(record, "skyColor", props.SkyColor)
	setNumericPtr(record, "skyIntensity", props.SkyIntensity)
	setStringPtr(record, "groundColor", props.GroundColor)
	setNumericPtr(record, "groundIntensity", props.GroundIntensity)
	setStringPtr(record, "envMap", props.EnvMap)
	setNumericPtr(record, "envIntensity", props.EnvIntensity)
	setNumericPtr(record, "envRotation", props.EnvRotation)
	setNumericPtr(record, "exposure", props.Exposure)
	setStringPtr(record, "toneMapping", props.ToneMapping)
	setStringPtr(record, "fogColor", props.FogColor)
	setNumericPtr(record, "fogDensity", props.FogDensity)
	return trimEmptyRecord(record)
}

func trimEmptyRecord(record map[string]any) map[string]any {
	if len(record) == 0 {
		return nil
	}
	return record
}

func setStringPtr(record map[string]any, key string, value *string) {
	if record == nil || value == nil {
		return
	}
	setString(record, key, *value)
}

func setEnumPtr[T ~string](record map[string]any, key string, value *T) {
	if record == nil || value == nil {
		return
	}
	setString(record, key, strings.TrimSpace(string(*value)))
}

func setBoolPtr(record map[string]any, key string, value *bool) {
	if record == nil || value == nil || strings.TrimSpace(key) == "" {
		return
	}
	record[key] = *value
}

func setIntPtr(record map[string]any, key string, value *int) {
	if record == nil || value == nil {
		return
	}
	setInt(record, key, *value)
}

func setVector3Ptr(record map[string]any, prefix string, value *Vector3) {
	if record == nil || value == nil {
		return
	}
	if prefix == "" {
		setNumeric(record, "x", value.X)
		setNumeric(record, "y", value.Y)
		setNumeric(record, "z", value.Z)
		return
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return
	}
	setNumeric(record, prefix+"X", value.X)
	setNumeric(record, prefix+"Y", value.Y)
	setNumeric(record, prefix+"Z", value.Z)
}

func setEulerPtr(record map[string]any, prefix string, value *Euler) {
	if record == nil || value == nil {
		return
	}
	if prefix == "" {
		setNumeric(record, "rotationX", value.X)
		setNumeric(record, "rotationY", value.Y)
		setNumeric(record, "rotationZ", value.Z)
		return
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return
	}
	setNumeric(record, prefix+"X", value.X)
	setNumeric(record, prefix+"Y", value.Y)
	setNumeric(record, prefix+"Z", value.Z)
}

func setScalePtr(record map[string]any, value *Vector3) {
	if record == nil || value == nil {
		return
	}
	setNumeric(record, "scaleX", value.X)
	setNumeric(record, "scaleY", value.Y)
	setNumeric(record, "scaleZ", value.Z)
}
