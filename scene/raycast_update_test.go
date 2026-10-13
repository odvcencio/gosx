package scene

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

func updateTestGeometry() BufferGeometry {
	var g BufferGeometry
	for i := 0; i < 24; i++ {
		x := float64(i) * 3
		g.Positions = append(g.Positions, x-1, -1, 0, x+1, -1, 0, x, 1, 0)
		g.Indices = append(g.Indices, i*3, i*3+1, i*3+2)
	}
	return g
}

func TestSceneAcceleratorUpdateOwnsAndReusesExactGeometry(t *testing.T) {
	g := updateTestGeometry()
	graph := NewGraph(Mesh{ID: "mesh", Geometry: g})
	a := NewSceneAccelerator(graph)
	original := a.meshSoups["mesh"]
	if original == nil || len(original.tree.nodes) == 0 {
		t.Fatal("fixture did not build a triangle hierarchy")
	}
	g.Positions = slices.Clone(g.Positions)
	g.Indices = slices.Clone(g.Indices)
	graph = NewGraph(Group{Position: Vec3(0, 2, 0), Scale: Vec3(2, 2, 2), Children: []Node{Mesh{ID: "mesh", Geometry: g, Pickable: Bool(false)}}})
	a.Update(graph)
	if a.meshSoups["mesh"] != original {
		t.Fatal("identical decoded streams rebuilt the hierarchy")
	}
	ray := Ray{Origin: Vec3(0, 2, 5), Direction: Vec3(0, 0, -1)}
	if hit, ok := a.Raycast(ray); !ok || hit.ID != "mesh" {
		t.Fatal("new parent transform was not applied", hit, ok)
	}
	if _, ok := a.Raycast(ray, PickableOnly()); ok {
		t.Fatal("old interaction flags survived update")
	}
	// The source is mutable. Editing it cannot corrupt the retained tree or its
	// equality comparison; Update must detect an edit to that same slice.
	g.Positions[0], g.Positions[3], g.Positions[6] = 100, 102, 101
	if _, ok := a.Raycast(ray); !ok {
		t.Fatal("source edit mutated the owned snapshot")
	}
	a.Update(graph)
	if a.meshSoups["mesh"] == original {
		t.Fatal("changed positions reused a stale hierarchy")
	}
	if _, ok := a.Raycast(ray); ok {
		t.Fatal("updated geometry retained the old triangle")
	}
	before := a.meshSoups["mesh"]
	g.Indices[0] = 4
	a.Update(graph)
	if a.meshSoups["mesh"] == before {
		t.Fatal("changed topology reused a stale hierarchy")
	}
	a.Update(NewGraph())
	if a.PrimitiveCount() != 0 || len(a.meshSoups) != 0 || len(a.soups) != 0 || a.previousMeshSoups != nil {
		t.Fatal("removed geometry was retained")
	}
}

func TestSceneAcceleratorSharedPositionsKeepDistinctTopology(t *testing.T) {
	g := updateTestGeometry()
	other := g
	other.Indices = []int{3, 4, 5}
	a := NewSceneAccelerator(NewGraph(Mesh{ID: "first", Geometry: g}, Mesh{ID: "second", Geometry: other}))
	trace := a.Trace(Ray{Origin: Vec3(0, 0, 5), Direction: Vec3(0, 0, -1)})
	if len(trace.Hits) != 1 || trace.Hits[0].ID != "first" {
		t.Fatalf("shared positions reused different indices: %+v", trace.Hits)
	}
}

func TestSceneAcceleratorUpdateMatchesGraphWalk(t *testing.T) {
	a := NewSceneAccelerator(parityGraph(7, 90))
	random := rand.New(rand.NewSource(123))
	for _, graph := range []Graph{parityGraph(11, 45), parityGraph(13, 90), parityGraph(13, 90), NewGraph()} {
		a.Update(graph)
		for i := 0; i < 400; i++ {
			ray := Ray{Origin: Vec3(random.Float64()*40-20, random.Float64()*40-20, random.Float64()*40-20), Direction: Vec3(random.Float64()*2-1, random.Float64()*2-1, random.Float64()*2-1)}
			options := []RaycastOption{PointThreshold(.2), MaxDistance(30)}
			if i%2 == 0 {
				options = append(options, PickableOnly())
			}
			want, got := TraceGraph(graph, ray, options...), a.Trace(ray, options...)
			if len(want.Hits) != len(got.Hits) {
				t.Fatalf("ray %d: hits=%d want=%d", i, len(got.Hits), len(want.Hits))
			}
			for j := range want.Hits {
				assertHitsEqual(t, i, j, got.Hits[j], want.Hits[j])
			}
		}
	}
}

func BenchmarkSceneAcceleratorDecodedMeshUpdate(b *testing.B) {
	var nodes []Node
	for i := 0; i < 28; i++ {
		nodes = append(nodes, Mesh{ID: fmt.Sprint(i), Geometry: updateTestGeometry()})
	}
	graph := NewGraph(nodes...)
	a := NewSceneAccelerator(graph)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.Update(graph)
	}
}

func BenchmarkSceneAcceleratorDecodedMeshRebuild(b *testing.B) {
	var nodes []Node
	for i := 0; i < 28; i++ {
		nodes = append(nodes, Mesh{ID: fmt.Sprint(i), Geometry: updateTestGeometry()})
	}
	graph := NewGraph(nodes...)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		NewSceneAccelerator(graph)
	}
}
