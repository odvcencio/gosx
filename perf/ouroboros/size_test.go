package ouroboros

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func TestBuildSizeEvidenceAttributesRouteAssetsAndDedupesTotals(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	writeTestFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.1111.js"), "shared-runtime")
	writeTestFile(t, filepath.Join(runtimeDir, "bootstrap-feature-islands.2222.js"), "islands-feature")
	writeTestFile(t, filepath.Join(runtimeDir, "bootstrap-feature-engines.3333.js"), "engine-feature")
	writeTestFile(t, filepath.Join(runtimeDir, "gosx-runtime.4444.wasm"), "wasm-full")
	writeTestFile(t, filepath.Join(runtimeDir, "gosx-runtime-islands.5555.wasm"), "wasm-islands")
	writeTestFile(t, filepath.Join(runtimeDir, "wasm_exec.6666.js"), "shim")
	writeTestFile(t, filepath.Join(runtimeDir, "devtools-lantern.7777.js"), "devtools")
	writeTestFile(t, filepath.Join(runtimeDir, "youtube-audio.8888.js"), "youtube")
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "wasm": {"file": "gosx-runtime.4444.wasm", "hash": "4444", "size": 9},
    "wasmIslands": {"file": "gosx-runtime-islands.5555.wasm", "hash": "5555", "size": 12},
    "wasmExec": {"file": "wasm_exec.6666.js", "hash": "6666", "size": 4},
    "bootstrapRuntime": {"file": "bootstrap-runtime.1111.js", "hash": "1111", "size": 14},
    "bootstrapFeatureIslands": {"file": "bootstrap-feature-islands.2222.js", "hash": "2222", "size": 15},
    "bootstrapFeatureEngines": {"file": "bootstrap-feature-engines.3333.js", "hash": "3333", "size": 14},
    "devtoolsLantern": {"file": "devtools-lantern.7777.js", "hash": "7777", "size": 8},
    "youtubeAudio": {"file": "youtube-audio.8888.js", "hash": "8888", "size": 7}
  },
  "islands": [],
  "css": []
}`)
	writeTestFile(t, filepath.Join(dir, "export.json"), `{
  "routes": [
    {"path": "/island/counter", "file": "island/counter/index.html", "capabilities": {"wasm": true}},
    {"path": "/canvas-board", "file": "canvas-board/index.html", "capabilities": {"engines": 1}}
  ]
}`)
	writeTestFile(t, filepath.Join(dir, "static", "island", "counter", "index.html"), `<script src="/gosx/bootstrap-runtime.js"></script><script src="/gosx/bootstrap-feature-islands.js"></script><script src="/gosx/youtube-audio.js"></script>`)
	writeTestFile(t, filepath.Join(dir, "static", "canvas-board", "index.html"), `<script src="/gosx/bootstrap-runtime.js"></script><script src="/gosx/bootstrap-feature-engines.js"></script><script src="/gosx/devtools-lantern.js"></script>`)

	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		GeneratedAt:  "2026-08-09T00:00:00Z",
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != SchemaVersion || report.Contract != ContractO02 {
		t.Fatalf("unexpected schema header: %#v", report)
	}
	if report.Totals.RouteCount != 2 || report.Totals.RoutesWithExplicitRefs != 2 {
		t.Fatalf("unexpected route totals: %#v", report.Totals)
	}
	if report.Totals.AssetCount != 8 {
		t.Fatalf("unexpected asset count: %#v", report.Totals)
	}
	shared := findAssetByURL(report.Assets, "/gosx/bootstrap-runtime.js")
	if shared == nil {
		t.Fatal("missing bootstrap-runtime asset")
	}
	if got, want := shared.SHA256, shaHex("shared-runtime"); got != want {
		t.Fatalf("sha256 = %s, want %s", got, want)
	}
	if len(shared.UsedByRoutes) != 2 {
		t.Fatalf("shared asset routes = %#v", shared.UsedByRoutes)
	}
	devtools := findAssetByURL(report.Assets, "/gosx/devtools-lantern.js")
	if devtools == nil || devtools.Bytes != int64(len("devtools")) || strings.Join(devtools.UsedByRoutes, ",") != "/canvas-board" {
		t.Fatalf("bad devtools attribution: %#v", devtools)
	}
	youtube := findAssetByURL(report.Assets, "/gosx/youtube-audio.js")
	if youtube == nil || youtube.Bytes != int64(len("youtube")) || strings.Join(youtube.UsedByRoutes, ",") != "/island/counter" {
		t.Fatalf("bad youtube attribution: %#v", youtube)
	}
	for _, route := range report.Routes {
		if route.SharedRawBytes != int64(len("shared-runtime")) {
			t.Fatalf("route %s shared bytes = %d", route.Route, route.SharedRawBytes)
		}
	}
	routes := routeEvidenceByPath(report.Routes)
	if routes["/island/counter"].UniqueRawBytes != int64(len("islands-feature")+len("youtube")) {
		t.Fatalf("island route inherited wrong unique bytes: %#v", routes["/island/counter"])
	}
	if routes["/canvas-board"].UniqueRawBytes != int64(len("engine-feature")+len("devtools")) {
		t.Fatalf("canvas route inherited wrong unique bytes: %#v", routes["/canvas-board"])
	}
}

func TestBuildSizeEvidenceResolvesHashedURLsWithQuery(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	writeTestFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.abcd.js"), "hashed-runtime")
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "bootstrapRuntime": {"file": "bootstrap-runtime.abcd.js", "hash": "abcd", "size": 14}
  },
  "islands": [],
  "css": []
}`)
	writeTestFile(t, filepath.Join(dir, "export.json"), `{"routes":[{"path":"/a","file":"a/index.html","capabilities":{"bootstrap":true}}]}`)
	writeTestFile(t, filepath.Join(dir, "static", "a", "index.html"), `<script src="gosx/assets/runtime/bootstrap-runtime.abcd.js?v=abcd"></script>`)

	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Routes) != 1 || len(report.Routes[0].AssetIDs) != 1 {
		t.Fatalf("unexpected route attribution: %#v", report.Routes)
	}
	if asset := findAssetByURL(report.Assets, "/gosx/assets/runtime/bootstrap-runtime.abcd.js"); asset == nil || asset.ManifestHash != "abcd" {
		t.Fatalf("hashed asset not resolved through manifest: %#v", report.Assets)
	}
}

func TestBuildSizeEvidenceRecordsNoncanonicalUnresolvedRefs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "build.json"), `{"runtime":{},"islands":[],"css":[]}`)
	writeTestFile(t, filepath.Join(dir, "export.json"), `{"routes":[{"path":"/a","file":"a/index.html","capabilities":{"bootstrap":true}}]}`)
	writeTestFile(t, filepath.Join(dir, "static", "a", "index.html"), `<script src="/gosx/assets/runtime/missing.js"></script>`)

	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || report.Canonical || len(report.Unresolved) == 0 {
		t.Fatalf("expected noncanonical unresolved refs, got %#v", report)
	}
}

