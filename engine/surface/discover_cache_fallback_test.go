package surface

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestDiscoverPreservesPriorEntryOnBuildFailure ensures that when a build
// fails on the next Discover pass, the previously written manifest entry
// (with valid WasmPath/WasmURL/Hash) is preserved and marked stale rather
// than clobbered with empty strings. Closes defect 2.
func TestDiscoverPreservesPriorEntryOnBuildFailure(t *testing.T) {
	cacheDir := t.TempDir()

	// Seed a prior good manifest entry and the cached WASM file it references.
	const hash = "abc12345abc12345"
	cachedWasm := filepath.Join(cacheDir, hash+".wasm")
	if err := os.WriteFile(cachedWasm, []byte("\x00asm\x01\x00\x00\x00"), 0o644); err != nil {
		t.Fatalf("seed cached wasm: %v", err)
	}
	seedManifest(t, cacheDir, "Graph", cachedWasm, "/gosx/engines/Graph.abc12345.wasm", hash)

	// Run a discovery pass that simulates a build failure for "Graph".
	// We do not walk a real .gsx tree; we exercise the manifest-merge path
	// directly via the test-only helper.
	syntheticErr := errors.New("synthetic")
	mergeManifestForTest(t, cacheDir, "Graph", surfaceManifestEntry{
		Component:    "Graph",
		WASMPath:     "",
		WASMURL:      "",
		Hash:         "",
		Capabilities: []string{"canvas"},
	}, syntheticErr)

	// Read the manifest and verify the prior entry survived with Stale + LastBuildError.
	got := readManifest(t, cacheDir)
	entry := got.byComponent("Graph")
	if entry == nil {
		t.Fatal("Graph entry missing from manifest")
	}
	if entry.WASMURL != "/gosx/engines/Graph.abc12345.wasm" {
		t.Errorf("WasmURL = %q, want preserved /gosx/engines/Graph.abc12345.wasm", entry.WASMURL)
	}
	if entry.Hash != hash {
		t.Errorf("Hash = %q, want preserved %q", entry.Hash, hash)
	}
	if !entry.Stale {
		t.Errorf("Stale = false, want true")
	}
	if entry.LastBuildError == "" {
		t.Errorf("LastBuildError empty, want %q", syntheticErr.Error())
	}
}

// TestDiscoverDoesNotPreserveEntryWhenCachedFileMissing ensures the
// stale-fallback path verifies the cached .wasm still exists. If a user has
// wiped the cache directory but the manifest still mentions it, we cannot
// reuse it — we must let the empty entry win and surface the failure.
func TestDiscoverDoesNotPreserveEntryWhenCachedFileMissing(t *testing.T) {
	cacheDir := t.TempDir()
	// Seed a manifest entry whose WasmPath points at a file we never create.
	seedManifest(t, cacheDir, "Graph", filepath.Join(cacheDir, "missing.wasm"),
		"/gosx/engines/Graph.deadbeef.wasm", "deadbeef")

	syntheticErr := errors.New("synthetic")
	mergeManifestForTest(t, cacheDir, "Graph", surfaceManifestEntry{
		Component: "Graph",
	}, syntheticErr)

	got := readManifest(t, cacheDir)
	entry := got.byComponent("Graph")
	if entry == nil {
		t.Fatal("Graph entry missing")
	}
	// The cached file is gone — we must NOT keep the WasmURL because the route
	// would 404 anyway.
	if entry.WASMURL != "" {
		t.Errorf("WasmURL = %q, want empty (cached file missing)", entry.WASMURL)
	}
	if entry.LastBuildError == "" {
		t.Errorf("LastBuildError empty, want %q", syntheticErr.Error())
	}
}

// TestManifestVersionWritten asserts that the manifest grows a version field
// per spec Q-B so future readers can discriminate.
func TestManifestVersionWritten(t *testing.T) {
	cacheDir := t.TempDir()
	mergeManifestForTest(t, cacheDir, "Graph", surfaceManifestEntry{
		Component: "Graph",
		WASMURL:   "/gosx/engines/Graph.beef.wasm",
		Hash:      "beef",
	}, nil)

	raw, err := os.ReadFile(filepath.Join(cacheDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if probe.Version != 1 {
		t.Errorf("manifest version = %d, want 1", probe.Version)
	}
}

// seedManifest writes a manifest.json containing a single entry for component
// pointing at the given wasm file.
func seedManifest(t *testing.T, cacheDir, component, wasmPath, wasmURL, hash string) {
	t.Helper()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	m := surfaceManifest{
		Version: 1,
		Surfaces: []surfaceManifestEntry{{
			Component: component,
			WASMPath:  wasmPath,
			WASMURL:   wasmURL,
			Hash:      hash,
		}},
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}
}

// readManifest decodes manifest.json under cacheDir.
func readManifest(t *testing.T, cacheDir string) *surfaceManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cacheDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m surfaceManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return &m
}

// byComponent returns a pointer to the manifest entry for the named
// component, or nil if absent. The pointer aliases the slice element so
// tests can read its post-merge fields.
func (m *surfaceManifest) byComponent(name string) *surfaceManifestEntry {
	for i := range m.Surfaces {
		if m.Surfaces[i].Component == name {
			return &m.Surfaces[i]
		}
	}
	return nil
}
