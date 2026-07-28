package hdrimage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Image holds linear RGB pixels in scanline order, top row first.
type Image struct {
	Width, Height int
	// Pix stores three float32 components per pixel: red, green, blue.
	Pix []float32
	// Source names the decoder that produced the image, "radiance" or "exr".
	Source string
}

// At returns the red, green, and blue value of one pixel.
func (img *Image) At(x, y int) (float32, float32, float32) {
	if img == nil || x < 0 || y < 0 || x >= img.Width || y >= img.Height {
		return 0, 0, 0
	}
	i := (y*img.Width + x) * 3
	return img.Pix[i], img.Pix[i+1], img.Pix[i+2]
}

// Decode errors.
var (
	ErrFormat      = errors.New("hdrimage: unrecognized file format")
	ErrUnsupported = errors.New("hdrimage: unsupported feature")
	ErrTruncated   = errors.New("hdrimage: data truncated")
	ErrTooLarge    = errors.New("hdrimage: image exceeds the pixel budget")
)

// MaxPixels bounds the decoded pixel count. The decoder holds twelve bytes
// per pixel, so this cap reserves at most 768 MB. An 8192x4096 panorama
// holds 33.5 million pixels and passes. A 16384x8192 panorama holds 134
// million pixels and fails with ErrTooLarge, so a build must downsample it
// first.
const MaxPixels = 64 << 20

// Decode reads either a Radiance RGBE file or an OpenEXR file. It selects the
// decoder from the leading magic bytes.
func Decode(data []byte) (*Image, error) {
	switch {
	case len(data) >= 4 && string(data[:2]) == "#?":
		return DecodeRadiance(data)
	case len(data) >= 4 && data[0] == 0x76 && data[1] == 0x2F && data[2] == 0x31 && data[3] == 0x01:
		return DecodeEXR(data)
	default:
		return nil, fmt.Errorf("%w: unknown magic bytes", ErrFormat)
	}
}

// DecodeRadiance reads a Radiance RGBE (.hdr) image. It accepts the flat and
// the run-length encoded scanline layouts, and the "-Y rows +X columns"
// resolution line that every common exporter writes.
func DecodeRadiance(data []byte) (*Image, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	format := ""
	exposure := float32(1)
	sawMagic := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("%w: header ended early", ErrTruncated)
		}
		line = strings.TrimRight(line, "\r\n")
		if !sawMagic {
			if !strings.HasPrefix(line, "#?") {
				return nil, fmt.Errorf("%w: missing #? signature", ErrFormat)
			}
			sawMagic = true
			continue
		}
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "FORMAT":
			format = strings.TrimSpace(value)
		case "EXPOSURE":
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 32); err == nil && parsed != 0 {
				exposure *= float32(parsed)
			}
		}
	}
	if format != "" && format != "32-bit_rle_rgbe" && format != "32-bit_rle_xyze" {
		return nil, fmt.Errorf("%w: radiance FORMAT %q", ErrUnsupported, format)
	}

	resolution, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("%w: missing resolution line", ErrTruncated)
	}
	width, height, err := parseResolution(strings.TrimRight(resolution, "\r\n"))
	if err != nil {
		return nil, err
	}
	if int64(width)*int64(height) > MaxPixels {
		return nil, fmt.Errorf("%w: %dx%d", ErrTooLarge, width, height)
	}

	img := &Image{Width: width, Height: height, Pix: make([]float32, width*height*3), Source: "radiance"}
	scanline := make([]byte, width*4)
	scale := float32(1)
	if exposure != 0 {
		scale = 1 / exposure
	}
	for y := 0; y < height; y++ {
		if err := readRadianceScanline(reader, scanline, width); err != nil {
			return nil, fmt.Errorf("scanline %d: %w", y, err)
		}
		row := img.Pix[y*width*3:]
		for x := 0; x < width; x++ {
			r, g, b := rgbeToFloat(scanline[x*4], scanline[x*4+1], scanline[x*4+2], scanline[x*4+3])
			row[x*3+0] = r * scale
			row[x*3+1] = g * scale
			row[x*3+2] = b * scale
		}
	}
	return img, nil
}

