package physics

import (
	"math"
	"math/rand"
	"testing"
)

// castTargets returns one collider of every shape a cast can hit, each placed
// around center.
func castTargets(center Vec3, rotation Quat) map[string]ColliderConfig {
	hull := []Vec3{
		{X: -0.8, Y: -0.6, Z: -0.7}, {X: 0.9, Y: -0.5, Z: -0.6}, {X: 0.1, Y: 0.9, Z: -0.4},
		{X: -0.3, Y: -0.4, Z: 0.9}, {X: 0.6, Y: 0.5, Z: 0.7}, {X: -0.7, Y: 0.6, Z: 0.3},
	}
	return map[string]ColliderConfig{
		"sphere":   {Shape: ShapeSphere, Radius: 0.8, Offset: center},
		"box":      {Shape: ShapeBox, Width: 1.6, Height: 1.2, Depth: 1.0, Offset: center, Rotation: rotation},
		"capsule":  {Shape: ShapeCapsule, Radius: 0.5, Height: 1.2, Offset: center, Rotation: rotation},
		"cylinder": {Shape: ShapeCylinder, Radius: 0.7, Height: 1.4, Offset: center, Rotation: rotation},
		"cone":     {Shape: ShapeCone, Radius: 0.8, Height: 1.5, Offset: center, Rotation: rotation},
		"hull":     {Shape: ShapeConvexHull, Vertices: hull, Offset: center, Rotation: rotation},
		"mesh":     meshBoxConfig(center, rotation, Vec3{X: 0.9, Y: 0.7, Z: 0.8}),
	}
}

// meshBoxConfig is a closed box made of twelve triangles.
func meshBoxConfig(center Vec3, rotation Quat, half Vec3) ColliderConfig {
	v := []Vec3{
		{X: -half.X, Y: -half.Y, Z: -half.Z}, {X: half.X, Y: -half.Y, Z: -half.Z},
		{X: half.X, Y: half.Y, Z: -half.Z}, {X: -half.X, Y: half.Y, Z: -half.Z},
		{X: -half.X, Y: -half.Y, Z: half.Z}, {X: half.X, Y: -half.Y, Z: half.Z},
		{X: half.X, Y: half.Y, Z: half.Z}, {X: -half.X, Y: half.Y, Z: half.Z},
	}
	idx := []int{
		0, 2, 1, 0, 3, 2, // -Z
		4, 5, 6, 4, 6, 7, // +Z
		0, 1, 5, 0, 5, 4, // -Y
		3, 7, 6, 3, 6, 2, // +Y
		0, 4, 7, 0, 7, 3, // -X
		1, 2, 6, 1, 6, 5, // +X
	}
	return ColliderConfig{Shape: ShapeTriangleMesh, Vertices: v, Indices: idx, Offset: center, Rotation: rotation}
}

func castShapes() map[string]QueryShape {
	return map[string]QueryShape{
		"sphere":  SphereShape(0.45),
		"capsule": CapsuleShape(0.35, 0.9),
		"box":     BoxShape(0.7, 0.9, 0.5),
	}
}

// referenceOverlap decides overlap without the swept GJK the casts use. A
// sphere against a sphere, box, capsule, cylinder or mesh triangle uses the
// exact closest point; every other pair uses referenceGJKOverlap; a plane is
// the half-space test.
func referenceOverlap(target *Collider, shape QueryShape, center Vec3, rotation Quat) bool {
	if target.Shape == ShapePlane {
		normal, offset := target.Plane()
		core, margin := shape.core(center, rotation)
		return normal.Dot(core.support(normal.Neg()))-offset-margin <= 0
	}
	if shape.Shape == ShapeSphere {
		if d, ok := pointDistance(target, center); ok {
			return d <= shape.Radius
		}
	}
	moving := shape
	full, margin := moving.core(center, rotation)
	full.margin = margin
	if target.Shape == ShapeTriangleMesh {
		basis := mat3FromQuat(target.WorldRotation())
		origin := target.WorldCenter()
		for _, tri := range target.mesh.tris {
			world := triangleShape(origin.Add(basis.mul(tri.a)), origin.Add(basis.mul(tri.b)), origin.Add(basis.mul(tri.c)))
			if referenceGJKOverlap(world, full) {
				return true
			}
		}
		return false
	}
	other, ok := newConvexShape(target)
	return ok && referenceGJKOverlap(full, other)
}

