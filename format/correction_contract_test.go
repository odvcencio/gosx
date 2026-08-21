package format

import (
	"bytes"
	"strings"
	"testing"
)

func TestSourceCompactNestedTreesOwnDestinationIndentAndConverge(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "four levels of elements",
			src:  "package main\n\nfunc Page() Node {\n\treturn <a><b><c><d /></c></b></a>\n}\n",
			want: "package main\n\nfunc Page() Node {\n\treturn <a>\n\t\t<b>\n\t\t\t<c>\n\t\t\t\t<d />\n\t\t\t</c>\n\t\t</b>\n\t</a>\n}\n",
		},
		{
			name: "fragment and raw element",
			src:  "package main\n\nfunc Page() Node {\n\treturn <><a><b><c><d /></c></b></a><script>const x = 1;</script></>\n}\n",
			want: "package main\n\nfunc Page() Node {\n\treturn <>\n\t\t<a>\n\t\t\t<b>\n\t\t\t\t<c>\n\t\t\t\t\t<d />\n\t\t\t\t</c>\n\t\t\t</b>\n\t\t</a>\n\t\t<script>const x = 1;</script>\n\t</>\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := Source([]byte(tt.src))
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if string(first) != tt.want {
				t.Fatalf("unexpected first pass\ngot:\n%s\nwant:\n%s", first, tt.want)
			}
			second, err := Source(first)
			if err != nil {
				t.Fatalf("Source(second pass): %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("formatting did not converge\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

func TestSourceCanonicalizesOnlyInterAttributeGaps(t *testing.T) {
	source := []byte("package main\n\nfunc Page() Node {\n\treturn <Card title  =  \"a\"   {...props}   data={ map[string]string{\"x\": \"y\"} }   raw={`a  b`}  ></Card>\n}\n")
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	want := "package main\n\nfunc Page() Node {\n\treturn <Card title  =  \"a\" {...props} data={ map[string]string{\"x\": \"y\"} } raw={`a  b`}></Card>\n}\n"
	if string(formatted) != want {
		t.Fatalf("formatter rewrote a complete attribute node\ngot:\n%s\nwant:\n%s", formatted, want)
	}
	for _, opaque := range []string{
		`title  =  "a"`,
		`{...props}`,
		`data={ map[string]string{"x": "y"} }`,
		"raw={`a  b`}",
	} {
		if !strings.Contains(string(formatted), opaque) {
			t.Fatalf("complete attribute span %q was not byte-opaque:\n%s", opaque, formatted)
		}
	}
	second, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if !bytes.Equal(formatted, second) {
		t.Fatalf("attribute formatting did not converge\nfirst:\n%s\nsecond:\n%s", formatted, second)
	}
}

func TestSourcePreservesCommentsAndNestedAttributeStrings(t *testing.T) {
	source := []byte("package main\n\nfunc Page() Node {\n\treturn <Card /* opening */ title  =  \"/* value */\" /* between */ {...props} data={ /* nested */ `// value` } />\n}\n")
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, opaque := range []string{
		`/* opening */`,
		`title  =  "/* value */"`,
		`/* between */`,
		`{...props}`,
		"data={ /* nested */ `// value` }",
	} {
		if !strings.Contains(string(formatted), opaque) {
			t.Fatalf("formatted tag lost opaque bytes %q:\n%s", opaque, formatted)
		}
	}
	second, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if !bytes.Equal(formatted, second) {
		t.Fatalf("comment/string formatting did not converge\nfirst:\n%s\nsecond:\n%s", formatted, second)
	}
}

func TestSourceCanonicalizesMixedDestinationIndentOnce(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		child  string
	}{
		{name: "one space", prefix: " ", child: "\t "},
		{name: "two spaces", prefix: "  ", child: "\t  "},
		{name: "three spaces", prefix: "   ", child: "\t   "},
		{name: "mixed tab and spaces", prefix: "\t   ", child: "\t\t   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "package main\n\nfunc Page() Node {\n" + tt.prefix + "return <a><b><c /></b></a>\n}\n"
			formatted, err := Source([]byte(source))
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			lines := strings.Split(string(formatted), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, tt.child) && strings.Contains(line, " \t") {
					t.Fatalf("generated indentation contains space before tab: %q", line)
				}
			}
			if !strings.Contains(string(formatted), "\n"+tt.child+"<b>") {
				t.Fatalf("child indentation did not use display columns: want prefix %q\n%s", tt.child, formatted)
			}
			second, err := Source(formatted)
			if err != nil {
				t.Fatalf("Source(second pass): %v", err)
			}
			if !bytes.Equal(formatted, second) {
				t.Fatalf("mixed indentation did not converge\nfirst:\n%s\nsecond:\n%s", formatted, second)
			}
		})
	}
}

