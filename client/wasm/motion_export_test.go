//go:build js && wasm

package main

import (
	"encoding/binary"
	"math"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/motion"
)

// buildMotionTestProgram builds a minimal vec3 keyframe timeline (TargetID=0,
// PropID=0) animating position from origin to (10,0,0) over one second, encodes
// it to the wire format, and returns both the blob and the source timeline so
// the test can compare the WASM export output against a direct motion.Eval.
func buildMotionTestProgram() (blob []byte, tl *motion.Timeline) {
	tl = &motion.Timeline{
		ID: "motion-export-test",
		Children: []motion.Positioned{
			{
				At: motion.Position{Kind: motion.PosAbs, Val: 0},
				Track: &motion.Track{
					TargetID: 0,
					PropID:   0,
					Keys: []motion.Key{
						{T: 0, Value: motion.Vec3V(0, 0, 0)},
						{T: 1, Value: motion.Vec3V(10, 0, 0)},
					},
					Interp: motion.InterpLinear,
				},
			},
		},
	}
	blob = motion.EncodeProgram(tl, []string{"mesh0"}, []string{"position"})
	return blob, tl
}

// jsUint8ArrayFromGo materializes a JS Uint8Array initialized from a Go byte slice.
func jsUint8ArrayFromGo(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}

// decodeLEFloat64s reads n little-endian float64s from the front of b.
func decodeLEFloat64s(b []byte, n int) []float64 {
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[i*8:]))
	}
	return out
}

// referenceWrites runs a direct motion.Eval on tl at time t and returns the
// packed float64 writes — the oracle the WASM tick path must reproduce exactly.
func referenceWrites(tl *motion.Timeline, t float64, reduced bool) []float64 {
	buf := motion.NewWriteBuf(64)
	motion.Eval(tl, t, motion.Policy{ReducedMotion: reduced}, buf)
	w := buf.Writes()
	// Copy out — Writes() is a view into the buffer.
	out := make([]float64, len(w))
	copy(out, w)
	return out
}

// TestMotionExportLoadTickRoundTrip drives the WASM glue end to end: a JS
// Uint8Array program in → __gosx_motion_load → __gosx_motion_tick → JS bytes out,
// decoded LE and asserted equal to a direct motion.Eval reference.
func TestMotionExportLoadTickRoundTrip(t *testing.T) {
	blob, tl := buildMotionTestProgram()

	// Load via the handler with a faked args slice.
	jsBytes := jsUint8ArrayFromGo(blob)
	handle := motionLoad([]js.Value{jsBytes})
	if handle < 1 {
		t.Fatalf("motionLoad returned handle=%d, want >= 1", handle)
	}

	// Output buffer: a Uint8Array viewing a Float64Array's buffer, room for 64 floats.
	const capFloats = 64
	f64 := js.Global().Get("Float64Array").New(capFloats)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))
	if out.Get("length").Int() != capFloats*8 {
		t.Fatalf("output Uint8Array length = %d, want %d", out.Get("length").Int(), capFloats*8)
	}

	n := motionTick([]js.Value{
		js.ValueOf(handle),
		js.ValueOf(0.5),
		js.ValueOf(false),
		out,
	})
	if n <= 0 {
		t.Fatalf("motionTick returned n=%d, want > 0", n)
	}

	// Copy the JS bytes back to Go and decode the first n float64s LE.
	gotBytes := make([]byte, out.Get("length").Int())
	js.CopyBytesToGo(gotBytes, out)
	got := decodeLEFloat64s(gotBytes, n)

	want := referenceWrites(tl, 0.5, false)
	if n != len(want) {
		t.Fatalf("tick n=%d, reference Eval produced %d floats", n, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("float %d: tick=%v, Eval=%v", i, got[i], want[i])
		}
	}
}

