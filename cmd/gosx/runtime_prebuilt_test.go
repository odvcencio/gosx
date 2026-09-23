package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/ouroboros"
)

// fakeRuntimeArtifactFetcher is the "fake artifact store" resolution,
// verification, and caching tests exercise instead of a real network fetch.
// It records every requested (tag, name) pair so a test can prove a cache
// hit made zero further calls.
type fakeRuntimeArtifactFetcher struct {
	assets map[string][]byte // key: tag + "/" + name
	calls  []string
	err    error
}

func (f *fakeRuntimeArtifactFetcher) FetchAsset(_ context.Context, tag, name string) ([]byte, error) {
	key := tag + "/" + name
	f.calls = append(f.calls, key)
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.assets[key]
	if !ok {
		return nil, errors.New("fake fetcher: no asset for " + key)
	}
	// Return a copy: callers must never be able to corrupt the fixture by
	// mutating a returned slice in place.
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildFakeRuntimeRelease constructs a self-consistent manifest + asset set
// (every SHA256/Bytes value in the manifest genuinely matches the paired
// asset bytes) for tag, the same shape the release workflow's
// build-runtime --ouroboros-out step already produces as
// wasm/runtime-artifacts.json.
func buildFakeRuntimeRelease(tag string) *fakeRuntimeArtifactFetcher {
	variants := []struct{ id, file string }{
		{"core", "gosx-runtime-core.wasm"},
		{"engine", "gosx-runtime-engine.wasm"},
		{"collab", "gosx-runtime-collab.wasm"},
		{"full", "gosx-runtime.wasm"},
		{"islands", "gosx-runtime-islands.wasm"},
	}
	shimData := []byte("tinygo wasm_exec.js fixture for " + tag)
	shim := ouroboros.AssetMetrics{
		File:   runtimeManifestWASMExecName,
		SHA256: sha256Hex(shimData),
		Bytes:  int64(len(shimData)),
	}

	evidence := ouroboros.RuntimeBuildEvidence{
		SchemaVersion: ouroboros.SchemaVersion,
		Contract:      ouroboros.ContractO02,
		Variants:      make([]ouroboros.RuntimeArtifactVariant, 0, len(variants)),
	}
	assets := map[string][]byte{
		tag + "/" + runtimeManifestWASMExecName: shimData,
	}
	for _, v := range variants {
		data := []byte("wasm bytes for " + v.id + " @ " + tag)
		assets[tag+"/"+v.file] = data
		evidence.Variants = append(evidence.Variants, ouroboros.RuntimeArtifactVariant{
			ID:     v.id,
			Status: "measured",
			File:   v.file,
			SHA256: sha256Hex(data),
			Bytes:  int64(len(data)),
			Shim:   &shim,
		})
	}
	manifestData, err := json.Marshal(evidence)
	if err != nil {
		panic(err)
	}
	assets[tag+"/"+runtimeManifestAssetName] = manifestData
	return &fakeRuntimeArtifactFetcher{assets: assets}
}

func TestResolvePrebuiltRuntimeFetchesVerifiesAndCaches(t *testing.T) {
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	cacheRoot := t.TempDir()

	bundle, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher, "v1.2.3")
	if err != nil {
		t.Fatalf("resolvePrebuiltRuntime: %v", err)
	}
	if len(bundle.Evidence.Variants) != 5 {
		t.Fatalf("expected 5 variants, got %d", len(bundle.Evidence.Variants))
	}
	firstFetchCalls := len(fetcher.calls)
	if firstFetchCalls == 0 {
		t.Fatal("expected the fake fetcher to be called on a cold cache")
	}
	for _, v := range bundle.Evidence.Variants {
		path, meta, ok := bundle.VariantPath(v.ID)
		if !ok {
			t.Fatalf("variant %s not resolvable from bundle", v.ID)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read staged variant %s: %v", v.ID, err)
		}
		if sha256Hex(data) != meta.SHA256 {
			t.Fatalf("staged variant %s hash mismatch", v.ID)
		}
	}
	if _, err := os.ReadFile(bundle.WASMExecPath()); err != nil {
		t.Fatalf("read staged wasm_exec.js: %v", err)
	}

	// Second resolution against the same cache root must be a pure cache
	// hit: no further fetcher calls.
	fetcher2 := buildFakeRuntimeRelease("v1.2.3")
	bundle2, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher2, "v1.2.3")
	if err != nil {
		t.Fatalf("resolvePrebuiltRuntime (cache hit): %v", err)
	}
	if len(fetcher2.calls) != 0 {
		t.Fatalf("expected zero fetcher calls on cache hit, got %d: %v", len(fetcher2.calls), fetcher2.calls)
	}
	if bundle2.Dir != bundle.Dir {
		t.Fatalf("cache hit dir = %q, want %q", bundle2.Dir, bundle.Dir)
	}
}

