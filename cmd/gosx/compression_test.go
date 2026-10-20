package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/internal/bundlepolicy"
)

func TestCompressionBuildSidecars(t *testing.T) {
	root := t.TempDir()
	html := "<!DOCTYPE html><p>" + strings.Repeat("static export body ", 128) + "</p>"
	if err := writeExportPage(root, "/nested", html); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"styles.css":       bytes.Repeat([]byte(".page { color: black; }\n"), 128),
		"script.js":        bytes.Repeat([]byte("console.log('ready');\n"), 128),
		"data.json":        []byte(`{"value":"` + strings.Repeat("text ", 256) + `"}`),
		"site.webmanifest": []byte(`{"name":"` + strings.Repeat("app ", 256) + `"}`),
		"extensionless":    bytes.Repeat([]byte("plain public text\n"), 128),
		"small.txt":        []byte("small"),
		"image.png":        bytes.Repeat([]byte("binary image"), 128),
		"font.woff2":       bytes.Repeat([]byte("binary font"), 128),
		"video.mp4":        bytes.Repeat([]byte("binary video"), 128),
		"audio.mp3":        bytes.Repeat([]byte("binary audio"), 128),
		"app.wasm":         bytes.Repeat([]byte("binary wasm"), 128),
		"data.bin":         bytes.Repeat([]byte{0, 1, 2, 3}, 512),
		"archive.zip":      bytes.Repeat([]byte("binary zip"), 128),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), body, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeTextSidecars(root, bundlepolicy.Config{}); err != nil {
		t.Fatal(err)
	}
	files["nested/index.html"] = []byte(html)
	for name, body := range files {
		want := name == "nested/index.html" || name == "styles.css" || name == "script.js" || name == "data.json" || name == "site.webmanifest" || name == "extensionless"
		for _, ext := range []string{".br", ".gz"} {
			compressed, err := os.ReadFile(filepath.Join(root, name) + ext)
			if !want {
				if !os.IsNotExist(err) {
					t.Errorf("unexpected sidecar for %s%s: %v", name, ext, err)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			var reader io.Reader = brotli.NewReader(bytes.NewReader(compressed))
			if ext == ".gz" {
				gz, err := gzip.NewReader(bytes.NewReader(compressed))
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				reader = gz
			}
			decoded, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(decoded, body) {
				t.Fatalf("sidecar %s%s does not encode the original: %v", name, ext, err)
			}
			if len(compressed) >= len(body) {
				t.Fatal("sidecar is larger than its source")
			}
		}
	}
	if err := writeTextSidecars(root, bundlepolicy.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "styles.css.br.br")); !os.IsNotExist(err) {
		t.Fatal("sidecars must not be compressed again")
	}
}

func TestCompressionBuildSidecarExclusions(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "styles.css")
	if err := os.WriteFile(target, bytes.Repeat([]byte(".page {color:black;} "), 128), 0644); err != nil {
		t.Fatal(err)
	}
	policy := bundlepolicy.Config{Exclude: []string{"public/styles.css.br"}}
	if err := writeTextSidecars(root, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + ".br"); !os.IsNotExist(err) {
		t.Fatal("an excluded variant was generated")
	}
	if _, err := os.Stat(target + ".gz"); err != nil {
		t.Fatal("allowed gzip variant is missing")
	}
}
