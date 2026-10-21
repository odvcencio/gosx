package webrtc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pion "github.com/pion/webrtc/v4"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay/hubnet"
	"m31labs.dev/gosx/netplay/signal"
)

// Config configures a Server.
type Config struct {
	// ICEServers are the STUN and TURN servers. The relay uses them for its
	// own connections and sends them to each browser in a signal.Config
	// message, so TURN credentials stay in the app's server configuration.
	ICEServers []signal.ICEServer
	// FallbackAfter is how long a client waits for the data channel before it
	// uses the WebSocket fallback. Zero lets the client choose (4 s).
	FallbackAfter time.Duration
	// ConnectTimeout is how long the server keeps a connection that has not
	// opened its data channel (default 30 s).
	ConnectTimeout time.Duration
	// MaxPeers bounds concurrent connections, open or pending (default 64).
	MaxPeers int
	// QueueLen is the receive queue length of each Peer, in datagrams
	// (default 128).
	QueueLen int
	// ConfigureICE may adjust pion's setting engine: UDP port range,
	// NAT1To1IPs behind a fixed public address, interface filters. Optional.
	ConfigureICE func(*pion.SettingEngine)
}

// Server is the relay-side answerer. It turns browser offers into data
// channels and hands each open channel to the game loop as a Peer.
type Server struct {
	cfg Config
	api *pion.API

	mu     sync.Mutex
	conns  map[string]*serverConn
	accept chan *Peer
	closed bool
}

type serverConn struct {
	s       *Server
	id      string
	session string
	pc      *pion.PeerConnection
	reply   func(signal.Message) error
	timer   *time.Timer

	mu         sync.Mutex
	answered   bool
	pending    []*signal.Candidate
	candidates int
	peer       *Peer
	closed     bool
}

// maxRemoteCandidates bounds the trickled candidates accepted per connection.
const maxRemoteCandidates = 64

// NewServer returns a Server for cfg.
func NewServer(cfg Config) (*Server, error) {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 30 * time.Second
	}
	if cfg.MaxPeers == 0 {
		cfg.MaxPeers = 64
	}
	if cfg.FallbackAfter < 0 || cfg.FallbackAfter > time.Duration(signal.MaxFallbackMs)*time.Millisecond {
		return nil, fmt.Errorf("netplay/webrtc: FallbackAfter must be 0 to %d ms", signal.MaxFallbackMs)
	}
	probe := signal.Config{Session: "probe", ICEServers: cfg.ICEServers}
	if err := probe.Validate(); err != nil {
		return nil, err
	}
	return &Server{
		cfg: cfg, api: newAPI(cfg.ConfigureICE),
		conns: map[string]*serverConn{}, accept: make(chan *Peer, 16),
	}, nil
}

// AttachHub routes the netplay signaling events of h to the server. It does
// not install "join" or "leave" handlers; call Remove from your own leave
// handler when a hub client disconnects.
func (s *Server) AttachHub(h *hub.Hub) *hubnet.Signaling {
	var sig *hubnet.Signaling
	sig = hubnet.AttachSignaling(h, func(clientID string, m signal.Message) {
		s.Handle(clientID, m, func(r signal.Message) error { return sig.Send(clientID, r) })
	})
	return sig
}