func TestResolvePrebuiltRuntimeRejectsTamperedAssetDifferentLength(t *testing.T) {
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	// Corrupt the "core" variant bytes so they no longer match the manifest
	// hash (or size) the same fetcher response advertises.
	fetcher.assets["v1.2.3/gosx-runtime-core.wasm"] = []byte("tampered bytes")

	cacheRoot := t.TempDir()
	_, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher, "v1.2.3")
	if err == nil {
		t.Fatal("expected a hash-mismatched asset to fail verification")
	}
	if !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
	// A failed verification must never leave a manifest file behind for a
	// later call to mistake for a complete, verified cache entry.
	if _, statErr := os.Stat(filepath.Join(cacheRoot, "v1.2.3", runtimeManifestAssetName)); statErr == nil {
		t.Fatal("tampered fetch must not publish a cache manifest")
	}
}

func TestResolvePrebuiltRuntimeRejectsTamperedAssetSameLength(t *testing.T) {
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	original := fetcher.assets["v1.2.3/gosx-runtime-core.wasm"]
	tampered := make([]byte, len(original))
	copy(tampered, original)
	tampered[0] ^= 0xFF // flip a bit, keep the same length
	fetcher.assets["v1.2.3/gosx-runtime-core.wasm"] = tampered

	_, err := resolvePrebuiltRuntime(context.Background(), t.TempDir(), fetcher, "v1.2.3")
	if err == nil {
		t.Fatal("expected a hash-mismatched asset to fail verification")
	}
	if !strings.Contains(err.Error(), "integrity verification") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolvePrebuiltRuntimeRefetchesWhenCacheIsCorrupted(t *testing.T) {
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	cacheRoot := t.TempDir()

	if _, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher, "v1.2.3"); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}

	// Corrupt one cached file on disk directly, simulating bit rot or a
	// tampered local cache. A cache "hit" must re-verify, not trust
	// presence alone.
	corrupted := filepath.Join(cacheRoot, "v1.2.3", "gosx-runtime-core.wasm")
	if err := os.WriteFile(corrupted, []byte("corrupted on disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	fetcher2 := buildFakeRuntimeRelease("v1.2.3")
	bundle, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher2, "v1.2.3")
	if err != nil {
		t.Fatalf("resolve after corruption: %v", err)
	}
	if len(fetcher2.calls) == 0 {
		t.Fatal("expected corrupted cache entry to trigger a re-fetch")
	}
	path, meta, ok := bundle.VariantPath("core")
	if !ok {
		t.Fatal("core variant missing after refetch")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256Hex(data) != meta.SHA256 {
		t.Fatal("refetched core variant still does not match manifest hash")
	}
}

func TestResolvePrebuiltRuntimePropagatesFetchError(t *testing.T) {
	fetcher := &fakeRuntimeArtifactFetcher{err: errors.New("network unreachable")}
	_, err := resolvePrebuiltRuntime(context.Background(), t.TempDir(), fetcher, "v1.2.3")
	if err == nil || !strings.Contains(err.Error(), "network unreachable") {
		t.Fatalf("expected fetch error to propagate, got %v", err)
	}
}

// TestValidatePrebuiltRuntimeFileNameRejectsUnsafeOrUnknownNames pins
// validatePrebuiltRuntimeFileName's two independent checks: no path
// separator or ".." segment (regardless of whether the result would
// technically resolve inside the cache dir), and membership in the fixed
// runtimeBuildTargets() file list. A manifest-supplied variant File is
// untrusted input; either check failing must reject the name before it is
// ever joined onto a directory on disk.
func TestValidatePrebuiltRuntimeFileNameRejectsUnsafeOrUnknownNames(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		wantErr bool
	}{
		{"known core variant", "gosx-runtime-core.wasm", false},
		{"known full variant", "gosx-runtime.wasm", false},
		{"known islands variant", "gosx-runtime-islands.wasm", false},
		{"empty", "", true},
		{"parent traversal", "../../../../etc/cron.d/evil", true},
		{"embedded traversal", "gosx-runtime-core.wasm/../../evil", true},
		{"unix path separator", "sub/gosx-runtime-core.wasm", true},
		{"windows path separator", `sub\gosx-runtime-core.wasm`, true},
		{"absolute path", "/etc/passwd", true},
		{"bare dotdot", "..", true},
		{"unknown but otherwise safe name", "gosx-runtime-unknown.wasm", true},
		{"manifest asset name itself, not a variant", runtimeManifestAssetName, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePrebuiltRuntimeFileName(c.file)
			if c.wantErr && err == nil {
				t.Fatalf("validatePrebuiltRuntimeFileName(%q): expected an error, got nil", c.file)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("validatePrebuiltRuntimeFileName(%q): unexpected error: %v", c.file, err)
			}
		})
	}
}

