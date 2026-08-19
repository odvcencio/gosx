package gosx_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	vmpkg "m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/route"
)

// This file proves a strict island end to end: lowering accepts it, the
// server renders it with proven props, and the client VM (the same Go code
// the production WASM build ships, run natively here) hydrates it and
// answers a real dispatched event with the correct new state.
//
// Typed props cross the server/client boundary as plain JSON, the same as a
// legacy island: localComponentProps (route/fileprogram.go) proves the
// call's field coverage and leaf types against the strict island's declared
// props struct, exactly as it does for an ordinary strict component, then
// hands the proven map[string]any to env.island for JSON serialization. On
// the client, client/vm/island.go's parseProps exposes every serialized key
// as both a flat prop (legacy convention, e.g. "Label") and a field of a
// reserved "props" object binding (client/vm/island.go, parseProps) — so a
// strict island body's props.Label selector resolves with no VM change.

type counterProps struct {
	Label string
	Start int
}

const strictCounterIslandSource = `package app

import "m31labs.dev/gosx/signal"

type CounterProps struct {
	Label string
	Start int
}

//gosx:island
component Counter(props: CounterProps) {
	count := signal.New(props.Start)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<span>{props.Label}</span>
		<button onClick={increment}>{count.Get()}</button>
	</div>
}

component Page(props: CounterProps) {
	return <main><Counter label={props.Label} start={props.Start} /></main>
}
`

// TestStrictIslandRendersProvenPropsServerSide is the server-side half of
// the proof: it asserts on the actual rendered HTML bytes and the actual
// manifest JSON bytes a real page would ship to the browser.
func TestStrictIslandRendersProvenPropsServerSide(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictCounterIslandSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var counter *ir.Component
	for i, c := range prog.Components {
		if c.Name == "Counter" {
			counter = &prog.Components[i]
		}
	}
	if counter == nil {
		t.Fatalf("Counter component not found")
	}
	if counter.Syntax != ir.ComponentSyntaxStrict {
		t.Fatalf("Counter.Syntax = %v, want ComponentSyntaxStrict", counter.Syntax)
	}
	if !counter.IsIsland {
		t.Fatalf("Counter.IsIsland = false, want true")
	}

	// gosx check's own island gate: LowerIsland must succeed for every
	// IsIsland component (cmd/gosx/main.go's runCheck runs the same call).
	if _, err := ir.LowerIsland(prog, indexOf(prog, "Counter")); err != nil {
		t.Fatalf("LowerIsland(Counter): %v", err)
	}

	renderer := island.NewRenderer("test-bundle")
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		Props:        counterProps{Label: "Draft Pick", Start: 7},
		RenderIsland: renderer.RenderIslandFromProgram,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	const wantHTML = `<main><div id="gosx-island-0" data-gosx-island="Counter" data-gosx-enhance="island" data-gosx-enhance-layer="runtime" data-gosx-fallback="server"><div class="counter"> <span>Draft Pick</span> <button data-gosx-on-click="increment" data-gosx-handler="increment" data-gosx-path="0/3">7</button> </div></div></main>`
	if html != wantHTML {
		t.Fatalf("rendered HTML =\n%s\nwant\n%s", html, wantHTML)
	}

	manifestJSON, err := renderer.ManifestJSON()
	if err != nil {
		t.Fatalf("ManifestJSON: %v", err)
	}
	for _, want := range []string{`"Label": "Draft Pick"`, `"Start": 7`, `"handlerName": "increment"`} {
		if !strings.Contains(manifestJSON, want) {
			t.Fatalf("manifest JSON missing %q:\n%s", want, manifestJSON)
		}
	}
}

// TestStrictIslandClientVMHydratesAndDispatches is the client-side half of
// the proof: it runs the actual client/vm package (the code the production
// WASM build ships, compiled here as native Go) against the island program
// LowerIsland produced, feeds it the same JSON a real hydration script tag
// would carry, and dispatches the same click event a real browser click
// would dispatch — then asserts the resulting DOM text changed correctly.
func TestStrictIslandClientVMHydratesAndDispatches(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictCounterIslandSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	clientProg, err := ir.LowerIsland(prog, indexOf(prog, "Counter"))
	if err != nil {
		t.Fatalf("LowerIsland: %v", err)
	}
	if len(clientProg.Handlers) != 1 || clientProg.Handlers[0].Name != "increment" {
		t.Fatalf("client program handlers = %#v, want exactly one named %q", clientProg.Handlers, "increment")
	}

	isl := vmpkg.NewIsland(clientProg, `{"Label":"Draft Pick","Start":7}`)
	if isl == nil {
		t.Fatalf("NewIsland returned nil")
	}
	if got := strings.TrimSpace(renderTreeText(isl.CurrentTree())); got != "Draft Pick 7" {
		t.Fatalf("hydrated tree before dispatch = %q, want %q", got, "Draft Pick 7")
	}

	isl.Dispatch("increment", "{}")
	if got := strings.TrimSpace(renderTreeText(isl.CurrentTree())); got != "Draft Pick 8" {
		t.Fatalf("hydrated tree after dispatch = %q, want %q", got, "Draft Pick 8")
	}
}

func indexOf(prog *ir.Program, name string) int {
	for i, c := range prog.Components {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// renderTreeText concatenates every text/expr leaf in a ResolvedTree's node
// list, in order, giving a compact byte-level assertion surface without a
// full HTML serializer.
func renderTreeText(tree *vmpkg.ResolvedTree) string {
	if tree == nil {
		return ""
	}
	var b strings.Builder
	for _, n := range tree.Nodes {
		b.WriteString(n.Text)
	}
	return b.String()
}
