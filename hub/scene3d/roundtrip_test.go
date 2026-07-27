package scene3d

import (
	"encoding/json"
	"fmt"
	"testing"

	"m31labs.dev/gosx/crdt"
	"m31labs.dev/gosx/scene"
)

// allCommandKinds lists every kind scene/diff.go declares. The list is written
// out by hand on purpose. A new kind added to scene.CommandKind without a case
// here makes TestEveryCommandKindIsCovered fail, so a kind cannot slip into the
// protocol without a round-trip test.
var allCommandKinds = []scene.CommandKind{
	scene.CommandCreateObject,
	scene.CommandRemoveObject,
	scene.CommandSetTransform,
	scene.CommandSetMaterial,
	scene.CommandSetLight,
	scene.CommandSetCamera,
	scene.CommandSetParticles,
	scene.CommandSetPostEffects,
	scene.CommandSetInstancedMeshes,
	scene.CommandSetMaterials,
	scene.CommandSetModels,
	scene.CommandSetInstancedGLBMeshes,
	scene.CommandSetAnimations,
	scene.CommandSetEnvironment,
	scene.CommandSetPostUniforms,
}

// TestEveryCommandKindIsCovered proves the enumeration above is complete and
// that every kind has a home in the document schema. A kind with no slot and no
// field would be refused by Apply, so the schema and the protocol stay in step.
func TestEveryCommandKindIsCovered(t *testing.T) {
	// scene.CommandSetPostUniforms is the highest declared kind. Walking from
	// zero to it catches a kind inserted in the middle of the iota block.
	for kind := scene.CommandCreateObject; kind <= scene.CommandSetPostUniforms; kind++ {
		found := false
		for _, listed := range allCommandKinds {
			if listed == kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("command kind %d is declared in scene/diff.go but not covered here", kind)
		}
	}
	for _, kind := range allCommandKinds {
		switch kind {
		case scene.CommandCreateObject, scene.CommandRemoveObject:
			continue
		}
		_, object := fieldForKind[kind]
		_, collection := slotForKind[kind]
		if !object && !collection {
			t.Fatalf("command kind %d has no document slot and no object field", kind)
		}
	}
}

// roundTripCase is one command kind, the command that exercises it, and the
// commands a receiving peer must see.
type roundTripCase struct {
	name string
	// seed runs before the command under test. A patch or a removal needs the
	// object to exist first.
	seed []scene.Command
	// command is the mutation under test.
	command scene.Command
	// wantWatch is the stream a peer must recover from the document change.
	wantWatch []scene.Command
	// wantBootstrap is the stream Commands() must produce after the mutation.
	// A nil value means "the same as the command under test".
	wantBootstrap []scene.Command
}

