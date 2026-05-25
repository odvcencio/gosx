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

	gosx "github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/internal/buildsurface"
	"github.com/odvcencio/gosx/ir"
)

// surfaceManifestEntry is the per-component record written to the JSON manifest.
type surfaceManifestEntry struct {
	Component    string            `json:"component"`
	WASMPath     string            `json:"wasmPath"`
	WASMURL      string            `json:"wasmURL"`
	Hash         string            `json:"hash"`
	PropsType    string            `json:"propsType,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	MountAttrs   map[string]string `json:"mountAttrs,omitempty"`
}

// surfaceManifest is written to .gosx/cache/surfaces/manifest.json.
type surfaceManifest struct {
	Surfaces []surfaceManifestEntry `json:"surfaces"`
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
	var manifest surfaceManifest

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
		return discoverFile(path, cacheDir, outputDir, mux, &manifest)
	})
	if err != nil {
		return fmt.Errorf("surface.Discover: walk %s: %w", projectRoot, err)
	}

	// Write the manifest.
	manifestPath := filepath.Join(cacheDir, "manifest.json")
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("surface.Discover: marshal manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, raw, 0644); err != nil {
		return fmt.Errorf("surface.Discover: write manifest: %w", err)
	}

	return nil
}

// discoverFile handles a single .gsx file: compiles its surface components,
// registers routes, and appends manifest entries.
func discoverFile(gszPath, cacheDir, outputDir string, mux *http.ServeMux, manifest *surfaceManifest) error {
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
		if buildErr != nil {
			// Build may fail legitimately during Chunk C integration; log and
			// register a placeholder so the renderer still works.
			fmt.Fprintf(os.Stderr, "gosx/surface: build WASM for %q: %v\n", sp.Name, buildErr)
		}

		// Build the public URL. When the WASM isn't ready the URL is empty and
		// the client bootstrap will skip mounting (no silent crash).
		wasmURL := ""
		if hash != "" {
			wasmURL = fmt.Sprintf("/gosx/engines/%s.%s.wasm", sp.Name, hash)
		}

		// Register the file-serving route if the WASM was built.
		if wasmPath != "" && wasmURL != "" {
			localPath := wasmPath // capture for closure
			handlerMu.Lock()
			mux.HandleFunc(wasmURL, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/wasm")
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				http.ServeFile(w, r, localPath)
			})
			handlerMu.Unlock()
		}

		// Register in the in-memory registry for Renderer.Mount.
		registry.register(sp.Name, &registryEntry{
			wasmURL:      wasmURL,
			hash:         hash,
			propsType:    sp.PropsTypeName,
			capabilities: sp.Capabilities,
			mountAttrs:   sp.MountAttrs,
		})

		// Append to manifest.
		manifest.Surfaces = append(manifest.Surfaces, surfaceManifestEntry{
			Component:    sp.Name,
			WASMPath:     wasmPath,
			WASMURL:      wasmURL,
			Hash:         hash,
			PropsType:    sp.PropsTypeName,
			Capabilities: sp.Capabilities,
			MountAttrs:   sp.MountAttrs,
		})
	}
	return nil
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
