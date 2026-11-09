package scene

import (
	"math"

	"m31labs.dev/gosx/det"
)

// Wind is the scene's one wind field. Set it on Environment.Wind. Batches
// that opt in (ScatterSpecies.Wind, which lowers to InstancedMeshIR.Wind)
// sway under it in the vertex stage on WebGPU and WebGL2, and Sample answers
// the same field on the CPU for gameplay: sails, flags, particles, smoke.
//
// The field blows along Direction at Speed. Gust fronts, Gust strong and
// GustScale meters apart, roll downwind at GustSpeed, so neighbouring plants
// bend together and a gust visibly crosses a meadow.
//
// Other materials can read the same values. On WebGPU the renderer writes two
// vec4 at byte 160 of the frame uniform buffer (group 0, binding 0):
// wind = (dirX, dirZ, speed, time) and windGust = (gust, gustScale,
// gustSpeed, frequency); a custom WGSL shader appends `wind: vec4f, windGust:
// vec4f` to its FrameUniforms struct to read them. On WebGL2 any program
// that declares `uniform vec4 u_wind[3]` receives those two vec4 followed by
// (sway, height, 0, 0) for the batch being drawn.
type Wind struct {
	// Direction is the direction the wind blows toward, in degrees: 0 toward
	// +Z, positive values turn toward +X (the Ocean.WindDirection convention).
	Direction float64 `json:"direction,omitempty"`
	// Speed is in meters per second, 0..40. Zero means 4. Sway reaches its
	// full bend at 10 m/s.
	Speed float64 `json:"speed,omitempty"`
	// Gust is the gust strength, 0..1: speed swings by ±Gust around Speed.
	// Zero means 0.35.
	Gust float64 `json:"gust,omitempty"`
	// GustScale is the distance between gust fronts in meters. Zero means 24.
	GustScale float64 `json:"gustScale,omitempty"`
	// GustSpeed is how fast gust fronts travel, in m/s. Zero means Speed.
	GustSpeed float64 `json:"gustSpeed,omitempty"`
	// Frequency is the flutter rate in Hz, 0.1..8. Zero means 1.3.
	Frequency float64 `json:"frequency,omitempty"`
}

// WindIR is the wire form of Wind under environment.wind with every default
// resolved and the direction as a unit XZ vector.
type WindIR struct {
	DirectionX float64 `json:"directionX"`
	DirectionZ float64 `json:"directionZ"`
	Speed      float64 `json:"speed"`
	Gust       float64 `json:"gust"`
	GustScale  float64 `json:"gustScale"`
	GustSpeed  float64 `json:"gustSpeed"`
	Frequency  float64 `json:"frequency"`
}

// SceneIR resolves the wind's defaults. A nil wind lowers to nil.
func (w *Wind) SceneIR() *WindIR {
	if w == nil {
		return nil
	}
	finite := func(v, fallback float64) float64 {
		if math.IsNaN(v) || math.IsInf(v, 0) || v == 0 {
			return fallback
		}
		return v
	}
	angle := normalizeOceanWindDirection(finite(w.Direction, 0)) * math.Pi / 180
	speed := clampFloat(finite(w.Speed, 4), 0, 40)
	gust := clampFloat(finite(w.Gust, 0.35), 0, 1)
	return &WindIR{
		DirectionX: det.Sin(angle),
		DirectionZ: det.Cos(angle),
		Speed:      speed,
		Gust:       gust,
		GustScale:  clampFloat(finite(w.GustScale, 24), 1, 10000),
		GustSpeed:  clampFloat(finite(w.GustSpeed, speed), 0, 200),
		Frequency:  clampFloat(finite(w.Frequency, 1.3), 0.1, 8),
	}
}

// Sample returns the wind velocity, in m/s, at world (x, z) and scene time t
// seconds: the direction scaled by Speed and the passing gust fronts. It is
// the same field the browser sways vegetation with.
func (w *Wind) Sample(x, z, t float64) Vector3 {
	ir := w.SceneIR()
	if ir == nil {
		return Vector3{}
	}
	speed := float64(ir.Speed * ir.gustFactor(x, z, t))
	return Vector3{X: float64(ir.DirectionX * speed), Z: float64(ir.DirectionZ * speed)}
}

// gustFactor is 1 ± Gust: a travelling sine front along the wind with a slow
// cross-wind wobble so fronts are not ruler-straight. The browser evaluates
// the same expression (sceneWindGust in terrain.ts and the shader twins).
func (ir *WindIR) gustFactor(x, z, t float64) float64 {
	along := float64(x*ir.DirectionX) + float64(z*ir.DirectionZ)
	across := float64(x*ir.DirectionZ) - float64(z*ir.DirectionX)
	wobble := float64(0.35 * det.Sin(float64(across*(2*math.Pi))/float64(ir.GustScale*1.7)))
	phase := float64((along-float64(ir.GustSpeed*t))*(2*math.Pi))/ir.GustScale + wobble
	return 1 + float64(ir.Gust*det.Sin(phase))
}
