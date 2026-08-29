package main

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

var wantFaces = []string{"nz", "pz", "px", "nx", "py", "ny"}

var commonScenes = []string{"off", "on", "ambient-only"}

var nzScenes = append(commonScenes[:3:3],
	"no-caster", "no-receiver", "discarded", "equal", "moved", "moved-off",
	"slot1", "slot0-paired", "mixed-slot1")

func objectOf(t *testing.T, ir scene.SceneIR, id string) scene.ObjectIR {
	t.Helper()
	count := 0
	var out scene.ObjectIR
	for _, o := range ir.Objects {
		if o.ID == id {
			count++
			out = o
		}
	}
	if count != 1 {
		t.Fatalf("object %q count = %d, want exactly 1", id, count)
	}
	return out
}

func lightOf(t *testing.T, ir scene.SceneIR, id string) scene.LightIR {
	t.Helper()
	for _, l := range ir.Lights {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("scene has no light %q", id)
	return scene.LightIR{}
}

func pointLights(ir scene.SceneIR) []scene.LightIR {
	var out []scene.LightIR
	for _, l := range ir.Lights {
		if l.Kind == "point" {
			out = append(out, l)
		}
	}
	return out
}

func TestDeterministicMarshal(t *testing.T) {
	a, err := json.Marshal(buildFixture())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := json.Marshal(buildFixture())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("fixture JSON is not deterministic across builds")
	}
	var f fixture
	if err := json.Unmarshal(a, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Schema != schema {
		t.Fatalf("schema = %q, want %q", f.Schema, schema)
	}
}

func TestSceneInventory(t *testing.T) {
	f := buildFixture()
	if len(f.Faces) != len(wantFaces) {
		t.Fatalf("faces = %d, want %d", len(f.Faces), len(wantFaces))
	}
	total := 0
	for _, name := range wantFaces {
		face, ok := f.Faces[name]
		if !ok {
			t.Fatalf("missing face %q", name)
		}
		total += len(face.Scenes)
		for _, key := range commonScenes {
			if _, ok := face.Scenes[key]; !ok {
				t.Fatalf("%s: missing scene %q", name, key)
			}
		}
		if name == "nz" {
			if len(face.Scenes) != len(nzScenes) {
				t.Fatalf("nz scenes = %d, want %d", len(face.Scenes), len(nzScenes))
			}
			for _, key := range nzScenes {
				if _, ok := face.Scenes[key]; !ok {
					t.Fatalf("nz: missing scene %q", key)
				}
			}
		} else if len(face.Scenes) != 3 {
			t.Fatalf("%s scenes = %d, want 3", name, len(face.Scenes))
		}
	}
	if total != 27 {
		t.Fatalf("total scenes = %d, want 27", total)
	}
}

func TestTransitions(t *testing.T) {
	f := buildFixture()
	for _, name := range wantFaces {
		face := f.Faces[name]
		if name != "nz" {
			if len(face.Transitions) != 0 {
				t.Fatalf("%s transitions = %d, want 0", name, len(face.Transitions))
			}
			continue
		}
		want := []struct{ name, from, to string }{
			{"off-to-on", "off", "on"},
			{"on-to-moved", "on", "moved"},
			{"moved-to-off", "moved", "off"},
		}
		if len(face.Transitions) != len(want) {
			t.Fatalf("nz transitions = %d, want %d", len(face.Transitions), len(want))
		}
		for i, w := range want {
			got := face.Transitions[i]
			if got.Name != w.name || got.From != w.from || got.To != w.to {
				t.Fatalf("nz transition %d = %s/%s/%s, want %s/%s/%s",
					i, got.Name, got.From, got.To, w.name, w.from, w.to)
			}
			if len(got.Commands) == 0 {
				t.Fatalf("nz transition %s has no real diff commands", got.Name)
			}
		}
	}
}

func TestFaceCameras(t *testing.T) {
	want := map[string]cameraJSON{
		"nz": {X: 0, Y: 0, Z: 4, RotationX: 0, RotationY: 0, RotationZ: 0, FOV: 50},
		"pz": {X: 0, Y: 0, Z: -4, RotationY: math.Pi, FOV: 50},
		"px": {X: -4, Y: 0, Z: 0, RotationY: -math.Pi / 2, FOV: 50},
		"nx": {X: 4, Y: 0, Z: 0, RotationY: math.Pi / 2, FOV: 50},
		"py": {X: 0, Y: -4, Z: 0, RotationX: math.Pi / 2, FOV: 50},
		"ny": {X: 0, Y: 4, Z: 0, RotationX: -math.Pi / 2, FOV: 50},
	}
	for _, name := range wantFaces {
		if got := buildFixture().Faces[name].Camera; got != want[name] {
			t.Fatalf("%s camera = %+v, want %+v", name, got, want[name])
		}
	}
}

func TestTransformedGeometry(t *testing.T) {
	f := buildFixture()
	for _, fs := range faces {
		face := f.Faces[fs.name]
		receiver := objectOf(t, face.Scenes["on"], receiverID)
		if receiver.X != fs.receiverPos[0] || receiver.Y != fs.receiverPos[1] || receiver.Z != fs.receiverPos[2] {
			t.Fatalf("%s receiver pos = %v/%v/%v", fs.name, receiver.X, receiver.Y, receiver.Z)
		}
		if receiver.Width != fs.receiverDims[0] || receiver.Height != fs.receiverDims[1] || receiver.Depth != fs.receiverDims[2] {
			t.Fatalf("%s receiver dims = %v/%v/%v", fs.name, receiver.Width, receiver.Height, receiver.Depth)
		}
		caster := objectOf(t, face.Scenes["on"], casterID)
		if caster.X != fs.casterPos[0] || caster.Y != fs.casterPos[1] || caster.Z != fs.casterPos[2] {
			t.Fatalf("%s caster pos = %v/%v/%v", fs.name, caster.X, caster.Y, caster.Z)
		}
		if caster.Width != fs.casterDims[0] || caster.Height != fs.casterDims[1] || caster.Depth != fs.casterDims[2] {
			t.Fatalf("%s caster dims = %v/%v/%v", fs.name, caster.Width, caster.Height, caster.Depth)
		}
		key := lightOf(t, face.Scenes["on"], lightID)
		if key.X != fs.keyPos[0] || key.Y != fs.keyPos[1] || key.Z != fs.keyPos[2] {
			t.Fatalf("%s key pos = %v/%v/%v", fs.name, key.X, key.Y, key.Z)
		}
	}
}

func TestPointControls(t *testing.T) {
	f := buildFixture()
	for _, name := range wantFaces {
		face := f.Faces[name]
		off := lightOf(t, face.Scenes["off"], lightID)
		on := lightOf(t, face.Scenes["on"], lightID)
		if off.CastShadow || !on.CastShadow {
			t.Fatalf("%s off/on cast = %v/%v", name, off.CastShadow, on.CastShadow)
		}
		for _, l := range []scene.LightIR{off, on} {
			if l.Color != "#ffffff" || l.Intensity != 6 || l.Range != 6.5 || l.Decay != 2 {
				t.Fatalf("%s key color/intensity/range/decay = %q/%v/%v/%v", name, l.Color, l.Intensity, l.Range, l.Decay)
			}
			if l.ShadowBias != 0.0001 || l.ShadowSize != 512 || l.ShadowSoftness != 0 {
				t.Fatalf("%s key shadow controls wrong: %+v", name, l)
			}
		}
		// off differs from on ONLY by the cast flag.
		off.CastShadow, on.CastShadow = false, false
		if !reflect.DeepEqual(off, on) {
			t.Fatalf("%s off/on lights differ beyond castShadow", name)
		}
		// ambient-only removes only the white point light.
		ao := face.Scenes["ambient-only"]
		if len(ao.Lights) != 1 || ao.Lights[0].ID != ambientID {
			t.Fatalf("%s ambient-only lights = %+v", name, ao.Lights)
		}
		if ao.Lights[0].Color != "#404040" || ao.Lights[0].Intensity != 0.3 {
			t.Fatalf("%s ambient-only ambient wrong: %+v", name, ao.Lights[0])
		}
		// Shared geometry identical between on and ambient-only.
		if !reflect.DeepEqual(face.Scenes["ambient-only"].Objects, face.Scenes["on"].Objects) {
			t.Fatalf("%s ambient-only objects differ from on", name)
		}
		// Ordered light nodes: ambient first, then the key.
		onLights := face.Scenes["on"].Lights
		if len(onLights) != 2 || onLights[0].ID != ambientID || onLights[1].ID != lightID {
			t.Fatalf("%s on light order = %+v", name, onLights)
		}
	}
}

func TestNZDetails(t *testing.T) {
	f := buildFixture().Faces["nz"]
	if objectOf(t, f.Scenes["no-caster"], casterID).CastShadow {
		t.Fatal("no-caster caster must not cast")
	}
	nr := objectOf(t, f.Scenes["no-receiver"], receiverID)
	if nr.ReceiveShadow {
		t.Fatal("no-receiver receiver must not receive")
	}
	discarded, err := json.Marshal(objectOf(t, f.Scenes["discarded"], casterID))
	if err != nil || !strings.Contains(string(discarded), `"opacity":0.25`) {
		t.Fatalf("discarded caster opacity wrong: %s (%v)", discarded, err)
	}
	equal, err := json.Marshal(objectOf(t, f.Scenes["equal"], casterID))
	if err != nil || !strings.Contains(string(equal), `"opacity":0.5`) {
		t.Fatalf("equal caster opacity wrong: %s (%v)", equal, err)
	}
	moved := lightOf(t, f.Scenes["moved"], lightID)
	base := lightOf(t, f.Scenes["on"], lightID)
	if math.Abs(moved.X-(base.X+0.6)) > 1e-12 || moved.Y != base.Y || moved.Z != base.Z {
		t.Fatalf("moved pos = %v/%v/%v, want x shifted by +0.6", moved.X, moved.Y, moved.Z)
	}
	if !moved.CastShadow {
		t.Fatal("moved must cast")
	}
	movedOff := lightOf(t, f.Scenes["moved-off"], lightID)
	if movedOff.CastShadow {
		t.Fatal("moved-off must not cast")
	}
	movedOff.CastShadow, moved.CastShadow = false, false
	if !reflect.DeepEqual(movedOff, moved) {
		t.Fatal("moved/moved-off lights differ beyond castShadow")
	}
}

func TestSlotOrdering(t *testing.T) {
	nz := buildFixture().Faces["nz"]

	slot1 := pointLights(nz.Scenes["slot1"])
	if len(slot1) != 2 || slot1[0].ID != blackPointID || slot1[1].ID != lightID {
		t.Fatalf("slot1 point order = %+v", slot1)
	}
	slot0 := pointLights(nz.Scenes["slot0-paired"])
	if len(slot0) != 2 || slot0[0].ID != lightID || slot0[1].ID != blackPointID {
		t.Fatalf("slot0-paired point order = %+v", slot0)
	}
	for _, black := range []scene.LightIR{slot1[0], slot0[1]} {
		if black.Color != "#000000" || black.Intensity != 0.5 || !black.CastShadow {
			t.Fatalf("black point wrong: %+v", black)
		}
		key := lightOf(t, nz.Scenes["slot1"], lightID)
		if black.X == key.X && black.Y == key.Y && black.Z == key.Z {
			t.Fatal("black point position must be distinct from key")
		}
	}

	// mixed-slot1: typed fixture emits lights in authored order:
	// point-ambient, black directional, white key.
	lights := nz.Scenes["mixed-slot1"].Lights
	if len(lights) != 3 ||
		lights[0].ID != ambientID ||
		lights[1].Kind != "directional" || lights[1].ID != blackDirID ||
		lights[2].ID != lightID {
		t.Fatalf("mixed-slot1 light order = %+v", lights)
	}
	dir := lights[1]
	if dir.Color != "#000000" || dir.Intensity != 0.5 || !dir.CastShadow || dir.ShadowSize != 256 {
		t.Fatalf("black directional wrong: %+v", dir)
	}
	if dir.DirectionX != 0.7 || dir.DirectionY != -0.7 || dir.DirectionZ != -1 {
		t.Fatalf("black directional direction = %v/%v/%v", dir.DirectionX, dir.DirectionY, dir.DirectionZ)
	}
}
