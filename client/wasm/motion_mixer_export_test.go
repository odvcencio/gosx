//go:build js && wasm

package main

import (
	"fmt"
	"math"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/motion"
)

// clipAJSON is a single rotation channel on node 0 that holds the identity quat.
const clipAJSON = `{"duration":1,"channels":[{"node":0,"property":"rotation","interpolation":"LINEAR","times":[0,1],"values":[0,0,0,1,0,0,0,1]}]}`

// clipBJSON is a single rotation channel on node 0 holding a 90-deg-about-Y quat.
// 90deg about Y = (0, sin45, 0, cos45) = (0, 0.70710678, 0, 0.70710678).
const clipBJSON = `{"duration":1,"channels":[{"node":0,"property":"rotation","interpolation":"LINEAR","times":[0,1],"values":[0,0.7071067811865476,0,0.7071067811865476,0,0.7071067811865476,0,0.7071067811865476]}]}`

// decodeMixerWrites reads the JS Uint8Array `out` (written by mixer_update) and
// decodes the first n float64s LE.
func decodeMixerWrites(out js.Value, n int) []float64 {
	b := make([]byte, out.Get("length").Int())
	js.CopyBytesToGo(b, out)
	return decodeLEFloat64s(b, n)
}

// referenceMixerBlended drives a motion.Mixer directly with the same A/B
// crossfade sequence and returns the blended packed writes — the oracle the
// WASM mixer path must reproduce.
func referenceMixerBlended() []float64 {
	m := motion.NewMixer()

	chA := []motion.ClipChannel{{
		Node: 0, Property: "rotation", Interp: "LINEAR",
		Times:  []float64{0, 1},
		Values: []float64{0, 0, 0, 1, 0, 0, 0, 1},
	}}
	tlA, durA := motion.BuildClipTimeline(chA)
	m.AddClip("A", tlA, durA)

	h := math.Sqrt2 / 2
	chB := []motion.ClipChannel{{
		Node: 0, Property: "rotation", Interp: "LINEAR",
		Times:  []float64{0, 1},
		Values: []float64{0, h, 0, h, 0, h, 0, h},
	}}
	tlB, durB := motion.BuildClipTimeline(chB)
	m.AddClip("B", tlB, durB)

	m.Play("A", motion.PlayOptions{})
	m.Play("B", motion.PlayOptions{FadeIn: 1})
	m.Stop("A", motion.StopOptions{FadeOut: 1})

	buf := motion.NewWriteBuf(64)
	m.Update(0.5, motion.Policy{}, buf)
	w := buf.Writes()
	out := make([]float64, len(w))
	copy(out, w)
	return out
}

