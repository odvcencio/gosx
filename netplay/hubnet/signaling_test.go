package hubnet

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay/signal"
)

type got struct {
	client string
	msg    signal.Message
}

func startHub(t *testing.T) (*hub.Hub, *websocket.Conn, string) {
	t.Helper()
	h := hub.New("netplay-test")
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	// __welcome carries the server-side client ID.
	var env hub.Message
	if err := ws.ReadJSON(&env); err != nil || env.Event != "__welcome" {
		t.Fatalf("welcome: %v %v", env, err)
	}
	var w struct {
		ClientID string `json:"clientId"`
	}
	_ = json.Unmarshal(env.Data, &w)
	return h, ws, w.ClientID
}

func sendEvent(t *testing.T, ws *websocket.Conn, m signal.Message) {
	t.Helper()
	ev, data, err := signal.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(hub.Message{Event: ev, Data: data}); err != nil {
		t.Fatal(err)
	}
}

func TestSignalingDeliversValidMessages(t *testing.T) {
	h, ws, id := startHub(t)
	var mu sync.Mutex
	var in []got
	ch := make(chan struct{}, 8)
	AttachSignaling(h, func(client string, m signal.Message) {
		mu.Lock()
		in = append(in, got{client, m})
		mu.Unlock()
		ch <- struct{}{}
	})
	sendEvent(t, ws, &signal.Hello{Session: "s1"})
	sendEvent(t, ws, &signal.Offer{Session: "s1", SDP: "v=0\r\n"})
	sendEvent(t, ws, &signal.Candidate{Session: "s1", Candidate: "candidate:1 1 udp 1 1.2.3.4 5 typ host"})
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d messages", i)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if in[0].client != id || in[0].msg.Event() != signal.EventHello || in[1].msg.Event() != signal.EventOffer || in[2].msg.Event() != signal.EventCandidate {
		t.Fatalf("unexpected deliveries: %+v", in)
	}
}

func TestSignalingDropsInvalidMessages(t *testing.T) {
	h, ws, _ := startHub(t)
	ch := make(chan signal.Message, 8)
	AttachSignaling(h, func(_ string, m signal.Message) { ch <- m })
	bad := []string{
		`{"session":"","sdp":"v=0"}`,
		`{"session":"s","sdp":"not sdp"}`,
		`{"session":"s","sdp":"v=0\r\n` + strings.Repeat(`a=b\r\n`, signal.MaxSDPBytes/5+10) + `"}`,
	}
	for _, d := range bad {
		if err := ws.WriteJSON(hub.Message{Event: signal.EventOffer, Data: json.RawMessage(d)}); err != nil {
			t.Fatal(err)
		}
	}
	// A valid message afterwards proves the connection survived and the bad ones were skipped.
	sendEvent(t, ws, &signal.Hello{Session: "ok"})
	select {
	case m := <-ch:
		if m.Event() != signal.EventHello {
			t.Fatalf("first delivery was %s, want the valid hello", m.Event())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestSignalingSendReachesClient(t *testing.T) {
	h, ws, id := startHub(t)
	s := AttachSignaling(h, func(string, signal.Message) {})
	if err := s.Send(id, &signal.Answer{Session: "s", SDP: "v=0\r\n"}); err != nil {
		t.Fatal(err)
	}
	var env hub.Message
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ws.ReadJSON(&env); err != nil {
		t.Fatal(err)
	}
	m, err := signal.Parse(env.Event, env.Data)
	if err != nil || m.Event() != signal.EventAnswer {
		t.Fatalf("got %v %v", m, err)
	}
	if err := s.Send(id, &signal.Answer{Session: "s", SDP: "bogus"}); err == nil {
		t.Fatal("Send accepted an invalid message")
	}
}
