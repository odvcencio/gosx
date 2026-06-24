package motion

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// goldenSample is one time-sample in the corpus: T is the sample time and
// Writes is the flat packed WriteBuf output at that time.
type goldenSample struct {
	T      float64   `json:"t"`
	Writes []float64 `json:"writes"`
}

// goldenCase is one named test scenario: a Timeline, a tolerance, and samples.
type goldenCase struct {
	Name     string         `json:"name"`
	Tol      float64        `json:"tol"`
	Timeline Timeline       `json:"timeline"`
	Samples  []goldenSample `json:"samples"`
}

// ---------------------------------------------------------------------------
// JSON marshalling helpers for IR types (test-only; not compiled into WASM).
// ---------------------------------------------------------------------------

// --- ValueArity ---

func (a ValueArity) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint8(a))
}

func (a *ValueArity) UnmarshalJSON(b []byte) error {
	var v uint8
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*a = ValueArity(v)
	return nil
}

// --- Interp ---

func (i Interp) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint8(i))
}

func (i *Interp) UnmarshalJSON(b []byte) error {
	var v uint8
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*i = Interp(v)
	return nil
}

// --- TargetKind ---

func (k TargetKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint8(k))
}

func (k *TargetKind) UnmarshalJSON(b []byte) error {
	var v uint8
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*k = TargetKind(v)
	return nil
}

// --- PositionKind ---

func (pk PositionKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint8(pk))
}

func (pk *PositionKind) UnmarshalJSON(b []byte) error {
	var v uint8
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*pk = PositionKind(v)
	return nil
}

// --- LoopMode ---

func (lm LoopMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(lm))
}

func (lm *LoopMode) UnmarshalJSON(b []byte) error {
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*lm = LoopMode(v)
	return nil
}

// --- GeneratorKind ---

func (gk GeneratorKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint8(gk))
}

func (gk *GeneratorKind) UnmarshalJSON(b []byte) error {
	var v uint8
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*gk = GeneratorKind(v)
	return nil
}

// --- EaseKind ---

func (ek EaseKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(uint8(ek))
}

func (ek *EaseKind) UnmarshalJSON(b []byte) error {
	var v uint8
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*ek = EaseKind(v)
	return nil
}

// ---------------------------------------------------------------------------
// Corpus builder helpers
// ---------------------------------------------------------------------------

// sampleTimeline runs Eval at each t value and records the Writes output.
func sampleTimeline(tl *Timeline, ts []float64) []goldenSample {
	buf := NewWriteBuf(256)
	samples := make([]goldenSample, len(ts))
	for i, t := range ts {
		buf.Reset()
		Eval(tl, t, Policy{}, buf)
		w := buf.Writes()
		ws := make([]float64, len(w))
		copy(ws, w)
		samples[i] = goldenSample{T: t, Writes: ws}
	}
	return samples
}

