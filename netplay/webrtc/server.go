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
	// NextBinary receives the hub binary frames that are not netplay's
	// fallback frames, so an app's own binary protocol keeps working after
	// AttachHub. Optional.
	NextBinary hub.BinaryMessageHandler
	// ConfigureICE may adjust pion's setting engine: UDP port range,
	// NAT1To1IPs behind a fixed public address, interface filters. Optional.
	ConfigureICE func(*pion.SettingEngine)
	// OnHello, if set, is called for each valid hello a hub client sends,
	// once per hub connection (the client sends it again after a hub
	// reconnect, with a new client ID). previousClientID is the client that
	// owned session before this hello, or "" if none: the app can seat the
	// player on clientID, or Rebind the seat from previousClientID once Leave
	// reported it gone.
	// It runs on the hub's handler goroutine and must return quickly.
	OnHello func(clientID, session, previousClientID string)
}

// Server is the relay-side answerer. It turns browser offers into data
// channels and hands each open channel to the game loop as a Peer.
type Server struct {
	cfg Config
	api *pion.API

	mu    sync.Mutex
	conns map[string]*serverConn
	links map[string]*Link
	// announced maps the session each hub client named in its hello, in both
	// directions. A later hello for a session replaces the earlier owner.
	// superseded holds, per session, the previous owner after a newer hello
	// took the session, so Rebind can still move the seat from it. Only one
	// per session is kept.
	byClient   map[string]string
	bySession  map[string]string
	superseded map[string]string
	// left marks hub clients the app reported gone with Leave; only such a
	// client's seat can be moved by Rebind.
	left   map[string]bool
	fb     *hubnet.Fallback
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

// ID returns the hub client ID the connection serves. It changes on Rebind.
func (c *serverConn) ID() string {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	return c.id
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
		conns: map[string]*serverConn{}, links: map[string]*Link{}, accept: make(chan *Peer, 16),
	}, nil
}

