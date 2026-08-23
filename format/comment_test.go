package format

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSourcePreservesExpressionContainerComments(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		comments []string
	}{
		{
			name: "leading and trailing block comments",
			source: `package main

func Page() Node {
	return <p>{/* leading */ "A" /* trailing */}</p>
}
`,
			comments: []string{"/* leading */", "/* trailing */"},
		},
		{
			name: "trailing line comment",
			source: `package main

func Page() Node {
	return <p>{"A" // trailing
}</p>
}
`,
			comments: []string{"// trailing"},
		},
		{
			name: "comment between sibling expressions",
			source: `package main

func Page() Node {
	return <p><b>A</b>{/* between */ " "}<i>B</i></p>
}
`,
			comments: []string{"/* between */"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			formatted, err := Source([]byte(tc.source))
			if err != nil {
				t.Fatalf("Source: %v", err)
			}
			for _, comment := range tc.comments {
				if !bytes.Contains(formatted, []byte(comment)) {
					t.Fatalf("formatter dropped %q:\n%s", comment, formatted)
				}
			}
			twice, err := Source(formatted)
			if err != nil {
				t.Fatalf("Source (second pass): %v\n%s", err, formatted)
			}
			if !bytes.Equal(formatted, twice) {
				t.Fatalf("formatter is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, twice)
			}
			want := renderFormatterFixture(t, []byte(tc.source))
			if got := renderFormatterFixture(t, formatted); got != want {
				t.Fatalf("formatted source changed rendered HTML\nwant: %q\ngot:  %q\nformatted:\n%s", want, got, formatted)
			}
			if got := renderFormatterFixture(t, twice); got != want {
				t.Fatalf("twice-formatted source changed rendered HTML\nwant: %q\ngot:  %q", want, got)
			}
		})
	}
}

func TestSourceRetainsCommentOnlyExpressionBeforeCompile(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <p>{/* comment-only */}</p>
}
`)
	if _, err := gosx.Compile(source); err == nil || !strings.Contains(err.Error(), "missing identifier") {
		t.Fatalf("expected the grammar to reject a comment-only expression with missing identifier, got %v", err)
	}
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if !bytes.Equal(formatted, source) {
		t.Fatalf("formatter dropped unsupported comment-only expression\nsource:\n%s\nformatted:\n%s", source, formatted)
	}
	twice, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source (second pass): %v", err)
	}
	if !bytes.Equal(twice, source) {
		t.Fatalf("comment-only expression changed on second pass\nsource:\n%s\ntwice:\n%s", source, twice)
	}
}
