package docs

import (
	"os"
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
	for _, want := range []string{`<main>`, `class="badge"`, `Inbox`, `0`, `</main>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
	if strings.Contains(html, "className") {
		t.Fatalf("rendered HTML retained className alias: %q", html)
	}
}

func TestDocumentedConcatSampleCompilesAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictConcatSample))
	if err != nil {
		t.Fatalf("compile documented concat sample: %v", err)
	}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render documented concat sample: %v", err)
	}
	if !strings.Contains(html, `class="badge tone-alert"`) {
		t.Fatalf("rendered HTML %q does not contain the concatenated class", html)
	}
}

func TestDocumentedConditionalSampleCompilesAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictConditionalSample))
	if err != nil {
		t.Fatalf("compile documented conditional sample: %v", err)
	}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render documented conditional sample: %v", err)
	}
	if !strings.Contains(html, "Ready") || strings.Contains(html, "Pending") {
		t.Fatalf("rendered HTML %q does not match the cond=true branch", html)
	}
}

// TestDocumentedEachSampleCompilesAndRenders proves the #182 docs sample:
// a strict <Each of={...} as="stat" index="i"> loop over a same-file
// struct slice, called from a legacy Page via a single tier-2 spread.
func TestDocumentedEachSampleCompilesAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictEachSample))
	if err != nil {
		t.Fatalf("compile documented each sample: %v", err)
	}
	// Stat's name must match the .gsx element schema's bare struct name
	// exactly (case-sensitive) — requireStrictSliceValue proves identity by
	// reflect.Type.Name(), not by field-shape compatibility.
	type Stat struct {
		Label string
		Value string
	}
	type loaderCard struct {
		Stats []Stat
	}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		Values: map[string]any{
			"loaderCard": loaderCard{Stats: []Stat{{Label: "Views", Value: "12"}}},
		},
	})
	if err != nil {
		t.Fatalf("render documented each sample: %v", err)
	}
	for _, want := range []string{"<li>0: Views = 12</li>"} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
}

// TestDocumentedSpreadSampleCompilesAndRenders proves the #184 docs
// sample end to end: Page (legacy) spreads a loader-shaped struct into
// Panel (a legacy tier-2 spread), and Panel forwards its own props
// verbatim to Badge with a bare {...props} spread (a strict-to-strict
// tier-1 spread, gosx#182/#184 M-1) — both the outer call and Panel's own
// forwarding call must render, not just compile.
func TestDocumentedSpreadSampleCompilesAndRenders(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictSpreadSample))
	if err != nil {
		t.Fatalf("compile documented spread sample: %v", err)
	}
	type loaderRow struct {
		Label string
		Count int
	}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{
		Values: map[string]any{
			"data": map[string]any{
				"loaderRow": loaderRow{Label: "Inbox", Count: 3},
			},
		},
	})
	if err != nil {
		t.Fatalf("render documented spread sample: %v", err)
	}
	if !strings.Contains(html, "Inbox: 3") {
		t.Fatalf("rendered HTML %q does not contain the spread values", html)
	}
	if want := `<div class="panel"><span>Inbox: 3</span></div>`; !strings.Contains(html, want) {
		t.Fatalf("rendered HTML %q does not contain Panel's own forwarding call %q", html, want)
	}
}

func TestDocumentedComponentsPageCompilesAndTypeChecks(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gosx.Compile(source); err != nil {
		t.Fatalf("compile components docs page: %v", err)
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

// TestStrictComponentCallsRejectSpreadPropsAndPositionalChildren covers the
// v0.39 shape rejections that remain unconditional; the v0.44 (#182/#184)
// docs page ("Loops and spread props" below) covers the two narrow cases
// v0.44 admits: a spread whose source type exactly matches the callee's
// props type (a strict caller) or a legacy caller's single spread. The
// "spread props" case here uses a mismatched source type so it stays
// rejected under the new, narrower rule — a bare-props spread of the exact
// same type is now valid, so this fixture must not reuse that shape.
func TestStrictComponentCallsRejectSpreadPropsAndPositionalChildren(t *testing.T) {
	tests := map[string]string{
		"spread props of mismatched type": `package profile

type BadgeProps struct { Label string }

component Badge(props: BadgeProps) {
	return <span>{props.Label}</span>
}

type PageProps struct { Label string }

component Page(props: PageProps) {
	return <Badge {...props} />
}`,
		"positional children": `package profile

type BadgeProps struct { Label string }

component Badge(props: BadgeProps) {
	return <span>{props.Label}</span>
}

component Page() {
	return <Badge Label="Inbox">unbound child</Badge>
}`,
		"element spread": `package profile

type PageProps struct { Attrs map[string]any }

component Page(props: PageProps) {
	return <div {...props.Attrs}>content</div>
}`,
	}

	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := gosx.Compile([]byte(source)); err == nil {
				t.Fatalf("strict component call accepted %s", name)
			}
		})
	}
}
