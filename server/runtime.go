package server

import (
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/island"
	islandprogram "m31labs.dev/gosx/island/program"
)

// PageRuntime tracks page-scoped islands, engines, and hubs plus the bootstrap
// assets needed to mount them on the client.
type PageRuntime struct {
	renderer *island.Renderer
	active   bool
	head     []gosx.Node
}

// PageRuntimeSummary describes the bootstrap/runtime surface declared by a page.
type PageRuntimeSummary struct {
	Bootstrap                   bool
	Runtime                     bool
	BootstrapMode               string
	Manifest                    bool
	RuntimePath                 string
	WASMExecPath                string
	StandardGoWASMExecPath      string
	PatchPath                   string
	BootstrapPath               string
	BootstrapFeatureIslandsPath string
	BootstrapFeatureEnginesPath string
	BootstrapFeatureHubsPath    string
	BootstrapFeatureScene3DPath string
	HLSPath                     string
	Islands                     int
	ComputeIslands              int
	Engines                     int
	Hubs                        int
}

// NewPageRuntime creates an empty runtime registry for a page response.
func NewPageRuntime() *PageRuntime {
	renderer := island.NewRenderer("gosx-page")
	renderer.SetRuntime("/gosx/runtime.wasm", "", 0)
	return &PageRuntime{
		renderer: renderer,
	}
}

// EnableBootstrap opts the current page into the shared bootstrap runtime even
// when it does not need the WASM bridge.
func (r *PageRuntime) EnableBootstrap() {
	if r == nil {
		return
	}
	r.active = true
	if r.renderer != nil {
		r.renderer.EnableBootstrap()
	}
}

// Engine registers a client engine and returns its server-rendered mount shell.
func (r *PageRuntime) Engine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if r == nil {
		return fallback
	}
	r.active = true
	return r.renderer.RenderEngine(cfg, fallback)
}

// Island registers a compiled island program and returns its server-rendered shell.
func (r *PageRuntime) Island(prog *islandprogram.Program, props any) gosx.Node {
	if r == nil || prog == nil {
		return gosx.Text("")
	}
	r.active = true
	return r.renderer.RenderIslandFromProgram(prog, props)
}

// IslandWithProgramAsset registers an exact browser-loadable program asset
// before rendering an island. It is useful for request-time compiled programs
// that are not present in the production build manifest.
func (r *PageRuntime) IslandWithProgramAsset(prog *islandprogram.Program, props any, ref, format, hash string) gosx.Node {
	if r == nil || prog == nil || strings.TrimSpace(ref) == "" {
		return gosx.Text("")
	}
	r.renderer.SetProgramAsset(prog.Name, ref, format, hash)
	return r.Island(prog, props)
}

// ComputeIsland registers a headless island program for page-scoped client
// compute. It shares the island VM and signal bridge without owning a DOM root.
func (r *PageRuntime) ComputeIsland(cfg island.ComputeIslandConfig) string {
	if r == nil {
		return ""
	}
	id, err := r.renderer.RegisterComputeIsland(cfg)
	if err != nil {
		return ""
	}
	r.active = true
	return id
}

// BindHub registers a realtime hub connection for the current page.
func (r *PageRuntime) BindHub(name, path string, bindings []hydrate.HubBinding) string {
	if r == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	r.active = true
	return r.renderer.BindHub(name, path, bindings)
}

// BindHubInput registers a realtime hub and asks the GoSX bootstrap to forward
// browser input snapshots to it.
func (r *PageRuntime) BindHubInput(name, path string, bindings []hydrate.HubBinding, input hydrate.HubInputConfig) string {
	if r == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	r.active = true
	return r.renderer.BindHubInput(name, path, bindings, input)
}

// ClientIdentity asks the GoSX bootstrap to maintain a stable anonymous client
// identity for this page.
func (r *PageRuntime) ClientIdentity(config hydrate.ClientIdentityConfig) {
	if r == nil {
		return
	}
	r.active = true
	r.renderer.SetClientIdentity(config)
}

// TextBlock renders a text-layout node and enables the shared bootstrap
// runtime only when client-side refinement is requested.
func (r *PageRuntime) TextBlock(props TextBlockProps, args ...any) gosx.Node {
	if r != nil && TextBlockRequiresBootstrap(props) {
		r.EnableBootstrap()
	}
	return TextBlock(props, args...)
}

