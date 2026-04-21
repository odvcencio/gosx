package headless

import (
	"image"
	"image/color"
	"testing"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/bundle"
	"github.com/orisano/pixelmatch"
)

func TestBundleFrameMatchesGoldenFullscreenQuad(t *testing.T) {
	d, surface := New(8, 8)
	r, err := bundle.New(bundle.Config{Device: d, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer r.Destroy()

	err = r.Frame(engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Passes: []engine.RenderPassBundle{{
			Positions: []float64{
				-100, -100, 0,
				100, -100, 0,
				-100, 100, 0,
				-100, 100, 0,
				100, -100, 0,
				100, 100, 0,
			},
			Colors: []float64{
				1, 0, 0,
				1, 0, 0,
				1, 0, 0,
				1, 0, 0,
				1, 0, 0,
				1, 0, 0,
			},
			VertexCount: 6,
		}},
	}, 8, 8, 0)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}

	want := solidGolden(8, 8, color.RGBA{R: 255, A: 255})
	assertGoldenMatch(t, d.Framebuffer(), want, 0)
}

func solidGolden(width, height int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func assertGoldenMatch(t *testing.T, got, want image.Image, maxDiff int) {
	t.Helper()
	diff, err := pixelmatch.MatchPixel(got, want, pixelmatch.Threshold(0.01))
	if err != nil {
		t.Fatalf("golden image compare: %v", err)
	}
	if diff > maxDiff {
		t.Fatalf("golden image diff = %d, want <= %d", diff, maxDiff)
	}
}
