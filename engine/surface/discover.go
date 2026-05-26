package surface

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/buildsurface"
	"m31labs.dev/gosx/ir"
)

// surfaceManifestEntry is the per-component record written to the JSON manifest.
//
// Stale and LastBuildError support the spec §B fallback semantics: when a
// rebuild fails but a prior good WASM is still on disk, we keep WasmPath/
// WasmURL/Hash pointing at the cached file, set Stale=true, and record the
// build error so embedders can surface it.
type surfaceManifestEntry struct {
	Component      string            `json:"component"`
	WASMPath       string            `json:"wasmPath"`
	WASMURL        string            `json:"wasmURL"`
	Hash           string            `json:"hash"`
	PropsType      string            `json:"propsType,omitempty"`
	Capabilities   []string          `json:"capabilities,omitempty"`
	MountAttrs     map[string]string `json:"mountAttrs,omitempty"`
	Stale          bool              `json:"stale,omitempty"`
	LastBuildError string            `json:"lastBuildError,omitempty"`
}

// surfaceManifest is written to .gosx/cache/surfaces/manifest.json.
//
// Version is a schema discriminator (spec Q-B). Bump when a field's
// semantics change incompatibly so future readers can detect drift.
type surfaceManifest struct {
	Version  int                    `json:"version"`
	Surfaces []surfaceManifestEntry `json:"surfaces"`
}

// manifestSchemaVersion is the current value of surfaceManifest.Version.
const manifestSchemaVersion = 1

// BuildEvent is the kind of structured event passed to a BuildEventHook.
type BuildEvent int

const (
	// BuildSuccess is fired after a successful WASM compile for a component.
	BuildSuccess BuildEvent = iota
	// BuildFailure is fired when a compile fails. The Err field on the
	// event payload carries the underlying error.
	BuildFailure
	// BuildFailureStaleFallback is fired when a compile fails but a prior
	// cached WASM survives and is reused.
	BuildFailureStaleFallback
)

// BuildEventHook receives notifications about Discover's per-component build
// outcomes. Embedders may use it to surface stale state in their UI.
type BuildEventHook func(component string, event BuildEvent, err error)

var (
	buildEventMu   sync.RWMutex
	buildEventHook BuildEventHook
)

// OnBuildEvent installs hook as the process-wide build-event listener,
// returning the previously installed hook (nil if none). Pass nil to clear.
// hook is called inline from Discover; it must be fast and goroutine-safe.
func OnBuildEvent(hook BuildEventHook) BuildEventHook {
	buildEventMu.Lock()
	prev := buildEventHook
	buildEventHook = hook
	buildEventMu.Unlock()
	return prev
}

// fireBuildEvent invokes the installed hook (if any). Safe to call with no hook.
func fireBuildEvent(component string, event BuildEvent, err error) {
	buildEventMu.RLock()
	hook := buildEventHook
	buildEventMu.RUnlock()
	if hook != nil {
		hook(component, event, err)
	}
}

// handler is the singleton http.Handler returned by Handler(). It is built
// incrementally as Discover finds surfaces.
var (
	handlerMu   sync.RWMutex
	handlerMux  *http.ServeMux
	handlerOnce sync.Once
)

func ensureHandler() *http.ServeMux {
	handlerOnce.Do(func() {
		handlerMux = http.NewServeMux()
	})
	return handlerMux
}

