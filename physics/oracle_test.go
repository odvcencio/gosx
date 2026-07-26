package physics

import (
	"math"
	"math/rand"
	"testing"
)

// Independent penetration oracle.
//
// The penetration depth of two convex shapes is, by definition, the smallest
// value of the Minkowski difference's support function over all unit directions:
//
//	depth = min over |n|=1 of ( supportA(n) - supportB(-n) ) . n
//
// and the minimising n is the contact normal pointing from A toward B. The
// oracle below computes that minimum directly with a coarse sweep over the
// sphere followed by a shrinking local search. It shares no code with GJK or
// EPA, so the two cannot agree on a shared mistake. Only the support functions
// are common, and TestSupportFunctionsReturnExtremePoints pins those.

// supportGap returns the Minkowski support distance along a unit direction.
func supportGap(a, b convexShape, dir Vec3) float64 {
	return a.support(dir).Sub(b.support(dir.Neg())).Dot(dir)
}

// penetrationBySupportSearch returns the oracle normal and depth.
//
// The support gap over the sphere can hold several local minima, so the coarse
// sweep keeps its best oracleStarts directions and refines all of them. A single
// start can settle in a shallow valley and then overstate the depth.
func penetrationBySupportSearch(a, b convexShape, coarse int) (Vec3, float64) {
	const oracleStarts = 12
	type candidate struct {
		dir Vec3
		gap float64
	}
	starts := make([]candidate, 0, oracleStarts+1)
	insert := func(dir Vec3, gap float64) {
		at := len(starts)
		for at > 0 && starts[at-1].gap > gap {
			at--
		}
		if at >= oracleStarts {
			return
		}
		starts = append(starts, candidate{})
		copy(starts[at+1:], starts[at:])
		starts[at] = candidate{dir: dir, gap: gap}
		if len(starts) > oracleStarts {
			starts = starts[:oracleStarts]
		}
	}
	for _, dir := range fibonacciDirections(coarse) {
		insert(dir, supportGap(a, b, dir))
	}

	spread := 4 * math.Pi / math.Sqrt(float64(coarse))
	best := math.Inf(1)
	bestDir := Vec3{X: 1}
	for _, start := range starts {
		dir, gap := refineSupportMinimum(a, b, start.dir, start.gap, spread)
		if gap < best {
			best = gap
			bestDir = dir
		}
	}
	return bestDir, best
}

// refineSupportMinimum walks a ring around the current direction and halves the
// ring radius whenever no sample improves. Forty halvings take the angular step
// from the coarse spacing down to machine precision.
func refineSupportMinimum(a, b convexShape, dir Vec3, gap, spread float64) (Vec3, float64) {
	const ringSamples = 24
	for round := 0; round < 40; round++ {
		u, v := orthonormalBasis(dir)
		improved := false
		for i := 0; i < ringSamples; i++ {
			angle := 2 * math.Pi * float64(i) / ringSamples
			candidate := dir.
				Add(u.Mul(spread * math.Cos(angle))).
				Add(v.Mul(spread * math.Sin(angle))).
				Normalize()
			if candidate.Len2() <= epsilon {
				continue
			}
			if probe := supportGap(a, b, candidate); probe < gap {
				gap = probe
				dir = candidate
				improved = true
			}
		}
		if !improved {
			spread *= 0.5
		}
	}
	return dir, gap
}

