package bc7

import "math"

// This file fits one endpoint pair to one channel group of one subset.
//
// A BC7 subset stores two endpoints and one index per texel. So the decoded
// texels of a subset all sit on the line segment between the two endpoints, at
// the positions the weight table allows. Fitting a subset therefore means
// finding the best line, and the best index per texel on it.
//
// The steps, in order of what each one buys:
//
//  1. Take the principal axis of the texel colours. A per-channel bounding box
//     picks a diagonal that no texel lies near when the colours run along a
//     slanted axis, which is the common case in a photograph.
//  2. Assign indices, then re-solve the two endpoints by least squares with
//     those indices fixed. Repeat. Each pass lowers the error or stops.
//  3. Choose the parity bits. They shift the reachable endpoint grid, so the
//     choice changes which endpoints exist, not only how they round.
//
// Every channel group BC7 uses is a contiguous run of channels: red to blue,
// red to alpha, or alpha alone. So the code carries the group as a half-open
// range instead of a slice of channel numbers. That keeps the inner loops free
// of a second memory reference and lets the compiler drop the bounds checks.

// seedKind names how fitLine starts.
type seedKind uint8

const (
	// seedPCA starts from the principal axis.
	seedPCA seedKind = iota
	// seedBoundingBox starts from the per-channel minimum and maximum. It
	// exists so the tests can measure what the principal axis buys.
	seedBoundingBox
)

// The channel groups a BC7 mode fits together, as half-open ranges.
const (
	chanRGBLo, chanRGBHi   = 0, 3
	chanRGBALo, chanRGBAHi = 0, 4
	chanALo, chanAHi       = 3, 4
)

// fitWork holds the buffers one fit reuses. It lives in scratch, so a fit
// allocates nothing.
type fitWork struct {
	pal    [16][4]int32
	combos [4][2]int8
}

// lineFit holds the result of fitting one subset channel group.
type lineFit struct {
	// stored holds the two endpoint values as the block will store them,
	// before any parity bit joins them.
	stored [2][4]uint32
	// parity holds the parity bit of each endpoint, or -1 when the mode has
	// none. A shared parity mode reports the same bit twice.
	parity [2]int8
	// idx holds the chosen index of every texel, addressed by texel number so
	// the caller can pack without a second mapping.
	idx [16]uint8
	// err is the weighted squared error of this channel group, in 8-bit code
	// units.
	err float64
}

// seedEndpoints computes the starting endpoint pair for one subset.
func seedEndpoints(block *[16][4]int32, members []uint8, lo, hi int, seed seedKind, e0, e1 *[4]float64) {
	if seed == seedBoundingBox {
		boundingBoxEndpoints(block, members, lo, hi, e0, e1)
		return
	}
	principalAxisEndpoints(block, members, lo, hi, e0, e1)
}

// fitLine finds the best endpoint pair for one subset and one channel group.
//
// members lists the texels of the subset. lo and hi bound the channels this call
// owns, so a mode with a separate alpha index set calls fitLine twice. prec is
// the stored endpoint precision before any parity bit.
func fitLine(
	work *fitWork,
	block *[16][4]int32,
	members []uint8,
	lo, hi int,
	weights *[4]float64,
	indexBits, prec int,
	pkind pbitKind,
	rounds int,
	exhaustive, exact bool,
	seed seedKind,
) lineFit {
	var e0, e1 [4]float64
	seedEndpoints(block, members, lo, hi, seed, &e0, &e1)
	return fitLineFrom(work, block, members, lo, hi, weights,
		indexBits, prec, pkind, rounds, exhaustive, exact, e0, e1)
}

