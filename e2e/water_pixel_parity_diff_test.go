//go:build e2e

package e2e

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/orisano/pixelmatch"
)

type pixelDiffStats struct {
	width, height int
	differing     int // pixelmatch's perceptual differing-pixel count
	maxDelta      int // max |channel_a - channel_b| over every pixel/channel (R,G,B,A), strict
	deltaPixels   int // count of pixels with ANY channel delta > 1 (beyond FP-reassociation noise)
}

// pixelDiffPNGBytes decodes two PNGs of identical dimensions and reports both
// pixelmatch's perceptual diff count and a strict per-channel max delta / an
// over-threshold pixel count, so a caller can distinguish "bit-identical",
// "a handful of +/-1 LSB reassociation pixels", and "materially different".
func pixelDiffPNGBytes(t *testing.T, a, b []byte) pixelDiffStats {
	t.Helper()
	imgA, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatalf("decode image A: %v", err)
	}
	imgB, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode image B: %v", err)
	}
	return pixelDiffImages(t, imgA, imgB)
}

func pixelDiffImages(t *testing.T, imgA, imgB image.Image) pixelDiffStats {
	t.Helper()
	boundsA := imgA.Bounds()
	boundsB := imgB.Bounds()
	if boundsA != boundsB {
		t.Fatalf("image size mismatch: A=%v B=%v", boundsA, boundsB)
	}

	differing, err := pixelmatch.MatchPixel(imgA, imgB)
	if err != nil {
		t.Fatalf("pixelmatch: %v", err)
	}

	maxDelta := 0
	deltaPixels := 0
	absDiff := func(x, y int32) int {
		d := int(x) - int(y)
		if d < 0 {
			d = -d
		}
		return d
	}
	for y := boundsA.Min.Y; y < boundsA.Max.Y; y++ {
		for x := boundsA.Min.X; x < boundsA.Max.X; x++ {
			ar, ag, ab, aa := imgA.At(x, y).RGBA()
			br, bg, bb, ba := imgB.At(x, y).RGBA()
			// RGBA() returns 16-bit-per-channel values; downshift to 8-bit
			// (>>8) so the reported delta matches the PNG's actual 8-bit
			// channel precision (a 257-scaled 16-bit delta of 1 is a 0 at
			// 8-bit, i.e. genuinely identical output).
			dr := absDiff(int32(ar>>8), int32(br>>8))
			dg := absDiff(int32(ag>>8), int32(bg>>8))
			db := absDiff(int32(ab>>8), int32(bb>>8))
			da := absDiff(int32(aa>>8), int32(ba>>8))
			m := dr
			if dg > m {
				m = dg
			}
			if db > m {
				m = db
			}
			if da > m {
				m = da
			}
			if m > maxDelta {
				maxDelta = m
			}
			if m > 1 {
				deltaPixels++
			}
		}
	}

	return pixelDiffStats{
		width:       boundsA.Dx(),
		height:      boundsA.Dy(),
		differing:   differing,
		maxDelta:    maxDelta,
		deltaPixels: deltaPixels,
	}
}
