package gameboy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrameworkPlayerAssets(t *testing.T) {
	for name, contentType := range map[string]string{"runtime.js": "javascript", "player.css": "text/css", "vendor/boytacean-0.13.2/boytacean_bg.wasm": "application/wasm", "vendor/boytacean-0.13.2/LICENSE": "text/plain"} {
		b, err := assets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if !strings.HasSuffix(assetURL(name), hex.EncodeToString(sum[:6])) {
			t.Fatalf("asset %s has stale version", name)
		}
		w := httptest.NewRecorder()
		Assets().ServeHTTP(w, httptest.NewRequest(http.MethodGet, assetURL(name), nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), contentType) || w.Body.Len() != len(b) {
			t.Fatalf("asset %s returned %d %s", name, w.Code, w.Header().Get("Content-Type"))
		}
	}
	w := httptest.NewRecorder()
	Assets().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/gosx/gameboy/Pokemon-Yellow.gbc", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("game cartridge must not be a framework asset")
	}
}
