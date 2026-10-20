package scene

import "math"

// Haze enables height-dependent aerial perspective in linear light. Nil is off.
type Haze struct {
	// Density is extinction per metre, 0..0.1. Zero uses the browser default 0.0015.
	Density float64 `json:"density,omitempty"`
	// HeightFalloff is inverse metres, 0..1. Zero uses the default 0.04.
	HeightFalloff float64 `json:"heightFalloff,omitempty"`
	// SunScatter is the forward-scattering contribution, 0..1. Zero uses default 0.25.
	SunScatter float64 `json:"sunScatter,omitempty"`
}

func normalizeHaze(h *Haze) *Haze {
	if h == nil {
		return nil
	}
	out := *h
	out.Density = clampAtmosphereParam(h.Density, 0, 0.1)
	out.HeightFalloff = clampAtmosphereParam(h.HeightFalloff, 0, 1)
	out.SunScatter = clampAtmosphereParam(h.SunScatter, 0, 1)
	return &out
}

// Invalid authored atmosphere values retain the browser-default marker.
func clampAtmosphereParam(value, minimum, maximum float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return clampSkyParam(value, minimum, maximum)
}
