package scene

import (
	"math"
	"testing"
)

func TestRaycastGraphReturnsClosestMesh(t *testing.T) {
	graph := NewGraph(
		Mesh{
			ID:       "far",
			Geometry: SphereGeometry{Radius: 1},
			Position: Vec3(0, 0, -6),
		},
		Mesh{
			ID:       "near",
			Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2},
			Position: Vec3(0, 0, -2),
		},
	)
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected ray hit")
	}
	if hit.ID != "near" || hit.Kind != "box" {
		t.Fatalf("expected near box hit, got %#v", hit)
	}
	if math.Abs(hit.Distance-5) > 1e-9 {
		t.Fatalf("expected distance 5, got %v", hit.Distance)
	}
}

func TestRaycastGraphPickableOnlyAndMaxDistance(t *testing.T) {
	notPickable := false
	graph := NewGraph(
		Mesh{
			ID:       "shield",
			Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2},
			Position: Vec3(0, 0, -2),
			Pickable: &notPickable,
		},
		Mesh{
			ID:       "target",
			Geometry: SphereGeometry{Radius: 1},
			Position: Vec3(0, 0, -6),
		},
	)
	hit, ok := RaycastGraph(
		graph,
		Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)},
		PickableOnly(),
	)
	if !ok || hit.ID != "target" {
		t.Fatalf("expected pickable target, got %#v ok=%v", hit, ok)
	}
	if _, ok := RaycastGraph(
		graph,
		Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)},
		PickableOnly(),
		MaxDistance(4),
	); ok {
		t.Fatal("expected max distance to reject hit")
	}
}

func TestRaycastPickableChildSurvivesNonPickableParent(t *testing.T) {
	notPickable := false
	graph := NewGraph(Mesh{
		ID: "decoration", Geometry: BoxGeometry{Width: 4, Height: 4, Depth: 0.2}, Pickable: &notPickable,
		Children: []Node{Mesh{ID: "control", Geometry: SphereGeometry{Radius: 0.5}, Position: Vec3(0, 0, -1)}},
	})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)}, PickableOnly())
	if !ok || hit.ID != "control" {
		t.Fatalf("pickable child was hidden by non-pickable parent: %#v ok=%v", hit, ok)
	}
}

func TestRaycastGraphUsesGroupTransforms(t *testing.T) {
	graph := NewGraph(Group{
		Position: Vec3(3, 0, 0),
		Children: []Node{
			Mesh{
				ID:       "nested",
				Geometry: CubeGeometry{Size: 2},
				Position: Vec3(0, 0, -2),
			},
		},
	})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(3, 0, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected nested hit")
	}
	if hit.ID != "nested" {
		t.Fatalf("expected nested hit, got %#v", hit)
	}
	if math.Abs(hit.Point.X-3) > 1e-9 {
		t.Fatalf("expected world-space hit point to include group transform, got %#v", hit.Point)
	}
}

func TestRaycastGraphReturnsExactInstancedMeshIndex(t *testing.T) {
	graph := NewGraph(InstancedMesh{
		ID:        "pieces",
		Count:     3,
		Geometry:  SphereGeometry{Radius: 0.5},
		Positions: []Vector3{Vec3(-2, 0, 0), Vec3(0, 0, 0), Vec3(2, 0, 0)},
		Scales:    []Vector3{Vec3(1, 0.5, 1), Vec3(1, 0.5, 1), Vec3(1, 0.5, 1)},
	})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 3, 0), Direction: Vec3(0, -1, 0)}, PickableOnly())
	if !ok {
		t.Fatal("expected instanced sphere hit")
	}
	if hit.ID != "pieces" || hit.InstanceIndex == nil || *hit.InstanceIndex != 1 {
		t.Fatalf("expected pieces instance 1, got %#v", hit)
	}
	if hit.Method != "analytic-sphere" || math.Abs(hit.Point.Y-0.25) > 1e-9 {
		t.Fatalf("expected exact scaled sphere surface, got %#v", hit)
	}
}

func TestRaycastGraphFiltersNonPickableInstancedMesh(t *testing.T) {
	notPickable := false
	graph := NewGraph(InstancedMesh{
		ID:        "decorative-pieces",
		Count:     2,
		Geometry:  SphereGeometry{Radius: 0.5},
		Positions: []Vector3{Vec3(0, 0, 0), Vec3(0, 0, -2)},
		Pickable:  &notPickable,
	})
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)}, PickableOnly())
	if trace.Closest != nil {
		t.Fatalf("expected non-pickable instances to be filtered, got %#v", trace.Closest)
	}
	if trace.FilteredPrimitives != 2 || trace.PrimitivesTested != 0 || trace.InstancesTested != 0 {
		t.Fatalf("unexpected filtered instance telemetry: %#v", trace)
	}
}