// corpusCases returns the full set of golden cases ready for serialisation.
// These definitions are the canonical specification; the JSON on disk is
// generated from this function via TestRegenerateGolden.
func corpusCases() []goldenCase {
	var cases []goldenCase

	// -----------------------------------------------------------------------
	// 1. transform_lerp: vec3 linear interpolation
	// -----------------------------------------------------------------------
	{
		tl := Timeline{
			Children: []Positioned{
				{
					At: Position{Kind: PosAbs, Val: 0},
					Track: &Track{
						TargetID: 1,
						PropID:   0,
						Keys: []Key{
							{T: 0, Value: Vec3V(0, 0, 0)},
							{T: 1, Value: Vec3V(10, 20, 30)},
						},
						Interp: InterpLinear,
					},
				},
			},
		}
		cases = append(cases, goldenCase{
			Name:     "transform_lerp",
			Tol:      1e-9,
			Timeline: tl,
			Samples:  sampleTimeline(&tl, []float64{0, 0.25, 0.5, 1.0}),
		})
	}

	// -----------------------------------------------------------------------
	// 2. quat_slerp: quat slerp + shortest-arc sign-flip
	// -----------------------------------------------------------------------
	{
		// Track 1: identity → 90° around Z (shortest-arc test)
		// Track 2: identity → negated identity (sign-flip; shortest-arc must pick the
		//           short path, so [0,0,0,1]→[0,0,0,-1] is a 180° rotation).
		tl := Timeline{
			Children: []Positioned{
				{
					At: Position{Kind: PosAbs, Val: 0},
					Track: &Track{
						TargetID: 1,
						PropID:   1,
						Keys: []Key{
							{T: 0, Value: Value{ArityQuat, [4]float64{0, 0, 0, 1}}},
							{T: 1, Value: Value{ArityQuat, [4]float64{0, 0, 0.7071068, 0.7071068}}},
						},
						Interp: InterpLinear,
					},
				},
				{
					At: Position{Kind: PosAbs, Val: 0},
					Track: &Track{
						TargetID: 2,
						PropID:   1,
						Keys: []Key{
							{T: 0, Value: Value{ArityQuat, [4]float64{0, 0, 0, 1}}},
							{T: 1, Value: Value{ArityQuat, [4]float64{0, 0, 0, -1}}},
						},
						Interp: InterpLinear,
					},
				},
			},
		}
		cases = append(cases, goldenCase{
			Name:     "quat_slerp",
			Tol:      1e-9,
			Timeline: tl,
			Samples:  sampleTimeline(&tl, []float64{0, 0.5, 1.0}),
		})
	}

	// -----------------------------------------------------------------------
	// 3. spring_settle: GenSpring vec2 base (from=0, to=1)
	// -----------------------------------------------------------------------
	{
		tl := Timeline{
			Children: []Positioned{
				{
					At: Position{Kind: PosAbs, Val: 0},
					Track: &Track{
						TargetID: 1,
						PropID:   2,
						Gen: &Generator{
							Kind:   GenSpring,
							Base:   Vec2V(0, 1),
							Spring: Spring{Mass: 1, Stiffness: 100, Damping: 10},
						},
					},
				},
			},
		}
		cases = append(cases, goldenCase{
			Name:     "spring_settle",
			Tol:      1e-6,
			Timeline: tl,
			Samples:  sampleTimeline(&tl, []float64{0, 0.1, 0.5, 2.0}),
		})
	}

	// -----------------------------------------------------------------------
	// 4. multi_track: two positioned tracks — vec3 at PosAbs 0, scalar at PosAbs 0.5
	// -----------------------------------------------------------------------
	{
		tl := Timeline{
			Children: []Positioned{
				{
					At: Position{Kind: PosAbs, Val: 0},
					Track: &Track{
						TargetID: 1,
						PropID:   0,
						Keys: []Key{
							{T: 0, Value: Vec3V(0, 0, 0)},
							{T: 1, Value: Vec3V(10, 20, 30)},
						},
						Interp: InterpLinear,
					},
				},
				{
					At: Position{Kind: PosAbs, Val: 0.5},
					Track: &Track{
						TargetID: 2,
						PropID:   0,
						Keys: []Key{
							{T: 0, Value: ScalarV(0)},
							{T: 1, Value: ScalarV(100)},
						},
						Interp: InterpLinear,
					},
				},
			},
		}
		// Samples:
		//  t=0.25: vec3 track active (localT=0.25), scalar not yet started (localT=-0.25 → clamped to 0)
		//  t=0.75: both active
		//  t=1.25: vec3 past end (clamped to last), scalar mid-way (localT=0.75)
		cases = append(cases, goldenCase{
			Name:     "multi_track",
			Tol:      1e-9,
			Timeline: tl,
			Samples:  sampleTimeline(&tl, []float64{0.25, 0.75, 1.25}),
		})
	}

	// -----------------------------------------------------------------------
	// 5. stagger: ExpandStagger over 3 targets, FromCenter, Delay 0.1
	// -----------------------------------------------------------------------
	{
		template := Track{
			Prop:   "opacity",
			PropID: 3,
			Keys: []Key{
				{T: 0, Value: ScalarV(0)},
				{T: 1, Value: ScalarV(10)},
			},
			Interp: InterpLinear,
		}
		tracks := ExpandStagger(template, []int{1, 2, 3}, StaggerSpec{
			From:  FromCenter,
			Delay: 0.1,
		})
		// N=3, center=1.0 → distances: [1.0, 0.0, 1.0] → delays: [0.1, 0.0, 0.1]
		//   track 0 (targetID=1): keys shifted by 0.1 → active [0.1, 1.1]
		//   track 1 (targetID=2): keys at [0.0, 1.0]  → active [0.0, 1.0]
		//   track 2 (targetID=3): keys shifted by 0.1 → active [0.1, 1.1]
		// At t=0.5:
		//   track0 localT = 0.5-0.1=0.4 → alpha=0.4 → 4.0
		//   track1 localT = 0.5       → alpha=0.5 → 5.0
		//   track2 localT = 0.5-0.1=0.4 → alpha=0.4 → 4.0

		children := make([]Positioned, len(tracks))
		for i := range tracks {
			tr := tracks[i]
			children[i] = Positioned{
				At:    Position{Kind: PosAbs, Val: 0},
				Track: &tr,
			}
		}
		tl := Timeline{Children: children}
		cases = append(cases, goldenCase{
			Name:     "stagger",
			Tol:      1e-9,
			Timeline: tl,
			Samples:  sampleTimeline(&tl, []float64{0.5}),
		})
	}

	// -----------------------------------------------------------------------
	// 6. cubic_spline: vec3 glTF CUBICSPLINE track with in/out tangents
	// -----------------------------------------------------------------------
	{
		out0 := Vec3V(4, 0, -2) // left key out-tangent
		in1 := Vec3V(-1, 3, 5)  // right key in-tangent
		tl := Timeline{
			Children: []Positioned{
				{
					At: Position{Kind: PosAbs, Val: 0},
					Track: &Track{
						TargetID: 1,
						PropID:   0,
						Keys: []Key{
							{T: 0, Value: Vec3V(0, 0, 0), OutTangent: &out0},
							{T: 1, Value: Vec3V(10, 20, 30), InTangent: &in1},
						},
						Interp: InterpCubicSpline,
					},
				},
			},
		}
		cases = append(cases, goldenCase{
			Name:     "cubic_spline",
			Tol:      1e-9,
			Timeline: tl,
			Samples:  sampleTimeline(&tl, []float64{0, 0.25, 0.5, 0.75, 1.0}),
		})
	}

	return cases
}

