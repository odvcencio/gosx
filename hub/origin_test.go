package hub

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestOriginAllowlistMatches(t *testing.T) {
	a, err := NewOriginAllowlist(
		"https://play.example.com",
		"http://localhost:8080",
		"https://*.portal.test",
		"HTTPS://Upper.Example.org:443",
		"http://[::1]:9000",
	)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		origin string
		want   bool
	}{
		{"https://play.example.com", true},
		{"https://play.example.com:443", true},
		{"https://PLAY.example.com", true},
		{"http://play.example.com", false}, // scheme differs
		{"https://play.example.com:8443", false},
		{"https://evil-play.example.com", false},
		{"https://play.example.com.evil.test", false},
		{"http://localhost:8080", true},
		{"http://localhost", false},
		{"https://game.portal.test", true},
		{"https://a.b.portal.test", true},
		{"https://portal.test", false}, // the apex is not a subdomain
		{"https://xportal.test", false},
		{"http://game.portal.test", false},
		{"https://upper.example.org", true},
		{"http://[::1]:9000", true},
		{"null", false},
		{"", false},
		{"https://play.example.com/path", false},
		{"ftp://play.example.com", false},
		{"https://*.portal.test", false}, // a literal wildcard is never an origin
	}
	for _, c := range cases {
		if got := a.Allowed(c.origin); got != c.want {
			t.Errorf("Allowed(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
	withNull := MustOriginAllowlist("null")
	if !withNull.Allowed("null") {
		t.Fatal("explicit null entry should allow sandboxed iframes")
	}
}

func TestOriginAllowlistRejectsBadEntries(t *testing.T) {
	for _, entry := range []string{
		"", "example.com", "ftp://example.com", "https://example.com/game",
		"https://user@example.com", "https://ex*ample.com", "https://*.*.example.com",
		"https://a.*.example.com", "*", "https://*.",
	} {
		if _, err := NewOriginAllowlist(entry); !errors.Is(err, ErrInvalidOrigin) {
			t.Errorf("NewOriginAllowlist(%q) error = %v, want ErrInvalidOrigin", entry, err)
		}
	}
}

func TestOriginAllowlistCheckRequest(t *testing.T) {
	a := MustOriginAllowlist("https://play.example.com")
	req := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://api.example.com/ws", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !a.Check(req("")) {
		t.Fatal("missing Origin should pass Check; RequireOrigin refuses it separately")
	}
	if !a.Check(req("http://api.example.com")) {
		t.Fatal("same-origin request should pass")
	}
	if !a.Check(req("https://play.example.com")) {
		t.Fatal("listed origin should pass")
	}
	if a.Check(req("https://evil.test")) {
		t.Fatal("unlisted origin should fail")
	}
}

func TestHubCheckOriginAdmitsListedCrossOrigin(t *testing.T) {
	h := New("cross-origin")
	h.CheckOrigin = MustOriginAllowlist("https://play.example.com").Check
	h.RequireOrigin = true
	server := httptest.NewServer(h)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	if conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://evil.test"}}); err == nil {
		conn.Close()
		t.Fatal("unlisted origin connected")
	}
	if conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil {
		conn.Close()
		t.Fatal("RequireOrigin should still refuse a missing Origin")
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Origin": []string{"https://play.example.com"}})
	if err != nil {
		t.Fatalf("listed origin: %v", err)
	}
	closeAndWait(t, h, conn)
}

func TestHubDisconnectWithCodeSendsCode(t *testing.T) {
	h := New("close-code")
	joined := make(chan string, 1)
	h.On("join", func(ctx *Context) { joined <- ctx.Client.ID })
	server := httptest.NewServer(h)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	id := <-joined
	long := strings.Repeat("é", 100) // 200 bytes; must be cut at a rune boundary
	if !h.DisconnectWithCode(id, 4321, long) {
		t.Fatal("DisconnectWithCode refused a connected client")
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		var ce *websocket.CloseError
		if !errors.As(err, &ce) {
			t.Fatalf("read error %v, want a close frame", err)
		}
		if ce.Code != 4321 {
			t.Fatalf("close code %d, want 4321", ce.Code)
		}
		if len(ce.Text) > maxCloseReason || !strings.HasPrefix(long, ce.Text) || len(ce.Text)%2 != 0 {
			t.Fatalf("reason %q (%d bytes) not cut at a rune boundary within %d bytes", ce.Text, len(ce.Text), maxCloseReason)
		}
		break
	}
	if h.DisconnectWithCode(id, 4000, "again") {
		// The read pump may not have removed the client yet; a second call on
		// a closed client must not report success once it has.
		deadline := time.Now().Add(2 * time.Second)
		for h.ClientCount() > 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if h.DisconnectWithCode(id, 4000, "again") {
			t.Fatal("DisconnectWithCode succeeded for a removed client")
		}
	}
}

func TestNormalizeCloseCode(t *testing.T) {
	for code, want := range map[int]int{
		1000: 1000, 1001: 1001, 1008: 1008, 1011: 1011, 3000: 3000, 4999: 4999,
		0: 1008, 1005: 1008, 1006: 1008, 2999: 1008, 5000: 1008, -1: 1008,
	} {
		if got := normalizeCloseCode(code); got != want {
			t.Errorf("normalizeCloseCode(%d) = %d, want %d", code, got, want)
		}
	}
}

func closeAndWait(t *testing.T, h *Hub, conn *websocket.Conn) {
	t.Helper()
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for h.ClientCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}
