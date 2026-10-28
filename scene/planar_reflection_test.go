package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMeshPlanarReflectionWireDefaultsAndCanonicalIR(t *testing.T) {
	for _, policy := range []*bool{nil, Bool(true), Bool(false)} {
		props := Props{Graph: NewGraph(Group{ID: "sky", Children: []Node{Mesh{ID: "moon", Geometry: SphereGeometry{Radius: 60}, PlanarReflection: policy}}})}
		legacy := props.SceneIR()
		if len(legacy.Objects) != 1 {
			t.Fatal("missing mesh")
		}
		object := legacy.Objects[0]
		wire, err := json.Marshal(object.legacyProps())
		if err != nil {
			t.Fatal(err)
		}
		canonical := props.CanonicalIR()
		encoded, err := json.Marshal(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if policy == nil {
			if strings.Contains(string(wire), "planarReflection") || strings.Contains(string(encoded), "planarReflection") {
				t.Fatal("default changed wire contract")
			}
		} else {
			if object.PlanarReflection == nil || *object.PlanarReflection != *policy {
				t.Fatal("lowering lost policy")
			}
			var decoded IR
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, n := range decoded.Nodes {
				if n.ID == "moon" {
					found = true
					if n.Mesh.PlanarReflection == nil || *n.Mesh.PlanarReflection != *policy {
						t.Fatal("canonical round trip lost policy")
					}
				}
			}
			if !found {
				t.Fatal("missing canonical mesh")
			}
		}
	}
}
func TestPlanarReflectionLivePatchKeepsGeometryAndRestoresDefault(t *testing.T) {
	for _, pair := range [][2]*bool{{nil, Bool(false)}, {Bool(false), nil}, {Bool(false), Bool(true)}} {
		before := ObjectIR{ID: "moon", Kind: "mesh", PlanarReflection: pair[0], Vertices: &MeshVertices{Positions: make([]float64, 30000)}}
		after := before
		after.PlanarReflection = pair[1]
		result := DiffScene(SceneIR{Objects: []ObjectIR{before}}, SceneIR{Objects: []ObjectIR{after}}, DiffOptions{PatchObjectState: true})
		if len(result.Commands) != 1 || result.Commands[0].Kind != CommandSetTransform {
			t.Fatalf("not a compact state patch: %#v", result.Commands)
		}
		encoded, err := json.Marshal(result.Commands[0].Data)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err = json.Unmarshal(encoded, &payload); err != nil {
			t.Fatal(err)
		}
		if got, ok := payload["planarReflection"]; !ok || got != (pair[1] == nil || *pair[1]) {
			t.Fatalf("policy reset lost: %s", encoded)
		}
		if strings.Contains(string(encoded), "vertices") || len(encoded) > 1024 {
			t.Fatalf("geometry was resent: %d bytes", len(encoded))
		}
		assertObjectPatchMatchesReference(t, before, after)
	}
}
