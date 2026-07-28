package texture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // Decode reads JPEG sources.
	_ "image/png"  // Decode reads PNG sources.
)

// MaxPixels bounds one decoded image.
//
// The package holds pixels as linear float32 RGBA, so one pixel costs 16
// bytes. At the bound a single image holds 16777216 * 16 bytes, which is 256
// MiB, and the mip chain adds about a third of that again. That is the largest
// working set a build machine should carry for one texture.
//
// 16777216 pixels is 4096 by 4096, the usual ceiling for an authored PBR
// texture set. Decode refuses a larger image and names the bound, so an author
// who really needs an 8K source downsamples it first, on purpose, rather than
// finding out through an out-of-memory kill.
//
// Decode checks the bound from the image header, before it allocates pixels. A
// crafted PNG cannot spend the memory it declares.
const MaxPixels = 1 << 24

// Errors the package returns.
var (
	// ErrUnsupportedSource reports an image format the standard library
	// cannot decode. WebP and AVIF land here.
	ErrUnsupportedSource = errors.New("texture: unsupported source image format")
	// ErrTooManyPixels reports an image above MaxPixels.
	ErrTooManyPixels = errors.New("texture: image exceeds MaxPixels")
	// ErrShape reports a size or channel count the operation cannot use.
	ErrShape = errors.New("texture: invalid image shape")
)

// AlphaMode records whether the colour channels carry the alpha factor.
type AlphaMode int

const (
	// AlphaStraight means the colour channels are independent of alpha.
	// PNG stores straight alpha, and glTF expects it.
	AlphaStraight AlphaMode = iota
	// AlphaPremultiplied means the colour channels already carry alpha.
	AlphaPremultiplied
)

// String names the alpha mode for a manifest or a metric.
func (a AlphaMode) String() string {
	if a == AlphaPremultiplied {
		return "premultiplied"
	}
	return "straight"
}

// Image holds linear-light RGBA pixels as float32. Pix has four values per
// pixel in row-major order, and holds Width*Height*4 values.
//
// The type stores linear light on purpose. Every filter in this package is a
// weighted sum, and a weighted sum is only correct on linear values.
type Image struct {
	Width, Height int
	Pix           []float32
	Alpha         AlphaMode
}

// NewImage allocates a transparent black image.
func NewImage(width, height int) *Image {
	if width < 1 || height < 1 {
		return &Image{Width: 0, Height: 0}
	}
	return &Image{Width: width, Height: height, Pix: make([]float32, width*height*4)}
}

// Pixels returns the pixel count.
func (img *Image) Pixels() int { return img.Width * img.Height }

// At returns the four linear channels of one pixel.
func (img *Image) At(x, y int) (float32, float32, float32, float32) {
	i := (y*img.Width + x) * 4
	return img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
}

// Set writes the four linear channels of one pixel.
func (img *Image) Set(x, y int, r, g, b, a float32) {
	i := (y*img.Width + x) * 4
	img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, a
}

// Clone returns a deep copy.
func (img *Image) Clone() *Image {
	out := &Image{Width: img.Width, Height: img.Height, Alpha: img.Alpha}
	out.Pix = make([]float32, len(img.Pix))
	copy(out.Pix, img.Pix)
	return out
}

