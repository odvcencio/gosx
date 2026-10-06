package scene_test

import (
	"encoding/json"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
	"m31labs.dev/selena"
	"testing"
)

func TestSelenaNativeProgramsSurviveIRAndRendererTransport(t *testing.T) {
	source := []byte(`material Portable { surface(geo) -> color { return rgb(1, 0, 0) } }`)
	material, _, err := scene.CompileSelenaMaterial(source, scene.SelenaMaterialOptions{Targets: selena.AllTargets()})
	if err != nil {
		t.Fatal(err)
	}
	props := scene.Props{Graph: scene.NewGraph(scene.Mesh{
		ID: "portable", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: material,
	})}
	canonical := props.CanonicalIR()
	encoded, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var decoded scene.IR
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Materials) != 1 {
		t.Fatalf("materials: %d", len(decoded.Materials))
	}
	legacy := props.SceneIR()
	data, _ := json.Marshal(legacy)
	var decodedLegacy scene.SceneIR
	if err := json.Unmarshal(data, &decodedLegacy); err != nil {
		t.Fatal(err)
	}
	frame := preview.BundleIR(decodedLegacy, preview.Options{})
	if len(frame.Materials) != 1 {
		t.Fatalf("native materials: %d", len(frame.Materials))
	}
	for _, target := range []string{"wgsl", "glsl", "metal", "gles"} {
		authored, ok := material.ShaderProgram(target)
		if !ok {
			t.Fatalf("missing authored %s", target)
		}
		irProgram, ok := decoded.Materials[0].ShaderProgram(target)
		if !ok || irProgram != authored {
			t.Fatalf("canonical %s changed", target)
		}
		nativeProgram, ok := frame.Materials[0].ShaderProgram(target)
		if !ok || nativeProgram != authored {
			t.Fatalf("native %s changed", target)
		}
	}
	found := false
	for _, diagnostic := range frame.Diagnostics {
		found = found || diagnostic.Code == "scene.native.custom_material_fallback"
	}
	if !found {
		t.Fatal("native preview must disclose standard material fallback")
	}
	defaultMaterial, _, err := scene.CompileSelenaMaterial(source, scene.SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := defaultMaterial.ShaderLayout["programs"]; ok {
		t.Fatal("default browser transport must not duplicate shaders")
	}
}