// pointDistance is the exact distance from p to a solid collider, for the
// shapes with a closed-form closest point.
func pointDistance(c *Collider, p Vec3) (float64, bool) {
	basis := mat3FromQuat(c.WorldRotation())
	local := basis.mulT(p.Sub(c.WorldCenter()))
	switch c.Shape {
	case ShapeSphere:
		return math.Max(0, local.Len()-c.Radius), true
	case ShapeBox:
		h := c.halfExtents()
		d := Vec3{X: math.Max(0, math.Abs(local.X)-h.X), Y: math.Max(0, math.Abs(local.Y)-h.Y), Z: math.Max(0, math.Abs(local.Z)-h.Z)}
		return d.Len(), true
	case ShapeCapsule:
		y := clampFloat(local.Y, -c.Height/2, c.Height/2)
		return math.Max(0, local.Sub(Vec3{Y: y}).Len()-c.Radius), true
	case ShapeCylinder:
		radial := math.Hypot(local.X, local.Z)
		return math.Hypot(math.Max(0, radial-c.Radius), math.Max(0, math.Abs(local.Y)-c.Height/2)), true
	case ShapeTriangleMesh:
		best := math.Inf(1)
		for _, tri := range c.mesh.tris {
			best = math.Min(best, closestPointOnTriangle(local, tri.a, tri.b, tri.c).Sub(local).Len())
		}
		return best, true
	}
	return 0, false
}

// referenceGJKOverlap is the boolean GJK of gjkOverlap with every support
// direction normalized first, which makes it exact on cylinder and cone rims.
func referenceGJKOverlap(a, b convexShape) bool {
	support := func(dir Vec3) minkowskiPoint { return minkowskiSupport(a, b, dir.Normalize()) }
	var simplex gjkSimplex
	dir := b.center.Sub(a.center)
	if dir.Len2() <= epsilon {
		dir = Vec3{X: 1}
	}
	simplex.points[0] = support(dir)
	simplex.count = 1
	dir = simplex.points[0].diff.Neg()
	if dir.Len2() <= 1e-30 {
		return true
	}
	for i := 0; i < 128; i++ {
		next := support(dir)
		if next.diff.Dot(dir) < 0 {
			return false
		}
		simplex.points[simplex.count] = next
		simplex.count++
		contains, newDir := gjkReduce(&simplex)
		if contains {
			return true
		}
		if newDir.Len2() <= 1e-30 {
			return true
		}
		dir = newDir
	}
	return false
}

// referenceCast finds the first touch of shape moving from origin along
// direction by marching in small steps with referenceOverlap, then bisecting.
// It shares no code with the swept GJK the casts use.
func referenceCast(target *Collider, shape QueryShape, origin Vec3, rotation Quat, direction Vec3, maxDistance float64) (float64, bool) {
	direction = direction.Normalize()
	overlaps := func(d float64) bool {
		return referenceOverlap(target, shape, origin.Add(direction.Mul(d)), rotation)
	}
	if overlaps(0) {
		return 0, true
	}
	const samples = 4000
	step := maxDistance / samples
	low := 0.0
	for i := 1; i <= samples; i++ {
		high := float64(i) * step
		if !overlaps(high) {
			low = high
			continue
		}
		for j := 0; j < 60; j++ {
			mid := (low + high) * 0.5
			if overlaps(mid) {
				high = mid
			} else {
				low = mid
			}
		}
		return high, true
	}
	return 0, false
}

func randomQuat(rng *rand.Rand) Quat {
	return QuatFromAxisAngle(Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5}, rng.Float64()*2*math.Pi)
}

