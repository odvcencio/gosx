package scene

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/motion"
)

// emissiveMesh builds a Props with one mesh carrying a 4-component (color) LINEAR
// emissive material-uniform animation from black-opaque to white-opaque over [0,1].
func emissiveMesh(id string) Props {
	return Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       id,
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					MaterialAnims: []MaterialUniformAnim{
						{
							Uniform: "emissive",
							Arity:   4,
							Times:   []float64{0, 1},
							Values:  []float64{0, 0, 0, 1, 1, 1, 1, 1},
							Interp:  "LINEAR",
						},
					},
				},
			},
		},
	}
}

// TestMaterialMotionProgram_NonEmpty verifies a mesh with a MaterialAnims entry
// produces a non-empty MaterialMotionProgram in SceneIR.
func TestMaterialMotionProgram_NonEmpty(t *testing.T) {
	ir := emissiveMesh("glow-cube").SceneIR()
	if len(ir.MaterialMotionProgram) == 0 {
		t.Fatal("MaterialMotionProgram is empty, want non-empty for animated material")
	}
}

// TestMaterialMotionProgram_DecodeRoundTrip verifies DecodeProgram round-trips
// the material program: the track targets the mesh id with Kind==TargetMaterial,
// prop "emissive", and Eval at t=0.5 lerps to [0.5,0.5,0.5,1].
func TestMaterialMotionProgram_DecodeRoundTrip(t *testing.T) {
	ir := emissiveMesh("glow-cube").SceneIR()
	if len(ir.MaterialMotionProgram) == 0 {
		t.Fatal("MaterialMotionProgram is empty")
	}

	tl, targetRefs, propRefs, err := motion.DecodeProgram(ir.MaterialMotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	if tl == nil || len(tl.Children) != 1 {
		t.Fatalf("decoded timeline has %v children, want 1", tlChildCount(tl))
	}
	track := tl.Children[0].Track
	if track == nil {
		t.Fatal("decoded track is nil")
	}

	// Target kind must be TargetMaterial.
	if track.Target.Kind != motion.TargetMaterial {
		t.Errorf("Target.Kind = %v, want TargetMaterial (%v)", track.Target.Kind, motion.TargetMaterial)
	}

	// Ref == mesh id.
	if track.TargetID < 0 || track.TargetID >= len(targetRefs) {
		t.Fatalf("targetID %d out of range [0,%d)", track.TargetID, len(targetRefs))
	}
	if got := targetRefs[track.TargetID]; got != "glow-cube" {
		t.Errorf("targetRefs[%d] = %q, want %q", track.TargetID, got, "glow-cube")
	}

	// Prop == "emissive".
	if track.PropID < 0 || track.PropID >= len(propRefs) {
		t.Fatalf("propID %d out of range [0,%d)", track.PropID, len(propRefs))
	}
	if got := propRefs[track.PropID]; got != "emissive" {
		t.Errorf("propRefs[%d] = %q, want %q", track.PropID, got, "emissive")
	}

	// Eval at t=0.5 → lerped [0.5,0.5,0.5,1]. Width-4 color arity.
	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 0.5, motion.Policy{}, buf)
	got := buf.Writes()
	if len(got) != 7 {
		t.Fatalf("expected 7 floats (target,prop,arity,r,g,b,a), got %d: %v", len(got), got)
	}
	if got[2] != float64(motion.ArityColor) {
		t.Errorf("arity = %v, want ArityColor (%v)", got[2], motion.ArityColor)
	}
	want := []float64{0.5, 0.5, 0.5, 1}
	const eps = 1e-9
	for i, w := range want {
		if math.Abs(got[3+i]-w) > eps {
			t.Errorf("component[%d] = %.10f, want %.10f (full: %v)", i, got[3+i], w, got)
		}
	}
}

// TestMaterialMotionProgram_ScalarUniform verifies an Arity:1 uniform (roughness)
// lowers to an ArityScalar track and evals correctly mid-segment.
func TestMaterialMotionProgram_ScalarUniform(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "rough-cube",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					MaterialAnims: []MaterialUniformAnim{
						{
							Uniform: "roughness",
							Arity:   1,
							Times:   []float64{0, 1},
							Values:  []float64{0.2, 0.8},
							Interp:  "LINEAR",
						},
					},
				},
			},
		},
	}
	ir := props.SceneIR()
	if len(ir.MaterialMotionProgram) == 0 {
		t.Fatal("MaterialMotionProgram is empty for scalar uniform")
	}

	tl, targetRefs, propRefs, err := motion.DecodeProgram(ir.MaterialMotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	track := tl.Children[0].Track
	if track.Keys[0].Value.Arity != motion.ArityScalar {
		t.Errorf("key arity = %v, want ArityScalar (%v)", track.Keys[0].Value.Arity, motion.ArityScalar)
	}
	if targetRefs[track.TargetID] != "rough-cube" {
		t.Errorf("targetRef = %q, want rough-cube", targetRefs[track.TargetID])
	}
	if propRefs[track.PropID] != "roughness" {
		t.Errorf("propRef = %q, want roughness", propRefs[track.PropID])
	}

	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 0.5, motion.Policy{}, buf)
	got := buf.Writes()
	if len(got) != 4 {
		t.Fatalf("expected 4 floats (target,prop,arity,v), got %d: %v", len(got), got)
	}
	if got[2] != float64(motion.ArityScalar) {
		t.Errorf("arity = %v, want ArityScalar", got[2])
	}
	if math.Abs(got[3]-0.5) > 1e-9 {
		t.Errorf("roughness@0.5 = %.10f, want 0.5", got[3])
	}
}

