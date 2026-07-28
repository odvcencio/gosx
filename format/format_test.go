package format

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
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
	source := []byte("package main\n\nfunc Page() Node {\n\treturn <article>\n\t\t{CodeBlock(\"go\", `func Demo() Node {\n\t\t    title := \"Scene\"\n\n\t\t    \t\n\t\t    return <Scene3D ariaLabel={title}>\n\t\t        <div class=\"fallback\">Ready</div>\n\t\t    </Scene3D>\n\t\t}`)}\n\t</article>\n}\n")
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	output := string(formatted)
	if strings.Count(output, `    return <Scene3D ariaLabel={title}>`) != 1 {
		t.Fatalf("expected raw string example indentation to stay stable, got:\n%s", output)
	}
	if !strings.Contains(output, "title := \"Scene\"\n\n\n") {
		t.Fatalf("expected empty and whitespace-only raw-string lines to normalize to zero width, got:\n%s", output)
	}
	for lineNumber, line := range strings.Split(output, "\n") {
		if line != "" && strings.Trim(line, " \t\r") == "" {
			t.Fatalf("line %d contains whitespace-only blank content %q:\n%s", lineNumber+1, line, output)
		}
	}

	reformatted, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source (second pass): %v", err)
	}
	if string(reformatted) != output {
		t.Fatalf("raw string formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", output, reformatted)
	}
}
