package scene

// PostEffectIR is the typed compatibility record for one post-processing
// effect, lowered from the typed PostFX surface into the legacy props bag
// the JS post-processor reads.
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
		}
	}
	return out
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
