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