func roundTripCases() []roundTripCase {
	cube := scene.ObjectIR{ID: "cube", Kind: "box", Width: 2, Color: "#ff0000"}
	label := scene.LabelIR{ID: "tag", Text: "hello"}
	sprite := scene.SpriteIR{ID: "spark", Src: "/spark.png"}
	overlay := scene.HTMLIR{ID: "panel", HTML: "<b>hi</b>"}
	light := scene.LightIR{ID: "sun", Kind: "directional", Intensity: 1.5}
	transform := scene.Command{
		Kind:     scene.CommandSetTransform,
		ObjectID: "cube",
		Data:     map[string]any{"x": 4.0, "y": 1.0, "z": 0.0},
	}
	material := scene.Command{
		Kind:     scene.CommandSetMaterial,
		ObjectID: "cube",
		Data:     map[string]any{"color": "#00ff00", "roughness": 0.25},
	}
	lightPatch := scene.Command{
		Kind:     scene.CommandSetLight,
		ObjectID: "sun",
		Data:     map[string]any{"intensity": 2.0},
	}

	createCube := scene.CreateObjectCommand(cube)
	createLight := scene.CreateLightCommand(light)

	return []roundTripCase{
		{
			name:          "CreateObject",
			command:       createCube,
			wantWatch:     []scene.Command{scene.RemoveObjectCommand("cube"), createCube},
			wantBootstrap: []scene.Command{createCube},
		},
		{
			name:          "CreateLabel",
			command:       scene.CreateLabelCommand(label),
			wantWatch:     []scene.Command{scene.RemoveObjectCommand("tag"), scene.CreateLabelCommand(label)},
			wantBootstrap: []scene.Command{scene.CreateLabelCommand(label)},
		},
		{
			name:          "CreateSprite",
			command:       scene.CreateSpriteCommand(sprite),
			wantWatch:     []scene.Command{scene.RemoveObjectCommand("spark"), scene.CreateSpriteCommand(sprite)},
			wantBootstrap: []scene.Command{scene.CreateSpriteCommand(sprite)},
		},
		{
			name:          "CreateHTML",
			command:       scene.CreateHTMLCommand(overlay),
			wantWatch:     []scene.Command{scene.RemoveObjectCommand("panel"), scene.CreateHTMLCommand(overlay)},
			wantBootstrap: []scene.Command{scene.CreateHTMLCommand(overlay)},
		},
		{
			name:          "CreateLight",
			command:       createLight,
			wantWatch:     []scene.Command{scene.RemoveObjectCommand("sun"), createLight},
			wantBootstrap: []scene.Command{createLight},
		},
		{
			name:          "RemoveObject",
			seed:          []scene.Command{createCube},
			command:       scene.RemoveObjectCommand("cube"),
			wantWatch:     []scene.Command{scene.RemoveObjectCommand("cube")},
			wantBootstrap: []scene.Command{},
		},
		{
			name:          "SetTransform",
			seed:          []scene.Command{createCube},
			command:       transform,
			wantWatch:     []scene.Command{transform},
			wantBootstrap: []scene.Command{createCube, transform},
		},
		{
			name:          "SetMaterial",
			seed:          []scene.Command{createCube},
			command:       material,
			wantWatch:     []scene.Command{material},
			wantBootstrap: []scene.Command{createCube, material},
		},
		{
			name:          "SetLight",
			seed:          []scene.Command{createLight},
			command:       lightPatch,
			wantWatch:     []scene.Command{lightPatch},
			wantBootstrap: []scene.Command{createLight, lightPatch},
		},
		{
			name:    "SetCamera",
			command: scene.SetCameraCommand(scene.IRCamera{Kind: "perspective", Z: 7, FOV: 55}),
		},
		{
			name: "SetParticles",
			command: scene.SetParticlesCommand(
				[]scene.PointsIR{{ID: "dust", Count: 128, Size: 0.2}},
				[]scene.ComputeParticlesIR{{ID: "smoke", Count: 64}},
				[]scene.WaterSystemIR{{ID: "pond", PoolWidth: 10}},
			),
		},
		{
			name:    "SetPostEffects",
			command: scene.SetPostEffectsCommand([]scene.PostEffectIR{scene.TonemapIR{Mode: "aces", Exposure: 1.2}}, 1<<20),
		},
		{
			name:    "SetInstancedMeshes",
			command: scene.SetInstancedMeshesCommand([]scene.InstancedMeshIR{{ID: "grass", Kind: "box", Count: 512}}),
		},
		{
			name:    "SetMaterials",
			command: scene.SetMaterialsCommand([]scene.IRMaterial{{Name: "brass", Kind: "standard", Metalness: 0.9}}),
		},
		{
			name:    "SetModels",
			command: scene.SetModelsCommand([]scene.ModelIR{{ObjectIR: scene.ObjectIR{ID: "robot"}, Src: "/robot.glb"}}),
		},
		{
			name:    "SetInstancedGLBMeshes",
			command: scene.SetInstancedGLBMeshesCommand([]scene.InstancedGLBMeshIR{{ID: "tree", Src: "/tree.glb", Instances: []scene.MeshInstanceIR{{X: 1}}}}),
		},
		{
			name:    "SetAnimations",
			command: scene.SetAnimationsCommand([]scene.AnimationClipIR{{Name: "idle", Duration: 2}}),
		},
		{
			name:    "SetEnvironment",
			command: scene.SetEnvironmentCommand(scene.EnvironmentIR{AmbientColor: "#101010", Exposure: 1.1}),
		},
		{
			name:    "SetPostUniforms",
			command: scene.SetPostUniformsCommand([]scene.PostUniformPatch{{Name: "glitch", Uniforms: map[string]any{"amount": 0.4}}}),
		},
		{
			name:    "SetEnvironmentNilData",
			command: scene.Command{Kind: scene.CommandSetEnvironment},
		},
	}
}

