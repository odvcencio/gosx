package bcn

import (
	"encoding/binary"
	"fmt"
	"math"
)

// BC1Options controls the colour encoder.
type BC1Options struct {
	// Transfer names the function the encoder applies to the colour
	// channels. Pick TransferSRGB for a colour target and TransferUnorm for
	// a data target. The zero value is illegal. Read the package doc.
	Transfer Transfer
	// Quality trades encode time for image quality.
	Quality Quality
	// Alpha picks the opaque variant or the one-bit-alpha variant.
	Alpha BC1Alpha
	// AlphaCutoff is the linear alpha below which a texel becomes
	// transparent. Zero means 0.5. The field applies to BC1Cutout only.
	AlphaCutoff float32
	// Workers sets the goroutine count. Zero or one runs on the calling
	// goroutine, and a negative value asks for one worker for each
	// processor. The output never depends on this field.
	Workers int
}

// BC1Alpha selects how BC1 treats the alpha channel.
type BC1Alpha int

const (
	// BC1Opaque ignores alpha. The encoder never emits the transparent
	// index, so the payload decodes the same under the RGB and the RGBA
	// VkFormat.
	BC1Opaque BC1Alpha = iota
	// BC1Cutout keeps one alpha bit. A texel below AlphaCutoff decodes as
	// transparent black. The block that holds it must use the three-colour
	// mode, which costs one palette entry, so a cutout block carries more
	// colour error than an opaque one.
	BC1Cutout
)

// Format returns the format the option pair produces.
func (a BC1Alpha) Format() Format {
	if a == BC1Cutout {
		return FormatBC1RGBA
	}
	return FormatBC1RGB
}

// bc1Tuning switches the parts of the search on and off. The tests use it to
// measure what each part buys and to build deliberately weaker encoders.
type bc1Tuning struct {
	// boundingBox starts a candidate from the per-channel bounding box.
	boundingBox bool
	// principalAxis starts a candidate from the dominant eigenvector of the
	// colour covariance.
	principalAxis bool
	// refineIters caps the least-squares rounds of each candidate.
	refineIters int
	// clusterFit enumerates the ways to split the sorted texel order into
	// palette classes.
	clusterFit bool
	// clusterKeep is how many cluster candidates get an exact score after
	// quantization.
	clusterKeep int
	// threeColor also tries the three-entry palette on an opaque block.
	threeColor bool
	// polishSweeps caps the one-step endpoint walk that runs on every
	// candidate.
	polishSweeps int
}

// bc1TuningFor returns the tuning of one quality level.
//
// Every count here comes from measurement.
//
//   - The refinement cap is four. TestBC1RefinementIterations reports the gain of
//     each round: round one is worth 0.08 to 0.33 dB, round four is worth 0.01 dB
//     at most, and rounds five to eight move no image by more than 0.006 dB.
//   - The cluster search keeps four splits for exact scoring.
//     TestBC1ClusterKeepCount reports 0.35 dB from the third and fourth split on a
//     smooth banded image and about 0.005 dB elsewhere.
//   - The polish runs at most four sweeps and stops early. Most blocks stop after
//     one sweep, which is why the cap is cheap.
func bc1TuningFor(q Quality) bc1Tuning {
	if q == QualityFast {
		return bc1Tuning{boundingBox: true, refineIters: 1}
	}
	return bc1Tuning{
		boundingBox:   true,
		principalAxis: true,
		refineIters:   4,
		clusterFit:    true,
		clusterKeep:   4,
		threeColor:    true,
		polishSweeps:  4,
	}
}

// EncodeBC1 compresses s as BC1, eight bytes for each 4x4 block.
//
// Every block holds two RGB565 endpoints and sixteen 2-bit indices. The integer
// order of the two endpoints selects the mode, so the encoder swaps the pair and
// remaps the indices when the mode it picked needs the other order.
func EncodeBC1(s *Surface, opts BC1Options) ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if !opts.Transfer.valid() {
		return nil, fmt.Errorf("%w: got %s", ErrTransfer, opts.Transfer)
	}
	if opts.Alpha != BC1Opaque && opts.Alpha != BC1Cutout {
		return nil, fmt.Errorf("%w: alpha mode %d", ErrShape, int(opts.Alpha))
	}
	cutoff := opts.AlphaCutoff
	if cutoff <= 0 {
		cutoff = 0.5
	}
	tuning := bc1TuningFor(opts.Quality)
	cutout := opts.Alpha == BC1Cutout
	return encodeBlocks(s, 8, opts.Workers, func(bx, by int, dst []byte) {
		texels, mask := gatherColor(s, bx, by, opts.Transfer, cutout, cutoff)
		encodeColorBlock(&texels, mask, tuning, false, dst)
	}), nil
}

