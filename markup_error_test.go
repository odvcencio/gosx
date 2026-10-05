package gosx

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRejectsMalformedMarkup(t *testing.T) {
	for _, tc := range []struct {
		name, markup, message, hint string
		column                      int
	}{
		{"mismatched", "<h2>Next steps</h3>", "mismatched closing tag </h3>; expected </h2>", "replace </h3> with </h2>", 23},
		{"unclosed", "<h2>Next steps", "unclosed tag <h2>; expected </h2>", "add </h2>", 9},
		{"nested missing close", "<div><h2>Next steps</div>", "mismatched closing tag </div>; expected </h2>", "close <h2> before this tag", 28},
		{"outer missing close", "<div><h2>Next steps</h2>", "unclosed tag <div>", "add </div>", 9},
		{"fragment missing close", "<><h2>Next steps</h2>", "unclosed tag <>", "add </>", 9},
		{"fragment closes before child", "<><h2>Next steps</>", "unclosed tag <h2>; expected </h2>", "add </h2>", 11},
		{"raw mismatch", "<script>let n = 1;</style>", "mismatched closing tag </style>; expected </script>", "replace </style> with </script>", 27},
		{"raw unclosed", "<style>.card { color: red; }", "unclosed tag <style>", "add </style>", 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package app\n\nfunc Page() Node {\n\treturn " + tc.markup + "\n}\n")
			for _, parse := range []struct {
				name string
				fn   func() error
			}{
				{"parse", func() error { _, _, err := Parse(source); return err }},
				{"compile", func() error { _, err := Compile(source); return err }},
			} {
				t.Run(parse.name, func(t *testing.T) {
					err := parse.fn()
					var diagnostic *ParseError
					if !errors.As(err, &diagnostic) {
						t.Fatalf("error = %v, want ParseError", err)
					}
					if diagnostic.Line != 4 || diagnostic.Column != tc.column {
						t.Fatalf("position = %d:%d, want 4:%d", diagnostic.Line, diagnostic.Column, tc.column)
					}
					for _, want := range []string{tc.message, "hint: ", tc.hint, "\treturn " + tc.markup} {
						if !strings.Contains(err.Error(), want) {
							t.Fatalf("error %q missing %q", err, want)
						}
					}
				})
			}
		})
	}
}

func TestParseMarkupValidationPreservesGoAndRawText(t *testing.T) {
	for _, source := range []string{
		"package app\nfunc Page() Node {\n return <><div><img /></div><Demo.Card /></>\n}\n",
		"package app\nfunc Page() Node {\n return Fragment(\n <div><script>if (n < 2) { s = '<h2>'; }</SCRIPT></div>,\n )\n}\n",
		"package app\nfunc Page() Node {\n return Fragment(\n <div><style>.a > .b { color: red; }</style></div>,\n )\n}\n",
		"package app\nfunc Page() Node {\n return <script>{ClientScript()}</script>\n}\n",
		"package app\nvar s = `<h2>missing close`\n// <h3>also not markup\nfunc less(a, b int) bool { return a < b }\n",
		"package app\nfunc Page() Node {\n return <p>{`<h2>`}</p>\n}\n",
	} {
		tree, _, err := Parse([]byte(source))
		if err != nil || tree.RootNode().HasError() {
			t.Errorf("Parse(%q): %v", source, err)
		}
	}
}
