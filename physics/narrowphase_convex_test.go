package physics

import (
	"math"
	"testing"
)

// --- Cylinder ---------------------------------------------------------------

func TestCollideCylinderPlaneRestingOnCap(t *testing.T) {
	// Cylinder radius 0.5, height 2, centre at y=0.9. The bottom cap sits at
	// y=-0.1, so it dips 0.1 below the +Y plane at the origin.
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2, Offset: Vec3{Y: 0.9}})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(cylinder, plane)
	if !ok {
		t.Fatal("expected a cylinder-plane contact")
	}
	if !contact.Normal.Near(Vec3{Y: -1}, 1e-12) {
		t.Fatalf("normal = %+v, want -Y (cylinder toward plane)", contact.Normal)
	}
	if contact.PointCount != 4 {
		t.Fatalf("PointCount = %d, want 4 rim points for a flat cap", contact.PointCount)
	}
	for i := 0; i < contact.PointCount; i++ {
		point := contact.Points[i]
		if math.Abs(point.Penetration-0.1) > 1e-12 {
			t.Fatalf("point[%d] penetration = %v, want 0.1", i, point.Penetration)
		}
		if math.Abs(point.Point.Y-(-0.1)) > 1e-12 {
			t.Fatalf("point[%d] Y = %v, want -0.1 on the cap plane", i, point.Point.Y)
		}
		radial := math.Hypot(point.Point.X, point.Point.Z)
		if math.Abs(radial-0.5) > 1e-12 {
			t.Fatalf("point[%d] radial distance = %v, want 0.5 on the rim", i, radial)
		}
	}
}

func TestCollideCylinderPlaneRestingOnSide(t *testing.T) {
	// Rotate the cylinder 90 degrees about Z so it lies on its side. The lowest
	// line of the lateral surface then touches the plane along two rim points.
	rotation := QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2)
	cylinder := NewCollider(ColliderConfig{
		Shape: ShapeCylinder, Radius: 0.5, Height: 2,
		Offset: Vec3{Y: 0.45}, Rotation: rotation,
	})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(cylinder, plane)
	if !ok {
		t.Fatal("expected a cylinder-plane side contact")
	}
	if contact.PointCount != 2 {
		t.Fatalf("PointCount = %d, want 2 contacts at the two cap rims", contact.PointCount)
	}
	for i := 0; i < contact.PointCount; i++ {
		if got := contact.Points[i].Penetration; math.Abs(got-0.05) > 1e-9 {
			t.Fatalf("point[%d] penetration = %v, want 0.05", i, got)
		}
	}
}

func TestCollideCylinderPlaneSeparated(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2, Offset: Vec3{Y: 1.5}})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	if _, ok := Collide(cylinder, plane); ok {
		t.Fatal("expected no contact for a cylinder 0.5 above the plane")
	}
}

func TestCollideCylinderSphereOnSide(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Z: 1.3}, Radius: 0.5})

	contact, ok := Collide(cylinder, sphere)
	if !ok {
		t.Fatal("expected a cylinder-sphere contact")
	}
	if contact.Normal.Dot(Vec3{Z: 1}) < 0.9999 {
		t.Fatalf("normal = %+v, want +Z", contact.Normal)
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.2) > 1e-4 {
		t.Fatalf("penetration = %v, want 0.2", got)
	}
}

func TestCollideCylinderSphereOnCap(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 1.4}, Radius: 0.5})

	contact, ok := Collide(cylinder, sphere)
	if !ok {
		t.Fatal("expected a cylinder cap contact")
	}
	if contact.Normal.Dot(Vec3{Y: 1}) < 0.9999 {
		t.Fatalf("normal = %+v, want +Y", contact.Normal)
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.1) > 1e-4 {
		t.Fatalf("penetration = %v, want 0.1", got)
	}
	// The cap sits at y=1 and the sphere surface at y=0.9, so the midpoint is
	// y=0.95.
	if got := contact.Points[0].Point.Y; math.Abs(got-0.95) > 1e-3 {
		t.Fatalf("contact Y = %v, want 0.95", got)
	}
}

func TestCollideCylinderSphereSeparated(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 2})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 3}, Radius: 0.5})
	if _, ok := Collide(cylinder, sphere); ok {
		t.Fatal("expected no contact for a sphere 1.5 units clear of the cylinder")
	}
}

func TestCollideCylinderBoxFaceContact(t *testing.T) {
	box := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4})
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2, Offset: Vec3{Y: 1.9}})

	contact, ok := Collide(box, cylinder)
	if !ok {
		t.Fatal("expected a box-cylinder contact")
	}
	if !contact.Normal.Near(Vec3{Y: 1}, 1e-9) {
		t.Fatalf("normal = %+v, want +Y (box toward cylinder)", contact.Normal)
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.1) > 1e-9 {
		t.Fatalf("penetration = %v, want 0.1", got)
	}
	if got := contact.Points[0].Point.Y; math.Abs(got-0.95) > 1e-6 {
		t.Fatalf("contact Y = %v, want 0.95 midway between the two surfaces", got)
	}
}

