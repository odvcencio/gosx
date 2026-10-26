package scene

import (
	"math"
	"testing"
)

func TestInstanceParentUsesSharedPivotWithoutBakingAncestorTwice(t *testing.T) {
	graph := NewGraph(Group{ID: "rotor", Position: Vec3(3, 4, 0), Scale: Vec3(2, 3, 1), Spin: Rotate(0, 0, math.Pi/2), Children: []Node{
		Mesh{ID: "ship", Position: Vec3(2, 0, 0), Geometry: BoxGeometry{}},
		InstancedMesh{ID: "windows", ParentNode: "ship", Count: 1, Positions: []Vector3{Vec3(1, 0, 0)}, Geometry: BoxGeometry{Width: .2, Height: .2, Depth: .2}},
	}})
	ir := graph.SceneIR()
	if got := ir.InstancedMeshes[0].Transforms[12]; got != 1 {
		t.Fatalf("baked explicit parent's structural ancestor: %g", got)
	}
	accelerator := NewSceneAccelerator(graph)
	for _, seconds := range []float64{0, 1, 2, 3, 4, .5} {
		matrices, err := ir.ParentWorldMatrices(seconds)
		if err != nil {
			t.Fatal(err)
		}
		position := affinePoint(affineMatrix(matrices["ship"]), Vec3(1, 0, 0))
		angle := seconds * math.Pi / 2
		expected := Vec3(3+6*math.Cos(angle), 4+6*math.Sin(angle), 0)
		if !vectorClose(position, expected, 1e-12) {
			t.Fatalf("at %g got %v want %v", seconds, position, expected)
		}
		ray := Ray{Origin: Vec3(position.X, position.Y, 5), Direction: Vec3(0, 0, -1)}
		walk, ok := RaycastGraph(graph, ray, AtTime(seconds))
		if !ok || walk.ID != "windows" || walk.InstanceIndex == nil || *walk.InstanceIndex != 0 {
			t.Fatalf("at %g walk missed carried instance: %+v", seconds, walk)
		}
		fast, ok := accelerator.Raycast(ray, AtTime(seconds))
		if !ok || fast.ID != walk.ID || math.Abs(fast.Distance-walk.Distance) > 1e-12 {
			t.Fatalf("at %g BVH differs: %+v %+v", seconds, walk, fast)
		}
	}
}

func TestInstanceParentExactPrefixMissingAndModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		objects []ObjectIR
		models  []ModelIR
		x       float64
		found   bool
	}{
		{name: "exact wins", objects: []ObjectIR{{ID: "ship/hull", X: 90}, {ID: "ship", X: 10}}, x: 10, found: true},
		{name: "prefix", objects: []ObjectIR{{ID: "ship/hull", X: 20}}, x: 20, found: true},
		{name: "model exact wins", objects: []ObjectIR{{ID: "ship/hull", X: 90}}, models: []ModelIR{{ObjectIR: ObjectIR{ID: "ship", X: 30}}}, x: 30, found: true},
		{name: "missing", objects: []ObjectIR{{ID: "ship-two", X: 50}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ir := SceneIR{Objects: tc.objects, Models: tc.models, InstancedMeshes: []InstancedMeshIR{{ID: "windows", ParentNode: "ship"}}}
			matrices, err := ir.ParentWorldMatrices(0)
			if err != nil {
				t.Fatal(err)
			}
			matrix, found := matrices["ship"]
			if found != tc.found || found && matrix[12] != tc.x {
				t.Fatalf("found=%v matrix=%v", found, matrix)
			}
		})
	}
	graph := NewGraph(Group{Position: Vec3(100, 0, 0), Children: []Node{InstancedMesh{ID: "free", ParentNode: "missing", Count: 1, Positions: []Vector3{Vec3(2, 0, 0)}, Geometry: BoxGeometry{}}}})
	ray := Ray{Origin: Vec3(2, 0, 5), Direction: Vec3(0, 0, -1)}
	if hit, ok := RaycastGraph(graph, ray); !ok || hit.ID != "free" || hit.InstanceIndex == nil || *hit.InstanceIndex != 0 {
		t.Fatalf("missing parent didn't fall back to world: %+v", hit)
	}
	if hit, ok := NewSceneAccelerator(graph).Raycast(ray); !ok || hit.ID != "free" || hit.InstanceIndex == nil || *hit.InstanceIndex != 0 {
		t.Fatalf("BVH missing parent fallback: %+v", hit)
	}
	if n := testing.AllocsPerRun(20, func() { _, _ = (SceneIR{}).ParentWorldMatrices(1) }); n != 0 {
		t.Fatalf("unparented path allocates %g", n)
	}
}

func TestInstanceParentCanonicalValidation(t *testing.T) {
	ir := (Props{Graph: NewGraph(Mesh{ID: "ship", Geometry: BoxGeometry{}}, InstancedMesh{ID: "windows", ParentNode: "ship", Count: 1, Geometry: BoxGeometry{}})}).CanonicalIR()
	if err := ir.Validate(); err != nil {
		t.Fatal(err)
	}
}
