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

func valuePtrEqual(a, b *Value) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return valuesEqual(*a, *b)
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
	if a.OscArity != b.OscArity || a.OscBase != b.OscBase || a.OscAmp != b.OscAmp || a.OscFreq != b.OscFreq || a.OscPhase != b.OscPhase {
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
		if !valuePtrEqual(ka.InTangent, kb.InTangent) || !valuePtrEqual(ka.OutTangent, kb.OutTangent) {
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

// TestWireCubicSplineRoundTrip: a cubicspline track with in/out tangents must
// survive an encode/decode round-trip (MOT2).
func TestWireCubicSplineRoundTrip(t *testing.T) {
	in0 := Value{Arity: ArityVec3, F: [4]float64{0.1, 0.2, 0.3, 0}}
	out0 := Value{Arity: ArityVec3, F: [4]float64{0.4, 0.5, 0.6, 0}}
	in1 := Value{Arity: ArityVec3, F: [4]float64{-1, -2, -3, 0}}
	out1 := Value{Arity: ArityVec3, F: [4]float64{7, 8, 9, 0}}

	tl := &Timeline{
		ID: "cubic",
		Children: []Positioned{
			{
				At: Position{Kind: PosAbs, Val: 0},
				Track: &Track{
					Target: Target{Kind: TargetSceneNode, Ref: "n"},
					Prop:   "position",
					Keys: []Key{
						{T: 0, Value: Vec3V(0, 0, 0), OutTangent: &out0, InTangent: &in0},
						{T: 1, Value: Vec3V(10, 20, 30), OutTangent: &out1, InTangent: &in1},
					},
					Interp: InterpCubicSpline,
				},
			},
		},
		Speed: 1,
	}

	blob := EncodeProgram(tl, nil, nil)
	// Verify it really used the current wire magic (MOT3).
	if string(blob[:4]) != "MOT3" {
		t.Fatalf("expected MOT3 magic, got %q", string(blob[:4]))
	}

	got, _, _, err := DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	if !timelinesEqual(tl, got) {
		t.Fatalf("cubicspline round-trip mismatch:\n orig=%+v\n  got=%+v", tl, got)
	}
	gk := got.Children[0].Track.Keys[0]
	if gk.InTangent == nil || gk.OutTangent == nil {
		t.Fatal("tangents lost on round-trip")
	}
}

// TestWireMOT1BackCompat: a legacy "MOT1" blob (no tangent fields) must still
// decode, yielding keys with nil tangents.
func TestWireMOT1BackCompat(t *testing.T) {
	// Build a MOT1 blob by hand using the legacy per-key layout: the encoder
	// here mirrors putKey WITHOUT the two trailing tangent presence bytes.
	b := make([]byte, 0, 128)
	b = append(b, 'M', 'O', 'T', '1')
	b = putStringList(b, nil) // targetRefs
	b = putStringList(b, nil) // propRefs
	// timeline:
	b = putString(b, "legacy") // ID
	b = putU32(b, 1)           // 1 child
	// positioned:
	b = putPosition(b, Position{Kind: PosAbs, Val: 0})
	b = putU8(b, 0) // disc = Track
	// track:
	b = putTarget(b, Target{Kind: TargetSceneNode, Ref: "n"})
	b = putString(b, "x") // prop
	b = putU32(b, 1)      // 1 key
	// legacy key (MOT1: T, Value, ease-presence only — NO tangent bytes):
	b = putF64(b, 0)
	b = putValue(b, ScalarV(42))
	b = putBool(b, false) // ease absent
	// end key
	b = putGenerator(b, nil)
	b = putU8(b, uint8(InterpLinear))
	b = putEase(b, Ease{Kind: EaseLinear})
	b = putI32(b, 0) // targetID
	b = putI32(b, 0) // propID
	// timeline tail:
	b = putI32(b, 0)      // loop
	b = putBool(b, false) // alternate
	b = putF64(b, 1)      // speed
	b = putBool(b, false) // autoplay

	got, _, _, err := DecodeProgram(b)
	if err != nil {
		t.Fatalf("MOT1 decode error: %v", err)
	}
	if got.ID != "legacy" || len(got.Children) != 1 {
		t.Fatalf("MOT1 decode shape wrong: %+v", got)
	}
	k := got.Children[0].Track.Keys[0]
	if !valuesEqual(k.Value, ScalarV(42)) {
		t.Errorf("MOT1 key value = %+v, want ScalarV(42)", k.Value)
	}
	if k.InTangent != nil || k.OutTangent != nil {
		t.Errorf("MOT1 key should have nil tangents, got in=%v out=%v", k.InTangent, k.OutTangent)
	}
}

func TestWireNilPositionedNoPanic(t *testing.T) {
	// A Positioned with neither Track nor Sub set must not panic in EncodeProgram,
	// and DecodeProgram must round-trip to a Positioned with a non-nil zero Track.
	tl := &Timeline{
		Children: []Positioned{
			{At: Position{Kind: PosAbs}},
		},
	}
	var blob []byte
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("EncodeProgram panicked on nil Track+Sub Positioned: %v", r)
			}
		}()
		blob = EncodeProgram(tl, nil, nil)
	}()

	got, _, _, err := DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}
	if len(got.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(got.Children))
	}
	if got.Children[0].Track == nil {
		t.Fatal("expected decoded Positioned to have non-nil (zero) Track")
	}
	if got.Children[0].Sub != nil {
		t.Fatal("expected decoded Positioned to have nil Sub")
	}
}

