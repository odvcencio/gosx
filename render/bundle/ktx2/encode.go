package ktx2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Encode errors.
var (
	// ErrEncodeFormat reports a VkFormat this writer cannot describe. The
	// writer emits a Basic Data Format Descriptor, so it only accepts the
	// uncompressed formats it can describe channel by channel.
	ErrEncodeFormat = errors.New("ktx2: cannot encode this VkFormat")
	// ErrEncodeShape reports level bytes that disagree with the declared
	// width, height, depth, layer, and face counts.
	ErrEncodeShape = errors.New("ktx2: level bytes do not match the declared shape")
	// ErrEncodeBlockCompressed reports a block-compressed VkFormat. The
	// writer refuses these on purpose. A BC7, ASTC, or ETC2 payload needs a
	// rate-distortion block encoder, and this package has none. Writing the
	// container without the encoder would produce a file that claims a
	// format nobody filled in.
	//
	// The error wraps ErrEncodeFormat, so a caller that only checks the
	// broader case keeps working.
	ErrEncodeBlockCompressed = fmt.Errorf("%w: this writer has no block encoder for compressed formats", ErrEncodeFormat)
)

// Supercompression selects the payload compression the writer applies.
type Supercompression int

const (
	// SupercompressionNone writes plain payload bytes with mip padding.
	SupercompressionNone Supercompression = schemeNone
	// SupercompressionZlib writes DEFLATE payload bytes (KTX2 scheme 3).
	// Parse decompresses this scheme with compress/zlib.
	SupercompressionZlib Supercompression = schemeZlib
)

// EncodeOptions controls container-level choices that Image does not carry.
type EncodeOptions struct {
	// Supercompression selects the payload compression scheme.
	Supercompression Supercompression
	// ZlibLevel selects the DEFLATE level, from -1 (default) to 9.
	ZlibLevel int
	// Writer fills the KTXwriter key. The writer supplies a default when
	// this field is empty.
	Writer string
	// KeyValues adds extra key/value entries. The writer sorts keys and
	// rejects keys that contain a NUL byte.
	KeyValues map[string]string
}

const defaultWriterID = "GoSX assetpipe ktx2 writer"

