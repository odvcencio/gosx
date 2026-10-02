package scene

import (
	"math"
	"strings"
)

// Ocean describes an open-ocean surface in the Scene3D environment. A nil
// Ocean keeps existing scenes unchanged; see Environment.Ocean and the wire IR.
type Ocean struct {
	// Level is the world Y of the mean surface. Default: 0.
	Level float64 `json:"level,omitempty"`
	// WindDirection is the direction waves travel, in degrees. 0 travels toward
	// +Z; positive values turn toward +X. Values are normalized into [0, 360).
	WindDirection float64 `json:"windDirection,omitempty"`
	// WaveHeight is in meters, 0.05-6. Default: 0.8.
	WaveHeight float64 `json:"waveHeight,omitempty"`
	// WaveLength is in meters, 2-120. Default: 18.
	WaveLength float64 `json:"waveLength,omitempty"`
	// Choppiness is 0-1. Default: 0.6.
	Choppiness float64 `json:"choppiness,omitempty"`
	// Speed is 0-4. Default: 1.
	Speed float64 `json:"speed,omitempty"`
	// DeepColor is an sRGB hex color. Default: "#03141f".
	DeepColor string `json:"deepColor,omitempty"`
	// ShallowColor is an sRGB hex color. Default: "#1f6f78".
	ShallowColor string `json:"shallowColor,omitempty"`
	// ScatterColor is an sRGB hex color. Default: "#2fa58f".
	ScatterColor string `json:"scatterColor,omitempty"`
	// FoamColor is an sRGB hex color. Default: "#e9eef0".
	FoamColor string `json:"foamColor,omitempty"`
	// Clarity is in meters, 0.5-40. Default: 4.
	Clarity float64 `json:"clarity,omitempty"`
	// Roughness is 0.01-0.5. Default: 0.06.
	Roughness float64 `json:"roughness,omitempty"`
	// Foam is 0-1. Default: 0.6.
	Foam float64 `json:"foam,omitempty"`
	// Surf is the shore run-up amount, 0-1. Default: 0.5.
	Surf float64 `json:"surf,omitempty"`
	// Extent is in meters, 100-20000. Default: 4000.
	Extent      float64           `json:"extent,omitempty"`
	Bathymetry  *OceanBathymetry  `json:"bathymetry,omitempty"`
	Reflections *OceanReflections `json:"reflections,omitempty"`
}

// OceanBathymetry maps a grayscale image's R channel into world-space seabed
// heights over the XZ rectangle. MinHeight is the world Y for 0 and MaxHeight
// is the world Y for 1.
type OceanBathymetry struct {
	Src       string  `json:"src"`
	MinX      float64 `json:"minX"`
	MinZ      float64 `json:"minZ"`
	MaxX      float64 `json:"maxX"`
	MaxZ      float64 `json:"maxZ"`
	MinHeight float64 `json:"minHeight"`
	MaxHeight float64 `json:"maxHeight"`
	// Encoding is "linear" (the default: R maps MinHeight..MaxHeight) or
	// "signed-sqrt": s = 2R - 1 and height = sign(s) * s^2 * max(|MinHeight|,
	// |MaxHeight|). Signed-sqrt spends the 8-bit steps near y = 0, so a gentle
	// beach gets millimetre steps at the waterline instead of 5 cm terraces.
	Encoding string `json:"encoding,omitempty"`
}

// normalizeOcean trims colors and bathymetry URLs, preserves zero as the
// default marker, clamps authored nonzero parameters, and returns nil for a
// missing Ocean so existing scene wire bytes remain unchanged.
func normalizeOcean(ocean *Ocean) *Ocean {
	if ocean == nil {
		return nil
	}
	out := *ocean
	out.WindDirection = normalizeOceanWindDirection(ocean.WindDirection)
	out.WaveHeight = clampOceanParam(ocean.WaveHeight, 0.05, 6)
	out.WaveLength = clampOceanParam(ocean.WaveLength, 2, 120)
	out.Choppiness = clampOceanParam(ocean.Choppiness, 0, 1)
	out.Speed = clampOceanParam(ocean.Speed, 0, 4)
	out.DeepColor = strings.TrimSpace(ocean.DeepColor)
	out.ShallowColor = strings.TrimSpace(ocean.ShallowColor)
	out.ScatterColor = strings.TrimSpace(ocean.ScatterColor)
	out.FoamColor = strings.TrimSpace(ocean.FoamColor)
	out.Clarity = clampOceanParam(ocean.Clarity, 0.5, 40)
	out.Roughness = clampOceanParam(ocean.Roughness, 0.01, 0.5)
	out.Foam = clampOceanParam(ocean.Foam, 0, 1)
	out.Surf = clampOceanParam(ocean.Surf, 0, 1)
	out.Extent = clampOceanParam(ocean.Extent, 100, 20000)
	out.Bathymetry = normalizeOceanBathymetry(ocean.Bathymetry)
	out.Reflections = normalizeOceanReflections(ocean.Reflections)
	return &out
}

func clampOceanParam(value, minimum, maximum float64) float64 {
	if value == 0 {
		return 0
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func normalizeOceanWindDirection(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	value = math.Mod(value, 360)
	if value == 0 {
		return 0
	}
	if value < 0 {
		value += 360
	}
	return value
}

func normalizeOceanBathymetry(bathymetry *OceanBathymetry) *OceanBathymetry {
	if bathymetry == nil {
		return nil
	}
	out := *bathymetry
	out.Src = strings.TrimSpace(out.Src)
	out.Encoding = strings.ToLower(strings.TrimSpace(out.Encoding))
	if out.Encoding != "signed-sqrt" {
		out.Encoding = ""
	}
	if out.Src == "" || out.MaxX <= out.MinX || out.MaxZ <= out.MinZ || out.MaxHeight <= out.MinHeight {
		return nil
	}
	return &out
}
