//go:build js && wasm

package netsync

import (
	"sync"

	"m31labs.dev/gosx/hub/socket"
	"m31labs.dev/gosx/room/wire"
)

// DialWebSocket opens a browser WebSocket to url through hub/socket. It does
// not import net/http.
func DialWebSocket(url string) (Conn, error) {
	c := &browserConn{state: wire.ConnConnecting}
	s, err := socket.Dial(url, socket.Callbacks{
		Open: func() {
			c.mu.Lock()
			if c.state == wire.ConnConnecting {
				c.state = wire.ConnOpen
			}
			c.mu.Unlock()
		},
		Message: func(m socket.Message) {
			if !m.IsBinary {
				return // the hub's JSON envelope (welcome, presence) is not ours
			}
			c.mu.Lock()
			c.queue.push(m.Binary)
			c.mu.Unlock()
		},
		Closed: func(e socket.CloseEvent) {
			c.mu.Lock()
			c.state, c.code, c.reason = wire.ConnClosed, e.Code, e.Reason
			if c.code == 0 {
				c.code = abnormalClose
			}
			c.mu.Unlock()
		},
	})
	if err != nil {
		return nil, err
	}
	c.socket = s
	return c, nil
}

type browserConn struct {
	socket  *socket.Socket
	mu      sync.Mutex
	state   wire.ConnState
	code    int
	reason  string
	queue   frameQueue
	scratch [][]byte
}

func (c *browserConn) Send(frame []byte) error { return c.socket.SendBinary(frame) }

func (c *browserConn) Poll(fn func([]byte)) wire.ConnState {
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

func (c *browserConn) Close(code int, reason string) {
	_ = c.socket.Close(code, reason)
	c.mu.Lock()
	if c.state != wire.ConnClosed {
		c.state, c.code, c.reason = wire.ConnClosed, code, reason
	}
	c.mu.Unlock()
	c.socket.Dispose()
}

func (c *browserConn) CloseStatus() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code, c.reason
}
