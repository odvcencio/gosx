package island

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/island/program"
)

func writeDevRendererManifest(t *testing.T, root, digest string, version int64) {
	t.Helper()
	path := filepath.Join(root, "build", buildmanifest.DevRuntimeManifestFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{
		WASM:             buildmanifest.HashedAsset{Hash: digest, Size: 123},
		WASMExec:         buildmanifest.HashedAsset{Hash: strings.Repeat("b", 64)},
		BootstrapRuntime: buildmanifest.HashedAsset{Hash: strings.Repeat("c", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(version, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestDevRuntimeMetadataStartsColdAndRefreshes(t *testing.T) {
	t.Setenv("GOSX_DEV", "1")
	root := t.TempDir()
	SetManifestRoot(root)
	t.Cleanup(ResetManifestRoot)
	first := strings.Repeat("a", 64)
	writeDevRendererManifest(t, root, first, 100)
	r := NewRenderer("counter")
	r.RenderIslandFromProgram(program.CounterProgram(), nil)
	ref := r.Manifest().Runtime
	if ref.Hash != first || ref.Path != "/gosx/runtime.wasm?v="+first || ref.Size != 123 {
		t.Fatalf("cold dev runtime = %+v", ref)
	}
	head := gosx.RenderHTML(r.PageHead())
	if !strings.Contains(head, "/gosx/wasm_exec.js?v="+strings.Repeat("b", 64)) || !strings.Contains(head, `integrity="sha256-`) {
		t.Fatal("development scripts lost their version or integrity metadata")
	}
	second := strings.Repeat("e", 64)
	writeDevRendererManifest(t, root, second, 101)
	next := NewRenderer("counter")
	next.RenderIslandFromProgram(program.CounterProgram(), nil)
	if next.Manifest().Runtime.Hash != second || r.Manifest().Runtime.Hash != first {
		t.Fatal("metadata refresh was stale or mutated an existing request")
	}
}

func TestDevRuntimeMetadataOverridesStaleProductionOnlyInDev(t *testing.T) {
	root := t.TempDir()
	SetManifestRoot(root)
	t.Cleanup(ResetManifestRoot)
	devDigest := strings.Repeat("a", 64)
	writeDevRendererManifest(t, root, devDigest, 100)
	prod := filepath.Join(root, "dist", "build.json")
	if err := os.MkdirAll(filepath.Dir(prod), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prod, []byte(`{"runtime":{"wasm":{"file":"prod.wasm","hash":"prod","size":456}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSX_DEV", "1")
	r := NewRenderer("counter")
	r.RenderIslandFromProgram(program.CounterProgram(), nil)
	if r.Manifest().Runtime.Hash != devDigest {
		t.Fatal("stale production output masked development assets")
	}
	t.Setenv("GOSX_DEV", "")
	r = NewRenderer("counter")
	r.RenderIslandFromProgram(program.CounterProgram(), nil)
	if r.Manifest().Runtime.Hash != "prod" {
		t.Fatal("development metadata changed production manifest selection")
	}
	t.Setenv("GOSX_DEV", "1")
	if err := os.WriteFile(filepath.Join(root, "build", buildmanifest.DevRuntimeManifestFile), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if loadDefaultBuildManifest() != nil {
		t.Fatal("invalid development metadata silently selected a production build")
	}
}

func TestDevRuntimeMetadataKeepsStaticPagesWithoutWASM(t *testing.T) {
	t.Setenv("GOSX_DEV", "1")
	root := t.TempDir()
	SetManifestRoot(root)
	t.Cleanup(ResetManifestRoot)
	writeDevRendererManifest(t, root, strings.Repeat("a", 64), 100)
	r := NewRenderer("static")
	if r.clientManifest().Runtime.Path != "" || strings.Contains(gosx.RenderHTML(r.PageHead()), "wasm_exec.js") {
		t.Fatal("development metadata made a static page load WASM")
	}
}