func TestCollideCylinderCylinderSideBySide(t *testing.T) {
	a := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4})
	b := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4, Offset: Vec3{X: 1.7}})

	contact, ok := Collide(a, b)
	if !ok {
		t.Fatal("expected a cylinder-cylinder contact")
	}
	if contact.Normal.Dot(Vec3{X: 1}) < 0.999 {
		t.Fatalf("normal = %+v, want +X", contact.Normal)
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.3) > 3e-3 {
		t.Fatalf("penetration = %v, want 0.3", got)
	}
}

func TestCollideCylinderCapsule(t *testing.T) {
	cylinder := NewCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1, Height: 4})
	capsule := NewCollider(ColliderConfig{Shape: ShapeCapsule, Radius: 0.5, Height: 2, Offset: Vec3{X: 1.4}})

	contact, ok := Collide(cylinder, capsule)
	if !ok {
		t.Fatal("expected a cylinder-capsule contact")
	}
	if contact.Normal.Dot(Vec3{X: 1}) < 0.999 {
		t.Fatalf("normal = %+v, want +X", contact.Normal)
	}
	// Cylinder wall at x=1, capsule segment at x=1.4 with radius 0.5, so the
	// capsule wall reaches x=0.9 and the overlap is 0.1.
	if got := contact.Points[0].Penetration; math.Abs(got-0.1) > 2e-3 {
		t.Fatalf("penetration = %v, want 0.1", got)
	}
}

// --- Cone -------------------------------------------------------------------

func TestCollideConePlaneRestingOnBase(t *testing.T) {
	// Cone radius 1, height 2, apex on +Y. Centre at y=0.9 puts the base disc
	// at y=-0.1, so it dips 0.1 below the plane.
	cone := NewCollider(ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2, Offset: Vec3{Y: 0.9}})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(cone, plane)
	if !ok {
		t.Fatal("expected a cone-plane contact")
	}
	if !contact.Normal.Near(Vec3{Y: -1}, 1e-12) {
		t.Fatalf("normal = %+v, want -Y", contact.Normal)
	}
	if contact.PointCount != 4 {
		t.Fatalf("PointCount = %d, want 4 base rim points", contact.PointCount)
	}
	for i := 0; i < contact.PointCount; i++ {
		if got := contact.Points[i].Penetration; math.Abs(got-0.1) > 1e-12 {
			t.Fatalf("point[%d] penetration = %v, want 0.1", i, got)
		}
	}
}

func TestCollideConePlaneApexDown(t *testing.T) {
	// Flip the cone so the apex points at the plane. Only the apex touches.
	rotation := QuatFromAxisAngle(Vec3{X: 1}, math.Pi)
	cone := NewCollider(ColliderConfig{
		Shape: ShapeCone, Radius: 1, Height: 2,
		Offset: Vec3{Y: 0.95}, Rotation: rotation,
	})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(cone, plane)
	if !ok {
		t.Fatal("expected a cone apex contact")
	}
	if contact.PointCount != 1 {
		t.Fatalf("PointCount = %d, want 1 apex contact", contact.PointCount)
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.05) > 1e-9 {
		t.Fatalf("penetration = %v, want 0.05", got)
	}
	if got := contact.Points[0].Point; math.Abs(got.X) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Fatalf("apex contact = %+v, want it on the Y axis", got)
	}
}

func TestCollideConeSphereOnBaseRim(t *testing.T) {
	cone := NewCollider(ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2})
	// The base rim sits at (1,-1,0). Place a sphere just outside it along +X.
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{X: 1.4, Y: -1}, Radius: 0.5})

	contact, ok := Collide(cone, sphere)
	if !ok {
		t.Fatal("expected a cone-sphere rim contact")
	}
	if got := contact.Points[0].Penetration; got <= 0 || got > 0.2 {
		t.Fatalf("penetration = %v, want a shallow rim overlap", got)
	}
	if contact.Normal.X <= 0 {
		t.Fatalf("normal = %+v, want a positive X component (cone toward sphere)", contact.Normal)
	}
}

