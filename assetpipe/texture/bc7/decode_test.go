package bc7

import "testing"

// This file validates the bit layout against the specification, not against the
// encoder.
//
// Every block below is written out bit by bit by hand from the BC7 field order
// and field widths, and every expected texel is worked out by hand from the
// widening rule and the interpolation rule. A comment above each case shows the
// arithmetic, so a reader can re-derive it without running the code.
//
// A round trip through the encoder and this decoder would prove only that the
// pair agrees with itself. These vectors are the part that proves the pair
// agrees with the format.
//
// Source of the rules: the BC7 block decode description that appears in the
// Direct3D 11 "BC7 Format" reference and, with the same content, in the Vulkan
// specification appendix on BC7 compressed formats. No published byte-level test
// vector set exists for BC7 in either document, so the vectors here are derived
// rather than quoted. The derivation is written out so it can be audited.

// solid builds the expected texel set where every texel holds the same colour.
func solid(px [4]uint8) [16][4]uint8 {
	var out [16][4]uint8
	for t := range out {
		out[t] = px
	}
	return out
}

func checkDecode(t *testing.T, name string, block []byte, want [16][4]uint8) {
	t.Helper()
	if len(block) != BlockBytes {
		t.Fatalf("%s: block holds %d bytes, want %d", name, len(block), BlockBytes)
	}
	got := DecodeBlock(block)
	for texel := 0; texel < 16; texel++ {
		if got[texel] != want[texel] {
			t.Errorf("%s: texel %d = %v, want %v", name, texel, got[texel], want[texel])
		}
	}
}

// TestDecodeReservedModeIsZero checks the reserved encoding.
//
// A block whose first 8 bits are all zero selects no mode. The specification
// calls that reserved and requires zero in every channel, alpha included.
func TestDecodeReservedModeIsZero(t *testing.T) {
	var block [BlockBytes]byte
	checkDecode(t, "all zero bytes", block[:], solid([4]uint8{0, 0, 0, 0}))
	if BlockMode(block[:]) != -1 {
		t.Errorf("BlockMode of a reserved block = %d, want -1", BlockMode(block[:]))
	}
}

// TestDecodeMode6HandVector checks mode 6, its parity bits, and its 4-bit
// indices.
//
// Field order for mode 6: 7 mode bits, then R0 R1 G0 G1 B0 B1 A0 A1 at 7 bits
// each, then P0 P1, then the indices. Texel 0 is the anchor, so it stores 3 bits
// and the other fifteen store 4.
//
// The vector sets R0=G0=B0=A0=127... no: it sets
//
//	R0=G0=B0=0, R1=G1=B1=127, A0=A1=127, P0=0, P1=1
//
// so endpoint 0 widens to (0,0,0,254) and endpoint 1 widens to
// (255,255,255,255):
//
//	R0 = (0<<1)|0   = 0   -> 8 bits already, stays 0
//	R1 = (127<<1)|1 = 255 -> stays 255
//	A0 = (127<<1)|0 = 254 -> stays 254
//	A1 = (127<<1)|1 = 255 -> stays 255
//
// Index 0 selects weight 0 and index 15 selects weight 64, so texel 0 decodes to
// endpoint 0 and every other texel decodes to endpoint 1.
//
// Bits set, counted from byte 0 bit 0: 6 (the mode bit), 14 to 20 (R1),
// 28 to 34 (G1), 42 to 48 (B1), 49 to 55 (A0), 56 to 62 (A1), 64 (P1),
// 68 to 127 (the fifteen 4-bit indices, all 15). Bit 63 is P0 and stays clear.
func TestDecodeMode6HandVector(t *testing.T) {
	block := []byte{
		0x40, 0xC0, 0x1F, 0xF0, 0x07, 0xFC, 0xFF, 0x7F,
		0xF1, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}
	want := solid([4]uint8{255, 255, 255, 255})
	want[0] = [4]uint8{0, 0, 0, 254}
	checkDecode(t, "mode 6 endpoints", block, want)
	if BlockMode(block) != 6 {
		t.Errorf("BlockMode = %d, want 6", BlockMode(block))
	}
}

