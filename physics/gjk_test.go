package physics

import (
	"math"
	"math/rand"
	"testing"
)

// randomUnitQuat returns a uniformly distributed rotation.
func randomUnitQuat(rng *rand.Rand) Quat {
	u1 := rng.Float64()
	u2 := rng.Float64() * 2 * math.Pi
	u3 := rng.Float64() * 2 * math.Pi
	s1 := math.Sqrt(1 - u1)
	s2 := math.Sqrt(u1)
	return Quat{
		X: s1 * math.Sin(u2),
		Y: s1 * math.Cos(u2),
		Z: s2 * math.Sin(u3),
		W: s2 * math.Cos(u3),
	}.Normalize()
}

func mustShape(t *testing.T, c *Collider) convexShape {
	t.Helper()
	shape, ok := newConvexShape(c)
	if !ok {
		t.Fatalf("newConvexShape(%v) failed: %v", c.Shape, c.Err())
	}
	return shape
}

func TestGJKSphereSphereMatchesAnalyticPenetration(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeSphere, Radius: 1})
	b := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.5}, Radius: 1})

	simplex, overlap := gjkOverlap(mustShape(t, a), mustShape(t, b))
	if !overlap {
		t.Fatal("gjkOverlap missed two overlapping spheres")
	}
	result, ok := epaPenetration(mustShape(t, a), mustShape(t, b), simplex)
	if !ok {
		t.Fatal("epaPenetration failed on two overlapping spheres")
	}
	// Two curved surfaces are the worst case for EPA, because the polytope is
	// inscribed in a ball and the depth converges from below. The error stays
	// under 0.3% of the depth at the shipped tolerance, and the normal stays
	// within 0.6 degrees. Production never routes a sphere pair here;
	// collideSphereSphere is analytic.
	if math.Abs(result.Depth-0.5) > 2e-3 {
		t.Fatalf("depth = %v, want 0.5", result.Depth)
	}
	if result.Normal.Dot(Vec3{X: 1}) < 0.99995 {
		t.Fatalf("normal = %+v, want within 0.6 degrees of +X (A toward B)", result.Normal)
	}
	if !result.ContactPoint().Near(Vec3{X: 0.75}, 5e-3) {
		t.Fatalf("contact point = %+v, want (0.75,0,0)", result.ContactPoint())
	}
}

// TestGJKCylinderSphereMatchesAnalyticPenetration pins the accuracy of a pair
// that production really does route through GJK. The cylinder's lateral surface
// is flat along its axis, so EPA lands on it almost exactly.
func TestGJKCylinderSphereMatchesAnalyticPenetration(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.2}, Radius: 0.5})

	manifold, ok := Collide(cylinder, sphere)
	if !ok {
		t.Fatal("expected a cylinder-sphere contact")
	}
	// Radial distance 1.2, radii sum 1.5, so the overlap is 0.3.
	if got := manifold.Points[0].Penetration; math.Abs(got-0.3) > 1e-4 {
		t.Fatalf("penetration = %v, want 0.3", got)
	}
	if manifold.Normal.Dot(Vec3{X: 1}) < 0.9999 {
		t.Fatalf("normal = %+v, want +X (cylinder toward sphere)", manifold.Normal)
	}
	// The contact sits between the cylinder wall at x=1 and the sphere wall at
	// x=0.7, so the midpoint is x=0.85.
	if got := manifold.Points[0].Point; math.Abs(got.X-0.85) > 1e-3 {
		t.Fatalf("contact point = %+v, want x near 0.85", got)
	}
}

func TestGJKSeparatedShapesReportNoOverlap(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	b := NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{X: 5}, Radius: 0.5, Height: 2})
	if _, overlap := gjkOverlap(mustShape(t, a), mustShape(t, b)); overlap {
		t.Fatal("gjkOverlap reported overlap for shapes 5 units apart")
	}
}

