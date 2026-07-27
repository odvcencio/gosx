package scene

import (
	"encoding/json"
	"testing"
)

// movedObjectScenes returns two scenes whose single object moved and changed
// nothing else.
func movedObjectScenes() (SceneIR, SceneIR) {
	record := ObjectIR{
		ID: "crate", Kind: "box", Width: 2, Height: 2, Depth: 2,
		MaterialKind: "standard", Color: "#8899aa",
		Points: []Vector3{{X: 1}, {Y: 2}},
		X:      1, Y: 2, Z: 3, RotationY: 0.5, ScaleX: 2, ScaleY: 2, ScaleZ: 2,
	}
	moved := record
	moved.X, moved.Y, moved.Z = 4, 5, 6
	moved.RotationY = 1.25
	return SceneIR{Objects: []ObjectIR{record}}, SceneIR{Objects: []ObjectIR{moved}}
}

// TestDiffCommandsKeepsRemoveCreatePairForMovedObject locks the default down.
// A consumer folds the pair into a move, so switching the default to a patch
// would change the shape of its input without warning.
func TestDiffCommandsKeepsRemoveCreatePairForMovedObject(t *testing.T) {
	previous, next := movedObjectScenes()
	commands := DiffCommands(previous, next)
	wantKinds := []CommandKind{CommandRemoveObject, CommandCreateObject}
	if len(commands) != len(wantKinds) {
		t.Fatalf("commands = %#v, want a remove plus a create", commands)
	}
	for index, kind := range wantKinds {
		if commands[index].Kind != kind {
			t.Errorf("command %d kind = %d, want %d", index, commands[index].Kind, kind)
		}
		if commands[index].ObjectID != "crate" {
			t.Errorf("command %d objectID = %q, want crate", index, commands[index].ObjectID)
		}
	}
}

// TestDiffScenePatchTransformsReplacesPairForMovedObject proves the opt-in patch
// path ships one command with the whole transform.
func TestDiffScenePatchTransformsReplacesPairForMovedObject(t *testing.T) {
	previous, next := movedObjectScenes()
	diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	if len(diff.Commands) != 1 {
		t.Fatalf("commands = %#v, want one transform patch", diff.Commands)
	}
	command := diff.Commands[0]
	if command.Kind != CommandSetTransform {
		t.Fatalf("kind = %d, want CommandSetTransform (%d)", command.Kind, CommandSetTransform)
	}
	if command.ObjectID != "crate" {
		t.Errorf("objectID = %q, want crate", command.ObjectID)
	}
	patch, ok := command.Data.(TransformPatch)
	if !ok {
		t.Fatalf("data = %#v, want a TransformPatch", command.Data)
	}
	want := TransformPatch{X: 4, Y: 5, Z: 6, RotationY: 1.25, ScaleX: 2, ScaleY: 2, ScaleZ: 2}
	if patch != want {
		t.Errorf("patch = %#v, want %#v", patch, want)
	}
	if len(diff.RemountFields) != 0 {
		t.Errorf("RemountFields = %v, want none", diff.RemountFields)
	}
}

// TestDiffScenePatchTransformsDeclinesForAnyOtherChange is the safety case. The
// patch carries nine floats, so any other changed field would be dropped. Each
// case changes the transform AND one other field, and the diff has to fall back
// to the pair that carries the whole record.
func TestDiffScenePatchTransformsDeclinesForAnyOtherChange(t *testing.T) {
	visible := false
	cases := map[string]func(record *ObjectIR){
		"color":                func(record *ObjectIR) { record.Color = "#ff0000" },
		"geometry":             func(record *ObjectIR) { record.Width = 9 },
		"nested slice":         func(record *ObjectIR) { record.Points = []Vector3{{X: 1}, {Y: 99}} },
		"map field":            func(record *ObjectIR) { record.CustomUniforms = map[string]any{"gain": 2.0} },
		"pointer field":        func(record *ObjectIR) { record.Visible = &visible },
		"spin, not transform":  func(record *ObjectIR) { record.SpinY = 0.5 },
		"drift, not transform": func(record *ObjectIR) { record.DriftSpeed = 0.5 },
		"kind":                 func(record *ObjectIR) { record.Kind = "sphere" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			previous, next := movedObjectScenes()
			mutate(&next.Objects[0])
			diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
			if len(diff.Commands) != 2 {
				t.Fatalf("commands = %#v, want a remove plus a create", diff.Commands)
			}
			if diff.Commands[0].Kind != CommandRemoveObject || diff.Commands[1].Kind != CommandCreateObject {
				t.Errorf("kinds = %d and %d, want remove then create", diff.Commands[0].Kind, diff.Commands[1].Kind)
			}
			for _, command := range diff.Commands {
				if command.Kind == CommandSetTransform {
					t.Error("a patch shipped for a record that changed outside the transform: the other change is lost")
				}
			}
		})
	}
}

