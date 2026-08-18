package server

import "testing"

// TestNavigationFilterAttrConstants pins the data-gosx-filter* constants
// declared in navigation_contract.go against their identical literal values
// in client/runtime/host/navigation.ts (gosx#215). It mirrors
// TestNavigationWatchAttrConstants in navigation_contract_countdown_test.go:
// a rename or a typo in any one of the two places would otherwise only
// surface as a silently inert filter in a browser, not a test failure.
func TestNavigationFilterAttrConstants(t *testing.T) {
	if got, want := NavigationFilterAttr, "data-gosx-filter"; got != want {
		t.Fatalf("NavigationFilterAttr = %q, want %q", got, want)
	}
	if got, want := NavigationFilterTextAttr, "data-gosx-filter-text"; got != want {
		t.Fatalf("NavigationFilterTextAttr = %q, want %q", got, want)
	}
	if got, want := NavigationFilterAnnounceAttr, "data-gosx-filter-announce"; got != want {
		t.Fatalf("NavigationFilterAnnounceAttr = %q, want %q", got, want)
	}
	if got, want := NavigationFilterHiddenClass, "gosx-filter-row--hidden"; got != want {
		t.Fatalf("NavigationFilterHiddenClass = %q, want %q", got, want)
	}
}

// TestNavigationHeartbeatAttrConstants is TestNavigationFilterAttrConstants'
// gosx#216 counterpart, pinning the two data-gosx-heartbeat* constants
// against their identical literal values in
// client/runtime/host/navigation.ts.
func TestNavigationHeartbeatAttrConstants(t *testing.T) {
	if got, want := NavigationHeartbeatAttr, "data-gosx-heartbeat"; got != want {
		t.Fatalf("NavigationHeartbeatAttr = %q, want %q", got, want)
	}
	if got, want := NavigationHeartbeatIntervalAttr, "data-gosx-heartbeat-interval"; got != want {
		t.Fatalf("NavigationHeartbeatIntervalAttr = %q, want %q", got, want)
	}
}
