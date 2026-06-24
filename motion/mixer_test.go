package motion

import (
	"math"
	"testing"
)

// --- helpers ----------------------------------------------------------------

// scalarClip builds a single-track timeline animating one scalar prop from a→b
// over `dur` seconds, with InterpLinear.
func scalarClip(targetID, propID int, a, b, dur float64) *Timeline {
	tr := &Track{
		TargetID: targetID,
		PropID:   propID,
		Interp:   InterpLinear,
		Keys: []Key{
			{T: 0, Value: ScalarV(a)},
			{T: dur, Value: ScalarV(b)},
		},
	}
	return &Timeline{Children: []Positioned{{At: Position{Kind: PosAbs}, Track: tr}}}
}

// vec3Clip builds a single-track timeline animating one vec3 prop from a→b.
func vec3Clip(targetID, propID int, a, b [3]float64, dur float64) *Timeline {
	tr := &Track{
		TargetID: targetID,
		PropID:   propID,
		Interp:   InterpLinear,
		Keys: []Key{
			{T: 0, Value: Vec3V(a[0], a[1], a[2])},
			{T: dur, Value: Vec3V(b[0], b[1], b[2])},
		},
	}
	return &Timeline{Children: []Positioned{{At: Position{Kind: PosAbs}, Track: tr}}}
}

// constQuatClip builds a single-key (constant) quaternion track.
func constQuatClip(targetID, propID int, q Quat) *Timeline {
	tr := &Track{
		TargetID: targetID,
		PropID:   propID,
		Interp:   InterpLinear,
		Keys:     []Key{{T: 0, Value: QuatV(q)}},
	}
	return &Timeline{Children: []Positioned{{At: Position{Kind: PosAbs}, Track: tr}}}
}

// findWrite scans a packed WriteBuf for the (targetID, propID) key and returns
// its arity and value components, plus whether it was found.
func findWrite(out *WriteBuf, targetID, propID int) (ValueArity, []float64, bool) {
	w := out.Writes()
	i := 0
	for i < len(w) {
		tid := int(w[i])
		pid := int(w[i+1])
		arity := ValueArity(w[i+2])
		width := arity.Width()
		if tid == targetID && pid == propID {
			vals := make([]float64, width)
			copy(vals, w[i+3:i+3+width])
			return arity, vals, true
		}
		i += 3 + width
	}
	return 0, nil, false
}

func countWrites(out *WriteBuf) int {
	w := out.Writes()
	i := 0
	n := 0
	for i < len(w) {
		arity := ValueArity(w[i+2])
		i += 3 + arity.Width()
		n++
	}
	return n
}

const tol = 1e-9

// --- tests ------------------------------------------------------------------

func TestMixerSingleClipNoFade(t *testing.T) {
	m := NewMixer()
	// scalar prop animating 0→10 over 2s on node 1 prop 0.
	m.AddClip("a", scalarClip(1, 0, 0, 10, 2), 2)
	m.Play("a", PlayOptions{}) // no fade → full weight immediately

	out := NewWriteBuf(16)
	// advance 0.5s → t=0.5 → value = 10*(0.5/2) = 2.5
	m.Update(0.5, Policy{}, out)
	_, v, ok := findWrite(out, 1, 0)
	if !ok {
		t.Fatalf("no write for (1,0)")
	}
	if math.Abs(v[0]-2.5) > tol {
		t.Fatalf("after dt=0.5 value=%v want 2.5", v[0])
	}

	// advance another 1.0s → t=1.5 → value = 7.5
	m.Update(1.0, Policy{}, out)
	_, v, _ = findWrite(out, 1, 0)
	if math.Abs(v[0]-7.5) > tol {
		t.Fatalf("after dt total 1.5 value=%v want 7.5", v[0])
	}
}

func TestMixerLoopWrap(t *testing.T) {
	m := NewMixer()
	m.AddClip("a", scalarClip(1, 0, 0, 10, 2), 2)
	m.Play("a", PlayOptions{Loop: true})

	out := NewWriteBuf(16)
	// advance 2.5s with loop → time wraps to 0.5 → value 2.5
	m.Update(2.5, Policy{}, out)
	_, v, ok := findWrite(out, 1, 0)
	if !ok {
		t.Fatalf("no write")
	}
	if math.Abs(v[0]-2.5) > tol {
		t.Fatalf("looped value=%v want 2.5", v[0])
	}
}