// TestMixerExportCrossfade drives the WASM mixer glue: create, add two rotation
// clips via JSON, play A then crossfade to B while fading A out, update, and
// assert the decoded blended rotation matches a direct motion.Mixer run.
func TestMixerExportCrossfade(t *testing.T) {
	mh := mixerCreate(nil)
	if mh < 1 {
		t.Fatalf("mixerCreate handle=%d, want >= 1", mh)
	}

	if ok := mixerAddClip([]js.Value{
		js.ValueOf(mh), js.ValueOf("A"), js.ValueOf(clipAJSON),
	}); !ok {
		t.Fatal("mixerAddClip A returned false")
	}
	if ok := mixerAddClip([]js.Value{
		js.ValueOf(mh), js.ValueOf("B"), js.ValueOf(clipBJSON),
	}); !ok {
		t.Fatal("mixerAddClip B returned false")
	}

	// play("A"): fadeIn=0, loop=false, speed=1, weight=1.
	mixerPlay([]js.Value{
		js.ValueOf(mh), js.ValueOf("A"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})
	// play("B"): fadeIn=1.
	mixerPlay([]js.Value{
		js.ValueOf(mh), js.ValueOf("B"),
		js.ValueOf(1.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})
	// stop("A"): fadeOut=1.
	mixerStop([]js.Value{js.ValueOf(mh), js.ValueOf("A"), js.ValueOf(1.0)})

	if !mixerIsPlaying([]js.Value{js.ValueOf(mh), js.ValueOf("A")}) {
		t.Error("A should still be playing (fading out)")
	}
	if !mixerIsPlaying([]js.Value{js.ValueOf(mh), js.ValueOf("B")}) {
		t.Error("B should be playing")
	}

	const capFloats = 64
	f64 := js.Global().Get("Float64Array").New(capFloats)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))

	n := mixerUpdate([]js.Value{
		js.ValueOf(mh), js.ValueOf(0.5), js.ValueOf(false), out,
	})
	if n <= 0 {
		t.Fatalf("mixerUpdate n=%d, want > 0", n)
	}

	got := decodeMixerWrites(out, n)
	want := referenceMixerBlended()
	if n != len(want) {
		t.Fatalf("mixerUpdate n=%d, reference produced %d floats", n, len(want))
	}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Errorf("float %d: mixer=%v, reference=%v", i, got[i], want[i])
		}
	}

	// Sanity: the blended quat (last 4 floats) should be the slerp midpoint of
	// identity and 90deg-about-Y — i.e. 45deg about Y at the 50/50 blend point.
	// got layout per write: [tid, pid, arity, x, y, z, w]. One write expected.
	if n != 7 {
		t.Fatalf("expected one quat write (7 floats), got n=%d", n)
	}
	qx, qy, qz, qw := got[3], got[4], got[5], got[6]
	mag := math.Sqrt(qx*qx + qy*qy + qz*qz + qw*qw)
	if math.Abs(mag-1.0) > 1e-9 {
		t.Errorf("blended quat not unit length: |q|=%v", mag)
	}
	// 45deg about Y: y = sin(22.5deg), w = cos(22.5deg).
	wantY := math.Sin(math.Pi / 8)
	wantW := math.Cos(math.Pi / 8)
	if math.Abs(qy-wantY) > 1e-9 || math.Abs(qw-wantW) > 1e-9 {
		t.Errorf("blended quat = (%v,%v,%v,%v), want ~(0,%v,0,%v)", qx, qy, qz, qw, wantY, wantW)
	}
}

// TestMixerExportIsPlayingAndDestroy verifies is_playing tracks state and that
// destroy frees the handle (a later update returns 0, no panic).
func TestMixerExportIsPlayingAndDestroy(t *testing.T) {
	mh := mixerCreate(nil)
	if mh < 1 {
		t.Fatalf("mixerCreate handle=%d", mh)
	}
	if ok := mixerAddClip([]js.Value{
		js.ValueOf(mh), js.ValueOf("A"), js.ValueOf(clipAJSON),
	}); !ok {
		t.Fatal("add clip A failed")
	}

	if mixerIsPlaying([]js.Value{js.ValueOf(mh), js.ValueOf("A")}) {
		t.Error("A should not be playing before play()")
	}
	mixerPlay([]js.Value{
		js.ValueOf(mh), js.ValueOf("A"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})
	if !mixerIsPlaying([]js.Value{js.ValueOf(mh), js.ValueOf("A")}) {
		t.Error("A should be playing after play()")
	}

	// Destroy, then update on the freed handle must be a no-op (0, no panic).
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("update after destroy panicked: %v", r)
		}
	}()
	mixerDestroy([]js.Value{js.ValueOf(mh)})

	f64 := js.Global().Get("Float64Array").New(64)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))
	if n := mixerUpdate([]js.Value{js.ValueOf(mh), js.ValueOf(0.5), js.ValueOf(false), out}); n != 0 {
		t.Errorf("update after destroy = %d, want 0", n)
	}
	if mixerIsPlaying([]js.Value{js.ValueOf(mh), js.ValueOf("A")}) {
		t.Error("is_playing on destroyed handle should be false")
	}
}

