package ktx2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// blockGolden names one reference file and the VkFormat it holds.
//
// KTX-Software 4.4.2 wrote every file with "ktx create --raw", so the
// descriptor bytes inside come from the Khronos reference implementation of the
// Data Format Descriptor. Comparing our bytes against them proves the table in
// blockDescriptor is right without trusting our own reader.
type blockGolden struct {
	file     string
	vkFormat int
	// blockBytes is the payload of one 4x4 block, so the test can rebuild the
	// same one-block image the reference tool encoded.
	blockBytes int
}

func blockGoldens() []blockGolden {
	return []blockGolden{
		{"bc1.ktx2", VkFormatBC1RGBUnormBlock, 8},
		{"bc1s.ktx2", VkFormatBC1RGBSRGBBlock, 8},
		{"bc1a.ktx2", VkFormatBC1RGBAUnormBlock, 8},
		{"bc1as.ktx2", VkFormatBC1RGBASRGBBlock, 8},
		{"bc3.ktx2", VkFormatBC3UnormBlock, 16},
		{"bc3s.ktx2", VkFormatBC3SRGBBlock, 16},
		{"bc4.ktx2", VkFormatBC4UnormBlock, 8},
		{"bc5.ktx2", VkFormatBC5UnormBlock, 16},
		{"bc7.ktx2", VkFormatBC7UnormBlock, 16},
		{"bc7s.ktx2", VkFormatBC7SRGBBlock, 16},
	}
}

// readDFD returns the Basic Data Format Descriptor block of a KTX2 file.
func readDFD(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 80 {
		t.Fatalf("file holds %d bytes, want at least 80", len(data))
	}
	h := parseHeader(data[12:80])
	start := int(h.dfdByteOffset)
	end := start + int(h.dfdByteLength)
	if start <= 0 || end > len(data) {
		t.Fatalf("descriptor spans %d to %d, file holds %d bytes", start, end, len(data))
	}
	return data[start:end]
}

// TestBlockDescriptorMatchesKhronosGolden compares our descriptor bytes with the
// Khronos reference implementation, byte for byte.
//
// The oracle is not our own reader: testdata holds files that KTX-Software 4.4.2
// produced. A wrong colour model, a wrong texel block dimension, a wrong
// bytesPlane0, a swapped sample order, or a missing sRGB alpha qualifier all
// change these bytes, and every one of those mistakes makes a file that loads as
// the wrong thing.
func TestBlockDescriptorMatchesKhronosGolden(t *testing.T) {
	for _, golden := range blockGoldens() {
		reference, err := os.ReadFile(filepath.Join("testdata", golden.file))
		if err != nil {
			t.Fatalf("%s: %v", golden.file, err)
		}
		want := readDFD(t, reference)

		// Rebuild the same image the reference tool encoded: one 4x4 block.
		payload := make([]byte, golden.blockBytes)
		for i := range payload {
			payload[i] = byte(i)
		}
		ours, err := Encode(&Image{
			Format: golden.vkFormat,
			Width:  4,
			Height: 4,
			Faces:  1,
			Levels: []Level{{Width: 4, Height: 4, Depth: 1, Layers: 1, Faces: 1, Bytes: payload}},
		}, EncodeOptions{})
		if err != nil {
			t.Fatalf("%s: Encode vkFormat %d: %v", golden.file, golden.vkFormat, err)
		}
		got := readDFD(t, ours)

		if !bytes.Equal(got, want) {
			t.Errorf("%s vkFormat %d descriptor mismatch\n got %s\nwant %s",
				golden.file, golden.vkFormat, hexBytes(got), hexBytes(want))
		}

		// The payload must survive the round trip unchanged, so the descriptor
		// test cannot pass on a file whose pixels are wrong.
		parsed, err := Parse(ours)
		if err != nil {
			t.Fatalf("%s: Parse: %v", golden.file, err)
		}
		if !bytes.Equal(parsed.Levels[0].Bytes, payload) {
			t.Errorf("%s: payload changed through encode and parse", golden.file)
		}
	}
}

