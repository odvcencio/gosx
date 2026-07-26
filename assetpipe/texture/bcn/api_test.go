package bcn

import "testing"

// TestFormatDescriptions checks the small helpers the integration layer reads to
// size a payload and to name a format in a manifest.
func TestFormatDescriptions(t *testing.T) {
	cases := []struct {
		format   Format
		name     string
		channels int
	}{
		{FormatBC1RGB, "bc1-rgb", 3},
		{FormatBC1RGBA, "bc1-rgba", 4},
		{FormatBC3, "bc3", 4},
		{FormatBC4, "bc4", 1},
		{FormatBC5, "bc5", 2},
		{FormatUnknown, "unknown", 0},
		{Format(99), "unknown", 0},
	}
	for _, tc := range cases {
		if got := tc.format.String(); got != tc.name {
			t.Errorf("format %d is named %q, want %q", int(tc.format), got, tc.name)
		}
		if got := tc.format.Channels(); got != tc.channels {
			t.Errorf("%s stores %d channels, want %d", tc.name, got, tc.channels)
		}
	}
}

// TestAlphaModePicksTheFormat checks the mapping the caller uses to name the
// VkFormat that matches its BC1 payload.
func TestAlphaModePicksTheFormat(t *testing.T) {
	if got := BC1Opaque.Format(); got != FormatBC1RGB {
		t.Errorf("BC1Opaque names %s, want %s", got, FormatBC1RGB)
	}
	if got := BC1Cutout.Format(); got != FormatBC1RGBA {
		t.Errorf("BC1Cutout names %s, want %s", got, FormatBC1RGBA)
	}
}

// TestEnumNames checks the strings a manifest or a metric records.
func TestEnumNames(t *testing.T) {
	if got := TransferSRGB.String(); got != "srgb" {
		t.Errorf("TransferSRGB is named %q", got)
	}
	if got := TransferUnorm.String(); got != "unorm" {
		t.Errorf("TransferUnorm is named %q", got)
	}
	if got := TransferUnspecified.String(); got != "unspecified" {
		t.Errorf("TransferUnspecified is named %q", got)
	}
	if got := QualityFast.String(); got != "fast" {
		t.Errorf("QualityFast is named %q", got)
	}
	// The zero value means QualityHigh, because an asset pipeline runs once and
	// the texture ships forever.
	if got := QualityDefault.String(); got != "high" {
		t.Errorf("QualityDefault is named %q, want high", got)
	}
	if bc1TuningFor(QualityDefault) != bc1TuningFor(QualityHigh) {
		t.Error("QualityDefault must tune the same as QualityHigh")
	}
	if bc4TuningFor(QualityDefault) != bc4TuningFor(QualityHigh) {
		t.Error("QualityDefault must tune the same as QualityHigh for BC4")
	}
}

// TestAlphaCutoffMovesTheThreshold checks the option the caller sets to move the
// cutout edge.
func TestAlphaCutoffMovesTheThreshold(t *testing.T) {
	// One block whose alpha rises across the row, so a higher cutoff cuts more
	// texels.
	surface := srgbSurface(4, 4, func(x, y int) RGBA8 {
		return RGBA8{R: 200, G: 100, B: 50, A: uint8(x * 80)}
	})
	count := func(cutoff float32) int {
		payload, err := EncodeBC1(surface, BC1Options{
			Transfer: TransferSRGB, Alpha: BC1Cutout, AlphaCutoff: cutoff,
		})
		if err != nil {
			t.Fatalf("EncodeBC1: %v", err)
		}
		texels, err := DecodeBlockBC1(payload)
		if err != nil {
			t.Fatalf("DecodeBlockBC1: %v", err)
		}
		cut := 0
		for _, texel := range texels {
			if texel.A == 0 {
				cut++
			}
		}
		return cut
	}
	low := count(0.1)
	high := count(0.9)
	t.Logf("cutoff 0.1 cuts %d texels, cutoff 0.9 cuts %d", low, high)
	if high <= low {
		t.Errorf("a higher cutoff must cut more texels, got %d then %d", low, high)
	}
	// The default cutoff is 0.5, so it must sit between the two.
	middle := count(0)
	if middle < low || middle > high {
		t.Errorf("the default cutoff cut %d texels, want between %d and %d", middle, low, high)
	}
}

// TestSurfaceAccessors checks the two helpers a caller uses to build a surface by
// hand.
func TestSurfaceAccessors(t *testing.T) {
	s := NewSurface(2, 2)
	s.Set(1, 1, 0.25, 0.5, 0.75, 1)
	r, g, b, a := s.At(1, 1)
	if r != 0.25 || g != 0.5 || b != 0.75 || a != 1 {
		t.Fatalf("got (%g, %g, %g, %g)", r, g, b, a)
	}
	if empty := NewSurface(0, 4); empty.Width != 0 || empty.Pix != nil {
		t.Error("NewSurface must return an empty surface for a zero size")
	}
}
