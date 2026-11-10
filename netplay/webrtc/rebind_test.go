package webrtc

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/client"
	"m31labs.dev/gosx/netplay/signal"
	"m31labs.dev/gosx/netplay/webrtc/internal/duel"
)

func TestRebindChecksSessionAndMovesTheLink(t *testing.T) {
	s, err := NewServer(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var c collector
	s.Handle("old", &signal.Hello{Session: "tok-a"}, c.reply)
	s.Handle("other", &signal.Hello{Session: "tok-b"}, c.reply)
	link := s.Link("old")
	// A client that announced another session cannot take the seat.
	if err := s.Rebind("old", "other"); !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("rebind to another session: %v", err)
	}
	if err := s.Rebind("old", "ghost"); !errors.Is(err, ErrUnknownClient) {
		t.Fatalf("rebind to a client that said no hello: %v", err)
	}
	s.Handle("new", &signal.Hello{Session: "tok-a"}, c.reply)
	if err := s.Rebind("old", "new"); err != nil {
		t.Fatal(err)
	}
	if got := s.Link("new"); got != link || link.ID() != "new" {
		t.Fatalf("link not moved: %p %p id %q", got, link, link.ID())
	}
	if id, _ := s.ClientBySession("tok-a"); id != "new" {
		t.Fatalf("session maps to %q", id)
	}
	if _, ok := s.SessionOf("old"); ok {
		t.Fatal("old client still has a session")
	}
}

func TestAnnounceCapNeverRefusesAReconnect(t *testing.T) {
	s, _ := NewServer(Config{})
	defer s.Close()
	var c collector
	for i := 0; i < maxAnnounced; i++ {
		s.Handle(fmt.Sprintf("c%d", i), &signal.Hello{Session: fmt.Sprintf("s%d", i)}, c.reply)
	}
	s.Handle("fresh", &signal.Hello{Session: "brand-new"}, c.reply)
	if _, ok := s.ClientBySession("brand-new"); ok {
		t.Fatal("a new session was accepted past the cap")
	}
	s.Handle("again", &signal.Hello{Session: "s17"}, c.reply) // a reconnect of seat s17
	if id, _ := s.ClientBySession("s17"); id != "again" {
		t.Fatalf("reconnect refused at the cap: s17 -> %q", id)
	}
}

// A match over the WebSocket fallback survives the hub dropping a player: the
// client says hello again, the server rebinds the seat, and play continues.
func TestMatchContinuesAfterHubReconnectOnWebSocketFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time match")
	}
	var mu sync.Mutex
	seat := map[string]string{} // session token -> current hub client ID
	var srv *Server
	rebinds := 0
	var err error
	srv, err = NewServer(Config{OnHello: func(id, session string) {
		mu.Lock()
		defer mu.Unlock()
		if old, ok := seat[session]; ok && old != id {
			if err := srv.Rebind(old, id); err != nil {
				t.Errorf("rebind %s -> %s: %v", old, id, err)
				return
			}
			rebinds++
		}
		seat[session] = id
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	h := hub.New("rebind")
	srv.AttachHub(h)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	tokens := [2]string{"seat-token-a", "seat-token-b"}
	var conns [2]*client.Conn
	for i := range conns {
		conns[i], err = client.Dial(client.Options{HubURL: url, Session: tokens[i], FallbackAfter: 100 * time.Millisecond, ConfigWait: 50 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		c := conns[i]
		t.Cleanup(func() { c.Close() })
	}
	deadline := time.Now().Add(5 * time.Second)
	var ids [2]string
	for time.Now().Before(deadline) {
		mu.Lock()
		ids = [2]string{seat[tokens[0]], seat[tokens[1]]}
		mu.Unlock()
		if ids[0] != "" && ids[1] != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if ids[0] == "" || ids[1] == "" {
		t.Fatal("clients never said hello")
	}
	links := [2]*Link{srv.Link(ids[0]), srv.Link(ids[1])}
	relay, err := netplay.NewRelay(netplay.RelayConfig[duel.Pad]{
		NewGame: func() netplay.Game[duel.Pad] { return duel.NewGame() }, Codec: duel.Codec{},
		Peers: [2]netplay.Transport{links[0], links[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sess [2]*netplay.Session[duel.Pad]
	for i := 0; i < 2; i++ {
		sess[i], err = netplay.NewSession(netplay.Config[duel.Pad]{
			Game: duel.NewGame(), Codec: duel.Codec{}, Transport: conns[i], LocalPlayer: i, InputDelay: 2, Relay: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	var next [2]time.Duration
	dropped := false
	var frameAtDrop int64
	for sess[0].Frame() < frameAtDrop+300 || !dropped {
		<-tick.C
		now := time.Since(start)
		if now > 40*time.Second {
			t.Fatalf("match stuck: frames %d / %d, errs %v / %v, rebinds %d", sess[0].Frame(), sess[1].Frame(), sess[0].Err(), sess[1].Err(), rebinds)
		}
		relay.Tick(now)
		for i := 0; i < 2; i++ {
			if now < next[i] {
				continue
			}
			next[i] += time.Second / 60
			sess[i].Advance(now, duel.Input(7, i, sess[i].Frame()))
			if err := sess[i].Err(); err != nil {
				t.Fatalf("session %d ended: %v (rebinds %d)", i, err, rebinds)
			}
		}
		if !dropped && sess[0].Frame() >= 120 && conns[0].Mode() == client.ModeWebSocket {
			dropped = true
			frameAtDrop = sess[0].Frame()
			if !h.Disconnect(ids[0], "test drop") {
				t.Fatal("hub did not know the client")
			}
		}
	}
	mu.Lock()
	n := rebinds
	mu.Unlock()
	if n != 1 {
		t.Fatalf("rebinds = %d, want 1", n)
	}
	if links[0].ID() == ids[0] {
		t.Fatal("link still serves the old client")
	}
	if rep := relay.Report(); len(rep.DesyncFrames) != 0 || rep.Ended {
		t.Fatalf("relay report %+v", rep)
	}
	t.Logf("frames %d / %d after the drop at %d; mode %v", sess[0].Frame(), sess[1].Frame(), frameAtDrop, links[0].Mode())
}
