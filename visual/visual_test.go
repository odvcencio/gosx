package visual

import (
	"bytes"
	"context"
	"errors"
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
	oldCapture := captureForAssert
	captureForAssert = func(ctx context.Context, url string, opts CaptureOptions) ([]byte, error) {
		if url != "https://example.test/update" {
			t.Fatalf("url = %q, want update URL", url)
		}
		if opts.Viewport.Width != 640 || opts.Viewport.Height != 360 {
			t.Fatalf("viewport = %#v, want 640x360", opts.Viewport)
		}
		return solidPNG(t, 8, 8, color.RGBA{G: 255, A: 255}), nil
	}
	t.Cleanup(func() { captureForAssert = oldCapture })

	baseline := filepath.Join(t.TempDir(), "baseline.png")
	err := Assert(context.Background(), "https://example.test/update", AssertOptions{
		BaselinePath: baseline,
		Update:       true,
		CaptureOptions: CaptureOptions{
			Viewport: Viewport{Width: 640, Height: 360, Scale: 1},
		},
	})
	if err != nil {
		t.Fatalf("Assert update: %v", err)
	}

	data, err := os.ReadFile(baseline)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	result, err := Diff(data, solidPNG(t, 8, 8, color.RGBA{G: 255, A: 255}))
	if err != nil {
		t.Fatalf("Diff written baseline: %v", err)
	}
	if result.Mismatched != 0 {
		t.Fatalf("written baseline mismatched = %d, want 0", result.Mismatched)
	}
}

func TestAssertWritesDiffAndCurrentOnMismatch(t *testing.T) {
	oldCapture := captureForAssert
	captureForAssert = func(context.Context, string, CaptureOptions) ([]byte, error) {
		return solidPNG(t, 6, 6, color.RGBA{B: 255, A: 255}), nil
	}
	t.Cleanup(func() { captureForAssert = oldCapture })

	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.png")
	diffPath := filepath.Join(dir, "baseline.diff.png")
	if err := os.WriteFile(baseline, solidPNG(t, 6, 6, color.RGBA{R: 255, A: 255}), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	err := Assert(context.Background(), "https://example.test/mismatch", AssertOptions{
		BaselinePath: baseline,
		DiffOutPath:  diffPath,
		Threshold:    0,
	})
	var mismatch *AssertMismatch
	if !errors.As(err, &mismatch) {
		t.Fatalf("Assert error = %v, want *AssertMismatch", err)
	}
	if mismatch.Result.DiffPct != 100 {
		t.Fatalf("DiffPct = %f, want 100", mismatch.Result.DiffPct)
	}
	if _, err := os.Stat(diffPath); err != nil {
		t.Fatalf("stat diff: %v", err)
	}
	currentPath := filepath.Join(dir, "baseline.current.png")
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("stat current: %v", err)
	}
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
