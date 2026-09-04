package gosx_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	clientvm "m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/route"
)

const composedIslandSource = `package app

import "m31labs.dev/gosx/signal"

type BadgeProps struct {
	Label string
	Count int
	Active bool
}

type DashboardProps struct {
	Label string
	Start int
}

type ValueProps struct {
	Text string
}

component Value(props: ValueProps) {
	return <h2>{props.Text}</h2>
}

component Badge(props: BadgeProps) {
	return <article data-active={props.Active}><header>{slotTitle}</header><Value text={props.Label} /><span>{props.Count}</span><div>{children}</div></article>
}

//gosx:island
component Dashboard(props: DashboardProps) {
	count := signal.New(props.Start)
	increment := func() { count.Set(count.Get() + 1) }
	return <section><Badge label={props.Label} count={count.Get()} active><strong slot="Title">Primary</strong><button onClick={increment}>{count.Get()}</button></Badge><Badge label="Second" count={count.Get()} active={false}><em>{props.Label}</em></Badge></section>
}

component Page() {
	return <main><Dashboard label="Inbox" start={4} /></main>
}
`

const flattenedIslandSource = `package app

import "m31labs.dev/gosx/signal"

type DashboardProps struct {
	Label string
	Start int
}

//gosx:island
component Dashboard(props: DashboardProps) {
	count := signal.New(props.Start)
	increment := func() { count.Set(count.Get() + 1) }
	return <section><article data-active={true}><header><strong>Primary</strong></header><h2>{props.Label}</h2><span>{count.Get()}</span><div><button onClick={increment}>{count.Get()}</button></div></article><article data-active={false}><header></header><h2>{"Second"}</h2><span>{count.Get()}</span><div><em>{props.Label}</em></div></article></section>
}
`

func TestIslandPureViewCompositionMatchesHandFlattenedProgram(t *testing.T) {
	composed := compileIslandProgram(t, composedIslandSource, "Dashboard")
	flattened := compileIslandProgram(t, flattenedIslandSource, "Dashboard")

	composedBinary, err := islandprogram.EncodeBinary(composed)
	if err != nil {
		t.Fatalf("EncodeBinary(composed): %v", err)
	}
	flattenedBinary, err := islandprogram.EncodeBinary(flattened)
	if err != nil {
		t.Fatalf("EncodeBinary(flattened): %v", err)
	}
	if !bytes.Equal(composedBinary, flattenedBinary) {
		t.Fatalf("composed program = %d bytes, flattened = %d bytes; want byte-identical .gxi output\ncomposed=%#v\nflattened=%#v", len(composedBinary), len(flattenedBinary), composed, flattened)
	}
	t.Logf("encoded island program: composed=%d bytes flattened=%d bytes", len(composedBinary), len(flattenedBinary))

	const props = `{"Label":"Inbox","Start":4}`
	composedTree := clientvm.ResolveInitialTree(composed, props)
	flattenedTree := clientvm.ResolveInitialTree(flattened, props)
	if got, want := renderTreeText(composedTree), renderTreeText(flattenedTree); got != want {
		t.Fatalf("composed initial tree text = %q, flattened = %q", got, want)
	}
	if got := strings.Join(resolvedTags(composedTree), ","); strings.Count(got, "article") != 2 {
		t.Fatalf("resolved tags = %q, want two independently inlined article invocations", got)
	}
	root := composed.Nodes[composed.Root]
	if len(root.Children) != 2 || root.Children[0] == root.Children[1] {
		t.Fatalf("root children = %#v, want two hygienically cloned component invocations", root.Children)
	}

	live := clientvm.NewIsland(composed, props)
	if got := strings.TrimSpace(renderTreeText(live.CurrentTree())); !strings.Contains(got, "Inbox4") {
		t.Fatalf("initial composed text = %q", got)
	}
	live.Dispatch("increment", `{}`)
	if got := renderTreeText(live.CurrentTree()); strings.Count(got, "5") != 3 {
		t.Fatalf("text after one parent handler dispatch = %q, want all three signal projections updated", got)
	}
}