// TestGJKEPAProducesMinimumTranslationVector checks the EPA result the way a
// solver uses it: moving B by normal*depth must just separate the pair, and
// moving it by slightly less must not.
func TestGJKEPAProducesMinimumTranslationVector(t *testing.T) {
	rng := rand.New(rand.NewSource(20260726))
	cases := 0
	for trial := 0; trial < 400; trial++ {
		rotA := randomUnitQuat(rng)
		rotB := randomUnitQuat(rng)
		offset := Vec3{
			X: (rng.Float64() - 0.5) * 2.4,
			Y: (rng.Float64() - 0.5) * 2.4,
			Z: (rng.Float64() - 0.5) * 2.4,
		}
		a := NewCollider(ColliderConfig{Shape: ShapeBox, Rotation: rotA, Width: 1.4, Height: 1, Depth: 0.8})
		b := NewCollider(ColliderConfig{Shape: ShapeBox, Rotation: rotB, Offset: offset, Width: 1, Height: 1.2, Depth: 1})

		shapeA := mustShape(t, a)
		shapeB := mustShape(t, b)
		simplex, overlap := gjkOverlap(shapeA, shapeB)
		if !overlap {
			continue
		}
		result, ok := epaPenetration(shapeA, shapeB, simplex)
		if !ok {
			t.Fatalf("trial %d: epaPenetration failed on an overlapping pair", trial)
		}
		if result.Depth <= 1e-4 {
			continue
		}
		cases++

		// Pushing B along the normal by the full depth separates the pair.
		pushed := NewCollider(ColliderConfig{
			Shape: ShapeBox, Rotation: rotB,
			Offset: offset.Add(result.Normal.Mul(result.Depth + 1e-6)),
			Width:  1, Height: 1.2, Depth: 1,
		})
		if _, still := gjkOverlap(shapeA, mustShape(t, pushed)); still {
			t.Fatalf("trial %d: pair still overlaps after moving B by normal*depth (depth=%v normal=%+v)",
				trial, result.Depth, result.Normal)
		}

		// Pushing by 80% of the depth must leave them overlapping, which proves
		// the depth is not an overestimate.
		short := NewCollider(ColliderConfig{
			Shape: ShapeBox, Rotation: rotB,
			Offset: offset.Add(result.Normal.Mul(result.Depth * 0.8)),
			Width:  1, Height: 1.2, Depth: 1,
		})
		if _, still := gjkOverlap(shapeA, mustShape(t, short)); !still {
			t.Fatalf("trial %d: pair separated after moving B by only 80%% of depth %v",
				trial, result.Depth)
		}
	}
	if cases < 100 {
		t.Fatalf("only %d overlapping trials produced a usable depth; the test is not exercising EPA", cases)
	}
}

// TestGJKEPAAgreesWithReferenceSAT compares GJK and EPA against an independent
// brute-force separating-axis test over all 15 box axes. The reference makes no
// use of the shipped SAT code, so the two paths cannot share a mistake.
func TestGJKEPAAgreesWithReferenceSAT(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	compared := 0
	for trial := 0; trial < 500; trial++ {
		rotA := randomUnitQuat(rng)
		rotB := randomUnitQuat(rng)
		offset := Vec3{
			X: (rng.Float64() - 0.5) * 2.2,
			Y: (rng.Float64() - 0.5) * 2.2,
			Z: (rng.Float64() - 0.5) * 2.2,
		}
		a := NewCollider(ColliderConfig{Shape: ShapeBox, Rotation: rotA, Width: 1, Height: 1, Depth: 1})
		b := NewCollider(ColliderConfig{Shape: ShapeBox, Rotation: rotB, Offset: offset, Width: 1, Height: 1, Depth: 1})

		wantNormal, wantDepth, wantOverlap := referenceBoxSAT(a, b)
		gjk, gjkOK := collideConvexGJK(a, b)
		if !wantOverlap {
			if gjkOK && gjk.Points[0].Penetration > 1e-6 {
				t.Fatalf("trial %d: GJK reported depth %v but the reference found separation",
					trial, gjk.Points[0].Penetration)
			}
			continue
		}
		if wantDepth <= 1e-4 {
			continue
		}
		if !gjkOK {
			t.Fatalf("trial %d: reference found depth %v but GJK found no overlap", trial, wantDepth)
		}
		compared++
		if math.Abs(gjk.Points[0].Penetration-wantDepth) > 1e-6 {
			t.Fatalf("trial %d: GJK depth %v, reference depth %v", trial, gjk.Points[0].Penetration, wantDepth)
		}
		if gjk.Normal.Dot(wantNormal) < 0.999 {
			t.Fatalf("trial %d: GJK normal %+v, reference normal %+v (depth %v)",
				trial, gjk.Normal, wantNormal, wantDepth)
		}
	}
	if compared < 100 {
		t.Fatalf("only %d comparable trials; the test is not exercising EPA", compared)
	}
}