func TestShapeCastMatchesReferenceForEveryPair(t *testing.T) {
	rotation := QuatFromAxisAngle(Vec3{X: 0.3, Y: 1, Z: 0.2}, 0.6)
	moving := QuatFromAxisAngle(Vec3{X: 1, Y: 0.4, Z: -0.3}, 0.9)
	targets := castTargets(Vec3{X: 4, Y: 0.15, Z: -0.1}, rotation)
	for shapeName, shape := range castShapes() {
		for targetName, config := range targets {
			target := NewCollider(config)
			if err := target.Err(); err != nil {
				t.Fatalf("%s: %v", targetName, err)
			}
			origin := Vec3{X: 0, Y: 0.1, Z: 0.05}
			direction := Vec3{X: 1, Y: 0.02, Z: -0.01}
			hit, ok := target.ShapeCast(shape, origin, moving, direction, 8)
			want, wantOK := referenceCast(target, shape, origin, moving, direction, 8)
			if ok != wantOK {
				t.Fatalf("%s vs %s: hit=%v, reference hit=%v", shapeName, targetName, ok, wantOK)
			}
			if !ok {
				continue
			}
			if math.Abs(hit.Distance-want) > 2e-4 {
				t.Errorf("%s vs %s: distance %.7f, reference %.7f", shapeName, targetName, hit.Distance, want)
			}
			if math.Abs(hit.Fraction*8-hit.Distance) > 1e-12 {
				t.Errorf("%s vs %s: fraction %v does not match distance %v", shapeName, targetName, hit.Fraction, hit.Distance)
			}
			if math.Abs(hit.Normal.Len()-1) > 1e-9 {
				t.Errorf("%s vs %s: normal %+v is not unit", shapeName, targetName, hit.Normal)
			}
			if hit.Normal.Dot(direction.Normalize()) >= 0 {
				t.Errorf("%s vs %s: normal %+v does not face the cast", shapeName, targetName, hit.Normal)
			}
			if hit.Collider != target || hit.StartOverlap {
				t.Errorf("%s vs %s: collider %p start %v", shapeName, targetName, hit.Collider, hit.StartOverlap)
			}
			// The contact point lies on the target surface: a tiny ball there
			// touches it, and the same ball pushed out along the normal does not.
			probe := SphereShape(1e-3)
			if !target.Overlaps(probe, hit.Point, IdentityQuat()) {
				t.Errorf("%s vs %s: point %+v is off the surface", shapeName, targetName, hit.Point)
			}
			if target.Overlaps(probe, hit.Point.Add(hit.Normal.Mul(5e-3)), IdentityQuat()) {
				t.Errorf("%s vs %s: normal %+v points into the target", shapeName, targetName, hit.Normal)
			}
		}
	}
}

func TestShapeCastAgainstPlane(t *testing.T) {
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}, Distance: -2})
	cases := []struct {
		name  string
		shape QueryShape
		rot   Quat
		want  float64
	}{
		{"sphere", SphereShape(0.5), IdentityQuat(), 1.5},
		{"capsule", CapsuleShape(0.25, 1), IdentityQuat(), 1.25},
		{"lying capsule", CapsuleShape(0.25, 1), QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2), 1.75},
		{"box", BoxShape(1, 1, 1), IdentityQuat(), 1.5},
		{"tilted box", BoxShape(1, 1, 1), QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/4), 2 - math.Sqrt2/2},
	}
	for _, tc := range cases {
		hit, ok := plane.ShapeCast(tc.shape, Vec3{}, tc.rot, Vec3{Y: -1}, 10)
		if !ok || math.Abs(hit.Distance-tc.want) > 1e-9 {
			t.Fatalf("%s: hit=%v distance=%v, want %v", tc.name, ok, hit.Distance, tc.want)
		}
		if !hit.Normal.Near(Vec3{Y: 1}, 1e-12) || math.Abs(hit.Point.Y+2) > 1e-9 {
			t.Fatalf("%s: normal %+v point %+v", tc.name, hit.Normal, hit.Point)
		}
	}
	if _, ok := plane.ShapeCast(SphereShape(0.5), Vec3{}, IdentityQuat(), Vec3{X: 1}, 10); ok {
		t.Fatal("a cast parallel to a plane must miss it")
	}
	if _, ok := plane.ShapeCast(SphereShape(0.5), Vec3{}, IdentityQuat(), Vec3{Y: 1}, 10); ok {
		t.Fatal("a cast away from a plane must miss it")
	}
}