// gatherColor copies one block of colour and builds the transparency mask.
func gatherColor(s *Surface, bx, by int, t Transfer, cutout bool, cutoff float32) ([16]vec3, uint16) {
	var codes [16]RGBA8
	s.gatherRGBA(bx, by, t, &codes)
	var texels [16]vec3
	var mask uint16
	for i, c := range codes {
		texels[i] = vec3{float64(c.R), float64(c.G), float64(c.B)}
		if cutout && float64(c.A) < float64(cutoff)*255 {
			mask |= 1 << uint(i)
		}
	}
	return texels, mask
}

// encodeColorBlock writes the eight bytes of one BC1-layout colour block and
// returns the squared error it believes the block carries.
//
// forceFour selects the BC3 rule, where the colour block always decodes with the
// four-colour mode. A BC3 block never carries a transparency mask, because its
// alpha block already holds eight bits for each texel.
//
// The returned error must equal the error the decoder produces. The two can only
// differ through a mistake in the endpoint order or in an index map, and
// TestEncoderBeliefMatchesTheDecoder checks every block of every test image.
func encodeColorBlock(texels *[16]vec3, mask uint16, tuning bc1Tuning, forceFour bool, dst []byte) float64 {
	if mask == 0xFFFF {
		// Every texel is transparent. Two equal endpoints select the
		// three-colour mode, and index 3 decodes to transparent black.
		binary.LittleEndian.PutUint16(dst[0:2], 0)
		binary.LittleEndian.PutUint16(dst[2:4], 0)
		binary.LittleEndian.PutUint32(dst[4:8], 0xFFFFFFFF)
		return 0
	}

	// A block with a transparent texel must use the three-colour mode,
	// because that mode is the only one whose index 3 means transparent.
	allowFour := mask == 0
	allowThree := mask != 0 || (tuning.threeColor && !forceFour)
	if forceFour {
		allowFour = true
		allowThree = false
	}

	mean, lo, hi, count := blockStats(texels, mask)
	axis, haveAxis := principalAxis(texels, mask, mean)

	best := colorFit{sse: math.Inf(1)}
	// polished remembers the endpoint pairs the polish already walked. Two
	// candidates often refine to the same pair, and the polish is the most
	// expensive step, so skipping the repeat costs nothing and saves a third of
	// the work on smooth content. The skip changes no output, because the polish
	// is deterministic.
	var polished [2 * maxCandidates]uint32
	seen := 0

	// try refines one starting pair, polishes it once, and keeps the winner.
	try := func(three bool, a, b uint16) {
		fit := leastSquaresColor(texels, mask, evalPair(texels, mask, a, b, three), tuning.refineIters)
		if tuning.polishSweeps > 0 {
			key := uint32(fit.a)<<16 | uint32(fit.b)
			if three {
				key |= 1 << 31
			}
			for _, done := range polished[:seen] {
				if done == key {
					considerColor(&best, fit)
					return
				}
			}
			if seen < len(polished) {
				polished[seen] = key
				seen++
			}
			fit = polishColor(texels, mask, fit, tuning.polishSweeps)
		}
		considerColor(&best, fit)
	}

	for _, three := range [2]bool{false, true} {
		if three && !allowThree {
			continue
		}
		if !three && !allowFour {
			continue
		}
		if tuning.boundingBox {
			try(three, quantize565(lo), quantize565(hi))
		}
		if tuning.principalAxis && haveAxis {
			pLo, pHi := axisExtremes(texels, mask, mean, axis)
			try(three, quantize565(pLo), quantize565(pHi))
		}
		if tuning.clusterFit && haveAxis && count >= 2 {
			clusterFit(texels, mask, mean, axis, count, three, tuning, try)
		}
	}
	if math.IsInf(best.sse, 1) {
		// No candidate survived, which only an axis-free block reaches.
		// Store the mean, which is exact for a block of one colour.
		try(mask != 0, quantize565(mean), quantize565(mean))
	}
	emitColorBlock(&best, forceFour, dst)
	return best.sse
}

// considerColor keeps the candidate with the smaller squared error.
func considerColor(best *colorFit, candidate colorFit) {
	if candidate.sse < best.sse {
		*best = candidate
	}
}

