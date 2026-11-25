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
	names := []string{
		"a.wasm", "a.js", "a.mjs", "a.css", "a.json", "a.webmanifest", "a.txt", "a.svg", "a.png",
		"a.jpg", "a.jpeg", "a.gif", "a.webp", "a.avif", "a.ico", "a.glb", "a.gltf", "a.bin",
		"a.woff", "a.woff2", "a.ttf", "a.otf", "a.mp3", "a.ogg", "a.wav", "a.mp4", "a.webm",
		"a.ktx2", "a.wgsl", "UPPER.GLB", "Mixed.Ktx2", "noext", "a.unknownext",
	}
	const want = "b14cc1ae00f63c547ffac6f8432f3aa133b708d55925cdfef1c2ea624ed04775"
	if got := singleFileDigest(t, names); got != want {
		t.Errorf("payload digest = %s, want %s", got, want)
	}
}

// The shared type table gives .htm, .xml and .pdf their own types. The
// exporter's old table lacked them, so they packed as application/octet-stream
// and the payload of a game that ships one of them changes. The digest below
// was recorded with the shared table, for these three files only.
func TestSingleFileOutputForTypesTheOldTableLacked(t *testing.T) {
	const want = "fa445affc77ffa9567691429db54992abe0d78e214f547938bdde2001c3797e6"
	if got := singleFileDigest(t, []string{"a.htm", "a.xml", "a.pdf"}); got != want {
		t.Errorf("payload digest = %s, want %s", got, want)
	}
}

func singleFileDigest(t *testing.T, names []string) string {
	t.Helper()
	static := filepath.Join(t.TempDir(), "static")
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
	return hex.EncodeToString(sum[:])
}
