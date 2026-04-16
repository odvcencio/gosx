// Package island provides the island rendering and manifest generation system.
//
// Islands are component subtrees that are server-rendered and then hydrated
// on the client. This package handles:
// - Marking components as islands
// - Generating hydration manifests
// - Rendering island wrapper HTML with anchor IDs
// - Serializing props for client delivery
package island

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/buildmanifest"
	"github.com/odvcencio/gosx/client/vm"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/island/program"
)

// Renderer handles island-aware rendering of GoSX component trees.
type Renderer struct {
	manifest                             *hydrate.Manifest
	counter                              int
	bundleID                             string
	programDir                           string // directory where island programs are stored
	programFormat                        string // "json" or "bin"
	programAssets                        map[string]programAsset
	wasmExecPath                         string
	patchPath                            string
	bootstrapPath                        string
	bootstrapLitePath                    string
	bootstrapRuntimePath                 string
	bootstrapFeatureIslandsPath          string
	bootstrapFeatureEnginesPath          string
	bootstrapFeatureHubsPath             string
	bootstrapFeatureScene3dPath          string
	bootstrapFeatureScene3dWebGPUPath    string
	bootstrapFeatureScene3dGLTFPath      string
	bootstrapFeatureScene3dAnimationPath string
	videoHLSPath                         string
	islandRuntime                        hydrate.RuntimeRef
	runtimeAssets                        buildmanifest.RuntimeAssets
	bootstrapOnly                        bool
}

type programAsset struct {
	path   string
	format string
	hash   string
}

// Summary describes the client bootstrap/runtime surface required by a page.
type Summary struct {
	Bootstrap                   bool
	BootstrapMode               string
	Manifest                    bool
	RuntimePath                 string
	WASMExecPath                string
	PatchPath                   string
	BootstrapPath               string
	BootstrapFeatureIslandsPath string
	BootstrapFeatureEnginesPath string
	BootstrapFeatureHubsPath    string
	BootstrapFeatureScene3DPath string
	HLSPath                     string
	Islands                     int
	Engines                     int
	Hubs                        int
}

func enhancementKindForEngine(cfg engine.Config) string {
	if cfg.Kind == engine.KindVideo {
		return "video"
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Name), "GoSXScene3D") {
		return "scene3d"
	}
	return "engine"
}

func enhancementFallbackForNode(node gosx.Node, fallback string) string {
	if node.IsZero() {
		return fallback
	}
	return "server"
}

// NewRenderer creates an island renderer.
func NewRenderer(bundleID string) *Renderer {
	runtimeAssets := loadDefaultRuntimeAssets()
	renderer := &Renderer{
		manifest:      hydrate.NewManifest(),
		bundleID:      bundleID,
		programFormat: "json", // default to dev mode
		programAssets: make(map[string]programAsset),
		runtimeAssets: runtimeAssets,
	}
	renderer.wasmExecPath = renderer.versionCompatRuntimePath("/gosx/wasm_exec.js", strings.TrimSpace(runtimeAssets.WASMExec.Hash))
	renderer.patchPath = renderer.versionCompatRuntimePath("/gosx/patch.js", strings.TrimSpace(runtimeAssets.Patch.Hash))
	renderer.bootstrapPath = renderer.versionCompatRuntimePath("/gosx/bootstrap.js", strings.TrimSpace(runtimeAssets.Bootstrap.Hash))
	renderer.bootstrapLitePath = renderer.versionCompatRuntimePath("/gosx/bootstrap-lite.js", strings.TrimSpace(runtimeAssets.BootstrapLite.Hash))
	renderer.bootstrapRuntimePath = renderer.versionCompatRuntimePath("/gosx/bootstrap-runtime.js", strings.TrimSpace(runtimeAssets.BootstrapRuntime.Hash))
	renderer.bootstrapFeatureIslandsPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-islands.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureIslands.Hash))
	renderer.bootstrapFeatureEnginesPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-engines.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureEngines.Hash))
	renderer.bootstrapFeatureHubsPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-hubs.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureHubs.Hash))
	renderer.bootstrapFeatureScene3dPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-scene3d.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureScene3D.Hash))
	renderer.bootstrapFeatureScene3dWebGPUPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-scene3d-webgpu.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureScene3DWebGPU.Hash))
	renderer.bootstrapFeatureScene3dGLTFPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-scene3d-gltf.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureScene3DGLTF.Hash))
	renderer.bootstrapFeatureScene3dAnimationPath = renderer.versionCompatRuntimePath("/gosx/bootstrap-feature-scene3d-animation.js", strings.TrimSpace(runtimeAssets.BootstrapFeatureScene3DAnimation.Hash))
	renderer.videoHLSPath = renderer.versionCompatRuntimePath("/gosx/hls.min.js", strings.TrimSpace(runtimeAssets.VideoHLS.Hash))
	if manifest := loadDefaultBuildManifest(); manifest != nil {
		_ = renderer.ApplyBuildManifest(manifest, "/gosx/assets")
	}
	return renderer
}

func loadDefaultRuntimeAssets() buildmanifest.RuntimeAssets {
	manifest := loadDefaultBuildManifest()
	if manifest != nil {
		return manifest.Runtime
	}
	return buildmanifest.RuntimeAssets{}
}

var (
	manifestRootMu  sync.RWMutex
	manifestRoot    string
	manifestRootSet bool
)

// SetManifestRoot configures the directory where loadDefaultBuildManifest
// searches for build.json. When set (by the server package's SetRuntimeRoot),
// this overrides the default app-CWD search — the renderer's manifest lookup
// stays aligned with the file-serving root. Passing an empty string still
// counts as "explicitly set" and disables manifest loading, causing the
// renderer to emit plain /gosx/* URLs that route through the source fallback.
func SetManifestRoot(root string) {
	manifestRootMu.Lock()
	defer manifestRootMu.Unlock()
	manifestRoot = strings.TrimSpace(root)
	manifestRootSet = true
}

