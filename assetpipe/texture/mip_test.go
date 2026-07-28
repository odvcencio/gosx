package texture

import (
	"math"
	"testing"
)

// linearMean returns the mean of one channel over a whole image.
func linearMean(img *Image, channel int) float64 {
	var total float64
	for pixel := 0; pixel < img.Pixels(); pixel++ {
		total += float64(img.Pix[pixel*4+channel])
	}
	return total / float64(img.Pixels())
}

// TestMipLevelCount pins the chain length against the KTX2 rule: halve each
// edge, floor at one, and stop when both edges are one.
func TestMipLevelCount(t *testing.T) {
	cases := []struct{ w, h, want int }{
		{1, 1, 1}, {2, 2, 2}, {4, 4, 3}, {256, 256, 9},
		{2048, 2048, 12}, {8, 1, 4}, {1024, 512, 11},
	}
	for _, tc := range cases {
		if got := MipLevelCount(tc.w, tc.h); got != tc.want {
			t.Errorf("MipLevelCount(%d, %d) = %d, want %d", tc.w, tc.h, got, tc.want)
		}
	}
}

// TestMipChainKeepsAConstantConstant is the first thing to check. A constant
// image must survive every level of every filter untouched.
func TestMipChainKeepsAConstantConstant(t *testing.T) {
	const value = 0.42
	for _, filter := range []Filter{Lanczos3, Mitchell, Triangle, Box} {
		chain, err := MipChain(constantImage(64, 64, value), MipOptions{Filter: filter})
		if err != nil {
			t.Fatal(err)
		}
		if len(chain) != 7 {
			t.Fatalf("%s built %d levels, want 7", filter, len(chain))
		}
		for level, mip := range chain {
			for i, v := range mip.Pix {
				if math.Abs(float64(v-value)) > 1e-6 {
					t.Fatalf("%s level %d index %d holds %g, want %g", filter, level, i, v, value)
				}
			}
		}
	}
}

// TestMipChainKeepsLinearMean is the classic sRGB mipmapping check.
//
// The source is a one-texel checkerboard of sRGB black and sRGB white. Half the
// texels are linear 0 and half are linear 1, so the correct average light is
// linear 0.5, which encodes back to sRGB code 188.
//
// A pipeline that averages sRGB code values instead lands near code 128, which
// is linear 0.216. That is a loss of 57 percent of the light in the first mip
// level alone, and it compounds down the chain. Every "why does my texture go
// dark in the distance" report is this bug.
//
// The test asserts both directions: the answer is 188, and the answer is NOT
// anywhere near 128.
func TestMipChainKeepsLinearMean(t *testing.T) {
	const size = 64
	src := NewImage(size, size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Decode through the real transfer function, exactly as Decode
			// would for a black-and-white checkerboard PNG.
			code := uint8(0)
			if (x+y)%2 == 0 {
				code = 255
			}
			v := SRGBToLinear(code)
			src.Set(x, y, v, v, v, 1)
		}
	}

	chain, err := MipChain(src, MipOptions{Filter: Box})
	if err != nil {
		t.Fatal(err)
	}
	last := chain[len(chain)-1]
	if last.Width != 1 || last.Height != 1 {
		t.Fatalf("last level is %dx%d, want 1x1", last.Width, last.Height)
	}
	mean, _, _, _ := last.At(0, 0)
	if math.Abs(float64(mean)-0.5) > 1e-5 {
		t.Fatalf("the 1x1 level holds linear %g, want the exact mean 0.5", mean)
	}
	code := LinearToSRGB8(mean)
	if code != 188 {
		t.Fatalf("the 1x1 level encodes to sRGB %d, want 188 (linear 0.5)", code)
	}
	if code >= 120 && code <= 140 {
		t.Fatalf("sRGB code %d means the chain averaged code values, not light", code)
	}

	// Every level must hold the same light, not just the last one.
	base := linearMean(chain[0], 0)
	for level, mip := range chain {
		got := linearMean(mip, 0)
		if math.Abs(got-base) > 1e-5 {
			t.Errorf("level %d mean is %g, level 0 mean is %g", level, got, base)
		}
	}
}