// TestDecodeMode6Interpolation checks the blend arithmetic on the same
// endpoints.
//
// The vector keeps the endpoints of the case above and sets every non-anchor
// index to 1. Index 1 of the 4-bit table is weight 4, so
//
//	R = (60*0   + 4*255 + 32) >> 6 = 1052  >> 6 = 16
//	A = (60*254 + 4*255 + 32) >> 6 = 16292 >> 6 = 254
//
// Only bytes 8 to 15 change: each 4-bit index field holds 1, so each byte holds
// two set bits at positions 0 and 4.
func TestDecodeMode6Interpolation(t *testing.T) {
	block := []byte{
		0x40, 0xC0, 0x1F, 0xF0, 0x07, 0xFC, 0xFF, 0x7F,
		0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
	}
	want := solid([4]uint8{16, 16, 16, 254})
	want[0] = [4]uint8{0, 0, 0, 254}
	checkDecode(t, "mode 6 interpolation", block, want)
}

// TestDecodeMode5HandVector checks mode 5 and its two independent index sets.
//
// Field order for mode 5: 6 mode bits, 2 rotation bits, then R0 R1 G0 G1 B0 B1
// at 7 bits each, then A0 A1 at 8 bits each, then the colour indices, then the
// alpha indices. Both index sets are 2 bits wide and both anchor at texel 0.
//
// The vector sets rotation 0, R0=64, R1=127, every green and blue endpoint to 0,
// A0=0 and A1=255:
//
//	R0 = unquantize(64, 7)  = 128 | (128>>7) = 129
//	R1 = unquantize(127, 7) = 254 | (254>>7) = 255
//	A0 = 0 and A1 = 255, because 8-bit alpha needs no widening
//
// Colour index 0 and alpha index 0 for texel 0; colour index 3 and alpha index 3
// for the rest. Weight 0 picks endpoint 0 and weight 64 picks endpoint 1.
//
// Bits set: 5 (mode), 14 (the top bit of R0=64), 15 to 21 (R1), 58 to 65 (A1),
// 67 to 96 (the fifteen 2-bit colour indices), 98 to 127 (the fifteen 2-bit
// alpha indices). Bits 66 and 97 are the two 1-bit anchor fields and stay clear.
func TestDecodeMode5HandVector(t *testing.T) {
	block := []byte{
		0x20, 0xC0, 0x3F, 0x00, 0x00, 0x00, 0x00, 0xFC,
		0xFB, 0xFF, 0xFF, 0xFF, 0xFD, 0xFF, 0xFF, 0xFF,
	}
	want := solid([4]uint8{255, 0, 0, 255})
	want[0] = [4]uint8{129, 0, 0, 0}
	checkDecode(t, "mode 5 split alpha", block, want)
	if BlockMode(block) != 5 {
		t.Errorf("BlockMode = %d, want 5", BlockMode(block))
	}
}

// TestDecodeMode5Rotation checks the channel rotation.
//
// Rotation 1 swaps red and alpha after interpolation. The vector keeps the mode
// 5 case above but sets rotation 1 and A1 to 128, so the swap is visible:
//
//	before the swap: texel 0 is (129,0,0,0) and the rest are (255,0,0,128)
//	after the swap:  texel 0 is (0,0,0,129) and the rest are (128,0,0,255)
//
// Byte 0 gains bit 6, the low rotation bit. A1=128 sets only bit 65, its top
// bit, so byte 7 loses its A1 bits and byte 8 keeps bit 1.
func TestDecodeMode5Rotation(t *testing.T) {
	block := []byte{
		0x60, 0xC0, 0x3F, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xFA, 0xFF, 0xFF, 0xFF, 0xFD, 0xFF, 0xFF, 0xFF,
	}
	want := solid([4]uint8{128, 0, 0, 255})
	want[0] = [4]uint8{0, 0, 0, 129}
	checkDecode(t, "mode 5 rotation 1", block, want)
}

