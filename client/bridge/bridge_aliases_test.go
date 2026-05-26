package bridge

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/signal"
)

// TestStoreSetSurfaceEventReadableViaSceneAlias is the ADR 0007 C1.1
// acceptance: writes to $surface.event.<X> are readable via $scene.event.<X>
// because the Store routes both names through ResolveAlias to the same
// underlying signal. Phase 1 consumers that still subscribe to
// $scene.event.* keep working — that's the whole point of the alias.
func TestStoreSetSurfaceEventReadableViaSceneAlias(t *testing.T) {
	store := NewStore()
	store.Set("$surface.event.selectedID", vm.StringVal("rect-42"))

	got, ok := store.Get("$scene.event.selectedID")
	if !ok {
		t.Fatalf("legacy $scene.event.selectedID lookup failed after $surface.event.selectedID write")
	}
	if got.Str != "rect-42" {
		t.Errorf("alias read = %q, want %q", got.Str, "rect-42")
	}
}

// TestStoreSetSceneAliasReachesSurfaceTarget proves the reverse direction:
// a hand-rolled caller (e.g. an old test) writing $scene.event.* still has
// its value visible at $surface.event.* — neither namespace is "lossy" with
// respect to the other.
func TestStoreSetSceneAliasReachesSurfaceTarget(t *testing.T) {
	store := NewStore()
	store.Set("$scene.event.selectedID", vm.StringVal("legacy-id"))

	got, ok := store.Get("$surface.event.selectedID")
	if !ok {
		t.Fatalf("$surface.event lookup failed after $scene.event write")
	}
	if got.Str != "legacy-id" {
		t.Errorf("canonical read = %q, want %q", got.Str, "legacy-id")
	}
}

// TestStoreSignalAliasNotifiesLegacySubscriber covers C1.4: existing
// SceneAdapter pick consumers that subscribed via $scene.event.* still
// receive change notifications when the renderer writes to
// $surface.event.*. Subscribers attach to the SAME underlying signal
// because Signal() resolves the alias.
func TestStoreSignalAliasNotifiesLegacySubscriber(t *testing.T) {
	store := NewStore()

	// Legacy consumer subscribes via $scene.event.* name.
	legacySig := store.Signal("$scene.event.selectedID", vm.StringVal(""))
	fired := 0
	unsub := legacySig.Subscribe(func() { fired++ })
	defer unsub()

	// Renderer writes via $surface.event.* (the post-ADR-0007 canonical name).
	store.Set("$surface.event.selectedID", vm.StringVal("hit"))

	if fired == 0 {
		t.Fatalf("legacy subscriber did not fire when $surface.event.selectedID was written")
	}
	if v := legacySig.Get().Str; v != "hit" {
		t.Errorf("legacy signal value after canonical write = %q, want %q", v, "hit")
	}
}

// TestStoreAliasMapsToSameSignalInstance is a sharper version of the above:
// store.Signal($scene.event.X) and store.Signal($surface.event.X) return
// the IDENTICAL *signal.Signal[vm.Value] pointer. If this ever becomes
// false, two subscriber sets would drift apart.
func TestStoreAliasMapsToSameSignalInstance(t *testing.T) {
	store := NewStore()
	canonical := store.Signal("$surface.event.selectedID", vm.StringVal(""))
	legacy := store.Signal("$scene.event.selectedID", vm.StringVal(""))
	if canonical != legacy {
		t.Fatalf("alias did not converge: canonical=%p legacy=%p", canonical, legacy)
	}
}

// TestStoreNonAliasNamesAreUnaffected is the negative guard against an
// over-eager rewrite swallowing non-event names. Any custom signal under a
// different namespace must round-trip unchanged.
func TestStoreNonAliasNamesAreUnaffected(t *testing.T) {
	store := NewStore()
	store.Set("$custom.counter", vm.IntVal(42))
	got, ok := store.Get("$custom.counter")
	if !ok || got.Num != 42 {
		t.Errorf("custom signal lost through alias path: ok=%v got=%v", ok, got)
	}
}

// TestStoreAliasTableMatchesADR0007 asserts that the package signal alias
// table is in sync with the bridge's store — calling ResolveAlias for every
// legacy name and storing under both forms must converge.
func TestStoreAliasTableMatchesADR0007(t *testing.T) {
	store := NewStore()
	for _, legacy := range signal.LegacySceneEventNames() {
		canonical := signal.ResolveAlias(legacy)
		store.Set(canonical, vm.StringVal(legacy))
		got, ok := store.Get(legacy)
		if !ok {
			t.Errorf("legacy lookup %q failed after canonical %q write", legacy, canonical)
			continue
		}
		if got.Str != legacy {
			t.Errorf("legacy lookup %q = %q, want %q", legacy, got.Str, legacy)
		}
	}
}
