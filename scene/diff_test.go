package scene

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffCommandsCreateReplaceAndRemoveSceneRecords(t *testing.T) {
	previous := SceneIR{
		Objects: []ObjectIR{
			{ID: "cube", Kind: "box", Width: 1, X: 3, Color: "#f00"},
			{ID: "gone", Kind: "sphere", Radius: 1},
		},
		Labels: []LabelIR{
			{ID: "title", Text: "Old", X: 1},
		},
		Lights: []LightIR{
			{ID: "sun", Kind: "directional", Intensity: 0.5},
		},
	}
	next := SceneIR{
		Objects: []ObjectIR{
			{ID: "cube", Kind: "box", Width: 1, X: 0, Color: "#f00"},
			{ID: "new", Kind: "sphere", Radius: 2},
		},
		Sprites: []SpriteIR{
			{ID: "marker", Src: "/marker.png", X: 2},
		},
		Lights: []LightIR{
			{ID: "sun", Kind: "directional", Intensity: 1.25},
		},
	}

	commands := DiffCommands(previous, next)
	wantKinds := []CommandKind{
		CommandRemoveObject, // gone
		CommandRemoveObject, // cube changed
		CommandCreateObject, // cube replacement
		CommandCreateObject, // new
		CommandRemoveObject, // title
		CommandCreateObject, // marker
		CommandRemoveObject, // sun changed
		CommandCreateObject, // sun replacement
	}
	if len(commands) != len(wantKinds) {
		t.Fatalf("commands = %d, want %d: %#v", len(commands), len(wantKinds), commands)
	}
	for i, want := range wantKinds {
		if commands[i].Kind != want {
			t.Fatalf("command %d kind = %d, want %d: %#v", i, commands[i].Kind, want, commands[i])
		}
	}
	if commands[0].ObjectID != "gone" || commands[1].ObjectID != "cube" || commands[2].ObjectID != "cube" {
		t.Fatalf("unexpected object command order: %#v", commands[:3])
	}
	payload := commandPayloadMap(t, commands[2])
	props := payloadMap(t, payload, "props")
	if _, ok := props["x"]; ok {
		t.Fatalf("replacement payload should rely on remove+create for zero-value reset, props=%#v", props)
	}
	if got := payload["geometry"]; got != "box" {
		t.Fatalf("object geometry = %#v, want box", got)
	}
	lightPayload := commandPayloadMap(t, commands[len(commands)-1])
	if got := lightPayload["kind"]; got != "light" {
		t.Fatalf("light payload kind = %#v", got)
	}
}

func TestDiffPropsCommandsLowerTypedScenes(t *testing.T) {
	previous := Props{
		Graph: NewGraph(Mesh{
			ID:       "box",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Position: Vec3(1, 0, 0),
		}),
	}
	next := Props{
		Graph: NewGraph(Mesh{
			ID:       "box",
			Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Position: Vec3(2, 0, 0),
		}),
	}

	commands := DiffPropsCommands(previous, next)
	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want remove+create", commands)
	}
	if commands[0].Kind != CommandRemoveObject || commands[0].ObjectID != "box" {
		t.Fatalf("remove command = %#v", commands[0])
	}
	data, err := MarshalCommands(commands)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, snippet := range []string{`"kind":1`, `"objectId":"box"`, `"geometry":"box"`} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected %s in %s", snippet, text)
		}
	}
}

func TestMarshalCommandsNilIsEmptyArray(t *testing.T) {
	data, err := MarshalCommands(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[]" {
		t.Fatalf("nil commands marshal = %s", data)
	}
}

func commandPayloadMap(t *testing.T, command Command) map[string]any {
	t.Helper()
	data, err := json.Marshal(command.Data)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func payloadMap(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	data, err := json.Marshal(payload[key])
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