func TestCollideConeBoxApexContact(t *testing.T) {
	// Cone apex points down at a box top face.
	rotation := QuatFromAxisAngle(Vec3{X: 1}, math.Pi)
	box := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 4, Height: 2, Depth: 4})
	cone := NewCollider(ColliderConfig{
		Shape: ShapeCone, Radius: 1, Height: 2,
		Offset: Vec3{Y: 1.95}, Rotation: rotation,
	})

	contact, ok := Collide(box, cone)
	if !ok {
		t.Fatal("expected a box-cone contact")
	}
	if !contact.Normal.Near(Vec3{Y: 1}, 1e-6) {
		t.Fatalf("normal = %+v, want +Y", contact.Normal)
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.05) > 1e-6 {
		t.Fatalf("penetration = %v, want 0.05", got)
	}
}

func TestCollideConeSeparated(t *testing.T) {
	cone := NewCollider(ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2})
	box := NewCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1, Offset: Vec3{X: 4}})
	if _, ok := Collide(cone, box); ok {
		t.Fatal("expected no contact for shapes 4 units apart")
	}
}

// --- Convex hull ------------------------------------------------------------

func TestCollideConvexHullPlaneRestsOnFace(t *testing.T) {
	hull := NewCollider(ColliderConfig{
		Shape:    ShapeConvexHull,
		Offset:   Vec3{Y: 0.4},
		Vertices: boxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5}),
	})
	plane := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	contact, ok := Collide(hull, plane)
	if !ok {
		t.Fatal("expected a hull-plane contact")
	}
	if !contact.Normal.Near(Vec3{Y: -1}, 1e-12) {
		t.Fatalf("normal = %+v, want -Y", contact.Normal)
	}
	if contact.PointCount != 4 {
		t.Fatalf("PointCount = %d, want the 4 bottom corners", contact.PointCount)
	}
	for i := 0; i < contact.PointCount; i++ {
		if got := contact.Points[i].Penetration; math.Abs(got-0.1) > 1e-12 {
			t.Fatalf("point[%d] penetration = %v, want 0.1", i, got)
		}
	}
}

func TestCollideConvexHullTetrahedronAgainstSphere(t *testing.T) {
	// A regular-ish tetrahedron with one face on the y=0 plane and its apex up.
	hull := NewCollider(ColliderConfig{
		Shape: ShapeConvexHull,
		Vertices: []Vec3{
			{X: -1, Y: 0, Z: -1},
			{X: 1, Y: 0, Z: -1},
			{X: 0, Y: 0, Z: 1},
			{X: 0, Y: 2, Z: 0},
		},
	})
	sphere := NewCollider(ColliderConfig{Shape: ShapeSphere, Offset: Vec3{Y: 2.4}, Radius: 0.5})

	contact, ok := Collide(hull, sphere)
	if !ok {
		t.Fatal("expected a hull apex contact")
	}
	if got := contact.Points[0].Penetration; math.Abs(got-0.1) > 1e-4 {
		t.Fatalf("penetration = %v, want 0.1", got)
	}
	if contact.Normal.Dot(Vec3{Y: 1}) < 0.999 {
		t.Fatalf("normal = %+v, want +Y", contact.Normal)
	}
}

// --- Resting behaviour ------------------------------------------------------

func TestWorldCylinderRestsOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})

	for i := 0; i < 300; i++ {
		world.Step(1.0 / 60.0)
	}

	if body.Position.Y < 0.45 || body.Position.Y > 0.55 {
		t.Fatalf("cylinder should rest with its centre near y=0.5, got y=%v", body.Position.Y)
	}
	if math.Abs(body.Velocity.Y) > 0.2 {
		t.Fatalf("cylinder should be near rest, velocity=%+v", body.Velocity)
	}
}

func TestWorldConeRestsOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeCone, Radius: 0.5, Height: 1})

	for i := 0; i < 300; i++ {
		world.Step(1.0 / 60.0)
	}

	if body.Position.Y < 0.45 || body.Position.Y > 0.55 {
		t.Fatalf("cone should rest with its centre near y=0.5, got y=%v", body.Position.Y)
	}
}

func TestWorldConvexHullRestsOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 8})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.5, Y: 0.5, Z: 0.5})})

	for i := 0; i < 300; i++ {
		world.Step(1.0 / 60.0)
	}

	if body.Position.Y < 0.45 || body.Position.Y > 0.55 {
		t.Fatalf("hull should rest with its centre near y=0.5, got y=%v", body.Position.Y)
	}
}

func TestWorldCylinderStackOnPlane(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})

	bottom := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0, Friction: 0.5})
	bottom.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})
	top := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 5}, Restitution: 0, Friction: 0.5})
	top.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})

	for i := 0; i < 600; i++ {
		world.Step(1.0 / 60.0)
	}

	if bottom.Position.Y < 0.45 || bottom.Position.Y > 0.6 {
		t.Fatalf("bottom cylinder should rest near y=0.5, got y=%v", bottom.Position.Y)
	}
	if top.Position.Y < 1.4 || top.Position.Y > 1.65 {
		t.Fatalf("top cylinder should stack near y=1.5, got y=%v", top.Position.Y)
	}
	if math.Abs(top.Velocity.Y) > 0.3 {
		t.Fatalf("top cylinder should approach rest, got velocity=%+v", top.Velocity)
	}
}

