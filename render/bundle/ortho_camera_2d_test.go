package bundle

import (
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

// TestOrthoCamera2DReturnsRenderCameraWithOrthoMode verifies the constructor
// produces a RenderCamera tagged with OrthoCamera2DMode so the renderer can
// branch into the 2D pipeline path. This is the primary contract — the
// CanvasBoardAdapter writes its camera through this constructor only.
func TestOrthoCamera2DReturnsRenderCameraWithOrthoMode(t *testing.T) {
	cam := OrthoCamera2D(1.0, 0, 0, 800, 600)
	if cam.Mode != OrthoCamera2DMode {
		t.Fatalf("Mode = %q, want %q", cam.Mode, OrthoCamera2DMode)
	}
	if !IsOrthoCamera2D(cam) {
		t.Fatalf("IsOrthoCamera2D = false, want true")
	}
}

// TestOrthoCamera2DEncodesZoomAndPan stashes pan into X/Y and zoom into Z so
// downstream consumers (computeMVP, the renderer's 2D path) can read it back
// without inventing new RenderCamera fields. Tests the field layout contract.
func TestOrthoCamera2DEncodesZoomAndPan(t *testing.T) {
	cam := OrthoCamera2D(2.5, 100, -50, 800, 600)
	if cam.X != 100 {
		t.Errorf("panX (in X) = %v, want 100", cam.X)
	}
	if cam.Y != -50 {
		t.Errorf("panY (in Y) = %v, want -50", cam.Y)
	}
	if cam.Z != 2.5 {
		t.Errorf("zoom (in Z) = %v, want 2.5", cam.Z)
	}
}

// TestOrthoCamera2DDefaultsZoomToOne guards against a sneaky divide-by-zero
// inside the projection matrix when callers forget to set zoom.
func TestOrthoCamera2DDefaultsZoomToOne(t *testing.T) {
	cam := OrthoCamera2D(0, 0, 0, 800, 600)
	if cam.Z != 1 {
		t.Errorf("zoom defaulted to %v, want 1", cam.Z)
	}
	cam = OrthoCamera2D(-3, 0, 0, 800, 600)
	if cam.Z != 1 {
		t.Errorf("negative zoom defaulted to %v, want 1", cam.Z)
	}
}

// TestIsOrthoCamera2DRejectsPerspectiveCamera ensures Scene3D's camera shape
// is not misclassified as 2D — a regression here would cause the renderer to
// silently drop lighting/depth on a 3D scene.
func TestIsOrthoCamera2DRejectsPerspectiveCamera(t *testing.T) {
	cam := engine.RenderCamera{Z: 6, FOV: math.Pi / 3, Near: 0.1, Far: 100}
	if IsOrthoCamera2D(cam) {
		t.Fatal("perspective camera misclassified as 2D")
	}
}

// TestComputeMVPOrthoCamera2DMapsCenter checks that the world point (panX, panY)
// projects to the framebuffer center under the 2D matrix. This is the property
// that lets <CanvasBoard> implement pan-around-cursor cleanly.
func TestComputeMVPOrthoCamera2DMapsCenter(t *testing.T) {
	width, height := 800, 600
	panX, panY := 250.0, -120.0
	cam := OrthoCamera2D(1.0, panX, panY, width, height)
	m := computeMVP(cam, width, height)

	// Multiply (panX, panY, 0, 1) by the matrix; expect NDC (0, 0).
	x := m[0]*float32(panX) + m[4]*float32(panY) + m[8]*0 + m[12]
	y := m[1]*float32(panX) + m[5]*float32(panY) + m[9]*0 + m[13]
	w := m[3]*float32(panX) + m[7]*float32(panY) + m[11]*0 + m[15]
	if w == 0 {
		t.Fatalf("MVP produced w=0 for pan point")
	}
	ndcX := x / w
	ndcY := y / w
	if math.Abs(float64(ndcX)) > 1e-5 {
		t.Errorf("pan point NDC x = %v, want ~0", ndcX)
	}
	if math.Abs(float64(ndcY)) > 1e-5 {
		t.Errorf("pan point NDC y = %v, want ~0", ndcY)
	}
}

// TestComputeMVPOrthoCamera2DScalesByZoom checks that a world distance of
// (width/2) pixels at zoom=1 maps to NDC=1 (right edge of viewport), and that
// zoom=2 halves the visible world span.
func TestComputeMVPOrthoCamera2DScalesByZoom(t *testing.T) {
	width, height := 800, 600

	cam1 := OrthoCamera2D(1.0, 0, 0, width, height)
	m1 := computeMVP(cam1, width, height)
	// World x = +400 at zoom=1 → NDC x = +1.
	x := m1[0]*400 + m1[12]
	w := m1[3]*400 + m1[15]
	if w == 0 {
		t.Fatalf("MVP produced w=0")
	}
	if ndc := x / w; math.Abs(float64(ndc-1)) > 1e-4 {
		t.Errorf("at zoom=1, world x=400 → NDC %v, want +1", ndc)
	}

	cam2 := OrthoCamera2D(2.0, 0, 0, width, height)
	m2 := computeMVP(cam2, width, height)
	// World x = +200 at zoom=2 should also map to NDC x = +1.
	x = m2[0]*200 + m2[12]
	w = m2[3]*200 + m2[15]
	if ndc := x / w; math.Abs(float64(ndc-1)) > 1e-4 {
		t.Errorf("at zoom=2, world x=200 → NDC %v, want +1", ndc)
	}
}

// TestConfigure2DBundleDisablesLightingDepthPostFX is the A1.3 acceptance:
// the 2D-mode pipeline-config switch (per ADR 0004) strips lighting,
// post-FX, and shadow-casting flags from a bundle. The CanvasBoardAdapter
// calls Configure2DBundle at the end of RenderBundle to enforce this.
func TestConfigure2DBundleDisablesLightingDepthPostFX(t *testing.T) {
	cam := OrthoCamera2D(1.0, 0, 0, 800, 600)
	b := &engine.RenderBundle{
		Camera: cam,
		Lights: []engine.RenderLight{
			{Kind: "directional", Intensity: 1, CastShadow: true},
		},
		Environment: engine.RenderEnvironment{
			AmbientIntensity: 0.5,
			Exposure:         1.1,
		},
		PostEffects: []engine.RenderPostEffect{
			{Kind: "bloom", Intensity: 0.3},
		},
		PostFXMaxPixels: 921600,
	}

	out := Configure2DBundle(b)
	if out != b {
		t.Fatalf("Configure2DBundle should return b for chaining")
	}
	if len(b.Lights) != 0 {
		t.Errorf("Lights = %#v, want nil", b.Lights)
	}
	if b.Environment != (engine.RenderEnvironment{}) {
		t.Errorf("Environment = %#v, want zero", b.Environment)
	}
	if len(b.PostEffects) != 0 {
		t.Errorf("PostEffects = %#v, want nil", b.PostEffects)
	}
	if b.PostFXMaxPixels != 0 {
		t.Errorf("PostFXMaxPixels = %d, want 0", b.PostFXMaxPixels)
	}
}

// TestConfigure2DBundleIsNoOpOnPerspectiveBundle protects against accidental
// stripping of Scene3D bundle fields if Configure2DBundle is called on the
// wrong bundle. The gate is camera-Mode-based.
func TestConfigure2DBundleIsNoOpOnPerspectiveBundle(t *testing.T) {
	b := &engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 5, FOV: math.Pi / 3, Near: 0.1, Far: 100},
		Lights: []engine.RenderLight{{Kind: "directional", Intensity: 1}},
		Environment: engine.RenderEnvironment{
			AmbientIntensity: 0.5,
		},
		PostEffects: []engine.RenderPostEffect{{Kind: "bloom"}},
	}
	Configure2DBundle(b)
	if len(b.Lights) != 1 {
		t.Errorf("3D bundle Lights mutated: %#v", b.Lights)
	}
	if b.Environment.AmbientIntensity != 0.5 {
		t.Errorf("3D bundle Environment mutated: %#v", b.Environment)
	}
	if len(b.PostEffects) != 1 {
		t.Errorf("3D bundle PostEffects mutated: %#v", b.PostEffects)
	}
}

