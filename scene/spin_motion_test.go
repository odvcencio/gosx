package scene

import (
	"math"
	"testing"

	"m31labs.dev/gosx/motion"
)

// TestSpinMotionTrackHelper verifies that spinMotionTrack maps Euler axes
// to Generator.Spin correctly, and that the produced track is a GenSpin
// targeting "rotation" on the expected node.
func TestSpinMotionTrackHelper(t *testing.T) {
	spin := Euler{X: 0, Y: 1.2, Z: 0}
	track := spinMotionTrack(spin, "cube-1")

	if track.Target.Kind != motion.TargetSceneNode {
		t.Errorf("Target.Kind = %v, want TargetSceneNode", track.Target.Kind)
	}
	if track.Target.Ref != "cube-1" {
		t.Errorf("Target.Ref = %q, want %q", track.Target.Ref, "cube-1")
	}
	if track.Prop != "rotation" {
		t.Errorf("Prop = %q, want %q", track.Prop, "rotation")
	}
	if track.Gen == nil {
		t.Fatal("Gen is nil, want non-nil Generator")
	}
	if track.Gen.Kind != motion.GenSpin {
		t.Errorf("Gen.Kind = %v, want GenSpin", track.Gen.Kind)
	}
	want := [3]float64{0, 1.2, 0}
	if track.Gen.Spin != want {
		t.Errorf("Gen.Spin = %v, want %v", track.Gen.Spin, want)
	}
}

// TestSpinMotionTrackEval verifies that a GenSpin track produced by
// spinMotionTrack, when fed to motion.Eval at t=1.0, emits the expected
// quaternion QuatFromEuler(0, 1.2, 0) ≈ {0, sin(0.6), 0, cos(0.6)}.
func TestSpinMotionTrackEval(t *testing.T) {
	spin := Euler{X: 0, Y: 1.2, Z: 0}
	track := spinMotionTrack(spin, "cube-1")

	tl := &motion.Timeline{
		Children: []motion.Positioned{
			{
				At:    motion.Position{Kind: motion.PosAbs, Val: 0},
				Track: &track,
			},
		},
	}

	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 1.0, motion.Policy{}, buf)

	got := buf.Writes()
	// Each write is packed as: [targetID, propID, arity, x, y, z, w]
	// arity for ArityQuat == 4.
	if len(got) != 7 {
		t.Fatalf("expected 7 floats in WriteBuf, got %d: %v", len(got), got)
	}

	// got[0] = targetID (0 — not interned), got[1] = propID (0), got[2] = arity
	if got[2] != float64(motion.ArityQuat) {
		t.Errorf("arity = %v, want ArityQuat (%v)", got[2], motion.ArityQuat)
	}

	q := motion.QuatFromEuler(0, 1.2*1.0, 0)
	wantX, wantY, wantZ, wantW := q.X, q.Y, q.Z, q.W

	const eps = 1e-9
	if math.Abs(got[3]-wantX) > eps {
		t.Errorf("quat.X = %.10f, want %.10f", got[3], wantX)
	}
	if math.Abs(got[4]-wantY) > eps {
		t.Errorf("quat.Y = %.10f, want %.10f", got[4], wantY)
	}
	if math.Abs(got[5]-wantZ) > eps {
		t.Errorf("quat.Z = %.10f, want %.10f", got[5], wantZ)
	}
	if math.Abs(got[6]-wantW) > eps {
		t.Errorf("quat.W = %.10f, want %.10f", got[6], wantW)
	}
}

