package server

import "testing"

// TestNavigationCountdownAttrConstants pins the six data-gosx-countdown-*
// constants declared in navigation_contract.go against their identical
// literal values in client/runtime/host/navigation.ts and ir/validate.go
// (gosx#178 review finding m11). It mirrors
// TestNavigationRevalidateAttrConstants in navigation_contract_revalidate_test.go:
// a rename or a typo in any one of the three places would otherwise only
// surface as a silently inert countdown in a browser, not a test failure.
func TestNavigationCountdownAttrConstants(t *testing.T) {
	if got, want := NavigationCountdownAttr, "data-gosx-countdown"; got != want {
		t.Fatalf("NavigationCountdownAttr = %q, want %q", got, want)
	}
	if got, want := NavigationCountdownFormatAttr, "data-gosx-countdown-format"; got != want {
		t.Fatalf("NavigationCountdownFormatAttr = %q, want %q", got, want)
	}
	if got, want := NavigationCountdownSegmentAttr, "data-gosx-countdown-segment"; got != want {
		t.Fatalf("NavigationCountdownSegmentAttr = %q, want %q", got, want)
	}
	if got, want := NavigationCountdownWarnAttr, "data-gosx-countdown-warn"; got != want {
		t.Fatalf("NavigationCountdownWarnAttr = %q, want %q", got, want)
	}
	if got, want := NavigationCountdownThenAttr, "data-gosx-countdown-then"; got != want {
		t.Fatalf("NavigationCountdownThenAttr = %q, want %q", got, want)
	}
	if got, want := NavigationCountdownWarnClass, "gosx-countdown--warn"; got != want {
		t.Fatalf("NavigationCountdownWarnClass = %q, want %q", got, want)
	}
}