// ResetManifestRoot clears the override and restores legacy app-CWD lookup.
// Intended for test teardown.
func ResetManifestRoot() {
	manifestRootMu.Lock()
	defer manifestRootMu.Unlock()
	manifestRoot = ""
	manifestRootSet = false
}

func loadDefaultBuildManifest() *buildmanifest.Manifest {
	manifestRootMu.RLock()
	root := manifestRoot
	explicit := manifestRootSet
	manifestRootMu.RUnlock()

	if !explicit {
		// Legacy fallback: search the app CWD. Preserved for backward
		// compatibility with tests and callers that haven't adopted
		// SetManifestRoot.
		root = strings.TrimSpace(os.Getenv("GOSX_APP_ROOT"))
		if root == "" {
			wd, err := os.Getwd()
			if err != nil {
				return nil
			}
			root = wd
		}
	}

	if root == "" {
		return nil
	}

	for _, candidate := range []string{
		filepath.Join(root, "build.json"),
		filepath.Join(root, "dist", "build.json"),
	} {
		manifest, err := buildmanifest.Load(candidate)
		if err == nil && manifest != nil {
			return manifest
		}
	}
	return nil
}

// SetProgramDir sets the directory where island programs are stored.
func (r *Renderer) SetProgramDir(dir string) {
	r.programDir = dir
}

// SetProgramFormat sets the program format ("json" or "bin").
func (r *Renderer) SetProgramFormat(format string) {
	r.programFormat = format
}

// SetProgramAsset registers an exact program asset for a component.
// Use this when build output is content-hashed and can't be inferred from name.
func (r *Renderer) SetProgramAsset(componentName, path, format, hash string) {
	if format == "" {
		format = r.programFormat
	}
	r.programAssets[componentName] = programAsset{
		path:   path,
		format: format,
		hash:   hash,
	}
}

// SetRuntime registers the shared WASM runtime in the manifest.
func (r *Renderer) SetRuntime(path string, hash string, size int64) {
	if hash == "" {
		hash = r.compatRuntimeHash(path)
	}
	path = r.versionCompatRuntimePath(path, hash)
	r.manifest.Runtime = hydrate.RuntimeRef{
		Path: path,
		Hash: hash,
		Size: size,
	}
}

// SetIslandRuntime registers the smaller shared WASM runtime used by pages
// that hydrate islands but do not need the shared engine bridge.
func (r *Renderer) SetIslandRuntime(path string, hash string, size int64) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if hash == "" {
		hash = r.compatRuntimeHash(path)
	}
	path = r.versionCompatRuntimePath(path, hash)
	r.islandRuntime = hydrate.RuntimeRef{
		Path: path,
		Hash: hash,
		Size: size,
	}
}

// SetBundle registers a WASM bundle in the manifest.
func (r *Renderer) SetBundle(id string, path string) {
	r.manifest.Bundles[id] = hydrate.BundleRef{Path: path}
}

// SetClientAssetPaths overrides the default runtime script URLs used in PageHead.
func (r *Renderer) SetClientAssetPaths(wasmExecPath, patchPath, bootstrapPath string) {
	if wasmExecPath != "" {
		r.wasmExecPath = r.versionCompatRuntimePath(wasmExecPath, r.compatRuntimeHash(wasmExecPath))
	}
	if patchPath != "" {
		r.patchPath = r.versionCompatRuntimePath(patchPath, r.compatRuntimeHash(patchPath))
	}
	if bootstrapPath != "" {
		r.bootstrapPath = r.versionCompatRuntimePath(bootstrapPath, r.compatRuntimeHash(bootstrapPath))
	}
}

// SetBootstrapLitePath overrides the bootstrap-only runtime script URL used on
// pages that only need document/presentation/text-layout enhancement.
func (r *Renderer) SetBootstrapLitePath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.bootstrapLitePath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

// SetBootstrapRuntimePath overrides the selective runtime bootstrap script URL
// used on pages that need runtime features but not the Scene3D engine bundle.
func (r *Renderer) SetBootstrapRuntimePath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.bootstrapRuntimePath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

// SetBootstrapFeaturePaths overrides the selective runtime feature chunk URLs.
func (r *Renderer) SetBootstrapFeaturePaths(islandsPath, enginesPath, hubsPath string) {
	if strings.TrimSpace(islandsPath) != "" {
		r.bootstrapFeatureIslandsPath = r.versionCompatRuntimePath(islandsPath, r.compatRuntimeHash(islandsPath))
	}
	if strings.TrimSpace(enginesPath) != "" {
		r.bootstrapFeatureEnginesPath = r.versionCompatRuntimePath(enginesPath, r.compatRuntimeHash(enginesPath))
	}
	if strings.TrimSpace(hubsPath) != "" {
		r.bootstrapFeatureHubsPath = r.versionCompatRuntimePath(hubsPath, r.compatRuntimeHash(hubsPath))
	}
}

// SetBootstrapFeatureScene3DPath overrides the async Scene3D feature chunk URL.
func (r *Renderer) SetBootstrapFeatureScene3DPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.bootstrapFeatureScene3dPath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

// SetBootstrapFeatureScene3DWebGPUPath overrides the async Scene3D WebGPU
// sub-feature chunk URL. Emitted inline after the main scene3d script tag,
// gated on navigator.gpu so non-WebGPU browsers skip the download.
func (r *Renderer) SetBootstrapFeatureScene3DWebGPUPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.bootstrapFeatureScene3dWebGPUPath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

// SetBootstrapFeatureScene3DGLTFPath overrides the lazy Scene3D GLTF
// sub-feature chunk URL. NOT emitted as a static <script> tag — the
// main scene3d bundle's loadSceneModelAsset() helper dynamically
// inserts a <script> with this URL the first time a .glb/.gltf model
// is requested. Pages that never load models never fetch the chunk.
func (r *Renderer) SetBootstrapFeatureScene3DGLTFPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.bootstrapFeatureScene3dGLTFPath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

