package scene

import (
	"math"
	"math/rand"
	"testing"
)

func TestRaycastBroadphaseKeepsExactResults(t *testing.T) {
	// A tangent ray grazes the sphere surface. A bounding sphere that understates
	// the geometry would drop this hit, so the test pins the broadphase radius.
	graph := NewGraph(Mesh{ID: "ball", Geometry: SphereGeometry{Radius: 1}})
	ray := Ray{Origin: Vec3(0.999999, 0, 5), Direction: Vec3(0, 0, -1)}
	hit, ok := RaycastGraph(graph, ray)
	if !ok {
		t.Fatal("tangent ray must still hit the sphere")
	}
	if hit.Method != "analytic-sphere" {
		t.Fatalf("expected the exact sphere test to run, got %q", hit.Method)
	}
}

func TestRaycastBroadphaseRejectsOffAxisNodes(t *testing.T) {
	graph := NewGraph(
		Mesh{ID: "left", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: Vec3(-20, 0, -2)},
		Mesh{ID: "right", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: Vec3(20, 0, -2)},
		Mesh{ID: "center", Geometry: BoxGeometry{Width: 1, Height: 1, Depth: 1}, Position: Vec3(0, 0, -2)},
	)
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)})
	if trace.Closest == nil || trace.Closest.ID != "center" {
		t.Fatalf("expected the center box, got %#v", trace.Closest)
	}
	if trace.PrimitivesTested != 1 {
		t.Fatalf("broadphase must leave one exact test, got %d", trace.PrimitivesTested)
	}
	if trace.BroadphaseRejected != 2 {
		t.Fatalf("broadphase must reject two boxes, got %d", trace.BroadphaseRejected)
	}
}

func TestRaycastBroadphaseHonorsMaxDistance(t *testing.T) {
	graph := NewGraph(Mesh{ID: "far", Geometry: SphereGeometry{Radius: 1}, Position: Vec3(0, 0, -50)})
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 0), Direction: Vec3(0, 0, -1)}, MaxDistance(10))
	if trace.Closest != nil {
		t.Fatalf("expected the distance cap to reject the hit, got %#v", trace.Closest)
	}
	if trace.BroadphaseRejected != 1 || trace.PrimitivesTested != 0 {
		t.Fatalf("expected a broadphase rejection, got %#v", trace)
	}
}

func TestRaycastPointsReturnsParticleIndex(t *testing.T) {
	graph := NewGraph(Points{
		ID:        "cloud",
		Count:     3,
		Positions: []Vector3{Vec3(-2, 0, 0), Vec3(0, 0, 0), Vec3(2, 0, 0)},
	})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 5, 0), Direction: Vec3(0, -1, 0)})
	if !ok {
		t.Fatal("expected a particle hit")
	}
	if hit.ID != "cloud" || hit.Kind != "points" || hit.Method != "point-threshold" {
		t.Fatalf("unexpected points hit shape: %#v", hit)
	}
	if hit.InstanceIndex == nil || *hit.InstanceIndex != 1 {
		t.Fatalf("expected particle index 1, got %#v", hit.InstanceIndex)
	}
	if math.Abs(hit.Distance-(5-DefaultPointThreshold)) > 1e-9 {
		t.Fatalf("expected the hit on the threshold sphere surface, got %v", hit.Distance)
	}
}

func TestRaycastPointsThresholdOptionWidensHits(t *testing.T) {
	graph := NewGraph(Points{ID: "cloud", Count: 1, Positions: []Vector3{Vec3(0, 0, 0)}})
	ray := Ray{Origin: Vec3(0.4, 5, 0), Direction: Vec3(0, -1, 0)}
	if _, ok := RaycastGraph(graph, ray); ok {
		t.Fatal("the default threshold must miss a particle 0.4 units off axis")
	}
	if _, ok := RaycastGraph(graph, ray, PointThreshold(0.5)); !ok {
		t.Fatal("a 0.5 threshold must hit a particle 0.4 units off axis")
	}
}

