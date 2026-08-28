package imagepipe

import (
	"encoding/base64"
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

// gopherDocWebpBase64 is gopher-doc.1bpp.lossless.webp (75x100) from the
// golang.org/x/image/webp package's own test corpus (BSD-3-Clause,
// https://cs.opensource.google/go/x/image, LICENSE), already a transitive
// build input via the golang.org/x/image module dependency this package
// blank-imports for WebP decoding. Embedded here (and, separately, in
// server/image_test.go) so a webp probe/decode test is self-contained and
// does not depend on the module cache layout -- and, since Encode
// no longer has a built-in WebP path (gosx ships none; see RegisterEncoder),
// does not depend on one being registered either.
const gopherDocWebpBase64 = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

// writeTestWebP writes the embedded gopherDocWebpBase64 fixture (a real,
// fixed 75x100 WebP file) to dir/name.webp and returns the full path.
// It ignores width/height -- a probe/decode-only test cares that the file
// is a genuine WebP with positive dimensions, not a chosen size, and this
// package's own Encode has no built-in WebP path to generate one to size.
func writeTestWebP(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(gopherDocWebpBase64)
	if err != nil {
		t.Fatalf("decode embedded webp fixture: %v", err)
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
