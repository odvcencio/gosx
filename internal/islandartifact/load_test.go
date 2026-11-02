package islandartifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/island/program"
)

func TestLoadRequiresMatchingSourceAndArtifact(t *testing.T) {
	t.Setenv("GOSX_DEV", "")
	t.Setenv("GOSX_ENV", "production")
	root := t.TempDir()
	source := filepath.Join(root, "app", "page.gsx")
	artifact := filepath.Join(root, "dist", "assets", "islands", "Sample.gxi")
	for _, dir := range []string{filepath.Dir(source), filepath.Dir(artifact)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	sourceBody := []byte("package app\n")
	if err := os.WriteFile(source, sourceBody, 0644); err != nil {
		t.Fatal(err)
	}
	data, err := program.EncodeBinary(&program.Program{Name: "Sample"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, data, 0644); err != nil {
		t.Fatal(err)
	}
	manifest := buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{{Name: "Sample", Format: "bin", SourceFile: "app/page.gsx", SourceHash: buildmanifest.ContentHash(sourceBody), HashedAsset: buildmanifest.HashedAsset{File: "Sample.gxi", Hash: buildmanifest.ContentHash(data), Size: int64(len(data))}}}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "build.json"), encoded, 0644); err != nil {
		t.Fatal(err)
	}
	got, found, err := Load(source, "Sample")
	if err != nil || !found || got.Name != "Sample" {
		t.Fatalf("matching artifact: found=%v, program=%v, err=%v", found, got, err)
	}
	t.Setenv("GOSX_ENV", "development")
	t.Setenv("GOSX_APP_ROOT", "")
	if _, found, err := Load(source, "Sample"); found || err != nil {
		t.Fatalf("local source must not use a stale dist: found=%v, err=%v", found, err)
	}
	deployedSource := filepath.Join(root, "dist", "app", "page.gsx")
	if err := os.MkdirAll(filepath.Dir(deployedSource), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deployedSource, sourceBody, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSX_APP_ROOT", filepath.Join(root, "dist"))
	if _, found, err := Load(deployedSource, "Sample"); !found || err != nil {
		t.Fatalf("deployed bundle without production env: found=%v, err=%v", found, err)
	}
	t.Setenv("GOSX_ENV", "production")
	if err := os.WriteFile(source, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Load(source, "Sample"); !found || err == nil || !strings.Contains(err.Error(), "source differs") {
		t.Fatalf("stale source: found=%v, err=%v", found, err)
	}
	t.Setenv("GOSX_DEV", "1")
	if _, found, err := Load(source, "Sample"); found || err != nil {
		t.Fatalf("development must compile current source: found=%v, err=%v", found, err)
	}
}
