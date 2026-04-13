package democtl

import "math/rand"

// Identity is a display-name + accent-color pair suitable for presence UI.
type Identity struct {
	Name  string
	Color string // hex like "#f59e0b"
}

// namePool is the internal pool of pronounceable, unisex, naturalist names.
var namePool = []string{
	"Auroch", "Basalt", "Cirrus", "Dune", "Ember", "Fern", "Glacier",
	"Hearth", "Iris", "Juniper", "Kelp", "Lumen", "Marrow", "Nimbus",
	"Opal", "Plume", "Quartz", "Rune", "Slate", "Tundra", "Umbra",
	"Vireo", "Willow", "Xenith", "Yarrow", "Zephyr",
}

// colorPool is the internal pool of accessible hex colors tuned for near-black
// backgrounds (#0b0b0d).
var colorPool = []string{
	"#f59e0b", "#ef4444", "#10b981", "#3b82f6", "#8b5cf6",
	"#ec4899", "#14b8a6", "#f97316", "#84cc16", "#06b6d4",
}

// Pick returns an Identity drawn from the fixed name+color pools using the
// supplied *rand.Rand. Callers control seeding. Pick never blocks and never
// panics on a non-nil rng. Passing nil panics (programmer error).
func Pick(rng *rand.Rand) Identity {
	if rng == nil {
		panic("democtl.Pick: rng must not be nil")
	}
	return Identity{
		Name:  namePool[rng.Intn(len(namePool))],
		Color: colorPool[rng.Intn(len(colorPool))],
	}
}

// NamePool returns a defensive copy of the internal name pool. Primarily for
// tests that want to assert "Pick returns something from this pool".
func NamePool() []string {
	return append([]string(nil), namePool...)
}

// ColorPool returns a defensive copy of the internal color pool.
func ColorPool() []string {
	return append([]string(nil), colorPool...)
}