func TestMixerNoLoopClampsAtDuration(t *testing.T) {
	m := NewMixer()
	m.AddClip("a", scalarClip(1, 0, 0, 10, 2), 2)
	m.Play("a", PlayOptions{Loop: false})

	out := NewWriteBuf(16)
	// dt=1.0 → t=1.0 → value 5.0; clip still playing.
	m.Update(1.0, Policy{}, out)
	if !m.IsPlaying("a") {
		t.Fatalf("clip should still be playing at t=1.0")
	}
	// dt=2.0 → t=3.0 >= duration 2 → non-loop clip removed after this update.
	m.Update(2.0, Policy{}, out)
	if m.IsPlaying("a") {
		t.Fatalf("non-loop clip past duration should be removed")
	}
}

func TestMixerSpeed(t *testing.T) {
	m := NewMixer()
	m.AddClip("a", scalarClip(1, 0, 0, 10, 4), 4)
	m.Play("a", PlayOptions{Speed: 2})

	out := NewWriteBuf(16)
	// dt=1.0 at speed 2 → t=2.0 → value = 10*(2/4) = 5.0
	m.Update(1.0, Policy{}, out)
	_, v, _ := findWrite(out, 1, 0)
	if math.Abs(v[0]-5.0) > tol {
		t.Fatalf("speed-2 value=%v want 5.0", v[0])
	}
}

func TestMixerFadeInRamp(t *testing.T) {
	// Two clips on the same node+prop. Base clip "rest" holds value 0 at full
	// weight; fading clip "b" holds value 10. The blended value reflects b's
	// effective weight per the incremental sceneAnimBlendValue scheme.
	m := NewMixer()
	m.AddClip("rest", scalarClip(1, 0, 0, 0, 10), 10) // constant 0, long duration
	m.AddClip("b", scalarClip(1, 0, 10, 10, 10), 10)  // constant 10, long duration

	m.Play("rest", PlayOptions{}) // weight 1, no fade
	m.Play("b", PlayOptions{FadeIn: 1.0})

	out := NewWriteBuf(16)
	// After dt=0.5, b.weight ramps to 0.5. rest is processed first (weight 1),
	// then b blends in with t = 0.5/(1+0.5) = 1/3 → blended = 0 + (10-0)*1/3.
	m.Update(0.5, Policy{}, out)
	_, v, _ := findWrite(out, 1, 0)
	want := 10.0 / 3.0
	if math.Abs(v[0]-want) > 1e-6 {
		t.Fatalf("mid-fade-in value=%v want %v", v[0], want)
	}

	// Continue to full fade-in: dt totaling 1.0, b.weight=1.
	// blended = 0 + (10-0)*1/(1+1) = 5.0
	m.Update(0.5, Policy{}, out)
	_, v, _ = findWrite(out, 1, 0)
	if math.Abs(v[0]-5.0) > 1e-6 {
		t.Fatalf("full-fade-in value=%v want 5.0", v[0])
	}
}

