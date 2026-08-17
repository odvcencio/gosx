package route

import (
	"testing"

	"m31labs.dev/gosx"
)

// TestDefaultRenderedComponentSortsAttrsDeterministically proves gosx#188:
// defaultRenderedComponent used to iterate its attrs map in Go's randomized
// map order, so two renders of the same input could emit the same
// attributes in a different order. Sorting the names before emission makes
// the output byte-identical across repeated calls, regardless of map
// iteration order.
func TestDefaultRenderedComponentSortsAttrsDeterministically(t *testing.T) {
	attrs := map[string]any{
		"lang":      "go",
		"source":    "func main() {}",
		"title":     "Example",
		"line":      "12",
		"theme":     "dark",
		"copyable":  true,
		"data-slot": "primary",
	}
	want := `<div data-gosx-component="CodeBlock" copyable data-slot="primary" lang="go" line="12" source="func main() {}" theme="dark" title="Example"></div>`

	first := defaultRenderedComponent("CodeBlock", attrs, "")
	if first != want {
		t.Fatalf("attribute order not sorted alphabetically:\n got:  %s\n want: %s", first, want)
	}

	for i := 0; i < 20; i++ {
		got := defaultRenderedComponent("CodeBlock", attrs, "")
		if got != first {
			t.Fatalf("render %d differs from render 0 (nondeterministic attribute order):\n render 0: %s\n render %d: %s", i, first, i, got)
		}
	}
}

// TestUnresolvedComponentReferenceRendersDeterministically proves gosx#188
// end to end through the public render entry: a <CodeBlock/> reference with
// six attributes that gosx does not resolve locally falls back to
// defaultRenderedComponent (route/fileprogram.go's writeElement, the
// data-gosx-component hydration marker). Twenty renders of the same
// compiled program must produce byte-identical HTML.
func TestUnresolvedComponentReferenceRendersDeterministically(t *testing.T) {
	src := []byte(`package main

func Page() Node {
	return <CodeBlock lang="go" source="func main() {}" title="Example" line="12" theme="dark" copyable="true"/>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	first, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render 0: %v", err)
	}

	for i := 1; i < 20; i++ {
		got, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
		if err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("render %d differs from render 0 (nondeterministic attribute order):\n render 0: %s\n render %d: %s", i, first, i, got)
		}
	}
}
