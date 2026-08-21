package transpile

import (
	"strconv"
	"strings"
	"testing"
)

// wrap builds a minimal .gsx source whose Page() returns the given GSX body.
func wrap(body string) string {
	return "package demo\n\nimport . \"m31labs.dev/gosx\"\n\nfunc Page() Node {\n\treturn Fragment(\n\t\t" + body + ",\n\t)\n}\n"
}

func transpileBody(t *testing.T, body string) string {
	t.Helper()
	out, err := Transpile([]byte(wrap(body)), Options{SourceFile: "demo.gsx"})
	if err != nil {
		t.Fatalf("transpile %s: %v", body, err)
	}
	return out
}

// rawHTMLArg returns the string passed to the first gosx.RawHTML(...) call.
func rawHTMLArg(t *testing.T, out string) string {
	t.Helper()
	const marker = "gosx.RawHTML("
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("no gosx.RawHTML(...) in output:\n%s", out)
	}
	lit, err := strconv.QuotedPrefix(out[i+len(marker):])
	if err != nil {
		t.Fatalf("no quoted literal after %s: %v", marker, err)
	}
	got, err := strconv.Unquote(lit)
	if err != nil {
		t.Fatalf("unquote %s: %v", lit, err)
	}
	return got
}

// TestInlineScriptBodyIsOpaque pins the core contract: <script>/<style> bodies
// are script source, not markup. Before raw-text elements existed, `{` lexed as
// a GSX expression hole and `<` opened a bogus element, so these bodies were
// silently corrupted or dropped while Transpile still reported success.
func TestInlineScriptBodyIsOpaque(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		js   string
	}{
		{"object literal", `window.cfg = {a: 1, b: 2};`},
		{"less than", `if (a < b) { run(); }`},
		{"for loop", `for (var i = 0; i < n; i++) { go(i); }`},
		{"arrow and template", "const f = (x) => `v=${x}`;"},
		{"comparison chain", `if (a < b && c > d) { x({y: 1}); }`},
		{"html string inside js", `el.innerHTML = "<span>hi</span>";`},
		{"closing tag inside string", `var s = "</div>"; use(s);`},
		{"ampersands", `if (a && b) { go(); }`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := transpileBody(t, "<div><script>"+tc.js+"</script></div>")
			if got := rawHTMLArg(t, out); got != tc.js {
				t.Errorf("script body altered\n want: %q\n  got: %q", tc.js, got)
			}
		})
	}
}

func TestRawCloseMatchingIsCaseInsensitiveAndTagSpecific(t *testing.T) {
	t.Parallel()

	script := `const css = "</style>"; const near = "</scriptx>"; /* </style> */ // </style>
if (ok) { run(); }`
	out := transpileBody(t, `<div><script>`+script+`</ScRiPt 	><span>after</span></div>`)
	if got := rawHTMLArg(t, out); got != script {
		t.Fatalf("mixed-case script body or embedded style close changed\n want: %q\n  got: %q", script, got)
	}
	if !strings.Contains(out, "after") {
		t.Fatalf("matching script close did not leave following sibling available:\n%s", out)
	}

	style := `.a::before { content: "</script>"; } /* </script> */`
	out = transpileBody(t, "<div><style>"+style+"</StYlE \r\n></div>")
	if got := rawHTMLArg(t, out); got != style {
		t.Fatalf("mixed-case style body or embedded script close changed\n want: %q\n  got: %q", style, got)
	}
}

