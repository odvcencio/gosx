package scene

import (
	"math"
	"math/rand"
	"testing"
)

// parityGraph mixes every raycastable node kind and every geometry that owns an
// exact narrow phase, so the accelerator and the graph walk must agree over the
// full coverage surface.
func parityGraph(seed int64, count int) Graph {
	random := rand.New(rand.NewSource(seed))
	position := func() Vector3 {
		return Vec3(random.Float64()*20-10, random.Float64()*20-10, random.Float64()*20-10)
	}
	notPickable := false
	nodes := []Node{}
	for i := 0; i < count; i++ {
		switch i % 9 {
		case 6:
			// Torus and pyramid: exact quartic and exact triangles.
			nodes = append(nodes,
				Mesh{
					ID:       "ring",
					Geometry: TorusGeometry{Radius: 0.9, Tube: 0.3},
					Position: position(),
					Rotation: Euler{X: random.Float64(), Z: random.Float64()},
				},
				Mesh{
					ID:       "spire",
					Geometry: PyramidGeometry{Width: 1.2, Height: 1.8, Depth: 0.9},
					Position: position(),
					Rotation: Euler{Y: random.Float64()},
					Scale:    Vec3(1, 1+random.Float64(), 1),
				},
			)
			continue
		case 7:
			// A polyline picks through its stroke threshold, so it also exercises
			// the pick radius the accelerator pads its boxes with.
			nodes = append(nodes, Mesh{
				ID: "wire",
				Geometry: LinesGeometry{
					Points:   []Vector3{Vec3(-1, 0, 0), Vec3(1, 0.4, 0.2), Vec3(0.5, 1.2, -0.8), Vec3(-0.8, 0.3, 0.9)},
					Segments: [][2]int{{0, 1}, {1, 2}, {2, 3}},
				},
				Position: position(),
				Rotation: Euler{Y: random.Float64()},
				Scale:    Vec3(2, 2, 2),
			})
			continue
		case 8:
			// A tessellated knot and an authored triangle mesh. The accelerator
			// answers the triangle mesh through a per-geometry hierarchy and the walk
			// answers it in triangle order, so this pins that they agree.
			nodes = append(nodes,
				Mesh{
					ID:       "knot",
					Geometry: TorusKnotGeometry{Radius: 1.1, Tube: 0.25, RadialSegments: 6, TubularSegments: 24},
					Position: position(),
					Rotation: Euler{X: random.Float64(), Y: random.Float64()},
				},
				Mesh{
					ID:       "panel",
					Geometry: parityBuffer,
					Position: position(),
					Rotation: Euler{Z: random.Float64()},
				},
				Mesh{
					ID:       "deck",
					Geometry: parityPolygon,
					Position: position(),
				},
			)
			continue
		}
		switch i % 6 {
		case 0:
			nodes = append(nodes, Mesh{ID: "sphere", Geometry: SphereGeometry{Radius: 0.5 + random.Float64()}, Position: position()})
		case 1:
			nodes = append(nodes, Mesh{
				ID:       "box",
				Geometry: BoxGeometry{Width: 1, Height: 2, Depth: 0.5},
				Position: position(),
				Rotation: Euler{X: random.Float64(), Y: random.Float64(), Z: random.Float64()},
				Scale:    Vec3(1+random.Float64(), 1, 1),
			})
		case 2:
			nodes = append(nodes, Group{
				Position: position(),
				Rotation: Euler{Y: random.Float64()},
				Children: []Node{
					Mesh{ID: "child", Geometry: CylinderGeometry{RadiusTop: 0.4, RadiusBottom: 0.6, Height: 1.5}, Position: position()},
					Mesh{ID: "blocked", Geometry: CubeGeometry{Size: 1}, Position: position(), Pickable: &notPickable},
				},
			})
		case 3:
			positions := make([]Vector3, 8)
			for p := range positions {
				positions[p] = position()
			}
			nodes = append(nodes, Points{ID: "cloud", Count: len(positions), Positions: positions, Position: position()})
		case 4:
			instancePositions := make([]Vector3, 12)
			instanceScales := make([]Vector3, 12)
			instanceRotations := make([]Euler, 12)
			for p := range instancePositions {
				instancePositions[p] = position()
				instanceScales[p] = Vec3(0.5+random.Float64(), 0.5+random.Float64(), 0.5+random.Float64())
				instanceRotations[p] = Euler{Z: random.Float64()}
			}
			nodes = append(nodes, InstancedMesh{
				ID:        "instances",
				Count:     len(instancePositions),
				Geometry:  SphereGeometry{Radius: 0.4},
				Positions: instancePositions,
				Rotations: instanceRotations,
				Scales:    instanceScales,
			})
		default:
			nodes = append(nodes,
				Sprite{ID: "badge", Position: position(), Scale: 1 + random.Float64()},
				Decal{ID: "mark", Width: 2, Height: 2, Position: position(), Rotation: Euler{X: random.Float64()}},
				Model{ID: "prop", Src: "/prop.glb", Bounds: 1 + random.Float64(), Position: position()},
			)
		}
	}
	return Graph{Nodes: nodes}
}

