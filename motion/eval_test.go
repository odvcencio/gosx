package motion

import (
	"math"
	"testing"
)

// helpers to build test values

func vec3(x, y, z float64) Value {
	return Vec3V(x, y, z)
}

func scalar(v float64) Value {
	return ScalarV(v)
}

// TestEvalLinearMidpoint: single linear track, sample at midpoint.
func TestEvalLinearMidpoint(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 5,
					PropID:   2,
					Keys: []Key{
						{T: 0, Value: vec3(0, 0, 0)},
						{T: 1, Value: vec3(10, 0, 0)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	// expected: [targetID=5, propID=2, arity=ArityVec3(2), 5, 0, 0]
	want := []float64{5, 2, float64(ArityVec3), 5, 0, 0}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
	}
}

// TestEvalClampStart: sample before first key clamps to first key.
func TestEvalClampStart(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 5,
					PropID:   2,
					Keys: []Key{
						{T: 0, Value: vec3(0, 0, 0)},
						{T: 1, Value: vec3(10, 0, 0)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, -1, Policy{}, out)
	got := out.Writes()
	want := []float64{5, 2, float64(ArityVec3), 0, 0, 0}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("index %d: got %v, want %v", i, got[i], v)
		}
	}
}

// TestEvalClampEnd: sample past last key clamps to last key.
func TestEvalClampEnd(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 5,
					PropID:   2,
					Keys: []Key{
						{T: 0, Value: vec3(0, 0, 0)},
						{T: 1, Value: vec3(10, 0, 0)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 2, Policy{}, out)
	got := out.Writes()
	want := []float64{5, 2, float64(ArityVec3), 10, 0, 0}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("index %d: got %v, want %v", i, got[i], v)
		}
	}
}

// TestEvalStep: step interpolation holds previous key.
func TestEvalStep(t *testing.T) {
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
					Interp: InterpStep,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	// step at t=0.5 holds key[0].Value = 0
	want := []float64{1, 0, float64(ArityScalar), 0}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("index %d: got %v, want %v", i, got[i], v)
		}
	}
}

// TestEvalPosAbsOffset: track with PosAbs offset=1.0; localT = t - 1.0.
func TestEvalPosAbsOffset(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 1.0},
				Track: &Track{
					TargetID: 3,
					PropID:   1,
					Keys: []Key{
						{T: 0, Value: vec3(0, 0, 0)},
						{T: 1, Value: vec3(10, 0, 0)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	// t=1.5 → localT=0.5 → midpoint → (5,0,0)
	Eval(tl, 1.5, Policy{}, out)
	got := out.Writes()
	want := []float64{3, 1, float64(ArityVec3), 5, 0, 0}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("index %d: got %v, want %v", i, got[i], v)
		}
	}
}

// --- Generator tests (Task 1.6c) ---

// TestEvalGenSpinQuat: GenSpin track emits ArityQuat for t=1.0.
// Spin=[0,1.2,0] → QuatFromEuler(0, 1.2*1.0, 0) = {0, sin(0.6), 0, cos(0.6)}.
func TestEvalGenSpinQuat(t *testing.T) {
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
	out := NewWriteBuf(64)
	Eval(tl, 1.0, Policy{}, out)
	got := out.Writes()
	// packed: [targetID=7, propID=3, arity=ArityQuat(4), X, Y, Z, W]
	want := []float64{7, 3, float64(ArityQuat), 0, math.Sin(0.6), 0, math.Cos(0.6)}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if math.Abs(got[i]-v) > eps {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
	}
}

// TestEvalGenDriftVec3: GenDrift track emits ArityVec3 at t=pi/2.
// Drift=[0,0.5,0], DriftSpeed=[0,1,0] → y=2+0.5*sin(pi/2)=2.5; x=1, z=3.
func TestEvalGenDriftVec3(t *testing.T) {
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
						Base:       Vec3V(1, 2, 3),
						Drift:      [3]float64{0, 0.5, 0},
						DriftSpeed: [3]float64{0, 1, 0},
						DriftPhase: [3]float64{0, 0, 0},
					},
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, math.Pi/2, Policy{}, out)
	got := out.Writes()
	// packed: [targetID=8, propID=1, arity=ArityVec3(2), x, y, z]
	want := []float64{8, 1, float64(ArityVec3), 1, 2.5, 3}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if math.Abs(got[i]-v) > eps {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
	}
}

