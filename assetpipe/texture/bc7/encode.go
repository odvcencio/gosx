package bc7

import "math"

// This file selects a mode and a partition for one block.
//
// Mode selection measures. Every enabled mode encodes the block, and the lowest
// exact palette error wins. No heuristic reads the block content and names a
// mode. That matters because the modes trade endpoint precision against index
// precision against subset count, and which trade wins depends on the block in
// ways a rule of thumb gets wrong. TestInternalErrorMatchesMeasuredError proves
// the error the selection compares is the error the decoder will produce.

// config is the resolved encoder setting. Options maps onto it, and the tests
// build one directly so they can degrade one step at a time.
type config struct {
	space ColorSpace
	// modes is the bitmask of modes the encoder may pick.
	modes ModeMask
	// partitions bounds how many candidate partitions a multi-subset mode
	// evaluates in full. The ranking decides which ones.
	partitions int
	// rounds bounds the least squares refinement passes per fit.
	rounds int
	// rotations is 1 to try only the identity channel rotation, or 4 to try
	// all of them in mode 4 and mode 5.
	rotations int
	// exhaustiveParity measures every parity bit assignment with a full index
	// pass. The cheap path picks by endpoint rounding error alone.
	exhaustiveParity bool
	// exactIndices checks the two neighbouring indices as well as the one the
	// projection picks. See the assignIndices comment for the measured cost and
	// the measured gain.
	exactIndices bool
	// seed picks the endpoint initialization.
	seed seedKind
	// weights scales the squared error of each channel.
	weights [4]float64
	// skipAnchorFix exists for the mutation tests. It disables the endpoint
	// swap that keeps an anchor index inside its shortened field, which is the
	// classic silent BC7 corruption. Never set it in production.
	skipAnchorFix bool
}

// candidate is one mode's best answer for one block.
type candidate struct {
	spec blockSpec
	err  float64
}

// The partition ranking sums raw moments per subset: one sum per channel and one
// per distinct channel pair. The layout puts every red, green and blue term
// first, so a block with constant alpha sums a 9 entry prefix instead of all 14.
//
//	0 r   1 g   2 b
//	3 rr  4 rg  5 rb  6 gg  7 gb  8 bb
//	9 a   10 ra 11 ga 12 ba 13 aa
const (
	momentCount = 14
	momentsRGB  = 9
)

// sumIdx maps a channel to its sum slot, and prodIdx maps a channel pair to its
// product slot.
var (
	sumIdx  = [4]int{0, 1, 2, 9}
	prodIdx = [4][4]int{
		{3, 4, 5, 10},
		{4, 6, 7, 11},
		{5, 7, 8, 12},
		{10, 11, 12, 13},
	}
)

// momentsFor returns how many moment slots a channel count needs.
func momentsFor(chans int) int {
	if chans == 3 {
		return momentsRGB
	}
	return momentCount
}

// scratch holds the per-block working set. One scratch serves one goroutine, so
// the encoder allocates nothing inside the block loop.
type scratch struct {
	block   [16][4]int32
	rotated [16][4]int32

	members [3][16]uint8
	lens    [3]int

	moments [16][momentCount]float64
	total   [momentCount]float64

	scores [64]float64
	order  [64]uint8

	rank2    [64]uint8
	rank3    [64]uint8
	have2    bool
	have3    bool
	haveMoms bool
	// rankChans is 3 when the block's alpha is constant and 4 otherwise. A
	// constant channel contributes zero covariance, so dropping it from the
	// ranking cannot change the score, only the cost.
	rankChans int

	seeds            [64]seedEntry
	work             fitWork
	fitA, fitB, fitC lineFit
}

// seedEntry caches one subset's starting endpoint pair for the current block.
type seedEntry struct {
	key    int32
	e0, e1 [4]float64
}

// seedKey identifies one cached seed.
//
// The key must name the subset count as well as the partition number. Partition 5
// of the two-subset table and partition 5 of the three-subset table hold different
// texels, so a key that leaves the count out serves mode 2 the seed that mode 1
// computed. That fault is invisible in a single-mode encode and shows up as a
// larger partition budget producing a worse image.
func seedKey(subsets, partition, subset, channelKey int) int {
	return subsets<<10 | partition<<4 | subset<<1 | channelKey
}

