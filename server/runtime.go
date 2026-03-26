package server

import (
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/island"
)

// PageRuntime tracks page-scoped engines and the bootstrap assets needed to
// mount them on the client.
type PageRuntime struct {
	renderer *island.Renderer
	active   bool
}

// NewPageRuntime creates an empty runtime registry for a page response.
func NewPageRuntime() *PageRuntime {
	return &PageRuntime{
		renderer: island.NewRenderer("gosx-page"),
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
