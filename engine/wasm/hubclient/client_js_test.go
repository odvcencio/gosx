//go:build js && wasm

package hubclient

import (
	"encoding/json"
	"errors"
	"syscall/js"
	"testing"
)

func TestWASMRawHubDispatchAndCallerOwnedReconnect(t *testing.T) {
	var callbacks []js.Value
	var sent []string
	var disposals int
	openFn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		callbacks = append(callbacks, args[2])
		sendFn := js.FuncOf(func(_ js.Value, values []js.Value) any { sent = append(sent, values[0].String()); return true })
		stateFn := js.FuncOf(func(_ js.Value, _ []js.Value) any { return 1 })
		closeFn := js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
		disposeFn := js.FuncOf(func(_ js.Value, _ []js.Value) any { disposals++; return nil })
		return js.ValueOf(map[string]any{"send": sendFn, "state": stateFn, "close": closeFn, "dispose": disposeFn})
	})
	defer openFn.Release()
	js.Global().Set("__gosx", js.ValueOf(map[string]any{"host": map[string]any{"wasmHub": map[string]any{"open": openFn}}}))
	defer js.Global().Delete("__gosx")
	var received string
	client, err := Dial("/ws/ROOM", Options{}, Handler{
		OnMessage: func(event string, data json.RawMessage) { received = event + ":" + string(data) },
	})
	if err != nil {
		t.Fatal(err)
	}
	callbacks[0].Invoke("message", `{"event":"state","data":{"tick":2}}`, 0, "")
	if received != `state:{"tick":2}` {
		t.Fatalf("received %q", received)
	}
	if err := client.Send("input", map[string]int{"dash": 1}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != `{"event":"input","data":{"dash":1}}` {
		t.Fatalf("sent %v", sent)
	}
	if err := client.Reconnect("/ws/ROOM?resume=token"); err != nil {
		t.Fatal(err)
	}
	if disposals != 1 || len(callbacks) != 2 {
		t.Fatalf("reconnect: disposed %d, callbacks %d", disposals, len(callbacks))
	}
	client.Dispose()
	if disposals != 2 || !errors.Is(client.Send("input", nil), ErrDisposed) {
		t.Fatal("dispose did not stop transport")
	}
}