// prepare drops the caches a previous block filled.
func (s *scratch) prepare() {
	s.have2 = false
	s.have3 = false
	s.haveMoms = false
	s.rankChans = 3
	first := s.block[0][3]
	for t := 1; t < 16; t++ {
		if s.block[t][3] != first {
			s.rankChans = 4
			break
		}
	}
	for i := range s.seeds {
		s.seeds[i].key = -1
	}
}

// seedFor returns the cached seed for one subset, computing it on a miss.
//
// The cache is direct mapped and holds one block's worth of keys. A collision
// only costs a recomputation, never a wrong answer, because the key is checked.
func (s *scratch) seedFor(members []uint8, lo, hi, key int, seed seedKind) ([4]float64, [4]float64) {
	slot := key & (len(s.seeds) - 1)
	entry := &s.seeds[slot]
	if entry.key == int32(key) {
		return entry.e0, entry.e1
	}
	var e0, e1 [4]float64
	seedEndpoints(&s.block, members, lo, hi, seed, &e0, &e1)
	entry.key = int32(key)
	entry.e0, entry.e1 = e0, e1
	return e0, e1
}

// buildMoments precomputes the per-texel raw moments the ranking sums.
func (s *scratch) buildMoments() {
	if s.haveMoms {
		return
	}
	s.total = [momentCount]float64{}
	n := momentsFor(s.rankChans)
	for t := 0; t < 16; t++ {
		var v [4]float64
		for c := 0; c < 4; c++ {
			v[c] = float64(s.block[t][c])
			s.moments[t][sumIdx[c]] = v[c]
		}
		for a := 0; a < 4; a++ {
			for b := a; b < 4; b++ {
				s.moments[t][prodIdx[a][b]] = v[a] * v[b]
			}
		}
		for i := 0; i < n; i++ {
			s.total[i] += s.moments[t][i]
		}
	}
	s.haveMoms = true
}

// rankPartitions orders every partition of one shape by how much error a two
// endpoint line must leave behind.
//
// The score is the residual variance of each subset: the total spread minus the
// spread the dominant axis explains. It ignores quantization and index
// granularity, so it is a ranking and not an error. The encoder then measures the
// top candidates for real.
//
// The dominant eigenvalue comes from one power step, not from a converged
// iteration. For a symmetric positive semidefinite covariance the quantity
// |Av|^2/(v.Av) is a lower bound on the largest eigenvalue, and it is exact when
// the axes line up with the channels. That is accurate enough to order 64
// candidates, and it costs a handful of multiplies instead of a matrix loop.
//
// One ranking per subset count serves every mode of that shape, and it always
// scores all four channels. Modes 0 to 3 store no alpha, so an alpha-aware
// ranking looks wrong for them at first sight. It is not: when alpha is constant
// its covariance is zero and the score is unchanged, and when alpha varies those
// modes lose to a mode that stores it.
// TestConstantAlphaDoesNotChangeTheRanking proves the first half on the
// arithmetic, and TestMorePartitionsNeverHurt covers the second.
func (s *scratch) rankPartitions(subsets int, sw *[4]float64) []uint8 {
	if subsets == 3 {
		if s.have3 {
			return s.rank3[:]
		}
	} else if s.have2 {
		return s.rank2[:]
	}
	s.buildMoments()
	n := momentsFor(s.rankChans)
	chans := s.rankChans

	if subsets == 2 {
		for p := 0; p < 64; p++ {
			table := &partition2[p]
			var one [momentCount]float64
			n1 := 0.0
			for t := 0; t < 16; t++ {
				if table[t] == 0 {
					continue
				}
				n1++
				addMoments(&one, &s.moments[t], n)
			}
			var zero [momentCount]float64
			for i := 0; i < n; i++ {
				zero[i] = s.total[i] - one[i]
			}
			s.scores[p] = residual(&zero, 16-n1, chans, sw) + residual(&one, n1, chans, sw)
			s.order[p] = uint8(p)
		}
	} else {
		for p := 0; p < 64; p++ {
			table := &partition3[p]
			var one, two [momentCount]float64
			n1, n2 := 0.0, 0.0
			for t := 0; t < 16; t++ {
				switch table[t] {
				case 1:
					n1++
					addMoments(&one, &s.moments[t], n)
				case 2:
					n2++
					addMoments(&two, &s.moments[t], n)
				}
			}
			var zero [momentCount]float64
			for i := 0; i < n; i++ {
				zero[i] = s.total[i] - one[i] - two[i]
			}
			s.scores[p] = residual(&zero, 16-n1-n2, chans, sw) +
				residual(&one, n1, chans, sw) + residual(&two, n2, chans, sw)
			s.order[p] = uint8(p)
		}
	}

	// Insertion sort keeps the order stable and allocates nothing. Sixty-four
	// entries make it cheaper than any allocating sort.
	for i := 1; i < 64; i++ {
		key := s.order[i]
		score := s.scores[key]
		j := i - 1
		for j >= 0 && s.scores[s.order[j]] > score {
			s.order[j+1] = s.order[j]
			j--
		}
		s.order[j+1] = key
	}

	if subsets == 3 {
		s.rank3 = s.order
		s.have3 = true
		return s.rank3[:]
	}
	s.rank2 = s.order
	s.have2 = true
	return s.rank2[:]
}

