package physics

import (
	"runtime"
	"testing"
)

// A kinematic body that moves every step (a character, a platform) lives in
// the static grid and changes cells as it goes. Once it has visited its cells,
// a rebuild must not allocate: the grid once recorded every refilled static
// cell in a list it never read or trimmed, which grew without bound.
func TestStaticGridDoesNotGrowWithMovingKinematicBodies(t *testing.T) {
	w := NewWorld(WorldConfig{FixedTimestep: 1.0 / 60, BroadPhaseCell: 1})
	mover := w.AddBody(BodyConfig{IsKinematic: true})
	mover.AddCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.4, Height: 1})
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 20, Height: 1, Depth: 20, Offset: Vec3{Y: -1}})
	step := 0
	tour := func() {
		// Shuttle across eight cells and back.
		x := float64(step%16) - 4
		if step%16 >= 8 {
			x = float64(16-step%16) - 4
		}
		mover.Position = Vec3{X: x}
		w.broadphase.Rebuild(w.colliders)
		step++
	}
	for i := 0; i < 64; i++ {
		tour()
	}
	// Count bytes, not allocations: a list that grows by appending
	// allocates rarely but without end.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 20000; i++ {
		tour()
	}
	runtime.ReadMemStats(&after)
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 4096 {
		t.Fatalf("20000 rebuilds with a shuttling kinematic body allocated %d bytes", grown)
	}
}
