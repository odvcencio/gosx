package bc7

import (
	"math"
	"math/rand"
	"testing"
)

// This file builds the test images and the measurement helpers.
//
// Every builder states its pixels as 8-bit codes in the target colour space and
// then converts to linear light, which is what Source holds. So the encoder
// converts the codes straight back and the measured error is error on the exact
// codes the test designed. Nothing hides in a conversion.

// codeImage builds a Source from 8-bit codes in one colour space.
func codeImage(width, height int, space ColorSpace, at func(x, y int) [4]uint8) Source {
	src := Source{Width: width, Height: height, Pix: make([]float32, width*height*4)}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px := at(x, y)
			linear := decodeTexel(space, px)
			i := (y*width + x) * 4
			copy(src.Pix[i:i+4], linear[:])
		}
	}
	return src
}

// testCase pairs one image with its colour space and a short name.
type testCase struct {
	name  string
	space ColorSpace
	src   Source
}

// buildTestImages returns the coverage set.
//
// Each entry targets one thing a BC7 mode exists for. The three-region image is
// the one that only a multi-subset mode can hold, and the uncorrelated-alpha
// image is the one that only a split-alpha mode can hold.
func buildTestImages() []testCase {
	const n = 32
	rng := rand.New(rand.NewSource(11))

	return []testCase{
		{"solid", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			return [4]uint8{200, 120, 40, 255}
		})},
		{"gradient", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			return [4]uint8{uint8(x * 255 / (n - 1)), uint8(y * 255 / (n - 1)), 128, 255}
		})},
		{"hardEdge", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			// The edge cuts through the middle of every block column, so no
			// block gets a clean single-colour fit.
			if (x+y)%8 < 4 {
				return [4]uint8{250, 10, 10, 255}
			}
			return [4]uint8{10, 10, 250, 255}
		})},
		{"threeRegions", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			// Three colour clusters inside every 4 by 4 block, each with its own
			// small ramp. The ramps matter: with three flat colours a two-subset
			// mode can put two of them at the ends of one line and reproduce the
			// block exactly, so the image would not test a three-subset mode at
			// all. A ramp inside each cluster removes that escape.
			ramp := uint8((x%4 + y%4) * 6)
			switch (x % 3) + (y % 3) {
			case 0, 1:
				return [4]uint8{240 - ramp, 20 + ramp, 20, 255}
			case 2, 3:
				return [4]uint8{20, 240 - ramp, 20 + ramp, 255}
			default:
				return [4]uint8{20 + ramp, 20, 240 - ramp, 255}
			}
		})},
		{"outlier", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			// One bright texel per block over a flat field. A bounding box fit
			// stretches the whole line to reach it and loses the flat part.
			if x%4 == 1 && y%4 == 2 {
				return [4]uint8{255, 255, 255, 255}
			}
			return [4]uint8{60, 70, 80, 255}
		})},
		{"alphaUncorrelated", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			// Colour runs along x and alpha runs along y, so the two share no
			// axis. A joint RGBA fit has to compromise; a split-alpha mode does
			// not.
			return [4]uint8{
				uint8(x * 255 / (n - 1)),
				uint8(255 - x*255/(n-1)),
				128,
				uint8(y * 255 / (n - 1)),
			}
		})},
		{"opaque", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			return [4]uint8{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255}
		})},
		{"transparent", SRGB, codeImage(n, n, SRGB, func(x, y int) [4]uint8 {
			return [4]uint8{uint8(x * 8 % 256), 100, 200, 0}
		})},
		{"normalMap", Linear, codeImage(n, n, Linear, func(x, y int) [4]uint8 {
			// A tangent-space normal map is data, not colour, so it must take
			// the Linear path. The vectors sweep a hemisphere.
			u := float64(x)/float64(n-1)*2 - 1
			v := float64(y)/float64(n-1)*2 - 1
			w := math.Sqrt(math.Max(0, 1-u*u-v*v))
			return [4]uint8{
				uint8((u*0.5 + 0.5) * 255),
				uint8((v*0.5 + 0.5) * 255),
				uint8((w*0.5 + 0.5) * 255),
				255,
			}
		})},
	}
}

// photoLike builds a larger image with the statistics of a base colour map:
// smooth areas, edges, and noise. The throughput benchmarks use it.
func photoLike(width, height int) Source {
	rng := rand.New(rand.NewSource(4242))
	return codeImage(width, height, SRGB, func(x, y int) [4]uint8 {
		fx := float64(x) / float64(width)
		fy := float64(y) / float64(height)
		base := 0.5 + 0.35*math.Sin(fx*9)*math.Cos(fy*7)
		edge := 0.0
		if int(fx*8)%2 == int(fy*8)%2 {
			edge = 0.18
		}
		noise := (rng.Float64() - 0.5) * 0.05
		clamp := func(v float64) uint8 {
			return uint8(clamp01(v) * 255)
		}
		return [4]uint8{
			clamp(base + edge + noise),
			clamp(base*0.8 + noise),
			clamp(1 - base + noise*2),
			255,
		}
	})
}

// measure encodes one image with one config and returns the peak signal-to-noise
// ratio of the result.
//
// The measurement decodes the packed blocks. It does not read the encoder's own
// error accounting, so a packing fault lowers the number instead of hiding.
func measure(src Source, space ColorSpace, cfg config) (float64, Stats) {
	cfg.space = space
	_, stats := encodeImage(src, cfg, 1)
	return stats.PSNR(), stats
}

// baseConfig returns the resolved config for one quality level. It panics on a
// bad level, which only a test typo can cause.
func baseConfig(t *testing.T, space ColorSpace, q Quality) config {
	t.Helper()
	cfg, err := resolve(Options{Space: space, Quality: q})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return cfg
}

// TestCodeRoundTripIsExact proves the transfer function pair loses nothing.
//
// Every 8-bit code must survive the trip to linear light and back. If it did not,
// every measurement in this file would carry a conversion error that has nothing
// to do with BC7.
func TestCodeRoundTripIsExact(t *testing.T) {
	for code := 0; code < 256; code++ {
		linear := srgbDecodeLUT[code]
		if got := linearToSRGB8(linear); int(got) != code {
			t.Errorf("sRGB code %d round-tripped to %d", code, got)
		}
		if got := linearToUnorm8(float32(code) / 255); int(got) != code {
			t.Errorf("unorm code %d round-tripped to %d", code, got)
		}
	}
}