func TestRaycastPointsUsesGroupTransform(t *testing.T) {
	graph := NewGraph(Group{
		Position: Vec3(0, 0, -4),
		Children: []Node{Points{ID: "cloud", Count: 1, Positions: []Vector3{Vec3(0, 0, 0)}}},
	})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected the group transform to move the particle onto the ray")
	}
	if math.Abs(hit.Point.Z-(-4+DefaultPointThreshold)) > 1e-9 {
		t.Fatalf("expected a world-space hit point near z=-3.9, got %#v", hit.Point)
	}
}

func TestRaycastSpriteHitsBillboardCenter(t *testing.T) {
	graph := NewGraph(Sprite{ID: "badge", Position: Vec3(0, 0, -3), Scale: 2})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected a sprite hit")
	}
	if hit.ID != "badge" || hit.Kind != "sprite" {
		t.Fatalf("unexpected sprite hit shape: %#v", hit)
	}
	// Scale 2 doubles the threshold radius, so the surface sits 0.2 units nearer.
	if math.Abs(hit.Distance-(6-2*DefaultPointThreshold)) > 1e-9 {
		t.Fatalf("expected the sprite radius to follow Scale, got %v", hit.Distance)
	}
}

func TestRaycastSpriteSkipsAnchoredSprites(t *testing.T) {
	graph := NewGraph(Sprite{ID: "pinned", Target: "ship", Position: Vec3(0, 0, -3)})
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)})
	if trace.Closest != nil {
		t.Fatalf("anchored sprites must not raycast on their own position, got %#v", trace.Closest)
	}
	if trace.FilteredPrimitives != 1 {
		t.Fatalf("expected one filtered sprite, got %d", trace.FilteredPrimitives)
	}
}

func TestRaycastDecalHitsItsPlane(t *testing.T) {
	graph := NewGraph(Decal{ID: "scorch", Width: 4, Height: 4, Position: Vec3(0, 0, -2)})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected a decal hit through the mesh plane path")
	}
	if hit.ID != "scorch" || hit.Kind != "plane" || hit.Method != "analytic-plane" {
		t.Fatalf("expected the exact plane test, got %#v", hit)
	}
	if math.Abs(hit.Distance-5) > 1e-9 {
		t.Fatalf("expected distance 5, got %v", hit.Distance)
	}
	// A ray outside the 4x4 plane must miss.
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(3, 0, 3), Direction: Vec3(0, 0, -1)}); ok {
		t.Fatal("a ray outside the decal plane must miss")
	}
}

func TestRaycastDecalHonorsPickableOnly(t *testing.T) {
	notPickable := false
	graph := NewGraph(Decal{ID: "scorch", Width: 4, Height: 4, Position: Vec3(0, 0, -2), Pickable: &notPickable})
	if _, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)}, PickableOnly()); ok {
		t.Fatal("a non-pickable decal must be filtered")
	}
}

func TestRaycastModelUsesBoundsBox(t *testing.T) {
	graph := NewGraph(Model{ID: "robot", Src: "/robot.glb", Bounds: 2, Position: Vec3(0, 0, -4)})
	hit, ok := RaycastGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)})
	if !ok {
		t.Fatal("expected a model bounds hit")
	}
	if hit.ID != "robot" || hit.Kind != "model" || hit.Method != "bounds-aabb" {
		t.Fatalf("unexpected model hit shape: %#v", hit)
	}
	if math.Abs(hit.Distance-7) > 1e-9 {
		t.Fatalf("expected distance 7 to the near face, got %v", hit.Distance)
	}
}