func TestCollectTransferredAssetsRejectsCanonicalUnmanifestedDirectAsset(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "assets", "runtime", "loose.js"), "loose")
	refs := map[string]string{"/gosx/assets/runtime/loose.js": "route-transfer"}
	assets, unresolved, err := collectTransferredAssets(dir, &buildmanifest.Manifest{}, refs, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 0 || len(unresolved) != 1 || !strings.Contains(unresolved[0].Reason, "not found") {
		t.Fatalf("canonical direct asset bypass was not rejected: assets=%#v unresolved=%#v", assets, unresolved)
	}
	assets, unresolved, err = collectTransferredAssets(dir, &buildmanifest.Manifest{}, refs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 0 || len(unresolved) != 1 {
		t.Fatalf("noncanonical direct asset bypass was not rejected: assets=%#v unresolved=%#v", assets, unresolved)
	}
}

func TestAttributeRoutesDoesNotStaleAssetPointersAfterDirectAppend(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	writeTestFile(t, filepath.Join(runtimeDir, "bootstrap-runtime.1111.js"), "shared-runtime")
	writeTestFile(t, filepath.Join(runtimeDir, "direct.2222.js"), "direct-only")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			BootstrapRuntime: buildmanifest.HashedAsset{File: "bootstrap-runtime.1111.js", Hash: "1111", Size: int64(len("shared-runtime"))},
			Bootstrap:        buildmanifest.HashedAsset{File: "direct.2222.js", Hash: "2222", Size: int64(len("direct-only"))},
		},
	}
	report := &SizeEvidence{DistDir: dir}
	assets, unresolved, err := collectTransferredAssets(dir, manifest, map[string]string{"/gosx/bootstrap-runtime.js": "manifest-runtime"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("unexpected unresolved assets: %#v", unresolved)
	}
	report.Assets = assets
	htmlA := `<script src="/gosx/assets/runtime/direct.2222.js"></script>`
	htmlB := `<script src="/gosx/bootstrap-runtime.js"></script>`
	htmlC := `<script src="/gosx/bootstrap-runtime.js"></script>`
	writeTestFile(t, filepath.Join(dir, "a", "index.html"), htmlA)
	writeTestFile(t, filepath.Join(dir, "b", "index.html"), htmlB)
	writeTestFile(t, filepath.Join(dir, "c", "index.html"), htmlC)
	err = attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{
		{Path: "/a", File: "a/index.html", SHA256: sha256String(htmlA), Bytes: int64Ptr(int64(len(htmlA)))},
		{Path: "/b", File: "b/index.html", SHA256: sha256String(htmlB), Bytes: int64Ptr(int64(len(htmlB)))},
		{Path: "/c", File: "c/index.html", SHA256: sha256String(htmlC), Bytes: int64Ptr(int64(len(htmlC)))},
	}}, manifest, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	shared := findAssetByURL(report.Assets, "/gosx/bootstrap-runtime.js")
	if shared == nil {
		t.Fatal("missing shared runtime asset")
	}
	if got, want := strings.Join(shared.UsedByRoutes, ","), "/b,/c"; got != want {
		t.Fatalf("shared UsedByRoutes = %q, want %q", got, want)
	}
	routes := routeEvidenceByPath(report.Routes)
	if routes["/a"].UniqueRawBytes != int64(len("direct-only")) || routes["/a"].SharedRawBytes != 0 {
		t.Fatalf("bad direct route bytes: %#v", routes["/a"])
	}
	if routes["/b"].SharedRawBytes != int64(len("shared-runtime")) || routes["/b"].UniqueRawBytes != 0 {
		t.Fatalf("bad first shared route bytes: %#v", routes["/b"])
	}
	if routes["/c"].SharedRawBytes != int64(len("shared-runtime")) || routes["/c"].UniqueRawBytes != 0 {
		t.Fatalf("bad second shared route bytes: %#v", routes["/c"])
	}
}

func TestDirectAssetSourceBindsExactManifestBucket(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "assets", "runtime", "same.js"), "runtime")
	writeTestFile(t, filepath.Join(dir, "assets", "css", "same.js"), "css")
	writeTestFile(t, filepath.Join(dir, "assets", "islands", "same.js"), "island")
	manifest := &buildmanifest.Manifest{
		Islands: []buildmanifest.IslandAsset{{Name: "same", HashedAsset: buildmanifest.HashedAsset{File: "same.js", Hash: "island"}}},
		CSS:     []buildmanifest.CSSAsset{{Component: "same", HashedAsset: buildmanifest.HashedAsset{File: "same.js", Hash: "css"}}},
	}
	_, _, ok := directAssetSource(dir, manifest, "runtime/same.js")
	if ok {
		t.Fatal("runtime direct ref resolved through CSS or island basename collision")
	}
	sourcePath, asset, ok := directAssetSource(dir, manifest, "css/same.js")
	if !ok || asset.Hash != "css" || !strings.HasSuffix(sourcePath, filepath.Join("assets", "css", "same.js")) {
		t.Fatalf("css direct ref did not bind exact bucket: path=%s asset=%#v ok=%v", sourcePath, asset, ok)
	}
}

func TestManifestRuntimeRefsIncludeLanternAndYouTubeAudio(t *testing.T) {
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			DevtoolsLantern: buildmanifest.HashedAsset{File: "devtools-lantern.1111.js", Hash: "1111", Size: 8},
			YouTubeAudio:    buildmanifest.HashedAsset{File: "youtube-audio.2222.js", Hash: "2222", Size: 7},
		},
	}
	refs := map[string]string{}
	addManifestRuntimeRefs(refs, manifest)
	if refs["/gosx/devtools-lantern.js"] != "manifest-runtime" {
		t.Fatalf("missing devtools lantern manifest ref: %#v", refs)
	}
	if refs["/gosx/youtube-audio.js"] != "manifest-runtime" {
		t.Fatalf("missing youtube audio manifest ref: %#v", refs)
	}
}

