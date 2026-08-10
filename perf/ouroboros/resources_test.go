package ouroboros

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDecodeResourceManifestStrictRejectsUnknownTrailingBoundsAndRouteOrder(t *testing.T) {
	dir := t.TempDir()
	writeResourceManifestFixture(t, dir, nil, nil)
	body := readTestFile(t, filepath.Join(dir, CanonicalResourceManifestRef))

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	raw["unknown"] = true
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
	if _, err := DecodeResourceManifestStrict(strings.NewReader(string(body) + " {}")); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing JSON error = %v", err)
	}
	raw = decodeResourceManifestMap(t, body)
	raw["resources"] = make([]any, maxResourceManifestResources+1)
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "bounds") {
		t.Fatalf("bounds error = %v", err)
	}
	raw = decodeResourceManifestMap(t, body)
	routes := raw["routes"].([]any)
	routes[0], routes[1] = routes[1], routes[0]
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "route[0]") {
		t.Fatalf("route order error = %v", err)
	}
}

func TestDecodeResourceManifestStrictRejectsNonCanonicalHashesAndResourceOrder(t *testing.T) {
	dir := t.TempDir()
	resA := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
	resB := testResource(t, dir, "res-b", "/resources/b.bin", "_ouroboros/resources/b.bin", "b")
	resA.UsedByRoutes = []string{"R10"}
	resB.UsedByRoutes = []string{"R10"}
	writeResourceManifestFixture(t, dir, []ResourceManifestResource{resA, resB}, nil)
	body := readTestFile(t, filepath.Join(dir, CanonicalResourceManifestRef))

	raw := decodeResourceManifestMap(t, body)
	resources := raw["resources"].([]any)
	first := resources[0].(map[string]any)
	first["sha256"] = strings.TrimPrefix(first["sha256"].(string), "sha256:")
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "invalid metrics") {
		t.Fatalf("bare hash error = %v", err)
	}

	raw = decodeResourceManifestMap(t, body)
	resources = raw["resources"].([]any)
	first = resources[0].(map[string]any)
	first["sha256"] = strings.ToUpper(first["sha256"].(string))
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "invalid metrics") {
		t.Fatalf("uppercase hash error = %v", err)
	}

	raw = decodeResourceManifestMap(t, body)
	resources = raw["resources"].([]any)
	resources[0], resources[1] = resources[1], resources[0]
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "sorted by url then id") {
		t.Fatalf("resource order error = %v", err)
	}
}

func TestDecodeResourceManifestStrictRejectsEncodedTraversalAndFiniteDynamics(t *testing.T) {
	dir := t.TempDir()
	res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
	res.UsedByRoutes = []string{"R10"}
	writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
	body := readTestFile(t, filepath.Join(dir, CanonicalResourceManifestRef))

	raw := decodeResourceManifestMap(t, body)
	resources := raw["resources"].([]any)
	resources[0].(map[string]any)["url"] = "/%2e%2e/secret.js"
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "percent encoding") {
		t.Fatalf("encoded traversal resource URL error = %v", err)
	}

	raw = decodeResourceManifestMap(t, body)
	resources = raw["resources"].([]any)
	resources[0].(map[string]any)["aliases"] = []any{"/%2e%2e/secret.js"}
	if _, err := DecodeResourceManifestStrict(strings.NewReader(mustJSON(t, raw))); err == nil || !strings.Contains(err.Error(), "percent encoding") {
		t.Fatalf("encoded traversal alias error = %v", err)
	}

	dynamics := []ResourceManifestDynamic{{
		ID: "dyn-r10-model", RouteID: "R10", Route: "/demos/water", Kind: "api", URL: "/water/model.glb", Producer: "test",
	}}
	writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, dynamics)
	body = readTestFile(t, filepath.Join(dir, CanonicalResourceManifestRef))
	if _, err := DecodeResourceManifestStrict(strings.NewReader(string(body))); err == nil || !strings.Contains(err.Error(), "finite resource") {
		t.Fatalf("finite dynamic URL error = %v", err)
	}
}

func TestLoadResourceManifestRejectsTraversalSymlinkTamperMissingAndExtra(t *testing.T) {
	t.Run("traversal", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
		res.OutputPath = "../escape.bin"
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "path is unsafe") {
			t.Fatalf("traversal error = %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		if err := osRemoveAndSymlink(filepath.Join(dir, "outside.bin"), filepath.Join(dir, res.OutputPath)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlink error = %v", err)
		}
	})
	t.Run("tamper", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
		res.Bytes++
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "metrics mismatch") {
			t.Fatalf("tamper error = %v", err)
		}
	})
	t.Run("same_path_file_tamper", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		writeTestFile(t, filepath.Join(dir, res.OutputPath), "different")
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "metrics mismatch") {
			t.Fatalf("same path tamper error = %v", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		removeTestFile(t, filepath.Join(dir, res.OutputPath))
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "missing file") {
			t.Fatalf("missing error = %v", err)
		}
	})
	t.Run("extra", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/a.bin", "_ouroboros/resources/a.bin", "a")
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		writeTestFile(t, filepath.Join(dir, "_ouroboros", "resources", "extra.bin"), "extra")
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "extra resource file") {
			t.Fatalf("extra error = %v", err)
		}
	})
	t.Run("extra_nested_producer_root", func(t *testing.T) {
		dir := t.TempDir()
		res := testResource(t, dir, "res-a", "/resources/nested/a.bin", "_ouroboros/resources/nested/a.bin", "a")
		writeResourceManifestFixture(t, dir, []ResourceManifestResource{res}, nil)
		writeTestFile(t, filepath.Join(dir, "_ouroboros", "resources", "extra.bin"), "extra")
		if _, err := LoadAndValidateResourceManifest(dir, CanonicalResourceManifestRef, true); err == nil || !strings.Contains(err.Error(), "extra resource file") {
			t.Fatalf("nested extra error = %v", err)
		}
	})
}