// fitLineFrom refines a given endpoint pair.
//
// A caller that evaluates the same subset of the same partition for several modes
// reuses one seed, because the seed depends only on the texels and the channel
// range. Modes 1, 3 and 7 all read the top of the same partition ranking, so the
// saving is real.
func fitLineFrom(
	work *fitWork,
	block *[16][4]int32,
	members []uint8,
	lo, hi int,
	weights *[4]float64,
	indexBits, prec int,
	pkind pbitKind,
	rounds int,
	exhaustive, exact bool,
	e0, e1 [4]float64,
) lineFit {
	var best lineFit
	best.err = math.Inf(1)

	for round := 0; ; round++ {
		count := fillParityCombos(&work.combos, pkind, exhaustive, &e0, &e1, lo, hi, prec)
		roundBest := lineFit{err: math.Inf(1)}
		for i := 0; i < count; i++ {
			combo := work.combos[i]
			var cand lineFit
			cand.parity = combo
			var u0, u1 [4]uint32
			for c := lo; c < hi; c++ {
				cand.stored[0][c] = quantizeEndpoint(e0[c], prec, combo[0])
				cand.stored[1][c] = quantizeEndpoint(e1[c], prec, combo[1])
				u0[c] = widenEndpoint(cand.stored[0][c], prec, combo[0])
				u1[c] = widenEndpoint(cand.stored[1][c], prec, combo[1])
			}
			cand.err = assignIndices(work, block, members, lo, hi, weights, &u0, &u1, indexBits, exact, &cand.idx)
			if cand.err < roundBest.err {
				roundBest = cand
			}
		}
		if roundBest.err < best.err {
			best = roundBest
		}
		if best.err == 0 || round >= rounds {
			break
		}
		var n0, n1 [4]float64
		if !leastSquares(block, members, lo, hi, &roundBest.idx, indexBits, &n0, &n1) {
			break
		}
		if sameEndpoints(&e0, &e1, &n0, &n1, lo, hi) {
			break
		}
		e0, e1 = n0, n1
	}
	return best
}

// powerIterations is how many times principalAxisEndpoints multiplies by the
// covariance matrix.
//
// Six passes from a start vector that already leans towards the answer converge
// far past the precision an 8-bit endpoint can keep. The loop does not rescale
// between passes: a covariance entry reaches at most 16*255*255, so six passes
// stay near 1e42, which float64 holds with room to spare. One normalize at the
// end removes five square roots from the hottest path in the encoder.
const powerIterations = 6

// principalAxisEndpoints projects the subset onto its dominant colour axis and
// takes the extremes.
//
// Power iteration finds the dominant eigenvector of the covariance matrix. The
// start vector is the column of the covariance with the largest diagonal, which
// already leans towards the answer.
func principalAxisEndpoints(block *[16][4]int32, members []uint8, lo, hi int, e0, e1 *[4]float64) {
	var mean [4]float64
	n := float64(len(members))
	for _, t := range members {
		px := &block[t]
		for c := lo; c < hi; c++ {
			mean[c] += float64(px[c])
		}
	}
	for c := lo; c < hi; c++ {
		mean[c] /= n
	}

	var cov [4][4]float64
	for _, t := range members {
		px := &block[t]
		var d [4]float64
		for c := lo; c < hi; c++ {
			d[c] = float64(px[c]) - mean[c]
		}
		for a := lo; a < hi; a++ {
			for b := a; b < hi; b++ {
				cov[a][b] += d[a] * d[b]
			}
		}
	}
	for a := lo; a < hi; a++ {
		for b := a + 1; b < hi; b++ {
			cov[b][a] = cov[a][b]
		}
	}

	start := lo
	for c := lo; c < hi; c++ {
		if cov[c][c] > cov[start][start] {
			start = c
		}
	}
	if cov[start][start] <= 0 {
		// Every texel of the subset holds the same colour. One point is the
		// whole answer.
		for c := lo; c < hi; c++ {
			e0[c] = mean[c]
			e1[c] = mean[c]
		}
		return
	}

	var axis [4]float64
	for c := lo; c < hi; c++ {
		axis[c] = cov[c][start] / cov[start][start]
	}
	for iter := 0; iter < powerIterations; iter++ {
		var next [4]float64
		for a := lo; a < hi; a++ {
			var sum float64
			for b := lo; b < hi; b++ {
				sum += cov[a][b] * axis[b]
			}
			next[a] = sum
		}
		axis = next
	}
	if !normalize(&axis, lo, hi) {
		for c := lo; c < hi; c++ {
			e0[c] = mean[c]
			e1[c] = mean[c]
		}
		return
	}

	tmin, tmax := math.Inf(1), math.Inf(-1)
	for _, t := range members {
		px := &block[t]
		var proj float64
		for c := lo; c < hi; c++ {
			proj += (float64(px[c]) - mean[c]) * axis[c]
		}
		if proj < tmin {
			tmin = proj
		}
		if proj > tmax {
			tmax = proj
		}
	}
	for c := lo; c < hi; c++ {
		e0[c] = clampCode(mean[c] + tmin*axis[c])
		e1[c] = clampCode(mean[c] + tmax*axis[c])
	}
}