// TestMixerExportBadClipJSON asserts malformed clip JSON yields add_clip=false
// with no panic.
func TestMixerExportBadClipJSON(t *testing.T) {
	mh := mixerCreate(nil)
	if mh < 1 {
		t.Fatalf("mixerCreate handle=%d", mh)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("bad clip JSON panicked: %v", r)
		}
	}()
	if ok := mixerAddClip([]js.Value{
		js.ValueOf(mh), js.ValueOf("X"), js.ValueOf("{not valid json"),
	}); ok {
		t.Error("mixerAddClip with bad JSON returned true, want false")
	}
	// Unknown mixer handle also returns false.
	if ok := mixerAddClip([]js.Value{
		js.ValueOf(999999), js.ValueOf("X"), js.ValueOf(clipAJSON),
	}); ok {
		t.Error("mixerAddClip on unknown handle returned true, want false")
	}
}

// TestMixerExportRegisteredOnGlobal verifies registration installs the mixer
// functions on the JS global.
func TestMixerExportRegisteredOnGlobal(t *testing.T) {
	registerMotionExports()
	for _, name := range []string{
		"__gosx_motion_mixer_create",
		"__gosx_motion_mixer_add_clip",
		"__gosx_motion_mixer_play",
		"__gosx_motion_mixer_stop",
		"__gosx_motion_mixer_update",
		"__gosx_motion_mixer_is_playing",
		"__gosx_motion_mixer_destroy",
	} {
		if fn := js.Global().Get(name); fn.Type() != js.TypeFunction {
			t.Errorf("%s not registered (type=%v)", name, fn.Type())
		}
	}

	mh := js.Global().Call("__gosx_motion_mixer_create").Int()
	if mh < 1 {
		t.Fatalf("global create handle=%d", mh)
	}
	if ok := js.Global().Call("__gosx_motion_mixer_add_clip",
		js.ValueOf(mh), js.ValueOf("A"), js.ValueOf(clipAJSON)).Bool(); !ok {
		t.Fatal("global add_clip returned false")
	}
	js.Global().Call("__gosx_motion_mixer_play",
		js.ValueOf(mh), js.ValueOf("A"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0))
	if !js.Global().Call("__gosx_motion_mixer_is_playing",
		js.ValueOf(mh), js.ValueOf("A")).Bool() {
		t.Error("global is_playing A = false, want true")
	}
	js.Global().Call("__gosx_motion_mixer_destroy", js.ValueOf(mh))
}

// mixerWrite is one decoded packed write from a mixer update: a
// [targetID, propID, arity] header followed by arity.Width() components.
type mixerWrite struct {
	target int
	prop   int
	arity  motion.ValueArity
	comps  []float64
}

// decodePackedWrites splits the flat LE float64 stream produced by mixerUpdate
// into per-write records. The packed layout is
// [targetID, propID, arity, comps…arity.Width()] per write. Bounds are checked
// before every index; a truncated stream yields an error instead of a panic.
func decodePackedWrites(vals []float64) ([]mixerWrite, error) {
	var out []mixerWrite
	for i := 0; i < len(vals); {
		if i+3 > len(vals) {
			return nil, fmt.Errorf("truncated write header at float %d", i)
		}
		w := mixerWrite{
			target: int(vals[i]),
			prop:   int(vals[i+1]),
			arity:  motion.ValueArity(int(vals[i+2])),
		}
		width := w.arity.Width()
		if i+3+width > len(vals) {
			return nil, fmt.Errorf("truncated body at float %d (arity %v, width %d)", i, vals[i+2], width)
		}
		w.comps = make([]float64, width)
		copy(w.comps, vals[i+3:i+3+width])
		out = append(out, w)
		i += 3 + width
	}
	return out, nil
}

