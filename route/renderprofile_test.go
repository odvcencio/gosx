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

	const want = `<div id="card" class="a-b" disabled><p>Tom &amp; Jerry</p><img src="https://example.com/x.png" alt="pic" /></div>`

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

// TestRenderProfileAttrWriterCannotVetoManagedFormContractAttrs is the
// gosx#185 B1 negative test for a vetoing profile: an AttrWriter that drops
// every "data-gosx-" attribute it is handed must not be able to make the
// author's own contract attributes disappear from the output. Before B1's
// fix, writeManagedFormContract computed presence from the pre-hook
// node.Attrs, saw the author-written attributes there, concluded the
// contract was already satisfied, and never re-added what AttrWriter had
// just vetoed out of the actual output — orphaning the contract entirely.
func TestRenderProfileAttrWriterCannotVetoManagedFormContractAttrs(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <form method="post" action="/a" data-gosx-form data-gosx-form-state="idle"></form>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			out := attrs[:0:0]
			for _, a := range attrs {
				if strings.HasPrefix(a.Name, "data-gosx-") {
					continue // try to veto every managed-form contract attribute
				}
				out = append(out, a)
			}
			return out
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !hasManagedFormAttr(html) {
		t.Fatalf("AttrWriter veto orphaned the bare %s contract attribute: %q", gosx.ManagedFormAttr, html)
	}
	if !strings.Contains(html, gosx.ManagedFormStateAttr+`="idle"`) {
		t.Fatalf("AttrWriter veto orphaned %s, or lost the author's original value: %q", gosx.ManagedFormStateAttr, html)
	}
}

// TestRenderProfileAttrWriterCannotAppendConflictingManagedFormContractAttrs
// is the gosx#185 B1 negative test for an appending profile: an AttrWriter
// that appends its own conflicting copy of a contract attribute must not
// win. Before B1's fix, the profile's copy was emitted first (from
// AttrWriter's output) and the contract's own copy second (from
// writeManagedFormContract, unconditionally, right after) — two attributes
// with the same name, with an HTML parser keeping whichever one it sees
// first, so the profile's forged value silently won.
func TestRenderProfileAttrWriterCannotAppendConflictingManagedFormContractAttrs(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <form method="post" action="/x/__actions/y" data-gosx-managed="true"></form>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			if tag != "form" {
				return attrs
			}
			return append(attrs,
				RenderAttr{Name: gosx.ManagedFormStateAttr, Value: "PROFILE-WINS"},
				RenderAttr{Name: gosx.EnhancementAttr, Value: "PROFILE-WINS"},
				RenderAttr{Name: gosx.RuntimeFallbackAttr, Value: "PROFILE-WINS"},
			)
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if strings.Contains(html, "PROFILE-WINS") {
		t.Fatalf("AttrWriter's appended contract-attribute copy reached output: %q", html)
	}
	for _, want := range []string{
		gosx.ManagedFormStateAttr + `="idle"`,
		gosx.EnhancementAttr + `="form"`,
		gosx.RuntimeFallbackAttr + `="native-form"`,
	} {
		if count := strings.Count(html, want); count != 1 {
			t.Fatalf("want exactly one %q in output, got %d: %q", want, count, html)
		}
	}
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

// emailishAttrWriter rewrites a recognized class value to its inline-style
// stub, merging it into any style attribute already on the element instead
// of emitting a second style attribute alongside the author's own (gosx#185
// M4). AttrWriter itself does no by-name merging — a profile that returns
// two RenderAttr entries named "style" gets two style attributes in the
// output, the same as if two ir.Attrs shared a name — and this demo is the
// starting point people copy, so it must not carry that bug into every
// downstream profile that starts from it.
func emailishAttrWriter(tag string, attrs []RenderAttr) []RenderAttr {
	// Collect by role, not by source order: the author's own style always
	// merges ahead of the class-derived addition, regardless of whether
	// class or style comes first in the element's attribute list.
	authorStyle, hasAuthorStyle := "", false
	classStyle, hasClassStyle := "", false
	for _, attr := range attrs {
		if attr.Boolean {
			continue
		}
		switch attr.Name {
		case "style":
			authorStyle, hasAuthorStyle = attr.Value, true
		case "class":
			if style, ok := emailishClassStyles[attr.Value]; ok {
				classStyle, hasClassStyle = style, true
			}
		}
	}
	hasStyle := hasAuthorStyle || hasClassStyle
	merged := mergeEmailishStyle(authorStyle, classStyle)

	out := make([]RenderAttr, 0, len(attrs)+1)
	styleWritten := false
	for _, attr := range attrs {
		switch {
		case !attr.Boolean && attr.Name == "class":
			if _, ok := emailishClassStyles[attr.Value]; ok {
				continue // merged into style above; the class attribute itself is dropped
			}
		case !attr.Boolean && attr.Name == "style":
			if hasStyle && !styleWritten {
				out = append(out, RenderAttr{Name: "style", Value: merged})
				styleWritten = true
			}
			continue
		}
		out = append(out, attr)
	}
	if hasStyle && !styleWritten {
		out = append(out, RenderAttr{Name: "style", Value: merged})
	}
	return out
}

// mergeEmailishStyle appends a new inline-style declaration onto an
// existing one, separating declarations the way hand-authored inline CSS
// does. An empty side of the merge contributes nothing, so the first
// declaration on an element never picks up a stray leading separator.
func mergeEmailishStyle(existing, addition string) string {
	switch {
	case existing == "":
		return addition
	case addition == "":
		return existing
	default:
		return existing + "; " + addition
	}
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

	// gosx#185 M4: an element that already carries its own style attribute
	// alongside a recognized class must end up with exactly one merged
	// style attribute, not two separate ones. This is the regression the
	// original, unmerged emailishAttrWriter would have hit.
	t.Run("class rewrite merges with an existing style attribute", func(t *testing.T) {
		prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <table><tr><td class="hit" style="color:blue">H</td></tr></table>
}
`))
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if count := strings.Count(html, `style="`); count != 1 {
			t.Fatalf("want exactly one style attribute, got %d: %q", count, html)
		}
		if !strings.Contains(html, `style="color:blue; background:#0f0"`) {
			t.Fatalf("expected the author's style and the class rewrite merged into one attribute: %q", html)
		}
	})
}

