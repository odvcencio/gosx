package scene

import (
	"math"
	"testing"

	"m31labs.dev/gosx/motion"
)

const animClipEps = 1e-9

// TestAnimationClipMotionTimeline_Translation verifies a clip with a single
// translation channel converts correctly and evaluates to the interpolated
// midpoint at t=0.5.
func TestAnimationClipMotionTimeline_Translation(t *testing.T) {
	clip := AnimationClip{
		Name:     "slide",
		Duration: 1.0,
		Channels: []AnimationChannel{
			{
				TargetNode:    3,
				Property:      "translation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0, 10, 20, 30},
			},
		},
	}

	tl := AnimationClipMotionTimeline(clip)

	if tl == nil {
		t.Fatal("AnimationClipMotionTimeline returned nil")
	}
	if len(tl.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tl.Children))
	}

	child := tl.Children[0]
	tr := child.Track
	if tr == nil {
		t.Fatal("Track is nil")
	}

	if tr.Target.Ref != "3" {
		t.Errorf("Target.Ref = %q, want %q", tr.Target.Ref, "3")
	}
	if tr.Prop != "translation" {
		t.Errorf("Prop = %q, want %q", tr.Prop, "translation")
	}
	if len(tr.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(tr.Keys))
	}
	if tr.Interp != motion.InterpLinear {
		t.Errorf("Interp = %v, want InterpLinear", tr.Interp)
	}

	// Eval at t=0.5 — midpoint between [0,0,0] and [10,20,30] should be [5,10,15].
	// PrepareTracks was called internally so TargetID=0, PropID=0.
	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 0.5, motion.Policy{}, buf)

	got := buf.Writes()
	// packed layout: [targetID, propID, arity, x, y, z] — width 3 for vec3
	// total = 3 + 3 = 6
	if len(got) != 6 {
		t.Fatalf("expected 6 floats in WriteBuf, got %d: %v", len(got), got)
	}
	if got[0] != 0 {
		t.Errorf("targetID = %v, want 0", got[0])
	}
	if got[1] != 0 {
		t.Errorf("propID = %v, want 0", got[1])
	}
	if got[2] != float64(motion.ArityVec3) {
		t.Errorf("arity = %v, want ArityVec3 (%v)", got[2], motion.ArityVec3)
	}

	wantXYZ := [3]float64{5, 10, 15}
	for i, w := range wantXYZ {
		if math.Abs(got[3+i]-w) > animClipEps {
			t.Errorf("component[%d] = %v, want %v", i, got[3+i], w)
		}
	}
}

// TestAnimationClipMotionTimeline_Rotation verifies that a rotation channel
// produces ArityQuat keys and that Eval at t=0.5 Slerps between the two
// endpoint quaternions.
func TestAnimationClipMotionTimeline_Rotation(t *testing.T) {
	// Two unit quaternions: identity {0,0,0,1} and 180° Y-rotation {0,1,0,0}.
	qa := motion.Quat{X: 0, Y: 0, Z: 0, W: 1}
	qb := motion.Quat{X: 0, Y: 1, Z: 0, W: 0}

	clip := AnimationClip{
		Name:     "spin",
		Duration: 1.0,
		Channels: []AnimationChannel{
			{
				TargetNode:    0,
				Property:      "rotation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1},
				Values: []float64{
					qa.X, qa.Y, qa.Z, qa.W,
					qb.X, qb.Y, qb.Z, qb.W,
				},
			},
		},
	}

	tl := AnimationClipMotionTimeline(clip)

	if len(tl.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tl.Children))
	}
	tr := tl.Children[0].Track
	if tr == nil {
		t.Fatal("Track is nil")
	}
	if len(tr.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(tr.Keys))
	}
	if tr.Keys[0].Value.Arity != motion.ArityQuat {
		t.Errorf("key[0].Arity = %v, want ArityQuat", tr.Keys[0].Value.Arity)
	}

	// Eval at t=0.5.
	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 0.5, motion.Policy{}, buf)

	got := buf.Writes()
	// packed: [targetID, propID, arity, x, y, z, w] = 7 floats
	if len(got) != 7 {
		t.Fatalf("expected 7 floats, got %d: %v", len(got), got)
	}
	if got[2] != float64(motion.ArityQuat) {
		t.Errorf("arity = %v, want ArityQuat (%v)", got[2], motion.ArityQuat)
	}

	// Expected: Slerp(identity, {0,1,0,0}, 0.5) — 90° Y rotation = {0, sin(π/4), 0, cos(π/4)}.
	want := motion.Slerp(qa, qb, 0.5)
	wantComponents := [4]float64{want.X, want.Y, want.Z, want.W}
	for i, w := range wantComponents {
		if math.Abs(got[3+i]-w) > animClipEps {
			t.Errorf("quat[%d] = %.10f, want %.10f", i, got[3+i], w)
		}
	}
}

