package headless

import (
	"image"
	"image/color"
	"math"

	"m31labs.dev/gosx/render/gpu"
)

// This file gives the CPU backend a fragment stage for the fullscreen passes.
//
// The backend used to turn every post-effect pass into a copy. That was honest
// about geometry — the frame survived instead of going black — but it made the
// present pass a copy too, so a headless frame was the raw linear colour cut to
// eight bits. The browser writes lit colour to a high dynamic range target and
// tone maps it into the swap chain, so a bright surface rolls off there and
// clipped here. Frames read darker in the browser, and an emissive material read
// flat white on the server.
//
// That reached users the day build-time poster frames shipped. A poster is the
// still image the page paints while the runtime loads, and the runtime replaces
// it with the live canvas. A poster that clips where the canvas rolls off makes
// the swap visible.
//
// Each pass here is a term-for-term reading of the WGSL that render/bundle
// compiles for a real device. Read the WGSL first, then this file. Where the two
// disagree the WGSL wins, because a browser and a native GPU both run the WGSL
// and only the tests run this.
//
// Three passes still copy: ambient occlusion, depth of field, and every Selena
// custom pass. They read the depth attachment or author-supplied WGSL, which
// this file does not interpret. A copy keeps the frame and makes the effect
// visibly absent, which is the honest failure.

// postFragment shades one pixel of a fullscreen pass. uv runs from zero to one
// across the destination, with v zero at the top row, which is the orientation
// the fullscreen triangle in every bundle shader produces.
type postFragment func(uv [2]float32) [3]float32

// postTarget is the destination of a fullscreen pass: either the surface
// framebuffer or one colour texture.
type postTarget struct {
	img  *image.RGBA
	tex  *Texture
	w, h int
}

// postDestination resolves the single colour attachment of a fullscreen pass.
// It reports false when the pass stores nothing, so a discarded pass costs no
// per-pixel work.
func (r *RenderPassEncoder) postDestination() (postTarget, bool) {
	for _, att := range r.desc.ColorAttachments {
		if att.StoreOp == gpu.StoreOpDiscard {
			continue
		}
		if _, ok := att.View.(*SurfaceView); ok {
			fb := r.device.framebuffer
			if fb == nil {
				return postTarget{}, false
			}
			bounds := fb.Bounds()
			return postTarget{img: fb, w: bounds.Dx(), h: bounds.Dy()}, true
		}
		if view, ok := att.View.(*TextureView); ok && view.owner != nil {
			return postTarget{tex: view.owner, w: view.owner.width, h: view.owner.height}, true
		}
	}
	return postTarget{}, false
}

// runPostPass evaluates shade once per destination pixel.
//
// The loop walks the destination, not the source, because a pass may downsample:
// the bloom bright pass reads a full-resolution target and writes a half-size
// one. Sampling by destination uv keeps that case right with no special case.
//
// Alpha is one. Every bundle post shader returns vec4(colour, 1.0).
func runPostPass(dst postTarget, shade postFragment) {
	if dst.w <= 0 || dst.h <= 0 || shade == nil {
		return
	}
	invW := 1 / float32(dst.w)
	invH := 1 / float32(dst.h)
	for y := 0; y < dst.h; y++ {
		v := (float32(y) + 0.5) * invH
		for x := 0; x < dst.w; x++ {
			rgb := shade([2]float32{(float32(x) + 0.5) * invW, v})
			out := color.RGBA{
				R: clampByte(rgb[0]),
				G: clampByte(rgb[1]),
				B: clampByte(rgb[2]),
				A: 255,
			}
			if dst.img != nil {
				bounds := dst.img.Bounds()
				dst.img.SetRGBA(bounds.Min.X+x, bounds.Min.Y+y, out)
				continue
			}
			writeTextureRGBA(dst.tex, -1, x, y, out)
		}
	}
}

// postFragmentFor returns the fragment stage of one fullscreen pass, or nil
// when this backend does not run that pass.
//
// The labels are the pass labels render/bundle records, not the pipeline
// labels. Both exist and they differ: the present pipeline is "bundle.present"
// and its pass is "bundle.present.compose".
//
// dst carries the destination size, which each stage needs to pick its sampler.
// Read newPostSampler for why.
func postFragmentFor(label string, bg *BindGroup, dst postTarget) postFragment {
	if bg == nil {
		return nil
	}
	switch label {
	case "bundle.present", "bundle.present.compose":
		return newComposePresentFragment(bg, dst)
	case "bundle.postfx.vignette":
		return newVignetteFragment(bg, dst)
	case "bundle.postfx.colorGrade":
		return newColorGradeFragment(bg, dst)
	case "bundle.bloom.bright":
		return newBrightPassFragment(bg, dst)
	case "bundle.bloom.blurH", "bundle.bloom.blurV":
		return newBlurFragment(bg, dst)
	}
	return nil
}

