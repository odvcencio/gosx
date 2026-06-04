package scene

import (
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

const selenaDefaultsSource = `
material Defaults {
    param baseColor : color = rgb(0.78, 0.42, 0.98)
    param gain : float = 1.0
    surface(geo) -> color {
        return baseColor * gain
    }
}
`

func TestCompileSelenaMaterialFeedsScene3D(t *testing.T) {
	material, layout, err := CompileSelenaMaterial([]byte(selenaDefaultsSource), SelenaMaterialOptions{
		Standard: StandardMaterial{Color: "#ffffff", Roughness: 0.35},
		Uniforms: map[string]any{"gain": float32(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if layout.Material != "Defaults" {
		t.Fatalf("layout material = %q, want Defaults", layout.Material)
	}
	fieldNames := map[string]bool{}
	for _, field := range layout.UniformBlock.Fields {
		fieldNames[field.Name] = true
	}
	if layout.UniformBlock.Size == 0 || !fieldNames["baseColor"] || !fieldNames["gain"] {
		t.Fatalf("uniform layout = %+v, want authored baseColor and gain fields", layout.UniformBlock)
	}
	for name, source := range map[string]string{
		"VertexGLSL":   material.VertexGLSL,
		"FragmentGLSL": material.FragmentGLSL,
		"VertexWGSL":   material.VertexWGSL,
		"FragmentWGSL": material.FragmentWGSL,
	} {
		if strings.TrimSpace(source) == "" {
			t.Fatalf("%s is empty", name)
		}
	}
	if !strings.Contains(material.VertexWGSL, "vertexMain") || !strings.Contains(material.FragmentWGSL, "fragmentMain") {
		t.Fatalf("WGSL material does not carry both stage entrypoints:\n%s", material.VertexWGSL)
	}
	if got := material.Uniforms["gain"]; got != float32(2) {
		t.Fatalf("gain uniform = %#v, want float32(2)", got)
	}
	if got, want := material.Uniforms["baseColor"], []float32{0.78, 0.42, 0.98}; !reflect.DeepEqual(got, want) {
		t.Fatalf("baseColor uniform = %#v, want %#v", got, want)
	}
	if material.ShaderBackend != "selena" {
		t.Fatalf("shader backend = %q, want selena", material.ShaderBackend)
	}
	if got := material.ShaderLayout["schemaVersion"]; got != "selena.descriptor.v1" {
		t.Fatalf("shader layout schema = %#v", got)
	}

	props := Props{
		Graph: NewGraph(Mesh{
			ID:       "selena-box",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: material,
		}),
	}
	ir := props.SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(ir.Objects))
	}
	object := ir.Objects[0]
	if object.MaterialKind != "custom" || object.CustomVertex == "" || object.CustomFragment == "" ||
		object.CustomVertexWGSL == "" || object.CustomFragmentWGSL == "" {
		t.Fatalf("selena material did not lower into custom shader slots: %+v", object)
	}
	if object.ShaderBackend != "selena" || object.ShaderLayout["material"] != "Defaults" {
		t.Fatalf("selena descriptor did not reach SceneIR: backend=%q layout=%#v", object.ShaderBackend, object.ShaderLayout)
	}

	capable := backendSet(ir.BackendCaps.Capable)
	for _, backend := range []capability.Backend{capability.BackendWebGPU, capability.BackendWebGL, capability.BackendCanvas2D} {
		if !capable[backend] {
			t.Fatalf("backend %q excluded by selena material, caps=%+v", backend, ir.BackendCaps)
		}
	}
}

func TestCompileSelenaMaterialReportsMissingMaterial(t *testing.T) {
	_, _, err := CompileSelenaMaterial([]byte(selenaDefaultsSource), SelenaMaterialOptions{Material: "Missing"})
	if err == nil || !strings.Contains(err.Error(), `material "Missing" not found`) {
		t.Fatalf("missing material error = %v", err)
	}
}
