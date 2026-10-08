package server

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/httpcache"
	"m31labs.dev/gosx/internal/httpcompress"
	"m31labs.dev/gosx/island"
)

// bootstrapStub is a minimal no-op bootstrap script served when the real
// bootstrap.js has not been built yet (e.g. during `go run` development).
const bootstrapStub = `// Minimal bootstrap stub — gosx build not yet run
(function(){
  var gosx = window.__gosx || {};
  window.__gosx = gosx;
  if (!gosx.navigation && window.__gosx_page_nav) gosx.navigation = window.__gosx_page_nav;
  gosx.version = "dev";
  gosx.islands = new Map();
  gosx.computeIslands = new Map();
  gosx.engines = new Map();
  gosx.hubs = new Map();
  gosx.controllers = new Map();
  gosx.textLayouts = new Map();
  gosx.sharedSignals = { values: new Map(), subscribers: new Map(), nextID: 0 };
  gosx.input = { pending: null, frameHandle: 0, providers: Object.create(null) };
  gosx.ready = false;
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
  gosx.ready = true;
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

	// Embedded runtime assets resolve before (and independently of) the
	// runtime asset root.
	if name == "devtools-lantern.js" {
		serveDevtoolsLantern(w, r)
		return
	}
	if name == "youtube-audio.js" {
		serveYouTubeAudioBridge(w, r)
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
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		serveImmutableRuntimeFile(w, r, fsPath, !a.compressionOff)
		return
	}

	if version := strings.TrimSpace(r.URL.Query().Get("v")); version != "" {
		if fsPath, ok := a.runtimeCompatBuiltPath(root, name); ok {
			MarkObservedRequest(r, "runtime", "/gosx/"+name)
			serveImmutableRuntimeFile(w, r, fsPath, !a.compressionOff)
			return
		}
	}

	if fsPath, ok := runtimeCompatSourcePath(root, name); ok {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		serveRuntimeFileWithCompression(w, r, fsPath, !a.compressionOff)
		return
	}

	if fsPath, ok := a.runtimeCompatBuiltPath(root, name); ok {
		MarkObservedRequest(r, "runtime", "/gosx/"+name)
		serveImmutableRuntimeFile(w, r, fsPath, !a.compressionOff)
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

func serveImmutableRuntimeFile(w http.ResponseWriter, r *http.Request, fsPath string, compression bool) {
	setRuntimeContentType(w.Header(), fsPath)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(fsPath)))
	}
	httpcache.MarkImmutableAsset(w, r)
	serveRuntimeFileWithCompression(w, r, fsPath, compression)
}

func serveRuntimeFile(w http.ResponseWriter, r *http.Request, fsPath string) {
	serveRuntimeFileWithCompression(w, r, fsPath, true)
}

func serveRuntimeFileWithCompression(w http.ResponseWriter, r *http.Request, fsPath string, compression bool) {
	// Preserve the media type of a compiled program when serving its compressed
	// sidecar; otherwise ServeFile infers a type from the .gz or .br filename.
	if strings.EqualFold(filepath.Ext(fsPath), ".gxi") {
		setRuntimeContentType(w.Header(), fsPath)
	}
	if compression {
		addAcceptEncodingVary(w.Header())
	}
	if compression && canServeCompressedFile(r) && requestAcceptsBrotli(r) {
		if brPath := fsPath + ".br"; isFile(brPath) {
			w.Header().Set("Content-Encoding", "br")
			setRuntimeContentType(w.Header(), fsPath)
			http.ServeFile(w, r, brPath)
			return
		}
	}
	if compression && canServeCompressedFile(r) && requestAcceptsGzip(r) {
		if gzPath := fsPath + ".gz"; isFile(gzPath) {
			w.Header().Set("Content-Encoding", "gzip")
			setRuntimeContentType(w.Header(), fsPath)
			http.ServeFile(w, r, gzPath)
			return
		}
	}
	http.ServeFile(w, r, fsPath)
}

func requestAcceptsBrotli(r *http.Request) bool {
	return requestAcceptsEncoding(r, "br")
}

func requestAcceptsGzip(r *http.Request) bool {
	return requestAcceptsEncoding(r, "gzip")
}

func requestAcceptsEncoding(r *http.Request, encoding string) bool {
	if r == nil {
		return false
	}
	return httpcompress.Accepts(strings.Join(r.Header.Values("Accept-Encoding"), ","), encoding)
}

func setRuntimeContentType(h http.Header, fsPath string) {
	if h.Get("Content-Type") != "" {
		return
	}
	switch strings.ToLower(filepath.Ext(fsPath)) {
	case ".gxi":
		h.Set("Content-Type", "application/octet-stream")
	case ".wasm":
		h.Set("Content-Type", "application/wasm")
	case ".js":
		h.Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		h.Set("Content-Type", "text/css; charset=utf-8")
	case ".json":
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
}

func runtimeManifestDirectAssetPath(root, name string) (string, bool) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/")
	if !strings.HasPrefix(name, "assets/") {
		return "", false
	}
	rel := strings.TrimPrefix(name, "assets/")
	for _, assetsRoot := range []string{
		filepath.Join(root, "assets"),
		filepath.Join(root, "dist", "assets"),
	} {
		target, ok := safeArtifactPath(assetsRoot, rel)
		if ok && isFile(target) {
			return target, true
		}
	}
	return "", false
}

func runtimeCompatSourcePath(root, name string) (string, bool) {
	buildDir := filepath.Join(root, "build")
	candidates := map[string]string{
		"runtime.wasm":                                   filepath.Join(buildDir, "gosx-runtime.wasm"),
		"runtime-islands.wasm":                           filepath.Join(buildDir, "gosx-runtime-islands.wasm"),
		"runtime-core.wasm":                              filepath.Join(buildDir, "gosx-runtime-core.wasm"),
		"runtime-engine.wasm":                            filepath.Join(buildDir, "gosx-runtime-engine.wasm"),
		"runtime-collab.wasm":                            filepath.Join(buildDir, "gosx-runtime-collab.wasm"),
		"wasm_exec.js":                                   filepath.Join(buildDir, "wasm_exec.js"),
		"standard-go-wasm_exec.js":                       filepath.Join(buildDir, "standard-go-wasm_exec.js"),
		"bootstrap.js":                                   filepath.Join(buildDir, "bootstrap.js"),
		"bootstrap-lite.js":                              filepath.Join(buildDir, "bootstrap-lite.js"),
		"bootstrap-runtime.js":                           filepath.Join(buildDir, "bootstrap-runtime.js"),
		"bootstrap-feature-islands.js":                   filepath.Join(buildDir, "bootstrap-feature-islands.js"),
		"bootstrap-feature-engines.js":                   filepath.Join(buildDir, "bootstrap-feature-engines.js"),
		"bootstrap-feature-hubs.js":                      filepath.Join(buildDir, "bootstrap-feature-hubs.js"),
		"bootstrap-feature-controllers.js":               filepath.Join(buildDir, "bootstrap-feature-controllers.js"),
		"bootstrap-controller-input.js":                  filepath.Join(buildDir, "bootstrap-controller-input.js"),
		"bootstrap-feature-textlayout.js":                filepath.Join(buildDir, "bootstrap-feature-textlayout.js"),
		"bootstrap-feature-scene3d.js":                   filepath.Join(buildDir, "bootstrap-feature-scene3d.js"),
		"bootstrap-feature-scene3d-command.js":           filepath.Join(buildDir, "bootstrap-feature-scene3d-command.js"),
		"bootstrap-feature-scene3d-hydrate.js":           filepath.Join(buildDir, "bootstrap-feature-scene3d-hydrate.js"),
		"bootstrap-feature-scene3d-pipeline-recovery.js": filepath.Join(buildDir, "bootstrap-feature-scene3d-pipeline-recovery.js"),
		"bootstrap-feature-scene3d-instance-stream.js":   filepath.Join(buildDir, "bootstrap-feature-scene3d-instance-stream.js"),
		"patch.js":         filepath.Join(buildDir, "patch.js"),
		"hls.min.js":       filepath.Join(buildDir, "hls.min.js"),
		"stripe-bridge.js": filepath.Join(buildDir, "stripe-bridge.js"),
		"relay.js":         filepath.Join(buildDir, "relay.js"),
	}
	if direct, ok := candidates[name]; ok && isFile(direct) {
		return direct, true
	}
	if strings.HasPrefix(name, "bootstrap") || name == "patch.js" {
		// Dev staging copies feature chunks (including the scene3d split
		// chunks not listed in `candidates`) into the app build dir; serve
		// them from there first so freshly staged chunks resolve.
		if buildPath := filepath.Join(buildDir, filepath.FromSlash(name)); isFile(buildPath) {
			return buildPath, true
		}
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
	if name == "stripe-bridge.js" {
		clientPath := filepath.Join(root, "client", "js", name)
		if isFile(clientPath) {
			return clientPath, true
		}
	}
	if name == "relay.js" {
		clientPath := filepath.Join(root, "client", "js", name)
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

// runtimeCompatBuiltPath maps an unhashed /gosx/ runtime name to its
// content-hashed build output. It reads build.json from the runtime root, or
// from root/dist when the app runs from its project root: the same order the
// island renderer uses to pick hashed URLs, so every URL a page can fall back
// to resolves through the manifest the page was rendered from.
func (a *App) runtimeCompatBuiltPath(root, name string) (string, bool) {
	if !isFile(filepath.Join(root, "build.json")) {
		if distRoot := filepath.Join(root, "dist"); isFile(filepath.Join(distRoot, "build.json")) {
			root = distRoot
		}
	}
	manifest, ok := a.runtimeBuildManifest(root)
	if !ok {
		return "", false
	}
	assetsDir := filepath.Join(root, "assets")

	switch name {
	case "runtime.wasm":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASM.File)
	case "runtime-islands.wasm":
		if strings.TrimSpace(manifest.Runtime.WASMIslands.File) == "" {
			return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASM.File)
		}
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASMIslands.File)
	case "runtime-core.wasm":
		return runtimeManifestVariantAssetPath(assetsDir, manifest, "core")
	case "runtime-engine.wasm":
		return runtimeManifestVariantAssetPath(assetsDir, manifest, "engine")
	case "runtime-collab.wasm":
		return runtimeManifestVariantAssetPath(assetsDir, manifest, "collab")
	case "wasm_exec.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.WASMExec.File)
	case "standard-go-wasm_exec.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.StandardGoWASMExec.File)
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
	case "bootstrap-feature-controllers.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureControllers.File)
	case "bootstrap-controller-input.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapControllerInput.File)
	case "bootstrap-feature-textlayout.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureTextlayout.File)
	case "bootstrap-feature-scene3d.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3D.File)
	case "bootstrap-feature-scene3d-command.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DCommand.File)
	case "bootstrap-feature-scene3d-hydrate.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DHydrate.File)
	case "bootstrap-feature-scene3d-pipeline-recovery.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DPipelineRecovery.File)
	case "bootstrap-feature-scene3d-webgpu.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DWebGPU.File)
	case "bootstrap-feature-scene3d-webgl.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DWebGL.File)
	case "bootstrap-feature-scene3d-gltf.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DGLTF.File)
	case "bootstrap-feature-scene3d-animation.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DAnimation.File)
	case "bootstrap-feature-scene3d-compute.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DCompute.File)
	case "bootstrap-feature-scene3d-walk.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DWalk.File)
	case "bootstrap-feature-scene3d-particle-burst.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DParticleBurst.File)
	case "bootstrap-feature-scene3d-zoom.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DZoom.File)
	case "bootstrap-feature-scene3d-timeline.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DTimeline.File)
	case "bootstrap-feature-scene3d-vessel.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DVessel.File)
	case "bootstrap-feature-scene3d-ocean-query.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DOceanQuery.File)
	case "bootstrap-feature-scene3d-decompress.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DDecompress.File)
	case "bootstrap-feature-scene3d-instance-stream.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.BootstrapFeatureScene3DInstanceStream.File)
	case "patch.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Patch.File)
	case "hls.min.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.VideoHLS.File)
	case "stripe-bridge.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.StripeBridge.File)
	case "relay.js":
		return runtimeManifestAssetPath(assetsDir, "runtime", manifest.Runtime.Relay.File)
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

func runtimeManifestVariantAssetPath(assetsDir string, manifest *buildmanifest.Manifest, variant string) (string, bool) {
	if manifest == nil {
		return "", false
	}
	asset, ok := manifest.Runtime.WASMVariants[strings.TrimSpace(variant)]
	if !ok {
		return "", false
	}
	return runtimeManifestAssetPath(assetsDir, "runtime", asset.File)
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

// runtimeMetaInitMu guards the lazy allocation of App.runtimeMeta. Without
// this lock, two concurrent requests can each see a nil cache, each allocate
// their own *runtimeManifestCache, and each lock a different mutex — so the
// per-cache mutex protects nothing. This lock only covers the short
// check-and-set of the pointer; the cache's own mutex still guards the
// manifest read below.
var runtimeMetaInitMu sync.Mutex

func (a *App) runtimeBuildManifest(root string) (*buildmanifest.Manifest, bool) {
	if a == nil {
		return nil, false
	}
	manifestPath := filepath.Join(root, "build.json")
	info, err := os.Stat(manifestPath)
	if err != nil || info.IsDir() {
		return nil, false
	}

	runtimeMetaInitMu.Lock()
	if a.runtimeMeta == nil {
		a.runtimeMeta = &runtimeManifestCache{}
	}
	cache := a.runtimeMeta
	runtimeMetaInitMu.Unlock()

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
