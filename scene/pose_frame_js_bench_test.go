//go:build js && wasm

package scene

import (
	"fmt"
	"syscall/js"
	"testing"
)

// This measures Go/WASM encoding, copying to Uint8Array, JSON command
// conversion, and syscall/js dispatch. The renderer itself is not exercised.
func BenchmarkDispatchCrowdPoseFrame(b *testing.B) {
	instances := make([]MeshInstanceIR, 180)
	for i := range instances {
		instances[i] = MeshInstanceIR{ID: fmt.Sprintf("actor-%d", i), X: float64(i), ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: "Run", AnimationTime: .25, AnimationLoop: true}
	}
	frame := PoseFrame{Batches: []PoseBatch{{ID: "crowd", Instances: instances}}}
	before := make([]Command, 16)
	for i := range before {
		before[i] = Command{Kind: CommandRemoveObject, ObjectID: fmt.Sprintf("effect-%d", i)}
	}
	catch := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	promise := js.Global().Get("Object").New()
	promise.Set("catch", catch)
	dispatch := js.FuncOf(func(js.Value, []js.Value) any { return promise })
	root := js.Global().Get("Object").New()
	api := js.Global().Get("Object").New()
	api.Set("dispatchPoseFrame", dispatch)
	root.Set("scene3d", api)
	previous := js.Global().Get("__gosx")
	js.Global().Set("__gosx", root)
	b.Cleanup(func() {
		js.Global().Set("__gosx", previous)
		dispatch.Release()
		catch.Release()
	})
	for _, scenario := range []struct {
		name   string
		before []Command
	}{{"pose-only", nil}, {"with-16-commands", before}} {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := DispatchPoseFrameAfterCommands("crowd-mount", scenario.before, frame, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
