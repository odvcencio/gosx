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