func TestTraceGraphReportsSortedHitsAndWork(t *testing.T) {
	graph := NewGraph(InstancedMesh{
		ID:        "stack",
		Count:     2,
		Geometry:  SphereGeometry{Radius: 0.5},
		Positions: []Vector3{Vec3(0, 0, -2), Vec3(0, 0, -5)},
	})
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 2), Direction: Vec3(0, 0, -1)})
	if trace.NodesVisited != 1 || trace.PrimitivesTested != 2 || trace.InstancesTested != 2 {
		t.Fatalf("unexpected traversal telemetry: %#v", trace)
	}
	if len(trace.Hits) != 2 || trace.Closest == nil || *trace.Closest.InstanceIndex != 0 {
		t.Fatalf("expected two sorted instance hits, got %#v", trace)
	}
	if trace.Hits[0].Distance >= trace.Hits[1].Distance {
		t.Fatalf("hits are not nearest-first: %#v", trace.Hits)
	}
}

func TestCylinderRaycastRejectsBoundingBoxCorner(t *testing.T) {
	graph := NewGraph(Mesh{
		ID:       "round-board",
		Geometry: CylinderGeometry{RadiusTop: 1, RadiusBottom: 1, Height: 0.5},
	})
	// This ray crosses the old AABB top face but is outside the circular cap.
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.9, 2, 0.9), Direction: Vec3(0, -1, 0)}); ok {
		t.Fatalf("expected cylinder corner miss, got %#v", hit)
	}
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.5, 2, 0.5), Direction: Vec3(0, -1, 0)})
	if !ok || hit.Method != "analytic-frustum" || math.Abs(hit.Point.Y-0.25) > 1e-9 {
		t.Fatalf("expected exact cap hit, got %#v ok=%v", hit, ok)
	}
}

func TestCylinderRaycastPreservesZeroRadiusConeTip(t *testing.T) {
	graph := NewGraph(Mesh{ID: "cone", Geometry: CylinderGeometry{RadiusTop: 0, RadiusBottom: 1, Height: 1}})
	if hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0.2, 0.49, 2), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatalf("ray above cone envelope should miss, got %#v", hit)
	}
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0.49, 2), Direction: Vec3(0, 0, -1)})
	if !ok || hit.Method != "analytic-frustum" {
		t.Fatalf("ray through cone axis should hit, got %#v ok=%v", hit, ok)
	}
}

func TestSceneIRPropagatesDeepHierarchyTransforms(t *testing.T) {
	var node Node = Mesh{
		ID:       "leaf",
		Geometry: CubeGeometry{Size: 1},
		Position: Vec3(1, 0, -3),
	}
	for i := 0; i < 1000; i++ {
		node = Group{
			Position: Vec3(0.001, 0, 0),
			Children: []Node{node},
		}
	}

	ir := NewGraph(node).SceneIR()
	if len(ir.Objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(ir.Objects))
	}
	obj := ir.Objects[0]
	if obj.ID != "leaf" {
		t.Fatalf("object ID = %q, want leaf", obj.ID)
	}
	if math.Abs(obj.X-2) > 1e-9 || math.Abs(obj.Z+3) > 1e-9 {
		t.Fatalf("deep hierarchy world transform = (%v,%v,%v), want near (2,0,-3)", obj.X, obj.Y, obj.Z)
	}
}

func TestRaycastMeshHonorsLeafScale(t *testing.T) {
	// A unit box at origin misses a ray at x=1.5; scaled 2x it must hit.
	ray := Ray{Origin: Vector3{X: 1.5, Y: 0, Z: 6}, Direction: Vector3{Z: -1}}
	unit := Mesh{ID: "unit", Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2}}
	scaled := Mesh{ID: "scaled", Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2}, Scale: Vector3{X: 2, Y: 2, Z: 2}}
	if trace := TraceGraph(Graph{Nodes: []Node{unit}}, ray); trace.Closest != nil {
		t.Fatalf("unit box unexpectedly hit: %+v", trace.Closest)
	}
	trace := TraceGraph(Graph{Nodes: []Node{scaled}}, ray)
	if trace.Closest == nil || trace.Closest.ID != "scaled" {
		t.Fatalf("scaled box must hit, got %+v", trace.Closest)
	}
}
