//go:build !js || !wasm

package scene

import (
	"fmt"
	"image"
	"image/color"
	_ "image/png" // DecodeHeightmapPNG reads 8- and 16-bit PNG files.
	"io"
)

// DecodeHeightmapPNG reads a grayscale PNG. A 16-bit file keeps every bit; an
// 8-bit or color file uses its luminance scaled to 16 bits. The image's top
// row becomes the terrain's minimum-Z row.
//
// DecodeHeightmapPNG is not available in js/wasm builds, which do not link the
// PNG codec. In the browser, build the heightmap with HeightmapFromRaw, or
// decode the PNG on the server.
func DecodeHeightmapPNG(r io.Reader) (*Heightmap, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("scene: decode heightmap: %w", err)
	}
	bounds := img.Bounds()
	columns, rows := bounds.Dx(), bounds.Dy()
	if columns < 2 || rows < 2 || columns*rows > MaxHeightmapSamples {
		return nil, errHeightmapSize
	}
	h := &Heightmap{Columns: columns, Rows: rows, Data: make([]uint16, columns*rows)}
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			var value uint16
			switch typed := img.(type) {
			case *image.Gray16:
				value = typed.Gray16At(bounds.Min.X+column, bounds.Min.Y+row).Y
			case *image.Gray:
				value = uint16(typed.GrayAt(bounds.Min.X+column, bounds.Min.Y+row).Y) * 257
			default:
				value = color.Gray16Model.Convert(img.At(bounds.Min.X+column, bounds.Min.Y+row)).(color.Gray16).Y
			}
			h.Data[row*columns+column] = value
		}
	}
	return h, nil
}
