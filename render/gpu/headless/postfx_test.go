package headless

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
	"m31labs.dev/gosx/render/gpu"
)

// These tests cover the fragment stages this backend runs for the fullscreen
// passes. Each one states a number the pass must produce, not only that the
// frame moved, because a frame that moves in the wrong direction still moves.

const (
	toneProbeWidth  = 24
	toneProbeHeight = 16
)

// toneProbeScene fills the whole frame with one background colour and draws
// nothing. The present pass then reads one constant, so the framebuffer holds
// the output of the tone-map operator for one known input and the arithmetic is
// checkable by hand.
func toneProbeScene(background, toneMapping string, exposure float64) engine.RenderBundle {
	return engine.RenderBundle{
		Background: background,
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Environment: engine.RenderEnvironment{
			ToneMapping: toneMapping,
			Exposure:    exposure,
		},
	}
}

func renderToneProbe(t *testing.T, frame engine.RenderBundle) *image.RGBA {
	t.Helper()
	device, surface := New(toneProbeWidth, toneProbeHeight)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	if err := renderer.Frame(frame, toneProbeWidth, toneProbeHeight, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return cloneTestRGBA(device.Framebuffer())
}

// TestPresentPassAppliesAuthoredToneMap is the pixel proof that the present pass
// runs on this backend.
//
// The background is 0x33 on every channel, which is 51 of 255 and 0.2 in the
// linear range the lit stage writes. Each expected byte below is that 0.2 put
// through the named operator by hand:
//
//   - none clamps, so 0.2 stays 0.2 and the byte stays 0x33.
//   - ACES gives 0.2*(2.51*0.2+0.03) / (0.2*(2.43*0.2+0.59)+0.14), which is
//     0.2995, so the byte is 76. The Narkowicz fit has a slope above one near
//     black, so it lifts a dark background rather than darkening it.
//   - Reinhard gives 0.2/1.2, which is 0.1667, so the byte is 42.
//   - The Hejl curve subtracts its 0.004 black point first, then gives
//     0.196*(6.2*0.196+0.5) / (0.196*(6.2*0.196+1.7)+0.06), which is 0.5325, so
//     the byte is 136. That curve bakes the sRGB transfer in, which is why it
//     lands so much higher than the other two.
//
// The four values are far apart, so a pass that ran the wrong operator cannot
// pass this test by rounding.
func TestPresentPassAppliesAuthoredToneMap(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		wantR uint8
	}{
		{"none", 0x33},
		{"linear", 0x33},
		{"", 76},
		{"aces", 76},
		{"reinhard", 42},
		{"filmic", 136},
	} {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			got := renderToneProbe(t, toneProbeScene("#333333", tc.mode, 0)).RGBAAt(toneProbeWidth/2, toneProbeHeight/2)
			if diff := int(got.R) - int(tc.wantR); diff > 1 || diff < -1 {
				t.Fatalf("tone map %q presented red %d, want about %d; the present pass ran the wrong operator or none at all",
					tc.mode, got.R, tc.wantR)
			}
			if got.R != got.G || got.G != got.B {
				t.Fatalf("tone map %q presented %+v; a grey input must stay grey", tc.mode, got)
			}
		})
	}
}

// TestPresentPassAppliesExposure proves the exposure lane reaches the frame and
// lands before the operator.
//
// Under the clamp there is no curve, so the result is the linear product and the
// arithmetic is exact: 0.2 at exposure 2 is 0.4, which is byte 102.
//
// Under ACES the curve is monotonic, so a higher exposure must give a higher
// byte until the curve saturates. The second half checks that ordering, which is
// what rejects an exposure applied after the operator: the operator clamps at
// one, so a later multiply would change nothing at all.
func TestPresentPassAppliesExposure(t *testing.T) {
	clamped := renderToneProbe(t, toneProbeScene("#333333", "none", 2)).RGBAAt(toneProbeWidth/2, toneProbeHeight/2)
	if diff := int(clamped.R) - 102; diff > 1 || diff < -1 {
		t.Errorf("exposure 2 under the clamp presented red %d, want about 102; the exposure lane does not reach the pass", clamped.R)
	}

	previous := -1
	for _, exposure := range []float64{0.5, 1, 2, 4} {
		got := renderToneProbe(t, toneProbeScene("#333333", "aces", exposure)).RGBAAt(toneProbeWidth/2, toneProbeHeight/2)
		if int(got.R) <= previous {
			t.Fatalf("exposure %.1f presented red %d, which is not above the previous step %d; "+
				"exposure must apply before the operator", exposure, got.R, previous)
		}
		previous = int(got.R)
	}
}