// SetBootstrapFeatureScene3DAnimationPath overrides the lazy Scene3D
// animation mixer sub-feature chunk URL. Consumers that want to drive
// keyframe animations or skeletal clips can fetch this chunk via
// window.__gosx_scene3d_animation_api.createMixer(). Pages without
// animations never fetch the chunk.
func (r *Renderer) SetBootstrapFeatureScene3DAnimationPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.bootstrapFeatureScene3dAnimationPath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

// SetVideoHLSPath overrides the runtime HLS library URL used by the built-in
// video engine when native HLS playback is unavailable.
func (r *Renderer) SetVideoHLSPath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	r.videoHLSPath = r.versionCompatRuntimePath(path, r.compatRuntimeHash(path))
}

func (r *Renderer) compatRuntimeHash(path string) string {
	switch compatRuntimePath(path) {
	case "/gosx/runtime.wasm":
		return strings.TrimSpace(r.runtimeAssets.WASM.Hash)
	case "/gosx/runtime-islands.wasm":
		return strings.TrimSpace(r.runtimeAssets.WASMIslands.Hash)
	case "/gosx/wasm_exec.js":
		return strings.TrimSpace(r.runtimeAssets.WASMExec.Hash)
	case "/gosx/bootstrap.js":
		return strings.TrimSpace(r.runtimeAssets.Bootstrap.Hash)
	case "/gosx/bootstrap-lite.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapLite.Hash)
	case "/gosx/bootstrap-runtime.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapRuntime.Hash)
	case "/gosx/bootstrap-feature-islands.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureIslands.Hash)
	case "/gosx/bootstrap-feature-engines.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureEngines.Hash)
	case "/gosx/bootstrap-feature-hubs.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureHubs.Hash)
	case "/gosx/bootstrap-feature-scene3d.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureScene3D.Hash)
	case "/gosx/bootstrap-feature-scene3d-webgpu.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureScene3DWebGPU.Hash)
	case "/gosx/bootstrap-feature-scene3d-gltf.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureScene3DGLTF.Hash)
	case "/gosx/bootstrap-feature-scene3d-animation.js":
		return strings.TrimSpace(r.runtimeAssets.BootstrapFeatureScene3DAnimation.Hash)
	case "/gosx/patch.js":
		return strings.TrimSpace(r.runtimeAssets.Patch.Hash)
	case "/gosx/hls.min.js":
		return strings.TrimSpace(r.runtimeAssets.VideoHLS.Hash)
	default:
		return ""
	}
}

func (r *Renderer) versionCompatRuntimePath(path, hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return path
	}
	parsed, err := neturl.Parse(path)
	if err != nil || parsed == nil || parsed.Scheme != "" || parsed.Host != "" {
		return path
	}
	switch compatRuntimePath(path) {
	case "/gosx/runtime.wasm", "/gosx/runtime-islands.wasm", "/gosx/wasm_exec.js", "/gosx/bootstrap.js", "/gosx/bootstrap-lite.js", "/gosx/bootstrap-runtime.js", "/gosx/bootstrap-feature-islands.js", "/gosx/bootstrap-feature-engines.js", "/gosx/bootstrap-feature-hubs.js", "/gosx/bootstrap-feature-scene3d.js", "/gosx/bootstrap-feature-scene3d-webgpu.js", "/gosx/bootstrap-feature-scene3d-gltf.js", "/gosx/bootstrap-feature-scene3d-animation.js", "/gosx/patch.js", "/gosx/hls.min.js":
		query := parsed.Query()
		if query.Get("v") == "" {
			query.Set("v", hash)
			parsed.RawQuery = query.Encode()
		}
		return parsed.String()
	default:
		return path
	}
}

func compatRuntimePath(path string) string {
	parsed, err := neturl.Parse(path)
	if err != nil || parsed == nil {
		return strings.TrimSpace(path)
	}
	return strings.TrimSpace(parsed.Path)
}

// ApplyBuildManifest wires hashed runtime and island asset URLs into the renderer.
// assetBaseURL should be the public URL prefix that serves dist/assets.
func (r *Renderer) ApplyBuildManifest(manifest *buildmanifest.Manifest, assetBaseURL string) error {
	if manifest == nil {
		return fmt.Errorf("build manifest is nil")
	}

	runtime := manifest.RuntimeURLs(assetBaseURL)
	if runtime.WASM != "" {
		r.SetRuntime(runtime.WASM, manifest.Runtime.WASM.Hash, manifest.Runtime.WASM.Size)
		r.SetBundle(r.bundleID, runtime.WASM)
	}
	if runtime.WASMIslands != "" {
		r.SetIslandRuntime(runtime.WASMIslands, manifest.Runtime.WASMIslands.Hash, manifest.Runtime.WASMIslands.Size)
	}
	r.SetClientAssetPaths(runtime.WASMExec, runtime.Patch, runtime.Bootstrap)
	r.SetBootstrapLitePath(runtime.BootstrapLite)
	r.SetBootstrapRuntimePath(runtime.BootstrapRuntime)
	r.SetBootstrapFeaturePaths(runtime.BootstrapFeatureIslands, runtime.BootstrapFeatureEngines, runtime.BootstrapFeatureHubs)
	r.SetBootstrapFeatureScene3DPath(runtime.BootstrapFeatureScene3D)
	r.SetBootstrapFeatureScene3DWebGPUPath(runtime.BootstrapFeatureScene3DWebGPU)
	r.SetBootstrapFeatureScene3DGLTFPath(runtime.BootstrapFeatureScene3DGLTF)
	r.SetBootstrapFeatureScene3DAnimationPath(runtime.BootstrapFeatureScene3DAnimation)
	r.SetVideoHLSPath(runtime.VideoHLS)

	for _, asset := range manifest.Islands {
		r.SetProgramAsset(asset.Name, manifest.IslandURL(assetBaseURL, asset), asset.Format, asset.Hash)
	}

	return nil
}

// LoadBuildManifest reads a build manifest from disk and applies its asset URLs.
func (r *Renderer) LoadBuildManifest(path, assetBaseURL string) error {
	manifest, err := buildmanifest.Load(path)
	if err != nil {
		return err
	}
	return r.ApplyBuildManifest(manifest, assetBaseURL)
}

