package ktx2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"testing"
)

// buildKTX2 generates a minimal valid KTX2 byte stream for testing the
// parser. Everything past the level data is left empty — real KTX2 files
// have DFDs + KVDs but the parser doesn't consume them here.
func buildKTX2(vkFormat uint32, scheme uint32, width, height uint32, levelData [][]byte, uncompressedLengths []uint64) []byte {
	return buildKTX2WithMetadata(vkFormat, scheme, width, height, 0, 0, 1, levelData, uncompressedLengths)
}

func buildKTX2WithMetadata(vkFormat uint32, scheme uint32, width, height, depth, layers, faces uint32, levelData [][]byte, uncompressedLengths []uint64) []byte {
	var buf bytes.Buffer
	buf.Write(identifier)

	// Header — 68 bytes starting at offset 12.
	h := make([]byte, 68)
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(h[off:], v) }
	put64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(h[off:], v) }
	put32(0, vkFormat)
	put32(4, 1)       // typeSize
	put32(8, width)   // pixelWidth
	put32(12, height) // pixelHeight
	put32(16, depth)  // pixelDepth (0 = 1)
	put32(20, layers) // layerCount (0 = 1)
	put32(24, faces)  // faceCount
	put32(28, uint32(len(levelData)))
	put32(32, scheme)
	put32(36, 0) // dfdByteOffset
	put32(40, 0) // dfdByteLength
	put32(44, 0) // kvdByteOffset
	put32(48, 0) // kvdByteLength
	put64(52, 0) // sgdByteOffset
	put64(60, 0) // sgdByteLength
	buf.Write(h)

	// Level index: 3 u64 per level = 24 bytes.
	levelCount := len(levelData)
	indexStart := buf.Len()
	dataStart := indexStart + levelCount*24

	indexBytes := make([]byte, levelCount*24)
	offsets := make([]uint64, levelCount)
	running := uint64(dataStart)
	for i, lvl := range levelData {
		offsets[i] = running
		running += uint64(len(lvl))
	}
	for i := 0; i < levelCount; i++ {
		binary.LittleEndian.PutUint64(indexBytes[i*24+0:], offsets[i])
		binary.LittleEndian.PutUint64(indexBytes[i*24+8:], uint64(len(levelData[i])))
		binary.LittleEndian.PutUint64(indexBytes[i*24+16:], uncompressedLengths[i])
	}
	buf.Write(indexBytes)

	// Level data — just concatenate.
	for _, lvl := range levelData {
		buf.Write(lvl)
	}
	return buf.Bytes()
}

// TestParseUncompressedRGBA verifies round-trip parsing of a minimal 2x2
// RGBA8 texture with no supercompression.
func TestParseUncompressedRGBA(t *testing.T) {
	pixels := make([]byte, 2*2*4)
	for i := range pixels {
		pixels[i] = byte(i * 4)
	}
	ktx := buildKTX2(VkFormatR8G8B8A8Unorm, schemeNone, 2, 2, [][]byte{pixels}, []uint64{uint64(len(pixels))})

	img, err := Parse(ktx)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if img.Width != 2 || img.Height != 2 {
		t.Errorf("dims: want 2x2, got %dx%d", img.Width, img.Height)
	}
	if img.Depth != 1 || img.Layers != 1 || img.Faces != 1 {
		t.Errorf("metadata: want depth=1 layers=1 faces=1, got depth=%d layers=%d faces=%d", img.Depth, img.Layers, img.Faces)
	}
	if img.Format != VkFormatR8G8B8A8Unorm {
		t.Errorf("format: want %d, got %d", VkFormatR8G8B8A8Unorm, img.Format)
	}
	if len(img.Levels) != 1 {
		t.Fatalf("levels: want 1, got %d", len(img.Levels))
	}
	if !bytes.Equal(img.Levels[0].Bytes, pixels) {
		t.Errorf("level 0 bytes do not round-trip")
	}
	if got := img.Levels[0].RowPitch(img.Format); got != 8 {
		t.Errorf("row pitch: want 8, got %d", got)
	}
}

// TestParseZlibCompression verifies zlib-supercompressed level data is
// inflated on parse.
func TestParseZlibCompression(t *testing.T) {
	original := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	zw.Write(original)
	zw.Close()

	ktx := buildKTX2(VkFormatR8G8B8A8Unorm, schemeZlib, 2, 2,
		[][]byte{compressed.Bytes()}, []uint64{uint64(len(original))})

	img, err := Parse(ktx)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Equal(img.Levels[0].Bytes, original) {
		t.Errorf("zlib round-trip: want %v, got %v", original, img.Levels[0].Bytes)
	}
}

// TestParseRejectsBadIdentifier ensures a non-KTX2 byte stream fails fast.
func TestParseRejectsBadIdentifier(t *testing.T) {
	bad := make([]byte, 200)
	copy(bad, []byte("not a ktx2 fi"))
	_, err := Parse(bad)
	if !errors.Is(err, ErrInvalidIdentifier) {
		t.Errorf("want ErrInvalidIdentifier, got %v", err)
	}
}

