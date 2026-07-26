package hdrimage

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"
)

// OpenEXR compression identifiers from the file format specification.
const (
	exrCompressNone = 0
	exrCompressRLE  = 1
	exrCompressZIPS = 2
	exrCompressZIP  = 3
	exrCompressPIZ  = 4
	exrCompressPXR  = 5
	exrCompressB44  = 6
	exrCompressB44A = 7
	exrCompressDWAA = 8
	exrCompressDWAB = 9
)

// OpenEXR pixel types.
const (
	exrPixelUint  = 0
	exrPixelHalf  = 1
	exrPixelFloat = 2
)

type exrChannel struct {
	name      string
	pixelType int32
	xSampling int32
	ySampling int32
}

func (c exrChannel) size() int {
	if c.pixelType == exrPixelHalf {
		return 2
	}
	return 4
}

// DecodeEXR reads the scanline subset of OpenEXR that build machines meet in
// practice: one part, one image level, and NONE, RLE, ZIPS, or ZIP
// compression. It rejects tiled, deep, multi-part, and lossy-compressed files
// with a named error rather than a wrong image.
func DecodeEXR(data []byte) (*Image, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("%w: header requires 8 bytes", ErrTruncated)
	}
	if binary.LittleEndian.Uint32(data) != 20000630 {
		return nil, fmt.Errorf("%w: bad exr magic", ErrFormat)
	}
	version := binary.LittleEndian.Uint32(data[4:])
	flags := version >> 8
	if flags&0x02 != 0 {
		return nil, fmt.Errorf("%w: tiled exr", ErrUnsupported)
	}
	if flags&0x08 != 0 {
		return nil, fmt.Errorf("%w: deep exr", ErrUnsupported)
	}
	if flags&0x10 != 0 {
		return nil, fmt.Errorf("%w: multi-part exr", ErrUnsupported)
	}

	pos := 8
	var channels []exrChannel
	compression := int32(-1)
	var xMin, yMin, xMax, yMax int32
	sawDataWindow := false
	for {
		name, next, err := readNullString(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		if name == "" {
			break
		}
		attrType, next, err := readNullString(data, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		if pos+4 > len(data) {
			return nil, fmt.Errorf("%w: attribute size", ErrTruncated)
		}
		size := int(int32(binary.LittleEndian.Uint32(data[pos:])))
		pos += 4
		if size < 0 || pos+size > len(data) {
			return nil, fmt.Errorf("%w: attribute %q payload", ErrTruncated, name)
		}
		payload := data[pos : pos+size]
		pos += size

		switch name {
		case "channels":
			parsed, err := parseEXRChannels(payload)
			if err != nil {
				return nil, err
			}
			channels = parsed
		case "compression":
			if len(payload) < 1 {
				return nil, fmt.Errorf("%w: compression attribute", ErrTruncated)
			}
			compression = int32(payload[0])
		case "dataWindow":
			if len(payload) < 16 {
				return nil, fmt.Errorf("%w: dataWindow attribute", ErrTruncated)
			}
			xMin = int32(binary.LittleEndian.Uint32(payload[0:]))
			yMin = int32(binary.LittleEndian.Uint32(payload[4:]))
			xMax = int32(binary.LittleEndian.Uint32(payload[8:]))
			yMax = int32(binary.LittleEndian.Uint32(payload[12:]))
			sawDataWindow = true
		}
		_ = attrType
	}
	if !sawDataWindow {
		return nil, fmt.Errorf("%w: missing dataWindow", ErrFormat)
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("%w: missing channels", ErrFormat)
	}
	width := int(xMax-xMin) + 1
	height := int(yMax-yMin) + 1
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: dataWindow %dx%d", ErrFormat, width, height)
	}
	if int64(width)*int64(height) > MaxPixels {
		return nil, fmt.Errorf("%w: %dx%d", ErrTooLarge, width, height)
	}

	linesPerBlock, err := exrLinesPerBlock(compression)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		if channel.xSampling != 1 || channel.ySampling != 1 {
			return nil, fmt.Errorf("%w: subsampled channel %q", ErrUnsupported, channel.name)
		}
		if channel.pixelType == exrPixelUint {
			return nil, fmt.Errorf("%w: uint channel %q", ErrUnsupported, channel.name)
		}
	}

	// Channels appear inside every block in alphabetical order.
	sort.Slice(channels, func(i, j int) bool { return channels[i].name < channels[j].name })
	lineBytes := 0
	offsets := make([]int, len(channels))
	for i, channel := range channels {
		offsets[i] = lineBytes
		lineBytes += width * channel.size()
	}

	blocks := (height + linesPerBlock - 1) / linesPerBlock
	if pos+blocks*8 > len(data) {
		return nil, fmt.Errorf("%w: offset table", ErrTruncated)
	}
	table := make([]uint64, blocks)
	for i := range table {
		table[i] = binary.LittleEndian.Uint64(data[pos+i*8:])
	}

	img := &Image{Width: width, Height: height, Pix: make([]float32, width*height*3), Source: "exr"}
	target := map[string]int{"R": 0, "G": 1, "B": 2}
	if _, hasRed := channelIndex(channels, "R"); !hasRed {
		if _, hasLuma := channelIndex(channels, "Y"); hasLuma {
			target = map[string]int{"Y": -1}
		} else {
			return nil, fmt.Errorf("%w: exr has no R/G/B or Y channel", ErrUnsupported)
		}
	}

	scratch := make([]byte, linesPerBlock*lineBytes)
	for _, offset := range table {
		if offset+8 > uint64(len(data)) {
			return nil, fmt.Errorf("%w: block header", ErrTruncated)
		}
		blockY := int32(binary.LittleEndian.Uint32(data[offset:]))
		blockSize := int(int32(binary.LittleEndian.Uint32(data[offset+4:])))
		start := int(offset) + 8
		if blockSize < 0 || start+blockSize > len(data) {
			return nil, fmt.Errorf("%w: block payload", ErrTruncated)
		}
		payload := data[start : start+blockSize]

		lines := linesPerBlock
		if remaining := int(yMax-blockY) + 1; remaining < lines {
			lines = remaining
		}
		if lines <= 0 {
			continue
		}
		want := lines * lineBytes
		block, err := exrDecompress(compression, payload, want, scratch)
		if err != nil {
			return nil, err
		}
		if len(block) < want {
			return nil, fmt.Errorf("%w: block holds %d bytes, want %d", ErrTruncated, len(block), want)
		}
		for line := 0; line < lines; line++ {
			y := int(blockY-yMin) + line
			if y < 0 || y >= height {
				continue
			}
			row := block[line*lineBytes:]
			for ci, channel := range channels {
				slot, ok := target[channel.name]
				if !ok {
					continue
				}
				src := row[offsets[ci] : offsets[ci]+width*channel.size()]
				dst := img.Pix[y*width*3:]
				for x := 0; x < width; x++ {
					value := readEXRSample(src, x, channel.pixelType)
					if slot < 0 {
						dst[x*3+0] = value
						dst[x*3+1] = value
						dst[x*3+2] = value
						continue
					}
					dst[x*3+slot] = value
				}
			}
		}
	}
	return img, nil
}