// RenderIsland wraps a component in an island container and registers it in the manifest.
func (r *Renderer) RenderIsland(componentName string, props any, content gosx.Node) gosx.Node {
	id, err := r.manifest.AddIsland(componentName, r.bundleID, props)
	if err != nil {
		return gosx.El("div",
			gosx.Attrs(gosx.Attr("class", "gosx-error")),
			gosx.Text(fmt.Sprintf("island error: %v", err)),
		)
	}

	// Set program ref fields on the new entry
	lastIdx := len(r.manifest.Islands) - 1
	r.applyProgramRef(&r.manifest.Islands[lastIdx], componentName)

	r.counter++

	// Wrap content in an island root div
	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("data-gosx-island", componentName),
			gosx.Attr("data-gosx-enhance", "island"),
			gosx.Attr("data-gosx-enhance-layer", "runtime"),
			gosx.Attr("data-gosx-fallback", enhancementFallbackForNode(content, "none")),
		),
		content,
	)
}

// RenderIslandWithEvents wraps a component with event bindings.
func (r *Renderer) RenderIslandWithEvents(componentName string, props any, events []hydrate.EventSlot, content gosx.Node) gosx.Node {
	id, err := r.manifest.AddIsland(componentName, r.bundleID, props)
	if err != nil {
		return gosx.El("div", gosx.Text(fmt.Sprintf("island error: %v", err)))
	}

	// Add events and program ref to the last island entry
	lastIdx := len(r.manifest.Islands) - 1
	r.manifest.Islands[lastIdx].Events = events
	r.applyProgramRef(&r.manifest.Islands[lastIdx], componentName)

	r.counter++

	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("data-gosx-island", componentName),
			gosx.Attr("data-gosx-enhance", "island"),
			gosx.Attr("data-gosx-enhance-layer", "runtime"),
			gosx.Attr("data-gosx-fallback", enhancementFallbackForNode(content, "none")),
		),
		content,
	)
}

// Manifest returns the generated hydration manifest.
func (r *Renderer) Manifest() *hydrate.Manifest {
	return r.manifest
}

func (r *Renderer) clientManifest() *hydrate.Manifest {
	if r == nil || r.manifest == nil {
		return nil
	}
	manifest := *r.manifest
	if len(r.manifest.Bundles) > 0 {
		manifest.Bundles = make(map[string]hydrate.BundleRef, len(r.manifest.Bundles))
		for id, bundle := range r.manifest.Bundles {
			manifest.Bundles[id] = bundle
		}
	}
	if !r.needsSharedRuntime() {
		manifest.Runtime = hydrate.RuntimeRef{}
	} else {
		manifest.Runtime = r.selectedRuntimeRef()
		if manifest.Bundles != nil && r.bundleID != "" && manifest.Runtime.Path != "" {
			manifest.Bundles[r.bundleID] = hydrate.BundleRef{
				Path: manifest.Runtime.Path,
				Hash: manifest.Runtime.Hash,
				Size: manifest.Runtime.Size,
			}
		}
	}
	return &manifest
}

