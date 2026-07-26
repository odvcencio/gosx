package bc7

import "testing"

// TestModeBitBudget proves every mode spends exactly 128 bits.
//
// This is the cheapest guard against a wrong field width in the mode table. An
// endpoint field one bit too wide pushes the index data off the end of the
// block, and the decoder then reads zero for the last texels, which looks like a
// dark corner rather than a bug.
func TestModeBitBudget(t *testing.T) {
	for mode := 0; mode < 8; mode++ {
		m := modes[mode]
		limit := 1 << uint(m.partitionBits)
		for partition := 0; partition < limit; partition++ {
			if got := m.bits(mode, partition); got != blockBits {
				t.Errorf("mode %d partition %d: modeInfo.bits = %d, want %d",
					mode, partition, got, blockBits)
			}
			if got := packedBits(mode, partition); got != blockBits {
				t.Errorf("mode %d partition %d: packBlock wrote %d bits, want %d",
					mode, partition, got, blockBits)
			}
		}
	}
}

// TestDecodeReadsExactlyOneBlock proves the decoder consumes 128 bits for every
// mode and partition, so the encoder and the decoder agree on the field layout.
func TestDecodeReadsExactlyOneBlock(t *testing.T) {
	for mode := 0; mode < 8; mode++ {
		m := modes[mode]
		limit := 1 << uint(m.partitionBits)
		for partition := 0; partition < limit; partition++ {
			spec := blockSpec{mode: mode, partition: partition}
			block := packBlock(&spec)
			r := bitReader{buf: block[:]}
			consumeBlock(&r, mode, partition)
			if r.pos != blockBits {
				t.Errorf("mode %d partition %d: decoder read %d bits, want %d",
					mode, partition, r.pos, blockBits)
			}
		}
	}
}

// consumeBlock walks the fields of one block the way DecodeBlock does, and
// reports where the cursor lands.
func consumeBlock(r *bitReader, mode, partition int) {
	m := modes[mode]
	r.get(mode + 1)
	r.get(m.partitionBits)
	r.get(m.rotationBits)
	r.get(m.indexSelBits)
	count := m.subsets * 2
	r.get(0)
	for c := 0; c < 3; c++ {
		for e := 0; e < count; e++ {
			r.get(m.colorBits)
		}
	}
	if m.alphaBits > 0 {
		for e := 0; e < count; e++ {
			r.get(m.alphaBits)
		}
	}
	switch m.pbit {
	case pbitPerEndpoint:
		for e := 0; e < count; e++ {
			r.get(1)
		}
	case pbitShared:
		for s := 0; s < m.subsets; s++ {
			r.get(1)
		}
	}
	for texel := 0; texel < 16; texel++ {
		n := m.indexBits
		if isAnchor(m.subsets, partition, texel) {
			n--
		}
		r.get(n)
	}
	if m.indexBits2 > 0 {
		for texel := 0; texel < 16; texel++ {
			n := m.indexBits2
			if texel == 0 {
				n--
			}
			r.get(n)
		}
	}
}

// TestPartitionTablesUseEverySubset proves no partition wastes a subset.
//
// A partition that leaves a subset empty would leave two endpoints unused, and
// the encoder's per-subset fit would divide by a zero texel count.
func TestPartitionTablesUseEverySubset(t *testing.T) {
	for p := range partition2 {
		var seen [2]bool
		for _, s := range partition2[p] {
			if s > 1 {
				t.Fatalf("partition2[%d] holds subset %d, want 0 or 1", p, s)
			}
			seen[s] = true
		}
		if !seen[0] || !seen[1] {
			t.Errorf("partition2[%d] does not use both subsets: %v", p, partition2[p])
		}
		if partition2[p][0] != 0 {
			t.Errorf("partition2[%d] texel 0 is subset %d, want 0", p, partition2[p][0])
		}
	}
	for p := range partition3 {
		var seen [3]bool
		for _, s := range partition3[p] {
			if s > 2 {
				t.Fatalf("partition3[%d] holds subset %d, want 0 to 2", p, s)
			}
			seen[s] = true
		}
		if !seen[0] || !seen[1] || !seen[2] {
			t.Errorf("partition3[%d] does not use all three subsets: %v", p, partition3[p])
		}
		if partition3[p][0] != 0 {
			t.Errorf("partition3[%d] texel 0 is subset %d, want 0", p, partition3[p][0])
		}
	}
}

