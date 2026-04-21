package ktx2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// identifier is the required 12-byte prefix of every KTX2 file. Copied
// verbatim from the spec section 3.1.
var identifier = []byte{0xAB, 'K', 'T', 'X', ' ', '2', '0', 0xBB, 0x0D, 0x0A, 0x1A, 0x0A}

// Supercompression scheme IDs from the KTX2 spec.
const (
	schemeNone    = 0
	schemeBasisLZ = 1
	schemeZstd    = 2
	schemeZlib    = 3
)

// VkFormat values we know how to pass through without a transcoder.
const (
	VkFormatR8G8B8A8Unorm = 37
	VkFormatR8G8B8A8SRGB  = 43
	VkFormatB8G8R8A8Unorm = 44
	VkFormatB8G8R8A8SRGB  = 50

	VkFormatBC7UnormBlock = 145
	VkFormatBC7SRGBBlock  = 146

	VkFormatETC2R8G8B8UnormBlock   = 147
	VkFormatETC2R8G8B8SRGBBlock    = 148
	VkFormatETC2R8G8B8A8UnormBlock = 151
	VkFormatETC2R8G8B8A8SRGBBlock  = 152

	VkFormatASTC4x4UnormBlock = 157
	VkFormatASTC4x4SRGBBlock  = 158
	VkFormatASTC6x6UnormBlock = 165
	VkFormatASTC6x6SRGBBlock  = 166
	VkFormatASTC8x8UnormBlock = 171
	VkFormatASTC8x8SRGBBlock  = 172
)

// Errors the parser can return.
var (
	ErrInvalidIdentifier = errors.New("ktx2: invalid file identifier")
	ErrTruncated         = errors.New("ktx2: data truncated")
	ErrUnsupportedFormat = errors.New("ktx2: unsupported VkFormat")
	ErrUnsupportedScheme = errors.New("ktx2: unsupported supercompression scheme")
	// ErrUnsupportedGeometry is retained for callers that match older
	// parser errors. Parse now preserves KTX2 layer/face/depth metadata.
	ErrUnsupportedGeometry = errors.New("ktx2: unsupported geometry (layers/faces/depth)")
)

// Image is a parsed KTX2 asset ready for GPU upload. Levels are ordered
// from highest (level 0 = full size) to lowest (smallest mip). Level bytes
// are ready-to-upload payloads in the VkFormat reported by Format.
type Image struct {
	// Format is the KTX2 VkFormat value — map to gpu.TextureFormat at
	// upload time via FormatToGPU.
	Format int

	// Width, Height are the dimensions of mip level 0 in pixels.
	Width, Height, Depth int

	// Layers and Faces are normalized KTX2 metadata counts. A KTX2
	// layerCount or pixelDepth of 0 is reported as 1 here.
	Layers, Faces int

	// Levels holds per-mip pixel data. len(Levels) == original KTX2
	// levelCount; levels are pre-decompressed if a supercompression
	// scheme was in effect.
	Levels []Level
}

// Level describes one mip level's ready-to-upload bytes + dimensions.
type Level struct {
	Width, Height, Depth int
	Layers, Faces        int
	Bytes                []byte
}

// BlockInfo describes how a VkFormat is arranged for upload. For
// uncompressed formats, block dimensions are 1x1 and BytesPerBlock is the
// per-pixel stride.
type BlockInfo struct {
	Width, Height int
	BytesPerBlock int
	Compressed    bool
}

// BasisTranscoder decodes a BasisLZ payload into uncompressed pixels for
// the given VkFormat. Implementations live outside this package so that
// the Basis decoder (sizable) can opt-in without pulling it into core.
// A nil transcoder means BasisLZ textures raise ErrUnsupportedScheme.
type BasisTranscoder interface {
	Transcode(basisData []byte, targetFormat int, width, height int) ([]byte, error)
}

var registeredBasis BasisTranscoder

// RegisterBasisTranscoder installs a global Basis transcoder. Passing
// nil clears any registration. Safe to call at package init from a
// sibling package.
func RegisterBasisTranscoder(t BasisTranscoder) { registeredBasis = t }

