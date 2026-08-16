package route

import (
	"errors"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// renderProfileGoldenSrc is the representative program the nil-profile
// golden test renders. It exercises a static attribute, an expression
// attribute that resolves to a boolean-presence attribute, nested elements,
// a void element, and text that needs HTML escaping — the same shapes a
// RenderProfile's AttrWriter and Validate hooks must see correctly when a
// profile is active.
const renderProfileGoldenSrc = `package main

func Page() Node {
	return <div id="card" class="a-b" disabled={true}>
		<p>Tom &amp; Jerry</p>
		<img src="https://example.com/x.png" alt="pic" />
	</div>
}
`

// TestRenderProfileNilIsByteIdenticalToNoProfile is the byte-identity
// golden required by gosx#185: a nil *RenderProfile, an explicit
// ProgramRenderEnv{Profile: nil}, and an empty *RenderProfile{} (both hooks
// unset) must all render the exact same bytes as calling
// RenderProgramComponent with no Profile field at all. Every profile-aware
// branch this change adds to the file-program renderer is gated on a
// non-nil AttrWriter or Validate field specifically, not merely a non-nil
// *RenderProfile, so this also proves an "opted in but inactive" profile
// changes nothing.
func TestRenderProfileNilIsByteIdenticalToNoProfile(t *testing.T) {
	prog, err := gosx.Compile([]byte(renderProfileGoldenSrc))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	const want = `<div id="card" class="a-b" disabled> <p>Tom &amp; Jerry</p> <img src="https://example.com/x.png" alt="pic" /> </div>`

	baseline, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{})
	if err != nil {
		t.Fatalf("render (no Profile field set): %v", err)
	}
	if baseline != want {
		t.Fatalf("baseline render does not match the pinned golden.\n got: %q\nwant: %q", baseline, want)
	}

	explicitNil, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: nil})
	if err != nil {
		t.Fatalf("render (explicit nil Profile): %v", err)
	}
	if explicitNil != baseline {
		t.Fatalf("explicit nil Profile diverged from the no-field baseline.\n got: %q\nwant: %q", explicitNil, baseline)
	}

	emptyProfile, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: &RenderProfile{}})
	if err != nil {
		t.Fatalf("render (empty *RenderProfile{}): %v", err)
	}
	if emptyProfile != baseline {
		t.Fatalf("empty *RenderProfile{} (both hooks nil) diverged from the no-field baseline.\n got: %q\nwant: %q", emptyProfile, baseline)
	}
}

// TestRenderProfileAttrWriterCalledPerElement proves AttrWriter runs exactly
// once for every rendered ir.NodeElement, in document order, with that
// element's tag name.
func TestRenderProfileAttrWriterCalledPerElement(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <section>
		<h1>Title</h1>
		<p>One <b>two</b> three</p>
	</section>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var tags []string
	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			tags = append(tags, tag)
			return attrs
		},
	}
	_, err = RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	want := []string{"section", "h1", "p", "b"}
	if len(tags) != len(want) {
		t.Fatalf("AttrWriter called %d times, want %d; tags=%v", len(tags), len(want), tags)
	}
	for i, tag := range want {
		if tags[i] != tag {
			t.Fatalf("AttrWriter call %d: tag = %q, want %q; full sequence=%v", i, tags[i], tag, tags)
		}
	}
}

// TestRenderProfileAttrWriterVetoAppendRewrite proves the three shapes an
// AttrWriter can return compose in a single call: dropping a resolved entry
// vetoes it, changing a Value rewrites it, and adding a new RenderAttr
// appends it.
func TestRenderProfileAttrWriterVetoAppendRewrite(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <a href="/plain" secret="drop-me">go</a>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			out := make([]RenderAttr, 0, len(attrs)+1)
			for _, a := range attrs {
				switch a.Name {
				case "secret":
					continue // veto
				case "href":
					out = append(out, RenderAttr{Name: "href", Value: "https://safe.example" + a.Value}) // rewrite
				default:
					out = append(out, a)
				}
			}
			out = append(out, RenderAttr{Name: "data-profile", Value: "on"}) // append
			return out
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if strings.Contains(html, "secret") {
		t.Fatalf("vetoed attribute survived: %q", html)
	}
	if !strings.Contains(html, `href="https://safe.example/plain"`) {
		t.Fatalf("rewritten href missing: %q", html)
	}
	if !strings.Contains(html, `data-profile="on"`) {
		t.Fatalf("appended attribute missing: %q", html)
	}
}

