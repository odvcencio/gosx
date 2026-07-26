package bcn

import (
	"fmt"
	"testing"
)

// TestBlockBytes checks the payload size of one block against the published
// layouts.
func TestBlockBytes(t *testing.T) {
	want := map[Format]int{
		FormatBC1RGB:  8,
		FormatBC1RGBA: 8,
		FormatBC3:     16,
		FormatBC4:     8,
		FormatBC5:     16,
		FormatUnknown: 0,
	}
	for format, bytes := range want {
		if got := format.BlockBytes(); got != bytes {
			t.Errorf("%s block is %d bytes, want %d", format, got, bytes)
		}
	}
}

// TestPayloadSizeCountsWholeBlocks checks the padding rule. A level smaller than
// 4x4 still costs one whole block, which is what the GPU allocates.
func TestPayloadSizeCountsWholeBlocks(t *testing.T) {
	cases := []struct {
		width, height int
		blocks        int
	}{
		{4, 4, 1},
		{5, 4, 2},
		{5, 5, 4},
		{1, 1, 1},
		{2, 2, 1},
		{2048, 2048, 512 * 512},
	}
	for _, tc := range cases {
		want := tc.blocks * 8
		if got := PayloadSize(FormatBC1RGB, tc.width, tc.height); got != want {
			t.Errorf("%dx%d BC1 needs %d bytes, want %d", tc.width, tc.height, got, want)
		}
		if got := PayloadSize(FormatBC3, tc.width, tc.height); got != want*2 {
			t.Errorf("%dx%d BC3 needs %d bytes, want %d", tc.width, tc.height, got, want*2)
		}
	}
	if got := PayloadSize(FormatUnknown, 4, 4); got != 0 {
		t.Errorf("an unknown format needs %d bytes, want 0", got)
	}
}

// TestGPUBytesTable reports the number that decides whether a texture fits in
// memory: the whole mip chain, on the GPU, after padding.
//
// The report is the point of this test. The assertions only hold the arithmetic
// still.
func TestGPUBytesTable(t *testing.T) {
	const size = 2048
	base := RGBA8MipChainBytes(size, size)
	t.Logf("rgba8unorm 2048x2048 mip chain: %s", mib(base))
	for _, format := range []Format{FormatBC1RGB, FormatBC1RGBA, FormatBC3, FormatBC4, FormatBC5} {
		chain := MipChainBytes(format, size, size)
		level0 := PayloadSize(format, size, size)
		t.Logf("%-9s block %2d bytes, level 0 %s, mip chain %s, ratio %.2f to 1, bits per texel %.2f",
			format, format.BlockBytes(), mib(level0), mib(chain),
			float64(base)/float64(chain), float64(format.BlockBytes()*8)/16)
		if chain <= level0 {
			t.Errorf("%s mip chain %d must exceed level 0 at %d", format, chain, level0)
		}
	}

	// Both totals below are derived by hand.
	//
	// A 2048 by 2048 chain holds twelve levels. BC1 costs eight bytes for each
	// 4x4 block, so the level sizes are 2097152, 524288, 131072, 32768, 8192,
	// 2048, 512, 128, 32, and then 8 bytes for each of the 4x4, 2x2 and 1x1
	// levels, because a level below 4x4 still costs one whole block. The sum is
	// 2796216 bytes.
	if got := MipChainBytes(FormatBC1RGB, size, size); got != 2796216 {
		t.Errorf("BC1 2048x2048 mip chain is %d bytes, want 2796216", got)
	}
	// rgba8unorm costs four bytes for each texel, and the twelve level texel
	// counts sum to 5592405, so the chain is 22369620 bytes.
	if got := RGBA8MipChainBytes(size, size); got != 22369620 {
		t.Errorf("rgba8unorm 2048x2048 mip chain is %d bytes, want 22369620", got)
	}
	// The chain ratio sits a little below the block ratio of 8 to 1, because the
	// three smallest levels each pay for a whole block.
	if ratio := float64(RGBA8MipChainBytes(size, size)) / float64(MipChainBytes(FormatBC1RGB, size, size)); ratio < 7.9 || ratio > 8.0 {
		t.Errorf("the BC1 chain ratio is %.4f, want between 7.9 and 8.0", ratio)
	}
	if got := MipChainBytes(FormatUnknown, size, size); got != 0 {
		t.Errorf("an unknown format needs %d bytes, want 0", got)
	}
}

// TestPayloadLengthMatchesTheEncoder checks every encoder returns the length
// PayloadSize promises. The integration layer sizes its KTX2 level index from
// that promise.
func TestPayloadLengthMatchesTheEncoder(t *testing.T) {
	for _, size := range [][2]int{{4, 4}, {7, 5}, {16, 16}, {1, 1}} {
		width, height := size[0], size[1]
		colour := srgbSurface(width, height, func(x, y int) RGBA8 {
			return RGBA8{R: uint8(x * 9), G: uint8(y * 7), B: 100, A: uint8(x * 5)}
		})
		checks := []struct {
			format Format
			encode func() ([]byte, error)
		}{
			{FormatBC1RGB, func() ([]byte, error) {
				return EncodeBC1(colour, BC1Options{Transfer: TransferSRGB})
			}},
			{FormatBC1RGBA, func() ([]byte, error) {
				return EncodeBC1(colour, BC1Options{Transfer: TransferSRGB, Alpha: BC1Cutout})
			}},
			{FormatBC3, func() ([]byte, error) {
				return EncodeBC3(colour, BC3Options{Transfer: TransferSRGB})
			}},
			{FormatBC4, func() ([]byte, error) {
				return EncodeBC4(colour, BC4Options{Transfer: TransferUnorm})
			}},
			{FormatBC5, func() ([]byte, error) {
				return EncodeBC5(colour, BC5Options{Transfer: TransferUnorm, Second: ChannelG})
			}},
		}
		for _, check := range checks {
			payload, err := check.encode()
			if err != nil {
				t.Fatalf("%s at %dx%d: %v", check.format, width, height, err)
			}
			want := PayloadSize(check.format, width, height)
			if len(payload) != want {
				t.Errorf("%s at %dx%d produced %d bytes, want %d",
					check.format, width, height, len(payload), want)
			}
			// The decoder must accept exactly that length.
			if _, err := Decode(check.format, payload, width, height); err != nil {
				t.Errorf("%s at %dx%d: Decode: %v", check.format, width, height, err)
			}
		}
	}
}

func mib(bytes int) string {
	return fmt.Sprintf("%.3f MiB", float64(bytes)/(1024*1024))
}
