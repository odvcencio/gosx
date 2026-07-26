package physics

import (
	"math"
	"math/rand"
	"testing"
)

// fibonacciDirections returns count unit vectors spread evenly over the sphere.
func fibonacciDirections(count int) []Vec3 {
	golden := math.Pi * (3 - math.Sqrt(5))
	out := make([]Vec3, 0, count)
	for i := 0; i < count; i++ {
		y := 1 - 2*(float64(i)+0.5)/float64(count)
		radius := math.Sqrt(math.Max(0, 1-y*y))
		theta := golden * float64(i)
		out = append(out, Vec3{X: math.Cos(theta) * radius, Y: y, Z: math.Sin(theta) * radius})
	}
	return out
}

// shapeSample names one collider configuration used by the bound and grazing
// tests. All eight declared shapes appear here.
type shapeSample struct {
	name   string
	config ColliderConfig
}

func allShapeSamples() []shapeSample {
	meshVertices, meshIndices := gridMesh(3, 4)
	return []shapeSample{
		{name: "box", config: ColliderConfig{Shape: ShapeBox, Width: 1.2, Height: 0.8, Depth: 1.6}},
		{name: "sphere", config: ColliderConfig{Shape: ShapeSphere, Radius: 0.7}},
		{name: "capsule", config: ColliderConfig{Shape: ShapeCapsule, Radius: 0.4, Height: 1.1}},
		{name: "cylinder", config: ColliderConfig{Shape: ShapeCylinder, Radius: 0.6, Height: 1.3}},
		{name: "cone", config: ColliderConfig{Shape: ShapeCone, Radius: 0.55, Height: 1.7}},
		{name: "convexhull", config: ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.5, Y: 0.9, Z: 0.4})}},
		{name: "trianglemesh", config: ColliderConfig{Shape: ShapeTriangleMesh, Vertices: meshVertices, Indices: meshIndices}},
	}
}

// TestColliderAABBNeverUnderstatesGeometry is the guard against the worst kind
// of bound bug: a box that is too small makes the broadphase drop real
// contacts, and nothing in the simulation reports it.
//
// The support function is the authority on where a shape reaches. Every support
// point, in every direction, must lie inside the reported bounds.
func TestColliderAABBNeverUnderstatesGeometry(t *testing.T) {
	rng := rand.New(rand.NewSource(1234))
	directions := fibonacciDirections(2000)
	// The axis directions are the tight corners of any AABB, so test them too.
	directions = append(directions,
		Vec3{X: 1}, Vec3{X: -1}, Vec3{Y: 1}, Vec3{Y: -1}, Vec3{Z: 1}, Vec3{Z: -1})

	for _, sample := range allShapeSamples() {
		t.Run(sample.name, func(t *testing.T) {
			for trial := 0; trial < 25; trial++ {
				config := sample.config
				config.Rotation = randomUnitQuat(rng)
				config.Offset = Vec3{
					X: (rng.Float64() - 0.5) * 8,
					Y: (rng.Float64() - 0.5) * 8,
					Z: (rng.Float64() - 0.5) * 8,
				}
				collider := NewCollider(config)
				if err := collider.Err(); err != nil {
					t.Fatalf("collider is invalid: %v", err)
				}
				box := collider.AABB()
				if !box.IsFinite() {
					t.Fatalf("trial %d: AABB = %+v, want finite", trial, box)
				}

				// A mesh has no support function, so check its vertices instead.
				if collider.Shape == ShapeTriangleMesh {
					center := collider.WorldCenter()
					basis := mat3FromQuat(collider.WorldRotation())
					for index, tri := range collider.mesh.tris {
						for corner, local := range [3]Vec3{tri.a, tri.b, tri.c} {
							world := center.Add(basis.mul(local))
							if !box.Expand(1e-9).Contains(world) {
								t.Fatalf("trial %d: triangle %d corner %d at %+v escapes AABB %+v",
									trial, index, corner, world, box)
							}
						}
					}
					continue
				}

				shape, ok := newConvexShape(collider)
				if !ok {
					t.Fatalf("trial %d: newConvexShape failed", trial)
				}
				padded := box.Expand(1e-9)
				for _, dir := range directions {
					point := shape.support(dir)
					if !padded.Contains(point) {
						t.Fatalf("trial %d: support point %+v along %+v escapes AABB %+v",
							trial, point, dir, box)
					}
				}
			}
		})
	}
}