// referenceBoxSAT is a deliberately simple separating-axis test. It projects
// every box vertex onto each of the 15 candidate axes, which are the face
// normals of the Minkowski difference of two boxes, and keeps the axis that
// needs the shortest translation. The returned normal points from A toward B,
// and translating B along it by the returned depth separates the pair.
//
// Note that the translation along one axis is min(maxA-minB, maxB-minA), not
// the length of the interval overlap. The two agree only when neither
// projection contains the other.
func referenceBoxSAT(a, b *Collider) (Vec3, float64, bool) {
	vertsA := worldBoxVertices(a)
	vertsB := worldBoxVertices(b)
	rotA := a.WorldRotation()
	rotB := b.WorldRotation()
	axesA := [3]Vec3{rotA.Rotate(Vec3{X: 1}), rotA.Rotate(Vec3{Y: 1}), rotA.Rotate(Vec3{Z: 1})}
	axesB := [3]Vec3{rotB.Rotate(Vec3{X: 1}), rotB.Rotate(Vec3{Y: 1}), rotB.Rotate(Vec3{Z: 1})}

	axes := make([]Vec3, 0, 15)
	axes = append(axes, axesA[0], axesA[1], axesA[2], axesB[0], axesB[1], axesB[2])
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			cross := axesA[i].Cross(axesB[j])
			if cross.Len2() > 1e-10 {
				axes = append(axes, cross.Normalize())
			}
		}
	}

	bestDepth := math.Inf(1)
	bestAxis := Vec3{}
	for _, axis := range axes {
		minA, maxA := projectPoints(vertsA, axis)
		minB, maxB := projectPoints(vertsB, axis)
		pushForward := maxA - minB
		pushBackward := maxB - minA
		if pushForward <= 0 || pushBackward <= 0 {
			return Vec3{}, 0, false
		}
		if pushForward <= pushBackward {
			if pushForward < bestDepth {
				bestDepth = pushForward
				bestAxis = axis
			}
			continue
		}
		if pushBackward < bestDepth {
			bestDepth = pushBackward
			bestAxis = axis.Neg()
		}
	}
	return bestAxis, bestDepth, true
}

func worldBoxVertices(c *Collider) []Vec3 {
	center := c.WorldCenter()
	rotation := c.WorldRotation()
	out := make([]Vec3, 0, 8)
	for _, local := range boxVertices(c.halfExtents()) {
		out = append(out, center.Add(rotation.Rotate(local)))
	}
	return out
}

func projectPoints(points []Vec3, axis Vec3) (float64, float64) {
	low := math.Inf(1)
	high := math.Inf(-1)
	for _, p := range points {
		d := axis.Dot(p)
		low = math.Min(low, d)
		high = math.Max(high, d)
	}
	return low, high
}

func deepestPenetration(m ContactManifold) float64 {
	deepest := 0.0
	for i := 0; i < m.PointCount; i++ {
		deepest = maxFloat(deepest, m.Points[i].Penetration)
	}
	return deepest
}

