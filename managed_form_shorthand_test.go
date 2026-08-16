package gosx

import (
	"fmt"
	"strings"
	"testing"
)

// wantManagedFormDefaults builds the exact five-attribute contract markup
// ManagedFormAttrs(ManagedFormOptions{}) produces, from the runtime-contract
// constants rather than string literals, so the test tracks the contract
// even if an attribute name ever changes.
func wantManagedFormDefaults() string {
	return fmt.Sprintf(
		`<form %s %s="idle" %s="form" %s="bootstrap" %s="native-form"></form>`,
		ManagedFormAttr, ManagedFormStateAttr, EnhancementAttr, EnhancementLayerAttr, RuntimeFallbackAttr,
	)
}

func TestManagedFormShorthandExpandsToContractDefaults(t *testing.T) {
	node := El("form", Attrs(BoolAttr(ManagedFormShorthandAttr)))
	if got, want := RenderHTML(node), wantManagedFormDefaults(); got != want {
		t.Fatalf("RenderHTML bare shorthand = %q, want %q", got, want)
	}
}

func TestManagedFormShorthandAcceptsExplicitTrueValue(t *testing.T) {
	node := El("form", Attrs(Attr(ManagedFormShorthandAttr, "true")))
	if got, want := RenderHTML(node), wantManagedFormDefaults(); got != want {
		t.Fatalf(`RenderHTML data-gosx-managed="true" = %q, want %q`, got, want)
	}
}

func TestManagedFormShorthandFalseValueOptsOut(t *testing.T) {
	node := El("form", Attrs(Attr(ManagedFormShorthandAttr, "false")))
	want := fmt.Sprintf(`<form %s="false"></form>`, ManagedFormShorthandAttr)
	if got := RenderHTML(node); got != want {
		t.Fatalf(`RenderHTML data-gosx-managed="false" = %q, want %q (expected no expansion)`, got, want)
	}
}

func TestManagedFormShorthandOmittedLeavesFormUnmanaged(t *testing.T) {
	node := El("form", Attrs(Attr("action", "/save")))
	want := `<form action="/save"></form>`
	if got := RenderHTML(node); got != want {
		t.Fatalf("RenderHTML unmanaged form = %q, want %q", got, want)
	}
}

// TestManagedFormShorthandKeepsAuthorOverride covers the merge rule: an
// author-specified contract attribute survives with its authored value and
// is not duplicated by the shorthand's default for the same name.
func TestManagedFormShorthandKeepsAuthorOverride(t *testing.T) {
	node := El("form", Attrs(
		BoolAttr(ManagedFormShorthandAttr),
		Attr(ManagedFormStateAttr, "pending"),
	))
	want := fmt.Sprintf(
		`<form %s %s="form" %s="bootstrap" %s="native-form" %s="pending"></form>`,
		ManagedFormAttr, EnhancementAttr, EnhancementLayerAttr, RuntimeFallbackAttr, ManagedFormStateAttr,
	)
	got := RenderHTML(node)
	if got != want {
		t.Fatalf("RenderHTML shorthand with override = %q, want %q", got, want)
	}
	if n := strings.Count(got, ManagedFormStateAttr); n != 1 {
		t.Fatalf("expected %s to appear once, appeared %d times in %q", ManagedFormStateAttr, n, got)
	}
}

// TestManagedFormShorthandNonFormElementLeftUntouched documents the
// decision to scope expansion to <form> elements only: the managed-form
// contract (submit interception, native fallback) is meaningless on any
// other element, so the shorthand attribute passes through unexpanded
// rather than silently attaching form semantics to, say, a <div>.
func TestManagedFormShorthandNonFormElementLeftUntouched(t *testing.T) {
	node := El("div", Attrs(BoolAttr(ManagedFormShorthandAttr)))
	want := fmt.Sprintf(`<div %s></div>`, ManagedFormShorthandAttr)
	if got := RenderHTML(node); got != want {
		t.Fatalf("RenderHTML shorthand on non-form element = %q, want %q", got, want)
	}
}

// TestManagedFormShorthandEmptyStringValueIsTruthy covers gosx#179 F3: a
// trimmed empty string (data-gosx-managed="") expands the same way as the
// bare attribute. A bare HTML boolean attribute and its `=""` spelling are
// indistinguishable once parsed, so the two must not diverge.
func TestManagedFormShorthandEmptyStringValueIsTruthy(t *testing.T) {
	node := El("form", Attrs(Attr(ManagedFormShorthandAttr, "")))
	if got, want := RenderHTML(node), wantManagedFormDefaults(); got != want {
		t.Fatalf(`RenderHTML data-gosx-managed="" = %q, want %q`, got, want)
	}
}

// TestManagedFormShorthandTruthyTreatsNilAsAbsent covers gosx#179 F3's nil
// alignment: ManagedFormShorthandTruthy(false, nil) must return false, the
// same "not present" judgment fileManagedFormShorthandTruthy
// (route/fileprogram.go) always made for a nil attrValue result, so the two
// callers agree without either one special-casing nil itself.
func TestManagedFormShorthandTruthyTreatsNilAsAbsent(t *testing.T) {
	if got := ManagedFormShorthandTruthy(false, nil); got {
		t.Fatalf("ManagedFormShorthandTruthy(false, nil) = %v, want false", got)
	}
}

func TestManagedFormShorthandCaseInsensitiveFormTag(t *testing.T) {
	node := El("FORM", Attrs(BoolAttr(ManagedFormShorthandAttr)))
	got := RenderHTML(node)
	for _, want := range []string{ManagedFormAttr, ManagedFormStateAttr, EnhancementAttr, EnhancementLayerAttr, RuntimeFallbackAttr} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderHTML uppercase FORM tag missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, ManagedFormShorthandAttr) {
		t.Fatalf("RenderHTML uppercase FORM tag left shorthand attribute in place: %s", got)
	}
}