// TestTransformPatchResolvesUnsetScale pins the one place the IR convention and
// the patch convention differ. A zero scale component in ObjectIR means "unset",
// and the runtime reads it as 1. A patch merges over an object whose scale the
// runtime already resolved, so a raw zero would collapse the object to a point.
func TestTransformPatchResolvesUnsetScale(t *testing.T) {
	previous := SceneIR{Objects: []ObjectIR{{ID: "a", Kind: "box"}}}
	next := SceneIR{Objects: []ObjectIR{{ID: "a", Kind: "box", X: 1}}}
	diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	if len(diff.Commands) != 1 {
		t.Fatalf("commands = %#v, want one patch", diff.Commands)
	}
	patch, ok := diff.Commands[0].Data.(TransformPatch)
	if !ok {
		t.Fatalf("data = %#v, want a TransformPatch", diff.Commands[0].Data)
	}
	if patch.ScaleX != 1 || patch.ScaleY != 1 || patch.ScaleZ != 1 {
		t.Errorf("scale = %v, %v, %v; want 1, 1, 1 for an unset IR scale", patch.ScaleX, patch.ScaleY, patch.ScaleZ)
	}

	// An authored scale passes through untouched.
	next.Objects[0].ScaleX, next.Objects[0].ScaleY, next.Objects[0].ScaleZ = 0.5, 3, 0.25
	diff = DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	patch = diff.Commands[0].Data.(TransformPatch)
	if patch.ScaleX != 0.5 || patch.ScaleY != 3 || patch.ScaleZ != 0.25 {
		t.Errorf("scale = %v, %v, %v; want 0.5, 3, 0.25", patch.ScaleX, patch.ScaleY, patch.ScaleZ)
	}
}

// TestTransformPatchShipsEveryKeyIncludingZero proves a reset replicates. The
// runtime shallow-merges the patch over the live object, so an omitted key keeps
// the old value: a move back to the origin has to write the zero.
func TestTransformPatchShipsEveryKeyIncludingZero(t *testing.T) {
	previous := SceneIR{Objects: []ObjectIR{{ID: "a", Kind: "box", X: 5, Y: 5, Z: 5, RotationX: 1, ScaleX: 3, ScaleY: 3, ScaleZ: 3}}}
	next := SceneIR{Objects: []ObjectIR{{ID: "a", Kind: "box"}}}
	diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	if len(diff.Commands) != 1 {
		t.Fatalf("commands = %#v, want one patch", diff.Commands)
	}
	encoded, err := json.Marshal(diff.Commands[0].Data)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	wantKeys := map[string]float64{
		"x": 0, "y": 0, "z": 0,
		"rotationX": 0, "rotationY": 0, "rotationZ": 0,
		"scaleX": 1, "scaleY": 1, "scaleZ": 1,
	}
	if len(decoded) != len(wantKeys) {
		t.Errorf("patch keys = %v, want exactly %d keys", decoded, len(wantKeys))
	}
	for key, want := range wantKeys {
		value, ok := decoded[key]
		if !ok {
			t.Errorf("patch omits %q; the runtime would keep the old value and the reset would be lost", key)
			continue
		}
		if value != want {
			t.Errorf("patch %q = %v, want %v", key, value, want)
		}
	}
}

