package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestRenderedHTMLCarriesNoGSXMarkupComments is the end-to-end proof for
// "comments inside GSX markup compile away": it runs the real pipeline —
// gosx.Compile followed by RenderProgramComponent, the same two calls
// image_attrs_test.go and render_component_test.go already use to render a
// .gsx fixture — over a page whose markup mixes every comment shape the
// product owner's report and the fix cover, and checks the emitted HTML for
// each one by name.
//
// This is what image_attrs_test.go's comment calls "the real pipeline": it
// is one call short of compile_test.go's gosx.Compile (which stops at the
// IR) and does not need readme_example_test.go's separate-module go build
// (which only type-checks, and never produces an HTML string to inspect).
func TestRenderedHTMLCarriesNoGSXMarkupComments(t *testing.T) {
	const source = `package docs

func Page() Node {
	return <div class="page">
		// explain why this block exists
		<h2>Title</h2>
		{/* JSX-style block comment */}
		{// a line-comment-only expression
		}
		<p>LABEL // detail</p>
		<p>See https://example.com/docs for more</p>
		<pre>
			// this is displayed code, not a GSX comment
		</pre>
	</div>
}
`

	prog, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, absent := range []string{
		"explain why this block exists",
		"JSX-style block comment",
		"a line-comment-only expression",
	} {
		if strings.Contains(html, absent) {
			t.Errorf("rendered HTML kept comment text %q:\n%s", absent, html)
		}
	}

	for _, present := range []string{
		"<h2>Title</h2>",
		"LABEL // detail",
		"https://example.com/docs",
		"this is displayed code, not a GSX comment",
	} {
		if !strings.Contains(html, present) {
			t.Errorf("rendered HTML dropped required content %q:\n%s", present, html)
		}
	}
}
