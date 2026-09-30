package scene

// SkyClouds adds a drifting procedural daylight cloud deck. Nil is off.
type SkyClouds struct {
	// Coverage is 0..1; zero makes the deck clear.
	Coverage float64 `json:"coverage"`
	// Altitude is metres above sea level, 100..12000. Default 1500.
	Altitude float64 `json:"altitude,omitempty"`
	// Scale is the world-space noise wavelength in metres, 100..20000. Default 3000.
	Scale float64 `json:"scale,omitempty"`
	// Speed is drift in metres/second, 0..100. Zero uses the default 8.
	Speed float64 `json:"speed,omitempty"`
	// Direction is degrees, 0 toward +Z, 90 toward +X.
	Direction float64 `json:"direction,omitempty"`
	// Opacity is 0..1. Zero uses the default 0.85.
	Opacity float64 `json:"opacity,omitempty"`
}

func normalizeSkyClouds(c *SkyClouds) *SkyClouds {
	if c == nil {
		return nil
	}
	out := *c
	out.Coverage = clampSkyParam(c.Coverage, 0, 1)
	out.Altitude = clampSkyParam(c.Altitude, 100, 12000)
	out.Scale = clampSkyParam(c.Scale, 100, 20000)
	out.Speed = clampSkyParam(c.Speed, 0, 100)
	out.Direction = normalizeOceanWindDirection(c.Direction)
	out.Opacity = clampSkyParam(c.Opacity, 0, 1)
	return &out
}
