package scene

import "math"

// SkyNight adds a night layer to a physical Sky: a seeded star field and a
// moon disc with phase, both drawn by the sky shader. Nil is off, and a scene
// without it keeps a byte-identical wire.
//
// The ocean takes the moon as its key light once the sun is below the horizon:
// a glint path and crest scatter scaled by the moon's illumination and dimmed
// by Clouds.Coverage. The same cloud cover dims the stars. The layer fades in
// as the sun sinks through twilight (full strength near 18 degrees below the
// horizon), so one authored scene can run through dusk and dawn.
//
// The zero SkyNight is a new moon over a default star field: no moon light and
// a medium star density. Zero means the documented default everywhere except
// MoonPhase, where zero is the new moon.
type SkyNight struct {
	// StarDensity is the share of star cells that hold a star, 0..1.
	// Zero means 0.5. A negative value hides the stars.
	StarDensity float64 `json:"starDensity,omitempty"`
	// StarBrightness scales star radiance, 0..4. Zero means 1.
	StarBrightness float64 `json:"starBrightness,omitempty"`
	// StarSeed picks the star pattern. The same seed draws the same sky on
	// every backend and every frame.
	StarSeed uint32 `json:"starSeed,omitempty"`
	// MoonDirection points from the scene toward the moon. A zero vector means
	// 40 degrees above the horizon, opposite the sun's compass bearing.
	MoonDirection Vector3 `json:"moonDirection,omitzero"`
	// MoonPhase is the position in the lunar cycle, 0..1 (values wrap):
	// 0 new moon, 0.25 first quarter (right half lit), 0.5 full moon,
	// 0.75 last quarter (left half lit). Illumination follows
	// (1 - cos(2*pi*phase)) / 2.
	MoonPhase float64 `json:"moonPhase,omitempty"`
	// MoonRadius is the disc's angular radius in degrees, 0..5. Zero means
	// 0.9, larger than the real 0.26 so the phase reads at game camera
	// distances. A negative value hides the disc and the moon's light.
	MoonRadius float64 `json:"moonRadius,omitempty"`
	// MoonIntensity scales the moon's disc and the light it casts on the
	// sea, 0..4. Zero means 1.
	MoonIntensity float64 `json:"moonIntensity,omitempty"`
}

// MoonIllumination returns the lit fraction of the moon's disc, 0..1.
func (n SkyNight) MoonIllumination() float64 {
	return (1 - math.Cos(2*math.Pi*wrapUnit(n.MoonPhase))) / 2
}

func wrapUnit(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v - math.Floor(v)
}

func finiteOr(v, fallback float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	return v
}

func normalizeSkyNight(n *SkyNight, sun Vector3) *SkyNight {
	if n == nil {
		return nil
	}
	out := SkyNight{
		StarDensity:    math.Min(finiteOr(n.StarDensity, 0), 1),
		StarBrightness: clampSkyParam(finiteOr(n.StarBrightness, 0), 0, 4),
		StarSeed:       n.StarSeed,
		MoonPhase:      wrapUnit(n.MoonPhase),
		MoonRadius:     math.Min(finiteOr(n.MoonRadius, 0), 5),
		MoonIntensity:  clampSkyParam(finiteOr(n.MoonIntensity, 0), 0, 4),
	}
	d := n.MoonDirection
	length := math.Sqrt(d.X*d.X + d.Y*d.Y + d.Z*d.Z)
	if length < 1e-9 || math.IsNaN(length) || math.IsInf(length, 0) {
		az := 180.0
		if math.Hypot(sun.X, sun.Z) > 1e-6 {
			az = math.Atan2(-sun.X, sun.Z) * 180 / math.Pi // the sun's bearing plus 180
		}
		out.MoonDirection = SunDirectionFromAngles(40, az)
	} else {
		out.MoonDirection = Vector3{X: d.X / length, Y: d.Y / length, Z: d.Z / length}
	}
	return &out
}