// TestCommandRoundTripIsLossless applies one command of every kind and proves
// both recovery channels return it unchanged, byte for byte.
//
// Byte equality is the right bar. The browser runtime consumes the JSON of a
// command, and Command.Data is an `any` field, so a decode into a Go value
// could never reproduce the original typed value. Preserving the exact bytes
// preserves everything the runtime can observe.
func TestCommandRoundTripIsLossless(t *testing.T) {
	for _, testCase := range roundTripCases() {
		t.Run(testCase.name, func(t *testing.T) {
			doc, bound := newBoundDoc(t)
			_ = doc

			var seen []scene.Command
			bound.Watch(func(commands []scene.Command) {
				seen = append(seen, commands...)
			})

			if len(testCase.seed) > 0 {
				if _, err := bound.Apply(testCase.seed, "seed"); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			seen = nil

			if _, err := bound.Apply([]scene.Command{testCase.command}, "mutate"); err != nil {
				t.Fatalf("apply: %v", err)
			}

			wantWatch := testCase.wantWatch
			if wantWatch == nil {
				wantWatch = []scene.Command{testCase.command}
			}
			assertCommandsEqual(t, "watch", wantWatch, seen)

			wantBootstrap := testCase.wantBootstrap
			if wantBootstrap == nil {
				wantBootstrap = []scene.Command{testCase.command}
			}
			got, err := bound.Commands()
			if err != nil {
				t.Fatalf("commands: %v", err)
			}
			assertCommandsEqual(t, "bootstrap", wantBootstrap, got)
		})
	}
}

// TestApplyRefusesCommandWithoutObjectID proves a per-object command that names
// no object is refused instead of silently dropped, and that the refusal leaves
// the document untouched.
func TestApplyRefusesCommandWithoutObjectID(t *testing.T) {
	kinds := []scene.CommandKind{
		scene.CommandCreateObject,
		scene.CommandRemoveObject,
		scene.CommandSetTransform,
		scene.CommandSetMaterial,
		scene.CommandSetLight,
	}
	for _, kind := range kinds {
		t.Run(fmt.Sprintf("kind%d", kind), func(t *testing.T) {
			_, bound := newBoundDoc(t)
			if _, err := bound.Apply([]scene.Command{{Kind: kind}}, "bad"); err == nil {
				t.Fatal("want an error for a per-object command with no objectId")
			}
			commands, err := bound.Commands()
			if err != nil {
				t.Fatal(err)
			}
			if len(commands) != 0 {
				t.Fatalf("refused command still wrote %d commands into the document", len(commands))
			}
		})
	}
}

// TestApplyRefusesUnknownCommandKind proves an unknown kind cannot enter the
// document. A kind the schema does not cover would otherwise vanish.
func TestApplyRefusesUnknownCommandKind(t *testing.T) {
	_, bound := newBoundDoc(t)
	if _, err := bound.Apply([]scene.Command{{Kind: scene.CommandKind(9999)}}, "bad"); err == nil {
		t.Fatal("want an error for an unknown command kind")
	}
}

// TestApplyValidatesBeforeWriting proves a stream with a bad command at the end
// writes nothing at all, so a caller never sees a half-applied mutation.
func TestApplyValidatesBeforeWriting(t *testing.T) {
	_, bound := newBoundDoc(t)
	good := scene.CreateObjectCommand(scene.ObjectIR{ID: "cube", Kind: "box"})
	if _, err := bound.Apply([]scene.Command{good, {Kind: scene.CommandSetTransform}}, "half"); err == nil {
		t.Fatal("want an error for the malformed second command")
	}
	commands, err := bound.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("partially applied stream wrote %d commands", len(commands))
	}
}