// TestMaterialMotionProgram_NoneIsNil verifies a mesh with no MaterialAnims
// leaves MaterialMotionProgram nil and the JSON omits the key.
func TestMaterialMotionProgram_NoneIsNil(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "plain-cube",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
				},
			},
		},
	}
	ir := props.SceneIR()
	if len(ir.MaterialMotionProgram) != 0 {
		t.Errorf("MaterialMotionProgram should be nil/empty, got %d bytes", len(ir.MaterialMotionProgram))
	}
	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if strings.Contains(string(data), "materialMotionProgram") {
		t.Errorf("JSON should not contain 'materialMotionProgram', got: %s", string(data))
	}
}

// TestMaterialMotionProgram_JSONRoundTrip verifies marshal → unmarshal → decode
// survives for the material program.
func TestMaterialMotionProgram_JSONRoundTrip(t *testing.T) {
	ir := emissiveMesh("glow-cube").SceneIR()
	if len(ir.MaterialMotionProgram) == 0 {
		t.Fatal("MaterialMotionProgram empty before marshal")
	}

	data, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	if !strings.Contains(string(data), "materialMotionProgram") {
		t.Errorf("JSON should contain 'materialMotionProgram', got: %s", string(data))
	}

	var ir2 SceneIR
	if err := json.Unmarshal(data, &ir2); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if len(ir2.MaterialMotionProgram) == 0 {
		t.Fatal("MaterialMotionProgram empty after JSON round-trip")
	}

	tl, targetRefs, _, err := motion.DecodeProgram(ir2.MaterialMotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram after JSON round-trip error: %v", err)
	}
	if tl == nil || len(tl.Children) == 0 {
		t.Fatal("decoded timeline empty after JSON round-trip")
	}
	track := tl.Children[0].Track
	if track == nil || track.Target.Kind != motion.TargetMaterial {
		t.Fatal("decoded track missing or not TargetMaterial after JSON round-trip")
	}
	if targetRefs[track.TargetID] != "glow-cube" {
		t.Errorf("targetRef = %q, want glow-cube", targetRefs[track.TargetID])
	}
}

// TestMaterialMotionProgram_IndependentFromSpin verifies a mesh with BOTH Spin
// and MaterialAnims emits BOTH MotionProgram (spin) and MaterialMotionProgram
// independently, and legacy SpinX/Y/Z fields are still present.
func TestMaterialMotionProgram_IndependentFromSpin(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Mesh{
					ID:       "spin-glow",
					Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Spin:     Euler{X: 0, Y: 1.2, Z: 0},
					MaterialAnims: []MaterialUniformAnim{
						{
							Uniform: "emissive",
							Arity:   4,
							Times:   []float64{0, 1},
							Values:  []float64{0, 0, 0, 1, 1, 1, 1, 1},
							Interp:  "LINEAR",
						},
					},
				},
			},
		},
	}
	ir := props.SceneIR()

	if len(ir.MotionProgram) == 0 {
		t.Error("MotionProgram (spin) is empty, want non-empty")
	}
	if len(ir.MaterialMotionProgram) == 0 {
		t.Error("MaterialMotionProgram is empty, want non-empty")
	}

	// The two programs must be distinct payloads (separate routing).
	if string(ir.MotionProgram) == string(ir.MaterialMotionProgram) {
		t.Error("MotionProgram and MaterialMotionProgram are identical, want distinct")
	}

	// Spin program: rotation track, TargetSceneNode.
	spinTL, _, spinProps, err := motion.DecodeProgram(ir.MotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram(MotionProgram) error: %v", err)
	}
	spinTrack := spinTL.Children[0].Track
	if spinTrack.Target.Kind != motion.TargetSceneNode {
		t.Errorf("spin Target.Kind = %v, want TargetSceneNode", spinTrack.Target.Kind)
	}
	if spinProps[spinTrack.PropID] != "rotation" {
		t.Errorf("spin prop = %q, want rotation", spinProps[spinTrack.PropID])
	}

	// Material program: emissive track, TargetMaterial.
	matTL, _, matProps, err := motion.DecodeProgram(ir.MaterialMotionProgram)
	if err != nil {
		t.Fatalf("DecodeProgram(MaterialMotionProgram) error: %v", err)
	}
	matTrack := matTL.Children[0].Track
	if matTrack.Target.Kind != motion.TargetMaterial {
		t.Errorf("material Target.Kind = %v, want TargetMaterial", matTrack.Target.Kind)
	}
	if matProps[matTrack.PropID] != "emissive" {
		t.Errorf("material prop = %q, want emissive", matProps[matTrack.PropID])
	}

	// Legacy SpinX/Y/Z still emitted on the object record.
	if len(ir.Objects) != 1 {
		t.Fatalf("Objects = %d, want 1", len(ir.Objects))
	}
	if ir.Objects[0].SpinY != 1.2 {
		t.Errorf("Objects[0].SpinY = %v, want 1.2", ir.Objects[0].SpinY)
	}
}

// tlChildCount is a nil-safe child counter for test diagnostics.
func tlChildCount(tl *motion.Timeline) int {
	if tl == nil {
		return -1
	}
	return len(tl.Children)
}
