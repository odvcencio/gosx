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
	// Spectrum switches the surface from the six-wave Gerstner sum to the
	// spectral ocean. When it is set, WaveHeight, WaveLength and Speed are
	// ignored (the spectrum defines height and length; time runs at 1x) and
	// WindDirection sets the wind direction. The other fields keep their meaning.
	Spectrum *OceanSpectrum `json:"spectrum,omitempty"`
	// Masks lists node IDs whose meshes remove water: a hull, a dry dock, a cave.
	Masks []string `json:"masks,omitempty"`
	// Presentation sets the water's clock, drift and sea-state transition. It
	// is declarative: an app changes it through an ordinary scene update.
	Presentation *OceanPresentation `json:"presentation,omitempty"`
	// Wake adds a spectral ship wake behind the vessel the scene node NodeID
	// names. It needs Spectrum; without one the wake is not drawn. See OceanWake.
	Wake *OceanWake `json:"wake,omitempty"`
	// SwellWaves is computed during normalization from Spectrum and
	// WindDirection and is read-only: whatever an author sets is replaced. It
	// holds the DefaultOceanSwellCount strongest bins of the first two cascades
	// as plane waves, for ship motion and the browser's first frames.
	SwellWaves []OceanWave `json:"swellWaves,omitempty"`
	// GraphParams and GraphTween are computed during normalization when Spectrum
	// is set and are read-only: whatever an author sets is replaced. They are the
	// value parameters of the Elio ocean pass graph and the sea-state tween
	// seconds; see ocean_graph.go for each name and its source.
	GraphParams map[string]float64 `json:"graphParams,omitempty"`
	GraphTween  float64            `json:"graphTween,omitempty"`
	// GraphNodes is computed with GraphParams and read-only: it maps the scene
	// nodes the Elio graph reads (its logical names, such as OceanGraphHullNode)
	// to scene node IDs, so the runner can resolve the graph's node sources.
	GraphNodes map[string]string `json:"graphNodes,omitempty"`
}

// OceanWake switches on the ship wake: a pressure-driven wave field in a
// window that follows the hull, with Kelvin-angle arms and foam. The Elio
// ocean graph computes it (wake passes gated by its wakeEnabled parameter) and
// the surface adds its height, slopes and turbulence to the sea.
//
// The hull is the scene node NodeID, normally a vessel; the runtime reads that
// node's world position, heading and speed every frame, so the author never
// feeds the wake a speed. Length and Beam are the hull's waterline size in
// metres and shape the pressure source: zero means the graph's defaults (200
// and 30), nonzero values are clamped to 5-500 and 1-80, and non-finite or
// negative values count as zero. A wake with no NodeID cannot be enabled.
type OceanWake struct {
	// NodeID is the scene node that carries the wake. Required.
	NodeID string `json:"nodeId,omitempty"`
	// Enabled draws the wake. The node stays bound when it is false, so a later
	// scene update can switch the wake on without re-binding.
	Enabled bool `json:"enabled,omitempty"`
	// Length and Beam are the hull's waterline size in metres.
	Length float64 `json:"length,omitempty"`
	Beam   float64 `json:"beam,omitempty"`
	// Debug renders the wake's height field alone, as a colour ramp (blue
	// below the rest surface, red above, grey outside the wake window) on a flat
	// sea. It is a diagnostic for looking at the simulation from above and
	// changes no graph parameter.
	Debug bool `json:"debug,omitempty"`
}

// OceanPresentation is how the sea is presented over time, apart from what it
// looks like. It exists so a host that fast-forwards a voyage never speeds up
// the waves: wave animation keeps natural time and only the water's apparent
// motion past the hull follows the host (R9), and a change of sea state
// arrives gradually (R10). All fields are presentation only; nothing here feeds
// back into a simulation.
//
// Each field is the value of a named parameter of the Elio ocean pass graph
// (`elio emit ts --set ocean --backend webgpu`, kernel set "ocean"), which the
// GoSX graph runner fills from the normalized scene:
//
//	TimeScale         graph param timeScale (f32, source value): wave evolution
//	                  time = presentation time * timeScale
//	WaterDriftX       graph param driftX (f32, source value, m/s)
//	WaterDriftZ       graph param driftZ (f32, source value, m/s): the runtime
//	                  integrates the water-frame offset from these every frame;
//	                  the cascades, the persistent foam and the wakes are
//	                  sampled at xz - offset
//	TransitionSeconds the tween (seconds) of the graph params the spectrum
//	                  drives: windSpeed, fetch, gamma, spread, swell,
//	                  windDirDeg, swellDirDeg and lambda
//
// Zero is the default marker for TimeScale and TransitionSeconds, as elsewhere
// in Ocean; a zero drift is no drift. TimeScale cannot be zero on the wire, so
// to freeze the sea an app pauses the scene clock instead.
type OceanPresentation struct {
	// TimeScale multiplies presentation time for wave evolution. 1 is natural
	// motion. 0-4, default 1. A voyage at 1,000x game speed keeps TimeScale at 1
	// so waves stay natural.
	TimeScale float64 `json:"timeScale,omitempty"`
	// WaterDriftX and WaterDriftZ are the apparent velocity of the water past
	// the world origin, in m/s, -100 to 100 each. The runtime integrates the
	// water-frame offset every frame, so the app updates them only when the
	// ship's speed or heading changes. A fast-forwarding host sets a believable
	// apparent speed. Non-finite values become 0.
	WaterDriftX float64 `json:"waterDriftX,omitempty"`
	WaterDriftZ float64 `json:"waterDriftZ,omitempty"`
	// TransitionSeconds: when Spectrum changes, the runtime interpolates the
	// spectrum parameters over this much presentation time. 0.1-600, default 20.
	TransitionSeconds float64 `json:"transitionSeconds,omitempty"`
}

