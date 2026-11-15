//go:build !(js && wasm)

package netsync

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/room/wire"
)

// DialWebSocket opens a WebSocket to url with gorilla/websocket, for native
// clients, headless bots and tests. The handshake runs on its own goroutine.
func DialWebSocket(url string) (Conn, error) {
	c := &nativeConn{state: wire.ConnConnecting}
	go c.run(url)
	return c, nil
}

type nativeConn struct {
	mu      sync.Mutex
	ws      *websocket.Conn
	state   wire.ConnState
	code    int
	reason  string
	queue   frameQueue
	scratch [][]byte
	closing bool
	writeMu sync.Mutex
}

func (c *nativeConn) run(url string) {
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		c.finish(abnormalClose, err.Error())
		return
	}
	ws.SetReadLimit(wire.MaxStateSize + 1<<16)
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		_ = ws.Close()
		return
	}
	c.ws, c.state = ws, wire.ConnOpen
	c.mu.Unlock()
	for {
		typ, data, err := ws.ReadMessage()
		if err != nil {
			code, reason := abnormalClose, err.Error()
			var ce *websocket.CloseError
			if errors.As(err, &ce) {
				code, reason = ce.Code, ce.Text
			}
			c.finish(code, reason)
			_ = ws.Close()
			return
		}
		if typ == websocket.BinaryMessage {
			c.mu.Lock()
			c.queue.push(data)
			c.mu.Unlock()
		}
	}
}

func (c *nativeConn) finish(code int, reason string) {
	c.mu.Lock()
	if c.state != wire.ConnClosed {
		c.state, c.code, c.reason = wire.ConnClosed, code, reason
	}
	c.mu.Unlock()
}

func (c *nativeConn) Send(frame []byte) error {
	c.mu.Lock()
	ws, state := c.ws, c.state
	c.mu.Unlock()
	if ws == nil || state != wire.ConnOpen {
		return errNotOpen
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return ws.WriteMessage(websocket.BinaryMessage, frame)
}

func (c *nativeConn) Poll(fn func([]byte)) wire.ConnState {
	c.mu.Lock()
	frames := c.queue.take(c.scratch)
	c.scratch = nil
	state := c.state
	c.mu.Unlock()
	for _, f := range frames {
		fn(f)
	}
	c.mu.Lock()
	c.queue.recycle(frames)
	clear(frames)
	c.scratch = frames[:0]
	c.mu.Unlock()
	return state
}

func (c *nativeConn) Close(code int, reason string) {
	c.mu.Lock()
	c.closing = true
	ws := c.ws
	if c.state != wire.ConnClosed {
		c.state, c.code, c.reason = wire.ConnClosed, code, reason
	}
	c.mu.Unlock()
	if ws != nil {
		c.writeMu.Lock()
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		c.writeMu.Unlock()
		_ = ws.Close()
	}
}

func (c *nativeConn) CloseStatus() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code, c.reason
}