func TestDirectAssetSourceBindsNewRuntimeAssetsToRuntimeBucket(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "assets", "runtime", "devtools-lantern.1111.js"), "runtime-devtools")
	writeTestFile(t, filepath.Join(dir, "assets", "runtime", "youtube-audio.2222.js"), "runtime-youtube")
	writeTestFile(t, filepath.Join(dir, "assets", "css", "devtools-lantern.1111.js"), "css-devtools")
	manifest := &buildmanifest.Manifest{
		Runtime: buildmanifest.RuntimeAssets{
			DevtoolsLantern: buildmanifest.HashedAsset{File: "devtools-lantern.1111.js", Hash: "runtime-devtools"},
			YouTubeAudio:    buildmanifest.HashedAsset{File: "youtube-audio.2222.js", Hash: "runtime-youtube"},
		},
		CSS: []buildmanifest.CSSAsset{{Component: "devtools", HashedAsset: buildmanifest.HashedAsset{File: "devtools-lantern.1111.js", Hash: "css"}}},
	}
	sourcePath, asset, ok := directAssetSource(dir, manifest, "runtime/devtools-lantern.1111.js")
	if !ok || asset.Hash != "runtime-devtools" || !strings.HasSuffix(sourcePath, filepath.Join("assets", "runtime", "devtools-lantern.1111.js")) {
		t.Fatalf("devtools direct ref did not bind runtime bucket: path=%s asset=%#v ok=%v", sourcePath, asset, ok)
	}
	sourcePath, asset, ok = directAssetSource(dir, manifest, "runtime/youtube-audio.2222.js")
	if !ok || asset.Hash != "runtime-youtube" || !strings.HasSuffix(sourcePath, filepath.Join("assets", "runtime", "youtube-audio.2222.js")) {
		t.Fatalf("youtube direct ref did not bind runtime bucket: path=%s asset=%#v ok=%v", sourcePath, asset, ok)
	}
}

func TestCollectTransferredAssetsFailsClosedForNewRuntimeAssets(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		manifest := &buildmanifest.Manifest{
			Runtime: buildmanifest.RuntimeAssets{
				DevtoolsLantern: buildmanifest.HashedAsset{File: "devtools-lantern.1111.js", Hash: "1111", Size: 8},
			},
		}
		assets, unresolved, err := collectTransferredAssets(dir, manifest, map[string]string{"/gosx/devtools-lantern.js": "manifest-runtime"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(assets) != 0 || len(unresolved) != 1 || !strings.Contains(unresolved[0].Reason, "not found") {
			t.Fatalf("missing devtools did not fail closed: assets=%#v unresolved=%#v", assets, unresolved)
		}
	})
	t.Run("size_mismatch", func(t *testing.T) {
		dir := t.TempDir()
		runtimeDir := filepath.Join(dir, "assets", "runtime")
		writeTestFile(t, filepath.Join(runtimeDir, "youtube-audio.2222.js"), "youtube")
		manifest := &buildmanifest.Manifest{
			Runtime: buildmanifest.RuntimeAssets{
				YouTubeAudio: buildmanifest.HashedAsset{File: "youtube-audio.2222.js", Hash: "2222", Size: 999},
			},
		}
		assets, unresolved, err := collectTransferredAssets(dir, manifest, map[string]string{"/gosx/youtube-audio.js": "manifest-runtime"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(assets) != 0 || len(unresolved) != 1 || !strings.Contains(unresolved[0].Reason, "size") {
			t.Fatalf("size mismatch did not fail closed: assets=%#v unresolved=%#v", assets, unresolved)
		}
	})
}

func TestBuildSizeEvidenceRequiresInventoryForCanonicalMode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "build.json"), `{"runtime":{},"islands":[],"css":[]}`)
	_, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires --inventory") {
		t.Fatalf("BuildSizeEvidenceWithOptions error = %v, want inventory requirement", err)
	}
}

func TestBuildSizeEvidencePreflightsExportBeforeSourceArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "build.json"), `{"runtime":{},"islands":[],"css":[]}`)
	artifactRoot := filepath.Join(t.TempDir(), "missing-export-artifacts")
	_, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath:  filepath.Join(dir, "build.json"),
		DistDir:       dir,
		RepoRoot:      ".",
		ArtifactRoot:  artifactRoot,
		InventoryPath: filepath.Join(t.TempDir(), "missing-inventory.json"),
		Canonical:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires export.json") {
		t.Fatalf("BuildSizeEvidenceWithOptions error = %v, want export preflight", err)
	}
	if _, statErr := os.Stat(artifactRoot); !os.IsNotExist(statErr) {
		t.Fatalf("artifact root was mutated before export preflight failure: %v", statErr)
	}
}

func TestLoadExportEvidenceStrictRejectsMissingMalformedAndDuplicateRoutes(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadExportEvidence(filepath.Join(dir, "export.json"), true); err == nil {
		t.Fatal("expected missing export.json rejection")
	}
	malformed := filepath.Join(dir, "malformed.json")
	writeTestFile(t, malformed, `{`)
	if _, err := loadExportEvidence(malformed, true); err == nil {
		t.Fatal("expected malformed export.json rejection")
	}
	emptyRoute := filepath.Join(dir, "empty-route.json")
	writeTestFile(t, emptyRoute, `{"routes":[{"path":"","file":"index.html"}]}`)
	if _, err := loadExportEvidence(emptyRoute, true); err == nil || !strings.Contains(err.Error(), "empty route") {
		t.Fatalf("loadExportEvidence empty route error = %v", err)
	}
	duplicateRoute := filepath.Join(dir, "duplicate-route.json")
	writeTestFile(t, duplicateRoute, `{"routes":[{"path":"/a","file":"a.html"},{"path":"/a","file":"b.html"}]}`)
	if _, err := loadExportEvidence(duplicateRoute, true); err == nil || !strings.Contains(err.Error(), "duplicate route") {
		t.Fatalf("loadExportEvidence duplicate route error = %v", err)
	}
	unknownField := filepath.Join(dir, "unknown-field.json")
	writeTestFile(t, unknownField, `{"routes":[],"unknown":true}`)
	if _, err := loadExportEvidence(unknownField, true); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("loadExportEvidence unknown field error = %v", err)
	}
	trailing := filepath.Join(dir, "trailing.json")
	writeTestFile(t, trailing, `{"routes":[{"path":"/static","file":"static/index.html"}]} {"routes":[]}`)
	if _, err := loadExportEvidence(trailing, true); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("loadExportEvidence trailing JSON error = %v", err)
	}
	missingCanonical := filepath.Join(dir, "missing-canonical.json")
	writeTestFile(t, missingCanonical, `{"routes":[{"path":"/static","file":"static/index.html"}]}`)
	if _, err := loadExportEvidence(missingCanonical, true); err == nil || !strings.Contains(err.Error(), "missing canonical Ouroboros routes") {
		t.Fatalf("loadExportEvidence missing canonical routes error = %v", err)
	}
}

