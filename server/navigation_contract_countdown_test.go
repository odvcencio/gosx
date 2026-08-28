package server

import "testing"

// TestNavigationCountdownAttrConstants pins the data-gosx-countdown-*
// constants declared in navigation_contract.go against their identical
// literal values in client/runtime/host/navigation.ts and ir/validate.go
// (gosx#178 review finding m11; extended for gosx#213's
// NavigationCountdownCueAttr). It mirrors
// TestNavigationRevalidateAttrConstants in navigation_contract_revalidate_test.go:
// a rename or a typo in any one of the three places would otherwise only
// surface as a silently inert countdown in a browser, not a test failure.
//
// NavigationCountdownWarnClass (the single fixed class the pre-gosx#213
// single-value form of NavigationCountdownWarnAttr always toggled) is gone:
// the pairs grammar has the author name every class explicitly, so there is
// no longer one fixed class name to pin.
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
	if got, want := NavigationCountdownCueAttr, "data-gosx-countdown-cue"; got != want {
		t.Fatalf("NavigationCountdownCueAttr = %q, want %q", got, want)
	}
	if got, want := NavigationCountdownThenAttr, "data-gosx-countdown-then"; got != want {
		t.Fatalf("NavigationCountdownThenAttr = %q, want %q", got, want)
	}
}

// TestNavigationWatchAttrConstants is TestNavigationCountdownAttrConstants'
// gosx#214 counterpart, pinning the three data-gosx-watch* constants
// against their identical literal values in
// client/runtime/host/navigation.ts.
func TestNavigationWatchAttrConstants(t *testing.T) {
	if got, want := NavigationWatchAttr, "data-gosx-watch"; got != want {
		t.Fatalf("NavigationWatchAttr = %q, want %q", got, want)
	}
	if got, want := NavigationWatchEffectAttr, "data-gosx-watch-effect"; got != want {
		t.Fatalf("NavigationWatchEffectAttr = %q, want %q", got, want)
	}
	if got, want := NavigationWatchTitleAttr, "data-gosx-watch-title"; got != want {
		t.Fatalf("NavigationWatchTitleAttr = %q, want %q", got, want)
	}
}
