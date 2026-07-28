package bcn

import (
	"fmt"
	"math"
)

// BC4Options controls the single-channel encoder.
type BC4Options struct {
	// Transfer must be TransferUnorm. BC4 has no sRGB VkFormat, so a colour
	// texture cannot use it. Naming the value keeps the caller honest.
	Transfer Transfer
	// Quality trades encode time for image quality.
	Quality Quality
	// Channel picks the source channel. The zero value picks red.
	Channel Channel
	// Workers sets the goroutine count. Zero or one runs on the calling
	// goroutine, and a negative value asks for one worker for each
	// processor. The output never depends on this field.
	Workers int
}

// bc4Weights8 maps each palette index of the eight-value mode to its position
// between the low and the high endpoint. Index 0 holds the high endpoint,
// because that mode stores the larger value first.
var bc4Weights8 = [8]float64{1, 0, 6.0 / 7, 5.0 / 7, 4.0 / 7, 3.0 / 7, 2.0 / 7, 1.0 / 7}

// bc4Weights6 maps each palette index of the six-value mode. The last two
// entries hold the constants 0 and 255, which no endpoint controls, so they
// carry the weight -1 and drop out of the least-squares solve.
var bc4Weights6 = [8]float64{0, 1, 1.0 / 5, 2.0 / 5, 3.0 / 5, 4.0 / 5, -1, -1}

// bc4Fit holds one candidate block encoding.
type bc4Fit struct {
	sse    float64
	e0, e1 uint8
	bits   uint64
}

// bc4Tuning switches the parts of the search on and off. The tests use it to
// measure what each part buys and to build deliberately weaker encoders.
type bc4Tuning struct {
	// refineIters caps the least-squares rounds. Zero keeps the initial
	// endpoints, which is the plain bounding box.
	refineIters int
	// jitter searches every endpoint pair within this many codes of the
	// refined pair.
	jitter int
	// polishSweeps caps how often the jitter search repeats from its own
	// winner. Zero disables the search whatever jitter says.
	polishSweeps int
	// sixValue also tries the mode that spends two entries on 0 and 255.
	sixValue bool
	// interiorStart adds a second six-value candidate that ignores the
	// texels already at 0 or 255.
	interiorStart bool
}

// bc4TuningFor returns the tuning of one quality level.
//
// The counts come from measurement.
//
//   - The refinement cap is four. TestBC4RefinementIterations reports 0.96 dB from
//     round one, 0.27 dB from round two, 0.12 dB from round three, and 0.01 dB or
//     less from round four. Rounds five to eight move no image by more than
//     0.008 dB.
//   - The jitter walk repeats at most four times and stops early.
//     TestBC4QualityLadder reports 0.10 to 0.16 dB from the first pass and up to
//     0.32 dB from the repeats.
func bc4TuningFor(q Quality) bc4Tuning {
	if q == QualityFast {
		return bc4Tuning{refineIters: 1, jitter: 0, sixValue: true}
	}
	return bc4Tuning{refineIters: 4, jitter: 1, polishSweeps: 4, sixValue: true, interiorStart: true}
}

// EncodeBC4 compresses one channel of s as BC4, eight bytes for each 4x4 block.
//
// The blocks run left to right and then top to bottom. A size that is not a
// multiple of four repeats the last row or column into the padding, so the real
// texels keep control of the fit.
func EncodeBC4(s *Surface, opts BC4Options) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if opts.Transfer != TransferUnorm {
		return nil, fmt.Errorf("%w: BC4 has no sRGB VkFormat, so it needs TransferUnorm, got %s",
			ErrTransfer, opts.Transfer)
	}
	if opts.Channel < ChannelR || opts.Channel > ChannelA {
		return nil, fmt.Errorf("%w: %d", ErrChannel, int(opts.Channel))
	}
	tuning := bc4TuningFor(opts.Quality)
	return encodeBlocks(s, 8, opts.Workers, func(bx, by int, dst []byte) {
		var values [16]float64
		s.gatherChannel(bx, by, opts.Channel, &values)
		encodeBC4Block(&values, tuning, dst)
	}), nil
}

