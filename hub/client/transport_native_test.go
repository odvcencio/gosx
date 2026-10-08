//go:build !(js && wasm)

package hubclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/hub"
)

func TestCloseCancelsNativeHandshake(t *testing.T) {
	for _, phase := range []string{"upgrade", "proxy"} {
		t.Run(phase, func(t *testing.T) {
			requestSeen, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "proxy" && r.Method != http.MethodConnect {
					t.Errorf("proxy method=%s, want CONNECT", r.Method)
				}
				close(requestSeen)
				<-release // Accept the request but never complete the handshake.
				// On assertion failure, stop the dial before restoring the default
				// dialer; an implicit 200 response could otherwise advance CONNECT.
				if cn, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = cn.Close()
				}
			}))
			defer server.Close()
			defer close(release)
			endpoint := wsURL(server.URL)
			if phase == "proxy" {
				proxyURL, err := url.Parse(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				previous := websocket.DefaultDialer
				dialer := *previous
				dialer.Proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
				websocket.DefaultDialer = &dialer
				t.Cleanup(func() { websocket.DefaultDialer = previous })
				endpoint = "ws://example.test/stalled"
			}
			c := New(Options{URL: endpoint})
			c.Connect()
			awaitLifecycle(t, requestSeen, "native handshake request")
			closeClient(t, c)
			if c.State() != StateClosed {
				t.Fatal("cancelled dial did not reach StateClosed")
			}
		})
	}
}

func TestNativeDroppedSocketIsClosedBeforeRetry(t *testing.T) {
	serverReady, drop := make(chan struct{}), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.Close()
		close(serverReady)
		<-drop
	}))
	defer server.Close()
	defer close(drop)
	retrying := make(chan struct{})
	c := New(Options{
		URL:     wsURL(server.URL),
		Backoff: Backoff{Base: time.Hour, Max: time.Hour},
		OnStateChange: func(s State) {
			if s == StateReconnecting {
				close(retrying)
			}
		},
	})
	counted := make(chan *closeCountingConn, 1)
	c.dial = &nativeRecordingDialer{counted: counted}
	c.Connect()
	awaitLifecycle(t, serverReady, "native connection")
	var cn *closeCountingConn
	select {
	case cn = <-counted:
	case <-time.After(2 * time.Second):
		t.Fatal("native dial did not return")
	}
	// Let the server drop the socket, then verify disposal before retrying.
	drop <- struct{}{}
	awaitLifecycle(t, retrying, "reconnect after server drop")
	if got := cn.closes.Load(); got != 1 {
		t.Fatalf("dropped transport Close calls=%d, want 1", got)
	}
	closeClient(t, c)
	if got := cn.closes.Load(); got != 1 {
		t.Fatalf("retired transport Close calls=%d, want 1", got)
	}
}

func TestCloseSendsNormalWebSocketFrame(t *testing.T) {
	h := hub.New("normal-close")
	defer h.Close(context.Background())
	o := &disconnectObserver{events: make(chan hub.DisconnectEvent, 1)}
	if _, err := h.UseObserver(o); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	c := New(Options{URL: wsURL(server.URL)})
	c.Connect()
	waitFor(t, 2*time.Second, func() bool { return c.State() == StateConnected })
	closeClient(t, c)
	select {
	case event := <-o.events:
		if event.Reason != "peer_closed" {
			t.Fatalf("disconnect reason=%q, want peer_closed", event.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hub did not observe client closure")
	}
}

type disconnectObserver struct {
	hub.NoopObserver
	events chan hub.DisconnectEvent
}

func (o *disconnectObserver) ClientDisconnected(_ *hub.Hub, _ *hub.Client, event hub.DisconnectEvent) {
	o.events <- event
}

type closeCountingConn struct {
	conn
	closes atomic.Int32
}

func (c *closeCountingConn) Close() error {
	c.closes.Add(1)
	return c.conn.Close()
}

type nativeRecordingDialer struct {
	counted chan *closeCountingConn
	dialed  atomic.Bool
}

func (d *nativeRecordingDialer) Dial(ctx context.Context, rawURL string, header http.Header) (conn, error) {
	if !d.dialed.CompareAndSwap(false, true) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	cn, err := (nativeDialer{}).Dial(ctx, rawURL, header)
	if err != nil {
		return nil, err
	}
	counted := &closeCountingConn{conn: cn}
	d.counted <- counted
	return counted, nil
}
