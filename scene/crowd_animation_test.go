package scene

import (
	"encoding/json"
	"os"
	"testing"
)

func TestMeshInstanceAnimationRoundTrip(t *testing.T) {
	p := Props{Graph: NewGraph(InstancedGLBMesh{ID: "actors", Src: "/actor.glb", Instances: []MeshInstance{{ID: "a", Scale: Vec3(1, 1, 1), Animation: "Walk", AnimationTime: 1.25, AnimationLoop: true}, {ID: "b", Scale: Vec3(1, 1, 1), Animation: "Attack", AnimationTime: .8}}})}
	ir := p.SceneIR()
	b, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SceneIR
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	a := decoded.InstancedGLBMeshes[0].Instances
	if a[0].Animation != "Walk" || a[0].AnimationTime != 1.25 || !a[0].AnimationLoop || a[1].Animation != "Attack" || a[1].AnimationTime != .8 || a[1].AnimationLoop {
		t.Fatalf("pose lost: %+v", a)
	}
}

func TestMeshInstanceAnimationLegacyProps(t *testing.T) {
	p := Props{Graph: NewGraph(InstancedGLBMesh{ID: "crowd", Src: "/rig.glb", Instances: []MeshInstance{{ID: "a", Animation: "Walk", AnimationTime: 1.25, AnimationLoop: true}}})}
	scene := p.LegacyProps()["scene"].(map[string]any)
	meshes := scene["instancedGLBMeshes"].([]map[string]any)
	instance := meshes[0]["instances"].([]map[string]any)[0]
	if instance["animation"] != "Walk" || instance["animationTime"] != 1.25 || instance["animationLoop"] != true {
		t.Fatalf("legacy pose lost: %#v", instance)
	}
}
func TestMeshInstanceAnimationPublicSchema(t *testing.T) {
	b, err := os.ReadFile("schema/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	defs := doc["$defs"].(map[string]any)
	entry := defs["meshInstance"].(map[string]any)["allOf"].([]any)[1].(map[string]any)["properties"].(map[string]any)
	for key, kind := range map[string]string{"animation": "string", "animationTime": "number", "animationLoop": "boolean"} {
		field, ok := entry[key].(map[string]any)
		if !ok || field["type"] != kind {
			t.Fatalf("schema field %s missing/type", key)
		}
	}
	if entry["animationTime"].(map[string]any)["minimum"] != float64(0) {
		t.Fatal("negative times must fail schema")
	}
}
