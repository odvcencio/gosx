package netsync

import (
	"m31labs.dev/gosx/room/wire"
)

// Conn is one connection to a room server. The browser build dials a
// WebSocket through hub/socket; native builds dial with gorilla/websocket;
// room.LocalConn satisfies Conn in-process; SimConn wraps any Conn in a
// simulated network.
//
// A Conn never blocks the game loop: Send queues or writes one frame, and Poll
// hands over what arrived since the last Poll.
type Conn interface {
	// Send sends one binary frame. It must not keep frame after returning.
	Send(frame []byte) error
	// Poll calls fn for each frame received since the last Poll, in arrival
	// order, and reports the connection state. fn must not keep frame.
	Poll(fn func(frame []byte)) wire.ConnState
	// Close closes the connection with a WebSocket close code.
	Close(code int, reason string)
	// CloseStatus returns the close code and reason once Poll reported
	// wire.ConnClosed. Code 1006 means the connection failed without a close
	// frame.
	CloseStatus() (code int, reason string)
}

// Dialer opens a connection to a room server URL, including the join query.
// It returns at once; the handshake finishes in the background and Poll
// reports wire.ConnOpen when it has.
type Dialer func(url string) (Conn, error)

// abnormalClose is the WebSocket code for a connection that ended without a
// close frame.
const abnormalClose = 1006

// failedConn is a Conn that closed before it opened.
type failedConn struct {
	code   int
	reason string
}

// FailedConn returns a Conn that is already closed with code. Dialers use it
// to report a refused connection the way a WebSocket would.
func FailedConn(code int, reason string) Conn { return &failedConn{code: code, reason: reason} }

func (c *failedConn) Send([]byte) error                      { return errNotOpen }
func (c *failedConn) Poll(func([]byte)) wire.ConnState       { return wire.ConnClosed }
func (c *failedConn) Close(int, string)                      {}
func (c *failedConn) CloseStatus() (code int, reason string) { return c.code, c.reason }

type netsyncError string

func (e netsyncError) Error() string { return "netsync: " + string(e) }

const errNotOpen = netsyncError("connection not open")

// frameQueue holds received frames with buffer reuse, so a transport that
// copies frames out of its own storage does not allocate per frame once warm.
type frameQueue struct {
	frames [][]byte
	free   [][]byte
}

func (q *frameQueue) push(frame []byte) {
	var buf []byte
	if n := len(q.free); n > 0 {
		buf, q.free = q.free[n-1][:0], q.free[:n-1]
	}
	q.frames = append(q.frames, append(buf, frame...))
}

// take moves the queued frames into out (reusing its storage) and empties
// the queue. Return the frames with recycle after use.
func (q *frameQueue) take(out [][]byte) [][]byte {
	out = append(out[:0], q.frames...)
	clear(q.frames)
	q.frames = q.frames[:0]
	return out
}

func (q *frameQueue) recycle(frames [][]byte) {
	for _, f := range frames {
		if cap(f) <= 1<<16 { // keep small buffers; large keyframes are rare
			q.free = append(q.free, f)
		}
	}
}
