package camera

import "math"

// ShakeConfig tunes camera shake.
type ShakeConfig struct {
	// MaxOffset is the largest shake translation along the camera's right, up
	// and back axes, in scene units, at trauma 1. Zero fields mean no shake on
	// that axis; if all three are zero the default (0.25, 0.25, 0) is used.
	MaxOffset Vec3
	// MaxRoll is the largest shake roll in radians at trauma 1. Zero selects 0.03.
	MaxRoll float64
	// Frequency is how fast the shake wobbles, in Hz. Zero selects 18.
	Frequency float64
	// Decay is how much trauma drains per second. Zero selects 1.2.
	Decay float64
	// Exponent shapes the response: shake = trauma^Exponent. Zero selects 2,
	// which makes small hits subtle and big hits violent.
	Exponent float64
	// Seed picks the noise phase, so two cameras shake differently.
	Seed uint32
}

func (c ShakeConfig) withDefaults() ShakeConfig {
	if c.MaxOffset == (Vec3{}) {
		c.MaxOffset = Vec3{0.25, 0.25, 0}
	}
	if c.MaxRoll == 0 {
		c.MaxRoll = 0.03
	}
	if c.Frequency <= 0 {
		c.Frequency = 18
	}
	if c.Decay <= 0 {
		c.Decay = 1.2
	}
	if c.Exponent <= 0 {
		c.Exponent = 2
	}
	return c
}

// Shake is trauma-based camera shake. A hit adds trauma (0..1); trauma drains
// over time; the visible shake grows with trauma raised to Exponent. The noise
// is a fixed function of time and Seed, so a replay shakes the same way.
type Shake struct {
	cfg    ShakeConfig
	trauma float64
	t      float64
	// Scale, if set, multiplies the shake and gates it: the settings manager's
	// ShakeScale returns 0 when motion is reduced or the shake slider is 0.
	Scale func() float64
}

// NewShake creates a Shake.
func NewShake(cfg ShakeConfig, scale func() float64) *Shake {
	return &Shake{cfg: cfg.withDefaults(), Scale: scale}
}

// AddTrauma adds a hit of the given strength (0..1) and clamps the total to 1.
// While Scale reports 0, hits are discarded, so a player who turns shake off
// does not get a burst when turning it back on.
func (s *Shake) AddTrauma(amount float64) {
	if !finite(amount) || amount <= 0 || s.scale() <= 0 {
		return
	}
	s.trauma = math.Min(1, s.trauma+amount)
}

// Trauma returns the current trauma.
func (s *Shake) Trauma() float64 { return s.trauma }

// Clear removes all trauma.
func (s *Shake) Clear() { s.trauma = 0 }

func (s *Shake) scale() float64 {
	if s.Scale == nil {
		return 1
	}
	return math.Max(0, math.Min(1, s.Scale()))
}

func noise(t, phase float64) float64 {
	return 0.6*math.Sin(t+phase) + 0.4*math.Sin(2.31*t+phase*1.7+1.3)
}

func seedPhase(seed uint32, k int) float64 {
	h := (seed + uint32(k)*2654435761) * 2246822519
	h ^= h >> 15
	return float64(h&0xffff) / 65535 * 2 * math.Pi
}

// Update advances time by dt seconds and returns the camera-space offset
// (right, up, back) and the roll for this frame.
func (s *Shake) Update(dt float64) (offset Vec3, roll float64) {
	if !finite(dt) || dt < 0 {
		dt = 0
	}
	s.t += dt
	k := s.scale()
	if k <= 0 {
		s.trauma = 0
		return Vec3{}, 0
	}
	if s.trauma <= 0 {
		return Vec3{}, 0
	}
	amp := math.Pow(s.trauma, s.cfg.Exponent) * k
	w := s.t * s.cfg.Frequency * 2 * math.Pi
	offset = Vec3{
		s.cfg.MaxOffset.X * amp * noise(w, seedPhase(s.cfg.Seed, 0)),
		s.cfg.MaxOffset.Y * amp * noise(w, seedPhase(s.cfg.Seed, 1)),
		s.cfg.MaxOffset.Z * amp * noise(w, seedPhase(s.cfg.Seed, 2)),
	}
	roll = s.cfg.MaxRoll * amp * noise(w, seedPhase(s.cfg.Seed, 3))
	s.trauma = math.Max(0, s.trauma-s.cfg.Decay*dt)
	return offset, roll
}
