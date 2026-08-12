package docs

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

func TestDocumentedStrictComponentCompilesAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictComponentSample))
	if err != nil {
		t.Fatalf("compile documented strict component: %v", err)
	}
	if len(prog.Components) != 2 {
		t.Fatalf("component count = %d, want 2", len(prog.Components))
	}
	for _, component := range prog.Components {
		if component.Syntax != ir.ComponentSyntaxStrict {
			t.Fatalf("component %s syntax = %v, want strict", component.Name, component.Syntax)
		}
	}

	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render documented strict component: %v", err)
	}
	for _, want := range []string{`<main>`, `class="badge"`, `Inbox`, `3`, `</main>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
	if strings.Contains(html, "className") {
		t.Fatalf("rendered HTML retained className alias: %q", html)
	}
}

func TestDocumentedLegacyComponentStillCompilesAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(legacyComponentSample))
	if err != nil {
		t.Fatalf("compile documented legacy component: %v", err)
	}
	if len(prog.Components) != 1 || prog.Components[0].Syntax != ir.ComponentSyntaxLegacy {
		t.Fatalf("legacy components = %#v", prog.Components)
	}

	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": map[string]any{
				"title":     "Profile",
				"showInbox": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("render documented legacy component: %v", err)
	}
	for _, want := range []string{`<h1>Profile</h1>`, `href="/inbox"`, `Open inbox`} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
}

func TestStrictServerSnippetRejectsUnrenderableHelperCall(t *testing.T) {
	source := []byte(`package profile

type Props struct { Label string }

func decorate(value string) string { return value }

component Card(props: Props) {
	return <span>{decorate(props.Label)}</span>
}`)
	if _, err := gosx.Compile(source); err == nil {
		t.Fatal("strict component accepted a free helper call that the server renderer cannot execute")
	}
}
