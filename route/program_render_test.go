package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	islandprogram "m31labs.dev/gosx/island/program"
)

// RenderProgramComponent is the public entry to the file-program renderer for
// callers that compile a component with gosx.Compile and want server-side static
// HTML: {expr} evaluated, local <Component/> resolved, and local island children
// inlined via the RenderIsland callback. This proves a generated "slide"
// component (the gosx-slides use case) renders with a pure expr evaluated AND an
// island child inlined, from a single compiled source.
func TestRenderProgramComponentEvaluatesExprAndInlinesIsland(t *testing.T) {
	src := []byte(`package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	return <div><span>{count.Get()}</span></div>
}

func Slide() Node {
	return <section><h1>Hi</h1><p>{2 + 3}</p><Counter/></section>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var islandCalled bool
	html, err := RenderProgramComponent(prog, "Slide", ProgramRenderEnv{
		RenderIsland: func(p *islandprogram.Program, props any) gosx.Node {
			islandCalled = true
			return gosx.El("div", gosx.Attrs(gosx.Attr("data-gosx-island", "Counter")))
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "<h1>Hi</h1>") {
		t.Fatalf("missing prose heading: %s", html)
	}
	if !strings.Contains(html, ">5<") {
		t.Fatalf("expr {2 + 3} not evaluated to 5: %s", html)
	}
	if !islandCalled {
		t.Fatal("RenderIsland callback not invoked for <Counter/>")
	}
	if !strings.Contains(html, `data-gosx-island="Counter"`) {
		t.Fatalf("island child not inlined: %s", html)
	}
}

// A pure static component (no island, no bindings) renders evaluated exprs.
func TestRenderProgramComponentPureStatic(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Slide() Node {
	return <section><p>{"a" + "b"}</p><p>{6 * 7}</p></section>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Slide", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, ">ab<") || !strings.Contains(html, ">42<") {
		t.Fatalf("pure exprs not evaluated: %s", html)
	}
}