func TestShapeCastStartOverlap(t *testing.T) {
	box := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 2, Height: 2, Depth: 2})
	sphere := SphereShape(0.5)

	// Deep inside: moving in any direction leaves, which must not count as a
	// hit only when the direction leads out along the separation normal.
	hit, ok := box.ShapeCast(sphere, Vec3{X: 0.9}, IdentityQuat(), Vec3{X: -1}, 1)
	if !ok || !hit.StartOverlap || hit.Distance != 0 {
		t.Fatalf("moving deeper: hit=%v %+v", ok, hit)
	}
	// EPA on a round shape converges to 1e-4 of its size.
	if !hit.Normal.Near(Vec3{X: 1}, 1e-4) {
		t.Fatalf("start overlap normal = %+v, want +X", hit.Normal)
	}
	if _, ok := box.ShapeCast(sphere, Vec3{X: 0.9}, IdentityQuat(), Vec3{X: 1}, 1); ok {
		t.Fatal("moving out of an overlap must not report a hit")
	}

	// Touching at the surface and moving along it slides freely.
	if _, ok := box.ShapeCast(sphere, Vec3{X: 1.5}, IdentityQuat(), Vec3{Z: 1}, 1); ok {
		t.Fatal("sliding along a touched face must not report a hit")
	}
	hit, ok = box.ShapeCast(sphere, Vec3{X: 1.5}, IdentityQuat(), Vec3{X: -1}, 1)
	if !ok || !hit.StartOverlap {
		t.Fatalf("pushing into a touched face: hit=%v %+v", ok, hit)
	}

	// The same rules hold for a mesh, through the triangle path.
	mesh := NewCollider(meshBoxConfig(Vec3{}, IdentityQuat(), Vec3{X: 1, Y: 1, Z: 1}))
	if _, ok := mesh.ShapeCast(sphere, Vec3{X: 1.5}, IdentityQuat(), Vec3{Z: 1}, 0.5); ok {
		t.Fatal("sliding along a touched mesh face must not report a hit")
	}
	if _, ok := mesh.ShapeCast(sphere, Vec3{X: 1.2}, IdentityQuat(), Vec3{X: 1}, 1); ok {
		t.Fatal("moving out of a mesh face overlap must not report a hit")
	}
}

func TestWorldShapeCastPicksNearestAndBreaksTiesByIndex(t *testing.T) {
	build := func(reverse bool, cell float64) (*World, []*Collider) {
		w := NewWorld(WorldConfig{FixedTimestep: 1.0 / 60, BroadPhaseCell: cell})
		configs := []ColliderConfig{
			{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1, Offset: Vec3{X: 5, Z: 0.6}},
			{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1, Offset: Vec3{X: 5, Z: -0.6}},
			{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1, Offset: Vec3{X: 9}},
		}
		if reverse {
			configs[0], configs[1] = configs[1], configs[0]
		}
		var out []*Collider
		for _, config := range configs {
			out = append(out, w.AddCollider(config))
		}
		return w, out
	}
	for _, cell := range []float64{0.5, 2, 16} {
		for _, reverse := range []bool{false, true} {
			w, colliders := build(reverse, cell)
			hit, ok := w.SphereCast(Vec3{}, 0.5, Vec3{X: 1}, 20, QueryFilter{})
			if !ok {
				t.Fatal("expected a hit")
			}
			// Both near boxes are hit at the same distance; the first one added
			// has the lower index and must win.
			if hit.Collider != colliders[0] {
				t.Fatalf("cell %v reverse %v: hit collider %d, want %d", cell, reverse, hit.Collider.index, colliders[0].index)
			}
		}
	}
}

