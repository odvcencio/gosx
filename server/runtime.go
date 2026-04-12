package server

import (
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/hydrate"
	"github.com/odvcencio/gosx/island"
	islandprogram "github.com/odvcencio/gosx/island/program"
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

// BindHub registers a realtime hub connection for the current page.
func (r *PageRuntime) BindHub(name, path string, bindings []hydrate.HubBinding) string {
	if r == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	r.active = true
	return r.renderer.BindHub(name, path, bindings)
}

// TextBlock renders a text-layout node and enables the shared bootstrap
// runtime only when client-side refinement is requested.
func (r *PageRuntime) TextBlock(props TextBlockProps, args ...any) gosx.Node {
	if r != nil && TextBlockRequiresBootstrap(props) {
		r.EnableBootstrap()
	}
	return TextBlock(props, args...)
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
		PatchPath:                   summary.PatchPath,
		BootstrapPath:               summary.BootstrapPath,
		BootstrapFeatureIslandsPath: summary.BootstrapFeatureIslandsPath,
		BootstrapFeatureEnginesPath: summary.BootstrapFeatureEnginesPath,
		BootstrapFeatureHubsPath:    summary.BootstrapFeatureHubsPath,
		BootstrapFeatureScene3DPath: summary.BootstrapFeatureScene3DPath,
		HLSPath:                     summary.HLSPath,
		Islands:                     summary.Islands,
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
