package webrtc

import (
	"errors"
	"sync"
	"sync/atomic"

	pion "github.com/pion/webrtc/v4"

	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/dgram"
)

// Data channel parameters. Both ends create the channel as negotiated, so
// neither side depends on which one sends the offer.
const (
	ChannelLabel = "netplay"
	ChannelID    = 0

	// maxBuffered is the bytes queued in the data channel above which Send
	// drops the datagram. Old input is worth less than fresh input, so a
	// congested link drops instead of adding latency.
	maxBuffered = 16 * 1024

	defaultQueueLen = 128
)

// ErrClosed is returned by Send after a Peer closes.
var ErrClosed = errors.New("netplay/webrtc: peer closed")

// channelInit returns the unordered, zero-retransmit channel parameters.
func channelInit() *pion.DataChannelInit {
	ordered, negotiated := false, true
	maxRetransmits, id := uint16(0), uint16(ChannelID)
	return &pion.DataChannelInit{Ordered: &ordered, MaxRetransmits: &maxRetransmits, Negotiated: &negotiated, ID: &id}
}

// PeerStats are counters for one Peer.
type PeerStats struct {
	Sent, Received       uint64
	SendDropped          uint64 // datagrams refused because the channel was congested or closed
	ReceiveDropped       uint64 // datagrams dropped because the receive queue was full or oversize
	SendBytes, RecvBytes uint64
}

// Peer is one WebRTC data channel. It implements netplay.Transport: Send hands
// a datagram to the channel and Recv takes the next received one without
// blocking. Both are safe to call from the game loop while pion's goroutines
// deliver packets.
type Peer struct {
	id      string
	session string
	pc      *pion.PeerConnection
	dc      *pion.DataChannel
	in      *dgram.Queue

	done      chan struct{}
	closeOnce sync.Once
	err       atomic.Pointer[error]
	onClose   func()
	sig       *hubSignaler // set by DialHub; nil for a server-side Peer

	sent, recv, sendDrop, sendBytes, recvBytes atomic.Uint64
}

var _ netplay.Transport = (*Peer)(nil)

func newPeer(id, session string, pc *pion.PeerConnection, dc *pion.DataChannel, queueLen int) *Peer {
	if queueLen <= 0 {
		queueLen = defaultQueueLen
	}
	p := &Peer{id: id, session: session, pc: pc, dc: dc, in: dgram.NewQueue(queueLen), done: make(chan struct{})}
	dc.OnMessage(func(m pion.DataChannelMessage) {
		p.recv.Add(1)
		p.recvBytes.Add(uint64(len(m.Data)))
		p.in.Push(m.Data)
	})
	return p
}

// ID returns the hub client ID the peer connected through ("" for a client-side Peer).
func (p *Peer) ID() string { return p.id }

// Session returns the session label from signaling.
func (p *Peer) Session() string { return p.session }

// Done is closed when the peer closes.
func (p *Peer) Done() <-chan struct{} { return p.done }

// Err returns why the peer closed, or nil while it is open or after Close.
func (p *Peer) Err() error {
	if e := p.err.Load(); e != nil {
		return *e
	}
	return nil
}

// Send implements netplay.Transport. It drops the datagram, returning nil,
// when the channel has too much queued; it returns an error when the channel
// is closed or p is over netplay.MaxPacketSize.
func (p *Peer) Send(b []byte) error {
	if len(b) > netplay.MaxPacketSize {
		return netplay.ErrPacketTooLarge
	}
	select {
	case <-p.done:
		return ErrClosed
	default:
	}
	if p.dc.BufferedAmount() > maxBuffered {
		p.sendDrop.Add(1)
		return nil
	}
	if err := p.dc.Send(b); err != nil {
		p.sendDrop.Add(1)
		return err
	}
	p.sent.Add(1)
	p.sendBytes.Add(uint64(len(b)))
	return nil
}

// Recv implements netplay.Transport.
func (p *Peer) Recv(buf []byte) (int, bool) { return p.in.Pop(buf) }

// Stats returns the peer's counters.
func (p *Peer) Stats() PeerStats {
	return PeerStats{
		Sent: p.sent.Load(), Received: p.recv.Load(), SendDropped: p.sendDrop.Load(),
		ReceiveDropped: p.in.Dropped(), SendBytes: p.sendBytes.Load(), RecvBytes: p.recvBytes.Load(),
	}
}

// Close closes the data channel and the connection.
func (p *Peer) Close() error { return p.shutdown(nil) }

func (p *Peer) shutdown(cause error) error {
	var err error
	p.closeOnce.Do(func() {
		if cause != nil {
			p.err.Store(&cause)
		}
		close(p.done)
		err = p.pc.Close()
		if p.onClose != nil {
			p.onClose()
		}
	})
	return err
}