// TestEvalGenSpringScalar: GenSpring track emits ArityScalar.
// Base=[0,1] (from=0, to=1). At t=0 → 0; at t=2.0 → settled within 0.05 of 1.0.
func TestEvalGenSpringScalar(t *testing.T) {
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
						Base:   Vec2V(0, 1),
						Spring: sp,
					},
				},
			},
		},
	}
	out := NewWriteBuf(64)

	// t=0 → from value exactly
	Eval(tl, 0, Policy{}, out)
	got0 := out.Writes()
	if len(got0) < 4 {
		t.Fatalf("t=0: expected 4 floats, got %v", got0)
	}
	if got0[3] != 0 {
		t.Errorf("t=0: got %v, want 0", got0[3])
	}

	// t=2.0 → settled near 1.0
	out.Reset()
	Eval(tl, 2.0, Policy{}, out)
	got2 := out.Writes()
	if len(got2) < 4 {
		t.Fatalf("t=2: expected 4 floats, got %v", got2)
	}
	if math.Abs(got2[3]-1.0) > 0.05 {
		t.Errorf("t=2: got %v, want within 0.05 of 1.0", got2[3])
	}
}

// TestEvalGenSpinZeroAlloc: generator (spin) eval must not allocate.
func TestEvalGenSpinZeroAlloc(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 10,
					PropID:   0,
					Gen: &Generator{
						Kind: GenSpin,
						Spin: [3]float64{1, 0, 0},
					},
				},
			},
		},
	}
	out := NewWriteBuf(64)
	allocs := testing.AllocsPerRun(1000, func() {
		out.Reset()
		Eval(tl, 0.5, Policy{}, out)
	})
	if allocs != 0 {
		t.Errorf("expected 0 allocs per run (spin generator), got %v", allocs)
	}
}

// TestEvalSkipsGenerator: tracks with Gen != nil are skipped (pre-1.6c behavior removed;
// now generators emit values — this test is superseded but kept as sanity check that
// a GenNone generator emits nothing if it falls through).
// Actually: post-1.6c GenSpin/Drift/Spring all emit. GenNone should emit nothing.
func TestEvalGenNoneSkips(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs},
				Track: &Track{
					TargetID: 7,
					PropID:   3,
					Gen:      &Generator{Kind: GenNone},
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	if len(out.Writes()) != 0 {
		t.Errorf("expected no writes for GenNone generator track, got %v", out.Writes())
	}
}


// TestEvalGenShortBaseNoPanic: short Base.F slices must not panic and must emit nothing.
func TestEvalGenShortBaseNoPanic(t *testing.T) {
	// GenSpring with only 1 element in Base.F (needs 2).
	// GenDrift with only 2 elements in Base.F (needs 3).
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 11,
					PropID:   0,
					Gen: &Generator{
						Kind:   GenSpring,
						Base:   ScalarV(0), // ArityScalar width=1, needs 2 → guard fires
						Spring: Spring{Mass: 1, Stiffness: 100, Damping: 10},
					},
				},
			},
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 12,
					PropID:   1,
					Gen: &Generator{
						Kind: GenDrift,
						Base: Vec2V(0, 0), // ArityVec2 width=2, needs 3 → guard fires
					},
				},
			},
		},
	}
	out := NewWriteBuf(64)
	// Must not panic; must emit no writes.
	Eval(tl, 0.5, Policy{}, out)
	if out.Len() != 0 {
		t.Errorf("expected no writes for short Base.F generators, got %d floats: %v", out.Len(), out.Writes())
	}
	// Also test reduced-motion path.
	out.Reset()
	Eval(tl, 0.5, Policy{ReducedMotion: true}, out)
	if out.Len() != 0 {
		t.Errorf("reduced-motion: expected no writes for short Base.F generators, got %d floats: %v", out.Len(), out.Writes())
	}
}

// TestEvalSkipsEmptyKeys: tracks with no keys are skipped.
func TestEvalSkipsEmptyKeys(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At:    Position{Kind: PosAbs},
				Track: &Track{TargetID: 7, PropID: 3, Keys: nil},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	if len(out.Writes()) != 0 {
		t.Errorf("expected no writes for empty keys track, got %v", out.Writes())
	}
}