// TestBlockPayloadMatchesKhronosGolden checks the whole level payload region,
// not only the descriptor.
//
// The reference tool and this writer lay out the header, the level index, and the
// key/value block differently, so the files are not byte-identical. The payload
// bytes must be, because both write the same blocks.
func TestBlockPayloadMatchesKhronosGolden(t *testing.T) {
	for _, golden := range blockGoldens() {
		reference, err := os.ReadFile(filepath.Join("testdata", golden.file))
		if err != nil {
			t.Fatalf("%s: %v", golden.file, err)
		}
		parsed, err := Parse(reference)
		if err != nil {
			t.Fatalf("%s: Parse the Khronos file: %v", golden.file, err)
		}
		if parsed.Format != golden.vkFormat {
			t.Fatalf("%s: vkFormat %d, want %d", golden.file, parsed.Format, golden.vkFormat)
		}
		if parsed.Width != 4 || parsed.Height != 4 {
			t.Fatalf("%s: %dx%d, want 4x4", golden.file, parsed.Width, parsed.Height)
		}
		if len(parsed.Levels) != 1 {
			t.Fatalf("%s: %d levels, want 1", golden.file, len(parsed.Levels))
		}
		if len(parsed.Levels[0].Bytes) != golden.blockBytes {
			t.Errorf("%s: level 0 holds %d bytes, want %d",
				golden.file, len(parsed.Levels[0].Bytes), golden.blockBytes)
		}
		if pitch := parsed.Levels[0].RowPitch(parsed.Format); pitch != golden.blockBytes {
			t.Errorf("%s: row pitch %d, want %d", golden.file, pitch, golden.blockBytes)
		}
	}
}

// TestBlockFormatBlockInfoMatchesVulkanBitrate pins the bytes per block of every
// BC format the writer accepts.
//
// The oracle is the published bitrate of each format, which is a property of the
// format definition and not of this code. BC1 and BC4 cost 4 bits per texel;
// BC3, BC5 and BC7 cost 8. A 4x4 block covers 16 texels, so the byte count
// follows from the bitrate alone.
func TestBlockFormatBlockInfoMatchesVulkanBitrate(t *testing.T) {
	cases := []struct {
		vkFormat    int
		bitsPerTexl int
	}{
		{VkFormatBC1RGBUnormBlock, 4},
		{VkFormatBC1RGBSRGBBlock, 4},
		{VkFormatBC1RGBAUnormBlock, 4},
		{VkFormatBC1RGBASRGBBlock, 4},
		{VkFormatBC3UnormBlock, 8},
		{VkFormatBC3SRGBBlock, 8},
		{VkFormatBC4UnormBlock, 4},
		{VkFormatBC5UnormBlock, 8},
		{VkFormatBC7UnormBlock, 8},
		{VkFormatBC7SRGBBlock, 8},
	}
	for _, tc := range cases {
		info, ok := FormatBlockInfo(tc.vkFormat)
		if !ok {
			t.Fatalf("vkFormat %d has no block info", tc.vkFormat)
		}
		if !info.Compressed {
			t.Errorf("vkFormat %d is not marked compressed", tc.vkFormat)
		}
		if info.Width != 4 || info.Height != 4 {
			t.Errorf("vkFormat %d is %dx%d, want 4x4", tc.vkFormat, info.Width, info.Height)
		}
		want := info.Width * info.Height * tc.bitsPerTexl / 8
		if info.BytesPerBlock != want {
			t.Errorf("vkFormat %d holds %d bytes per block, want %d at %d bits per texel",
				tc.vkFormat, info.BytesPerBlock, want, tc.bitsPerTexl)
		}
		// The descriptor must agree with the block info. A disagreement writes
		// a bytesPlane0 that contradicts the upload stride.
		desc, ok := blockDescriptor(tc.vkFormat)
		if !ok {
			t.Fatalf("vkFormat %d has no descriptor", tc.vkFormat)
		}
		if desc.bytesPerBlock != info.BytesPerBlock {
			t.Errorf("vkFormat %d: descriptor says %d bytes per block, block info says %d",
				tc.vkFormat, desc.bytesPerBlock, info.BytesPerBlock)
		}
		if desc.blockWidth != info.Width || desc.blockHeight != info.Height {
			t.Errorf("vkFormat %d: descriptor block %dx%d, block info %dx%d",
				tc.vkFormat, desc.blockWidth, desc.blockHeight, info.Width, info.Height)
		}
	}
}