// axisExtremes projects the texels onto the axis and returns the two extremes.
func axisExtremes(texels *[16]vec3, mask uint16, mean, axis vec3) (vec3, vec3) {
	minT, maxT := math.Inf(1), math.Inf(-1)
	for i := range texels {
		if mask>>uint(i)&1 != 0 {
			continue
		}
		t := texels[i].sub(mean).dot(axis)
		minT = smaller(minT, t)
		maxT = larger(maxT, t)
	}
	return mean.add(axis.scale(minT)), mean.add(axis.scale(maxT))
}

// clusterCandidate holds one split the cluster search kept.
//
// The record holds the normal equations of the split, not the endpoints. Solving
// them costs a division, and the search only needs the endpoints of the few splits
// it keeps.
type clusterCandidate struct {
	score      float64
	a2, ab, b2 float64
	ax, bx     vec3
}

// clusterFit searches the ways to split the sorted texel order into classes.
//
// The optimal endpoint pair for a fixed class assignment has a closed form, and so
// does the squared error of that pair. So the search costs a constant amount of
// work for each split once the prefix sums exist, and it can afford to try every
// split. libsquish calls this a cluster fit.
//
// The search is complete, not bounded. A run over the sorted order covers every
// assignment an optimal encoding can use, because the palette lies on a line and
// the nearest entry of a line is monotone along it. So no bound costs quality
// here. The cost is the split count: 969 for a four-class split of sixteen texels
// and 153 for a three-class split.
//
// # How the inner loop stays cheap
//
// Write the predicted colour of a texel as lo*(1-w) + hi*w, where w is the weight
// of its class. Then the squared error of the whole split is a quadratic in lo and
// hi whose normal equations need five numbers: three coefficients built from the
// class counts, and two weighted colour sums. At the minimum the squared error
// drops to a constant minus
//
//	(b2*|ax|^2 - 2*ab*(ax . bx) + a2*|bx|^2) / (a2*b2 - ab*ab)
//
// so the search ranks a split from those five numbers alone. It never builds the
// endpoints, and it never touches the sixteen texels again.
//
// Moving one texel across one class boundary changes each of the five numbers by a
// fixed step, so the loops carry them forward instead of rebuilding them. That
// turns about ninety operations for each split into about thirty.
func clusterFit(texels *[16]vec3, mask uint16, mean, axis vec3, count int, three bool, tuning bc1Tuning, try func(three bool, a, b uint16)) {
	// Sort the texels along the axis with an insertion sort over fixed
	// arrays. Sixteen elements never justify a heap allocation, and this
	// function runs once for each block of every texture.
	var keys [16]float64
	var sorted [16]vec3
	filled := 0
	for i := range texels {
		if mask>>uint(i)&1 != 0 {
			continue
		}
		key := texels[i].sub(mean).dot(axis)
		position := filled
		for position > 0 && keys[position-1] > key {
			keys[position] = keys[position-1]
			sorted[position] = sorted[position-1]
			position--
		}
		keys[position] = key
		sorted[position] = texels[i]
		filled++
	}

	// Prefix sums let each inner loop start without a scan. sums[k] holds the
	// total of the first k sorted texels.
	var sums [17]vec3
	for k := 0; k < filled; k++ {
		sums[k+1] = sums[k].add(sorted[k])
	}
	total := sums[count]

	keep := tuning.clusterKeep
	if keep < 1 {
		keep = 1
	}
	if keep > maxClusterKeep {
		keep = maxClusterKeep
	}
	var top [maxClusterKeep]clusterCandidate
	held := 0

	push := func(candidate clusterCandidate) {
		if held == keep && candidate.score >= top[held-1].score {
			return
		}
		position := held
		for position > 0 && top[position-1].score > candidate.score {
			position--
		}
		if held < keep {
			held++
		}
		copy(top[position+1:held], top[position:held-1])
		top[position] = candidate
	}

	// score ranks one split from its normal equations. A lower number is better,
	// and the constant term every split shares drops out.
	score := func(a2, ab, b2 float64, ax, bx vec3) {
		det := a2*b2 - ab*ab
		if det < 1e-9 {
			return
		}
		numerator := b2*ax.dot(ax) - 2*ab*ax.dot(bx) + a2*bx.dot(bx)
		push(clusterCandidate{-numerator / det, a2, ab, b2, ax, bx})
	}

	if three {
		// Weights 0, one half and 1. Moving one texel from the last class to
		// the middle one adds a quarter to a2 and to ab, takes three quarters
		// from b2, and moves half of the texel from bx to ax.
		for i := 0; i <= count; i++ {
			a2 := float64(i)
			ab := 0.0
			b2 := float64(count - i)
			ax := sums[i]
			bx := total.sub(sums[i])
			for j := i; j <= count; j++ {
				score(a2, ab, b2, ax, bx)
				if j == count {
					break
				}
				half := sorted[j].scale(0.5)
				a2 += 0.25
				ab += 0.25
				b2 -= 0.75
				ax = ax.add(half)
				bx = bx.sub(half)
			}
		}
	} else {
		// Weights 0, one third, two thirds and 1. Moving one texel from the
		// last class to the third one adds one ninth to a2 and two ninths to
		// ab, takes five ninths from b2, and moves one third of the texel from
		// bx to ax.
		const oneNinth = 1.0 / 9
		for i := 0; i <= count; i++ {
			for j := i; j <= count; j++ {
				middle := sums[j].sub(sums[i])
				a2 := float64(i) + float64(j-i)*4*oneNinth
				ab := float64(j-i) * 2 * oneNinth
				b2 := float64(j-i)*oneNinth + float64(count-j)
				ax := sums[i].add(middle.scale(2.0 / 3))
				bx := middle.scale(1.0 / 3).add(total.sub(sums[j]))
				for k := j; k <= count; k++ {
					score(a2, ab, b2, ax, bx)
					if k == count {
						break
					}
					third := sorted[k].scale(1.0 / 3)
					a2 += oneNinth
					ab += 2 * oneNinth
					b2 -= 5 * oneNinth
					ax = ax.add(third)
					bx = bx.sub(third)
				}
			}
		}
	}

	// The closed form works on real endpoints. Solve, clamp into the colour cube
	// and quantize the few splits the search kept, then let refinement recover
	// what the quantization lost.
	for _, candidate := range top[:held] {
		det := candidate.a2*candidate.b2 - candidate.ab*candidate.ab
		lo := candidate.ax.scale(candidate.b2 / det).sub(candidate.bx.scale(candidate.ab / det))
		hi := candidate.bx.scale(candidate.a2 / det).sub(candidate.ax.scale(candidate.ab / det))
		try(three, quantize565(clampVec3(lo)), quantize565(clampVec3(hi)))
	}
}