func readEXRSample(src []byte, x int, pixelType int32) float32 {
	if pixelType == exrPixelHalf {
		return HalfToFloat32(binary.LittleEndian.Uint16(src[x*2:]))
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(src[x*4:]))
}

func channelIndex(channels []exrChannel, name string) (int, bool) {
	for i, channel := range channels {
		if channel.name == name {
			return i, true
		}
	}
	return 0, false
}

func exrLinesPerBlock(compression int32) (int, error) {
	switch compression {
	case exrCompressNone, exrCompressRLE, exrCompressZIPS:
		return 1, nil
	case exrCompressZIP:
		return 16, nil
	case exrCompressPIZ:
		return 0, fmt.Errorf("%w: PIZ compression", ErrUnsupported)
	case exrCompressPXR:
		return 0, fmt.Errorf("%w: PXR24 compression", ErrUnsupported)
	case exrCompressB44, exrCompressB44A:
		return 0, fmt.Errorf("%w: B44 compression", ErrUnsupported)
	case exrCompressDWAA, exrCompressDWAB:
		return 0, fmt.Errorf("%w: DWA compression", ErrUnsupported)
	}
	return 0, fmt.Errorf("%w: compression id %d", ErrUnsupported, compression)
}

func exrDecompress(compression int32, payload []byte, want int, scratch []byte) ([]byte, error) {
	if len(payload) >= want {
		// OpenEXR stores a block raw when compression did not shrink it.
		return payload, nil
	}
	switch compression {
	case exrCompressNone:
		return payload, nil
	case exrCompressZIP, exrCompressZIPS:
		zr, err := zlib.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("hdrimage: exr zlib reader: %w", err)
		}
		defer zr.Close()
		raw, err := io.ReadAll(io.LimitReader(zr, int64(want)+1))
		if err != nil {
			return nil, fmt.Errorf("hdrimage: exr zlib read: %w", err)
		}
		return exrReconstruct(raw, scratch), nil
	case exrCompressRLE:
		raw, err := exrRLEDecode(payload, want)
		if err != nil {
			return nil, err
		}
		return exrReconstruct(raw, scratch), nil
	}
	return nil, fmt.Errorf("%w: compression id %d", ErrUnsupported, compression)
}

