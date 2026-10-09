//go:build js && wasm

package hubclient

import (
	"context"
	"fmt"
	"net/http"

	"m31labs.dev/gosx/hub/socket"
)

// Browser handshakes cannot set request headers or compression options.
func newDefaultDialer(bool) dialer { return browserDialer{} }

type browserDialer struct{}

func (browserDialer) Dial(ctx context.Context, rawURL string, _ http.Header) (conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bc := &browserConn{eventStream: newEventStream(64)}
	ws, err := socket.Dial(rawURL, socket.Callbacks{
		Open: func() { bc.emit(frameEvent{Kind: frameOpen}) },
		Message: func(message socket.Message) {
			// eventStream outlives the callback, so take ownership here. The
			// direct socket API can consume binary frames without this copy.
			data := []byte(message.Text)
			if message.IsBinary {
				data = append([]byte(nil), message.Binary...)
			}
			bc.emit(frameEvent{Kind: frameMessage, Data: data, Binary: message.IsBinary})
		},
		Error: func(err error) { bc.emit(frameEvent{Kind: frameError, Err: err}) },
		Closed: func(event socket.CloseEvent) {
			var err error
			if event.Code != 1000 {
				err = fmt.Errorf("hubclient: websocket closed: code=%d reason=%q", event.Code, event.Reason)
			}
			bc.finish(err)
		},
	})
	if err != nil {
		return nil, err
	}
	bc.ws = ws
	return bc, nil
}

type browserConn struct {
	*eventStream
	ws           *socket.Socket
	finished     bool
	active       int
	eventsClosed bool
}

// Closing a retiring channel waits for any callback already suspended in emit.
// Browser callbacks and the client goroutines share the WASM event loop.
func (c *browserConn) emit(event frameEvent) bool {
	if c.finished {
		return false
	}
	c.active++
	defer func() { c.active--; c.closeEventsIfFinished() }()
	return c.eventStream.emit(event)
}
func (c *browserConn) closeEventsIfFinished() {
	if c.finished && c.active == 0 && !c.eventsClosed {
		c.eventsClosed = true
		close(c.events)
	}
}
func (c *browserConn) finish(err error) {
	if c.finished {
		return
	}
	c.finished = true
	c.active++
	c.eventStream.emit(frameEvent{Kind: frameClosed, Err: err})
	c.active--
	c.closeEventsIfFinished()
}
func (c *browserConn) Send(data []byte, binary bool) error {
	if binary {
		return c.ws.SendBinary(data)
	}
	return c.ws.SendText(string(data))
}
func (c *browserConn) Close() error {
	// The retiring consumer must not block a final browser callback on a
	// full channel. Dispose also releases callbacks if the host never closes.
	c.stop()
	c.ws.Dispose()
	c.finish(nil)
	return nil
}
