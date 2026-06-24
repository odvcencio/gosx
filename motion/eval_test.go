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

// cubicTL builds a single 2-key cubicspline scalar track with the given
// out-tangent (left key) and in-tangent (right key).
func cubicTL(vK, bK, vK1, aK1 float64) *Timeline {
	in := ScalarV(aK1)
	out := ScalarV(bK)
	return &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 5,
					PropID:   2,
					Keys: []Key{
						{T: 0, Value: ScalarV(vK), OutTangent: &out},
						{T: 1, Value: ScalarV(vK1), InTangent: &in},
					},
					Interp: InterpCubicSpline,
				},
			},
		},
	}
}

// TestEvalCubicSplineMidSegment: a 2-key cubicspline track evaluated mid-segment
// must match the Hermite oracle and differ from a plain linear lerp.
func TestEvalCubicSplineMidSegment(t *testing.T) {
	// vK=0, out-tangent bK=4, vK1=10, in-tangent aK1=2, delta=1 (t in [0,1]).
	tl := cubicTL(0, 4, 10, 2)
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	// layout: [targetID=5, propID=2, arity=ArityScalar(0), value]
	if len(got) != 4 {
		t.Fatalf("len mismatch: got %v, want 4 floats", got)
	}
	gotVal := got[3]

	s := 0.5
	s2 := s * s
	s3 := s2 * s
	wantHermite := (2*s3-3*s2+1)*0 + 1.0*(s3-2*s2+s)*4 + (-2*s3+3*s2)*10 + 1.0*(s3-s2)*2
	if math.Abs(gotVal-wantHermite) > 1e-12 {
		t.Errorf("cubicspline mid value = %v, want Hermite %v", gotVal, wantHermite)
	}

	// And it must differ from linear (which would be 5.0).
	if math.Abs(gotVal-5.0) < 1e-9 {
		t.Errorf("cubicspline value %v matches linear 5.0 — tangents ignored", gotVal)
	}
}

// TestEvalCubicSplineEndpoints: endpoints clamp to the key values.
func TestEvalCubicSplineEndpoints(t *testing.T) {
	tl := cubicTL(3, 9, 17, -4)
	out := NewWriteBuf(64)

	// At t=0 → clamp to first key (value 3).
	out.Reset()
	Eval(tl, 0, Policy{}, out)
	if g := out.Writes(); len(g) != 4 || math.Abs(g[3]-3) > 1e-12 {
		t.Errorf("t=0 → %v, want value 3", g)
	}

	// At t=1 → clamp to last key (value 17).
	out.Reset()
	Eval(tl, 1, Policy{}, out)
	if g := out.Writes(); len(g) != 4 || math.Abs(g[3]-17) > 1e-12 {
		t.Errorf("t=1 → %v, want value 17", g)
	}
}

// TestEvalCubicSplineFallbackNoTangents: a cubicspline track whose keys lack
// tangents falls back to linear interpolation (defensive).
func TestEvalCubicSplineFallbackNoTangents(t *testing.T) {
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					TargetID: 5,
					PropID:   2,
					Keys: []Key{
						{T: 0, Value: ScalarV(0)},
						{T: 1, Value: ScalarV(10)},
					},
					Interp: InterpCubicSpline, // but no tangents → linear
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	g := out.Writes()
	if len(g) != 4 || math.Abs(g[3]-5.0) > 1e-12 {
		t.Errorf("no-tangent cubicspline at t=0.5 → %v, want linear 5.0", g)
	}
}

// TestEvalCubicSplineZeroAlloc: cubicspline eval must preserve zero allocation.
func TestEvalCubicSplineZeroAlloc(t *testing.T) {
	tl := cubicTL(0, 4, 10, 2)
	out := NewWriteBuf(64)
	allocs := testing.AllocsPerRun(1000, func() {
		out.Reset()
		Eval(tl, 0.5, Policy{}, out)
	})
	if allocs != 0 {
		t.Errorf("expected 0 allocs per run, got %v", allocs)
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

// --- TargetMaterial tests ---

// TestEvalMaterialColorTrack: a TargetMaterial track with Prop:"emissive" and two
// Color (ArityColor) keyframes, sampled mid-segment, must emit a linearly-lerped
// packed write [targetID, propID, ArityColor(=5), r, g, b, a].
func TestEvalMaterialColorTrack(t *testing.T) {
	const eps = 1e-12
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					Target:   Target{Kind: TargetMaterial, Ref: "mat1"},
					TargetID: 10,
					Prop:     "emissive",
					PropID:   3,
					Keys: []Key{
						{T: 0, Value: Value{Arity: ArityColor, F: [4]float64{0, 0, 0, 1}}},
						{T: 1, Value: Value{Arity: ArityColor, F: [4]float64{1, 0.5, 0.25, 1}}},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(64)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	// packed: [targetID=10, propID=3, arity=ArityColor(5), r, g, b, a]
	// lerp at alpha=0.5: r=0.5, g=0.25, b=0.125, a=1.0
	want := []float64{10, 3, float64(ArityColor), 0.5, 0.25, 0.125, 1.0}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v (len=%d), want %v (len=%d)", got, len(got), want, len(want))
	}
	for i, v := range want {
		if math.Abs(got[i]-v) > eps {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
	}
}

// TestEvalMaterialScalarTrack: a TargetMaterial track with Prop:"roughness" and two
// Scalar keyframes, sampled mid-segment, must lerp the scalar value correctly.
func TestEvalMaterialScalarTrack(t *testing.T) {
	const eps = 1e-12
	tl := &Timeline{
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					Target:   Target{Kind: TargetMaterial, Ref: "mat1"},
					TargetID: 11,
					Prop:     "roughness",
					PropID:   4,
					Keys: []Key{
						{T: 0, Value: ScalarV(0.2)},
						{T: 1, Value: ScalarV(0.8)},
					},
					Interp: InterpLinear,
				},
			},
		},
	}
	out := NewWriteBuf(32)
	Eval(tl, 0.5, Policy{}, out)
	got := out.Writes()
	// packed: [targetID=11, propID=4, arity=ArityScalar(0), 0.5]
	want := []float64{11, 4, float64(ArityScalar), 0.5}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i, v := range want {
		if math.Abs(got[i]-v) > eps {
			t.Errorf("index %d: got %v, want %v (full: %v)", i, got[i], v, got)
		}
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
