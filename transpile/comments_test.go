package transpile

import (
	"strconv"
	"strings"
	"testing"
)

// TestWholeLineCommentsCompileAway covers the transpiler-level half of
// "comments inside GSX markup compile away": a `//` line inside element
// children — the developer's own comment — must not reach the emitted Go
// as a gosx.Text call, and dropping it must leave no blank-line artifact
// behind. The rule the whole table pins is "line's first non-whitespace
// characters are //", not "line contains //" — a mid-line `//` (a label
// divider) or a `//` inside a URL both keep the whole line as text.
func TestWholeLineCommentsCompileAway(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		// wantAbsent, if set, must not appear anywhere in the transpiled
		// output — the surest way to catch a comment surviving into a
		// gosx.Text call.
		wantAbsent string
		// wantText, if set, must appear as the exact argument to a
		// gosx.Text(...) call somewhere in the output.
		wantText string
	}{
		{
			name:       "comment on its own line before an element sibling is dropped",
			body:       "<div>\n\t// explain why this block exists\n\t<h2>Title</h2>\n</div>",
			wantAbsent: "explain why this block exists",
		},
		{
			// A bare "//" line with nothing after it is the one edge case
			// the CHANGELOG calls out by name: it is indistinguishable from
			// a real comment line, so it is dropped too. A real divider
			// with nothing else on its line must move into an expression,
			// {"// detail"}, or share a line with neighboring text.
			name:       "bare // with nothing after it is dropped like any other comment line",
			body:       "<div>\n\t//\n\t<h2>Title</h2>\n</div>",
			wantAbsent: "//",
		},
		{
			name:       "comment between sibling elements is dropped",
			body:       "<div><h2>Title</h2>\n\t// mid-document comment\n\t<p>Body</p></div>",
			wantAbsent: "mid-document comment",
		},
		{
			name:       "comment before the first child is dropped",
			body:       "<div>\n\t// leading comment\n\t<span>x</span></div>",
			wantAbsent: "leading comment",
		},
		{
			name:     "// after other text on the same line stays text",
			body:     `<div>LABEL // detail</div>`,
			wantText: "LABEL // detail",
		},
		{
			name:     "a URL containing // stays text",
			body:     `<div>https://example.com/foo</div>`,
			wantText: "https://example.com/foo",
		},
		{
			name:     "a URL on its own line stays text (// is not first on the line)",
			body:     "<div>\n\thttps://example.com/foo\n</div>",
			wantText: "\n\thttps://example.com/foo\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := transpileBody(t, tc.body)
			if tc.wantAbsent != "" && strings.Contains(out, tc.wantAbsent) {
				t.Errorf("comment survived into output:\n%s", out)
			}
			if tc.wantText != "" {
				want := "gosx.Text(" + strconv.Quote(tc.wantText) + ")"
				if !strings.Contains(out, want) {
					t.Errorf("expected %s in output:\n%s", want, out)
				}
			}
		})
	}
}

// TestWholeLineCommentDropLeavesNoTextNode goes further than "the comment
// string is absent": a child whose only content was a whole-line comment
// must emit no gosx.Text call at all, matching the existing rule for a
// purely whitespace child (a blank line between tags already produces
// nothing). A stray empty or whitespace-only gosx.Text call would be the
// "stray whitespace artifact" the product owner's report described.
func TestWholeLineCommentDropLeavesNoTextNode(t *testing.T) {
	t.Parallel()

	out := transpileBody(t, "<div>\n\t// only a comment here\n</div>")
	if strings.Contains(out, "gosx.Text(") {
		t.Fatalf("expected no gosx.Text call once the only content is a comment:\n%s", out)
	}
	if !strings.Contains(out, `gosx.El("div")`) {
		t.Fatalf("expected a childless div, got:\n%s", out)
	}
}

// TestCommentPreservedInAttributeString guards against over-eager
// stripping: an attribute value is Go/JSX attribute syntax, not GSX element
// child text, so emitGSXText's line-comment rule must never run over it.
func TestCommentPreservedInAttributeString(t *testing.T) {
	t.Parallel()

	out := transpileBody(t, `<div data-note="// not a comment, an attribute value">x</div>`)
	if !strings.Contains(out, `// not a comment, an attribute value`) {
		t.Fatalf("attribute string content was altered:\n%s", out)
	}
}