// SetProgramDir tells the page runtime where island programs are served from,
// so each registered island's manifest entry carries a fetchable programRef
// (e.g. "/gosx/islands" -> "/gosx/islands/<Name>.json"). Without this (or
// SetProgramAsset), Island()-registered programs have an empty programRef and the
// client bootstrap has no program to fetch, so they never hydrate. It mirrors
// island.Renderer.SetProgramDir and must be called before Island().
func (r *PageRuntime) SetProgramDir(dir string) {
	if r == nil || r.renderer == nil {
		return
	}
	r.renderer.SetProgramDir(dir)
}

// SetProgramFormat sets the program asset format ("json" or "bin") used when a
// programRef is inferred from SetProgramDir. It mirrors
// island.Renderer.SetProgramFormat.
func (r *PageRuntime) SetProgramFormat(format string) {
	if r == nil || r.renderer == nil {
		return
	}
	r.renderer.SetProgramFormat(format)
}

// SetProgramAsset registers an exact program asset path for a component, used
// when the build output is content-hashed and cannot be inferred from the name.
// It mirrors island.Renderer.SetProgramAsset and overrides any SetProgramDir
// inference for that component. Call before Island().
func (r *PageRuntime) SetProgramAsset(componentName, path, format, hash string) {
	if r == nil || r.renderer == nil {
		return
	}
	r.renderer.SetProgramAsset(componentName, path, format, hash)
}

// AddHead appends managed head nodes that should render after the shared
// runtime bootstrap assets.
func (r *PageRuntime) AddHead(nodes ...gosx.Node) {
	if r == nil {
		return
	}
	for _, node := range nodes {
		if node.IsZero() {
			continue
		}
		r.head = append(r.head, node)
	}
}

// ManagedScript appends a GoSX-managed external script to the page runtime.
func (r *PageRuntime) ManagedScript(src string, opts ManagedScriptOptions, args ...any) {
	if r == nil {
		return
	}
	r.AddHead(ManagedScript(src, opts, args...))
}

// LifecycleScript appends a page lifecycle helper script after the shared
// runtime assets so it can chain onto bootstrap/dispose hooks safely.
func (r *PageRuntime) LifecycleScript(src string, args ...any) {
	if r == nil {
		return
	}
	r.AddHead(LifecycleScript(src, args...))
}

// Head renders the preload, manifest, and bootstrap tags required by the page runtime.
func (r *PageRuntime) Head() gosx.Node {
	if r == nil {
		return gosx.Text("")
	}
	nodes := []gosx.Node{}
	if r.active {
		nodes = append(nodes,
			r.renderer.PreloadHints(),
			r.renderer.PageHead(),
		)
	}
	nodes = append(nodes, r.head...)
	if len(nodes) == 0 {
		return gosx.Text("")
	}
	return gosx.Fragment(nodes...)
}

// Active reports whether the page registered any runtime engines.
func (r *PageRuntime) Active() bool {
	return r != nil && r.active
}

// Summary reports the bootstrap/runtime surface declared by the page runtime.
func (r *PageRuntime) Summary() PageRuntimeSummary {
	if r == nil || r.renderer == nil || !r.active {
		return PageRuntimeSummary{BootstrapMode: "none"}
	}
	summary := r.renderer.Summary()
	return PageRuntimeSummary{
		Bootstrap:                   summary.Bootstrap,
		Runtime:                     strings.TrimSpace(summary.RuntimePath) != "",
		BootstrapMode:               summary.BootstrapMode,
		Manifest:                    summary.Manifest,
		RuntimePath:                 summary.RuntimePath,
		WASMExecPath:                summary.WASMExecPath,
		StandardGoWASMExecPath:      summary.StandardGoWASMExecPath,
		PatchPath:                   summary.PatchPath,
		BootstrapPath:               summary.BootstrapPath,
		BootstrapFeatureIslandsPath: summary.BootstrapFeatureIslandsPath,
		BootstrapFeatureEnginesPath: summary.BootstrapFeatureEnginesPath,
		BootstrapFeatureHubsPath:    summary.BootstrapFeatureHubsPath,
		BootstrapFeatureScene3DPath: summary.BootstrapFeatureScene3DPath,
		HLSPath:                     summary.HLSPath,
		Islands:                     summary.Islands,
		ComputeIslands:              summary.ComputeIslands,
		Engines:                     summary.Engines,
		Hubs:                        summary.Hubs,
	}
}

func (r *PageRuntime) usesCompatRuntimeAssets() bool {
	if r == nil || r.renderer == nil {
		return false
	}
	head := gosx.RenderHTML(r.renderer.PageHead())
	return strings.Contains(head, "/gosx/")
}
