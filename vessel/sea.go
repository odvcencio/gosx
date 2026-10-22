package vessel

import "m31labs.dev/gosx/det"

// WaveField reports the water surface height at a world position and time.
// Height is in metres, x and z are world metres, t is simulation seconds.
// A WaveField used in a deterministic simulation must itself be deterministic.
type WaveField interface {
	Height(x, z, t float64) float64
}

// WaveFunc adapts a function to a [WaveField]. It lets a game wrap another
// ocean query, for example the vertical component of scene.OceanQuery, though
// that query uses the platform math library and is not bit-identical across
// platforms.
type WaveFunc func(x, z, t float64) float64

// Height implements [WaveField].
func (f WaveFunc) Height(x, z, t float64) float64 { return f(x, z, t) }

// The wave table mirrors scene/ocean_waves.json, which the Scene3D ocean and
// scene.OceanQuery use.
var (
	seaRatio  = [6]float64{1, 0.73, 0.53, 0.39, 0.28, 0.21}
	seaAngle  = [6]float64{0, 0.38, -0.46, 0.83, -0.95, 1.4}
	seaWeight = [6]float64{1, 0.62, 0.42, 0.28, 0.19, 0.13}
)

const (
	seaGravity   = 9.81
	seaPhaseStep = 0.6180339887
	twoPi        = 6.283185307179586
	pi           = 3.141592653589793
)

// SeaConfig mirrors the wave fields of scene.Ocean. A zero field takes the
// same default as the ocean: WaveHeight 0.8, WaveLength 18, Speed 1.
type SeaConfig struct {
	Level         float64 // mean water height
	WaveHeight    float64 // significant wave height scale, metres
	WaveLength    float64 // longest component wavelength, metres
	WindDirection float64 // degrees, as in scene.Ocean
	Speed         float64 // time scale
	Components    int     // 1..6; 0 means 6

	// Swell, when it is not empty, replaces the six-wave table: the sea is the
	// sum of these plane waves (the swell waves of scene.Ocean.Spectrum, from
	// scene.OceanSwellWaves or the normalized Ocean.SwellWaves). WaveHeight,
	// WaveLength, WindDirection, Speed and Components are then ignored, as they
	// are for a spectral Ocean, and time runs at 1x.
	Swell []SwellWave
}

type seaWave struct{ wx, wz, k, omega, amp, phase float64 }

// SwellWave is one plane wave of a spectral sea: the surface rises by
// Amplitude*cos(K*(DirX*x + DirZ*z) - Omega*t + Phase) metres. It has the
// fields of scene.OceanWave, so a game converts with vessel.SwellWave(w).
type SwellWave struct {
	DirX, DirZ, K, Omega, Amplitude, Phase float64
}

// Sea is a deterministic sum of sinusoids built from the Scene3D ocean wave
// table, or from the swell waves of a spectral ocean. It models vertical motion
// only, without the ocean's horizontal Gerstner displacement, so it differs
// from the rendered surface by a small fraction of the wave height.
type Sea struct {
	level float64
	waves [6]seaWave
	count int
	swell []SwellWave
}

// NewSea builds a [Sea] from c.
func NewSea(c SeaConfig) *Sea {
	if len(c.Swell) > 0 {
		return &Sea{level: c.Level, swell: append([]SwellWave(nil), c.Swell...)}
	}
	h, l, sp := c.WaveHeight, c.WaveLength, c.Speed
	if h == 0 {
		h = 0.8
	}
	if l == 0 {
		l = 18
	}
	if sp == 0 {
		sp = 1
	}
	n := c.Components
	if n <= 0 || n > 6 {
		n = 6
	}
	s := &Sea{level: c.Level, count: n}
	var weights float64
	for i := 0; i < n; i++ {
		weights += float64(seaWeight[i] * seaWeight[i])
	}
	wind := float64(c.WindDirection*pi) / 180
	for i := 0; i < n; i++ {
		angle := wind + seaAngle[i]
		k := twoPi / float64(l*seaRatio[i])
		phase := det.Mod(float64(float64(i)*seaPhaseStep), 1)
		s.waves[i] = seaWave{
			wx: det.Sin(angle), wz: det.Cos(angle), k: k,
			omega: float64(det.Sqrt(float64(seaGravity*k)) * sp),
			amp:   float64(seaWeight[i]*h) / det.Sqrt(float64(8*weights)),
			phase: float64(phase * twoPi),
		}
	}
	return s
}

// Height implements [WaveField].
func (s *Sea) Height(x, z, t float64) float64 {
	y := s.level
	for i := range s.swell {
		w := &s.swell[i]
		arg := float64(w.K*(float64(w.DirX*x)+float64(w.DirZ*z))) - float64(w.Omega*t) + w.Phase
		y += float64(w.Amplitude * det.Cos(arg))
	}
	for i := 0; i < s.count; i++ {
		w := &s.waves[i]
		arg := float64(w.k*(float64(w.wx*x)+float64(w.wz*z))) - float64(w.omega*t) + w.phase
		y += float64(w.amp * det.Sin(arg))
	}
	return y
}
