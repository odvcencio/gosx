package preview_test

import (
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
	"testing"
)

func TestTypedOrthographicIngress(t *testing.T) {
	camera := scene.OrthographicCamera{Position: scene.Vec3(.2, .3, 5), Rotation: scene.Euler{X: .12, Y: -.18}, Left: 0, Right: 4, Bottom: -1, Top: 3, Zoom: 2, Near: .2, Far: 80}
	props := scene.Props{OrthographicCamera: &camera}
	canonical := props.CanonicalIR().Camera
	if canonical.Kind != "orthographic" || canonical.Right != 4 || canonical.Zoom != 2 || canonical.X != .2 {
		t.Fatalf("canonical camera %+v", canonical)
	}
	got := preview.Bundle(props, preview.Options{}).Camera
	if got.Mode != "orthographic" || got.X != .2 || got.Y != .3 || got.Z != 5 || got.RotationX != .12 || got.RotationY != -.18 || got.Left != 0 || got.Right != 4 || got.Bottom != -1 || got.Top != 3 || got.Zoom != 2 || got.Near != .2 || got.Far != 80 {
		t.Fatalf("typed camera lost: %+v", got)
	}
	override := scene.PerspectiveCamera{Position: scene.Vec3(1, 2, 7), FOV: 60}
	if a, b := preview.Bundle(props, preview.Options{Camera: &override}).Camera, preview.Bundle(scene.Props{Camera: override}, preview.Options{}).Camera; a != b {
		t.Fatalf("override %v != %v", a, b)
	}
	for _, empty := range []*scene.OrthographicCamera{nil, {}} {
		if a, b := preview.Bundle(scene.Props{OrthographicCamera: empty}, preview.Options{}).Camera, preview.Bundle(scene.Props{}, preview.Options{}).Camera; a != b {
			t.Fatalf("empty changed defaults %v %v", a, b)
		}
	}
	if got := preview.BundleIR(props.SceneIR(), preview.Options{}).Camera; got.Mode == "orthographic" {
		t.Fatal("bare SceneIR gained presentation")
	}
}
