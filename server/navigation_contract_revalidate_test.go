package server

import "testing"

func TestNavigationRevalidateAttrConstants(t *testing.T) {
	if got, want := NavigationRevalidateIntervalAttr, "data-gosx-revalidate-interval"; got != want {
		t.Fatalf("NavigationRevalidateIntervalAttr = %q, want %q", got, want)
	}
	if got, want := NavigationRevalidateSrcAttr, "data-gosx-revalidate-src"; got != want {
		t.Fatalf("NavigationRevalidateSrcAttr = %q, want %q", got, want)
	}
}
