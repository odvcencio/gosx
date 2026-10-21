package webrtc

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
	"m31labs.dev/gosx/netplay/client"
)

func recvWait(t *testing.T, name string, recv func([]byte) (int, bool)) string {
	t.Helper()
	buf := make([]byte, netplay.MaxPacketSize)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n, ok := recv(buf); ok {
			return string(buf[:n])
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s: nothing received", name)
	return ""
}

func TestLinkFollowsClientBetweenChannelAndWebSocket(t *testing.T) {
	r := connectPlayers(t, "link")
	link := r.server.Link(r.relay[0].ID())
	cl := r.client[0]

	// The data channel carries traffic while the client uses it.
	if link.Mode() != client.ModeWebRTC {
		t.Fatalf("mode %v with an open channel", link.Mode())
	}
	if err := link.Send([]byte("on rtc")); err != nil {
		t.Fatal(err)
	}
	if got := recvWait(t, "client dc", cl.Recv); got != "on rtc" {
		t.Fatalf("got %q", got)
	}

	// The client falls back: it sends on the hub WebSocket. The relay then
	// answers on the WebSocket, even though pion still reports the channel open.
	if err := cl.sig.sendBinary([]byte("ws up")); err != nil {
		t.Fatal(err)
	}
	if got := recvWait(t, "relay ws", link.Recv); got != "ws up" {
		t.Fatalf("got %q", got)
	}
	if link.Mode() != client.ModeWebSocket || !link.Degraded() {
		t.Fatalf("mode %v after a WebSocket datagram", link.Mode())
	}
	if err := link.Send([]byte("on ws")); err != nil {
		t.Fatal(err)
	}
	if got := recvWait(t, "client ws", cl.sig.bin.Pop); got != "on ws" {
		t.Fatalf("got %q", got)
	}
	if _, ok := cl.Recv(make([]byte, 16)); ok {
		t.Fatal("the datagram also went over the channel")
	}

	// Traffic on the channel moves the relay back.
	if err := cl.Send([]byte("rtc again")); err != nil {
		t.Fatal(err)
	}
	if got := recvWait(t, "relay dc", link.Recv); got != "rtc again" {
		t.Fatalf("got %q", got)
	}
	if link.Mode() != client.ModeWebRTC {
		t.Fatalf("mode %v after channel traffic", link.Mode())
	}
	if err := link.Send([]byte("back")); err != nil {
		t.Fatal(err)
	}
	if got := recvWait(t, "client dc", cl.Recv); got != "back" {
		t.Fatalf("got %q", got)
	}
}

func TestLinkBeforeAnyPathDropsAndRejectsOversize(t *testing.T) {
	s, _ := NewServer(Config{})
	defer s.Close()
	l := s.Link("nobody")
	if l.Mode() != client.ModeConnecting {
		t.Fatalf("mode %v", l.Mode())
	}
	if err := l.Send([]byte("x")); err != nil {
		t.Fatalf("send before connect: %v", err)
	}
	if err := l.Send(make([]byte, netplay.MaxPacketSize+1)); err != netplay.ErrPacketTooLarge {
		t.Fatalf("oversize: %v", err)
	}
	if _, ok := l.Recv(make([]byte, 8)); ok {
		t.Fatal("recv on a fresh link returned data")
	}
	if s.Link("nobody") != l {
		t.Fatal("Link is not stable per client")
	}
	s.Remove("nobody")
	if s.Link("nobody") == l {
		t.Fatal("Remove kept the link")
	}
}

func TestBinaryFramesThatAreNotNetplayReachNextHandler(t *testing.T) {
	got := make(chan []byte, 1)
	s, _ := NewServer(Config{NextBinary: func(_ *hub.Client, d []byte) bool { got <- append([]byte(nil), d...); return true }})
	defer s.Close()
	h := hub.New("netplay-next")
	s.AttachHub(h)
	ts := httptest.NewServer(h)
	defer ts.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.WriteMessage(websocket.BinaryMessage, []byte{9, 1, 2})
	select {
	case d := <-got:
		if len(d) != 3 || d[0] != 9 {
			t.Fatalf("got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next handler never ran")
	}
}

// TestMatchOverWebSocketFallbackHasZeroDesyncs runs a whole match with both
// players on the degraded path: client.Dial with no browser binding falls
// back to hub binary frames, and the relay's Links answer on them.
func TestMatchOverWebSocketFallbackHasZeroDesyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time match")
	}
	srv, err := NewServer(Config{ConfigureICE: loopbackICE})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := hub.New("netplay-fallback")
	srv.AttachHub(h)
	var mu sync.Mutex
	var ids []string
	h.On("join", func(ctx *hub.Context) { mu.Lock(); ids = append(ids, ctx.Client.ID); mu.Unlock() })
	ts := httptest.NewServer(h)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	var conns [2]*client.Conn
	for i := 0; i < 2; i++ {
		c, err := client.Dial(client.Options{HubURL: url, Session: "fb" + string(rune('a'+i)), FallbackAfter: 150 * time.Millisecond, ConfigWait: 50 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns[i] = c
		deadline := time.Now().Add(3 * time.Second)
		for {
			mu.Lock()
			n := len(ids)
			mu.Unlock()
			if n == i+1 || time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	mu.Lock()
	if len(ids) != 2 {
		mu.Unlock()
		t.Fatalf("%d clients joined", len(ids))
	}
	links := [2]netplay.Transport{srv.Link(ids[0]), srv.Link(ids[1])}
	mu.Unlock()
	for i, c := range conns {
		deadline := time.Now().Add(3 * time.Second)
		for c.Mode() != client.ModeWebSocket && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if !c.Degraded() {
			t.Fatalf("client %d mode %v, want the WebSocket fallback", i, c.Mode())
		}
	}
	runMatchOn(t, "fallback", 240, [2]netplay.Transport{conns[0], conns[1]}, links)
}