// TestResolvePrebuiltRuntimeRejectsPathTraversalVariantFileName is a
// regression test: resolvePrebuiltRuntime used to join a manifest's
// variant.File onto the cache directory (both to fetch-and-write and, via
// loadVerifiedPrebuiltRuntimeCache, to re-read) with no validation. A
// manifest naming a variant's File with a ".." segment could make that join
// escape the cache directory entirely — writing a fetched, attacker- (or
// compromised-release-host-) controlled payload to an arbitrary path on
// disk. This proves such a manifest is rejected outright, and that nothing
// escapes the cache root in the process.
func TestResolvePrebuiltRuntimeRejectsPathTraversalVariantFileName(t *testing.T) {
	fetcher := buildFakeRuntimeRelease("v1.2.3")

	var evidence ouroboros.RuntimeBuildEvidence
	if err := json.Unmarshal(fetcher.assets["v1.2.3/"+runtimeManifestAssetName], &evidence); err != nil {
		t.Fatal(err)
	}
	const escapePayload = "planted-by-a-malicious-or-compromised-release-host"
	for i := range evidence.Variants {
		if evidence.Variants[i].ID != "core" {
			continue
		}
		evidence.Variants[i].File = "../../escaped-gosx-runtime-core.wasm"
		evidence.Variants[i].SHA256 = sha256Hex([]byte(escapePayload))
		evidence.Variants[i].Bytes = int64(len(escapePayload))
	}
	manifestData, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	fetcher.assets["v1.2.3/"+runtimeManifestAssetName] = manifestData
	fetcher.assets["v1.2.3/../../escaped-gosx-runtime-core.wasm"] = []byte(escapePayload)

	cacheRoot := t.TempDir()
	outsideMarker := filepath.Join(filepath.Dir(cacheRoot), "escaped-gosx-runtime-core.wasm")
	defer os.Remove(outsideMarker)

	_, err = resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher, "v1.2.3")
	if err == nil {
		t.Fatal("expected a path-traversal variant file name to be rejected")
	}
	if !strings.Contains(err.Error(), "not a known runtime variant file") && !strings.Contains(err.Error(), "path separator") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(outsideMarker); statErr == nil {
		t.Fatal("path-traversal variant file name must not write outside the cache root")
	}
	if _, statErr := os.Stat(filepath.Join(cacheRoot, "v1.2.3", runtimeManifestAssetName)); statErr == nil {
		t.Fatal("a rejected manifest must not publish a cache manifest")
	}
}