func TestMixerCrossfadeQuatSlerpMidpoint(t *testing.T) {
	// A = identity, B = 90° about Y, both constant on node 2 prop 1.
	idA := Quat{0, 0, 0, 1}
	rotB := QuatFromEuler(0, math.Pi/2, 0) // 90° about Y

	m := NewMixer()
	m.AddClip("A", constQuatClip(2, 1, idA), 10)
	m.AddClip("B", constQuatClip(2, 1, rotB), 10)

	m.Play("A", PlayOptions{}) // full weight
	out := NewWriteBuf(16)
	m.Update(0.0, Policy{}, out) // settle: only A → equals A
	arity, v, _ := findWrite(out, 2, 1)
	if arity != ArityQuat {
		t.Fatalf("expected quat arity, got %v", arity)
	}
	if math.Abs(v[3]-1) > tol {
		t.Fatalf("A-only should be identity, got %v", v)
	}

	// Crossfade: fade B in over 1s and A out over 1s.
	m.Play("B", PlayOptions{FadeIn: 1.0})
	m.Stop("A", StopOptions{FadeOut: 1.0})

	// Half fade: dt=0.5 → A.weight=0.5, B.weight=0.5.
	// A processed first (weight 0.5) → accum = A, totalWeight 0.5.
	// B blends: t = 0.5/(0.5+0.5) = 0.5 → slerp(A,B,0.5) = 45° about Y.
	m.Update(0.5, Policy{}, out)
	_, v, _ = findWrite(out, 2, 1)
	mid := Slerp(idA, rotB, 0.5)
	if quatDist(Quat{v[0], v[1], v[2], v[3]}, mid) > 1e-6 {
		t.Fatalf("half-fade crossfade=%v want slerp midpoint %v", v, mid)
	}

	// Complete the fade: dt totaling 1.0 → A.weight→0 and removed; B.weight=1.
	m.Update(0.5, Policy{}, out)
	if m.IsPlaying("A") {
		t.Fatalf("A should be removed after fade-out completes")
	}
	if !m.IsPlaying("B") {
		t.Fatalf("B should still be playing")
	}
	_, v, _ = findWrite(out, 2, 1)
	if quatDist(Quat{v[0], v[1], v[2], v[3]}, rotB) > 1e-6 {
		t.Fatalf("after crossfade value=%v want B=%v", v, rotB)
	}
}

func TestMixerVecBlend(t *testing.T) {
	// Two clips animating translation (vec3) on the same node+prop, both
	// constant. A = (0,0,0) weight 1, B = (10,20,30) weight 1 (full, no fade).
	m := NewMixer()
	m.AddClip("A", vec3Clip(3, 2, [3]float64{0, 0, 0}, [3]float64{0, 0, 0}, 1), 1)
	m.AddClip("B", vec3Clip(3, 2, [3]float64{10, 20, 30}, [3]float64{10, 20, 30}, 1), 1)

	m.Play("A", PlayOptions{})
	m.Play("B", PlayOptions{}) // both weight 1 immediately

	out := NewWriteBuf(32)
	m.Update(0.0, Policy{}, out)
	arity, v, ok := findWrite(out, 3, 2)
	if !ok {
		t.Fatalf("no vec write")
	}
	if arity != ArityVec3 {
		t.Fatalf("arity=%v want Vec3", arity)
	}
	// A first (w=1, accum=(0,0,0), tw=1). B blends with t=1/(1+1)=0.5 →
	// average: (5,10,15).
	want := [3]float64{5, 10, 15}
	for i := 0; i < 3; i++ {
		if math.Abs(v[i]-want[i]) > 1e-9 {
			t.Fatalf("vec blend=%v want %v", v, want)
		}
	}
}

func TestMixerIsPlayingAndRemoval(t *testing.T) {
	m := NewMixer()
	m.AddClip("a", scalarClip(1, 0, 0, 10, 2), 2)
	if m.IsPlaying("a") {
		t.Fatalf("not playing before Play")
	}
	m.Play("a", PlayOptions{})
	if !m.IsPlaying("a") {
		t.Fatalf("should be playing after Play")
	}
	out := NewWriteBuf(16)
	// Immediate stop removes it.
	m.Stop("a", StopOptions{FadeOut: 0})
	if m.IsPlaying("a") {
		t.Fatalf("immediate stop should remove")
	}
	// nothing emitted now.
	m.Update(0.1, Policy{}, out)
	if countWrites(out) != 0 {
		t.Fatalf("expected no writes after removal, got %d", countWrites(out))
	}
}

func TestMixerFadeOutRemoval(t *testing.T) {
	m := NewMixer()
	m.AddClip("a", scalarClip(1, 0, 0, 10, 5), 5)
	m.Play("a", PlayOptions{})
	m.Stop("a", StopOptions{FadeOut: 1.0})

	out := NewWriteBuf(16)
	// Half-fade: still playing.
	m.Update(0.5, Policy{}, out)
	if !m.IsPlaying("a") {
		t.Fatalf("should still play mid-fade-out")
	}
	// Complete fade.
	m.Update(0.6, Policy{}, out)
	if m.IsPlaying("a") {
		t.Fatalf("should be removed after fade-out completes")
	}
}

