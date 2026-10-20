package server

import (
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

// The framework-owned navigation builder is the only source of the navigation
// runtime. AddHead content never suppresses it, so an app cannot accidentally
// install a second runtime with a stale request nonce.
func TestPageStateNavigationOwnerAlwaysInjectsOncePerHeadRender(t *testing.T) {
	s := NewPageState()
	s.SetNavigationHead(navigationScriptWithNonce)
	s.AddHead(gosx.RawHTML(`<script>if (document.querySelector('[data-gosx-navigation="true"]')) { /* noop */ }</script>`))

	first := gosx.RenderHTML(s.Head())
	second := gosx.RenderHTML(s.Head())
	marker := `<script data-gosx-navigation="true"`
	if got := strings.Count(first, marker); got != 1 {
		t.Fatalf("expected one framework navigation script on first Head call, got %d:\n%s", got, first)
	}
	if got := strings.Count(second, marker); got != 1 {
		t.Fatalf("expected one framework navigation script on second Head call, got %d:\n%s", got, second)
	}
}
