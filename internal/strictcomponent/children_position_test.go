package strictcomponent

import (
	"strings"
	"testing"
)

// TestChildExpressionPositionAdmitsChildren proves B1: the bare children
// identifier is renderable in a child expression hole. The renderer already
// binds the name (writeLocalComponent, route/fileprogram.go); this removes
// the validator rule that had nothing behind it.
func TestChildExpressionPositionAdmitsChildren(t *testing.T) {
	for _, source := range []string{"children", "(children)", "((children))"} {
		if err := ValidateServerChildExpressionScope(source, Scope{}); err != nil {
			t.Fatalf("ValidateServerChildExpressionScope(%q) = %v, want nil", source, err)
		}
	}
}

// TestAttributePositionRejectsChildren proves B5, the dangerous half of the
// relaxation. An attribute value is written inside quotes, so a rendered node
// there would splice markup into an HTML attribute. The attribute entry point
// keeps refusing the identifier, and says why.
func TestAttributePositionRejectsChildren(t *testing.T) {
	const want = "children renders as element content, not as an attribute value"
	for _, source := range []string{"children", "(children)"} {
		err := ValidateServerExpressionScope(source, Scope{})
		if err == nil {
			t.Fatalf("ValidateServerExpressionScope(%q) = nil, want a refusal", source)
		}
		if err.Error() != want {
			t.Fatalf("ValidateServerExpressionScope(%q) = %q, want %q", source, err, want)
		}
	}
}

// TestValidateServerExpressionKeepsAttributeDefault proves the fail-closed
// default: the pre-children entry points are the attribute position, so any
// caller that states no position keeps refusing children.
func TestValidateServerExpressionKeepsAttributeDefault(t *testing.T) {
	if err := ValidateServerExpression("children"); err == nil {
		t.Fatal("ValidateServerExpression(children) = nil, want a refusal")
	}
}

// TestChildPositionRejectsEveryOperationOnChildren proves the containment
// that keeps children from becoming an escape hatch. The admission is one
// identifier, in one position, with one operation: emission. Reading a field
// of it, concatenating it, and branching on it all stay refused, in the child
// position too, by rules this change does not touch.
func TestChildPositionRejectsEveryOperationOnChildren(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"field", "children.Something", "selector must be a field chain rooted at props"},
		{"concat", `"prefix " + children`, "is not renderable; strict concatenation accepts string literals and props field selectors only"},
		{"index", "children[0]", "index expressions are not supported"},
		{"call", "children()", "calls are not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateServerChildExpressionScope(test.source, Scope{})
			if err == nil {
				t.Fatalf("ValidateServerChildExpressionScope(%q) = nil, want a refusal", test.source)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to contain %q", err, test.want)
			}
		})
	}
}

// TestCondPositionRejectsChildren proves W9's half of the containment: an
// <If cond> needs a rooted selector, so children never reaches it. The
// message is the cond validator's own, unchanged.
func TestCondPositionRejectsChildren(t *testing.T) {
	_, _, _, err := ValidateServerCondExpressionScope("children", Scope{})
	if err == nil {
		t.Fatal("ValidateServerCondExpressionScope(children) = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "strict cond must be a bool props field") {
		t.Fatalf("error = %q, want the cond selector refusal", err)
	}
}

// TestIsChildrenExpression pins the one spelling test the lowerer's
// AcceptsChildren pass and this validator share, so the two can never
// disagree about what children looks like.
func TestIsChildrenExpression(t *testing.T) {
	yes := []string{"children", "(children)", " children "}
	no := []string{"props", "childrenList", "children.Field", "children()", `"children"`, "", "child ren"}
	for _, source := range yes {
		if !IsChildrenExpression(source) {
			t.Fatalf("IsChildrenExpression(%q) = false, want true", source)
		}
	}
	for _, source := range no {
		if IsChildrenExpression(source) {
			t.Fatalf("IsChildrenExpression(%q) = true, want false", source)
		}
	}
}

// TestChildPositionKeepsEveryOtherRule proves the child entry point differs
// from the attribute one in exactly one identifier and nothing else.
func TestChildPositionKeepsEveryOtherRule(t *testing.T) {
	shared := []string{"props", "nil", "someLocal", "1 + 2", "-props.Count", `'a'`}
	for _, source := range shared {
		childErr := ValidateServerChildExpressionScope(source, Scope{})
		attrErr := ValidateServerExpressionScope(source, Scope{})
		if childErr == nil || attrErr == nil {
			t.Fatalf("%q: child=%v attr=%v, want both refused", source, childErr, attrErr)
		}
		if childErr.Error() != attrErr.Error() {
			t.Fatalf("%q: child=%q attr=%q, want identical messages", source, childErr, attrErr)
		}
	}
	accepted := []string{`"text"`, "42", "true", "props.Label", `"a" + props.Label`}
	for _, source := range accepted {
		if err := ValidateServerChildExpressionScope(source, Scope{}); err != nil {
			t.Fatalf("ValidateServerChildExpressionScope(%q) = %v, want nil", source, err)
		}
		if err := ValidateServerExpressionScope(source, Scope{}); err != nil {
			t.Fatalf("ValidateServerExpressionScope(%q) = %v, want nil", source, err)
		}
	}
}