// TestCommentPreservedInRawTextElements pins the raw-text side of "not
// inside raw-text elements": <script>/<style> bodies never reach
// emitGSXText at all (see emitRawTextElement/rawTextBody), so a `//` line
// there is always JS/CSS source, never a GSX comment.
func TestCommentPreservedInRawTextElements(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"script", "<div><script>\n\t// a real JS comment, not GSX markup\n\tvar a = 1;\n</script></div>"},
		{"style", "<div><style>\n\t// not valid CSS, but still opaque source\n\t.a { color: red; }\n</style></div>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := transpileBody(t, tc.body)
			if got := rawHTMLArg(t, out); !strings.Contains(got, "// a real JS comment") && !strings.Contains(got, "// not valid CSS") {
				t.Fatalf("raw-text body altered:\n%s", got)
			}
		})
	}
}

// TestCommentPreservedInPreAndTextarea covers the two ordinary jsx_element
// tags (unlike <script>/<style>, they are not raw-text elements at the
// grammar level; see isVerbatimTextTag) that must still keep a `//` line
// verbatim, because their text content displays exactly as written — a
// code sample inside <pre>, a default value inside <textarea>.
func TestCommentPreservedInPreAndTextarea(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tag  string
	}{
		{"pre", "pre"},
		{"textarea", "textarea"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := "<" + tc.tag + ">\n\t// keep me literally\n</" + tc.tag + ">"
			out := transpileBody(t, body)
			if !strings.Contains(out, "keep me literally") {
				t.Fatalf("%s content was altered:\n%s", tc.tag, out)
			}
		})
	}

	// A <pre> nested inside another verbatim element must still be treated
	// as verbatim after the inner one closes (verbatimTextDepth is a
	// counter, not a bool) — proven by keeping a comment line that follows
	// a closed nested <pre>.
	t.Run("depth restores after a nested pre closes", func(t *testing.T) {
		t.Parallel()
		out := transpileBody(t, "<pre><pre>inner</pre>\n\t// still inside the outer pre\n</pre>")
		if !strings.Contains(out, "still inside the outer pre") {
			t.Fatalf("comment inside nested <pre> was stripped:\n%s", out)
		}
	})
}

// TestCommentOnlyExpressionContainersCompileAway covers {/* ... */} and
// {// ...}: before grammar.go's jsx_expression_container made its
// expression field Optional, a brace pair holding only a Go comment failed
// to parse at all ("missing identifier"). Both must now parse and emit
// nothing — no gosx.Expr call, no stray text.
func TestCommentOnlyExpressionContainersCompileAway(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"block comment", `<div>{/* just a comment */}</div>`},
		{"line comment", "<div>{// just a comment\n}</div>"},
		{"multi-line block comment", "<div>{/* line one\nline two\nline three */}</div>"},
		{"comment with surrounding whitespace", "<div>{  /* padded */  }</div>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := transpileBody(t, tc.body)
			if strings.Contains(out, "gosx.Expr(") {
				t.Fatalf("comment-only expression container should emit nothing, got:\n%s", out)
			}
			if !strings.Contains(out, `gosx.El("div")`) {
				t.Fatalf("expected a childless div, got:\n%s", out)
			}
		})
	}
}

// TestMixedCommentAndExpressionStillInterpolates is the counterweight to
// TestCommentOnlyExpressionContainersCompileAway: a container that mixes a
// comment with a real expression must keep working exactly as before —
// only a container whose brace body is comments and whitespace and
// nothing else compiles away.
func TestMixedCommentAndExpressionStillInterpolates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"leading block comment", `<div>{/* why */ data}</div>`},
		{"trailing block comment", `<div>{data /* why */}</div>`},
		{"leading line comment", "<div>{\n\t// why\n\tdata\n}</div>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := transpileBody(t, tc.body)
			if !strings.Contains(out, "gosx.Expr(data)") {
				t.Fatalf("expected the real expression to still interpolate, got:\n%s", out)
			}
		})
	}
}
