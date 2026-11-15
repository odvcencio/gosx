//go:build !js || !wasm

package scene

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"
)

func TestDecodeHeightmapPNGKeepsSixteenBits(t *testing.T) {
	img := image.NewGray16(image.Rect(0, 0, 3, 2))
	values := []uint16{0, 1, 65535, 1234, 40000, 65534}
	for i, value := range values {
		img.SetGray16(i%3, i/3, color.Gray16{Y: value})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	h, err := DecodeHeightmapPNG(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.Columns != 3 || h.Rows != 2 || !reflect.DeepEqual(h.Data, values) {
		t.Fatalf("decoded %dx%d %v, want 3x2 %v", h.Columns, h.Rows, h.Data, values)
	}

	gray := image.NewGray(image.Rect(0, 0, 2, 2))
	gray.SetGray(1, 1, color.Gray{Y: 255})
	buf.Reset()
	if err := png.Encode(&buf, gray); err != nil {
		t.Fatal(err)
	}
	h8, err := DecodeHeightmapPNG(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h8.Data[3] != 65535 || h8.Data[0] != 0 {
		t.Fatalf("8-bit PNG did not scale to 16 bits: %v", h8.Data)
	}
}
