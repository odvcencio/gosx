package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
)

func TestGoWASMURLRelocatedBundleAndCompressedServing(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "original")
	body := append([]byte("\x00asm\x01\x00\x00\x00"), bytes.Repeat([]byte("module bytes"), 100)...)
	hash := buildmanifest.ContentHash(body)
	asset := buildmanifest.HashedAsset{File: "controls." + hash + ".wasm", Hash: hash, Size: int64(len(body))}
	assetPath := filepath.Join(root, "assets", "go-wasm", asset.File)
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, body, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeTestGzip(assetPath+".gz", body); err != nil {
		t.Fatal(err)
	}
	if err := writeTestBrotli(assetPath+".br", body); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(buildmanifest.Manifest{GoWASM: map[string]buildmanifest.HashedAsset{"controls": asset}})
	if err := os.WriteFile(filepath.Join(root, "build.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(parent, "relocated")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.SetRuntimeRoot(moved)
	url := app.GoWASMURL("controls")
	if url != "/gosx/assets/go-wasm/"+asset.File || app.GoWASMURL("missing") != "" {
		t.Fatalf("relocated asset URL %q", url)
	}
	handler := app.Build()
	for _, encoding := range []string{"identity", "br", "gzip"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Accept-Encoding", encoding)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/wasm" || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("%s response: %d %v", encoding, w.Code, w.Header())
		}
		var reader io.Reader = w.Body
		if encoding == "br" {
			reader = brotli.NewReader(reader)
		} else if encoding == "gzip" {
			gz, err := gzip.NewReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			reader = gz
		}
		got, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("%s decoded asset: %v", encoding, err)
		}
	}
	app.SetRuntimeRoot(t.TempDir())
	if app.GoWASMURL("controls") != "" {
		t.Fatal("missing manifest retained a previous bundle URL")
	}
}
