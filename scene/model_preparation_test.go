package scene

import (
	"encoding/json"
	"testing"
)

func TestModelPreparationSnapshotValidation(t *testing.T) {
	for _, requests := range [][]ModelPreparation{nil, {{ID: "", Src: "/hero.glb"}}, {{ID: "hero", Src: " "}}, {{ID: "hero", Src: "/a"}, {ID: "hero", Src: "/b"}}, make([]ModelPreparation, 33)} {
		if _, err := modelPreparationSnapshot(requests); err == nil {
			t.Fatalf("accepted invalid selection: %#v", requests)
		}
	}
	requests := []ModelPreparation{{ID: "hero", Src: "/hero.glb"}, {ID: "enemy", Src: "/enemy.glb"}}
	data, err := modelPreparationSnapshot(requests)
	if err != nil {
		t.Fatal(err)
	}
	requests[0].Src = "/changed.glb"
	var got []ModelPreparation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "hero" || got[0].Src != "/hero.glb" {
		t.Fatalf("snapshot changed: %#v", got)
	}
}

func TestPropsModelPreparationSerializesOnlyAuthoredCapability(t *testing.T) {
	for _, value := range []*bool{nil, Bool(false), Bool(true)} {
		props := Props{ModelPreparation: value}
		data, err := props.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		got, present := payload["modelPreparation"]
		if present != (value != nil) || value != nil && got != *value {
			t.Fatalf("capability: %#v", payload)
		}
	}
}

func TestModelPreparationRigidGeometryOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		data, err := modelPreparationSnapshot([]ModelPreparation{{ID: "prop", Src: "/prop.glb", RigidGeometry: enabled}})
		if err != nil {
			t.Fatal(err)
		}
		var got []map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		value, present := got[0]["rigidGeometry"]
		if present != enabled || enabled && value != true {
			t.Fatalf("rigid preparation opt-in: %s", data)
		}
	}
}