// addMoments accumulates the first n raw moments of one texel.
func addMoments(dst, src *[momentCount]float64, n int) {
	for i := 0; i < n; i++ {
		dst[i] += src[i]
	}
}

// residual returns the spread one subset leaves after its best line.
func residual(sums *[momentCount]float64, n float64, chans int, sw *[4]float64) float64 {
	if n <= 1 {
		return 0
	}
	var cov [4][4]float64
	for a := 0; a < chans; a++ {
		for b := a; b < chans; b++ {
			v := (sums[prodIdx[a][b]] - sums[sumIdx[a]]*sums[sumIdx[b]]/n) * sw[a] * sw[b]
			cov[a][b] = v
			cov[b][a] = v
		}
	}
	trace := 0.0
	top := 0
	for c := 0; c < chans; c++ {
		trace += cov[c][c]
		if cov[c][c] > cov[top][top] {
			top = c
		}
	}
	if cov[top][top] <= 0 {
		return 0
	}
	norm := 0.0
	for a := 0; a < chans; a++ {
		norm += cov[a][top] * cov[a][top]
	}
	lambda := norm / cov[top][top]
	if lambda > trace {
		return 0
	}
	return trace - lambda
}

// allTexels lists every texel of a single-subset block.
var allTexels = [16]uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// encodeBlock picks the best mode for one block and returns the packed bytes.
//
// The returned candidate always exists, because every enabled mode yields a
// finite error. Keep that property: a block loop with no fallback would emit an
// all-zero block, and an all-zero block is the reserved encoding that decodes to
// transparent black.
func encodeBlock(sc *scratch, cfg *config) candidate {
	sc.prepare()
	best := candidate{err: math.Inf(1)}

	// alphaPenalty is what a mode without stored alpha pays. Those modes decode
	// alpha as 255, so the cost is real and mode selection must see it.
	alphaPenalty := 0.0
	for t := 0; t < 16; t++ {
		d := float64(sc.block[t][3] - 255)
		alphaPenalty += cfg.weights[3] * d * d
	}

	for _, mode := range modeOrder {
		if cfg.modes&(1<<uint(mode)) == 0 {
			continue
		}
		var cand candidate
		switch mode {
		case 4, 5:
			cand = trySplitAlphaMode(mode, sc, cfg, best.err)
		case 6:
			cand = tryJointMode(mode, sc, cfg)
		case 7:
			cand = tryPartitionedMode(mode, sc, cfg, chanRGBALo, chanRGBAHi, 0, best.err)
		default:
			cand = tryPartitionedMode(mode, sc, cfg, chanRGBLo, chanRGBHi, alphaPenalty, best.err)
		}
		if cand.err < best.err {
			best = cand
		}
	}
	return best
}

