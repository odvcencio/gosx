package hub_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/server"
)

var _ server.ShutdownSource = (*hub.Hub)(nil)

func TestShutdownExternalHostAndHijackedHub(t *testing.T) {
	a := server.New()
	h := hub.New("game")
	defer h.Close(context.Background())
	a.Mount("/ws", h)
	if _, err := a.UseShutdownSource("room", h); err != nil {
		t.Fatal(err)
	}
	external := httptest.NewServer(a.Build())
	defer external.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(external.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := external.Config.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if h.ClientCount() != 1 {
		t.Fatal("HTTP shutdown unexpectedly closed hijacked socket")
	}
	if err := a.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if h.ClientCount() != 0 {
		t.Fatal("explicit hub Drain did not finish")
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("hub socket remained open")
	}
}