// exrReconstruct undoes the delta predictor and the two-half interleave that
// OpenEXR applies before ZIP and RLE compression.
func exrReconstruct(raw []byte, scratch []byte) []byte {
	for i := 1; i < len(raw); i++ {
		raw[i] = byte(int(raw[i-1]) + int(raw[i]) - 128)
	}
	out := scratch
	if len(out) < len(raw) {
		out = make([]byte, len(raw))
	}
	out = out[:len(raw)]
	half := (len(raw) + 1) / 2
	first, second := 0, half
	for i := 0; i < len(raw); i++ {
		if i%2 == 0 {
			out[i] = raw[first]
			first++
			continue
		}
		out[i] = raw[second]
		second++
	}
	return out
}

func exrRLEDecode(src []byte, want int) ([]byte, error) {
	out := make([]byte, 0, want)
	for i := 0; i < len(src); {
		control := int8(src[i])
		i++
		if control < 0 {
			count := int(-int(control))
			if i+count > len(src) {
				return nil, fmt.Errorf("%w: exr rle literal run", ErrTruncated)
			}
			out = append(out, src[i:i+count]...)
			i += count
			continue
		}
		count := int(control) + 1
		if i >= len(src) {
			return nil, fmt.Errorf("%w: exr rle repeat run", ErrTruncated)
		}
		value := src[i]
		i++
		for j := 0; j < count; j++ {
			out = append(out, value)
		}
	}
	return out, nil
}

func parseEXRChannels(payload []byte) ([]exrChannel, error) {
	var channels []exrChannel
	pos := 0
	for pos < len(payload) {
		if payload[pos] == 0 {
			break
		}
		name, next, err := readNullString(payload, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		if pos+16 > len(payload) {
			return nil, fmt.Errorf("%w: channel %q record", ErrTruncated, name)
		}
		channels = append(channels, exrChannel{
			name:      name,
			pixelType: int32(binary.LittleEndian.Uint32(payload[pos:])),
			xSampling: int32(binary.LittleEndian.Uint32(payload[pos+8:])),
			ySampling: int32(binary.LittleEndian.Uint32(payload[pos+12:])),
		})
		pos += 16
	}
	return channels, nil
}

func readNullString(data []byte, pos int) (string, int, error) {
	if pos >= len(data) {
		return "", pos, fmt.Errorf("%w: string", ErrTruncated)
	}
	end := bytes.IndexByte(data[pos:], 0)
	if end < 0 {
		return "", pos, fmt.Errorf("%w: unterminated string", ErrTruncated)
	}
	return string(data[pos : pos+end]), pos + end + 1, nil
}
