//go:build !js || !wasm

package scene

import (
	"bytes"
	"image/png"
	"testing"
)

func testRGB(n int) []byte {
	rgb := make([]byte, 3*n*n)
	for i := range rgb {
		rgb[i] = byte(i*7 + i/251)
	}
	return rgb
}

func checkDecodedRGB(t *testing.T, data []byte, rgb []byte, n int) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != n || img.Bounds().Dy() != n {
		t.Fatalf("size = %v, want %dx%d", img.Bounds(), n, n)
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			k := 3 * (y*n + x)
			if byte(r>>8) != rgb[k] || byte(g>>8) != rgb[k+1] || byte(b>>8) != rgb[k+2] || a != 0xffff {
				t.Fatalf("pixel (%d,%d) = %d,%d,%d,%d want %d,%d,%d,255", x, y, r>>8, g>>8, b>>8, a>>8, rgb[k], rgb[k+1], rgb[k+2])
			}
		}
	}
}

func TestStoredPNGDecodes(t *testing.T) {
	for _, n := range []int{1, 32, 256} {
		rgb := testRGB(n)
		checkDecodedRGB(t, encodeStoredPNG(rgb, n, n), rgb, n)
	}
}

func TestStoredPNGMatchesNativeEncoderPixels(t *testing.T) {
	const n = 64
	rgb := make([]byte, 3*n*n)
	for k := 0; k < n*n; k++ {
		rgb[3*k] = byte(k * 3)
		rgb[3*k+1] = byte(k / 5)
	}
	native, err := encodeShorelinePNG(rgb, n)
	if err != nil {
		t.Fatal(err)
	}
	checkDecodedRGB(t, native, rgb, n)
	checkDecodedRGB(t, encodeStoredPNG(rgb, n, n), rgb, n)
}
