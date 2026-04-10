package scene

// PostFXMaxPixelsUnbounded opts out of the postfx pixel cap entirely.
// Set PostFX.MaxPixels to this constant when you explicitly need the
// v0.14.0 behavior of running postfx at full canvas resolution.
//
// Value is 1<<30 (1,073,741,824) — effectively unbounded because the
// scaling factor clamps to 1.0 for any physically realistic canvas.
// We use a large integer rather than a negative sentinel so the field
// type remains a simple int with intuitive semantics (bigger = less
// aggressive scaling).
const PostFXMaxPixelsUnbounded = 1 << 30

// Common PostFX.MaxPixels presets. Picked values correspond to common
// display resolutions; pick the one closest to the maximum perceptual
// quality you need for your scene.
const (
	PostFXMaxPixels540p  = 960 * 540   //   518_400
	PostFXMaxPixels720p  = 1280 * 720  //   921_600
	PostFXMaxPixels1080p = 1920 * 1080 // 2_073_600 (default)
	PostFXMaxPixels1440p = 2560 * 1440 // 3_686_400
	PostFXMaxPixels4K    = 3840 * 2160 // 8_294_400
)

// PostFX is the post-processing effect chain attached to a Scene3D.
//
// When Effects is empty (the default), the scene renders directly to the
// canvas with no offscreen framebuffer. When Effects has at least one entry,
// the scene renders to an HDR offscreen target and the effect chain runs in
// order, ping-ponging between framebuffers, with the final pass blitting to
// the canvas.
//
// Effects run in declaration order. A typical galaxy chain is:
//
//	scene.PostFX{Effects: []scene.PostEffect{
//	    scene.Bloom{Threshold: 0.7, Strength: 0.6, Radius: 12},
//	    scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
//	}}
type PostFX struct {
	Effects []PostEffect

	// MaxPixels caps the postfx offscreen pipeline at this many pixels
	// (width*height, after devicePixelRatio). When the canvas exceeds the
	// cap, the pipeline scales down uniformly to fit; below the cap, it
	// runs at native resolution. Memory stays flat across display sizes.
	//
	//   zero value (0): apply the safe default cap of 1080p (2_073_600).
	//                   Recommended for most scenes; protects against
	//                   accidental multi-hundred-megabyte framebuffer
	//                   allocations on high-DPR displays.
	//   positive:       explicit cap in pixels. Typically set via one of
	//                   the PostFXMaxPixels* constants.
	//   negative:       treated as zero (safe default). Not recommended;
	//                   use PostFXMaxPixelsUnbounded to opt out instead.
	//
	// Scaling formula:
	//     factor = min(1, sqrt(MaxPixels / canvasPixels))
	//
	// Note: canvasPixels reflects the backing-store size (includes DPR).
	MaxPixels int
}

// PostEffect is the interface satisfied by every post-processing effect.
// Concrete types include Tonemap, Bloom, Vignette, and ColorGrade.
//
// The interface is sealed via an unexported method so external packages
// cannot define their own effects without coordination with the renderer.
type PostEffect interface {
	isPostEffect()
}

// TonemapMode selects the tone mapping curve.
type TonemapMode int

const (
	// TonemapACES is the ACES filmic curve. Default.
	TonemapACES TonemapMode = iota
	// TonemapReinhard is the simple Reinhard operator (placeholder; the
	// JS-side implementation currently uses ACES regardless of mode and a
	// future task will branch on mode).
	TonemapReinhard
	// TonemapFilmic is a filmic curve placeholder.
	TonemapFilmic
)

// Tonemap maps HDR scene colors into the displayable [0,1] range.
//
// The JS-side implementation in client/js/bootstrap-src/16-scene-webgl.js
// uses an ACES filmic curve. The Mode field is reserved for future
// per-mode shader branches; today all modes route through ACES.
type Tonemap struct {
	Mode     TonemapMode
	Exposure float32 // multiplier applied before the curve (default 1.0)
}

func (Tonemap) isPostEffect() {}

// Bloom adds an HDR-driven glow around bright pixels.
//
// Implementation: bright-pass extracts pixels above the luminance Threshold
// into a half-resolution FBO, separable Gaussian blur runs horizontally and
// vertically, and the result is additively composited back onto the scene at
// Strength.
type Bloom struct {
	Threshold float32 // luminance above which pixels bloom (default 0.8)
	Strength  float32 // intensity of the bloom contribution (default 0.5)
	Radius    float32 // blur radius in pixels (default 5)
}

func (Bloom) isPostEffect() {}

// Vignette darkens the screen edges.
type Vignette struct {
	Intensity float32 // 0..1, default 1.0
}

func (Vignette) isPostEffect() {}

// ColorGrade applies exposure / contrast / saturation adjustments.
type ColorGrade struct {
	Exposure   float32 // multiplier (default 1.0)
	Contrast   float32 // 0..2, default 1.0
	Saturation float32 // 0..2, default 1.0
}

func (ColorGrade) isPostEffect() {}

// resolveMaxPixels normalizes the field for IR emission. Zero or negative
// values become the default 1080p cap; positive values pass through.
func (p PostFX) resolveMaxPixels() int {
	if p.MaxPixels <= 0 {
		return PostFXMaxPixels1080p
	}
	return p.MaxPixels
}

// migrateEnvironmentTonemap implements backwards compatibility for the
// pre-PostFX Environment.ToneMapping / Environment.Exposure fields. When
// those fields are set and the existing PostFX chain does NOT already
// include a Tonemap effect, this returns a Tonemap effect to prepend.
//
// Returns nil if no synthesis is needed (no legacy fields, or user already
// declared a Tonemap explicitly).
func migrateEnvironmentTonemap(env Environment, existing []PostEffectIR) PostEffectIR {
	if env.ToneMapping == "" && env.Exposure == 0 {
		return nil
	}
	for _, e := range existing {
		if _, isTonemap := e.(TonemapIR); isTonemap {
			return nil
		}
	}
	mode := env.ToneMapping
	if mode == "" {
		mode = "aces"
	}
	exposure := env.Exposure
	if exposure == 0 {
		exposure = 1.0
	}
	return TonemapIR{Mode: mode, Exposure: exposure}
}
