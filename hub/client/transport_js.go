//go:build js && wasm

package hubclient

import (
	"errors"
	"fmt"
	"net/http"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// newDefaultDialer returns the browser transport, which drives the host
// WebSocket object directly. Header is ignored: the WebSocket API does not
// allow custom request headers, so a resume token or other identity must
// travel as a URL query parameter (see Options.ResumeParam) or a cookie the
// browser attaches automatically.
//
// enableCompression is ignored: the browser's WebSocket constructor takes no
// compression option at all. Every major browser already offers
// permessage-deflate on every WebSocket handshake and negotiates,
// (de)compresses, and delivers frames to "message" transparently — there is
// nothing for this transport to configure. A hub that opts into
// EnableCompression (see package hub) is enough to compress traffic to a
// browser client; Options.EnableCompression only matters for the native
// transport in transport_native.go.
func newDefaultDialer(enableCompression bool) dialer { return browserDialer{} }

type browserDialer struct{}

func (browserDialer) Dial(rawURL string, _ http.Header) (result conn, dialErr error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			dialErr = fmt.Errorf("hubclient: dial %q: %v", rawURL, r)
		}
	}()
	ws := js.Global().Get("WebSocket").New(rawURL)
	ws.Set("binaryType", "arraybuffer")
	bc := &browserConn{ws: ws, events: make(chan frameEvent, 64)}
	bc.bind()
	return bc, nil
}

type browserConn struct {
	ws                                  js.Value
	events                              chan frameEvent
	onOpen, onMessage, onError, onClose js.Func
}

func (c *browserConn) bind() {
	c.onOpen = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		c.events <- frameEvent{Kind: frameOpen}
		return nil
	})
	c.onMessage = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].Get("data")
		if data.Type() == js.TypeString {
			c.events <- frameEvent{Kind: frameMessage, Data: []byte(data.String())}
			return nil
		}
		// binaryType is "arraybuffer": a binary frame arrives as an
		// ArrayBuffer, which needs a Uint8Array view before CopyBytesToGo can
		// read it.
		view := js.Global().Get("Uint8Array").New(data)
		buf := make([]byte, view.Get("length").Int())
		js.CopyBytesToGo(buf, view)
		c.events <- frameEvent{Kind: frameMessage, Data: buf, Binary: true}
		return nil
	})
	c.onError = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		c.events <- frameEvent{Kind: frameError, Err: errors.New("hubclient: websocket error")}
		return nil
	})
	c.onClose = js.FuncOf(func(_ js.Value, args []js.Value) any {
		var err error
		if len(args) > 0 {
			code := args[0].Get("code").Int()
			if code != 1000 {
				err = fmt.Errorf("hubclient: websocket closed: code=%d reason=%q", code, args[0].Get("reason").String())
			}
		}
		// The "close" event is the WebSocket API's one guaranteed terminal
		// event — it fires whether the socket closed locally, remotely, or
		// because "error" preceded it — so it is the only safe place to
		// close the channel and release the JS callbacks.
		c.events <- frameEvent{Kind: frameClosed, Err: err}
		close(c.events)
		c.release()
		return nil
	})
	c.ws.Call("addEventListener", "open", c.onOpen)
	c.ws.Call("addEventListener", "message", c.onMessage)
	c.ws.Call("addEventListener", "error", c.onError)
	c.ws.Call("addEventListener", "close", c.onClose)
}

func (c *browserConn) release() {
	c.ws.Call("removeEventListener", "open", c.onOpen)
	c.ws.Call("removeEventListener", "message", c.onMessage)
	c.ws.Call("removeEventListener", "error", c.onError)
	c.ws.Call("removeEventListener", "close", c.onClose)
	c.onOpen.Release()
	c.onMessage.Release()
	c.onError.Release()
	c.onClose.Release()
}

func (c *browserConn) Send(data []byte, binary bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hubclient: send: %v", r)
		}
	}()
	const readyStateOpen = 1
	if c.ws.Get("readyState").Int() != readyStateOpen {
		return errors.New("hubclient: send on a connection that is not open")
	}
	if binary {
		c.ws.Call("send", jsutil.NewUint8ArrayFromBytes(data))
		return nil
	}
	c.ws.Call("send", string(data))
	return nil
}

func (c *browserConn) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("hubclient: close: %v", r)
		}
	}()
	c.ws.Call("close", 1000)
	return nil
}

func (c *browserConn) Events() <-chan frameEvent {
	return c.events
}
