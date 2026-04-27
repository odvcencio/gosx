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