// OceanSpectrum describes a wind sea by its JONSWAP spectrum. Zero fields mean
// the documented default, as elsewhere in Ocean. Fetch is in kilometres, as
// oceanographic tables give it; the other inputs are SI.
type OceanSpectrum struct {
	// WindSpeed is m/s at 10 m, 0.5-40. Default: 9.
	WindSpeed float64 `json:"windSpeed,omitempty"`
	// Fetch is km, 1-2000. Default: 300.
	Fetch float64 `json:"fetch,omitempty"`
	// PeakEnhancement is the JONSWAP gamma, 1-7. Default: 3.3.
	PeakEnhancement float64 `json:"peakEnhancement,omitempty"`
	// Spread is the directional sharpness, 1-32. Default: 8.
	Spread float64 `json:"spread,omitempty"`
	// Swell is the 0-1 share of a long swell. Default: 0.
	Swell float64 `json:"swell,omitempty"`
	// SwellDirection is the direction the swell travels, in degrees, as
	// WindDirection. Values are normalized into [0, 360).
	SwellDirection float64 `json:"swellDirection,omitempty"`
	// Cascades are the tiles. Empty means the four defaults: tile sizes 1600, 250,
	// 37 and 5.3 m with bands [2pi/1600, 0.12), [0.12, 0.80), [0.80, 5.4) and
	// [5.4, 37) rad/m. At most four.
	Cascades []OceanCascade `json:"cascades,omitempty"`
	// Seed picks the realization. Any value, including 0, is a seed.
	Seed uint32 `json:"seed"`
}

// OceanCascade is one tile of the spectral ocean. Its band is [KMin, KMax) in
// rad/m, and KMax stays below pi*64/Size, the Nyquist wavenumber of a 64-point
// cascade, so the Nyquist row and column are always empty.
type OceanCascade struct {
	Size float64 `json:"size"` // tile size, metres
	KMin float64 `json:"kMin"` // band start, rad/m
	KMax float64 `json:"kMax"` // band end, rad/m; below pi*64/Size
}

// OceanPreset returns a sea state by name: "calm", "moderate", "rough" or
// "storm" (case-insensitive; any other name gives "moderate"). The presets
// follow the Beaufort scale in wind speed over a fetch that keeps the
// significant wave height Hs = 4 sqrt(m0) in the usual range. The Hs below is
// measured on the realized four-cascade spectrum with seed 1 and the default
// cascades (TestOceanSwellTruncationError):
//
//	calm      Beaufort 2-3,  3 m/s over  30 km:             Hs 0.4 m, a short choppy sea
//	moderate  Beaufort 4,    7 m/s over 100 km:             Hs 1.7 m
//	rough     Beaufort 6,   11 m/s over 200 km:             Hs 3.7 m
//	storm     Beaufort 9,   20 m/s over 250 km, gamma 2:    Hs 7.4 m, a less peaked sea
//
// Swell shares grow with the sea state and travel toward +Z (SwellDirection 0);
// set SwellDirection to turn them. The seed is 1; change it for another sea of
// the same state.
func OceanPreset(name string) OceanSpectrum {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "calm":
		return OceanSpectrum{WindSpeed: 3, Fetch: 30, PeakEnhancement: 3.3, Spread: 16, Swell: 0.1, Seed: 1}
	case "rough":
		return OceanSpectrum{WindSpeed: 11, Fetch: 200, PeakEnhancement: 3.3, Spread: 6, Swell: 0.25, Seed: 1}
	case "storm":
		return OceanSpectrum{WindSpeed: 20, Fetch: 250, PeakEnhancement: 2, Spread: 4, Swell: 0.35, Seed: 1}
	}
	return OceanSpectrum{WindSpeed: 7, Fetch: 100, PeakEnhancement: 3.3, Spread: 8, Swell: 0.15, Seed: 1}
}

