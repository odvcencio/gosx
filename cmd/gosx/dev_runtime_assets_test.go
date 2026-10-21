package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestDevRuntimeManifestBindsEveryStagedAsset(t *testing.T) {
	buildDir := t.TempDir()
	var assets RuntimeAssets
	if err := stageDevRuntimeJavaScript(filepath.Join("..", ".."), buildDir, &assets); err != nil {
		t.Fatal(err)
	}
	// Unit fixtures only; the browser test uses a real compiler-produced module.
	for name, data := range map[string][]byte{
		"gosx-runtime.wasm":        {0, 97, 115, 109, 1, 0, 0, 0},
		"wasm_exec.js":             []byte("// shared Go shim fixture"),
		"standard-go-wasm_exec.js": []byte("// standard Go shim fixture"),
	} {
		if err := os.WriteFile(filepath.Join(buildDir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDevRuntimeManifest(buildDir, assets); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(buildDir, buildmanifest.DevRuntimeManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BuildManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	check := func(name string, asset HashedAsset) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(buildDir, name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(body)
		if asset.Hash != hex.EncodeToString(digest[:]) || asset.Integrity != buildmanifest.ContentIntegrity(body) || asset.Size != int64(len(body)) || asset.File != "" {
			t.Fatalf("%s metadata does not describe staged bytes: %+v", name, asset)
		}
	}
	check("gosx-runtime.wasm", manifest.Runtime.WASM)
	check("wasm_exec.js", manifest.Runtime.WASMExec)
	check("standard-go-wasm_exec.js", manifest.Runtime.StandardGoWASMExec)
	for _, asset := range runtimeJSAssetSpecs("", &manifest.Runtime) {
		check(asset.name+".js", *asset.dest)
	}
	for _, chunk := range runtimeFeatureChunks {
		check("bootstrap-feature-"+chunk.name+".js", manifest.Runtime.Features[chunk.name])
	}
}

func TestDevRuntimeManifestFailurePreservesPublishedMetadata(t *testing.T) {
	buildDir := t.TempDir()
	path := filepath.Join(buildDir, buildmanifest.DevRuntimeManifestFile)
	previous := []byte(`{"previous":"published"}`)
	if err := os.WriteFile(path, previous, 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeDevRuntimeManifest(buildDir, RuntimeAssets{}); err == nil {
		t.Fatal("manifest published without a staged native runtime")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(previous) {
		t.Fatalf("failed staging replaced published metadata: %q, %v", got, err)
	}
	matches, err := filepath.Glob(filepath.Join(buildDir, ".gosx-dev-runtime-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging left temporary publications: %v, %v", matches, err)
	}
}
