package vm

import (
	"math"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/motion"
)

func approx(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func quatApprox(a, b motion.Quat, eps float64) bool {
	// Quaternions q and -q represent the same rotation.
	same := approx(a.X, b.X, eps) && approx(a.Y, b.Y, eps) &&
		approx(a.Z, b.Z, eps) && approx(a.W, b.W, eps)
	neg := approx(a.X, -b.X, eps) && approx(a.Y, -b.Y, eps) &&
		approx(a.Z, -b.Z, eps) && approx(a.W, -b.W, eps)
	return same || neg
}

func TestObjectMatchesTarget3Way(t *testing.T) {
	// Channel TargetID "3".
	tests := []struct {
		name        string
		targetID    string
		objectID    string
		objectIndex int
		want        bool
	}{
		{"direct string id", "3", "3", 99, true},
		{"named id at index 3", "3", "hero", 3, true},
		{"index+1 fallback", "3", "hero", 2, true},
		{"no match", "3", "hero", 7, false},
		{"empty target never matches", "", "3", 3, false},
		{"named id direct", "hero", "hero", 12, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := objectMatchesTarget(tc.targetID, tc.objectID, tc.objectIndex); got != tc.want {
				t.Fatalf("objectMatchesTarget(%q,%q,%d)=%v want %v",
					tc.targetID, tc.objectID, tc.objectIndex, got, tc.want)
			}
		})
	}
}

func translationAnim(targetID string) rootengine.RenderAnimation {
	return rootengine.RenderAnimation{
		Name:     "clip",
		Duration: 1,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      targetID,
			Property:      "translation",
			Times:         []float64{0, 1},
			Values:        []float64{0, 0, 0, 10, 20, 30},
			Interpolation: "LINEAR",
		}},
	}
}

func TestBuildAndEvalTranslation(t *testing.T) {
	anims := []rootengine.RenderAnimation{translationAnim("3")}
	tl, dur := buildObjectClipTimeline(anims, "3", 3)
	if tl == nil {
		t.Fatal("expected a timeline for matching translation channel")
	}
	if dur != 1 {
		t.Fatalf("duration=%v want 1", dur)
	}
	buf := motion.NewWriteBuf(16)
	trs := evalClipTRS(tl, dur, 0.5, buf)
	if !trs.HasT {
		t.Fatal("expected HasT")
	}
	if trs.HasR || trs.HasS {
		t.Fatalf("unexpected R/S: %+v", trs)
	}
	want := [3]float64{5, 10, 15}
	for i := range want {
		if !approx(trs.T[i], want[i], 1e-9) {
			t.Fatalf("T=%v want %v", trs.T, want)
		}
	}
}

func TestBuildReconciliation3Way(t *testing.T) {
	// channel targets "3"
	cases := []struct {
		name        string
		objectID    string
		objectIndex int
		wantTL      bool
	}{
		{"id equals 3", "3", 50, true},
		{"named id at index 3", "hero", 3, true},
		{"index+1 (object index 2)", "hero", 2, true},
		{"non-match", "hero", 9, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anims := []rootengine.RenderAnimation{translationAnim("3")}
			tl, _ := buildObjectClipTimeline(anims, tc.objectID, tc.objectIndex)
			if (tl != nil) != tc.wantTL {
				t.Fatalf("buildObjectClipTimeline tl!=nil=%v want %v", tl != nil, tc.wantTL)
			}
		})
	}
}

func TestEvalScale(t *testing.T) {
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 2,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "5",
			Property:      "scale",
			Times:         []float64{0, 2},
			Values:        []float64{1, 1, 1, 3, 3, 3},
			Interpolation: "LINEAR",
		}},
	}}
	tl, dur := buildObjectClipTimeline(anims, "5", 5)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(16)
	trs := evalClipTRS(tl, dur, 1.0, buf) // halfway → 2,2,2
	if !trs.HasS {
		t.Fatal("expected HasS")
	}
	want := [3]float64{2, 2, 2}
	for i := range want {
		if !approx(trs.S[i], want[i], 1e-9) {
			t.Fatalf("S=%v want %v", trs.S, want)
		}
	}
}

