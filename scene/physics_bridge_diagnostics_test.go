package scene

import (
	"strings"
	"testing"

	"m31labs.dev/gosx/physics"
)

func TestPhysicsSpecMapsCylinderAndCone(t *testing.T) {
	ir := IR{Physics: &IRPhysics{
		Bodies: []IRRigidBody{{
			ID:   "wheel",
			Mass: 1,
			Colliders: []IRCollider{
				{Shape: "cylinder", Radius: 0.5, Height: 1},
				{Shape: "cone", Radius: 0.5, Height: 1},
			},
		}},
	}}

	spec := ir.PhysicsSpec()
	if len(spec.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %v, want none", spec.Diagnostics)
	}
	if len(spec.Bodies) != 1 || len(spec.Bodies[0].Colliders) != 2 {
		t.Fatalf("spec bodies = %+v, want one body with two colliders", spec.Bodies)
	}
	if spec.Bodies[0].Colliders[0].Shape != physics.ShapeCylinder {
		t.Fatalf("first collider shape = %v, want cylinder", spec.Bodies[0].Colliders[0].Shape)
	}
	if spec.Bodies[0].Colliders[1].Shape != physics.ShapeCone {
		t.Fatalf("second collider shape = %v, want cone", spec.Bodies[0].Colliders[1].Shape)
	}

	world, err := physics.BuildWorldChecked(spec)
	if err != nil {
		t.Fatalf("BuildWorldChecked() error = %v, want nil", err)
	}
	if len(world.Colliders()) != 2 {
		t.Fatalf("world collider count = %d, want 2", len(world.Colliders()))
	}
}

func TestPhysicsSpecReportsShapesItCannotBuild(t *testing.T) {
	cases := []struct {
		shape   string
		wantMsg string
	}{
		{shape: "convex", wantMsg: "needs vertex data"},
		{shape: "convexhull", wantMsg: "needs vertex data"},
		{shape: "mesh", wantMsg: "needs vertex and index data"},
		{shape: "trianglemesh", wantMsg: "needs vertex and index data"},
		{shape: "blob", wantMsg: "unknown collider shape"},
		{shape: "", wantMsg: "collider Shape is empty"},
	}
	for _, testCase := range cases {
		t.Run(testCase.shape, func(t *testing.T) {
			ir := IR{Physics: &IRPhysics{
				Bodies: []IRRigidBody{{
					ID:        "crate",
					Mass:      1,
					Colliders: []IRCollider{{Shape: testCase.shape, Radius: 1, Height: 1}},
				}},
			}}
			spec := ir.PhysicsSpec()
			if len(spec.Diagnostics) != 1 {
				t.Fatalf("Diagnostics = %v, want exactly one entry", spec.Diagnostics)
			}
			message := spec.Diagnostics[0]
			if !strings.Contains(message, testCase.wantMsg) {
				t.Fatalf("diagnostic %q must mention %q", message, testCase.wantMsg)
			}
			if !strings.Contains(message, "crate") {
				t.Fatalf("diagnostic %q must name the declaring body", message)
			}
			if _, err := physics.BuildWorldChecked(spec); err == nil {
				t.Fatal("BuildWorldChecked must return an error for a rejected shape")
			}
		})
	}
}

func TestPhysicsSpecReportsStaticColliderProblems(t *testing.T) {
	ir := IR{Physics: &IRPhysics{
		Static: []IRCollider{
			{Shape: "plane", Normal: IRVector3{Y: 1}},
			{Shape: "mesh"},
		},
	}}
	spec := ir.PhysicsSpec()
	if len(spec.Diagnostics) != 1 {
		t.Fatalf("Diagnostics = %v, want exactly one entry", spec.Diagnostics)
	}
	if !strings.Contains(spec.Diagnostics[0], "scene static geometry") {
		t.Fatalf("diagnostic %q must name the scene static geometry", spec.Diagnostics[0])
	}
	if len(spec.Static) != 1 {
		t.Fatalf("static collider count = %d, want the plane to survive", len(spec.Static))
	}
}
