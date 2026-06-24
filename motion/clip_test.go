package motion

import (
	"math"
	"testing"
)

// evalOne runs Eval at time t and returns the single packed write's value
// components, or nil if no write was produced for targetID/propID.
func evalOne(t *testing.T, tl *Timeline, time float64, targetID, propID int) (ValueArity, []float64) {
	t.Helper()
	buf := NewWriteBuf(64)
	Eval(tl, time, Policy{}, buf)
	w := buf.Writes()
	i := 0
	for i < len(w) {
		tid := int(w[i])
		pid := int(w[i+1])
		arity := ValueArity(w[i+2])
		width := arity.Width()
		if tid == targetID && pid == propID {
			out := make([]float64, width)
			copy(out, w[i+3:i+3+width])
			return arity, out
		}
		i += 3 + width
	}
	return 0, nil
}

// TestBuildClipTimelineTranslationLinear builds a translation LINEAR channel and
// samples mid-segment; the result must be the exact linear lerp.
func TestBuildClipTimelineTranslationLinear(t *testing.T) {
	ch := ClipChannel{
		Node:     3,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 10, 20, 30}, // 2 keys * vec3
	}
	tl, dur := BuildClipTimeline([]ClipChannel{ch})
	if dur != 1 {
		t.Fatalf("duration = %v, want 1", dur)
	}

	// TargetID = node index (3); PropID = fixed translation constant (0).
	arity, v := evalOne(t, tl, 0.5, 3, 0)
	if arity != ArityVec3 {
		t.Fatalf("arity = %v, want ArityVec3", arity)
	}
	want := []float64{5, 10, 15}
	for i := range want {
		if math.Abs(v[i]-want[i]) > 1e-12 {
			t.Errorf("translation[%d] = %v, want %v", i, v[i], want[i])
		}
	}
}

// TestBuildClipTimelineRotationQuat builds a rotation channel and verifies the
// resulting track is ArityQuat and slerps between the two unit quaternions.
func TestBuildClipTimelineRotationQuat(t *testing.T) {
	// Identity quat -> 90deg about Y. glTF quats are (x,y,z,w).
	h := math.Sqrt2 / 2
	ch := ClipChannel{
		Node:     0,
		Property: "rotation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 1, 0, h, 0, h}, // 2 keys * quat(4)
	}
	tl, dur := BuildClipTimeline([]ClipChannel{ch})
	if dur != 1 {
		t.Fatalf("duration = %v, want 1", dur)
	}

	// TargetID = node index (0); PropID = fixed rotation constant (1).
	arity, v := evalOne(t, tl, 0.5, 0, 1)
	if arity != ArityQuat {
		t.Fatalf("arity = %v, want ArityQuat", arity)
	}

	// Oracle: slerp of identity -> 45deg about Y is the slerp midpoint (unit quat).
	qa := Quat{0, 0, 0, 1}
	qb := Quat{0, h, 0, h}
	want := Slerp(qa, qb, 0.5)
	got := []float64{v[0], v[1], v[2], v[3]}
	wantArr := []float64{want.X, want.Y, want.Z, want.W}
	for i := range wantArr {
		if math.Abs(got[i]-wantArr[i]) > 1e-9 {
			t.Errorf("rotation[%d] = %v, want %v", i, got[i], wantArr[i])
		}
	}
}

// TestBuildClipTimelineCubicSpline builds a CUBICSPLINE channel with non-trivial
// tangents and asserts the mid-segment result matches the Hermite oracle and
// differs from a plain linear interpolation.
func TestBuildClipTimelineCubicSpline(t *testing.T) {
	// One vec3 channel, 2 keys. CUBICSPLINE layout per key: [inTangent, value, outTangent].
	// Asymmetric tangents so the midpoint differs from a plain linear lerp.
	w := 3
	// key0: in=(0,0,0) value=(0,0,0) out=(20,0,0)
	// key1: in=(2,0,0) value=(10,0,0) out=(0,0,0)
	values := []float64{
		0, 0, 0 /*in0*/, 0, 0, 0 /*v0*/, 20, 0, 0, /*out0*/
		2, 0, 0 /*in1*/, 10, 0, 0 /*v1*/, 0, 0, 0, /*out1*/
	}
	ch := ClipChannel{
		Node:     1,
		Property: "translation",
		Interp:   "CUBICSPLINE",
		Times:    []float64{0, 1},
		Values:   values,
	}
	tl, dur := BuildClipTimeline([]ClipChannel{ch})
	if dur != 1 {
		t.Fatalf("duration = %v, want 1", dur)
	}

	// TargetID = node index (1); PropID = fixed translation constant (0).
	arity, got := evalOne(t, tl, 0.5, 1, 0)
	if arity != ArityVec3 {
		t.Fatalf("arity = %v, want ArityVec3", arity)
	}

	// Hermite oracle: delta = 1, s = 0.5.
	// vK=(0,0,0) bK=out0=(20,0,0) vK1=(10,0,0) aK1=in1=(2,0,0).
	var oracle [4]float64
	CubicHermiteInto(oracle[:w],
		Value{Arity: ArityVec3, F: [4]float64{0, 0, 0}},
		Value{Arity: ArityVec3, F: [4]float64{20, 0, 0}},
		Value{Arity: ArityVec3, F: [4]float64{10, 0, 0}},
		Value{Arity: ArityVec3, F: [4]float64{2, 0, 0}},
		1.0, 0.5)
	for i := 0; i < w; i++ {
		if math.Abs(got[i]-oracle[i]) > 1e-12 {
			t.Errorf("cubicspline[%d] = %v, want %v (Hermite oracle)", i, got[i], oracle[i])
		}
	}

	// Must differ from a plain linear lerp (5,0,0) on the X axis.
	if math.Abs(got[0]-5.0) < 1e-9 {
		t.Errorf("cubicspline X = %v matches linear lerp 5.0; tangents not applied", got[0])
	}
}