// TestRenderProfileAttrWriterInvalidNameFailsClosed is the gosx#185 M1
// regression test: html.EscapeString does not escape a space or an "=", so
// an AttrWriter that returns a Name containing either could otherwise
// smuggle extra attributes past one Name field. The render must fail
// closed with a *RenderProfileError instead of emitting whatever an HTML
// parser would make of the unescaped syntax characters.
func TestRenderProfileAttrWriterInvalidNameFailsClosed(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <div title="benign">x</div>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			return []RenderAttr{{Name: `x onmouseover=alert(1) y`, Value: "z"}}
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err == nil {
		t.Fatalf("expected an error for an invalid attribute name, got rendered html %q", html)
	}
	if html != "" {
		t.Fatalf("expected no partial output on an invalid attribute name, got %q", html)
	}
	var profileErr *RenderProfileError
	if !errors.As(err, &profileErr) {
		t.Fatalf("error is not a *RenderProfileError: %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), "invalid attribute name") {
		t.Fatalf("error message %q does not name the invalid-attribute-name failure", err.Error())
	}
}

// TestRenderProfileAttrWriterPanicFailsClosed is the gosx#185 m5 regression
// test: a panicking AttrWriter must not crash the calling process. It has
// to become an ordinary *RenderProfileError instead, recoverable the same
// way a reported Validate diagnostic is.
func TestRenderProfileAttrWriterPanicFailsClosed(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <div>x</div>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		AttrWriter: func(tag string, attrs []RenderAttr) []RenderAttr {
			panic("boom")
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err == nil {
		t.Fatalf("expected an error from the panicking AttrWriter, got rendered html %q", html)
	}
	if html != "" {
		t.Fatalf("expected no partial output after an AttrWriter panic, got %q", html)
	}
	var profileErr *RenderProfileError
	if !errors.As(err, &profileErr) {
		t.Fatalf("error is not a *RenderProfileError: %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), "panicked") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error message %q does not name the panic or its value", err.Error())
	}
}

// TestRenderProfileValidatePanicFailsClosed is the gosx#185 m5 regression
// test for Validate: a panic there must become a *RenderProfileError too,
// not crash the caller.
func TestRenderProfileValidatePanicFailsClosed(t *testing.T) {
	prog, err := gosx.Compile([]byte(`package main

func Page() Node {
	return <div>x</div>
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	profile := &RenderProfile{
		Validate: func(p *ir.Program) []ir.Diagnostic {
			panic("kaboom")
		},
	}
	html, err := RenderProgramComponent(prog, "Page", ProgramRenderEnv{Profile: profile})
	if err == nil {
		t.Fatalf("expected an error from the panicking Validate, got rendered html %q", html)
	}
	if html != "" {
		t.Fatalf("expected no partial output after a Validate panic, got %q", html)
	}
	var profileErr *RenderProfileError
	if !errors.As(err, &profileErr) {
		t.Fatalf("error is not a *RenderProfileError: %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), "panicked") || !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("error message %q does not name the panic or its value", err.Error())
	}
}
