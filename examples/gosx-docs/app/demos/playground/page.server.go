package playground

import (
	"encoding/base64"
	"fmt"
	"strings"

	"m31labs.dev/gosx/action"
	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/route"
)

// playgroundCompiler is the per-process Compiler instance used by the
// /demos/playground action endpoint. Constructed once at init; holds the
// rate limiter and LRU cache state.
var playgroundCompiler *Compiler

// RegisterManagedActions installs the playground's global managed endpoint on
// the application's single route router. File modules render forms; they do
// not own action registries or action paths.
func RegisterManagedActions(router *route.Router) error {
	if router == nil {
		return fmt.Errorf("managed action router is nil")
	}
	if playgroundCompiler == nil {
		return fmt.Errorf("playground compiler initialization failed")
	}
	return router.RegisterManagedPOST("compile", action.Config{}, NewCompileAction(playgroundCompiler))
}

func init() {
	c, err := NewCompiler(DefaultCompileConfig())
	if err == nil {
		playgroundCompiler = c
	}

	docsapp.RegisterStaticDocsPage(
		"Playground",
		"A live editor for the constrained GoSX island language and shared browser VM.",
		route.FileModuleOptions{
			Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
				def := DefaultPreset()
				compiled, err := CompileSource([]byte(def.Source))
				if err != nil {
					return nil, err
				}
				islandProgram, err := program.DecodeBinary(compiled.Program)
				if err != nil {
					return nil, err
				}
				programRef := "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(compiled.Program)
				return map[string]any{
					"source":  def.Source,
					"slug":    def.Slug,
					"presets": Presets(),
					"preview": ctx.Runtime().IslandWithProgramAsset(islandProgram, map[string]any{}, programRef, "bin", ""),
					// Initial compiler-output facts for the default preset, computed
					// server-side so the panel shows real numbers on first paint
					// instead of a placeholder that over-promises (see page.gsx).
					"initialProgramBytes": len(compiled.Program),
					"initialNodeCount":    compiled.NodeCount,
					"initialExprCount":    compiled.ExprCount,
					"initialDiagnostics":  len(compiled.Diagnostics),
					// Initial editor line/char counts, computed with the same
					// formula the client uses after edits (playground-editor.js)
					// so the numbers never visibly jump on hydration.
					"initialLines": strings.Count(def.Source, "\n") + 1,
					"initialChars": len(def.Source),
				}, nil
			},
		},
	)
}
