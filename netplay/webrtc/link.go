package webrtc

import (
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/client"
	"m31labs.dev/gosx/netplay/hubnet"
)

// wsPreference is how long after the last inbound WebSocket datagram a Link
// keeps answering on the WebSocket instead of an open data channel. A client
// that fell back sends on the WebSocket; the relay must answer on the path the
// client is reading even while pion still believes the channel is open.
const wsPreference = time.Second

// Link is the relay's transport for one hub client. It prefers the client's
// WebRTC data channel and answers on the hub WebSocket when the client uses
// that path (the degraded fallback). It implements netplay.Transport, so
// pass it to netplay.RelayConfig.Peers.
//
// Get one with Server.Link(clientID). A Link exists before the data channel
// opens; until either path carries traffic, Send drops datagrams.
type Link struct {
	id   string
	s    *Server
	peer atomic.Pointer[Peer]

	lastWS, lastRTC atomic.Int64 // monotonic nanoseconds of the last inbound datagram per path
}

var _ netplay.Transport = (*Link)(nil)

// Link returns the transport for a hub client, creating it on first use. Call
// Remove when the client leaves.
func (s *Server) Link(clientID string) *Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.links[clientID]
	if !ok {
		l = &Link{id: clientID, s: s}
		s.links[clientID] = l
	}
	return l
}

func (s *Server) unlinkPeer(clientID string, p *Peer) {
	s.mu.Lock()
	l := s.links[clientID]
	s.mu.Unlock()
	if l != nil {
		l.peer.CompareAndSwap(p, nil)
	}
}

func (l *Link) fallback() *hubnet.FallbackPeer {
	l.s.mu.Lock()
	fb := l.s.fb
	l.s.mu.Unlock()
	if fb == nil {
		return nil
	}
	return fb.Peer(l.id)
}

var epoch = time.Now()

func nowNanos() int64 { return int64(time.Since(epoch)) + 1 }

// Mode reports the path in use: ModeWebRTC while the data channel is open and
// the client reads it, ModeWebSocket when the client has fallen back, and
// ModeConnecting before either carries traffic.
func (l *Link) Mode() client.Mode {
	if l.useRTC() {
		return client.ModeWebRTC
	}
	if fb := l.fallback(); fb != nil && fb.Active() {
		return client.ModeWebSocket
	}
	return client.ModeConnecting
}

// Degraded reports whether the client is on the WebSocket fallback.
func (l *Link) Degraded() bool { return l.Mode() == client.ModeWebSocket }

func (l *Link) openPeer() *Peer {
	p := l.peer.Load()
	if p == nil {
		return nil
	}
	select {
	case <-p.Done():
		return nil
	default:
		return p
	}
}

func (l *Link) useRTC() bool {
	if l.openPeer() == nil {
		return false
	}
	ws := l.lastWS.Load()
	return !(ws > l.lastRTC.Load() && nowNanos()-ws < int64(wsPreference))
}

// Send implements netplay.Transport.
func (l *Link) Send(b []byte) error {
	if len(b) > netplay.MaxPacketSize {
		return netplay.ErrPacketTooLarge
	}
	if l.useRTC() {
		return l.openPeer().Send(b)
	}
	if fb := l.fallback(); fb != nil && fb.Active() {
		return fb.Send(b)
	}
	return nil
}

// Recv implements netplay.Transport. It reads both paths.
func (l *Link) Recv(buf []byte) (int, bool) {
	if p := l.peer.Load(); p != nil {
		if n, ok := p.Recv(buf); ok {
			l.lastRTC.Store(nowNanos())
			return n, true
		}
	}
	if fb := l.fallback(); fb != nil {
		if n, ok := fb.Recv(buf); ok {
			l.lastWS.Store(nowNanos())
			return n, true
		}
	}
	return 0, false
}

// Peer returns the open data channel, or nil.
func (l *Link) Peer() *Peer { return l.openPeer() }