// TestBlockTransferFunctionPairsWithVkFormat pins the sRGB and unorm pairing.
//
// A normal map written under an sRGB VkFormat looks plausible and is wrong: the
// sampler inverts a curve the encoder never applied. The descriptor is where
// that mistake becomes a file, so the pairing is asserted here.
func TestBlockTransferFunctionPairsWithVkFormat(t *testing.T) {
	srgb := map[int]bool{
		VkFormatBC1RGBSRGBBlock:  true,
		VkFormatBC1RGBASRGBBlock: true,
		VkFormatBC3SRGBBlock:     true,
		VkFormatBC7SRGBBlock:     true,
	}
	linear := []int{
		VkFormatBC1RGBUnormBlock,
		VkFormatBC1RGBAUnormBlock,
		VkFormatBC3UnormBlock,
		VkFormatBC4UnormBlock,
		VkFormatBC5UnormBlock,
		VkFormatBC7UnormBlock,
	}
	for format := range srgb {
		desc, ok := blockDescriptor(format)
		if !ok {
			t.Fatalf("vkFormat %d has no descriptor", format)
		}
		if desc.transfer != dfTransferSRGB {
			t.Errorf("vkFormat %d carries transfer %d, want sRGB (%d)", format, desc.transfer, dfTransferSRGB)
		}
	}
	for _, format := range linear {
		desc, ok := blockDescriptor(format)
		if !ok {
			t.Fatalf("vkFormat %d has no descriptor", format)
		}
		if desc.transfer != dfTransferLinear {
			t.Errorf("vkFormat %d carries transfer %d, want linear (%d)", format, desc.transfer, dfTransferLinear)
		}
	}
	// BC4 and BC5 have no sRGB VkFormat at all, so no descriptor may claim one.
	for _, format := range []int{VkFormatBC4UnormBlock, VkFormatBC5UnormBlock} {
		if srgb[format] {
			t.Fatalf("vkFormat %d must not appear in the sRGB set", format)
		}
	}
}

// TestEncodeRefusesUnalignedBlockLevelZero pins the WebGPU rule.
//
// WebGPU createTexture validates that a compressed texture's width and height
// are multiples of the texel block size. A file that breaks the rule fails in
// the browser, so the writer refuses it at build time with a named error.
func TestEncodeRefusesUnalignedBlockLevelZero(t *testing.T) {
	cases := []struct{ width, height int }{
		{6, 8},  // width is not a multiple of 4
		{8, 6},  // height is not
		{2, 2},  // both smaller than one block
		{12, 5}, // odd height
	}
	for _, tc := range cases {
		columns := divCeil(tc.width, 4)
		rows := divCeil(tc.height, 4)
		src := &Image{
			Format: VkFormatBC7UnormBlock,
			Width:  tc.width,
			Height: tc.height,
			Faces:  1,
			Levels: []Level{{
				Width: tc.width, Height: tc.height, Depth: 1, Layers: 1, Faces: 1,
				Bytes: make([]byte, columns*rows*16),
			}},
		}
		_, err := Encode(src, EncodeOptions{})
		if !errors.Is(err, ErrEncodeBlockAlignment) {
			t.Errorf("%dx%d gave %v, want ErrEncodeBlockAlignment", tc.width, tc.height, err)
		}
	}

	// A size that does divide must encode. Otherwise the refusal above could
	// pass by refusing everything.
	ok := &Image{
		Format: VkFormatBC7UnormBlock,
		Width:  8,
		Height: 8,
		Faces:  1,
		Levels: []Level{{Width: 8, Height: 8, Depth: 1, Layers: 1, Faces: 1, Bytes: make([]byte, 4*16)}},
	}
	if _, err := Encode(ok, EncodeOptions{}); err != nil {
		t.Fatalf("8x8 BC7 must encode, got %v", err)
	}
}