func TestWorldQueriesApplyFilters(t *testing.T) {
	w := NewWorld(WorldConfig{FixedTimestep: 1.0 / 60})
	const enemy, pickup = 1 << 1, 1 << 2
	wall := w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 2, Depth: 2, Offset: Vec3{X: 6}})
	foe := w.AddBody(BodyConfig{ID: "foe", Mass: 1, Position: Vec3{X: 4}})
	foeCollider := foe.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5, Layer: enemy})
	zone := w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 2, Depth: 2, Offset: Vec3{X: 2}, IsTrigger: true, Layer: pickup})

	cast := func(filter QueryFilter) *Collider {
		hit, ok := w.SphereCast(Vec3{}, 0.25, Vec3{X: 1}, 20, filter)
		if !ok {
			return nil
		}
		return hit.Collider
	}
	if got := cast(QueryFilter{}); got != foeCollider {
		t.Fatalf("default filter hit %v, want the foe (triggers are skipped)", got)
	}
	if got := cast(QueryFilter{IncludeTriggers: true}); got != zone {
		t.Fatalf("IncludeTriggers hit %v, want the trigger zone", got)
	}
	if got := cast(QueryFilter{Mask: LayerDefault}); got != wall {
		t.Fatalf("default-layer mask hit %v, want the wall", got)
	}
	if got := cast(QueryFilter{ExcludeBody: foe}); got != wall {
		t.Fatalf("ExcludeBody hit %v, want the wall", got)
	}
	if got := cast(QueryFilter{ExcludeCollider: foeCollider}); got != wall {
		t.Fatalf("ExcludeCollider hit %v, want the wall", got)
	}
	// A query with a Layer only sees colliders whose Mask includes it: the
	// foe collides with the default and enemy layers, not with pickups.
	foeCollider.SetCollisionFilter(enemy, LayerDefault|enemy)
	if got := cast(QueryFilter{Layer: enemy}); got != foeCollider {
		t.Fatalf("enemy-layer query hit %v, want the foe", got)
	}
	if got := cast(QueryFilter{Layer: pickup}); got != wall {
		t.Fatalf("pickup-layer query hit %v; the foe masks out pickups", got)
	}

	if hit, ok := w.RaycastFiltered(Ray{Direction: Vec3{X: 1}}, 0, QueryFilter{Mask: LayerDefault}); !ok || hit.Collider != wall {
		t.Fatalf("filtered raycast hit %v", hit.Collider)
	}
	if hit, ok := w.Raycast(Ray{Direction: Vec3{X: 1}}, 0); !ok || hit.Collider != zone {
		t.Fatalf("Raycast keeps hitting triggers; hit %v", hit.Collider)
	}
	if got := w.OverlapBox(Vec3{X: 4}, IdentityQuat(), Vec3{X: 6, Y: 1, Z: 1}, QueryFilter{IncludeTriggers: true}, nil); len(got) != 3 ||
		got[0] != wall || got[1] != foeCollider || got[2] != zone {
		t.Fatalf("overlap returned %v, want all three in index order", got)
	}
}

func TestWorldQueriesSeeBodiesMovedBetweenSteps(t *testing.T) {
	w := NewWorld(WorldConfig{FixedTimestep: 1.0 / 60})
	platform := w.AddBody(BodyConfig{ID: "platform", IsKinematic: true, Position: Vec3{X: 50}})
	platform.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	w.StepFixed()
	if _, ok := w.SphereCast(Vec3{}, 0.25, Vec3{X: 1}, 10, QueryFilter{}); ok {
		t.Fatal("platform is out of reach")
	}
	platform.Position = Vec3{X: 5}
	hit, ok := w.SphereCast(Vec3{}, 0.25, Vec3{X: 1}, 10, QueryFilter{})
	if !ok || math.Abs(hit.Distance-4.25) > 1e-6 {
		t.Fatalf("moved platform: hit=%v distance=%v, want 4.25", ok, hit.Distance)
	}
	// A body-less collider added after the step is seen at once.
	wall := w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1, Offset: Vec3{X: 2}})
	if hit, ok := w.SphereCast(Vec3{}, 0.25, Vec3{X: 1}, 10, QueryFilter{}); !ok || hit.Collider != wall {
		t.Fatalf("new wall: hit=%v %v", ok, hit.Collider)
	}
	w.RemoveCollider(wall)
	if hit, ok := w.SphereCast(Vec3{}, 0.25, Vec3{X: 1}, 10, QueryFilter{}); !ok || hit.Body != platform {
		t.Fatalf("after removing the wall: hit=%v %v", ok, hit.Collider)
	}
}

