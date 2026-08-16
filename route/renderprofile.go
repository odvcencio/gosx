package route

import (
	"fmt"
	"strings"

	"m31labs.dev/gosx/ir"
)

// RenderProfile is an EXPERIMENTAL render-time hook for
// RenderProgramComponent. It exists so a downstream renderer that targets a
// stricter or different HTML dialect — for example an email renderer, which
// needs inline styles instead of classes and a stricter element allowlist —
// can rewrite attribute emission and refuse to render an unsafe program,
// without forking the file-program renderer (gosx#185).
//
// The surface is intentionally small: an attribute-writer hook and a
// pre-render validation pass. There is no plugin registry and no CLI
// surface. It may change or be removed in a future minor release; a caller
// that depends on it directly should pin an exact gosx version.
//
// A nil *RenderProfile reproduces today's rendering exactly, byte for byte
// — every profile-aware code path in the file-program renderer is gated on
// a non-nil field of this struct, so an empty *RenderProfile{} (both fields
// nil) also reproduces it exactly.
//
// Only RenderProgramComponent sets fileRenderOptions.Profile from
// ProgramRenderEnv.Profile: a file-routed page or layout, rendered through
// renderFileNode instead, has no way to install a profile (gosx#185 m6).
//
// # Coverage map (gosx#185 M2)
//
// AttrWriter reaches every ir.NodeElement (`<div>`, `<a>`, `<script>`, a
// hand-written `<form>`, ...) the renderer writes, wherever it sits: a
// plain top-level element, an element that is a strict or legacy nested
// component's own markup, an element inside an If/Show/When branch, and
// each per-iteration element inside an Each/For loop all get their own
// call.
//
// AttrWriter does NOT reach:
//
//   - A builtin component's own markup — Link's rendered `<a>`, Image's
//     rendered `<img>`, and every other builtin (Form, ActionForm, Motion,
//     Video, TextBlock, Stylesheet, Surface, Worker, Scene3D) writes its
//     element directly, bypassing this hook entirely.
//   - An unknown, engine, or bound component's markup, rendered through
//     defaultRenderedComponent. Name this one explicitly: it is different
//     in kind from the builtins above, because it emits the author's own
//     attributes verbatim, with no escaping or contract logic of its own
//     for AttrWriter to stand in for.
//   - An ir.NodeRawHTML node, or a runtime gosx.RawHTML value returned from
//     an expression: both are opaque strings the renderer copies through
//     unexamined. Validate cannot see inside either kind of raw HTML any
//     more than AttrWriter can — a profile that must inspect or reject raw
//     HTML has no hook for it today.
//   - An island subtree rendered through env.island (ProgramRenderEnv's
//     RenderIsland): island markup comes from a separate renderer, outside
//     this file-program renderer's write path.
//
// It also may see an author-written copy of a #179 managed-form
// runtime-contract attribute (data-gosx-form, its -state/-mode/-project
// variants, the client-runtime -form-error-describedby wiring attribute,
// the shared -enhance/-enhance-layer/-fallback progressive-enhancement
// attributes, and the data-gosx-managed shorthand) in AttrWriter's input,
// but the renderer discards any add, change, or removal the hook's
// returned copy makes to one of those names, reinserts the original value
// at its original position, and computes the contract's own presence check
// from that reconciled, effective list — a profile cannot weaken the
// managed-form contract by vetoing or rewriting it (gosx#185 B1).
//
// Text-node escaping and the void-element list (ir.VoidElements) are not
// configurable through a profile either. Both are cheap to reach from
// here, but a profile that could turn off text escaping would violate this
// type's own escape-after-the-hook guarantee, and the HTML5 void-element
// set is a fact about the format, not a per-consumer policy. A consumer
// that needs a different void-element set for a non-HTML target should
// treat that as a lowering-time concern, upstream of RenderProgramComponent.
type RenderProfile struct {
	// AttrWriter, when set, runs once per rendered ir.NodeElement, after
	// every attribute on it has been evaluated ({expr} attributes resolved,
	// {...spread} attributes expanded and flattened, and any managed-form
	// shorthand attribute removed — the hook never sees the shorthand
	// itself, or the runtime-contract attributes it expands into) and
	// before HTML escaping. It receives the element's tag name and its
	// resolved attributes, and returns the attributes to emit: change a
	// Value to rewrite, omit an entry to veto, or append a new RenderAttr
	// to add one.
	//
	// The renderer escapes every returned attribute's Name and Value
	// unconditionally after AttrWriter returns. RenderAttr's Value field is
	// a plain string with no "pre-escaped" or "raw" variant, so there is no
	// value AttrWriter can return that skips escaping. A returned Name that
	// is not a valid HTML attribute name — empty, whitespace-only, or
	// containing a character (space, a control character, `"`, `'`, `>`,
	// `/`, or `=`) that would end an attribute-name token early — fails the
	// whole render with a *RenderProfileError naming the offending tag and
	// Name instead of being escaped and emitted (gosx#185 M1): unlike an
	// ordinary attribute value, a Name is never sanitized for you, because
	// a profile is trusted code and a bad Name here is a bug worth
	// stopping the render for, not input to defend against.
	//
	// See the Coverage map above for exactly which elements this hook
	// reaches and does not, and the contract-attribute-name rule that
	// applies to what it can do to a #179 managed-form contract attribute
	// specifically.
	//
	// A panic inside AttrWriter is recovered and converted into a
	// *RenderProfileError naming the hook and the tag it was called for
	// (gosx#185 m5); it does not crash the calling process.
	AttrWriter AttrWriter

	// Validate, when set, runs once per render, before any output is
	// written, against the compiled *ir.Program. A non-empty return value
	// aborts the render: RenderProgramComponent returns a *RenderProfileError
	// wrapping the diagnostics and an empty HTML string. Rendering is fail
	// closed — a profile that finds a problem never lets partial or
	// unvalidated output reach the caller. Validate runs before
	// RenderProgramComponent even checks that the named component exists,
	// so a Validate refusal is reported ahead of a "component not found"
	// error for the same call (gosx#185 n2).
	//
	// Validate must not modify prog. The renderer may run concurrently
	// with the same *ir.Program across independent requests or goroutines,
	// so a mutation here would race every other render sharing it.
	//
	// Validate walks the WHOLE program, not only the component this render
	// call is about to render: a component this call never reaches still
	// gets checked, and can still fail the render on its own. This catches
	// a problem before it ships in some other, unrelated page, but it has
	// two costs to weigh — a large program with many components pays this
	// pass's full cost on every single render, and a genuine problem in a
	// component this particular render path never exercises can still
	// refuse an otherwise entirely fine render (a false positive from this
	// call's point of view, even though the diagnostic itself is real).
	//
	// A panic inside Validate is recovered and converted into a
	// *RenderProfileError naming the hook (gosx#185 m5); it does not crash
	// the calling process.
	Validate func(*ir.Program) []ir.Diagnostic
}

