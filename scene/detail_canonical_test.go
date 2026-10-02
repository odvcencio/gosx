package scene

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCanonicalDetailIdentityAndRoundTrip(t *testing.T) {
	sand := &Detail{Ground: &DetailLayer{Albedo: "/sand.png", Scale: 3}, Steep: &DetailLayer{Normal: "/cliff.png"}, Triplanar: Bool(false), Stochastic: Bool(false), FadeEnd: 12}
	rock := cloneDetail(sand)
	rock.Ground.Albedo = "/rock.png"
	material := func(detail *Detail) StandardMaterial { return StandardMaterial{Color: "#ffffff", Detail: detail} }
	ir := (Props{Graph: NewGraph(
		Mesh{ID: "sand", Geometry: CubeGeometry{Size: 1}, Material: material(sand)},
		Mesh{ID: "rock", Geometry: CubeGeometry{Size: 1}, Material: material(rock)},
		Model{ID: "sand-model", Src: "/model.glb", Detail: sand, Material: material(nil)},
		InstancedMesh{ID: "sand-instances", Count: 1, Geometry: CubeGeometry{Size: 1}, Material: material(sand)},
	)}).CanonicalIR()
	if len(ir.Materials) != 2 {
		t.Fatalf("sand and rock collapsed to %d materials, want 2", len(ir.Materials))
	}
	for _, node := range ir.Nodes {
		want := 0
		if node.ID == "rock" {
			want = 1
		}
		if node.MaterialIndex != want {
			t.Errorf("%s material index = %d, want %d", node.ID, node.MaterialIndex, want)
		}
	}
	wire, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip IR
	if err := json.Unmarshal(wire, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ir.Materials, roundTrip.Materials) {
		t.Fatal("canonical material JSON round trip changed materials")
	}
	var decoded struct {
		Materials []struct {
			Detail *Detail `json:"detail"`
		} `json:"materials"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	for i, want := range []*Detail{sand, rock} {
		if !reflect.DeepEqual(decoded.Materials[i].Detail, want) {
			t.Fatalf("material %d detail = %#v, want %#v", i, decoded.Materials[i].Detail, want)
		}
	}
}

func TestCanonicalDetailConversionsOwnTheirData(t *testing.T) {
	for _, convert := range []struct {
		name  string
		apply func(*Detail) IRMaterial
	}{
		{"object", func(d *Detail) IRMaterial { return materialFromObjectIR(ObjectIR{Detail: d}) }},
		{"instance", func(d *Detail) IRMaterial { return materialFromInstancedIR(InstancedMeshIR{Detail: d}) }},
	} {
		t.Run(convert.name, func(t *testing.T) {
			authored := &Detail{Ground: &DetailLayer{Albedo: "/sand.png"}, Steep: &DetailLayer{Normal: "/rock.png"}, Triplanar: Bool(false), Stochastic: Bool(false)}
			material := convert.apply(authored)
			before, err := canonicalMaterialKey(material)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Detail *Detail `json:"detail"`
			}
			if err := json.Unmarshal([]byte(before), &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded.Detail, authored) {
				t.Fatal("conversion dropped detail")
			}
			authored.Ground.Albedo = "/changed.png"
			authored.Steep.Normal = "/changed.png"
			*authored.Triplanar = true
			*authored.Stochastic = true
			after, err := canonicalMaterialKey(material)
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatal("canonical detail aliases authored data")
			}
			if changed, _ := canonicalMaterialKey(convert.apply(authored)); changed == before {
				t.Fatal("detail changes did not change material identity")
			}
		})
	}
}