func TestEvalScaleUniform(t *testing.T) {
	// Uniform (stride-1) scale channel broadcasts to all three axes.
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 1,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "0",
			Property:      "scale",
			Times:         []float64{0, 1},
			Values:        []float64{1, 5},
			Interpolation: "LINEAR",
		}},
	}}
	tl, dur := buildObjectClipTimeline(anims, "0", 0)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(16)
	trs := evalClipTRS(tl, dur, 0.5, buf) // halfway → 3,3,3
	if !trs.HasS {
		t.Fatal("expected HasS")
	}
	want := [3]float64{3, 3, 3}
	for i := range want {
		if !approx(trs.S[i], want[i], 1e-9) {
			t.Fatalf("S=%v want %v", trs.S, want)
		}
	}
}

// TestEvalRotationEulerSingleAxis covers the production rotation form: a single
// per-axis rotationY Euler channel. The result must equal QuatFromEuler of the
// sampled Euler angle at t.
func TestEvalRotationEulerSingleAxis(t *testing.T) {
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 1,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "3",
			Property:      "rotationY",
			Times:         []float64{0, 1},
			Values:        []float64{0, math.Pi}, // 0 → pi radians about Y
			Interpolation: "LINEAR",
		}},
	}}
	tl, dur := buildObjectClipTimeline(anims, "3", 3)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(16)
	trs := evalClipTRS(tl, dur, 0.5, buf) // halfway → pi/2 about Y
	if !trs.HasR {
		t.Fatal("expected HasR")
	}
	// The keyframe values are quaternions of pi/2 about Y at the endpoints? No:
	// the track is built by sampling Euler at each keyframe time (0 and pi),
	// then slerping. Slerp(Q(0), Q(pi about Y), 0.5) == Q(pi/2 about Y).
	wantQ := motion.QuatFromEuler(0, math.Pi/2, 0).Normalize()
	if !quatApprox(trs.R, wantQ, 1e-9) {
		t.Fatalf("R=%+v want %+v", trs.R, wantQ)
	}
}

// TestEvalRotationEulerThreeAxes covers grouped rotationX/Y/Z reconstruction at
// a shared keyframe time.
func TestEvalRotationEulerThreeAxes(t *testing.T) {
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 1,
		Channels: []rootengine.RenderAnimationChannel{
			{TargetID: "3", Property: "rotationX", Times: []float64{0, 1}, Values: []float64{0, 0.4}, Interpolation: "LINEAR"},
			{TargetID: "3", Property: "rotationY", Times: []float64{0, 1}, Values: []float64{0, 0.6}, Interpolation: "LINEAR"},
			{TargetID: "3", Property: "rotationZ", Times: []float64{0, 1}, Values: []float64{0, 0.8}, Interpolation: "LINEAR"},
		},
	}}
	tl, dur := buildObjectClipTimeline(anims, "3", 3)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(16)
	// Sample near the end of the loop window (just under duration=1). Linear
	// blend at alpha≈1 between Q(0,0,0) and Q(0.4,0.6,0.8) is ≈ Q(0.4,0.6,0.8).
	// (t=1.0 would wrap to 0 via the loop, so use t<duration.)
	trs := evalClipTRS(tl, dur, 0.999999999, buf)
	if !trs.HasR {
		t.Fatal("expected HasR")
	}
	wantQ := motion.QuatFromEuler(0.4, 0.6, 0.8).Normalize()
	if !quatApprox(trs.R, wantQ, 1e-9) {
		t.Fatalf("R=%+v want %+v", trs.R, wantQ)
	}
}

