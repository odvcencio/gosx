package room

import (
	"errors"
	"sync"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/room/wire"
)

// link is one client connection attached to a room: a hub WebSocket client or
// an in-process LocalConn. Room calls these with Room.mu held, so they must not
// block or call back into the room.
type link interface {
	id() string
	// sendReliable delivers one lifecycle frame in order. It copies frame.
	sendReliable(frame []byte)
	// local reports whether the room must offer snapshots to this link itself.
	// Hub links get snapshots through Hub.BroadcastBinarySnapshot.
	local() *LocalConn
	// close ends the connection with a WebSocket close code. It may block, so
	// Room calls it after releasing Room.mu.
	close(code int, reason string)
}

type hubLink struct {
	h        *hub.Hub
	clientID string
}

func (l hubLink) id() string               { return l.clientID }
func (l hubLink) sendReliable(f []byte)    { l.h.SendBinary(l.clientID, f) }
func (l hubLink) local() *LocalConn        { return nil }
func (l hubLink) close(code int, r string) { l.h.DisconnectWithCode(l.clientID, code, r) }

// ErrConnClosed is returned by LocalConn.Send after the connection closed.
var ErrConnClosed = errors.New("room: connection closed")

// LocalConn is an in-process client connection to a room, with the same frames
// and delivery rules as a WebSocket client: lifecycle frames arrive in order,
// and snapshots coalesce so a slow reader gets the newest keyframe and delta
// instead of a backlog. Use it for headless clients (load tests, server-side
// agents) and for deterministic tests that put a simulated network between a
// netsync client and a room. Its methods match netsync.Conn.
//
// Send hands a frame to the room synchronously. Poll delivers what the room
// sent since the last Poll. LocalConn is safe for concurrent use.
type LocalConn struct {
	room     *Room
	clientID string

	mu       sync.Mutex
	reliable [][]byte
	epoch    uint64 // epoch of the pending or last delivered baseline
	baseline []byte // pending keyframe for epoch, nil once delivered
	delta    []byte // pending delta for epoch
	closed   bool
	code     int
	reason   string
	scratch  [][]byte
}

func (c *LocalConn) id() string        { return c.clientID }
func (c *LocalConn) local() *LocalConn { return c }

func (c *LocalConn) sendReliable(frame []byte) {
	c.mu.Lock()
	if !c.closed {
		c.reliable = append(c.reliable, append([]byte(nil), frame...))
	}
	c.mu.Unlock()
}

// offerSnapshot follows Hub.BroadcastBinarySnapshot: a newer epoch replaces
// both pending frames, the same epoch replaces only the delta. Frames are
// shared, never mutated, so they are kept without copying.
func (c *LocalConn) offerSnapshot(epoch uint64, baseline, delta []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || epoch < c.epoch {
		return
	}
	if epoch > c.epoch {
		c.epoch, c.baseline, c.delta = epoch, baseline, delta
		return
	}
	if delta != nil {
		c.delta = delta
	}
}

func (c *LocalConn) close(code int, reason string) {
	c.mu.Lock()
	if !c.closed {
		c.closed, c.code, c.reason = true, code, reason
		c.baseline, c.delta = nil, nil
	}
	c.mu.Unlock()
}

// Send delivers one client frame to the room.
func (c *LocalConn) Send(frame []byte) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrConnClosed
	}
	c.room.onFrame(c.clientID, frame)
	return nil
}

// Poll calls fn for each frame the room sent since the last Poll: lifecycle
// frames first, in order, then the pending keyframe and delta. Frames stay
// valid after fn returns (they are never reused), but fn must not modify them.
// It returns ConnClosed once the room closed the connection and every frame
// sent before the close was delivered.
func (c *LocalConn) Poll(fn func(frame []byte)) wire.ConnState {
	c.mu.Lock()
	// Take the scratch slice so a concurrent Poll cannot share it.
	frames := append(c.scratch[:0], c.reliable...)
	c.scratch = nil
	clear(c.reliable)
	c.reliable = c.reliable[:0]
	if c.baseline != nil {
		frames = append(frames, c.baseline)
		c.baseline = nil
	}
	if c.delta != nil {
		frames = append(frames, c.delta)
		c.delta = nil
	}
	closed := c.closed
	c.mu.Unlock()
	for _, f := range frames {
		fn(f)
	}
	clear(frames)
	c.mu.Lock()
	c.scratch = frames[:0]
	c.mu.Unlock()
	if closed {
		return wire.ConnClosed
	}
	return wire.ConnOpen
}

// Close leaves the room, as a client closing its socket does. The seat stays
// reserved for the reconnect grace.
func (c *LocalConn) Close(code int, reason string) {
	c.mu.Lock()
	already := c.closed
	if !already {
		c.closed, c.code, c.reason = true, code, reason
	}
	c.mu.Unlock()
	if !already {
		c.room.detach(c.clientID)
	}
}

// CloseStatus returns the close code and reason once the connection closed.
func (c *LocalConn) CloseStatus() (code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.code, c.reason
}
