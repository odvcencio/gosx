package docs

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestNormalizeCodeBlockSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lang   string
		source string
		want   string
	}{
		{
			name:   "common continuation indent",
			lang:   "go",
			source: "outer\n\tchild\n\t\tgrandchild\n\tpeer",
			want:   "outer\nchild\n\tgrandchild\npeer",
		},
		{
			name:   "mixed tabs and spaces use four-column tab stops",
			lang:   "javascript",
			source: "outer\n\tchild\n    peer\n\t\tgrandchild",
			want:   "outer\nchild\npeer\n\tgrandchild",
		},
		{
			name:   "edge blanks trimmed and interior blanks retained",
			lang:   "gosx",
			source: "\n\t\n\t<section>\n\t\t<h1>Title</h1>\n\t\n\t</section>\n\t\n",
			want:   "<section>\n\t<h1>Title</h1>\n\n</section>",
		},
		{
			name:   "nested intentional indent keeps its relative depth",
			lang:   "go",
			source: "    if ready {\n        run()\n    }",
			want:   "if ready {\n    run()\n}",
		},
		{
			name:   "zero indent is a no-op",
			lang:   "go",
			source: "func main() {\n\treturn\n}",
			want:   "func main() {\n\treturn\n}",
		},
		{
			name:   "javascript alias activates dedent",
			lang:   "js",
			source: "const ready = true\n\tstart()\n\tfinish()",
			want:   "const ready = true\nstart()\nfinish()",
		},
		{
			name:   "plain text remains byte exact",
			lang:   "text",
			source: "\n\troot\n\t\tchild\n",
			want:   "\n\troot\n\t\tchild\n",
		},
		{
			name:   "unknown language remains byte exact",
			lang:   "python",
			source: "\n\tif ready:\n\t\tstart()\n",
			want:   "\n\tif ready:\n\t\tstart()\n",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeCodeBlockSource(test.lang, test.source); got != test.want {
				t.Fatalf("normalizeCodeBlockSource(%q, %q) = %q, want %q", test.lang, test.source, got, test.want)
			}
		})
	}
}

func TestCodeBlockUsesNormalizedSourceForContentAndGutter(t *testing.T) {
	t.Parallel()

	node := CodeBlock("http", "\n\tGET /health HTTP/1.1\n\t\tX-Trace: nested\n\tHost: example.test\n\n")
	rendered := gosx.RenderHTML(node)

	if !strings.Contains(rendered, "GET /health HTTP/1.1\n\tX-Trace: nested\nHost: example.test") {
		t.Fatalf("rendered code did not use normalized source: %q", rendered)
	}
	if !strings.Contains(rendered, ">1\n2\n3</pre>") {
		t.Fatalf("line-number gutter did not use normalized 3-line source: %q", rendered)
	}
	if strings.Contains(rendered, ">1\n2\n3\n4</pre>") {
		t.Fatalf("line-number gutter counted trimmed edge blanks: %q", rendered)
	}
}