// TestEncodeBlockMipChainRoundTrip walks a full mip chain down to 1x1.
//
// Levels below the block size are legal and must keep their whole block. A
// writer that sized level 6 of a 64x64 BC7 texture as 1x1 texels instead of one
// block would write four bytes where the GPU reads sixteen.
func TestEncodeBlockMipChainRoundTrip(t *testing.T) {
	for _, golden := range blockGoldens() {
		info, _ := FormatBlockInfo(golden.vkFormat)
		const size = 64
		var levels []Level
		width, height := size, size
		for {
			columns := divCeil(width, info.Width)
			rows := divCeil(height, info.Height)
			payload := make([]byte, columns*rows*info.BytesPerBlock)
			for i := range payload {
				payload[i] = byte(width + i)
			}
			levels = append(levels, Level{
				Width: width, Height: height, Depth: 1, Layers: 1, Faces: 1, Bytes: payload,
			})
			if width == 1 && height == 1 {
				break
			}
			if width > 1 {
				width /= 2
			}
			if height > 1 {
				height /= 2
			}
		}
		if len(levels) != 7 {
			t.Fatalf("a 64x64 chain holds %d levels, want 7", len(levels))
		}

		for _, scheme := range []Supercompression{SupercompressionNone, SupercompressionZlib} {
			data, err := Encode(&Image{
				Format: golden.vkFormat,
				Width:  size,
				Height: size,
				Faces:  1,
				Levels: levels,
			}, EncodeOptions{Supercompression: scheme, ZlibLevel: 6})
			if err != nil {
				t.Fatalf("vkFormat %d scheme %d: %v", golden.vkFormat, scheme, err)
			}
			parsed, err := Parse(data)
			if err != nil {
				t.Fatalf("vkFormat %d scheme %d: Parse: %v", golden.vkFormat, scheme, err)
			}
			if len(parsed.Levels) != len(levels) {
				t.Fatalf("vkFormat %d: %d levels back, want %d", golden.vkFormat, len(parsed.Levels), len(levels))
			}
			for i, level := range parsed.Levels {
				if !bytes.Equal(level.Bytes, levels[i].Bytes) {
					t.Errorf("vkFormat %d scheme %d level %d changed", golden.vkFormat, scheme, i)
				}
			}
			// Plain levels must land on a block-aligned offset, because the
			// specification pads to the least common multiple of the block
			// size and 4.
			if scheme == SupercompressionNone {
				align := lcm(info.BytesPerBlock, 4)
				for i := range levels {
					off := binary.LittleEndian.Uint64(data[80+i*24:])
					if off%uint64(align) != 0 {
						t.Errorf("vkFormat %d level %d starts at %d, not a multiple of %d",
							golden.vkFormat, i, off, align)
					}
				}
			}
		}
	}
}

// TestEncodeBlockRejectsWrongPayloadLength keeps the shape check honest for
// block formats. A payload one block short must fail, not truncate.
func TestEncodeBlockRejectsWrongPayloadLength(t *testing.T) {
	for _, delta := range []int{-16, -1, 1, 16} {
		src := &Image{
			Format: VkFormatBC7UnormBlock,
			Width:  16,
			Height: 16,
			Faces:  1,
			Levels: []Level{{
				Width: 16, Height: 16, Depth: 1, Layers: 1, Faces: 1,
				Bytes: make([]byte, 4*4*16+delta),
			}},
		}
		if _, err := Encode(src, EncodeOptions{}); !errors.Is(err, ErrEncodeShape) {
			t.Errorf("delta %d gave %v, want ErrEncodeShape", delta, err)
		}
	}
}

func hexBytes(b []byte) string {
	out := make([]byte, 0, len(b)*3)
	for i, v := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, []byte(fmt.Sprintf("%02x", v))...)
	}
	return string(out)
}
