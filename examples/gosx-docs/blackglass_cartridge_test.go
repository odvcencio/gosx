package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

func testBlackglassROM() []byte {
	rom := make([]byte, 32768)
	copy(rom[0x134:], "TEST CARTRIDGE")
	var checksum byte
	for _, value := range rom[0x134:0x14d] {
		checksum -= value + 1
	}
	rom[0x14d] = checksum
	return rom
}

func TestBlackglassDeploymentCartridgeIsOptionalAndBoundToItsDigest(t *testing.T) {
	t.Setenv("GOSX_BLACKGLASS_ROM_FILE", "")
	root := t.TempDir()
	app := server.New()
	if err := mountBlackglassCartridge(app, root); err != nil {
		t.Fatal(err)
	}
	request := func(handler http.Handler, method, path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	plain := app.Build()
	manifest := request(plain, http.MethodGet, "/api/blackglass-cartridge")
	if manifest.Code != 200 || !strings.Contains(manifest.Body.String(), `"available":false`) {
		t.Fatalf("unconfigured manifest: %d %s", manifest.Code, manifest.Body.String())
	}
	if w := request(plain, http.MethodGet, "/api/blackglass-cartridge/rom"); w.Code != 404 {
		t.Fatalf("unconfigured ROM route: %d", w.Code)
	}
	rom := testBlackglassROM()
	path := filepath.Join(root, "runtime-assets", "blackglass-cartridge.gbc")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rom, 0600); err != nil {
		t.Fatal(err)
	}
	app = server.New()
	if err := mountBlackglassCartridge(app, root); err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	manifest = request(handler, http.MethodGet, "/api/blackglass-cartridge")
	var data struct {
		Available bool
		SHA256    string
		Bytes     int
		URL       string
	}
	if err := json.Unmarshal(manifest.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(rom)
	if !data.Available || data.SHA256 != hex.EncodeToString(digest[:]) || data.Bytes != len(rom) || strings.Contains(manifest.Body.String(), root) {
		t.Fatalf("invalid deployment manifest: %s", manifest.Body.String())
	}
	if !strings.Contains(manifest.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("cartridge manifest must be refreshed across deployments")
	}
	served := request(handler, http.MethodGet, data.URL)
	if served.Code != 200 || !bytes.Equal(served.Body.Bytes(), rom) || served.Header().Get("ETag") != `"`+data.SHA256+`"` {
		t.Fatalf("ROM response: %d %s", served.Code, served.Header())
	}
	if w := request(handler, http.MethodHead, data.URL); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD response: %d, %d bytes", w.Code, w.Body.Len())
	}
	if w := request(handler, http.MethodGet, "/api/blackglass-cartridge/rom?sha256=stale"); w.Code != 410 {
		t.Fatalf("stale cartridge hash accepted: %d", w.Code)
	}
	if w := request(handler, http.MethodPost, data.URL); w.Code != 405 {
		t.Fatalf("cartridge mutation allowed: %d", w.Code)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if w := request(handler, http.MethodGet, data.URL); !bytes.Equal(w.Body.Bytes(), rom) {
		t.Fatal("startup cartridge snapshot changed after source removal")
	}
}

func TestBlackglassDeploymentCartridgeRejectsInvalidInputs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "invalid.gbc")
	t.Setenv("GOSX_BLACKGLASS_ROM_FILE", path)
	if err := mountBlackglassCartridge(server.New(), root); err == nil {
		t.Fatal("explicit missing cartridge silently accepted")
	}
	for _, rom := range [][]byte{make([]byte, 12), make([]byte, 32768), make([]byte, blackglassROMMaxBytes+1)} {
		if err := os.WriteFile(path, rom, 0600); err != nil {
			t.Fatal(err)
		}
		if err := mountBlackglassCartridge(server.New(), root); err == nil {
			t.Fatalf("invalid cartridge accepted: %d bytes", len(rom))
		}
	}
}
