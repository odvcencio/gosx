package scene

import (
	"bytes"
	"encoding/json"
	"m31labs.dev/gosx/vessel"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVesselPoseFrameRoundTripAndSize(t *testing.T) {
	frame := VesselPoseFrame{Vessels: []VesselPose{
		{ID: "liner", Time: 12.5, X: 1234.5678, Y: 0.25, Z: -98765.4321, Heading: 1.5, Pitch: -0.01, Roll: 0.02, Speed: 9.75, Snap: true},
		{ID: "tender", Time: 12.5, X: -1, Y: 0, Z: 2},
	}}
	b, err := EncodeVesselPoseFrame(nil, frame)
	if err != nil {
		t.Fatal(err)
	}
	if want := VesselPoseFrameSize(frame); len(b) != want || want != 6+(51+5)+(51+6) {
		t.Fatalf("encoded %d bytes, size says %d", len(b), want)
	}
	one, _ := EncodeVesselPoseFrame(nil, VesselPoseFrame{Vessels: frame.Vessels[:1]})
	if len(one) != 57+len("liner") {
		t.Fatalf("one-ship frame is %d bytes, want %d", len(one), 57+len("liner"))
	}
	got, err := DecodeVesselPoseFrame(b)
	if err != nil || !reflect.DeepEqual(got, frame) {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	for _, bad := range [][]byte{nil, b[:5], b[:len(b)-1], append(append([]byte{}, b...), 0), append([]byte("GSV2"), b[4:]...)} {
		if _, err := DecodeVesselPoseFrame(bad); err == nil {
			t.Errorf("decoded a malformed frame of %d bytes", len(bad))
		}
	}
	if _, err := EncodeVesselPoseFrame(nil, VesselPoseFrame{Vessels: []VesselPose{{}}}); err == nil {
		t.Error("an empty ID should be rejected")
	}
}

func TestVesselExternalPoseIsOptIn(t *testing.T) {
	plain, _ := json.Marshal(Vessel{NodeID: "ship"})
	if strings.Contains(string(plain), "externalPose") {
		t.Fatal("externalPose must stay opt-in")
	}
	on, _ := json.Marshal(Vessel{NodeID: "ship", ExternalPose: true})
	if !strings.Contains(string(on), `"externalPose":true`) {
		t.Fatalf("missing externalPose: %s", on)
	}
}

// The JavaScript decoder (client/js/scene3d-vessel-external.test.js) reads the
// same bytes, so the two sides cannot drift apart unnoticed.
func TestVesselPoseFrameGolden(t *testing.T) {
	frame := VesselPoseFrame{Vessels: []VesselPose{
		{ID: "liner", Time: 12.5, X: 1234.5, Y: 0.25, Z: -98765.5, Heading: 1.5, Pitch: -0.25, Roll: 0.125, Speed: 9.75, Snap: true},
		{ID: "tender", Time: 12.5, X: -1, Y: 0, Z: 2, Heading: -3},
	}}
	got, err := EncodeVesselPoseFrame(nil, frame)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "client", "js", "testdata", "vessel-pose-frame.bin")
	if os.Getenv("UPDATE_VESSEL_POSE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoding changed; rerun with UPDATE_VESSEL_POSE_GOLDEN=1 and update the JavaScript decoder")
	}
}

func TestVesselDecksFromLayoutWire(t *testing.T) {
	layout := vessel.StandardLayout(vessel.Liner())
	decks := VesselDecksFromLayout(layout)
	if len(decks) != 14 || len(decks[0].Polygons) != 1 || len(decks[0].Ramps) != 2 || len(decks[13].Ramps) != 0 {
		t.Fatalf("decks %d, deck 0 polygons %d ramps %d", len(decks), len(decks[0].Polygons), len(decks[0].Ramps))
	}
	if len(decks[5].Holes) < 3 {
		t.Errorf("deck 5 should have an atrium and two stair holes, has %d", len(decks[5].Holes))
	}
	v := Vessel{NodeID: "liner", ExternalPose: true, Decks: decks}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back Vessel
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back.Decks, decks) {
		t.Fatalf("decks did not survive JSON: %v", err)
	}
	plain, _ := json.Marshal(Vessel{NodeID: "ship"})
	if strings.Contains(string(plain), "decks") {
		t.Error("decks must stay opt-in")
	}
	t.Logf("liner decks JSON: %d bytes for %d decks", len(b), len(decks))
}

// TestVesselDeckConformanceFixture writes (with UPDATE_VESSEL_DECK_FIXTURE=1) and
// checks the fixture that client/js/scene3d-vessel-decks.test.js replays in
// JavaScript: the same decks, the same rolled poses, and Go's answers.
func TestVesselDeckConformanceFixture(t *testing.T) {
	layout := vessel.StandardLayout(vessel.Liner())
	type probe struct {
		Pose    [6]float64 `json:"pose"`
		Local   [3]float64 `json:"local"`
		Deck    int        `json:"deck"`  // Go DeckAt on the local point, feet at Local[1]
		World   [3]float64 `json:"world"` // Go ToWorld(local)
		Floor   float64    `json:"floor"` // Go FloorAt height, ship-local
		FloorOK bool       `json:"floorOk"`
	}
	var probes []probe
	poses := []vessel.Pose{{}, {X: 500, Y: 1, Z: -300, Heading: 1.1, Pitch: 0.04, Roll: 0.21}, {X: -80, Y: -0.5, Z: 40, Heading: -2.4, Pitch: -0.05, Roll: -0.15}}
	spots := [][2]float64{{-10, 60}, {12, -50}, {-14, 0}, {0, 0}, {0.1, -86}, {30, 30}}
	for _, pose := range poses {
		for di := range layout.Decks {
			for _, s := range spots {
				for _, dy := range []float64{0, 1.2} {
					local := vessel.Vec3{X: s[0], Y: layout.Decks[di].Y + dy, Z: s[1]}
					w := pose.ToWorld(local)
					h, _, ok := layout.FloorAt(local.X, local.Y, local.Z)
					probes = append(probes, probe{
						Pose: [6]float64{pose.X, pose.Y, pose.Z, pose.Heading, pose.Pitch, pose.Roll}, Local: [3]float64{local.X, local.Y, local.Z},
						Deck: layout.DeckAt(local.X, local.Y, local.Z), World: [3]float64{w.X, w.Y, w.Z}, Floor: h, FloorOK: ok,
					})
				}
			}
		}
	}
	fixture := struct {
		Decks  []VesselDeck `json:"decks"`
		Probes []probe      `json:"probes"`
	}{VesselDecksFromLayout(layout), probes}
	got, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "client", "js", "testdata", "vessel-decks-liner.json")
	if os.Getenv("UPDATE_VESSEL_DECK_FIXTURE") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("deck fixture is stale; rerun with UPDATE_VESSEL_DECK_FIXTURE=1")
	}
}