// Accept blocks until a client's data channel opens and returns it.
func (s *Server) Accept(ctx context.Context) (*Peer, error) {
	select {
	case p := <-s.accept:
		return p, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Handle processes one validated signaling message from clientID. reply sends
// a message back to that client. Handle returns quickly: pion does the slow
// work on its own goroutines.
func (s *Server) Handle(clientID string, m signal.Message, reply func(signal.Message) error) {
	switch m := m.(type) {
	case *signal.Hello:
		cfg := &signal.Config{Session: m.Session, ICEServers: s.cfg.ICEServers, FallbackAfterMs: int(s.cfg.FallbackAfter / time.Millisecond)}
		_ = reply(cfg)
	case *signal.Offer:
		if err := s.offer(clientID, m, reply); err != nil {
			code := signal.CodeInternal
			if errors.Is(err, errBusy) {
				code = signal.CodeBusy
			}
			_ = reply(&signal.Failure{Session: m.Session, Code: code, Message: "webrtc setup failed"})
		}
	case *signal.Candidate:
		s.candidate(clientID, m)
	}
}

var errBusy = errors.New("netplay/webrtc: too many peers")

func (s *Server) offer(clientID string, m *signal.Offer, reply func(signal.Message) error) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("netplay/webrtc: server closed")
	}
	old := s.conns[clientID]
	if old == nil && len(s.conns) >= s.cfg.MaxPeers {
		s.mu.Unlock()
		return errBusy
	}
	delete(s.conns, clientID)
	s.mu.Unlock()
	if old != nil {
		old.close(nil)
	}

	pc, err := s.api.NewPeerConnection(pion.Configuration{ICEServers: pionServers(s.cfg.ICEServers)})
	if err != nil {
		return err
	}
	c := &serverConn{s: s, id: clientID, session: m.Session, pc: pc, reply: reply}
	dc, err := pc.CreateDataChannel(ChannelLabel, channelInit())
	if err != nil {
		_ = pc.Close()
		return err
	}
	peer := newPeer(clientID, m.Session, pc, dc, s.cfg.QueueLen)
	peer.onClose = func() { c.detach() }
	pc.OnICECandidate(c.onLocalCandidate)
	pc.OnConnectionStateChange(func(st pion.PeerConnectionState) {
		if st == pion.PeerConnectionStateFailed || st == pion.PeerConnectionStateClosed {
			c.close(fmt.Errorf("netplay/webrtc: connection %s", st))
		}
	})
	dc.OnOpen(func() { c.opened(peer) })

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = pc.Close()
		return errors.New("netplay/webrtc: server closed")
	}
	s.conns[clientID] = c
	s.mu.Unlock()
	c.timer = time.AfterFunc(s.cfg.ConnectTimeout, func() {
		c.mu.Lock()
		open := c.peer != nil
		c.mu.Unlock()
		if !open {
			c.close(errors.New("netplay/webrtc: connect timeout"))
		}
	})

	fail := func(err error) error { c.close(err); return err }
	if err := pc.SetRemoteDescription(pion.SessionDescription{Type: pion.SDPTypeOffer, SDP: m.SDP}); err != nil {
		return fail(err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return fail(err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return fail(err)
	}
	ld := pc.LocalDescription()
	if err := reply(&signal.Answer{Session: m.Session, SDP: ld.SDP}); err != nil {
		return fail(err)
	}
	c.mu.Lock()
	c.answered = true
	pending := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, cm := range pending {
		_ = reply(cm)
	}
	return nil
}

func (c *serverConn) onLocalCandidate(cand *pion.ICECandidate) {
	m := candidateMessage(c.session, cand)
	c.mu.Lock()
	if !c.answered {
		c.pending = append(c.pending, m)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	_ = c.reply(m)
}

func (c *serverConn) opened(p *Peer) {
	c.mu.Lock()
	if c.closed || c.peer != nil {
		c.mu.Unlock()
		return
	}
	c.peer = p
	c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
	select {
	case c.s.accept <- p:
	default:
		p.shutdown(errors.New("netplay/webrtc: accept queue full"))
	}
}

func (s *Server) candidate(clientID string, m *signal.Candidate) {
	s.mu.Lock()
	c := s.conns[clientID]
	s.mu.Unlock()
	if c == nil || c.session != m.Session || m.Candidate == "" {
		return
	}
	c.mu.Lock()
	c.candidates++
	over := c.candidates > maxRemoteCandidates
	c.mu.Unlock()
	if over {
		return
	}
	_ = c.pc.AddICECandidate(candidateInit(m))
}

// detach removes c from the server's table.
func (c *serverConn) detach() {
	c.s.mu.Lock()
	if c.s.conns[c.id] == c {
		delete(c.s.conns, c.id)
	}
	c.s.mu.Unlock()
}

func (c *serverConn) close(cause error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	peer := c.peer
	c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.detach()
	if peer != nil {
		peer.shutdown(cause)
		return
	}
	_ = c.pc.Close()
}

// Remove closes the connection of a hub client that left.
func (s *Server) Remove(clientID string) {
	s.mu.Lock()
	c := s.conns[clientID]
	delete(s.conns, clientID)
	s.mu.Unlock()
	if c != nil {
		c.close(nil)
	}
}

// Close closes every connection and stops accepting offers.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	conns := s.conns
	s.conns = map[string]*serverConn{}
	s.mu.Unlock()
	for _, c := range conns {
		c.close(nil)
	}
	return nil
}