// SourceInfo records what the decoder found in the source file.
type SourceInfo struct {
	Format     string     `json:"format"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Bytes      int        `json:"bytes"`
	BitDepth   int        `json:"bitDepth"`
	ColorSpace ColorSpace `json:"-"`
}

// Decode reads a PNG or JPEG source into linear float32 RGBA.
//
// space tells Decode how the file encodes its colour channels. An sRGB source
// passes through the inverse transfer function; a Linear source does not. The
// alpha channel never passes through a transfer function.
//
// Decode reads the header first and refuses an image above MaxPixels before it
// allocates any pixels.
func Decode(data []byte, space ColorSpace) (*Image, SourceInfo, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, SourceInfo{}, fmt.Errorf("%w: %v", ErrUnsupportedSource, err)
	}
	if config.Width < 1 || config.Height < 1 {
		return nil, SourceInfo{}, fmt.Errorf("%w: header declares %dx%d", ErrShape, config.Width, config.Height)
	}
	pixels := int64(config.Width) * int64(config.Height)
	if pixels > MaxPixels {
		return nil, SourceInfo{}, fmt.Errorf("%w: %dx%d is %d pixels, bound is %d",
			ErrTooManyPixels, config.Width, config.Height, pixels, MaxPixels)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, SourceInfo{}, fmt.Errorf("%w: %v", ErrUnsupportedSource, err)
	}
	info := SourceInfo{
		Format:     format,
		Width:      config.Width,
		Height:     config.Height,
		Bytes:      len(data),
		BitDepth:   8,
		ColorSpace: space,
	}
	bounds := src.Bounds()
	out := NewImage(bounds.Dx(), bounds.Dy())

	// A 16-bit source keeps its precision. Every other source normalizes
	// through NRGBA, which image/draw converts correctly, including the
	// premultiplied and YCbCr cases.
	switch typed := src.(type) {
	case *image.NRGBA64, *image.RGBA64, *image.Gray16:
		info.BitDepth = 16
		wide := image.NewNRGBA64(bounds)
		draw.Draw(wide, bounds, src, bounds.Min, draw.Src)
		fill16(out, wide, space)
	case *image.NRGBA:
		fill8(out, typed, space)
	default:
		narrow := image.NewNRGBA(bounds)
		draw.Draw(narrow, bounds, src, bounds.Min, draw.Src)
		fill8(out, narrow, space)
	}
	return out, info, nil
}

// fill8 copies an 8-bit straight-alpha image into linear float32 pixels.
func fill8(dst *Image, src *image.NRGBA, space ColorSpace) {
	bounds := src.Bounds()
	for y := 0; y < dst.Height; y++ {
		row := src.Pix[src.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
		for x := 0; x < dst.Width; x++ {
			r, g, b, a := row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]
			i := (y*dst.Width + x) * 4
			if space == SRGB {
				dst.Pix[i] = srgbDecodeLUT[r]
				dst.Pix[i+1] = srgbDecodeLUT[g]
				dst.Pix[i+2] = srgbDecodeLUT[b]
			} else {
				dst.Pix[i] = float32(r) / 255
				dst.Pix[i+1] = float32(g) / 255
				dst.Pix[i+2] = float32(b) / 255
			}
			dst.Pix[i+3] = float32(a) / 255
		}
	}
}

// fill16 copies a 16-bit straight-alpha image into linear float32 pixels.
func fill16(dst *Image, src *image.NRGBA64, space ColorSpace) {
	bounds := src.Bounds()
	for y := 0; y < dst.Height; y++ {
		row := src.Pix[src.PixOffset(bounds.Min.X, bounds.Min.Y+y):]
		for x := 0; x < dst.Width; x++ {
			off := x * 8
			r := float64(uint16(row[off])<<8|uint16(row[off+1])) / 65535
			g := float64(uint16(row[off+2])<<8|uint16(row[off+3])) / 65535
			b := float64(uint16(row[off+4])<<8|uint16(row[off+5])) / 65535
			a := float64(uint16(row[off+6])<<8|uint16(row[off+7])) / 65535
			i := (y*dst.Width + x) * 4
			if space == SRGB {
				dst.Pix[i] = float32(srgbToLinear(r))
				dst.Pix[i+1] = float32(srgbToLinear(g))
				dst.Pix[i+2] = float32(srgbToLinear(b))
			} else {
				dst.Pix[i] = float32(r)
				dst.Pix[i+1] = float32(g)
				dst.Pix[i+2] = float32(b)
			}
			dst.Pix[i+3] = float32(a)
		}
	}
}

// EncodeBytes writes one image as tightly packed 8-bit samples.
//
// channels selects how many samples each pixel gets, from 1 to 4. Channel 0 is
// red, 1 is green, 2 is blue, and 3 is alpha.
//
// space decides the transfer function of the colour channels. Alpha stays
// linear, which matches both the sRGB specification and the Basic Data Format
// Descriptor the KTX2 writer emits.
func EncodeBytes(img *Image, space ColorSpace, channels int) ([]byte, error) {
	if img == nil || img.Width < 1 || img.Height < 1 {
		return nil, fmt.Errorf("%w: empty image", ErrShape)
	}
	if channels < 1 || channels > 4 {
		return nil, fmt.Errorf("%w: %d channels, want 1 to 4", ErrShape, channels)
	}
	out := make([]byte, img.Width*img.Height*channels)
	for pixel := 0; pixel < img.Width*img.Height; pixel++ {
		src := pixel * 4
		dst := pixel * channels
		for c := 0; c < channels; c++ {
			value := img.Pix[src+c]
			if space == SRGB && c < 3 {
				out[dst+c] = LinearToSRGB8(value)
				continue
			}
			out[dst+c] = LinearToUnorm8(value)
		}
	}
	return out, nil
}