func TestLoadExportEvidenceStrictBindsCanonicalRouteFiles(t *testing.T) {
	dir := t.TempDir()
	writeCase := func(name string, mutate func([]exportEvidenceRoute)) string {
		t.Helper()
		routes := canonicalExportEvidenceRoutesForTest()
		mutate(routes)
		body, err := json.Marshal(struct {
			Routes           []exportEvidenceRoute `json:"routes"`
			ResourceManifest string                `json:"resourceManifest"`
		}{
			Routes:           routes,
			ResourceManifest: CanonicalResourceManifestRef,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name+".json")
		writeTestFile(t, path, string(body))
		return path
	}

	swapped := writeCase("swapped", func(routes []exportEvidenceRoute) {
		routes[0].File, routes[1].File = routes[1].File, routes[0].File
	})
	if _, err := loadExportEvidence(swapped, true); err == nil || !strings.Contains(err.Error(), "route /static file") {
		t.Fatalf("swapped route files error = %v", err)
	}

	duplicate := writeCase("duplicate", func(routes []exportEvidenceRoute) {
		routes[1].File = routes[0].File
	})
	if _, err := loadExportEvidence(duplicate, true); err == nil || !strings.Contains(err.Error(), "duplicates HTML file") {
		t.Fatalf("duplicate route file error = %v", err)
	}

	alternate := writeCase("alternate-contained", func(routes []exportEvidenceRoute) {
		routes[1].File = "alternate/lite/index.html"
	})
	if _, err := loadExportEvidence(alternate, true); err == nil || !strings.Contains(err.Error(), "route /lite file") {
		t.Fatalf("alternate contained route file error = %v", err)
	}
}

func TestLoadExportEvidenceStrictRequiresRouteHTMLIdentity(t *testing.T) {
	dir := t.TempDir()
	writeCase := func(name string, mutate func([]exportEvidenceRoute)) string {
		t.Helper()
		routes := canonicalExportEvidenceRoutesForTest()
		mutate(routes)
		body, err := json.Marshal(struct {
			Routes           []exportEvidenceRoute `json:"routes"`
			ResourceManifest string                `json:"resourceManifest"`
		}{
			Routes:           routes,
			ResourceManifest: CanonicalResourceManifestRef,
		})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name+".json")
		writeTestFile(t, path, string(body))
		return path
	}

	missingHash := writeCase("missing-hash", func(routes []exportEvidenceRoute) {
		routes[0].SHA256 = ""
	})
	if _, err := loadExportEvidence(missingHash, true); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("missing hash error = %v", err)
	}

	missingBytes := writeCase("missing-bytes", func(routes []exportEvidenceRoute) {
		routes[0].Bytes = nil
	})
	if _, err := loadExportEvidence(missingBytes, true); err == nil || !strings.Contains(err.Error(), "bytes is required") {
		t.Fatalf("missing bytes error = %v", err)
	}
}

func TestValidateExportHTMLAttributionRejectsTamperedCanonicalHTML(t *testing.T) {
	dir := t.TempDir()
	original := `A<script src="/gosx/bootstrap-runtime.js"></script>`
	route := exportEvidenceRoute{
		Path:   "/lite",
		File:   "lite/index.html",
		SHA256: sha256String(original),
		Bytes:  int64Ptr(int64(len(original))),
	}
	writeTestFile(t, filepath.Join(dir, "lite", "index.html"), `B<script src="/gosx/bootstrap-runtime.js"></script>`)
	err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{route}})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("tampered route HTML error = %v", err)
	}
}

func TestValidateExportHTMLAttributionRejectsWrongCanonicalHTMLHash(t *testing.T) {
	dir := t.TempDir()
	body := `<script src="/gosx/bootstrap-runtime.js"></script>`
	writeTestFile(t, filepath.Join(dir, "lite", "index.html"), body)
	err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{{
		Path:   "/lite",
		File:   "lite/index.html",
		SHA256: "sha256:" + strings.Repeat("0", 64),
		Bytes:  int64Ptr(int64(len(body))),
	}}})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("wrong route HTML hash error = %v", err)
	}
}

func TestValidateExportHTMLAttributionRejectsCanonicalStaticFallbackOnlyHTML(t *testing.T) {
	dir := t.TempDir()
	body := `<script src="/gosx/bootstrap-runtime.js"></script>`
	writeTestFile(t, filepath.Join(dir, "static", "lite", "index.html"), body)
	err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{{
		Path:   "/lite",
		File:   "lite/index.html",
		SHA256: sha256String(body),
		Bytes:  int64Ptr(int64(len(body))),
	}}})
	if err == nil || !strings.Contains(err.Error(), "missing or unsafe route file") {
		t.Fatalf("canonical fallback-only route HTML error = %v", err)
	}
}

func TestBuildSizeEvidencePreservesNoncanonicalAlternateContainedHTML(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "assets", "runtime", "bootstrap-runtime.1111.js"), "shared-runtime")
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "bootstrapRuntime": {"file": "bootstrap-runtime.1111.js", "hash": "1111", "size": 14}
  },
  "islands": [],
  "css": []
}`)
	writeTestFile(t, filepath.Join(dir, "export.json"), `{
  "routes": [
    {"path": "/lite", "file": "alternate/lite/index.html"},
    {"path": "/also-lite", "file": "alternate/lite/index.html"}
  ]
}`)
	writeTestFile(t, filepath.Join(dir, "static", "alternate", "lite", "index.html"), `<script src="/gosx/bootstrap-runtime.js"></script>`)

	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Routes) != 2 {
		t.Fatalf("noncanonical alternate route files were not preserved: %#v", report.Routes)
	}
	if got := report.Routes[0].File; got != "alternate/lite/index.html" {
		t.Fatalf("route file = %q, want alternate/lite/index.html", got)
	}
}

func TestValidateExportHTMLAttributionAllowsR00OnlyWithoutRefs(t *testing.T) {
	dir := t.TempDir()
	staticHTML := `<html>SSR only</html>`
	writeTestFile(t, filepath.Join(dir, "static", "index.html"), staticHTML)
	if err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{{Path: "/static", File: "static/index.html", SHA256: sha256String(staticHTML), Bytes: int64Ptr(int64(len(staticHTML)))}}}); err != nil {
		t.Fatalf("R00 without runtime refs failed: %v", err)
	}
	liteHTML := `<html>missing runtime refs</html>`
	writeTestFile(t, filepath.Join(dir, "lite", "index.html"), liteHTML)
	err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{{Path: "/lite", File: "lite/index.html", SHA256: sha256String(liteHTML), Bytes: int64Ptr(int64(len(liteHTML)))}}})
	if err == nil || !strings.Contains(err.Error(), "incomplete asset attribution") {
		t.Fatalf("non-R00 without runtime refs error = %v", err)
	}
}

func TestAttributeRoutesStrictRejectsMissingUnsafeAndIncompleteHTML(t *testing.T) {
	dir := t.TempDir()
	report := &SizeEvidence{DistDir: dir}
	err := attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{{Path: "/missing", File: "missing.html"}}}, nil, nil, true)
	if err == nil || !strings.Contains(err.Error(), "missing or unsafe route file") {
		t.Fatalf("attributeRoutes missing HTML error = %v", err)
	}
	report = &SizeEvidence{DistDir: dir}
	err = attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{{Path: "/unsafe", File: "../outside.html"}}}, nil, nil, true)
	if err == nil || !strings.Contains(err.Error(), "missing or unsafe route file") {
		t.Fatalf("attributeRoutes unsafe HTML error = %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "empty", "index.html"), `<html></html>`)
	report = &SizeEvidence{DistDir: dir}
	err = attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{{Path: "/empty", File: "empty/index.html"}}}, nil, nil, true)
	if err == nil || !strings.Contains(err.Error(), "incomplete asset attribution") {
		t.Fatalf("attributeRoutes incomplete attribution error = %v", err)
	}
}

func TestBuildSizeEvidenceRecordsNoncanonicalUnsafeManifestPaths(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "escape.js"), "escape")
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "bootstrapRuntime": {"file": "../escape.js", "hash": "bad", "size": 6}
  },
  "islands": [],
  "css": []
}`)

	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || len(report.Unresolved) == 0 || !strings.Contains(report.Unresolved[0].Reason, "not found") {
		t.Fatalf("unexpected unsafe path evidence: %#v", report)
	}
}

