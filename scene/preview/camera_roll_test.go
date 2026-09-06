package preview_test

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

func TestCameraRollCapability(t *testing.T) {
	p := scene.Props{Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 0, 5)}, Graph: scene.NewGraph(scene.Mesh{Geometry: scene.BoxGeometry{Width: 2, Height: 1, Depth: 1}})}
	opts := preview.Options{Width: 16, Height: 16, DisableShadows: true, DisablePostFX: true}
	base, err := preview.Render(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	p.Camera.Rotation.Z = .4
	posterOpts := preview.NewPosterOptions(opts)
	// This intentionally simple two-colour fixture isolates the unsupported
	// capability gate rather than the independent shading-quality floor.
	posterOpts.Floors.UniqueColors = 2
	poster, err := preview.PosterFromJSON(p.RawPropsJSON(), posterOpts)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(preview.WritePoster(io.Discard, poster), preview.ErrLowFidelity) {
		t.Fatal("rolled poster not gated")
	}
	posterOpts.Floors.AllowDroppedRecords = true
	allowed, err := preview.PosterFromJSON(p.RawPropsJSON(), posterOpts)
	if err != nil {
		t.Fatal(err)
	}
	if err := preview.WritePoster(io.Discard, allowed); err != nil {
		t.Fatalf("explicit escape: %v", err)
	}
	for _, wire := range []bool{false, true} {
		var result *preview.Result
		if wire {
			result, err = preview.RenderJSON(p.RawPropsJSON(), opts)
		} else {
			result, err = preview.Render(p, opts)
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(base.Image.Pix, result.Image.Pix) {
			t.Fatal("roll unexpectedly implemented")
		}
		count := 0
		for _, d := range result.Bundle.Diagnostics {
			if d.Code == "scene.preview.unsupported_camera_roll" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("wire=%v ignored roll has %d diagnostics: %+v", wire, count, result.Bundle.Diagnostics)
		}
	}
	for _, angle := range []float64{0, 2 * math.Pi, -4 * math.Pi, 1e-10, .4} {
		o := opts
		o.Camera = &scene.PerspectiveCamera{Rotation: scene.Euler{Z: angle}}
		frame := preview.Bundle(p, o)
		count := 0
		for _, d := range frame.Diagnostics {
			if d.Code == "scene.preview.unsupported_camera_roll" {
				count++
			}
		}
		if (count == 1) != (angle == .4) {
			t.Fatalf("override angle %g diagnostics %+v", angle, frame.Diagnostics)
		}
	}
}

func TestOrthographicCameraRollCapability(t *testing.T) {
	p := scene.Props{OrthographicCamera: &scene.OrthographicCamera{Position: scene.Vec3(0, 0, 5), Rotation: scene.Euler{Z: .4}, Left: -2, Right: 2, Top: 1, Bottom: -1, Zoom: 2}, Graph: scene.NewGraph(scene.Mesh{Geometry: scene.BoxGeometry{Width: 2, Height: 1, Depth: 1}})}
	for _, wire := range []bool{false, true} {
		for _, override := range []*scene.PerspectiveCamera{nil, {Position: scene.Vec3(1, 2, 7), FOV: 60}} {
			opts := preview.Options{Width: 8, Height: 8, Camera: override, DisableShadows: true, DisablePostFX: true}
			var result *preview.Result
			var err error
			if wire {
				result, err = preview.RenderJSON(p.RawPropsJSON(), opts)
			} else {
				result, err = preview.Render(p, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, d := range result.Bundle.Diagnostics {
				if d.Code == "scene.preview.unsupported_camera_roll" {
					count++
				}
			}
			if override == nil {
				if count != 1 || result.Bundle.Camera.Mode != "orthographic" || result.Bundle.Camera.RotationZ != .4 {
					t.Fatalf("wire=%v lost/duplicate orthographic roll: camera=%+v diagnostics=%+v", wire, result.Bundle.Camera, result.Bundle.Diagnostics)
				}
			} else if count != 0 || result.Bundle.Camera != preview.Bundle(scene.Props{}, opts).Camera {
				t.Fatalf("wire=%v override did not govern: camera=%+v diagnostics=%+v", wire, result.Bundle.Camera, result.Bundle.Diagnostics)
			}
		}
	}
}
