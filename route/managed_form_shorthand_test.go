package route

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// These tests render a .gsx fixture through the real pipeline —
// gosx.Compile followed by RenderProgramComponent, the same entry point
// route.RenderProgramComponent documents and cmd/gosx's `gosx render` and
// the file-based dev/prod server both use — so they catch a regression in
// the actual .gsx render path, not just in a Node-API-only helper.
//
// gosx#179: data-gosx-managed on a <form> rendered through the .gsx
// pipeline used to serve unexpanded, because only node.go's RenderHTML
// (the Go Node API path) expanded the shorthand. The file-program
// renderer in this package lowers .gsx templates and wrote attributes
// through its own path, which never called the shared expansion helper.

func compileManagedFormFixture(t *testing.T, formOpenTag string) string {
	t.Helper()
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t" + formOpenTag + "\n" +
		"\t\t\t<input name=\"q\" value=\"docs\"></input>\n" +
		"\t\t</form>\n" +
		"\t</main>\n" +
		"}\n"
	prog, err := gosx.Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return html
}

func TestFileRendererExpandsManagedFormShorthandTrue(t *testing.T) {
	html := compileManagedFormFixture(t, `<form method="post" action="/x/__actions/y" data-gosx-managed="true">`)

	for _, want := range []string{
		`method="post"`,
		`action="/x/__actions/y"`,
		gosx.ManagedFormAttr,
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered managed-shorthand form html %q", want, html)
		}
	}
	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute removed from output, got %q", html)
	}
	// The shorthand must not add a mode attribute — the HTML method
	// attribute stays authoritative, matching node.go's expansion rule.
	if strings.Contains(html, gosx.ManagedFormModeAttr) {
		t.Fatalf("expected no %s from the shorthand alone, got %q", gosx.ManagedFormModeAttr, html)
	}
}

func TestFileRendererExpandsManagedFormShorthandBare(t *testing.T) {
	html := compileManagedFormFixture(t, `<form action="/save" data-gosx-managed>`)

	for _, want := range []string{
		gosx.ManagedFormAttr,
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered bare-shorthand form html %q", want, html)
		}
	}
	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute removed from output, got %q", html)
	}
}

func TestFileRendererManagedFormShorthandFalseOptsOut(t *testing.T) {
	html := compileManagedFormFixture(t, `<form action="/save" data-gosx-managed="false">`)

	if !strings.Contains(html, `data-gosx-managed="false"`) {
		t.Fatalf("expected the literal opt-out attribute in output, got %q", html)
	}
	for _, unwanted := range []string{
		gosx.ManagedFormAttr,
		gosx.ManagedFormStateAttr,
		gosx.EnhancementAttr,
		gosx.EnhancementLayerAttr,
		gosx.RuntimeFallbackAttr,
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("expected no expansion for data-gosx-managed=\"false\", found %q in %q", unwanted, html)
		}
	}
}

func TestFileRendererManagedFormShorthandKeepsAuthorOverride(t *testing.T) {
	html := compileManagedFormFixture(t, `<form action="/save" data-gosx-managed data-gosx-form-state="pending">`)

	if !strings.Contains(html, gosx.ManagedFormStateAttr+`="pending"`) {
		t.Fatalf("expected author-written %s=\"pending\" to survive, got %q", gosx.ManagedFormStateAttr, html)
	}
	if n := strings.Count(html, gosx.ManagedFormStateAttr); n != 1 {
		t.Fatalf("expected %s to appear once, appeared %d times in %q", gosx.ManagedFormStateAttr, n, html)
	}
	for _, want := range []string{
		gosx.ManagedFormAttr,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered override html %q", want, html)
		}
	}
}

// TestFileRendererManagedFormShorthandOmittedLeavesFormUnmanaged documents
// that a plain POST form without the shorthand (and without an auto-managed
// GET method) stays unmanaged, so the shorthand test set above proves an
// actual change of behavior rather than a form the renderer always manages.
func TestFileRendererManagedFormShorthandOmittedLeavesFormUnmanaged(t *testing.T) {
	html := compileManagedFormFixture(t, `<form method="post" action="/save">`)

	for _, unwanted := range []string{
		gosx.ManagedFormAttr,
		gosx.ManagedFormStateAttr,
		gosx.EnhancementAttr,
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("expected unmanaged form, found %q in %q", unwanted, html)
		}
	}
}

// TestFileRendererManagedFormShorthandDynamicExpressionExpands covers the
// shorthand reaching a form through a .gsx dynamic attribute expression
// (data-gosx-managed={expr}) instead of a string literal, the other way the
// shorthand can appear in a .gsx template.
func TestFileRendererManagedFormShorthandDynamicExpressionExpands(t *testing.T) {
	src := []byte(`package docs

func Page() Node {
	return <main>
		<form action="/save" data-gosx-managed={managed}>
			<input name="q" value="docs"></input>
		</form>
	</main>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	managedHTML, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"managed": true},
	})
	if err != nil {
		t.Fatalf("render (managed=true): %v", err)
	}
	if !strings.Contains(managedHTML, gosx.ManagedFormAttr) {
		t.Fatalf("expected expansion when the expression evaluates truthy, got %q", managedHTML)
	}
	if strings.Contains(managedHTML, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute removed from output, got %q", managedHTML)
	}

	unmanagedHTML, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"managed": false},
	})
	if err != nil {
		t.Fatalf("render (managed=false): %v", err)
	}
	if strings.Contains(unmanagedHTML, gosx.ManagedFormAttr) {
		t.Fatalf("expected no expansion when the expression evaluates falsy, got %q", unmanagedHTML)
	}
}