// TestParseTruncated fails cleanly when the header runs off the end.
func TestParseTruncated(t *testing.T) {
	_, err := Parse(identifier) // only 12 bytes — no header
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

// TestParseUnsupportedFormat raises ErrUnsupportedFormat rather than
// silently passing through bytes the renderer doesn't know how to upload.
func TestParseUnsupportedFormat(t *testing.T) {
	const vkFormatBC1RGBUnormBlock = 131
	payload := make([]byte, 16)
	ktx := buildKTX2(vkFormatBC1RGBUnormBlock, schemeNone, 4, 4, [][]byte{payload}, []uint64{16})
	_, err := Parse(ktx)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("want ErrUnsupportedFormat, got %v", err)
	}
}

func TestParsePreservesGeometryMetadata(t *testing.T) {
	level0 := make([]byte, 4*4*4*2*6*4)
	level1 := make([]byte, 2*2*2*2*6*4)
	ktx := buildKTX2WithMetadata(VkFormatR8G8B8A8Unorm, schemeNone, 4, 4, 4, 2, 6,
		[][]byte{level0, level1}, []uint64{uint64(len(level0)), uint64(len(level1))})

	img, err := Parse(ktx)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if img.Width != 4 || img.Height != 4 || img.Depth != 4 || img.Layers != 2 || img.Faces != 6 {
		t.Fatalf("image metadata: got %dx%dx%d layers=%d faces=%d", img.Width, img.Height, img.Depth, img.Layers, img.Faces)
	}
	if got := img.Levels[1]; got.Width != 2 || got.Height != 2 || got.Depth != 2 || got.Layers != 2 || got.Faces != 6 {
		t.Fatalf("level 1 metadata: got %dx%dx%d layers=%d faces=%d", got.Width, got.Height, got.Depth, got.Layers, got.Faces)
	}
	if !bytes.Equal(img.Levels[0].Bytes, level0) || !bytes.Equal(img.Levels[1].Bytes, level1) {
		t.Fatal("level payloads do not round-trip")
	}
}

func TestCompressedPassThroughFormats(t *testing.T) {
	tests := []struct {
		name       string
		format     int
		width      uint32
		height     uint32
		payloadLen int
		rowPitch   int
		blocksX    int
		blocksY    int
	}{
		{"BC7", VkFormatBC7UnormBlock, 8, 4, 32, 32, 2, 1},
		{"ASTC4x4", VkFormatASTC4x4SRGBBlock, 4, 4, 16, 16, 1, 1},
		{"ASTC6x6", VkFormatASTC6x6UnormBlock, 7, 6, 32, 32, 2, 1},
		{"ASTC8x8", VkFormatASTC8x8SRGBBlock, 9, 9, 64, 32, 2, 2},
		{"ETC2RGB", VkFormatETC2R8G8B8SRGBBlock, 5, 4, 16, 16, 2, 1},
		{"ETC2RGBA8", VkFormatETC2R8G8B8A8UnormBlock, 5, 4, 32, 32, 2, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !IsSupportedFormat(tc.format) {
				t.Fatalf("format %d should be supported", tc.format)
			}
			payload := make([]byte, tc.payloadLen)
			for i := range payload {
				payload[i] = byte(i)
			}
			ktx := buildKTX2(uint32(tc.format), schemeNone, tc.width, tc.height,
				[][]byte{payload}, []uint64{uint64(len(payload))})

			img, err := Parse(ktx)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			level := img.Levels[0]
			if !bytes.Equal(level.Bytes, payload) {
				t.Fatal("compressed bytes do not pass through")
			}
			if got := level.RowPitch(tc.format); got != tc.rowPitch {
				t.Errorf("row pitch: want %d, got %d", tc.rowPitch, got)
			}
			if got := level.BlockColumns(tc.format); got != tc.blocksX {
				t.Errorf("block columns: want %d, got %d", tc.blocksX, got)
			}
			if got := level.BlockRows(tc.format); got != tc.blocksY {
				t.Errorf("block rows: want %d, got %d", tc.blocksY, got)
			}
		})
	}
}

// TestBasisTranscoderHook verifies a registered Basis transcoder gets
// control for BasisLZ supercompressed data.
func TestBasisTranscoderHook(t *testing.T) {
	t.Cleanup(func() { RegisterBasisTranscoder(nil) })

	called := false
	RegisterBasisTranscoder(&stubBasis{
		fn: func(data []byte, fmt_, w, h int) ([]byte, error) {
			called = true
			return []byte{0xDE, 0xAD, 0xBE, 0xEF}, nil
		},
	})

	raw := []byte{0x42, 0x42, 0x42, 0x42}
	ktx := buildKTX2(VkFormatR8G8B8A8Unorm, schemeBasisLZ, 1, 1,
		[][]byte{raw}, []uint64{4})

	img, err := Parse(ktx)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !called {
		t.Error("Basis transcoder was not invoked")
	}
	if !bytes.Equal(img.Levels[0].Bytes, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("transcoder output did not propagate: %v", img.Levels[0].Bytes)
	}
}

type stubBasis struct {
	fn func(data []byte, fmt_, w, h int) ([]byte, error)
}

func (s *stubBasis) Transcode(data []byte, fmt_ int, w, h int) ([]byte, error) {
	return s.fn(data, fmt_, w, h)
}
