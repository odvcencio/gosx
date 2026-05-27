package surface

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gosx "m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// defaultNow is the seam currentTimeFn delegates to by default. Tests
// override currentTimeFn directly to pin a specific moment.
func defaultNow() time.Time { return time.Now() }

// surfaceManifestEntry is the per-component record written to the JSON manifest.
//
// BytecodeURL / BytecodePath / SurfaceKind name the shared-VM bytecode
// program the client bootstrap fetches and hydrates through the shared
// runtime. The legacy per-component WASM fields are gone — see ADR
// 0003 (supersedure) and ADR 0005 (buildsurface deletion).
type surfaceManifestEntry struct {
	Component    string            `json:"component"`
	Hash         string            `json:"hash"`
	PropsType    string            `json:"propsType,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	MountAttrs   map[string]string `json:"mountAttrs,omitempty"`

	BytecodePath string `json:"bytecodePath,omitempty"`
	BytecodeURL  string `json:"bytecodeURL,omitempty"`
	SurfaceKind  string `json:"surfaceKind,omitempty"`
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
const manifestSchemaVersion = 2

// BuildEvent is the kind of structured event passed to a BuildEventHook.
type BuildEvent int

const (
	// BuildSuccess is fired after a successful bytecode lower for a component.
	BuildSuccess BuildEvent = iota
	// BuildFailure is fired when lowering fails. The Err field on the
	// event payload carries the underlying error.
	BuildFailure
)

// BuildEventHook receives notifications about Discover's per-component build
// outcomes.
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

// Handler returns an http.Handler that serves /gosx/engines/ bytecode
// assets registered by Discover. Compose this into the host app's mux:
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
// surface component to shared-VM bytecode via ir/golower, registers an
// in-memory asset route for each, and writes
// .gosx/cache/surfaces/manifest.json.
//
// Discover is idempotent; call it once from server.New() or equivalent.
// Set the environment variable GOSX_DISABLE_SURFACE_AUTO to any non-empty
// value to skip discovery entirely (useful in test environments that do not
// have a real .gsx tree).
//
// All surfaces lower through the unified shared-VM bytecode path. The
// per-component WASM backend (internal/buildsurface) was deleted per
// ADR 0005; the `surface=wasm` escape hatch (ADR 0006) is no longer
// recognized and fails the build.
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

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("surface.Discover: create cache dir: %w", err)
	}

	mux := ensureHandler()
	manifest := surfaceManifest{Version: manifestSchemaVersion}

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
		return discoverFile(path, cacheDir, mux, &manifest)
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

// discoverFile handles a single .gsx file: lowers each surface component
// to bytecode, registers routes, and appends manifest entries. Surfaces
// carrying the legacy `//gosx:engine surface=wasm` escape-hatch
// annotation are rejected with a clear error pointing at the deletion
// ADR — the WASM backend they targeted no longer exists.
func discoverFile(gszPath, cacheDir string, mux *http.ServeMux, manifest *surfaceManifest) error {
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

	if hasWASMEscapeHatch(src) {
		err := fmt.Errorf(
			"gosx/surface: %s declares //gosx:engine surface=wasm, which is no longer supported. "+
				"The per-component WASM backend was removed per ADR 0005 (buildsurface deletion). "+
				"Remove the `surface=wasm` annotation; surfaces now lower to shared-VM bytecode automatically",
			gszPath,
		)
		fmt.Fprintln(os.Stderr, err)
		return nil //nolint:nilerr — log + skip, do not fail the whole walk
	}

	for i, comp := range prog.Components {
		if !comp.EngineSurface {
			continue
		}

		sp, err := ir.LowerEngineSurface(prog, i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gosx/surface: skip component %q in %s: lower error: %v\n", comp.Name, gszPath, err)
			continue
		}

		discoverComponentBytecode(sp, gszPath, cacheDir, mux, manifest)
	}
	return nil
}

// discoverComponentBytecode runs the bytecode lowering pipeline for one
// component: golower → JSON cache → manifest entry → http route
// registration → in-memory registry register.
//
// Lowering errors are non-fatal at the file level (other components in
// the same file still get a chance to build) but they do fire a
// BuildFailure event so the embedder can surface them.
func discoverComponentBytecode(sp *ir.SurfaceProgram, gszPath, cacheDir string, mux *http.ServeMux, manifest *surfaceManifest) {
	res, err := LowerToBytecode(sp, cacheDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gosx/surface: lower bytecode for %q (%s): %v\n", sp.Name, gszPath, err)
		fireBuildEvent(sp.Name, BuildFailure, err)
		return
	}
	fireBuildEvent(sp.Name, BuildSuccess, nil)

	entry := surfaceManifestEntry{
		Component:    sp.Name,
		Hash:         res.Hash,
		PropsType:    sp.PropsTypeName,
		Capabilities: sp.Capabilities,
		MountAttrs:   sp.MountAttrs,
		BytecodeURL:  res.JSONURL,
		BytecodePath: res.JSONPath,
		SurfaceKind:  res.SurfaceKind.String(),
	}

	// Register an http route that serves the JSON bytecode.
	localPath := res.JSONPath
	handlerMu.Lock()
	mux.HandleFunc(res.JSONURL, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, localPath)
	})
	handlerMu.Unlock()

	registry.register(sp.Name, &registryEntry{
		hash:         entry.Hash,
		propsType:    entry.PropsType,
		capabilities: entry.Capabilities,
		mountAttrs:   entry.MountAttrs,
		bytecodeURL:  res.JSONURL,
		bytecodePath: res.JSONPath,
		surfaceKind:  res.SurfaceKind,
	})
	manifest.Surfaces = append(manifest.Surfaces, entry)
}

// currentTimeFn is a small seam tests can override; retained for
// parity with prior call sites that pinned "now".
var currentTimeFn = defaultNow