// OceanBathymetry maps a grayscale image's R channel into world-space seabed
// heights over the XZ rectangle. MinHeight is the world Y for 0 and MaxHeight
// is the world Y for 1. Outside the rectangle, the ocean is deep water.
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
	out.Spectrum = normalizeOceanSpectrum(ocean.Spectrum)
	out.Masks = normalizeOceanMasks(ocean.Masks)
	out.Presentation = normalizeOceanPresentation(ocean.Presentation)
	out.Wake = normalizeOceanWake(ocean.Wake)
	out.SwellWaves, out.GraphParams, out.GraphTween, out.GraphNodes = nil, nil, 0, nil
	if out.Spectrum != nil {
		out.SwellWaves = OceanSwellWaves(*out.Spectrum, out.WindDirection, DefaultOceanSwellCount)
		out.GraphParams = oceanGraphParams(&out)
		out.GraphTween = oceanGraphTween(&out)
		out.GraphNodes = oceanGraphNodes(&out)
	}
	return &out
}

// normalizeOceanWake trims the node ID and keeps the sizes in range; zero stays the default marker.
func normalizeOceanWake(wake *OceanWake) *OceanWake {
	if wake == nil {
		return nil
	}
	out := *wake
	out.NodeID = strings.TrimSpace(wake.NodeID)
	out.Length = oceanWakeSize(wake.Length, 5, 500)
	out.Beam = oceanWakeSize(wake.Beam, 1, 80)
	return &out
}

func oceanWakeSize(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	return math.Min(hi, math.Max(lo, v))
}

// normalizeOceanSpectrum clamps authored nonzero parameters and keeps zero as
// the default marker. A cascade list that is empty, longer than four or invalid
// becomes nil, the four defaults.
func normalizeOceanSpectrum(spectrum *OceanSpectrum) *OceanSpectrum {
	if spectrum == nil {
		return nil
	}
	out := *spectrum
	out.WindSpeed = clampOceanParam(spectrum.WindSpeed, 0.5, 40)
	out.Fetch = clampOceanParam(spectrum.Fetch, 1, 2000)
	out.PeakEnhancement = clampOceanParam(spectrum.PeakEnhancement, 1, 7)
	out.Spread = clampOceanParam(spectrum.Spread, 1, 32)
	out.Swell = clampOceanParam(spectrum.Swell, 0, 1)
	out.SwellDirection = normalizeOceanWindDirection(spectrum.SwellDirection)
	out.Cascades = normalizeOceanCascades(spectrum.Cascades)
	return &out
}

func normalizeOceanCascades(cascades []OceanCascade) []OceanCascade {
	if len(cascades) == 0 || len(cascades) > 4 {
		return nil
	}
	out := make([]OceanCascade, len(cascades))
	for i, c := range cascades {
		for _, v := range []float64{c.Size, c.KMin, c.KMax} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil
			}
		}
		if c.Size <= 0 || c.KMin < 0 || c.KMax <= c.KMin {
			return nil
		}
		c.Size = clampOceanParam(c.Size, 1, 20000)
		if limit := oceanNyquist64(c.Size); c.KMax > limit {
			c.KMax = limit
		}
		if c.KMin >= c.KMax {
			return nil
		}
		out[i] = c
	}
	return out
}

// oceanNyquist64 is the Nyquist wavenumber of a 64-point cascade of the given
// tile size, the upper limit of any band: pi*64/size, rounded down by one part
// in 1e9 so the band stays strictly below it.
func oceanNyquist64(size float64) float64 {
	return float64(math.Pi * 64 / size * (1 - 1e-9))
}

// normalizeOceanPresentation clamps authored nonzero parameters and keeps zero
// as the default marker. A nil presentation stays nil so existing wire bytes
// are unchanged.
func normalizeOceanPresentation(presentation *OceanPresentation) *OceanPresentation {
	if presentation == nil {
		return nil
	}
	out := *presentation
	out.TimeScale = clampOceanParam(finiteOceanParam(presentation.TimeScale), 0, 4)
	out.WaterDriftX = clampOceanDrift(presentation.WaterDriftX)
	out.WaterDriftZ = clampOceanDrift(presentation.WaterDriftZ)
	out.TransitionSeconds = clampOceanParam(finiteOceanParam(presentation.TransitionSeconds), 0.1, 600)
	return &out
}

func finiteOceanParam(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func clampOceanDrift(value float64) float64 {
	return math.Max(-100, math.Min(100, finiteOceanParam(value)))
}

func normalizeOceanMasks(masks []string) []string {
	var out []string
	for _, mask := range masks {
		mask = strings.TrimSpace(mask)
		if mask == "" {
			continue
		}
		duplicate := false
		for _, seen := range out {
			duplicate = duplicate || seen == mask
		}
		if !duplicate {
			out = append(out, mask)
		}
	}
	return out
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
