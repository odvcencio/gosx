package scene

import "strings"

// OceanReflections opts into geometry reflections. Nil or an empty Mode is off.
type OceanReflections struct {
	// Mode is "ssr", "planar", or "ssr+planar".
	Mode string `json:"mode,omitempty"`
	// Resolution scales reflection targets, 0.125..1. Default 0.5.
	Resolution float64 `json:"resolution,omitempty"`
	// Strength scales reflected geometry, 0..1. Zero means default 1.
	Strength float64 `json:"strength,omitempty"`
}

func normalizeOceanReflections(r *OceanReflections) *OceanReflections {
	if r == nil {
		return nil
	}
	out := *r
	out.Mode = strings.ToLower(strings.TrimSpace(r.Mode))
	switch out.Mode {
	case "ssr", "planar", "ssr+planar":
	default:
		return nil
	}
	out.Resolution = clampAtmosphereParam(r.Resolution, 0.125, 1)
	out.Strength = clampAtmosphereParam(r.Strength, 0, 1)
	return &out
}