// parityBuffer is a folded strip of authored triangles. Two of them share an
// edge, so a ray along that edge has to break its tie the same way in both paths.
var parityBuffer = BufferGeometry{
	Positions: []float64{
		-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0,
		-1, 1, 0, 1, 1, 0, 1, 1, -1.5, -1, 1, -1.5,
	},
	Indices: []int{0, 1, 2, 0, 2, 3, 4, 5, 6, 4, 6, 7},
}

// parityPolygon is an earcut triangulation with a hole, which is the case
// PolygonGeometry produces.
var parityPolygon = PolygonGeometry(
	[]float64{-1.5, -1.5, 1.5, -1.5, 1.5, 1.5, -1.5, 1.5},
	[][]float64{{-0.4, -0.4, -0.4, 0.4, 0.4, 0.4, 0.4, -0.4}},
	0,
)

func TestSceneAcceleratorMatchesGraphWalk(t *testing.T) {
	graph := parityGraph(7, 90)
	accel := NewSceneAccelerator(graph)
	if accel.PrimitiveCount() == 0 {
		t.Fatal("accelerator holds no primitives")
	}
	random := rand.New(rand.NewSource(11))
	seen := map[string]int{}
	for i := 0; i < 1500; i++ {
		ray := Ray{
			Origin:    Vec3(random.Float64()*40-20, random.Float64()*40-20, random.Float64()*40-20),
			Direction: Vec3(random.Float64()*2-1, random.Float64()*2-1, random.Float64()*2-1),
		}
		if normalizeVector(ray.Direction) == (Vector3{}) {
			continue
		}
		options := []RaycastOption{}
		if i%3 == 0 {
			options = append(options, PickableOnly())
		}
		if i%5 == 0 {
			options = append(options, MaxDistance(15))
		}
		want := TraceGraph(graph, ray, options...)
		got := accel.Trace(ray, options...)
		if len(want.Hits) != len(got.Hits) {
			t.Fatalf("ray %d: hit count = %d, want %d", i, len(got.Hits), len(want.Hits))
		}
		for h := range want.Hits {
			assertHitsEqual(t, i, h, got.Hits[h], want.Hits[h])
			seen[want.Hits[h].Kind+"/"+want.Hits[h].Method]++
		}
		if (want.Closest == nil) != (got.Closest == nil) {
			t.Fatalf("ray %d: closest presence mismatch", i)
		}
		if want.Closest != nil {
			assertHitsEqual(t, i, -1, *got.Closest, *want.Closest)
		}
	}

	// Parity over paths nothing reached would prove nothing. Require every exact
	// narrow phase to answer at least one ray.
	required := []string{
		"sphere/analytic-sphere",
		"box/analytic-aabb",
		"cube/analytic-aabb",
		"plane/analytic-plane",
		"cylinder/analytic-frustum",
		"torus/analytic-torus",
		"pyramid/analytic-pyramid",
		"lines/line-threshold",
		"torusknot/tessellated-triangle",
		"gltf-mesh/mesh-triangle",
		"points/point-threshold",
		"sprite/point-threshold",
		"model/bounds-aabb",
	}
	for _, path := range required {
		if seen[path] == 0 {
			t.Fatalf("no ray reached %s; the parity graph covers %#v", path, seen)
		}
	}
	t.Logf("hits per exact path: %#v", seen)
}