func parseResolution(line string) (int, int, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return 0, 0, fmt.Errorf("%w: resolution line %q", ErrFormat, line)
	}
	if fields[0] != "-Y" || fields[2] != "+X" {
		return 0, 0, fmt.Errorf("%w: resolution orientation %q %q", ErrUnsupported, fields[0], fields[2])
	}
	height, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: rows %q", ErrFormat, fields[1])
	}
	width, err := strconv.Atoi(fields[3])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: columns %q", ErrFormat, fields[3])
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("%w: resolution %dx%d", ErrFormat, width, height)
	}
	return width, height, nil
}

// readRadianceScanline fills dst with width RGBE quads.
func readRadianceScanline(reader *bufio.Reader, dst []byte, width int) error {
	if width < 8 || width > 0x7FFF {
		return readFlatScanline(reader, dst, width)
	}
	head, err := reader.Peek(4)
	if err != nil {
		return fmt.Errorf("%w: scanline header", ErrTruncated)
	}
	if head[0] != 2 || head[1] != 2 || int(head[2])<<8|int(head[3]) != width {
		return readFlatScanline(reader, dst, width)
	}
	if _, err := reader.Discard(4); err != nil {
		return fmt.Errorf("%w: scanline header", ErrTruncated)
	}
	for component := 0; component < 4; component++ {
		x := 0
		for x < width {
			count, err := reader.ReadByte()
			if err != nil {
				return fmt.Errorf("%w: run header", ErrTruncated)
			}
			if count > 128 {
				runLength := int(count) - 128
				value, err := reader.ReadByte()
				if err != nil {
					return fmt.Errorf("%w: run value", ErrTruncated)
				}
				if x+runLength > width {
					return fmt.Errorf("%w: run overruns the scanline", ErrFormat)
				}
				for i := 0; i < runLength; i++ {
					dst[(x+i)*4+component] = value
				}
				x += runLength
				continue
			}
			runLength := int(count)
			if runLength == 0 || x+runLength > width {
				return fmt.Errorf("%w: literal run overruns the scanline", ErrFormat)
			}
			for i := 0; i < runLength; i++ {
				value, err := reader.ReadByte()
				if err != nil {
					return fmt.Errorf("%w: literal value", ErrTruncated)
				}
				dst[(x+i)*4+component] = value
			}
			x += runLength
		}
	}
	return nil
}

// readFlatScanline reads the old layout, where a pixel of 1,1,1,n repeats the
// previous pixel n times shifted by the repeat index.
func readFlatScanline(reader *bufio.Reader, dst []byte, width int) error {
	shift := 0
	for x := 0; x < width; {
		var pixel [4]byte
		if _, err := io.ReadFull(reader, pixel[:]); err != nil {
			return fmt.Errorf("%w: pixel", ErrTruncated)
		}
		if pixel[0] == 1 && pixel[1] == 1 && pixel[2] == 1 {
			if x == 0 {
				return fmt.Errorf("%w: repeat marker starts the scanline", ErrFormat)
			}
			count := int(pixel[3]) << shift
			if x+count > width {
				count = width - x
			}
			prev := dst[(x-1)*4 : x*4]
			for i := 0; i < count; i++ {
				copy(dst[(x+i)*4:], prev)
			}
			x += count
			shift += 8
			continue
		}
		copy(dst[x*4:], pixel[:])
		x++
		shift = 0
	}
	return nil
}

func rgbeToFloat(r, g, b, e byte) (float32, float32, float32) {
	if e == 0 {
		return 0, 0, 0
	}
	scale := float32(math.Ldexp(1, int(e)-(128+8)))
	return float32(r) * scale, float32(g) * scale, float32(b) * scale
}
