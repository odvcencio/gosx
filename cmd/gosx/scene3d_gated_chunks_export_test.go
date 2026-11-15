package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island"
)

// A static export finds runtime chunks only through the URLs a page
// publishes. A terrain-and-crowd scene must publish both chunk URLs, so the
// export copies both hashed files and rewrites the attributes to relative
// URLs that still resolve when the bundle is served under a subpath.
func TestStaticExportShipsTerrainAndCrowdChunks(t *testing.T) {
	dist, out := t.TempDir(), t.TempDir()
	files := map[string]string{
		"bootstrap-feature-scene3d.b1.js":         "base",
		"bootstrap-feature-scene3d-terrain.t1.js": "terrain chunk",
		"bootstrap-feature-scene3d-crowd.c1.js":   "crowd chunk",
	}
	for name, body := range files {
		mustWriteFile(t, filepath.Join(dist, "assets", "runtime", name), body)
	}
	manifest := &BuildManifest{}
	manifest.Runtime.BootstrapFeatureScene3D = HashedAsset{File: "bootstrap-feature-scene3d.b1.js", Hash: "b1"}
	manifest.Runtime.Features = map[string]HashedAsset{
		"scene3d-terrain": {File: "bootstrap-feature-scene3d-terrain.t1.js", Hash: "t1"},
		"scene3d-crowd":   {File: "bootstrap-feature-scene3d-crowd.c1.js", Hash: "c1"},
	}

	r := island.NewRenderer("main")
	r.ApplyBuildManifest(manifest, "/gosx/assets")
	props := json.RawMessage(`{"scene":{"environment":{"terrain":{"width":64},"crowds":[{"id":"stand"}]}}}`)
	r.RenderEngine(engine.Config{Name: "GoSXScene3D", Kind: engine.KindSurface, Props: props}, gosx.Text(""))
	page := "<!doctype html><html><head></head><body>" + gosx.RenderHTML(r.BootstrapScript()) + "</body></html>"

	refs := map[string]struct{}{}
	addExportRuntimeAssetRefs(refs, page)
	if err := stageManifestCompatibilityRuntime(dist, manifest, out, sortedExportRuntimeAssetRefs(refs)); err != nil {
		t.Fatal(err)
	}
	rewritten, err := rewriteStaticExportHTMLWith("/play", page, staticRewriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []struct{ attr, file, body string }{
		{"data-gosx-scene3d-terrain-url", "bootstrap-feature-scene3d-terrain.t1.js", "terrain chunk"},
		{"data-gosx-scene3d-crowd-url", "bootstrap-feature-scene3d-crowd.c1.js", "crowd chunk"},
	} {
		got, err := os.ReadFile(filepath.Join(out, "gosx", "assets", "runtime", chunk.file))
		if err != nil || string(got) != chunk.body {
			t.Fatalf("export did not copy %s: %q, %v (refs %v)", chunk.file, got, err, sortedExportRuntimeAssetRefs(refs))
		}
		if want := chunk.attr + `="../gosx/assets/runtime/` + chunk.file + `"`; !strings.Contains(rewritten, want) {
			t.Fatalf("exported page lacks the relative %s:\n%s", want, rewritten)
		}
	}
}
