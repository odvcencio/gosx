package island

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRendererTemplateIsolationAndManifestRefresh(t *testing.T) {
	t.Setenv("GOSX_ENV", "development")
	root := t.TempDir()
	path := filepath.Join(root, "build.json")
	write := func(file string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"runtime":{"wasm":{"file":"`+file+`","hash":"a","size":10}}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("first.wasm")
	SetManifestRoot(root)
	ResetBuildManifestCache()
	t.Cleanup(func() { ResetManifestRoot(); ResetBuildManifestCache() })
	a, b := NewRenderer("a"), NewRenderer("b")
	if a.manifest == b.manifest {
		t.Fatal("request state shared")
	}
	if a.manifest.Bundles["a"].Path == "" || b.manifest.Bundles["b"].Path == "" {
		t.Fatal("bundle IDs not retargeted")
	}
	a.SetBundle("extra", "/extra.wasm")
	a.SetProgramAsset("Counter", "/counter.bin", "bin", "hash")
	a.SetRuntime("/changed.wasm", "changed", 20)
	if _, ok := b.manifest.Bundles["extra"]; ok {
		t.Fatal("bundle mutation leaked")
	}
	if _, ok := b.programAssets["Counter"]; ok {
		t.Fatal("program mutation leaked")
	}
	if b.manifest.Runtime.Path == a.manifest.Runtime.Path {
		t.Fatal("runtime mutation leaked")
	}
	write("second-longer.wasm")
	c := NewRenderer("c")
	if c.manifest.Runtime.Path == b.manifest.Runtime.Path {
		t.Fatal("new manifest version ignored")
	}
}
