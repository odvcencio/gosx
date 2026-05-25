package surface

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
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
		wasmPath, hash, buildErr := buildsurface.Build(ctx, sp, buildsurface.Options{
			Compiler:  buildsurface.CompilerGo,
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
