package physics

import (
	"errors"
	"strings"
	"testing"
)

func TestColliderValidationRejectsUnusableShapes(t *testing.T) {
	cases := []struct {
		name    string
		config  ColliderConfig
		wantErr string
	}{
		{
			name:    "cylinder without radius",
			config:  ColliderConfig{Shape: ShapeCylinder, Height: 2},
			wantErr: "cylinder collider needs Radius > 0",
		},
		{
			name:    "cylinder without height",
			config:  ColliderConfig{Shape: ShapeCylinder, Radius: 1},
			wantErr: "cylinder collider needs Radius > 0",
		},
		{
			name:    "cone without radius",
			config:  ColliderConfig{Shape: ShapeCone, Height: 2},
			wantErr: "cone collider needs Radius > 0",
		},
		{
			name:    "convex hull with no vertices",
			config:  ColliderConfig{Shape: ShapeConvexHull},
			wantErr: "convex hull collider needs at least 4 Vertices",
		},
		{
			name: "convex hull with three vertices",
			config: ColliderConfig{Shape: ShapeConvexHull, Vertices: []Vec3{
				{}, {X: 1}, {Y: 1},
			}},
			wantErr: "convex hull collider needs at least 4 Vertices",
		},
		{
			name: "coplanar convex hull",
			config: ColliderConfig{Shape: ShapeConvexHull, Vertices: []Vec3{
				{}, {X: 1}, {Z: 1}, {X: 1, Z: 1},
			}},
			wantErr: "Vertices are coplanar",
		},
		{
			name:    "triangle mesh with no geometry",
			config:  ColliderConfig{Shape: ShapeTriangleMesh},
			wantErr: "triangle mesh collider needs Vertices plus Indices",
		},
		{
			name: "triangle mesh with out-of-range indices",
			config: ColliderConfig{
				Shape:    ShapeTriangleMesh,
				Vertices: []Vec3{{}, {X: 1}, {Z: 1}},
				Indices:  []int{0, 1, 9},
			},
			wantErr: "triangle mesh collider needs Vertices plus Indices",
		},
		{
			name:    "box with zero depth",
			config:  ColliderConfig{Shape: ShapeBox, Width: 1, Height: 1},
			wantErr: "box collider needs Width, Height and Depth > 0",
		},
		{
			name:    "sphere with zero radius",
			config:  ColliderConfig{Shape: ShapeSphere},
			wantErr: "sphere collider needs Radius > 0",
		},
		{
			name:    "unknown shape",
			config:  ColliderConfig{Shape: ColliderShape(42)},
			wantErr: "unknown collider shape 42",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			collider := NewCollider(testCase.config)
			err := collider.Err()
			if err == nil {
				t.Fatalf("Err() = nil, want an error mentioning %q", testCase.wantErr)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("Err() = %q, want it to mention %q", err.Error(), testCase.wantErr)
			}
			// An unusable collider must never poison the broadphase with an
			// unbounded box.
			box := collider.AABB()
			if !box.IsFinite() {
				t.Fatalf("AABB = %+v, want a finite box for an unusable collider", box)
			}
			// It must also never generate a contact.
			ground := NewCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
			if _, ok := Collide(collider, ground); ok {
				t.Fatal("an unusable collider must not produce a contact")
			}
			if _, ok := Collide(ground, collider); ok {
				t.Fatal("an unusable collider must not produce a contact in either order")
			}
		})
	}
}

func TestWorldDiagnosticsReportUnusableColliders(t *testing.T) {
	world := NewWorld(DefaultWorldConfig())
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	body := world.AddBody(BodyConfig{ID: "wheel", Mass: 1})
	body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5}) // no height

	diagnostics := world.Diagnostics()
	if len(diagnostics) != 1 {
		t.Fatalf("Diagnostics() returned %d entries, want 1: %v", len(diagnostics), diagnostics)
	}
	entry := diagnostics[0]
	if entry.Shape != ShapeCylinder {
		t.Fatalf("diagnostic shape = %v, want cylinder", entry.Shape)
	}
	if entry.BodyID != "wheel" {
		t.Fatalf("diagnostic body = %q, want \"wheel\"", entry.BodyID)
	}
	if entry.ColliderIndex == 0 {
		t.Fatal("diagnostic must carry the collider index")
	}
	message := entry.Error()
	for _, want := range []string{"cylinder", "wheel"} {
		if !strings.Contains(message, want) {
			t.Fatalf("diagnostic message %q must mention %q", message, want)
		}
	}
	if world.Err() == nil {
		t.Fatal("Err() must be non-nil when diagnostics exist")
	}
}