// Encode writes img as a KTX2 container. The writer emits the 12-byte
// identifier, the 68-byte header, the level index, a Basic Data Format
// Descriptor, a key/value block, and the level payloads.
//
// The writer stores level payloads from the smallest mip to the largest, as
// the KTX2 specification requires. Level 0 stays first in the level index.
//
// Encode accepts uncompressed formats only. A Basic Data Format Descriptor
// must describe every channel, and this writer does not build descriptors for
// block-compressed formats. Encode returns ErrEncodeFormat for those.
func Encode(img *Image, opts EncodeOptions) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("%w: nil image", ErrEncodeShape)
	}
	desc, ok := formatDescriptor(img.Format)
	if !ok {
		if block, known := FormatBlockInfo(img.Format); known && block.Compressed {
			return nil, fmt.Errorf("%w: vkFormat %d needs a %dx%d block encoder", ErrEncodeBlockCompressed, img.Format, block.Width, block.Height)
		}
		return nil, fmt.Errorf("%w: vkFormat %d", ErrEncodeFormat, img.Format)
	}
	if len(img.Levels) == 0 {
		return nil, fmt.Errorf("%w: no levels", ErrEncodeShape)
	}
	width := max1(img.Width)
	height := max1(img.Height)
	depth := img.Depth
	layers := img.Layers
	faces := max1(img.Faces)
	if faces != 1 && faces != 6 {
		return nil, fmt.Errorf("%w: faceCount %d must be 1 or 6", ErrEncodeShape, faces)
	}

	// Validate every level against the declared shape before writing bytes.
	for i, level := range img.Levels {
		wantWidth := max1(width >> i)
		wantHeight := max1(height >> i)
		wantDepth := max1(max1(depth) >> i)
		perImage := wantWidth * wantHeight * wantDepth * desc.bytesPerPixel
		images := max1(layers) * faces
		want := perImage * images
		if len(level.Bytes) != want {
			return nil, fmt.Errorf("%w: level %d has %d bytes, want %d (%dx%dx%d, %d images, %d bytes per pixel)",
				ErrEncodeShape, i, len(level.Bytes), want, wantWidth, wantHeight, wantDepth, images, desc.bytesPerPixel)
		}
	}

	scheme := uint32(opts.Supercompression)
	switch opts.Supercompression {
	case SupercompressionNone, SupercompressionZlib:
	default:
		return nil, fmt.Errorf("%w: scheme %d", ErrUnsupportedScheme, scheme)
	}

	payloads := make([][]byte, len(img.Levels))
	for i, level := range img.Levels {
		if opts.Supercompression == SupercompressionZlib {
			packed, err := zlibCompress(level.Bytes, opts.ZlibLevel)
			if err != nil {
				return nil, fmt.Errorf("ktx2: compress level %d: %w", i, err)
			}
			payloads[i] = packed
			continue
		}
		payloads[i] = level.Bytes
	}

	dfd := buildBasicDescriptor(desc)
	kvd := buildKeyValueData(opts)

	levelCount := len(img.Levels)
	indexBytes := levelCount * 24
	dfdOffset := 80 + indexBytes
	kvdOffset := dfdOffset + len(dfd)
	dataStart := kvdOffset + len(kvd)

	// Mip padding aligns each plain level to the least common multiple of the
	// texel block size and 4. Supercompressed levels need no padding.
	align := 1
	if opts.Supercompression == SupercompressionNone {
		align = lcm(desc.bytesPerPixel, 4)
	}

	type placed struct {
		offset int
		length int
	}
	slots := make([]placed, levelCount)
	cursor := dataStart
	// Write the smallest mip first, as the specification requires.
	for i := levelCount - 1; i >= 0; i-- {
		cursor = alignUp(cursor, align)
		slots[i] = placed{offset: cursor, length: len(payloads[i])}
		cursor += len(payloads[i])
	}
	total := cursor

	out := make([]byte, total)
	copy(out, identifier)

	h := out[12:80]
	putU32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(h[off:], v) }
	putU64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(h[off:], v) }
	putU32(0, uint32(img.Format))
	putU32(4, uint32(desc.typeSize))
	putU32(8, uint32(width))
	putU32(12, uint32(height))
	putU32(16, uint32(depth)) // 0 means a 2D texture.
	putU32(20, uint32(layers))
	putU32(24, uint32(faces))
	putU32(28, uint32(levelCount))
	putU32(32, scheme)
	putU32(36, uint32(dfdOffset))
	putU32(40, uint32(len(dfd)))
	putU32(44, uint32(kvdOffset))
	putU32(48, uint32(len(kvd)))
	putU64(52, 0) // sgdByteOffset, unused without BasisLZ.
	putU64(60, 0) // sgdByteLength.

	for i := 0; i < levelCount; i++ {
		off := 80 + i*24
		binary.LittleEndian.PutUint64(out[off+0:], uint64(slots[i].offset))
		binary.LittleEndian.PutUint64(out[off+8:], uint64(slots[i].length))
		binary.LittleEndian.PutUint64(out[off+16:], uint64(len(img.Levels[i].Bytes)))
	}
	copy(out[dfdOffset:], dfd)
	copy(out[kvdOffset:], kvd)
	for i := 0; i < levelCount; i++ {
		copy(out[slots[i].offset:], payloads[i])
	}
	return out, nil
}