// TestConvexHullMatchesEquivalentBox uses an eight-vertex hull that describes
// the same volume as a box collider and checks the two agree.
func TestConvexHullMatchesEquivalentBox(t *testing.T) {
	half := Vec3{X: 0.5, Y: 0.6, Z: 0.7}
	hullVerts := boxVertices(half)
	rng := rand.New(rand.NewSource(99))
	compared := 0
	for trial := 0; trial < 200; trial++ {
		rotation := randomUnitQuat(rng)
		offset := Vec3{
			X: (rng.Float64() - 0.5) * 2,
			Y: (rng.Float64() - 0.5) * 2,
			Z: (rng.Float64() - 0.5) * 2,
		}
		sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.4})
		box := NewCollider(ColliderConfig{
			Shape: ShapeBox, Rotation: rotation, Offset: offset,
			Width: 1, Height: 1.2, Depth: 1.4,
		})
		hull := NewCollider(ColliderConfig{
			Shape: ShapeConvexHull, Rotation: rotation, Offset: offset,
			Vertices: hullVerts,
		})

		boxManifold, boxOK := Collide(sphere, box)
		hullManifold, hullOK := Collide(sphere, hull)
		if boxOK != hullOK {
			if boxOK && deepestPenetration(boxManifold) > 1e-3 {
				t.Fatalf("trial %d: box collided (depth %v) but the equivalent hull did not",
					trial, deepestPenetration(boxManifold))
			}
			continue
		}
		if !boxOK {
			continue
		}
		boxDepth := deepestPenetration(boxManifold)
		if boxDepth <= 1e-3 {
			continue
		}
		compared++
		if math.Abs(deepestPenetration(hullManifold)-boxDepth) > 3e-3 {
			t.Fatalf("trial %d: hull depth %v disagrees with box depth %v",
				trial, deepestPenetration(hullManifold), boxDepth)
		}
		// EPA's depth converges much faster than its normal: the depth error is
		// quadratic in the facet angle, the normal error only linear. Compare
		// normals up to half the sphere radius of penetration, which is far
		// beyond anything the solver leaves standing. Deeper than that, require
		// only that the reported vector really separates the pair.
		if boxDepth <= 0.2 {
			if hullManifold.Normal.Dot(boxManifold.Normal) < 0.98 {
				t.Fatalf("trial %d: hull normal %+v disagrees with box normal %+v at depth %v",
					trial, hullManifold.Normal, boxManifold.Normal, boxDepth)
			}
			continue
		}
		hullDepth := deepestPenetration(hullManifold)
		pushed := NewCollider(ColliderConfig{
			Shape: ShapeSphere, Radius: 0.4,
			Offset: hullManifold.Normal.Neg().Mul(hullDepth + 1e-3),
		})
		if _, still := Collide(pushed, hull); still {
			t.Fatalf("trial %d: pair still in contact after moving the sphere by the reported depth %v along %+v",
				trial, hullDepth, hullManifold.Normal)
		}
	}
	if compared < 40 {
		t.Fatalf("only %d comparable trials; the hull path is not being exercised", compared)
	}
}

func boxVertices(half Vec3) []Vec3 {
	return []Vec3{
		{X: -half.X, Y: -half.Y, Z: -half.Z},
		{X: half.X, Y: -half.Y, Z: -half.Z},
		{X: -half.X, Y: half.Y, Z: -half.Z},
		{X: half.X, Y: half.Y, Z: -half.Z},
		{X: -half.X, Y: -half.Y, Z: half.Z},
		{X: half.X, Y: -half.Y, Z: half.Z},
		{X: -half.X, Y: half.Y, Z: half.Z},
		{X: half.X, Y: half.Y, Z: half.Z},
	}
}

func TestSupportFunctionsReturnExtremePoints(t *testing.T) {
	cylinder := mustShape(t, NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 2, Height: 4}))
	if got := cylinder.support(Vec3{Y: 1}); !got.Near(Vec3{Y: 2}, 1e-12) {
		t.Fatalf("cylinder support +Y = %+v, want (0,2,0) on the rim plane", got)
	}
	if got := cylinder.support(Vec3{X: 1}); !got.Near(Vec3{X: 2}, 1e-12) {
		t.Fatalf("cylinder support +X = %+v, want (2,0,0)", got)
	}
	if got := cylinder.support(Vec3{X: 1, Y: 1}); !got.Near(Vec3{X: 2, Y: 2}, 1e-12) {
		t.Fatalf("cylinder support +X+Y = %+v, want the top rim corner (2,2,0)", got)
	}

	cone := mustShape(t, NewCollider(ColliderConfig{Shape: ShapeCone, Radius: 2, Height: 4}))
	if got := cone.support(Vec3{Y: 1}); !got.Near(Vec3{Y: 2}, 1e-12) {
		t.Fatalf("cone support +Y = %+v, want the apex (0,2,0)", got)
	}
	if got := cone.support(Vec3{Y: -1}); !got.Near(Vec3{Y: -2}, 1e-12) {
		t.Fatalf("cone support -Y = %+v, want a base point at y=-2", got)
	}
	if got := cone.support(Vec3{X: 1}); !got.Near(Vec3{X: 2, Y: -2}, 1e-12) {
		t.Fatalf("cone support +X = %+v, want the base rim (2,-2,0)", got)
	}

	capsule := mustShape(t, NewCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.5, Height: 2}))
	if got := capsule.support(Vec3{Y: 1}); !got.Near(Vec3{Y: 1.5}, 1e-12) {
		t.Fatalf("capsule support +Y = %+v, want (0,1.5,0)", got)
	}
	// Along +X the whole lateral surface is extreme, so any y is a valid
	// support point. The segment support picks y = 0.
	if got := capsule.support(Vec3{X: 1}); !got.Near(Vec3{X: 0.5}, 1e-12) {
		t.Fatalf("capsule support +X = %+v, want (0.5,0,0)", got)
	}
}

