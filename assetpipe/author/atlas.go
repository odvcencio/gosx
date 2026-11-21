package author

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strconv"
)

// Atlas is a texture of Cols x Rows square cells of Cell pixels, painted one
// cell at a time.
type Atlas struct {
	Cols, Rows, Cell int
	Pix              *image.RGBA
}

// NewAtlas returns a black atlas.
func NewAtlas(cols, rows, cell int) *Atlas {
	return &Atlas{cols, rows, cell, image.NewRGBA(image.Rect(0, 0, cols*cell, rows*cell))}
}

// Paint fills one cell. f gets the pixel's column and row inside the cell
// and returns red, green and blue in 0..255; values are clamped and then
// truncated, as a float-to-byte cast does.
func (a *Atlas) Paint(cell int, f func(col, row int) (r, g, b float64)) {
	x0, y0 := cell%a.Cols*a.Cell, cell/a.Cols*a.Cell
	for row := 0; row < a.Cell; row++ {
		for col := 0; col < a.Cell; col++ {
			r, g, b := f(col, row)
			a.Pix.SetRGBA(x0+col, y0+row, color.RGBA{clampByte(r), clampByte(g), clampByte(b), 255})
		}
	}
}

func clampByte(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

// PNG encodes the atlas losslessly at the best compression.
func (a *Atlas) PNG() []byte {
	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&out, a.Pix); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// JPEG encodes the atlas at the given quality. The standard library encoder
// subsamples chroma 4:2:0, so keep cell borders inside the cells' insets.
func (a *Atlas) JPEG(quality int) []byte {
	var out bytes.Buffer
	if err := jpeg.Encode(&out, a.Pix, &jpeg.Options{Quality: quality}); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// RGB is a colour in 0..255 per channel.
type RGB [3]float64

// Hex parses "#rrggbb".
func Hex(s string) (RGB, error) {
	if len(s) != 7 || s[0] != '#' {
		return RGB{}, fmt.Errorf("author: colour %q is not #rrggbb", s)
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return RGB{}, fmt.Errorf("author: colour %q: %w", s, err)
	}
	return RGB{float64(v >> 16), float64(v >> 8 & 255), float64(v & 255)}, nil
}

// MustHex parses "#rrggbb" and panics on a malformed colour.
func MustHex(s string) RGB {
	c, err := Hex(s)
	if err != nil {
		panic(err)
	}
	return c
}

// HexOf formats a colour as "#rrggbb", rounding each channel.
func HexOf(c RGB) string {
	b := func(v float64) int { return int(math.Round(math.Max(0, math.Min(255, v)))) }
	return fmt.Sprintf("#%02x%02x%02x", b(c[0]), b(c[1]), b(c[2]))
}
