package scene

import (
	"encoding/json"
	"strconv"
	"strings"
)

// PostEffectIR is the typed compatibility record for one post-processing
// effect, lowered from the typed PostFX surface into the legacy props bag
// the JS post-processor reads.
//
// Concrete types additionally implement json.Marshaler so SceneIR can
// serialize a []PostEffectIR field via the standard reflection-based
// encoder without going through an intermediate map[string]any.
// legacyProps is preserved for tests and introspection callers.
type PostEffectIR interface {
	legacyProps() map[string]any
}

// TonemapIR lowers Tonemap into the bundle.postEffects[i] shape:
//
//	{kind: "toneMapping", exposure: 1.0}
type TonemapIR struct {
	Mode     string  // "aces" | "reinhard" | "filmic"
	Exposure float64 // multiplier applied before the curve
}

func (ir TonemapIR) legacyProps() map[string]any {
	out := map[string]any{"kind": "toneMapping"}
	if ir.Exposure != 0 {
		out["exposure"] = ir.Exposure
	} else {
		out["exposure"] = 1.0
	}
	if ir.Mode != "" {
		out["mode"] = ir.Mode
	}
	return out
}

// MarshalJSON encodes the IR shape directly without allocating a
// map[string]any. Output is byte-identical (up to key order) to what
// legacyProps + json.Marshal would produce.
func (ir TonemapIR) MarshalJSON() ([]byte, error) {
	exposure := ir.Exposure
	if exposure == 0 {
		exposure = 1.0
	}
	var b strings.Builder
	b.Grow(64)
	b.WriteString(`{"kind":"toneMapping","exposure":`)
	b.WriteString(strconv.FormatFloat(exposure, 'f', -1, 64))
	if ir.Mode != "" {
		b.WriteString(`,"mode":`)
		b.WriteString(jsonString(ir.Mode))
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// BloomIR lowers Bloom into the bundle.postEffects[i] shape:
//
//	{kind: "bloom", threshold: 0.8, intensity: 0.5, radius: 5.0}
//
// Note: the Go-side field is named Strength, but the JS shader uniform is
// u_intensity. We translate at the IR boundary so the public Go API can use
// the more intuitive name without renaming the shader.
type BloomIR struct {
	Threshold float64
	Strength  float64
	Radius    float64
	Scale     float64 // emitted only when in (0, 1]
}

func (ir BloomIR) legacyProps() map[string]any {
	threshold := ir.Threshold
	if threshold == 0 {
		threshold = 0.8
	}
	strength := ir.Strength
	if strength == 0 {
		strength = 0.5
	}
	radius := ir.Radius
	if radius == 0 {
		radius = 5.0
	}
	out := map[string]any{
		"kind":      "bloom",
		"threshold": threshold,
		"intensity": strength,
		"radius":    radius,
	}
	if ir.Scale > 0 && ir.Scale <= 1 {
		out["scale"] = ir.Scale
	}
	return out
}

// MarshalJSON produces the same bytes as legacyProps + json.Marshal
// without allocating the intermediate map.
func (ir BloomIR) MarshalJSON() ([]byte, error) {
	threshold := ir.Threshold
	if threshold == 0 {
		threshold = 0.8
	}
	strength := ir.Strength
	if strength == 0 {
		strength = 0.5
	}
	radius := ir.Radius
	if radius == 0 {
		radius = 5.0
	}
	var b strings.Builder
	b.Grow(96)
	b.WriteString(`{"kind":"bloom","threshold":`)
	b.WriteString(strconv.FormatFloat(threshold, 'f', -1, 64))
	b.WriteString(`,"intensity":`)
	b.WriteString(strconv.FormatFloat(strength, 'f', -1, 64))
	b.WriteString(`,"radius":`)
	b.WriteString(strconv.FormatFloat(radius, 'f', -1, 64))
	if ir.Scale > 0 && ir.Scale <= 1 {
		b.WriteString(`,"scale":`)
		b.WriteString(strconv.FormatFloat(ir.Scale, 'f', -1, 64))
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// VignetteIR lowers Vignette.
type VignetteIR struct {
	Intensity float64
}

func (ir VignetteIR) legacyProps() map[string]any {
	intensity := ir.Intensity
	if intensity == 0 {
		intensity = 1.0
	}
	return map[string]any{
		"kind":      "vignette",
		"intensity": intensity,
	}
}

// MarshalJSON encodes the IR shape directly.
func (ir VignetteIR) MarshalJSON() ([]byte, error) {
	intensity := ir.Intensity
	if intensity == 0 {
		intensity = 1.0
	}
	var b strings.Builder
	b.Grow(48)
	b.WriteString(`{"kind":"vignette","intensity":`)
	b.WriteString(strconv.FormatFloat(intensity, 'f', -1, 64))
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// ColorGradeIR lowers ColorGrade.
type ColorGradeIR struct {
	Exposure   float64
	Contrast   float64
	Saturation float64
}

func (ir ColorGradeIR) legacyProps() map[string]any {
	exposure := ir.Exposure
	if exposure == 0 {
		exposure = 1.0
	}
	contrast := ir.Contrast
	if contrast == 0 {
		contrast = 1.0
	}
	saturation := ir.Saturation
	if saturation == 0 {
		saturation = 1.0
	}
	return map[string]any{
		"kind":       "colorGrade",
		"exposure":   exposure,
		"contrast":   contrast,
		"saturation": saturation,
	}
}

// MarshalJSON encodes the IR shape directly.
func (ir ColorGradeIR) MarshalJSON() ([]byte, error) {
	exposure := ir.Exposure
	if exposure == 0 {
		exposure = 1.0
	}
	contrast := ir.Contrast
	if contrast == 0 {
		contrast = 1.0
	}
	saturation := ir.Saturation
	if saturation == 0 {
		saturation = 1.0
	}
	var b strings.Builder
	b.Grow(96)
	b.WriteString(`{"kind":"colorGrade","exposure":`)
	b.WriteString(strconv.FormatFloat(exposure, 'f', -1, 64))
	b.WriteString(`,"contrast":`)
	b.WriteString(strconv.FormatFloat(contrast, 'f', -1, 64))
	b.WriteString(`,"saturation":`)
	b.WriteString(strconv.FormatFloat(saturation, 'f', -1, 64))
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// SSAOIR lowers SSAO.
type SSAOIR struct {
	Radius    float64
	Intensity float64
	Bias      float64
}

func (ir SSAOIR) legacyProps() map[string]any {
	radius := ir.Radius
	if radius == 0 {
		radius = 4.0
	}
	intensity := ir.Intensity
	if intensity == 0 {
		intensity = 0.55
	}
	out := map[string]any{
		"kind":      "ssao",
		"radius":    radius,
		"intensity": intensity,
	}
	if ir.Bias != 0 {
		out["bias"] = ir.Bias
	}
	return out
}

// MarshalJSON encodes the IR shape directly.
func (ir SSAOIR) MarshalJSON() ([]byte, error) {
	radius := ir.Radius
	if radius == 0 {
		radius = 4.0
	}
	intensity := ir.Intensity
	if intensity == 0 {
		intensity = 0.55
	}
	var b strings.Builder
	b.Grow(96)
	b.WriteString(`{"kind":"ssao","radius":`)
	b.WriteString(strconv.FormatFloat(radius, 'f', -1, 64))
	b.WriteString(`,"intensity":`)
	b.WriteString(strconv.FormatFloat(intensity, 'f', -1, 64))
	if ir.Bias != 0 {
		b.WriteString(`,"bias":`)
		b.WriteString(strconv.FormatFloat(ir.Bias, 'f', -1, 64))
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// DOFIR lowers DOF.
type DOFIR struct {
	FocusDistance float64
	Aperture      float64
	MaxBlur       float64
}

func (ir DOFIR) legacyProps() map[string]any {
	focusDistance := ir.FocusDistance
	if focusDistance == 0 {
		focusDistance = 8.0
	}
	aperture := ir.Aperture
	if aperture == 0 {
		aperture = 0.04
	}
	maxBlur := ir.MaxBlur
	if maxBlur == 0 {
		maxBlur = 8.0
	}
	return map[string]any{
		"kind":          "dof",
		"focusDistance": focusDistance,
		"aperture":      aperture,
		"maxBlur":       maxBlur,
	}
}

// MarshalJSON encodes the IR shape directly.
func (ir DOFIR) MarshalJSON() ([]byte, error) {
	focusDistance := ir.FocusDistance
	if focusDistance == 0 {
		focusDistance = 8.0
	}
	aperture := ir.Aperture
	if aperture == 0 {
		aperture = 0.04
	}
	maxBlur := ir.MaxBlur
	if maxBlur == 0 {
		maxBlur = 8.0
	}
	var b strings.Builder
	b.Grow(104)
	b.WriteString(`{"kind":"dof","focusDistance":`)
	b.WriteString(strconv.FormatFloat(focusDistance, 'f', -1, 64))
	b.WriteString(`,"aperture":`)
	b.WriteString(strconv.FormatFloat(aperture, 'f', -1, 64))
	b.WriteString(`,"maxBlur":`)
	b.WriteString(strconv.FormatFloat(maxBlur, 'f', -1, 64))
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// FXAAIR lowers FXAA into the bundle.postEffects[i] shape:
//
//	{kind: "fxaa"}
//
// FXAA has no tunable fields, so this IR carries no data beyond its kind.
type FXAAIR struct{}

func (ir FXAAIR) legacyProps() map[string]any {
	return map[string]any{"kind": "fxaa"}
}

// MarshalJSON encodes the IR shape directly.
func (ir FXAAIR) MarshalJSON() ([]byte, error) {
	return []byte(`{"kind":"fxaa"}`), nil
}

// CustomPostIR lowers CustomPost into the bundle.postEffects[i] shape.
// The WGSL/GLSL shaders and binding layout are forwarded from the material's
// CustomMaterial fields so the JS runtime can build pipelines from them
// without contacting the Go server again.
//
// Stage controls insertion point in the browser post chain:
//   - "" / "beforeTonemap" → after bloom, before tonemap (default)
//   - "afterTonemap"       → after tonemap, in the LDR region
type CustomPostIR struct {
	Name          string         `json:"name"`
	Stage         string         `json:"stage,omitempty"`
	FragmentWGSL  string         `json:"fragmentWGSL,omitempty"`
	VertexWGSL    string         `json:"vertexWGSL,omitempty"`
	FragmentGLSL  string         `json:"fragmentGLSL,omitempty"`
	VertexGLSL    string         `json:"vertexGLSL,omitempty"`
	ShaderBackend string         `json:"shaderBackend,omitempty"`
	ShaderLayout  map[string]any `json:"shaderLayout,omitempty"`
	Uniforms      map[string]any `json:"uniforms,omitempty"`
}

func (ir CustomPostIR) legacyProps() map[string]any {
	out := map[string]any{"kind": "customPost", "name": ir.Name}
	if ir.Stage != "" {
		out["stage"] = ir.Stage
	}
	if ir.FragmentWGSL != "" {
		out["fragmentWGSL"] = ir.FragmentWGSL
	}
	if ir.VertexWGSL != "" {
		out["vertexWGSL"] = ir.VertexWGSL
	}
	if ir.FragmentGLSL != "" {
		out["fragmentGLSL"] = ir.FragmentGLSL
	}
	if ir.VertexGLSL != "" {
		out["vertexGLSL"] = ir.VertexGLSL
	}
	if ir.ShaderBackend != "" {
		out["shaderBackend"] = ir.ShaderBackend
	}
	if len(ir.ShaderLayout) > 0 {
		out["shaderLayout"] = ir.ShaderLayout
	}
	if len(ir.Uniforms) > 0 {
		out["uniforms"] = ir.Uniforms
	}
	return out
}

// MarshalJSON encodes CustomPostIR directly. Output shape matches legacyProps.
func (ir CustomPostIR) MarshalJSON() ([]byte, error) {
	// Use a named struct alias to avoid infinite recursion while still picking
	// up the json tags from the original struct.
	type alias CustomPostIR
	type wrapper struct {
		Kind string `json:"kind"`
		alias
	}
	return json.Marshal(wrapper{Kind: "customPost", alias: alias(ir)})
}

// sceneIR converts the typed PostFX into the IR slice consumed by SceneIR.
func (pfx PostFX) sceneIR() []PostEffectIR {
	if len(pfx.Effects) == 0 {
		return nil
	}
	out := make([]PostEffectIR, 0, len(pfx.Effects))
	for _, e := range pfx.Effects {
		switch ev := e.(type) {
		case Tonemap:
			out = append(out, TonemapIR{
				Mode:     tonemapModeString(ev.Mode),
				Exposure: float64(ev.Exposure),
			})
		case Bloom:
			out = append(out, BloomIR{
				Threshold: float64(ev.Threshold),
				Strength:  float64(ev.Strength),
				Radius:    float64(ev.Radius),
				Scale:     float64(ev.Scale),
			})
		case Vignette:
			out = append(out, VignetteIR{
				Intensity: float64(ev.Intensity),
			})
		case ColorGrade:
			out = append(out, ColorGradeIR{
				Exposure:   float64(ev.Exposure),
				Contrast:   float64(ev.Contrast),
				Saturation: float64(ev.Saturation),
			})
		case SSAO:
			out = append(out, SSAOIR{
				Radius:    float64(ev.Radius),
				Intensity: float64(ev.Intensity),
				Bias:      float64(ev.Bias),
			})
		case DOF:
			out = append(out, DOFIR{
				FocusDistance: float64(ev.FocusDistance),
				Aperture:      float64(ev.Aperture),
				MaxBlur:       float64(ev.MaxBlur),
			})
		case FXAA:
			out = append(out, FXAAIR{})
		case CustomPost:
			ir := lowerCustomPost(ev)
			if ir != nil {
				out = append(out, *ir)
			}
		}
	}
	return out
}

// lowerCustomPost lowers a CustomPost scene value to a CustomPostIR. Returns
// nil when the material is absent (nothing to lower) so the caller can skip it.
func lowerCustomPost(cp CustomPost) *CustomPostIR {
	if cp.Material == nil {
		return nil
	}
	stage := string(cp.Stage)
	if stage == "" {
		stage = string(CustomPostBeforeTonemap)
	}
	ir := &CustomPostIR{
		Name:          cp.Name,
		Stage:         stage,
		FragmentWGSL:  cp.Material.FragmentWGSL,
		VertexWGSL:    cp.Material.VertexWGSL,
		FragmentGLSL:  cp.Material.FragmentGLSL,
		VertexGLSL:    cp.Material.VertexGLSL,
		ShaderBackend: cp.Material.ShaderBackend,
		ShaderLayout:  cp.Material.ShaderLayout,
	}
	// Merge material defaults with per-call overrides. Overrides win.
	merged := make(map[string]any, len(cp.Material.Uniforms)+len(cp.Uniforms))
	for k, v := range cp.Material.Uniforms {
		merged[k] = v
	}
	for k, v := range cp.Uniforms {
		merged[k] = v
	}
	if len(merged) > 0 {
		ir.Uniforms = merged
	}
	return ir
}

func tonemapModeString(m TonemapMode) string {
	switch m {
	case TonemapReinhard:
		return "reinhard"
	case TonemapFilmic:
		return "filmic"
	case TonemapACES:
		fallthrough
	default:
		return "aces"
	}
}