// TestEvalRotationCombinedQuat covers the defensive combined width-4 rotation
// channel form (Phase-1 / glTF style).
func TestEvalRotationCombinedQuat(t *testing.T) {
	q0 := motion.QuatFromEuler(0, 0, 0).Normalize()
	q1 := motion.QuatFromEuler(0, math.Pi/2, 0).Normalize()
	anims := []rootengine.RenderAnimation{{
		Name:     "clip",
		Duration: 1,
		Channels: []rootengine.RenderAnimationChannel{{
			TargetID:      "3",
			Property:      "rotation",
			Times:         []float64{0, 1},
			Values:        []float64{q0.X, q0.Y, q0.Z, q0.W, q1.X, q1.Y, q1.Z, q1.W},
			Interpolation: "LINEAR",
		}},
	}}
	tl, dur := buildObjectClipTimeline(anims, "3", 3)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(16)
	trs := evalClipTRS(tl, dur, 0.5, buf)
	if !trs.HasR {
		t.Fatal("expected HasR")
	}
	wantQ := motion.Slerp(q0, q1, 0.5)
	if !quatApprox(trs.R, wantQ, 1e-9) {
		t.Fatalf("R=%+v want %+v", trs.R, wantQ)
	}
}

func TestEvalLooping(t *testing.T) {
	anims := []rootengine.RenderAnimation{translationAnim("3")}
	tl, dur := buildObjectClipTimeline(anims, "3", 3)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(16)
	at05 := evalClipTRS(tl, dur, 0.5, buf)
	at15 := evalClipTRS(tl, dur, 1.5, buf) // wraps to 0.5
	for i := 0; i < 3; i++ {
		if !approx(at05.T[i], at15.T[i], 1e-9) {
			t.Fatalf("loop mismatch: t=0.5 %v vs t=1.5 %v", at05.T, at15.T)
		}
	}
}

func TestNoMatch(t *testing.T) {
	anims := []rootengine.RenderAnimation{translationAnim("3")}
	tl, dur := buildObjectClipTimeline(anims, "other", 99)
	if tl != nil || dur != 0 {
		t.Fatalf("expected (nil,0) for non-matching object, got (%v,%v)", tl, dur)
	}
	// evalClipTRS(nil, ...) must return a zero clipTRS without panic.
	buf := motion.NewWriteBuf(16)
	trs := evalClipTRS(nil, 0, 0.5, buf)
	if trs.HasT || trs.HasR || trs.HasS {
		t.Fatalf("expected zero clipTRS, got %+v", trs)
	}
}

func TestMultiChannelObject(t *testing.T) {
	// One object with translation + scale + rotationY channels across two clips.
	anims := []rootengine.RenderAnimation{
		{
			Name:     "move",
			Duration: 1,
			Channels: []rootengine.RenderAnimationChannel{{
				TargetID: "3", Property: "translation",
				Times: []float64{0, 1}, Values: []float64{0, 0, 0, 10, 0, 0}, Interpolation: "LINEAR",
			}},
		},
		{
			Name:     "spin",
			Duration: 1,
			Channels: []rootengine.RenderAnimationChannel{
				{TargetID: "3", Property: "rotationY", Times: []float64{0, 1}, Values: []float64{0, math.Pi}, Interpolation: "LINEAR"},
				{TargetID: "3", Property: "scale", Times: []float64{0, 1}, Values: []float64{1, 1, 1, 2, 2, 2}, Interpolation: "LINEAR"},
			},
		},
	}
	tl, dur := buildObjectClipTimeline(anims, "3", 3)
	if tl == nil {
		t.Fatal("expected timeline")
	}
	buf := motion.NewWriteBuf(32)
	trs := evalClipTRS(tl, dur, 0.5, buf)
	if !trs.HasT || !trs.HasR || !trs.HasS {
		t.Fatalf("expected T,R,S all set: %+v", trs)
	}
	if !approx(trs.T[0], 5, 1e-9) {
		t.Fatalf("T.x=%v want 5", trs.T[0])
	}
	if !approx(trs.S[0], 1.5, 1e-9) {
		t.Fatalf("S.x=%v want 1.5", trs.S[0])
	}
	wantQ := motion.QuatFromEuler(0, math.Pi/2, 0).Normalize()
	if !quatApprox(trs.R, wantQ, 1e-9) {
		t.Fatalf("R=%+v want %+v", trs.R, wantQ)
	}
}
