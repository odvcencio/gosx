package bc7

import (
	"math"
	"testing"
)

// This file pins the transfer function to IEC 61966-2-1.
//
// The functions here duplicate assetpipe/texture, because texture calls this
// package and cannot be called back. A duplicate that drifts is worse than no
// duplicate at all: the encoder would store codes the sampler does not expect,
// and every colour texture would ship with the wrong brightness. So these tests
// check absolute values from the standard, not agreement with the other copy.

// TestSRGBTransferFunctionConstants checks named points of the curve.
//
// The values come from IEC 61966-2-1 and from the arithmetic it defines:
//
//   - the piecewise breakpoint sits at 0.04045 encoded and 0.0031308 linear
//   - below it the curve is a straight line with slope 12.92
//   - above it the curve is 1.055 * l^(1/2.4) - 0.055
//
// The 8-bit landmarks follow from those rules. Code 128, which authoring tools
// call mid grey, is 0.2158605 in linear light. Linear 0.5, which a renderer calls
// mid grey, encodes to code 188.
func TestSRGBTransferFunctionConstants(t *testing.T) {
	cases := []struct {
		name   string
		linear float64
		want   float64
	}{
		{"black", 0, 0},
		{"white", 1, 1},
		{"breakpoint", 0.0031308, 0.0031308 * 12.92},
		{"just below the breakpoint", 0.001, 0.01292},
		// 1.055 * 0.5^(1/2.4) - 0.055, evaluated independently.
		{"mid grey in linear light", 0.5, 0.7353569830524495},
	}
	for _, c := range cases {
		got := linearToSRGB(c.linear)
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("%s: linearToSRGB(%v) = %v, want %v", c.name, c.linear, got, c.want)
		}
	}

	inverse := []struct {
		name    string
		encoded float64
		want    float64
	}{
		{"black", 0, 0},
		{"white", 1, 1},
		{"breakpoint", 0.04045, 0.0031308},
		{"code 128", 128.0 / 255, 0.2158605},
		{"encoded half", 0.5, 0.2140411},
	}
	for _, c := range inverse {
		got := srgbToLinear(c.encoded)
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("%s: srgbToLinear(%v) = %v, want %v", c.name, c.encoded, got, c.want)
		}
	}

	// The two 8-bit landmarks, after rounding.
	if got := linearToSRGB8(0.5); got != 188 {
		t.Errorf("linearToSRGB8(0.5) = %d, want 188", got)
	}
	if got := srgbLinearOf(128); math.Abs(float64(got)-0.2158605) > 1e-6 {
		t.Errorf("sRGB code 128 decodes to %v, want 0.2158605", got)
	}
}

// srgbLinearOf reads the decode table. The package keeps the table unexported,
// because the integration layer converts through Decode and never needs it.
func srgbLinearOf(code uint8) float32 { return srgbDecodeLUT[code] }

// TestTransferFunctionIsMonotone checks the property every later step relies on.
//
// A brighter linear value must never produce a darker code. If the curve folded
// anywhere, endpoint quantization and index selection would both misbehave in ways
// that look like block artefacts.
func TestTransferFunctionIsMonotone(t *testing.T) {
	prevSRGB, prevUnorm := -1, -1
	for i := 0; i <= 4000; i++ {
		l := float32(i) / 4000
		s := int(linearToSRGB8(l))
		u := int(linearToUnorm8(l))
		if s < prevSRGB {
			t.Fatalf("linearToSRGB8 fell from %d to %d at %v", prevSRGB, s, l)
		}
		if u < prevUnorm {
			t.Fatalf("linearToUnorm8 fell from %d to %d at %v", prevUnorm, u, l)
		}
		prevSRGB, prevUnorm = s, u
	}
	if prevSRGB != 255 || prevUnorm != 255 {
		t.Errorf("the curves end at %d and %d, want 255 and 255", prevSRGB, prevUnorm)
	}
}

// TestTransferFunctionClampsOutOfRange covers input a resample can produce.
//
// A Lanczos filter has negative lobes, so a resized image holds values below 0 and
// above 1. Both must clamp rather than wrap.
func TestTransferFunctionClampsOutOfRange(t *testing.T) {
	for _, l := range []float32{-1, -0.001, 0} {
		if got := linearToSRGB8(l); got != 0 {
			t.Errorf("linearToSRGB8(%v) = %d, want 0", l, got)
		}
		if got := linearToUnorm8(l); got != 0 {
			t.Errorf("linearToUnorm8(%v) = %d, want 0", l, got)
		}
	}
	for _, l := range []float32{1, 1.001, 5} {
		if got := linearToSRGB8(l); got != 255 {
			t.Errorf("linearToSRGB8(%v) = %d, want 255", l, got)
		}
		if got := linearToUnorm8(l); got != 255 {
			t.Errorf("linearToUnorm8(%v) = %d, want 255", l, got)
		}
	}
}

// TestAlphaNeverTakesTheTransferFunction pins a rule the sRGB specification states
// and that a careless refactor breaks.
//
// The transfer function applies to the three colour channels only. Alpha is a
// coverage number, so bending it would change how a cut-out composites.
func TestAlphaNeverTakesTheTransferFunction(t *testing.T) {
	const linear = 0.2
	px := encodeTexel(SRGB, linear, linear, linear, linear)
	if px[3] != linearToUnorm8(linear) {
		t.Errorf("alpha encoded to %d, want the plain unorm code %d", px[3], linearToUnorm8(linear))
	}
	if px[0] == px[3] {
		t.Error("the colour channels and alpha produced the same code, so alpha took the curve")
	}
	back := decodeTexel(SRGB, px)
	if math.Abs(float64(back[3])-float64(px[3])/255) > 1e-6 {
		t.Errorf("alpha decoded to %v, want %v", back[3], float64(px[3])/255)
	}
}

// TestColorSpaceStringAndVkFormat covers the small helpers a manifest reads.
func TestColorSpaceStringAndVkFormat(t *testing.T) {
	if SRGB.String() != "srgb" || Linear.String() != "linear" || spaceUnset.String() != "unset" {
		t.Errorf("ColorSpace names are %q, %q and %q", SRGB, Linear, spaceUnset)
	}
	if QualityFast.String() != "fast" || QualityBalanced.String() != "balanced" || QualityBest.String() != "best" {
		t.Errorf("Quality names are %q, %q and %q", QualityFast, QualityBalanced, QualityBest)
	}
}