// TestMotionExportRegisteredOnGlobal verifies registration installs callable
// functions on the JS global and that they behave the same as the handlers.
func TestMotionExportRegisteredOnGlobal(t *testing.T) {
	registerMotionExports()

	loadFn := js.Global().Get("__gosx_motion_load")
	tickFn := js.Global().Get("__gosx_motion_tick")
	if loadFn.Type() != js.TypeFunction {
		t.Fatalf("__gosx_motion_load not registered (type=%v)", loadFn.Type())
	}
	if tickFn.Type() != js.TypeFunction {
		t.Fatalf("__gosx_motion_tick not registered (type=%v)", tickFn.Type())
	}

	blob, tl := buildMotionTestProgram()
	jsBytes := jsUint8ArrayFromGo(blob)
	handle := js.Global().Call("__gosx_motion_load", jsBytes).Int()
	if handle < 1 {
		t.Fatalf("__gosx_motion_load returned %d, want >= 1", handle)
	}

	const capFloats = 64
	f64 := js.Global().Get("Float64Array").New(capFloats)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))
	n := js.Global().Call("__gosx_motion_tick",
		js.ValueOf(handle), js.ValueOf(0.5), js.ValueOf(false), out).Int()

	want := referenceWrites(tl, 0.5, false)
	if n != len(want) {
		t.Fatalf("global tick n=%d, reference %d", n, len(want))
	}

	gotBytes := make([]byte, out.Get("length").Int())
	js.CopyBytesToGo(gotBytes, out)
	got := decodeLEFloat64s(gotBytes, n)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("float %d: global tick=%v, Eval=%v", i, got[i], want[i])
		}
	}
}

// TestMotionExportTooSmallBuffer asserts a too-small output Uint8Array does not
// panic: tick returns the FULL float count, and the bytes that fit decode
// correctly against the reference prefix.
func TestMotionExportTooSmallBuffer(t *testing.T) {
	blob, tl := buildMotionTestProgram()
	handle := motionLoad([]js.Value{jsUint8ArrayFromGo(blob)})
	if handle < 1 {
		t.Fatalf("load handle=%d", handle)
	}

	want := referenceWrites(tl, 0.5, false)
	if len(want) < 2 {
		t.Fatalf("reference produced %d floats; test needs >= 2 to exercise truncation", len(want))
	}

	// Tiny buffer: room for exactly 1 float64 (8 bytes), smaller than the full output.
	const tinyFloats = 1
	f64 := js.Global().Get("Float64Array").New(tinyFloats)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("motionTick with tiny buffer panicked: %v", r)
		}
	}()

	n := motionTick([]js.Value{
		js.ValueOf(handle),
		js.ValueOf(0.5),
		js.ValueOf(false),
		out,
	})

	// Full count must still be reported so JS knows to grow and re-tick.
	if n != len(want) {
		t.Fatalf("truncated tick n=%d, want full count %d", n, len(want))
	}

	// The single float that fit must match the reference's first value.
	gotBytes := make([]byte, out.Get("length").Int())
	js.CopyBytesToGo(gotBytes, out)
	got := decodeLEFloat64s(gotBytes, tinyFloats)
	if got[0] != want[0] {
		t.Errorf("truncated float 0: got=%v, want=%v", got[0], want[0])
	}
}

// TestMotionExportLoadBadBytes asserts a bad program blob yields handle -1.
func TestMotionExportLoadBadBytes(t *testing.T) {
	bad := jsUint8ArrayFromGo([]byte{0x00, 0x01, 0x02})
	if h := motionLoad([]js.Value{bad}); h != -1 {
		t.Fatalf("motionLoad(bad bytes) = %d, want -1", h)
	}
}

// TestMotionExportTickUnknownHandle asserts an unknown handle yields n=0 (no panic).
func TestMotionExportTickUnknownHandle(t *testing.T) {
	f64 := js.Global().Get("Float64Array").New(64)
	out := js.Global().Get("Uint8Array").New(f64.Get("buffer"))
	n := motionTick([]js.Value{
		js.ValueOf(999999),
		js.ValueOf(0.5),
		js.ValueOf(false),
		out,
	})
	if n != 0 {
		t.Fatalf("motionTick(unknown handle) = %d, want 0", n)
	}
}
