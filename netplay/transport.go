package netplay

import "errors"

// MaxPacketSize is the largest datagram netplay sends, in bytes.
const MaxPacketSize = 1200

// ErrPacketTooLarge is returned by transports for datagrams over MaxPacketSize.
var ErrPacketTooLarge = errors.New("netplay: packet exceeds MaxPacketSize")

// Transport sends and receives unreliable datagrams. Implementations may
// drop, delay, duplicate or reorder packets. Send must copy p if it keeps it.
type Transport interface {
	// Send queues one datagram for the other side.
	Send(p []byte) error
	// Recv copies the next received datagram into buf and returns its length.
	// It never blocks; ok is false when nothing is waiting.
	Recv(buf []byte) (n int, ok bool)
}

const loopbackSlots = 256

// Loopback is one end of an in-process, zero-latency transport pair. When the
// receiving queue is full, Send drops the new datagram, as a lossy link would.
// Steady-state use does not allocate.
type Loopback struct {
	peer *Loopback
	buf  [loopbackSlots][MaxPacketSize]byte
	lens [loopbackSlots]int
	head int
	n    int
}

// NewLoopbackPair returns two connected Loopback ends.
func NewLoopbackPair() (a, b *Loopback) {
	a, b = &Loopback{}, &Loopback{}
	a.peer, b.peer = b, a
	return a, b
}

// Send implements Transport.
func (l *Loopback) Send(p []byte) error {
	if len(p) > MaxPacketSize {
		return ErrPacketTooLarge
	}
	q := l.peer
	if q.n == loopbackSlots {
		return nil
	}
	i := (q.head + q.n) % loopbackSlots
	q.lens[i] = copy(q.buf[i][:], p)
	q.n++
	return nil
}

// Recv implements Transport.
func (l *Loopback) Recv(buf []byte) (int, bool) {
	if l.n == 0 {
		return 0, false
	}
	n := copy(buf, l.buf[l.head][:l.lens[l.head]])
	l.head = (l.head + 1) % loopbackSlots
	l.n--
	return n, true
}