// postSampler reads one bound texture the way the present sampler does: linear
// filtering with clamp-to-edge addressing.
//
// nearest is set when the source and the destination hold the same number of
// texels. Every sample then lands on a texel centre, the four bilinear taps
// carry weights of one and zero, and the result is one exact fetch. Taking that
// fetch directly is not an approximation; it is the same number for a quarter of
// the reads. The present pass reads a full-resolution colour target into a
// full-resolution target, and it is the one pass every frame runs, so this is
// where the cost of the whole file sits.
//
// A blur tap steps off the texel grid, so the blur stage forces the filtered
// path even when the sizes match.
type postSampler struct {
	tex     *Texture
	nearest bool
}

func newPostSampler(tex *Texture, dst postTarget) postSampler {
	return postSampler{
		tex:     tex,
		nearest: tex != nil && tex.width == dst.w && tex.height == dst.h,
	}
}

// filtered returns a copy of the sampler with the fast path turned off.
func (s postSampler) filtered() postSampler {
	s.nearest = false
	return s
}

// at reads one colour. A missing texture reads black; read samplePostRGB for
// why that differs from sampleTextureRGB.
func (s postSampler) at(uv [2]float32) [3]float32 {
	if s.tex == nil {
		return [3]float32{}
	}
	if s.nearest {
		return postTexelRGB(s.tex, int(uv[0]*float32(s.tex.width)), int(uv[1]*float32(s.tex.height)))
	}
	return samplePostRGB(s.tex, uv)
}

// postBindTexture returns the texture bound at one binding index, or nil.
func postBindTexture(bg *BindGroup, binding int) *Texture {
	for _, entry := range bg.desc.Entries {
		if entry.Binding != binding {
			continue
		}
		if view, ok := entry.TextureView.(*TextureView); ok {
			return view.owner
		}
		return nil
	}
	return nil
}

// postBindVec4 returns the first sixteen bytes of the uniform buffer bound at
// one binding index, read as four floats. A missing or short buffer reads as
// four zeros, which every pass below treats as "the author set nothing".
func postBindVec4(bg *BindGroup, binding int) [4]float32 {
	for _, entry := range bg.desc.Entries {
		if entry.Binding != binding {
			continue
		}
		buf, ok := entry.Buffer.(*Buffer)
		if !ok || buf == nil || len(buf.data) < 16 {
			return [4]float32{}
		}
		return readVec4At(buf.data, 0)
	}
	return [4]float32{}
}

// samplePostRGB reads one texture with bilinear filtering and clamp-to-edge
// addressing, which is what buildPresentSampler asks a real device for.
//
// A missing texture reads black. sampleTextureRGB returns white for a missing
// texture instead, because it serves material slots where an unbound map must
// not tint the surface. A missing bloom target must add nothing, so the two
// helpers cannot share a fallback.
//
// A pass whose source and destination are the same size lands every sample on a
// texel centre, so the four taps collapse to one exact fetch and no filtering
// error enters the frame.
func samplePostRGB(t *Texture, uv [2]float32) [3]float32 {
	if t == nil || t.width <= 0 || t.height <= 0 {
		return [3]float32{}
	}
	x := uv[0]*float32(t.width) - 0.5
	y := uv[1]*float32(t.height) - 0.5
	x0 := int(math.Floor(float64(x)))
	y0 := int(math.Floor(float64(y)))
	tx := x - float32(x0)
	ty := y - float32(y0)
	c00 := postTexelRGB(t, x0, y0)
	c10 := postTexelRGB(t, x0+1, y0)
	c01 := postTexelRGB(t, x0, y0+1)
	c11 := postTexelRGB(t, x0+1, y0+1)
	return [3]float32{
		mix(mix(c00[0], c10[0], tx), mix(c01[0], c11[0], tx), ty),
		mix(mix(c00[1], c10[1], tx), mix(c01[1], c11[1], tx), ty),
		mix(mix(c00[2], c10[2], tx), mix(c01[2], c11[2], tx), ty),
	}
}