func TestBuildSizeEvidenceRecordsNoncanonicalSymlinkEscapedAsset(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "outside.js"), "escape")
	runtimeDir := filepath.Join(dir, "assets", "runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "outside.js"), filepath.Join(runtimeDir, "escape.js")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "bootstrapRuntime": {"file": "escape.js", "hash": "bad", "size": 6}
  },
  "islands": [],
  "css": []
}`)

	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || len(report.Unresolved) == 0 {
		t.Fatalf("expected unresolved symlink evidence, got %#v", report)
	}
}

func TestWriteNewJSONFileRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := WriteNewJSONFile(path, map[string]string{"first": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewJSONFile(path, map[string]string{"second": "true"}); err == nil {
		t.Fatal("expected overwrite refusal")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "second") {
		t.Fatalf("overwrite changed evidence: %s", body)
	}
}

func TestBuildSizeEvidenceRejectsInventoryOverlayMismatch(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "build.json"), `{"runtime":{},"islands":[],"css":[]}`)
	writeCanonicalExportSkeleton(t, dir)
	repoRoot, err := resolveRepoRootForEvidence(".")
	if err != nil {
		t.Fatal(err)
	}
	current, err := Collect(context.Background(), CollectOptions{RepoRoot: repoRoot, ArtifactRoot: t.TempDir(), Canopy: false, Git: true})
	if err != nil {
		t.Fatal(err)
	}
	current.OverlayHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	current.Overlay.Hash = current.OverlayHash
	inventoryPath := filepath.Join(t.TempDir(), "source-inventory.json")
	data, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, inventoryPath, string(data))

	tmpParent := filepath.Join(repoRoot, "tmp")
	if err := os.MkdirAll(tmpParent, 0o755); err != nil {
		t.Fatal(err)
	}
	artifactRoot, err := os.MkdirTemp(tmpParent, "size-evidence-mismatch-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(artifactRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(artifactRoot) })

	_, err = BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath:  filepath.Join(dir, "build.json"),
		DistDir:       dir,
		RepoRoot:      ".",
		ArtifactRoot:  artifactRoot,
		InventoryPath: inventoryPath,
		Canonical:     true,
	})
	if err == nil || !(strings.Contains(err.Error(), "source identity mismatch") || strings.Contains(err.Error(), "manifest overlayHash mismatch")) {
		t.Fatalf("expected source identity mismatch, got %v", err)
	}
}

func TestMaterializeOverlayInputsRewritesReplayableContainedRefs(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	artifactRoot := filepath.Join(root, "build", "ouroboros", "materialized")
	if err := materializeOverlayInputs(root, filepath.Join(artifactRoot, "source"), inv); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(inv.Overlay.PatchPath, "build/ouroboros/materialized/source/") {
		t.Fatalf("patch path was not materialized under artifact root: %s", inv.Overlay.PatchPath)
	}
	if !strings.HasPrefix(inv.Overlay.ArchivePath, "build/ouroboros/materialized/source/") {
		t.Fatalf("archive path was not materialized under artifact root: %s", inv.Overlay.ArchivePath)
	}
	if _, err := ReplayInventoryReconstruction(context.Background(), root, inv); err != nil {
		t.Fatalf("materialized inventory is not replayable: %v", err)
	}
}

func TestSourceIdentityHandoffPredictsMaterializedInventoryWithoutCreatingRoot(t *testing.T) {
	repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
	futureRoot := filepath.Join(repoRoot, "build", "future-browser-out")
	if err := os.RemoveAll(futureRoot); err != nil {
		t.Fatal(err)
	}
	handoff, err := BuildSourceIdentityHandoff(context.Background(), repoRoot, inventoryPath, futureRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(futureRoot); !os.IsNotExist(err) {
		t.Fatalf("source identity handoff created future root: %v", err)
	}
	plan, err := PredictCanonicalInventoryMaterialization(context.Background(), repoRoot, inventoryPath, futureRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSourceIdentityHandoffForMaterialization(handoff, plan); err != nil {
		t.Fatalf("handoff does not bind predicted materialization: %v", err)
	}
	if handoff.ArtifactRoot != plan.ArtifactRoot || handoff.InventoryRef != CanonicalSourceInventoryRef || handoff.Source.InventorySHA256 != plan.SHA256 {
		t.Fatalf("handoff did not record canonical root/ref/sha: %#v plan=%#v", handoff, plan)
	}
	if handoff.Source.TrackedDiffHash != OverlayClean || handoff.Source.UntrackedIncludedSourceHash != OverlayClean {
		t.Fatalf("clean handoff did not normalize clean hashes: %#v", handoff.Source)
	}
	handoffPath := filepath.Join(t.TempDir(), "source-identity.json")
	if err := WriteNewJSONFile(handoffPath, handoff); err != nil {
		t.Fatal(err)
	}
	roundtrip, err := ReadSourceIdentityHandoffStrict(handoffPath)
	if err != nil {
		t.Fatalf("strict read rejected clean handoff: %v", err)
	}
	if roundtrip.Source.TrackedDiffHash != OverlayClean || roundtrip.Source.UntrackedIncludedSourceHash != OverlayClean {
		t.Fatalf("strict clean handoff roundtrip changed clean hashes: %#v", roundtrip.Source)
	}
}

func TestMaterializeCanonicalInventoryWritesPredictedBytes(t *testing.T) {
	repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
	materializedRoot := filepath.Join(repoRoot, "build", "materialized-browser-out")
	if err := os.RemoveAll(materializedRoot); err != nil {
		t.Fatal(err)
	}
	plan, err := PredictCanonicalInventoryMaterialization(context.Background(), repoRoot, inventoryPath, materializedRoot)
	if err != nil {
		t.Fatal(err)
	}
	gotPath, err := MaterializeCanonicalInventory(context.Background(), repoRoot, inventoryPath, materializedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != plan.Path {
		t.Fatalf("materialized path = %s, want %s", gotPath, plan.Path)
	}
	body, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, plan.Bytes) {
		t.Fatalf("materialized bytes differ from predicted bytes")
	}
}

func TestWriteCanonicalInventoryFileWritesPredictedBytesAndRejectsPrettyDrift(t *testing.T) {
	repoRoot, inventoryPath := writeReplayableCanonicalInventory(t)
	materializedRoot := filepath.Join(repoRoot, "build", "canonical-inventory-writer")
	plan, err := PredictCanonicalInventoryMaterialization(context.Background(), repoRoot, inventoryPath, materializedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteCanonicalInventoryFile(plan.Path, plan.Inventory); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(plan.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, plan.Bytes) {
		t.Fatalf("canonical inventory writer bytes differ from predicted materialization")
	}
	if bytes.Contains(body, []byte("\n  \"")) || bytes.HasSuffix(body, []byte("\n")) {
		t.Fatalf("canonical inventory writer emitted pretty formatting")
	}
	if err := WriteJSONFile(plan.Path, plan.Inventory); err != nil {
		t.Fatal(err)
	}
	pretty, err := os.ReadFile(plan.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(pretty, plan.Bytes) {
		t.Fatalf("pretty inventory unexpectedly matched canonical bytes")
	}
	if err := validateReusableMaterializedInventory(context.Background(), repoRoot, plan.Path, plan.Bytes); err == nil {
		t.Fatalf("pretty inventory formatting drift was accepted")
	}
}

func TestDirtySourceIdentityHandoffPredictsMaterializedOverlayBytes(t *testing.T) {
	repoRoot, inventoryPath := writeDirtyReplayableCanonicalInventory(t)
	materializedRoot := filepath.Join(repoRoot, "build", "dirty-materialized-browser-out")
	plan, err := PredictCanonicalInventoryMaterialization(context.Background(), repoRoot, inventoryPath, materializedRoot)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := BuildSourceIdentityHandoff(context.Background(), repoRoot, inventoryPath, materializedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Source.TrackedDiffHash == OverlayClean || handoff.Source.UntrackedIncludedSourceHash == OverlayClean {
		t.Fatalf("dirty handoff lost dirty hashes: %#v", handoff.Source)
	}
	if err := ValidateSourceIdentityHandoffForMaterialization(handoff, plan); err != nil {
		t.Fatal(err)
	}
	gotPath, err := MaterializeCanonicalInventory(context.Background(), repoRoot, inventoryPath, materializedRoot)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, plan.Bytes) {
		t.Fatalf("dirty materialized bytes differ from predicted bytes")
	}
	if !strings.Contains(plan.Inventory.Overlay.PatchPath, "build/dirty-materialized-browser-out/source/tracked-overlay.patch") {
		t.Fatalf("dirty patch ref was not rewritten for future root: %s", plan.Inventory.Overlay.PatchPath)
	}
	if !strings.Contains(plan.Inventory.Overlay.ArchivePath, "build/dirty-materialized-browser-out/source/untracked-sources") {
		t.Fatalf("dirty archive ref was not rewritten for future root: %s", plan.Inventory.Overlay.ArchivePath)
	}
	if _, err := os.Stat(filepath.Join(materializedRoot, "source", "tracked-overlay.patch")); err != nil {
		t.Fatalf("materialized tracked patch missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(materializedRoot, "source", "untracked-sources")); err != nil {
		t.Fatalf("materialized untracked archive missing: %v", err)
	}
}

func writeReplayableCanonicalInventory(t *testing.T) (repoRoot, inventoryPath string) {
	return writeReplayableCanonicalInventoryWithDirtyOverlay(t, false)
}

func writeDirtyReplayableCanonicalInventory(t *testing.T) (repoRoot, inventoryPath string) {
	return writeReplayableCanonicalInventoryWithDirtyOverlay(t, true)
}

func writeReplayableCanonicalInventoryWithDirtyOverlay(t *testing.T, dirty bool) (repoRoot, inventoryPath string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitOutput(t, root, "rev-parse", "HEAD"))
	if dirty {
		writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\nwindow.__gosx_dirty = true;\n")
		writeFile(t, root, "client/js/bootstrap-src/05-extra.js", "window.__gosx_extra = true;\n")
	}
	overlay, err := BuildOverlayEvidence(context.Background(), root, base)
	if err != nil {
		t.Fatalf("BuildOverlayEvidence: %v", err)
	}
	artifactRoot := filepath.Join(root, "perf", "ouroboros")
	if dirty {
		if err := WriteOverlayArtifacts(context.Background(), root, artifactRoot, overlay); err != nil {
			t.Fatalf("WriteOverlayArtifacts: %v", err)
		}
		overlay.PatchPath = filepath.ToSlash(filepath.Join("perf", "ouroboros", "tracked-overlay.patch"))
		overlay.ArchivePath = filepath.ToSlash(filepath.Join("perf", "ouroboros", "untracked-sources"))
	}
	inv := &Inventory{
		BaseRevision: base,
		OverlayHash:  overlay.Hash,
		Overlay:      overlay,
	}
	inv.SchemaVersion = SchemaVersion
	inv.Contract = ContractO02
	inv.Initiative = Initiative
	inv.Spec = Spec
	inv.CorpusID = CorpusID
	inv.ArtifactRoot = artifactRoot
	inv.Scope = DefaultScope()
	inv.Files = FileInventory{
		Included: []SourceFile{},
		Sidecars: []SourceFile{},
		Embedded: []SourceFile{},
		Excluded: []ExcludedFile{},
		Audit:    []ExcludedFile{},
	}
	inv.Totals = Totals{ByExtension: map[string]int{}}
	inv.Structural = Structural{Gotreesitter: ParseSummary{Language: "javascript"}}
	inv.Surface = Surface{
		GosxNames:               []GosxName{},
		BroaderBrowserGosxNames: []GosxName{},
		SerializationSites:      []SerializationSite{},
	}
	fresh, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect fixture inventory: %v", err)
	}
	inv.Surface.CompatibilityAudit = fresh.Surface.CompatibilityAudit
	inv.Ratchets = []ScopeRatchet{{ID: "test", Scope: "test", Status: "pass", Definition: "test"}}
	inv.Manifest = minimalReplayableCorpusManifest(inv)
	inventoryPath = filepath.Join(t.TempDir(), "source-inventory.json")
	if err := WriteJSONFile(inventoryPath, inv); err != nil {
		t.Fatal(err)
	}
	return root, inventoryPath
}

func minimalReplayableCorpusManifest(inv *Inventory) CorpusManifest {
	size := int64(1)
	route := func(id, current, future string) FixtureRoute {
		return FixtureRoute{ID: id, Route: "/" + strings.ToLower(id), FixtureApp: "fixtures", Purpose: "test", ExpectedRuntime: "test", ExpectedTinyGoCurrent: current, ExpectedTinyGoFuture: future}
	}
	ref := func(id, kind string) *ArtifactRef {
		return &ArtifactRef{SchemaVersion: "gosx.ouroboros.artifact-ref.v1", Path: kind + "/" + id + ".json", BaseRevision: inv.BaseRevision, OverlayHash: inv.OverlayHash, SHA256: "sha256:" + strings.Repeat("a", 64)}
	}
	return CorpusManifest{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		BaseRevision:  inv.BaseRevision,
		OverlayHash:   inv.OverlayHash,
		ArtifactRoot:  inv.ArtifactRoot,
		Scope:         inv.Scope,
		FixtureRoutes: []FixtureRoute{
			route("R00", "runtime", "core"),
			route("R01", "islands", "engine"),
			route("R02", "none", "collab"),
			route("R03", "none", "full"),
		},
		Variants: []RuntimeVariant{
			{ID: "runtime", Generation: "current", Status: "measured", SizeBytes: &size, BudgetBytes: &size, SizeArtifact: ref("runtime", "size"), WASMArtifact: ref("runtime", "wasm"), SelectedByRoutes: []string{"R00"}},
			{ID: "islands", Generation: "current", Status: "measured", SizeBytes: &size, BudgetBytes: &size, SizeArtifact: ref("islands", "size"), WASMArtifact: ref("islands", "wasm"), SelectedByRoutes: []string{"R01"}},
			{ID: "core", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R00"}},
			{ID: "engine", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R01"}},
			{ID: "collab", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R02"}},
			{ID: "full", Generation: "future", Status: "planned", SelectedByRoutes: []string{"R03"}},
		},
	}
}

func TestReadSourceIdentityHandoffStrictRejectsTampering(t *testing.T) {
	dir := t.TempDir()
	valid := SourceIdentityHandoff{
		SchemaVersion: SourceIdentityHandoffSchemaVersion,
		Contract:      ContractO02,
		ArtifactRoot:  filepath.Join(dir, "out"),
		InventoryRef:  CanonicalSourceInventoryRef,
		Source: SourceIdentityHandoffSource{
			BaseRevision:                "abc1234",
			OverlayHash:                 OverlayClean,
			TrackedDiffHash:             sha256String(""),
			UntrackedIncludedSourceHash: sha256String(""),
			InventoryRef:                CanonicalSourceInventoryRef,
			InventorySHA256:             sha256String("inventory"),
		},
	}
	path := filepath.Join(dir, "source-identity.json")
	if err := WriteNewJSONFile(path, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSourceIdentityHandoffStrict(path); err != nil {
		t.Fatalf("valid handoff rejected: %v", err)
	}
	handoffJSON := func(source SourceIdentityHandoffSource) string {
		t.Helper()
		body, err := json.Marshal(SourceIdentityHandoff{
			SchemaVersion: SourceIdentityHandoffSchemaVersion,
			Contract:      ContractO02,
			ArtifactRoot:  filepath.Join(dir, "case-out"),
			InventoryRef:  CanonicalSourceInventoryRef,
			Source:        source,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "unknown", body: `{"schemaVersion":"gosx.ouroboros.source-identity.v1","contractVersion":"O0.2","artifactRoot":"/tmp/out","inventoryRef":"source/source-inventory.json","source":{"baseRevision":"abc1234","overlayHash":"sha256:clean","trackedDiffHash":"sha256:` + strings.Repeat("a", 64) + `","untrackedIncludedSourceHash":"sha256:` + strings.Repeat("b", 64) + `","inventoryRef":"source/source-inventory.json","inventorySha256":"sha256:` + strings.Repeat("c", 64) + `"},"unknown":true}`, want: "unknown"},
		{name: "trailing", body: `{"schemaVersion":"gosx.ouroboros.source-identity.v1","contractVersion":"O0.2","artifactRoot":"/tmp/out","inventoryRef":"source/source-inventory.json","source":{"baseRevision":"abc1234","overlayHash":"sha256:clean","trackedDiffHash":"sha256:` + strings.Repeat("a", 64) + `","untrackedIncludedSourceHash":"sha256:` + strings.Repeat("b", 64) + `","inventoryRef":"source/source-inventory.json","inventorySha256":"sha256:` + strings.Repeat("c", 64) + `"}} {}`, want: "trailing"},
		{name: "clean-hashes", body: handoffJSON(SourceIdentityHandoffSource{BaseRevision: "abc1234", OverlayHash: OverlayClean, TrackedDiffHash: OverlayClean, UntrackedIncludedSourceHash: OverlayClean, InventoryRef: CanonicalSourceInventoryRef, InventorySHA256: "sha256:" + strings.Repeat("c", 64)}), want: ""},
		{name: "dirty-hashes", body: handoffJSON(SourceIdentityHandoffSource{BaseRevision: "abc1234", OverlayHash: "sha256:" + strings.Repeat("d", 64), TrackedDiffHash: "sha256:" + strings.Repeat("a", 64), UntrackedIncludedSourceHash: "sha256:" + strings.Repeat("b", 64), InventoryRef: CanonicalSourceInventoryRef, InventorySHA256: "sha256:" + strings.Repeat("c", 64)}), want: ""},
		{name: "malformed-hash", body: handoffJSON(SourceIdentityHandoffSource{BaseRevision: "abc1234", OverlayHash: OverlayClean, TrackedDiffHash: "bad", UntrackedIncludedSourceHash: "sha256:" + strings.Repeat("b", 64), InventoryRef: CanonicalSourceInventoryRef, InventorySHA256: "sha256:" + strings.Repeat("c", 64)}), want: "trackedDiffHash"},
		{name: "clean-inventory-sha", body: handoffJSON(SourceIdentityHandoffSource{BaseRevision: "abc1234", OverlayHash: OverlayClean, TrackedDiffHash: OverlayClean, UntrackedIncludedSourceHash: OverlayClean, InventoryRef: CanonicalSourceInventoryRef, InventorySHA256: OverlayClean}), want: "inventorySha256"},
		{name: "oversize", body: strings.Repeat(" ", MaxSourceIdentityHandoffBytes+1), want: "exceeds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(tampered, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := ReadSourceIdentityHandoffStrict(tampered)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ReadSourceIdentityHandoffStrict rejected %s: %v", tc.name, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ReadSourceIdentityHandoffStrict error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBuildSizeEvidenceCanonicalSourceIdentityIncludesStaticAndCompatibility(t *testing.T) {
	repoRoot, inventoryPath, artifactRoot := writeCurrentCanonicalInventory(t)
	dist := writeCanonicalSizeDist(t)
	report, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath:  filepath.Join(dist, "build.json"),
		DistDir:       dist,
		RepoRoot:      repoRoot,
		ArtifactRoot:  artifactRoot,
		InventoryPath: inventoryPath,
		Canonical:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalSourceIdentity(t, report.Source)
	if _, err := os.Stat(filepath.Join(artifactRoot, "perf", "runtime-json-static.jsonl")); err != nil {
		t.Fatalf("runtime JSON static evidence was not written: %v", err)
	}
}

func TestCanonicalSourceIdentityRejectsMissingRuntimeJSONBindings(t *testing.T) {
	err := requireRuntimeJSONStaticCompatibilityMatch(RuntimeJSONStaticIdentity{
		SourceIdentityHash: "sha256:source",
		SemanticHash:       "sha256:semantic",
		CountsHash:         "sha256:counts",
		GlobalNameHash:     "sha256:globals",
	}, &CompatibilityAuditIdentity{SchemaVersion: compatibilityAuditSchemaVersion, Status: "pass", CanonicalAvailable: true})
	if err == nil || !strings.Contains(err.Error(), "missing runtime JSON bindings") {
		t.Fatalf("missing compatibility bindings error = %v", err)
	}
	err = requireRuntimeJSONStaticCompatibilityMatch(RuntimeJSONStaticIdentity{
		SourceIdentityHash: "sha256:source",
		SemanticHash:       "sha256:semantic",
		CountsHash:         "sha256:counts",
		GlobalNameHash:     "sha256:globals",
	}, &CompatibilityAuditIdentity{
		SchemaVersion:                 compatibilityAuditSchemaVersion,
		Status:                        "pass",
		CanonicalAvailable:            true,
		RuntimeJSONSourceIdentityHash: "sha256:source",
		RuntimeJSONSemanticHash:       "sha256:tamper",
		RuntimeJSONCountsHash:         "sha256:counts",
		RuntimeJSONGlobalNameHash:     "sha256:globals",
	})
	if err == nil || !strings.Contains(err.Error(), "runtimeJSONSemanticHash") {
		t.Fatalf("tampered compatibility binding error = %v", err)
	}
}

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeCanonicalExportSkeleton(t *testing.T, dir string) {
	t.Helper()
	type route struct {
		Path   string `json:"path"`
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	}
	raw := struct {
		Routes           []route `json:"routes"`
		ResourceManifest string  `json:"resourceManifest"`
	}{ResourceManifest: CanonicalResourceManifestRef}
	for _, item := range canonicalExportEvidenceRoutesForTest() {
		routePath := item.Path
		file := item.File
		body := `<script src="/gosx/bootstrap-runtime.js"></script>`
		if routePath == "/static" {
			body = `<html>SSR only</html>`
		}
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(file)), body)
		raw.Routes = append(raw.Routes, route{Path: routePath, File: file, SHA256: sha256String(body), Bytes: int64(len(body))})
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "export.json"), string(data))
	writeResourceManifestFixture(t, dir, nil, nil)
}

func canonicalExportEvidenceRoutesForTest() []exportEvidenceRoute {
	ids := canonicalRouteIDs()
	routes := make([]exportEvidenceRoute, 0, len(ids))
	for _, id := range ids {
		routePath := canonicalOuroborosRoutePath(id)
		routes = append(routes, exportEvidenceRoute{
			Path:   routePath,
			File:   canonicalOuroborosRouteFile(routePath),
			SHA256: sha256String(routePath),
			Bytes:  int64Ptr(int64(len(routePath))),
		})
	}
	return routes
}

func int64Ptr(value int64) *int64 {
	return &value
}

func writeCurrentCanonicalInventory(t *testing.T) (repoRoot, inventoryPath, artifactRoot string) {
	t.Helper()
	root, err := resolveRepoRootForEvidence(".")
	if err != nil {
		t.Fatal(err)
	}
	currentOverlay, err := BuildOverlayEvidence(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	if currentOverlay.Hash == OverlayClean {
		artifactRoot = filepath.Join(t.TempDir(), "size-evidence-canonical")
	} else {
		tmpParent := filepath.Join(root, "tmp")
		if err := os.MkdirAll(tmpParent, 0o755); err != nil {
			t.Fatal(err)
		}
		artifactRoot, err = os.MkdirTemp(tmpParent, "size-evidence-canonical-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(artifactRoot) })
	}
	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, ArtifactRoot: artifactRoot, Canopy: false, Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if inv.OverlayHash != OverlayClean {
		overlayRoot := filepath.Join(artifactRoot, "source-input")
		if err := WriteOverlayArtifacts(context.Background(), root, overlayRoot, inv.Overlay); err != nil {
			t.Fatal(err)
		}
		inv.Overlay.PatchPath = relTo(root, filepath.Join(overlayRoot, "tracked-overlay.patch"))
		if len(inv.Overlay.UntrackedSources) > 0 {
			inv.Overlay.ArchivePath = relTo(root, filepath.Join(overlayRoot, "untracked-sources"))
		}
	}
	inventoryPath = filepath.Join(artifactRoot, "source-inventory.json")
	if err := WriteJSONFile(inventoryPath, inv); err != nil {
		t.Fatal(err)
	}
	return root, inventoryPath, artifactRoot
}

func writeCanonicalSizeDist(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "assets", "runtime", "bootstrap-runtime.1111.js"), "shared-runtime")
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "bootstrapRuntime": {"file": "bootstrap-runtime.1111.js", "hash": "1111", "size": 14}
  },
  "islands": [],
  "css": []
}`)
	writeCanonicalExportSkeleton(t, dir)
	return dir
}

