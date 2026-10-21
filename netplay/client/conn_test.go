//go:build !(js && wasm)

package client

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/hubnet"
	"m31labs.dev/gosx/netplay/signal"
)

// fakeLink is a scripted browser data channel.
type fakeLink struct {
	mu       sync.Mutex
	cfg      rtcConfig
	opened   bool
	onSignal func(string, []byte)
	onState  func(string)
	sent     [][]byte
	incoming [][]byte
	signals  []string
	closed   bool
	refuse   bool // Send reports a drop
}

func (f *fakeLink) Open(cfg rtcConfig, onSignal func(string, []byte), onState func(string)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg, f.onSignal, f.onState, f.opened = cfg, onSignal, onState, true
	return nil
}
func (f *fakeLink) Signal(event string, data []byte) {
	f.mu.Lock()
	f.signals = append(f.signals, event)
	f.mu.Unlock()
}
func (f *fakeLink) Send(p []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse {
		return false
	}
	f.sent = append(f.sent, append([]byte(nil), p...))
	return true
}
func (f *fakeLink) Recv(buf []byte) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.incoming) == 0 {
		return 0, false
	}
	n := copy(buf, f.incoming[0])
	f.incoming = f.incoming[1:]
	return n, true
}
func (f *fakeLink) Close() { f.mu.Lock(); f.closed = true; f.mu.Unlock() }

func (f *fakeLink) state(s string) {
	f.mu.Lock()
	cb := f.onState
	f.mu.Unlock()
	cb(s)
}
func (f *fakeLink) isOpened() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.opened }
func (f *fakeLink) sentCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.sent) }
func (f *fakeLink) gotSignals() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.signals...)
}

// relayRig is a hub with signaling and fallback handlers, like a real relay
// without pion.
type relayRig struct {
	url    string
	hub    *hub.Hub
	sig    *hubnet.Signaling
	fb     *hubnet.Fallback
	mu     sync.Mutex
	client string
	offers []*signal.Offer
	hellos int
	// onOffer replies to an offer; nil means no reply.
	onOffer func(r *relayRig, id string, o *signal.Offer)
	config  *signal.Config // sent for hello (Session is filled in)
}

func newRelayRig(t *testing.T) *relayRig {
	t.Helper()
	r := &relayRig{hub: hub.New("clienttest")}
	r.fb = hubnet.AttachFallback(r.hub, nil)
	r.sig = hubnet.AttachSignaling(r.hub, func(id string, m signal.Message) {
		r.mu.Lock()
		r.client = id
		cfg, onOffer := r.config, r.onOffer
		r.mu.Unlock()
		switch m := m.(type) {
		case *signal.Hello:
			r.mu.Lock()
			r.hellos++
			r.mu.Unlock()
			if cfg != nil {
				c := *cfg
				c.Session = m.Session
				_ = r.sig.Send(id, &c)
			}
		case *signal.Offer:
			r.mu.Lock()
			r.offers = append(r.offers, m)
			r.mu.Unlock()
			if onOffer != nil {
				onOffer(r, id, m)
			}
		}
	})
	ts := httptest.NewServer(r.hub)
	t.Cleanup(ts.Close)
	r.url = "ws" + strings.TrimPrefix(ts.URL, "http")
	return r
}

func (r *relayRig) clientID() string { r.mu.Lock(); defer r.mu.Unlock(); return r.client }

func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func dial(t *testing.T, r *relayRig, link *fakeLink, mod func(*Options)) (*Conn, *modeLog) {
	t.Helper()
	ml := &modeLog{}
	o := Options{HubURL: r.url, Session: "sess", FallbackAfter: 200 * time.Millisecond, ConfigWait: 50 * time.Millisecond, OnMode: ml.add}
	o.link = func() (rtcLink, error) {
		if link == nil {
			return nil, errNoBrowser
		}
		return link, nil
	}
	if mod != nil {
		mod(&o)
	}
	c, err := Dial(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, ml
}

type modeLog struct {
	mu sync.Mutex
	m  []Mode
}

func (l *modeLog) add(m Mode) { l.mu.Lock(); l.m = append(l.m, m); l.mu.Unlock() }
func (l *modeLog) list() []Mode {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Mode(nil), l.m...)
}

func recvOne(c *Conn) ([]byte, bool) {
	buf := make([]byte, netplay.MaxPacketSize)
	n, ok := c.Recv(buf)
	return append([]byte(nil), buf[:n]...), ok
}

