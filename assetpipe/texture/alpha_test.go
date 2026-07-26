package texture

import (
	"math"
	"testing"
)

func TestAnalyzeAlphaClassifies(t *testing.T) {
	opaque := constantImage(4, 4, 1)
	stats := AnalyzeAlpha(opaque)
	if !stats.Constant || !stats.Opaque || !stats.Binary || stats.Mode() != "opaque" {
		t.Fatalf("an all-opaque image classified as %+v mode %q", stats, stats.Mode())
	}

	cut := NewImage(4, 1)
	cut.Set(0, 0, 1, 1, 1, 1)
	cut.Set(1, 0, 1, 1, 1, 0)
	cut.Set(2, 0, 1, 1, 1, 1)
	cut.Set(3, 0, 1, 1, 1, 0)
	stats = AnalyzeAlpha(cut)
	if stats.Constant || stats.Opaque || !stats.Binary || stats.Mode() != "mask" {
		t.Fatalf("a cutout image classified as %+v mode %q", stats, stats.Mode())
	}

	blend := NewImage(2, 1)
	blend.Set(0, 0, 1, 1, 1, 1)
	blend.Set(1, 0, 1, 1, 1, 0.5)
	stats = AnalyzeAlpha(blend)
	if stats.Binary || stats.Mode() != "blend" {
		t.Fatalf("a blended image classified as %+v mode %q", stats, stats.Mode())
	}
	if math.Abs(float64(stats.Min)-0.5) > 1e-6 || stats.Max != 1 {
		t.Fatalf("alpha range %g to %g, want 0.5 to 1", stats.Min, stats.Max)
	}
}

func TestIsGrayscale(t *testing.T) {
	gray := NewImage(2, 1)
	gray.Set(0, 0, 0.25, 0.25, 0.25, 1)
	gray.Set(1, 0, 0.75, 0.75, 0.75, 1)
	if !IsGrayscale(gray) {
		t.Fatal("three equal channels must read as grayscale")
	}
	colour := gray.Clone()
	colour.Set(1, 0, 0.75, 0.7, 0.75, 1)
	if IsGrayscale(colour) {
		t.Fatal("a green shift of 0.05 must break the grayscale test")
	}
}

// TestPremultiplyRoundTrip checks that the pair is an exact inverse where alpha
// allows it, and documents the one case it cannot recover.
func TestPremultiplyRoundTrip(t *testing.T) {
	src := NewImage(3, 1)
	src.Set(0, 0, 0.8, 0.4, 0.2, 1)
	src.Set(1, 0, 0.8, 0.4, 0.2, 0.5)
	src.Set(2, 0, 0.8, 0.4, 0.2, 0) // Invisible: colour is unrecoverable.

	img := src.Clone()
	Premultiply(img)
	if img.Alpha != AlphaPremultiplied {
		t.Fatal("Premultiply must record the new mode")
	}
	r, _, _, _ := img.At(1, 0)
	if math.Abs(float64(r)-0.4) > 1e-6 {
		t.Fatalf("premultiplied red is %g, want 0.4", r)
	}
	Unpremultiply(img)
	if img.Alpha != AlphaStraight {
		t.Fatal("Unpremultiply must record the new mode")
	}
	for pixel := 0; pixel < 2; pixel++ {
		for c := 0; c < 4; c++ {
			got, want := img.Pix[pixel*4+c], src.Pix[pixel*4+c]
			if math.Abs(float64(got-want)) > 1e-6 {
				t.Fatalf("pixel %d channel %d round tripped to %g, want %g", pixel, c, got, want)
			}
		}
	}
	// A zero-alpha texel keeps whatever the premultiply left, which is zero.
	// That is correct: the colour was never visible and cannot be restored.
	r, _, _, a := img.At(2, 0)
	if a != 0 || r != 0 {
		t.Fatalf("zero-alpha texel came back as red %g alpha %g, want 0 and 0", r, a)
	}
	// Calling either function twice must do nothing.
	before := img.Clone()
	Unpremultiply(img)
	for i := range img.Pix {
		if img.Pix[i] != before.Pix[i] {
			t.Fatal("Unpremultiply is not idempotent")
		}
	}
}