// TestDecodeMode4HandVector checks mode 4, its 6-bit alpha, and the index
// selection bit.
//
// Field order for mode 4: 5 mode bits, 2 rotation bits, 1 index selection bit,
// then R0 R1 G0 G1 B0 B1 at 5 bits each, then A0 A1 at 6 bits each, then the
// 2-bit index set, then the 3-bit index set. Index selection 0 sends the 2-bit
// set to colour and the 3-bit set to alpha.
//
// The vector sets rotation 0, selection 0, R0=0, R1=31, green and blue to 0,
// A0=0, A1=63:
//
//	R1 = unquantize(31, 5) = 248 | (248>>5) = 255
//	A1 = unquantize(63, 6) = 252 | (252>>6) = 255
//
// Every texel but texel 0 takes colour index 3 and alpha index 7, both of which
// are weight 64.
//
// Bits set: 4 (mode), 13 to 17 (R1), 44 to 49 (A1), 51 to 80 (the fifteen 2-bit
// colour indices), 83 to 127 (the fifteen 3-bit alpha indices). Bit 50 is the
// 1-bit colour anchor and bits 81 to 82 are the 2-bit alpha anchor.
func TestDecodeMode4HandVector(t *testing.T) {
	block := []byte{
		0x10, 0xE0, 0x03, 0x00, 0x00, 0xF0, 0xFB, 0xFF,
		0xFF, 0xFF, 0xF9, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}
	want := solid([4]uint8{255, 0, 0, 255})
	want[0] = [4]uint8{0, 0, 0, 0}
	checkDecode(t, "mode 4 index selection 0", block, want)
	if BlockMode(block) != 4 {
		t.Errorf("BlockMode = %d, want 4", BlockMode(block))
	}
}

