package ir_test

import (
	"fmt"
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
// m14, updated for gosx#213's threshold:class pairs grammar: a static
// data-gosx-countdown-warn value with no ":" pair at all disables every
// warn threshold silently at run time — Validate now catches it at check
// time.
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
	want := `invalid data-gosx-countdown-warn value "soon": must be a comma-separated list of threshold:class pairs`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidCountdownWarnPair covers gosx#213: a
// threshold:class pair with a valid threshold but a bad piece elsewhere in
// the comma-separated list (here, a second pair with no class token at
// all) fails the WHOLE value, not just that one pair — matching
// parseCountdownTierPairs' fail-closed-as-a-whole behavior in
// navigation.ts exactly.
func TestValidateRejectsInvalidCountdownWarnPair(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-warn="30s:is-warn,10s:"></span>
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
	want := `invalid data-gosx-countdown-warn value "30s:is-warn,10s:": must be a comma-separated list of threshold:class pairs`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidCountdownCue covers gosx#213's
// data-gosx-countdown-cue: a cue name outside the fixed "beep"/"chime"
// vocabulary disables every cue threshold silently at run time — Validate
// now catches it at check time.
func TestValidateRejectsInvalidCountdownCue(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-cue="10s:klaxon"></span>
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
	want := `invalid data-gosx-countdown-cue value "10s:klaxon": must be a comma-separated list of threshold:cue pairs using "beep" or "chime"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateAllowsValidCountdownWarnAndCue proves gosx#213's two pairs
// attributes do not false-positive on well-formed multi-tier values.
func TestValidateAllowsValidCountdownWarnAndCue(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-warn="30s:is-warn,10s:is-critical" data-gosx-countdown-cue="10s:beep"></span>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid countdown warn/cue attributes, got %+v", diags)
	}
}

// TestValidateRejectsInvalidWatchCondition covers gosx#214:
// data-gosx-watch with no "=" cannot be parsed into an attrName/valueRef
// pair, and disables the watcher silently at run time — Validate now
// catches it at check time.
func TestValidateRejectsInvalidWatchCondition(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-watch="no-equals-sign-here"></div>
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
	want := `invalid data-gosx-watch value "no-equals-sign-here": must be "<attrName>=<value>"`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidWatchEffect covers gosx#214's
// data-gosx-watch-effect: an unrecognized token shape fails the whole
// value at check time, even though the browser runtime only drops that
// one token at run time (see isValidWatchEffectValue's own doc comment for
// why check time is stricter here).
func TestValidateRejectsInvalidWatchEffect(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-watch="data-on-clock=true" data-gosx-watch-effect="class:is-active,flash-lights"></div>
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
	want := `invalid data-gosx-watch-effect value "class:is-active,flash-lights": must be a comma-separated list of "class:<name>", "title", or "cue:<name>" tokens`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateAllowsValidWatchConditionAndEffect proves gosx#214's two
// attributes do not false-positive on every well-formed shape: a literal
// condition, a selector-attribute reference condition, and every effect
// token kind.
func TestValidateAllowsValidWatchConditionAndEffect(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <main>
		<div data-gosx-watch="data-on-clock=true" data-gosx-watch-effect="class:is-active,class:is-glowing@#panel,title,cue:chime" data-gosx-watch-title="It's your pick!"></div>
		<div data-gosx-watch="data-seat=@#viewer[data-seat-id]"></div>
	</main>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid watch condition/effect attributes, got %+v", diags)
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
	return <div data-gosx-countdown="2026-08-22T16:00:00-04:00" data-gosx-countdown-warn="1m30s:is-warn" data-gosx-countdown-then="revalidate">
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

// TestValidateRejectsInvalidLiveInterval and
// TestValidateRejectsInvalidRegionInterval cover gosx#217's live-bound
// regions: data-gosx-live-interval and data-gosx-region-interval share
// data-gosx-revalidate-interval's own whole-seconds/whole-minutes subset,
// so a value outside it (here, whole hours) fails the same way.
func TestValidateRejectsInvalidLiveInterval(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-live-src="/api/live/week" data-gosx-live-interval="1h"></div>
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
	want := `invalid data-gosx-live-interval value "1h": must be a whole number of seconds or minutes`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

func TestValidateRejectsInvalidRegionInterval(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <div data-gosx-region-src="/api/wire/events" data-gosx-region-interval="not-a-duration"></div>
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
	want := `invalid data-gosx-region-interval value "not-a-duration": must be a whole number of seconds or minutes`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidRegionMode covers gosx#217's region growth
// modes: data-gosx-region-mode accepts only "replace", "append", or
// "prepend", matched byte for byte — a typo ("Append"), or embedded
// whitespace ("append ", " prepend") the run-time's own untrimmed ===
// comparison would also reject, must fail check-time, never silently
// fall back to a destructive "replace" swap at run time.
func TestValidateRejectsInvalidRegionMode(t *testing.T) {
	for _, value := range []string{"Append", "append ", " prepend"} {
		t.Run(value, func(t *testing.T) {
			source := []byte(fmt.Sprintf(`package main

func Page() Node {
	return <div data-gosx-region-src="/api/wire/events" data-gosx-region-mode="%s"></div>
}
`, value))
			prog, err := parse(t, source)
			if err != nil {
				t.Fatalf("Lower failed: %v", err)
			}

			diags := ir.Validate(prog)
			if len(diags) != 1 {
				t.Fatalf("expected exactly one diagnostic for value %q, got %+v", value, diags)
			}
			want := fmt.Sprintf(`invalid data-gosx-region-mode value %q: must be "replace", "append", or "prepend"`, value)
			if diags[0].Message != want {
				t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
			}
		})
	}
}

// TestValidateAllowsValidRegionModes proves each of the three recognized
// data-gosx-region-mode values, plus data-gosx-region-key and
// data-gosx-region-cursor alongside them, does not false-positive.
func TestValidateAllowsValidRegionModes(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <main>
		<ul data-gosx-region-src="/api/wire/events" data-gosx-region-mode="replace"></ul>
		<ul data-gosx-region-src="/api/wire/events" data-gosx-region-mode="append"></ul>
		<ul data-gosx-region-src="/api/wire/events" data-gosx-region-mode="prepend" data-gosx-region-key="data-tape-key" data-gosx-region-cursor="data-pick-number"></ul>
	</main>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid region modes, got %+v", diags)
	}
}

// TestValidateRejectsInvalidLiveBind covers a data-gosx-live-bind key with
// an embedded space, which parseLiveBindKey in navigation.ts also rejects
// at run time.
func TestValidateRejectsInvalidLiveBind(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-live-bind="score t42"></span>
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
	want := `invalid data-gosx-live-bind value "score t42": must be a "."-separated chain of non-empty keys`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsInvalidLiveFlashClass covers a data-gosx-live-flash-class
// value with embedded whitespace, the same rule
// isValidCountdownWarnClassToken already applies to a countdown warn class.
func TestValidateRejectsInvalidLiveFlashClass(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-live-bind="score:t42" data-gosx-live-flash-class="score flash"></span>
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
	want := `invalid data-gosx-live-flash-class value "score flash": must be one class name with no embedded whitespace`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateAllowsValidLiveAndRegionAttrs proves gosx#217's attributes do
// not false-positive on every well-formed shape: a live text binding with a
// flat key, a live text binding with a nested dot-separated key and a flash
// class, and a region fragment refresh pair.
func TestValidateAllowsValidLiveAndRegionAttrs(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <main>
		<div data-gosx-live-src="/api/live/week" data-gosx-live-interval="10s">
			<span data-gosx-live-bind="score:t42" data-gosx-live-flash-class="score-flash">0.0</span>
			<span data-gosx-live-bind="status.mode">SCHEDULED</span>
		</div>
		<div data-gosx-region-src="/api/wire/events" data-gosx-region-interval="20s"></div>
	</main>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid live/region attributes, got %+v", diags)
	}
}

// TestValidateAllowsValidLiveBindAttrAndClassAttrs proves gosx#217's
// attribute and class bind grammar does not false-positive on every
// well-formed shape: an href pair, a data-gosx-countdown retarget pair,
// and a multi-pair class bind.
func TestValidateAllowsValidLiveBindAttrAndClassAttrs(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <b data-gosx-countdown="2026-09-06T17:00:00Z" data-gosx-live-bind-attr="href:link,data-gosx-countdown:clock.deadline" data-gosx-live-bind-class="pick-clock--warn:clock.warn,pick-clock--paused:clock.paused">1:30</b>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	diags := ir.Validate(prog)
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for valid live-bind-attr/class attributes, got %+v", diags)
	}
}

// TestValidateRejectsInvalidLiveBindAttrTargetShape covers a
// data-gosx-live-bind-attr target whose shape could never work as an HTML
// attribute name at all (embedded whitespace), the same
// liveBindAttrTargetNamePattern guard liveBindAttrTargetAllowed applies at
// run time in client/runtime/host/navigation.ts.
func TestValidateRejectsInvalidLiveBindAttrTargetShape(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-live-bind-attr="data-x onclick:score"></span>
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
	want := `invalid data-gosx-live-bind-attr value "data-x onclick:score": must be a comma-separated list of target:key pairs`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateRejectsRefusedLiveBindAttrTargets covers the check-time half
// of liveBindAttrTargetAllowed's run-time POSITIVE allowlist
// (client/runtime/host/navigation.ts): a target this file can already
// refuse by name alone, before a browser ever sees a payload — an event
// handler, style, srcdoc, a runtime-owned data-gosx-* attribute, and the
// two CSRF-token attributes the runtime itself reads.
func TestValidateRejectsRefusedLiveBindAttrTargets(t *testing.T) {
	for _, target := range []string{"onclick", "style", "srcdoc", "data-gosx-live-src", "data-csrf-token", "data-csrf", "class", "id"} {
		t.Run(target, func(t *testing.T) {
			source := []byte("package main\n\nfunc Page() Node {\n\treturn <span data-gosx-live-bind-attr=\"" + target + ":score\"></span>\n}\n")
			prog, err := parse(t, source)
			if err != nil {
				t.Fatalf("Lower failed: %v", err)
			}

			diags := ir.Validate(prog)
			if len(diags) != 1 {
				t.Fatalf("expected exactly one diagnostic for target %q, got %+v", target, diags)
			}
		})
	}
}

// TestValidateRejectsInvalidLiveBindClassTarget covers a
// data-gosx-live-bind-class target with embedded whitespace, the same
// isValidCountdownWarnClassToken rule a countdown warn class already
// applies.
func TestValidateRejectsInvalidLiveBindClassTarget(t *testing.T) {
	source := []byte(`package main

func Page() Node {
	return <span data-gosx-live-bind-class="pick clock:clock.warn"></span>
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
	want := `invalid data-gosx-live-bind-class value "pick clock:clock.warn": must be a comma-separated list of class:key pairs`
	if diags[0].Message != want {
		t.Fatalf("unexpected diagnostic message: got %q, want %q", diags[0].Message, want)
	}
}

// TestValidateWarningsFlagsUntypedLegacyProps covers step one of retiring
// legacy component syntax: an untyped legacy component (`func Name(props
// any) Node`) gets one SeverityWarning diagnostic naming the component and
// the strict replacement. ir.Validate itself must stay silent — the
// warning must never become a compile-blocking error.
func TestValidateWarningsFlagsUntypedLegacyProps(t *testing.T) {
	source := []byte(`package main

func FeatureCard(props any) Node {
	return <div class="card">{props}</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	if diags := ir.Validate(prog); len(diags) != 0 {
		t.Fatalf("expected ir.Validate to stay silent on an untyped legacy component, got %+v", diags)
	}

	warnings := ir.ValidateWarnings(prog)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %+v", warnings)
	}
	warning := warnings[0]
	if warning.Severity != ir.SeverityWarning {
		t.Fatalf("expected SeverityWarning, got %v", warning.Severity)
	}
	if !strings.Contains(warning.Message, "FeatureCard") {
		t.Fatalf("expected the warning to name FeatureCard, got %q", warning.Message)
	}
	if !strings.Contains(warning.Message, "v1.0") {
		t.Fatalf("expected the warning to say the form is removed before v1.0, got %q", warning.Message)
	}
	if !strings.Contains(warning.Hint, "component FeatureCard(props: FeatureCardProps)") {
		t.Fatalf("expected the hint to name the strict replacement, got %q", warning.Hint)
	}
}

// TestValidateWarningsSilentOnZeroPropsAndTypedLegacy covers the boundary
// of the untyped-legacy warning: a zero-props legacy function, a TYPED
// legacy component (its props struct declared in the same file), and a
// strict component must all stay silent. Warning on zero-props legacy
// functions is out of scope for this step — see the zero-props census in
// gosx's untyped-legacy-warning change.
func TestValidateWarningsSilentOnZeroPropsAndTypedLegacy(t *testing.T) {
	source := []byte(`package main

type CardProps struct {
	Title string
}

func Page() Node {
	return <div>{"hi"}</div>
}

func Card(props CardProps) Node {
	return <div>{props.Title}</div>
}

component Strict(props: CardProps) {
	return <div>{props.Title}</div>
}
`)
	prog, err := parse(t, source)
	if err != nil {
		t.Fatalf("Lower failed: %v", err)
	}

	warnings := ir.ValidateWarnings(prog)
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for zero-props, typed legacy, or strict components, got %+v", warnings)
	}
}