// TestTransformPatchShipsLessThanARecreate measures the reason the patch exists.
// Dragging an object through remove plus create ships the whole record, geometry
// included, every frame.
func TestTransformPatchShipsLessThanARecreate(t *testing.T) {
	previous, next := movedObjectScenes()
	// Give the record a buffer worth shipping, the way a real mesh has one.
	positions := make([]Vector3, 400)
	for index := range positions {
		positions[index] = Vector3{X: float64(index), Y: float64(index) * 0.5, Z: 1}
	}
	previous.Objects[0].Points = positions
	next.Objects[0].Points = positions

	pair, err := MarshalCommands(DiffCommands(previous, next))
	if err != nil {
		t.Fatalf("marshal pair: %v", err)
	}
	patch, err := MarshalCommands(DiffScene(previous, next, DiffOptions{PatchTransforms: true}).Commands)
	if err != nil {
		t.Fatalf("marshal patch: %v", err)
	}
	if len(patch) >= len(pair) {
		t.Errorf("patch is %d bytes and the remove plus create pair is %d; the patch has to be smaller", len(patch), len(pair))
	}
	t.Logf("patch %d bytes, remove plus create %d bytes", len(patch), len(pair))
}

// TestDiffScenePatchTransformsKeepsCreateAndRemove proves the option changes only
// the changed-record path. A new object still arrives whole, and a deleted object
// is still removed.
func TestDiffScenePatchTransformsKeepsCreateAndRemove(t *testing.T) {
	previous := SceneIR{Objects: []ObjectIR{
		{ID: "kept", Kind: "box"},
		{ID: "gone", Kind: "box"},
	}}
	next := SceneIR{Objects: []ObjectIR{
		{ID: "kept", Kind: "box", X: 2},
		{ID: "fresh", Kind: "sphere", Radius: 1},
	}}
	diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	if len(diff.Commands) != 3 {
		t.Fatalf("commands = %#v, want a remove, a patch, and a create", diff.Commands)
	}
	if diff.Commands[0].Kind != CommandRemoveObject || diff.Commands[0].ObjectID != "gone" {
		t.Errorf("command 0 = %#v, want the removal of gone", diff.Commands[0])
	}
	if diff.Commands[1].Kind != CommandSetTransform || diff.Commands[1].ObjectID != "kept" {
		t.Errorf("command 1 = %#v, want a transform patch for kept", diff.Commands[1])
	}
	if diff.Commands[2].Kind != CommandCreateObject || diff.Commands[2].ObjectID != "fresh" {
		t.Errorf("command 2 = %#v, want the creation of fresh", diff.Commands[2])
	}
}

// TestDiffScenePatchTransformsCoversObjectsOnly records the scope. A label, a
// sprite, an HTML overlay, and a light keep the pair.
func TestDiffScenePatchTransformsCoversObjectsOnly(t *testing.T) {
	previous := SceneIR{
		Labels:  []LabelIR{{ID: "l", Text: "a", X: 1}},
		Sprites: []SpriteIR{{ID: "s", Src: "/s.png", X: 1}},
		HTML:    []HTMLIR{{ID: "h", X: 1}},
		Lights:  []LightIR{{ID: "li", Kind: "point", X: 1}},
	}
	next := SceneIR{
		Labels:  []LabelIR{{ID: "l", Text: "a", X: 2}},
		Sprites: []SpriteIR{{ID: "s", Src: "/s.png", X: 2}},
		HTML:    []HTMLIR{{ID: "h", X: 2}},
		Lights:  []LightIR{{ID: "li", Kind: "point", X: 2}},
	}
	diff := DiffScene(previous, next, DiffOptions{PatchTransforms: true})
	if len(diff.Commands) != 8 {
		t.Fatalf("commands = %d, want a remove plus a create for each of the four records", len(diff.Commands))
	}
	for _, command := range diff.Commands {
		if command.Kind == CommandSetTransform {
			t.Errorf("record %q got a transform patch; the option covers objects only", command.ObjectID)
		}
	}
}

