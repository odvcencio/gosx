package gosx

import "testing"

func TestBooleanAttributesKeepPresenceAndDataValueSemantics(t *testing.T) {
	node := El("input", Attrs(
		BoolAttr("disabled"),
		Attr("checked", true),
		Attr("selected", false),
		BoolAttr("data-presence"),
		Attr("data-ready", true),
		Attr("data-stale", false),
	))

	const want = `<input disabled checked data-presence data-ready="true" data-stale="false" />`
	if got := RenderHTML(node); got != want {
		t.Fatalf("RenderHTML boolean attributes = %q, want %q", got, want)
	}
}