// TestComputeMVPOrthoCamera2DProjectionIsOrthographic verifies that two points
// at different "depths" (Z values, which the 2D path zeroes out) map to the
// same NDC X/Y. This proves the projection is parallel (no perspective divide
// from Z) — the defining property of orthographic projection.
func TestComputeMVPOrthoCamera2DProjectionIsOrthographic(t *testing.T) {
	width, height := 800, 600
	cam := OrthoCamera2D(1.0, 0, 0, width, height)
	m := computeMVP(cam, width, height)

	worldX := float32(100)
	worldY := float32(50)

	x1 := m[0]*worldX + m[4]*worldY + m[8]*0 + m[12]
	y1 := m[1]*worldX + m[5]*worldY + m[9]*0 + m[13]
	w1 := m[3]*worldX + m[7]*worldY + m[11]*0 + m[15]

	x2 := m[0]*worldX + m[4]*worldY + m[8]*0.5 + m[12]
	y2 := m[1]*worldX + m[5]*worldY + m[9]*0.5 + m[13]
	w2 := m[3]*worldX + m[7]*worldY + m[11]*0.5 + m[15]

	if w1 != w2 {
		t.Errorf("orthographic should produce same w regardless of z: %v vs %v", w1, w2)
	}
	if x1/w1 != x2/w2 || y1/w1 != y2/w2 {
		t.Errorf("orthographic projection should be Z-invariant in xy: (%v,%v) vs (%v,%v)",
			x1/w1, y1/w1, x2/w2, y2/w2)
	}
}
