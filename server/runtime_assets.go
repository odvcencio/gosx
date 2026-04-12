package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/island"
)

// bootstrapStub is a minimal no-op bootstrap script served when the real
// bootstrap.js has not been built yet (e.g. during `go run` development).
const bootstrapStub = `// Minimal bootstrap stub — gosx build not yet run
(function(){
  window.__gosx = window.__gosx || { version: "dev", islands: new Map(), engines: new Map(), hubs: new Map(), textLayouts: new Map(), sharedSignals: { values: new Map(), subscribers: new Map() }, input: {}, ready: true };
  window.__gosx_engine_factories = window.__gosx_engine_factories || Object.create(null);
  window.__gosx_register_engine_factory = window.__gosx_register_engine_factory || function(name, factory) { window.__gosx_engine_factories[name] = factory; };
  // Mount engines from page manifest
  try {
    var scripts = document.querySelectorAll('script:not([src])');
    scripts.forEach(function(s) {
      try {
        var m = JSON.parse(s.textContent);
        if (m && m.engines) {
          m.engines.forEach(function(e) {
            var mount = document.getElementById(e.mountId);
            var factory = window.__gosx_engine_factories[e.component];
            if (mount && factory && mount.children.length === 0) {
              factory({ mount: mount, id: e.id, kind: e.kind, component: e.component, props: e.props || {}, capabilities: e.capabilities || [], programRef: e.programRef || '', runtime: null, emit: function(){} });
            }
          });
        }
      } catch(ex) {}
    });
  } catch(ex) {}
})();
`

type runtimeManifestCache struct {
	mu       sync.Mutex
	root     string
	modTime  time.Time
	manifest *buildmanifest.Manifest
}

// SetRuntimeRoot overrides the filesystem root used to resolve `/gosx/*`
// compatibility assets for engines and future page runtimes.
func (a *App) SetRuntimeRoot(root string) {
	a.runtimeRoot = strings.TrimSpace(root)
	island.SetManifestRoot(a.runtimeRoot)
}

func (a *App) effectiveRuntimeRoot() string {
	if a == nil {
		return ""
	}
	if root := strings.TrimSpace(a.runtimeRoot); root != "" {
		return root
	}
	if publicDir := strings.TrimSpace(a.publicDir); publicDir != "" {
		parent := filepath.Dir(publicDir)
		if parent != "" && parent != "." {
			return parent
		}
	}
	return ResolveAppRoot("")
}

func (a *App) hasCompatRuntimeAssets() bool {
	root := a.effectiveRuntimeRoot()
	if root == "" {
		return false
	}
	if isFile(filepath.Join(root, "build", "bootstrap.js")) {
		return true
	}
	return isFile(filepath.Join(root, "build.json")) && isDir(filepath.Join(root, "assets"))
}

func (a *App) serveRuntimeAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/gosx/")), "/")
	if name == "" || strings.HasPrefix(name, "../") {
		http.NotFound(w, r)
		return
	}

	root := a.effectiveRuntimeRoot()
	if root == "" {
		if name == "bootstrap.js" || name == "bootstrap-lite.js" || name == "bootstrap-runtime.js" {
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Cache-Control", "no-cache")
			MarkObservedRequest(r, "runtime", "/gosx/"+name)
			w.Write([]byte(bootstrapStub))
			return
		}
		http.NotFound(w, r)
		return
	}

	if fsPath, ok := runtimeManifestDirectAssetPath(root, name); ok {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		http.ServeFile(w, r, fsPath)
		return
	}

	if version := strings.TrimSpace(r.URL.Query().Get("v")); version != "" {
		if fsPath, ok := a.runtimeCompatBuiltPath(root, name); ok {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			MarkObservedRequest(r, "runtime", "/gosx/"+name)
			http.ServeFile(w, r, fsPath)
			return
		}
	}

	if fsPath, ok := runtimeCompatSourcePath(root, name); ok {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		http.ServeFile(w, r, fsPath)
		return
	}

	if fsPath, ok := a.runtimeCompatBuiltPath(root, name); ok {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		http.ServeFile(w, r, fsPath)
		return
	}

	if name == "bootstrap.js" || name == "bootstrap-lite.js" || name == "bootstrap-runtime.js" {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-cache")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		w.Write([]byte(bootstrapStub))
		return
	}

	http.NotFound(w, r)
}

func runtimeManifestDirectAssetPath(root, name string) (string, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if !strings.HasPrefix(name, "assets/") {
		return "", false
	}
	target, ok := safeArtifactPath(filepath.Join(root, "assets"), strings.TrimPrefix(name, "assets/"))
	if !ok || !isFile(target) {
		return "", false
	}
	return target, true
}