// TestWireTargetMaterialRoundTrip: a TargetMaterial track with a Color keyframe
// survives EncodeProgram/DecodeProgram with Kind preserved.
func TestWireTargetMaterialRoundTrip(t *testing.T) {
	matTrack := &Track{
		Target: Target{Kind: TargetMaterial, Ref: "mat1"},
		Prop:   "emissive",
		Keys: []Key{
			{T: 0, Value: Value{Arity: ArityColor, F: [4]float64{0, 0, 0, 1}}},
			{T: 1, Value: Value{Arity: ArityColor, F: [4]float64{1, 0.5, 0.25, 1}}},
		},
		Interp:   InterpLinear,
		Ease:     Ease{Kind: EaseLinear},
		TargetID: 0,
		PropID:   0,
	}
	tl := &Timeline{
		ID: "mat-tl",
		Children: []Positioned{
			{At: Position{Kind: PosAbs, Val: 0}, Track: matTrack},
		},
		Speed: 1,
	}
	targetRefs := []string{"mat1"}
	propRefs := []string{"emissive"}

	blob := EncodeProgram(tl, targetRefs, propRefs)
	gotTL, gotTargetRefs, gotPropRefs, err := DecodeProgram(blob)
	if err != nil {
		t.Fatalf("DecodeProgram error: %v", err)
	}

	// Check ref tables survive.
	if len(gotTargetRefs) != 1 || gotTargetRefs[0] != "mat1" {
		t.Errorf("targetRefs: got %v, want [mat1]", gotTargetRefs)
	}
	if len(gotPropRefs) != 1 || gotPropRefs[0] != "emissive" {
		t.Errorf("propRefs: got %v, want [emissive]", gotPropRefs)
	}

	// Check the track survived.
	if len(gotTL.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(gotTL.Children))
	}
	tr := gotTL.Children[0].Track
	if tr == nil {
		t.Fatal("decoded track is nil")
	}
	// TargetMaterial Kind must survive the round-trip.
	if tr.Target.Kind != TargetMaterial {
		t.Errorf("Target.Kind: got %d, want TargetMaterial (%d)", tr.Target.Kind, TargetMaterial)
	}
	if tr.Target.Ref != "mat1" {
		t.Errorf("Target.Ref: got %q, want %q", tr.Target.Ref, "mat1")
	}
	if tr.Prop != "emissive" {
		t.Errorf("Prop: got %q, want %q", tr.Prop, "emissive")
	}
	// 2 keyframes with ArityColor preserved.
	if len(tr.Keys) != 2 {
		t.Fatalf("Keys len: got %d, want 2", len(tr.Keys))
	}
	if tr.Keys[0].Value.Arity != ArityColor {
		t.Errorf("key[0].Arity: got %v, want ArityColor", tr.Keys[0].Value.Arity)
	}
	if tr.Keys[1].Value.Arity != ArityColor {
		t.Errorf("key[1].Arity: got %v, want ArityColor", tr.Keys[1].Value.Arity)
	}
}