func assertCanonicalSourceIdentity(t *testing.T, source SourceIdentity) {
	t.Helper()
	if !source.StrictInventory || !source.CurrentOverlayVerified || !source.ReconstructionProof {
		t.Fatalf("source identity proof incomplete: %#v", source)
	}
	if source.RuntimeJSONStatic == nil || !source.RuntimeJSONStatic.Validated {
		t.Fatalf("runtime JSON static identity missing: %#v", source.RuntimeJSONStatic)
	}
	if source.CompatibilityAudit == nil {
		t.Fatalf("compatibility audit identity missing")
	}
	if source.CompatibilityAudit.RuntimeJSONSourceIdentityHash != source.RuntimeJSONStatic.SourceIdentityHash ||
		source.CompatibilityAudit.RuntimeJSONSemanticHash != source.RuntimeJSONStatic.SemanticHash ||
		source.CompatibilityAudit.RuntimeJSONCountsHash != source.RuntimeJSONStatic.CountsHash ||
		source.CompatibilityAudit.RuntimeJSONGlobalNameHash != source.RuntimeJSONStatic.GlobalNameHash {
		t.Fatalf("compatibility audit does not bind static identity: static=%#v audit=%#v", source.RuntimeJSONStatic, source.CompatibilityAudit)
	}
}

func routeEvidenceByPath(routes []RouteAssetEvidence) map[string]RouteAssetEvidence {
	out := map[string]RouteAssetEvidence{}
	for _, route := range routes {
		out[route.Route] = route
	}
	return out
}

func findAssetByURL(assets []TransferredAsset, url string) *TransferredAsset {
	for i := range assets {
		if assets[i].URL == url {
			return &assets[i]
		}
	}
	return nil
}

func shaHex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}