// Parse reads a KTX2 container from data and returns a ready-to-upload
// Image. All bytes must be present — streaming is a future enhancement.
func Parse(data []byte) (*Image, error) {
	if len(data) < 80 {
		return nil, fmt.Errorf("%w: header requires 80 bytes, got %d", ErrTruncated, len(data))
	}
	if !bytes.Equal(data[:12], identifier) {
		return nil, ErrInvalidIdentifier
	}

	h := parseHeader(data[12:80])
	if h.levelCount == 0 {
		h.levelCount = 1
	}
	baseDepth := normalizedCount(h.pixelDepth)
	layers := normalizedCount(h.layerCount)
	faces := normalizedCount(h.faceCount)

	// Level index: 24 bytes per level, starting at byte 80.
	indexStart := 80
	indexBytes := int(h.levelCount) * 24
	if len(data) < indexStart+indexBytes {
		return nil, fmt.Errorf("%w: level index out of range", ErrTruncated)
	}
	levels := make([]levelEntry, h.levelCount)
	for i := 0; i < int(h.levelCount); i++ {
		off := indexStart + i*24
		levels[i] = levelEntry{
			byteOffset:             binary.LittleEndian.Uint64(data[off+0:]),
			byteLength:             binary.LittleEndian.Uint64(data[off+8:]),
			uncompressedByteLength: binary.LittleEndian.Uint64(data[off+16:]),
		}
	}

	img := &Image{
		Format: int(h.vkFormat),
		Width:  int(h.pixelWidth),
		Height: int(h.pixelHeight),
		Depth:  baseDepth,
		Layers: layers,
		Faces:  faces,
		Levels: make([]Level, h.levelCount),
	}

	for i, entry := range levels {
		lvlWidth := max1(int(h.pixelWidth) >> i)
		lvlHeight := max1(int(h.pixelHeight) >> i)
		lvlDepth := max1(baseDepth >> i)

		if uint64(len(data)) < entry.byteOffset+entry.byteLength {
			return nil, fmt.Errorf("%w: level %d data out of range", ErrTruncated, i)
		}
		raw := data[entry.byteOffset : entry.byteOffset+entry.byteLength]

		decoded, err := decompress(h.supercompressionScheme, raw, entry.uncompressedByteLength, int(h.vkFormat), lvlWidth, lvlHeight)
		if err != nil {
			return nil, fmt.Errorf("level %d: %w", i, err)
		}
		img.Levels[i] = Level{
			Width:  lvlWidth,
			Height: lvlHeight,
			Depth:  lvlDepth,
			Layers: layers,
			Faces:  faces,
			Bytes:  decoded,
		}
	}
	return img, nil
}

// BytesPerPixel returns the byte stride of one pixel in the given VkFormat,
// or 0 if the parser does not know the format. Clients should check
// IsSupportedFormat before upload planning.
func BytesPerPixel(vkFormat int) int {
	switch vkFormat {
	case VkFormatR8G8B8A8Unorm, VkFormatR8G8B8A8SRGB,
		VkFormatB8G8R8A8Unorm, VkFormatB8G8R8A8SRGB:
		return 4
	}
	return 0
}

// IsSupportedFormat reports whether the parser returns ready-to-upload
// bytes for the given VkFormat without format transcoding.
func IsSupportedFormat(vkFormat int) bool {
	_, ok := FormatBlockInfo(vkFormat)
	return ok
}

// FormatBlockInfo reports the upload block geometry for supported
// uncompressed and block-compressed pass-through formats.
func FormatBlockInfo(vkFormat int) (BlockInfo, bool) {
	if bpp := BytesPerPixel(vkFormat); bpp > 0 {
		return BlockInfo{Width: 1, Height: 1, BytesPerBlock: bpp}, true
	}

	switch vkFormat {
	case VkFormatBC7UnormBlock, VkFormatBC7SRGBBlock,
		VkFormatETC2R8G8B8A8UnormBlock, VkFormatETC2R8G8B8A8SRGBBlock,
		VkFormatASTC4x4UnormBlock, VkFormatASTC4x4SRGBBlock:
		return BlockInfo{Width: 4, Height: 4, BytesPerBlock: 16, Compressed: true}, true

	case VkFormatETC2R8G8B8UnormBlock, VkFormatETC2R8G8B8SRGBBlock:
		return BlockInfo{Width: 4, Height: 4, BytesPerBlock: 8, Compressed: true}, true

	case VkFormatASTC6x6UnormBlock, VkFormatASTC6x6SRGBBlock:
		return BlockInfo{Width: 6, Height: 6, BytesPerBlock: 16, Compressed: true}, true

	case VkFormatASTC8x8UnormBlock, VkFormatASTC8x8SRGBBlock:
		return BlockInfo{Width: 8, Height: 8, BytesPerBlock: 16, Compressed: true}, true
	}
	return BlockInfo{}, false
}

