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

// hasManagedFormAttr reports whether html contains the bare
// gosx.ManagedFormAttr contract attribute as its own attribute — not
// merely as a prefix of a longer attribute that happens to share the name,
// such as data-gosx-form-state or data-gosx-form-mode. A plain
// strings.Contains(html, gosx.ManagedFormAttr) check passes on either of
// those alone, which would let a regression that dropped the bare
// attribute go undetected (gosx#179 F6). The bare attribute is always
// preceded by a space (it never opens a tag), and is followed by either
// another space (another attribute follows) or the tag's closing ">".
func hasManagedFormAttr(html string) bool {
	return strings.Contains(html, " "+gosx.ManagedFormAttr+" ") ||
		strings.Contains(html, " "+gosx.ManagedFormAttr+">")
}

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
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered managed-shorthand form html %q", want, html)
		}
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the bare %s attribute in rendered managed-shorthand form html %q", gosx.ManagedFormAttr, html)
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
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered bare-shorthand form html %q", want, html)
		}
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the bare %s attribute in rendered bare-shorthand form html %q", gosx.ManagedFormAttr, html)
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
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered override html %q", want, html)
		}
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the bare %s attribute in rendered override html %q", gosx.ManagedFormAttr, html)
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
	if !hasManagedFormAttr(managedHTML) {
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

// TestFileRendererManagedFormShorthandPreservesAuthorModeAttribute covers
// gosx#179 F1: managedFormAttrs used to strip server.NavigationFormModeAttr
// unconditionally, so an author-written data-gosx-form-mode next to the
// shorthand silently vanished — a managed-GET form (method="post" plus
// data-gosx-form-mode="get") would then be rewritten to managed POST by
// the client runtime's refreshManagedForms, which reads the HTML method
// attribute once no data-gosx-form-mode survives. The fix only strips the
// mode attribute when the contract computed its own (contractMode != ""),
// which the shorthand alone never does.
func TestFileRendererManagedFormShorthandPreservesAuthorModeAttribute(t *testing.T) {
	html := compileManagedFormFixture(t, `<form method="post" action="/a" data-gosx-form-mode="get" data-gosx-managed>`)

	if !strings.Contains(html, gosx.ManagedFormModeAttr+`="get"`) {
		t.Fatalf("expected author-written %s=\"get\" to survive, got %q", gosx.ManagedFormModeAttr, html)
	}
	if n := strings.Count(html, gosx.ManagedFormModeAttr); n != 1 {
		t.Fatalf("expected %s to appear once, appeared %d times in %q", gosx.ManagedFormModeAttr, n, html)
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the bare %s attribute in rendered html %q", gosx.ManagedFormAttr, html)
	}
	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute removed from output, got %q", html)
	}
}

// TestFileRendererManagedFormShorthandDropsActionNameOnRawForm documents a
// known, low-impact side effect of the shorthand-expansion strip logic
// (gosx#179 F8): a raw <form>'s author-written actionName attribute — a
// prop meaningful only on the <ActionForm> builtin, never real HTML — does
// not survive expansion. managedFormAttrs strips actionName
// unconditionally, a rule the F1 fix (which only made the mode-attribute
// strip conditional) leaves untouched. This has no runtime impact: a raw
// <form>'s actionName was never valid HTML and the browser ignores it
// either way.
func TestFileRendererManagedFormShorthandDropsActionNameOnRawForm(t *testing.T) {
	html := compileManagedFormFixture(t, `<form method="post" action="/a" actionName="save" data-gosx-managed>`)

	if strings.Contains(html, "actionName") {
		t.Fatalf("expected actionName stripped from expanded raw <form>, got %q", html)
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the form still expanded, got %q", html)
	}
}

// TestFileRendererStripsSpreadSuppliedManagedFormShorthand covers gosx#179
// F4: stripManagedFormShorthandAttr matches attrs by ir.Attr.Name, but a
// {...extra}-supplied shorthand has no Name of its own — the key lives
// inside the spread map's evaluated value, rendered later by
// renderFileSpreadAttrs. Detection already worked (attrValue searches
// spread maps too), so the shorthand still expanded; only the strip step
// missed it, leaving the raw data-gosx-managed key rendered beside the full
// contract it triggered.
func TestFileRendererStripsSpreadSuppliedManagedFormShorthand(t *testing.T) {
	src := []byte(`package docs

func Page() Node {
	return <main>
		<form method="post" action="/save" {...extra}>
			<input name="q" value="docs"></input>
		</form>
	</main>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"extra": map[string]any{"data-gosx-managed": "true"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the spread-supplied shorthand to expand, got %q", html)
	}
	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected the spread-supplied shorthand key stripped from output, got %q", html)
	}
}

// TestFileRendererSpreadShorthandFalseLeavesFormUnmanaged is the opt-out
// counterpart to the spread-supplied truthy case above: when the
// spread-supplied value is falsy, the shorthand key must survive exactly
// as authored, matching the direct-attribute opt-out rule
// (TestFileRendererManagedFormShorthandFalseOptsOut).
func TestFileRendererSpreadShorthandFalseLeavesFormUnmanaged(t *testing.T) {
	src := []byte(`package docs

func Page() Node {
	return <main>
		<form method="post" action="/save" {...extra}>
			<input name="q" value="docs"></input>
		</form>
	</main>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{
		Values: map[string]any{"extra": map[string]any{"data-gosx-managed": "false"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(html, `data-gosx-managed="false"`) {
		t.Fatalf("expected the literal opt-out attribute in output, got %q", html)
	}
	if hasManagedFormAttr(html) {
		t.Fatalf("expected no expansion for a falsy spread-supplied shorthand, got %q", html)
	}
}

// TestFileRendererManagedFormBuiltinStripsShorthand covers gosx#179 F9: the
// <Form>/<ActionForm> builtins are always managed, so an author-written
// data-gosx-managed shorthand beside them is pure noise. writeManagedForm
// used to render it unchanged next to the full contract it did nothing to
// produce; it must now be stripped, matching how the shorthand disappears
// once it does something on a raw <form>.
func TestFileRendererManagedFormBuiltinStripsShorthand(t *testing.T) {
	src := []byte(`package docs

func Page() Node {
	return <main>
		<Form method="post" action="/save" data-gosx-managed>
			<input name="q" value="docs"></input>
		</Form>
	</main>
}
`)
	prog, err := gosx.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute stripped from <Form> builtin output, got %q", html)
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the builtin's own contract in output, got %q", html)
	}
}