// TestBroadphaseNeverDropsANarrowPhaseHit proves the bounding volume test is a
// pure filter. For every mesh and every instance it calls the unchanged narrow
// phase directly, with no broadphase in front, and requires TraceGraph to report
// exactly the same hits. Mesh and InstancedMesh are the two kinds the browser
// pick path also covers, so a false negative here would split native picking
// from GPU picking.
func TestBroadphaseNeverDropsANarrowPhaseHit(t *testing.T) {
	random := rand.New(rand.NewSource(19))
	geometries := []Geometry{
		SphereGeometry{Radius: 0.8},
		BoxGeometry{Width: 1.5, Height: 0.4, Depth: 2.2},
		CubeGeometry{Size: 1.1},
		PlaneGeometry{Width: 2, Height: 3},
		CylinderGeometry{RadiusTop: 0.3, RadiusBottom: 0.9, Height: 1.7},
		TorusGeometry{Radius: 0.7, Tube: 0.2},
		PyramidGeometry{Width: 1, Height: 2, Depth: 1},
	}

	hits, rejects, narrowMisses := 0, 0, 0
	for trial := 0; trial < 3000; trial++ {
		geometry := geometries[random.Intn(len(geometries))]
		position := Vec3(random.Float64()*4-2, random.Float64()*4-2, random.Float64()*4-2)
		rotation := Euler{X: random.Float64() * 6, Y: random.Float64() * 6, Z: random.Float64() * 6}
		scale := Vec3(0.4+random.Float64()*2, 0.4+random.Float64()*2, 0.4+random.Float64()*2)
		ray := Ray{
			Origin:    Vec3(random.Float64()*8-4, random.Float64()*8-4, random.Float64()*8-4),
			Direction: normalizeVector(Vec3(random.Float64()*2-1, random.Float64()*2-1, random.Float64()*2-1)),
		}
		if ray.Direction == (Vector3{}) {
			continue
		}

		// Reference: the narrow phase alone, exactly as it ran before the
		// broadphase existed.
		world := combineTransforms(identityTransform(), localTransform(position, rotation))
		factor := scaleFactor(scale)
		wantHit, wantOK := raycastTransformedGeometry(geometry, world, scale, ray, DefaultPointThreshold/factor)

		mesh := Mesh{ID: "subject", Geometry: geometry, Position: position, Rotation: rotation, Scale: scale}
		trace := TraceGraph(Graph{Nodes: []Node{mesh}}, ray)
		gotOK := trace.Closest != nil
		if gotOK != wantOK {
			radius, strokes := geometryBounds(geometry)
			t.Fatalf("trial %d: mesh hit = %v, want %v (geometry %T, radius %v)",
				trial, gotOK, wantOK, geometry, radius*factor+strokes*DefaultPointThreshold)
		}
		if wantOK && math.Abs(trace.Closest.Distance-wantHit.Distance) > 1e-12 {
			t.Fatalf("trial %d: mesh distance = %v, want %v", trial, trace.Closest.Distance, wantHit.Distance)
		}
		switch {
		case wantOK:
			hits++
		case trace.BroadphaseRejected > 0:
			rejects++
		default:
			narrowMisses++
		}

		// The same subject as a single instance must behave identically.
		instanced := InstancedMesh{
			ID:        "subject",
			Count:     1,
			Geometry:  geometry,
			Positions: []Vector3{position},
			Rotations: []Euler{rotation},
			Scales:    []Vector3{scale},
		}
		instancedTrace := TraceGraph(Graph{Nodes: []Node{instanced}}, ray)
		if (instancedTrace.Closest != nil) != wantOK {
			t.Fatalf("trial %d: instance hit = %v, want %v", trial, instancedTrace.Closest != nil, wantOK)
		}
		if wantOK && math.Abs(instancedTrace.Closest.Distance-wantHit.Distance) > 1e-12 {
			t.Fatalf("trial %d: instance distance = %v, want %v", trial, instancedTrace.Closest.Distance, wantHit.Distance)
		}
	}

	// Require a real mix. Without this the test could pass while every ray took
	// one branch, which would prove nothing about the filter.
	t.Logf("hits=%d broadphase rejects=%d narrow-phase misses=%d", hits, rejects, narrowMisses)
	if hits < 100 || rejects < 100 || narrowMisses < 100 {
		t.Fatalf("weak trial mix: hits=%d rejects=%d narrowMisses=%d", hits, rejects, narrowMisses)
	}
}

