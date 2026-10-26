package hub

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSendBinaryTargetsOneClient(t *testing.T) {
	h := New("sendbin")
	srv := httptest.NewServer(h)
	defer srv.Close()
	dial := func() *websocket.Conn {
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		return ws
	}
	a, b := dial(), dial()
	// Wait for both registrations.
	deadline := time.Now().Add(2 * time.Second)
	for h.ClientCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	var ids []string
	h.mu.RLock()
	for id := range h.clients {
		ids = append(ids, id)
	}
	h.mu.RUnlock()
	if len(ids) != 2 {
		t.Fatalf("%d clients", len(ids))
	}
	if h.SendBinary("nobody", []byte{0, 1}) {
		t.Fatal("SendBinary reported delivery to a missing client")
	}
	if h.SendBinary(ids[0], nil) {
		t.Fatal("SendBinary accepted an empty payload")
	}
	if !h.SendBinary(ids[0], []byte{0, 7, 7}) {
		t.Fatal("SendBinary refused a connected client")
	}
	// Exactly one of the two sockets sees the binary frame.
	seen := 0
	for _, ws := range []*websocket.Conn{a, b} {
		ws.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				break
			}
			if typ == websocket.BinaryMessage && len(data) == 3 && data[1] == 7 {
				seen++
			}
		}
	}
	if seen != 1 {
		t.Fatalf("%d clients received the frame, want 1", seen)
	}
	// Close politely and wait for the hub to drop both clients, so the read
	// pump's log line cannot land inside a later test's log capture.
	for _, ws := range []*websocket.Conn{a, b} {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		ws.Close()
	}
	for h.ClientCount() > 0 && time.Now().Before(deadline.Add(3*time.Second)) {
		time.Sleep(time.Millisecond)
	}
}