// modeOrder is the order encodeBlock tries the modes.
//
// Mode 6 comes first because it is the strongest single mode, so it sets a tight
// error bound that lets every later mode abandon a hopeless partition early. The
// order changes no result, only the work: mode selection still keeps the lowest
// measured error.
var modeOrder = [8]int{6, 1, 3, 7, 5, 4, 2, 0}

// tryJointMode fits one subset over RGBA with a single index set. Mode 6 is the
// only mode shaped that way.
func tryJointMode(mode int, sc *scratch, cfg *config) candidate {
	m := modes[mode]
	members := allTexels[:]
	fit := fitLine(&sc.work, &sc.block, members, chanRGBALo, chanRGBAHi, &cfg.weights,
		m.indexBits, m.colorBits, m.pbit, cfg.rounds, cfg.exhaustiveParity, cfg.exactIndices, cfg.seed)
	if !cfg.skipAnchorFix {
		fit.fixAnchor(members, 0, m.indexBits)
	}
	var cand candidate
	cand.err = fit.err
	cand.spec.mode = mode
	cand.spec.parity = [6]int8{-1, -1, -1, -1, -1, -1}
	for k := 0; k < 2; k++ {
		cand.spec.stored[k] = fit.stored[k]
		cand.spec.parity[k] = fit.parity[k]
	}
	cand.spec.idx = fit.idx
	return cand
}

// trySplitAlphaMode fits mode 4 and mode 5, which give alpha its own endpoint
// pair and its own index set.
//
// Those modes are the right answer when alpha does not track colour, for example
// a leaf cut-out over a busy texture. A joint fit would drag the colour line
// towards the alpha edge and blur both.
//
// The rotation swaps alpha with one colour channel. The encoder swaps its input
// the same way, and the decoder swaps the output back, because a swap is its own
// inverse. The rotation also serves an opaque block: it moves one colour channel
// into the private index set, which is why a two-axis colour gradient encodes
// far better with it.
func trySplitAlphaMode(mode int, sc *scratch, cfg *config, bound float64) candidate {
	m := modes[mode]
	members := allTexels[:]
	best := candidate{err: math.Inf(1)}
	if bound < best.err {
		best.err = bound
	}
	found := false

	selectors := 1
	if m.indexSelBits > 0 {
		selectors = 2
	}
	rotations := cfg.rotations
	if m.rotationBits == 0 {
		rotations = 1
	}

	for rot := 0; rot < rotations; rot++ {
		src := &sc.block
		weights := cfg.weights
		if rot > 0 {
			sc.rotated = sc.block
			for t := 0; t < 16; t++ {
				sc.rotated[t][rot-1], sc.rotated[t][3] = sc.rotated[t][3], sc.rotated[t][rot-1]
			}
			weights[rot-1], weights[3] = weights[3], weights[rot-1]
			src = &sc.rotated
		}
		for sel := 0; sel < selectors; sel++ {
			// Index set 1 is indexBits wide and index set 2 is indexBits2 wide.
			// The selector decides which set drives colour.
			colorIdxBits, alphaIdxBits := m.indexBits, m.indexBits2
			if sel == 1 {
				colorIdxBits, alphaIdxBits = m.indexBits2, m.indexBits
			}
			colorFit := fitLine(&sc.work, src, members, chanRGBLo, chanRGBHi, &weights,
				colorIdxBits, m.colorBits, pbitNone, cfg.rounds, cfg.exhaustiveParity, cfg.exactIndices, cfg.seed)
			if colorFit.err >= best.err {
				// Alpha can only add error, so this rotation and selector
				// cannot win.
				continue
			}
			alphaFit := fitLine(&sc.work, src, members, chanALo, chanAHi, &weights,
				alphaIdxBits, m.alphaBits, pbitNone, cfg.rounds, cfg.exhaustiveParity, cfg.exactIndices, cfg.seed)
			if !cfg.skipAnchorFix {
				colorFit.fixAnchor(members, 0, colorIdxBits)
				alphaFit.fixAnchor(members, 0, alphaIdxBits)
			}
			err := colorFit.err + alphaFit.err
			if err >= best.err {
				continue
			}
			var cand candidate
			cand.err = err
			cand.spec.mode = mode
			cand.spec.rotation = rot
			cand.spec.indexSel = sel
			cand.spec.parity = [6]int8{-1, -1, -1, -1, -1, -1}
			for k := 0; k < 2; k++ {
				cand.spec.stored[k][0] = colorFit.stored[k][0]
				cand.spec.stored[k][1] = colorFit.stored[k][1]
				cand.spec.stored[k][2] = colorFit.stored[k][2]
				cand.spec.stored[k][3] = alphaFit.stored[k][3]
			}
			if sel == 0 {
				cand.spec.idx = colorFit.idx
				cand.spec.idx2 = alphaFit.idx
			} else {
				cand.spec.idx = alphaFit.idx
				cand.spec.idx2 = colorFit.idx
			}
			best = cand
			found = true
		}
	}
	if !found {
		// Nothing beat the bound, so report a loss rather than an empty spec.
		return candidate{err: math.Inf(1)}
	}
	return best
}

