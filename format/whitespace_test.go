package format

import (
	"bytes"
	gofmt "go/format"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/transpile"
)

func TestSourcePreservesRenderedContent(t *testing.T) {
	cases := []struct {
		name, markup string
	}{
		{"mixed adjacent", `<p>A<span>/</span>B</p>`},
		{"adjacent elements", `<p><span>A</span><span>B</span></p>`},
		{"self closing", `<p>A<br/>B</p>`},
		{"explicit spaces", `<p>  A  <span>/</span>  B  </p>`},
		{"whitespace sibling", `<p><span>A</span> <span>B</span></p>`},
		{"expressions", `<p>A{props.Label}<span>/</span>{props.Label}B</p>`},
		{"fragment", `<>A<span>/</span>B</>`},
		{"empty fragment", `<div><></></div>`},
		{"structural whitespace", "<article>\n\n    <section>\n  <p>text</p>\n </section>\n</article>"},
		{"long text", `<p>This intentionally long sentence must retain exactly the same rendered text before and after formatting.</p>`},
		{"multiline prose", "<article>\n\t<p>\n\t\tFirst line with  two spaces.\n\t\t\tSecond line.\n\t</p>\n</article>"},
		{"preformatted", "<div><pre>first\n  <code>second\n\tthird</code>\n\n  </pre></div>"},
		{"textarea", "<div><textarea>first\n  second\n</textarea></div>"},
		{"script", "<div>\n<script>var msg = `first\n  second`;\n\n  </script>\n</div>"},
		{"style", "<div>\n<style>.a::before { content: \"a  b\"; }\n  .b { color: red; }</style>\n</div>"},
		{"raw string expression", "<div>\n{`first\n\t  second\n\n\t  `}\n</div>"},
		{"multiline attribute literal", "<div title={`first\n  second`}><span>body</span></div>"},
		{"attribute wrapping", `<p class="a-long-class-name-that-forces-the-formatter-to-wrap-the-opening-tag-onto-multiple-lines" data-label="example">A<span>/</span>B</p>`},
		{"comment line", "<div>\n\t// hidden comment\n\t<p>Visible</p>\n</div>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\n\nfunc Page(props any) Node {\n\treturn " + tc.markup + "\n}\n")
			formatted, err := Source(source)
			if err != nil {
				t.Fatal(err)
			}
			if before, after := renderFixture(t, source), renderFixture(t, formatted); before != after {
				t.Errorf("formatting changed rendered HTML\nbefore: %q\nafter:  %q\nsource:\n%s", before, after, formatted)
			}
			// Compare canonical generated Go as well: the IR renderer and the
			// transpiler have different rules for whitespace-only text nodes.
			if before, after := transpileFixture(t, source), transpileFixture(t, formatted); !bytes.Equal(before, after) {
				t.Errorf("formatting changed generated Go\nbefore:\n%s\nafter:\n%s", before, after)
			}
			again, err := Source(formatted)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(formatted, again) {
				t.Errorf("formatting is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
			}
		})
	}
}

func TestSourceFormatsAttributesAndExistingStructuralIndentation(t *testing.T) {
	source := []byte("package app\nfunc Page() Node {\n\treturn <article  class=\"page\">\n     <p>body</p>\n</article>\n}\n")
	formatted, err := Source(source)
	if err != nil {
		t.Fatal(err)
	}
	want := "\treturn <article class=\"page\">\n\t\t<p>body</p>\n\t</article>"
	if !strings.Contains(string(formatted), want) {
		t.Fatalf("expected canonical tag attributes and existing structural indentation, got:\n%s", formatted)
	}
}

func renderFixture(t *testing.T, source []byte) string {
	t.Helper()
	program, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	node, err := route.RenderProgramComponentNode(program, "Page", route.ProgramRenderEnv{Props: map[string]any{"Label": "value"}})
	if err != nil {
		t.Fatal(err)
	}
	return gosx.RenderHTML(node)
}

func transpileFixture(t *testing.T, source []byte) []byte {
	t.Helper()
	generated, err := transpile.Transpile(source, transpile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := gofmt.Source([]byte(generated))
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
