package buildmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.json")
	data := []byte(`{
  "runtime": {
    "wasm": {"file": "gosx-runtime.11111111.wasm", "hash": "11111111", "size": 10},
    "wasmExec": {"file": "wasm_exec.22222222.js", "hash": "22222222", "size": 20},
    "bootstrap": {"file": "bootstrap.33333333.js", "hash": "33333333", "size": 30},
    "patch": {"file": "patch.44444444.js", "hash": "44444444", "size": 40},
    "videoHLS": {"file": "hls.min.77777777.js", "hash": "77777777", "size": 70}
  },
  "islands": [
    {"name": "Counter", "format": "bin", "file": "Counter.55555555.gxi", "hash": "55555555", "size": 50}
  ],
  "css": [
    {"component": "Counter", "source": "app/counter.css", "file": "app_counter.66666666.css", "hash": "66666666", "size": 60}
  ]
}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	runtime := manifest.RuntimeURLs("/gosx/assets")
	if runtime.WASM != "/gosx/assets/runtime/gosx-runtime.11111111.wasm" {
		t.Fatalf("unexpected wasm url: %s", runtime.WASM)
	}
	if runtime.Bootstrap != "/gosx/assets/runtime/bootstrap.33333333.js" {
		t.Fatalf("unexpected bootstrap url: %s", runtime.Bootstrap)
	}
	if runtime.VideoHLS != "/gosx/assets/runtime/hls.min.77777777.js" {
		t.Fatalf("unexpected video hls url: %s", runtime.VideoHLS)
	}

	islandAsset, ok := manifest.IslandAssetByName("Counter")
	if !ok {
		t.Fatal("expected Counter island asset")
	}
	if got := manifest.IslandURL("/gosx/assets", islandAsset); got != "/gosx/assets/islands/Counter.55555555.gxi" {
		t.Fatalf("unexpected island url: %s", got)
	}
	if got := manifest.CSSURL("/gosx/assets", manifest.CSS[0]); got != "/gosx/assets/css/app_counter.66666666.css" {
		t.Fatalf("unexpected css url: %s", got)
	}
	cssAsset, ok := manifest.CSSAssetBySource("app/counter.css")
	if !ok {
		t.Fatal("expected css asset by source")
	}
	if cssAsset.File != "app_counter.66666666.css" {
		t.Fatalf("unexpected css asset file: %s", cssAsset.File)
	}
}

func TestExportFilePath(t *testing.T) {
	cases := map[string]string{
		"/":              "index.html",
		"":               "index.html",
		"/docs":          filepath.Join("docs", "index.html"),
		"/docs/runtime/": filepath.Join("docs", "runtime", "index.html"),
		"docs/runtime":   filepath.Join("docs", "runtime", "index.html"),
	}
	for input, want := range cases {
		if got := ExportFilePath(input); got != want {
			t.Fatalf("ExportFilePath(%q) = %q, want %q", input, got, want)
		}
	}
}
