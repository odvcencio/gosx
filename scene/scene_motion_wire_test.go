package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/motion"
)

// TestMotionProgram_SpinProducesNonEmpty verifies that a scene with a spinning
// mesh produces a non-nil, non-empty MotionProgram in SceneIR.
func TestMotionProgram_SpinProducesNonEmpty(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "spinning-cube",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Spin:     Euler{X: 0, Y: 1.2, Z: 0},
				},
			},
		},
	}
	ir := props.SceneIR()

	if len(ir.MotionProgram) == 0 {
		t.Fatal("MotionProgram is empty, want non-empty for spinning mesh")
	}
}

// TestMotionProgram_DecodeRoundTrip verifies that motion.DecodeProgram round-trips
// the MotionProgram: the decoded timeline's track targets the mesh's id, uses
// "rotation" as the prop, and motion.Eval at t=1.0 yields QuatFromEuler(0,1.2,0).
func TestMotionProgram_DecodeRoundTrip(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "spinning-cube",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Spin:     Euler{X: 0, Y: 1.2, Z: 0},
				},
			},
		},
	}
	ir := props.SceneIR()

	if len(ir.MotionProgram) == 0 {
		t.Fatal("MotionProgram is empty")
	}

	tl, targetRefs, propRefs, err := motion.DecodeProgram(ir.MotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	if tl == nil {
		t.Fatal("decoded timeline is nil")
	}
	if len(tl.Children) != 1 {
		t.Fatalf("decoded timeline has %d children, want 1", len(tl.Children))
	}

	track := tl.Children[0].Track
	if track == nil {
		t.Fatal("decoded track is nil")
	}

	// Verify target ref (object id) is "spinning-cube".
	targetID := track.TargetID
	if targetID < 0 || targetID >= len(targetRefs) {
		t.Fatalf("targetID %d out of range [0, %d)", targetID, len(targetRefs))
	}
	if got := targetRefs[targetID]; got != "spinning-cube" {
		t.Errorf("targetRefs[%d] = %q, want %q", targetID, got, "spinning-cube")
	}

	// Verify prop ref is "rotation".
	propID := track.PropID
	if propID < 0 || propID >= len(propRefs) {
		t.Fatalf("propID %d out of range [0, %d)", propID, len(propRefs))
	}
	if got := propRefs[propID]; got != "rotation" {
		t.Errorf("propRefs[%d] = %q, want %q", propID, got, "rotation")
	}

	// Eval at t=1.0 and verify the quaternion matches QuatFromEuler(0, 1.2, 0).
	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 1.0, motion.Policy{}, buf)
	got := buf.Writes()
	if len(got) != 7 {
		t.Fatalf("expected 7 floats in WriteBuf, got %d: %v", len(got), got)
	}
	// got[2] is arity, got[3..6] are x,y,z,w.
	if got[2] != float64(motion.ArityQuat) {
		t.Errorf("arity = %v, want ArityQuat (%v)", got[2], motion.ArityQuat)
	}
	q := motion.QuatFromEuler(0, 1.2*1.0, 0)
	const eps = 1e-9
	if math.Abs(got[3]-q.X) > eps {
		t.Errorf("quat.X = %.10f, want %.10f", got[3], q.X)
	}
	if math.Abs(got[4]-q.Y) > eps {
		t.Errorf("quat.Y = %.10f, want %.10f", got[4], q.Y)
	}
	if math.Abs(got[5]-q.Z) > eps {
		t.Errorf("quat.Z = %.10f, want %.10f", got[5], q.Z)
	}
	if math.Abs(got[6]-q.W) > eps {
		t.Errorf("quat.W = %.10f, want %.10f", got[6], q.W)
	}
}

// TestMotionProgram_NoSpinIsNil verifies that a scene with no spinning objects
// leaves MotionProgram nil/empty, and that JSON output omits the "motionProgram" key.
func TestMotionProgram_NoSpinIsNil(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "static-cube",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
				},
			},
		},
	}
	ir := props.SceneIR()

	if len(ir.MotionProgram) != 0 {
		t.Errorf("MotionProgram should be nil/empty for static scene, got %d bytes", len(ir.MotionProgram))
	}

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if strings.Contains(string(data), "motionProgram") {
		t.Errorf("JSON should not contain 'motionProgram' for static scene, got: %s", string(data))
	}
}

// TestMotionProgram_JSONBase64RoundTrip verifies that json.Marshal → json.Unmarshal
// preserves MotionProgram (as base64) and that DecodeProgram still works after the
// round-trip.
func TestMotionProgram_JSONBase64RoundTrip(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "spinner",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Spin:     Euler{X: 0, Y: 0.5, Z: 0},
				},
			},
		},
	}
	ir := props.SceneIR()

	if len(ir.MotionProgram) == 0 {
		t.Fatal("MotionProgram is empty before marshal")
	}

	// Marshal to JSON.
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	// Verify "motionProgram" key is present in the JSON.
	if !strings.Contains(string(data), "motionProgram") {
		t.Errorf("JSON should contain 'motionProgram', got: %s", string(data))
	}

	// Unmarshal back.
	var ir2 SceneIR
	if err := json.Unmarshal(data, &ir2); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if len(ir2.MotionProgram) == 0 {
		t.Fatal("MotionProgram is empty after JSON round-trip")
	}

	// Verify it still decodes correctly.
	tl, targetRefs, _, err := motion.DecodeProgram(ir2.MotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram after JSON round-trip error: %v", err)
	}
	if tl == nil || len(tl.Children) == 0 {
		t.Fatal("decoded timeline empty after JSON round-trip")
	}
	track := tl.Children[0].Track
	if track == nil {
		t.Fatal("decoded track is nil after JSON round-trip")
	}
	if targetRefs[track.TargetID] != "spinner" {
		t.Errorf("targetRefs[%d] = %q, want %q", track.TargetID, targetRefs[track.TargetID], "spinner")
	}
}

// TestMotionProgram_SpinTracksUnchanged verifies that adding MotionProgram is
// non-breaking: SpinTracks is still populated and the legacy SpinX/Y/Z fields
// are still present in ObjectIR.
func TestMotionProgram_SpinTracksUnchanged(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "cube",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Spin:     Euler{X: 0, Y: 1.2, Z: 0},
				},
			},
		},
	}
	ir := props.SceneIR()

	// SpinTracks still populated (in-memory, non-breaking).
	if len(ir.SpinTracks) != 1 {
		t.Fatalf("SpinTracks = %d, want 1", len(ir.SpinTracks))
	}
	if ir.SpinTracks[0].Target.Ref != "cube" {
		t.Errorf("SpinTracks[0].Target.Ref = %q, want %q", ir.SpinTracks[0].Target.Ref, "cube")
	}

	// Legacy SpinY still in ObjectIR.
	if len(ir.Objects) != 1 {
		t.Fatalf("Objects = %d, want 1", len(ir.Objects))
	}
	if ir.Objects[0].SpinY != 1.2 {
		t.Errorf("Objects[0].SpinY = %v, want 1.2", ir.Objects[0].SpinY)
	}
}