// TestAnimationClipMotionTimeline_StepInterp verifies that STEP interpolation
// holds the lower key value and does not interpolate.
func TestAnimationClipMotionTimeline_StepInterp(t *testing.T) {
	clip := AnimationClip{
		Name:     "step",
		Duration: 2.0,
		Channels: []AnimationChannel{
			{
				TargetNode:    1,
				Property:      "translation",
				Interpolation: "STEP",
				Times:         []float64{0, 1, 2},
				Values: []float64{
					1, 2, 3,
					4, 5, 6,
					7, 8, 9,
				},
			},
		},
	}

	tl := AnimationClipMotionTimeline(clip)

	tr := tl.Children[0].Track
	if tr.Interp != motion.InterpStep {
		t.Errorf("Interp = %v, want InterpStep", tr.Interp)
	}

	// At t=0.9 (between keys 0 and 1), STEP should hold key[0] = {1,2,3}.
	buf := motion.NewWriteBuf(32)
	motion.Eval(tl, 0.9, motion.Policy{}, buf)
	got := buf.Writes()
	if len(got) != 6 {
		t.Fatalf("expected 6 floats, got %d", len(got))
	}
	wantXYZ := [3]float64{1, 2, 3}
	for i, w := range wantXYZ {
		if math.Abs(got[3+i]-w) > animClipEps {
			t.Errorf("STEP component[%d] = %v, want %v", i, got[3+i], w)
		}
	}

	// At t=1.5 (between keys 1 and 2), STEP should hold key[1] = {4,5,6}.
	buf.Reset()
	motion.Eval(tl, 1.5, motion.Policy{}, buf)
	got = buf.Writes()
	wantXYZ = [3]float64{4, 5, 6}
	for i, w := range wantXYZ {
		if math.Abs(got[3+i]-w) > animClipEps {
			t.Errorf("STEP component[%d] = %v, want %v (at t=1.5)", i, got[3+i], w)
		}
	}
}

