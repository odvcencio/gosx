package bc7

import (
	"math/rand"
	"testing"
)

// expectFromSpec computes the texels a block spec must decode to.
//
// It applies the widening rule and the interpolation rule directly to the spec,
// and it never touches the bitstream. So a disagreement with DecodeBlock is a
// bit layout fault in packBlock or in the decoder's field reading, which is
// exactly the fault class this test exists to find.
func expectFromSpec(s *blockSpec) [16][4]uint8 {
	m := modes[s.mode]
	count := m.subsets * 2
	parity := func(int) int8 { return -1 }
	switch m.pbit {
	case pbitPerEndpoint:
		parity = func(e int) int8 { return s.parity[e] & 1 }
	case pbitShared:
		parity = func(e int) int8 { return s.parity[e/2*2] & 1 }
	}

	var ep [6][4]uint32
	for e := 0; e < count; e++ {
		p := parity(e)
		for c := 0; c < 3; c++ {
			ep[e][c] = widenEndpoint(s.stored[e][c], m.colorBits, p)
		}
		if m.alphaBits > 0 {
			ep[e][3] = widenEndpoint(s.stored[e][3], m.alphaBits, p)
		} else {
			ep[e][3] = 255
		}
	}

	table := partitionTable(m.subsets, s.partition)
	w1 := weightsFor(m.indexBits)
	w2 := weightsFor(m.indexBits2)

	var out [16][4]uint8
	for t := 0; t < 16; t++ {
		subset := 0
		if m.subsets > 1 {
			subset = int(table[t])
		}
		e0, e1 := &ep[subset*2], &ep[subset*2+1]
		colorW := w1[s.idx[t]]
		alphaW := colorW
		if m.indexBits2 > 0 {
			if s.indexSel == 0 {
				alphaW = w2[s.idx2[t]]
			} else {
				colorW = w2[s.idx2[t]]
				alphaW = w1[s.idx[t]]
			}
		}
		var px [4]uint8
		for c := 0; c < 3; c++ {
			px[c] = interpolate(e0[c], e1[c], colorW)
		}
		px[3] = interpolate(e0[3], e1[3], alphaW)
		switch s.rotation {
		case 1:
			px[0], px[3] = px[3], px[0]
		case 2:
			px[1], px[3] = px[3], px[1]
		case 3:
			px[2], px[3] = px[3], px[2]
		}
		out[t] = px
	}
	return out
}

// randomSpec builds a legal block spec for one mode.
func randomSpec(rng *rand.Rand, mode int) blockSpec {
	m := modes[mode]
	s := blockSpec{mode: mode}
	if m.partitionBits > 0 {
		s.partition = rng.Intn(1 << uint(m.partitionBits))
	}
	if m.rotationBits > 0 {
		s.rotation = rng.Intn(4)
	}
	if m.indexSelBits > 0 {
		s.indexSel = rng.Intn(2)
	}
	count := m.subsets * 2
	for e := 0; e < 6; e++ {
		s.parity[e] = -1
	}
	for e := 0; e < count; e++ {
		for c := 0; c < 3; c++ {
			s.stored[e][c] = uint32(rng.Intn(1 << uint(m.colorBits)))
		}
		if m.alphaBits > 0 {
			s.stored[e][3] = uint32(rng.Intn(1 << uint(m.alphaBits)))
		}
	}
	switch m.pbit {
	case pbitPerEndpoint:
		for e := 0; e < count; e++ {
			s.parity[e] = int8(rng.Intn(2))
		}
	case pbitShared:
		for sub := 0; sub < m.subsets; sub++ {
			bit := int8(rng.Intn(2))
			s.parity[sub*2] = bit
			s.parity[sub*2+1] = bit
		}
	}
	for t := 0; t < 16; t++ {
		bits := m.indexBits
		if isAnchor(m.subsets, s.partition, t) {
			bits--
		}
		s.idx[t] = uint8(rng.Intn(1 << uint(bits)))
	}
	if m.indexBits2 > 0 {
		for t := 0; t < 16; t++ {
			bits := m.indexBits2
			if t == 0 {
				bits--
			}
			s.idx2[t] = uint8(rng.Intn(1 << uint(bits)))
		}
	}
	return s
}

// TestPackAndDecodeAgreeOnEveryMode drives random legal specs through the packer
// and the decoder.
//
// The expected texels come from expectFromSpec, which reads the spec and not the
// bytes. So the test fails when a field lands at the wrong bit offset, even
// though a round trip through the encoder would still look self-consistent.
func TestPackAndDecodeAgreeOnEveryMode(t *testing.T) {
	rng := rand.New(rand.NewSource(20260726))
	for mode := 0; mode < 8; mode++ {
		for trial := 0; trial < 400; trial++ {
			spec := randomSpec(rng, mode)
			block := packBlock(&spec)
			got := DecodeBlock(block[:])
			want := expectFromSpec(&spec)
			if got != want {
				t.Fatalf("mode %d partition %d rotation %d indexSel %d:\n got %v\nwant %v\nblock %x",
					mode, spec.partition, spec.rotation, spec.indexSel, got, want, block)
			}
			if BlockMode(block[:]) != mode {
				t.Fatalf("mode %d: BlockMode read %d", mode, BlockMode(block[:]))
			}
		}
	}
}

