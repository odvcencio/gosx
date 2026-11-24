package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The single-file exporter's payload must not change when the type table
// moves: the football game ships that file and a changed byte changes its
// hash. The digest was recorded from the exporter's own private table before
// it was removed.
func TestSingleFileOutputIsUnchanged(t *testing.T) {
	static := filepath.Join(t.TempDir(), "static")
	names := []string{
		"a.wasm", "a.js", "a.mjs", "a.css", "a.json", "a.webmanifest", "a.txt", "a.svg", "a.png",
		"a.jpg", "a.jpeg", "a.gif", "a.webp", "a.avif", "a.ico", "a.glb", "a.gltf", "a.bin",
		"a.woff", "a.woff2", "a.ttf", "a.otf", "a.mp3", "a.ogg", "a.wav", "a.mp4", "a.webm",
		"a.ktx2", "a.wgsl", "UPPER.GLB", "Mixed.Ktx2", "noext", "a.unknownext",
	}
	if err := os.MkdirAll(static, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("<html><head><title>T</title></head><body></body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(static, "assets-"+name), []byte("bytes of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "game.html")
	if _, err := writeSingleFile(static, out); err != nil {
		t.Fatal(err)
	}
	packed, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	_, payload := decodeSingleFile(t, packed)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	const want = "b14cc1ae00f63c547ffac6f8432f3aa133b708d55925cdfef1c2ea624ed04775"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("payload digest = %s, want %s", got, want)
	}
}