// TestVignettePassDarkensTheEdgeNotTheCentre proves the vignette fragment stage
// runs and runs the right way round.
//
// The scene is one flat background, so every pixel starts equal. The pass must
// leave the centre untouched and darken the corner, because the falloff is a
// smoothstep on the distance from the centre.
func TestVignettePassDarkensTheEdgeNotTheCentre(t *testing.T) {
	plain := renderToneProbe(t, toneProbeScene("#808080", "none", 0))
	frame := toneProbeScene("#808080", "none", 0)
	frame.PostEffects = []engine.RenderPostEffect{{Kind: "vignette", Intensity: 1}}
	vignetted := renderToneProbe(t, frame)

	centreX, centreY := toneProbeWidth/2, toneProbeHeight/2
	if got, want := vignetted.RGBAAt(centreX, centreY).R, plain.RGBAAt(centreX, centreY).R; got != want {
		t.Errorf("the vignette changed the centre pixel from %d to %d; the falloff starts at distance 0.3", want, got)
	}
	corner := vignetted.RGBAAt(0, 0).R
	plainCorner := plain.RGBAAt(0, 0).R
	if corner >= plainCorner {
		t.Fatalf("the vignette left the corner at %d against %d plain; it must darken the edge", corner, plainCorner)
	}
	if corner > plainCorner/2 {
		t.Errorf("the vignette only took the corner from %d to %d; at intensity 1 the corner of this frame sits past the smoothstep midpoint",
			plainCorner, corner)
	}
}

// TestColorGradePassAppliesSaturation proves the colour-grade fragment stage
// runs and mixes toward Rec.709 luma.
//
// The background is a saturated blue. At saturation zero the pass must replace
// every channel with the luma of the colour, so the frame reads grey. The check
// computes that luma from the linear background rather than restating it, which
// keeps the expectation tied to the input.
func TestColorGradePassAppliesSaturation(t *testing.T) {
	const linearBlue = 0xC0 / 255.0
	frame := toneProbeScene("#0000c0", "none", 0)
	frame.PostEffects = []engine.RenderPostEffect{
		{Kind: "colorGrade", Params: map[string]float64{"saturation": 0.0001}},
	}
	got := renderToneProbe(t, frame).RGBAAt(toneProbeWidth/2, toneProbeHeight/2)

	wantLuma := uint8(math.Round(0.0722 * linearBlue * 255))
	for _, ch := range []struct {
		name  string
		value uint8
	}{{"red", got.R}, {"green", got.G}, {"blue", got.B}} {
		if diff := int(ch.value) - int(wantLuma); diff > 1 || diff < -1 {
			t.Errorf("at saturation zero the %s channel reads %d, want about %d, which is the Rec.709 luma of the background",
				ch.name, ch.value, wantLuma)
		}
	}
}

