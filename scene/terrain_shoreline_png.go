//go:build !js || !wasm

package scene

import (
	"bytes"
	"image"
	"image/png"
)

// encodeShorelinePNG compresses n*n RGB texels into an opaque PNG.
func encodeShorelinePNG(rgb []byte, n int) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for k := 0; k < n*n; k++ {
		img.Pix[4*k] = rgb[3*k]
		img.Pix[4*k+1] = rgb[3*k+1]
		img.Pix[4*k+2] = rgb[3*k+2]
		img.Pix[4*k+3] = 255
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