func TestFallsBackToWebSocketAfterTimeout(t *testing.T) {
	r := newRelayRig(t)
	link := &fakeLink{} // opens its channel never
	c, ml := dial(t, r, link, nil)
	if c.Mode() != ModeConnecting || c.Degraded() {
		t.Fatalf("mode %v right after Dial", c.Mode())
	}
	// Before the timeout, Send drops silently instead of using the WebSocket.
	if err := c.Send([]byte("early")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "degraded mode", func() bool { return c.Mode() == ModeWebSocket })
	if !c.Degraded() {
		t.Fatal("WebSocket mode must report Degraded")
	}
	waitFor(t, "client id", func() bool { return r.clientID() != "" })
	peer := r.fb.Peer(r.clientID())
	if peer.Active() {
		t.Fatalf("early packet leaked to the WebSocket")
	}

	if err := c.Send([]byte("hello relay")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	waitFor(t, "relay receives fallback frame", func() bool { n, ok := peer.Recv(buf); return ok && string(buf[:n]) == "hello relay" })
	_ = peer.Send([]byte("hello client"))
	waitFor(t, "client receives fallback frame", func() bool { p, ok := recvOne(c); return ok && string(p) == "hello client" })
	if got := ml.list(); len(got) != 1 || got[0] != ModeWebSocket {
		t.Fatalf("mode callbacks %v", got)
	}
}

func TestWebRTCPathWhenChannelOpens(t *testing.T) {
	r := newRelayRig(t)
	r.config = &signal.Config{ICEServers: []signal.ICEServer{{URLs: []string{"stun:relay.example:3478"}}}}
	r.onOffer = func(r *relayRig, id string, o *signal.Offer) {
		_ = r.sig.Send(id, &signal.Answer{Session: o.Session, SDP: "v=0\r\n"})
	}
	link := &fakeLink{}
	c, ml := dial(t, r, link, nil)
	waitFor(t, "link opened", link.isOpened)
	if link.cfg.Session != "sess" || len(link.cfg.ICEServers) != 1 || link.cfg.ICEServers[0].URLs[0] != "stun:relay.example:3478" {
		t.Fatalf("link config %+v (the relay's ICE servers must reach the browser)", link.cfg)
	}
	// The browser's offer goes out over the hub.
	link.onSignal(signal.EventOffer, []byte(`{"session":"sess","sdp":"v=0\r\nbrowser"}`))
	waitFor(t, "offer at relay", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.offers) == 1 })
	if !strings.Contains(r.offers[0].SDP, "browser") {
		t.Fatalf("offer %+v", r.offers[0])
	}
	// The relay's answer is handed to the link.
	waitFor(t, "answer at link", func() bool { s := link.gotSignals(); return len(s) == 1 && s[0] == signal.EventAnswer })

	link.state(linkOpen)
	waitFor(t, "WebRTC mode", func() bool { return c.Mode() == ModeWebRTC })
	if c.Degraded() {
		t.Fatal("WebRTC must not be degraded")
	}
	if err := c.Send([]byte("via rtc")); err != nil {
		t.Fatal(err)
	}
	if link.sentCount() != 1 || !bytes.Equal(link.sent[0], []byte("via rtc")) {
		t.Fatalf("link sent %q", link.sent)
	}
	link.mu.Lock()
	link.incoming = [][]byte{[]byte("from rtc")}
	link.mu.Unlock()
	if p, ok := recvOne(c); !ok || string(p) != "from rtc" {
		t.Fatalf("recv %q %v", p, ok)
	}
	if got := ml.list(); len(got) != 1 || got[0] != ModeWebRTC {
		t.Fatalf("mode callbacks %v", got)
	}
	// Invalid oversize datagrams are refused in every mode.
	if err := c.Send(make([]byte, netplay.MaxPacketSize+1)); err != netplay.ErrPacketTooLarge {
		t.Fatalf("oversize: %v", err)
	}
}

func TestRelayFailureFallsBackImmediately(t *testing.T) {
	r := newRelayRig(t)
	r.onOffer = func(r *relayRig, id string, o *signal.Offer) {
		_ = r.sig.Send(id, &signal.Failure{Session: o.Session, Code: signal.CodeUnavailable})
	}
	link := &fakeLink{}
	c, _ := dial(t, r, link, func(o *Options) { o.FallbackAfter = 30 * time.Second })
	waitFor(t, "link opened", link.isOpened)
	link.onSignal(signal.EventOffer, []byte(`{"session":"sess","sdp":"v=0\r\n"}`))
	waitFor(t, "degraded mode from failure message", func() bool { return c.Mode() == ModeWebSocket })
	link.mu.Lock()
	closed := link.closed
	link.mu.Unlock()
	if !closed {
		t.Fatal("the failed link was not closed")
	}
}

