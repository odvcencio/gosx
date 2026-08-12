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
	writeTestFile(t, filepath.Join(dir, "build.json"), `{
  "runtime": {
    "wasm": {"file": "gosx-runtime.4444.wasm", "hash": "4444", "size": 9},
    "wasmIslands": {"file": "gosx-runtime-islands.5555.wasm", "hash": "5555", "size": 12},
    "wasmExec": {"file": "wasm_exec.6666.js", "hash": "6666", "size": 4},
    "bootstrapRuntime": {"file": "bootstrap-runtime.1111.js", "hash": "1111", "size": 14},
    "bootstrapFeatureIslands": {"file": "bootstrap-feature-islands.2222.js", "hash": "2222", "size": 15},
    "bootstrapFeatureEngines": {"file": "bootstrap-feature-engines.3333.js", "hash": "3333", "size": 14}
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
	writeTestFile(t, filepath.Join(dir, "static", "island", "counter", "index.html"), `<script src="/gosx/bootstrap-runtime.js"></script><script src="/gosx/bootstrap-feature-islands.js"></script>`)
	writeTestFile(t, filepath.Join(dir, "static", "canvas-board", "index.html"), `<script src="/gosx/bootstrap-runtime.js"></script><script src="/gosx/bootstrap-feature-engines.js"></script>`)

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
	if report.Totals.AssetCount != 6 {
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
	for _, route := range report.Routes {
		if route.SharedRawBytes != int64(len("shared-runtime")) {
			t.Fatalf("route %s shared bytes = %d", route.Route, route.SharedRawBytes)
		}
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
	if len(assets) != 0 || len(unresolved) != 1 || !strings.Contains(unresolved[0].Reason, "build manifest hash") {
		t.Fatalf("canonical direct asset bypass was not rejected: assets=%#v unresolved=%#v", assets, unresolved)
	}
	assets, unresolved, err = collectTransferredAssets(dir, &buildmanifest.Manifest{}, refs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || len(unresolved) != 0 {
		t.Fatalf("noncanonical direct asset did not retain reduced behavior: assets=%#v unresolved=%#v", assets, unresolved)
	}
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

func TestValidateExportHTMLAttributionAllowsR00OnlyWithoutRefs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "static", "index.html"), `<html>SSR only</html>`)
	if err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{{Path: "/static", File: "static/index.html"}}}); err != nil {
		t.Fatalf("R00 without runtime refs failed: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "lite", "index.html"), `<html>missing runtime refs</html>`)
	err := validateExportHTMLAttribution(dir, exportEvidence{routes: []exportEvidenceRoute{{Path: "/lite", File: "lite/index.html"}}})
	if err == nil || !strings.Contains(err.Error(), "incomplete asset attribution") {
		t.Fatalf("non-R00 without runtime refs error = %v", err)
	}
}

func TestAttributeRoutesStrictRejectsMissingUnsafeAndIncompleteHTML(t *testing.T) {
	dir := t.TempDir()
	report := &SizeEvidence{DistDir: dir}
	err := attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{{Path: "/missing", File: "missing.html"}}}, nil, true)
	if err == nil || !strings.Contains(err.Error(), "missing or unsafe route file") {
		t.Fatalf("attributeRoutes missing HTML error = %v", err)
	}
	report = &SizeEvidence{DistDir: dir}
	err = attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{{Path: "/unsafe", File: "../outside.html"}}}, nil, true)
	if err == nil || !strings.Contains(err.Error(), "missing or unsafe route file") {
		t.Fatalf("attributeRoutes unsafe HTML error = %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "static", "empty", "index.html"), `<html></html>`)
	report = &SizeEvidence{DistDir: dir}
	err = attributeRoutes(report, exportEvidence{routes: []exportEvidenceRoute{{Path: "/empty", File: "empty/index.html"}}}, nil, true)
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

func TestNormalizePortableInventoryMetadataRemovesHostRootIdentity(t *testing.T) {
	left := minimalValidInventory()
	right := minimalValidInventory()
	left.ArtifactRoot = "/home/runner/work/gosx/evidence"
	left.Manifest.ArtifactRoot = left.ArtifactRoot
	right.ArtifactRoot = `C:\\work\\gosx\\evidence`
	right.Manifest.ArtifactRoot = right.ArtifactRoot

	normalizePortableInventoryMetadata(left)
	normalizePortableInventoryMetadata(right)
	leftBody, err := canonicalInventoryJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	rightBody, err := canonicalInventoryJSON(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftBody, rightBody) {
		t.Fatalf("portable inventory identity still depends on host root\nleft: %s\nright: %s", leftBody, rightBody)
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
	paths := []string{
		"/static",
		"/lite",
		"/island/counter",
		"/islands/kitchen",
		"/action/form",
		"/canvas-board",
		"/hub/echo",
		"/video-sync",
		"/scene/basic",
		"/navigation/a",
		"/navigation/b",
		"/demos/water",
	}
	type route struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	raw := struct {
		Routes []route `json:"routes"`
	}{}
	for _, routePath := range paths {
		file := strings.TrimPrefix(routePath, "/") + "/index.html"
		raw.Routes = append(raw.Routes, route{Path: routePath, File: file})
		body := `<script src="/gosx/bootstrap-runtime.js"></script>`
		if routePath == "/static" {
			body = `<html>SSR only</html>`
		}
		writeTestFile(t, filepath.Join(dir, "static", filepath.FromSlash(file)), body)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "export.json"), string(data))
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
