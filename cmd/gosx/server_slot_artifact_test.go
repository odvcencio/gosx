package main

import (
	"m31labs.dev/gosx"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestServerSlotTinyGoArtifact uses the production scratch-module builder.
// It is opt-in artifact preparation, not a browser acceptance assertion.
func TestServerSlotTinyGoArtifact(t *testing.T) {
	dir := os.Getenv("GOSX_E2B_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("explicit E2b artifact directory required")
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == string(filepath.Separator) {
		t.Fatal("artifact directory must be an explicit clean absolute directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("artifact directory must already exist, without symlink", err)
	}
	output := filepath.Join(dir, "runtime-tinygo-core-r1.wasm")
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("refusing to overwrite artifact", output, err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tinygo, err := exec.LookPath("tinygo")
	if err != nil {
		t.Fatal(err)
	}
	if err := buildTinyGoWASM(root, root, output, tinygo, "gosx_tiny_runtime", "gosx_runtime_core"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fresh production-builder core runtime path=%s bytes=%d hash=%s", output, len(data), contentHash(data))
}

func TestServerSlotExportAsset(t *testing.T) {
	refs := map[string]struct{}{}
	r := island.NewRenderer("export")
	p := &program.Program{Name: "Panels", Nodes: []program.Node{{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}}, {Kind: program.NodeServerSlot, Tag: "div", Text: "Profile"}}}
	node, err := r.RenderWithServerSlots(p, struct{}{}, island.ServerSlots{"Profile": gosx.El("input", gosx.Attrs(gosx.Attr("value", "caller")))})
	if err != nil {
		t.Fatal(err)
	}
	page := gosx.RenderHTML(gosx.Fragment(r.PageHead(), node))
	if _, err := rewriteStaticExportHTML("/", page); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{strings.ReplaceAll(page, runtimehost.ServerSlotRuntimeHash(), "0000000000000000"), strings.ReplaceAll(page, runtimehost.ServerSlotRuntimeIntegrity(), "sha256-"+strings.Repeat("A", 43)+"=")} {
		if _, err := rewriteStaticExportHTML("/", stale); err == nil || !strings.Contains(err.Error(), "stale ServerSlot") {
			t.Fatal("stale export accepted", err)
		}
	}
	addExportRuntimeAssetRefs(refs, page)
	if _, ok := refs["/gosx/server-slots.js"]; !ok {
		t.Fatal("actual manifest asset not discovered")
	}
	// Other runtime copy roles retain their existing tests; this control owns
	// only the newly discovered embedded capability leaf.
	refs = map[string]struct{}{"/gosx/server-slots.js": {}}
	dir := t.TempDir()
	if err := copyExportRuntime(t.TempDir(), dir, exportManifest{AssetRefs: sortedExportRuntimeAssetRefs(refs)}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "gosx", "server-slots.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != runtimehost.ServerSlotRuntime() {
		t.Fatal("exported capability differs from embedded asset")
	}
}