func TestMixerReducedMotion(t *testing.T) {
	// A linear scalar clip 0→10; reduced motion collapses to the last key (rest).
	m := NewMixer()
	m.AddClip("a", scalarClip(1, 0, 0, 10, 2), 2)
	m.Play("a", PlayOptions{})

	out := NewWriteBuf(16)
	m.Update(0.5, Policy{ReducedMotion: true}, out)
	_, v, ok := findWrite(out, 1, 0)
	if !ok {
		t.Fatalf("no write")
	}
	// Eval with ReducedMotion emits the last key (10).
	if math.Abs(v[0]-10) > tol {
		t.Fatalf("reduced-motion value=%v want 10 (rest)", v[0])
	}
}

func TestMixerBlendMathHandComputed(t *testing.T) {
	// Three scalar clips on (1,0) with values 4, 8, 12 and weights 1, 0.5, 0.25
	// processed in that order. Hand-compute sceneAnimBlendValue:
	//   start accum=4, tw=1
	//   blend 8 w=0.5: t=0.5/1.5=1/3 → 4 + (8-4)/3 = 5.3333..., tw=1.5
	//   blend 12 w=0.25: t=0.25/1.75=1/7 → 5.3333 + (12-5.3333)/7, tw=1.75
	m := NewMixer()
	m.AddClip("c1", scalarClip(1, 0, 4, 4, 1), 1)
	m.AddClip("c2", scalarClip(1, 0, 8, 8, 1), 1)
	m.AddClip("c3", scalarClip(1, 0, 12, 12, 1), 1)
	m.Play("c1", PlayOptions{Weight: 1})
	m.Play("c2", PlayOptions{Weight: 0.5})
	m.Play("c3", PlayOptions{Weight: 0.25})

	out := NewWriteBuf(16)
	m.Update(0.0, Policy{}, out)
	_, v, _ := findWrite(out, 1, 0)

	// Hand compute.
	accum := 4.0
	tw := 1.0
	for _, p := range []struct{ val, w float64 }{{8, 0.5}, {12, 0.25}} {
		tt := p.w / (tw + p.w)
		accum = accum + (p.val-accum)*tt
		tw += p.w
	}
	if math.Abs(v[0]-accum) > 1e-9 {
		t.Fatalf("blend math=%v want %v", v[0], accum)
	}
}

// quatDist returns the angular-ish distance between two quaternions (1 - |dot|),
// sign-independent — 0 when identical (or antipodal, same rotation).
func quatDist(a, b Quat) float64 {
	dot := a.X*b.X + a.Y*b.Y + a.Z*b.Z + a.W*b.W
	return 1 - math.Abs(dot)
}

