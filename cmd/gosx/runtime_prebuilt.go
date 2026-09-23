package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"m31labs.dev/gosx/perf/ouroboros"
)

// wasmCompilerPrebuilt marks a production WASM build staged from a
// release-matched prebuilt runtime instead of a local TinyGo invocation.
// resolveWASMCompilerForProject is the only place that returns it; every
// other resolveWASMCompiler caller (gosx build-runtime's own self-build) is
// unaffected and still requires a real TinyGo toolchain.
const wasmCompilerPrebuilt wasmCompiler = "Prebuilt"

// runtimeModeEnv selects how a production `gosx build` resolves its WASM
// compiler:
//   - unset or "auto" (default): prefer TinyGo on PATH; when TinyGo is
//     absent, fall back to a release-matched prebuilt runtime.
//   - "tinygo": require TinyGo; never attempt a prebuilt fetch.
//   - "prebuilt": skip the TinyGo probe and require a prebuilt runtime.
const runtimeModeEnv = "GOSX_RUNTIME_MODE"

// runtimeCacheEnv overrides the on-disk directory prebuilt runtime downloads
// verify into and are cached under. Empty uses os.UserCacheDir()/gosx/runtime.
const runtimeCacheEnv = "GOSX_RUNTIME_CACHE"

// runtimeReleaseRepoEnv overrides the GitHub "owner/repo" prebuilt runtime
// release assets publish from. Empty uses defaultRuntimeReleaseOwner/Repo.
// A fork that republishes its own GoSX releases under a different repository
// sets this so its projects resolve prebuilt runtimes from that fork instead
// of upstream.
const runtimeReleaseRepoEnv = "GOSX_RUNTIME_RELEASE_REPO"

const (
	defaultRuntimeReleaseOwner = "odvcencio"
	defaultRuntimeReleaseRepo  = "gosx"
)

// Fixed asset names the release workflow's runtime-publishing step uploads
// alongside the per-variant WASM files (whose names come from the manifest
// itself; see runtimeBuildTargets in runtimepaths.go).
const (
	runtimeManifestAssetName    = "gosx-runtime-artifacts.json"
	runtimeManifestWASMExecName = "gosx-runtime-wasm_exec.js"
)

// maxRuntimeAssetBytes bounds a single fetched release asset. The largest
// real artifact (the "full" runtime variant) is a few megabytes after
// wasm-opt -Oz; this ceiling exists to stop a misbehaving or compromised
// server from streaming unbounded data into the cache, not to accommodate
// legitimate growth headroom.
const maxRuntimeAssetBytes = 64 << 20 // 64 MiB

// releaseVersionPattern matches a canonical stable release tag/module
// version this CLI's own release-gate enforces (scripts/check-release-tag-
// grammar.sh): "v" + MAJOR.MINOR.PATCH, no leading zeroes, no pseudo-version
// or prerelease suffix. Only versions in this shape have a published
// prebuilt runtime; anything else (a pseudo-version from an untagged commit,
// a local go.mod replace, an empty result) means the project is pinned to
// unreleased source and must build the runtime from source.
var releaseVersionPattern = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

func looksLikeStableGoSXReleaseVersion(version string) bool {
	return releaseVersionPattern.MatchString(strings.TrimSpace(version))
}

// runtimeArtifactFetcher retrieves one named release asset (a WASM variant,
// the wasm_exec.js shim, or the integrity manifest) for a specific GoSX
// release tag. The production implementation issues one HTTPS GET per asset
// against GitHub's release-download URL; tests substitute an in-memory fake
// so resolution, verification, and caching are provable without a network.
type runtimeArtifactFetcher interface {
	FetchAsset(ctx context.Context, tag, name string) ([]byte, error)
}

// newDefaultRuntimeArtifactFetcher constructs the real, network-backed
// fetcher. It is a package variable so tests can substitute a fake fetcher
// factory and exercise the full resolveWASMCompilerForProject → doBuild path
// with no TinyGo and no network access.
var newDefaultRuntimeArtifactFetcher = func(owner, repo string) runtimeArtifactFetcher {
	return &httpReleaseRuntimeFetcher{Owner: owner, Repo: repo, Client: &http.Client{Timeout: 30 * time.Second}}
}

type httpReleaseRuntimeFetcher struct {
	Owner  string
	Repo   string
	Client *http.Client
}

