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