// TestEPAMatchesIndependentSupportOracle checks every shape pair that routes
// through GJK against the oracle. It covers curved-against-curved contacts,
// which no other test in this package can verify from first principles.
func TestEPAMatchesIndependentSupportOracle(t *testing.T) {
	cases := []struct {
		name string
		a    ColliderConfig
		b    ColliderConfig
	}{
		{
			name: "cylinder side against sphere",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4},
			b:    ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.2}, Radius: 0.5},
		},
		{
			name: "cylinder cap against sphere",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:    ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 1.4}, Radius: 0.5},
		},
		{
			name: "cylinder rim against sphere",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:    ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.2, Y: 1.2}, Radius: 0.5},
		},
		{
			name: "cylinder against box face",
			a:    ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4},
			b:    ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 1.9}, Radius: 0.5, Height: 2},
		},
		{
			name: "cylinder against tilted box",
			a: ColliderConfig{
				Shape: ShapeBox, Width: 2, Height: 2, Depth: 2,
				Rotation: QuatFromAxisAngle(Vec3{X: 1, Z: 1}, 0.6),
			},
			b: ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 1.9}, Radius: 0.5, Height: 2},
		},
		{
			name: "cylinder against cylinder",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4},
			b:    ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{X: 1.7}, Radius: 1, Height: 4},
		},
		{
			name: "crossed cylinders",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 4},
			b: ColliderConfig{
				Shape: ShapeCylinder, Offset: Vec3{Y: 0.9}, Radius: 0.5, Height: 4,
				Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2),
			},
		},
		{
			name: "cylinder against capsule",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4},
			b:    ColliderConfig{Shape: ShapeCapsule, Offset: Vec3{X: 1.4}, Radius: 0.5, Height: 2},
		},
		{
			name: "cone against sphere on the flank",
			a:    ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2},
			b:    ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 0.9, Y: -0.2}, Radius: 0.5},
		},
		{
			name: "cone apex against box",
			a:    ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4},
			b: ColliderConfig{
				Shape: ShapeCone, Offset: Vec3{Y: 1.95}, Radius: 1, Height: 2,
				Rotation: QuatFromAxisAngle(Vec3{X: 1}, math.Pi),
			},
		},
		{
			name: "cone against cone",
			a:    ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2},
			b:    ColliderConfig{Shape: ShapeCone, Offset: Vec3{X: 1.2, Y: 0.3}, Radius: 1, Height: 2},
		},
		{
			name: "cone against cylinder",
			a:    ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:    ColliderConfig{Shape: ShapeCone, Offset: Vec3{Y: 1.4}, Radius: 0.7, Height: 1.4},
		},
		{
			name: "hull against hull",
			a:    ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5})},
			b: ColliderConfig{
				Shape: ShapeConvexHull, Offset: Vec3{Y: 0.9},
				Vertices: boxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5}),
			},
		},
		{
			name: "hull against sphere",
			a:    ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5})},
			b:    ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 0.8, Y: 0.4}, Radius: 0.5},
		},
		{
			name: "tetrahedron hull against cylinder",
			a: ColliderConfig{Shape: ShapeConvexHull, Vertices: []Vec3{
				{X: -1, Z: -1}, {X: 1, Z: -1}, {Z: 1}, {Y: 2},
			}},
			b: ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 2.3}, Radius: 0.6, Height: 1},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			a := NewCollider(testCase.a)
			b := NewCollider(testCase.b)
			if err := a.Err(); err != nil {
				t.Fatalf("collider A invalid: %v", err)
			}
			if err := b.Err(); err != nil {
				t.Fatalf("collider B invalid: %v", err)
			}

			shapeA := mustShape(t, a)
			shapeB := mustShape(t, b)
			wantNormal, wantDepth := penetrationBySupportSearch(shapeA, shapeB, 8000)
			if wantDepth <= 0 {
				t.Fatalf("oracle reports no overlap (depth %v); the case is not exercising EPA", wantDepth)
			}

			manifold, ok := Collide(a, b)
			if !ok {
				t.Fatalf("narrowphase found no contact, oracle depth = %v", wantDepth)
			}
			gotDepth := deepestPenetration(manifold)

			// EPA stops once the remaining gap falls under its tolerance, so
			// allow twice that plus a small absolute margin.
			tolerance := 2*epaGapTolerance(shapeA, shapeB) + 1e-9
			if math.Abs(gotDepth-wantDepth) > tolerance {
				t.Fatalf("depth = %v, oracle = %v, difference %v exceeds tolerance %v",
					gotDepth, wantDepth, math.Abs(gotDepth-wantDepth), tolerance)
			}
			if manifold.Normal.Dot(wantNormal) < 0.999 {
				t.Fatalf("normal = %+v, oracle normal = %+v (dot %v)",
					manifold.Normal, wantNormal, manifold.Normal.Dot(wantNormal))
			}

			// The reported depth must be a real minimum translation: pushing B
			// by it separates the pair, and pushing by less does not.
			pushed := testCase.b
			pushed.Offset = pushed.Offset.Add(manifold.Normal.Mul(gotDepth + tolerance + 1e-6))
			if _, still := Collide(a, NewCollider(pushed)); still {
				t.Fatalf("pair still in contact after moving B by the reported depth %v", gotDepth)
			}
			short := testCase.b
			short.Offset = short.Offset.Add(manifold.Normal.Mul(gotDepth * 0.5))
			if _, still := Collide(a, NewCollider(short)); !still {
				t.Fatalf("pair separated after moving B by only half the reported depth %v", gotDepth)
			}
		})
	}
}