func assertHitsEqual(t *testing.T, ray, index int, got, want RayHit) {
	t.Helper()
	if got.ID != want.ID || got.Kind != want.Kind || got.Method != want.Method {
		t.Fatalf("ray %d hit %d: identity = %#v, want %#v", ray, index, got, want)
	}
	if math.Abs(got.Distance-want.Distance) > 1e-9 {
		t.Fatalf("ray %d hit %d: distance = %v, want %v", ray, index, got.Distance, want.Distance)
	}
	if (got.InstanceIndex == nil) != (want.InstanceIndex == nil) {
		t.Fatalf("ray %d hit %d: instance index presence mismatch", ray, index)
	}
	if got.InstanceIndex != nil && *got.InstanceIndex != *want.InstanceIndex {
		t.Fatalf("ray %d hit %d: instance index = %d, want %d", ray, index, *got.InstanceIndex, *want.InstanceIndex)
	}
	if got.Pickable != want.Pickable {
		t.Fatalf("ray %d hit %d: pickable = %v, want %v", ray, index, got.Pickable, want.Pickable)
	}
}

func TestSceneAcceleratorMatchesLargerPointThreshold(t *testing.T) {
	// The accelerator sizes its boxes with the build threshold. A query that
	// raises the threshold must pad the box tests, or it would drop hits.
	graph := NewGraph(Points{
		ID:        "cloud",
		Count:     4,
		Positions: []Vector3{Vec3(0.9, 0, 0), Vec3(-0.9, 0, 0), Vec3(0, 0.9, 0), Vec3(0, -0.9, 0)},
	})
	accel := NewSceneAccelerator(graph)
	ray := Ray{Origin: Vec3(0, 0, 5), Direction: Vec3(0, 0, -1)}
	want := TraceGraph(graph, ray, PointThreshold(1))
	got := accel.Trace(ray, PointThreshold(1))
	if len(want.Hits) != 4 {
		t.Fatalf("expected the wide threshold to reach four particles, got %d", len(want.Hits))
	}
	if len(got.Hits) != len(want.Hits) {
		t.Fatalf("accelerator hit count = %d, want %d", len(got.Hits), len(want.Hits))
	}
}

func TestSceneAcceleratorReportsBroadphaseWork(t *testing.T) {
	graph := benchInstancedGraph(4096)
	accel := NewSceneAccelerator(graph)
	trace := accel.Trace(Ray{Origin: Vec3(0, 20, 0), Direction: Vec3(0, -1, 0)})
	if trace.PrimitivesTested == 0 {
		t.Fatal("expected at least one exact instance test")
	}
	if trace.PrimitivesTested > 64 {
		t.Fatalf("BVH must cut the exact tests well below 4096, got %d", trace.PrimitivesTested)
	}
	if trace.BroadphaseRejected != accel.PrimitiveCount()-trace.PrimitivesTested {
		t.Fatalf("broadphase telemetry does not add up: %#v", trace)
	}
}

