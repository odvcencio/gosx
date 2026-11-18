package server

import (
	"m31labs.dev/gosx/buildmanifest"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeAssetInfo(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "build")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "gosx-runtime.wasm")
	if _, _, err := RuntimeAssetInfo(root); err == nil {
		t.Fatal("missing runtime accepted")
	}
	for _, data := range []string{"first", "first", "second runtime"} {
		if err := os.WriteFile(file, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		hash, size, err := RuntimeAssetInfo(root)
		if err != nil || hash != buildmanifest.ContentHash([]byte(data)) || size != int64(len(data)) {
			t.Fatalf("hash %s size %d error %v", hash, size, err)
		}
	}
}