// TestEPAMatchesOracleOnRandomCurvedPairs runs the oracle over random poses of
// the shapes that have no analytic narrowphase. Random poses reach the rim and
// apex features that hand-written cases tend to miss.
func TestEPAMatchesOracleOnRandomCurvedPairs(t *testing.T) {
	rng := rand.New(rand.NewSource(31337))
	builders := []func(Quat, Vec3) ColliderConfig{
		func(r Quat, o Vec3) ColliderConfig {
			return ColliderConfig{Shape: ShapeCylinder, Rotation: r, Offset: o, Radius: 0.6, Height: 1.4}
		},
		func(r Quat, o Vec3) ColliderConfig {
			return ColliderConfig{Shape: ShapeCone, Rotation: r, Offset: o, Radius: 0.7, Height: 1.6}
		},
		func(r Quat, o Vec3) ColliderConfig {
			return ColliderConfig{Shape: ShapeSphere, Rotation: r, Offset: o, Radius: 0.5}
		},
		func(r Quat, o Vec3) ColliderConfig {
			return ColliderConfig{Shape: ShapeCapsule, Rotation: r, Offset: o, Radius: 0.35, Height: 0.9}
		},
		func(r Quat, o Vec3) ColliderConfig {
			return ColliderConfig{Shape: ShapeBox, Rotation: r, Offset: o, Width: 1, Height: 0.8, Depth: 1.2}
		},
		func(r Quat, o Vec3) ColliderConfig {
			return ColliderConfig{
				Shape: ShapeConvexHull, Rotation: r, Offset: o,
				Vertices: boxVertices(Vec3{X: 0.45, Y: 0.6, Z: 0.35}),
			}
		},
	}

	checked := 0
	for trial := 0; trial < 240; trial++ {
		buildA := builders[trial%len(builders)]
		buildB := builders[(trial/len(builders))%len(builders)]
		configA := buildA(randomUnitQuat(rng), Vec3{})
		configB := buildB(randomUnitQuat(rng), Vec3{
			X: (rng.Float64() - 0.5) * 2.2,
			Y: (rng.Float64() - 0.5) * 2.2,
			Z: (rng.Float64() - 0.5) * 2.2,
		})
		// Skip the pairs that have their own analytic routine; the oracle has
		// nothing to add there and the analytic tolerance differs.
		if hasAnalyticNarrowphase(configA.Shape, configB.Shape) {
			continue
		}

		a := NewCollider(configA)
		b := NewCollider(configB)
		shapeA := mustShape(t, a)
		shapeB := mustShape(t, b)

		_, wantDepth := penetrationBySupportSearch(shapeA, shapeB, 4000)
		manifold, ok := Collide(a, b)
		tolerance := 4*epaGapTolerance(shapeA, shapeB) + 1e-6

		if wantDepth <= tolerance {
			// Grazing or separated. Only require that any reported contact is
			// shallow, because the two paths disagree inside the tolerance band.
			if ok && deepestPenetration(manifold) > wantDepth+tolerance {
				t.Fatalf("trial %d (%v vs %v): reported depth %v, oracle %v",
					trial, configA.Shape, configB.Shape, deepestPenetration(manifold), wantDepth)
			}
			continue
		}
		if !ok {
			t.Fatalf("trial %d: oracle depth %v but no contact reported\nA: %#v\nB: %#v",
				trial, wantDepth, configA, configB)
		}
		checked++
		if math.Abs(deepestPenetration(manifold)-wantDepth) > tolerance {
			t.Fatalf("trial %d: depth %v, oracle %v, tolerance %v\nA: %#v\nB: %#v",
				trial, deepestPenetration(manifold), wantDepth, tolerance, configA, configB)
		}
	}
	if checked < 40 {
		t.Fatalf("only %d overlapping trials; the test is not exercising EPA", checked)
	}
}