// AttrWriter rewrites, vetoes, or appends an element's attributes before
// they render. See RenderProfile.AttrWriter for exactly when it runs and
// what it can and cannot see.
type AttrWriter func(tag string, attrs []RenderAttr) []RenderAttr

// RenderAttr is one resolved HTML attribute: an attribute name plus either a
// text value or a boolean-presence marker, after expression evaluation and
// spread/shorthand expansion, before HTML escaping.
//
// RenderAttr is the only type an AttrWriter hook exchanges with the
// renderer. It carries no raw-HTML or pre-escaped-string variant, so an
// AttrWriter has no way to bypass the unconditional escaping the renderer
// applies to Name and Value after the hook returns.
type RenderAttr struct {
	// Name is the attribute name, for example "class" or "href".
	Name string

	// Value is the attribute's text value, for example a URL or a class
	// list. Value is ignored when Boolean is true.
	Value string

	// Boolean marks a valueless, presence-only attribute, for example the
	// `disabled` in `<button disabled>`. The renderer emits only Name for a
	// boolean attribute, matching how an AttrBool or a boolean-typed
	// expression attribute renders without a profile.
	//
	// Boolean, not Value, is what makes an attribute absent from a
	// rendered element: appending RenderAttr{Name: "disabled", Value:
	// "false"} with Boolean left false renders disabled="false", and HTML
	// treats ANY value on a boolean attribute — including the literal text
	// "false" — as present, so the button stays disabled (gosx#185 m3). A
	// profile that wants an element to render as not-disabled must omit
	// the attribute entirely, not set Value to a falsy-looking string.
	Boolean bool
}

// RenderProfileError reports that a RenderProfile's Validate pass refused to
// render a program. Diagnostics is never empty when this error is returned.
// RenderProgramComponent and the file-program renderer both return it before
// writing any output, so a refusal is total: the render never returns
// partial HTML alongside this error.
type RenderProfileError struct {
	Diagnostics []ir.Diagnostic
}

func (e *RenderProfileError) Error() string {
	if e == nil || len(e.Diagnostics) == 0 {
		return "render profile: validation refused the render"
	}
	if len(e.Diagnostics) == 1 {
		return fmt.Sprintf("render profile: validation refused the render: %s", e.Diagnostics[0].String())
	}
	messages := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		messages[i] = d.String()
	}
	return fmt.Sprintf("render profile: validation refused the render (%d diagnostics): %s", len(e.Diagnostics), strings.Join(messages, "; "))
}