// maxClusterKeep bounds the exact scoring pass of the cluster search.
const maxClusterKeep = 8

// maxCandidates bounds the candidate list of one mode: a bounding box, a principal
// axis, a fallback, and the kept cluster splits.
const maxCandidates = maxClusterKeep + 3

func clampVec3(v vec3) vec3 {
	return vec3{clampChannel(v.x), clampChannel(v.y), clampChannel(v.z)}
}

// clampChannel holds one channel inside the code range.
func clampChannel(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// emitColorBlock writes the endpoints and the indices of one colour block.
//
// The endpoint order carries the mode, so this function may swap the pair. A
// swap moves every texel to the mirrored palette entry, which is why each mode
// carries two index maps.
func emitColorBlock(fit *colorFit, forceFour bool, dst []byte) {
	fourColour := forceFour || !fit.three
	c0, c1 := fit.a, fit.b
	indexMap := mapFour[:]

	switch {
	case fourColour && fit.a == fit.b:
		// The four-colour mode needs color0 > color1, which equal
		// endpoints cannot give. Every palette entry holds the same
		// colour either way, so write index 0 everywhere. A BC1 decoder
		// then reads the three-colour mode and returns that colour, and a
		// BC3 decoder reads the four-colour mode and returns it too.
		//
		// The branch runs only when the block holds no transparent texel,
		// because the four-colour mode cannot store one.
		binary.LittleEndian.PutUint16(dst[0:2], c0)
		binary.LittleEndian.PutUint16(dst[2:4], c0)
		binary.LittleEndian.PutUint32(dst[4:8], 0)
		return
	case fourColour && fit.a < fit.b:
		c0, c1 = fit.b, fit.a
		indexMap = mapFourSwap[:]
	case !fourColour && fit.a > fit.b:
		c0, c1 = fit.b, fit.a
		indexMap = mapThreeSwap[:]
	case !fourColour:
		indexMap = mapThree[:]
	}

	var bits uint32
	for i, class := range fit.class {
		index := uint32(3)
		if class != classTransparent {
			index = uint32(indexMap[class])
		}
		bits |= index << uint(2*i)
	}
	binary.LittleEndian.PutUint16(dst[0:2], c0)
	binary.LittleEndian.PutUint16(dst[2:4], c1)
	binary.LittleEndian.PutUint32(dst[4:8], bits)
}