// TestNamespaceIsolation proves two scenes in one document do not see each
// other's objects. Without the namespace check in parseKey, a shared document
// would leak objects between scenes.
func TestNamespaceIsolation(t *testing.T) {
	doc := crdt.NewDoc()
	left, err := Bind(doc, "left")
	if err != nil {
		t.Fatal(err)
	}
	right, err := Bind(doc, "right")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := left.Apply([]scene.Command{scene.CreateObjectCommand(scene.ObjectIR{ID: "cube", Kind: "box"})}, "left"); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Apply([]scene.Command{scene.CreateObjectCommand(scene.ObjectIR{ID: "sphere", Kind: "sphere"})}, "right"); err != nil {
		t.Fatal(err)
	}
	leftIDs, err := left.PresentObjectIDs()
	if err != nil {
		t.Fatal(err)
	}
	rightIDs, err := right.PresentObjectIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(leftIDs) != 1 || leftIDs[0] != "cube" {
		t.Fatalf("left namespace = %v, want [cube]", leftIDs)
	}
	if len(rightIDs) != 1 || rightIDs[0] != "sphere" {
		t.Fatalf("right namespace = %v, want [sphere]", rightIDs)
	}
}

// TestBindRejectsBadNamespace proves a namespace with a slash is refused. A
// slash separates key parts, so such a namespace could collide with an object
// ID from a neighboring namespace.
func TestBindRejectsBadNamespace(t *testing.T) {
	for _, namespace := range []string{"", "a/b"} {
		if _, err := Bind(crdt.NewDoc(), namespace); err == nil {
			t.Fatalf("Bind(%q) = nil error, want a refusal", namespace)
		}
	}
	if _, err := Bind(nil, "scene"); err == nil {
		t.Fatal("Bind(nil, ...) = nil error, want a refusal")
	}
}

// TestObjectIDWithSlashRoundTrips proves an object ID that contains a slash
// still parses back. The key splits on the LAST slash, so the field code is
// recovered and the ID keeps its slashes.
func TestObjectIDWithSlashRoundTrips(t *testing.T) {
	_, bound := newBoundDoc(t)
	id := "group/inner/mesh"
	create := scene.CreateObjectCommand(scene.ObjectIR{ID: id, Kind: "box"})
	if _, err := bound.Apply([]scene.Command{create}, "nested"); err != nil {
		t.Fatal(err)
	}
	got, err := bound.Commands()
	if err != nil {
		t.Fatal(err)
	}
	assertCommandsEqual(t, "nested id", []scene.Command{create}, got)
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func newBoundDoc(t *testing.T) (*crdt.Doc, *Doc) {
	t.Helper()
	doc := crdt.NewDoc()
	bound, err := Bind(doc, "scene")
	if err != nil {
		t.Fatal(err)
	}
	return doc, bound
}

func assertCommandsEqual(t *testing.T, label string, want, got []scene.Command) {
	t.Helper()
	wantJSON, err := scene.MarshalCommands(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := scene.MarshalCommands(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("%s stream mismatch\n want %s\n  got %s", label, wantJSON, gotJSON)
	}
}

// canonicalView renders the scene a document describes, with its removal
// history, for comparison between two peers that share that history.
func canonicalView(t *testing.T, d *Doc) []byte {
	t.Helper()
	view, err := d.View()
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	raw, err := view.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return raw
}

// canonicalVisible renders only the scene a document shows, without its removal
// history. Use it to compare two documents built along different paths.
func canonicalVisible(t *testing.T, d *Doc) []byte {
	t.Helper()
	view, err := d.View()
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	raw, err := view.Visible().Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return raw
}

// objectByID finds one materialized object. It fails the test when the object
// is missing, so a caller never compares against a zero value by accident.
func objectByID(t *testing.T, view View, id string) scene.ObjectIR {
	t.Helper()
	for _, record := range view.IR.Objects {
		if record.ID == id {
			return record
		}
	}
	raw, _ := json.Marshal(view.IR.Objects)
	t.Fatalf("object %q missing from view: %s", id, raw)
	return scene.ObjectIR{}
}