func (f *httpReleaseRuntimeFetcher) FetchAsset(ctx context.Context, tag, name string) ([]byte, error) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", f.Owner, f.Repo, tag, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRuntimeAssetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if len(data) > maxRuntimeAssetBytes {
		return nil, fmt.Errorf("fetch %s: exceeds %d byte limit", url, maxRuntimeAssetBytes)
	}
	return data, nil
}

// prebuiltRuntimeBundle is a verified, on-disk set of prebuilt runtime
// artifacts for one released m31labs.dev/gosx version, plus the build
// evidence manifest that named and hashed them.
type prebuiltRuntimeBundle struct {
	Dir      string
	Evidence ouroboros.RuntimeBuildEvidence
}

// VariantPath returns the cached file path and manifest record for the
// variant with the given id ("core", "engine", "collab", "full", "islands"
// — the same ids runtimeBuildTargets in runtimepaths.go assigns).
func (b prebuiltRuntimeBundle) VariantPath(id string) (string, ouroboros.RuntimeArtifactVariant, bool) {
	for _, v := range b.Evidence.Variants {
		if v.ID == id && v.Status == "measured" {
			return filepath.Join(b.Dir, v.File), v, true
		}
	}
	return "", ouroboros.RuntimeArtifactVariant{}, false
}

// WASMExecPath returns the cached path of the TinyGo wasm_exec.js shim the
// bundle's variants were built against.
func (b prebuiltRuntimeBundle) WASMExecPath() string {
	return filepath.Join(b.Dir, runtimeManifestWASMExecName)
}

// resolveWASMCompilerForProject is the consumer entry point production
// `gosx build` uses to pick a WASM compiler. It never changes behavior for a
// project that already has TinyGo on PATH (mode "auto", the default, tries
// TinyGo first) or that opts into TinyGo explicitly. It only engages the
// prebuilt runtime path when TinyGo is unavailable, the project is pinned to
// a real released m31labs.dev/gosx version (not a local replace or
// unreleased pseudo-version), and --offline was not requested.
func resolveWASMCompilerForProject(opts BuildOptions, projectDir string, lookPath func(string) (string, error)) (wasmCompiler, string, *prebuiltRuntimeBundle, error) {
	if opts.Dev {
		return wasmCompilerGo, "", nil, nil
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv(runtimeModeEnv)))
	switch mode {
	case "", "auto", "tinygo":
		compiler, path, tinyGoErr := resolveWASMCompiler(opts, lookPath)
		if tinyGoErr == nil {
			return compiler, path, nil, nil
		}
		if mode == "tinygo" {
			return "", "", nil, tinyGoErr
		}
		bundle, prebuiltErr := resolvePrebuiltRuntimeForProject(context.Background(), opts, projectDir)
		if prebuiltErr != nil {
			return "", "", nil, fmt.Errorf("%w; prebuilt runtime fallback also failed: %v", tinyGoErr, prebuiltErr)
		}
		return wasmCompilerPrebuilt, bundle.Dir, &bundle, nil
	case "prebuilt":
		bundle, err := resolvePrebuiltRuntimeForProject(context.Background(), opts, projectDir)
		if err != nil {
			return "", "", nil, err
		}
		return wasmCompilerPrebuilt, bundle.Dir, &bundle, nil
	default:
		return "", "", nil, fmt.Errorf("%s=%q is not one of auto, tinygo, prebuilt", runtimeModeEnv, mode)
	}
}

// resolvePrebuiltRuntimeForProject resolves and verifies the prebuilt
// runtime for the exact m31labs.dev/gosx version projectDir's module graph
// requires (reusing resolveProjectGoSXVersion, the same check
// checkVersionSkew runs). --offline never blocks a cache hit: it only
// disables the network fetch a cache *miss* would otherwise need, via
// offlineRuntimeArtifactFetcher below, so a version already verified and
// cached by an earlier online build keeps working offline.
func resolvePrebuiltRuntimeForProject(ctx context.Context, opts BuildOptions, projectDir string) (prebuiltRuntimeBundle, error) {
	version, hasLocalReplace, ok := resolveProjectGoSXVersionFunc(projectDir)
	if !ok || hasLocalReplace || strings.TrimSpace(version) == "" {
		return prebuiltRuntimeBundle{}, fmt.Errorf("project is not pinned to a released m31labs.dev/gosx version (local source build); TinyGo is required")
	}
	if !looksLikeStableGoSXReleaseVersion(version) {
		return prebuiltRuntimeBundle{}, fmt.Errorf("m31labs.dev/gosx %s is not a stable release version; no prebuilt runtime is published for it", version)
	}
	cacheRoot, err := runtimeCacheRoot()
	if err != nil {
		return prebuiltRuntimeBundle{}, err
	}
	var fetcher runtimeArtifactFetcher
	if opts.Offline {
		fetcher = offlineRuntimeArtifactFetcher{}
	} else {
		owner, repo := runtimeReleaseRepo()
		fetcher = newDefaultRuntimeArtifactFetcher(owner, repo)
	}
	return resolvePrebuiltRuntime(ctx, cacheRoot, fetcher, version)
}