// postTexelRGB fetches one texel with clamp-to-edge addressing.
//
// It reads the eight-bit mirror every non-depth texture carries, because that is
// the branch readTextureRGBA takes first for layer zero and a blur takes
// seventy-two of these reads per output texel. The fallback covers a texture
// with no mirror, so the two paths return the same value and only the cost
// differs. The golden suite proves that: it did not move when this read replaced
// the call.
func postTexelRGB(t *Texture, x, y int) [3]float32 {
	x = clampTextureIndex(x, t.width)
	y = clampTextureIndex(y, t.height)
	if off := (y*t.width + x) * 4; off+3 < len(t.rgba) {
		return [3]float32{
			float32(t.rgba[off+0]) / 255,
			float32(t.rgba[off+1]) / 255,
			float32(t.rgba[off+2]) / 255,
		}
	}
	col := readTextureRGBA(t, 0, x, y)
	return [3]float32{
		float32(col.R) / 255,
		float32(col.G) / 255,
		float32(col.B) / 255,
	}
}

// -----------------------------------------------------------------------
// Tone-map operators. These mirror composePresentWGSL in render/bundle/bloom.go
// and the browser copy WGSL_POST_TONEMAPPING_FRAGMENT.
// -----------------------------------------------------------------------

// acesFilmicToneMap is the Narkowicz fit of the ACES curve. All three copies of
// the engine carry the same five constants.
func acesFilmicToneMap(x [3]float32) [3]float32 {
	const (
		a = 2.51
		b = 0.03
		c = 2.43
		d = 0.59
		e = 0.14
	)
	var out [3]float32
	for i, v := range x {
		out[i] = clamp01f((v * (a*v + b)) / (v*(c*v+d) + e))
	}
	return out
}

// reinhardToneMap is x/(1+x) per channel.
func reinhardToneMap(x [3]float32) [3]float32 {
	var out [3]float32
	for i, v := range x {
		out[i] = v / (1 + v)
	}
	return out
}

// filmicToneMap is the Hejl and Burgess-Dawson curve. It bakes the sRGB
// transfer into the operator, so it is brighter and more contrasted than ACES
// at the same exposure.
func filmicToneMap(x [3]float32) [3]float32 {
	var out [3]float32
	for i, v := range x {
		c := v - 0.004
		if c < 0 {
			c = 0
		}
		out[i] = clamp01f((c * (6.2*c + 0.5)) / (c*(6.2*c+1.7) + 0.06))
	}
	return out
}

