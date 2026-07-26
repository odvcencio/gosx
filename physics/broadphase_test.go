package physics

import (
	"math"
	"math/rand"
	"testing"
)

// bruteForcePairs is the reference broadphase: every pair, filtered by the same
// rules the grid uses, ordered by collider index.
func bruteForcePairs(colliders []*Collider, skin float64) []ColliderPair {
	var pairs []ColliderPair
	for i := 0; i < len(colliders); i++ {
		for j := i + 1; j < len(colliders); j++ {
			a := colliders[i]
			b := colliders[j]
			if a == nil || b == nil || a == b {
				continue
			}
			if a.Body != nil && a.Body == b.Body {
				continue
			}
			if immovableCollider(a) && immovableCollider(b) {
				continue
			}
			boxA := a.AABB().Expand(skin)
			boxB := b.AABB().Expand(skin)
			if boxA.IsFinite() && boxB.IsFinite() && !boxA.Overlaps(boxB) {
				continue
			}
			if b.index < a.index {
				a, b = b, a
			}
			pairs = append(pairs, ColliderPair{A: a, B: b})
		}
	}
	return pairs
}

func pairSetKey(p ColliderPair) [2]int {
	return [2]int{colliderIndex(p.A), colliderIndex(p.B)}
}

func TestCandidatePairsMatchBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60.0, BroadPhaseCell: 2})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	world.AddCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: -1}, Width: 40, Height: 1, Depth: 40})
	for i := 0; i < 120; i++ {
		body := world.AddBody(BodyConfig{
			Mass: 1,
			Position: Vec3{
				X: (rng.Float64() - 0.5) * 20,
				Y: rng.Float64() * 6,
				Z: (rng.Float64() - 0.5) * 20,
			},
			Velocity: Vec3{X: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
		})
		switch i % 4 {
		case 0:
			body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.4 + rng.Float64()*0.4})
		case 1:
			body.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.8, Height: 0.8, Depth: 0.8})
		case 2:
			body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.4, Height: 1})
		case 3:
			body.AddCollider(ColliderConfig{Shape: ShapeCone, Radius: 0.4, Height: 1})
		}
	}

	for step := 0; step < 20; step++ {
		got := world.CandidatePairs()
		want := bruteForcePairs(world.colliders, world.broadphase.skin)

		if len(got) != len(want) {
			t.Fatalf("step %d: grid returned %d pairs, brute force found %d", step, len(got), len(want))
		}
		wantSet := make(map[[2]int]bool, len(want))
		for _, pair := range want {
			wantSet[pairSetKey(pair)] = true
		}
		for _, pair := range got {
			if !wantSet[pairSetKey(pair)] {
				t.Fatalf("step %d: grid produced pair %v that brute force did not", step, pairSetKey(pair))
			}
			if colliderIndex(pair.A) > colliderIndex(pair.B) {
				t.Fatalf("step %d: pair %v is not ordered by collider index", step, pairSetKey(pair))
			}
		}
		// The pair order must be stable and sorted, because replay determinism
		// depends on the contact order.
		for i := 1; i < len(got); i++ {
			prevA, prevB := orderedColliderIndexes(got[i-1].A, got[i-1].B)
			currA, currB := orderedColliderIndexes(got[i].A, got[i].B)
			if prevA > currA || (prevA == currA && prevB >= currB) {
				t.Fatalf("step %d: pairs are not sorted at index %d: (%d,%d) then (%d,%d)",
					step, i, prevA, prevB, currA, currB)
			}
		}
		world.StepFixed()
	}
}

func TestCandidatePairsReturnsCopySafeSlice(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 0.4}})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	first := world.CandidatePairs()
	if len(first) == 0 {
		t.Fatal("expected at least one candidate pair")
	}
	kept := first[0]
	_ = world.CandidatePairs()
	if first[0] != kept {
		t.Fatal("CandidatePairs must return a slice the next call cannot overwrite")
	}
}

func TestSpatialHashQueryStaticAABBFindsOnlySolidStatics(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, BroadPhaseCell: 2})
	staticBox := world.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})
	plane := world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}, Distance: -10})
	trigger := world.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2, IsTrigger: true})
	farBox := world.AddCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{X: 60}, Width: 2, Height: 2, Depth: 2})
	dynamic := world.AddBody(BodyConfig{Mass: 1})
	dynamicCollider := dynamic.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	world.broadphase.Rebuild(world.colliders)
	found := world.broadphase.QueryStaticAABB(NewAABB(Vec3{X: -1, Y: -1, Z: -1}, Vec3{X: 1, Y: 1, Z: 1}), nil)

	seen := make(map[*Collider]int, len(found))
	for _, collider := range found {
		seen[collider]++
	}
	for _, collider := range found {
		if seen[collider] != 1 {
			t.Fatalf("collider %d appears %d times; the query must deduplicate", collider.index, seen[collider])
		}
	}
	if seen[staticBox] != 1 {
		t.Fatal("query missed the overlapping static box")
	}
	if seen[plane] != 1 {
		t.Fatal("query missed the plane, which has unbounded extent")
	}
	if seen[trigger] != 0 {
		t.Fatal("query must skip trigger colliders")
	}
	if seen[farBox] != 0 {
		t.Fatal("query must skip a static box outside the box")
	}
	if seen[dynamicCollider] != 0 {
		t.Fatal("query must skip dynamic colliders")
	}
}

func TestSpatialHashQueryAABBMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var colliders []*Collider
	hash := NewSpatialHash(2)
	for i := 0; i < 200; i++ {
		body := NewRigidBody(BodyConfig{Mass: 1, Position: Vec3{
			X: (rng.Float64() - 0.5) * 40,
			Y: (rng.Float64() - 0.5) * 40,
			Z: (rng.Float64() - 0.5) * 40,
		}})
		collider := body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.3 + rng.Float64()})
		collider.index = i + 1
		colliders = append(colliders, collider)
	}
	hash.Rebuild(colliders)

	for trial := 0; trial < 50; trial++ {
		center := Vec3{
			X: (rng.Float64() - 0.5) * 44,
			Y: (rng.Float64() - 0.5) * 44,
			Z: (rng.Float64() - 0.5) * 44,
		}
		half := 1 + rng.Float64()*5
		box := AABBFromCenterHalfExtents(center, Vec3{X: half, Y: half, Z: half})

		got := hash.QueryAABB(box, nil)
		gotSet := make(map[*Collider]bool, len(got))
		for _, collider := range got {
			gotSet[collider] = true
		}
		for _, collider := range colliders {
			want := collider.AABB().Expand(hash.skin).Overlaps(box)
			if want && !gotSet[collider] {
				t.Fatalf("trial %d: query missed collider %d", trial, collider.index)
			}
			if !want && gotSet[collider] {
				t.Fatalf("trial %d: query returned collider %d, which does not overlap", trial, collider.index)
			}
		}
	}
}

// TestSweepBodyMatchesFullStaticScan pins the CCD change: the grid-driven
// candidate lookup must find exactly the same nearest hit that a scan over
// every static collider finds.
func TestSweepBodyMatchesFullStaticScan(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, BroadPhaseCell: 2})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}, Distance: -5})
	for i := 0; i < 200; i++ {
		world.AddCollider(ColliderConfig{
			Shape: ShapeBox,
			Offset: Vec3{
				X: (rng.Float64() - 0.5) * 30,
				Y: (rng.Float64() - 0.5) * 30,
				Z: (rng.Float64() - 0.5) * 30,
			},
			Width: 1 + rng.Float64(), Height: 1 + rng.Float64(), Depth: 1 + rng.Float64(),
		})
		world.AddCollider(ColliderConfig{
			Shape: ShapeSphere,
			Offset: Vec3{
				X: (rng.Float64() - 0.5) * 30,
				Y: (rng.Float64() - 0.5) * 30,
				Z: (rng.Float64() - 0.5) * 30,
			},
			Radius: 0.5 + rng.Float64(),
		})
	}
	body := world.AddBody(BodyConfig{Mass: 1})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.4})

	world.broadphase.Rebuild(world.colliders)
	world.broadphaseStep = world.steps
	checked := 0
	for trial := 0; trial < 500; trial++ {
		body.Position = Vec3{
			X: (rng.Float64() - 0.5) * 30,
			Y: (rng.Float64() - 0.5) * 30,
			Z: (rng.Float64() - 0.5) * 30,
		}
		displacement := Vec3{
			X: (rng.Float64() - 0.5) * 12,
			Y: (rng.Float64() - 0.5) * 12,
			Z: (rng.Float64() - 0.5) * 12,
		}
		gotHit, gotOK := world.sweepBody(body, displacement)
		wantHit, wantOK := referenceSweepBody(world, body, displacement)
		if gotOK != wantOK {
			t.Fatalf("trial %d: grid sweep hit=%v, full scan hit=%v", trial, gotOK, wantOK)
		}
		if !gotOK {
			continue
		}
		checked++
		if math.Abs(gotHit.Distance-wantHit.Distance) > 1e-12 {
			t.Fatalf("trial %d: grid distance %v, full scan distance %v", trial, gotHit.Distance, wantHit.Distance)
		}
		if gotHit.Collider != wantHit.Collider {
			t.Fatalf("trial %d: grid hit collider %d, full scan hit collider %d",
				trial, colliderIndex(gotHit.Collider), colliderIndex(wantHit.Collider))
		}
	}
	if checked < 50 {
		t.Fatalf("only %d trials produced a hit; the test is not exercising the sweep", checked)
	}
}

// referenceSweepBody is the pre-optimisation sweep: scan every collider and
// filter inside the loop.
func referenceSweepBody(w *World, body *RigidBody, displacement Vec3) (ccdHit, bool) {
	if displacement.Len2() <= epsilon {
		return ccdHit{}, false
	}
	distance := displacement.Len()
	direction := displacement.Div(distance)
	best := ccdHit{Distance: math.Inf(1)}
	found := false
	for _, moving := range body.colliders {
		if moving == nil || moving.IsTrigger {
			continue
		}
		origin := moving.WorldCenter()
		radius, ok := movingSweepRadius(moving)
		if !ok {
			continue
		}
		for _, target := range w.colliders {
			if target == nil || target == moving || target.Body == body || target.IsTrigger || !staticCollider(target) {
				continue
			}
			hit, ok := sweepSphereLikeCollider(origin, radius, direction, distance, target)
			if !ok {
				continue
			}
			if !found || closerSweepHit(hit, best) {
				best = hit
				found = true
			}
		}
	}
	return best, found
}

func TestSweepUsesFreshPosesAfterPositionSolve(t *testing.T) {
	// A fast bullet must not tunnel even though the broadphase was filled
	// before the position solve moved bodies.
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: -1}, Width: 100, Height: 2, Depth: 100})
	bullet := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Velocity: Vec3{Y: -600}})
	bullet.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.05})

	world.StepFixed()

	if bullet.Position.Y < 0 {
		t.Fatalf("bullet tunneled through the static box to y=%v", bullet.Position.Y)
	}
}
