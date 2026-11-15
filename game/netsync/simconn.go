package netsync

import (
	"math/rand/v2"
	"time"

	"m31labs.dev/gosx/room/wire"
)

// SimNet describes a simulated link for SimConn. Up is the client-to-server
// one-way delay and Down the server-to-client one. Each frame gets a uniform
// extra delay in [0, Jitter], so frames can reorder. Loss drops that fraction
// of frames that may be lost: inputs, pings, pongs and snapshots. Lifecycle
// frames (Welcome, Room) are never dropped and keep their order, as they would
// on a WebSocket.
type SimNet struct {
	Seed     uint64
	Up, Down time.Duration
	Jitter   time.Duration
	Loss     float64
}

// SimConn wraps a Conn (typically a room.LocalConn) in a deterministic
// simulated network driven by a caller-owned clock. Frames move when the
// client polls: Poll first delivers due client frames to the inner Conn, then
// collects the inner Conn's new frames and schedules them, then hands over the
// frames whose time has come. The same seed and the same call sequence give
// the same deliveries.
type SimConn struct {
	inner Conn
	net   SimNet
	now   func() time.Duration
	rng   *rand.Rand

	up, down     []simFrame
	lastReliable time.Duration
	innerState   wire.ConnState
	down0        bool // link cut: everything is dropped
	seq          uint64

	// Counters for assertions.
	SentUp, DroppedUp, SentDown, DroppedDown int
}

type simFrame struct {
	at   time.Duration
	seq  uint64
	data []byte
}

// NewSimConn wraps inner.
func NewSimConn(inner Conn, net SimNet, now func() time.Duration) *SimConn {
	return &SimConn{inner: inner, net: net, now: now, rng: rand.New(rand.NewPCG(net.Seed, 0x73696d636f6e6e)), innerState: wire.ConnConnecting}
}

// Cut drops every frame in both directions while cut is true, like a dead
// link that has not yet timed out.
func (c *SimConn) Cut(cut bool) { c.down0 = cut }

// Inner returns the wrapped connection.
func (c *SimConn) Inner() Conn { return c.inner }

func (c *SimConn) delay(base time.Duration) time.Duration {
	d := base
	if c.net.Jitter > 0 {
		d += time.Duration(c.rng.Int64N(int64(c.net.Jitter) + 1))
	}
	return d
}

func (c *SimConn) lossy(frame []byte) bool {
	k, ok := wire.KindOf(frame)
	if ok && k.Reliable() {
		return false
	}
	return c.down0 || (c.net.Loss > 0 && c.rng.Float64() < c.net.Loss)
}

func insert(q []simFrame, f simFrame) []simFrame {
	q = append(q, f)
	for i := len(q) - 1; i > 0 && (q[i-1].at > f.at || (q[i-1].at == f.at && q[i-1].seq > f.seq)); i-- {
		q[i], q[i-1] = q[i-1], q[i]
	}
	return q
}

// Send schedules frame for the server.
func (c *SimConn) Send(frame []byte) error {
	if c.innerState == wire.ConnClosed {
		return errNotOpen
	}
	c.SentUp++
	if c.lossy(frame) {
		c.DroppedUp++
		return nil
	}
	c.seq++
	c.up = insert(c.up, simFrame{at: c.now() + c.delay(c.net.Up), seq: c.seq, data: append([]byte(nil), frame...)})
	return nil
}

// Poll moves frames whose time has come in both directions.
func (c *SimConn) Poll(fn func([]byte)) wire.ConnState {
	now := c.now()
	n := 0
	for ; n < len(c.up) && c.up[n].at <= now; n++ {
		_ = c.inner.Send(c.up[n].data)
	}
	c.up = append(c.up[:0], c.up[n:]...)

	c.innerState = c.inner.Poll(func(frame []byte) {
		c.SentDown++
		if c.lossy(frame) {
			c.DroppedDown++
			return
		}
		at := now + c.delay(c.net.Down)
		if k, _ := wire.KindOf(frame); k.Reliable() {
			at = max(at, c.lastReliable)
			c.lastReliable = at
		}
		c.seq++
		c.down = insert(c.down, simFrame{at: at, seq: c.seq, data: append([]byte(nil), frame...)})
	})

	n = 0
	for ; n < len(c.down) && c.down[n].at <= now; n++ {
		fn(c.down[n].data)
	}
	c.down = append(c.down[:0], c.down[n:]...)
	if c.innerState == wire.ConnClosed && len(c.down) == 0 {
		return wire.ConnClosed
	}
	if c.innerState == wire.ConnClosed {
		return wire.ConnOpen // the close arrives after the frames in flight
	}
	return c.innerState
}

// Close closes the inner connection.
func (c *SimConn) Close(code int, reason string) {
	c.inner.Close(code, reason)
	c.innerState = wire.ConnClosed
	c.down = c.down[:0]
}

// CloseStatus returns the inner connection's close status.
func (c *SimConn) CloseStatus() (int, string) { return c.inner.CloseStatus() }
