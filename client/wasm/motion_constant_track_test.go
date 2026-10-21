//go:build js && wasm

package main

import (
	"math"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/motion"
)

func TestMotionExportConstantTrackInvalidClock(t *testing.T) {
	registerMotionExports()
	values := []motion.Value{
		motion.ScalarV(math.Copysign(0, -1)), motion.Vec2V(-2, 3), motion.Vec3V(1, -2, 4),
		motion.Vec4V(-1, 2, -3, 4), motion.QuatV(motion.Quat{X: .6, W: .8}), motion.ColorV(.2, .4, .8, 1),
	}
	constant := &motion.Timeline{}
	var want []float64
	for id, value := range values {
		constant.Children = append(constant.Children, motion.Positioned{Track: &motion.Track{
			TargetID: id, Keys: []motion.Key{{T: .5, Value: value}},
		}})
		want = append(want, float64(id), 0, float64(value.Arity))
		want = append(want, value.F[:value.Arity.Width()]...)
	}
	animated := &motion.Timeline{Children: []motion.Positioned{{Track: &motion.Track{Keys: []motion.Key{
		{T: 0, Value: motion.ScalarV(0)}, {T: 1, Value: motion.ScalarV(10)},
	}}}}}
	load, tick, unload := js.Global().Get("__gosx_motion_load"), js.Global().Get("__gosx_motion_tick"), js.Global().Get("__gosx_motion_unload")
	handle := load.Invoke(jsUint8ArrayFromGo(motion.EncodeProgram(constant, nil, nil))).Int()
	neighbor := load.Invoke(jsUint8ArrayFromGo(motion.EncodeProgram(animated, nil, nil))).Int()
	if handle < 1 || neighbor < 1 {
		t.Fatalf("program loads returned %d and %d", handle, neighbor)
	}
	// Avoid another JavaScript callback while a failed export is unwinding.
	completed := false
	defer func() {
		if completed {
			unload.Invoke(handle)
			unload.Invoke(neighbor)
		}
	}()
	output := js.Global().Get("Uint8Array").New(64 * 8)
	bytes := make([]byte, 64*8)
	for _, reduced := range []bool{false, true, false} {
		for _, clock := range []struct {
			name string
			at   float64
		}{{"finite", .25}, {"NaN", math.NaN()}, {"-Inf", math.Inf(-1)}, {"+Inf", math.Inf(1)}, {"finite-after-invalid", .75}} {
			t.Logf("constant-export input=%s reduced=%v expectedWrites=%d", clock.name, reduced, len(want))
			n := tick.Invoke(handle, clock.at, reduced, output).Int()
			if n != len(want) {
				t.Fatalf("constant export returned %d floats, want %d", n, len(want))
			}
			js.CopyBytesToGo(bytes, output)
			got := decodeLEFloat64s(bytes, n)
			for i, value := range want {
				if math.Float64bits(got[i]) != math.Float64bits(value) {
					t.Fatalf("clock=%s reduced=%v float %d: %v, want %v", clock.name, reduced, i, got, want)
				}
			}
			if n := tick.Invoke(neighbor, .5, false, output).Int(); n != 4 {
				t.Fatalf("neighboring animation returned %d floats after %s, want 4", n, clock.name)
			}
			js.CopyBytesToGo(bytes, output)
			neighborWrites := decodeLEFloat64s(bytes, 4)
			if neighborWrites[0] != 0 || neighborWrites[1] != 0 || neighborWrites[2] != 0 || neighborWrites[3] != 5 {
				t.Fatalf("neighboring animation stopped after %s: %v", clock.name, neighborWrites)
			}
			t.Logf("constant-export clock=%s reduced=%v constantWrites=%d bitwiseStable=true neighbor=%g", clock.name, reduced, n, neighborWrites[3])
		}
	}
	completed = true
}