// TestLoadVerifiedPrebuiltRuntimeCacheRejectsPathTraversalVariantFileName
// covers the read-side counterpart: a cache directory whose on-disk
// manifest.json has been tampered with (or was written by an older,
// unvalidated version of resolvePrebuiltRuntime) to name a variant File
// with a ".." segment must not be read via that traversal path either; the
// cache entry is rejected as a miss, the same as any other corrupted entry.
func TestLoadVerifiedPrebuiltRuntimeCacheRejectsPathTraversalVariantFileName(t *testing.T) {
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	cacheRoot := t.TempDir()
	if _, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher, "v1.2.3"); err != nil {
		t.Fatalf("initial resolve: %v", err)
	}

	dir := filepath.Join(cacheRoot, "v1.2.3")
	manifestData, err := os.ReadFile(filepath.Join(dir, runtimeManifestAssetName))
	if err != nil {
		t.Fatal(err)
	}
	var evidence ouroboros.RuntimeBuildEvidence
	if err := json.Unmarshal(manifestData, &evidence); err != nil {
		t.Fatal(err)
	}
	// The planted "outside" file's bytes are made to match the tampered
	// manifest's OWN recorded hash for the core variant, not the real core
	// wasm bytes. This is deliberate: it isolates the file-name validation
	// as the only thing standing between "cache hit" and "cache miss" here.
	// Without it, verifyRuntimeAssetDigest's pre-existing hash check would
	// independently reject a mismatched payload and this test would still
	// pass even if the path-traversal validation this item adds were
	// missing entirely — proving nothing about the fix.
	outsideSecret := filepath.Join(filepath.Dir(dir), "outside-secret.txt")
	outsideBytes := []byte("outside the version cache dir")
	if err := os.WriteFile(outsideSecret, outsideBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outsideSecret)

	for i := range evidence.Variants {
		if evidence.Variants[i].ID == "core" {
			evidence.Variants[i].File = "../outside-secret.txt"
			evidence.Variants[i].SHA256 = sha256Hex(outsideBytes)
			evidence.Variants[i].Bytes = int64(len(outsideBytes))
		}
	}
	tampered, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runtimeManifestAssetName), tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := loadVerifiedPrebuiltRuntimeCache(dir); ok {
		t.Fatal("expected a path-traversal variant file name to make the cache entry a miss, even though the traversed file's hash matches what the tampered manifest claims")
	}
}

func TestLooksLikeStableGoSXReleaseVersion(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"v0.56.9", true},
		{"v1.0.0", true},
		{"v0.56.09", false}, // leading zero
		{"v0.0.0-20240101000000-abcdef012345", false}, // pseudo-version
		{"v0.56.9-beta.1", false},                     // prerelease suffix
		{"0.56.9", false},                             // missing v prefix
		{"", false},
	}
	for _, c := range cases {
		if got := looksLikeStableGoSXReleaseVersion(c.version); got != c.want {
			t.Errorf("looksLikeStableGoSXReleaseVersion(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}

// stubResolveProjectGoSXVersion overrides resolveProjectGoSXVersionFunc for
// the duration of a test, restoring it on cleanup.
func stubResolveProjectGoSXVersion(t *testing.T, version string, hasLocalReplace, ok bool) {
	t.Helper()
	original := resolveProjectGoSXVersionFunc
	resolveProjectGoSXVersionFunc = func(string) (string, bool, bool) {
		return version, hasLocalReplace, ok
	}
	t.Cleanup(func() { resolveProjectGoSXVersionFunc = original })
}

func lookPathAlwaysFails(string) (string, error) {
	return "", errors.New("tinygo not found")
}

func lookPathAlwaysSucceeds(name string) (string, error) {
	return "/usr/local/bin/" + name, nil
}

func TestResolveWASMCompilerForProjectPrefersTinyGoWhenAvailable(t *testing.T) {
	original := newDefaultRuntimeArtifactFetcher
	called := false
	newDefaultRuntimeArtifactFetcher = func(string, string) runtimeArtifactFetcher {
		called = true
		return &fakeRuntimeArtifactFetcher{}
	}
	t.Cleanup(func() { newDefaultRuntimeArtifactFetcher = original })

	compiler, path, bundle, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), lookPathAlwaysSucceeds)
	if err != nil {
		t.Fatalf("resolveWASMCompilerForProject: %v", err)
	}
	if compiler != wasmCompilerTinyGo {
		t.Fatalf("compiler = %q, want TinyGo", compiler)
	}
	if path == "" {
		t.Fatal("expected a tinygo path")
	}
	if bundle != nil {
		t.Fatal("expected no prebuilt bundle when TinyGo is available")
	}
	if called {
		t.Fatal("must not attempt a prebuilt fetch when TinyGo is on PATH")
	}
}

func TestResolveWASMCompilerForProjectFallsBackToPrebuiltWhenTinyGoMissing(t *testing.T) {
	stubResolveProjectGoSXVersion(t, "v1.2.3", false, true)

	fetcher := buildFakeRuntimeRelease("v1.2.3")
	original := newDefaultRuntimeArtifactFetcher
	newDefaultRuntimeArtifactFetcher = func(owner, repo string) runtimeArtifactFetcher { return fetcher }
	t.Cleanup(func() { newDefaultRuntimeArtifactFetcher = original })

	t.Setenv(runtimeCacheEnv, t.TempDir())

	compiler, path, bundle, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), lookPathAlwaysFails)
	if err != nil {
		t.Fatalf("resolveWASMCompilerForProject: %v", err)
	}
	if compiler != wasmCompilerPrebuilt {
		t.Fatalf("compiler = %q, want Prebuilt", compiler)
	}
	if bundle == nil {
		t.Fatal("expected a resolved prebuilt bundle")
	}
	if path != bundle.Dir {
		t.Fatalf("path = %q, want bundle dir %q", path, bundle.Dir)
	}
	if len(fetcher.calls) == 0 {
		t.Fatal("expected the fake fetcher to be used")
	}
}