// boundingBoxEndpoints takes the per-channel minimum and maximum. It is the
// reference the principal axis has to beat.
func boundingBoxEndpoints(block *[16][4]int32, members []uint8, lo, hi int, e0, e1 *[4]float64) {
	for c := lo; c < hi; c++ {
		low, high := int32(255), int32(0)
		for _, t := range members {
			v := block[t][c]
			if v < low {
				low = v
			}
			if v > high {
				high = v
			}
		}
		e0[c] = float64(low)
		e1[c] = float64(high)
	}
}

// normalize scales a vector to unit length over one channel range. It reports
// false when the vector is too short to normalize.
func normalize(v *[4]float64, lo, hi int) bool {
	var sum float64
	for c := lo; c < hi; c++ {
		sum += v[c] * v[c]
	}
	if sum <= 1e-12 {
		return false
	}
	inv := 1 / math.Sqrt(sum)
	for c := lo; c < hi; c++ {
		v[c] *= inv
	}
	return true
}

// assignIndices picks the best index for every texel and returns the total
// weighted squared error.
//
// Every palette entry sits on the line between the two endpoints, so the
// perpendicular part of a texel's error does not depend on the index. Projecting
// onto the line therefore finds the best index without testing all of them.
//
// exact adds a check of the two neighbouring indices. It exists because
// interpolate rounds each palette entry to an integer, so rounding can in
// principle move the winner one step. Measurement says it almost never does:
// across the coverage images the check moved the peak signal-to-noise ratio by
// at most 0.03 dB, and it cost about a sixth of the encode time. So the
// exhaustive quality level keeps it as the reference and the other levels drop
// it. The returned error is exact either way, because it comes from the palette
// entry the call actually chose.
func assignIndices(
	work *fitWork,
	block *[16][4]int32,
	members []uint8,
	lo, hi int,
	weights *[4]float64,
	u0, u1 *[4]uint32,
	indexBits int,
	exact bool,
	idx *[16]uint8,
) float64 {
	table := weightsFor(indexBits)
	maxIdx := len(table) - 1
	pal := &work.pal
	for k, wk := range table {
		for c := lo; c < hi; c++ {
			pal[k][c] = int32(interpolate(u0[c], u1[c], wk))
		}
	}

	var dir [4]float64
	var den float64
	for c := lo; c < hi; c++ {
		d := float64(u1[c]) - float64(u0[c])
		dir[c] = d
		den += d * d
	}

	total := 0.0
	for _, t := range members {
		px := &block[t]
		k := 0
		if den > 0 {
			var num float64
			for c := lo; c < hi; c++ {
				num += (float64(px[c]) - float64(u0[c])) * dir[c]
			}
			p := int(num/den*nearestWeightRes + 0.5)
			if p < 0 {
				p = 0
			}
			if p > nearestWeightRes {
				p = nearestWeightRes
			}
			k = int(nearestWeight[indexBits][p])
		}
		bestK, bestErr := k, palErr(px, lo, hi, weights, &pal[k])
		if exact {
			if k > 0 {
				if e := palErr(px, lo, hi, weights, &pal[k-1]); e < bestErr {
					bestK, bestErr = k-1, e
				}
			}
			if k < maxIdx {
				if e := palErr(px, lo, hi, weights, &pal[k+1]); e < bestErr {
					bestK, bestErr = k+1, e
				}
			}
		}
		idx[t] = uint8(bestK)
		total += bestErr
	}
	return total
}

// palErr returns the weighted squared error of one texel against one palette
// entry.
func palErr(px *[4]int32, lo, hi int, weights *[4]float64, entry *[4]int32) float64 {
	var sum float64
	for c := lo; c < hi; c++ {
		d := float64(px[c] - entry[c])
		sum += weights[c] * d * d
	}
	return sum
}

// leastSquares re-solves the two endpoints with the indices held fixed.
//
// Every texel sits at (1-w)*e0 + w*e1 for its own weight w. The squared error is
// quadratic in the two endpoints, so setting both partial derivatives to zero
// gives one two by two system per channel, with one shared matrix. Solve it
// directly. The call reports false when the matrix is singular, which happens
// when every texel took the same index.
func leastSquares(
	block *[16][4]int32,
	members []uint8,
	lo, hi int,
	idx *[16]uint8,
	indexBits int,
	e0, e1 *[4]float64,
) bool {
	table := weightsFor(indexBits)
	var a, b, c2 float64
	var p, q [4]float64
	for _, t := range members {
		w := float64(table[idx[t]]) / 64
		o := 1 - w
		a += o * o
		b += o * w
		c2 += w * w
		px := &block[t]
		for c := lo; c < hi; c++ {
			v := float64(px[c])
			p[c] += o * v
			q[c] += w * v
		}
	}
	det := a*c2 - b*b
	if det < 1e-9 {
		return false
	}
	for c := lo; c < hi; c++ {
		e0[c] = clampCode((c2*p[c] - b*q[c]) / det)
		e1[c] = clampCode((a*q[c] - b*p[c]) / det)
	}
	return true
}

