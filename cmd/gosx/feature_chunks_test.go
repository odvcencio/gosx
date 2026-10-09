package main

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestRuntimeFeatureChunksAreWellFormed(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, chunk := range runtimeFeatureChunks {
		path := filepath.Join(root, "client", "js", "bootstrap-feature-"+chunk.name+".js")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("feature chunk %q: %v", chunk.name, err)
		}
		if chunk.role == "" {
			t.Errorf("feature chunk %q has no exclude role", chunk.name)
		}
		if _, ok := runtimeExcludableAssetRoles[chunk.role]; !ok {
			t.Errorf("feature chunk %q role %q is not in runtimeExcludableAssetRoles", chunk.name, chunk.role)
		}
	}
}

func TestExportResolvesGenericFeatureChunk(t *testing.T) {
	got, ok := exportRuntimeBuildPath("build", "/gosx/bootstrap-feature-engine-bridge.js")
	if !ok || got != filepath.Join("build", "bootstrap-feature-engine-bridge.js") {
		t.Fatalf("exportRuntimeBuildPath = %q, %v", got, ok)
	}
	if _, ok := exportRuntimeBuildPath("build", "/gosx/bootstrap-feature-../x.js"); ok {
		t.Fatal("a feature name with path separators must not resolve")
	}
	if _, ok := exportRuntimeBuildPath("build", "/gosx/bootstrap-feature-Bad Name.js"); ok {
		t.Fatal("an invalid feature name must not resolve")
	}
}

func TestManifestRuntimeRefResolvesFeatureChunk(t *testing.T) {
	manifest := &BuildManifest{}
	manifest.Runtime.Features = map[string]buildmanifest.HashedAsset{
		"engine-bridge": {File: "bootstrap-feature-engine-bridge.abcd.js"},
	}
	got, ok := manifestRuntimeRefSourcePath("dist", manifest, "/gosx/bootstrap-feature-engine-bridge.js")
	want := filepath.Join("dist", "assets", "runtime", "bootstrap-feature-engine-bridge.abcd.js")
	if !ok || got != want {
		t.Fatalf("manifestRuntimeRefSourcePath = %q, %v; want %q", got, ok, want)
	}
	if _, ok := manifestRuntimeRefSourcePath("dist", manifest, "/gosx/bootstrap-feature-painter.js"); ok {
		t.Fatal("a feature missing from the manifest must not resolve")
	}
}

func TestRuntimeSizeAssetsListFeatureChunks(t *testing.T) {
	m := &buildmanifest.Manifest{}
	m.Runtime.Features = map[string]buildmanifest.HashedAsset{
		"painter":       {File: "bootstrap-feature-painter.p.js"},
		"engine-bridge": {File: "bootstrap-feature-engine-bridge.e.js"},
	}
	var names []string
	for _, a := range runtimeSizeAssets(m) {
		if a.role == "feature chunk" && (a.file == "bootstrap-feature-painter.p.js" || a.file == "bootstrap-feature-engine-bridge.e.js") {
			names = append(names, a.name)
		}
	}
	if len(names) != 2 || names[0] != "bootstrap-feature-engine-bridge.js" || names[1] != "bootstrap-feature-painter.js" {
		t.Fatalf("feature chunk size rows = %v", names)
	}
}