// TestAnchorTablesMatchPartitionTables cross-checks the anchor tables against
// the partition tables.
//
// The anchor texel of subset s must belong to subset s. That is a joint
// constraint on both tables, so a single mistyped entry in either one fails
// here. The check is worth more than it looks: the anchor is not the first texel
// of the subset, so nothing else in the package would catch a slip.
func TestAnchorTablesMatchPartitionTables(t *testing.T) {
	for p := range partition2 {
		a := anchor2[p]
		if a == 0 {
			t.Errorf("anchor2[%d] is 0, which is already subset 0's anchor", p)
		}
		if got := partition2[p][a]; got != 1 {
			t.Errorf("anchor2[%d] = %d, but partition2[%d][%d] is subset %d, want 1",
				p, a, p, a, got)
		}
	}
	for p := range partition3 {
		a1, a2 := anchor3a[p], anchor3b[p]
		if a1 == 0 || a2 == 0 || a1 == a2 {
			t.Errorf("anchor3a[%d] = %d and anchor3b[%d] = %d must differ and must not be 0",
				p, a1, p, a2)
		}
		if got := partition3[p][a1]; got != 1 {
			t.Errorf("anchor3a[%d] = %d, but partition3[%d][%d] is subset %d, want 1",
				p, a1, p, a1, got)
		}
		if got := partition3[p][a2]; got != 2 {
			t.Errorf("anchor3b[%d] = %d, but partition3[%d][%d] is subset %d, want 2",
				p, a2, p, a2, got)
		}
	}
}

// TestAnchorIsNotTheFirstTexelOfItsSubset records the trap this format sets.
//
// A reader who guesses that the anchor is the first texel of the subset writes a
// plausible encoder with one wrong texel per block. Partition 0 of the
// two-subset table is the counter-example: subset 1 starts at texel 2, and its
// anchor is texel 15.
func TestAnchorIsNotTheFirstTexelOfItsSubset(t *testing.T) {
	if anchor2[0] != 15 {
		t.Fatalf("anchor2[0] = %d, want 15", anchor2[0])
	}
	first := -1
	for texel, sub := range partition2[0] {
		if sub == 1 {
			first = texel
			break
		}
	}
	if first != 2 {
		t.Fatalf("subset 1 of partition2[0] starts at texel %d, want 2", first)
	}
	if int(anchor2[0]) == first {
		t.Fatal("the test premise is broken: the anchor equals the first texel")
	}
}

// TestPartitionTableKnownEntries spot-checks rows whose shape is stated in the
// BC7 partition figures.
//
// Row 0 of the two-subset table splits the block into left and right column
// pairs. Row 32 alternates columns. Row 13 of the three-subset table gives each
// column of the block a subset, with the last two columns sharing subset 2.
// Row 15 of the two-subset table puts only the bottom row in subset 1.
func TestPartitionTableKnownEntries(t *testing.T) {
	cases := []struct {
		name string
		got  [16]uint8
		want [16]uint8
	}{
		{"partition2[0] column pairs", partition2[0],
			[16]uint8{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1}},
		{"partition2[15] bottom row", partition2[15],
			[16]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1}},
		{"partition2[32] alternating columns", partition2[32],
			[16]uint8{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1}},
		{"partition3[13] column bands", partition3[13],
			[16]uint8{0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2}},
		{"partition3[8] row bands", partition3[8],
			[16]uint8{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2}},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s:\n got %v\nwant %v", c.name, c.got, c.want)
		}
	}
}

// TestWeightTablesAreSymmetric proves the endpoint swap the anchor rule needs is
// free.
//
// Swapping the endpoint pair and mirroring every index must decode to the same
// colours. That holds only when weights[max-i] equals 64 minus weights[i].
func TestWeightTablesAreSymmetric(t *testing.T) {
	for _, bits := range []int{2, 3, 4} {
		w := weightsFor(bits)
		last := len(w) - 1
		if w[0] != 0 || w[last] != 64 {
			t.Errorf("%d-bit weights run %d to %d, want 0 to 64", bits, w[0], w[last])
		}
		for i := range w {
			if w[last-i] != 64-w[i] {
				t.Errorf("%d-bit weights are not symmetric at %d: %d and %d",
					bits, i, w[i], w[last-i])
			}
		}
		for i := 1; i < len(w); i++ {
			if w[i] <= w[i-1] {
				t.Errorf("%d-bit weights are not increasing at %d", bits, i)
			}
		}
	}
}

