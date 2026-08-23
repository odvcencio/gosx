package format

import (
	"bytes"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
)

func TestSourcePreservesRenderedSemanticWhitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name: "mixed text hole parentheses",
			source: `package main

func Page() Node {
	return <p>({"ABC"})</p>
}
`,
		},
		{
			name: "at domain adjacency",
			source: `package main

func Page() Node {
	return <p>{"support"}@stablekernel.com</p>
}
`,
		},
		{
			name: "punctuation adjacency",
			source: `package main

func Page() Node {
	return <p>/range:{"value"},next.</p>
}
`,
		},
		{
			name: "intentional one space boundaries",
			source: `package main

func Page() Node {
	return <p>before {"middle"} after</p>
}
`,
		},
		{
			name: "adjacent inline elements without space",
			source: `package main

func Page() Node {
	return <p><b>A</b><i>B</i></p>
}
`,
		},
		{
			name: "adjacent inline elements with space",
			source: `package main

func Page() Node {
	return <p><b>A</b> <i>B</i></p>
}
`,
		},
		{
			name: "expression element expression",
			source: `package main

func Page() Node {
	return <p>{"A"}<b>B</b>{"C"}</p>
}
`,
		},
		{
			name: "local component self closing fragment adjacency",
			source: `package main

func Badge(props any) Node {
	return <b>badge</b>
}

func Page() Node {
	return <><Badge /><i>!</i><Badge /></>
}
`,
		},
		{
			name: "long mixed run wraps at supplied spaces",
			source: `package main

func Page() Node {
	return <p><b>A</b> <b>B</b> <b>C</b> <b>D</b> <b>E</b> <b>F</b></p>
}
`,
		},
		{
			name:   "multiline prose",
			source: "package main\n\nfunc Page() Node {\n\treturn <p>\n\t\tThis is a long\n\t\tparagraph with UTF-8 café text.\n\t</p>\n}\n",
		},
		{
			name: "whitespace only boundary",
			source: `package main

func Page() Node {
	return <p><b>A</b>   <i>B</i></p>
}
`,
		},
		{
			name: "utf8 and entities",
			source: `package main

func Page() Node {
	return <p>café &amp; naïve → ok</p>
}
`,
		},
		{
			name: "attributes stay byte stable",
			source: `package main

func Page() Node {
	return <p data-label="a  b" data-utf8="café" aria-label="(ABC)">text</p>
}
`,
		},
		{
			name: "raw script and style",
			source: `package main

func Page() Node {
	return <div><script>if (a < b) { go(); }</script><style>.a > .b { color: red; }</style></div>
}
`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			formatted, err := Source([]byte(tc.source))
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			twice, err := Source(formatted)
			if err != nil {
				t.Fatalf("Source (second pass): %v\n%s", err, formatted)
			}
			if !bytes.Equal(formatted, twice) {
				t.Fatalf("formatter is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, twice)
			}

			want := renderFormatterFixture(t, []byte(tc.source))
			got := renderFormatterFixture(t, formatted)
			if got != want {
				t.Fatalf("formatted source changed rendered HTML\nwant: %q\ngot:  %q\nformatted:\n%s", want, got, formatted)
			}
			gotTwice := renderFormatterFixture(t, twice)
			if gotTwice != want {
				t.Fatalf("twice-formatted source changed rendered HTML\nwant: %q\ngot:  %q", want, gotTwice)
			}

			if tc.name == "long mixed run wraps at supplied spaces" &&
				!bytes.Contains(formatted, []byte("</b>\n\t\t<b>B</b>\n\t\t<b>C</b>")) {
				t.Fatalf("expected a child-boundary wrap at supplied spaces:\n%s", formatted)
			}
		})
	}
}

func renderFormatterFixture(t *testing.T, source []byte) string {
	t.Helper()
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatalf("Compile: %v\n%s", err, source)
	}
	html, err := route.RenderProgramComponent(prog, "Page", route.ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("RenderProgramComponent: %v\n%s", err, source)
	}
	return html
}