// Handler returns an http.Handler that serves /gosx/engines/ WASM assets
// registered by Discover. Compose this into the host app's mux:
//
//	mux.Handle("/gosx/engines/", surface.Handler())
//
// The handler is populated incrementally as Discover processes .gsx files.
// It is safe to call Handler before Discover.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerMu.RLock()
		mux := handlerMux
		handlerMu.RUnlock()
		if mux == nil {
			http.NotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Discover walks projectRoot looking for *.gsx files, lowers each engine
// surface component, compiles the corresponding WASM via
// internal/buildsurface.Build, registers in-memory asset routes, and writes
// .gosx/cache/surfaces/manifest.json.
//
// Discover is idempotent; call it once from server.New() or equivalent.
// Set the environment variable GOSX_DISABLE_SURFACE_AUTO to any non-empty
// value to skip discovery entirely (useful in test environments that do not
// have a real .gsx tree).
//
// Integration risks:
//  1. Chunk C's buildsurface.Build must be implemented before compiled WASM
//     is available. Until then, Discover logs a warning and continues; the
//     placeholder node is still rendered but the WASM URL will be empty.
//  2. Chunk C's JS bootstrap must call globalThis.__gosx_surface_register(name)
//     with the exact component name registered via runtime.Register, which in
//     turn comes from the manifest. Any name mismatch will silently fail to mount.
//  3. The event payload shape documented in runtime/runtime.go must match what
//     the JS bootstrap emits. See runtime/runtime.go for the canonical table.
func Discover(projectRoot string) error {
	if os.Getenv("GOSX_DISABLE_SURFACE_AUTO") != "" {
		return nil
	}

	// Resolve to an absolute path so that filepath.WalkDir does not receive "."
	// as the root, which would cause the skip-hidden-dirs heuristic to
	// immediately SkipDir the entire tree (d.Name() == "." starts with '.').
	if !filepath.IsAbs(projectRoot) {
		abs, err := filepath.Abs(projectRoot)
		if err == nil {
			projectRoot = abs
		}
	}

	cacheDir := filepath.Join(projectRoot, ".gosx", "cache", "surfaces")
	outputDir := filepath.Join(projectRoot, ".gosx", "cache", "surfaces", "wasm")

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("surface.Discover: create cache dir: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("surface.Discover: create output dir: %w", err)
	}

	mux := ensureHandler()
	manifest := surfaceManifest{Version: manifestSchemaVersion}

	// Read prior manifest so per-component build failures can fall back to
	// the last-known-good cached WASM (spec §B). A missing or unparseable
	// prior manifest is treated as "no priors known" — never fatal.
	priors := loadPreviousManifestByComponent(filepath.Join(cacheDir, "manifest.json"))

	err := filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden dirs, .gosx cache, and node_modules.
			if name == "node_modules" || (len(name) > 0 && name[0] == '.') {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".gsx") {
			return nil
		}
		return discoverFile(path, cacheDir, outputDir, mux, &manifest, priors)
	})
	if err != nil {
		return fmt.Errorf("surface.Discover: walk %s: %w", projectRoot, err)
	}

	// Write the manifest.
	if err := writeManifest(cacheDir, &manifest); err != nil {
		return err
	}

	return nil
}