// TestUnquantize checks the endpoint widening rule at its edges.
//
// Zero must stay 0 and full scale must reach 255. A plain left shift would give
// 248 for the brightest 5-bit endpoint, so white would never be white.
func TestUnquantize(t *testing.T) {
	cases := []struct {
		value uint32
		bits  int
		want  uint32
	}{
		{0, 5, 0},
		{31, 5, 255},  // 31<<3 = 248, then 248 | 248>>5 = 248 | 7 = 255
		{16, 5, 132},  // 16<<3 = 128, then 128 | 128>>5 = 128 | 4 = 132
		{0, 7, 0},     //
		{127, 7, 255}, // 127<<1 = 254, then 254 | 254>>7 = 254 | 1 = 255
		{64, 7, 129},  // 64<<1 = 128, then 128 | 128>>7 = 128 | 1 = 129
		{7, 4, 119},   // 7<<4 = 112, then 112 | 112>>4 = 112 | 7 = 119
		{15, 4, 255},
		{200, 8, 200}, // 8 bits pass through
	}
	for _, c := range cases {
		if got := unquantize(c.value, c.bits); got != c.want {
			t.Errorf("unquantize(%d, %d) = %d, want %d", c.value, c.bits, got, c.want)
		}
	}
}

// TestInterpolate checks the blend rule against values worked out by hand.
func TestInterpolate(t *testing.T) {
	cases := []struct {
		e0, e1, weight uint32
		want           uint8
	}{
		{0, 255, 0, 0},
		{0, 255, 64, 255},
		{0, 255, 32, 128},  // (32*0 + 32*255 + 32) >> 6 = 8192 >> 6 = 128
		{0, 255, 4, 16},    // (60*0 + 4*255 + 32) >> 6 = 1052 >> 6 = 16
		{254, 255, 4, 254}, // (60*254 + 4*255 + 32) >> 6 = 16292 >> 6 = 254
		{100, 100, 21, 100},
	}
	for _, c := range cases {
		if got := interpolate(c.e0, c.e1, c.weight); got != c.want {
			t.Errorf("interpolate(%d, %d, %d) = %d, want %d", c.e0, c.e1, c.weight, got, c.want)
		}
	}
}

// TestQuantizeTablesRoundTrip proves the quantization tables really pick the
// nearest reachable endpoint, including under a forced parity bit.
func TestQuantizeTablesRoundTrip(t *testing.T) {
	for prec := 4; prec <= 8; prec++ {
		for target := 0; target < 256; target++ {
			stored := quantizeEndpoint(float64(target), prec, -1)
			got := widenEndpoint(stored, prec, -1)
			best := bruteNearest(target, prec, -1)
			if int(got) != best {
				t.Fatalf("prec %d target %d: got %d, nearest is %d", prec, target, got, best)
			}
		}
	}
	for prec := 4; prec <= 7; prec++ {
		for parity := int8(0); parity < 2; parity++ {
			for target := 0; target < 256; target++ {
				stored := quantizeEndpoint(float64(target), prec, parity)
				got := widenEndpoint(stored, prec, parity)
				best := bruteNearest(target, prec, parity)
				if int(got) != best {
					t.Fatalf("prec %d parity %d target %d: got %d, nearest is %d",
						prec, parity, target, got, best)
				}
				if stored >= 1<<uint(prec) {
					t.Fatalf("prec %d parity %d target %d: stored %d overflows the field",
						prec, parity, target, stored)
				}
			}
		}
	}
}

// bruteNearest finds the reachable 8-bit endpoint nearest to a target by trying
// every stored value. It is the slow reference for the lookup tables.
func bruteNearest(target, prec int, parity int8) int {
	count := 1 << uint(prec)
	best, bestErr := 0, 1<<30
	for v := 0; v < count; v++ {
		got := int(widenEndpoint(uint32(v), prec, parity))
		d := got - target
		if d < 0 {
			d = -d
		}
		if d < bestErr {
			best, bestErr = got, d
		}
	}
	return best
}