// offlineRuntimeArtifactFetcher refuses every fetch with a clear diagnostic.
// resolvePrebuiltRuntime only calls a fetcher on a cache miss, so this type
// makes --offline behave exactly like a normal run for an already-verified,
// cached version, and fail with an unambiguous reason otherwise.
type offlineRuntimeArtifactFetcher struct{}

func (offlineRuntimeArtifactFetcher) FetchAsset(context.Context, string, string) ([]byte, error) {
	return nil, fmt.Errorf("prebuilt runtime fetch is disabled by --offline")
}

func runtimeCacheRoot() (string, error) {
	if v := strings.TrimSpace(os.Getenv(runtimeCacheEnv)); v != "" {
		return v, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve prebuilt runtime cache directory: %w", err)
	}
	return filepath.Join(base, "gosx", "runtime"), nil
}

func runtimeReleaseRepo() (owner, repo string) {
	owner, repo = defaultRuntimeReleaseOwner, defaultRuntimeReleaseRepo
	spec := strings.TrimSpace(os.Getenv(runtimeReleaseRepoEnv))
	if spec == "" {
		return owner, repo
	}
	parts := strings.SplitN(spec, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return owner, repo
	}
	return parts[0], parts[1]
}

// resolvePrebuiltRuntime returns a verified bundle for version, either from
// an already-verified cache entry under cacheRoot or by fetching and
// verifying it via fetcher and populating the cache for next time. Every
// file this function returns a path to has already had its SHA-256 checked
// against the manifest fetcher itself supplied — a caller never needs to
// re-verify.
func resolvePrebuiltRuntime(ctx context.Context, cacheRoot string, fetcher runtimeArtifactFetcher, version string) (prebuiltRuntimeBundle, error) {
	dir := filepath.Join(cacheRoot, version)
	if bundle, ok := loadVerifiedPrebuiltRuntimeCache(dir); ok {
		return bundle, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return prebuiltRuntimeBundle{}, fmt.Errorf("create prebuilt runtime cache dir: %w", err)
	}

	manifestData, err := fetcher.FetchAsset(ctx, version, runtimeManifestAssetName)
	if err != nil {
		return prebuiltRuntimeBundle{}, fmt.Errorf("fetch prebuilt runtime manifest for %s: %w", version, err)
	}
	var evidence ouroboros.RuntimeBuildEvidence
	if err := json.Unmarshal(manifestData, &evidence); err != nil {
		return prebuiltRuntimeBundle{}, fmt.Errorf("decode prebuilt runtime manifest for %s: %w", version, err)
	}

	var shim *ouroboros.AssetMetrics
	measured := 0
	for _, v := range evidence.Variants {
		if v.Status != "measured" {
			continue
		}
		measured++
		if v.File == "" || v.SHA256 == "" {
			return prebuiltRuntimeBundle{}, fmt.Errorf("prebuilt runtime manifest for %s: variant %s is missing its file name or hash", version, v.ID)
		}
		data, err := fetcher.FetchAsset(ctx, version, v.File)
		if err != nil {
			return prebuiltRuntimeBundle{}, fmt.Errorf("fetch prebuilt runtime %s (%s): %w", v.ID, v.File, err)
		}
		if err := verifyRuntimeAssetDigest(v.File, data, v.SHA256, v.Bytes); err != nil {
			return prebuiltRuntimeBundle{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, v.File), data, 0o644); err != nil {
			return prebuiltRuntimeBundle{}, fmt.Errorf("cache prebuilt runtime %s: %w", v.File, err)
		}
		if shim == nil && v.Shim != nil {
			shim = v.Shim
		}
	}
	if measured == 0 {
		return prebuiltRuntimeBundle{}, fmt.Errorf("prebuilt runtime manifest for %s lists no measured variants", version)
	}
	if shim == nil || shim.SHA256 == "" {
		return prebuiltRuntimeBundle{}, fmt.Errorf("prebuilt runtime manifest for %s carries no wasm_exec.js provenance", version)
	}
	shimData, err := fetcher.FetchAsset(ctx, version, runtimeManifestWASMExecName)
	if err != nil {
		return prebuiltRuntimeBundle{}, fmt.Errorf("fetch prebuilt runtime wasm_exec.js for %s: %w", version, err)
	}
	if err := verifyRuntimeAssetDigest(runtimeManifestWASMExecName, shimData, shim.SHA256, shim.Bytes); err != nil {
		return prebuiltRuntimeBundle{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, runtimeManifestWASMExecName), shimData, 0o644); err != nil {
		return prebuiltRuntimeBundle{}, fmt.Errorf("cache prebuilt runtime wasm_exec.js: %w", err)
	}
	// The manifest itself is written last: its presence is what
	// loadVerifiedPrebuiltRuntimeCache treats as "this cache entry is
	// complete", so a build interrupted partway through never looks
	// like a valid cache hit on the next run.
	if err := os.WriteFile(filepath.Join(dir, runtimeManifestAssetName), manifestData, 0o644); err != nil {
		return prebuiltRuntimeBundle{}, fmt.Errorf("cache prebuilt runtime manifest: %w", err)
	}
	return prebuiltRuntimeBundle{Dir: dir, Evidence: evidence}, nil
}