// TestRaycastCoverageManifest pins which node kinds the native raycast reaches,
// and records where the browser pick path disagrees. Keep this table in step
// with sceneRaycastPick in client/js/bootstrap-src/17-scene-input.ts. The
// WebGPU GPU picker resolves identity on the GPU and then calls the same two
// shared helpers (sceneRaycastPickGroup, sceneRaycastPickInstancedMeshes) for
// every geometric field, so whatever those helpers miss, GPU picking misses.
func TestRaycastCoverageManifest(t *testing.T) {
	cases := []struct {
		name string
		node Node
		// nativeHit records whether scene.TraceGraph reaches this kind.
		nativeHit bool
		// browserPick records whether sceneRaycastPick reaches it in the browser.
		browserPick bool
		// note explains a difference between the two columns.
		note string
	}{
		{
			name:        "mesh",
			node:        Mesh{ID: "m", Geometry: BoxGeometry{Width: 2, Height: 2, Depth: 2}, Position: Vec3(0, 0, -3)},
			nativeHit:   true,
			browserPick: true,
			note:        "native runs the analytic box test; the browser runs per-triangle tests over world positions",
		},
		{
			name:        "instanced mesh",
			node:        InstancedMesh{ID: "i", Count: 1, Geometry: SphereGeometry{Radius: 1}, Positions: []Vector3{Vec3(0, 0, -3)}},
			nativeHit:   true,
			browserPick: true,
		},
		{
			name:        "decal",
			node:        Decal{ID: "d", Width: 4, Height: 4, Position: Vec3(0, 0, -3)},
			nativeHit:   true,
			browserPick: true,
			note:        "a decal lowers to a plane mesh, so the browser already picked it; native now matches",
		},
		{
			name:        "points",
			node:        Points{ID: "p", Count: 1, Positions: []Vector3{Vec3(0, 0, -3)}},
			nativeHit:   true,
			browserPick: true,
			note:        "sceneRaycastPick calls sceneRaycastPickPoints, which tests every particle at the same radius; client/js/17-scene-input-pick.test.mjs pins the distance to the figure this walk returns",
		},
		{
			name:        "sprite",
			node:        Sprite{ID: "s", Position: Vec3(0, 0, -3)},
			nativeHit:   true,
			browserPick: true,
			note:        "a sprite answers a ray through the same helper, and its DOM overlay still takes pointer events",
		},
		{
			name:        "model with bounds",
			node:        Model{ID: "mo", Src: "/a.glb", Bounds: 2, Position: Vec3(0, 0, -3)},
			nativeHit:   true,
			browserPick: true,
			note:        "native tests the Bounds box; the browser tests the loaded glTF triangles, so native is coarser",
		},
		{
			name:        "model without bounds",
			node:        Model{ID: "mo", Src: "/a.glb", Position: Vec3(0, 0, -3)},
			nativeHit:   false,
			browserPick: true,
			note:        "no Bounds means no known world extent in Go; the browser has the loaded geometry",
		},
		{
			name:        "label",
			node:        Label{ID: "l", Text: "hello", Position: Vec3(0, 0, -3)},
			nativeHit:   false,
			browserPick: false,
			note:        "a label is DOM text with no world extent on either side",
		},
	}

	ray := Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			trace := TraceGraph(Graph{Nodes: []Node{testCase.node}}, ray)
			got := trace.Closest != nil
			if got != testCase.nativeHit {
				t.Fatalf("native hit = %v, want %v (%s)", got, testCase.nativeHit, testCase.note)
			}
			// The accelerator must agree with the walk on every kind.
			accel := NewSceneAccelerator(Graph{Nodes: []Node{testCase.node}})
			if _, ok := accel.Raycast(ray); ok != testCase.nativeHit {
				t.Fatalf("accelerator hit = %v, want %v", ok, testCase.nativeHit)
			}
		})
	}
}

func TestRaycastModelWithoutBoundsIsNotPickable(t *testing.T) {
	graph := NewGraph(Model{ID: "robot", Src: "/robot.glb", Position: Vec3(0, 0, -4)})
	trace := TraceGraph(graph, Ray{Origin: Vec3(0, 0, 4), Direction: Vec3(0, 0, -1)})
	if trace.Closest != nil {
		t.Fatalf("a model without Bounds has no known world extent, got %#v", trace.Closest)
	}
	if trace.FilteredPrimitives != 1 {
		t.Fatalf("expected one filtered model, got %d", trace.FilteredPrimitives)
	}
}
