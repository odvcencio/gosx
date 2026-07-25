package format

import (
	"strings"
	"testing"
)

// TestSourceKeepsRawTextElements pins a regression CI caught: when raw-text
// elements landed in the grammar, the formatter did not know the node kind and
// dropped every <script> and <style> from the file it was asked to format.
func TestSourceKeepsRawTextElements(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

func Page() Node {
	return <div>
		<script src="/app.js" defer></script>
		<p>copy</p>
	</div>
}
`)

	formatted, err := Source(src)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	out := string(formatted)
	if !strings.Contains(out, `<script src="/app.js" defer></script>`) {
		t.Fatalf("formatter dropped the <script> element, got:\n%s", out)
	}
	if !strings.Contains(out, "<p>copy</p>") {
		t.Fatalf("formatter dropped sibling content, got:\n%s", out)
	}
}

// TestSourceKeepsSingleLineScriptBodyVerbatim checks a single-line script body
// survives formatting character for character, including the operators the old
// parser corrupted.
func TestSourceKeepsSingleLineScriptBodyVerbatim(t *testing.T) {
	t.Parallel()

	body := `if (a < b) { go({x: 1}); } else { stop(a && b); }`
	src := []byte("package main\n\nfunc Page() Node {\n\treturn <div>\n\t\t<script>" + body + "</script>\n\t</div>\n}\n")

	formatted, err := Source(src)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if !strings.Contains(string(formatted), body) {
		t.Fatalf("script body altered\n want to contain: %q\n got:\n%s", body, formatted)
	}
}

// TestSourceMultiLineScriptBodyIsIndented records a KNOWN LIMITATION rather
// than a desired behavior.
//
// The formatter indents the interior lines of a multi-line raw-text body. For
// most JS and CSS that is harmless, but inside a multi-line template literal
// the added leading whitespace becomes part of the string the literal
// produces. Nothing is lost or reordered, so `gosx fmt --check` stays stable
// and idempotent; the body is simply re-indented.
//
// Until the formatter can reproduce raw-text spans without touching interior
// lines, put multi-line template literals in a .js asset rather than an inline
// <script>. This test exists so the limitation is visible and so a future fix
// has an obvious place to flip the assertion.
func TestSourceMultiLineScriptBodyIsIndented(t *testing.T) {
	t.Parallel()

	body := "var msg = `line one\nline two`;"
	src := []byte("package main\n\nfunc Page() Node {\n\treturn <div>\n\t\t<script>" + body + "</script>\n\t</div>\n}\n")

	formatted, err := Source(src)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	out := string(formatted)

	// Both lines must still be present: re-indentation is tolerated, loss is not.
	for _, want := range []string{"var msg = `line one", "line two`;"} {
		if !strings.Contains(out, want) {
			t.Fatalf("multi-line script body lost %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, body) {
		t.Log("multi-line raw-text bodies now round-trip verbatim; " +
			"tighten this test into an exact-match assertion")
	}
}

// TestSourceIsIdempotentWithRawText guards the fmt-check gate: formatting an
// already-formatted file must be a no-op, or `gosx fmt --check` fails in CI.
func TestSourceIsIdempotentWithRawText(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

func Page() Node {
	return <div>
		<style>.a > .b { color: red; }</style>
		<script>if (a < b) { go(); }</script>
	</div>
}
`)

	once, err := Source(src)
	if err != nil {
		t.Fatalf("Source (first pass): %v", err)
	}
	twice, err := Source(once)
	if err != nil {
		t.Fatalf("Source (second pass): %v", err)
	}
	if string(once) != string(twice) {
		t.Fatalf("formatting is not idempotent over raw-text elements\n first:\n%s\n second:\n%s", once, twice)
	}
}