// loadPreviousManifestByComponent returns a map of component → prior manifest
// entry. Returns an empty map on any error (missing file, unparseable JSON,
// wrong schema). This is intentionally permissive: a corrupt prior manifest
// must not block Discover.
func loadPreviousManifestByComponent(path string) map[string]surfaceManifestEntry {
	out := map[string]surfaceManifestEntry{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var prev surfaceManifest
	if err := json.Unmarshal(raw, &prev); err != nil {
		return out
	}
	for _, e := range prev.Surfaces {
		if e.Component == "" {
			continue
		}
		out[e.Component] = e
	}
	return out
}

// writeManifest serializes m to <cacheDir>/manifest.json with the schema
// version pinned to manifestSchemaVersion.
func writeManifest(cacheDir string, m *surfaceManifest) error {
	if m.Version == 0 {
		m.Version = manifestSchemaVersion
	}
	manifestPath := filepath.Join(cacheDir, "manifest.json")
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("surface.Discover: marshal manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, raw, 0644); err != nil {
		return fmt.Errorf("surface.Discover: write manifest: %w", err)
	}
	return nil
}

// mergeBuildResult takes the freshly attempted build's outcome and either
// applies it (success) or falls back to a prior entry (failure with a
// cached WASM still on disk). Returns the entry to write into the manifest
// and the URL+path pair that should be registered as the serving route.
//
// Semantics (spec §B):
//   - buildErr == nil:                    use fresh entry as-is.
//   - buildErr != nil, prior with cache:  reuse prior URL/path/hash, set Stale and LastBuildError.
//   - buildErr != nil, no prior or cache: keep fresh (empty) entry, record LastBuildError.
func mergeBuildResult(fresh surfaceManifestEntry, prior surfaceManifestEntry, hasPrior bool, buildErr error) (entry surfaceManifestEntry, serveURL, servePath string) {
	if buildErr == nil {
		return fresh, fresh.WASMURL, fresh.WASMPath
	}
	// Build failed. Try to recover from prior.
	if hasPrior && prior.WASMPath != "" && prior.WASMURL != "" {
		if _, statErr := os.Stat(prior.WASMPath); statErr == nil {
			merged := prior
			// Refresh non-binary fields from the fresh attempt (capabilities,
			// propsType, mountAttrs may legitimately change without recompiling).
			if fresh.PropsType != "" {
				merged.PropsType = fresh.PropsType
			}
			if len(fresh.Capabilities) > 0 {
				merged.Capabilities = fresh.Capabilities
			}
			if len(fresh.MountAttrs) > 0 {
				merged.MountAttrs = fresh.MountAttrs
			}
			merged.Stale = true
			merged.LastBuildError = buildErr.Error()
			return merged, merged.WASMURL, merged.WASMPath
		}
	}
	// No usable fallback: record the failure on the empty entry.
	fresh.LastBuildError = buildErr.Error()
	return fresh, "", ""
}

// discoverFile handles a single .gsx file: compiles its surface components,
// registers routes, and appends manifest entries. priors contains the
// previous manifest's entries keyed by component name so that build failures
// can fall back to the last-known-good WASM (spec §B).
func discoverFile(gszPath, cacheDir, outputDir string, mux *http.ServeMux, manifest *surfaceManifest, priors map[string]surfaceManifestEntry) error {
	src, err := os.ReadFile(gszPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", gszPath, err)
	}

	prog, err := gosx.Compile(src)
	if err != nil {
		// Compilation errors in user .gsx are non-fatal; log and continue.
		fmt.Fprintf(os.Stderr, "gosx/surface: skip %s: compile error: %v\n", gszPath, err)
		return nil //nolint:nilerr
	}

	// Populate Dir and PackagePath so LowerEngineSurface can read source files.
	prog.Dir = filepath.Dir(gszPath)

	for i, comp := range prog.Components {
		if !comp.EngineSurface {
			continue
		}

		sp, err := ir.LowerEngineSurface(prog, i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gosx/surface: skip component %q in %s: lower error: %v\n", comp.Name, gszPath, err)
			continue
		}

		ctx := context.Background()
		// Default to TinyGo for size — Go-WASM is 20+ MB for nontrivial programs
		// and violates the gosx "inspectable runtime cost" principle. Fall back
		// to the standard gc toolchain only when TinyGo isn't on PATH OR when
		// no compatible Go SDK is installed (TinyGo ≤0.40 caps at Go 1.25).
		compiler := buildsurface.CompilerTinyGo
		if reason := tinyGoUnavailableReason(); reason != "" {
			compiler = buildsurface.CompilerGo
			fmt.Fprintf(os.Stderr, "gosx/surface: %s; falling back to standard Go (-trimpath -ldflags=-s -w) — WASM will be larger than TinyGo output\n", reason)
		}
		wasmPath, hash, buildErr := buildsurface.Build(ctx, sp, buildsurface.Options{
			Compiler:  compiler,
			CacheDir:  cacheDir,
			OutputDir: outputDir,
		})

		freshWasmURL := ""
		if hash != "" {
			freshWasmURL = fmt.Sprintf("/gosx/engines/%s.%s.wasm", sp.Name, hash)
		}

		fresh := surfaceManifestEntry{
			Component:    sp.Name,
			WASMPath:     wasmPath,
			WASMURL:      freshWasmURL,
			Hash:         hash,
			PropsType:    sp.PropsTypeName,
			Capabilities: sp.Capabilities,
			MountAttrs:   sp.MountAttrs,
		}

		prior, hasPrior := priors[sp.Name]
		entry, serveURL, servePath := mergeBuildResult(fresh, prior, hasPrior, buildErr)

		switch {
		case buildErr == nil:
			fireBuildEvent(sp.Name, BuildSuccess, nil)
		case entry.Stale:
			fmt.Fprintf(os.Stderr, "gosx/surface: reusing stale WASM for %q (build failed: %v)\n", sp.Name, buildErr)
			fireBuildEvent(sp.Name, BuildFailureStaleFallback, buildErr)
		default:
			// Either there was no prior entry, or its cached WASM is gone.
			fmt.Fprintf(os.Stderr, "gosx/surface: build WASM for %q: %v\n", sp.Name, buildErr)
			fireBuildEvent(sp.Name, BuildFailure, buildErr)
		}

		// Register the file-serving route if (now) the WASM is available —
		// either freshly built or reused from cache.
		if serveURL != "" && servePath != "" {
			localPath := servePath // capture for closure
			handlerMu.Lock()
			mux.HandleFunc(serveURL, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/wasm")
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				http.ServeFile(w, r, localPath)
			})
			handlerMu.Unlock()
		}

		// Register in the in-memory registry for Renderer.Mount.
		registry.register(sp.Name, &registryEntry{
			wasmURL:      entry.WASMURL,
			hash:         entry.Hash,
			propsType:    entry.PropsType,
			capabilities: entry.Capabilities,
			mountAttrs:   entry.MountAttrs,
			stale:        entry.Stale,
		})

		// Append to manifest.
		manifest.Surfaces = append(manifest.Surfaces, entry)
	}
	return nil
}