// writesByKey indexes decoded writes by (target, prop), failing on duplicates.
func writesByKey(t *testing.T, ws []mixerWrite) map[[2]int]mixerWrite {
	t.Helper()
	m := make(map[[2]int]mixerWrite, len(ws))
	for _, w := range ws {
		k := [2]int{w.target, w.prop}
		if _, dup := m[k]; dup {
			t.Fatalf("duplicate write for target=%d prop=%d", w.target, w.prop)
		}
		m[k] = w
	}
	return m
}

// assertWrite looks up the write for (target, prop) and asserts its arity,
// component width, and values. Callers never assume output order: they index
// by (target, prop) and assert the total write count so stray writes fail.
func assertWrite(t *testing.T, byKey map[[2]int]mixerWrite, target, prop int, wantArity motion.ValueArity, wantComps []float64) {
	t.Helper()
	w, ok := byKey[[2]int{target, prop}]
	if !ok {
		t.Fatalf("no write for target=%d prop=%d; decoded writes: %v", target, prop, byKey)
	}
	if w.arity != wantArity {
		t.Errorf("write (target=%d prop=%d) arity=%d, want %d", target, prop, w.arity, wantArity)
	}
	if len(w.comps) != len(wantComps) {
		t.Fatalf("write (target=%d prop=%d) has %d components, want %d", target, prop, len(w.comps), len(wantComps))
	}
	for i := range wantComps {
		if diff := w.comps[i] - wantComps[i]; math.IsNaN(diff) || math.Abs(diff) > 1e-9 {
			t.Errorf("write (target=%d prop=%d) comp[%d]=%v, want %v", target, prop, i, w.comps[i], wantComps[i])
		}
	}
}

// updateAndDecode runs one mixerUpdate on the handle and decodes the packed
// writes read back from the JS Uint8Array as LE float64s. The buffer is sized
// for the small clips these tests register; an update that would overflow it
// fails the test rather than silently decoding trailing zeros.
func updateAndDecode(t *testing.T, mh int, dt float64) []mixerWrite {
	t.Helper()
	const capFloats = 256
	f64 := js.Global().Get("Float64Array").New(capFloats)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))
	n := mixerUpdate([]js.Value{js.ValueOf(mh), js.ValueOf(dt), js.ValueOf(false), out})
	if n <= 0 {
		t.Fatalf("mixerUpdate n=%d, want > 0", n)
	}
	if n > capFloats {
		t.Fatalf("mixerUpdate n=%d exceeds buffer capacity %d", n, capFloats)
	}
	ws, err := decodePackedWrites(decodeMixerWrites(out, n))
	if err != nil {
		t.Fatalf("decodePackedWrites: %v", err)
	}
	return ws
}

