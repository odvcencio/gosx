//go:build js && wasm

package hubclient

import (
	"context"
	"runtime"
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
		if callback, ok := listeners["close"]; ok {
			callback.Invoke(js.ValueOf(map[string]any{"code": 1000, "reason": ""}))
		}
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
	connection, err := (browserDialer{}).Dial(context.Background(), "ws://example.test/pending", nil)
	if err != nil {
		t.Fatal(err)
	}
	c := connection.(*browserConn)
	for range cap(c.events) {
		c.emit(frameEvent{Kind: frameMessage})
	}
	done := make(chan struct{})
	defer func() {
		// If Close stops retiring the event producer, release the blocked JS
		// callback before restoring globals. Otherwise even a failed timeout
		// assertion cannot finish the test while that callback is suspended.
		c.stop()
		awaitLifecycle(t, done, "browser close cleanup")
	}()
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

// A message callback can be suspended on a full queue when an unrelated Go
// goroutine retires the client. Closing the event channel before that producer
// returns would race a send on the closed channel.
func TestBrowserCloseWaitsForSuspendedEventProducer(t *testing.T) {
	for range 100 {
		c := &browserConn{eventStream: newEventStream(1)}
		c.emit(frameEvent{Kind: frameOpen})
		done := make(chan struct{})
		go func() {
			defer close(done)
			if c.emit(frameEvent{Kind: frameMessage}) {
				t.Error("retired producer delivered a message")
			}
		}()
		for c.active == 0 {
			runtime.Gosched()
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		awaitLifecycle(t, done, "suspended browser producer")
		for range c.events {
		}
		if !c.eventsClosed {
			t.Fatal("retired event channel remained open")
		}
	}
}
