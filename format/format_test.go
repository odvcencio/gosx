package format

import (
	"strings"
	"testing"

	"github.com/odvcencio/gosx"
)

func TestSourceFormatsNestedElements(t *testing.T) {
	formatted, err := Source([]byte(`package main

func Page() Node {
	return <main><section><h1>Hi</h1></section></main>
}
`))
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	output := string(formatted)
	if strings.Contains(output, "<main><section>") {
		t.Fatalf("expected nested elements to expand, got:\n%s", output)
	}
	for _, snippet := range []string{"<main>", "<section>", "<h1>Hi</h1>"} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected %q in formatted output:\n%s", snippet, output)
		}
	}
}

func TestSourcePreservesFragmentIndentationInsideReturnStatements(t *testing.T) {
	formatted, err := Source([]byte(`package main

func NavLink(props any) Node {
	return <>
		<If when={props.Active}>
			<a href={props.Href}>{props.Label}</a>
		</If>
		<If when={props.Active == false}>
			<a href={props.Href}>{props.Label}</a>
		</If>
	</>
}
`))
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	output := string(formatted)
	if strings.Contains(output, "return <>\n\t<If") {
		t.Fatalf("expected fragment children to stay nested under return indentation, got:\n%s", output)
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted source should compile, got %v\n%s", err, output)
	}
}

func TestSourceNormalizesWrappedTextWithoutDrift(t *testing.T) {
	formatted, err := Source([]byte(`package main

func Page() Node {
	return <article>
		<p>
			This example is a real GoSX app, not a brochure hung next to one.
					Routes, server actions, auth, client navigation, and Scene3D all live in the same
							codebase.
		</p>
	</article>
}
`))
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	output := string(formatted)
	if strings.Contains(output, "\n\t\t\t\t\t\t") {
		t.Fatalf("expected wrapped text indentation drift to be removed, got:\n%s", output)
	}
	if !strings.Contains(output, "Routes, server actions, auth, client navigation, and Scene3D all live in the same codebase.") {
		t.Fatalf("expected wrapped text to normalize to one logical line, got:\n%s", output)
	}
}

func TestSourceKeepsRawStringCodeExamplesStable(t *testing.T) {
	formatted, err := Source([]byte("package main\n\nfunc Page() Node {\n\treturn <article>\n\t\t{DocsCodeBlock(\"gosx\", `func Demo() Node {\n\t\t    return <Scene3D>\n\t\t        <div class=\"fallback\">Ready</div>\n\t\t    </Scene3D>\n\t\t}`)}\n\t</article>\n}\n"))
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	output := string(formatted)
	if strings.Count(output, "    return <Scene3D>") != 1 {
		t.Fatalf("expected raw string example indentation to stay stable, got:\n%s", output)
	}
}