// fillParityCombos writes the parity bit assignments a fit should evaluate and
// returns how many it wrote.
//
// Exhaustive mode measures every assignment with a full index pass, which is the
// reference. The cheaper mode picks the assignment whose grid sits closest to the
// real-valued endpoints, which costs one pass instead of four.
func fillParityCombos(
	out *[4][2]int8,
	pkind pbitKind,
	exhaustive bool,
	e0, e1 *[4]float64,
	lo, hi, prec int,
) int {
	if !exhaustive || pkind == pbitNone {
		p0, p1 := chooseParity(e0, e1, lo, hi, prec, pkind)
		out[0] = [2]int8{p0, p1}
		return 1
	}
	if pkind == pbitShared {
		out[0] = [2]int8{0, 0}
		out[1] = [2]int8{1, 1}
		return 2
	}
	out[0] = [2]int8{0, 0}
	out[1] = [2]int8{0, 1}
	out[2] = [2]int8{1, 0}
	out[3] = [2]int8{1, 1}
	return 4
}

// chooseParity picks the parity bits whose endpoint grid sits closest to the
// real-valued endpoints.
func chooseParity(e0, e1 *[4]float64, lo, hi, prec int, pkind pbitKind) (int8, int8) {
	switch pkind {
	case pbitPerEndpoint:
		return bestParityFor(e0, lo, hi, prec), bestParityFor(e1, lo, hi, prec)
	case pbitShared:
		zero := parityErr(e0, lo, hi, prec, 0) + parityErr(e1, lo, hi, prec, 0)
		one := parityErr(e0, lo, hi, prec, 1) + parityErr(e1, lo, hi, prec, 1)
		if one < zero {
			return 1, 1
		}
		return 0, 0
	}
	return -1, -1
}

// bestParityFor picks the parity bit of one endpoint.
func bestParityFor(e *[4]float64, lo, hi, prec int) int8 {
	if parityErr(e, lo, hi, prec, 1) < parityErr(e, lo, hi, prec, 0) {
		return 1
	}
	return 0
}

// parityErr returns the squared endpoint rounding error one parity bit costs.
func parityErr(e *[4]float64, lo, hi, prec int, parity int8) float64 {
	var sum float64
	for c := lo; c < hi; c++ {
		stored := quantizeEndpoint(e[c], prec, parity)
		got := float64(widenEndpoint(stored, prec, parity))
		d := got - e[c]
		sum += d * d
	}
	return sum
}

// sameEndpoints reports whether a refinement pass moved nothing worth
// re-quantizing.
func sameEndpoints(a0, a1, b0, b1 *[4]float64, lo, hi int) bool {
	for c := lo; c < hi; c++ {
		if math.Abs(a0[c]-b0[c]) > 0.25 || math.Abs(a1[c]-b1[c]) > 0.25 {
			return false
		}
	}
	return true
}

func clampCode(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// swapEndpoints exchanges the endpoint pair and mirrors every index of the
// subset.
//
// The weight table is symmetric about 32, so the mirrored index picks the
// mirrored weight and the decoded colours do not change. That is what makes the
// anchor rule free to satisfy.
func (f *lineFit) swapEndpoints(members []uint8, indexBits int) {
	f.stored[0], f.stored[1] = f.stored[1], f.stored[0]
	f.parity[0], f.parity[1] = f.parity[1], f.parity[0]
	high := uint8((1 << uint(indexBits)) - 1)
	for _, t := range members {
		f.idx[t] = high - f.idx[t]
	}
}

// fixAnchor makes the anchor index fit its shortened field.
//
// The anchor texel of a subset stores indexBits-1 bits, and the missing high bit
// is defined to be zero. So the anchor index must stay below half the range.
// When it does not, swap the endpoints and mirror the indices, which is free.
//
// Skip this and the packer drops the anchor's high bit. The decoder then reads a
// different index for one texel per subset, and the image looks right except for
// one wrong texel per block. Never make that trade.
func (f *lineFit) fixAnchor(members []uint8, anchor, indexBits int) {
	if int(f.idx[anchor]) >= 1<<uint(indexBits-1) {
		f.swapEndpoints(members, indexBits)
	}
}
