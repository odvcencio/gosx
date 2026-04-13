package visual

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// solidPNG returns a PNG of the given size with a solid fill.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestDiffIdentical(t *testing.T) {
	red := solidPNG(t, 100, 100, color.RGBA{R: 255, A: 255})
	result, err := Diff(red, red)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !result.DimensionsMatch {
		t.Errorf("DimensionsMatch = false, want true")
	}
	if result.Mismatched != 0 {
		t.Errorf("Mismatched = %d, want 0", result.Mismatched)
	}
	if result.DiffPct != 0 {
		t.Errorf("DiffPct = %f, want 0", result.DiffPct)
	}
}

func TestDiffDifferent(t *testing.T) {
	red := solidPNG(t, 100, 100, color.RGBA{R: 255, A: 255})
	blue := solidPNG(t, 100, 100, color.RGBA{B: 255, A: 255})
	result, err := Diff(red, blue)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !result.DimensionsMatch {
		t.Errorf("DimensionsMatch = false, want true")
	}
	if result.Mismatched != 100*100 {
		t.Errorf("Mismatched = %d, want %d", result.Mismatched, 100*100)
	}
	if result.DiffPct != 100 {
		t.Errorf("DiffPct = %f, want 100", result.DiffPct)
	}
}

func TestDiffDimensionMismatch(t *testing.T) {
	small := solidPNG(t, 100, 100, color.RGBA{A: 255})
	large := solidPNG(t, 200, 100, color.RGBA{A: 255})
	result, err := Diff(small, large)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if result.DimensionsMatch {
		t.Errorf("DimensionsMatch = true, want false")
	}
	if result.DiffPct != 100 {
		t.Errorf("DiffPct = %f, want 100", result.DiffPct)
	}
}

func TestAssertUpdateCreatesBaseline(t *testing.T) {
	if _, skip := os.LookupEnv("CHROME_PATH"); !skip {
		t.Skip("CHROME_PATH not set; visual.Assert requires a browser")
	}
	// This test is a placeholder to document the Update workflow.
	// It is skipped when no browser is available.
}

func TestDefaultBaselinePath(t *testing.T) {
	got := DefaultBaselinePath("https://example.com/")
	want := filepath.Join("testdata", "visual")
	if !bytes.Contains([]byte(got), []byte(want)) {
		t.Errorf("DefaultBaselinePath = %q, want prefix %q", got, want)
	}
	if !bytes.HasSuffix([]byte(got), []byte(".png")) {
		t.Errorf("DefaultBaselinePath = %q, want .png suffix", got)
	}
}