// TestScriptExpressionContainerStillInterpolates is the counterweight to
// raw-text handling, and it exists because raw-text elements broke it.
//
// `<script>{ClientScript()}</script>` injects the value of a Go call and is the
// established way to ship a server-built script body. The first raw-text
// implementation swallowed `{ClientScript()}` as literal JS, so pages shipped
// the identifier as source and the browser raised a ReferenceError. Nothing in
// the Go build failed; only a browser smoke test caught it.
func TestScriptExpressionContainerStillInterpolates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"bare call", `<div><script>{ClientScript()}</script></div>`},
		{"call with args", `<div><script>{BuildScript(cfg, 2)}</script></div>`},
		{"identifier", `<div><script>{scriptBody}</script></div>`},
		{"padded with spaces", `<div><script> {ClientScript()} </script></div>`},
		{"with attributes", `<div><script defer>{ClientScript()}</script></div>`},
		{"style element", `<div><style>{CriticalCSS()}</sTyLe></div>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := transpileBody(t, tc.body)
			if !strings.Contains(out, "gosx.Expr(") {
				t.Errorf("expression hole was not interpolated (likely swallowed as raw text):\n%s", out)
			}
			if strings.Contains(out, "gosx.RawHTML(\"{") {
				t.Errorf("expression hole emitted as literal script source:\n%s", out)
			}
		})
	}
}

// TestInlineStyleBodyIsOpaque covers the other raw-text element. CSS child
// combinators (`>`) and blocks (`{}`) hit the same lexer hazards as JS.
func TestInlineStyleBodyIsOpaque(t *testing.T) {
	t.Parallel()

	css := `.a > .b { color: red; } @media (max-width: 40rem) { .a { color: blue; } }`
	out := transpileBody(t, "<div><style>"+css+"</style></div>")
	if got := rawHTMLArg(t, out); got != css {
		t.Errorf("style body altered\n want: %q\n  got: %q", css, got)
	}
}

// TestInlineScriptNotEscaped guards the emit side. Script source must go out as
// raw HTML; escaping would turn `&&` into `&amp;&amp;` and break the script.
func TestInlineScriptNotEscaped(t *testing.T) {
	t.Parallel()

	out := transpileBody(t, `<div><script>if (a && b) { go(); }</script></div>`)
	if strings.Contains(out, "gosx.Text(") {
		t.Errorf("script body emitted as escaped Text, want RawHTML:\n%s", out)
	}
	if !strings.Contains(out, `gosx.El("script"`) {
		t.Errorf("expected a script element in output:\n%s", out)
	}
}

// TestRawTextElementKeepsAttributes checks attributes survive on the raw
// element, since the opening tag lexes `<script` as one token.
func TestRawTextElementKeepsAttributes(t *testing.T) {
	t.Parallel()

	out := transpileBody(t, `<div><script defer type="module">var a = 1;</script></div>`)
	for _, want := range []string{"defer", "module"} {
		if !strings.Contains(out, want) {
			t.Errorf("attribute %q missing from output:\n%s", want, out)
		}
	}
}

// TestEmptyRawTextElement covers <script src=...></script>, which carries no
// body at all.
func TestEmptyRawTextElement(t *testing.T) {
	t.Parallel()

	out := transpileBody(t, `<div><script src="/a.js"></script></div>`)
	if !strings.Contains(out, `gosx.El("script"`) {
		t.Errorf("expected script element:\n%s", out)
	}
}

// TestOrdinaryElementsStillParse is the regression half of the raw-text work.
// Every case here broke at some point while the raw-text rules were being
// developed: sharing a closing rule with jsx_element made the LR generator
// offer raw text after ordinary tags, and giving the two openings a common `<`
// prefix made the parser take the raw path for every nested element.
func TestOrdinaryElementsStillParse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{"text child", `<div>hello</div>`},
		{"nested element", `<div><span>x</span></div>`},
		{"s-prefixed tags", `<div><section><strong>x</strong></section></div>`},
		{"deep nesting", `<div><p><em><b>x</b></em></p></div>`},
		{"adjacent text and element", `<p>a<span>b</span>c</p>`},
		{"self closing", `<div><br /></div>`},
		{"fragment", `<><div>a</div><div>b</div></>`},
		{"attributes", `<div class="a" data-x="1"><span>y</span></div>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Transpile([]byte(wrap(tc.body)), Options{SourceFile: "demo.gsx"}); err != nil {
				t.Errorf("%s failed to transpile: %v", tc.body, err)
			}
		})
	}
}

// TestScriptSiblingsSurvive checks content after a raw-text element still
// parses — the scanner must stop at the closing tag rather than run on.
func TestScriptSiblingsSurvive(t *testing.T) {
	t.Parallel()

	out := transpileBody(t, `<div><script>var a = 1;</script><span>after</span></div>`)
	if !strings.Contains(out, "after") {
		t.Errorf("content after </script> was swallowed:\n%s", out)
	}
}

// TestUnterminatedScriptDoesNotSwallowFile pins the scanner guard. With no
// closing tag the scanner must decline rather than consume to end-of-input.
func TestUnterminatedScriptDoesNotSwallowFile(t *testing.T) {
	t.Parallel()

	// Deliberately malformed; the contract is only that it fails cleanly
	// rather than silently producing a truncated file.
	_, err := Transpile([]byte(wrap(`<div><script>var a = 1;</div>`)), Options{SourceFile: "demo.gsx"})
	if err == nil {
		t.Error("expected an error for an unterminated <script>, got none")
	}
}
