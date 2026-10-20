package main

import (
	"m31labs.dev/gosx/buildmanifest"
	"path/filepath"
	"testing"
)

func TestZoomAssetUsesItsHashedBundleAndSizeRole(t *testing.T) {
	manifest := &BuildManifest{Runtime: buildmanifest.RuntimeAssets{BootstrapFeatureScene3DZoom: buildmanifest.HashedAsset{File: "zoom.hash.js"}}}
	got, ok := manifestRuntimeRefSourcePath("dist", manifest, "/gosx/bootstrap-feature-scene3d-zoom.js")
	if !ok || got != filepath.Join("dist", "assets", "runtime", "zoom.hash.js") {
		t.Fatalf("zoom reference resolves to %q, %v", got, ok)
	}
	for _, asset := range runtimeSizeAssets(manifest) {
		if asset.name == "bootstrap-feature-scene3d-zoom.js" && asset.file == "zoom.hash.js" && asset.role == "scene3d zoom chunk" {
			return
		}
	}
	t.Fatal("zoom bundle must appear in the Scene3D size inventory")
}
