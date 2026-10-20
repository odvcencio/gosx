//go:build !(js && wasm)

package hubclient

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/hub"
)

func waitForCompressionWelcome(t *testing.T, c *Client, welcome <-chan json.RawMessage) {
	t.Helper()
	select {
	case data := <-welcome:
		var payload struct {
			ClientID string `json:"clientId"`
		}
		if err := json.Unmarshal(data, &payload); err != nil || payload.ClientID == "" {
			t.Fatalf("invalid registration welcome: %s (decode error: %v)", data, err)
		}
		if c.State() != StateConnected {
			t.Fatalf("welcomed client state = %s, want connected", c.State())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for registration welcome")
	}
}

// The client receives an actual successful upgrade before the server's Write
// returns. This reproduces a legal scheduling gap without a sleep or a mocked
// transport: StateConnected is true while hub registration is still pending.
func TestClientCompressionWelcomeOrdersBroadcastAfterRegistration(t *testing.T) {
	h := hub.New("client-compression-registration")
	h.EnableCompression = true
	handshake := make(chan struct{})
	registration := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(registration) }) }
	server := httptest.NewUnstartedServer(h)
	server.Listener = &compressionHandshakeListener{Listener: server.Listener, handshake: handshake, registration: registration}
	server.Start()
	c := New(Options{URL: wsURL(server.URL), EnableCompression: true})
	t.Cleanup(func() {
		release()
		c.Close()
		server.Close()
	})
	welcome := make(chan json.RawMessage, 1)
	received := make(chan json.RawMessage, 2)
	c.On("__welcome", func(data json.RawMessage) { welcome <- data })
	c.On("snapshot", func(data json.RawMessage) { received <- data })
	c.Connect()
	select {
	case <-handshake:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for actual upgrade bytes")
	}
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	if count := h.ClientCount(); count != 0 {
		t.Fatalf("registration barrier has %d clients, want zero", count)
	}
	h.Broadcast("snapshot", strings.Repeat("before-registration", 100))
	select {
	case <-welcome:
		t.Fatal("welcome arrived before registration was allowed")
	default:
	}
	t.Log("actual WebSocket connected before server registration; early broadcast had zero recipients")
	release()
	waitForCompressionWelcome(t, c, welcome)
	if count := h.ClientCount(); count != 1 {
		t.Fatalf("welcomed hub has %d clients, want one", count)
	}
	payload := strings.Repeat("after-registration", 100)
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	h.Broadcast("snapshot", payload)
	select {
	case got := <-received:
		if !bytes.Equal(got, want) {
			t.Fatalf("first snapshot = %s, want exact post-registration payload %s", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for compressed snapshot after registration welcome")
	}
	select {
	case got := <-received:
		t.Fatalf("unexpected extra snapshot: %s", got)
	default:
	}
}

type compressionHandshakeListener struct {
	net.Listener
	handshake    chan struct{}
	registration <-chan struct{}
}

func (l *compressionHandshakeListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &compressionHandshakeConn{Conn: c, handshake: l.handshake, registration: l.registration}, nil
}

type compressionHandshakeConn struct {
	net.Conn
	handshake    chan struct{}
	registration <-chan struct{}
	once         sync.Once
}

func (c *compressionHandshakeConn) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	if err == nil && bytes.HasPrefix(data[:n], []byte("HTTP/1.1 101 ")) {
		c.once.Do(func() {
			close(c.handshake)
			<-c.registration
		})
	}
	return n, err
}