// ManifestJSON returns the manifest as a JSON string.
func (r *Renderer) ManifestJSON() (string, error) {
	manifest := r.clientManifest()
	if manifest == nil {
		return "", nil
	}
	data, err := manifest.Marshal()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ManifestScript returns an HTML script tag containing the manifest.
func (r *Renderer) ManifestScript() gosx.Node {
	manifest := r.clientManifest()
	if manifest == nil {
		return gosx.Text("")
	}
	data, err := manifest.Marshal()
	if err != nil {
		return gosx.Text("")
	}
	return gosx.RawHTML(fmt.Sprintf(
		`<script id="gosx-manifest" type="application/json">%s</script>`,
		string(data),
	))
}

// BootstrapScript returns the script tags needed for island hydration.
func (r *Renderer) BootstrapScript() gosx.Node {
	if !r.needsClientBootstrap() {
		return gosx.Text("")
	}

	var b strings.Builder
	mode := "full"
	if r.needsLiteBootstrap() {
		mode = "lite"
	}
	if (len(r.manifest.Islands) > 0 || r.hasWASMEngines() || r.needsSharedRuntimeEngineBridge()) && r.wasmExecPath != "" {
		b.WriteString(fmt.Sprintf(`<script data-gosx-script="wasm-exec" src="%s"></script>`, html.EscapeString(r.wasmExecPath)))
		b.WriteByte('\n')
	}
	if len(r.manifest.Islands) > 0 && r.patchPath != "" {
		b.WriteString(fmt.Sprintf(`<script data-gosx-script="patch" src="%s"></script>`, html.EscapeString(r.patchPath)))
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf(`<script data-gosx-script="bootstrap" data-gosx-bootstrap-mode="%s" src="%s"></script>`, mode, html.EscapeString(r.selectedBootstrapPath())))
	if scene3dPath := r.selectedBootstrapFeaturePath("scene3d"); scene3dPath != "" {
		b.WriteByte('\n')
		b.WriteString(`<script defer data-gosx-script="feature-scene3d"`)
		// Embed hashed URLs for lazy sub-feature chunks as data-* attributes
		// so the main scene3d bundle can use immutable hashed URLs at first
		// dynamic-load time instead of the unhashed compat URL. Lets the
		// browser cache the sub-feature forever, keyed on its content hash.
		if gltfPath := r.bootstrapFeatureScene3dGLTFPath; gltfPath != "" {
			b.WriteString(` data-gosx-scene3d-gltf-url="`)
			b.WriteString(html.EscapeString(gltfPath))
			b.WriteByte('"')
		}
		if animPath := r.bootstrapFeatureScene3dAnimationPath; animPath != "" {
			b.WriteString(` data-gosx-scene3d-animation-url="`)
			b.WriteString(html.EscapeString(animPath))
			b.WriteByte('"')
		}
		b.WriteString(` src="`)
		b.WriteString(html.EscapeString(scene3dPath))
		b.WriteString("\">\x3c/script>")

		// WebGPU sub-feature: lazy-load only when navigator.gpu exists.
		// Safari and Firefox-on-most-platforms skip the download
		// entirely, saving ~55KB gzip / 120KB raw per load. Chromium
		// browsers fetch it in parallel with the main scene3d chunk so
		// the scene mount can pick webgpu on the first render. Inlined
		// rather than added to a manifest loader to keep the HTML byte
		// cost minimal — the script body is ~190 bytes before gzip.
		if webgpuPath := r.selectedBootstrapFeaturePath("scene3d-webgpu"); webgpuPath != "" {
			b.WriteByte('\n')
			// Gate the sub-chunk load behind DOMContentLoaded so it runs
			// AFTER the parser-inserted defer scripts (which include the
			// main bootstrap-feature-scene3d.js). Trying s.async = false
			// on the dynamically-inserted script element doesn't work
			// reliably in every Chromium build — the sub-chunk still
			// races the main bundle and usually wins, firing the
			// IIFE's early-return guard because __gosx_scene3d_api
			// isn't defined yet. Waiting for DCL sidesteps the ordering
			// problem entirely: by the time DCL fires, all parser-
			// inserted defer scripts have already executed, so the main
			// scene3d bundle has published its API.
			//
			// NB: Go raw-string (backticks) doesn't process \x escapes,
			// so the closing </script> must come from a double-quoted
			// string — hence the split. Historical bug from v0.17.16:
			// using `\x3c/script>` inside a raw-string literal produced
			// a literal "\x3c/script>" in the HTML, which JS parsed as
			// a syntax error, silently dropping the entire inline loader.
			b.WriteString(`<script>if(navigator.gpu){var _w=function(){var s=document.createElement('script');s.async=false;s.dataset.gosxScript='feature-scene3d-webgpu';s.src=`)
			b.WriteString(htmlJSStringLiteral(webgpuPath))
			b.WriteString(";document.head.appendChild(s);};")
			b.WriteString("if(document.readyState==='loading'){document.addEventListener('DOMContentLoaded',_w);}else{_w();}}")
			b.WriteString("\x3c/script>")
		}
	}
	return gosx.RawHTML(b.String())
}

// htmlJSStringLiteral returns a JS string literal (including surrounding
// quotes) safe to embed inside an inline <script> tag. Uses JSON encoding
// plus a </script> guard so the result can sit inside an HTML context
// without being terminated early by a literal </script> in the payload.
func htmlJSStringLiteral(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	// HTML spec forbids the literal string "</" inside a script element
	// if it starts a closing tag; escape the forward slash to be safe.
	out := strings.ReplaceAll(string(b), "</", `<\/`)
	return out
}

// RenderEngine registers an engine instance in the hydration manifest and,
// for surface engines, emits a mount element the client runtime can attach to.
// The optional JS runtime metadata is escape-hatch only; built-in engines can
// rely on factories registered directly on the shared bootstrap runtime.
func (r *Renderer) RenderEngine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if cfg.Name == "" {
		return renderEngineError(fmt.Errorf("name required"))
	}
	if cfg.Kind == "" {
		return renderEngineError(fmt.Errorf("kind required"))
	}
	if !engine.KindSupported(cfg.Kind) {
		return renderEngineError(fmt.Errorf("unsupported engine kind: %q", cfg.Kind))
	}
	if err := engine.ValidateCapabilities(cfg.Capabilities); err != nil {
		return renderEngineError(err)
	}

	props, err := engineProps(cfg.Props)
	if err != nil {
		return renderEngineError(err)
	}

	mountID := cfg.MountID
	if engine.KindNeedsMount(cfg.Kind) && mountID == "" {
		mountID = fmt.Sprintf("gosx-engine-mount-%d", len(r.manifest.Engines))
	}

	id, err := r.manifest.AddEngineWithRuntime(
		cfg.Name,
		string(cfg.Kind),
		cfg.WASMPath,
		mountID,
		string(cfg.Runtime),
		props,
		engineCapabilities(cfg.Capabilities),
		cfg.PixelSurface,
	)
	if err != nil {
		return renderEngineError(err)
	}

	if !engine.KindNeedsMount(cfg.Kind) {
		return gosx.Text("")
	}

	attrs := []any{
		gosx.Attr("id", mountID),
		gosx.Attr("data-gosx-engine", cfg.Name),
		gosx.Attr("data-gosx-engine-id", id),
		gosx.Attr("data-gosx-engine-kind", string(cfg.Kind)),
		gosx.Attr("data-gosx-enhance", enhancementKindForEngine(cfg)),
		gosx.Attr("data-gosx-enhance-layer", "runtime"),
		gosx.Attr("data-gosx-fallback", enhancementFallbackForNode(fallback, "none")),
	}
	for name, value := range cfg.MountAttrs {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		switch name {
		case "id", "data-gosx-engine", "data-gosx-engine-id", "data-gosx-engine-kind", "data-gosx-engine-capabilities":
			continue
		}
		attrs = append(attrs, gosx.Attr(name, value))
	}
	if len(cfg.Capabilities) > 0 {
		attrs = append(attrs, gosx.Attr("data-gosx-engine-capabilities", strings.Join(engineCapabilities(cfg.Capabilities), " ")))
	}
	if ps := cfg.PixelSurface; ps != nil {
		attrs = append(attrs,
			gosx.Attr("data-gosx-pixel-width", fmt.Sprint(ps.Width)),
			gosx.Attr("data-gosx-pixel-height", fmt.Sprint(ps.Height)),
		)
		if ps.Scaling != "" {
			attrs = append(attrs, gosx.Attr("data-gosx-pixel-scaling", string(ps.Scaling)))
		}
	}

	args := []any{gosx.Attrs(attrs...)}
	if !fallback.IsZero() {
		args = append(args, fallback)
	}

	return gosx.El("div", args...)
}

// BindHub registers a realtime hub connection that can update shared island
// signals on the client runtime.
func (r *Renderer) BindHub(name, path string, bindings []hydrate.HubBinding) string {
	return r.manifest.AddHub(name, path, bindings)
}