// AttachHub routes the netplay signaling events of h to the server and installs
// the WebSocket fallback's binary frame handler (Config.NextBinary gets the
// other binary frames). It does not install "join" or "leave" handlers; call
// Remove from your own leave handler when a hub client disconnects.
func (s *Server) AttachHub(h *hub.Hub) *hubnet.Signaling {
	s.mu.Lock()
	s.fb = hubnet.AttachFallback(h, s.cfg.NextBinary)
	s.mu.Unlock()
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
		prev := s.announce(clientID, m.Session)
		if s.cfg.OnHello != nil {
			s.cfg.OnHello(clientID, m.Session, prev)
		}
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

// maxAnnounced bounds the hello table, so a client that sends many session
// names cannot grow it without limit. Remove frees an entry.
const maxAnnounced = 4096

func (s *Server) announce(clientID, session string) (previous string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.byClient == nil {
		s.byClient, s.bySession, s.superseded, s.left = map[string]string{}, map[string]string{}, map[string]string{}, map[string]bool{}
	}
	if old, ok := s.byClient[clientID]; ok && old != session && s.bySession[old] == clientID {
		delete(s.bySession, old)
	}
	if prev, ok := s.bySession[session]; ok && prev != clientID {
		previous = prev
		// The session moved to a new hub client. Keep the previous owner
		// (one per session) so Rebind can move its seat.
		if older, ok := s.superseded[session]; ok {
			delete(s.byClient, older)
			delete(s.left, older)
		}
		s.superseded[session] = prev
	}
	// The cap applies to new sessions only: a reconnect, which replaces an
	// owner above or an entry of this client, is never refused.
	if _, known := s.byClient[clientID]; !known && len(s.byClient) >= maxAnnounced {
		if _, seated := s.bySession[session]; !seated {
			return
		}
	}
	s.byClient[clientID], s.bySession[session] = session, clientID
	return previous
}

// ClientBySession returns the hub client that most recently announced session
// in a hello, and false if none did or that client was removed. After a hub
// reconnect the same session maps to the new client ID, so a relay can point a
// seat at it. Treat a session name as a label, not a credential: any client can
// announce any name, so authenticate the seat in your own join handler.
func (s *Server) ClientBySession(session string) (clientID string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clientID, ok = s.bySession[session]
	return clientID, ok
}

// SessionOf returns the session a hub client named in its latest hello. A
// client whose session a newer hello took keeps its entry (one per session)
// until Remove or Rebind, so a seat can still be moved from it.
func (s *Server) SessionOf(clientID string) (session string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok = s.byClient[clientID]
	return session, ok
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
	peer.onClose = func() { c.detach(); s.unlinkPeer(c.ID(), peer) }
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
	c.s.Link(c.ID()).peer.Store(p)
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

// Errors returned by Rebind.
var (
	ErrUnknownClient   = errors.New("netplay/webrtc: unknown hub client")
	ErrSessionMismatch = errors.New("netplay/webrtc: the new client did not announce the same session")
	// ErrClientLive: the old client has not been reported gone with Leave.
	ErrClientLive = errors.New("netplay/webrtc: the old client is still on the hub; call Leave when it leaves")
	// ErrDefaultSession: the session is the default label "netplay", which
	// many unrelated players share, so it cannot identify a seat.
	ErrDefaultSession = errors.New("netplay/webrtc: the default session label cannot identify a seat; use a seat token")
	// ErrSeatTaken: the new client already has a Link or connection.
	ErrSeatTaken = errors.New("netplay/webrtc: the new client already has a Link or connection")
)

// DefaultSession is the label netplay/client uses when Options.Session is
// empty. Rebind refuses it.
const DefaultSession = "netplay"

// Leave tells the server that a hub client left (call it from your hub
// "leave" handler instead of Remove) and starts the reconnect grace: the
// seat stays, with its Link and data channel, until you Rebind it to a
// client that reconnects with the same session token, or call Remove when
// the grace ends. Leave returns the newest other client that announced the
// same session, or "" if none has; if it returns one, call Rebind(oldID,
// thatID).
func (s *Server) Leave(oldID string) (newID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byClient[oldID]
	if !ok {
		return ""
	}
	if s.left == nil {
		s.left = map[string]bool{}
	}
	s.left[oldID] = true
	if cur := s.bySession[sess]; cur != oldID {
		return cur
	}
	return ""
}

// Rebind moves a seat from the hub client oldID to newID after a hub
// reconnect: the Link (and with it the relay's transport, the WebRTC data
// channel and the WebSocket fallback) now serves newID, and the relay keeps
// using the same Link.
//
// Rebind changes nothing and returns an error unless all of these hold:
// both clients said hello with the same session (ErrSessionMismatch); the
// session is not the default label "netplay" (ErrDefaultSession); the app
// reported the old client gone with Leave (ErrClientLive), so a live client
// cannot lose its seat to another that names the same session; and newID has
// no Link or connection yet (ErrSeatTaken), so do not call Link(newID)
// before Rebind.
//
// Use it with OnHello and Leave: in OnHello, try Rebind(oldID, newID) when the
// session already has an owner; in your hub leave handler, call Leave(id) and,
// if it returns a client, Rebind(id, thatClient). Remove the seat when the
// reconnect grace ends (at most the session stall timeout, 5 s by default).
//
// A session name is only as private as you make it: any client can announce
// any name. Use an unguessable seat token from your matchmaker as the
// session, or check the new client's authenticated identity before Rebind.
func (s *Server) Rebind(oldID, newID string) error {
	if oldID == newID {
		return nil
	}
	s.mu.Lock()
	so, okOld := s.byClient[oldID]
	sn, okNew := s.byClient[newID]
	if !okOld || !okNew {
		s.mu.Unlock()
		return ErrUnknownClient
	}
	if so != sn || so == "" {
		s.mu.Unlock()
		return ErrSessionMismatch
	}
	if so == DefaultSession {
		s.mu.Unlock()
		return ErrDefaultSession
	}
	if !s.left[oldID] {
		s.mu.Unlock()
		return ErrClientLive
	}
	if s.links[newID] != nil || s.conns[newID] != nil {
		s.mu.Unlock()
		return ErrSeatTaken
	}
	l, c, fb := s.links[oldID], s.conns[oldID], s.fb
	if l == nil && c == nil {
		s.mu.Unlock()
		return ErrUnknownClient
	}
	if l != nil {
		delete(s.links, oldID)
		l.id.Store(&newID)
		s.links[newID] = l
	}
	if c != nil {
		delete(s.conns, oldID)
		c.id = newID
		s.conns[newID] = c
		c.mu.Lock()
		if c.peer != nil {
			c.peer.id.Store(&newID)
		}
		c.mu.Unlock()
	}
	delete(s.byClient, oldID)
	delete(s.left, oldID)
	if s.superseded[so] == oldID {
		delete(s.superseded, so)
	}
	s.bySession[so] = newID
	s.byClient[newID] = so
	s.mu.Unlock()
	if fb != nil {
		fb.Forget(oldID)
	}
	return nil
}

// Remove closes the connection of a hub client that left and forgets its Link.
func (s *Server) Remove(clientID string) {
	s.mu.Lock()
	c := s.conns[clientID]
	delete(s.conns, clientID)
	delete(s.links, clientID)
	delete(s.left, clientID)
	if sess, ok := s.byClient[clientID]; ok {
		delete(s.byClient, clientID)
		if s.bySession[sess] == clientID {
			delete(s.bySession, sess)
		}
		if s.superseded[sess] == clientID {
			delete(s.superseded, sess)
		}
	}
	fb := s.fb
	s.mu.Unlock()
	if fb != nil {
		fb.Forget(clientID)
	}
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
