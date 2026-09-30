package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetailNilDoesNotChangeWire(t *testing.T) {
	material := StandardMaterial{Texture: "/base.png"}
	ir := (Props{Graph: NewGraph(Mesh{Material: material, Geometry: CubeGeometry{Size: 1}}, Model{Src: "/a.glb"})}).SceneIR()
	wire, err := json.Marshal(ir)
	if err != nil || strings.Contains(string(wire), "detail") {
		t.Fatalf("nil detail changed wire: %s, %v", wire, err)
	}
	if _, present := material.legacyMaterial()["detail"]; present {
		t.Fatal("nil detail present in material prop bag")
	}
}

func TestDetailLowersCamelCaseAndPreservesAuthoredValues(t *testing.T) {
	detail := &Detail{Ground: &DetailLayer{Albedo: "/sand.ktx2", Normal: "/normal.png", Roughness: "/rough.jpg", Scale: 3, NormalScale: 0.8, AlbedoMix: 0.4, RoughnessMix: 0.2}, Steep: &DetailLayer{Albedo: "/rock.png"}, SlopeStart: 25, SlopeEnd: 50, FadeStart: 5, FadeEnd: 12, Triplanar: Bool(false), Stochastic: Bool(false)}
	ir := (Props{Graph: NewGraph(Mesh{Material: StandardMaterial{Detail: detail}, Geometry: CubeGeometry{Size: 1}}, Model{Src: "/a.glb", Detail: detail})}).SceneIR()
	for _, got := range []*Detail{ir.Objects[0].Detail, ir.Models[0].Detail} {
		wire, err := json.Marshal(got)
		want := `{"ground":{"albedo":"/sand.ktx2","normal":"/normal.png","roughness":"/rough.jpg","scale":3,"normalScale":0.8,"albedoMix":0.4,"roughnessMix":0.2},"steep":{"albedo":"/rock.png"},"slopeStart":25,"slopeEnd":50,"triplanar":false,"fadeStart":5,"fadeEnd":12,"stochastic":false}`
		if err != nil || string(wire) != want {
			t.Fatalf("detail wire = %s (%v), want %s", wire, err, want)
		}
	}
	detail.Ground.Scale = 99
	if ir.Models[0].Detail.Ground.Scale != 3 {
		t.Fatal("lowered detail aliases authored data")
	}
}

func TestDetailDefaultsRemainBrowserOwnedAndModelWins(t *testing.T) {
	ir := (Props{Graph: NewGraph(Model{Src: "/a.glb", Material: StandardMaterial{Detail: &Detail{Steep: &DetailLayer{Scale: 7}}}, Detail: &Detail{Ground: &DetailLayer{}}})}).SceneIR()
	wire, err := json.Marshal(ir.Models[0].Detail)
	if err != nil || string(wire) != `{"ground":{}}` {
		t.Fatalf("Go baked defaults or ignored Model.Detail: %s, %v", wire, err)
	}
}
