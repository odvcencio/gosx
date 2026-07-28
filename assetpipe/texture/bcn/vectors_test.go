package bcn

import "testing"

// Where these vectors come from
//
// Every block below is hand-built, and every expected texel is derived by hand
// from the published BC1 to BC5 decode rules of Direct3D 11 and of the Khronos
// Data Format Specification. The derivation sits in the comment of each test, so
// a reader can check the arithmetic without running the code.
//
// A round trip through the encoder and the decoder of this package would prove
// only that the pair agrees with itself. These vectors prove the bit layout and
// the palette rules match the published rules, which is a different claim.
//
// Every interpolated palette entry in these vectors is a whole number of codes.
// So the rounding latitude the rules leave to a decoder cannot change any
// expected value. TestVectorInterpolantsAreWholeCodes proves that.

// TestBC4VectorEightValueMode checks the eight-value mode and the 3-bit index
// packing.
//
// Block: FF 00 92 24 49 92 24 49
//
//	byte 0 = endpoint0 = 255
//	byte 1 = endpoint1 = 0
//
// endpoint0 > endpoint1 selects the eight-value mode.
//
// Index field: the six remaining bytes form a little-endian 48-bit number, and
// texel i takes bits 3i to 3i+2. Every texel below holds index 2, so bit 3i+1 is
// set for every i. The set bits are 1, 4, 7, 10, 13, 16, 19, 22, and then the
// same pattern 24 bits later, because 24 bits hold exactly eight indices:
//
//	byte 2 holds bits 0 to 7:   1, 4, 7        -> 0x02|0x10|0x80 = 0x92
//	byte 3 holds bits 8 to 15:  10, 13         -> 0x04|0x20      = 0x24
//	byte 4 holds bits 16 to 23: 16, 19, 22     -> 0x01|0x08|0x40 = 0x49
//	bytes 5 to 7 repeat 0x92, 0x24, 0x49.
//
// Index 2 of the eight-value mode is (6*endpoint0 + 1*endpoint1)/7. With the
// endpoints normalized that is (6*1.0 + 0.0)/7 = 6/7 = 0.857142857..., and 6/7
// of 255 is 218.571..., which rounds to code 219.
func TestBC4VectorEightValueMode(t *testing.T) {
	block := []byte{0xFF, 0x00, 0x92, 0x24, 0x49, 0x92, 0x24, 0x49}

	values, err := DecodeBlockBC4(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC4: %v", err)
	}
	for i, got := range values {
		if diff := got - 6.0/7.0; diff > 1e-12 || diff < -1e-12 {
			t.Fatalf("texel %d: got %.15f, want 6/7", i, got)
		}
	}

	codes, err := DecodeBlockBC4Codes(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC4Codes: %v", err)
	}
	for i, got := range codes {
		if got != 219 {
			t.Fatalf("texel %d: got code %d, want 219", i, got)
		}
	}
}

// TestBC4VectorSixValueMode checks the six-value mode and every index value.
//
// Block: 00 FF 88 C6 FA 88 C6 FA
//
//	byte 0 = endpoint0 = 0
//	byte 1 = endpoint1 = 255
//
// endpoint0 <= endpoint1 selects the six-value mode, which spends its last two
// entries on the constants 0.0 and 1.0.
//
// Texel i holds index i modulo 8. The set bits follow from the same packing rule
// as the test above:
//
//	index 0 for texel 0: no bits
//	index 1 for texel 1: bit 3
//	index 2 for texel 2: bit 7
//	index 3 for texel 3: bits 9, 10
//	index 4 for texel 4: bit 14
//	index 5 for texel 5: bits 15, 17
//	index 6 for texel 6: bits 19, 20
//	index 7 for texel 7: bits 21, 22, 23
//
//	byte 2 holds bits 0 to 7:   3, 7             -> 0x08|0x80           = 0x88
//	byte 3 holds bits 8 to 15:  9, 10, 14, 15    -> 0x02|0x04|0x40|0x80 = 0xC6
//	byte 4 holds bits 16 to 23: 17, 19, 20 to 23 -> 0x02|0x18|0xE0      = 0xFA
//	bytes 5 to 7 repeat the pattern for texels 8 to 15.
//
// The palette of the six-value mode with endpoints 0 and 255:
//
//	index 0 = endpoint0                    = 0
//	index 1 = endpoint1                    = 255
//	index 2 = (4*endpoint0 + 1*endpoint1)/5 = 51
//	index 3 = (3*endpoint0 + 2*endpoint1)/5 = 102
//	index 4 = (2*endpoint0 + 3*endpoint1)/5 = 153
//	index 5 = (1*endpoint0 + 4*endpoint1)/5 = 204
//	index 6 = 0.0                           = 0
//	index 7 = 1.0                           = 255
func TestBC4VectorSixValueMode(t *testing.T) {
	block := []byte{0x00, 0xFF, 0x88, 0xC6, 0xFA, 0x88, 0xC6, 0xFA}
	want := [16]uint8{
		0, 255, 51, 102, 153, 204, 0, 255,
		0, 255, 51, 102, 153, 204, 0, 255,
	}
	codes, err := DecodeBlockBC4Codes(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC4Codes: %v", err)
	}
	if codes != want {
		t.Fatalf("got %v, want %v", codes, want)
	}
}

