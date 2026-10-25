package preview_test

import (
	"math"
	"testing"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/preview"
)

func TestBundleComposesExactGroupAffineForObjectsAndPoints(t *testing.T) {
	props := scene.Props{Graph: scene.NewGraph(scene.Group{
		Position: scene.Vec3(3, -2, 5),
		Scale:    scene.Vec3(2, 3, -4),
		Children: []scene.Node{
			scene.Mesh{
				ID: "mesh", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Position: scene.Vec3(1, 2, 3), Rotation: scene.Euler{Z: math.Pi / 2}, Scale: scene.Vec3(5, 6, 7),
			},
			scene.Points{
				ID: "points", Count: 1, Positions: []scene.Vector3{scene.Vec3(1, 0, 0)},
				Position: scene.Vec3(1, 2, 3), Rotation: scene.Euler{Z: math.Pi / 2},
			},
		},
	})}
	frame := preview.Bundle(props, preview.Options{})
	if len(frame.InstancedMeshes) != 1 || len(frame.Points) != 1 {
		t.Fatalf("unexpected bundle records: meshes=%d points=%d", len(frame.InstancedMeshes), len(frame.Points))
	}
	wantMatrix := []float64{
		0, 15, 0, 0,
		-12, 0, 0, 0,
		0, 0, -28, 0,
		5, 4, -7, 1,
	}
	assertFloatSliceNear(t, frame.InstancedMeshes[0].Transforms, wantMatrix, 1e-12)
	assertFloatSliceNear(t, frame.Points[0].Positions, []float64{5, 7, -7}, 1e-12)
}

func assertFloatSliceNear(t *testing.T, got, want []float64, tolerance float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d (%v)", len(got), len(want), got)
	}
	for index := range want {
		if math.Abs(got[index]-want[index]) > tolerance {
			t.Fatalf("value[%d] = %.15g, want %.15g (all=%v)", index, got[index], want[index], got)
		}
	}
}

func TestGroupSpinPreviewUsesSceneClockAndSharedPivot(t *testing.T) {
	props := scene.Props{Graph: scene.NewGraph(scene.Group{ID: "rotor", Spin: scene.Rotate(0, 0, math.Pi/2), Children: []scene.Node{
		scene.Mesh{ID: "blade", Geometry: scene.BoxGeometry{}, Position: scene.Vec3(2, 0, 0)},
		scene.Points{ID: "marker", Count: 1, Position: scene.Vec3(2, 0, 0), Positions: []scene.Vector3{{}}},
	}})}
	for _, seconds := range []float64{0, 1, 2, 3, 4, .5} {
		frame := preview.Bundle(props, preview.Options{Time: seconds})
		if len(frame.InstancedMeshes) != 1 || len(frame.Points) != 1 {
			t.Fatal("missing rotor children")
		}
		want := []float64{2 * math.Cos(seconds*math.Pi/2), 2 * math.Sin(seconds*math.Pi/2), 0}
		assertFloatSliceNear(t, frame.InstancedMeshes[0].Transforms[12:15], want, 1e-12)
		assertFloatSliceNear(t, frame.Points[0].Positions, want, 1e-12)
	}
}

func TestInstancedNamedParentPreviewSamplesGroupClock(t *testing.T) {
	props := scene.Props{Graph: scene.NewGraph(scene.Group{ID: "rotor", Position: scene.Vec3(3, 4, 0), Scale: scene.Vec3(2, 3, 1), Spin: scene.Rotate(0, 0, math.Pi/2), Children: []scene.Node{
		scene.Mesh{ID: "ship", Position: scene.Vec3(2, 0, 0), Geometry: scene.BoxGeometry{}},
		scene.InstancedMesh{ID: "windows", ParentNode: "ship", Count: 1, Positions: []scene.Vector3{scene.Vec3(1, 0, 0)}, Geometry: scene.BoxGeometry{}},
	}})}
	for _, seconds := range []float64{0, 1, 2, .5} {
		frame := preview.Bundle(props, preview.Options{Time: seconds})
		found := false
		for _, mesh := range frame.InstancedMeshes {
			if mesh.ID == "windows" {
				found = true
				assertFloatSliceNear(t, mesh.Transforms[12:15], []float64{3 + 6*math.Cos(seconds*math.Pi/2), 4 + 6*math.Sin(seconds*math.Pi/2), 0}, 1e-12)
			}
		}
		if !found {
			t.Fatal("missing carried batch")
		}
	}
	if x := props.SceneIR().InstancedMeshes[0].Transforms[12]; x != 1 {
		t.Fatalf("source changed: %g", x)
	}
}
