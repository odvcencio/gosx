package route

import (
	"fmt"
	"path/filepath"
	"runtime"

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
//   - Props supplies the typed props value when component is a strict
//     component (gosx#226): a `component Foo(props: FooProps)` declaration
//     compiles to an ir.Component with a declared PropsType, and — same as
//     a nested `<Foo {...props}/>` call inside another component's body —
//     rendering it as the entry component must prove a real struct value,
//     not a map. RenderProgramComponent proves Props at that identical
//     boundary (see strictSpreadProps): a nil Props, any map[string]any
//     (including one shaped exactly like FooProps — a map can omit keys
//     and has no field types to check ahead of the values, so it never
//     proves coverage), or a struct missing a rendered field or holding
//     one of the wrong type all fail closed with a descriptive error
//     instead of rendering. A same-shaped struct under a different Go type
//     name is accepted, matching a generated-Go spread caller's own
//     boundary (strictSpreadProps proves field coverage, not the source
//     struct's own type identity — see TestStrictSpreadProps). Ignored for
//     a legacy (non-strict) component, and for a strict component with no
//     declared props.
//   - RenderIsland turns a local //gosx:island child (referenced as <Name/>) into
//     a hydrated server-rendered mount — pass server.PageRuntime.Island or
//     island.(*Renderer).RenderIslandFromProgram. When nil, island children
//     degrade to inert placeholders.
//   - Profile installs an EXPERIMENTAL render-profile hook: an attribute
//     writer that can rewrite, veto, or append an element's attributes, and
//     a pre-render validation pass that can refuse to render the program
//     (gosx#185). A nil Profile reproduces today's rendering exactly, byte
//     for byte. See RenderProfile.
type ProgramRenderEnv struct {
	Values       map[string]any
	Funcs        map[string]any
	Props        any
	RenderIsland func(*islandprogram.Program, any) gosx.Node
	Profile      *RenderProfile
}

// RenderProgramComponent renders the named component of a compiled program (from
// gosx.Compile) to server-side static HTML: it evaluates {expr}, resolves local
// <Component/> references, and inlines local island children via env.RenderIsland.
//
// It is the public entry to the file-program renderer that powers file-based
// pages, for callers that compile and render components directly — e.g. a slide
// deck that lowers each slide to a generated component, or a plain http.Handler
// in the same package as a page.gsx file rendering one of that page's own
// components into a fragment (gosx#226, see LoadFileProgram) — instead of from
// on-disk page files. A single compiled source may declare the rendered
// component plus any sibling components and islands it references;
// cross-references resolve here at render time.
//
// component is a strict component's own render entry exactly the same way a
// nested call to it is: see ProgramRenderEnv.Props for the typed-props
// contract this requires.
//
// When env.Profile is set and its Validate hook reports any diagnostic,
// RenderProgramComponent returns a *RenderProfileError and an empty string;
// no partial HTML is ever returned alongside that error.
func RenderProgramComponent(prog *ir.Program, component string, env ProgramRenderEnv) (string, error) {
	html, _, err := renderFileProgramHTML(prog, component, fileRenderOptions{
		EvalEnv: fileRenderEnv{
			values:       env.Values,
			funcs:        env.Funcs,
			renderIsland: env.RenderIsland,
		},
		EntryProps: env.Props,
		Profile:    env.Profile,
	})
	return html, err
}

// LoadFileProgram compiles the .gsx file at path and returns its IR program,
// through the same stat-keyed cache (gosx#226) a file-routed page or layout
// renders through (route/filelayout.go's gsxCompileCache): a request that
// hits this file again before its next edit gets the cached *ir.Program back
// without re-parsing, and an edit invalidates the cache exactly the way it
// invalidates a running page's own hot reload — there is no second,
// independently-stale cache for a caller of this function to fall behind.
//
// This is the other half of gosx#226's fix: a plain http.Handler in the same
// package as a page.gsx file — for example, a fragment endpoint that data-
// gosx-region polls — loads that file's compiled program with this function
// and renders any component it declares, including one the page's own
// Page/Layout entry never renders directly, with RenderProgramComponent.
//
// path resolves relative to the process's current working directory, same as
// os.Open; pass an absolute path to make it independent of the working
// directory the binary starts in.
//
// Do not build that absolute path from runtime.Caller's own file value: a
// `-trimpath` build — `gosx build`'s default, and the shape of most
// container images — replaces that value with the calling module's own
// declared path, not a path that exists on the machine running the binary,
// so path resolves to a file that was never there (gosx#239). This mistake
// passes every local check: a plain `go run` or `go build` still records the
// real path, so it only surfaces once something builds with -trimpath.
// LoadFileProgramHere resolves a sibling of the calling file without this
// problem; prefer it when path is a sibling of the calling source file.
func LoadFileProgram(path string) (*ir.Program, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", path, err)
	}
	return loadCachedGSXProgram(abs)
}

// LoadFileProgramHere compiles the .gsx (or .html) file named name in the
// same directory as the calling source file and returns its IR program,
// through the same cache LoadFileProgram uses (gosx#226).
//
// Use this in place of building a path from runtime.Caller and
// filepath.Dir yourself, the pattern the "Rendering a fragment from a Go
// handler" example used before this function existed: see LoadFileProgram's
// own doc comment for why that pattern breaks under a `-trimpath` build
// (gosx#239). LoadFileProgramHere resolves the calling file the same
// trimpath-safe way RegisterFileModuleHere resolves a page's sibling .gsx
// file, so it does not have that problem.
//
// name is a bare file name, such as "page.gsx" — a sibling of the calling
// file, not a path elsewhere in the tree. For that, resolve an absolute path
// yourself and call LoadFileProgram directly.
func LoadFileProgramHere(name string) (*ir.Program, error) {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		return nil, fmt.Errorf("load %s: could not resolve the calling file", name)
	}
	dir := filepath.Dir(resolveCallerFilePath(file))
	return LoadFileProgram(filepath.Join(dir, name))
}
