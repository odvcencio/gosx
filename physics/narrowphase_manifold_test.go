package physics

import (
	"math"
	"testing"
)

// Angular contact impulses make a single point manifold inadequate: one point
// lets a flat face rock about it. These tests check that the GJK path now
// returns a contact patch, that the patch still agrees with EPA on depth, and
// that a curved feature still returns the one point it really has.

func manifoldPointCount(t *testing.T, a, b *Collider) (ContactManifold, int) {
	t.Helper()
	manifold, ok := Collide(a, b)
	if !ok {
		t.Fatalf("expected %v and %v to collide", a.Shape, b.Shape)
	}
	return manifold, manifold.PointCount
}

func TestConvexPairsReportAContactPatch(t *testing.T) {
	hullBox := func(half Vec3, offset Vec3) *Collider {
		return NewCollider(ColliderConfig{
			Shape:  ShapeConvexHull,
			Offset: offset,
			Vertices: []Vec3{
				{X: -half.X, Y: -half.Y, Z: -half.Z}, {X: half.X, Y: -half.Y, Z: -half.Z},
				{X: -half.X, Y: half.Y, Z: -half.Z}, {X: half.X, Y: half.Y, Z: -half.Z},
				{X: -half.X, Y: -half.Y, Z: half.Z}, {X: half.X, Y: -half.Y, Z: half.Z},
				{X: -half.X, Y: half.Y, Z: half.Z}, {X: half.X, Y: half.Y, Z: half.Z},
			},
		})
	}

	cases := []struct {
		name string
		a    *Collider
		b    *Collider
		want int
	}{
		{
			name: "cylinder cap on a box face",
			a:    NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
			b:    NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 1.45}, Radius: 0.5, Height: 1}),
			want: 4,
		},
		{
			name: "cylinder cap on a cylinder cap",
			a:    NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1}),
			b:    NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: 0.95}, Radius: 0.5, Height: 1}),
			want: 4,
		},
		{
			name: "hull face on a box face",
			a:    NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
			b:    hullBox(Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Vec3{Y: 1.45}),
			want: 4,
		},
		{
			name: "hull face on a hull face",
			a:    hullBox(Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Vec3{}),
			b:    hullBox(Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Vec3{Y: 0.95}),
			want: 4,
		},
		{
			// The cone apex sits on local +Y, so the default pose puts the base
			// disc down onto the box.
			name: "cone base on a box face",
			a:    NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
			b:    NewCollider(ColliderConfig{Shape: ShapeCone, Offset: Vec3{Y: 1.45}, Radius: 0.5, Height: 1}),
			want: 4,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			manifold, count := manifoldPointCount(t, testCase.a, testCase.b)
			if count < testCase.want {
				t.Fatalf("manifold has %d points, want at least %d", count, testCase.want)
			}
			// The patch must spread. Two points on the same spot carry the load
			// of one and let the body rock.
			spread := 0.0
			for i := 0; i < count; i++ {
				for j := i + 1; j < count; j++ {
					spread = math.Max(spread, manifold.Points[i].Point.Distance(manifold.Points[j].Point))
				}
			}
			if spread < 0.2 {
				t.Fatalf("contact patch spans only %v; the points are nearly coincident", spread)
			}
		})
	}
}

func TestCurvedFeaturesKeepASinglePoint(t *testing.T) {
	cases := []struct {
		name string
		a    *Collider
		b    *Collider
	}{
		{
			name: "sphere on a box",
			a:    NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
			b:    NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 1.45}, Radius: 0.5}),
		},
		{
			name: "sphere on a cylinder",
			a:    NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2}),
			b:    NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.4}, Radius: 0.5}),
		},
		{
			// Turn the cone over so its apex, a single point, meets the box.
			name: "cone apex on a box",
			a:    NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4}),
			b: NewCollider(ColliderConfig{
				Shape: ShapeCone, Offset: Vec3{Y: 1.45}, Radius: 0.5, Height: 1,
				Rotation: QuatFromAxisAngle(Vec3{X: 1}, math.Pi),
			}),
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, count := manifoldPointCount(t, testCase.a, testCase.b)
			if count != 1 {
				t.Fatalf("a curved feature touches at one point, got %d", count)
			}
		})
	}
}

// TestExpandedPatchAgreesWithEPADepth guards the acceptance gate. Whatever the
// clipping does, the deepest point of the manifold must still report the
// penetration EPA found, because EPA is what two independent oracles validate.
func TestExpandedPatchAgreesWithEPADepth(t *testing.T) {
	for _, gap := range []float64{0.99, 0.95, 0.9, 0.8, 0.6} {
		a := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})
		b := NewCollider(ColliderConfig{Shape: ShapeCylinder, Offset: Vec3{Y: gap}, Radius: 0.5, Height: 1})

		shapeA, _ := newConvexShape(a)
		shapeB, _ := newConvexShape(b)
		simplex, overlap := gjkOverlap(shapeA, shapeB)
		if !overlap {
			t.Fatalf("gap %v: shapes must overlap", gap)
		}
		want, ok := epaPenetration(shapeA, shapeB, simplex)
		if !ok {
			t.Fatalf("gap %v: EPA failed", gap)
		}

		manifold, _ := manifoldPointCount(t, a, b)
		deepest := 0.0
		for i := 0; i < manifold.PointCount; i++ {
			deepest = math.Max(deepest, manifold.Points[i].Penetration)
		}
		if math.Abs(deepest-want.Depth) > 1e-9 {
			t.Fatalf("gap %v: manifold depth %v, EPA depth %v", gap, deepest, want.Depth)
		}
	}
}

// TestCylinderRestsUprightOnAStaticBox is the behaviour the patch exists for. A
// single point manifold plus angular impulses lets the cylinder rock over; a
// patch holds it.
func TestCylinderRestsUprightOnAStaticBox(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 120.0, SolverIterations: 12})
	ground := world.AddBody(BodyConfig{Mass: 0})
	ground.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 10, Height: 2, Depth: 10})

	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 1.6}, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})

	for i := 0; i < 2400; i++ {
		world.StepFixed()
	}

	if body.Position.Y < 1.49 || body.Position.Y > 1.51 {
		t.Fatalf("cylinder should rest near y=1.5, got %v", body.Position.Y)
	}
	tilt := math.Hypot(math.Hypot(body.Rotation.X, body.Rotation.Y), body.Rotation.Z)
	if tilt > 0.02 {
		t.Fatalf("cylinder tipped over, rotation vector part = %v", tilt)
	}
	if body.AngularVelocity.Len() > 0.02 {
		t.Fatalf("cylinder should be still, angular velocity = %+v", body.AngularVelocity)
	}
}

// TestBoxBoxPatchNeverRepeatsAPoint pins the patch reduction. Clipping a face
// against an identical face lands points on the same corner, and a duplicate
// gives that corner double the grip.
func TestBoxBoxPatchNeverRepeatsAPoint(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1})
	b := NewCollider(ColliderConfig{Shape: ShapeBox, Offset: Vec3{Y: 0.999}, Width: 1, Height: 1, Depth: 1})

	manifold, count := manifoldPointCount(t, a, b)
	if count != 4 {
		t.Fatalf("two aligned box faces meet at four corners, got %d", count)
	}
	for i := 0; i < count; i++ {
		for j := i + 1; j < count; j++ {
			if manifold.Points[i].Point.Distance(manifold.Points[j].Point) < 0.1 {
				t.Fatalf("points %d and %d are duplicates: %+v and %+v",
					i, j, manifold.Points[i].Point, manifold.Points[j].Point)
			}
		}
	}
}
