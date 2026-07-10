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
// Concrete types include Tonemap, Bloom, Vignette, ColorGrade, SSAO, and DOF.
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
	// TonemapReinhard is the simple Reinhard operator.
	TonemapReinhard
	// TonemapFilmic is a compact filmic curve with a softer shoulder.
	TonemapFilmic
)

// Tonemap maps HDR scene colors into the displayable [0,1] range.
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

	// Scale is an additional bloom-internal downscale applied on top of
	// the PostFX.MaxPixels factor. The zero value maps to 0.5 (runtime
	// default, matching v0.14.0 behavior). Values outside (0, 1] are
	// silently dropped at the IR boundary — the JS runtime falls back
	// to 0.5 when no scale is emitted.
	//
	// Bloom is a low-frequency blur, so Scale can go much lower than the
	// main pipeline with no visible quality loss.
	Scale float32
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

// SSAO applies a screen-space ambient-occlusion style darkening pass.
type SSAO struct {
	Radius    float32 // sample radius in pixels (default 4)
	Intensity float32 // 0..2, default 0.55
	Bias      float32 // reserved for the depth-backed SSAO path
}

func (SSAO) isPostEffect() {}

// DOF applies a depth-of-field blur around the camera focus plane.
type DOF struct {
	FocusDistance float32 // world-space distance from camera (default 8)
	Aperture      float32 // blur strength multiplier (default 0.04)
	MaxBlur       float32 // max blur radius in pixels (default 8)
}

func (DOF) isPostEffect() {}

// CustomPostStage selects where a CustomPost pass executes within the
// bloom/tonemap chain.
type CustomPostStage string

const (
	// CustomPostBeforeTonemap places the pass after bloom, before tonemap.
	// This is the default when Stage is empty.
	CustomPostBeforeTonemap CustomPostStage = "beforeTonemap"
	// CustomPostAfterTonemap places the pass after tonemap, in the LDR region.
	CustomPostAfterTonemap CustomPostStage = "afterTonemap"
)

// CustomPost inserts a user-authored Selena post-process pass into the
// post-effect chain. The Selena post contract (WGSL fullscreen triangle via
// @builtin(vertex_index), entries vertexMain/fragmentMain, @group(0) bindings
// 0-4 sceneColor/sceneColorSampler/sceneDepth/sceneDepthSampler/UserUniforms)
// is emitted by selena materials with kind "post".
//
// Ordering: by default (Stage == "" or "beforeTonemap") custom passes run
// after bloom, before tonemap. Set Stage to "afterTonemap" to run after
// tonemapping in the LDR region.
//
// On failure (shader validation error, unsupported platform) the pass becomes
// an identity passthrough rather than aborting the frame.
type CustomPost struct {
	// Name is a stable identifier for diagnostics and pipeline caching.
	Name     string
	Material *CustomMaterial
	// Uniforms holds per-frame uniform overrides. Merged with the material's
	// own defaults; keys match the Selena param names.
	Uniforms map[string]any
	// Stage controls ordering relative to bloom/tonemap.
	// Empty string and "beforeTonemap" are equivalent (default).
	Stage CustomPostStage
}

func (CustomPost) isPostEffect() {}

// FXAA applies fast approximate anti-aliasing (the FXAA 3.11 quality
// preset) as a full-resolution pass.
//
// Post-processing renders the scene into an offscreen HDR framebuffer
// before compositing to the canvas, which defeats hardware MSAA — once any
// PostEffect is present, Props.MSAASamples no longer smooths the presented
// image. FXAA is how a post-FX scene gets edge smoothing back without a
// second full-scene MSAA-resolve render, and it is cheap enough to run at
// full pass resolution (unlike Bloom, which should run reduced).
//
// FXAA has no tunable fields — it is a fixed pass. Always place it LAST in
// Effects: it edge-searches the final tonemapped/graded LDR image via
// green-channel luma, so running it before Tonemap would search HDR data
// and search wrong.
type FXAA struct{}

func (FXAA) isPostEffect() {}

// GameplayPostFX returns a post-processing chain sized for 60fps skinned
// gameplay: half-resolution Bloom with a conservative threshold (so only
// emissive/specular hot spots bloom, not general geometry), ACES Tonemap,
// and a chain-end FXAA pass for edge anti-aliasing.
//
// Budget intent: this is deliberately the minimum chain that still buys
// "glow + clean edges" for active play — 1 bloom bright-pass + 2 separable
// blurs + 1 composite (all at half of the already-720p-capped resolution
// via Bloom.Scale), 1 tonemap pass, and 1 full-res FXAA pass. It
// intentionally omits SSAO, DOF, ColorGrade, and Vignette: those are
// cinematic/static-page effects that cost more passes than a 60fps frame
// budget can absorb alongside skinned-mesh rendering. Compose your own
// richer chain (SSAO+Bloom+Tonemap+ColorGrade+Vignette) for menus,
// portraits, and other static pages instead of reusing this preset there.
func GameplayPostFX() PostFX {
	return PostFX{
		MaxPixels: PostFXMaxPixels720p,
		Effects: []PostEffect{
			Bloom{Threshold: 0.92, Strength: 0.35, Radius: 6, Scale: 0.5},
			Tonemap{Mode: TonemapACES, Exposure: 1.0},
			FXAA{},
		},
	}
}

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
