package route

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

const strictInitialismSource = `package app
type LinkProps struct {
	HTMLFor string
	URL string
}
component AnchorLabel(props: LinkProps) {
	return <a for={props.HTMLFor} href={props.URL}>{props.HTMLFor}</a>
}
component Page() {
	return <AnchorLabel htmlFor="field" url="/docs" />
}
`

func TestRenderProgramComponentNormalizesStrictInitialismSchema(t *testing.T) {
	prog, err := gosx.Compile([]byte(strictInitialismSource))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	for _, want := range []string{`for="field"`, `href="/docs"`, `>field</a>`} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %q", want, html)
		}
	}
}

func TestDefaultFileRendererNormalizesStrictInitialismSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte(strictInitialismSource), 0o600); err != nil {
		t.Fatal(err)
	}
	node, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer: %v", err)
	}
	html := gosx.RenderHTML(node)
	if !strings.Contains(html, `for="field"`) || !strings.Contains(html, `href="/docs"`) {
		t.Fatalf("initialism props were lost: %q", html)
	}
}

func TestDefaultFileRendererDoesNotShellOutForCrossFileStrictComponent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "badge.gsx"), []byte(`package app
component Badge() {
	return <span>badge</span>
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "page.gsx")
	if err := os.WriteFile(path, []byte(`package app
component Page() {
	return <Badge />
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	node, err := DefaultFileRenderer(nil, FilePage{FilePath: path, Pattern: "/"})
	if err != nil {
		t.Fatalf("DefaultFileRenderer performs only context-free IR validation: %v", err)
	}
	if html := gosx.RenderHTML(node); !strings.Contains(html, `data-gosx-component="Badge"`) {
		t.Fatalf("unbound cross-file component should fail soft at runtime, got %q", html)
	}
}

func TestRenderProgramComponentLeavesLegacyCalleeAttrNamesUnchanged(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
func Legacy(props Props) Node {
	return <p>{props.HtmlFor}</p>
}
func Page() Node {
	return <Legacy htmlFor="legacy" />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	page := prog.Components[1]
	call := prog.NodeAt(page.Root)
	if call == nil || len(call.Attrs) != 1 || call.Attrs[0].Name != "htmlFor" {
		t.Fatalf("legacy callee attr changed: %#v", call)
	}
}

func TestRenderProgramComponentCannotReceiveDivergentStrictExpression(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
type Props struct { A int; B int }
component Page(props: Props) {
	return <main>{props.A / props.B}</main>
}
`))
	if err == nil || !strings.Contains(err.Error(), `binary operator "/" is not supported`) {
		t.Fatalf("Compile error = %v", err)
	}
}

func TestRenderProgramComponentStrictSafeLiteralParity(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
component Page() {
	return <main>{"text"}{42}{1.5}{true}{false}</main>
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if html != "<main>text421.5truefalse</main>" {
		t.Fatalf("html = %q", html)
	}
}

func TestStrictServerExpressionRejectsNilInTextAndAttrs(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "text", body: `<main>{nil}</main>`},
		{name: "attribute", body: `<main data-value={nil}>text</main>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gosx.Compile([]byte("package app\ncomponent Page() {\nreturn " + tc.body + "\n}\n"))
			if err == nil || !strings.Contains(err.Error(), "nil is not supported") {
				t.Fatalf("Compile error = %v", err)
			}
		})
	}
}

func TestStrictLocalCallRequiresEveryRenderedProp(t *testing.T) {
	_, err := gosx.Compile([]byte(`package app
type BadgeProps struct {
	Count int
	Enabled bool
	Unused string
}
component Badge(props: BadgeProps) {
	return <p>{props.Count}:{props.Enabled}</p>
}
component Page() {
	return <Badge />
}
`))
	if err == nil {
		t.Fatal("Compile unexpectedly accepted omitted rendered props")
	}
	for _, want := range []string{"requires prop Count", "requires prop Enabled"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile error %q does not contain %q", err, want)
		}
	}
}

func TestStrictLocalCallExplicitZeroValuesMatchGeneratedGo(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package app
type BadgeProps struct {
	Count int
	Enabled bool
}
component Badge(props: BadgeProps) {
	return <p>{props.Count}:{props.Enabled}</p>
}
component Page() {
	return <Badge count={0} enabled={false} />
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v", err)
	}
	if html != "<p>0:false</p>" {
		t.Fatalf("html = %q", html)
	}
}