func TestResolveWASMCompilerForProjectModeTinyGoNeverFallsBack(t *testing.T) {
	t.Setenv(runtimeModeEnv, "tinygo")
	original := newDefaultRuntimeArtifactFetcher
	called := false
	newDefaultRuntimeArtifactFetcher = func(string, string) runtimeArtifactFetcher {
		called = true
		return &fakeRuntimeArtifactFetcher{}
	}
	t.Cleanup(func() { newDefaultRuntimeArtifactFetcher = original })

	_, _, _, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), lookPathAlwaysFails)
	if err == nil {
		t.Fatal("expected an error when TinyGo is missing and mode=tinygo")
	}
	if called {
		t.Fatal("mode=tinygo must never attempt a prebuilt fetch")
	}
	if !strings.Contains(err.Error(), "TinyGo") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveWASMCompilerForProjectRejectsOfflineOnCacheMiss(t *testing.T) {
	stubResolveProjectGoSXVersion(t, "v1.2.3", false, true)
	t.Setenv(runtimeCacheEnv, t.TempDir())
	_, _, _, err := resolveWASMCompilerForProject(BuildOptions{Offline: true}, t.TempDir(), lookPathAlwaysFails)
	if err == nil || !strings.Contains(err.Error(), "--offline") {
		t.Fatalf("expected an --offline error, got %v", err)
	}
}

func TestResolveWASMCompilerForProjectOfflineSucceedsOnCacheHit(t *testing.T) {
	stubResolveProjectGoSXVersion(t, "v1.2.3", false, true)
	cacheRoot := t.TempDir()
	t.Setenv(runtimeCacheEnv, cacheRoot)

	// Populate the cache with a real online fetch first...
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	if _, err := resolvePrebuiltRuntime(context.Background(), cacheRoot, fetcher, "v1.2.3"); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	// ...then prove --offline reuses that verified cache entry without ever
	// touching newDefaultRuntimeArtifactFetcher (which would try a real
	// network call if invoked).
	original := newDefaultRuntimeArtifactFetcher
	newDefaultRuntimeArtifactFetcher = func(string, string) runtimeArtifactFetcher {
		t.Fatal("must not construct a network fetcher for an offline cache hit")
		return nil
	}
	t.Cleanup(func() { newDefaultRuntimeArtifactFetcher = original })

	compiler, _, bundle, err := resolveWASMCompilerForProject(BuildOptions{Offline: true}, t.TempDir(), lookPathAlwaysFails)
	if err != nil {
		t.Fatalf("resolveWASMCompilerForProject (offline cache hit): %v", err)
	}
	if compiler != wasmCompilerPrebuilt || bundle == nil {
		t.Fatalf("expected a prebuilt compiler and bundle, got %q %#v", compiler, bundle)
	}
}

func TestResolveWASMCompilerForProjectRejectsLocalReplaceDevBuild(t *testing.T) {
	stubResolveProjectGoSXVersion(t, "", true, true)
	_, _, _, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), lookPathAlwaysFails)
	if err == nil || !strings.Contains(err.Error(), "local source build") {
		t.Fatalf("expected a local-source-build error, got %v", err)
	}
}

func TestResolveWASMCompilerForProjectRejectsPseudoVersion(t *testing.T) {
	stubResolveProjectGoSXVersion(t, "v0.0.0-20240101000000-abcdef012345", false, true)
	_, _, _, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), lookPathAlwaysFails)
	if err == nil || !strings.Contains(err.Error(), "not a stable release version") {
		t.Fatalf("expected a not-a-stable-release error, got %v", err)
	}
}

func TestResolveWASMCompilerForProjectRejectsUnknownMode(t *testing.T) {
	t.Setenv(runtimeModeEnv, "bogus")
	_, _, _, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), lookPathAlwaysFails)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected an unknown-mode error, got %v", err)
	}
}

