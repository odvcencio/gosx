package preview_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

func TestOrthographicJSONIngress(t *testing.T) {
	props := scene.Props{Background: "#000000", OrthographicCamera: &scene.OrthographicCamera{Position: scene.Vec3(.2, .3, 5), Rotation: scene.Euler{X: .12, Y: -.18}, Left: -1, Right: 3, Bottom: -2, Top: 1, Zoom: 2, Near: .2, Far: 80}, Graph: scene.NewGraph(scene.Mesh{ID: "box", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: scene.StandardMaterial{Color: "#ffffff"}})}
	for _, size := range []int{8, 32} {
		opts := preview.Options{Width: size, Height: size, DisableShadows: true, DisablePostFX: true}
		a, err := preview.Render(props, opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := preview.RenderJSON(props.RawPropsJSON(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if a.Bundle.Camera != b.Bundle.Camera || !bytes.Equal(a.Image.Pix, b.Image.Pix) {
			t.Fatalf("size %d typed=%+v JSON=%+v pixelsEqual=%v", size, a.Bundle.Camera, b.Bundle.Camera, bytes.Equal(a.Image.Pix, b.Image.Pix))
		}
		camera := props.OrthographicCamera
		perspectiveOpts := opts
		perspectiveOpts.Background = props.Background
		perspectiveOpts.Camera = &scene.PerspectiveCamera{Position: camera.Position, Rotation: camera.Rotation, Near: camera.Near, Far: camera.Far}
		perspective, err := preview.RenderIR(props.SceneIR(), perspectiveOpts)
		if err != nil {
			t.Fatal(err)
		}
		coverage := func(pixels []byte) int {
			count := 0
			for i := 0; i < len(pixels); i += 4 {
				if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 0 {
					count++
				}
			}
			return count
		}
		orthoCoverage, perspectiveCoverage := coverage(a.Image.Pix), coverage(perspective.Image.Pix)
		if orthoCoverage == 0 || perspectiveCoverage == 0 || bytes.Equal(a.Image.Pix, perspective.Image.Pix) {
			t.Fatalf("size %d non-vacuous projection control: ortho coverage=%d perspective coverage=%d equal=%v", size, orthoCoverage, perspectiveCoverage, bytes.Equal(a.Image.Pix, perspective.Image.Pix))
		}
		t.Logf("size %d visible pixels: orthographic=%d perspective=%d; projection pixels differ", size, orthoCoverage, perspectiveCoverage)
	}
}

func TestJSONCameraCompatibility(t *testing.T) {
	for _, props := range []scene.Props{
		{}, {OrthographicCamera: &scene.OrthographicCamera{}},
		{Camera: scene.PerspectiveCamera{Position: scene.Vec3(1, 2, 5), FOV: 60}},
		{OrthographicCamera: &scene.OrthographicCamera{Right: 3, Zoom: 2}},
	} {
		props.Graph = scene.NewGraph(scene.Mesh{ID: "control", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}})
		for _, override := range []*scene.PerspectiveCamera{nil, {Position: scene.Vec3(2, 1, 8), FOV: 42}} {
			opts := preview.Options{Width: 2, Height: 2, Camera: override}
			got, err := preview.RenderJSON(props.RawPropsJSON(), opts)
			if err != nil {
				t.Fatal(err)
			}
			want := preview.Bundle(props, opts).Camera
			// Existing absent-camera JSON uses native default position, unlike
			// an explicitly supplied zero typed perspective camera.
			if override == nil && props.Camera == (scene.PerspectiveCamera{}) && (props.OrthographicCamera == nil || *props.OrthographicCamera == (scene.OrthographicCamera{})) {
				want = preview.BundleIR(props.SceneIR(), opts).Camera
			}
			if got.Bundle.Camera != want {
				t.Fatalf("got %+v want %+v", got.Bundle.Camera, want)
			}
			bare, err := json.Marshal(props.SceneIR())
			if err != nil {
				t.Fatal(err)
			}
			got, err = preview.RenderJSON(bare, opts)
			if err != nil {
				t.Fatal(err)
			}
			if want := preview.BundleIR(props.SceneIR(), opts).Camera; got.Bundle.Camera != want {
				t.Fatalf("bare got %+v want %+v", got.Bundle.Camera, want)
			}
		}
	}
}
