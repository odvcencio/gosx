package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

func assertNavigationRuntimeAsset(t *testing.T, dir string, asset HashedAsset) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, asset.File))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != runtimehost.NavigationRuntime {
		t.Fatal("navigation asset differs from the server's embedded runtime")
	}
	if asset.Integrity != buildmanifest.ContentIntegrity(data) || asset.Hash != contentHash(data) || asset.Size != int64(len(data)) {
		t.Fatalf("navigation manifest metadata = %#v", asset)
	}
	if want := "navigation." + contentHash(data) + ".js"; asset.File != want {
		t.Fatalf("navigation filename = %q, want %q", asset.File, want)
	}
	for _, ext := range []string{".gz", ".br"} {
		compressed, err := os.Open(filepath.Join(dir, asset.File) + ext)
		if err != nil {
			t.Fatal(err)
		}
		var reader io.Reader = brotli.NewReader(compressed)
		if ext == ".gz" {
			gz, err := gzip.NewReader(compressed)
			if err != nil {
				compressed.Close()
				t.Fatal(err)
			}
			defer gz.Close()
			reader = gz
		}
		decoded, err := io.ReadAll(reader)
		compressed.Close()
		if err != nil || string(decoded) != string(data) {
			t.Fatalf("navigation%s sidecar does not decode to runtime: %v", ext, err)
		}
	}
}

func TestNavigationRuntimeAssetBuildAndExport(t *testing.T) {
	buildDir := t.TempDir()
	runtimeDir := filepath.Join(buildDir, "assets", "runtime")
	asset, err := writeNavigationRuntimeAsset(runtimeDir, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	manifest := BuildManifest{Runtime: RuntimeAssets{Navigation: asset}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(buildDir, "build.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := buildmanifest.Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNavigationRuntimeAsset(t, runtimeDir, loaded.Runtime.Navigation)
	ref := "/gosx/assets/runtime/" + asset.File
	if got := loaded.RuntimeURLs("/gosx/assets").Navigation; got != ref {
		t.Fatalf("navigation manifest URL = %q, want %q", got, ref)
	}
	for _, production := range []bool{false, true} {
		outputDir := t.TempDir()
		export := exportManifest{AssetRefs: []string{ref}}
		if production {
			err = stageStaticBuildAssets(buildDir, &manifest, outputDir, export)
		} else {
			err = copyExportRuntime(buildDir, outputDir, export)
		}
		if err != nil {
			t.Fatal(err)
		}
		assertNavigationRuntimeAsset(t, filepath.Join(outputDir, "gosx", "assets", "runtime"), asset)
	}
}

func navigationManifestAsset(t *testing.T, filename string) HashedAsset {
	t.Helper()
	manifest, err := buildmanifest.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(manifest.Runtime.Navigation.File, "navigation.") {
		t.Fatalf("build manifest omitted navigation asset: %#v", manifest.Runtime.Navigation)
	}
	return manifest.Runtime.Navigation
}
