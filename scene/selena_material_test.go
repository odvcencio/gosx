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

const selenaBundleSource = `
material GalaxyDisk {
    param phase : float = 0.25
    param diskColor : color = rgb(0.44, 0.72, 1.0)
    surface(geo) -> color {
        return diskColor * phase
    }
}

material GalaxyCorona {
    param phase : float = 0.75
    param coronaColor : color = rgb(1.0, 0.58, 0.28)
    surface(geo) -> color {
        return coronaColor * phase
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

func TestCompileSelenaBundleCompilesOrderedMaterialSet(t *testing.T) {
	diskUniforms, err := SelenaUniforms(struct {
		Phase float32 `selena:"phase"`
	}{Phase: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	coronaUniforms, err := SelenaUniforms(struct {
		Phase float32 `selena:"phase"`
	}{Phase: 0.9})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := CompileSelenaBundle([]byte(selenaBundleSource),
		SelenaMaterialOptions{
			Material: "GalaxyDisk",
			Standard: StandardMaterial{
				Color:     "#ffffff",
				Emissive:  1.25,
				BlendMode: BlendAdditive,
			},
			Uniforms: diskUniforms,
		},
		SelenaMaterialOptions{
			Material: "GalaxyCorona",
			Standard: StandardMaterial{
				Color:     "#ffffff",
				Emissive:  1.75,
				BlendMode: BlendAdditive,
			},
			Uniforms: coronaUniforms,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle) != 2 {
		t.Fatalf("bundle materials = %d, want 2", len(bundle))
	}
	for i, want := range []string{"GalaxyDisk", "GalaxyCorona"} {
		got := bundle[i]
		if got.Name != want || got.Layout.Material != want {
			t.Fatalf("bundle[%d] = name %q layout %q, want %q", i, got.Name, got.Layout.Material, want)
		}
		if got.Material.ShaderBackend != "selena" {
			t.Fatalf("%s shader backend = %q, want selena", want, got.Material.ShaderBackend)
		}
		if got.Material.ShaderLayout["material"] != want {
			t.Fatalf("%s shader layout = %#v", want, got.Material.ShaderLayout)
		}
		if strings.TrimSpace(got.Material.VertexGLSL) == "" || strings.TrimSpace(got.Material.FragmentGLSL) == "" ||
			strings.TrimSpace(got.Material.VertexWGSL) == "" || strings.TrimSpace(got.Material.FragmentWGSL) == "" {
			t.Fatalf("%s missing emitted shader sources", want)
		}
	}
	if got := bundle[0].Material.Uniforms["phase"]; got != float32(0.4) {
		t.Fatalf("disk phase = %#v, want float32(0.4)", got)
	}
	if got := bundle[1].Material.Uniforms["phase"]; got != float32(0.9) {
		t.Fatalf("corona phase = %#v, want float32(0.9)", got)
	}
	if got := bundle[1].Material.Uniforms["coronaColor"]; !reflect.DeepEqual(got, []float32{1.0, 0.58, 0.28}) {
		t.Fatalf("corona color default = %#v", got)
	}
}

func TestCompileSelenaBundleDefaultsToLastMaterial(t *testing.T) {
	bundle, err := CompileSelenaBundle([]byte(selenaBundleSource))
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle) != 1 || bundle[0].Name != "GalaxyCorona" {
		t.Fatalf("default bundle = %+v, want last material GalaxyCorona", bundle)
	}
}

func TestSelenaUniformsConvertsTaggedStructsAndClonesSequences(t *testing.T) {
	type galaxyUniforms struct {
		Phase     float32    `selena:"phase"`
		DiskColor [3]float32 `selena:"diskColor"`
		Gain      float64    `json:"gain,omitempty"`
		Ignored   string     `selena:"-"`
	}
	uniforms, err := SelenaUniforms(galaxyUniforms{
		Phase:     0.25,
		DiskColor: [3]float32{0.44, 0.72, 1.0},
		Gain:      1.5,
		Ignored:   "nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := uniforms["phase"]; got != float32(0.25) {
		t.Fatalf("phase = %#v, want float32(0.25)", got)
	}
	if got, want := uniforms["diskColor"], []float32{0.44, 0.72, 1.0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("diskColor = %#v, want %#v", got, want)
	}
	if got := uniforms["gain"]; got != float64(1.5) {
		t.Fatalf("gain = %#v, want float64(1.5)", got)
	}
	if _, ok := uniforms["Ignored"]; ok {
		t.Fatalf("ignored field leaked into uniforms: %#v", uniforms)
	}
}

func TestSelenaUniformsClonesMapValues(t *testing.T) {
	color := []float32{1, 0.5, 0.25}
	uniforms, err := SelenaUniforms(map[string]any{"color": color})
	if err != nil {
		t.Fatal(err)
	}
	color[0] = 0
	if got, want := uniforms["color"], []float32{1, 0.5, 0.25}; !reflect.DeepEqual(got, want) {
		t.Fatalf("color = %#v, want cloned %#v", got, want)
	}
}

func TestSelenaUniformsRejectsUnsupportedRoot(t *testing.T) {
	_, err := SelenaUniforms(42)
	if err == nil || !strings.Contains(err.Error(), "struct or map") {
		t.Fatalf("unsupported root error = %v", err)
	}
}

func TestCompileSelenaMaterialReportsMissingMaterial(t *testing.T) {
	_, _, err := CompileSelenaMaterial([]byte(selenaDefaultsSource), SelenaMaterialOptions{Material: "Missing"})
	if err == nil || !strings.Contains(err.Error(), `material "Missing" not found`) {
		t.Fatalf("missing material error = %v", err)
	}
}
