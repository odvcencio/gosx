package motion

import (
	"math"
	"testing"
)

func makeTemplateTrack() Track {
	return Track{
		Prop: "opacity",
		Keys: []Key{
			{T: 0.0, Value: Value{Arity: ArityScalar, F: []float64{0.0}}},
			{T: 1.0, Value: Value{Arity: ArityScalar, F: []float64{1.0}}},
		},
		Interp: InterpLinear,
	}
}

func TestStaggerLinearFromCenter(t *testing.T) {
	template := makeTemplateTrack()
	targetIDs := []int{10, 11, 12, 13, 14}
	spec := StaggerSpec{From: FromCenter, Delay: 0.02}

	tracks := ExpandStagger(template, targetIDs, spec)

	if len(tracks) != 5 {
		t.Fatalf("expected 5 tracks, got %d", len(tracks))
	}

	// N=5, center=2.0; distances: [2,1,0,1,2]
	expectedDelays := []float64{0.04, 0.02, 0.0, 0.02, 0.04}

	for i, tr := range tracks {
		if tr.TargetID != targetIDs[i] {
			t.Errorf("track %d: TargetID=%d, want %d", i, tr.TargetID, targetIDs[i])
		}
		wantT0 := 0.0 + expectedDelays[i]
		wantT1 := 1.0 + expectedDelays[i]
		if math.Abs(tr.Keys[0].T-wantT0) > 1e-9 {
			t.Errorf("track %d: Keys[0].T=%.6f, want %.6f", i, tr.Keys[0].T, wantT0)
		}
		if math.Abs(tr.Keys[1].T-wantT1) > 1e-9 {
			t.Errorf("track %d: Keys[1].T=%.6f, want %.6f", i, tr.Keys[1].T, wantT1)
		}
	}
}

func TestStaggerLinearFromFirst(t *testing.T) {
	template := makeTemplateTrack()
	targetIDs := []int{10, 11, 12, 13, 14}
	spec := StaggerSpec{From: FromFirst, Delay: 0.02}

	tracks := ExpandStagger(template, targetIDs, spec)

	if len(tracks) != 5 {
		t.Fatalf("expected 5 tracks, got %d", len(tracks))
	}

	// distances: [0,1,2,3,4]
	expectedDelays := []float64{0.0, 0.02, 0.04, 0.06, 0.08}

	for i, tr := range tracks {
		if tr.TargetID != targetIDs[i] {
			t.Errorf("track %d: TargetID=%d, want %d", i, tr.TargetID, targetIDs[i])
		}
		wantT0 := 0.0 + expectedDelays[i]
		wantT1 := 1.0 + expectedDelays[i]
		if math.Abs(tr.Keys[0].T-wantT0) > 1e-9 {
			t.Errorf("track %d: Keys[0].T=%.6f, want %.6f", i, tr.Keys[0].T, wantT0)
		}
		if math.Abs(tr.Keys[1].T-wantT1) > 1e-9 {
			t.Errorf("track %d: Keys[1].T=%.6f, want %.6f", i, tr.Keys[1].T, wantT1)
		}
	}
}

func TestStaggerGrid3x3FromCenter(t *testing.T) {
	template := makeTemplateTrack()
	targetIDs := []int{0, 1, 2, 3, 4, 5, 6, 7, 8}
	spec := StaggerSpec{From: FromCenter, Delay: 0.1, Grid: [2]int{3, 3}}

	tracks := ExpandStagger(template, targetIDs, spec)

	if len(tracks) != 9 {
		t.Fatalf("expected 9 tracks, got %d", len(tracks))
	}

	// 3x3 grid, center = (1.0, 1.0)
	// index 4 → cell (1,1): dist=0, delay=0
	if math.Abs(tracks[4].Keys[0].T-0.0) > 1e-9 {
		t.Errorf("center track delay should be 0, got Keys[0].T=%.6f", tracks[4].Keys[0].T)
	}

	// index 1 → cell (1,0): dist=hypot(0,1)=1.0, delay=0.1
	wantEdge := 0.1 * 1.0
	if math.Abs(tracks[1].Keys[0].T-wantEdge) > 1e-9 {
		t.Errorf("edge track (idx 1): Keys[0].T=%.6f, want %.6f", tracks[1].Keys[0].T, wantEdge)
	}

	// index 0 → cell (0,0): dist=hypot(1,1)=√2, delay=0.1*√2
	wantCorner := 0.1 * math.Sqrt2
	if math.Abs(tracks[0].Keys[0].T-wantCorner) > 1e-9 {
		t.Errorf("corner track (idx 0): Keys[0].T=%.6f, want %.6f", tracks[0].Keys[0].T, wantCorner)
	}

	// verify TargetIDs
	for i, tr := range tracks {
		if tr.TargetID != targetIDs[i] {
			t.Errorf("track %d: TargetID=%d, want %d", i, tr.TargetID, targetIDs[i])
		}
	}
}

func TestStaggerPurityNoMutation(t *testing.T) {
	template := makeTemplateTrack()
	origT0 := template.Keys[0].T
	origT1 := template.Keys[1].T

	targetIDs := []int{1, 2, 3}
	spec := StaggerSpec{From: FromFirst, Delay: 0.1}

	tracks := ExpandStagger(template, targetIDs, spec)

	// template must be unmodified
	if template.Keys[0].T != origT0 {
		t.Errorf("template.Keys[0].T mutated: got %.6f, want %.6f", template.Keys[0].T, origT0)
	}
	if template.Keys[1].T != origT1 {
		t.Errorf("template.Keys[1].T mutated: got %.6f, want %.6f", template.Keys[1].T, origT1)
	}

	// output tracks must be independent copies (mutating output doesn't affect template)
	tracks[0].Keys[0].T = 999.0
	if template.Keys[0].T != origT0 {
		t.Errorf("template mutated after output track modification")
	}
	_ = tracks
}
