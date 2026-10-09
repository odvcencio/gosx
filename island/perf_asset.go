package island

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
	"m31labs.dev/gosx/internal/urlpath"
)

// PerfRuntimeFetch records a runtime request observed during a page visit.
// Phase is startup or after-ready; URL must be public and in the verified graph.
type PerfRuntimeFetch struct {
	URL   string
	Phase string
}

// PerfAssetOptions declares resolved browser/document gates and observed calls.
// A nil NavigatorGPU conservatively includes the inline loader's download.
// Model contents and video source/browser state require a non-nil
// RuntimeFetches trace, including an empty trace proving no additional loads.
type PerfAssetOptions struct {
	Backend        string
	Navigation     bool
	NavigatorGPU   *bool
	TextLayout     bool
	RuntimeFetches []PerfRuntimeFetch
}

// PerfAssetUses produces private page-use evidence without adding HTML bytes.
// It preserves dormant artifacts and fails when an actual runtime/program URL
// lacks a verified body. CSS, document ownership and model graphs are measured
// separately; this runtime graph alone never certifies whole-page coverage.
func (r *Renderer) PerfAssetUses(opts PerfAssetOptions) (*buildmanifest.PerfAssetUses, error) {
	fail := func(code, pointer string) (*buildmanifest.PerfAssetUses, error) {
		return nil, &buildmanifest.PerfAssetError{Code: code, Pointer: pointer}
	}
	backend := opts.Backend
	if backend == "" {
		backend = "none"
	}
	if backend != "none" && backend != "webgpu" && backend != "webgl2" {
		return fail("invalid-input", "/backend")
	}
	if r == nil || r.perfAssets == nil || r.manifest == nil {
		return fail("unknown-reachability", "/perfAssetUses")
	}
	if r.hasSceneEngines() != (backend != "none") {
		return fail("invalid-input", "/backend")
	}
	if backend == "webgpu" && opts.NavigatorGPU != nil && !*opts.NavigatorGPU {
		return fail("invalid-input", "/navigatorGPU")
	}
	uses := clonePerfAssetUses(r.perfAssets)
	byURL, byID := map[string]int{}, map[string]int{}
	for i := range uses.Assets {
		a := &uses.Assets[i]
		if suffix, ok := strings.CutPrefix(a.URL, "/gosx/assets/"); ok && a.ID != "framework/runtime/navigation.js" {
			a.URL = strings.TrimRight(r.perfAssetBaseURL, "/") + "/" + suffix
		}
		// Preview currently emits the relay compatibility URL. Bind that
		// actual URL to the staged relay identity; HTTP measurement still
		// verifies the served body's full SHA before certifying it.
		if a.ID == "framework/runtime/relay.js" && r.clientRuntimePlan().PreviewRelay && r.relayPath == "/gosx/relay.js" {
			if !strings.HasPrefix(a.SHA256, r.runtimeAssets.Relay.Hash) || r.runtimeAssets.Relay.Hash == "" {
				return fail("unknown-reachability", "/runtime/relay")
			}
			a.URL = r.relayPath
		}
		a.URL = urlpath.URL(r.basePath, a.URL)
		if previous, ok := byURL[a.URL]; ok && uses.Assets[previous].ID != a.ID {
			return fail("invalid-input", "/perfAssetUses/assets/"+strconv.Itoa(i)+"/url")
		}
		byURL[a.URL], byID[a.ID] = i, i
	}
	index := func(url string) (int, error) {
		i, ok := byURL[url]
		if !ok || url == "" {
			return 0, &buildmanifest.PerfAssetError{Code: "unknown-reachability", Pointer: "/perfAssetUses"}
		}
		return i, nil
	}
	var activate func(int, string) error
	active := map[string]bool{}
	activate = func(i int, phase string) error {
		a := &uses.Assets[i]
		key := a.ID + ":" + phase
		if active[key] {
			return nil
		}
		active[key] = true
		if a.Phase == "dormant" || phase == "startup" {
			a.Phase = phase
		}
		for _, dependency := range a.Dependencies {
			j, ok := byID[dependency]
			if !ok {
				return &buildmanifest.PerfAssetError{Code: "unknown-reachability", Pointer: "/perfAssetUses"}
			}
			if err := activate(j, phase); err != nil {
				return err
			}
		}
		return nil
	}
	mark := func(url, phase, condition string, prerequisites ...string) error {
		i, err := index(url)
		if err != nil {
			return err
		}
		uses.Assets[i].Condition = condition
		for _, path := range prerequisites {
			if path == "" {
				continue
			}
			j, err := index(path)
			if err != nil {
				return err
			}
			id := uses.Assets[j].ID
			if id == uses.Assets[i].ID {
				continue
			}
			found := false
			for _, previous := range uses.Assets[i].Dependencies {
				found = found || id == previous
			}
			if !found {
				uses.Assets[i].Dependencies = append(uses.Assets[i].Dependencies, id)
			}
		}
		return activate(i, phase)
	}
	public := func(path string) string { return urlpath.URL(r.basePath, path) }
	plan, summary := r.clientRuntimePlan(), r.Summary()
	if plan.StandardGoWASMExec && summary.StandardGoWASMExecPath == "" {
		return fail("unknown-reachability", "/runtime/standardGoWasmExec")
	}
	if plan.Bootstrap {
		if err := mark(public(summary.BootstrapPath), "startup", "always"); err != nil {
			return nil, err
		}
	}
	// Read actual emitted script sources instead of a second script-selection
	// table. Inline framework bodies belong to the document marginal, not to
	// another external asset fetch.
	head := gosx.RenderHTML(gosx.Fragment(r.PreloadHints(), r.PageHead()))
	if len(head) > 16<<20 {
		return fail("invalid-input", "/pageHead")
	}
	tokens := xhtml.NewTokenizer(strings.NewReader(head))
	for {
		kind := tokens.Next()
		if kind == xhtml.ErrorToken {
			if tokens.Err() != io.EOF {
				return fail("invalid-input", "/pageHead")
			}
			break
		}
		if kind != xhtml.StartTagToken && kind != xhtml.SelfClosingTagToken {
			continue
		}
		token := tokens.Token()
		var url, rel, as string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "src":
				if token.Data == "script" {
					url = attr.Val
				}
			case "href":
				if token.Data == "link" {
					url = attr.Val
				}
			case "rel":
				rel = attr.Val
			case "as":
				as = attr.Val
			}
		}
		if token.Data == "script" && url != "" || token.Data == "link" && (rel == "preload" || rel == "prefetch") && (as == "script" || as == "" || as == "fetch") {
			// Model/texture preloads are app resources measured by the page
			// resource graph. Runtime and program fetches must have bodies here.
			if token.Data == "link" && as == "fetch" {
				if i, ok := byURL[url]; !ok || uses.Assets[i].Kind == "model" || uses.Assets[i].Kind == "image" {
					continue
				}
			}
			if err := mark(public(url), "startup", "always"); err != nil {
				return nil, err
			}
		}
	}
	if plan.SharedRuntime {
		if summary.WASMExecPath == "" {
			return fail("unknown-reachability", "/runtime/wasmExec")
		}
		if err := mark(public(summary.RuntimePath), "startup", "always", public(summary.WASMExecPath)); err != nil {
			return nil, err
		}
	}
	// The monolith contains these feature implementations; selective runtime
	// actually fetches them. A missing selective URL still charges the
	// selected monolith, including in preview and lite fallback modes.
	if plan.Selective && r.bootstrapRuntimePath != "" && summary.BootstrapPath == r.bootstrapRuntimePath {
		for _, path := range []string{summary.BootstrapFeatureIslandsPath, summary.BootstrapFeatureEnginesPath,
			summary.BootstrapFeatureHubsPath, summary.BootstrapFeatureControllersPath} {
			if path == "" {
				continue
			}
			if err := mark(public(path), "startup", "always", public(summary.BootstrapPath)); err != nil {
				return nil, err
			}
		}
	}
	// Controller input is demand-loaded in both selective and monolithic hosts.
	if summary.BootstrapControllerInputPath != "" {
		if err := mark(public(summary.BootstrapControllerInputPath), "startup", "always", public(summary.BootstrapPath)); err != nil {
			return nil, err
		}
	}
	for _, entry := range r.manifest.Islands {
		if entry.Static {
			continue
		}
		if err := mark(public(entry.ProgramRef), "startup", "always", public(summary.RuntimePath)); err != nil {
			return nil, err
		}
	}
	for _, entry := range r.manifest.ComputeIslands {
		if err := mark(public(entry.ProgramRef), "startup", "always", public(summary.RuntimePath)); err != nil {
			return nil, err
		}
	}
	for _, entry := range r.manifest.Engines {
		if entry.ProgramRef == "" {
			continue
		}
		loader := summary.RuntimePath
		if entry.Runtime == "go-wasm" {
			loader = summary.StandardGoWASMExecPath
		}
		if err := mark(public(entry.ProgramRef), "startup", "always", public(loader)); err != nil {
			return nil, err
		}
	}
	if r.hasVideoEngines() && opts.RuntimeFetches == nil {
		return fail("unknown-reachability", "/runtimeFetches")
	}
	if r.hasSceneEngines() {
		base := public(summary.BootstrapFeatureScene3DPath)
		if base == "" {
			base = public(summary.BootstrapPath)
		}
		if !r.perfSceneBackendAllowed(backend) {
			return fail("backend-unavailable", "/backend")
		}
		// The emitted loader checks navigator.gpu before adapter acquisition.
		// Its download is independent of the eventual rendering backend.
		if path := r.selectedBootstrapFeaturePath("scene3d-webgpu"); summary.BootstrapFeatureScene3DPath != "" && path != "" && (opts.NavigatorGPU == nil || *opts.NavigatorGPU) {
			if err := mark(public(path), "startup", "always", base); err != nil {
				return nil, err
			}
		}
		if summary.BootstrapFeatureScene3DPath != "" {
			primary, condition := r.bootstrapFeatureScene3dWebGPUPath, "webgpu"
			if backend == "webgl2" {
				primary, condition = r.bootstrapFeatureScene3dWebGLPath, "webgl"
			}
			if err := mark(public(primary), "startup", condition, base); err != nil {
				return nil, err
			}
			if backend == "webgpu" && r.perfSceneAllowsWebGLFallback() {
				// Recovery is inside the WebGPU body. Only the WebGL fallback is an
				// additional device-loss fetch; the old recovery chunk stays dormant.
				if err := mark(public(r.bootstrapFeatureScene3dWebGLPath), "after-ready", "device-loss", base); err != nil {
					return nil, err
				}
			}
			gates, models, err := r.perfSceneStartupGates()
			if err != nil {
				return nil, err
			}
			if models && opts.RuntimeFetches == nil {
				return fail("unknown-reachability", "/runtimeFetches")
			}
			for _, entry := range []struct{ name, path string }{
				{"zoom", r.bootstrapFeatureScene3dZoomPath}, {"walk", r.bootstrapFeatureScene3dWalkPath},
				{"vessel", r.bootstrapFeatureScene3dVesselPath}, {"ocean-query", r.bootstrapFeatureScene3dOceanQueryPath},
				{"compute", r.bootstrapFeatureScene3dComputePath}, {"decompress", r.bootstrapFeatureScene3dDecompressPath},
				{"gltf", r.bootstrapFeatureScene3dGLTFPath},
			} {
				if gates[entry.name] {
					if err := mark(public(entry.path), "startup", "always", base); err != nil {
						return nil, err
					}
				}
			}
			// Preloads start downloads even when the mount's content gate is
			// false. Preserve those transfers and attach their Scene3D loader.
			for i := range uses.Assets {
				asset := uses.Assets[i]
				if strings.HasPrefix(asset.ID, "framework/runtime/bootstrap-feature-scene3d-") && asset.ID != "framework/runtime/bootstrap-feature-scene3d-hydrate.js" && asset.Phase == "startup" {
					if err := mark(asset.URL, "startup", asset.Condition, base); err != nil {
						return nil, err
					}
				}
			}
		}
		text, err := json.Marshal(r.manifest)
		if err != nil {
			return fail("unknown-reachability", "/manifest")
		}
		if summary.BootstrapFeatureScene3DPath != "" && (strings.Contains(string(text), `"labels":[{`) || strings.Contains(string(text), `"label":{`) || strings.Contains(string(text), `"kind":"label"`)) {
			if err := mark(public(summary.BootstrapFeatureTextLayoutPath), "startup", "always", public(summary.BootstrapPath)); err != nil {
				return nil, err
			}
		}
	}
	if opts.TextLayout && plan.Bootstrap {
		path := summary.BootstrapFeatureTextLayoutPath
		if path == "" {
			// Without a manifest URL the production forwarder uses this
			// compatibility URL; it still needs a body verified at that URL.
			path = "/gosx/bootstrap-feature-textlayout.js"
		}
		if err := mark(public(path), "startup", "always", public(summary.BootstrapPath)); err != nil {
			return nil, err
		}
	}
	for i, fetch := range opts.RuntimeFetches {
		pointer := "/runtimeFetches/" + strconv.Itoa(i)
		if fetch.Phase != "startup" && fetch.Phase != "after-ready" {
			return fail("invalid-input", pointer+"/phase")
		}
		j, err := index(fetch.URL)
		if err != nil {
			return nil, err
		}
		asset := uses.Assets[j]
		// Verified app programs and other bodies keep their declared identity
		// and prerequisites. Only framework runtime loads imply bootstrap.
		prerequisites, condition := []string{}, asset.Condition
		if strings.HasPrefix(asset.ID, "framework/runtime/") {
			prerequisites, condition = []string{public(summary.BootstrapPath)}, "always"
		}
		if strings.HasPrefix(asset.ID, "framework/runtime/bootstrap-feature-scene3d-") {
			base := public(summary.BootstrapFeatureScene3DPath)
			if base == "" {
				base = public(summary.BootstrapPath)
			}
			prerequisites = []string{base}
			if asset.ID == "framework/runtime/bootstrap-feature-scene3d-timeline.js" || asset.ID == "framework/runtime/bootstrap-feature-scene3d-particle-burst.js" {
				prerequisites = append(prerequisites, public(r.bootstrapFeatureScene3dCommandPath))
			}
			if asset.ID == "framework/runtime/bootstrap-feature-scene3d-particle-burst.js" {
				prerequisites = append(prerequisites, public(r.bootstrapFeatureScene3dComputePath))
			}
		}
		if asset.ID == "framework/runtime/hls.min.js" {
			condition = "hls-required"
		}
		if err := mark(fetch.URL, fetch.Phase, condition, prerequisites...); err != nil {
			return nil, err
		}
	}
	if opts.Navigation {
		if err := mark(public(runtimehost.NavigationRuntimePath), "startup", "always"); err != nil {
			return nil, err
		}
	}
	for i := range uses.Assets {
		sort.Strings(uses.Assets[i].Dependencies)
	}
	if err := (&buildmanifest.Manifest{PerfAssetUses: uses}).ValidatePerfAssetUses(); err != nil {
		return nil, err
	}
	return uses, nil
}

