package imagepipe

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// newTestImage generates a deterministic width x height RGBA gradient. It
// carries no external fixture: every pixel is a pure function of its own
// coordinates, so the same call always produces byte-identical pixels.
func newTestImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 255) / max(1, width-1)),
				G: uint8((y * 255) / max(1, height-1)),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}

// writeTestPNG writes a deterministic test image to dir/name.png and
// returns the full path.
func writeTestPNG(t *testing.T, dir, name string, width, height int) string {
	t.Helper()
	path := filepath.Join(dir, name+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, newTestImage(width, height)); err != nil {
		t.Fatalf("encode test PNG %s: %v", path, err)
	}
	return path
}

// writeTestJPEG writes a deterministic test image to dir/name.jpg and
// returns the full path.
func writeTestJPEG(t *testing.T, dir, name string, width, height int) string {
	t.Helper()
	path := filepath.Join(dir, name+".jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, newTestImage(width, height), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode test JPEG %s: %v", path, err)
	}
	return path
}

// writeTestWebP writes a deterministic test image to dir/name.webp (via
// this package's own Encode, so the fixture always matches whatever
// encoder version go.mod pins) and returns the full path.
func writeTestWebP(t *testing.T, dir, name string, width, height int) string {
	t.Helper()
	data, err := Encode(newTestImage(width, height), FormatWebP, EncodeOptions{})
	if err != nil {
		t.Fatalf("encode test WebP: %v", err)
	}
	path := filepath.Join(dir, name+".webp")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// readFileT reads path and fails the test on error.
func readFileT(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// writeFileT writes data to path and fails the test on error, returning
// path for convenient chaining.
func writeFileT(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}
