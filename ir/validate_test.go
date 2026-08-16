package ir_test

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/ir"
)

// TestValidateFlagsLengthMemberInLegacyCond covers gosx#164: a legacy
// component's <If cond={...}> reading .length on a slice used to render
// neither branch with no diagnostic anywhere. Validate must now fail closed.
func TestValidateFlagsLengthMemberInLegacyCond(t *testing.T) {
	source := []byte(`package main

func Page(data any) Node {
	return <div>
		<If cond={data.picks.length == 0}>
			<b>empty</b>
		</If>
	</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for .length in cond, got none")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, ".length") && strings.Contains(d.Message, "data.picks.length == 0") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a diagnostic naming the .length expression, got: %+v", diags)
	}
}

// TestValidateFlagsLengthMemberInTextExpr proves the rule covers any
// expression hole, not only <If cond={...}>: {data.picks.length} silently
// prints nothing today for the same reason a cond silently renders neither
// branch — there's no static type to tell a slice's .length from a resolvable
// member.
func TestValidateFlagsLengthMemberInTextExpr(t *testing.T) {
	source := []byte(`package main

func Page(data any) Node {
	return <span>{data.picks.length}</span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for .length in a text expression, got none")
	}
}

// TestValidateAllowsValidCondsInLegacyComponents proves the rule does not
// false-positive on ordinary legacy conditions, including the documented
// workaround of passing a precomputed boolean from a DataLoader.
func TestValidateAllowsValidCondsInLegacyComponents(t *testing.T) {
	source := []byte(`package main

func Page(data any, ok bool) Node {
	return <div>
		<If cond={data.picksEmpty}>
			<b>empty</b>
		</If>
		<If when={ok}>
			<b>ready</b>
		</If>
		<If cond={len(data.picks) == 0}>
			<b>also empty</b>
		</If>
	</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got: %+v", diags)
	}
}

// TestValidateSkipsLengthRuleForStrictComponents proves the rule stays scoped
// to legacy syntax: a strict component's props are Go-typed and checked by
// strictcheck (the real Go type checker), so Validate must not duplicate or
// second-guess that here.
func TestValidateSkipsLengthRuleForStrictComponents(t *testing.T) {
	source := []byte(`package main

type PageProps struct {
	Length int
}

component Page(props: PageProps) {
	return <div>{props.Length}</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for a strict component, got: %+v", diags)
	}
}

// TestValidateSkipsLengthRuleForTypedNonDataParam covers gosx#174 (PR #174
// review, M1): the reviewer's failing fixture. A legacy component can declare
// a typed parameter that is not named "data" and whose type genuinely has a
// "length" field — real, statically-checked Go that compiles fine. Before the
// fix, the rule flagged any ".length" selector anywhere in a legacy
// component's expression holes, so `r.length` here was rejected even though
// it never goes through the file router's reflective "data" binding (see
// route/fileeval.go: newFileRenderEnv binds ctx.Data to the literal
// identifier "data", never to a component's own parameter name).
func TestValidateSkipsLengthRuleForTypedNonDataParam(t *testing.T) {
	source := []byte(`package main

type ruler struct {
	length int
}

func Page(r *ruler) Node {
	return <div>{r.length}</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for a typed non-data parameter's .length field, got: %+v", diags)
	}
}

// TestValidateStillFlagsDataLengthAlongsideTypedParam proves the M1 fix does
// not overcorrect: in the same file, a genuine "data" selector's .length
// still fails, even though the typed r.length sibling above it does not.
func TestValidateStillFlagsDataLengthAlongsideTypedParam(t *testing.T) {
	source := []byte(`package main

type ruler struct {
	length int
}

func Page(data any, r *ruler) Node {
	return <div>
		<span>{r.length}</span>
		<span>{data.picks.length}</span>
	</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for data.picks.length, got none")
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "r.length") {
			t.Fatalf("did not expect r.length (typed, non-data) to be flagged, got: %+v", diags)
		}
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "data.picks.length") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a diagnostic naming data.picks.length, got: %+v", diags)
	}
}

// TestValidateSkipsLengthInsideHandlersAndComputeds asserts current behavior
// (unchanged by gosx#174): validateLegacyTemplateExprs only walks a
// component's rendered node tree (collectComponentNodeIDs from comp.Root). It
// does not walk ComponentScope.Handlers or ComponentScope.Computeds, which
// hold source text captured separately by analyzeBody for island lowering.
// A ".length" read inside a handler or computed body is therefore never
// scanned by this rule, whatever it is called on.
func TestValidateSkipsLengthInsideHandlersAndComputeds(t *testing.T) {
	source := []byte(`package main

func Page(data any) Node {
	isEmpty := func() bool {
		return data.length == 0
	}
	doubled := signal.Derive(func() int {
		return data.length * 2
	})
	_ = isEmpty
	_ = doubled
	return <div>{data.picks}</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}
	comp := prog.Components[0]
	if comp.Scope == nil {
		t.Fatal("expected a populated component scope")
	}
	if len(comp.Scope.Handlers) == 0 {
		t.Fatalf("expected the handler pattern to be recognized, got Handlers=%+v", comp.Scope.Handlers)
	}
	if len(comp.Scope.Computeds) == 0 {
		t.Fatalf("expected the computed pattern to be recognized, got Computeds=%+v", comp.Scope.Computeds)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics — .length only appears in Handlers/Computeds source text, which this rule does not scan, got: %+v", diags)
	}
}

// TestValidateRejectsInvalidCountdownInstant covers gosx#178: a static
// data-gosx-countdown value that is not a valid RFC3339 instant used to
// reach the browser runtime unchecked, where it renders a silently inert
// countdown. Validate must now fail closed at check time.
func TestValidateRejectsInvalidCountdownInstant(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="not-a-real-instant"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-countdown value "not-a-real-instant": not a valid RFC3339 instant`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidCountdownFormat covers gosx#178:
// data-gosx-countdown-format only supports "dhms" and "mm:ss". Any other
// static value must fail at check time with a clear message.
func TestValidateRejectsInvalidCountdownFormat(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-format="hh:mm"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-countdown-format value "hh:mm": must be "dhms" or "mm:ss"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateAllowsValidCountdownAttrs proves the rule does not
// false-positive on a well-formed countdown: a real RFC3339 instant paired
// with one of the two supported compact formats.
func TestValidateAllowsValidCountdownAttrs(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-format="mm:ss"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid countdown attributes, got %+v", diags)
	}
}

// TestValidateSkipsCountdownCheckForDynamicExpression proves the rule only
// applies to a static (AttrStatic) value. A dynamic expression
// (data-gosx-countdown={...}) is computed at render time, so its instant
// or format cannot be known here — it is exempt, and the browser runtime
// fails inert on a bad value it finds there instead.
func TestValidateSkipsCountdownCheckForDynamicExpression(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown={launchAt} data-gosx-countdown-format={format}></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for dynamic countdown expressions, got %+v", diags)
	}
}

// TestValidateRejectsCalendarInvalidCountdownInstant covers gosx#178 review
// finding M5: a static data-gosx-countdown value with an out-of-range day
// for its month (or a February 29 on a non-leap year) is not a valid
// RFC3339 instant — time.Parse already rejects it, and that rejection is
// exactly what the browser runtime's own parseCountdownInstant now matches
// (see the differential test coverage in client/js/runtime-14-navigation.test.js).
func TestValidateRejectsCalendarInvalidCountdownInstant(t *testing.T) {
	for _, bad := range []string{
		"2026-02-30T00:00:00Z",
		"2026-04-31T00:00:00Z",
		"2026-02-29T00:00:00Z", // 2026 is not a leap year
	} {
		source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="` + bad + `"></span>
}
`)
		prog, err := parse(t, source)
		if err != nil {
			t.Fatalf("Lower failed for %q: %v", bad, err)
		}

		diags := ir.Validate(prog)
		if len(diags) != 1 {
			t.Fatalf("expected exactly one diagnostic for %q, got %+v", bad, diags)
		}
		if !strings.Contains(diags[0].Message, "RFC3339") {
			t.Fatalf("expected an RFC3339 diagnostic for %q, got %q", bad, diags[0].Message)
		}
	}
}

// TestValidateRejectsInvalidCountdownSegment covers gosx#178 review finding
// m14: a static data-gosx-countdown-segment value outside the four names
// the runtime fills renders no diagnostic today and is simply ignored at
// run time (see findCountdownSegments in navigation.ts) — Validate now
// catches the mistake at check time.
func TestValidateRejectsInvalidCountdownSegment(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-countdown="2026-08-22T16:00:00-04:00">
		<b data-gosx-countdown-segment="weeks"></b>
	</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-countdown-segment value "weeks": must be "days", "hours", "minutes", or "seconds"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidCountdownWarn covers gosx#178 review finding
// m14: a static data-gosx-countdown-warn value outside the small
// declarative duration subset (a bare integer, or whole h/m/s components)
// disables the warn threshold silently at run time — Validate now catches
// it at check time.
func TestValidateRejectsInvalidCountdownWarn(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-warn="soon"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-countdown-warn value "soon": must be a bare integer number of seconds, or whole h/m/s components such as "30s" or "1m30s"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidCountdownThen covers gosx#178 review finding
// m14: data-gosx-countdown-then only supports "revalidate". Any other
// static value is silently ignored at run time — Validate now catches it
// at check time.
func TestValidateRejectsInvalidCountdownThen(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-then="reload"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %+v", diags)
	}
	want := `invalid data-gosx-countdown-then value "reload": must be "revalidate"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateAllowsValidCountdownSegmentWarnThen proves the m14 rules do
// not false-positive on well-formed values across all three attributes.
func TestValidateAllowsValidCountdownSegmentWarnThen(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-warn="1m30s" data-gosx-countdown-then="revalidate">
		<b data-gosx-countdown-segment="days"></b>
		<b data-gosx-countdown-segment="hours"></b>
		<b data-gosx-countdown-segment="minutes"></b>
		<b data-gosx-countdown-segment="seconds"></b>
	</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid countdown segment/warn/then attributes, got %+v", diags)
	}
}

// TestValidateRoutesCountdownChecksThroughComponentRef covers gosx#178
// review finding m14: a component reference (an uppercase tag, including a
// builtin like <Form>) can carry the same data-gosx-countdown-* attributes
// an element can. validateComponentRef must route its static attributes
// through the same checks validateElement already applies, so a bad value
// is caught here too, not only on a plain HTML element.
func TestValidateRoutesCountdownChecksThroughComponentRef(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <Form data-gosx-countdown="not-a-real-instant"></Form>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 1 {
		t.Fatalf("expected exactly one diagnostic for a component reference, got %+v", diags)
	}
	if !strings.Contains(diags[0].Message, "data-gosx-countdown") || !strings.Contains(diags[0].Message, "RFC3339") {
		t.Fatalf("expected the RFC3339 diagnostic routed through the component reference, got %q", diags[0].Message)
	}
}
