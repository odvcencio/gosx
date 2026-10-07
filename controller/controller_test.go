package controller_test

import (
	"bytes"
	"encoding/json"
	"reflect"
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
	request := controller.PickRequest{RequestID: "drag:1", Ray: controller.Ray{Origin: controller.Vector3{Z: 4}, Direction: controller.Vector3{Z: -1}}}
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
		ray := scene.Ray{Origin: scene.Vector3(decoded.Ray.Origin), Direction: scene.Vector3(decoded.Ray.Direction)}
		hit, ok := raycast(ray)
		if !ok {
			t.Fatal("expected native ray hit")
		}
		pickHit := controller.RayHit{
			ID: hit.ID, Kind: hit.Kind, Distance: hit.Distance,
			Point: controller.Vector3(hit.Point), Normal: controller.Vector3(hit.Normal),
			Pickable: hit.Pickable, InstanceIndex: hit.InstanceIndex, Method: hit.Method,
		}
		data, err := json.Marshal(controller.PickResult{RequestID: decoded.RequestID, Hit: &pickHit})
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

func TestPickMessagesMatchSceneJSON(t *testing.T) {
	instance := 0
	for _, hit := range []scene.RayHit{
		{},
		{ID: "end", Kind: "mesh", Distance: 3, Point: scene.Vec3(1, 2, 3),
			Normal: scene.Vec3(0, 0, 1), Pickable: true, InstanceIndex: &instance, Method: "native"},
	} {
		data, err := json.Marshal(hit)
		if err != nil {
			t.Fatal(err)
		}
		var local controller.RayHit
		if err := json.Unmarshal(data, &local); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(local)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("hit JSON changed: got %s, want %s", got, data)
		}
	}
	for _, ray := range []scene.Ray{{}, {Origin: scene.Vec3(1, 2, 3), Direction: scene.Vec3(0, 0, -1)}} {
		data, err := json.Marshal(ray)
		if err != nil {
			t.Fatal(err)
		}
		var local controller.Ray
		if err := json.Unmarshal(data, &local); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(local)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("ray JSON changed: got %s, want %s", got, data)
		}
	}
	data, err := json.Marshal(controller.PickResult{RequestID: "miss"})
	if err != nil {
		t.Fatal(err)
	}
	var miss controller.PickResult
	if err := json.Unmarshal(data, &miss); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(miss, controller.PickResult{RequestID: "miss"}) {
		t.Fatalf("miss changed: %+v", miss)
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