// tryPartitionedMode fits a mode with two or three subsets.
//
// alphaPenalty is the cost the mode pays for not storing alpha. Pass zero for a
// mode that stores it.
func tryPartitionedMode(mode int, sc *scratch, cfg *config, lo, hi int, alphaPenalty, bound float64) candidate {
	m := modes[mode]
	maxPartition := 1 << uint(m.partitionBits)

	var sw [4]float64
	for c := 0; c < 4; c++ {
		sw[c] = math.Sqrt(cfg.weights[c])
	}
	ranked := sc.rankPartitions(m.subsets, &sw)

	budget := cfg.partitions
	if budget < 1 {
		budget = 1
	}
	if budget > maxPartition {
		budget = maxPartition
	}

	// bound is the best error another mode already reached. A partition that
	// passes it cannot win, so the subset loop stops as soon as the running sum
	// does. best.err starts at the bound rather than at infinity, because a
	// partial sum from an abandoned partition must never look like a win.
	best := candidate{err: bound}
	found := false
	tried := 0
	fits := [3]*lineFit{&sc.fitA, &sc.fitB, &sc.fitC}
	channelKey := 0
	if hi == chanRGBAHi {
		channelKey = 1
	}

	for _, entry := range ranked {
		partition := int(entry)
		if partition >= maxPartition {
			continue
		}
		tried++

		table := partitionTable(m.subsets, partition)
		sc.lens = [3]int{}
		for t := 0; t < 16; t++ {
			sub := table[t]
			sc.members[sub][sc.lens[sub]] = uint8(t)
			sc.lens[sub]++
		}

		err := alphaPenalty
		complete := true
		for sub := 0; sub < m.subsets; sub++ {
			members := sc.members[sub][:sc.lens[sub]]
			key := seedKey(m.subsets, partition, sub, channelKey)
			e0, e1 := sc.seedFor(members, lo, hi, key, cfg.seed)
			*fits[sub] = fitLineFrom(&sc.work, &sc.block, members, lo, hi, &cfg.weights,
				m.indexBits, m.colorBits, m.pbit, cfg.rounds, cfg.exhaustiveParity, cfg.exactIndices, e0, e1)
			if !cfg.skipAnchorFix {
				fits[sub].fixAnchor(members, anchorTexel(m.subsets, partition, sub), m.indexBits)
			}
			err += fits[sub].err
			if err >= best.err {
				// The remaining subsets can only add error, so stop early. The
				// running sum is now partial, so it must not be compared again.
				complete = false
				break
			}
		}
		if complete && err < best.err {
			found = true
			best.err = err
			best.spec = blockSpec{mode: mode, partition: partition}
			best.spec.parity = [6]int8{-1, -1, -1, -1, -1, -1}
			for sub := 0; sub < m.subsets; sub++ {
				for k := 0; k < 2; k++ {
					best.spec.stored[sub*2+k] = fits[sub].stored[k]
					best.spec.parity[sub*2+k] = fits[sub].parity[k]
				}
				for _, t := range sc.members[sub][:sc.lens[sub]] {
					best.spec.idx[t] = fits[sub].idx[t]
				}
			}
		}
		if tried >= budget {
			break
		}
	}
	if !found {
		return candidate{err: math.Inf(1)}
	}
	return best
}
