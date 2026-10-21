//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/motion"
)

func TestMotionExportLongKeyframeSeeks(t *testing.T) {
	registerMotionExports()
	keys := make([]motion.Key, 8192)
	for i := range keys {
		keys[i] = motion.Key{T: float64(i) + float64(i%3)/8, Value: motion.ScalarV(float64(i%11)*13 - 7)}
	}
	timeline := &motion.Timeline{Children: []motion.Positioned{{
		At: motion.Position{Val: 6.25}, Track: &motion.Track{Keys: keys},
	}}}
	load, tick, unload := js.Global().Get("__gosx_motion_load"), js.Global().Get("__gosx_motion_tick"), js.Global().Get("__gosx_motion_unload")
	handle := load.Invoke(jsUint8ArrayFromGo(motion.EncodeProgram(timeline, []string{"mesh"}, []string{"weight"}))).Int()
	if handle < 1 {
		t.Fatalf("long-track load returned %d", handle)
	}
	defer unload.Invoke(handle)
	output := js.Global().Get("Uint8Array").New(4 * 8)
	bytes := make([]byte, 4*8)
	for _, reduced := range []bool{false, true, false} {
		for _, index := range []int{6001, 2, 4095, 0, 8190, 300, 6001} {
			left, right := keys[index], keys[index+1]
			at, want := 6.25+(left.T+right.T)/2, (left.Value.F[0]+right.Value.F[0])/2
			if reduced {
				want = keys[len(keys)-1].Value.F[0]
			}
			n := tick.Invoke(handle, at, reduced, output).Int()
			if n != 4 {
				t.Fatalf("long-track tick produced %d floats, want 4", n)
			}
			js.CopyBytesToGo(bytes, output)
			got := decodeLEFloat64s(bytes, n)
			if got[0] != 0 || got[1] != 0 || got[2] != float64(motion.ArityScalar) || got[3] != want {
				t.Fatalf("seek %g reduced=%v: %v, want scalar %g", at, reduced, got, want)
			}
			t.Logf("long-keyframe export: t=%g reduced=%v expected=%g observed=%g", at, reduced, want, got[3])
		}
	}
}