// RowPitch returns the number of bytes in one row of texel blocks for
// this level and format. It returns 0 for unknown formats.
func (l Level) RowPitch(vkFormat int) int {
	info, ok := FormatBlockInfo(vkFormat)
	if !ok {
		return 0
	}
	return divCeil(l.Width, info.Width) * info.BytesPerBlock
}

// BlockColumns returns the number of texel blocks across this level.
func (l Level) BlockColumns(vkFormat int) int {
	info, ok := FormatBlockInfo(vkFormat)
	if !ok {
		return 0
	}
	return divCeil(l.Width, info.Width)
}

// BlockRows returns the number of texel blocks down this level.
func (l Level) BlockRows(vkFormat int) int {
	info, ok := FormatBlockInfo(vkFormat)
	if !ok {
		return 0
	}
	return divCeil(l.Height, info.Height)
}

// --- internal ---

type header struct {
	vkFormat               uint32
	typeSize               uint32
	pixelWidth             uint32
	pixelHeight            uint32
	pixelDepth             uint32
	layerCount             uint32
	faceCount              uint32
	levelCount             uint32
	supercompressionScheme uint32
	dfdByteOffset          uint32
	dfdByteLength          uint32
	kvdByteOffset          uint32
	kvdByteLength          uint32
	sgdByteOffset          uint64
	sgdByteLength          uint64
}

func parseHeader(b []byte) header {
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }
	u64 := func(off int) uint64 { return binary.LittleEndian.Uint64(b[off:]) }
	return header{
		vkFormat:               u32(0),
		typeSize:               u32(4),
		pixelWidth:             u32(8),
		pixelHeight:            u32(12),
		pixelDepth:             u32(16),
		layerCount:             u32(20),
		faceCount:              u32(24),
		levelCount:             u32(28),
		supercompressionScheme: u32(32),
		dfdByteOffset:          u32(36),
		dfdByteLength:          u32(40),
		kvdByteOffset:          u32(44),
		kvdByteLength:          u32(48),
		sgdByteOffset:          u64(52),
		sgdByteLength:          u64(60),
	}
}

type levelEntry struct {
	byteOffset             uint64
	byteLength             uint64
	uncompressedByteLength uint64
}

func decompress(scheme uint32, raw []byte, uncompressedLen uint64, vkFormat, width, height int) ([]byte, error) {
	switch scheme {
	case schemeNone:
		if !IsSupportedFormat(vkFormat) {
			return nil, fmt.Errorf("%w: vkFormat %d (no transcoder)", ErrUnsupportedFormat, vkFormat)
		}
		return raw, nil

	case schemeZlib:
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("zlib reader: %w", err)
		}
		defer zr.Close()
		buf := make([]byte, 0, uncompressedLen)
		out, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("zlib read: %w", err)
		}
		if uncompressedLen != 0 && uint64(len(out)) != uncompressedLen {
			return nil, fmt.Errorf("zlib: expected %d bytes, got %d", uncompressedLen, len(out))
		}
		_ = buf
		return out, nil

	case schemeBasisLZ:
		if registeredBasis == nil {
			return nil, fmt.Errorf("%w: BasisLZ (register a transcoder)", ErrUnsupportedScheme)
		}
		return registeredBasis.Transcode(raw, vkFormat, width, height)

	case schemeZstd:
		return nil, fmt.Errorf("%w: Zstandard (bring-your-own decoder)", ErrUnsupportedScheme)
	}
	return nil, fmt.Errorf("%w: unknown scheme %d", ErrUnsupportedScheme, scheme)
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func normalizedCount(v uint32) int {
	return max1(int(v))
}

func divCeil(n, d int) int {
	if d <= 0 {
		return 0
	}
	return (n + d - 1) / d
}
