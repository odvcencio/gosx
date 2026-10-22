package hubnet

import (
	"sync"
	"sync/atomic"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/dgram"
)

// FrameTag is the first byte of every netplay datagram frame on a hub
// WebSocket. The hub reserves prefixes 1 to 255 for CRDT sync, so application
// frames start with 0x00.
const FrameTag byte = 0

// MaxFallbackPeers bounds the peers one Fallback keeps.
const MaxFallbackPeers = 256

const fallbackQueueLen = 128

// Fallback is the server side of netplay's WebSocket fallback. It carries
// netplay datagrams in hub binary frames: FrameTag, then the packet.
type Fallback struct {
	h    *hub.Hub
	next hub.BinaryMessageHandler

	mu    sync.Mutex
	peers map[string]*FallbackPeer
}

// AttachFallback installs the fallback's binary frame handler on h. Frames that
// do not start with FrameTag go to next (which may be nil), so an app's own
// binary protocol keeps working.
func AttachFallback(h *hub.Hub, next hub.BinaryMessageHandler) *Fallback {
	f := &Fallback{h: h, next: next, peers: map[string]*FallbackPeer{}}
	h.SetBinaryMessageHandler(f.handle)
	return f
}

func (f *Fallback) handle(c *hub.Client, data []byte) bool {
	if len(data) == 0 || data[0] != FrameTag {
		return f.next != nil && f.next(c, data)
	}
	if c == nil {
		return true
	}
	p := f.Peer(c.ID)
	p.active.Store(true)
	p.in.Push(data[1:]) // oversize frames are dropped and counted
	return true
}

// Peer returns the datagram link for a hub client, creating it on first use.
// Call Forget when the client leaves.
func (f *Fallback) Peer(clientID string) *FallbackPeer {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.peers[clientID]; ok {
		return p
	}
	if len(f.peers) >= MaxFallbackPeers {
		for id := range f.peers { // evict an arbitrary peer: the cap only matters under abuse
			delete(f.peers, id)
			break
		}
	}
	p := &FallbackPeer{h: f.h, id: clientID, in: dgram.NewQueue(fallbackQueueLen)}
	f.peers[clientID] = p
	return p
}

// Forget drops the link for a client that left.
func (f *Fallback) Forget(clientID string) {
	f.mu.Lock()
	delete(f.peers, clientID)
	f.mu.Unlock()
}

// FallbackPeer is one client's WebSocket datagram link. It implements
// netplay.Transport.
type FallbackPeer struct {
	h      *hub.Hub
	id     string
	in     *dgram.Queue
	active atomic.Bool

	mu  sync.Mutex
	buf [1 + netplay.MaxPacketSize]byte
}

var _ netplay.Transport = (*FallbackPeer)(nil)

// Active reports whether the client has sent at least one fallback frame, which
// is how the client says it has switched to the WebSocket path.
func (p *FallbackPeer) Active() bool { return p.active.Load() }

// Send implements netplay.Transport. A full client buffer drops the frame.
func (p *FallbackPeer) Send(b []byte) error {
	if len(b) > netplay.MaxPacketSize {
		return netplay.ErrPacketTooLarge
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf[0] = FrameTag
	n := 1 + copy(p.buf[1:], b)
	p.h.SendBinary(p.id, p.buf[:n])
	return nil
}

// Recv implements netplay.Transport.
func (p *FallbackPeer) Recv(buf []byte) (int, bool) { return p.in.Pop(buf) }

// Dropped returns the inbound datagrams lost to a full queue or oversize frame.
func (p *FallbackPeer) Dropped() uint64 { return p.in.Dropped() }
