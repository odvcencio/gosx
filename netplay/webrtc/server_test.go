package webrtc

import (
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/netplay/signal"
)

type collector struct {
	mu   sync.Mutex
	msgs []signal.Message
}

func (c *collector) reply(m signal.Message) error {
	c.mu.Lock()
	c.msgs = append(c.msgs, m)
	c.mu.Unlock()
	return nil
}

func (c *collector) last() signal.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return nil
	}
	return c.msgs[len(c.msgs)-1]
}

func TestHelloReturnsAppICEConfig(t *testing.T) {
	servers := []signal.ICEServer{
		{URLs: []string{"stun:stun.example.org:3478"}},
		{URLs: []string{"turn:turn.example.org:3478"}, Username: "u", Credential: "secret"},
	}
	s, err := NewServer(Config{ICEServers: servers, FallbackAfter: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var c collector
	s.Handle("c1", &signal.Hello{Session: "x"}, c.reply)
	cfg, ok := c.last().(*signal.Config)
	if !ok {
		t.Fatalf("reply %T, want *signal.Config", c.last())
	}
	if len(cfg.ICEServers) != 2 || cfg.ICEServers[1].Credential != "secret" || cfg.FallbackAfterMs != 3000 || cfg.Session != "x" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestNewServerRejectsBadConfig(t *testing.T) {
	if _, err := NewServer(Config{ICEServers: []signal.ICEServer{{URLs: []string{"http://nope"}}}}); err == nil {
		t.Fatal("accepted a non-STUN/TURN url")
	}
	if _, err := NewServer(Config{FallbackAfter: -time.Second}); err == nil {
		t.Fatal("accepted negative FallbackAfter")
	}
}

func TestOfferWithBadSDPReportsFailure(t *testing.T) {
	s, _ := NewServer(Config{})
	defer s.Close()
	var c collector
	s.Handle("c1", &signal.Offer{Session: "x", SDP: "v=0\r\ngarbage"}, c.reply)
	f, ok := c.last().(*signal.Failure)
	if !ok || f.Code != signal.CodeInternal {
		t.Fatalf("reply %#v, want internal failure", c.last())
	}
	s.mu.Lock()
	n := len(s.conns)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d connections left after a failed offer", n)
	}
}

func TestMaxPeersReportsBusy(t *testing.T) {
	r := connectPlayers(t, "busy")
	r.server.cfg.MaxPeers = 2 // both slots are in use
	var c collector
	r.server.Handle("third", &signal.Offer{Session: "x", SDP: "v=0\r\n"}, c.reply)
	f, ok := c.last().(*signal.Failure)
	if !ok || f.Code != signal.CodeBusy {
		t.Fatalf("reply %#v, want busy", c.last())
	}
}

func TestCandidateWithoutConnectionIsIgnored(t *testing.T) {
	s, _ := NewServer(Config{})
	defer s.Close()
	s.Handle("nobody", &signal.Candidate{Session: "x", Candidate: "candidate:1 1 udp 1 1.2.3.4 5 typ host"}, func(signal.Message) error { return nil })
}

func TestRemoveClosesPeer(t *testing.T) {
	r := connectPlayers(t, "rm")
	id := r.relay[0].ID()
	r.server.Remove(id)
	select {
	case <-r.relay[0].Done():
	case <-time.After(2 * time.Second):
		t.Fatal("peer did not close")
	}
	if err := r.relay[0].Send([]byte{1}); err != ErrClosed {
		t.Fatalf("send after close: %v", err)
	}
}

func TestClientBySessionFollowsHelloAndReconnect(t *testing.T) {
	var got [][2]string
	s, err := NewServer(Config{OnHello: func(id, sess, _ string) { got = append(got, [2]string{id, sess}) }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var c collector
	if _, ok := s.ClientBySession("seat-a"); ok {
		t.Fatal("unknown session found")
	}
	s.Handle("c1", &signal.Hello{Session: "seat-a"}, c.reply)
	s.Handle("c2", &signal.Hello{Session: "seat-b"}, c.reply)
	if id, ok := s.ClientBySession("seat-a"); !ok || id != "c1" {
		t.Fatalf("seat-a -> %q %v", id, ok)
	}
	if sess, ok := s.SessionOf("c2"); !ok || sess != "seat-b" {
		t.Fatalf("c2 -> %q %v", sess, ok)
	}
	// The hub client reconnects with a new ID and says hello again.
	s.Handle("c3", &signal.Hello{Session: "seat-a"}, c.reply)
	if id, _ := s.ClientBySession("seat-a"); id != "c3" {
		t.Fatalf("seat-a after reconnect -> %q, want c3", id)
	}
	if id, _ := s.ClientBySession("seat-a"); id == "c1" {
		t.Fatal("old client still owns the session")
	}
	// Removing the stale client must not drop the new owner.
	s.Remove("c1")
	if id, _ := s.ClientBySession("seat-a"); id != "c3" {
		t.Fatalf("seat-a after removing c1 -> %q, want c3", id)
	}
	s.Remove("c3")
	if _, ok := s.ClientBySession("seat-a"); ok {
		t.Fatal("removed client still found")
	}
	// A client that renames its session frees the old name.
	s.Handle("c2", &signal.Hello{Session: "seat-c"}, c.reply)
	if _, ok := s.ClientBySession("seat-b"); ok {
		t.Fatal("old session name kept")
	}
	if len(got) != 4 || got[2] != [2]string{"c3", "seat-a"} {
		t.Fatalf("OnHello calls %v", got)
	}
}