// hasAnalyticNarrowphase reports whether collidePair routes the pair to a
// hand-written routine instead of to GJK.
func hasAnalyticNarrowphase(a, b ColliderShape) bool {
	pair := func(x, y ColliderShape) bool {
		return (a == x && b == y) || (a == y && b == x)
	}
	return pair(ShapeSphere, ShapeSphere) ||
		pair(ShapeSphere, ShapeBox) ||
		pair(ShapeSphere, ShapeCapsule) ||
		pair(ShapeBox, ShapeBox) ||
		pair(ShapeCapsule, ShapeCapsule)
}

// TestEPANormalAccuracyAcrossPenetrationDepths pins the accuracy that the rest
// of the engine relies on. The contact feature here is a rounded hull corner,
// which is the slowest case for the normal to converge.
//
// Depth converges quadratically in the facet angle; the normal only linearly.
// The measured guarantee is a normal within one degree and a depth within 1e-4
// for penetrations up to half the sphere radius. The solver's position pass
// keeps steady-state penetration near positionSlop, which is three orders of
// magnitude shallower than that.
func TestEPANormalAccuracyAcrossPenetrationDepths(t *testing.T) {
	half := Vec3{X: 0.5, Y: 0.6, Z: 0.7}
	diagonal := Vec3{X: 1, Y: 1, Z: 1}.Normalize()
	corner := Vec3{X: half.X, Y: half.Y, Z: half.Z}
	const radius = 0.4

	for _, depth := range []float64{0.001, 0.005, 0.02, 0.08, 0.2} {
		center := corner.Add(diagonal.Mul(radius - depth))
		hull := NewCollider(ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(half)})
		sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: center, Radius: radius})

		manifold, ok := Collide(hull, sphere)
		if !ok {
			t.Fatalf("depth %v: no contact reported", depth)
		}
		got := deepestPenetration(manifold)
		if math.Abs(got-depth) > 1e-4 {
			t.Fatalf("depth %v: reported %v, error %v exceeds 1e-4", depth, got, math.Abs(got-depth))
		}
		// The true normal at a rounded corner points along the corner diagonal.
		if angle := math.Acos(math.Min(1, manifold.Normal.Dot(diagonal))) * 180 / math.Pi; angle > 1 {
			t.Fatalf("depth %v: normal %+v is %.3f degrees off the corner diagonal", depth, manifold.Normal, angle)
		}
	}
}

// TestEPAReportsRealDepthForDeepHullCylinderOverlap is a regression test. This
// pose used to make the expanding polytope break and report a depth of exactly
// zero, which the solver accepts as a contact and then corrects by nothing.
func TestEPAReportsRealDepthForDeepHullCylinderOverlap(t *testing.T) {
	hull := NewCollider(ColliderConfig{
		Shape:    ShapeConvexHull,
		Rotation: Quat{X: 0.16342987615306065, Y: -0.6007282588088927, Z: -0.1671327005334652, W: 0.7645148102302678},
		Vertices: boxVertices(Vec3{X: 0.45, Y: 0.6, Z: 0.35}),
	})
	cylinder := NewCollider(ColliderConfig{
		Shape:    ShapeCylinder,
		Offset:   Vec3{X: -0.5960293182063598, Y: -0.8544812946397902, Z: -0.5209598076617685},
		Rotation: Quat{X: 0.7655625128648598, Y: -0.456207171894925, Z: 0.28749552202947987, W: -0.3509065117957669},
		Radius:   0.6, Height: 1.4,
	})

	manifold, ok := Collide(hull, cylinder)
	if !ok {
		t.Fatal("expected a contact for a pair that overlaps by 0.46")
	}
	depth := deepestPenetration(manifold)
	if depth <= 0 {
		t.Fatalf("penetration = %v; a contact with no depth resolves nothing", depth)
	}
	shapeA := mustShape(t, hull)
	shapeB := mustShape(t, cylinder)
	_, want := penetrationBySupportSearch(shapeA, shapeB, 8000)
	if math.Abs(depth-want) > 2*epaGapTolerance(shapeA, shapeB)+1e-9 {
		t.Fatalf("penetration = %v, oracle = %v", depth, want)
	}
}
