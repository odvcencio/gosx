package scene

import "math"

// AutoExposure adapts the exposure of a scene to its average brightness over
// time, the way an eye or a camera does when it moves from a bright beach into
// a cave. Nil, the default, keeps the authored Exposure.
//
// The renderer measures the average log luminance of the linear HDR scene
// image each frame, computes the exposure that maps it to Key, and moves
// toward that exposure at SpeedUp (brightening) or SpeedDown (darkening)
// per second. The adapted value multiplies the authored exposure before tone
// mapping. A camera cut adapts at once. The scene must render through the
// post chain (for example with a tone-mapping effect); WebGPU measures with a
// compute pass and WebGL2 with a mip-chain reduction, and neither reads the
// value back to the CPU.
type AutoExposure struct {
	// MinExposure and MaxExposure bound the adapted multiplier. Zero uses
	// 0.1 and 8.
	MinExposure float64 `json:"minExposure,omitempty"`
	MaxExposure float64 `json:"maxExposure,omitempty"`
	// Key is the target average luminance (middle grey). Zero uses 0.18.
	Key float64 `json:"key,omitempty"`
	// SpeedUp and SpeedDown are adaptation rates per second. Zero uses 3
	// (brightening) and 1 (darkening).
	SpeedUp   float64 `json:"speedUp,omitempty"`
	SpeedDown float64 `json:"speedDown,omitempty"`
	// Compensation shifts the result in exposure stops (EV); +1 doubles it.
	Compensation float64 `json:"compensation,omitempty"`
}

// normalizeAutoExposure drops non-finite and negative values to the browser
// defaults (zero) and orders the bounds.
func normalizeAutoExposure(a *AutoExposure) *AutoExposure {
	if a == nil {
		return nil
	}
	positive := func(v float64) float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0
		}
		return v
	}
	out := AutoExposure{
		MinExposure: positive(a.MinExposure), MaxExposure: positive(a.MaxExposure), Key: positive(a.Key),
		SpeedUp: positive(a.SpeedUp), SpeedDown: positive(a.SpeedDown), Compensation: a.Compensation,
	}
	if math.IsNaN(out.Compensation) || math.IsInf(out.Compensation, 0) {
		out.Compensation = 0
	}
	out.Compensation = math.Max(-8, math.Min(8, out.Compensation))
	if out.MinExposure > 0 && out.MaxExposure > 0 && out.MinExposure > out.MaxExposure {
		out.MinExposure, out.MaxExposure = out.MaxExposure, out.MinExposure
	}
	return &out
}
