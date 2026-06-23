package motion

import (
	"math"
	"testing"
)

// --- Task 1.6d: reduced-motion policy tests ---

// TestReducedMotionKeyframeCollapse: with ReducedMotion=true, a keyframe track
// at t=0.5 (midpoint between 0→10) must emit the LAST key's value (10), not 5.
func TestReducedMotionKeyframeCollapse(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 1,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: scalar(0)},
						{T: 1, Value: scalar(10)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tl, 0.5, Policy{ReducedMotion: true}, out)
	got := out.Writes()
	// expect: [targetID=1, propID=0, arity=ArityScalar(0), 10]
	if len(got) < 4 {
		t.Fatalf("expected 4 floats, got %v", got)
	}
	if got[3] != 10.0 {
		t.Errorf("keyframe collapse: got %v, want 10.0 (last key)", got[3])
	}
}

// TestReducedMotionDriftCollapse: with ReducedMotion=true, GenDrift emits Base (no oscillation).
// Base=[1,2,3], Drift[1]=0.5 at t=pi/2 normally yields y=2.5; reduced should yield y=2.
func TestReducedMotionDriftCollapse(t *testing.T) {
	const eps = 1e-9
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 8,
					PropID:   1,
					Gen: &Generator{
						Kind:       GenDrift,
						Base:       Value{Arity: ArityVec3, F: []float64{1, 2, 3}},
						Drift:      [3]float64{0, 0.5, 0},
						DriftSpeed: [3]float64{0, 1, 0},
						DriftPhase: [3]float64{0, 0, 0},
					},
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tl, math.Pi/2, Policy{ReducedMotion: true}, out)
	got := out.Writes()
	// packed: [targetID=8, propID=1, arity=ArityVec3(2), x=1, y=2, z=3]
	want := []float64{8, 1, float64(ArityVec3), 1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if math.Abs(got[i]-v) > eps {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
	}
}

// TestReducedMotionSpinCollapse: with ReducedMotion=true, GenSpin emits identity quat {0,0,0,1}.
func TestReducedMotionSpinCollapse(t *testing.T) {
	const eps = 1e-9
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 7,
					PropID:   3,
					Gen: &Generator{
						Kind: GenSpin,
						Spin: [3]float64{0, 1.2, 0},
					},
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tl, 1.0, Policy{ReducedMotion: true}, out)
	got := out.Writes()
	// packed: [targetID=7, propID=3, arity=ArityQuat(4), X=0, Y=0, Z=0, W=1]
	want := []float64{7, 3, float64(ArityQuat), 0, 0, 0, 1}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if math.Abs(got[i]-v) > eps {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
	}
}

// TestReducedMotionSpringCollapse: with ReducedMotion=true, GenSpring emits Base.F[1] (the "to").
// Base=[0,1] at t=0 normally yields 0; reduced should yield 1 (settled target).
func TestReducedMotionSpringCollapse(t *testing.T) {
	sp := Spring{Mass: 1, Stiffness: 100, Damping: 10}
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 9,
					PropID:   0,
					Gen: &Generator{
						Kind:   GenSpring,
						Base:   Value{Arity: ArityVec2, F: []float64{0, 1}},
						Spring: sp,
					},
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tl, 0, Policy{ReducedMotion: true}, out)
	got := out.Writes()
	// packed: [targetID=9, propID=0, arity=ArityScalar(0), 1.0]
	if len(got) < 4 {
		t.Fatalf("expected 4 floats, got %v", got)
	}
	if got[3] != 1.0 {
		t.Errorf("spring collapse: got %v, want 1.0 (to value)", got[3])
	}
}

// TestReducedMotionRegression: with ReducedMotion=false the same tracks still animate.
// Keyframe at t=0.5 → 5 (midpoint); Drift at t=pi/2 → y=2.5; Spin at t=1 → rotated.
func TestReducedMotionRegression(t *testing.T) {
	const eps = 1e-9

	// Keyframe regression: t=0.5 → 5.0
	tlKF := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 1,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: scalar(0)},
						{T: 1, Value: scalar(10)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tlKF, 0.5, Policy{ReducedMotion: false}, out)
	got := out.Writes()
	if len(got) < 4 || got[3] != 5.0 {
		t.Errorf("keyframe regression: got %v, want 5.0", got)
	}

	// Drift regression: t=pi/2 → y=2.5
	tlDrift := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 8,
					PropID:   1,
					Gen: &Generator{
						Kind:       GenDrift,
						Base:       Value{Arity: ArityVec3, F: []float64{1, 2, 3}},
						Drift:      [3]float64{0, 0.5, 0},
						DriftSpeed: [3]float64{0, 1, 0},
						DriftPhase: [3]float64{0, 0, 0},
					},
				},
			},
		},
	}
	out.Reset()
	Eval(tlDrift, math.Pi/2, Policy{ReducedMotion: false}, out)
	gotDrift := out.Writes()
	if len(gotDrift) < 6 || math.Abs(gotDrift[4]-2.5) > eps {
		t.Errorf("drift regression: got %v, want y=2.5", gotDrift)
	}

	// Spin regression: t=1 → rotated quat (Y component = sin(0.6) != 0)
	tlSpin := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 7,
					PropID:   3,
					Gen: &Generator{
						Kind: GenSpin,
						Spin: [3]float64{0, 1.2, 0},
					},
				},
			},
		},
	}
	out.Reset()
	Eval(tlSpin, 1.0, Policy{ReducedMotion: false}, out)
	gotSpin := out.Writes()
	// Y = sin(0.6), should NOT be 0
	if len(gotSpin) < 7 || math.Abs(gotSpin[4]) < 0.1 {
		t.Errorf("spin regression: got %v, want Y=sin(0.6)≈0.565", gotSpin)
	}
}

// TestReducedMotionZeroAlloc: the reduced-motion path must also be zero-alloc.
func TestReducedMotionZeroAlloc(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 1,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: scalar(0)},
						{T: 1, Value: scalar(10)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(16)
	allocs := testing.AllocsPerRun(1000, func() {
		out.Reset()
		Eval(tl, 0.5, Policy{ReducedMotion: true}, out)
	})
	if allocs != 0 {
		t.Errorf("expected 0 allocs per run (reduced-motion path), got %v", allocs)
	}
}