// TestAABBOverlapIsNecessaryForContact closes the loop from the bound to the
// broadphase. The grid only tests pairs whose bounds overlap, so any contact the
// narrowphase can report must also have overlapping bounds. A failure here means
// the broadphase silently drops real contacts.
func TestAABBOverlapIsNecessaryForContact(t *testing.T) {
	rng := rand.New(rand.NewSource(555))
	samples := allShapeSamples()
	contacts := 0
	for trial := 0; trial < 6000; trial++ {
		sampleA := samples[trial%len(samples)]
		sampleB := samples[(trial/len(samples))%len(samples)]

		configA := sampleA.config
		configA.Rotation = randomUnitQuat(rng)
		configB := sampleB.config
		configB.Rotation = randomUnitQuat(rng)
		configB.Offset = Vec3{
			X: (rng.Float64() - 0.5) * 5,
			Y: (rng.Float64() - 0.5) * 5,
			Z: (rng.Float64() - 0.5) * 5,
		}

		a := NewCollider(configA)
		b := NewCollider(configB)
		manifolds := CollideAll(a, b, nil)
		if len(manifolds) == 0 {
			continue
		}
		contacts++
		if !a.AABB().Overlaps(b.AABB()) {
			t.Fatalf("trial %d (%s vs %s): contact reported but bounds %+v and %+v do not overlap",
				trial, sampleA.name, sampleB.name, a.AABB(), b.AABB())
		}
		for _, manifold := range manifolds {
			for i := 0; i < manifold.PointCount; i++ {
				point := manifold.Points[i].Point
				// The contact point must lie in the union of the two bounds,
				// widened by the penetration depth.
				slack := manifold.Points[i].Penetration + 1e-6
				union := a.AABB().Union(b.AABB()).Expand(slack)
				if !union.Contains(point) {
					t.Fatalf("trial %d (%s vs %s): contact point %+v lies outside the widened bounds %+v",
						trial, sampleA.name, sampleB.name, point, union)
				}
			}
		}
	}
	if contacts < 500 {
		t.Fatalf("only %d contacts across 6000 random pairs; the test is not exercising the narrowphase", contacts)
	}
}

// TestGrazingContactsAreNotMissed places every pair at a barely-overlapping pose
// and then at a clearly separated pose. A grazing overlap must report a shallow
// contact, and a clear gap must report none.
func TestGrazingContactsAreNotMissed(t *testing.T) {
	const (
		grazeOverlap = 1e-4
		clearGap     = 1e-2
	)

	// Each case places B along +Y above A. touchY is the centre height at which
	// the two surfaces exactly touch.
	cases := []struct {
		name   string
		a      ColliderConfig
		b      ColliderConfig
		touchY float64
	}{
		{
			name:   "cylinder cap on plane",
			a:      ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			b:      ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2},
			touchY: 1,
		},
		{
			name:   "cylinder side on plane",
			a:      ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			b:      ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2, Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2)},
			touchY: 0.5,
		},
		{
			name:   "cone base on plane",
			a:      ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			b:      ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2},
			touchY: 1,
		},
		{
			name:   "cone apex on plane",
			a:      ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			b:      ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2, Rotation: QuatFromAxisAngle(Vec3{X: 1}, math.Pi)},
			touchY: 1,
		},
		{
			name:   "hull on plane",
			a:      ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			b:      ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5})},
			touchY: 0.5,
		},
		{
			name:   "mesh on plane",
			a:      ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			b:      ColliderConfig{Shape: ShapeTriangleMesh, Vertices: mustGridVertices(), Indices: mustGridIndices()},
			touchY: 0,
		},
		{
			name:   "sphere on cylinder cap",
			a:      ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:      ColliderConfig{Shape: ShapeSphere, Radius: 0.5},
			touchY: 1.5,
		},
		{
			name:   "box on cylinder cap",
			a:      ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:      ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1},
			touchY: 1.5,
		},
		{
			name:   "capsule on cylinder cap",
			a:      ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:      ColliderConfig{Shape: ShapeCapsule, Radius: 0.4, Height: 1},
			touchY: 1.9,
		},
		{
			name:   "cylinder on cylinder cap",
			a:      ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:      ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1},
			touchY: 1.5,
		},
		{
			name:   "cone on cylinder cap",
			a:      ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:      ColliderConfig{Shape: ShapeCone, Radius: 0.5, Height: 1},
			touchY: 1.5,
		},
		{
			name:   "hull on cylinder cap",
			a:      ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2},
			b:      ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.4, Y: 0.4, Z: 0.4})},
			touchY: 1.4,
		},
		{
			name:   "sphere on cone base rim",
			a:      ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2, Rotation: QuatFromAxisAngle(Vec3{X: 1}, math.Pi)},
			b:      ColliderConfig{Shape: ShapeSphere, Radius: 0.5},
			touchY: 1.5,
		},
		{
			name:   "sphere on mesh floor",
			a:      ColliderConfig{Shape: ShapeTriangleMesh, Vertices: mustGridVertices(), Indices: mustGridIndices()},
			b:      ColliderConfig{Shape: ShapeSphere, Radius: 0.5},
			touchY: 0.5,
		},
		{
			name:   "box on mesh floor",
			a:      ColliderConfig{Shape: ShapeTriangleMesh, Vertices: mustGridVertices(), Indices: mustGridIndices()},
			b:      ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1},
			touchY: 0.5,
		},
		{
			name:   "cylinder on mesh floor",
			a:      ColliderConfig{Shape: ShapeTriangleMesh, Vertices: mustGridVertices(), Indices: mustGridIndices()},
			b:      ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1},
			touchY: 0.5,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			a := NewCollider(testCase.a)
			if err := a.Err(); err != nil {
				t.Fatalf("collider A is invalid: %v", err)
			}

			grazing := testCase.b
			grazing.Offset = grazing.Offset.Add(Vec3{Y: testCase.touchY - grazeOverlap})
			overlapping := NewCollider(grazing)
			if err := overlapping.Err(); err != nil {
				t.Fatalf("collider B is invalid: %v", err)
			}
			manifolds := CollideAll(a, overlapping, nil)
			if len(manifolds) == 0 {
				t.Fatalf("grazing overlap of %v was missed; a missed grazing contact lets a body sink", grazeOverlap)
			}
			deepest := 0.0
			for _, manifold := range manifolds {
				for i := 0; i < manifold.PointCount; i++ {
					deepest = maxFloat(deepest, manifold.Points[i].Penetration)
				}
			}
			if deepest > 10*grazeOverlap {
				t.Fatalf("grazing penetration = %v, want no more than %v", deepest, 10*grazeOverlap)
			}

			separated := testCase.b
			separated.Offset = separated.Offset.Add(Vec3{Y: testCase.touchY + clearGap})
			clear := NewCollider(separated)
			if got := CollideAll(a, clear, nil); len(got) != 0 {
				t.Fatalf("a %v gap still reported %d contacts with penetration %v",
					clearGap, len(got), got[0].Points[0].Penetration)
			}
		})
	}
}

