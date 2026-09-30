package scene

// DetailLayer adds world-space microstructure to a PBR surface. Zero numeric
// values are omitted so the browser applies defaults (2 m, 1, 0.6, 0.5).
type DetailLayer struct {
	Albedo       string  `json:"albedo,omitempty"`
	Normal       string  `json:"normal,omitempty"`
	Roughness    string  `json:"roughness,omitempty"`
	Scale        float64 `json:"scale,omitempty"`
	NormalScale  float64 `json:"normalScale,omitempty"`
	AlbedoMix    float64 `json:"albedoMix,omitempty"`
	RoughnessMix float64 `json:"roughnessMix,omitempty"`
}

// Detail layers augment the existing material, including imported glTF maps.
// Ground projects on world XZ. Steep defaults to triplanar projection; nil
// Steep uses Ground everywhere. Defaults are resolved by the browser: slopes
// 30/45 degrees, fade 8/14 m, and stochastic tiling enabled.
type Detail struct {
	Ground     *DetailLayer `json:"ground,omitempty"`
	Steep      *DetailLayer `json:"steep,omitempty"`
	SlopeStart float64      `json:"slopeStart,omitempty"`
	SlopeEnd   float64      `json:"slopeEnd,omitempty"`
	Triplanar  *bool        `json:"triplanar,omitempty"`
	FadeStart  float64      `json:"fadeStart,omitempty"`
	FadeEnd    float64      `json:"fadeEnd,omitempty"`
	Stochastic *bool        `json:"stochastic,omitempty"`
}

func cloneDetail(detail *Detail) *Detail {
	if detail == nil {
		return nil
	}
	out := *detail
	if detail.Ground != nil {
		layer := *detail.Ground
		out.Ground = &layer
	}
	if detail.Steep != nil {
		layer := *detail.Steep
		out.Steep = &layer
	}
	if detail.Triplanar != nil {
		out.Triplanar = Bool(*detail.Triplanar)
	}
	if detail.Stochastic != nil {
		out.Stochastic = Bool(*detail.Stochastic)
	}
	return &out
}
