package bcn

import (
	"math"
	"testing"
)

// hillClimb finds a strong endpoint pair by brute force.
//
// The function is a reference, not an encoder. It restarts from many random
// endpoint pairs, then walks every one-step change of the six endpoint channels
// until nothing improves. It costs thousands of times more than the encoder, so
// it lives in a test and measures how much the encoder leaves on the table.
func hillClimb(texels *[16]vec3, mask uint16, three bool, restarts int, seed uint32) colorFit {
	state := seed
	next := func() uint32 {
		state = state*1664525 + 1013904223
		return state >> 8
	}
	best := colorFit{sse: math.Inf(1)}
	starts := make([][2]uint16, 0, restarts+2)
	mean, lo, hi, _ := blockStats(texels, mask)
	starts = append(starts, [2]uint16{quantize565(lo), quantize565(hi)})
	starts = append(starts, [2]uint16{quantize565(mean), quantize565(mean)})
	for i := 0; i < restarts; i++ {
		starts = append(starts, [2]uint16{uint16(next() & 0xFFFF), uint16(next() & 0xFFFF)})
	}
	for _, start := range starts {
		current := evalPair(texels, mask, start[0], start[1], three)
		for {
			improved := false
			for _, shift := range []uint{11, 5, 0} {
				width := uint16(0x1F)
				if shift == 5 {
					width = 0x3F
				}
				for _, delta := range []int{-1, 1} {
					for endpoint := 0; endpoint < 2; endpoint++ {
						a, b := current.a, current.b
						target := &a
						if endpoint == 1 {
							target = &b
						}
						value := int((*target>>shift)&width) + delta
						if value < 0 || value > int(width) {
							continue
						}
						*target = (*target & ^(width << shift)) | uint16(value)<<shift
						candidate := evalPair(texels, mask, a, b, three)
						if candidate.sse < current.sse {
							current = candidate
							improved = true
						}
					}
				}
			}
			if !improved {
				break
			}
		}
		if current.sse < best.sse {
			best = current
		}
	}
	return best
}

// TestBC1ApproachesLocalOptimum measures the gap between the encoder and a search
// that costs thousands of times more.
//
// The check is independent of how the encoder searches. A bug that broke the
// cluster fit or the refinement would widen the gap, and a bug that made the
// encoder agree with itself would not hide from this.
//
// The reported number is the mean squared-error ratio over the blocks of the
// hardest test images. A ratio of 1.00 means the encoder matched the reference
// search on every block.
func TestBC1ApproachesLocalOptimum(t *testing.T) {
	for _, image := range colourImages() {
		t.Run(image.name, func(t *testing.T) {
			tuning := bc1TuningFor(QualityHigh)
			across, down := BlocksAcross(image.surface.Width), BlocksAcross(image.surface.Height)
			var encoderTotal, referenceTotal float64
			worst := 1.0
			blocks := 0
			for by := 0; by < down; by += 3 {
				for bx := 0; bx < across; bx += 3 {
					texels, mask := gatherColor(image.surface, bx, by, image.transfer, false, 0)
					var payload [8]byte
					encodeColorBlock(&texels, mask, tuning, false, payload[:])
					encoded := scoreBlockPayload(t, &texels, payload[:])

					reference := hillClimb(&texels, mask, false, 24, 0x9E3779B9)
					threeColour := hillClimb(&texels, mask, true, 24, 0x85EBCA77)
					best := reference.sse
					if threeColour.sse < best {
						best = threeColour.sse
					}

					encoderTotal += encoded
					referenceTotal += best
					if best > 0 {
						if ratio := encoded / best; ratio > worst {
							worst = ratio
						}
					}
					blocks++
				}
			}
			ratio := 1.0
			if referenceTotal > 0 {
				ratio = encoderTotal / referenceTotal
			}
			t.Logf("%d blocks: encoder error is %.3f times the reference search, worst block %.3f",
				blocks, ratio, worst)
			if ratio > 1.05 {
				t.Errorf("encoder squared error is %.3f times the reference search, want at most 1.05", ratio)
			}
		})
	}
}

// scoreBlockPayload decodes one encoded block and returns its squared error
// against the source texels. It goes through the package decoder on purpose, so
// the score covers the bit packing as well as the fit.
func scoreBlockPayload(t *testing.T, texels *[16]vec3, payload []byte) float64 {
	t.Helper()
	decoded, err := DecodeBlockBC1(payload)
	if err != nil {
		t.Fatalf("DecodeBlockBC1: %v", err)
	}
	total := 0.0
	for i, got := range decoded {
		total += texels[i].squaredDistance(vec3{float64(got.R), float64(got.G), float64(got.B)})
	}
	return total
}