func TestGenericCastMatchesDedicatedPaths(t *testing.T) {
	rotation := QuatFromAxisAngle(Vec3{X: 1, Y: 1}, 0.4)
	targets := castTargets(Vec3{X: 3, Y: 0.2}, rotation)
	for _, name := range []string{"box", "capsule", "sphere", "mesh", "hull"} {
		target := NewCollider(targets[name])
		for shapeName, shape := range castShapes() {
			var q castQuery
			if !prepareCast(&q, shape, Vec3{}, IdentityQuat(), Vec3{X: 1, Y: 0.03}, 6) {
				t.Fatal("prepare")
			}
			var tris []int
			var contacts []ContactManifold
			var probe Collider
			q.tris, q.contacts, q.probe = &tris, &contacts, &probe
			want, ok := castCollider(target, &q, 1)
			if !ok {
				t.Fatalf("%s vs %s: dedicated path missed", shapeName, name)
			}
			got, ok := castGeneric(target, &q, 1)
			if !ok {
				t.Fatalf("%s vs %s: generic path missed", shapeName, name)
			}
			if math.Abs(got.Fraction-want.Fraction)*6 > 1e-5 {
				t.Errorf("%s vs %s: generic distance %.7f, dedicated %.7f", shapeName, name, got.Fraction*6, want.Fraction*6)
			}
			if got.Normal.Dot(want.Normal) < 0.98 {
				t.Errorf("%s vs %s: generic normal %+v, dedicated %+v", shapeName, name, got.Normal, want.Normal)
			}
		}
	}
}

// checkCastAgainstReference compares one random cast with the brute-force
// reference. It is shared by the seeded test and the fuzz target.
func checkCastAgainstReference(t *testing.T, seed int64) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	names := []string{"sphere", "box", "capsule", "cylinder", "cone", "hull", "mesh"}
	shapes := []QueryShape{SphereShape(0.2 + rng.Float64()*0.5), CapsuleShape(0.15+rng.Float64()*0.4, rng.Float64()*1.2), BoxShape(0.2+rng.Float64(), 0.2+rng.Float64(), 0.2+rng.Float64())}
	center := Vec3{X: (rng.Float64() - 0.5) * 3, Y: (rng.Float64() - 0.5) * 3, Z: (rng.Float64() - 0.5) * 3}
	target := NewCollider(castTargets(center, randomQuat(rng))[names[rng.Intn(len(names))]])
	shape := shapes[rng.Intn(len(shapes))]
	rotation := randomQuat(rng)
	origin := Vec3{X: -4 + rng.Float64(), Y: (rng.Float64() - 0.5) * 2, Z: (rng.Float64() - 0.5) * 2}
	// Aim roughly at the target, so most casts hit and some graze or miss.
	aim := center.Add(Vec3{X: (rng.Float64() - 0.5) * 3, Y: (rng.Float64() - 0.5) * 3, Z: (rng.Float64() - 0.5) * 3})
	direction := aim.Sub(origin)
	const maxDistance = 9.0
	if referenceOverlap(target, shape, origin, rotation) {
		return
	}
	hit, ok := target.ShapeCast(shape, origin, rotation, direction, maxDistance)
	want, wantOK := referenceCast(target, shape, origin, rotation, direction, maxDistance)
	if ok && wantOK {
		if math.Abs(hit.Distance-want) > 5e-4 {
			t.Fatalf("seed %d (%v vs %v): distance %.7f, reference %.7f", seed, shape.Shape, target.Shape, hit.Distance, want)
		}
		return
	}
	if ok != wantOK {
		// A grazing pass can touch within the reference's resolution: accept
		// a disagreement only when the shape barely reaches the target.
		d := maxDistance
		if ok {
			d = hit.Distance
		} else {
			d = want
		}
		end := origin.Add(direction.Normalize().Mul(d))
		if !referenceOverlap(target, grownShape(shape, 1e-3), end, rotation) || referenceOverlap(target, grownShape(shape, -1e-3), end, rotation) {
			t.Fatalf("seed %d (%v vs %v): hit=%v (%.7f), reference hit=%v (%.7f)", seed, shape.Shape, target.Shape, ok, hit.Distance, wantOK, want)
		}
	}
}