// TestAnimationClipMotionTimeline_WireRoundTrip verifies that a timeline
// derived from an AnimationClip survives EncodeProgram → DecodeProgram.
func TestAnimationClipMotionTimeline_WireRoundTrip(t *testing.T) {
	clip := AnimationClip{
		Name:     "bounce",
		Duration: 2.0,
		Channels: []AnimationChannel{
			{
				TargetNode:    5,
				Property:      "translation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1, 2},
				Values:        []float64{0, 0, 0, 0, 5, 0, 0, 0, 0},
			},
			{
				TargetNode:    5,
				Property:      "scale",
				Interpolation: "STEP",
				Times:         []float64{0, 1},
				Values:        []float64{1, 1, 1, 2, 2, 2},
			},
		},
	}

	tl := AnimationClipMotionTimeline(clip)

	// Build ref slices from the timeline's interned IDs.
	// We need fresh interners to get the same ref tables.
	targets := motion.NewInterner()
	props := motion.NewInterner()
	motion.PrepareTracks(tl, targets, props)

	encoded := motion.EncodeProgram(tl, targets.Refs(), props.Refs())
	if len(encoded) == 0 {
		t.Fatal("EncodeProgram returned empty slice")
	}

	decoded, decodedTargetRefs, decodedPropRefs, err := motion.DecodeProgram(encoded)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}

	// Verify timeline ID.
	if decoded.ID != clip.Name {
		t.Errorf("decoded timeline ID = %q, want %q", decoded.ID, clip.Name)
	}

	// Verify child count.
	if len(decoded.Children) != len(tl.Children) {
		t.Fatalf("decoded children count = %d, want %d", len(decoded.Children), len(tl.Children))
	}

	// Verify target and prop refs survived.
	if len(decodedTargetRefs) != targets.Len() {
		t.Errorf("decoded targetRefs len = %d, want %d", len(decodedTargetRefs), targets.Len())
	}
	if len(decodedPropRefs) != props.Len() {
		t.Errorf("decoded propRefs len = %d, want %d", len(decodedPropRefs), props.Len())
	}

	// Verify each track survived.
	for i, orig := range tl.Children {
		dec := decoded.Children[i]
		if dec.Track == nil {
			t.Fatalf("decoded child[%d].Track is nil", i)
		}
		if dec.Track.Target.Ref != orig.Track.Target.Ref {
			t.Errorf("child[%d] Target.Ref = %q, want %q", i, dec.Track.Target.Ref, orig.Track.Target.Ref)
		}
		if dec.Track.Prop != orig.Track.Prop {
			t.Errorf("child[%d] Prop = %q, want %q", i, dec.Track.Prop, orig.Track.Prop)
		}
		if dec.Track.Interp != orig.Track.Interp {
			t.Errorf("child[%d] Interp = %v, want %v", i, dec.Track.Interp, orig.Track.Interp)
		}
		if len(dec.Track.Keys) != len(orig.Track.Keys) {
			t.Errorf("child[%d] key count = %d, want %d", i, len(dec.Track.Keys), len(orig.Track.Keys))
			continue
		}
		for j, origKey := range orig.Track.Keys {
			decKey := dec.Track.Keys[j]
			if math.Abs(decKey.T-origKey.T) > animClipEps {
				t.Errorf("child[%d] key[%d].T = %v, want %v", i, j, decKey.T, origKey.T)
			}
			if decKey.Value.Arity != origKey.Value.Arity {
				t.Errorf("child[%d] key[%d].Arity = %v, want %v", i, j, decKey.Value.Arity, origKey.Value.Arity)
			}
			w := origKey.Value.Arity.Width()
			for k := 0; k < w; k++ {
				if math.Abs(decKey.Value.F[k]-origKey.Value.F[k]) > animClipEps {
					t.Errorf("child[%d] key[%d].F[%d] = %v, want %v", i, j, k, decKey.Value.F[k], origKey.Value.F[k])
				}
			}
		}
	}
}

// TestAnimationClipMotionTimeline_MalformedChannel verifies that channels with
// a Values slice too short for the declared keyframe count are silently skipped,
// and no panic occurs.
func TestAnimationClipMotionTimeline_MalformedChannel(t *testing.T) {
	clip := AnimationClip{
		Name:     "malformed",
		Duration: 1.0,
		Channels: []AnimationChannel{
			{
				// Good channel.
				TargetNode:    0,
				Property:      "translation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0, 1, 1, 1},
			},
			{
				// Bad channel: Times has 2 entries but Values has only 3 floats (need 2*3=6).
				TargetNode:    1,
				Property:      "translation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0}, // too short — only 1 keyframe worth
			},
			{
				// Bad rotation channel: Times has 2 entries but Values has only 4 floats (need 2*4=8).
				TargetNode:    2,
				Property:      "rotation",
				Interpolation: "LINEAR",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0, 1}, // too short
			},
		},
	}

	// Must not panic.
	tl := AnimationClipMotionTimeline(clip)

	// Only the good channel should survive.
	if len(tl.Children) != 1 {
		t.Fatalf("expected 1 valid child (malformed channels skipped), got %d", len(tl.Children))
	}
	if tl.Children[0].Track.Target.Ref != "0" {
		t.Errorf("surviving track Target.Ref = %q, want %q", tl.Children[0].Track.Target.Ref, "0")
	}
}

// TestAnimationClipMotionTimeline_CubicSplineDegrades verifies that
// "CUBICSPLINE" interpolation is treated as LINEAR (degrades gracefully).
func TestAnimationClipMotionTimeline_CubicSplineDegrades(t *testing.T) {
	clip := AnimationClip{
		Name:     "cubic",
		Duration: 1.0,
		Channels: []AnimationChannel{
			{
				TargetNode:    0,
				Property:      "translation",
				Interpolation: "CUBICSPLINE",
				Times:         []float64{0, 1},
				Values:        []float64{0, 0, 0, 10, 0, 0},
			},
		},
	}

	tl := AnimationClipMotionTimeline(clip)

	if len(tl.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tl.Children))
	}
	tr := tl.Children[0].Track
	if tr.Interp != motion.InterpLinear {
		t.Errorf("CUBICSPLINE degraded to %v, want InterpLinear", tr.Interp)
	}
}
