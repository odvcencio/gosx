package preview_test

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

func TestRenderTypedSceneProducesVisiblePixels(t *testing.T) {
	props := scene.Props{
		Background:    "#05080c",
		Controls:      scene.ControlOrbit,
		ControlTarget: scene.Vec3(0, 0, 0),
		Camera:        scene.PerspectiveCamera{Position: scene.Vec3(0, 2.5, 5), FOV: 48, Near: 0.1, Far: 50},
		Environment:   scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.45},
		Graph: scene.NewGraph(scene.Mesh{
			ID: "authoring-cube", Geometry: scene.BoxGeometry{Width: 2, Height: 2, Depth: 2},
			Material: scene.StandardMaterial{Color: "#69e3c7", Roughness: 0.4},
		}),
	}
	result, err := preview.Render(props, preview.Options{Width: 128, Height: 96, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bundle.InstancedMeshes) != 1 || len(result.Bundle.Materials) != 1 {
		t.Fatalf("unexpected native bundle: %+v", result.Bundle)
	}
	background := color.RGBA{R: 5, G: 8, B: 12, A: 255}
	if countDifferent(result.Image, background) < 50 {
		t.Fatal("typed preview did not rasterize enough non-background pixels")
	}
	var encoded bytes.Buffer
	if err := preview.WritePNG(&encoded, result); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encoded.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("preview output is not PNG")
	}
}

func TestRenderJSONAcceptsRuntimePropsEnvelope(t *testing.T) {
	data := []byte(`{
		"background":"#010203",
		"camera":{"x":0,"y":0,"z":5,"fov":50,"near":0.1,"far":20},
		"scene":{"objects":[{"id":"sphere","kind":"sphere","radius":1,"segments":8,"color":"#ff8060"}]}
	}`)
	result, err := preview.RenderJSON(data, preview.Options{Width: 96, Height: 64, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Bundle.Background != "#010203" || len(result.Bundle.InstancedMeshes) != 1 {
		t.Fatalf("runtime props were not preserved: %+v", result.Bundle)
	}
}

func TestRenderWaterProducesAnimatedNativeVisualEvidence(t *testing.T) {
	props := scene.Props{
		Background: "#02070b", Controls: scene.ControlOrbit, ControlTarget: scene.Vec3(0, -0.5, 0),
		Camera:      scene.PerspectiveCamera{Position: scene.Vec3(1.2695827068526726, 1.1904730469627978, 3.395653196065958), FOV: 45, Near: 0.01, Far: 100},
		Environment: scene.Environment{AmbientColor: "#d8edf2", AmbientIntensity: 0.2},
		Graph: scene.NewGraph(scene.WaterSystem{
			ID: "water-main", Resolution: 256, SurfaceMeshResolution: 201,
			PoolWidth: 1, PoolHeight: 1, PoolLength: 1, SeedDrops: 20, DropStrength: 0.01,
			ShallowColor: "#7ad1eb", DeepColor: "#082e57", AboveWaterColor: scene.Vec3(0.25, 1, 1.25),
			Caustics: true, Reflection: true, Refraction: true,
		}),
	}
	first, err := preview.Render(props, preview.Options{Width: 160, Height: 100, Time: 0, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := preview.Render(props, preview.Options{Width: 160, Height: 100, Time: 0.75, DisableShadows: true, DisablePostFX: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Bundle.InstancedMeshes) != 6 || first.Bundle.InstancedMeshes[5].InstanceCount != 4096 {
		t.Fatalf("native water bundle did not contain pool + bounded heightfield: %+v", first.Bundle.InstancedMeshes)
	}
	for _, diagnostic := range first.Bundle.Diagnostics {
		if diagnostic.Code == "scene.preview.unsupported_water" {
			t.Fatalf("water must no longer be unsupported in native preview: %+v", diagnostic)
		}
	}
	if changed := countImageDifferences(first.Image, second.Image); changed < 40 {
		t.Fatalf("native water animation changed only %d pixels", changed)
	}
}

func countDifferent(image interface{ RGBAAt(int, int) color.RGBA }, background color.RGBA) int {
	count := 0
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			if image.RGBAAt(x, y) != background {
				count++
			}
		}
	}
	return count
}

func countImageDifferences(a, b interface {
	Bounds() image.Rectangle
	RGBAAt(int, int) color.RGBA
}) int {
	count := 0
	bounds := a.Bounds().Intersect(b.Bounds())
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				count++
			}
		}
	}
	return count
}
