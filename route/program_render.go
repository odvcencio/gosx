package route

import (
	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	islandprogram "m31labs.dev/gosx/island/program"
)

// ProgramRenderEnv supplies the expression bindings and inline-island renderer
// for RenderProgramComponent. All fields are optional:
//
//   - Values / Funcs bind identifiers and functions referenced by {expr} (e.g.
//     Funcs["strings"] = map[string]any{"ToUpper": strings.ToUpper}). Unresolved
//     identifiers render empty rather than erroring, so missing bindings fail soft.
//   - RenderIsland turns a local //gosx:island child (referenced as <Name/>) into
//     a hydrated server-rendered mount — pass server.PageRuntime.Island or
//     island.(*Renderer).RenderIslandFromProgram. When nil, island children
//     degrade to inert placeholders.
type ProgramRenderEnv struct {
	Values       map[string]any
	Funcs        map[string]any
	RenderIsland func(*islandprogram.Program, any) gosx.Node
}

// RenderProgramComponent renders the named component of a compiled program (from
// gosx.Compile) to server-side static HTML: it evaluates {expr}, resolves local
// <Component/> references, and inlines local island children via env.RenderIsland.
//
// It is the public entry to the file-program renderer that powers file-based
// pages, for callers that compile and render components directly — e.g. a slide
// deck that lowers each slide to a generated component — instead of from on-disk
// page files. A single compiled source may declare the rendered component plus
// any sibling components and islands it references; cross-references resolve here
// at render time.
func RenderProgramComponent(prog *ir.Program, component string, env ProgramRenderEnv) (string, error) {
	html, _, err := renderFileProgramHTML(prog, component, fileRenderOptions{
		EvalEnv: fileRenderEnv{
			values:       env.Values,
			funcs:        env.Funcs,
			renderIsland: env.RenderIsland,
		},
	})
	return html, err
}
