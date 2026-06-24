//go:build js && wasm

package main

import (
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