func clonePerfAssetUses(uses *buildmanifest.PerfAssetUses) *buildmanifest.PerfAssetUses {
	if uses == nil {
		return nil
	}
	copy := &buildmanifest.PerfAssetUses{Version: uses.Version, Assets: make([]buildmanifest.PerfAssetUse, len(uses.Assets))}
	for i, asset := range uses.Assets {
		copy.Assets[i] = asset
		copy.Assets[i].Dependencies = append([]string{}, asset.Dependencies...)
	}
	return copy
}

func (r *Renderer) perfSceneBackendAllowed(backend string) bool {
	for _, entry := range r.manifest.Engines {
		if !strings.EqualFold(entry.Component, "GoSXScene3D") {
			continue
		}
		var props scenePreloadProbe
		if len(entry.Props) != 0 && json.Unmarshal(entry.Props, &props) != nil {
			return false
		}
		gpu, gl := props.backends()
		if backend == "webgpu" && !gpu || backend == "webgl2" && !gl {
			return false
		}
	}
	return true
}

func (r *Renderer) perfSceneAllowsWebGLFallback() bool {
	for _, entry := range r.manifest.Engines {
		if !strings.EqualFold(strings.TrimSpace(entry.Component), "GoSXScene3D") {
			continue
		}
		var props scenePreloadProbe
		if len(entry.Props) != 0 && json.Unmarshal(entry.Props, &props) != nil {
			return false
		}
		// Device-loss fallback follows sceneBackendCapsAllowsKind, regardless
		// of the preferences used to choose the initial rendering backend.
		caps := props.BackendCaps
		if props.Scene != nil && props.Scene.BackendCaps != nil {
			caps = props.Scene.BackendCaps
		}
		if caps == nil || caps.Capable == nil {
			return true
		}
		for _, backend := range caps.Capable {
			if strings.EqualFold(backend, "webgl") || strings.EqualFold(backend, "webgl2") {
				return true
			}
		}
	}
	return false
}