func zlibCompress(data []byte, level int) ([]byte, error) {
	if level < -1 || level > 9 {
		level = -1
	}
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- data format descriptor ---

// KHR_DF constants the Basic Data Format Descriptor uses. See the Khronos
// Data Format Specification, section 5.
const (
	dfModelRGBSDA      = 1
	dfPrimariesBT709   = 1
	dfTransferLinear   = 1
	dfTransferSRGB     = 2
	dfSampleLinear     = 0x10
	dfSampleSigned     = 0x40
	dfSampleFloat      = 0x80
	dfChannelRed       = 0
	dfChannelGreen     = 1
	dfChannelBlue      = 2
	dfChannelAlpha     = 15
	dfDescriptorHeader = 24
	dfSampleWords      = 16
)

type sampleDesc struct {
	channel   int
	bitOffset int
	bitLength int
	qualifier int
	lower     uint32
	upper     uint32
}

type formatDesc struct {
	bytesPerPixel int
	typeSize      int
	transfer      int
	samples       []sampleDesc
}

// formatDescriptor returns the channel layout Encode writes into the Basic
// Data Format Descriptor. Only uncompressed formats appear here.
func formatDescriptor(vkFormat int) (formatDesc, bool) {
	unorm8 := func(order []int, srgb bool) formatDesc {
		transfer := dfTransferLinear
		if srgb {
			transfer = dfTransferSRGB
		}
		samples := make([]sampleDesc, 0, len(order))
		for i, channel := range order {
			qualifier := 0
			if srgb && channel == dfChannelAlpha {
				// The alpha channel stays linear in an sRGB format.
				qualifier = dfSampleLinear
			}
			samples = append(samples, sampleDesc{
				channel:   channel,
				bitOffset: i * 8,
				bitLength: 8,
				qualifier: qualifier,
				lower:     0,
				upper:     255,
			})
		}
		return formatDesc{bytesPerPixel: len(order), typeSize: 1, transfer: transfer, samples: samples}
	}
	sfloat := func(channels []int, bits int) formatDesc {
		samples := make([]sampleDesc, 0, len(channels))
		for i, channel := range channels {
			samples = append(samples, sampleDesc{
				channel:   channel,
				bitOffset: i * bits,
				bitLength: bits,
				qualifier: dfSampleSigned | dfSampleFloat,
				lower:     0xBF800000, // -1.0 as float32 bits.
				upper:     0x3F800000, // +1.0 as float32 bits.
			})
		}
		return formatDesc{
			bytesPerPixel: len(channels) * bits / 8,
			typeSize:      bits / 8,
			transfer:      dfTransferLinear,
			samples:       samples,
		}
	}

	rgba := []int{dfChannelRed, dfChannelGreen, dfChannelBlue, dfChannelAlpha}
	bgra := []int{dfChannelBlue, dfChannelGreen, dfChannelRed, dfChannelAlpha}
	r := []int{dfChannelRed}
	rg := []int{dfChannelRed, dfChannelGreen}
	rgb := []int{dfChannelRed, dfChannelGreen, dfChannelBlue}
	switch vkFormat {
	case VkFormatR8Unorm:
		return unorm8(r, false), true
	case VkFormatR8SRGB:
		return unorm8(r, true), true
	case VkFormatR8G8Unorm:
		return unorm8(rg, false), true
	case VkFormatR8G8SRGB:
		return unorm8(rg, true), true
	case VkFormatR8G8B8Unorm:
		return unorm8(rgb, false), true
	case VkFormatR8G8B8SRGB:
		return unorm8(rgb, true), true
	case VkFormatR8G8B8A8Unorm:
		return unorm8(rgba, false), true
	case VkFormatR8G8B8A8SRGB:
		return unorm8(rgba, true), true
	case VkFormatB8G8R8A8Unorm:
		return unorm8(bgra, false), true
	case VkFormatB8G8R8A8SRGB:
		return unorm8(bgra, true), true
	case VkFormatR16Sfloat:
		return sfloat([]int{dfChannelRed}, 16), true
	case VkFormatR16G16Sfloat:
		return sfloat([]int{dfChannelRed, dfChannelGreen}, 16), true
	case VkFormatR16G16B16A16Sfloat:
		return sfloat(rgba, 16), true
	case VkFormatR32Sfloat:
		return sfloat([]int{dfChannelRed}, 32), true
	case VkFormatR32G32Sfloat:
		return sfloat([]int{dfChannelRed, dfChannelGreen}, 32), true
	case VkFormatR32G32B32A32Sfloat:
		return sfloat(rgba, 32), true
	}
	return formatDesc{}, false
}

func buildBasicDescriptor(desc formatDesc) []byte {
	blockSize := dfDescriptorHeader + dfSampleWords*len(desc.samples)
	out := make([]byte, 4+blockSize)
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(out[off:], v) }
	put(0, uint32(len(out)))        // dfdTotalSize
	put(4, 0)                       // vendorId 0 (Khronos), descriptorType 0 (basic)
	put(8, 2|uint32(blockSize)<<16) // versionNumber 2, descriptorBlockSize
	put(12, uint32(dfModelRGBSDA)|  // colorModel
		uint32(dfPrimariesBT709)<<8| // colorPrimaries
		uint32(desc.transfer)<<16| // transferFunction
		0<<24) // flags: straight alpha
	put(16, 0)                          // texelBlockDimension 1x1x1x1 (stored as size-1)
	put(20, uint32(desc.bytesPerPixel)) // bytesPlane0, planes 1..3 zero
	put(24, 0)                          // bytesPlane4..7
	for i, sample := range desc.samples {
		off := 28 + i*dfSampleWords
		put(off+0, uint32(sample.bitOffset)|
			uint32(sample.bitLength-1)<<16|
			uint32(sample.channel|sample.qualifier)<<24)
		put(off+4, 0) // samplePosition0..3
		put(off+8, sample.lower)
		put(off+12, sample.upper)
	}
	return out
}