// TestSpinMotionLowering_MeshSpinTracks verifies that lowering a Mesh with
// Spin set produces a SpinTrack in the SceneIR, and that the legacy SpinX/Y/Z
// fields are STILL emitted unchanged (non-breaking regression).
func TestSpinMotionLowering_MeshSpinTracks(t *testing.T) {
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

	// --- Non-breaking regression: legacy SpinX/Y/Z still emitted ---
	if len(ir.Objects) != 1 {
		t.Fatalf("expected 1 object, got %d", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.SpinX != 0 || obj.SpinY != 1.2 || obj.SpinZ != 0 {
		t.Errorf("legacy spin mismatch: got (%v,%v,%v), want (0, 1.2, 0)", obj.SpinX, obj.SpinY, obj.SpinZ)
	}

	// --- New facade: SpinTracks populated ---
	if len(ir.SpinTracks) != 1 {
		t.Fatalf("expected 1 SpinTrack, got %d", len(ir.SpinTracks))
	}
	tr := ir.SpinTracks[0]
	if tr.Target.Ref != "spinning-cube" {
		t.Errorf("SpinTrack.Target.Ref = %q, want %q", tr.Target.Ref, "spinning-cube")
	}
	if tr.Gen == nil || tr.Gen.Kind != motion.GenSpin {
		t.Errorf("SpinTrack.Gen.Kind = %v, want GenSpin", tr.Gen.Kind)
	}
	want := [3]float64{0, 1.2, 0}
	if tr.Gen.Spin != want {
		t.Errorf("SpinTrack.Gen.Spin = %v, want %v", tr.Gen.Spin, want)
	}
}

// TestSpinMotionLowering_PointsSpinTracks verifies the same for Points nodes.
func TestSpinMotionLowering_PointsSpinTracks(t *testing.T) {
	props := Props{
		Graph: Graph{
			Nodes: []Node{
				Points{
					ID:   "stars",
					Spin: Rotate(0.1, 0.2, 0.3),
				},
			},
		},
	}
	ir := props.SceneIR()

	if len(ir.Points) != 1 {
		t.Fatalf("expected 1 points entry, got %d", len(ir.Points))
	}
	pts := ir.Points[0]
	// Non-breaking regression: legacy SpinX/Y/Z still correct.
	if pts.SpinX != 0.1 || pts.SpinY != 0.2 || pts.SpinZ != 0.3 {
		t.Errorf("legacy spin mismatch: got (%v,%v,%v), want (0.1,0.2,0.3)", pts.SpinX, pts.SpinY, pts.SpinZ)
	}

	if len(ir.SpinTracks) != 1 {
		t.Fatalf("expected 1 SpinTrack, got %d", len(ir.SpinTracks))
	}
	tr := ir.SpinTracks[0]
	if tr.Target.Ref != "stars" {
		t.Errorf("SpinTrack.Target.Ref = %q, want %q", tr.Target.Ref, "stars")
	}
	wantSpin := [3]float64{0.1, 0.2, 0.3}
	if tr.Gen.Spin != wantSpin {
		t.Errorf("SpinTrack.Gen.Spin = %v, want %v", tr.Gen.Spin, wantSpin)
	}
}

// TestSpinMotionLowering_NoSpinNoTrack verifies that a Mesh with zero Spin
// does NOT produce any SpinTrack (no false emissions).
func TestSpinMotionLowering_NoSpinNoTrack(t *testing.T) {
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
	if len(ir.SpinTracks) != 0 {
		t.Errorf("expected 0 SpinTracks for zero-spin mesh, got %d", len(ir.SpinTracks))
	}
}

// TestSpinMotionTimeline verifies SpinMotionTimeline wraps tracks correctly.
func TestSpinMotionTimeline(t *testing.T) {
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
	tl := ir.SpinMotionTimeline()

	if tl == nil {
		t.Fatal("SpinMotionTimeline returned nil, want non-nil")
	}
	if len(tl.Children) != 1 {
		t.Fatalf("timeline has %d children, want 1", len(tl.Children))
	}
	if !tl.Autoplay {
		t.Error("timeline Autoplay = false, want true")
	}
	if tl.Loop != -1 {
		t.Errorf("timeline Loop = %d, want -1 (infinite)", tl.Loop)
	}

	// Eval the timeline at t=2.0 and verify the quaternion.
	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 2.0, motion.Policy{}, buf)
	got := buf.Writes()
	if len(got) != 7 {
		t.Fatalf("expected 7 floats, got %d: %v", len(got), got)
	}
	q := motion.QuatFromEuler(0, 0.5*2.0, 0)
	const eps = 1e-9
	if math.Abs(got[4]-q.Y) > eps {
		t.Errorf("quat.Y = %.10f, want %.10f", got[4], q.Y)
	}
	if math.Abs(got[6]-q.W) > eps {
		t.Errorf("quat.W = %.10f, want %.10f", got[6], q.W)
	}
}
