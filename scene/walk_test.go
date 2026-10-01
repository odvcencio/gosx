package scene

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

func TestWalkGroundEncodingRoundTrip(t *testing.T) {
	for _, heights := range [][]float64{{-2, 0.4, 1.234, 8}, {3, 3, 3, 3}} {
		ground := NewWalkGround(-1, -2, 3, 4, 2, 2, heights)
		data, err := base64.StdEncoding.DecodeString(ground.Heights)
		if err != nil || len(data) != 2*len(heights) {
			t.Fatalf("height encoding: %v (%d bytes)", err, len(data))
		}
		for i, want := range heights {
			got := ground.MinHeight + float64(binary.LittleEndian.Uint16(data[2*i:]))/65535*(ground.MaxHeight-ground.MinHeight)
			if math.Abs(got-want) > (ground.MaxHeight-ground.MinHeight)/65535 {
				t.Fatalf("sample %d: got %g, want %g", i, got, want)
			}
		}
	}
}

func TestWalkGroundRejectsSampleMismatch(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for sample count mismatch")
		}
	}()
	NewWalkGround(0, 0, 1, 1, 2, 2, []float64{0})
}

func TestWalkLoweringIsOptInAndLeavesDefaultsToBrowser(t *testing.T) {
	for _, walk := range []*Walk{nil, {}} {
		props := Props{Controls: ControlFirstPerson, Walk: walk}
		for _, lowered := range []map[string]any{props.LegacyProps(), props.GoSXSpreadProps()} {
			_, present := lowered["walk"]
			if present != (walk != nil) {
				t.Fatalf("walk presence = %v", present)
			}
		}
		data, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if walk == nil {
			if _, present := decoded["walk"]; present {
				t.Fatal("nil walk emitted")
			}
		} else if string(decoded["walk"]) != "{}" {
			t.Fatalf("defaults baked in: %s", decoded["walk"])
		}
	}
	zero, disabled := 0.0, false
	data, err := json.Marshal(Walk{HeadBob: &zero, Gamepad: &disabled})
	if err != nil || string(data) != `{"headBob":0,"gamepad":false}` {
		t.Fatalf("explicit opt-outs: %s (%v)", data, err)
	}
}
