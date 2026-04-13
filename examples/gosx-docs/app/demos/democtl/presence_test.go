package democtl

import (
	"math/rand"
	"testing"
)

// TestPickIsDeterministicWithSeed verifies that two rand instances seeded
// identically produce the same 10-pick sequence.
func TestPickIsDeterministicWithSeed(t *testing.T) {
	const seed = 42
	rng1 := rand.New(rand.NewSource(seed))
	rng2 := rand.New(rand.NewSource(seed))

	for i := 0; i < 10; i++ {
		got1 := Pick(rng1)
		got2 := Pick(rng2)
		if got1 != got2 {
			t.Fatalf("pick %d: rng1=%+v rng2=%+v — not deterministic", i, got1, got2)
		}
	}
}

// TestPickReturnsFromPool verifies every Pick result comes from the declared pools.
func TestPickReturnsFromPool(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	names := NamePool()
	colors := ColorPool()

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	colorSet := make(map[string]bool, len(colors))
	for _, c := range colors {
		colorSet[c] = true
	}

	for i := 0; i < 50; i++ {
		id := Pick(rng)
		if !nameSet[id.Name] {
			t.Errorf("pick %d: Name %q not in NamePool", i, id.Name)
		}
		if !colorSet[id.Color] {
			t.Errorf("pick %d: Color %q not in ColorPool", i, id.Color)
		}
	}
}

// TestPickDistribution verifies that many picks exercise a broad variety of
// names and colors (guards against accidental hardcoded single index).
func TestPickDistribution(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	namesSeen := make(map[string]struct{})
	colorsSeen := make(map[string]struct{})

	for i := 0; i < 500; i++ {
		id := Pick(rng)
		namesSeen[id.Name] = struct{}{}
		colorsSeen[id.Color] = struct{}{}
	}

	if len(namesSeen) < 15 {
		t.Errorf("expected at least 15 unique names in 500 picks, got %d", len(namesSeen))
	}
	if len(colorsSeen) < 5 {
		t.Errorf("expected at least 5 unique colors in 500 picks, got %d", len(colorsSeen))
	}
}

// TestPickPanicsOnNilRng confirms that Pick(nil) panics.
func TestPickPanicsOnNilRng(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected Pick(nil) to panic, but it did not")
		}
	}()
	Pick(nil) //nolint:staticcheck // intentional nil to test panic
}

// TestNamePoolIsDefensiveCopy verifies that mutating the returned slice does
// not affect subsequent calls to NamePool.
func TestNamePoolIsDefensiveCopy(t *testing.T) {
	first := NamePool()
	original := first[0]
	first[0] = "MUTATED"

	second := NamePool()
	if second[0] != original {
		t.Errorf("NamePool returned the same backing slice: got %q after mutation, want %q",
			second[0], original)
	}
}

// TestColorPoolIsDefensiveCopy verifies that mutating the returned slice does
// not affect subsequent calls to ColorPool.
func TestColorPoolIsDefensiveCopy(t *testing.T) {
	first := ColorPool()
	original := first[0]
	first[0] = "#000000"

	second := ColorPool()
	if second[0] != original {
		t.Errorf("ColorPool returned the same backing slice: got %q after mutation, want %q",
			second[0], original)
	}
}

// TestNamePoolMinSize asserts the pool has at least 20 entries.
func TestNamePoolMinSize(t *testing.T) {
	if n := len(NamePool()); n < 20 {
		t.Errorf("NamePool has %d entries, want at least 20", n)
	}
}

// TestColorPoolMinSize asserts the pool has at least 8 entries.
func TestColorPoolMinSize(t *testing.T) {
	if n := len(ColorPool()); n < 8 {
		t.Errorf("ColorPool has %d entries, want at least 8", n)
	}
}
