package motion

import (
	"bytes"
	"testing"
)

// sampleProgram builds a representative timeline exercising every wire field:
// a keyframe Track (2 keys, per-key Ease on one key, track-level Ease with Args,
// Interp, TargetID/PropID), a generator Track (GenSpring with Base/Spring), and a
// nested Sub timeline containing one Track.
func sampleProgram() *Timeline {
	keyTrack := &Track{
		Target: Target{Kind: TargetSceneNode, Ref: "node-a"},
		Prop:   "position",
		Keys: []Key{
			{
				T:     0,
				Value: Value{Arity: ArityVec3, F: [4]float64{1, 2, 3, 0}},
				Ease:  nil, // optional ease absent
			},
			{
				T:     1.5,
				Value: Value{Arity: ArityVec3, F: [4]float64{4, 5, 6, 0}},
				Ease:  &Ease{Kind: EaseCubicBezier, Args: []float64{0.25, 0.1, 0.25, 1.0}},
			},
		},
		Gen:      nil, // optional generator absent
		Interp:   InterpCubicSpline,
		Ease:     Ease{Kind: EaseInOutPow, Args: []float64{3}},
		TargetID: 7,
		PropID:   2,
	}

	genTrack := &Track{
		Target: Target{Kind: TargetDOM, Ref: "#box"},
		Prop:   "scale",
		Keys:   nil,
		Gen: &Generator{
			Kind: GenSpring,
			Base: Value{Arity: ArityScalar, F: [4]float64{0.5, 0, 0, 0}},
			Spin: [3]float64{0.1, 0.2, 0.3},
			Spring: Spring{
				Mass:      2,
				Stiffness: 150,
				Damping:   12,
				Velocity:  -1,
			},
			Drift:      [3]float64{1, 2, 3},
			DriftSpeed: [3]float64{4, 5, 6},
			DriftPhase: [3]float64{7, 8, 9},
		},
		Interp:   InterpLinear,
		Ease:     Ease{Kind: EaseLinear, Args: nil},
		TargetID: 3,
		PropID:   1,
	}

	subTrack := &Track{
		Target:   Target{Kind: TargetCSSVar, Ref: "--accent"},
		Prop:     "color",
		Keys:     []Key{{T: 0.25, Value: Value{Arity: ArityColor, F: [4]float64{1, 0, 0, 1}}, Ease: &Ease{Kind: EaseSteps, Args: []float64{4}}}},
		Gen:      nil,
		Interp:   InterpStep,
		Ease:     Ease{Kind: EaseOutBack, Args: []float64{1.70158}},
		TargetID: 9,
		PropID:   5,
	}

	sub := &Timeline{
		ID: "sub-timeline",
		Children: []Positioned{
			{
				At:    Position{Kind: PosRel, Val: 0.5, Label: ""},
				Track: subTrack,
			},
		},
		Loop:      2,
		Alternate: true,
		Speed:     0.75,
		Autoplay:  false,
	}

	root := &Timeline{
		ID: "root",
		Children: []Positioned{
			{At: Position{Kind: PosAbs, Val: 0, Label: "start"}, Track: keyTrack},
			{At: Position{Kind: PosLabel, Val: 0, Label: "anchor"}, Track: genTrack},
			{At: Position{Kind: PosPrevRel, Val: 1.0, Label: ""}, Sub: sub},
		},
		Loop:      -1,
		Alternate: false,
		Speed:     1,
		Autoplay:  true,
	}
	return root
}

func floatsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func valuesEqual(a, b Value) bool {
	if a.Arity != b.Arity {
		return false
	}
	for i := 0; i < 4; i++ {
		if a.F[i] != b.F[i] {
			return false
		}
	}
	return true
}

func easesEqual(a, b Ease) bool {
	return a.Kind == b.Kind && floatsEqual(a.Args, b.Args)
}

func easePtrEqual(a, b *Ease) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return easesEqual(*a, *b)
}

func generatorsEqual(a, b *Generator) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	if a.Kind != b.Kind {
		return false
	}
	if !valuesEqual(a.Base, b.Base) {
		return false
	}
	if a.Spin != b.Spin || a.Drift != b.Drift || a.DriftSpeed != b.DriftSpeed || a.DriftPhase != b.DriftPhase {
		return false
	}
	return a.Spring == b.Spring
}

func tracksEqual(a, b *Track) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	if a.Target != b.Target {
		return false
	}
	if a.Prop != b.Prop {
		return false
	}
	if len(a.Keys) != len(b.Keys) {
		return false
	}
	for i := range a.Keys {
		ka, kb := a.Keys[i], b.Keys[i]
		if ka.T != kb.T || !valuesEqual(ka.Value, kb.Value) || !easePtrEqual(ka.Ease, kb.Ease) {
			return false
		}
	}
	if !generatorsEqual(a.Gen, b.Gen) {
		return false
	}
	if a.Interp != b.Interp {
		return false
	}
	if !easesEqual(a.Ease, b.Ease) {
		return false
	}
	return a.TargetID == b.TargetID && a.PropID == b.PropID
}