// TestBloomChainReachesThePresentPass proves the three bloom passes run and that
// their result reaches the composed frame.
//
// A bloom intensity of zero adds nothing, so the two frames differ only by the
// blur the chain produced. The frame is one flat colour above the threshold, so
// the blurred bloom target is flat too and the composed frame must be uniformly
// brighter, never darker.
func TestBloomChainReachesThePresentPass(t *testing.T) {
	plain := renderToneProbe(t, toneProbeScene("#c0c0c0", "none", 0))
	frame := toneProbeScene("#c0c0c0", "none", 0)
	frame.PostEffects = []engine.RenderPostEffect{{Kind: "bloom", Threshold: 0.1, Intensity: 1, Radius: 4}}
	bloomed := renderToneProbe(t, frame)

	x, y := toneProbeWidth/2, toneProbeHeight/2
	before := plain.RGBAAt(x, y).R
	after := bloomed.RGBAAt(x, y).R
	if after <= before {
		t.Fatalf("bloom left the centre at %d against %d plain; the bright pass or the blur produced nothing", after, before)
	}

	// The knee decides how much. Luma of 0.7529 grey is 0.7529, the threshold is
	// 0.1, so the excess is 0.6529 and the scale is 0.6529/1.6529, which is
	// 0.395. The bloom target therefore holds about 0.2974, and at intensity 1
	// the composed value is about 1.05, which clamps to 255.
	if after != 0xff {
		t.Errorf("bloom presented %d at the centre, want 255; a flat frame far above the threshold overruns the range", after)
	}
}

// TestBrightPassKneeIsContinuousAtTheThreshold is the numeric case for the soft
// knee that brightDivergentTerms in render/bundle/postfx_drift_test.go records.
//
// The bright pass fragment stage is a pure function of one texel, so this test
// drives it directly. A hard cut would step from zero to the whole colour as the
// luminance crosses the threshold. The knee must stay near zero just above it.
func TestBrightPassKneeIsContinuousAtTheThreshold(t *testing.T) {
	const threshold = 0.5

	// Luminance of a grey texel is the texel value, so these drive the knee
	// directly through the source colour.
	below := brightPassValue(t, threshold, 0.499)
	justAbove := brightPassValue(t, threshold, 0.501)
	wellAbove := brightPassValue(t, threshold, 1.0)

	if below != 0 {
		t.Errorf("a texel below the threshold contributed %.4f, want 0", below)
	}
	if justAbove <= 0 {
		t.Errorf("a texel just above the threshold contributed nothing; the knee must rise from zero")
	}
	if justAbove > 0.01 {
		t.Errorf("a texel one part in five hundred above the threshold contributed %.4f; that is a step, not a knee", justAbove)
	}
	if wellAbove <= justAbove*10 {
		t.Errorf("a texel well above the threshold contributed %.4f against %.4f just above; the knee must keep rising", wellAbove, justAbove)
	}
}

// brightPassValue runs the bright pass on one grey texel and returns the red
// channel it produces.
func brightPassValue(t *testing.T, threshold, grey float32) float32 {
	t.Helper()
	shade := newBrightPassFragment(brightPassProbeBindGroup(t, threshold, grey), postTarget{w: 1, h: 1})
	if shade == nil {
		t.Fatal("the bright pass fragment stage did not build; the probe bind group is wrong")
	}
	return shade([2]float32{0.5, 0.5})[0]
}

// brightPassProbeBindGroup binds a one-texel source of the given grey level plus
// the bloom uniform that carries the threshold.
//
// The texel goes through the same eight-bit store the real high dynamic range
// target uses on this backend, so the value the fragment stage reads is the
// value a real frame would give it.
func brightPassProbeBindGroup(t *testing.T, threshold, grey float32) *BindGroup {
	t.Helper()
	device, _ := New(1, 1)
	tex, err := device.CreateTexture(gpu.TextureDesc{Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	owner, ok := tex.(*Texture)
	if !ok {
		t.Fatalf("CreateTexture returned %T, want *Texture", tex)
	}
	level := clampByte(grey)
	writeTextureRGBA(owner, 0, 0, 0, color.RGBA{R: level, G: level, B: level, A: 255})

	uniform, err := device.CreateBuffer(gpu.BufferDesc{Size: 16, Usage: gpu.BufferUsageUniform | gpu.BufferUsageCopyDst})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	params := make([]byte, 16)
	binary.LittleEndian.PutUint32(params[0:4], math.Float32bits(threshold))
	device.Queue().WriteBuffer(uniform, 0, params)

	return &BindGroup{desc: gpu.BindGroupDesc{Entries: []gpu.BindGroupEntry{
		{Binding: 0, TextureView: tex.CreateView()},
		{Binding: 2, Buffer: uniform, Size: 16},
	}}}
}