// stripTinyGoFromPATH rebuilds PATH from the current process's real PATH
// directories, minus any entry literally named "tinygo", so every other
// tool a production build shells out to (go, and anything else already on
// PATH) stays resolvable while TinyGo specifically is not. t.Setenv restores
// the original PATH when the test ends.
func stripTinyGoFromPATH(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "tinygo" || seen[name] {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.IsDir() {
				continue
			}
			if err := os.Symlink(filepath.Join(dir, name), filepath.Join(binDir, name)); err == nil {
				seen[name] = true
			}
		}
	}
	t.Setenv("PATH", binDir)
	if _, err := exec.LookPath("tinygo"); err == nil {
		t.Fatal("PATH stripping failed: tinygo is still resolvable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("PATH stripping broke the go toolchain: %v", err)
	}
}

// TestRunBuildProdSucceedsWithoutTinyGoUsingPrebuiltRuntime is the
// consumer-build proof the prebuilt runtime mechanism exists for: strip
// TinyGo off PATH entirely (simulating chitin-choir/kiln-style consumers
// with no TinyGo toolchain installed) and confirm `gosx build` still
// produces a complete, correctly hashed dist/ output by resolving a
// release-matched prebuilt runtime instead. The fetcher is a fake in-memory
// store (no network); resolveProjectGoSXVersionFunc is stubbed to a stable
// release version because the fixture project's go.mod itself still points
// at local GoSX source (addLocalGoSXReplace) so `go list`/`go build` resolve
// hermetically -- this test's job is to prove the WASM-build stage's
// prebuilt path end to end, not to prove real network module resolution
// (that is exercised in isolation by the resolveWASMCompilerForProject unit
// tests above).
func TestRunBuildProdSucceedsWithoutTinyGoUsingPrebuiltRuntime(t *testing.T) {
	if raceDetectorEnabled {
		t.Skip("shells out to go list/go build subprocesses; race instrumentation adds no value")
	}

	dir := filepath.Join(t.TempDir(), "prebuilt-consumer-app")
	if err := RunInit(dir, "example.com/prebuilt-consumer-app", ""); err != nil {
		t.Fatal(err)
	}
	addLocalGoSXReplace(t, dir)
	tidyModule(t, dir)

	stubResolveProjectGoSXVersion(t, "v1.2.3", false, true)
	fetcher := buildFakeRuntimeRelease("v1.2.3")
	originalFetcherFactory := newDefaultRuntimeArtifactFetcher
	newDefaultRuntimeArtifactFetcher = func(string, string) runtimeArtifactFetcher { return fetcher }
	t.Cleanup(func() { newDefaultRuntimeArtifactFetcher = originalFetcherFactory })
	t.Setenv(runtimeCacheEnv, t.TempDir())

	stripTinyGoFromPATH(t)

	if err := RunBuildWithOptions(dir, BuildOptions{Dev: false}); err != nil {
		t.Fatalf("RunBuildWithOptions with no TinyGo on PATH: %v", err)
	}

	manifestData, err := os.ReadFile(filepath.Join(dir, "dist", "build.json"))
	if err != nil {
		t.Fatalf("read build manifest: %v", err)
	}
	var manifest BuildManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("decode build manifest: %v", err)
	}
	if manifest.Runtime.WASM.File == "" || manifest.Runtime.WASMExec.File == "" {
		t.Fatalf("expected runtime WASM/wasm_exec assets in manifest, got %#v", manifest.Runtime)
	}

	wasmData, err := os.ReadFile(filepath.Join(dir, "dist", "assets", "runtime", manifest.Runtime.WASM.File))
	if err != nil {
		t.Fatalf("read published full runtime wasm: %v", err)
	}
	if !strings.Contains(string(wasmData), "wasm bytes for full @ v1.2.3") {
		t.Fatalf("published runtime wasm does not trace back to the fake prebuilt release: %q", wasmData)
	}
	execData, err := os.ReadFile(filepath.Join(dir, "dist", "assets", "runtime", manifest.Runtime.WASMExec.File))
	if err != nil {
		t.Fatalf("read published wasm_exec.js: %v", err)
	}
	if !strings.Contains(string(execData), "tinygo wasm_exec.js fixture for v1.2.3") {
		t.Fatalf("published wasm_exec.js does not trace back to the fake prebuilt release: %q", execData)
	}
	if len(fetcher.calls) == 0 {
		t.Fatal("expected the fake prebuilt fetcher to be used")
	}
}
