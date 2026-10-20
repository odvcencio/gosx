package scene

import (
	_ "embed"
	"math"
)

// GeometricSpecularAA bounds the geometric normal variance added to the GGX
// lobe. Both controls are finite values clamped to [0,1]. Either zero disables
// filtering; Variance .15 and Threshold .2 are useful starting values. Filtering
// raises perceptual roughness, never lowers it or changes diffuse radiance.
type GeometricSpecularAA struct {
	Variance  float64 `json:"variance"`
	Threshold float64 `json:"threshold"`
}

func copySpecularAA(value *GeometricSpecularAA) *GeometricSpecularAA {
	if value == nil {
		return nil
	}
	return &GeometricSpecularAA{boundedAA(value.Variance), boundedAA(value.Threshold)}
}

func boundedAA(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Max(0, math.Min(1, value))
}

func specularAAFromValue(value any) *GeometricSpecularAA {
	switch v := value.(type) {
	case *GeometricSpecularAA:
		return copySpecularAA(v)
	case GeometricSpecularAA:
		return copySpecularAA(&v)
	case map[string]any:
		return copySpecularAA(&GeometricSpecularAA{mapFloat64(v["variance"]), mapFloat64(v["threshold"])})
	default:
		return nil
	}
}

// SelenaSpecularAASource provides gsxSpecularRoughness(normalWorld,
// perceptualRoughness, variance, threshold) for authored BRDFs. Prepend it before
// linking materials. Pass a normalized, smoothly interpolated geometric normal;
// call before divergent fragment control flow, and use its result consistently
// for direct specular, coat and environment sampling. Zero controls preserve
// the authored roughness exactly. Derivatives make this fragment-only.
//
//go:embed selena_specular_aa.sel
var SelenaSpecularAASource string
