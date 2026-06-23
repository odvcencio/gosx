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
