package controller_test

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/scene"
)

func TestControllerInputManifest(t *testing.T) {
	type intent struct {
		Kind string `json:"kind"`
		Tile string `json:"tile"`
	}
	projection := &controller.Projection{Value: intent{Kind: "play"}, Fields: map[string]string{"tile": "event.detail.input.id"}}
	config := controller.Config{
		Outputs: []controller.Output{{Name: "intent", Signal: "$intent", Event: "app:intent"}},
		Events:  []controller.Event{{Type: "gosx:scene3d:input", Output: "intent", Project: projection}},
		Keys:    []controller.KeyBinding{{Code: "Enter", Output: "intent", Project: projection}},
		Drags:   []controller.DragBinding{{Source: "[data-tile]", Output: "intent", Targets: []controller.DropTarget{{Target: "#board", Scene: true, RequestOutput: "ray", ResultSignal: "$hit", HitIDs: []string{"end"}}}}},
		Focus:   []controller.FocusOwner{{Target: "#modal", OpenSignal: "$modalOpen", InitialFocus: "button", ReturnFocus: "#open"}},
	}
	manifest := hydrate.NewManifest()
	manifest.AddController(config)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded hydrate.Manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.Controllers[0].Config
	if !got.NeedsInputRuntime() || got.Events[0].Project.Fields["tile"] != "event.detail.input.id" || got.Drags[0].Targets[0].ResultSignal != "$hit" || got.Focus[0].OpenSignal != "$modalOpen" || got.Outputs[0].Event != "app:intent" {
		t.Fatalf("input contracts lost in manifest: %+v", got)
	}
}

func TestPickResultAcceptsNativeRaycasts(t *testing.T) {
	graph := scene.NewGraph(scene.Mesh{ID: "end", Geometry: scene.BoxGeometry{Width: 2, Height: 2, Depth: 2}})
	request := controller.PickRequest{RequestID: "drag:1", Ray: scene.Ray{Origin: scene.Vec3(0, 0, 4), Direction: scene.Vec3(0, 0, -1)}}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded controller.PickRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, raycast := range []func(scene.Ray) (scene.RayHit, bool){
		func(ray scene.Ray) (scene.RayHit, bool) { return scene.RaycastGraph(graph, ray) },
		func(ray scene.Ray) (scene.RayHit, bool) { return scene.NewSceneAccelerator(graph).Raycast(ray) },
	} {
		hit, ok := raycast(decoded.Ray)
		if !ok {
			t.Fatal("expected native ray hit")
		}
		data, err := json.Marshal(controller.PickResult{RequestID: decoded.RequestID, Hit: &hit})
		if err != nil {
			t.Fatal(err)
		}
		var result controller.PickResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if result.RequestID != request.RequestID || result.Hit == nil || result.Hit.ID != "end" || result.Hit.Distance != 3 {
			t.Fatalf("native result = %+v", result)
		}
	}
}

func TestNeedsInputRuntime(t *testing.T) {
	if (controller.Config{Events: []controller.Event{{Type: "click", Output: "$click"}}}).NeedsInputRuntime() {
		t.Fatal("ordinary events must not fetch the input chunk")
	}
	for _, config := range []controller.Config{
		{Events: []controller.Event{{Project: &controller.Projection{}}}},
		{Keys: []controller.KeyBinding{{Project: &controller.Projection{}}}},
		{Drags: []controller.DragBinding{{}}},
		{Focus: []controller.FocusOwner{{}}},
		{Storage: &controller.Storage{}},
	} {
		if !config.NeedsInputRuntime() {
			t.Fatal("input contract did not select its runtime")
		}
	}
}