func TestIslandPureViewCompositionIsDeterministic(t *testing.T) {
	first := compileIslandProgram(t, composedIslandSource, "Dashboard")
	second := compileIslandProgram(t, composedIslandSource, "Dashboard")
	firstBinary, err := islandprogram.EncodeBinary(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBinary, err := islandprogram.EncodeBinary(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBinary, secondBinary) {
		t.Fatal("same source produced nondeterministic .gxi bytes")
	}
}

func TestIslandPureViewCompositionHonorsSameFileBuiltinShadows(t *testing.T) {
	const sourceTemplate = `package app

component SHADOW() {
	return <span>shadowed</span>
}

//gosx:island
component Root() {
	return <SHADOW />
}
`
	for _, tag := range []string{"If", "Each", "For", "Show", "When", "Link", "Image"} {
		t.Run(tag, func(t *testing.T) {
			source := strings.ReplaceAll(sourceTemplate, "SHADOW", tag)
			lowered := compileIslandProgram(t, source, "Root")
			if lowered.Nodes[lowered.Root].Tag != "span" {
				t.Fatalf("lowered same-file %s shadow = %#v, want composed span", tag, lowered.Nodes)
			}
		})
	}
}

func TestSyntheticJSXConditionalsIgnoreLocalIfShadow(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantConditionals int
		wantTrueTags     string
		wantFalseTags    string
	}{
		{
			name:             "logical and",
			body:             `{props.Show && <strong>shown</strong>}`,
			wantConditionals: 1,
			wantTrueTags:     "div,strong",
			wantFalseTags:    "div",
		},
		{
			name:             "jsx ternary",
			body:             `{props.Show ? <strong>shown</strong> : <small>hidden</small>}`,
			wantConditionals: 2,
			wantTrueTags:     "div,strong",
			wantFalseTags:    "div,small",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf(`package app

type RootProps struct { Show bool }

component If() {
	return <em>shadow</em>
}

//gosx:island
component Root(props: RootProps) {
	return <div>%s</div>
}

component ServerRoot(props: RootProps) {
	return <div>{props.Show && <strong>shown</strong>}</div>
}
`, tc.body)
			lowered := compileIslandProgram(t, source, "Root")
			conditionals := 0
			for _, node := range lowered.Nodes {
				if node.Kind == islandprogram.NodeConditional {
					conditionals++
				}
				if node.Tag == "em" || node.Text == "shadow" {
					t.Fatalf("compiler-owned conditional resolved through local If shadow: %#v", lowered.Nodes)
				}
			}
			if conditionals != tc.wantConditionals {
				t.Fatalf("conditional node count = %d, want %d: %#v", conditionals, tc.wantConditionals, lowered.Nodes)
			}

			for _, state := range []struct {
				show bool
				want string
			}{
				{show: true, want: tc.wantTrueTags},
				{show: false, want: tc.wantFalseTags},
			} {
				props := fmt.Sprintf(`{"Show":%t}`, state.show)
				tree := clientvm.ResolveInitialTree(lowered, props)
				if got := strings.Join(resolvedTags(tree), ","); got != state.want {
					t.Fatalf("resolved tags for props %s = %q, want %q", props, got, state.want)
				}

				prog, err := gosx.Compile([]byte(source))
				if err != nil {
					t.Fatalf("Compile server branch: %v", err)
				}
				html, err := route.RenderProgramComponent(prog, "ServerRoot", route.ProgramRenderEnv{
					Props: struct{ Show bool }{Show: state.show},
				})
				if err != nil {
					t.Fatalf("RenderProgramComponent: %v", err)
				}
				wantHTML := "<div></div>"
				if state.show {
					wantHTML = "<div><strong>shown</strong></div>"
				}
				if html != wantHTML {
					t.Fatalf("server HTML for Show=%t = %q, want %q", state.show, html, wantHTML)
				}
			}
		})
	}
}