// TestMixerExportWeightsLinearJSON drives a five-weight (>4) LINEAR weights
// clip — with negative and >1 components, so clamping or vec4 guessing would
// corrupt them — through the full WASM path: clip JSON → native scalar tracks →
// mixerUpdate → LE float64 Uint8Array. At t=0.5 LINEAR blending is the
// per-component mean of the two keys: [2.5, -2.5, 4.5, 2.5, 6.5].
func TestMixerExportWeightsLinearJSON(t *testing.T) {
	mh := mixerCreate(nil)
	if mh < 1 {
		t.Fatalf("mixerCreate handle=%d, want >= 1", mh)
	}
	defer mixerDestroy([]js.Value{js.ValueOf(mh)})

	const clipWJSON = `{"duration":1,"channels":[{"node":7,"property":"weights","weightCount":5,"interpolation":"LINEAR","times":[0,1],"values":[0,1,2,-3,4,5,-6,7,8,9]}]}`
	if ok := mixerAddClip([]js.Value{js.ValueOf(mh), js.ValueOf("W"), js.ValueOf(clipWJSON)}); !ok {
		t.Fatal("mixerAddClip weights clip returned false")
	}
	mixerPlay([]js.Value{
		js.ValueOf(mh), js.ValueOf("W"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})

	byKey := writesByKey(t, updateAndDecode(t, mh, 0.5))
	if len(byKey) != 5 {
		t.Fatalf("got %d distinct (target,prop) writes, want exactly 5 (no strays): %v", len(byKey), byKey)
	}
	// Stable scalar morph IDs: PropID = 1000+j for weight j of node 7.
	for j, want := range []float64{2.5, -2.5, 4.5, 2.5, 6.5} {
		assertWrite(t, byKey, 7, 1000+j, motion.ArityScalar, []float64{want})
	}
}

// TestMixerExportWeightsStepAndCubicJSON covers STEP hold semantics and
// CUBICSPLINE over a non-unit key interval with distinct in/value/out vectors.
// Expected values are hand-computed from the glTF interpolation definitions —
// an independent oracle, not the implementation under test.
func TestMixerExportWeightsStepAndCubicJSON(t *testing.T) {
	// STEP clip: two weights, three keys. STEP holds the previous key's value:
	// t=0.75 → key0 = [1, 2]; t=1.5 → key1 = [3, 4].
	const clipStepJSON = `{"duration":2,"channels":[{"node":4,"property":"weights","weightCount":2,"interpolation":"STEP","times":[0,1,2],"values":[1,2,3,4,-5,6]}]}`

	sh := mixerCreate(nil)
	if sh < 1 {
		t.Fatalf("mixerCreate handle=%d, want >= 1", sh)
	}
	defer mixerDestroy([]js.Value{js.ValueOf(sh)})
	if ok := mixerAddClip([]js.Value{js.ValueOf(sh), js.ValueOf("S"), js.ValueOf(clipStepJSON)}); !ok {
		t.Fatal("mixerAddClip STEP clip returned false")
	}
	mixerPlay([]js.Value{
		js.ValueOf(sh), js.ValueOf("S"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})

	byKey := writesByKey(t, updateAndDecode(t, sh, 0.75))
	if len(byKey) != 2 {
		t.Fatalf("STEP t=0.75: got %d writes, want 2: %v", len(byKey), byKey)
	}
	assertWrite(t, byKey, 4, 1000, motion.ArityScalar, []float64{1})
	assertWrite(t, byKey, 4, 1001, motion.ArityScalar, []float64{2})

	byKey = writesByKey(t, updateAndDecode(t, sh, 0.75))
	if len(byKey) != 2 {
		t.Fatalf("STEP t=1.5: got %d writes, want 2: %v", len(byKey), byKey)
	}
	assertWrite(t, byKey, 4, 1000, motion.ArityScalar, []float64{3})
	assertWrite(t, byKey, 4, 1001, motion.ArityScalar, []float64{4})

	// CUBICSPLINE clip: one weight, keys at t=0 and t=2 (delta=2, non-unit),
	// per-key [inTangent, value, outTangent] triplets with distinct vectors:
	// key0 = (10, 1, 20), key1 = (30, 5, 40).
	const clipCubicJSON = `{"duration":2,"channels":[{"node":3,"property":"weights","weightCount":1,"interpolation":"CUBICSPLINE","times":[0,2],"values":[10,1,20,30,5,40]}]}`

	ch := mixerCreate(nil)
	if ch < 1 {
		t.Fatalf("mixerCreate handle=%d, want >= 1", ch)
	}
	defer mixerDestroy([]js.Value{js.ValueOf(ch)})
	if ok := mixerAddClip([]js.Value{js.ValueOf(ch), js.ValueOf("C"), js.ValueOf(clipCubicJSON)}); !ok {
		t.Fatal("mixerAddClip CUBICSPLINE clip returned false")
	}
	mixerPlay([]js.Value{
		js.ValueOf(ch), js.ValueOf("C"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})

	byKey = writesByKey(t, updateAndDecode(t, ch, 0.5))
	if len(byKey) != 1 {
		t.Fatalf("CUBICSPLINE: got %d writes, want 1: %v", len(byKey), byKey)
	}

	// Independent oracle — the glTF CUBICSPLINE Hermite evaluated by hand at
	// t=0.5: delta=2, s=0.25;
	// p = h00*v0 + delta*h10*b0 + h01*v1 + delta*h11*a1
	//   = 0.84375*1 + 2*0.140625*20 + 0.15625*5 + 2*(-0.046875)*30 = 4.4375.
	delta, s := 2.0, 0.25
	s2, s3 := s*s, s*s*s
	h00, h10 := 2*s3-3*s2+1, s3-2*s2+s
	h01, h11 := -2*s3+3*s2, s3-s2
	wantCubic := h00*1 + delta*h10*20 + h01*5 + delta*h11*30
	assertWrite(t, byKey, 3, 1000, motion.ArityScalar, []float64{wantCubic})

	// Additional native cross-check (secondary oracle): a direct motion.Mixer
	// fed the same ClipChannel must produce the same packed write.
	ref := motion.NewMixer()
	tlC, durC := motion.BuildClipTimeline([]motion.ClipChannel{{
		Node: 3, Property: "weights", Interp: "CUBICSPLINE", WeightCount: 1,
		Times: []float64{0, 2}, Values: []float64{10, 1, 20, 30, 5, 40},
	}})
	ref.AddClip("C", tlC, durC)
	ref.Play("C", motion.PlayOptions{})
	refBuf := motion.NewWriteBuf(16)
	ref.Update(0.5, motion.Policy{}, refBuf)
	refWS, err := decodePackedWrites(refBuf.Writes())
	if err != nil {
		t.Fatalf("decodePackedWrites(reference): %v", err)
	}
	assertWrite(t, writesByKey(t, refWS), 3, 1000, motion.ArityScalar, []float64{wantCubic})
}

// TestMixerExportMixedTRSAndWeightsJSON registers one clip holding a vec3
// translation, a quat rotation, and a two-weight scalar channel. The packed
// write order is not assumed: writes are indexed by (target, prop) and the
// total count is asserted so stray writes fail.
func TestMixerExportMixedTRSAndWeightsJSON(t *testing.T) {
	mh := mixerCreate(nil)
	if mh < 1 {
		t.Fatalf("mixerCreate handle=%d, want >= 1", mh)
	}
	defer mixerDestroy([]js.Value{js.ValueOf(mh)})

	const clipMixedJSON = `{"duration":1,"channels":[` +
		`{"node":0,"property":"translation","interpolation":"LINEAR","times":[0,1],"values":[1,2,3,1,2,3]},` +
		`{"node":0,"property":"weights","weightCount":2,"interpolation":"LINEAR","times":[0,1],"values":[0.25,-1.5,1.75,2.5]},` +
		`{"node":1,"property":"rotation","interpolation":"LINEAR","times":[0,1],"values":[0,0,0,1,0,0,0,1]}` +
		`]}`
	if ok := mixerAddClip([]js.Value{js.ValueOf(mh), js.ValueOf("M"), js.ValueOf(clipMixedJSON)}); !ok {
		t.Fatal("mixerAddClip mixed clip returned false")
	}
	mixerPlay([]js.Value{
		js.ValueOf(mh), js.ValueOf("M"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})

	byKey := writesByKey(t, updateAndDecode(t, mh, 0.5))
	if len(byKey) != 4 {
		t.Fatalf("got %d distinct (target,prop) writes, want exactly 4 (no strays): %v", len(byKey), byKey)
	}
	// At t=0.5: translation is constant [1,2,3]; the weights blend to [1.0] and
	// [0.5]; the slerp of two identity quats is [0,0,0,1]. TRS props keep their
	// fixed PropIDs (translation=0, rotation=1); weights use 1000+j.
	assertWrite(t, byKey, 0, 0, motion.ArityVec3, []float64{1, 2, 3})
	assertWrite(t, byKey, 0, 1000, motion.ArityScalar, []float64{1.0})
	assertWrite(t, byKey, 0, 1001, motion.ArityScalar, []float64{0.5})
	assertWrite(t, byKey, 1, 1, motion.ArityQuat, []float64{0, 0, 0, 1})
}

// TestMixerExportMalformedWeightsRejected asserts that malformed weightCount or
// per-key layout rejects weights-only clips (add_clip=false — a missing
// weightCount is never guessed as vec4), and that a malformed weights channel
// inside a mixed clip is skipped without dropping its valid TRS sibling.
func TestMixerExportMalformedWeightsRejected(t *testing.T) {
	mh := mixerCreate(nil)
	if mh < 1 {
		t.Fatalf("mixerCreate handle=%d, want >= 1", mh)
	}
	defer mixerDestroy([]js.Value{js.ValueOf(mh)})

	cases := []struct{ name, clip string }{
		{"missing weightCount not guessed as vec4",
			`{"duration":1,"channels":[{"node":0,"property":"weights","interpolation":"LINEAR","times":[0,1],"values":[1,2,3,4,5,6,7,8]}]}`},
		{"zero weightCount",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":0,"interpolation":"LINEAR","times":[0,1],"values":[1,2,3,4]}]}`},
		{"negative weightCount",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":-2,"interpolation":"LINEAR","times":[0,1],"values":[1,2,3,4]}]}`},
		{"per-key width below weightCount",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":4,"interpolation":"LINEAR","times":[0,1],"values":[1,2,3,4,5,6]}]}`},
		{"per-key width above weightCount",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":2,"interpolation":"LINEAR","times":[0,1],"values":[1,2,3,4,5,6,7,8]}]}`},
		{"truncated keys",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":2,"interpolation":"LINEAR","times":[0,1],"values":[1,2,3]}]}`},
		{"cubic triplet width mismatch",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":3,"interpolation":"CUBICSPLINE","times":[0,1],"values":[1,2,3,4,5,6,7,8,9,10,11,12]}]}`},
		{"no keys",
			`{"duration":1,"channels":[{"node":0,"property":"weights","weightCount":2,"interpolation":"LINEAR","times":[],"values":[]}]}`},
	}
	for _, tc := range cases {
		if ok := mixerAddClip([]js.Value{js.ValueOf(mh), js.ValueOf(tc.name), js.ValueOf(tc.clip)}); ok {
			t.Errorf("%s: weights-only clip accepted, want rejection (add_clip=false)", tc.name)
		}
	}

	// A valid TRS sibling must survive a malformed weights channel in the same
	// clip: the weights channel is skipped, the translation track is kept.
	const mixedBadWeights = `{"duration":1,"channels":[` +
		`{"node":0,"property":"weights","weightCount":4,"interpolation":"LINEAR","times":[0,1],"values":[1,2]},` +
		`{"node":2,"property":"translation","interpolation":"LINEAR","times":[0,1],"values":[7,8,9,7,8,9]}` +
		`]}`
	if ok := mixerAddClip([]js.Value{js.ValueOf(mh), js.ValueOf("mixedBad"), js.ValueOf(mixedBadWeights)}); !ok {
		t.Fatal("mixed clip with valid TRS sibling rejected, want acceptance")
	}
	mixerPlay([]js.Value{
		js.ValueOf(mh), js.ValueOf("mixedBad"),
		js.ValueOf(0.0), js.ValueOf(false), js.ValueOf(1.0), js.ValueOf(1.0),
	})

	byKey := writesByKey(t, updateAndDecode(t, mh, 0.5))
	if len(byKey) != 1 {
		t.Fatalf("got %d writes, want exactly the 1 surviving TRS write (no weight strays): %v", len(byKey), byKey)
	}
	assertWrite(t, byKey, 2, 0, motion.ArityVec3, []float64{7, 8, 9})
}
