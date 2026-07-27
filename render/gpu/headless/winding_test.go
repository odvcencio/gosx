package headless

import (
	"image"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/render/bundle"
)

// This file records one gap that changes how you should read every other test
// here: the CPU rasterizer does no back-face culling.
//
// rasterizeTriangle measures the signed area of the screen-space triangle and
// keeps a pixel when all three edge functions share that sign. Both windings
// therefore fill. So a wrongly wound geometry generator looks correct in every
// headless frame, and "flip a winding" is not a usable sensitivity probe for a
// golden test in this package.
//
// The browser runtime does not cull back faces today either, which is why a
// wrongly wound cap has never shown up as a visible defect. Both change on the
// day culling arrives. Until then, winding correctness has to be checked in
// package scene/geom against the signed area of the generated triangles, not
// against a rendered image.

// windingTriangle returns a screen-filling triangle in one of the two windings.
// The three corners are identical; only their order differs.
func windingTriangle(reversed bool) engine.RenderPassBundle {
	positions := []float64{
		-2, -2, 0,
		2, -2, 0,
		0, 2, 0,
	}
	colors := []float64{
		1, 0, 0,
		1, 0, 0,
		1, 0, 0,
	}
	if reversed {
		positions = []float64{
			0, 2, 0,
			2, -2, 0,
			-2, -2, 0,
		}
	}
	return engine.RenderPassBundle{Positions: positions, Colors: colors, VertexCount: 3}
}

func renderWindingFrame(t *testing.T, reversed bool) *image.RGBA {
	t.Helper()
	const size = 32
	device, surface := New(size, size)
	renderer, err := bundle.New(bundle.Config{Device: device, Surface: surface})
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}
	defer renderer.Destroy()
	frame := engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Z: 5, FOV: 1, Near: 0.1, Far: 100},
		Passes:     []engine.RenderPassBundle{windingTriangle(reversed)},
	}
	if err := renderer.Frame(frame, size, size, 0); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	return cloneTestRGBA(device.Framebuffer())
}

// TestBothWindingsFill states the no-culling behaviour as a fact instead of an
// assumption. Invert this test on the day the rasterizer starts culling, and say
// which winding it keeps.
func TestBothWindingsFill(t *testing.T) {
	forward := renderWindingFrame(t, false)
	reversed := renderWindingFrame(t, true)

	forwardColors, forwardVariance := frameEvidence(forward)
	reversedColors, reversedVariance := frameEvidence(reversed)
	if forwardColors < 2 || forwardVariance <= 0 {
		t.Fatalf("the forward triangle drew nothing: %d colours, variance %.6f",
			forwardColors, forwardVariance)
	}
	if reversedColors < 2 || reversedVariance <= 0 {
		t.Fatalf("the reversed triangle drew nothing (%d colours, variance %.6f). "+
			"The rasterizer now culls one winding. That is a real capability, so invert this test, "+
			"and add a winding case to the golden matrix because a winding regression is now visible.",
			reversedColors, reversedVariance)
	}
	if !imagesEqual(forward, reversed) {
		t.Fatal("the two windings produced different pixels; rasterizeTriangle no longer treats them alike")
	}
}
