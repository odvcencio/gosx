//go:build js && wasm

package scene

import (
	"syscall/js"
	"testing"
	"time"
)

// The page receives the encoded frame and the time stamps in ms.
func TestDispatchTimedPoseFrameSendsTimes(t *testing.T) {
	root := js.Global().Get("Object").New()
	api := js.Global().Get("Object").New()
	var target string
	var bytes js.Value
	var options js.Value
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		target, bytes, options = args[0].String(), args[1], args[2]
		return js.Global().Get("Promise").Call("resolve", nil)
	})
	defer fn.Release()
	api.Set("dispatchPoseFrame", fn)
	root.Set("scene3d", api)
	previous := js.Global().Get("__gosx")
	js.Global().Set("__gosx", root)
	defer js.Global().Set("__gosx", previous)

	frame := PoseFrame{Batches: []PoseBatch{{ID: "units", Instances: []MeshInstanceIR{{ID: "a", X: 1, ScaleX: 1, ScaleY: 1, ScaleZ: 1}}}}}
	if err := DispatchTimedPoseFrameWith("arena", frame, 2*time.Second, 50*time.Millisecond, TimedPoseOptions{Delay: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if target != "arena" || bytes.Get("length").Int() < 8 || bytes.Index(3).Int() != '2' {
		t.Fatalf("target %q, frame length %v", target, bytes.Get("length"))
	}
	if options.Get("at").Float() != 2000 || options.Get("step").Float() != 50 || options.Get("delay").Float() != 20 {
		t.Fatalf("options at=%v step=%v delay=%v", options.Get("at"), options.Get("step"), options.Get("delay"))
	}
	if err := DispatchTimedPoseFrame("arena", frame, 0, 0); err == nil {
		t.Fatal("a zero step was dispatched")
	}
}