func TestSourceUsesExactRenderTextForBlockDiscardDecisions(t *testing.T) {
	// U+00A0 is considered whitespace by strings.TrimSpace, but it is an
	// intentional rendered separator under syntax.RenderText. A structural
	// parent must therefore stay in the source stream instead of discarding the
	// token as layout-only.
	source := []byte("package main\n\nfunc Page() Node {\n\treturn <p><a />\u00a0<b /></p>\n}\n")
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if !strings.Contains(string(formatted), "<a />\u00a0<b />") {
		t.Fatalf("exact non-ASCII separator was treated as layout-only:\n%s", formatted)
	}
	second, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if !bytes.Equal(formatted, second) {
		t.Fatalf("separator formatting did not converge\nfirst:\n%s\nsecond:\n%s", formatted, second)
	}
}

func TestSourcePreservesMultilineLiteralNBSPAndConverges(t *testing.T) {
	// The NBSP-only line is a literal text token, not an empty layout line.
	// Keep its UTF-8 bytes in the formatter stream and prove a second pass does
	// not turn the semantic token into structural padding.
	source := []byte("package main\n\nfunc Page() Node {\n\treturn <p>\n\t\t\u00a0\n\t</p>\n}\n")
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if !bytes.Contains(formatted, []byte("\n\t\t\u00a0\n")) {
		t.Fatalf("multiline literal NBSP was not preserved byte-for-byte:\n%s", formatted)
	}
	second, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source(second pass): %v", err)
	}
	if !bytes.Equal(formatted, second) {
		t.Fatalf("multiline literal NBSP formatting did not converge\nfirst:\n%s\nsecond:\n%s", formatted, second)
	}
}

func TestSourceBlocksAroundDescendantCommentLookingBytesOnly(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "nested https URL",
			src:  "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main><a href=\"https://example.com/docs\" /><span /></main>\n}\n",
			want: "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main>\n\t\t<a href=\"https://example.com/docs\" />\n\t\t<span />\n\t</main>\n}\n",
		},
		{
			name: "descendant attribute comment markers",
			src:  "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main><a title=\"// not a comment\" /><span data-note=\"/* not a comment */\" /></main>\n}\n",
			want: "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main>\n\t\t<a title=\"// not a comment\" />\n\t\t<span data-note=\"/* not a comment */\" />\n\t</main>\n}\n",
		},
		{
			name: "raw script and style bodies",
			src:  "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main><script>const url = \"https://example.com\"; // script comment\n</script><style>/* style comment */\n.card { color: red; }</style><span /></main>\n}\n",
			want: "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main>\n\t\t<script>const url = \"https://example.com\"; // script comment\n</script>\n\t\t<style>/* style comment */\n.card { color: red; }</style>\n\t\t<span />\n\t</main>\n}\n",
		},
		{
			name: "genuine direct inter-child comment trivia",
			src:  "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main><a /> /* direct inter-child comment */ <span /></main>\n}\n",
			want: "package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn <main><a /> /* direct inter-child comment */ <span /></main>\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := Source([]byte(tt.src))
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			if string(first) != tt.want {
				t.Fatalf("unexpected first pass\ngot:\n%s\nwant:\n%s", first, tt.want)
			}
			second, err := Source(first)
			if err != nil {
				t.Fatalf("Source(second pass): %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("formatting did not converge\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}