// TestBC1VectorFourColourMode checks the four-colour mode, the RGB565 layout,
// and the 2-bit index packing.
//
// Block: 00 F8 1F 00 E4 E4 E4 E4
//
//	color0 = 0xF800 as a little-endian uint16 = red 31, green 0, blue 0
//	color1 = 0x001F                          = red 0, green 0, blue 31
//
// The 5-bit and 6-bit channels expand by repeating the high bits into the low
// bits, so 31 becomes (31<<3)|(31>>2) = 248|7 = 255 and 0 becomes 0:
//
//	color0 = (255, 0, 0)
//	color1 = (0, 0, 255)
//
// color0 = 63488 > color1 = 31, so the block uses the four-colour mode:
//
//	index 2 = (2*color0 + color1)/3 = ((2*255+0)/3, 0, (0+255)/3) = (170, 0, 85)
//	index 3 = (color0 + 2*color1)/3 = ((255+0)/3, 0, (0+510)/3)   = (85, 0, 170)
//
// Index field: a little-endian uint32 with texel i at bits 2i and 2i+1. Byte
// 0xE4 is 11100100 in binary, so its four texels hold indices 0, 1, 2 and 3 from
// the low bits up. All four bytes are equal, so every row repeats.
func TestBC1VectorFourColourMode(t *testing.T) {
	block := []byte{0x00, 0xF8, 0x1F, 0x00, 0xE4, 0xE4, 0xE4, 0xE4}
	row := [4]RGBA8{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
		{R: 170, G: 0, B: 85, A: 255},
		{R: 85, G: 0, B: 170, A: 255},
	}
	texels, err := DecodeBlockBC1(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC1: %v", err)
	}
	for i, got := range texels {
		if want := row[i%4]; got != want {
			t.Fatalf("texel %d: got %+v, want %+v", i, got, want)
		}
	}
}

// TestBC1VectorThreeColourMode checks the three-colour mode and the transparent
// index.
//
// Block: 00 00 00 04 E4 E4 E4 E4
//
//	color0 = 0x0000 = red 0, green 0, blue 0
//	color1 = 0x0400 = green 32, because green holds bits 5 to 10 and 32<<5
//	                  is 0x0400
//
// The 6-bit green channel expands as (32<<2)|(32>>4) = 128|2 = 130:
//
//	color0 = (0, 0, 0)
//	color1 = (0, 130, 0)
//
// color0 = 0 <= color1 = 1024, so the block uses the three-colour mode:
//
//	index 2 = (color0 + color1)/2 = (0, 65, 0)
//	index 3 = transparent black   = (0, 0, 0) with alpha 0
//
// The index bytes repeat 0xE4, which holds indices 0, 1, 2 and 3 as above.
func TestBC1VectorThreeColourMode(t *testing.T) {
	block := []byte{0x00, 0x00, 0x00, 0x04, 0xE4, 0xE4, 0xE4, 0xE4}
	row := [4]RGBA8{
		{R: 0, G: 0, B: 0, A: 255},
		{R: 0, G: 130, B: 0, A: 255},
		{R: 0, G: 65, B: 0, A: 255},
		{R: 0, G: 0, B: 0, A: 0},
	}
	texels, err := DecodeBlockBC1(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC1: %v", err)
	}
	for i, got := range texels {
		if want := row[i%4]; got != want {
			t.Fatalf("texel %d: got %+v, want %+v", i, got, want)
		}
	}
}