// TestMipChainMeanBoundPerFilter states the mean-preservation bound of each
// kernel, measured rather than assumed.
//
// Box reproduces the exact mean. The other three carry negative lobes or a
// narrower footprint, so they drift a little. The bounds below are the honest
// numbers for this input, and they exist so a kernel change cannot quietly get
// worse.
func TestMipChainMeanBoundPerFilter(t *testing.T) {
	src := rampImage(64, 64)
	want := linearMean(src, 0)
	bounds := map[Filter]float64{
		Box:      1e-6,
		Triangle: 1e-3,
		Mitchell: 1e-3,
		Lanczos3: 2e-3,
	}
	for filter, bound := range bounds {
		chain, err := MipChain(src, MipOptions{Filter: filter})
		if err != nil {
			t.Fatal(err)
		}
		got, _, _, _ := chain[len(chain)-1].At(0, 0)
		if diff := math.Abs(float64(got) - want); diff > bound {
			t.Errorf("%s 1x1 level drifted %g from the mean %g, bound is %g", filter, diff, want, bound)
		}
	}
}

// TestMipChainPremultipliesAcrossAlphaEdge checks the alpha-aware path.
//
// The source is one opaque red texel in a 4x4 field of fully transparent green.
// Those green texels are invisible, so the only colour in the image is red.
//
// Filtering straight alpha averages the invisible green into the result and
// produces a mostly green texel, which is the halo every alpha-cut foliage
// texture shows when a pipeline gets this wrong. Filtering premultiplied values
// and dividing alpha back out gives pure red with one sixteenth alpha.
func TestMipChainPremultipliesAcrossAlphaEdge(t *testing.T) {
	src := NewImage(4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, 0, 1, 0, 0) // Invisible green.
		}
	}
	src.Set(0, 0, 1, 0, 0, 1) // One opaque red texel.

	chain, err := MipChain(src, MipOptions{Filter: Box, AlphaAware: true})
	if err != nil {
		t.Fatal(err)
	}
	last := chain[len(chain)-1]
	r, g, b, a := last.At(0, 0)
	if math.Abs(float64(a)-1.0/16) > 1e-6 {
		t.Fatalf("alpha is %g, want 1/16", a)
	}
	if math.Abs(float64(r)-1) > 1e-5 || g > 1e-5 || b > 1e-5 {
		t.Fatalf("colour is (%g, %g, %g), want pure red; the invisible green bled in", r, g, b)
	}

	// Without the alpha-aware path the green does bleed. Assert that too, so
	// the test proves the flag is what fixes it and not something else.
	naive, err := MipChain(src, MipOptions{Filter: Box, AlphaAware: false})
	if err != nil {
		t.Fatal(err)
	}
	nr, ng, _, _ := naive[len(naive)-1].At(0, 0)
	if ng <= nr {
		t.Fatalf("the straight-alpha chain should be dominated by green, got red %g green %g", nr, ng)
	}
}

// TestMipChainRestoresStraightAlpha checks that the alpha-aware path hands back
// straight alpha, which is what glTF and every KTX2 consumer expect.
func TestMipChainRestoresStraightAlpha(t *testing.T) {
	src := NewImage(8, 8)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, 1, 1, 1, float32(x)/7)
		}
	}
	chain, err := MipChain(src, MipOptions{Filter: Box, AlphaAware: true})
	if err != nil {
		t.Fatal(err)
	}
	for level, mip := range chain {
		if mip.Alpha != AlphaStraight {
			t.Fatalf("level %d reports %s alpha", level, mip.Alpha)
		}
		for pixel := 0; pixel < mip.Pixels(); pixel++ {
			if a := mip.Pix[pixel*4+3]; a > 0 && mip.Pix[pixel*4] < 0.99 {
				t.Fatalf("level %d pixel %d kept the alpha factor in white: %g at alpha %g",
					level, pixel, mip.Pix[pixel*4], a)
			}
		}
	}
}

// TestMipChainLevelsCap checks the Levels option.
func TestMipChainLevelsCap(t *testing.T) {
	chain, err := MipChain(constantImage(64, 64, 1), MipOptions{Levels: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 3 {
		t.Fatalf("built %d levels, want 3", len(chain))
	}
	if chain[2].Width != 16 || chain[2].Height != 16 {
		t.Fatalf("level 2 is %dx%d, want 16x16", chain[2].Width, chain[2].Height)
	}
}