func TestSceneAcceleratorHandlesEmptyGraph(t *testing.T) {
	accel := NewSceneAccelerator(Graph{})
	trace := accel.Trace(Ray{Origin: Vec3(0, 0, 1), Direction: Vec3(0, 0, -1)})
	if trace.Closest != nil || len(trace.Hits) != 0 {
		t.Fatalf("expected no hits from an empty graph, got %#v", trace)
	}
}

func TestSceneAcceleratorHandlesAxisAlignedRayThroughBoxFace(t *testing.T) {
	// A direction component of zero makes the reciprocal slab test divide by
	// zero. The traversal must still find the box.
	graph := NewGraph(Mesh{ID: "wall", Geometry: BoxGeometry{Width: 4, Height: 4, Depth: 1}})
	accel := NewSceneAccelerator(graph)
	hit, ok := accel.Raycast(Ray{Origin: Vec3(0, 0, 5), Direction: Vec3(0, 0, -1)})
	if !ok || hit.ID != "wall" {
		t.Fatalf("axis aligned ray must hit the wall, got %#v ok=%v", hit, ok)
	}
}

func TestBVHTraversalVisitsEveryPrimitiveOnAWideRay(t *testing.T) {
	boxes := make([]aabb, 257)
	for i := range boxes {
		boxes[i] = sphereAABB(Vec3(float64(i), 0, 0), 0.6)
	}
	tree := buildBVH(boxes)
	seen := make(map[int32]bool, len(boxes))
	tree.forEachCandidate(Ray{Origin: Vec3(-10, 0, 0), Direction: Vec3(1, 0, 0)}, 0, 0, func(prim int32) {
		seen[prim] = true
	})
	if len(seen) != len(boxes) {
		t.Fatalf("traversal reached %d of %d boxes", len(seen), len(boxes))
	}
}

func TestPartitionAroundRankSplitsOnTheAxis(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	for trial := 0; trial < 200; trial++ {
		size := 1 + random.Intn(64)
		centers := make([]Vector3, size)
		order := make([]int32, size)
		for i := range centers {
			// Repeat a small value set so ties appear often.
			centers[i] = Vec3(float64(random.Intn(5)), float64(random.Intn(5)), float64(random.Intn(5)))
			order[i] = int32(i)
		}
		random.Shuffle(size, func(i, j int) { order[i], order[j] = order[j], order[i] })
		rank := size / 2
		partitionAroundRank(order, centers, 1, rank)
		pivot := centers[order[rank]].Y
		for i := 0; i < rank; i++ {
			if centers[order[i]].Y > pivot {
				t.Fatalf("trial %d: element %d (%v) exceeds the rank value %v", trial, i, centers[order[i]].Y, pivot)
			}
		}
		for i := rank + 1; i < size; i++ {
			if centers[order[i]].Y < pivot {
				t.Fatalf("trial %d: element %d (%v) falls below the rank value %v", trial, i, centers[order[i]].Y, pivot)
			}
		}
		seen := make(map[int32]bool, size)
		for _, index := range order {
			if seen[index] {
				t.Fatalf("trial %d: partition duplicated index %d", trial, index)
			}
			seen[index] = true
		}
	}
}

func BenchmarkSceneAcceleratorWideNodes1000(b *testing.B) {
	accel := NewSceneAccelerator(benchWideGraph(1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(benchRay)
	}
}

func BenchmarkSceneAcceleratorInstanced10000(b *testing.B) {
	accel := NewSceneAccelerator(benchInstancedGraph(10000))
	ray := Ray{Origin: Vec3(0, 20, 0), Direction: Vec3(0, -1, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(ray)
	}
}

func BenchmarkSceneAcceleratorDeepHierarchy1000(b *testing.B) {
	accel := NewSceneAccelerator(benchDeepGraph(1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = accel.Trace(benchRay)
	}
}

func BenchmarkSceneAcceleratorBuildInstanced10000(b *testing.B) {
	graph := benchInstancedGraph(10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		accelSink = NewSceneAccelerator(graph)
	}
}

var accelSink *SceneAccelerator
