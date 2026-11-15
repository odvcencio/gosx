package gosx_test

import (
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/markup"
)

// The root names are aliases and thin wrappers, so existing callers keep
// compiling and a node built through either package is the same value.
var (
	_ markup.Node                                   = gosx.Node{}
	_ gosx.Node                                     = markup.Node{}
	_ markup.AttrList                               = gosx.AttrList{}
	_ gosx.ManagedFormOptions                       = markup.ManagedFormOptions{}
	_ gosx.ProgressiveEnhancementOptions            = markup.ProgressiveEnhancementOptions{}
	_ func(string, ...any) markup.Node              = gosx.El
	_ func(string, any) markup.Attribute            = gosx.Attr
	_ func(...any) gosx.AttrList                    = markup.Attrs
	_ func(gosx.Node) string                        = markup.RenderHTML
	_ func(markup.ManagedFormOptions) gosx.AttrList = gosx.ManagedFormAttrs
)

func TestRootReexportsMarkup(t *testing.T) {
	build := []struct {
		name string
		node gosx.Node
	}{
		{"gosx", gosx.El("form", gosx.Attrs(gosx.BoolAttr(gosx.ManagedFormShorthandAttr), gosx.Attr("action", "/save")), gosx.Text("<x>"))},
		{"markup", markup.El("form", markup.Attrs(markup.BoolAttr(markup.ManagedFormShorthandAttr), markup.Attr("action", "/save")), markup.Text("<x>"))},
		{"mixed", gosx.El("form", markup.Attrs(gosx.BoolAttr(markup.ManagedFormShorthandAttr), markup.Attr("action", "/save")), markup.Text("<x>"))},
	}
	want := `<form data-gosx-form data-gosx-form-state="idle" data-gosx-enhance="form" data-gosx-enhance-layer="bootstrap" data-gosx-fallback="native-form" action="/save">&lt;x&gt;</form>`
	for _, b := range build {
		if got := gosx.RenderHTML(b.node); got != want {
			t.Errorf("%s: gosx.RenderHTML = %q, want %q", b.name, got, want)
		}
		if got := markup.RenderHTML(b.node); got != want {
			t.Errorf("%s: markup.RenderHTML = %q, want %q", b.name, got, want)
		}
	}
	consts := map[string][2]string{
		"RuntimeFallbackAttr":      {gosx.RuntimeFallbackAttr, markup.RuntimeFallbackAttr},
		"EnhancementAttr":          {gosx.EnhancementAttr, markup.EnhancementAttr},
		"EnhancementLayerAttr":     {gosx.EnhancementLayerAttr, markup.EnhancementLayerAttr},
		"ManagedFormAttr":          {gosx.ManagedFormAttr, markup.ManagedFormAttr},
		"ManagedFormModeAttr":      {gosx.ManagedFormModeAttr, markup.ManagedFormModeAttr},
		"ManagedFormStateAttr":     {gosx.ManagedFormStateAttr, markup.ManagedFormStateAttr},
		"ManagedFormShorthandAttr": {gosx.ManagedFormShorthandAttr, markup.ManagedFormShorthandAttr},
	}
	for name, pair := range consts {
		if pair[0] != pair[1] {
			t.Errorf("gosx.%s = %q, markup.%s = %q", name, pair[0], name, pair[1])
		}
	}
}

// The root wrappers must cost nothing over calling markup directly: a
// function-variable re-export (var El = markup.El) would make the variadic
// arguments escape and add heap allocations to every server render.
func TestRootWrappersAllocateLikeMarkup(t *testing.T) {
	viaRoot := testing.AllocsPerRun(200, func() {
		n := gosx.El("ul", gosx.Attrs(gosx.Attr("class", "list"), gosx.BoolAttr("hidden")),
			gosx.El("li", gosx.Attrs(gosx.Attr("id", "a")), gosx.Text("one")),
			gosx.El("li", gosx.Text("two")))
		_ = gosx.RenderHTML(n)
	})
	viaMarkup := testing.AllocsPerRun(200, func() {
		n := markup.El("ul", markup.Attrs(markup.Attr("class", "list"), markup.BoolAttr("hidden")),
			markup.El("li", markup.Attrs(markup.Attr("id", "a")), markup.Text("one")),
			markup.El("li", markup.Text("two")))
		_ = markup.RenderHTML(n)
	})
	if viaRoot > viaMarkup {
		t.Fatalf("gosx wrappers allocate %.0f times per tree, markup %.0f", viaRoot, viaMarkup)
	}
	t.Logf("allocations per tree: gosx %.0f, markup %.0f", viaRoot, viaMarkup)
}
