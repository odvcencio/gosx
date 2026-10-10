package format

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSourceFormatsNestedElements(t *testing.T) {
	formatted, err := Source([]byte(`package main

func Page() Node {
	return <main  class="page"><section  class="card"><h1>Hi</h1></section></main>
}
`))
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	output := string(formatted)
	for _, snippet := range []string{`<main class="page"><section class="card">`, "<h1>Hi</h1>"} {
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

func TestSourcePreservesWrappedTextWithoutDrift(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <article>
		<p>
			This example is a real GoSX app, not a brochure hung next to one.
					Routes, server actions, auth, client navigation, and Scene3D all live in the same
							codebase.
		</p>
	</article>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}

	if !bytes.Equal(source, formatted) {
		t.Fatalf("expected authored multiline text to remain exact, got:\n%s", formatted)
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
	if !bytes.Equal(source, formatted) {
		t.Fatalf("expected raw-string literal whitespace to remain exact, got:\n%s", output)
	}

	reformatted, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source (second pass): %v", err)
	}
	if string(reformatted) != output {
		t.Fatalf("raw string formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", output, reformatted)
	}
}