// TestPackDropsNothingIntoTheModeBits proves the mode field is unary and that no
// later field writes back over it.
//
// A field written at a negative offset, or a partition value wider than its
// field, would corrupt the mode bits and change the block's mode. That failure
// looks like random mode selection, which is hard to spot in a histogram.
func TestPackDropsNothingIntoTheModeBits(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for mode := 0; mode < 8; mode++ {
		for trial := 0; trial < 200; trial++ {
			spec := randomSpec(rng, mode)
			block := packBlock(&spec)
			for bit := 0; bit < mode; bit++ {
				if block[bit>>3]>>uint(bit&7)&1 != 0 {
					t.Fatalf("mode %d: bit %d of the unary mode field is set", mode, bit)
				}
			}
			if block[mode>>3]>>uint(mode&7)&1 == 0 {
				t.Fatalf("mode %d: the terminating mode bit is clear", mode)
			}
		}
	}
}

// TestParityBitChangesTheEndpointLowBit is the direct test of the parity rule.
//
// A parity bit joins the endpoint as a new least significant bit after
// quantization. So at 7 stored colour bits plus one parity bit, the widened
// endpoint is exactly (stored<<1)|parity, and its low bit is the parity bit.
// Flipping the parity bit must move the endpoint by exactly one code.
//
// An implementation that prepends the parity bit, or that widens before
// appending it, still produces a plausible image. It just stores the wrong
// endpoints, off by up to two codes, everywhere.
func TestParityBitChangesTheEndpointLowBit(t *testing.T) {
	for stored := uint32(0); stored < 128; stored++ {
		even := widenEndpoint(stored, 7, 0)
		odd := widenEndpoint(stored, 7, 1)
		if even != stored*2 {
			t.Fatalf("widenEndpoint(%d, 7, 0) = %d, want %d", stored, even, stored*2)
		}
		if odd != stored*2+1 {
			t.Fatalf("widenEndpoint(%d, 7, 1) = %d, want %d", stored, odd, stored*2+1)
		}
	}

	// Mode 6 stores 7 colour bits and 7 alpha bits with one parity bit per
	// endpoint. Build one block, flip P0, and check the whole subset moves.
	base := blockSpec{mode: 6, parity: [6]int8{0, 0, -1, -1, -1, -1}}
	for c := 0; c < 4; c++ {
		base.stored[0][c] = 40
		base.stored[1][c] = 100
	}
	flipped := base
	flipped.parity[0] = 1

	before := DecodeBlock(sliceOf(packBlock(&base)))
	after := DecodeBlock(sliceOf(packBlock(&flipped)))
	// Index 0 selects endpoint 0 exactly, so texel 0 shows the parity change
	// with no interpolation in the way.
	if before[0][0] != 80 || after[0][0] != 81 {
		t.Fatalf("texel 0 red went from %d to %d, want 80 then 81", before[0][0], after[0][0])
	}
}

// TestSharedParityAppliesToBothEndpointsOfASubset checks the mode 1 layout.
//
// Mode 1 stores one parity bit per subset, not per endpoint. Both endpoints of a
// subset must take it, and the other subset must not move.
func TestSharedParityAppliesToBothEndpointsOfASubset(t *testing.T) {
	spec := blockSpec{mode: 1, partition: 0}
	spec.parity = [6]int8{0, 0, 0, 0, -1, -1}
	for e := 0; e < 4; e++ {
		for c := 0; c < 3; c++ {
			spec.stored[e][c] = 20
		}
	}
	// Give subset 0 parity 1 and subset 1 parity 0.
	spec.parity[0], spec.parity[1] = 1, 1
	got := DecodeBlock(sliceOf(packBlock(&spec)))

	// 6 stored bits plus one parity bit is 7 effective bits.
	want0 := unquantize(20<<1|1, 7)
	want1 := unquantize(20<<1|0, 7)
	if want0 == want1 {
		t.Fatal("the test premise is broken: both parities widen to the same value")
	}
	for texel := 0; texel < 16; texel++ {
		want := want1
		if partition2[0][texel] == 0 {
			want = want0
		}
		if uint32(got[texel][0]) != want {
			t.Errorf("texel %d red = %d, want %d", texel, got[texel][0], want)
		}
	}
}

func sliceOf(block [BlockBytes]byte) []byte {
	out := make([]byte, BlockBytes)
	copy(out, block[:])
	return out
}