// loadVerifiedPrebuiltRuntimeCache re-verifies every file a prior
// resolvePrebuiltRuntime call cached under dir. A cache hit never trusts
// disk contents on file presence alone — corruption or tampering since the
// last verified write is rejected exactly like a fresh fetch would be,
// falling through to re-fetch.
func loadVerifiedPrebuiltRuntimeCache(dir string) (prebuiltRuntimeBundle, bool) {
	manifestData, err := os.ReadFile(filepath.Join(dir, runtimeManifestAssetName))
	if err != nil {
		return prebuiltRuntimeBundle{}, false
	}
	var evidence ouroboros.RuntimeBuildEvidence
	if err := json.Unmarshal(manifestData, &evidence); err != nil {
		return prebuiltRuntimeBundle{}, false
	}
	var shim *ouroboros.AssetMetrics
	measured := 0
	for _, v := range evidence.Variants {
		if v.Status != "measured" {
			continue
		}
		measured++
		data, err := os.ReadFile(filepath.Join(dir, v.File))
		if err != nil {
			return prebuiltRuntimeBundle{}, false
		}
		if verifyRuntimeAssetDigest(v.File, data, v.SHA256, v.Bytes) != nil {
			return prebuiltRuntimeBundle{}, false
		}
		if shim == nil && v.Shim != nil {
			shim = v.Shim
		}
	}
	if measured == 0 || shim == nil {
		return prebuiltRuntimeBundle{}, false
	}
	shimData, err := os.ReadFile(filepath.Join(dir, runtimeManifestWASMExecName))
	if err != nil || verifyRuntimeAssetDigest(runtimeManifestWASMExecName, shimData, shim.SHA256, shim.Bytes) != nil {
		return prebuiltRuntimeBundle{}, false
	}
	return prebuiltRuntimeBundle{Dir: dir, Evidence: evidence}, true
}

func verifyRuntimeAssetDigest(name string, data []byte, wantSHA256 string, wantBytes int64) error {
	if wantBytes > 0 && int64(len(data)) != wantBytes {
		return fmt.Errorf("prebuilt runtime asset %s size mismatch: got %d bytes, manifest records %d", name, len(data), wantBytes)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, strings.TrimSpace(wantSHA256)) {
		return fmt.Errorf("prebuilt runtime asset %s failed integrity verification: sha256 %s does not match manifest %s", name, got, wantSHA256)
	}
	return nil
}

// writePrebuiltWASMExec stages bundle's verified wasm_exec.js into a
// project's runtime output directory the same way writeTinyGoWASMExec does
// for a live TinyGo toolchain: content-hashed filename plus SRI integrity.
func writePrebuiltWASMExec(bundle *prebuiltRuntimeBundle, runtimeDir string) (HashedAsset, error) {
	if bundle == nil {
		return HashedAsset{}, fmt.Errorf("prebuilt runtime bundle required for wasm_exec.js")
	}
	data, err := os.ReadFile(bundle.WASMExecPath())
	if err != nil {
		return HashedAsset{}, fmt.Errorf("read prebuilt wasm_exec.js: %w", err)
	}
	asset, err := writeHashed(runtimeDir, "wasm_exec", ".js", data)
	if err != nil {
		return HashedAsset{}, fmt.Errorf("write prebuilt wasm_exec.js: %w", err)
	}
	asset = withRuntimeIntegrity(asset, data)
	return asset, nil
}
