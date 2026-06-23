package motion

import (
	"testing"
)

// helpers to build test values

func vec3(x, y, z float64) Value {
	return Value{Arity: ArityVec3, F: []float64{x, y, z}}
}

func scalar(v float64) Value {
	return Value{Arity: ArityScalar, F: []float64{v}}
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

// TestEvalSkipsGenerator: tracks with Gen != nil are skipped.
func TestEvalSkipsGenerator(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs},
				Track: &Track{
					TargetID: 7,
					PropID:   3,
					Gen:      &Generator{Kind: GenSpin},
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	if len(out.Writes()) != 0 {
		t.Errorf("expected no writes for generator track, got %v", out.Writes())
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
