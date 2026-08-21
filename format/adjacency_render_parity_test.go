package format_test

import (
	"testing"

	"m31labs.dev/gosx/format"
	"m31labs.dev/gosx/transpile"
)

func TestSameLineAdjacencyPreservesStrictAndGeneratedLegacyHTML(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "element element",
			body: `<p><b>left</b>{" "}<i>right</i></p>`,
			want: `<p><b>left</b> <i>right</i></p>`,
		},
		{
			name: "element expression",
			body: `<p><b>left</b>{" "}{"right"}</p>`,
			want: `<p><b>left</b> right</p>`,
		},
		{
			name: "expression element",
			body: `<p>{"left"}{" "}<i>right</i></p>`,
			want: `<p>left <i>right</i></p>`,
		},
		{
			name: "fragment adjacency",
			body: `<p><>{"left"}{" "}<i>right</i></></p>`,
			want: `<p>left <i>right</i></p>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := []byte("package main\n\nimport \"m31labs.dev/gosx\"\n\nfunc Page() gosx.Node {\n\treturn " + tt.body + "\n}\n")
			formatted, err := format.Source(source)
			if err != nil {
				t.Fatalf("format.Source: %v", err)
			}
			formattedAgain, err := format.Source(formatted)
			if err != nil {
				t.Fatalf("format.Source(second pass): %v", err)
			}
			if string(formattedAgain) != string(formatted) {
				t.Fatalf("formatter did not converge\nfirst:\n%s\nsecond:\n%s", formatted, formattedAgain)
			}

			for _, tc := range []struct {
				name   string
				source []byte
			}{
				{name: "original", source: source},
				{name: "formatted once", source: formatted},
				{name: "formatted twice", source: formattedAgain},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := renderStrict(t, tc.source); got != tt.want {
						t.Fatalf("strict render changed adjacency: got %q, want %q", got, tt.want)
					}
					legacy, err := transpile.Transpile(tc.source, transpile.Options{SourceFile: "adjacency.gsx"})
					if err != nil {
						t.Fatalf("Transpile: %v", err)
					}
					if got := runGeneratedLegacy(t, legacy); got != tt.want {
						t.Fatalf("generated legacy render changed adjacency: got %q, want %q", got, tt.want)
					}
				})
			}
		})
	}
}
