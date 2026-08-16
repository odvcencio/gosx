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
// Scope notes:
//
//   - AttrWriter runs only for a raw HTML element (an ir.NodeElement — a
//     `<div>`, `<a>`, `<script>`, ...). It does not run for a builtin
//     component's own markup (`<Link>`, `<Form>`, `<Image>`, ...) or for a
//     strict/legacy component's own elements rendered through a nested
//     component call's subtree — those elements each get their own
//     AttrWriter call when the renderer reaches them as ir.NodeElement
//     nodes. It also does not see the managed-form runtime-contract
//     attributes (state/enhancement/fallback) that gosx#179 added: those
//     render unconditionally, after AttrWriter runs, so a profile cannot
//     weaken the managed-form contract by vetoing or rewriting it.
//   - Text-node escaping and the void-element list (ir.VoidElements) are
//     not configurable through a profile. Both are cheap to reach from
//     here, but a profile that could turn off text escaping would violate
//     this type's own escape-after-the-hook guarantee, and the HTML5 void-
//     element set is a fact about the format, not a per-consumer policy.
//     A consumer that needs a different void-element set for a non-HTML
//     target should treat that as a lowering-time concern, upstream of
//     RenderProgramComponent.
type RenderProfile struct {
	// AttrWriter, when set, runs once per rendered ir.NodeElement, after
	// every attribute on it has been evaluated ({expr} attributes resolved,
	// {...spread} attributes expanded and flattened, and any managed-form
	// shorthand expanded) and before HTML escaping. It receives the
	// element's tag name and its resolved attributes, and returns the
	// attributes to emit: change a Value to rewrite, omit an entry to veto,
	// or append a new RenderAttr to add one.
	//
	// The renderer escapes every returned attribute's Name and Value
	// unconditionally after AttrWriter returns. RenderAttr's Value field is
	// a plain string with no "pre-escaped" or "raw" variant, so there is no
	// value AttrWriter can return that skips escaping.
	AttrWriter AttrWriter

	// Validate, when set, runs once per render, before any output is
	// written, against the compiled *ir.Program. A non-empty return value
	// aborts the render: RenderProgramComponent returns a *RenderProfileError
	// wrapping the diagnostics and an empty HTML string. Rendering is fail
	// closed — a profile that finds a problem never lets partial or
	// unvalidated output reach the caller.
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
	if len(e.Diagnostics) == 0 {
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