// encodeBC4Block writes the eight bytes of one BC4 block and returns the squared
// error it believes the block carries.
//
// values holds the sixteen source codes of the block in row-major order, already
// quantized to eight bits. Quantizing first costs at most half a code and makes
// the error the same measurement an uncompressed 8-bit upload would report.
//
// The returned error must equal the error the decoder produces.
// TestEncoderBeliefMatchesTheDecoder checks it.
func encodeBC4Block(values *[16]float64, tuning bc4Tuning, dst []byte) float64 {
	lo, hi := values[0], values[0]
	for _, v := range values {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	iLo, iHi := int(lo), int(hi)

	best := bc4Fit{sse: math.Inf(1)}
	// The eight-value mode spreads all eight entries across the range, so it
	// wins on a smooth block.
	considerBC4(&best, fitBC4Mode(values, false, iLo, iHi, tuning))
	if tuning.sixValue {
		// The six-value mode pays two entries for the constants 0 and 255.
		// It wins whenever the block already holds those values, which a
		// cutout mask and a saturated data channel both do.
		considerBC4(&best, fitBC4Mode(values, true, iLo, iHi, tuning))
		if tuning.interiorStart {
			innerLo, innerHi, ok := bc4Interior(values)
			if ok && (innerLo != iLo || innerHi != iHi) {
				considerBC4(&best, fitBC4Mode(values, true, innerLo, innerHi, tuning))
			}
		}
	}
	if math.IsInf(best.sse, 1) {
		// Every mode failed, which only a constant block can do. The
		// six-value mode stores it exactly with equal endpoints.
		best = bc4Fit{e0: uint8(iLo), e1: uint8(iLo)}
	}
	putBC4(dst, best.e0, best.e1, best.bits)
	return best.sse
}

// considerBC4 keeps the candidate with the smaller squared error.
func considerBC4(best *bc4Fit, candidate bc4Fit) {
	if candidate.sse < best.sse {
		*best = candidate
	}
}

// bc4Interior returns the range of the texels that are not already at 0 or 255.
//
// The six-value mode reaches 0 and 255 for free, so its two endpoints should
// describe the rest of the block instead of stretching to the extremes.
func bc4Interior(values *[16]float64) (int, int, bool) {
	lo, hi := 256.0, -1.0
	for _, v := range values {
		if v <= 0 || v >= 255 {
			continue
		}
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if hi < 0 {
		return 0, 0, false
	}
	return int(lo), int(hi), true
}

// fitBC4Mode fits one mode and returns the best encoding it found.
//
// sixValue selects the mode that adds the constants 0 and 255. The mode lives in
// the endpoint order, so the function writes the larger endpoint first for the
// eight-value mode and the smaller endpoint first for the six-value mode.
func fitBC4Mode(values *[16]float64, sixValue bool, lo, hi int, tuning bc4Tuning) bc4Fit {
	if lo > hi {
		lo, hi = hi, lo
	}
	if !sixValue && hi == lo {
		// The eight-value mode needs endpoint0 > endpoint1. A constant
		// block cannot satisfy that, so leave it to the other mode.
		return bc4Fit{sse: math.Inf(1)}
	}
	e0, e1 := orderBC4(lo, hi, sixValue)
	sse, bits, weights := evalBC4(values, e0, e1)
	best := bc4Fit{sse: sse, e0: e0, e1: e1, bits: bits}

	curLo, curHi := lo, hi
	for round := 0; round < tuning.refineIters; round++ {
		solvedLo, solvedHi, ok := solveBC4(values, &weights)
		if !ok {
			break
		}
		nextLo, nextHi := clampCode(solvedLo), clampCode(solvedHi)
		if nextLo > nextHi {
			nextLo, nextHi = nextHi, nextLo
		}
		if !sixValue && nextLo == nextHi {
			break
		}
		if nextLo == curLo && nextHi == curHi {
			break
		}
		curLo, curHi = nextLo, nextHi
		e0, e1 = orderBC4(curLo, curHi, sixValue)
		sse, bits, nextWeights := evalBC4(values, e0, e1)
		weights = nextWeights
		if sse >= best.sse {
			// Refinement stopped paying. Keep the better pair.
			break
		}
		best = bc4Fit{sse: sse, e0: e0, e1: e1, bits: bits}
	}

	// Walk the endpoints one code at a time while it pays.
	//
	// The least-squares solve works on real numbers and then rounds. The
	// rounded pair is not always the best pair, because moving one endpoint by
	// one code shifts every interpolated palette entry. The walk repeats until
	// a sweep finds nothing, with a cap, so a block that already sits at a
	// local optimum pays one sweep.
	for sweep := 0; tuning.jitter > 0 && sweep < tuning.polishSweeps; sweep++ {
		bestLo, bestHi := int(best.e1), int(best.e0)
		if sixValue {
			bestLo, bestHi = int(best.e0), int(best.e1)
		}
		improved := false
		for dLo := -tuning.jitter; dLo <= tuning.jitter; dLo++ {
			for dHi := -tuning.jitter; dHi <= tuning.jitter; dHi++ {
				tryLo, tryHi := clampCode(float64(bestLo+dLo)), clampCode(float64(bestHi+dHi))
				if tryLo > tryHi {
					continue
				}
				if !sixValue && tryLo == tryHi {
					continue
				}
				je0, je1 := orderBC4(tryLo, tryHi, sixValue)
				jsse, jbits, _ := evalBC4(values, je0, je1)
				if jsse < best.sse {
					best = bc4Fit{sse: jsse, e0: je0, e1: je1, bits: jbits}
					improved = true
				}
			}
		}
		if !improved {
			break
		}
	}
	return best
}

// orderBC4 writes the endpoint pair in the byte order that selects the mode.
func orderBC4(lo, hi int, sixValue bool) (uint8, uint8) {
	if sixValue {
		return uint8(lo), uint8(hi)
	}
	return uint8(hi), uint8(lo)
}

func clampCode(v float64) int {
	rounded := math.Round(v)
	if rounded < 0 {
		return 0
	}
	if rounded > 255 {
		return 255
	}
	return int(rounded)
}

// evalBC4 scores one endpoint pair.
//
// The palette rounds to 8-bit codes first, so the score is the exact error the
// decoder in decode.go produces. An encoder that scored the unrounded palette
// would rank two candidates by a number no decoder ever computes.
func evalBC4(values *[16]float64, e0, e1 uint8) (float64, uint64, [16]float64) {
	pal := bc4Palette(e0, e1)
	var codes [8]float64
	for i, p := range pal {
		codes[i] = float64(roundCode(p))
	}
	weightTable := &bc4Weights8
	if e0 <= e1 {
		weightTable = &bc4Weights6
	}
	var sse float64
	var bits uint64
	var weights [16]float64
	for i, v := range values {
		bestIndex, bestErr := 0, math.Inf(1)
		for index, code := range codes {
			d := code - v
			d *= d
			if d < bestErr {
				bestErr, bestIndex = d, index
			}
		}
		sse += bestErr
		bits |= uint64(bestIndex) << (3 * i)
		weights[i] = weightTable[bestIndex]
	}
	return sse, bits, weights
}

// solveBC4 finds the endpoint pair with the smallest squared error while the
// indices stay fixed.
//
// With the index of each texel fixed, its position between the endpoints is a
// known weight w, and the value the decoder produces is lo*(1-w) + hi*w. So the
// squared error is a quadratic in two unknowns and its minimum solves a two by
// two linear system. Texels that landed on the constants 0 or 255 carry the
// weight -1 and drop out, because no endpoint moves them.
func solveBC4(values *[16]float64, weights *[16]float64) (float64, float64, bool) {
	var a2, ab, b2, ax, bx float64
	used := 0
	for i, w := range weights {
		if w < 0 {
			continue
		}
		used++
		a := 1 - w
		a2 += a * a
		ab += a * w
		b2 += w * w
		ax += a * values[i]
		bx += w * values[i]
	}
	if used < 2 {
		return 0, 0, false
	}
	det := a2*b2 - ab*ab
	if math.Abs(det) < 1e-9 {
		return 0, 0, false
	}
	lo := (b2*ax - ab*bx) / det
	hi := (a2*bx - ab*ax) / det
	return lo, hi, true
}

// putBC4 writes one BC4 block: endpoint0, endpoint1, then the 48-bit index field
// as six little-endian bytes.
func putBC4(dst []byte, e0, e1 uint8, bits uint64) {
	dst[0] = e0
	dst[1] = e1
	dst[2] = uint8(bits)
	dst[3] = uint8(bits >> 8)
	dst[4] = uint8(bits >> 16)
	dst[5] = uint8(bits >> 24)
	dst[6] = uint8(bits >> 32)
	dst[7] = uint8(bits >> 40)
}
