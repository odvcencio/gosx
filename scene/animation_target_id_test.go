package scene

import (
	"testing"

	"m31labs.dev/gosx/motion"
)

// materialProgramTargetRefs decodes a wire motion program and returns its
// interned target refs.
func materialProgramTargetRefs(t *testing.T, blob []byte) []string {
	t.Helper()
	_, targetRefs, _, err := motion.DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram: %v", err)
	}
	return targetRefs
}

// TestLowerAnimationClipResolvesStableTargetIDs guards the targeting substrate
// proven broken by real-browser QA on /demos/orrery: graph AnimationClip
// channels address nodes by index into the AUTHORED node list, which
// interleaves lights ahead of meshes. Lowering must resolve each TargetNode to
// its stable node ID so consumers never match against flattened renderable
// array positions.
func TestLowerAnimationClipResolvesStableTargetIDs(t *testing.T) {
	const heartY = 2.05
	clip := AnimationClip{
		Name:     "procession",
		Duration: 24,
		Channels: []AnimationChannel{
			{TargetNode: 3, Property: "translation", Interpolation: "LINEAR",
				Times: []float64{0, 6}, Values: []float64{0, 0, 0, -4, 0, 0}},
			{TargetNode: 2, Property: "translation", Interpolation: "LINEAR",
				Times: []float64{0, 12}, Values: []float64{0, 0, 0, 1, 0, 0}},
			// Out-of-range and anonymous targets lower without a TargetID but
			// stay on the wire with their authored index for diagnostics.
			{TargetNode: 99, Property: "translation",
				Times: []float64{0, 1}, Values: []float64{0, 0, 0, 1, 0, 0}},
		},
	}
	props := Props{Graph: NewGraph(
		DirectionalLight{ID: "key-light", Intensity: 1},
		PointLight{ID: "heart-light", Position: Vec3(0, heartY, 0)},
		Mesh{ID: "orrery-heart", Geometry: SphereGeometry{Radius: 0.5},
			Material: StandardMaterial{Color: "#2a2340"}, Position: Vec3(0, heartY, 0)},
		Mesh{ID: "orrery-planet", Geometry: SphereGeometry{Radius: 0.2},
			Material: StandardMaterial{Color: "#c98a5a"}, Position: Vec3(2, heartY, 0)},
		clip,
	)}
	ir := props.SceneIR()
	if len(ir.Animations) != 1 {
		t.Fatalf("expected 1 lowered animation, got %d", len(ir.Animations))
	}
	channels := ir.Animations[0].Channels
	if len(channels) != 3 {
		t.Fatalf("expected 3 lowered channels, got %d", len(channels))
	}
	if channels[0].TargetID != "orrery-planet" {
		t.Fatalf("channel 0 TargetID = %q, want %q", channels[0].TargetID, "orrery-planet")
	}
	if channels[1].TargetID != "orrery-heart" {
		t.Fatalf("channel 1 TargetID = %q, want %q", channels[1].TargetID, "orrery-heart")
	}
	if channels[2].TargetID != "" {
		t.Fatalf("out-of-range channel should omit TargetID, got %q", channels[2].TargetID)
	}
	for i, want := range []int{3, 2, 99} {
		if channels[i].TargetNode != want {
			t.Fatalf("channel %d TargetNode = %d, want %d (authored index must be preserved)", i, channels[i].TargetNode, want)
		}
	}
}

// TestLoweredMaterialAnimsShipStableMeshRefs proves MaterialUniformAnim
// lowering produces a decodable MaterialMotionProgram whose target refs are the
// mesh IDs (position-independent), the contract the shared bundle-path player
// relies on.
func TestLoweredMaterialAnimsShipStableMeshRefs(t *testing.T) {
	props := Props{Graph: NewGraph(
		Mesh{ID: "orrery-heart", Geometry: SphereGeometry{Radius: 0.5},
			Material: StandardMaterial{Color: "#2a2340", Emissive: 0.12},
			MaterialAnims: []MaterialUniformAnim{{
				Uniform: "emissive", Arity: 1, Interp: "LINEAR", Loop: true, Duration: 24,
				Times:  []float64{0, 13.2, 24},
				Values: []float64{0.12, 3.4, 0.12},
			}}},
	)}
	ir := props.SceneIR()
	if len(ir.MaterialMotionProgram) == 0 {
		t.Fatalf("expected MaterialMotionProgram to ship for MaterialAnims scenes")
	}
	targetRefs := materialProgramTargetRefs(t, ir.MaterialMotionProgram)
	found := false
	for _, ref := range targetRefs {
		if ref == "orrery-heart" {
			found = true
		}
	}
	if !found {
		t.Fatalf("material program target refs %v missing mesh ID %q", targetRefs, "orrery-heart")
	}
}
