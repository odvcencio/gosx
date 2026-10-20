package physics

import (
	"math"
	"math/rand"
	"testing"
)

// randomDirectionMinGap independently estimates the Minkowski support-gap
// minimum: coarse random global sampling, seeded with hint so a narrow
// minimum is not missed between samples, followed by the package's own
// shrinking-ring local descent (refineSupportMinimum, the same routine
// penetrationBySupportSearch uses) run from both the hint and the best random
// sample. It shares no code with the fast path itself: only the support
// functions are common, and those are pinned by
// TestSupportFunctionsReturnExtremePoints.
//
// This test builds its own oracle call instead of using
// penetrationBySupportSearch directly because a round cylinder side has a
// broad, nearly flat minimum: many nearby directions share almost the same
// gap, which is the case that routine's coarse-sweep starting points are
// least likely to land a refinement ring close enough to. Seeding the
// refinement with a directly-computed hint sidesteps that without weakening
// the check -- refineSupportMinimum descends from whatever direction it is
// given with no knowledge of how that direction was chosen, so a wrong hint
// would still be caught the moment global random sampling (or the descent
// itself) finds something cheaper.
func randomDirectionMinGap(rng *rand.Rand, a, b convexShape, hint Vec3, samples int) float64 {
	bestDir := hint
	best := supportGap(a, b, hint)
	for i := 0; i < samples; i++ {
		dir := Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()}
		if dir.Len2() <= epsilon {
			continue
		}
		dir = dir.Normalize()
		if gap := supportGap(a, b, dir); gap < best {
			best = gap
			bestDir = dir
		}
	}
	if _, refined := refineSupportMinimum(a, b, hint, supportGap(a, b, hint), 0.25); refined < best {
		best = refined
	}
	if _, refined := refineSupportMinimum(a, b, bestDir, best, 0.05); refined < best {
		best = refined
	}
	return best
}

// TestCylinderCylinderMatchesIndependentOracleOnParallelPairs exercises the
// exact domain collideCylinderCylinder's fast path answers: axes within
// cylinderParallelTolerance of parallel (or anti-parallel), with axial
// extents that overlap by more than the rim margin. The independent
// random-direction oracle shares no code with either the fast path or
// GJK/EPA, so agreement here is not the fast path checking its own math.
func TestCylinderCylinderMatchesIndependentOracleOnParallelPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	checked := 0
	for trial := 0; trial < 300; trial++ {
		radiusA := 0.3 + rng.Float64()*1.2
		radiusB := 0.3 + rng.Float64()*1.2
		heightA := 0.6 + rng.Float64()*3
		heightB := 0.6 + rng.Float64()*3

		// A shared axis direction, randomly oriented, with an anti-parallel
		// flip on B half the time: WorldAxis reports the collider's own
		// local +Y, so a cylinder rotated 180 degrees about any perpendicular
		// axis names the same physical line with an opposite-signed axis.
		axisRot := randomUnitQuat(rng)
		rotA := axisRot
		rotB := axisRot
		if trial%2 == 0 {
			rotB = QuatFromAxisAngle(anyPerpendicular(axisRot.Rotate(Vec3{Y: 1})).Normalize(), math.Pi).Mul(axisRot)
		}

		axis := axisRot.Rotate(Vec3{Y: 1}).Normalize()
		perpDir := anyPerpendicular(axis).Normalize()
		// Keep the radial offset inside combined-radius range so most trials
		// overlap; a few trials drift outside to exercise the "no contact"
		// path too.
		radialOffset := (rng.Float64()*2 - 0.5) * (radiusA + radiusB)
		// Keep the axial offset small relative to the shorter half-height, so
		// the axial ranges overlap by more than the rim margin.
		shortHalf := math.Min(heightA, heightB) * 0.5
		axialOffset := (rng.Float64()*2 - 1) * shortHalf * 0.6

		centerA := Vec3{}
		centerB := axis.Mul(axialOffset).Add(perpDir.Mul(radialOffset))

		a := NewCollider(ColliderConfig{Shape: ShapeCylinder, Rotation: rotA, Offset: centerA, Radius: radiusA, Height: heightA})
		b := NewCollider(ColliderConfig{Shape: ShapeCylinder, Rotation: rotB, Offset: centerB, Radius: radiusB, Height: heightB})

		// Confirm this trial actually lands in the fast path's applicability
		// domain, so the test is exercising collideCylinderCylinder's analytic
		// branch and not silently falling back to GJK every time.
		axisA := a.WorldAxis()
		axisB := b.WorldAxis()
		if math.Abs(axisA.Dot(axisB)) < 1-cylinderParallelTolerance {
			t.Fatalf("trial %d: axes not parallel by construction", trial)
		}

		shapeA := mustShape(t, a)
		shapeB := mustShape(t, b)

		manifold, ok := Collide(a, b)
		tolerance := 4*epaGapTolerance(shapeA, shapeB) + 1e-6

		hint := perpDir
		if ok {
			hint = manifold.Normal
		}
		wantDepth := randomDirectionMinGap(rng, shapeA, shapeB, hint, 4000)

		if wantDepth <= tolerance {
			if ok && deepestPenetration(manifold) > wantDepth+tolerance {
				t.Fatalf("trial %d: reported depth %v, oracle %v (grazing/separated band)",
					trial, deepestPenetration(manifold), wantDepth)
			}
			continue
		}
		if !ok {
			t.Fatalf("trial %d: oracle depth %v but no contact reported\nA: %#v\nB: %#v",
				trial, wantDepth, a, b)
		}
		checked++
		// The fast path's two-circle formula is a closed form, exact in the
		// regime its gates admit; randomDirectionMinGap is a numerical search
		// and, for the broad, nearly flat minimum a round cylinder side
		// produces, needs many more than 4000 samples plus refinement passes
		// to land within parts-per-thousand of that exact answer (confirmed
		// directly: on trials this test found hardest for a 4000-sample
		// search, several million brute-force samples still found nothing
		// cheaper than the fast path's own reported depth). A 1% relative
		// tolerance absorbs that gap while staying two orders of magnitude
		// tighter than the >=20% error an actual formula mistake produced
		// during development of this fast path.
		fastTolerance := maxFloat(tolerance, 0.01*wantDepth)
		if math.Abs(deepestPenetration(manifold)-wantDepth) > fastTolerance {
			t.Fatalf("trial %d: depth %v, independent search %v, tolerance %v",
				trial, deepestPenetration(manifold), wantDepth, fastTolerance)
		}
	}
	if checked < 60 {
		t.Fatalf("only %d overlapping trials; the test is not exercising the fast path", checked)
	}
}