func TestIslandPureViewCompositionRendersOneHydrationRoot(t *testing.T) {
	prog, err := gosx.Compile([]byte(composedIslandSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	renderer := island.NewRenderer("composition-test")
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		RenderIsland: renderer.RenderIslandFromProgram,
	})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if got := strings.Count(html, `data-gosx-island="Dashboard"`); got != 1 {
		t.Fatalf("rendered HTML has %d Dashboard hydration roots, want exactly one:\n%s", got, html)
	}
	if strings.Contains(html, `data-gosx-island="Badge"`) {
		t.Fatalf("pure-view callee became a second hydration root:\n%s", html)
	}
	if got := strings.Count(html, "<article"); got != 2 {
		t.Fatalf("server render has %d composed articles, want two:\n%s", got, html)
	}
	if !strings.Contains(html, "<h2>Inbox</h2>") || !strings.Contains(html, "<strong>Primary</strong>") {
		t.Fatalf("server render omitted composed props or named-slot content:\n%s", html)
	}
}

func TestIslandCompositionFailsClosedDuringCompile(t *testing.T) {
	tests := map[string]struct {
		source string
		want   string
	}{
		"root children": {
			source: `package app
//gosx:island
component Root() {
	return <div>{children}</div>
}`,
			want: "cannot declare caller children or named slots",
		},
		"nested island": {
			source: `package app
//gosx:island
component Child() {
	return <span>child</span>
}
//gosx:island
component Root() {
	return <div><Child /></div>
}`,
			want: "nested island <Child>",
		},
		"spread": {
			source: `package app
type ViewProps struct { Label string }
component View(props: ViewProps) {
	return <span>{props.Label}</span>
}
//gosx:island
component Root(props: ViewProps) {
	return <div><View {...props} /></div>
}`,
			want: "uses a spread inside island Root",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := gosx.Compile([]byte(tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Compile error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func BenchmarkIslandPureViewCompositionBaseline(b *testing.B) {
	composedIR, err := gosx.Compile([]byte(composedIslandSource))
	if err != nil {
		b.Fatal(err)
	}
	flattenedIR, err := gosx.Compile([]byte(flattenedIslandSource))
	if err != nil {
		b.Fatal(err)
	}
	composedIndex := indexOf(composedIR, "Dashboard")
	flattenedIndex := indexOf(flattenedIR, "Dashboard")

	b.Run("lower/composed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := ir.LowerIsland(composedIR, composedIndex); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("lower/flattened", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := ir.LowerIsland(flattenedIR, flattenedIndex); err != nil {
				b.Fatal(err)
			}
		}
	})

	composed := compileIslandProgram(b, composedIslandSource, "Dashboard")
	flattened := compileIslandProgram(b, flattenedIslandSource, "Dashboard")
	const props = `{"Label":"Inbox","Start":4}`
	b.Run("initial-resolve/composed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = clientvm.ResolveInitialTree(composed, props)
		}
	})
	b.Run("initial-resolve/flattened", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = clientvm.ResolveInitialTree(flattened, props)
		}
	})
}

type testingTB interface {
	Helper()
	Fatalf(string, ...any)
}

func compileIslandProgram(t testingTB, source, name string) *islandprogram.Program {
	t.Helper()
	prog, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	lowered, err := ir.LowerIsland(prog, indexOf(prog, name))
	if err != nil {
		t.Fatalf("LowerIsland(%s): %v", name, err)
	}
	return lowered
}

func resolvedTags(tree *clientvm.ResolvedTree) []string {
	if tree == nil {
		return nil
	}
	var tags []string
	for _, node := range tree.Nodes {
		if node.Tag != "" {
			tags = append(tags, node.Tag)
		}
	}
	return tags
}
