package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/scene"
)

var wantSceneKeys = []string{
	"off", "on", "ambient-only", "no-caster", "no-receiver",
	"discarded", "equal", "moved", "moved-off", "aimed", "aimed-off",
	"invalid-prefix",
}

var wantTransitionNames = []string{
	"off-to-on", "on-to-moved", "moved-to-aimed", "aimed-to-off",
}

func objectOf(t *testing.T, ir scene.SceneIR, id string) scene.ObjectIR {
	t.Helper()
	for _, o := range ir.Objects {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("scene has no object %q", id)
	return scene.ObjectIR{}
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

func spotLights(ir scene.SceneIR) []scene.LightIR {
	var out []scene.LightIR
	for _, l := range ir.Lights {
		if l.Kind == "spot" {
			out = append(out, l)
		}
	}
	return out
}

func ambientOf(t *testing.T, ir scene.SceneIR) scene.LightIR {
	t.Helper()
	for _, l := range ir.Lights {
		if l.ID == ambientID {
			return l
		}
	}
	t.Fatalf("scene has no ambient %q", ambientID)
	return scene.LightIR{}
}

func TestSceneInventory(t *testing.T) {
	f := buildFixture()
	if f.Schema != schema {
		t.Fatalf("schema = %q, want %q", f.Schema, schema)
	}
	if len(f.Scenes) != len(wantSceneKeys) {
		t.Fatalf("scenes = %d, want %d", len(f.Scenes), len(wantSceneKeys))
	}
	for _, key := range wantSceneKeys {
		if _, ok := f.Scenes[key]; !ok {
			t.Fatalf("missing scene %q", key)
		}
	}
}

func TestSharedReceiverAndAmbient(t *testing.T) {
	f := buildFixture()
	for name, ir := range f.Scenes {
		receiver := objectOf(t, ir, receiverID)
		if receiver.MaterialKind != "standard" {
			t.Fatalf("%s: receiver materialKind = %q", name, receiver.MaterialKind)
		}
		if receiver.Color != "#ffffff" {
			t.Fatalf("%s: receiver color = %q", name, receiver.Color)
		}
		if receiver.Width != 3 || receiver.Height != 2.2 || receiver.Depth != 0.1 {
			t.Fatalf("%s: receiver box = %v/%v/%v", name, receiver.Width, receiver.Height, receiver.Depth)
		}
		if receiver.X != 0 || receiver.Y != 0 || receiver.Z != -0.5 {
			t.Fatalf("%s: receiver position = %v/%v/%v", name, receiver.X, receiver.Y, receiver.Z)
		}
		if receiver.CastShadow {
			t.Fatalf("%s: receiver must not cast", name)
		}
		if receiver.Wireframe == nil || *receiver.Wireframe {
			t.Fatalf("%s: receiver must be non-wireframe", name)
		}
		if receiver.Roughness != 1 || receiver.Metalness != 0 || receiver.IOR == nil || *receiver.IOR != 1.5 {
			t.Fatalf("%s: receiver standard material values wrong", name)
		}

		ambient := ambientOf(t, ir)
		if ambient.Intensity != 0.3 || ambient.Color != "#404040" {
			t.Fatalf("%s: ambient = %q/%v", name, ambient.Color, ambient.Intensity)
		}
	}
	// ambient-only removes ONLY the primary spot: the receiver and caster
	// geometry and the ambient light are identical to the "on" scene.
	ao := f.Scenes["ambient-only"]
	on := f.Scenes["on"]
	if !reflect.DeepEqual(ao.Objects, on.Objects) {
		t.Fatalf("ambient-only objects differ from on: %+v vs %+v", ao.Objects, on.Objects)
	}
	if len(ao.Lights) != 1 || ao.Lights[0].ID != ambientID {
		t.Fatalf("ambient-only lights = %+v", ao.Lights)
	}
	if !reflect.DeepEqual(ao.Lights[0], ambientOf(t, on)) {
		t.Fatalf("ambient-only ambient differs from on: %+v vs %+v", ao.Lights[0], ambientOf(t, on))
	}
}

func TestSpotLightWireFields(t *testing.T) {
	f := buildFixture()
	light := lightOf(t, f.Scenes["on"], lightID)
	if light.Kind != "spot" {
		t.Fatalf("light kind = %q, want spot", light.Kind)
	}
	if light.Intensity != 6 {
		t.Fatalf("light intensity = %v, want 6", light.Intensity)
	}
	if light.X != -1.55 || light.Y != 1.35 || light.Z != 2.5 {
		t.Fatalf("light position = %v/%v/%v", light.X, light.Y, light.Z)
	}
	if light.DirectionX != 1 || light.DirectionY != -1 || light.DirectionZ != -2 {
		t.Fatalf("light direction = %v/%v/%v", light.DirectionX, light.DirectionY, light.DirectionZ)
	}
	if light.Angle != 0.75 {
		t.Fatalf("light angle = %v, want 0.75", light.Angle)
	}
	if light.Range != 6.5 {
		t.Fatalf("light range = %v, want 6.5", light.Range)
	}
	if light.ShadowSoftness != 0 {
		t.Fatalf("light softness = %v, want 0", light.ShadowSoftness)
	}
	if !light.CastShadow {
		t.Fatal("on-scene light must cast shadows")
	}
	// Wire-level assertions on the marshalled IR: exact shadow fields that
	// survive omitempty.
	raw, err := json.Marshal(light)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, want := range []string{`"castShadow":true`, `"shadowBias":0.0001`, `"shadowSize":512`, `"angle":0.75`, `"intensity":6`, `"range":6.5`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("spot light wire missing %s: %s", want, wire)
		}
	}
}

func TestCasterFlagsAndAlphaCutoffs(t *testing.T) {
	f := buildFixture()
	for _, name := range []string{"on", "off", "moved", "aimed", "moved-off", "aimed-off", "no-receiver", "invalid-prefix"} {
		caster := objectOf(t, f.Scenes[name], casterID)
		if !caster.CastShadow || caster.ReceiveShadow {
			t.Fatalf("%s: caster shadow flags wrong: cast=%v receive=%v", name, caster.CastShadow, caster.ReceiveShadow)
		}
		if caster.Width != 0.55 || caster.Height != 0.55 || caster.Depth != 0.15 {
			t.Fatalf("%s: caster box = %v/%v/%v", name, caster.Width, caster.Height, caster.Depth)
		}
		if caster.Color != "#c02020" {
			t.Fatalf("%s: caster color = %q", name, caster.Color)
		}
	}
	receiver := objectOf(t, f.Scenes["on"], receiverID)
	if receiver.CastShadow || !receiver.ReceiveShadow {
		t.Fatalf("receiver shadow flags wrong: cast=%v receive=%v", receiver.CastShadow, receiver.ReceiveShadow)
	}

	discarded, err := json.Marshal(objectOf(t, f.Scenes["discarded"], casterID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(discarded), `"opacity":0.25`) || !strings.Contains(string(discarded), `"alphaCutoff":0.5`) {
		t.Fatalf("discarded caster wire wrong: %s", discarded)
	}
	equal, err := json.Marshal(objectOf(t, f.Scenes["equal"], casterID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(equal), `"opacity":0.5`) || !strings.Contains(string(equal), `"alphaCutoff":0.5`) {
		t.Fatalf("equal caster wire wrong: %s", equal)
	}
}

func TestNegativeControls(t *testing.T) {
	f := buildFixture()
	for _, name := range []string{"off", "moved-off", "aimed-off"} {
		if lightOf(t, f.Scenes[name], lightID).CastShadow {
			t.Fatalf("%s: light must not cast shadows", name)
		}
	}
	if objectOf(t, f.Scenes["no-caster"], casterID).CastShadow {
		t.Fatal("no-caster caster must not cast")
	}
	if objectOf(t, f.Scenes["no-receiver"], receiverID).ReceiveShadow {
		t.Fatal("no-receiver receiver must not receive")
	}
	// on vs off: identical except the cast flag.
	onLight, offLight := lightOf(t, f.Scenes["on"], lightID), lightOf(t, f.Scenes["off"], lightID)
	onLight.CastShadow, offLight.CastShadow = false, false
	if !reflect.DeepEqual(onLight, offLight) {
		t.Fatalf("on/off lights differ beyond castShadow: %+v vs %+v", onLight, offLight)
	}
	// moved-off vs moved: identical except the cast flag.
	movedLight, movedOffLight := lightOf(t, f.Scenes["moved"], lightID), lightOf(t, f.Scenes["moved-off"], lightID)
	movedLight.CastShadow, movedOffLight.CastShadow = false, false
	if !reflect.DeepEqual(movedLight, movedOffLight) {
		t.Fatalf("moved/moved-off lights differ beyond castShadow")
	}
	// aimed-off vs aimed: identical except the cast flag.
	aimedLight, aimedOffLight := lightOf(t, f.Scenes["aimed"], lightID), lightOf(t, f.Scenes["aimed-off"], lightID)
	aimedLight.CastShadow, aimedOffLight.CastShadow = false, false
	if !reflect.DeepEqual(aimedLight, aimedOffLight) {
		t.Fatalf("aimed/aimed-off lights differ beyond castShadow")
	}
}

func TestMovedAndAimedDifferences(t *testing.T) {
	f := buildFixture()
	base := lightOf(t, f.Scenes["on"], lightID)
	moved := lightOf(t, f.Scenes["moved"], lightID)
	if moved.X != -0.95 || moved.Y != base.Y || moved.Z != base.Z {
		t.Fatalf("moved position = %v/%v/%v, want x shifted by +0.6", moved.X, moved.Y, moved.Z)
	}
	if moved.X == base.X {
		t.Fatal("moved light position must differ from base")
	}
	aimed := lightOf(t, f.Scenes["aimed"], lightID)
	if aimed.X != base.X || aimed.Y != base.Y || aimed.Z != base.Z {
		t.Fatal("aimed light must keep the base position")
	}
	if aimed.DirectionX == base.DirectionX && aimed.DirectionY == base.DirectionY && aimed.DirectionZ == base.DirectionZ {
		t.Fatal("aimed light direction must differ from base")
	}
}

func TestInvalidPrefix(t *testing.T) {
	f := buildFixture()
	spots := spotLights(f.Scenes["invalid-prefix"])
	if len(spots) != 3 {
		t.Fatalf("invalid-prefix spot count = %d, want 3", len(spots))
	}
	if spots[0].ID != blackSpot1 || spots[1].ID != blackSpot2 {
		t.Fatalf("prefix order = %q,%q, want %q,%q", spots[0].ID, spots[1].ID, blackSpot1, blackSpot2)
	}
	for _, black := range spots[:2] {
		if black.Color != "#000000" {
			t.Fatalf("black light %s color = %q", black.ID, black.Color)
		}
		if black.Intensity <= 0 {
			t.Fatalf("black light %s intensity = %v, want nonzero so omitempty never resurrects zero", black.ID, black.Intensity)
		}
		if black.Angle <= 1.5707963267948966 {
			t.Fatalf("black light %s angle = %v, want > 90 degrees half-cone", black.ID, black.Angle)
		}
		if !black.CastShadow {
			t.Fatalf("black light %s must request shadows", black.ID)
		}
	}
	if spots[2].ID != lightID || !spots[2].CastShadow || spots[2].Angle != 0.75 {
		t.Fatalf("valid primary spot must survive the prefix: %+v", spots[2])
	}
}

func TestTransitions(t *testing.T) {
	f := buildFixture()
	if len(f.Transitions) != len(wantTransitionNames) {
		t.Fatalf("transitions = %d, want %d", len(f.Transitions), len(wantTransitionNames))
	}
	payloads := make([]string, len(f.Transitions))
	for i, wantName := range wantTransitionNames {
		tr := f.Transitions[i]
		if tr.Name != wantName {
			t.Fatalf("transition %d name = %q, want %q", i, tr.Name, wantName)
		}
		if tr.From != tr.Name[:strings.Index(tr.Name, "-to-")] {
			t.Fatalf("transition %d from = %q", i, tr.From)
		}
		if tr.To != tr.Name[strings.Index(tr.Name, "-to-")+4:] {
			t.Fatalf("transition %d to = %q", i, tr.To)
		}
		if _, ok := f.Scenes[tr.From]; !ok {
			t.Fatalf("transition %d unknown from scene %q", i, tr.From)
		}
		if _, ok := f.Scenes[tr.To]; !ok {
			t.Fatalf("transition %d unknown to scene %q", i, tr.To)
		}
		// Each live transition replaces exactly the primary spot light with
		// the real remove + create pair. Remove commands carry no payload by
		// contract (Data is nil), so nil is allowed ONLY for the remove; the
		// create command must carry the light CommandPayload.
		if len(tr.Commands) != 2 {
			t.Fatalf("transition %d has %d commands, want the remove+create light pair", i, len(tr.Commands))
		}
		remove, create := tr.Commands[0], tr.Commands[1]
		if remove.Kind != scene.CommandRemoveObject || remove.ObjectID != lightID {
			t.Fatalf("transition %d first command = %+v, want remove of %q", i, remove, lightID)
		}
		if remove.Data != nil {
			t.Fatalf("transition %d remove payload = %+v, want nil", i, remove.Data)
		}
		if create.Kind != scene.CommandCreateObject || create.ObjectID != lightID {
			t.Fatalf("transition %d second command = %+v, want create of %q", i, create, lightID)
		}
		if create.Data == nil {
			t.Fatalf("transition %d create command must carry a payload", i)
		}
		payload, ok := create.Data.(scene.CommandPayload)
		if !ok {
			t.Fatalf("transition %d create payload type = %T, want scene.CommandPayload", i, create.Data)
		}
		if payload.Kind != "light" {
			t.Fatalf("transition %d create payload kind = %q, want light", i, payload.Kind)
		}
		props, ok := payload.Props.(scene.LightIR)
		if !ok {
			t.Fatalf("transition %d create props type = %T, want scene.LightIR", i, payload.Props)
		}
		if props.ID != lightID {
			t.Fatalf("transition %d create props id = %q, want %q", i, props.ID, lightID)
		}
		if !reflect.DeepEqual(props, lightOf(t, f.Scenes[tr.To], lightID)) {
			t.Fatalf("transition %d create payload disagrees with destination scene %q", i, tr.To)
		}
		raw, err := json.Marshal(tr.Commands)
		if err != nil {
			t.Fatal(err)
		}
		payloads[i] = string(raw)
		// All primitive objects must remain unchanged across the live
		// transitions; only lights move.
		if !reflect.DeepEqual(f.Scenes[tr.From].Objects, f.Scenes[tr.To].Objects) {
			t.Fatalf("transition %d changed primitive objects", i)
		}
	}
	for i := 0; i < len(payloads); i++ {
		for j := i + 1; j < len(payloads); j++ {
			if payloads[i] == payloads[j] {
				t.Fatalf("transitions %d and %d carry identical command payloads", i, j)
			}
		}
	}
	// off-to-on must change only the light, so its payload must differ from
	// on-to-moved (also only-light) while both are nonempty and real.
	if payloads[0] == "" || payloads[1] == "" {
		t.Fatal("live transitions must carry nonempty command payloads")
	}
}

func TestDeterministicJSON(t *testing.T) {
	first, err := json.Marshal(buildFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(buildFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("fixture JSON is not deterministic across builds")
	}
}