// TestNarrowphaseSurvivesRandomShapePairs hammers every shape combination with
// random poses. It checks that no pair panics and that every reported contact
// carries finite, usable numbers.
func TestNarrowphaseSurvivesRandomShapePairs(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	meshVertices, meshIndices := gridMesh(3, 6)

	build := func(kind int, rotation Quat, offset Vec3) *Collider {
		switch kind {
		case 0:
			return NewCollider(ColliderConfig{Shape: ShapeSphere, Rotation: rotation, Offset: offset, Radius: 0.6})
		case 1:
			return NewCollider(ColliderConfig{Shape: ShapeBox, Rotation: rotation, Offset: offset, Width: 1, Height: 1.2, Depth: 0.8})
		case 2:
			return NewCollider(ColliderConfig{Shape: ShapeCapsule, Rotation: rotation, Offset: offset, Radius: 0.4, Height: 1})
		case 3:
			return NewCollider(ColliderConfig{Shape: ShapeCylinder, Rotation: rotation, Offset: offset, Radius: 0.5, Height: 1.4})
		case 4:
			return NewCollider(ColliderConfig{Shape: ShapeCone, Rotation: rotation, Offset: offset, Radius: 0.6, Height: 1.6})
		case 5:
			return NewCollider(ColliderConfig{Shape: ShapeConvexHull, Rotation: rotation, Offset: offset,
				Vertices: boxVertices(Vec3{X: 0.5, Y: 0.7, Z: 0.4})})
		case 6:
			return NewCollider(ColliderConfig{Shape: ShapePlane, Rotation: rotation, Offset: offset, Normal: Vec3{Y: 1}})
		default:
			return NewCollider(ColliderConfig{Shape: ShapeTriangleMesh, Rotation: rotation, Offset: offset,
				Vertices: meshVertices, Indices: meshIndices})
		}
	}

	const kinds = 8
	contacts := 0
	for trial := 0; trial < 4000; trial++ {
		kindA := trial % kinds
		kindB := (trial / kinds) % kinds
		offset := Vec3{
			X: (rng.Float64() - 0.5) * 4,
			Y: (rng.Float64() - 0.5) * 4,
			Z: (rng.Float64() - 0.5) * 4,
		}
		a := build(kindA, randomUnitQuat(rng), Vec3{})
		b := build(kindB, randomUnitQuat(rng), offset)

		for _, manifold := range CollideAll(a, b, nil) {
			contacts++
			if manifold.PointCount <= 0 || manifold.PointCount > len(manifold.Points) {
				t.Fatalf("trial %d (%v vs %v): PointCount = %d", trial, a.Shape, b.Shape, manifold.PointCount)
			}
			if !isFiniteVec(manifold.Normal) {
				t.Fatalf("trial %d (%v vs %v): normal = %+v", trial, a.Shape, b.Shape, manifold.Normal)
			}
			if length := manifold.Normal.Len(); math.Abs(length-1) > 1e-9 {
				t.Fatalf("trial %d (%v vs %v): normal length = %v", trial, a.Shape, b.Shape, length)
			}
			for i := 0; i < manifold.PointCount; i++ {
				point := manifold.Points[i]
				if !isFiniteVec(point.Point) {
					t.Fatalf("trial %d (%v vs %v): contact point = %+v", trial, a.Shape, b.Shape, point.Point)
				}
				if point.Penetration < 0 || math.IsNaN(point.Penetration) || math.IsInf(point.Penetration, 0) {
					t.Fatalf("trial %d (%v vs %v): penetration = %v", trial, a.Shape, b.Shape, point.Penetration)
				}
			}
		}
	}
	if contacts < 500 {
		t.Fatalf("only %d contacts across 4000 random pairs; the sweep is not exercising the narrowphase", contacts)
	}
}

func isFiniteVec(v Vec3) bool {
	for _, component := range [3]float64{v.X, v.Y, v.Z} {
		if math.IsNaN(component) || math.IsInf(component, 0) {
			return false
		}
	}
	return true
}