// TestDecodeMode1HandVector checks mode 1, its shared parity bits, and its
// channel-major endpoint order.
//
// Field order for mode 1: 2 mode bits, 6 partition bits, then R0 R1 R2 R3 at 6
// bits each, then G0 to G3, then B0 to B3, then the two shared parity bits, then
// the 3-bit indices. Endpoints 0 and 1 belong to subset 0 and share P0.
// Endpoints 2 and 3 belong to subset 1 and share P1.
//
// The vector picks partition 0, which is
//
//	0 0 1 1 / 0 0 1 1 / 0 0 1 1 / 0 0 1 1
//
// and sets R2=R3=63 with every other colour value 0, P0=0 and P1=1:
//
//	subset 0: R = (0<<1)|0  = 0  -> unquantize(0, 7)  = 0
//	subset 1: R = (63<<1)|1 = 127 -> unquantize(127, 7) = 255
//	subset 1: G = B = (0<<1)|1 = 1 -> unquantize(1, 7) = 2
//
// Every index is 0, so each texel takes its own subset's endpoint 0. Mode 1
// stores no alpha, so alpha decodes to 255.
//
// Bits set: 1 (mode), 20 to 31 (R2 and R3), 81 (P1). The partition is 0 and
// every index is 0, so the rest is clear.
func TestDecodeMode1HandVector(t *testing.T) {
	block := []byte{
		0x02, 0x00, 0xF0, 0xFF, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	var want [16][4]uint8
	for texel := 0; texel < 16; texel++ {
		if partition2[0][texel] == 0 {
			want[texel] = [4]uint8{0, 0, 0, 255}
		} else {
			want[texel] = [4]uint8{255, 2, 2, 255}
		}
	}
	checkDecode(t, "mode 1 shared parity", block, want)
	if BlockMode(block) != 1 {
		t.Errorf("BlockMode = %d, want 1", BlockMode(block))
	}
}

// TestDecodeMode1AnchorFieldWidth is the direct test of the anchor rule.
//
// It picks partition 17, whose subset map is
//
//	0 1 1 1 / 0 0 0 1 / 0 0 0 0 / 0 0 0 0
//
// and whose subset 1 anchor is texel 2, not texel 1. So the index fields run
//
//	texel 0: 2 bits, the subset 0 anchor
//	texel 1: 3 bits
//	texel 2: 2 bits, the subset 1 anchor
//	texels 3 to 15: 3 bits each
//
// A decoder that reads 3 bits at texel 2 shifts every later field by one bit and
// corrupts most of the block. A decoder that anchors at the first texel of the
// subset, texel 1, shifts them the other way. Either mistake fails here.
//
// Endpoints: R1=R3=63 and every other colour value 0, with both parity bits 0,
// so both subsets run red from 0 to unquantize(126, 7) = 253.
//
// Indices: texel 0 is 1, texel 1 is 7, texel 2 is 3, and the rest are 0. The
// 3-bit weights are 0, 9, 18, 27, 37, 46, 55, 64, so
//
//	texel 0: (55*0 + 9*253  + 32) >> 6 = 2309 >> 6 = 36
//	texel 1: weight 64                              = 253
//	texel 2: (37*0 + 27*253 + 32) >> 6 = 6863 >> 6 = 107
//
// Bits set: 1 (mode), 2 and 6 (partition 17), 14 to 19 (R1), 26 to 31 (R3),
// 82 (texel 0 index 1), 84 to 86 (texel 1 index 7), 87 to 88 (texel 2 index 3).
func TestDecodeMode1AnchorFieldWidth(t *testing.T) {
	block := []byte{
		0x46, 0xC0, 0x0F, 0xFC, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xF4, 0x01, 0x00, 0x00, 0x00, 0x00,
	}
	want := solid([4]uint8{0, 0, 0, 255})
	want[0] = [4]uint8{36, 0, 0, 255}
	want[1] = [4]uint8{253, 0, 0, 255}
	want[2] = [4]uint8{107, 0, 0, 255}
	checkDecode(t, "mode 1 anchor field width", block, want)
}

// TestDecodeMode0HandVector checks mode 0, its three subsets, and its 4-bit
// endpoints with per-endpoint parity.
//
// Field order for mode 0: 1 mode bit, 4 partition bits, then R0 to R5 at 4 bits
// each, then G0 to G5, then B0 to B5, then P0 to P5, then the 3-bit indices.
//
// The vector picks three-subset partition 0, which is
//
//	0 0 1 1 / 0 0 1 1 / 0 2 2 1 / 2 2 2 2
//
// and sets R4=R5=15 with every other colour value 0, P2=P3=1 and the rest 0:
//
//	subset 0: every channel (0<<1)|0  = 0  -> unquantize(0, 5)  = 0
//	subset 1: every channel (0<<1)|1  = 1  -> unquantize(1, 5)  = 8
//	subset 2: red (15<<1)|0 = 30 -> unquantize(30, 5) = 240 | 7 = 247
//
// Every index is 0, so each texel takes its subset's endpoint 0. Mode 0 stores no
// alpha, so alpha decodes to 255.
//
// Bits set: 0 (mode), 21 to 28 (R4 and R5), 79 and 80 (P2 and P3).
func TestDecodeMode0HandVector(t *testing.T) {
	block := []byte{
		0x01, 0x00, 0xE0, 0x1F, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x80, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	bySubset := [3][4]uint8{
		{0, 0, 0, 255},
		{8, 8, 8, 255},
		{247, 0, 0, 255},
	}
	var want [16][4]uint8
	for texel := 0; texel < 16; texel++ {
		want[texel] = bySubset[partition3[0][texel]]
	}
	checkDecode(t, "mode 0 three subsets", block, want)
	if BlockMode(block) != 0 {
		t.Errorf("BlockMode = %d, want 0", BlockMode(block))
	}
}

// TestDecodeMode2HandVector checks mode 2, which stores no parity bit at all.
//
// Field order for mode 2: 3 mode bits, 6 partition bits, then R0 to R5 at 5 bits
// each, then G0 to G5, then B0 to B5, then the 2-bit indices.
//
// The vector picks three-subset partition 8, which splits the block into a top
// half and two quarter rows:
//
//	0 0 0 0 / 0 0 0 0 / 1 1 1 1 / 2 2 2 2
//
// and sets R2=R3=31 and B4=B5=31, everything else 0:
//
//	unquantize(31, 5) = 248 | (248>>5) = 255
//
// Every index is 0, so subset 0 decodes black, subset 1 decodes pure red and
// subset 2 decodes pure blue. Mode 2 stores no alpha, so alpha decodes to 255.
//
// Bits set: 2 (mode), 6 (partition 8), 19 to 28 (R2 and R3), 89 to 98 (B4, B5).
func TestDecodeMode2HandVector(t *testing.T) {
	block := []byte{
		0x44, 0x00, 0xF8, 0x1F, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xFE, 0x07, 0x00, 0x00, 0x00,
	}
	bySubset := [3][4]uint8{
		{0, 0, 0, 255},
		{255, 0, 0, 255},
		{0, 0, 255, 255},
	}
	var want [16][4]uint8
	for texel := 0; texel < 16; texel++ {
		want[texel] = bySubset[partition3[8][texel]]
	}
	checkDecode(t, "mode 2 no parity", block, want)
	if BlockMode(block) != 2 {
		t.Errorf("BlockMode = %d, want 2", BlockMode(block))
	}
}

// TestDecodeMode3HandVector checks mode 3, whose 7 colour bits plus one parity
// bit reach the full 8 bits, so no widening happens.
//
// Field order for mode 3: 4 mode bits, 6 partition bits, then R0 to R3 at 7 bits
// each, then G0 to G3, then B0 to B3, then P0 to P3, then the 2-bit indices.
//
// The vector picks two-subset partition 15, which puts only the bottom row in
// subset 1:
//
//	0 0 0 0 / 0 0 0 0 / 0 0 0 0 / 1 1 1 1
//
// and sets R2=R3=127 with P2=P3=1 and everything else 0:
//
//	subset 0: (0<<1)|0   = 0   -> 8 bits, stays 0
//	subset 1: (127<<1)|1 = 255 -> stays 255, and green and blue reach
//	          (0<<1)|1 = 1
//
// Every index is 0. Mode 3 stores no alpha, so alpha decodes to 255.
//
// Bits set: 3 (mode), 4 to 7 (partition 15), 24 to 37 (R2 and R3),
// 96 and 97 (P2 and P3).
func TestDecodeMode3HandVector(t *testing.T) {
	block := []byte{
		0xF8, 0x00, 0x00, 0xFF, 0x3F, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
	}
	var want [16][4]uint8
	for texel := 0; texel < 16; texel++ {
		if partition2[15][texel] == 0 {
			want[texel] = [4]uint8{0, 0, 0, 255}
		} else {
			want[texel] = [4]uint8{255, 1, 1, 255}
		}
	}
	checkDecode(t, "mode 3 eight effective bits", block, want)
	if BlockMode(block) != 3 {
		t.Errorf("BlockMode = %d, want 3", BlockMode(block))
	}
}

// TestDecodeMode7HandVector checks mode 7, the only two-subset mode that stores
// alpha.
//
// Field order for mode 7: 8 mode bits, 6 partition bits, then R0 to R3 at 5 bits
// each, then G0 to G3, then B0 to B3, then A0 to A3, then P0 to P3, then the
// 2-bit indices.
//
// The vector picks two-subset partition 15 again and sets R2=R3=31, A0=A1=31,
// A2=A3=0, and every parity bit 0. Five stored bits plus one parity bit give six
// effective bits:
//
//	(31<<1)|0 = 62 -> unquantize(62, 6) = 248 | (248>>6) = 251
//	(0<<1)|0  = 0  -> 0
//
// So subset 0 decodes to (0,0,0,251) and subset 1 decodes to (251,0,0,0).
//
// Bits set: 7 (mode), 8 to 11 (partition 15), 24 to 33 (R2 and R3),
// 74 to 83 (A0 and A1).
func TestDecodeMode7HandVector(t *testing.T) {
	block := []byte{
		0x80, 0x0F, 0x00, 0xFF, 0x03, 0x00, 0x00, 0x00,
		0x00, 0xFC, 0x0F, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	var want [16][4]uint8
	for texel := 0; texel < 16; texel++ {
		if partition2[15][texel] == 0 {
			want[texel] = [4]uint8{0, 0, 0, 251}
		} else {
			want[texel] = [4]uint8{251, 0, 0, 0}
		}
	}
	checkDecode(t, "mode 7 two subsets with alpha", block, want)
	if BlockMode(block) != 7 {
		t.Errorf("BlockMode = %d, want 7", BlockMode(block))
	}
}

// TestDecodeShortBlockIsSafe proves a truncated input cannot panic. The KTX2
// reader hands us whatever the file holds.
func TestDecodeShortBlockIsSafe(t *testing.T) {
	for n := 0; n < BlockBytes; n++ {
		got := DecodeBlock(make([]byte, n))
		if got != solid([4]uint8{0, 0, 0, 0}) {
			t.Errorf("a %d byte block decoded to something non-zero", n)
		}
	}
}
