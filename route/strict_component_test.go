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
func Page() Node {
	return <AnchorLabel htmlFor="field" url="/docs" />
}
`

func TestRenderProgramComponentNormalizesLegacyCallerToStrictCalleeSchema(t *testing.T) {
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
	path := filepath.Join(t.TempDir(), "page.gsx")
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
