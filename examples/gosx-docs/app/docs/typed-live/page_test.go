package docs

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/route"
)

func TestTypedProofRouteIsStrictAndRendersItsContract(t *testing.T) {
	source, err := os.ReadFile("page.gsx")
	if err != nil {
		t.Fatalf("read typed proof source: %v", err)
	}
	program, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("compile typed proof source: %v", err)
	}
	if len(program.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(program.Components))
	}
	for _, component := range program.Components {
		if component.Syntax != ir.ComponentSyntaxStrict {
			t.Fatalf("component %s syntax = %v, want strict", component.Name, component.Syntax)
		}
	}

	html, err := route.RenderProgramComponent(program, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render typed proof: %v", err)
	}
	for _, want := range []string{
		`class="typed-proof"`,
		`component Name(props: GoType)`,
		`gosx check + Go`,
		`server HTML`,
		`Read this route&#39;s source`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML missing %q: %s", want, html)
		}
	}
}
