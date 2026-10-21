package scene

import (
	"bytes"
	"encoding/json"
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