// TestRenderProfileValidateRefusesWithDiagnostics proves a Validate pass
// that reports diagnostics aborts the render entirely: RenderProgramComponent
// returns a *RenderProfileError carrying every diagnostic and an empty HTML
// string, never partial output.
func TestRenderProfileValidateRefusesWithDiagnostics(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <div>hello</div>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		Validate: func(p *ir.Program) []ir.Diagnostic {
			return []ir.Diagnostic{
				{Message: "first refusal reason"},
				{Message: "second refusal reason"},
			}
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err == nil {
		t.Fatalf("expected an error, got rendered html %q", html)
	}
	if html != "" {
		t.Fatalf("expected no partial output on refusal, got %q", html)
	}
	var profileErr *RenderProfileError
	if !errors.As(err, &profileErr) {
		t.Fatalf("error is not a *RenderProfileError: %v (%T)", err, err)
	}
	if len(profileErr.Diagnostics) != 2 {
		t.Fatalf("Diagnostics = %d entries, want 2: %v", len(profileErr.Diagnostics), profileErr.Diagnostics)
	}
	for _, want := range []string{"first refusal reason", "second refusal reason"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error message %q missing diagnostic %q", err.Error(), want)
		}
	}
}

// TestRenderProfileAttrWriterCannotBypassEscaping probes the escape-after-
// the-hook guarantee: an AttrWriter that tries to smuggle a quote and a
// <script> tag into a rewritten attribute value must see that value escaped
// in the output, exactly as an ordinary attribute value would be.
func TestRenderProfileAttrWriterCannotBypassEscaping(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <div title="benign">x</div>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	const injected = `x" onmouseover="evil()" data-y="<script>alert(1)</script>`
	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			for i := range attrs {
				if attrs[i].Name == "title" {
					attrs[i].Value = injected
				}
			}
			return attrs
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if strings.Contains(html, injected) {
		t.Fatalf("injected value survived unescaped: %q", html)
	}
	if strings.Contains(html, `onmouseover="evil()"`) {
		t.Fatalf("attribute boundary was broken out of: %q", html)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("raw <script> reached output: %q", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("expected the injected markup HTML-escaped in output: %q", html)
	}
	if !strings.Contains(html, "&#34;") {
		t.Fatalf("expected the injected quote HTML-escaped in output: %q", html)
	}
}

// TestRenderProfileManagedFormShorthandStillExpandsUnderProfile reuses the
// managed_form_shorthand_test.go fixture pattern (gosx#179) to prove an
// active render profile does not weaken the managed-form contract: the
// shorthand still expands to the full runtime-contract attribute set even
// though an AttrWriter is rewriting the form's ordinary attributes.
func TestRenderProfileManagedFormShorthandStillExpandsUnderProfile(t *testing.T) {
	html := compileManagedFormFixtureWithProfile(t,
		`<form method="post" action="/x/__actions/y" data-gosx-managed="true">`,
		&RenderProfile{
			AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
				// A benign, observable rewrite: append a marker attribute to
				// every element. If this ran on the managed-form contract
				// attributes too, the marker would appear more than once per
				// element and the contract assertions below would still pass
				// (this profile never touches them) — proving the contract
				// attributes are outside AttrWriter's reach, not merely
				// untouched by coincidence.
				return append(attrs, RenderAttr{Name: "data-profile-seen", Value: tag})
			},
		})

	for _, want := range []string{
		`method="post"`,
		`action="/x/__actions/y"`,
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.EnhancementLayerAttr + `="bootstrap"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
		`data-profile-seen="form"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in rendered managed-shorthand form html %q", want, html)
		}
	}
	if !hasManagedFormAttr(html) {
		t.Fatalf("expected the bare %s attribute in rendered managed-shorthand form html %q", gosx.ManagedFormAttr, html)
	}
	if strings.Contains(html, gosx.ManagedFormShorthandAttr) {
		t.Fatalf("expected shorthand attribute removed from output, got %q", html)
	}
}