func mustGridVertices() []Vec3 {
	vertices, _ := gridMesh(4, 8)
	return vertices
}

func mustGridIndices() []int {
	_, indices := gridMesh(4, 8)
	return indices
}

// TestSweptBoundNeverUnderstatesTheSweep guards the CCD candidate box the same
// way. Every point the swept sphere can reach must lie inside the query box, or
// the swept pass drops a real hit and a fast body tunnels.
func TestSweptBoundNeverUnderstatesTheSweep(t *testing.T) {
	rng := rand.New(rand.NewSource(808))
	for trial := 0; trial < 500; trial++ {
		origin := Vec3{
			X: (rng.Float64() - 0.5) * 20,
			Y: (rng.Float64() - 0.5) * 20,
			Z: (rng.Float64() - 0.5) * 20,
		}
		radius := 0.01 + rng.Float64()*2
		direction := Vec3{
			X: rng.Float64() - 0.5,
			Y: rng.Float64() - 0.5,
			Z: rng.Float64() - 0.5,
		}.Normalize()
		if direction.Len2() <= epsilon {
			continue
		}
		distance := rng.Float64() * 30

		box := sweptSphereBounds(origin, radius, direction, distance)
		if !box.IsFinite() {
			t.Fatalf("trial %d: swept bound %+v is not finite", trial, box)
		}
		padded := box.Expand(1e-9)
		for step := 0; step <= 40; step++ {
			t95 := distance * float64(step) / 40
			center := origin.Add(direction.Mul(t95))
			for _, dir := range fibonacciDirections(64) {
				surface := center.Add(dir.Mul(radius))
				if !padded.Contains(surface) {
					t.Fatalf("trial %d: swept sphere surface point %+v escapes bound %+v",
						trial, surface, box)
				}
			}
		}
	}
}

// TestCCDGrazingSweepFindsTheStaticTarget checks that a sweep which barely
// clips a static box is still reported. This is where an understated candidate
// box would show up as a tunnelling body.
func TestCCDGrazingSweepFindsTheStaticTarget(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0, BroadPhaseCell: 2})
	// A thin static wall standing at x=0, and a bullet flying along +X that
	// grazes its top edge.
	world.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.2, Height: 2, Depth: 4})
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: -10}})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.25})

	world.broadphase.Rebuild(world.colliders)
	world.broadphaseStep = world.steps

	// Sweep at the height where the swept sphere just reaches the wall top at
	// y=1. Anything below y = 1 + radius must hit.
	for _, height := range []float64{0, 0.5, 0.99, 1.0, 1.2, 1.249} {
		body.Position = Vec3{X: -10, Y: height}
		if _, ok := world.sweepBody(body, Vec3{X: 20}); !ok {
			t.Fatalf("sweep at height %v missed the wall; the candidate box understates the sweep", height)
		}
	}
	for _, height := range []float64{1.3, 2, 5} {
		body.Position = Vec3{X: -10, Y: height}
		if _, ok := world.sweepBody(body, Vec3{X: 20}); ok {
			t.Fatalf("sweep at height %v hit the wall, which tops out at y=1", height)
		}
	}
}