// TestBuildClipTimelineMalformedSkipped asserts a channel whose Values slice is
// too short for its keyframe count is silently dropped (no panic, no track).
func TestBuildClipTimelineMalformedSkipped(t *testing.T) {
	good := ClipChannel{
		Node:     0,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 1, 1, 1},
	}
	// Malformed: 2 keys * vec3 needs 6 values, only 4 provided.
	bad := ClipChannel{
		Node:     5,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 1},
	}
	// Malformed cubicspline: 2 keys * 3 * vec3 needs 18, only 9 provided.
	badCubic := ClipChannel{
		Node:     7,
		Property: "rotation",
		Interp:   "CUBICSPLINE",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 1, 0, 0, 0, 1, 0},
	}

	tl, _ := BuildClipTimeline([]ClipChannel{good, bad, badCubic})

	// Only the good channel must survive: exactly one Positioned child.
	if len(tl.Children) != 1 {
		t.Fatalf("timeline has %d children, want 1 (malformed skipped)", len(tl.Children))
	}
	if tl.Children[0].Track == nil || tl.Children[0].Track.Target.Ref != "0" {
		t.Fatalf("surviving track ref = %v, want node 0", tl.Children[0].Track)
	}
}

// TestBuildClipTimelineUnknownInterpDefaultsLinear asserts an unrecognized interp
// string falls back to InterpLinear.
func TestBuildClipTimelineUnknownInterpDefaultsLinear(t *testing.T) {
	ch := ClipChannel{
		Node:     0,
		Property: "scale",
		Interp:   "WAT",
		Times:    []float64{0, 1},
		Values:   []float64{1, 1, 1, 2, 2, 2},
	}
	tl, _ := BuildClipTimeline([]ClipChannel{ch})
	if got := tl.Children[0].Track.Interp; got != InterpLinear {
		t.Fatalf("unknown interp -> %v, want InterpLinear", got)
	}
	// TargetID = node index (0); PropID = fixed scale constant (2).
	arity, v := evalOne(t, tl, 0.5, 0, 2)
	if arity != ArityVec3 {
		t.Fatalf("arity = %v, want ArityVec3", arity)
	}
	want := []float64{1.5, 1.5, 1.5}
	for i := range want {
		if math.Abs(v[i]-want[i]) > 1e-12 {
			t.Errorf("scale[%d] = %v, want %v", i, v[i], want[i])
		}
	}
}

// TestBuildClipTimelineDurationFromKeys asserts duration falls back to the last
// keyframe time when no authored duration dominates.
func TestBuildClipTimelineDurationFromKeys(t *testing.T) {
	ch := ClipChannel{
		Node:     0,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    []float64{0, 2.5},
		Values:   []float64{0, 0, 0, 1, 0, 0},
	}
	_, dur := BuildClipTimeline([]ClipChannel{ch})
	if math.Abs(dur-2.5) > 1e-12 {
		t.Fatalf("duration = %v, want 2.5 (last keyframe)", dur)
	}
}

// TestBuildClipTimelineDirectIDs asserts that BuildClipTimeline assigns
// TargetID = channel.Node and PropID = fixed per-property constant, regardless
// of channel order or prior interning. This is the regression guard for the
// cross-clip consistency fix.
func TestBuildClipTimelineDirectIDs(t *testing.T) {
	// Node 7, rotation → TargetID=7, PropID=1 (propIDRotation).
	ch := ClipChannel{
		Node:     7,
		Property: "rotation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 1, 0, 0, 0, 1}, // identity quat at both keys
	}
	tl, _ := BuildClipTimeline([]ClipChannel{ch})
	if len(tl.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tl.Children))
	}
	tr := tl.Children[0].Track
	if tr.TargetID != 7 {
		t.Errorf("TargetID = %d, want 7 (node index)", tr.TargetID)
	}
	if tr.PropID != propIDRotation {
		t.Errorf("PropID = %d, want %d (propIDRotation)", tr.PropID, propIDRotation)
	}

	// translation → PropID=0
	chTrans := ClipChannel{
		Node:     3,
		Property: "translation",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{0, 0, 0, 1, 0, 0},
	}
	tl2, _ := BuildClipTimeline([]ClipChannel{chTrans})
	tr2 := tl2.Children[0].Track
	if tr2.TargetID != 3 {
		t.Errorf("TargetID = %d, want 3", tr2.TargetID)
	}
	if tr2.PropID != propIDTranslation {
		t.Errorf("PropID = %d, want %d (propIDTranslation)", tr2.PropID, propIDTranslation)
	}

	// scale → PropID=2
	chScale := ClipChannel{
		Node:     5,
		Property: "scale",
		Interp:   "LINEAR",
		Times:    []float64{0, 1},
		Values:   []float64{1, 1, 1, 2, 2, 2},
	}
	tl3, _ := BuildClipTimeline([]ClipChannel{chScale})
	tr3 := tl3.Children[0].Track
	if tr3.TargetID != 5 {
		t.Errorf("TargetID = %d, want 5", tr3.TargetID)
	}
	if tr3.PropID != propIDScale {
		t.Errorf("PropID = %d, want %d (propIDScale)", tr3.PropID, propIDScale)
	}
}
