package format

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSourcePreservesExpressionComments(t *testing.T) {
	for _, hole := range []string{
		`{/* Explain why this region stays server rendered. */}`,
		`{/* leading */ "visible" /* trailing */}`,
		"{// Keep the closing brace on its own line.\n}",
		"{\"visible\" // trailing explanation\n}",
	} {
		t.Run(hole, func(t *testing.T) {
			source := []byte("package app\ncomponent Page() {\n\treturn <main>" + hole + "<span>Ready</span></main>\n}\n")
			formatted, err := Source(source)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.ReplaceAll(string(formatted), "\t", ""), hole) {
				t.Fatalf("formatter discarded or changed expression trivia:\n%s", formatted)
			}
			tree, _, err := gosx.Parse(formatted)
			if err != nil || tree.RootNode().HasError() {
				t.Fatalf("formatted comment no longer parses: %v\n%s", err, formatted)
			}
			again, err := Source(formatted)
			if err != nil || !bytes.Equal(formatted, again) {
				t.Fatalf("comment formatting is not idempotent: %v\nfirst:\n%s\nsecond:\n%s", err, formatted, again)
			}
		})
	}
}

func TestSourceRejectsMalformedSyntax(t *testing.T) {
	source := []byte("package app\ncomponent Page() { return <div title={ >broken</div> }\n")
	formatted, err := Source(source)
	if err == nil || formatted != nil {
		t.Fatalf("expected a diagnostic and no replacement source; got %q, %v", formatted, err)
	}
}
