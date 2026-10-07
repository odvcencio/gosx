//go:build js && wasm

package wasm

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/scene"
)

func TestTypedSharedSignalNativePicking(t *testing.T) {
	value := js.Global().Call("eval", `(() => {
        let handler = null;
        return {
            subscribeSignal(name, fn) { this.name = name; handler = fn; return () => { handler = null; this.disposed = true; }; },
            setSignal(name, value) { this.output = name; this.result = value; return ""; },
            deliver(value) { if (handler) handler(value); },
        };
    })()`)
	ctx := Context{value: value}
	graph := scene.NewGraph(scene.Mesh{ID: "end", Geometry: scene.BoxGeometry{Width: 2, Height: 2, Depth: 2}})
	calls := 0
	dispose, err := SubscribeSignal[controller.PickRequest](ctx, "$dropRay", func(request controller.PickRequest) {
		calls++
		hit, ok := scene.RaycastGraph(graph, request.Ray)
		result := controller.PickResult{RequestID: request.RequestID}
		if ok {
			result.Hit = &hit
		}
		if err := ctx.SetSignal("$dropHit", result); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	input := js.Global().Get("JSON").Call("parse", `{"requestId":"drag:1","ray":{"origin":{"z":4},"direction":{"z":-1}}}`)
	value.Call("deliver", input)
	if calls != 1 || value.Get("name").String() != "$dropRay" || value.Get("output").String() != "$dropHit" || value.Get("result").Get("requestId").String() != "drag:1" || value.Get("result").Get("hit").Get("id").String() != "end" {
		t.Fatal("typed native picking did not cross the shared signal bridge")
	}
	value.Call("deliver", "invalid")
	if calls != 1 {
		t.Fatal("malformed signal value reached the typed handler")
	}
	dispose.Dispose()
	dispose.Dispose()
	value.Call("deliver", input)
	if calls != 1 || !value.Get("disposed").Bool() {
		t.Fatal("subscription survived disposal")
	}
}

func TestSharedSignalBridgeErrors(t *testing.T) {
	ctx := Context{value: js.ValueOf(map[string]any{})}
	if err := ctx.SetSignal("$hit", nil); err == nil {
		t.Fatal("missing setter accepted")
	}
	if _, err := SubscribeSignal[controller.PickRequest](ctx, "$ray", func(controller.PickRequest) {}); err == nil {
		t.Fatal("missing subscriber accepted")
	}
}