// TestEnvironmentCommandsCarryAShapeDiscriminator proves a receiver can tell the
// two environment payloads apart. Both diff paths send CommandSetEnvironment with
// the same key, and before the shape key existed a receiver holding raw JSON had
// to guess which Go type wrote it.
func TestEnvironmentCommandsCarryAShapeDiscriminator(t *testing.T) {
	fromScene := DiffCommands(
		SceneIR{Environment: EnvironmentIR{AmbientColor: "#111111"}},
		SceneIR{Environment: EnvironmentIR{AmbientColor: "#222222"}},
	)
	fromIR := DiffIRCommands(
		IR{Environment: IREnvironment{AmbientColor: "#111111"}},
		IR{Environment: IREnvironment{AmbientColor: "#222222"}},
	)
	if len(fromScene) != 1 || len(fromIR) != 1 {
		t.Fatalf("scene commands = %#v, IR commands = %#v; want one each", fromScene, fromIR)
	}
	if fromScene[0].Kind != CommandSetEnvironment || fromIR[0].Kind != CommandSetEnvironment {
		t.Fatalf("kinds = %d and %d, want CommandSetEnvironment for both", fromScene[0].Kind, fromIR[0].Kind)
	}
	if got := commandShape(t, fromScene[0]); got != EnvironmentShapeSceneIR {
		t.Errorf("DiffCommands environment shape = %q, want %q", got, EnvironmentShapeSceneIR)
	}
	if got := commandShape(t, fromIR[0]); got != EnvironmentShapeCanonicalIR {
		t.Errorf("DiffIRCommands environment shape = %q, want %q", got, EnvironmentShapeCanonicalIR)
	}
	if commandShape(t, fromScene[0]) == commandShape(t, fromIR[0]) {
		t.Error("both payloads carry the same shape, so a receiver still cannot tell them apart")
	}

	// The exported constructors stamp the same way, and the untyped one stamps
	// nothing it cannot name.
	if got := commandShape(t, SetEnvironmentCommand(EnvironmentIR{})); got != EnvironmentShapeSceneIR {
		t.Errorf("SetEnvironmentCommand(EnvironmentIR) shape = %q, want %q", got, EnvironmentShapeSceneIR)
	}
	if got := commandShape(t, SetEnvironmentCommand(&EnvironmentIR{})); got != EnvironmentShapeSceneIR {
		t.Errorf("SetEnvironmentCommand(*EnvironmentIR) shape = %q, want %q", got, EnvironmentShapeSceneIR)
	}
	if got := commandShape(t, SetEnvironmentCommand(IREnvironment{})); got != EnvironmentShapeCanonicalIR {
		t.Errorf("SetEnvironmentCommand(IREnvironment) shape = %q, want %q", got, EnvironmentShapeCanonicalIR)
	}
	if got := commandShape(t, SetEnvironmentCommand(&IREnvironment{})); got != EnvironmentShapeCanonicalIR {
		t.Errorf("SetEnvironmentCommand(*IREnvironment) shape = %q, want %q", got, EnvironmentShapeCanonicalIR)
	}
	if got := commandShape(t, SetEnvironmentCommand(map[string]any{"ambientColor": "#333333"})); got != "" {
		t.Errorf("SetEnvironmentCommand(map) shape = %q, want an unstamped payload", got)
	}
}

// TestEnvironmentCommandKeepsTheEnvironmentKey guards the runtime contract. The
// browser reads data.environment, so the discriminator may only be added beside
// it, never instead of it.
func TestEnvironmentCommandKeepsTheEnvironmentKey(t *testing.T) {
	command := SetSceneEnvironmentCommand(EnvironmentIR{AmbientColor: "#abcdef", Exposure: 1.5})
	payload := commandPayloadMap(t, command)
	environment := payloadMap(t, payload, "environment")
	if environment["ambientColor"] != "#abcdef" {
		t.Errorf("environment payload = %#v, want the ambient color", environment)
	}
	if environment["exposure"] != 1.5 {
		t.Errorf("environment exposure = %#v, want 1.5", environment["exposure"])
	}
}

func commandShape(t *testing.T, command Command) string {
	t.Helper()
	payload := commandPayloadMap(t, command)
	shape, ok := payload["shape"]
	if !ok {
		return ""
	}
	text, ok := shape.(string)
	if !ok {
		t.Fatalf("shape = %#v, want a string", shape)
	}
	return text
}
