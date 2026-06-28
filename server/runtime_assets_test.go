package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeManifestDirectAssetPathUsesDistAssetsForAppRoot(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "dist", "assets", "runtime", "bootstrap-feature-scene3d-webgpu.hash.js")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatalf("mkdir asset dir: %v", err)
	}
	if err := os.WriteFile(assetPath, []byte("webgpu"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	got, ok := runtimeManifestDirectAssetPath(root, "assets/runtime/bootstrap-feature-scene3d-webgpu.hash.js")
	if !ok {
		t.Fatal("expected dist runtime asset to resolve")
	}
	if got != assetPath {
		t.Fatalf("asset path = %q, want %q", got, assetPath)
	}
}

func TestRuntimeManifestDirectAssetPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if got, ok := runtimeManifestDirectAssetPath(root, "assets/../secret.js"); ok || got != "" {
		t.Fatalf("traversal path resolved to %q", got)
	}
}
