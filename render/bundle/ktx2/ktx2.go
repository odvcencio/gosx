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
)

// Errors the parser can return.
var (
	ErrInvalidIdentifier   = errors.New("ktx2: invalid file identifier")
	ErrTruncated           = errors.New("ktx2: data truncated")
	ErrUnsupportedFormat   = errors.New("ktx2: unsupported VkFormat")
	ErrUnsupportedScheme   = errors.New("ktx2: unsupported supercompression scheme")
	ErrUnsupportedGeometry = errors.New("ktx2: unsupported geometry (layers/faces/depth)")
)

// Image is a parsed KTX2 asset ready for GPU upload. Levels are ordered
// from highest (level 0 = full size) to lowest (smallest mip). All bytes
// are the uncompressed pixel payload in the VkFormat reported by Format.
type Image struct {
	// Format is the KTX2 VkFormat value — map to gpu.TextureFormat at
	// upload time via FormatToGPU.
	Format int

	// Width, Height are the dimensions of mip level 0 in pixels.
	Width, Height int

	// Levels holds per-mip pixel data. len(Levels) == original KTX2
	// levelCount; levels are pre-decompressed if a supercompression
	// scheme was in effect.
	Levels []Level
}

// Level describes one mip level's uncompressed pixel bytes + dimensions.
type Level struct {
	Width, Height int
	Bytes         []byte
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
	if h.faceCount > 1 || h.layerCount > 1 || h.pixelDepth > 1 {
		return nil, fmt.Errorf("%w: faces=%d layers=%d depth=%d",
			ErrUnsupportedGeometry, h.faceCount, h.layerCount, h.pixelDepth)
	}
	if h.levelCount == 0 {
		h.levelCount = 1
	}

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
		Levels: make([]Level, h.levelCount),
	}

	for i, entry := range levels {
		lvlWidth := max1(int(h.pixelWidth) >> i)
		lvlHeight := max1(int(h.pixelHeight) >> i)

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
// pixel bytes for the given VkFormat. Formats that require transcoding
// (Basis, BC7, ASTC) currently report false even when a transcoder is
// registered — clients route those through the transcoder directly.
func IsSupportedFormat(vkFormat int) bool {
	return BytesPerPixel(vkFormat) > 0
}

// --- internal ---

type header struct {
	vkFormat                uint32
	typeSize                uint32
	pixelWidth              uint32
	pixelHeight             uint32
	pixelDepth              uint32
	layerCount              uint32
	faceCount               uint32
	levelCount              uint32
	supercompressionScheme  uint32
	dfdByteOffset           uint32
	dfdByteLength           uint32
	kvdByteOffset           uint32
	kvdByteLength           uint32
	sgdByteOffset           uint64
	sgdByteLength           uint64
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
