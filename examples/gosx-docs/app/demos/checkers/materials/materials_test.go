package materials

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

func TestFamiliesCompilePortableSelenaArtifacts(t *testing.T) {
	for _, family := range Families() {
		profile, err := Compile(family)
		if err != nil {
			t.Fatalf("%s: %v", family, err)
		}
		if profile.Layout.Material == "" || profile.Selena.ShaderBackend != "selena" {
			t.Fatalf("%s missing Selena contract: %+v", family, profile)
		}
		for name, source := range map[string]string{"WGSL": profile.Selena.VertexWGSL, "GLSL vertex": profile.Selena.VertexGLSL, "GLSL fragment": profile.Selena.FragmentGLSL} {
			if strings.TrimSpace(source) == "" {
				t.Fatalf("%s missing %s", family, name)
			}
		}
		ir := (scene.Props{Graph: scene.NewGraph(scene.Mesh{ID: string(family), Geometry: scene.SphereGeometry{Radius: 1, Segments: 16}, Material: profile.Selena})}).SceneIR()
		if len(ir.Objects) != 1 || ir.Objects[0].ShaderBackend != "selena" || ir.Objects[0].CustomVertexWGSL == "" || ir.Objects[0].CustomFragment == "" {
			t.Fatalf("%s did not lower through SceneIR: %+v", family, ir.Objects)
		}
	}
}

func TestFamiliesHaveDistinctOpticalContracts(t *testing.T) {
	jade, _ := Compile(ImperialJade)
	wood, _ := Compile(CarvedWood)
	steel, _ := Compile(BrushedSteel)
	lacquer, _ := Compile(MidnightLacquer)
	porcelain, _ := Compile(MoonPorcelain)
	if jade.Selena.Transmission <= 0 || jade.Selena.Clearcoat <= 0 {
		t.Fatalf("jade optical envelope = %+v", jade.Selena.StandardMaterial)
	}
	if wood.Selena.Metalness != 0 || wood.Selena.Roughness <= jade.Selena.Roughness {
		t.Fatalf("wood optical envelope = %+v", wood.Selena.StandardMaterial)
	}
	if steel.Selena.Metalness < 0.8 || steel.Selena.Anisotropy <= 0 {
		t.Fatalf("steel optical envelope = %+v", steel.Selena.StandardMaterial)
	}
	if lacquer.Selena.Clearcoat < 0.9 || lacquer.Selena.Roughness >= wood.Selena.Roughness {
		t.Fatalf("lacquer optical envelope = %+v", lacquer.Selena.StandardMaterial)
	}
	if porcelain.Selena.Iridescence <= 0 || porcelain.Selena.Metalness >= 0.1 {
		t.Fatalf("porcelain optical envelope = %+v", porcelain.Selena.StandardMaterial)
	}
}

func TestResolveLabelsFallbackHonestly(t *testing.T) {
	active, err := Resolve(ImperialJade, RuntimeCapabilities{SelenaMaterials: true, Transmission: false})
	if err != nil {
		t.Fatal(err)
	}
	if !active.Fallback || active.Backend != "standard-pbr" || !strings.Contains(active.Label, "fallback") || active.Reason != "transmission unavailable" {
		t.Fatalf("dishonest fallback: %+v", active)
	}
	if _, ok := active.Material.(scene.StandardMaterial); !ok {
		t.Fatalf("fallback material type = %T", active.Material)
	}
	full, err := Resolve(BrushedSteel, RuntimeCapabilities{SelenaMaterials: true, Transmission: true, Anisotropy: true})
	if err != nil {
		t.Fatal(err)
	}
	if full.Fallback || full.Backend != "selena" || !strings.Contains(full.Label, "Selena") {
		t.Fatalf("full material = %+v", full)
	}
}

func TestUnknownFamilyFailsClosed(t *testing.T) {
	if _, err := Compile("marble-mystery"); err == nil {
		t.Fatal("unknown family compiled")
	}
}