func grownShape(shape QueryShape, by float64) QueryShape {
	switch shape.Shape {
	case ShapeBox:
		return BoxShape(shape.Width+2*by, shape.Height+2*by, shape.Depth+2*by)
	default:
		shape.Radius += by
		return shape
	}
}

func TestShapeCastRandomizedAgainstReference(t *testing.T) {
	count := 300
	if testing.Short() {
		count = 60
	}
	for seed := int64(1); seed <= int64(count); seed++ {
		checkCastAgainstReference(t, seed)
	}
}

func FuzzShapeCast(f *testing.F) {
	for seed := int64(1); seed <= 8; seed++ {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		checkCastAgainstReference(t, seed)
	})
}

func TestOverlapQueriesEveryShape(t *testing.T) {
	w := NewWorld(WorldConfig{FixedTimestep: 1.0 / 60})
	configs := castTargets(Vec3{}, QuatFromAxisAngle(Vec3{Y: 1}, 0.3))
	order := []string{"sphere", "box", "capsule", "cylinder", "cone", "hull", "mesh"}
	var colliders []*Collider
	for i, name := range order {
		config := configs[name]
		config.Offset = Vec3{X: float64(i) * 4}
		colliders = append(colliders, w.AddCollider(config))
	}
	floor := w.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}, Distance: -5})
	for i, c := range colliders {
		center := Vec3{X: float64(i) * 4}
		radius := 0.1
		if order[i] == "mesh" {
			// A mesh is a surface: a ball at its centre must reach a face.
			radius = 1
		}
		got := w.OverlapSphere(center, radius, QueryFilter{}, nil)
		if len(got) != 1 || got[0] != c {
			t.Fatalf("%s: overlap at its centre returned %v", order[i], got)
		}
		if got := w.OverlapCapsule(center.Add(Vec3{Z: 3}), IdentityQuat(), 0.3, 1, QueryFilter{}, nil); len(got) != 0 {
			t.Fatalf("%s: capsule far off overlapped %v", order[i], got)
		}
	}
	// A long box across the row overlaps every collider, reported in index
	// order; dropping it to the floor adds the plane.
	row := w.OverlapBox(Vec3{X: 12}, IdentityQuat(), Vec3{X: 30, Y: 0.2, Z: 0.2}, QueryFilter{}, nil)
	if len(row) != len(colliders) {
		t.Fatalf("row overlap returned %d colliders, want %d", len(row), len(colliders))
	}
	for i := range row {
		if row[i] != colliders[i] {
			t.Fatalf("row overlap out of index order at %d", i)
		}
	}
	if got := w.OverlapSphere(Vec3{X: 100, Y: -5}, 0.5, QueryFilter{}, nil); len(got) != 1 || got[0] != floor {
		t.Fatalf("floor overlap returned %v", got)
	}
}

func TestQueriesDoNotAllocateInSteadyState(t *testing.T) {
	w := queryBenchWorld(1)
	filter := QueryFilter{}
	var out []*Collider
	checks := map[string]func(){
		"sphere cast": func() {
			w.SphereCast(Vec3{X: -1, Y: 1, Z: 0.3}, 0.4, Vec3{X: 1, Y: -0.2}, 12, filter)
		},
		"capsule cast mesh": func() {
			w.CapsuleCast(Vec3{X: 3, Y: 3, Z: 3}, IdentityQuat(), 0.35, 1.1, Vec3{Y: -1}, 6, filter)
		},
		"box cast": func() {
			w.BoxCast(Vec3{Y: 1}, QuatFromAxisAngle(Vec3{Y: 1}, 0.4), Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Vec3{X: 1}, 15, filter)
		},
		"overlap": func() {
			out = w.OverlapCapsule(Vec3{X: 2, Y: 0.5}, IdentityQuat(), 0.5, 1, filter, out[:0])
		},
		"raycast": func() {
			w.RaycastFiltered(Ray{Origin: Vec3{Y: 5}, Direction: Vec3{X: 0.3, Y: -1}}, 30, filter)
		},
	}
	for name, run := range checks {
		run()
		if allocs := testing.AllocsPerRun(200, run); allocs != 0 {
			t.Errorf("%s allocates %.1f times per query", name, allocs)
		}
	}
}