// TestCylinderCylinderFastPathMatchesGJKNormalAndPoint pins the fast path's
// normal and contact point, not just depth, against the general GJK/EPA path
// on a direct side-to-side overlap.
func TestCylinderCylinderFastPathMatchesGJKNormalAndPoint(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4})
	b := NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{X: 1.7}, Radius: 1, Height: 4})

	fast, ok := collideCylinderCylinder(a, b)
	if !ok {
		t.Fatal("expected the fast path to report a contact")
	}
	slow, ok := collideConvexGJK(a, b)
	if !ok {
		t.Fatal("expected GJK to report a contact")
	}
	if fast.PointCount != 1 || slow.PointCount != 1 {
		t.Fatalf("expected single-point manifolds, got fast=%d slow=%d", fast.PointCount, slow.PointCount)
	}
	// The fast path's normal is exact; EPA's is only converged to within
	// epaRelativeTolerance, which the codebase's own accuracy tests (see
	// TestEPANormalAccuracyAcrossPenetrationDepths) put at "within one
	// degree." Two degrees leaves headroom without hiding a real mismatch.
	const maxAngle = 2 * math.Pi / 180
	if fast.Normal.Dot(slow.Normal) < math.Cos(maxAngle) {
		t.Fatalf("normal mismatch beyond %v degrees: fast=%+v slow=%+v", maxAngle*180/math.Pi, fast.Normal, slow.Normal)
	}
	if math.Abs(fast.Points[0].Penetration-slow.Points[0].Penetration) > 1e-3 {
		t.Fatalf("depth mismatch: fast=%v slow=%v", fast.Points[0].Penetration, slow.Points[0].Penetration)
	}
}

// TestCylinderCylinderFallsBackWhenAxesTilt checks that a clearly non-parallel
// pair still gets a correct answer through the GJK fallback, so the dispatch
// change in collidePair has not narrowed what collideCylinderCylinder can
// answer, only sped up the common case.
func TestCylinderCylinderFallsBackWhenAxesTilt(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4})
	b := NewCollider(ColliderConfig{
		Shape: ShapeCylinder, Offset: Vec3{X: 1.2, Y: 0.5},
		Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/4),
		Radius:   1, Height: 4,
	})

	fast, fastOK := collideCylinderCylinder(a, b)
	slow, slowOK := collideConvexGJK(a, b)
	if fastOK != slowOK {
		t.Fatalf("fast path and GJK disagree on overlap: fast=%v slow=%v", fastOK, slowOK)
	}
	if !fastOK {
		return
	}
	if math.Abs(deepestPenetration(fast)-deepestPenetration(slow)) > 1e-6 {
		t.Fatalf("expected the fast path to fall through to GJK bit-for-bit: fast depth=%v slow depth=%v",
			deepestPenetration(fast), deepestPenetration(slow))
	}
}

// TestCylinderCylinderFallsBackWhenAxiallyDisjoint checks two coaxial
// cylinders stacked end to end, which is a cap-to-cap contact the fast path's
// side formula cannot answer, so it must defer to GJK.
func TestCylinderCylinderFallsBackWhenAxiallyDisjoint(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2})
	b := NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 1.9}, Radius: 1, Height: 2})

	manifold, ok := Collide(a, b)
	if !ok {
		t.Fatal("expected the stacked cylinders to touch")
	}
	if manifold.Normal.Sub(Vec3{Y: 1}).Len() > 1e-6 {
		t.Fatalf("expected a cap-to-cap normal near +Y, got %+v", manifold.Normal)
	}
	depth := deepestPenetration(manifold)
	if math.Abs(depth-0.1) > 1e-6 {
		t.Fatalf("expected penetration depth 0.1, got %v", depth)
	}
}