// RenderIslandFromProgram renders an island entirely from its compiled IslandProgram.
// No manual event wiring needed — events are extracted from the program's node tree.
// Server HTML is generated with data-gosx-on-* attributes plus the legacy
// click shorthand data-gosx-handler for compatibility.
//
// This is the fully automatic path: write .gsx → compile → call this → done.
func (r *Renderer) RenderIslandFromProgram(prog *program.Program, props any) gosx.Node {
	// Extract event slots from the program's node tree
	events := extractEventSlots(prog)

	// Generate server-rendered HTML from the program's evaluated initial tree.
	content := renderProgramHTML(prog, props)

	// Register in manifest
	id, err := r.manifest.AddIsland(prog.Name, r.bundleID, props)
	if err != nil {
		return gosx.El("div", gosx.Text(fmt.Sprintf("island error: %v", err)))
	}

	lastIdx := len(r.manifest.Islands) - 1
	r.manifest.Islands[lastIdx].Events = events
	r.applyProgramRef(&r.manifest.Islands[lastIdx], prog.Name)

	r.counter++

	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("id", id),
			gosx.Attr("data-gosx-island", prog.Name),
			gosx.Attr("data-gosx-enhance", "island"),
			gosx.Attr("data-gosx-enhance-layer", "runtime"),
			gosx.Attr("data-gosx-fallback", enhancementFallbackForNode(content, "none")),
		),
		content,
	)
}

// extractEventSlots walks the program's node tree and extracts event bindings.
// Each slot gets a stable path-derived ID and selector relative to the island root.
func extractEventSlots(prog *program.Program) []hydrate.EventSlot {
	if len(prog.Nodes) == 0 {
		return nil
	}

	var slots []hydrate.EventSlot

	var walk func(nodeID program.NodeID, path string)
	walk = func(nodeID program.NodeID, path string) {
		if int(nodeID) >= len(prog.Nodes) {
			return
		}

		node := prog.Nodes[nodeID]
		for _, attr := range node.Attrs {
			if attr.Kind == program.AttrEvent {
				eventType := eventNameToType(attr.Name)
				slots = append(slots, hydrate.EventSlot{
					SlotID:         path + ":" + eventType + ":" + attr.Event,
					EventType:      eventType,
					TargetSelector: `[data-gosx-path="` + path + `"]`,
					HandlerName:    attr.Event,
				})
			}
		}

		for idx, child := range node.Children {
			walk(child, childProgramPath(path, idx))
		}
	}

	walk(prog.Root, "0")
	return slots
}

// eventNameToType converts GSX event attributes to DOM event types.
func eventNameToType(name string) string {
	switch name {
	case "onClick":
		return "click"
	case "onInput":
		return "input"
	case "onChange":
		return "change"
	case "onSubmit":
		return "submit"
	case "onKeyDown":
		return "keydown"
	case "onKeyUp":
		return "keyup"
	case "onFocus":
		return "focus"
	case "onBlur":
		return "blur"
	default:
		// Strip "on" prefix and lowercase
		if len(name) > 2 && name[:2] == "on" {
			return strings.ToLower(name[2:3]) + name[3:]
		}
		return name
	}
}

func renderProgramHTMLWithProps(prog *program.Program, propsJSON json.RawMessage) gosx.Node {
	resolved := vm.ResolveInitialTree(prog, string(propsJSON))
	html := RenderResolvedHTML(prog, resolved)
	return gosx.RawHTML(html)
}

// renderProgramHTML renders an IslandProgram's node tree to server HTML.
// Event attributes are rendered as data-gosx-on-* for delegated dispatch.
func renderProgramHTML(prog *program.Program, props any) gosx.Node {
	if len(prog.Nodes) == 0 {
		return gosx.Text("")
	}
	propsJSON, err := SerializeProps(props)
	if err != nil {
		return gosx.Text("")
	}
	return renderProgramHTMLWithProps(prog, propsJSON)
}

// RenderResolvedHTML renders a program tree from an already-resolved VM tree.
// This is used by the island test harness to assert post-dispatch DOM output.
func RenderResolvedHTML(prog *program.Program, resolved *vm.ResolvedTree) string {
	if resolved == nil || len(resolved.Nodes) == 0 {
		return ""
	}
	return renderResolvedNode(resolved, 0, "0")
}

// renderResolvedNode walks a resolved VM tree and produces its HTML
// representation. Writes directly into a shared builder to avoid the
// per-node string allocation the old implementation incurred via
// fmt.Sprintf for the open-tag attrs and close-tag.
func renderResolvedNode(resolved *vm.ResolvedTree, nodeIdx int, path string) string {
	if resolved == nil || nodeIdx < 0 || nodeIdx >= len(resolved.Nodes) {
		return ""
	}
	var b strings.Builder
	b.Grow(256)
	renderResolvedNodeInto(&b, resolved, nodeIdx, path)
	return b.String()
}

func renderResolvedNodeInto(b *strings.Builder, resolved *vm.ResolvedTree, nodeIdx int, path string) {
	if resolved == nil || nodeIdx < 0 || nodeIdx >= len(resolved.Nodes) {
		return
	}
	node := resolved.Nodes[nodeIdx]

	if node.Tag == "" {
		b.WriteString(html.EscapeString(node.Text))
		return
	}

	safeTag := html.EscapeString(node.Tag)
	b.WriteByte('<')
	b.WriteString(safeTag)

	for _, attr := range renderResolvedAttrs(&node, path) {
		safeName := html.EscapeString(attr.Name)
		if attr.Bool {
			b.WriteByte(' ')
			b.WriteString(safeName)
			continue
		}
		b.WriteByte(' ')
		b.WriteString(safeName)
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(attr.Value))
		b.WriteByte('"')
	}

	b.WriteByte('>')
	if node.Text != "" {
		b.WriteString(html.EscapeString(node.Text))
	}
	for idx, childIdx := range node.Children {
		renderResolvedNodeInto(b, resolved, childIdx, childProgramPath(path, idx))
	}
	b.WriteString("</")
	b.WriteString(safeTag)
	b.WriteByte('>')
}

