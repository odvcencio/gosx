package scene

import (
	"encoding/json"
	"strings"
)

// MotionBlurQuality selects how many samples the blur gathers along each
// pixel's velocity and how large the velocity tiles are.
type MotionBlurQuality string

const (
	MotionBlurLow    MotionBlurQuality = "low"    // 6 taps, 32 px tiles
	MotionBlurMedium MotionBlurQuality = "medium" // 10 taps, 24 px tiles (default)
	MotionBlurHigh   MotionBlurQuality = "high"   // 16 taps, 16 px tiles
)

// Motion blur defaults, applied when a field is zero.
const (
	DefaultMotionBlurStrength = 0.5 // shutter fraction of the frame: 0.5 is a 180 degree shutter
	DefaultMotionBlurMaxBlur  = 32  // longest blur, in pixels at 1080p
)

// MotionBlur blurs the scene along per-pixel motion. It is opt-in: add it to
// PostFX.Effects. It reads the renderer's motion vectors (camera motion, rigid
// bodies and skinned meshes), so it blurs a moving object along its path and a
// turning camera across the whole view.
//
// A pass finds the largest velocity in each screen tile, spreads it to the
// neighbor tiles, then gathers samples along each pixel's own velocity,
// weighted by depth so a sharp foreground object stays sharp against a blurred
// background. Put it before Tonemap and Bloom, after Decals, and before TAA or
// FXAA.
//
// On WebGL2 the pass runs only when the renderer produces motion vectors;
// otherwise the effect is skipped and the scene renders unblurred.
//
// MotionBlur belongs on a quality ladder rung's PostEffects list as
// "motionBlur" so the adaptive quality governor can shed it under load.
type MotionBlur struct {
	Quality MotionBlurQuality `json:"quality,omitempty"`
	// Strength scales velocity by the fraction of the frame the shutter is
	// open, 0..1. Default 0.5.
	Strength float32 `json:"strength,omitempty"`
	// MaxBlur caps the blur length in pixels at 1080p, 1..128. Default 32.
	MaxBlur float32 `json:"maxBlur,omitempty"`
}

func (MotionBlur) isPostEffect() {}

// MotionBlurIR is the wire form of MotionBlur with defaults applied.
type MotionBlurIR struct {
	Quality  string  `json:"quality"`
	Strength float64 `json:"strength"`
	MaxBlur  float64 `json:"maxBlur"`
	Samples  int     `json:"samples"`
	TileSize int     `json:"tileSize"`
}

func (ir MotionBlurIR) MarshalJSON() ([]byte, error) {
	type wire MotionBlurIR
	return json.Marshal(struct {
		Kind string `json:"kind"`
		wire
	}{PostEffectKindMotionBlur, wire(ir)})
}

func (ir MotionBlurIR) legacyProps() map[string]any {
	b, _ := ir.MarshalJSON()
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func (m MotionBlur) lower() MotionBlurIR {
	ir := MotionBlurIR{
		Strength: DefaultMotionBlurStrength,
		MaxBlur:  DefaultMotionBlurMaxBlur,
	}
	if m.Strength > 0 {
		ir.Strength = clampAtmosphereParam(float64(m.Strength), 0.01, 1)
	}
	if m.MaxBlur > 0 {
		ir.MaxBlur = clampAtmosphereParam(float64(m.MaxBlur), 1, 128)
	}
	switch MotionBlurQuality(strings.ToLower(string(m.Quality))) {
	case MotionBlurLow:
		ir.Quality, ir.Samples, ir.TileSize = "low", 6, 32
	case MotionBlurHigh:
		ir.Quality, ir.Samples, ir.TileSize = "high", 16, 16
	default:
		ir.Quality, ir.Samples, ir.TileSize = "medium", 10, 24
	}
	return ir
}
