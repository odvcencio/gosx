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
}

// NewPageRuntime creates an empty runtime registry for a page response.
func NewPageRuntime() *PageRuntime {
	renderer := island.NewRenderer("gosx-page")
	renderer.SetRuntime("/gosx/runtime.wasm", "", 0)
	return &PageRuntime{
		renderer: renderer,
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

// Head renders the preload, manifest, and bootstrap tags required by the page runtime.
func (r *PageRuntime) Head() gosx.Node {
	if r == nil || !r.active {
		return gosx.Text("")
	}
	return gosx.Fragment(
		r.renderer.PreloadHints(),
		r.renderer.PageHead(),
	)
}

// Active reports whether the page registered any runtime engines.
func (r *PageRuntime) Active() bool {
	return r != nil && r.active
}

func (r *PageRuntime) usesCompatRuntimeAssets() bool {
	if r == nil || r.renderer == nil {
		return false
	}
	head := gosx.RenderHTML(r.renderer.PageHead())
	return strings.Contains(head, "/gosx/")
}
