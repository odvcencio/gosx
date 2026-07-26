package texture

import (
	"math"
	"testing"
)

// TestSRGBRoundTripIsExact checks the transfer function pair against its own
// inverse over the whole 8-bit domain.
//
// The domain has 256 members, so "bounded error" can be the strongest possible
// bound: zero. A single code that moves would mean every texture the pipeline
// writes drifts one step per build.
func TestSRGBRoundTripIsExact(t *testing.T) {
	for code := 0; code < 256; code++ {
		linear := SRGBToLinear(uint8(code))
		back := LinearToSRGB8(linear)
		if int(back) != code {
			t.Fatalf("sRGB round trip moved code %d to %d (linear %g)", code, back, linear)
		}
	}
}

// TestSRGBMatchesSpecificationSamples checks the decode against values computed
// from IEC 61966-2-1 by hand, not against the package's own table.
func TestSRGBMatchesSpecificationSamples(t *testing.T) {
	cases := []struct {
		code uint8
		want float64
	}{
		{0, 0},
		{1, (1.0 / 255) / 12.92},       // Linear segment.
		{10, (10.0 / 255) / 12.92},     // Still linear: 0.0392 < 0.04045.
		{11, powSegment(11.0 / 255)},   // First code in the power segment.
		{128, powSegment(128.0 / 255)}, // Mid grey: about 0.2158.
		{187, powSegment(187.0 / 255)}, // Just below linear 0.5.
		{255, 1},                       //
	}
	for _, tc := range cases {
		got := float64(SRGBToLinear(tc.code))
		if math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("SRGBToLinear(%d) = %g, want %g", tc.code, got, tc.want)
		}
	}
}

func powSegment(c float64) float64 { return math.Pow((c+0.055)/1.055, 2.4) }

// TestLinearMidGreyEncodesTo188 pins the number the mip tests depend on.
//
// Linear 0.5 is sRGB code 188, not 128. Every "my mips got darker" bug is this
// number: a filter that averages code values lands near 128 and loses about
// forty percent of the light.
func TestLinearMidGreyEncodesTo188(t *testing.T) {
	if got := LinearToSRGB8(0.5); got != 188 {
		t.Fatalf("LinearToSRGB8(0.5) = %d, want 188", got)
	}
}

// TestLinearToUnorm8SkipsTheTransfer checks that a data channel stays linear.
func TestLinearToUnorm8SkipsTheTransfer(t *testing.T) {
	if got := LinearToUnorm8(0.5); got != 128 {
		t.Fatalf("LinearToUnorm8(0.5) = %d, want 128", got)
	}
	if got := LinearToUnorm8(-1); got != 0 {
		t.Fatalf("LinearToUnorm8(-1) = %d, want a clamp to 0", got)
	}
	if got := LinearToUnorm8(2); got != 255 {
		t.Fatalf("LinearToUnorm8(2) = %d, want a clamp to 255", got)
	}
}