func TestBuildSizeEvidenceUsesResourceManifestParentsDynamicsAndDeterministicReplay(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "build.json"), `{"runtime":{},"islands":[],"css":[]}`)
	writeCanonicalExportWithResourceManifest(t, dir)

	parent := testResource(t, dir, "res-a-parent", "/resources/a.glb", "_ouroboros/resources/a.glb", "parent-glb")
	child := testResource(t, dir, "res-b-child", "/resources/b.bin", "_ouroboros/resources/b.bin", "child-buffer")
	parent.UsedByRoutes = []string{"R10"}
	child.Parents = []string{parent.ID}
	child.UsedByRoutes = []string{"R10"}
	dynamics := []ResourceManifestDynamic{{
		ID: "dyn-r10-water", RouteID: "R10", Route: "/demos/water", Kind: "api", URL: "/api/water", Producer: "test",
	}}
	writeResourceManifestFixture(t, dir, []ResourceManifestResource{parent, child}, dynamics)

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
	replayed, err := BuildSizeEvidenceWithOptions(SizeEvidenceOptions{
		ManifestPath: filepath.Join(dir, "build.json"),
		DistDir:      dir,
		RepoRoot:     ".",
		ArtifactRoot: t.TempDir(),
		Canonical:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.BuildInput.ResourceManifestSHA256 == "" || report.ResourceManifestPath == "" {
		t.Fatalf("resource manifest identity missing: %#v", report.BuildInput)
	}
	if !sameSizeEvidenceAssets(report.Assets, replayed.Assets) || !sameSizeEvidenceRoutes(report.Routes, replayed.Routes) || report.Totals != replayed.Totals {
		t.Fatal("resource size evidence replay is not deterministic")
	}
	routes := routeEvidenceByPath(report.Routes)
	water := routes["/demos/water"]
	if got, want := water.RawBytes, int64(len("parent-glb")+len("child-buffer")); got != want {
		t.Fatalf("water raw bytes = %d, want %d: %#v", got, want, water)
	}
	if stringSliceContains(water.AssetIDs, dynamics[0].ID) {
		t.Fatalf("dynamic endpoint was counted as asset: %#v", water.AssetIDs)
	}
	if len(report.Notes) == 0 || !strings.Contains(strings.Join(report.Notes, " "), "dynamic endpoints") {
		t.Fatalf("missing dynamic endpoint note: %#v", report.Notes)
	}
}

func writeCanonicalExportWithResourceManifest(t *testing.T, dir string) {
	t.Helper()
	type route struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	raw := struct {
		Routes           []route `json:"routes"`
		ResourceManifest string  `json:"resourceManifest"`
	}{ResourceManifest: CanonicalResourceManifestRef}
	for _, id := range canonicalRouteIDs() {
		routePath := canonicalOuroborosRoutePath(id)
		file := strings.TrimPrefix(routePath, "/") + "/index.html"
		raw.Routes = append(raw.Routes, route{Path: routePath, File: file})
		body := `<script src="/gosx/bootstrap-runtime.js"></script>`
		if id == "R00" {
			body = `<html>SSR only</html>`
		}
		writeTestFile(t, filepath.Join(dir, "static", filepath.FromSlash(file)), body)
	}
	writeTestFile(t, filepath.Join(dir, "export.json"), mustJSON(t, raw))
}

func writeResourceManifestFixture(t *testing.T, dir string, resources []ResourceManifestResource, dynamics []ResourceManifestDynamic) {
	t.Helper()
	sortResourcesForTest(resources)
	manifest := ResourceManifest{
		SchemaVersion:    ResourceManifestSchemaVersion,
		Contract:         ContractO02,
		CorpusID:         CorpusID,
		Resources:        resources,
		Routes:           make([]ResourceManifestRoute, 0, len(canonicalRouteIDs())),
		DynamicEndpoints: dynamics,
	}
	for _, id := range canonicalRouteIDs() {
		row := ResourceManifestRoute{ID: id, Route: canonicalOuroborosRoutePath(id)}
		for _, res := range resources {
			if stringSliceContains(res.UsedByRoutes, id) && len(res.Parents) == 0 {
				row.Resources = append(row.Resources, res.ID)
			}
		}
		sort.Strings(row.Resources)
		manifest.Routes = append(manifest.Routes, row)
	}
	writeTestFile(t, filepath.Join(dir, CanonicalResourceManifestRef), mustJSON(t, manifest))
}

func testResource(t *testing.T, dir, id, url, outputPath, content string) ResourceManifestResource {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, outputPath), content)
	data := []byte(content)
	sum := sha256.Sum256(data)
	return ResourceManifestResource{
		ID:          id,
		URL:         url,
		OutputPath:  outputPath,
		Producer:    "test",
		Kind:        "asset",
		Source:      "fixture",
		ContentType: "application/octet-stream",
		SHA256:      "sha256:" + hex.EncodeToString(sum[:]),
		Bytes:       int64(len(data)),
		GzipBytes:   GzipLength(data),
		BrotliBytes: BrotliLength(data),
	}
}

func sortResourcesForTest(resources []ResourceManifestResource) {
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].URL == resources[j].URL {
			return resources[i].ID < resources[j].ID
		}
		return resources[i].URL < resources[j].URL
	})
}

func decodeResourceManifestMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func removeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func osRemoveAndSymlink(target, link string) error {
	if err := os.WriteFile(target, []byte("outside"), 0644); err != nil {
		return err
	}
	if err := os.Remove(link); err != nil {
		return err
	}
	return os.Symlink(target, link)
}