func TestLinkFailureFallsBackAndLateOpenUpgrades(t *testing.T) {
	r := newRelayRig(t)
	link := &fakeLink{}
	c, ml := dial(t, r, link, func(o *Options) { o.FallbackAfter = 30 * time.Second })
	waitFor(t, "link opened", link.isOpened)
	link.state(linkFailed)
	waitFor(t, "degraded mode", func() bool { return c.Mode() == ModeWebSocket })
	link.state(linkOpen) // e.g. a delayed ICE success
	waitFor(t, "upgrade", func() bool { return c.Mode() == ModeWebRTC })
	link.state(linkClosed)
	waitFor(t, "degrade again", func() bool { return c.Mode() == ModeWebSocket })
	got := ml.list()
	want := []Mode{ModeWebSocket, ModeWebRTC, ModeWebSocket}
	if len(got) != len(want) {
		t.Fatalf("mode callbacks %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mode callbacks %v, want %v", got, want)
		}
	}
}

func TestMissingBindingStillFallsBack(t *testing.T) {
	r := newRelayRig(t)
	c, _ := dial(t, r, nil, nil)
	waitFor(t, "degraded mode", func() bool { return c.Mode() == ModeWebSocket })
}

func TestBindingThatLoadsLateIsUsed(t *testing.T) {
	r := newRelayRig(t)
	link := &fakeLink{}
	var mu sync.Mutex
	tries := 0
	c, _ := dial(t, r, link, func(o *Options) {
		o.FallbackAfter = 5 * time.Second
		o.link = func() (rtcLink, error) {
			mu.Lock()
			defer mu.Unlock()
			tries++
			if tries < 4 {
				return nil, errNoBrowser
			}
			return link, nil
		}
	})
	waitFor(t, "link opened after retries", link.isOpened)
	link.state(linkOpen)
	waitFor(t, "WebRTC mode", func() bool { return c.Mode() == ModeWebRTC })
}

func TestRelayConfigSetsFallbackTimeout(t *testing.T) {
	r := newRelayRig(t)
	r.config = &signal.Config{FallbackAfterMs: 120}
	c, _ := dial(t, r, &fakeLink{}, func(o *Options) { o.FallbackAfter = 0; o.ConfigWait = time.Second })
	start := time.Now()
	waitFor(t, "degraded mode", func() bool { return c.Mode() == ModeWebSocket })
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v with a 120 ms relay timeout", d)
	}
}

func TestCloseStopsEverything(t *testing.T) {
	r := newRelayRig(t)
	link := &fakeLink{}
	c, _ := dial(t, r, link, nil)
	waitFor(t, "link opened", link.isOpened)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.Mode() != ModeClosed {
		t.Fatalf("mode %v", c.Mode())
	}
	if err := c.Send([]byte{1}); err != ErrClosed {
		t.Fatalf("send after close: %v", err)
	}
	if _, ok := recvOne(c); ok {
		t.Fatal("recv after close returned data")
	}
	link.mu.Lock()
	closed := link.closed
	link.mu.Unlock()
	if !closed {
		t.Fatal("link left open")
	}
	_ = c.Close() // idempotent
}

func TestDialRejectsBadOptions(t *testing.T) {
	if _, err := Dial(Options{}); err == nil {
		t.Fatal("empty HubURL accepted")
	}
	if _, err := Dial(Options{HubURL: "ws://x", Session: "bad session!"}); err == nil {
		t.Fatal("bad session accepted")
	}
	if _, err := Dial(Options{HubURL: "ws://x", FallbackAfter: -1}); err == nil {
		t.Fatal("negative FallbackAfter accepted")
	}
}

func TestSteadyStateSendRecvAllocatesNothingOnWebSocketPath(t *testing.T) {
	r := newRelayRig(t)
	c, _ := dial(t, r, nil, nil)
	waitFor(t, "degraded mode", func() bool { return c.Mode() == ModeWebSocket })
	buf := make([]byte, netplay.MaxPacketSize)
	p := []byte("0123456789")
	_ = c.Send(p)
	if a := testing.AllocsPerRun(50, func() { c.Recv(buf) }); a != 0 {
		t.Fatalf("Recv allocates %v per call", a)
	}
}
