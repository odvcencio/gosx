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
	// The old client is still on the hub: its seat cannot move.
	if err := s.Rebind("old", "new"); !errors.Is(err, ErrClientLive) {
		t.Fatalf("rebind from a live client: %v", err)
	}
	if got := s.Leave("old"); got != "new" {
		t.Fatalf("Leave returned %q, want the newer client", got)
	}
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
	rebinds := 0
	var srv *Server
	var err error
	// The documented pattern: OnHello tries to move the seat; a hub leave
	// handler calls Leave and moves it when the new client got there first.
	tryRebind := func(old, id string) {
		switch err := srv.Rebind(old, id); {
		case err == nil:
			mu.Lock()
			rebinds++
			mu.Unlock()
		case errors.Is(err, ErrClientLive):
			// the leave handler will finish it
		default:
			t.Errorf("rebind %s -> %s: %v", old, id, err)
		}
	}
	srv, err = NewServer(Config{OnHello: func(id, session, prev string) {
		if prev != "" {
			tryRebind(prev, id)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	h := hub.New("rebind")
	srv.AttachHub(h)
	h.On("leave", func(ctx *hub.Context) {
		if newID := srv.Leave(ctx.Client.ID); newID != "" {
			tryRebind(ctx.Client.ID, newID)
		}
	})
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
		ids[0], _ = srv.ClientBySession(tokens[0])
		ids[1], _ = srv.ClientBySession(tokens[1])
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
				t.Fatalf("session %d ended: %v", i, err)
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

func TestRebindRefusesDefaultLabelAndTakenSeat(t *testing.T) {
	s, _ := NewServer(Config{})
	defer s.Close()
	var c collector
	// Two players of different matches both use the default label.
	s.Handle("m1p1", &signal.Hello{Session: DefaultSession}, c.reply)
	s.Link("m1p1")
	s.Handle("m2p1", &signal.Hello{Session: DefaultSession}, c.reply)
	s.Leave("m1p1")
	if err := s.Rebind("m1p1", "m2p1"); !errors.Is(err, ErrDefaultSession) {
		t.Fatalf("default label: %v", err)
	}
	if s.Link("m1p1").ID() != "m1p1" {
		t.Fatal("seat moved")
	}
	// A Link for the new client already exists: it is not orphaned.
	s.Handle("a", &signal.Hello{Session: "tok-z"}, c.reply)
	la := s.Link("a")
	lb := s.Link("b")
	s.Handle("b", &signal.Hello{Session: "tok-z"}, c.reply)
	s.Leave("a")
	if err := s.Rebind("a", "b"); !errors.Is(err, ErrSeatTaken) {
		t.Fatalf("taken seat: %v", err)
	}
	if s.Link("b") != lb || la.ID() != "a" {
		t.Fatal("links changed on a refused rebind")
	}
}

// A second, live client that names a seat's token cannot take it.
func TestLiveSeatCannotBeTaken(t *testing.T) {
	var mu sync.Mutex
	var errs []error
	var srv *Server
	srv, _ = NewServer(Config{OnHello: func(id, session, prev string) {
		mu.Lock()
		defer mu.Unlock()
		if prev != "" {
			errs = append(errs, srv.Rebind(prev, id))
		}
	}})
	defer srv.Close()
	var c collector
	srv.Handle("victim", &signal.Hello{Session: "tok-a"}, c.reply)
	link := srv.Link("victim")
	srv.Handle("intruder", &signal.Hello{Session: "tok-a"}, c.reply)
	if link.ID() != "victim" {
		t.Fatalf("seat link now serves %q", link.ID())
	}
	if len(errs) != 1 || errs[0] == nil {
		t.Fatalf("rebind errors %v", errs)
	}
}
