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
    "wasmIslands": {"file": "gosx-runtime-islands.99999999.wasm", "hash": "99999999", "size": 9},
	"wasmVariants": {
	  "core": {"file": "gosx-runtime-core.aaaaaaa1.wasm", "hash": "aaaaaaa1", "size": 8, "variant": "core", "featureMask": 17},
	  "engine": {"file": "gosx-runtime-engine.bbbbbbb2.wasm", "hash": "bbbbbbb2", "size": 12, "variant": "engine", "featureMask": 11}
	},
	    "wasmExec": {"file": "wasm_exec.22222222.js", "hash": "22222222", "size": 20},
	    "standardGoWasmExec": {"file": "standard-go-wasm_exec.2a2a2a2a.js", "hash": "2a2a2a2a", "size": 21},
    "bootstrap": {"file": "bootstrap.33333333.js", "hash": "33333333", "size": 30},
    "bootstrapFeatureScene3dCommand": {"file": "bootstrap-feature-scene3d-command.3d3d3d3d.js", "hash": "3d3d3d3d", "size": 33},
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
	if runtime.WASMIslands != "/gosx/assets/runtime/gosx-runtime-islands.99999999.wasm" {
		t.Fatalf("unexpected islands wasm url: %s", runtime.WASMIslands)
	}
	if runtime.WASMVariants["core"] != "/gosx/assets/runtime/gosx-runtime-core.aaaaaaa1.wasm" {
		t.Fatalf("unexpected core wasm url: %s", runtime.WASMVariants["core"])
	}
	if runtime.Bootstrap != "/gosx/assets/runtime/bootstrap.33333333.js" {
		t.Fatalf("unexpected bootstrap url: %s", runtime.Bootstrap)
	}
	if runtime.StandardGoWASMExec != "/gosx/assets/runtime/standard-go-wasm_exec.2a2a2a2a.js" {
		t.Fatalf("unexpected standard-Go wasm exec url: %s", runtime.StandardGoWASMExec)
	}
	if runtime.BootstrapFeatureScene3DCommand != "/gosx/assets/runtime/bootstrap-feature-scene3d-command.3d3d3d3d.js" {
		t.Fatalf("unexpected scene3d command url: %s", runtime.BootstrapFeatureScene3DCommand)
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

func TestManifestStaleIslandsReportsChangedSource(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "counter.gsx")
	original := []byte("package app\n\n//gosx:island\nfunc Counter() Node {\n\treturn <div>0</div>\n}\n")
	if err := os.WriteFile(sourcePath, original, 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	manifest := &Manifest{Islands: []IslandAsset{
		{
			Name:        "Counter",
			Format:      "bin",
			HashedAsset: HashedAsset{File: "Counter.55555555.gxi", Hash: "55555555", Size: 50},
			SourceFile:  "counter.gsx",
			SourceHash:  ContentHash(original),
		},
	}}

	if stale := manifest.StaleIslands(dir); len(stale) != 0 {
		t.Fatalf("unchanged source reported stale: %v", stale)
	}

	changed := []byte("package app\n\n//gosx:island\nfunc Counter() Node {\n\treturn <div>1</div>\n}\n")
	if err := os.WriteFile(sourcePath, changed, 0644); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}

	stale := manifest.StaleIslands(dir)
	if len(stale) != 1 || stale[0].Name != "Counter" || stale[0].SourceFile != "counter.gsx" {
		t.Fatalf("changed source stale report = %+v, want one StaleIsland{Name: Counter, SourceFile: counter.gsx}", stale)
	}
}

func TestManifestStaleIslandsSkipsBackCompatManifest(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "counter.gsx")
	if err := os.WriteFile(sourcePath, []byte("package app\n"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	// A manifest written before issue #166 has no SourceFile/SourceHash on
	// its island assets. StaleIslands must not report anything for it —
	// there is nothing recorded to compare the current source against, and
	// reporting staleness here would be a false positive on every startup.
	manifest := &Manifest{Islands: []IslandAsset{
		{Name: "Counter", Format: "bin", HashedAsset: HashedAsset{File: "Counter.55555555.gxi", Hash: "55555555", Size: 50}},
	}}

	if stale := manifest.StaleIslands(dir); len(stale) != 0 {
		t.Fatalf("back-compat manifest (no SourceFile/SourceHash) reported stale: %v", stale)
	}
}

func TestManifestStaleIslandsSkipsMissingSourceTree(t *testing.T) {
	manifest := &Manifest{Islands: []IslandAsset{
		{
			Name:        "Counter",
			Format:      "bin",
			HashedAsset: HashedAsset{File: "Counter.55555555.gxi", Hash: "55555555", Size: 50},
			SourceFile:  "counter.gsx",
			SourceHash:  "deadbeef",
		},
	}}

	// No sourceRoot: nothing to compare against.
	if stale := manifest.StaleIslands(""); len(stale) != 0 {
		t.Fatalf("empty sourceRoot reported stale: %v", stale)
	}

	// sourceRoot given, but the .gsx file is not there (production image
	// shipping dist/ without app source) — skip silently, not an error.
	if stale := manifest.StaleIslands(t.TempDir()); len(stale) != 0 {
		t.Fatalf("missing source file reported stale: %v", stale)
	}
}

// TestContentHashLength locks in the documented length: 16 hex characters
// (8 bytes) of the sha256 digest, not 8 hex characters.
func TestContentHashLength(t *testing.T) {
	if got := ContentHash([]byte("gosx")); len(got) != 16 {
		t.Fatalf("ContentHash length = %d, want 16 (got %q)", len(got), got)
	}
}

// TestManifestStaleIslandsRejectsEscapingSourceFile asserts StaleIslands
// skips any island whose recorded SourceFile is not local to sourceRoot —
// for example a ".." path that would otherwise let hashing escape the
// resolved root — instead of reading whatever it points at.
func TestManifestStaleIslandsRejectsEscapingSourceFile(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.gsx")
	if err := os.WriteFile(outsidePath, []byte("package app\n"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	rel, err := filepath.Rel(dir, outsidePath)
	if err != nil {
		t.Fatalf("relativize: %v", err)
	}

	manifest := &Manifest{Islands: []IslandAsset{
		{
			Name:        "Counter",
			Format:      "bin",
			HashedAsset: HashedAsset{File: "Counter.55555555.gxi", Hash: "55555555", Size: 50},
			SourceFile:  filepath.ToSlash(rel), // "../<tmp>/secret.gsx" — escapes dir
			SourceHash:  "deadbeef",            // deliberately wrong; must never be reached
		},
	}}

	if stale := manifest.StaleIslands(dir); len(stale) != 0 {
		t.Fatalf("escaping SourceFile reported stale: %v", stale)
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
