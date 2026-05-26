package signal

import (
	"sort"
	"testing"
)

// TestResolveAliasRedirectsLegacySceneEventNames is the ADR 0007 keystone:
// every legacy $scene.event.<field> name resolves to the corresponding
// $surface.event.<field> target. Bridge consumers that haven't migrated
// transparently read the canonical signal through this redirection.
func TestResolveAliasRedirectsLegacySceneEventNames(t *testing.T) {
	cases := []struct{ in, want string }{
		{"$scene.event.selectedID", "$surface.event.selectedID"},
		{"$scene.event.pointerX", "$surface.event.pointerX"},
		{"$scene.event.hovered", "$surface.event.hovered"},
		{"$scene.event.clickCount", "$surface.event.clickCount"},
	}
	for _, tc := range cases {
		if got := ResolveAlias(tc.in); got != tc.want {
			t.Errorf("ResolveAlias(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestResolveAliasPassesThroughUnknownNames ensures we never accidentally
// redirect non-alias names. A typo like $scene.evnt.X should round-trip
// unchanged so the caller sees the underlying lookup miss.
func TestResolveAliasPassesThroughUnknownNames(t *testing.T) {
	names := []string{
		"$surface.event.selectedID",
		"$scene.something.notAnEvent",
		"$other.namespace.foo",
		"plain.signal",
		"",
	}
	for _, name := range names {
		if got := ResolveAlias(name); got != name {
			t.Errorf("ResolveAlias(%q) = %q, want passthrough", name, got)
		}
	}
}

// TestIsAliasIdentifiesLegacyNames guards the alias-sunset grep gate: when
// IsAlias returns false for every legacy name in a tracked repo, the alias
// table can be deleted.
func TestIsAliasIdentifiesLegacyNames(t *testing.T) {
	for _, name := range LegacySceneEventNames() {
		if !IsAlias(name) {
			t.Errorf("IsAlias(%q) = false, want true", name)
		}
	}
	if IsAlias("$surface.event.selectedID") {
		t.Errorf("canonical name misclassified as alias")
	}
	if IsAlias("totally-not-an-event") {
		t.Errorf("unknown name misclassified as alias")
	}
}

// TestAliasesOfReturnsLegacyNamesForTarget proves the reverse map works:
// a $surface.event.X canonical name reports its $scene.event.X back-ref.
// Used by callers that need to push change notifications to both names.
func TestAliasesOfReturnsLegacyNamesForTarget(t *testing.T) {
	aliases := AliasesOf("$surface.event.selectedID")
	if len(aliases) != 1 || aliases[0] != "$scene.event.selectedID" {
		t.Errorf("AliasesOf($surface.event.selectedID) = %#v, want [$scene.event.selectedID]", aliases)
	}
	if got := AliasesOf("$surface.event.dropTargetID"); got != nil {
		t.Errorf("AliasesOf(canvas-only field) = %#v, want nil — no legacy callers exist", got)
	}
}

// TestLegacySceneEventNamesAreSubsetOfSurfaceEventNames protects the
// invariant that every legacy $scene.event.X has a $surface.event.X target.
// A drift here would silently break the alias-redirect path.
func TestLegacySceneEventNamesAreSubsetOfSurfaceEventNames(t *testing.T) {
	surface := make(map[string]bool)
	for _, name := range SurfaceEventNames() {
		surface[name] = true
	}
	for _, legacy := range LegacySceneEventNames() {
		target := ResolveAlias(legacy)
		if !surface[target] {
			t.Errorf("alias %q resolves to %q which is missing from SurfaceEventNames", legacy, target)
		}
	}
}

// TestSurfaceEventNamesCoverPickFields is a coverage smoke — the renderer's
// pushPickToSignals path writes ~22 fields per pick. SurfaceEventNames must
// at least match that count or we have a sync bug.
func TestSurfaceEventNamesCoverPickFields(t *testing.T) {
	names := SurfaceEventNames()
	if len(names) < 22 {
		t.Errorf("SurfaceEventNames count = %d, want >= 22 (renderer writes 22+ fields)", len(names))
	}
	// Stable ordering — useful for snapshot-style tests downstream.
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if len(sorted) == 0 || sorted[0] == "" {
		t.Errorf("SurfaceEventNames empty or has empty entries: %#v", sorted)
	}
}

// TestIsSurfaceEventNameClassifiesNamespace is a string-prefix sanity check
// that downstream tooling can rely on.
func TestIsSurfaceEventNameClassifiesNamespace(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"$surface.event.selectedID", true},
		{"$surface.event.X", true},
		{"$scene.event.X", false},
		{"$surface.other.foo", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsSurfaceEventName(tc.name); got != tc.want {
			t.Errorf("IsSurfaceEventName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
