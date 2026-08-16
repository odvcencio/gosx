package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// TestPageStateAddHeadSetsNavigationScriptFlag covers PR #174 review M3:
// AddHead must record the navigation-marker flag itself, at add time, instead
// of leaving Head to rediscover it later by re-rendering every head node.
func TestPageStateAddHeadSetsNavigationScriptFlag(t *testing.T) {
	s := NewPageState()
	if s.hasNavigationScript {
		t.Fatal("expected hasNavigationScript to start false")
	}

	s.AddHead(gosx.El("meta", gosx.Attrs(gosx.Attr("name", "description"), gosx.Attr("content", "x"))))
	if s.hasNavigationScript {
		t.Fatal("expected an unrelated head node not to set hasNavigationScript")
	}

	s.AddHead(NavigationScript())
	if !s.hasNavigationScript {
		t.Fatal("expected AddHead(NavigationScript()) to set hasNavigationScript")
	}
}

// TestPageStateHeadSkipsAutomaticInjectionWhenManuallyAdded is the end-to-end
// counterpart: SetNavigationHead's builder must still defer to a manually
// added NavigationScript node, using the AddHead-time flag rather than a
// per-call render-and-scan.
func TestPageStateHeadSkipsAutomaticInjectionWhenManuallyAdded(t *testing.T) {
	s := NewPageState()
	s.SetNavigationHead(NavigationScriptWithNonce)
	s.AddHead(NavigationScript())

	html := gosx.RenderHTML(s.Head())
	if count := strings.Count(html, navigationScriptAttrMarker); count != 1 {
		t.Fatalf("expected exactly one navigation script marker, got %d:\n%s", count, html)
	}
}

// TestPageStateHeadCalledMultipleTimesStaysConsistent covers the doc comment
// on Head: the document shell may call Head() more than once per request (once
// per rendered layout level). Each call must see the same hasNavigationScript
// flag AddHead set once, not recompute it, and must not accumulate duplicate
// automatic injections across calls.
func TestPageStateHeadCalledMultipleTimesStaysConsistent(t *testing.T) {
	s := NewPageState()
	s.SetNavigationHead(NavigationScriptWithNonce)

	first := gosx.RenderHTML(s.Head())
	second := gosx.RenderHTML(s.Head())
	if strings.Count(first, navigationScriptAttrMarker) != 1 {
		t.Fatalf("expected the automatic script on the first Head() call, got:\n%s", first)
	}
	if strings.Count(second, navigationScriptAttrMarker) != 1 {
		t.Fatalf("expected the automatic script on the second Head() call, got:\n%s", second)
	}
}

// TestHeadContainsNavigationScriptMarkerRequiresOpeningTag covers PR #174
// review N1: the marker must match the literal opening tag
// `<script data-gosx-navigation="true"`, not a bare `data-gosx-navigation="`
// substring anywhere in the rendered node.
func TestHeadContainsNavigationScriptMarkerRequiresOpeningTag(t *testing.T) {
	if !headContainsNavigationScriptMarker(NavigationScript()) {
		t.Fatal("expected the real navigation script node to match the marker")
	}
	if !headContainsNavigationScriptMarker(NavigationScriptWithNonce("abc123")) {
		t.Fatal("expected the nonce variant to match the marker (nonce comes after the matched prefix)")
	}

	// A head script that only *queries* for the attribute — never opens a
	// <script data-gosx-navigation="true" ...> tag itself — must not match.
	queryOnly := gosx.RawHTML(`<script>if (document.querySelector('[data-gosx-navigation="true"]')) { /* noop */ }</script>`)
	if headContainsNavigationScriptMarker(queryOnly) {
		t.Fatal("expected a script that only queries the attribute not to match the marker")
	}
}

// TestPageStateHeadScriptQueryingAttributeDoesNotSuppressInjection is the
// false-positive regression PR #174 review explicitly calls out: a head
// script that queries [data-gosx-navigation="true"] (e.g. to detect whether
// the runtime is already present) must not be mistaken for the runtime script
// itself and must not suppress the automatic injection.
func TestPageStateHeadScriptQueryingAttributeDoesNotSuppressInjection(t *testing.T) {
	s := NewPageState()
	s.SetNavigationHead(NavigationScriptWithNonce)
	s.AddHead(gosx.RawHTML(`<script>if (document.querySelector('[data-gosx-navigation="true"]')) { /* noop */ }</script>`))

	if s.hasNavigationScript {
		t.Fatal("expected the query-only script not to set hasNavigationScript")
	}

	html := gosx.RenderHTML(s.Head())
	if count := strings.Count(html, navigationScriptAttrMarker); count != 1 {
		t.Fatalf("expected the automatic navigation script injection to still occur exactly once, got %d:\n%s", count, html)
	}
}
