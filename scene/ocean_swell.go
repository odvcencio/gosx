package scene

import "m31labs.dev/gosx/oceanspec"

// DefaultOceanSwellCount is the number of swell waves OceanSwellWaves returns
// when k is not positive, and the number the normalized SceneIR carries.
const DefaultOceanSwellCount = 24

// maxOceanSwellCount bounds k so the wave table stays a small fixed upload.
const maxOceanSwellCount = 64

// OceanWave is one plane wave of the swell: the surface rises by
// Amplitude*cos(K*(DirX*x + DirZ*z) - Omega*t + Phase) metres at the water
// frame's (x, z) after t seconds. Direction is the unit vector the wave
// travels along, K is in rad/m, Omega is the loop-quantized angular frequency
// in rad/s and Phase is in radians.
type OceanWave struct {
	DirX      float64 `json:"dx"`
	DirZ      float64 `json:"dz"`
	K         float64 `json:"k"`
	Omega     float64 `json:"omega"`
	Amplitude float64 `json:"amplitude"`
	Phase     float64 `json:"phase"`
}

var defaultOceanCascades = [4]OceanCascade{
	{Size: 1600, KMin: 6.283185307179586 / 1600, KMax: 0.12},
	{Size: 250, KMin: 0.12, KMax: 0.80},
	{Size: 37, KMin: 0.80, KMax: 5.4},
	{Size: 5.3, KMin: 5.4, KMax: 37},
}

// oceanSpectrumParams applies the spectrum defaults and converts it to the
// kernel's units. windDirection is in degrees, as Ocean.WindDirection.
func oceanSpectrumParams(s OceanSpectrum, windDirection float64) oceanspec.Params {
	p := oceanspec.Params{
		WindSpeed: s.WindSpeed, FetchKm: s.Fetch, Gamma: s.PeakEnhancement, Spread: s.Spread,
		Swell: s.Swell, WindDir: oceanspec.Direction(windDirection),
		SwellDir: oceanspec.Direction(s.SwellDirection), Seed: s.Seed,
	}
	if p.WindSpeed == 0 {
		p.WindSpeed = 9
	}
	if p.FetchKm == 0 {
		p.FetchKm = 300
	}
	if p.Gamma == 0 {
		p.Gamma = 3.3
	}
	if p.Spread == 0 {
		p.Spread = 8
	}
	return p
}

// oceanCascades returns the cascades of a spectrum, the four defaults when it
// authors none.
func oceanCascades(s OceanSpectrum) []oceanspec.Cascade {
	src := s.Cascades
	if len(src) == 0 {
		src = defaultOceanCascades[:]
	}
	out := make([]oceanspec.Cascade, len(src))
	for i, c := range src {
		out[i] = oceanspec.Cascade{Size: c.Size, KMin: c.KMin, KMax: c.KMax}
	}
	return out
}

func oceanWaves(waves []oceanspec.Wave) []OceanWave {
	out := make([]OceanWave, len(waves))
	for i, w := range waves {
		out[i] = OceanWave{DirX: w.DirX, DirZ: w.DirZ, K: w.K, Omega: w.Omega, Amplitude: w.Amplitude, Phase: w.Phase}
	}
	return out
}

// OceanSwellWaves returns the k strongest bins of the spectrum's first two
// cascades (the 1600 m and 250 m tiles by default) as plane waves, strongest
// first; k <= 0 means DefaultOceanSwellCount. Ties break by smaller |k|, then
// by signed wavenumber index. windDirection is in degrees, as
// Ocean.WindDirection.
//
// The arithmetic is the CPU port of Elio's ocean_spectrum kernel in package
// oceanspec, which is deterministic on amd64, js/wasm and arm64.
// testdata/ocean-swell-bins.json, written by Elio's interpreter, holds the two
// within 1e-9. The waves are what scene normalization ships in the SceneIR,
// what OceanQuery and vessel.Sea evaluate, and what the browser uses before
// the GPU surface exists.
func OceanSwellWaves(s OceanSpectrum, windDirection float64, k int) []OceanWave {
	if k <= 0 {
		k = DefaultOceanSwellCount
	}
	if k > maxOceanSwellCount {
		k = maxOceanSwellCount
	}
	cascades := oceanCascades(s)
	if len(cascades) > 2 {
		cascades = cascades[:2]
	}
	return oceanWaves(oceanspec.Strongest(oceanSpectrumParams(s, windDirection), cascades, k))
}
