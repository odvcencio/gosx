package hubnet

import (
	"bytes"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/netplay"
)

func pollRecv(t *testing.T, p *FallbackPeer) []byte {
	t.Helper()
	buf := make([]byte, netplay.MaxPacketSize)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n, ok := p.Recv(buf); ok {
			return append([]byte(nil), buf[:n]...)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no datagram")
	return nil
}

func TestFallbackRoundTrip(t *testing.T) {
	h, ws, id := startHub(t)
	fb := AttachFallback(h, nil)
	p := fb.Peer(id)
	if p.Active() {
		t.Fatal("peer active before any frame")
	}
	if err := ws.WriteMessage(websocket.BinaryMessage, []byte{0, 'a', 'b'}); err != nil {
		t.Fatal(err)
	}
	if got := pollRecv(t, p); !bytes.Equal(got, []byte("ab")) {
		t.Fatalf("got %q", got)
	}
	if !p.Active() {
		t.Fatal("peer not active after a frame")
	}
	if err := p.Send([]byte("xyz")); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	typ, data, err := ws.ReadMessage()
	if err != nil || typ != websocket.BinaryMessage || !bytes.Equal(data, []byte{0, 'x', 'y', 'z'}) {
		t.Fatalf("client got %d %v %v", typ, data, err)
	}
	if err := p.Send(make([]byte, netplay.MaxPacketSize+1)); err != netplay.ErrPacketTooLarge {
		t.Fatalf("oversize send: %v", err)
	}
}

func TestFallbackFramesBeforePeerExistsAreKept(t *testing.T) {
	h, ws, id := startHub(t)
	fb := AttachFallback(h, nil)
	_ = ws.WriteMessage(websocket.BinaryMessage, []byte{0, 1})
	// Wait until the hub has handled the frame by checking the lazily made peer.
	deadline := time.Now().Add(2 * time.Second)
	for !fb.Peer(id).Active() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := pollRecv(t, fb.Peer(id)); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("got %v", got)
	}
}

func TestFallbackPassesOtherFramesToNext(t *testing.T) {
	h, ws, id := startHub(t)
	next := make(chan []byte, 2)
	fb := AttachFallback(h, func(_ *hub.Client, data []byte) bool {
		next <- append([]byte(nil), data...)
		return true
	})
	_ = ws.WriteMessage(websocket.BinaryMessage, []byte{7, 7, 7})
	select {
	case d := <-next:
		if !bytes.Equal(d, []byte{7, 7, 7}) {
			t.Fatalf("next got %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next handler never ran")
	}
	if fb.Peer(id).Active() {
		t.Fatal("a non-netplay frame activated the peer")
	}
}

func TestFallbackDropsOversizeAndForgets(t *testing.T) {
	h, ws, id := startHub(t)
	fb := AttachFallback(h, nil)
	big := make([]byte, 1+netplay.MaxPacketSize+1)
	_ = ws.WriteMessage(websocket.BinaryMessage, big)
	_ = ws.WriteMessage(websocket.BinaryMessage, []byte{0, 42})
	if got := pollRecv(t, fb.Peer(id)); !bytes.Equal(got, []byte{42}) {
		t.Fatalf("got %v, the oversize frame must be dropped", got)
	}
	old := fb.Peer(id)
	fb.Forget(id)
	if fb.Peer(id) == old {
		t.Fatal("Forget kept the peer")
	}
}

func TestFallbackBoundsPeerCount(t *testing.T) {
	h, _, _ := startHub(t)
	fb := AttachFallback(h, nil)
	for i := 0; i < MaxFallbackPeers+10; i++ {
		fb.Peer(string(rune('A'+i%26)) + string(rune('a'+i/26)) + "-id")
	}
	fb.mu.Lock()
	n := len(fb.peers)
	fb.mu.Unlock()
	if n > MaxFallbackPeers {
		t.Fatalf("%d peers retained, limit %d", n, MaxFallbackPeers)
	}
}