func compileManagedFormFixtureWithProfile(t *testing.T, formOpenTag string, profile *RenderProfile) string {
	t.Helper()
	src := "package docs\n\n" +
		"func Page() Node {\n" +
		"\treturn <main>\n" +
		"\t\t" + formOpenTag + "\n" +
		"\t\t\t<input name=\"q\" value=\"docs\"></input>\n" +
		"\t\t</form>\n" +
		"\t</main>\n" +
		"}\n"
	prog, err := gosx.Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return html
}

// emailishClassStyles is the toy "email-ish" profile's class-to-inline-style
// table for TestRenderProfileEmailishDemo (b): it plays the role of
// gsxmail's own writer (gsx-email-spec.md §5 Architecture C, §14 U5), which
// today must reimplement HTML emission entirely because route offers no
// attribute hook to swap class for inline style.
var emailishClassStyles = map[string]string{
	"hit":  "background:#0f0",
	"miss": "background:#f00",
}

func emailishAttrWriter(tag string, attrs []RenderAttr) []RenderAttr {
	out := make([]RenderAttr, 0, len(attrs))
	for _, attr := range attrs {
		if !attr.Boolean && attr.Name == "class" {
			if style, ok := emailishClassStyles[attr.Value]; ok {
				out = append(out, RenderAttr{Name: "style", Value: style})
				continue
			}
		}
		out = append(out, attr)
	}
	return out
}

func emailishValidate(prog *ir.Program) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, node := range prog.Nodes {
		if node.Kind == ir.NodeElement && node.Tag == "script" {
			diags = append(diags, ir.Diagnostic{
				Span:    node.Span,
				Message: "element <script> is not allowed in an email template; mail clients do not run JavaScript",
			})
		}
	}
	return diags
}

// TestRenderProfileEmailishDemo is the gosx#185 acceptance demonstration: a
// minimal "email-ish" RenderProfile composes a validation pass that refuses
// a <script> element with an attribute hook that rewrites class attributes
// to inline style stubs, proving the two hooks together are enough to cover
// the gsxmail use case (gsx-email-spec.md §5 Architecture C, §14 U5) without
// gsxmail owning its own HTML writer.
func TestRenderProfileEmailishDemo(t *testing.T) {
	profile := &RenderProfile{
		AttrWriter: emailishAttrWriter,
		Validate:   emailishValidate,
	}

	t.Run("validation refuses a <script> element", func(t *testing.T) {
		prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <div><script>alert(1)</script></div>
}
`))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
		if err == nil {
			t.Fatalf("expected a validation error, got rendered html %q", html)
		}
		if html != "" {
			t.Fatalf("expected no partial output, got %q", html)
		}
		var profileErr *RenderProfileError
		if !errors.As(err, &profileErr) {
			t.Fatalf("error is not a *RenderProfileError: %v (%T)", err, err)
		}
		if len(profileErr.Diagnostics) != 1 {
			t.Fatalf("Diagnostics = %d entries, want 1: %v", len(profileErr.Diagnostics), profileErr.Diagnostics)
		}
		if !strings.Contains(profileErr.Diagnostics[0].Message, "<script>") {
			t.Fatalf("diagnostic message = %q, want it to name <script>", profileErr.Diagnostics[0].Message)
		}
	})

	t.Run("attribute hook rewrites class to inline style", func(t *testing.T) {
		prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <table><tr><td class="hit">H</td><td class="miss">M</td><td class="plain">P</td></tr></table>
}
`))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(html, `class="hit"`) || strings.Contains(html, `class="miss"`) {
			t.Fatalf("expected every recognized class attribute rewritten away, got %q", html)
		}
		if !strings.Contains(html, `style="background:#0f0"`) {
			t.Fatalf("expected hit's class rewritten to its style stub: %q", html)
		}
		if !strings.Contains(html, `style="background:#f00"`) {
			t.Fatalf("expected miss's class rewritten to its style stub: %q", html)
		}
		if !strings.Contains(html, `class="plain"`) {
			t.Fatalf("expected an unrecognized class value passed through unchanged: %q", html)
		}
	})
}