// TestEvalZeroAlloc: Eval must not allocate on the hot path after warm-up.
func TestEvalZeroAlloc(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 5,
					PropID:   2,
					Keys: []Key{
						{T: 0, Value: vec3(0, 0, 0)},
						{T: 1, Value: vec3(10, 0, 0)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	allocs := testing.AllocsPerRun(1000, func() {
		out.Reset()
		Eval(tl, 0.5, Policy{}, out)
	})
	if allocs != 0 {
		t.Errorf("expected 0 allocs per run, got %v", allocs)
	}
}

// TestEvalEasedOvershoot: EaseOutBack on a scalar track should overshoot past the
// endpoint at interior t values, and clamp exactly at t=0 and t=1.
func TestEvalEasedOvershoot(t *testing.T) {
	const eps = 1e-9
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
					Ease:   Ease{Kind: EaseOutBack},
				},
			},
		},
	}
	out := NewWriteBuf(16)

	// At t=0 → value should be 0 (clamp-start path, no easing).
	Eval(tl, 0, Policy{}, out)
	got0 := out.Writes()
	// got0 = [targetID, propID, arity, value]
	if len(got0) < 4 {
		t.Fatalf("t=0: expected 4 floats, got %v", got0)
	}
	if got0[3] < -eps || got0[3] > eps {
		t.Errorf("t=0: scalar = %v, want 0", got0[3])
	}

	// At t=1 → value should be 10 (clamp-end path, no easing).
	out.Reset()
	Eval(tl, 1, Policy{}, out)
	got1 := out.Writes()
	if len(got1) < 4 {
		t.Fatalf("t=1: expected 4 floats, got %v", got1)
	}
	if got1[3] < 10-eps || got1[3] > 10+eps {
		t.Errorf("t=1: scalar = %v, want 10", got1[3])
	}

	// At t=0.8 → EaseOutBack overshoots → scalar > 10.
	out.Reset()
	Eval(tl, 0.8, Policy{}, out)
	got08 := out.Writes()
	if len(got08) < 4 {
		t.Fatalf("t=0.8: expected 4 floats, got %v", got08)
	}
	if got08[3] <= 10.0 {
		t.Errorf("t=0.8 EaseOutBack overshoot: scalar = %v, want > 10.0", got08[3])
	}
}

// TestEvalPerKeyEaseOverride: per-key EaseLinear overrides a track-level EaseOutBack.
// At t=0.5 the segment should interpolate linearly → exactly 5.0.
func TestEvalPerKeyEaseOverride(t *testing.T) {
	perKeyLinear := Ease{Kind: EaseLinear}
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 2,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: scalar(0), Ease: &perKeyLinear},
						{T: 1, Value: scalar(10)},
					},
					Interp: InterpLinear,
					Ease:   Ease{Kind: EaseOutBack},
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	if len(got) < 4 {
		t.Fatalf("expected 4 floats, got %v", got)
	}
	if got[3] != 5.0 {
		t.Errorf("per-key EaseLinear override: scalar = %v, want exactly 5.0", got[3])
	}
}

// TestEvalDefaultLinearNoEase: track with zero-value Ease (EaseLinear) interpolates
// exactly as before — regression guard for existing behavior.
func TestEvalDefaultLinearNoEase(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 3,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: scalar(0)},
						{T: 1, Value: scalar(10)},
					},
					Interp: InterpLinear,
					// Ease zero-value = EaseLinear (iota 0) — no explicit set.
				},
			},
		},
	}
	out := NewWriteBuf(16)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	if len(got) < 4 {
		t.Fatalf("expected 4 floats, got %v", got)
	}
	if got[3] != 5.0 {
		t.Errorf("default EaseLinear: scalar = %v, want 5.0", got[3])
	}
}

// TestEvalEasedZeroAlloc: Eval must not allocate even when easing is applied.
func TestEvalEasedZeroAlloc(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 4,
					PropID:   0,
					Keys: []Key{
						{T: 0, Value: scalar(0)},
						{T: 1, Value: scalar(10)},
					},
					Interp: InterpLinear,
					Ease:   Ease{Kind: EaseOutBack},
				},
			},
		},
	}
	out := NewWriteBuf(16)
	allocs := testing.AllocsPerRun(1000, func() {
		out.Reset()
		Eval(tl, 0.5, Policy{}, out)
	})
	if allocs != 0 {
		t.Errorf("expected 0 allocs per run (eased track), got %v", allocs)
	}
}