// mergeManifestForTest is a test-only seam exposing the merge-and-write path
// without invoking the full Discover walk. Tests use it to assert spec §B
// fallback behavior under synthetic build outcomes.
//
// Callers seed cacheDir/manifest.json (via seedManifest) with the desired
// prior state, then call this helper. The merged manifest is written back to
// disk just like Discover would.
func mergeManifestForTest(t TestingTBLike, cacheDir, component string, fresh surfaceManifestEntry, buildErr error) {
	priors := loadPreviousManifestByComponent(filepath.Join(cacheDir, "manifest.json"))
	prior, hasPrior := priors[component]
	entry, _, _ := mergeBuildResult(fresh, prior, hasPrior, buildErr)

	switch {
	case buildErr == nil:
		fireBuildEvent(component, BuildSuccess, nil)
	case entry.Stale:
		fireBuildEvent(component, BuildFailureStaleFallback, buildErr)
	default:
		fireBuildEvent(component, BuildFailure, buildErr)
	}

	out := surfaceManifest{Version: manifestSchemaVersion, Surfaces: []surfaceManifestEntry{entry}}
	if err := writeManifest(cacheDir, &out); err != nil {
		t.Fatalf("mergeManifestForTest: writeManifest: %v", err)
	}
}

// TestingTBLike is the minimal testing.TB surface mergeManifestForTest needs.
// Defined to avoid importing testing into a non-test file.
type TestingTBLike interface {
	Helper()
	Fatalf(format string, args ...any)
}

// tinyGoUnavailableReason returns "" if TinyGo can actually build (binary on
// PATH AND a compatible Go SDK is available), or a human-readable reason why
// not. Honors GOSX_FORCE_GO_COMPILER as an explicit "skip TinyGo" override
// (set to any non-empty value when transitive dep go-directives push past
// TinyGo's supported Go range and you want the standard gc fallback).
func tinyGoUnavailableReason() string {
	if strings.TrimSpace(os.Getenv("GOSX_FORCE_GO_COMPILER")) != "" {
		return "GOSX_FORCE_GO_COMPILER set"
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		return "tinygo not on PATH (install: https://tinygo.org/getting-started/install/)"
	}
	// TinyGo (≤0.40.x) supports Go 1.19–1.25. We need to find either:
	//   - current Go in that range, or
	//   - an SDK under $HOME/sdk/go1.N matching, or
	//   - $GOSX_TINYGO_GOROOT pointing somewhere usable.
	// Be conservative: if we can't find a 1.19–1.25 SDK, fall back.
	if root := strings.TrimSpace(os.Getenv("GOSX_TINYGO_GOROOT")); root != "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, "sdk", "go1.*"))
		for _, root := range matches {
			base := filepath.Base(root)
			if !strings.HasPrefix(base, "go1.") {
				continue
			}
			rest := strings.TrimPrefix(base, "go1.")
			dot := strings.Index(rest, ".")
			minorStr := rest
			if dot >= 0 {
				minorStr = rest[:dot]
			}
			var minor int
			if _, err := fmt.Sscanf(minorStr, "%d", &minor); err != nil {
				continue
			}
			if minor >= 19 && minor <= 25 {
				// Check the SDK has a usable go binary AND that gosx's own
				// transitive deps don't require something higher than this SDK.
				if _, statErr := os.Stat(filepath.Join(root, "bin", "go")); statErr == nil {
					// Heuristic: we can't easily probe deep transitive go-directives
					// from here, so assume the SDK is usable. If TinyGo actually
					// fails to build (e.g. because the gosx module declares
					// go >= 1.26), Build() will return an error and the caller
					// will log it.
					return ""
				}
			}
		}
	}
	return "tinygo present but no compatible Go SDK in 1.19–1.25 range found under ~/sdk/go1.*"
}