func TestWorldDiagnosticsEmptyForSupportedShapes(t *testing.T) {
	vertices, indices := gridMesh(2, 4)
	world := NewWorld(DefaultWorldConfig())
	world.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}})
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	body := world.AddBody(BodyConfig{Mass: 1})
	body.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 0.5, Height: 1})
	body.AddCollider(ColliderConfig{Shape: ShapeCone, Radius: 0.5, Height: 1})
	body.AddCollider(ColliderConfig{Shape: ShapeConvexHull, Vertices: boxVertices(Vec3{X: 1, Y: 1, Z: 1})})

	if diagnostics := world.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("Diagnostics() = %v, want none", diagnostics)
	}
	if err := world.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

func TestWorldDiagnosticsReportMeshAgainstMesh(t *testing.T) {
	vertices, indices := gridMesh(2, 4)
	world := NewWorld(DefaultWorldConfig())
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{Y: 5}})
	body.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})

	err := world.Err()
	if err == nil {
		t.Fatal("Err() must report the unsupported mesh pairing")
	}
	if !errors.Is(err, ErrMeshPairUnsupported) {
		t.Fatalf("Err() = %v, want it to wrap ErrMeshPairUnsupported", err)
	}
}

func TestWorldDiagnosticsAllowTwoStaticMeshes(t *testing.T) {
	// Two immovable meshes never pair in the broadphase, so they are not a
	// problem.
	vertices, indices := gridMesh(2, 4)
	world := NewWorld(DefaultWorldConfig())
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices})
	world.AddCollider(ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices, Offset: Vec3{X: 10}})

	if err := world.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil for two static meshes", err)
	}
}

func TestBuildWorldCheckedSurfacesSpecDiagnostics(t *testing.T) {
	spec := WorldSpec{
		Config: DefaultWorldConfig(),
		Static: []ColliderConfig{{Shape: ShapePlane, Normal: Vec3{Y: 1}}},
		Bodies: []BodySpec{{
			Body:      BodyConfig{ID: "crate", Mass: 1},
			Colliders: []ColliderConfig{{Shape: ShapeCone, Radius: 1}}, // no height
		}},
		Diagnostics: []string{"collider shape \"blob\" is not a known shape"},
	}

	world, err := BuildWorldChecked(spec)
	if world == nil {
		t.Fatal("BuildWorldChecked must still return the world")
	}
	if err == nil {
		t.Fatal("BuildWorldChecked must report both problems")
	}
	message := err.Error()
	for _, want := range []string{"blob", "cone collider needs Radius > 0"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q must mention %q", message, want)
		}
	}
}

func TestBuildWorldCheckedReturnsNilErrorForCleanSpec(t *testing.T) {
	spec := WorldSpec{
		Config: DefaultWorldConfig(),
		Static: []ColliderConfig{{Shape: ShapePlane, Normal: Vec3{Y: 1}}},
		Bodies: []BodySpec{{
			Body:      BodyConfig{ID: "crate", Mass: 1},
			Colliders: []ColliderConfig{{Shape: ShapeCylinder, Radius: 0.5, Height: 1}},
		}},
	}
	world, err := BuildWorldChecked(spec)
	if err != nil {
		t.Fatalf("BuildWorldChecked() error = %v, want nil", err)
	}
	if len(world.Colliders()) != 2 {
		t.Fatalf("collider count = %d, want 2", len(world.Colliders()))
	}
}

func TestUnusableColliderStaysOutOfTheUnboundedBroadphaseList(t *testing.T) {
	world := NewWorld(WorldConfig{Gravity: Vec3{}, FixedTimestep: 1.0 / 60.0})
	world.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 1}) // no height
	body := world.AddBody(BodyConfig{Mass: 1, Position: Vec3{X: 50}})
	body.AddCollider(ColliderConfig{Shape: ShapeSphere, Radius: 0.5})

	pairs := world.CandidatePairs()
	if len(pairs) != 0 {
		t.Fatalf("candidate pairs = %d, want 0; an unusable collider must not pair with distant bodies", len(pairs))
	}
}