func TestWorldCylinderRestsOnStaticBox(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIter: 10})
	ground := world.AddBody(BodyConfig{Mass: 0})
	ground.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 6, Height: 2, Depth: 6})

	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 3}, Restitution: 0, Friction: 0.5})
	body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})

	for i := 0; i < 400; i++ {
		world.Step(1.0 / 60.0)
	}

	// The box top is at y=1 and the cylinder half height is 0.5.
	if body.Position.Y < 1.45 || body.Position.Y > 1.56 {
		t.Fatalf("cylinder should rest near y=1.5 on the box, got y=%v", body.Position.Y)
	}
}

// --- AABB -------------------------------------------------------------------

func TestColliderAABBIsFiniteAndTightForEveryShape(t *testing.T) {
	cases := []struct {
		name   string
		config ColliderConfig
		want   AABB
	}{
		{
			name:   "cylinder upright",
			config: ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 2},
			want:   AABB{Min: Vec3{X: -0.5, Y: -1, Z: -0.5}, Max: Vec3{X: 0.5, Y: 1, Z: 0.5}},
		},
		{
			name: "cylinder rotated onto X",
			config: ColliderConfig{
				Shape: ShapeCylinder, Radius: 0.5, Height: 2,
				Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/2),
			},
			want: AABB{Min: Vec3{X: -1, Y: -0.5, Z: -0.5}, Max: Vec3{X: 1, Y: 0.5, Z: 0.5}},
		},
		{
			name:   "cone upright",
			config: ColliderConfig{Shape: ShapeCone, Radius: 1, Height: 2},
			want:   AABB{Min: Vec3{X: -1, Y: -1, Z: -1}, Max: Vec3{X: 1, Y: 1, Z: 1}},
		},
		{
			name:   "convex hull",
			config: ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 1, Y: 2, Z: 3})},
			want:   AABB{Min: Vec3{X: -1, Y: -2, Z: -3}, Max: Vec3{X: 1, Y: 2, Z: 3}},
		},
		{
			name: "triangle mesh",
			config: ColliderConfig{
				Shape:    ShapeTriangleMesh,
				Vertices: []Vec3{{X: -2, Z: -2}, {X: 2, Z: -2}, {X: 0, Y: 1, Z: 2}},
				Indices:  []int{0, 1, 2},
			},
			want: AABB{Min: Vec3{X: -2, Y: 0, Z: -2}, Max: Vec3{X: 2, Y: 1, Z: 2}},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			collider := NewCollider(testCase.config)
			if err := collider.Err(); err != nil {
				t.Fatalf("collider is invalid: %v", err)
			}
			box := collider.AABB()
			if !box.IsFinite() {
				t.Fatalf("AABB = %+v, want a finite box", box)
			}
			if !box.Min.Near(testCase.want.Min, 1e-9) || !box.Max.Near(testCase.want.Max, 1e-9) {
				t.Fatalf("AABB = %+v, want %+v", box, testCase.want)
			}
		})
	}
}

func TestColliderAABBContainsSupportPoints(t *testing.T) {
	// Sample many directions and check the support point never escapes the
	// reported bounds. This catches a bound that is too tight.
	configs := []ColliderConfig{
		{Shape: ShapeCylinder, Radius: 0.7, Height: 1.4, Rotation: QuatFromAxisAngle(Vec3{X: 1, Y: 1}, 0.9)},
		{Shape: ShapeCone, Radius: 0.6, Height: 2.2, Rotation: QuatFromAxisAngle(Vec3{X: 1, Z: 2}, 2.1)},
		{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 0.4, Y: 0.9, Z: 1.3}),
			Rotation: QuatFromAxisAngle(Vec3{Y: 1, Z: 1}, 0.4)},
	}
	for _, config := range configs {
		collider := NewCollider(config)
		shape, ok := newConvexShape(collider)
		if !ok {
			t.Fatalf("%v: newConvexShape failed", config.Shape)
		}
		box := collider.AABB().Expand(1e-9)
		for i := 0; i < 500; i++ {
			angle := float64(i) * 0.37
			dir := Vec3{
				X: math.Cos(angle),
				Y: math.Sin(angle * 1.7),
				Z: math.Sin(angle),
			}
			if dir.Len2() <= epsilon {
				continue
			}
			point := shape.support(dir)
			if !box.Contains(point) {
				t.Fatalf("%v: support point %+v escapes AABB %+v", config.Shape, point, box)
			}
		}
	}
}
