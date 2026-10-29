package scene

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Planar reflection participation. The ocean's planar mirror draws every opaque mesh a second time, upside down under the
// water. A mesh is drawn unless its author opts out with PlanarReflection: Bool(false). Nil and true both mean "drawn", and
// a scene that never sets the field must serialize exactly as it did before the field existed.

// objectStatePatchJSON is the text DiffScene sends, with PatchObjectState on, to change one object's participation when
// nothing else about it changed. participates is "true", "false", or "" for a patch that leaves the value alone.
// client/js/scene3d-reflections.test.js applies the same text to the browser runtime.
func objectStatePatchJSON(participates string) string {
	text := `{"x":80,"y":380,"z":-1330,"rotationX":0,"rotationY":0,"rotationZ":0,"scaleX":1,"scaleY":1,"scaleZ":1,"parentMatrix":null,` +
		`"interactive":false,"label":"","interactiveOrder":0,"visible":true,"selected":false`
	if participates != "" {
		text += `,"planarReflection":` + participates
	}
	return text + `}`
}

// A mesh with enough geometry that a resent record would be obvious.
func moonObject(participates *bool) ObjectIR {
	return ObjectIR{ID: "moon", Kind: "sphere", Radius: 60, X: 80, Y: 380, Z: -1330, PlanarReflection: participates,
		Vertices: &MeshVertices{Count: 10000, Positions: make([]float64, 30000)}}
}