// TestMixerCrossClipBlend is the regression test for the cross-clip TargetID
// consistency fix. Before the fix, BuildClipTimeline used a per-clip interner so
// two clips with different channel order got different TargetIDs for the same glTF
// node — the mixer then failed to recognise shared nodes and blended incorrectly.
//
// Clip A animates rotation on nodes {2, 5} (in that order).
// Clip B animates rotation on nodes {5, 9} (in that order — different first node,
// so the old interner WOULD have assigned node 5 TargetID=0 in A and TargetID=0
// in B too, but node 2 vs 9 differ; more critically, two-node clips with different
// node sets means the first-seen intern order disagrees for the shared node once
// combined with a cross-clip scenario where neither is first).
//
// With the fix: TargetID == node index unconditionally, PropID fixed by property.
// The mixer must:
//   - Blend node 5's rotation (shared): result ≈ slerp(A_rot5, B_rot5, 0.5).
//   - Pass through node 2's rotation (A-only) = A_rot2.
//   - Pass through node 9's rotation (B-only) = B_rot9.
func TestMixerCrossClipBlend(t *testing.T) {
	// Constant quaternions per node per clip (no time animation needed — just
	// need distinct, recognisable rotations).
	rotA2 := QuatFromEuler(0, math.Pi/4, 0) // 45° about Y
	rotA5 := QuatFromEuler(math.Pi/6, 0, 0) // 30° about X
	rotB5 := QuatFromEuler(0, 0, math.Pi/3) // 60° about Z
	rotB9 := QuatFromEuler(0, math.Pi/2, 0) // 90° about Y

	// Build clip A: channels in order [node2-rotation, node5-rotation].
	clipA, durA := BuildClipTimeline([]ClipChannel{
		{
			Node:     2,
			Property: "rotation",
			Interp:   "LINEAR",
			Times:    []float64{0, 1},
			Values:   []float64{rotA2.X, rotA2.Y, rotA2.Z, rotA2.W, rotA2.X, rotA2.Y, rotA2.Z, rotA2.W},
		},
		{
			Node:     5,
			Property: "rotation",
			Interp:   "LINEAR",
			Times:    []float64{0, 1},
			Values:   []float64{rotA5.X, rotA5.Y, rotA5.Z, rotA5.W, rotA5.X, rotA5.Y, rotA5.Z, rotA5.W},
		},
	})

	// Build clip B: channels in order [node5-rotation, node9-rotation].
	// Note: node5 appears FIRST in B but SECOND in A — this is the ordering that
	// would have produced different TargetIDs under the old per-clip interner.
	clipB, durB := BuildClipTimeline([]ClipChannel{
		{
			Node:     5,
			Property: "rotation",
			Interp:   "LINEAR",
			Times:    []float64{0, 1},
			Values:   []float64{rotB5.X, rotB5.Y, rotB5.Z, rotB5.W, rotB5.X, rotB5.Y, rotB5.Z, rotB5.W},
		},
		{
			Node:     9,
			Property: "rotation",
			Interp:   "LINEAR",
			Times:    []float64{0, 1},
			Values:   []float64{rotB9.X, rotB9.Y, rotB9.Z, rotB9.W, rotB9.X, rotB9.Y, rotB9.Z, rotB9.W},
		},
	})

	m := NewMixer()
	m.AddClip("A", clipA, durA)
	m.AddClip("B", clipB, durB)
	m.Play("A", PlayOptions{}) // weight 1
	m.Play("B", PlayOptions{}) // weight 1

	out := NewWriteBuf(64)
	m.Update(0.0, Policy{}, out)

	const rotTol = 1e-6

	// --- Node 2 (only A): should equal rotA2 exactly. ---
	arity2, v2, ok2 := findWrite(out, 2, propIDRotation)
	if !ok2 {
		t.Fatalf("no write for node 2 (A-only)")
	}
	if arity2 != ArityQuat {
		t.Fatalf("node 2 arity = %v, want ArityQuat", arity2)
	}
	got2 := Quat{v2[0], v2[1], v2[2], v2[3]}
	if quatDist(got2, rotA2) > rotTol {
		t.Errorf("node 2 (A-only): got %v, want %v (rotA2)", got2, rotA2)
	}

	// --- Node 9 (only B): should equal rotB9 exactly. ---
	arity9, v9, ok9 := findWrite(out, 9, propIDRotation)
	if !ok9 {
		t.Fatalf("no write for node 9 (B-only)")
	}
	if arity9 != ArityQuat {
		t.Fatalf("node 9 arity = %v, want ArityQuat", arity9)
	}
	got9 := Quat{v9[0], v9[1], v9[2], v9[3]}
	if quatDist(got9, rotB9) > rotTol {
		t.Errorf("node 9 (B-only): got %v, want %v (rotB9)", got9, rotB9)
	}

	// --- Node 5 (shared): must be slerp(rotA5, rotB5, 0.5) — A blended first
	// at weight 1, then B blends at t = 1/(1+1) = 0.5. ---
	arity5, v5, ok5 := findWrite(out, 5, propIDRotation)
	if !ok5 {
		t.Fatalf("no write for node 5 (shared) — cross-clip blend FAILED (IDs disagree?)")
	}
	if arity5 != ArityQuat {
		t.Fatalf("node 5 arity = %v, want ArityQuat", arity5)
	}
	got5 := Quat{v5[0], v5[1], v5[2], v5[3]}
	want5 := Slerp(rotA5, rotB5, 0.5)
	if quatDist(got5, want5) > rotTol {
		t.Errorf("node 5 (shared blend): got %v, want slerp(rotA5,rotB5,0.5)=%v", got5, want5)
	}

	// --- Total write count: 3 (nodes 2, 5, 9 — one write each). ---
	n := countWrites(out)
	if n != 3 {
		t.Errorf("expected 3 writes (nodes 2,5,9), got %d", n)
	}
}
