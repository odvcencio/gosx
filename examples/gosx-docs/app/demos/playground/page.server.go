package playground

import (
	docsapp "github.com/odvcencio/gosx/examples/gosx-docs/app"
	"github.com/odvcencio/gosx/route"
)

// playgroundCompiler is the per-process Compiler instance used by the
// /demos/playground action endpoint. Constructed once at init; holds the
// rate limiter and LRU cache state.
var playgroundCompiler *Compiler

func init() {
	c, err := NewCompiler(DefaultCompileConfig())
	if err != nil {
		// Invariant violation — DefaultCompileConfig is supposed to
		// return a valid config. Fail loud.
		panic("playground: NewCompiler with default config: " + err.Error())
	}
	playgroundCompiler = c

	docsapp.RegisterStaticDocsPage(
		"Playground",
		"A live .gsx editor. Type a component on the left, watch it hydrate on the right.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				def := DefaultPreset()
				return map[string]any{
					"source":  def.Source,
					"slug":    def.Slug,
					"presets": Presets(),
				}, nil
			},
			Actions: route.FileActions{
				"compile": NewCompileAction(playgroundCompiler),
			},
		},
	)
}
