//go:build !js || !wasm

package rooms

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestOriginsAreTheSiteOriginPlusLoopbackOnlyOverHTTP(t *testing.T) {
	if got := Origins("https://gosx.m31labs.dev/"); len(got) != 1 || got[0] != "https://gosx.m31labs.dev" {
		t.Fatalf("https origins = %v, want only the site origin", got)
	}
	got := Origins("http://localhost:3000")
	want := []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dev origins = %v, want %v", got, want)
	}
	if got := Origins(""); got != nil {
		t.Fatalf("empty base origins = %v, want none", got)
	}
}

func TestConfigIsBoundedForAPublicSite(t *testing.T) {
	cfg := Config("https://gosx.m31labs.dev", false)
	if !cfg.RequireOrigin || len(cfg.AllowedOrigins) != 1 {
		t.Fatalf("origin policy = require %v, allowed %v", cfg.RequireOrigin, cfg.AllowedOrigins)
	}
	if cfg.MaxRooms != MaxRooms || cfg.MaxRooms > 32 || cfg.MaxSpectators != -1 {
		t.Fatalf("room caps = rooms %d, spectators %d", cfg.MaxRooms, cfg.MaxSpectators)
	}
	if cfg.ConnectsPerMinute != ConnectsPerMinute || cfg.CreatesPerMinute != CreatesPerMinute {
		t.Fatalf("rate limits = %d connects, %d creates", cfg.ConnectsPerMinute, cfg.CreatesPerMinute)
	}
	if cfg.IdleTimeout > 30*1e9 || cfg.ReconnectGrace > 30*1e9 {
		t.Fatalf("timers = idle %v, reconnect %v, want short", cfg.IdleTimeout, cfg.ReconnectGrace)
	}
	if cfg.TrustForwardedFor {
		t.Fatal("TrustForwardedFor must stay off unless the deployment opts in")
	}
	mode := cfg.Modes[0]
	if mode.Name != "round" || mode.Seats != 2 || mode.NewBot == nil {
		t.Fatalf("mode = %q with %d seats and bots %v; want a 2 seat round with bots", mode.Name, mode.Seats, mode.NewBot != nil)
	}
}

func dial(t *testing.T, srv *httptest.Server, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/demos/rooms/play/ws?mode=round"
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial(url, header)
}

func TestRejectedOriginIsRefusedBeforeASeatIsReserved(t *testing.T) {
	srv, err := NewServer("https://gosx.m31labs.dev")
	if err != nil {
		t.Fatal(err)
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/demos/rooms/play", srv).ServeHTTP(w, r)
	}))
	defer site.Close()
	defer func() { _ = srv.Close(t.Context()) }()

	for _, origin := range []string{"https://evil.example", "http://gosx.m31labs.dev", "null", ""} {
		conn, resp, err := dial(t, site, origin)
		if err == nil {
			conn.Close()
			t.Fatalf("origin %q connected, want it refused", origin)
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			code := 0
			if resp != nil {
				code = resp.StatusCode
			}
			t.Fatalf("origin %q status = %d, want 403", origin, code)
		}
	}
	if n := len(srv.Rooms(false)); n != 0 {
		t.Fatalf("%d rooms exist after refused connections, want none", n)
	}

	conn, _, err := dial(t, site, "https://gosx.m31labs.dev")
	if err != nil {
		t.Fatalf("allowed origin was refused: %v", err)
	}
	conn.Close()
}

func TestAssetHandlerServesPageAndCompressedWasmAndHidesMissingFilesInProduction(t *testing.T) {
	t.Setenv("GOSX_ENV", "production")
	root := t.TempDir()
	h := AssetHandler(root)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/rooms/client/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="stage"`) || !strings.Contains(rec.Body.String(), "./main.wasm") {
		t.Fatalf("client page = %d %q", rec.Code, rec.Body.String())
	}

	for _, name := range []string{"main.wasm", "wasm_exec.js"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/rooms/client/"+name, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("missing %s in production = %d, want 404", name, rec.Code)
		}
	}

	dir := filepath.Join(root, filepath.FromSlash(AssetDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	wasm := []byte("\x00asm\x01\x00\x00\x00 test")
	_ = os.WriteFile(filepath.Join(dir, "main.wasm"), wasm, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "wasm_exec.js"), []byte("// shim"), 0o644)
	gz, _ := os.Create(filepath.Join(dir, "main.wasm.gz"))
	zw := gzip.NewWriter(gz)
	_, _ = zw.Write(wasm)
	_ = zw.Close()
	_ = gz.Close()

	req := httptest.NewRequest(http.MethodGet, "/demos/rooms/client/main.wasm", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/wasm" || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("wasm = %d, type %q, encoding %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Encoding"))
	}
	body, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(body); string(got) != string(wasm) {
		t.Fatalf("decompressed wasm = %q", got)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/rooms/client/main.wasm", nil))
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.Len() != len(wasm) {
		t.Fatalf("plain wasm encoding %q, %d bytes", rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demos/rooms/client/../secret", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/demos/rooms/client/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
}

func TestClientSrcPointsAtTheSitesOwnRoomServer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://gosx.m31labs.dev/demos/rooms", nil)
	r.Host = "gosx.m31labs.dev"
	src := clientSrc(r)
	if !strings.HasPrefix(src, "/demos/rooms/client/?") || !strings.Contains(src, "mode=round") ||
		!strings.Contains(src, "wss%3A%2F%2Fgosx.m31labs.dev%2Fdemos%2Frooms%2Fplay%2Fws") {
		t.Fatalf("clientSrc = %q", src)
	}
}
