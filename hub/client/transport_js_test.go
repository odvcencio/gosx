//go:build js && wasm

package hubclient

import (
	"syscall/js"
	"testing"
)

func TestBrowserCloseReleasesCallbacksWithFullQueue(t *testing.T) {
	global := js.Global()
	previous := global.Get("WebSocket")
	defer global.Set("WebSocket", previous)
	listeners := make(map[string]js.Value)
	add := js.FuncOf(func(_ js.Value, args []js.Value) any {
		listeners[args[0].String()] = args[1]
		return nil
	})
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any {
		delete(listeners, args[0].String())
		return nil
	})
	closed := false
	closeSocket := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		closed = true
		listeners["close"].Invoke(js.ValueOf(map[string]any{"code": 1000, "reason": ""}))
		return nil
	})
	constructor := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		socket := global.Get("Object").New()
		socket.Set("readyState", 0)
		socket.Set("addEventListener", add)
		socket.Set("removeEventListener", remove)
		socket.Set("close", closeSocket)
		return socket
	})
	defer constructor.Release()
	defer add.Release()
	defer remove.Release()
	defer closeSocket.Release()
	global.Set("WebSocket", constructor)
	connection, err := (browserDialer{}).Dial("ws://example.test/pending", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := connection.(*browserConn)
	for range cap(c.events) {
		c.emit(frameEvent{Kind: frameMessage})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	awaitLifecycle(t, done, "browser close callback with a full queue")
	if !closed || len(listeners) != 0 {
		t.Fatalf("pending socket closed=%v, remaining callbacks=%d", closed, len(listeners))
	}
	for range c.events {
	}
}
