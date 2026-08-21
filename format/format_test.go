package format

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
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
	if !strings.Contains(output, "return <>\n\t\t<If") {
		t.Fatalf("expected fragment children to stay nested under return indentation, got:\n%s", output)
	}
	if !strings.Contains(output, "\n\t</>\n") {
		t.Fatalf("expected fragment close to align with return statement, got:\n%s", output)
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
	// Mixed text is an opaque CST span for the formatter. The shared semantic
	// layer, rather than source rewriting, removes the layout-only line breaks
	// when either pipeline renders it.
	if !strings.Contains(output, "This example is a real GoSX app, not a brochure hung next to one.\n") ||
		!strings.Contains(output, "same\n\t\t\t\t\t\t\tcodebase.") {
		t.Fatalf("expected mixed text bytes to remain available to the semantic layer, got:\n%s", output)
	}
	first, err := gosx.Compile([]byte(`package main

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
		t.Fatalf("Compile(source): %v", err)
	}
	second, err := gosx.Compile(formatted)
	if err != nil {
		t.Fatalf("Compile(formatted): %v", err)
	}
	if got, want := first.Components[0].Name, second.Components[0].Name; got != want {
		t.Fatalf("formatted source changed component identity: got %q want %q", got, want)
	}
	const wantHTML = `<article><p>This example is a real GoSX app, not a brochure hung next to one. Routes, server actions, auth, client navigation, and Scene3D all live in the same codebase.</p></article>`
	firstHTML, err := route.RenderProgramComponent(first, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render original source: %v", err)
	}
	secondHTML, err := route.RenderProgramComponent(second, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render formatted source: %v", err)
	}
	if firstHTML != wantHTML || secondHTML != wantHTML {
		t.Fatalf("formatted source changed rendered sentence\noriginal=%q\nformatted=%q\nwant=%q", firstHTML, secondHTML, wantHTML)
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
	start := strings.Index(string(source), "`")
	end := strings.LastIndex(string(source), "`")
	if start < 0 || end <= start || !strings.Contains(output, string(source[start:end+1])) {
		t.Fatalf("raw string literal changed byte-for-byte:\n%s", output)
	}

	reformatted, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source (second pass): %v", err)
	}
	if string(reformatted) != output {
		t.Fatalf("raw string formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", output, reformatted)
	}
}

func TestSourceCanonicalizesSafeTagGaps(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div  data-x={1}  ><Badge  tone="retro"   /></div>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	output := string(formatted)
	for _, want := range []string{`<div data-x={1}>`, `<Badge tone="retro" />`} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected canonical tag gap %q in:\n%s", want, output)
		}
	}
	second, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if string(second) != output {
		t.Fatalf("canonical output did not converge\nfirst:\n%s\nsecond:\n%s", output, second)
	}
}

func TestSourceKeepsOrdinaryComponentTagCaseSensitive(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <Script>const marker = "ordinary component";</Script>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if got := string(formatted); !strings.Contains(got, `<Script>const marker = "ordinary component";</Script>`) {
		t.Fatalf("ordinary component tag was not preserved as a case-sensitive element:\n%s", got)
	}
}

func TestSourceWrapsLongAttributeListsWithoutRebuildingValues(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <Card title="A deliberately long title that stays opaque" description="A second deliberately long value that stays opaque" data-mode="retro">ready</Card>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	output := string(formatted)
	if !strings.Contains(output, "<Card\n") {
		t.Fatalf("expected long attribute list to wrap safely, got:\n%s", output)
	}
	if strings.Contains(output, "\n>") {
		t.Fatalf("wrapped closing delimiter lost alignment with the opening tag:\n%s", output)
	}
	for _, value := range []string{
		`title="A deliberately long title that stays opaque"`,
		`description="A second deliberately long value that stays opaque"`,
		`data-mode="retro"`,
		">ready</Card>",
	} {
		if !strings.Contains(output, value) {
			t.Fatalf("wrapped tag lost opaque value %q:\n%s", value, output)
		}
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if string(again) != output {
		t.Fatalf("wrapped tag did not converge\nfirst:\n%s\nsecond:\n%s", output, again)
	}
}

func TestCanonicalIndentUsesTabStopsWithoutSpaceBeforeTab(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "tab", raw: "\t", want: "\t"},
		{name: "four spaces", raw: "    ", want: "\t"},
		{name: "tab then four spaces", raw: "\t    ", want: "\t\t"},
		{name: "four spaces then tab", raw: "    \t", want: "\t\t"},
		{name: "one space then tab", raw: " \t", want: "\t"},
		{name: "tab three spaces tab", raw: "\t   \t", want: "\t\t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalIndent(tt.raw)
			if got != tt.want {
				t.Fatalf("canonicalIndent(%q) = %q, want %q", tt.raw, got, tt.want)
			}
			if strings.Contains(got, " \t") {
				t.Fatalf("canonicalIndent(%q) emitted spaces before a tab: %q", tt.raw, got)
			}
			if gotAgain := canonicalIndent(got); gotAgain != got {
				t.Fatalf("canonicalIndent(%q) did not converge: first %q, second %q", tt.raw, got, gotAgain)
			}
		})
	}
}

func TestWrappedAttributesUseCanonicalContinuationIndentForMixedGoIndent(t *testing.T) {
	source := []byte(`package main

func Page() Node {
  return <Card title="A deliberately long title that stays opaque" description="A second deliberately long value that stays opaque" data-mode="retro">ready</Card>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	output := string(formatted)
	if strings.Contains(output, " \t") {
		t.Fatalf("wrapped attributes emitted spaces before tabs:\n%s", output)
	}
	if !strings.Contains(output, "\n\t  title=") || !strings.Contains(output, "\n  >ready") {
		t.Fatalf("wrapped attributes lost canonical mixed-indent continuation:\n%s", output)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if string(again) != output {
		t.Fatalf("mixed-indent wrapped tag did not converge\nfirst:\n%s\nsecond:\n%s", output, again)
	}
}
