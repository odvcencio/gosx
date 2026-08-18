package server

import "testing"

// TestNavigationLiveAttrConstants pins the data-gosx-live-* constants
// (gosx#217) against their identical literal values in
// client/runtime/host/navigation.ts and ir/validate.go, the same role
// TestNavigationCountdownAttrConstants and TestNavigationWatchAttrConstants
// play for their own attribute families: a rename or a typo in any one of
// the three places would otherwise only surface as a silently inert live
// region in a browser, not a test failure.
func TestNavigationLiveAttrConstants(t *testing.T) {
	if got, want := NavigationLiveSrcAttr, "data-gosx-live-src"; got != want {
		t.Fatalf("NavigationLiveSrcAttr = %q, want %q", got, want)
	}
	if got, want := NavigationLiveIntervalAttr, "data-gosx-live-interval"; got != want {
		t.Fatalf("NavigationLiveIntervalAttr = %q, want %q", got, want)
	}
	if got, want := NavigationLiveBindAttr, "data-gosx-live-bind"; got != want {
		t.Fatalf("NavigationLiveBindAttr = %q, want %q", got, want)
	}
	if got, want := NavigationLiveFlashClassAttr, "data-gosx-live-flash-class"; got != want {
		t.Fatalf("NavigationLiveFlashClassAttr = %q, want %q", got, want)
	}
}