// ---------------------------------------------------------------------------
// TestRegenerateGolden — env-gated; writes corpus JSON to testdata/golden/
// ---------------------------------------------------------------------------

func TestRegenerateGolden(t *testing.T) {
	if os.Getenv("MOTION_REGEN") == "" {
		t.Skip("set MOTION_REGEN=1 to regenerate golden corpus")
	}

	dir := filepath.Join("testdata", "golden")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	for _, c := range corpusCases() {
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatalf("marshal %s: %v", c.Name, err)
		}
		path := filepath.Join(dir, c.Name+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s", path)
	}
}

// ---------------------------------------------------------------------------
// TestGolden — always runs; verifies Eval reproduces every corpus sample
// ---------------------------------------------------------------------------

func TestGolden(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(matches) == 0 {
		t.Fatalf("golden corpus is empty — no *.json files found in %s; run MOTION_REGEN=1 go test ./motion/ -run TestRegenerateGolden to generate them", dir)
	}

	buf := NewWriteBuf(256)

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var c goldenCase
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}

		for _, s := range c.Samples {
			buf.Reset()
			Eval(&c.Timeline, s.T, Policy{}, buf)
			got := buf.Writes()

			if len(got) != len(s.Writes) {
				t.Errorf("[%s] t=%.6f: len mismatch: got %d floats, want %d; got=%v want=%v",
					c.Name, s.T, len(got), len(s.Writes), got, s.Writes)
				continue
			}
			for i, wantV := range s.Writes {
				gotV := got[i]
				if math.IsNaN(gotV) || math.IsNaN(wantV) || math.Abs(gotV-wantV) > c.Tol {
					t.Errorf("[%s] t=%.6f index %d: got %.15g, want %.15g (tol %.2g)",
						c.Name, s.T, i, gotV, wantV, c.Tol)
				}
			}
		}
	}

	t.Logf("verified %d golden corpus file(s)", len(matches))
}

// ---------------------------------------------------------------------------
// staggerDelayFor is a whitebox helper used by TestStaggerGoldenDelays to
// confirm the stagger delays embedded in the corpus match the formula.
// ---------------------------------------------------------------------------

func staggerDelayFor(n, i int, spec StaggerSpec) float64 {
	center := float64(n-1) / 2.0
	dist := math.Abs(float64(i) - center)
	return spec.Delay * dist
}

// TestStaggerGoldenDelays verifies stagger delays match the formula exactly.
func TestStaggerGoldenDelays(t *testing.T) {
	n := 3
	spec := StaggerSpec{From: FromCenter, Delay: 0.1}
	template := Track{
		Keys: []Key{
			{T: 0, Value: ScalarV(0)},
			{T: 1, Value: ScalarV(10)},
		},
		Interp: InterpLinear,
	}
	tracks := ExpandStagger(template, []int{1, 2, 3}, spec)
	for i, tr := range tracks {
		wantDelay := staggerDelayFor(n, i, spec)
		gotDelay := tr.Keys[0].T // first key T is 0 + delay
		if math.Abs(gotDelay-wantDelay) > 1e-12 {
			t.Errorf("track %d: delay=%.6f, want %.6f", i, gotDelay, wantDelay)
		}
		// Verify the corpus sample value at t=0.5 for tracks with non-zero delay.
		// localT = 0.5 - delay; alpha = (localT-0)/(1-0) = localT
		localT := 0.5 - wantDelay
		wantVal := localT * 10.0 // lerp 0→10 at alpha=localT
		buf := NewWriteBuf(16)
		tl := Timeline{Children: []Positioned{{At: Position{Kind: PosAbs, Val: 0}, Track: &tr}}}
		Eval(&tl, 0.5, Policy{}, buf)
		got := buf.Writes()
		if len(got) < 4 {
			t.Errorf("track %d: expected 4 floats, got %v", i, got)
			continue
		}
		gotVal := got[3]
		if math.Abs(gotVal-wantVal) > 1e-9 {
			fmt.Printf("track %d localT=%.4f wantVal=%.4f gotVal=%.4f\n", i, localT, wantVal, gotVal)
			t.Errorf("track %d at t=0.5: got %.9f, want %.9f", i, gotVal, wantVal)
		}
	}
}