func buildKeyValueData(opts EncodeOptions) []byte {
	writer := strings.TrimSpace(opts.Writer)
	if writer == "" {
		writer = defaultWriterID
	}
	pairs := map[string]string{"KTXwriter": writer}
	for key, value := range opts.KeyValues {
		key = strings.TrimSpace(key)
		if key == "" || strings.ContainsRune(key, 0) {
			continue
		}
		pairs[key] = value
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, key := range keys {
		value := pairs[key]
		entry := make([]byte, 0, len(key)+len(value)+2)
		entry = append(entry, key...)
		entry = append(entry, 0)
		entry = append(entry, value...)
		entry = append(entry, 0)
		var head [4]byte
		binary.LittleEndian.PutUint32(head[:], uint32(len(entry)))
		buf.Write(head[:])
		buf.Write(entry)
		for buf.Len()%4 != 0 {
			buf.WriteByte(0)
		}
	}
	return buf.Bytes()
}

// KeyValues reads the key/value data block from a KTX2 container. It returns
// an empty map when the container carries no entries.
func KeyValues(data []byte) (map[string]string, error) {
	if len(data) < 80 {
		return nil, fmt.Errorf("%w: header requires 80 bytes, got %d", ErrTruncated, len(data))
	}
	if !bytes.Equal(data[:12], identifier) {
		return nil, ErrInvalidIdentifier
	}
	h := parseHeader(data[12:80])
	out := map[string]string{}
	if h.kvdByteLength == 0 {
		return out, nil
	}
	start := int(h.kvdByteOffset)
	end := start + int(h.kvdByteLength)
	if start < 0 || end > len(data) || start > end {
		return nil, fmt.Errorf("%w: key/value block out of range", ErrTruncated)
	}
	block := data[start:end]
	for len(block) >= 4 {
		size := int(binary.LittleEndian.Uint32(block))
		block = block[4:]
		if size > len(block) {
			return nil, fmt.Errorf("%w: key/value entry out of range", ErrTruncated)
		}
		entry := block[:size]
		if idx := bytes.IndexByte(entry, 0); idx >= 0 {
			key := string(entry[:idx])
			value := entry[idx+1:]
			value = bytes.TrimSuffix(value, []byte{0})
			out[key] = string(value)
		}
		advance := size
		if pad := advance % 4; pad != 0 {
			advance += 4 - pad
		}
		if advance > len(block) {
			break
		}
		block = block[advance:]
	}
	return out, nil
}

func alignUp(v, align int) int {
	if align <= 1 {
		return v
	}
	if rem := v % align; rem != 0 {
		return v + align - rem
	}
	return v
}

func lcm(a, b int) int {
	if a <= 0 || b <= 0 {
		return 1
	}
	return a / gcd(a, b) * b
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