func runtimeCompatSourcePath(root, name string) (string, bool) {
	buildDir := filepath.Join(root, "build")
	candidates := map[string]string{
		"runtime.wasm":                 filepath.Join(buildDir, "gosx-runtime.wasm"),
		"wasm_exec.js":                 filepath.Join(buildDir, "wasm_exec.js"),
		"bootstrap.js":                 filepath.Join(buildDir, "bootstrap.js"),
		"bootstrap-lite.js":            filepath.Join(buildDir, "bootstrap-lite.js"),
		"bootstrap-runtime.js":         filepath.Join(buildDir, "bootstrap-runtime.js"),
		"bootstrap-feature-islands.js": filepath.Join(buildDir, "bootstrap-feature-islands.js"),
		"bootstrap-feature-engines.js": filepath.Join(buildDir, "bootstrap-feature-engines.js"),
		"bootstrap-feature-hubs.js":    filepath.Join(buildDir, "bootstrap-feature-hubs.js"),
		"bootstrap-feature-scene3d.js": filepath.Join(buildDir, "bootstrap-feature-scene3d.js"),
		"patch.js":                     filepath.Join(buildDir, "patch.js"),
		"hls.min.js":                   filepath.Join(buildDir, "hls.min.js"),
	}
	if direct, ok := candidates[name]; ok && isFile(direct) {
		return direct, true
	}
	if strings.HasPrefix(name, "bootstrap") || name == "patch.js" {
		clientPath := filepath.Join(root, "client", "js", name)
		if isFile(clientPath) {
			return clientPath, true
		}
	}
	if name == "hls.min.js" {
		clientPath := filepath.Join(root, "client", "js", "vendor", name)
		if isFile(clientPath) {
			return clientPath, true
		}
	}
	if strings.HasPrefix(name, "islands/") || strings.HasPrefix(name, "css/") {
		target := filepath.Join(buildDir, filepath.FromSlash(name))
		if isFile(target) {
			return target, true
		}
	}
	return "", false
}

func (a *App) runtimeCompatBuiltPath(root, name string) (string, bool) {
	manifest, ok := a.runtimeBuildManifest(root)
	if !ok {
		return "", false
	}
	assetsDir := filepath.Join(root, "assets")

	switch name {
	case "runtime.wasm":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASM.File)
	case "wasm_exec.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASMExec.File)
	case "bootstrap.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Bootstrap.File)
	case "bootstrap-lite.js":
		if strings.TrimSpace(manifest.Runtime.BootstrapLite.File) == "" {
			return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Bootstrap.File)
		}
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapLite.File)
	case "bootstrap-runtime.js":
		if strings.TrimSpace(manifest.Runtime.BootstrapRuntime.File) == "" {
			return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Bootstrap.File)
		}
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapRuntime.File)
	case "bootstrap-feature-islands.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureIslands.File)
	case "bootstrap-feature-engines.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureEngines.File)
	case "bootstrap-feature-hubs.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureHubs.File)
	case "bootstrap-feature-scene3d.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3D.File)
	case "patch.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Patch.File)
	case "hls.min.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.VideoHLS.File)
	}

	if strings.HasPrefix(name, "islands/") {
		base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
		if asset, ok := manifest.IslandAssetByName(base); ok {
			return runtimeManifestAssetPath(assetsDir, "islands", asset.File)
		}
	}
	if strings.HasPrefix(name, "css/") {
		requested := filepath.Base(name)
		for _, asset := range manifest.CSS {
			if filepath.Base(asset.Source) == requested || strings.TrimSpace(asset.Component)+".css" == requested {
				return runtimeManifestAssetPath(assetsDir, "css", asset.File)
			}
		}
	}
	return "", false
}

func runtimeManifestAssetPath(assetsDir, bucket, file string) (string, bool) {
	if strings.TrimSpace(file) == "" {
		return "", false
	}
	target, ok := safeArtifactPath(assetsDir, filepath.Join(bucket, file))
	if !ok || !isFile(target) {
		return "", false
	}
	return target, true
}

func (a *App) runtimeBuildManifest(root string) (*buildmanifest.Manifest, bool) {
	if a == nil {
		return nil, false
	}
	manifestPath := filepath.Join(root, "build.json")
	info, err := os.Stat(manifestPath)
	if err != nil || info.IsDir() {
		return nil, false
	}
	if a.runtimeMeta == nil {
		a.runtimeMeta = &runtimeManifestCache{}
	}

	cache := a.runtimeMeta
	modTime := info.ModTime().UTC()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.manifest != nil && cache.root == root && cache.modTime.Equal(modTime) {
		return cache.manifest, true
	}

	manifest, err := buildmanifest.Load(manifestPath)
	if err != nil {
		return nil, false
	}
	cache.root = root
	cache.modTime = modTime
	cache.manifest = manifest
	return manifest, true
}