func TestMeshPlanarReflectionLowersToObjectAndCanonicalRecords(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy *bool
		key    string // the JSON text the record must carry; empty means the key must be absent
	}{
		{"unset", nil, ""},
		{"true", Bool(true), `"planarReflection":true`},
		{"false", Bool(false), `"planarReflection":false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := Props{Graph: NewGraph(Group{ID: "sky", Children: []Node{Mesh{ID: "moon", Geometry: SphereGeometry{Radius: 60}, PlanarReflection: tc.policy}}})}
			objects := props.SceneIR().Objects
			if len(objects) != 1 {
				t.Fatalf("lowered %d objects, want 1", len(objects))
			}
			if got := objects[0].PlanarReflection; (got == nil) != (tc.policy == nil) || (got != nil && *got != *tc.policy) {
				t.Fatalf("ObjectIR.PlanarReflection = %v, want %v", got, tc.policy)
			}
			assertWireKey := func(label string, value any) {
				t.Helper()
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				has := strings.Contains(string(encoded), "planarReflection")
				if tc.key == "" && has {
					t.Fatalf("%s carries planarReflection for an unset field: %s", label, encoded)
				}
				if tc.key != "" && !strings.Contains(string(encoded), tc.key) {
					t.Fatalf("%s lacks %s: %s", label, tc.key, encoded)
				}
			}
			assertWireKey("SceneIR object", objects[0])
			assertWireKey("legacy props", props.LegacyProps()["scene"])
			canonical := props.CanonicalIR()
			assertWireKey("canonical IR", canonical)
			var found bool
			for _, node := range canonical.Nodes {
				if node.ID != "moon" {
					continue
				}
				found = true
				if got := node.Mesh.PlanarReflection; (got == nil) != (tc.policy == nil) || (got != nil && *got != *tc.policy) {
					t.Fatalf("canonical mesh PlanarReflection = %v, want %v", got, tc.policy)
				}
				// The canonical record survives a JSON round trip.
				encoded, err := json.Marshal(node)
				if err != nil {
					t.Fatal(err)
				}
				var decoded IRNode
				if err := json.Unmarshal(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				if got := decoded.Mesh.PlanarReflection; (got == nil) != (tc.policy == nil) || (got != nil && *got != *tc.policy) {
					t.Fatalf("decoded canonical mesh PlanarReflection = %v, want %v", got, tc.policy)
				}
			}
			if !found {
				t.Fatal("canonical IR lost the mesh")
			}
		})
	}
}

func TestLowerMeshElementCarriesPlanarReflection(t *testing.T) {
	if got := LowerMesh(MeshElementProps{ID: "moon", Kind: "sphere"}).Mesh.PlanarReflection; got != nil {
		t.Fatalf("unset element lowered to %v", *got)
	}
	if got := LowerMesh(MeshElementProps{ID: "moon", Kind: "sphere", PlanarReflection: Bool(false)}).Mesh.PlanarReflection; got == nil || *got {
		t.Fatalf("opt-out element lowered to %v", got)
	}
}

func TestModelPlanarReflectionLowersToModelRecord(t *testing.T) {
	for _, policy := range []*bool{nil, Bool(true), Bool(false)} {
		props := Props{Graph: NewGraph(Model{ID: "dome", Src: "/dome.glb", PlanarReflection: policy})}
		models := props.SceneIR().Models
		if len(models) != 1 {
			t.Fatalf("lowered %d models, want 1", len(models))
		}
		// The legacy map form carries the same value.
		legacy := props.LegacyProps()["scene"].(map[string]any)["models"].([]map[string]any)
		if value, has := legacy[0]["planarReflection"]; has != (policy != nil) || (has && value != *policy) {
			t.Fatalf("legacy model record planarReflection = %v (present %v), policy %v", value, has, policy)
		}
		got := models[0].PlanarReflection
		if (got == nil) != (policy == nil) || (got != nil && *got != *policy) {
			t.Fatalf("ModelIR.PlanarReflection = %v, want %v", got, policy)
		}
		encoded, err := json.Marshal(models[0])
		if err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(string(encoded), "planarReflection"); has != (policy != nil) {
			t.Fatalf("model JSON %s, policy %v", encoded, policy)
		}
	}
}

func TestPlanarReflectionStatePatchIsCompactAndNamesTheValueItSets(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after *bool
		sets          string // the value the patch names, or "" when participation is unchanged and the key is left out
	}{
		{"opt out of the default", nil, Bool(false), "false"},
		{"opt out of an explicit true", Bool(true), Bool(false), "false"},
		{"reset to the default", Bool(false), nil, "true"},
		{"reset to an explicit true", Bool(false), Bool(true), "true"},
		{"explicit true is the default", nil, Bool(true), ""},
		{"default is an explicit true", Bool(true), nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := moonObject(tc.before), moonObject(tc.after)
			diff := DiffScene(SceneIR{Objects: []ObjectIR{before}}, SceneIR{Objects: []ObjectIR{after}}, DiffOptions{PatchObjectState: true})
			if len(diff.Commands) != 1 || diff.Commands[0].Kind != CommandSetTransform || diff.Commands[0].ObjectID != "moon" {
				t.Fatalf("want one state patch for moon, got %#v", diff.Commands)
			}
			encoded, err := json.Marshal(diff.Commands[0].Data)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > 512 || strings.Contains(string(encoded), "vertices") {
				t.Fatalf("the patch resent geometry (%d bytes): %.200s", len(encoded), encoded)
			}
			if want := objectStatePatchJSON(tc.sets); string(encoded) != want {
				t.Fatalf("patch\n got %s\nwant %s", encoded, want)
			}
			assertObjectPatchMatchesReference(t, before, after)
		})
	}
}

func TestPlanarReflectionChangeAlongsideGeometryReplacesTheRecord(t *testing.T) {
	before, after := moonObject(nil), moonObject(Bool(false))
	after.Radius = 61
	for _, options := range []DiffOptions{{}, {PatchTransforms: true}, {PatchObjectState: true}} {
		commands := DiffScene(SceneIR{Objects: []ObjectIR{before}}, SceneIR{Objects: []ObjectIR{after}}, options).Commands
		if len(commands) != 2 || commands[0].Kind != CommandRemoveObject || commands[1].Kind != CommandCreateObject {
			t.Fatalf("options %+v: want remove then create, got %#v", options, commands)
		}
		encoded, err := json.Marshal(commands[1].Data)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"planarReflection":false`) {
			t.Fatalf("options %+v: the create payload lost the opt-out: %.200s", options, encoded)
		}
	}
	// A participation change alone, without the state option, also replaces the record: a transform patch cannot carry it.
	for _, options := range []DiffOptions{{}, {PatchTransforms: true}} {
		commands := DiffScene(SceneIR{Objects: []ObjectIR{moonObject(nil)}}, SceneIR{Objects: []ObjectIR{moonObject(Bool(false))}}, options).Commands
		if len(commands) != 2 || commands[1].Kind != CommandCreateObject {
			t.Fatalf("options %+v: want remove then create, got %#v", options, commands)
		}
	}
}

func TestUnchangedPlanarReflectionEmitsNothing(t *testing.T) {
	for _, policy := range []*bool{nil, Bool(true), Bool(false)} {
		scene := SceneIR{Objects: []ObjectIR{moonObject(policy)}}
		if commands := DiffScene(scene, scene, DiffOptions{PatchObjectState: true}).Commands; len(commands) != 0 {
			t.Fatalf("policy %v: identical scenes produced %#v", policy, commands)
		}
	}
}

func TestPublicSchemaDeclaresPlanarReflectionOnObjectsAndModels(t *testing.T) {
	data, err := os.ReadFile("schema/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	defs := doc["$defs"].(map[string]any)
	declared := false
	for _, part := range defs["object"].(map[string]any)["allOf"].([]any) {
		properties, _ := part.(map[string]any)["properties"].(map[string]any)
		if field, ok := properties["planarReflection"].(map[string]any); ok {
			declared = field["type"] == "boolean"
		}
	}
	if !declared {
		t.Fatal("the object definition does not declare planarReflection as a boolean")
	}
	// A model definition extends the object definition, so models carry the field too.
	extends := false
	for _, part := range defs["model"].(map[string]any)["allOf"].([]any) {
		if part.(map[string]any)["$ref"] == "#/$defs/object" {
			extends = true
		}
	}
	if !extends {
		t.Fatal("the model definition no longer extends the object definition")
	}
}