// applyToneMap selects one operator by mode and applies the exposure first.
//
// The mode numbers come from toneMapModeCode in render/bundle/bloom.go, which
// uses the browser table: 0 clamps, 1 is ACES, 2 is Reinhard, 3 is filmic.
// Anything else is ACES, which is what the default empty tone-map string means.
func applyToneMap(x [3]float32, mode, exposure float32) [3]float32 {
	if exposure < 0 {
		exposure = 0
	}
	exposed := [3]float32{
		maxF(x[0]*exposure, 0),
		maxF(x[1]*exposure, 0),
		maxF(x[2]*exposure, 0),
	}
	switch int(mode) {
	case 0:
		return [3]float32{clamp01f(exposed[0]), clamp01f(exposed[1]), clamp01f(exposed[2])}
	case 2:
		return reinhardToneMap(exposed)
	case 3:
		return filmicToneMap(exposed)
	}
	return acesFilmicToneMap(exposed)
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// -----------------------------------------------------------------------
// Per-pass fragment stages.
// -----------------------------------------------------------------------

// newComposePresentFragment mirrors composePresentWGSL: sample the high dynamic
// range target, add the blurred bloom target scaled by the authored intensity,
// then tone map.
//
// Binding numbers come from the shader: 0 is the colour source, 2 is the bloom
// target, 4 carries the bloom parameters, and 5 carries the tone-map mode and
// the exposure.
func newComposePresentFragment(bg *BindGroup, dst postTarget) postFragment {
	source := postBindTexture(bg, 0)
	if source == nil {
		return nil
	}
	src := newPostSampler(source, dst)
	// The bloom target is half resolution, so it always takes the filtered path.
	glowTex := postBindTexture(bg, 2)
	glow := newPostSampler(glowTex, dst)
	bloom := postBindVec4(bg, 4)
	present := postBindVec4(bg, 5)
	intensity := bloom[1]
	mode, exposure := present[0], present[1]
	return func(uv [2]float32) [3]float32 {
		hdr := src.at(uv)
		if intensity != 0 && glowTex != nil {
			g := glow.at(uv)
			hdr[0] += g[0] * intensity
			hdr[1] += g[1] * intensity
			hdr[2] += g[2] * intensity
		}
		return applyToneMap(hdr, mode, exposure)
	}
}

// newVignetteFragment mirrors vignetteWGSL in render/bundle/native_postfx.go.
// The falloff is a smoothstep on the distance from the centre of the frame,
// scaled by the authored intensity.
func newVignetteFragment(bg *BindGroup, dst postTarget) postFragment {
	source := postBindTexture(bg, 0)
	if source == nil {
		return nil
	}
	src := newPostSampler(source, dst)
	amount := maxF(postBindVec4(bg, 3)[0], 0)
	return func(uv [2]float32) [3]float32 {
		c := src.at(uv)
		dx := uv[0] - 0.5
		dy := uv[1] - 0.5
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		v := 1 - smoothstep(0.3, 0.7, dist*amount)
		return [3]float32{c[0] * v, c[1] * v, c[2] * v}
	}
}

// newColorGradeFragment mirrors colorGradeWGSL in
// render/bundle/native_postfx.go: exposure, then contrast about mid grey, then
// saturation about Rec.709 luma, then a clamp.
func newColorGradeFragment(bg *BindGroup, dst postTarget) postFragment {
	source := postBindTexture(bg, 0)
	if source == nil {
		return nil
	}
	src := newPostSampler(source, dst)
	params := postBindVec4(bg, 3)
	exposure := maxF(params[0], 0)
	contrast := maxF(params[1], 0)
	saturation := maxF(params[2], 0)
	return func(uv [2]float32) [3]float32 {
		c := src.at(uv)
		for i := range c {
			c[i] = mix(0.5, c[i]*exposure, contrast)
		}
		gray := rec709Luma(c)
		for i := range c {
			c[i] = clamp01f(mix(gray, c[i], saturation))
		}
		return c
	}
}

// newBrightPassFragment mirrors brightPassWGSL in render/bundle/bloom.go.
//
// The threshold is a soft knee: subtract the authored threshold from the
// luminance, then scale the colour by t/(t+1). The browser WebGPU copy cut hard
// until 2026-07-27 and now carries the same knee, so all three copies agree.
// TestBrightPassMatchesJSWebGPU in render/bundle pins the two shader copies, and
// this CPU copy must follow whichever way they move.
func newBrightPassFragment(bg *BindGroup, dst postTarget) postFragment {
	source := postBindTexture(bg, 0)
	if source == nil {
		return nil
	}
	// The source is full resolution and the target is half, so this pass takes
	// the filtered path and the filtering is the downsample.
	src := newPostSampler(source, dst)
	threshold := postBindVec4(bg, 2)[0]
	return func(uv [2]float32) [3]float32 {
		c := src.at(uv)
		lum := maxF(rec709Luma(c)-threshold, 0)
		soft := lum / (lum + 1)
		return [3]float32{c[0] * soft, c[1] * soft, c[2] * soft}
	}
}

// blurWeights are the nine-tap Gaussian weights of blurWGSL, folded to the five
// distinct values. Index i is the weight of the pair at distance i, except index
// zero, which is the centre tap and is counted once.
var blurWeights = [5]float32{0.227027, 0.194594, 0.121621, 0.054054, 0.016216}

// newBlurFragment mirrors blurWGSL in render/bundle/bloom.go. The axis comes
// from the bound texel offset, so one function serves both blur passes.
func newBlurFragment(bg *BindGroup, dst postTarget) postFragment {
	source := postBindTexture(bg, 0)
	if source == nil {
		return nil
	}
	// Every tap but the centre steps off the texel grid, so the nearest fast
	// path would drop the blur to a copy. Force the filtered path.
	src := newPostSampler(source, dst).filtered()
	off := postBindVec4(bg, 2)
	return func(uv [2]float32) [3]float32 {
		var sum [3]float32
		centre := src.at(uv)
		for i := range sum {
			sum[i] = centre[i] * blurWeights[0]
		}
		for step := 1; step < 5; step++ {
			w := blurWeights[step]
			d := float32(step)
			plus := src.at([2]float32{uv[0] + off[0]*d, uv[1] + off[1]*d})
			minus := src.at([2]float32{uv[0] - off[0]*d, uv[1] - off[1]*d})
			for i := range sum {
				sum[i] += (plus[i] + minus[i]) * w
			}
		}
		return sum
	}
}

// rec709Luma is the luminance weight vector both the bright pass and the colour
// grade use.
func rec709Luma(c [3]float32) float32 {
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}