func timelinesEqual(a, b *Timeline) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	if a.ID != b.ID {
		return false
	}
	if a.Loop != b.Loop || a.Alternate != b.Alternate || a.Speed != b.Speed || a.Autoplay != b.Autoplay {
		return false
	}
	if len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		ca, cb := a.Children[i], b.Children[i]
		if ca.At != cb.At {
			return false
		}
		if !tracksEqual(ca.Track, cb.Track) {
			return false
		}
		if !timelinesEqual(ca.Sub, cb.Sub) {
			return false
		}
	}
	return true
}

func TestWireRoundTrip(t *testing.T) {
	tl := sampleProgram()
	targetRefs := []string{"a", "b"}
	propRefs := []string{"position"}

	blob := EncodeProgram(tl, targetRefs, propRefs)
	if len(blob) == 0 {
		t.Fatal("EncodeProgram returned empty blob")
	}

	got, gotTargets, gotProps, err := DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}

	if !timelinesEqual(tl, got) {
		t.Fatalf("round-trip mismatch:\n orig=%+v\n  got=%+v", tl, got)
	}

	if !equalStrings(targetRefs, gotTargets) {
		t.Fatalf("targetRefs mismatch: want %v got %v", targetRefs, gotTargets)
	}
	if !equalStrings(propRefs, gotProps) {
		t.Fatalf("propRefs mismatch: want %v got %v", propRefs, gotProps)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWireDeterminism(t *testing.T) {
	tl := sampleProgram()
	targetRefs := []string{"a", "b"}
	propRefs := []string{"position"}

	b1 := EncodeProgram(tl, targetRefs, propRefs)
	b2 := EncodeProgram(tl, targetRefs, propRefs)
	if !bytes.Equal(b1, b2) {
		t.Fatalf("encode not deterministic: len %d vs %d", len(b1), len(b2))
	}
}

func TestWireBadInput(t *testing.T) {
	if _, _, _, err := DecodeProgram([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short/bad-magic input, got nil")
	}
	if _, _, _, err := DecodeProgram(nil); err == nil {
		t.Fatal("expected error for nil input, got nil")
	}
	// good magic but truncated body
	if _, _, _, err := DecodeProgram([]byte{'M', 'O', 'T', '1'}); err == nil {
		t.Fatal("expected error for truncated body, got nil")
	}
	// wrong magic, correct length
	if _, _, _, err := DecodeProgram([]byte{'X', 'X', 'X', 'X', 0, 0, 0, 0}); err == nil {
		t.Fatal("expected error for bad magic, got nil")
	}
}

func TestWireBadInputNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DecodeProgram panicked on bad input: %v", r)
		}
	}()
	// Feed a valid prefix then truncate at every length to ensure no panic.
	full := EncodeProgram(sampleProgram(), []string{"a", "b"}, []string{"position"})
	for i := 0; i <= len(full); i++ {
		_, _, _, _ = DecodeProgram(full[:i])
	}
	// Random garbage of various lengths.
	for n := 0; n < 64; n++ {
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = byte(i*7 + n)
		}
		_, _, _, _ = DecodeProgram(buf)
	}
}

func TestWireOptionalFields(t *testing.T) {
	// Track with Gen==nil and a Key with Ease==nil, plus an empty timeline.
	tl := &Timeline{
		ID: "opt",
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					Target: Target{Kind: TargetSceneNode, Ref: "n"},
					Prop:   "x",
					Keys: []Key{
						{T: 0, Value: Value{Arity: ArityScalar, F: [4]float64{0, 0, 0, 0}}, Ease: nil},
					},
					Gen:    nil,
					Interp: InterpLinear,
					Ease:   Ease{Kind: EaseLinear},
				},
			},
		},
		Speed: 1,
	}
	blob := EncodeProgram(tl, nil, nil)
	got, gotT, gotP, err := DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	if !timelinesEqual(tl, got) {
		t.Fatalf("optional-field round-trip mismatch:\n orig=%+v\n  got=%+v", tl, got)
	}
	if len(gotT) != 0 || len(gotP) != 0 {
		t.Fatalf("expected empty ref tables, got %v / %v", gotT, gotP)
	}
}

func TestWireEmptyTimeline(t *testing.T) {
	tl := &Timeline{ID: "", Children: nil, Speed: 0}
	blob := EncodeProgram(tl, nil, nil)
	got, _, _, err := DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	if !timelinesEqual(tl, got) {
		t.Fatalf("empty timeline round-trip mismatch:\n orig=%+v\n  got=%+v", tl, got)
	}
}