func renderResolvedAttrs(node *vm.ResolvedNode, path string) []vm.ResolvedAttr {
	if node == nil {
		return nil
	}
	// Fast path: no events → return the static attrs directly (no alloc).
	// Only elements with event handlers need the synthesized data-gosx-on-*
	// markers and the data-gosx-path attribute appended.
	if len(node.Events) == 0 {
		return node.Attrs
	}

	// Pre-size: static attrs + 1 marker per event + 1 extra for each click
	// + 1 for data-gosx-path.
	extra := 1 // data-gosx-path
	for _, event := range node.Events {
		extra++
		if eventNameToType(event.Name) == "click" {
			extra++
		}
	}
	attrs := make([]vm.ResolvedAttr, 0, len(node.Attrs)+extra)
	attrs = append(attrs, node.Attrs...)
	for _, event := range node.Events {
		eventType := eventNameToType(event.Name)
		attrs = append(attrs, vm.ResolvedAttr{
			Name:  "data-gosx-on-" + eventType,
			Value: event.Handler,
		})
		if eventType == "click" {
			attrs = append(attrs, vm.ResolvedAttr{
				Name:  "data-gosx-handler",
				Value: event.Handler,
			})
		}
	}
	attrs = append(attrs, vm.ResolvedAttr{
		Name:  "data-gosx-path",
		Value: path,
	})
	return attrs
}

func (r *Renderer) applyProgramRef(entry *hydrate.IslandEntry, componentName string) {
	if asset, ok := r.programAssets[componentName]; ok {
		entry.ProgramRef = asset.path
		entry.ProgramFormat = asset.format
		entry.ProgramHash = asset.hash
		return
	}

	if r.programDir == "" {
		return
	}

	entry.ProgramFormat = r.programFormat
	entry.ProgramRef = r.programDir + "/" + componentName + programFileExt(r.programFormat)
}

func programFileExt(format string) string {
	if format == "bin" {
		return ".gxi"
	}
	return ".json"
}

func childProgramPath(parent string, idx int) string {
	// Called per child per reconcile node — strconv + direct concat
	// avoids the fmt format-state scratch alloc on the hot path.
	idxStr := strconv.Itoa(idx)
	var b strings.Builder
	b.Grow(len(parent) + 1 + len(idxStr))
	b.WriteString(parent)
	b.WriteByte('/')
	b.WriteString(idxStr)
	return b.String()
}

// PreloadHints returns <link rel="preload"> tags for the HTML <head>.
// These tell the browser to start downloading WASM and island programs
// during HTML parsing, BEFORE the scripts execute — eliminating the
// serial dependency of HTML → JS → fetch WASM.
func (r *Renderer) PreloadHints() gosx.Node {
	if !r.needsClientBootstrap() {
		return gosx.Text("")
	}

	var b strings.Builder

	// Preload the shared WASM runtime only when the page declares islands or a
	// shared-runtime engine bridge.
	if r.needsSharedRuntime() {
		runtime := r.selectedRuntimeRef()
		if runtime.Path != "" {
			b.WriteString(fmt.Sprintf(`<link rel="preload" href="%s" as="fetch" type="application/wasm" crossorigin>`, runtime.Path))
			b.WriteByte('\n')
		}
	}

	// Prefetch island programs — downloaded during WASM compile.
	for _, island := range r.manifest.Islands {
		if island.ProgramRef != "" {
			b.WriteString(fmt.Sprintf(`<link rel="prefetch" href="%s">`, island.ProgramRef))
			b.WriteByte('\n')
		}
	}

	for _, entry := range r.manifest.Engines {
		if entry.ProgramRef != "" {
			if strings.HasSuffix(entry.ProgramRef, ".wasm") {
				b.WriteString(fmt.Sprintf(`<link rel="preload" href="%s" as="fetch" type="application/wasm" crossorigin>`, entry.ProgramRef))
			} else {
				b.WriteString(fmt.Sprintf(`<link rel="prefetch" href="%s">`, entry.ProgramRef))
			}
			b.WriteByte('\n')
		}
	}

	if r.usesSelectiveRuntimeBootstrap() {
		for _, path := range []string{
			r.selectedBootstrapFeaturePath("engines"),
			r.selectedBootstrapFeaturePath("hubs"),
			r.selectedBootstrapFeaturePath("islands"),
			r.selectedBootstrapFeaturePath("scene3d"),
		} {
			if strings.TrimSpace(path) == "" {
				continue
			}
			b.WriteString(fmt.Sprintf(`<link rel="preload" href="%s" as="script">`, path))
			b.WriteByte('\n')
		}
	}

	return gosx.RawHTML(b.String())
}

// PageHead returns all head elements needed for islands on this page.
// Includes preload hints (for <head>) and scripts (for end of <body>).
func (r *Renderer) PageHead() gosx.Node {
	if !r.needsClientBootstrap() {
		return gosx.Text("")
	}
	if r.needsLiteBootstrap() {
		return r.BootstrapScript()
	}
	return gosx.Fragment(
		r.ManifestScript(),
		r.BootstrapScript(),
	)
}

// EnableBootstrap marks the page as needing the shared bootstrap runtime even
// when it does not mount islands, engines, or hubs.
func (r *Renderer) EnableBootstrap() {
	if r == nil {
		return
	}
	r.bootstrapOnly = true
}

// Checksum computes a content hash for cache invalidation.
func Checksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

// SerializeProps converts props to JSON, validating serializability.
func SerializeProps(props any) (json.RawMessage, error) {
	data, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("props not serializable: %w", err)
	}
	return data, nil
}

func (r *Renderer) needsClientBootstrap() bool {
	return r.bootstrapOnly || len(r.manifest.Islands) > 0 || len(r.manifest.Engines) > 0 || len(r.manifest.Hubs) > 0
}

func (r *Renderer) needsLiteBootstrap() bool {
	return r != nil && r.bootstrapOnly && len(r.manifest.Islands) == 0 && len(r.manifest.Engines) == 0 && len(r.manifest.Hubs) == 0
}