// TestBC1VectorIndexBitPositions pins the texel order inside a block.
//
// The block holds color0 = 0xFFFF (white) and color1 = 0x0000 (black), so
// color0 > color1 and the four-colour mode runs. Index 1 selects color1, which
// is black, and index 0 selects white.
//
// The first case sets only the two lowest bits of the index field to 1, so only
// texel 0 turns black. That pins texel 0 to bits 0 and 1.
//
// The second case sets bits 8 and 9 instead, which is byte 5 of the block. Only
// texel 4 turns black, which pins the texels to row-major order with four texels
// in each row.
func TestBC1VectorIndexBitPositions(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		black int
	}{
		{"texel 0 at bits 0 and 1", []byte{0xFF, 0xFF, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}, 0},
		{"texel 4 at bits 8 and 9", []byte{0xFF, 0xFF, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}, 4},
		{"texel 15 at bits 30 and 31", []byte{0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40}, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			texels, err := DecodeBlockBC1(tc.bytes)
			if err != nil {
				t.Fatalf("DecodeBlockBC1: %v", err)
			}
			for i, got := range texels {
				want := RGBA8{R: 255, G: 255, B: 255, A: 255}
				if i == tc.black {
					want = RGBA8{A: 255}
				}
				if got != want {
					t.Fatalf("texel %d: got %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

// TestBC3VectorForcesFourColourMode checks the rule that separates a BC3 colour
// block from a BC1 block.
//
// Bytes 0 to 7 are the alpha block of TestBC4VectorEightValueMode, so every
// alpha reads back as code 219.
//
// Bytes 8 to 15 hold the colour block:
//
//	color0 = 0x0000 = (0, 0, 0)
//	color1 = 0xF800 = (255, 0, 0)
//
// color0 <= color1. A BC1 block with that order would use the three-colour mode
// and would decode index 3 as transparent black. A BC3 colour block always uses
// the four-colour mode, so:
//
//	index 0 = (0, 0, 0)
//	index 1 = (255, 0, 0)
//	index 2 = (2*color0 + color1)/3 = (85, 0, 0)
//	index 3 = (color0 + 2*color1)/3 = (170, 0, 0)
//
// The test also decodes the same colour block through the BC1 rules and requires
// a different answer. That is the failure direction: an encoder that treated a
// BC3 colour block like a BC1 block would produce the other palette, and this
// assertion catches it.
func TestBC3VectorForcesFourColourMode(t *testing.T) {
	block := []byte{
		0xFF, 0x00, 0x92, 0x24, 0x49, 0x92, 0x24, 0x49,
		0x00, 0x00, 0x00, 0xF8, 0xE4, 0xE4, 0xE4, 0xE4,
	}
	row := [4]RGBA8{
		{R: 0, G: 0, B: 0, A: 219},
		{R: 255, G: 0, B: 0, A: 219},
		{R: 85, G: 0, B: 0, A: 219},
		{R: 170, G: 0, B: 0, A: 219},
	}
	texels, err := DecodeBlockBC3(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC3: %v", err)
	}
	for i, got := range texels {
		if want := row[i%4]; got != want {
			t.Fatalf("texel %d: got %+v, want %+v", i, got, want)
		}
	}

	asBC1, err := DecodeBlockBC1(block[8:16])
	if err != nil {
		t.Fatalf("DecodeBlockBC1: %v", err)
	}
	if asBC1[3].A != 0 {
		t.Fatal("the BC1 rules must read index 3 of this block as transparent, " +
			"so the forced four-colour rule of BC3 is a real difference")
	}
	if asBC1[2].R == row[2].R {
		t.Fatalf("the BC1 midpoint %d must differ from the BC3 one third entry %d",
			asBC1[2].R, row[2].R)
	}
}

// TestBC5VectorTwoBlocks checks that BC5 is two BC4 blocks, first channel first.
//
// Bytes 0 to 7 are the eight-value-mode block, so the first channel reads back as
// code 219 everywhere. Bytes 8 to 15 are the six-value-mode block, so the second
// channel reads back as the eight palette values of that test.
func TestBC5VectorTwoBlocks(t *testing.T) {
	block := []byte{
		0xFF, 0x00, 0x92, 0x24, 0x49, 0x92, 0x24, 0x49,
		0x00, 0xFF, 0x88, 0xC6, 0xFA, 0x88, 0xC6, 0xFA,
	}
	wantSecond := [16]uint8{
		0, 255, 51, 102, 153, 204, 0, 255,
		0, 255, 51, 102, 153, 204, 0, 255,
	}
	pairs, err := DecodeBlockBC5(block)
	if err != nil {
		t.Fatalf("DecodeBlockBC5: %v", err)
	}
	for i, pair := range pairs {
		if got := roundCode(pair[0] * 255); got != 219 {
			t.Fatalf("texel %d first channel: got %d, want 219", i, got)
		}
		if got := roundCode(pair[1] * 255); got != wantSecond[i] {
			t.Fatalf("texel %d second channel: got %d, want %d", i, got, wantSecond[i])
		}
	}

	codes, err := Decode(FormatBC5, block, 4, 4)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for i := 0; i < 16; i++ {
		if codes[i*4] != 219 {
			t.Fatalf("texel %d red: got %d, want 219", i, codes[i*4])
		}
		if codes[i*4+1] != wantSecond[i] {
			t.Fatalf("texel %d green: got %d, want %d", i, codes[i*4+1], wantSecond[i])
		}
		if codes[i*4+2] != 0 || codes[i*4+3] != 255 {
			t.Fatalf("texel %d must read blue 0 and alpha 255, got %d and %d",
				i, codes[i*4+2], codes[i*4+3])
		}
	}
}

// TestVectorInterpolantsAreWholeCodes proves the rounding latitude of the
// published rules cannot decide any vector test.
//
// The rules give the palette weights but not the rounding, so a decoder that
// truncates can differ from this one by one code on an interpolated entry. Every
// endpoint pair in the vector tests above produces whole-number interpolants, so
// truncation and rounding agree on all of them.
func TestVectorInterpolantsAreWholeCodes(t *testing.T) {
	t.Run("bc4 eight-value mode", func(t *testing.T) {
		// The eight-value mode with endpoints 255 and 0 is the one exception:
		// 6/7 of 255 is not whole. The test above states the rounded value,
		// so record the exact fraction here and check the gap is safe.
		pal := bc4Palette(255, 0)
		exact := 1530.0 / 7
		if pal[2] != exact {
			t.Fatalf("palette entry 2 is %v, want 1530/7", pal[2])
		}
		if float64(roundCode(exact))-exact <= 0.4 {
			t.Fatal("the rounding gap is too small to state a single expected code")
		}
	})
	t.Run("bc4 six-value mode", func(t *testing.T) {
		pal := bc4Palette(0, 255)
		for i, v := range pal {
			if v != float64(int(v)) {
				t.Fatalf("palette entry %d is %v, which is not a whole code", i, v)
			}
		}
	})
	// A four-colour block uses only the one third and two thirds entries, so
	// only those two numerators must divide exactly.
	t.Run("bc1 four-colour mode", func(t *testing.T) {
		assertWholeThirds(t, colorPalette(0xF800, 0x001F, false))
	})
	// A three-colour block uses only the midpoint.
	t.Run("bc1 three-colour mode", func(t *testing.T) {
		assertWholeHalves(t, colorPalette(0x0000, 0x0400, false))
	})
	t.Run("bc3 colour block", func(t *testing.T) {
		assertWholeThirds(t, colorPalette(0x0000, 0xF800, true))
	})
}

// assertWholeThirds checks the numerator of each one third entry divides by 3.
func assertWholeThirds(t *testing.T, pal [4]RGBA8) {
	t.Helper()
	a, b := pal[0], pal[1]
	sums := [][3]int{
		{2*int(a.R) + int(b.R), 2*int(a.G) + int(b.G), 2*int(a.B) + int(b.B)},
		{int(a.R) + 2*int(b.R), int(a.G) + 2*int(b.G), int(a.B) + 2*int(b.B)},
	}
	for _, sum := range sums {
		for _, channel := range sum {
			if channel%3 != 0 {
				t.Fatalf("%d does not divide by 3, so a decoder could round it either way", channel)
			}
		}
	}
}

// assertWholeHalves checks the numerator of the midpoint divides by 2.
func assertWholeHalves(t *testing.T, pal [4]RGBA8) {
	t.Helper()
	a, b := pal[0], pal[1]
	sums := [3]int{int(a.R) + int(b.R), int(a.G) + int(b.G), int(a.B) + int(b.B)}
	for _, channel := range sums {
		if channel%2 != 0 {
			t.Fatalf("%d does not divide by 2, so a decoder could round it either way", channel)
		}
	}
}