// Summary reports the client-facing bootstrap/runtime requirements for the renderer.
func (r *Renderer) Summary() Summary {
	if r == nil {
		return Summary{BootstrapMode: "none"}
	}
	mode := "none"
	if r.needsLiteBootstrap() {
		mode = "lite"
	} else if r.needsClientBootstrap() {
		mode = "full"
	}
	summary := Summary{
		Bootstrap:     r.needsClientBootstrap(),
		BootstrapMode: mode,
		Manifest:      r.needsClientBootstrap() && !r.needsLiteBootstrap(),
		RuntimePath:   r.selectedRuntimePath(),
		WASMExecPath:  r.selectedWASMExecPath(),
		BootstrapPath: r.selectedBootstrapPath(),
		Islands:       len(r.manifest.Islands),
		Engines:       len(r.manifest.Engines),
		Hubs:          len(r.manifest.Hubs),
	}
	if len(r.manifest.Islands) > 0 {
		summary.PatchPath = r.patchPath
	}
	if r.hasVideoEngines() {
		summary.HLSPath = r.videoHLSPath
	}
	if r.usesSelectiveRuntimeBootstrap() {
		summary.BootstrapFeatureIslandsPath = r.selectedBootstrapFeaturePath("islands")
		summary.BootstrapFeatureEnginesPath = r.selectedBootstrapFeaturePath("engines")
		summary.BootstrapFeatureHubsPath = r.selectedBootstrapFeaturePath("hubs")
		summary.BootstrapFeatureScene3DPath = r.selectedBootstrapFeaturePath("scene3d")
	}
	if r.needsLiteBootstrap() {
		summary.PatchPath = ""
		summary.HLSPath = ""
	}
	return summary
}

func (r *Renderer) selectedBootstrapPath() string {
	if r.needsLiteBootstrap() {
		if strings.TrimSpace(r.bootstrapLitePath) != "" {
			return r.bootstrapLitePath
		}
	}
	if r.usesSelectiveRuntimeBootstrap() {
		if strings.TrimSpace(r.bootstrapRuntimePath) != "" {
			return r.bootstrapRuntimePath
		}
	}
	return r.bootstrapPath
}

func (r *Renderer) selectedBootstrapFeaturePath(name string) string {
	if !r.usesSelectiveRuntimeBootstrap() {
		return ""
	}
	switch name {
	case "islands":
		if len(r.manifest.Islands) == 0 {
			return ""
		}
		return r.bootstrapFeatureIslandsPath
	case "engines":
		if len(r.manifest.Engines) == 0 {
			return ""
		}
		return r.bootstrapFeatureEnginesPath
	case "hubs":
		if len(r.manifest.Hubs) == 0 {
			return ""
		}
		return r.bootstrapFeatureHubsPath
	case "scene3d":
		if !r.hasSceneEngines() {
			return ""
		}
		return r.bootstrapFeatureScene3dPath
	case "scene3d-webgpu":
		// Paired with "scene3d" — emitted only when the page has a
		// Scene3D engine AND the WebGPU sub-feature bundle exists.
		// The inline loader in RenderEntrypoints gates the actual
		// download on navigator.gpu so Safari / Firefox skip it.
		if !r.hasSceneEngines() {
			return ""
		}
		return r.bootstrapFeatureScene3dWebGPUPath
	default:
		return ""
	}
}

func (r *Renderer) selectedRuntimePath() string {
	if r == nil || !r.needsSharedRuntime() {
		return ""
	}
	return r.selectedRuntimeRef().Path
}

func (r *Renderer) selectedRuntimeRef() hydrate.RuntimeRef {
	if r == nil {
		return hydrate.RuntimeRef{}
	}
	if len(r.manifest.Islands) > 0 && !r.needsSharedRuntimeEngineBridge() && strings.TrimSpace(r.islandRuntime.Path) != "" {
		return r.islandRuntime
	}
	return r.manifest.Runtime
}

func (r *Renderer) selectedWASMExecPath() string {
	if r == nil {
		return ""
	}
	if len(r.manifest.Islands) > 0 || r.hasWASMEngines() || r.needsSharedRuntimeEngineBridge() {
		return r.wasmExecPath
	}
	return ""
}

func (r *Renderer) usesSelectiveRuntimeBootstrap() bool {
	return r != nil && r.needsClientBootstrap() && !r.needsLiteBootstrap()
}

func (r *Renderer) needsSharedRuntime() bool {
	return r != nil && (len(r.manifest.Islands) > 0 || r.needsSharedRuntimeEngineBridge())
}

func (r *Renderer) hasWASMEngines() bool {
	for _, entry := range r.manifest.Engines {
		if entry.ProgramRef != "" {
			return true
		}
	}
	return false
}

func (r *Renderer) hasVideoEngines() bool {
	for _, entry := range r.manifest.Engines {
		if strings.EqualFold(strings.TrimSpace(entry.Kind), string(engine.KindVideo)) {
			return true
		}
	}
	return false
}

func (r *Renderer) hasSceneEngines() bool {
	for _, entry := range r.manifest.Engines {
		if strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			return true
		}
	}
	return false
}

func (r *Renderer) needsSharedRuntimeEngineBridge() bool {
	for _, entry := range r.manifest.Engines {
		if strings.EqualFold(strings.TrimSpace(entry.Runtime), string(engine.RuntimeShared)) {
			return true
		}
	}
	return false
}

func engineProps(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var props any
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil, fmt.Errorf("invalid engine props JSON: %w", err)
	}
	return props, nil
}

func engineCapabilities(caps []engine.Capability) []string {
	if len(caps) == 0 {
		return nil
	}

	out := make([]string, len(caps))
	for i := range caps {
		out[i] = string(caps[i])
	}
	return out
}

func renderEngineError(err error) gosx.Node {
	return gosx.El("div",
		gosx.Attrs(gosx.Attr("class", "gosx-error")),
		gosx.Text(fmt.Sprintf("engine error: %v", err)),
	)
}
