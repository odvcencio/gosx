package bundle

import (
	"encoding/binary"
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
)

func TestQueuePickDestroysUnsubmittedStaging(t *testing.T) {
	old := &fakeBuffer{}
	r := &Renderer{
		pendingPick: &pickRequest{
			staging: old,
		},
	}

	r.QueuePick(4, 8, func(uint32) {})

	if !old.destroyed {
		t.Fatal("old unsubmitted staging buffer was not destroyed")
	}
	if r.pendingPick == nil || r.pendingPick.x != 4 || r.pendingPick.y != 8 {
		t.Fatalf("replacement pick = %#v", r.pendingPick)
	}
}

func TestQueuePickRetiresSubmittedStaging(t *testing.T) {
	old := &fakeBuffer{}
	req := &pickRequest{
		staging:     old,
		submitFrame: true,
	}
	r := &Renderer{pendingPick: req}

	r.QueuePick(4, 8, func(uint32) {})

	if old.destroyed {
		t.Fatal("submitted staging buffer was destroyed before readback cleanup")
	}
	if len(r.retiredPicks) != 1 || r.retiredPicks[0] != req {
		t.Fatalf("retired picks = %#v, want old request", r.retiredPicks)
	}
}

func TestQueuePickLeavesInFlightStagingOwnedByReadback(t *testing.T) {
	old := &fakeBuffer{}
	r := &Renderer{
		pendingPick: &pickRequest{
			staging:  old,
			inFlight: true,
		},
	}

	r.QueuePick(4, 8, func(uint32) {})

	if old.destroyed {
		t.Fatal("in-flight staging buffer was destroyed by replacement")
	}
	if len(r.retiredPicks) != 0 {
		t.Fatalf("retired picks = %#v, want none for in-flight request", r.retiredPicks)
	}
}

func TestBuildPickTargetsAssignsStableObjectInstanceIDs(t *testing.T) {
	bases, targets := buildPickTargets([]engine.RenderInstancedMesh{
		{ID: "left", InstanceCount: 2},
		{ID: "empty"},
		{ID: "right", InstanceCount: 1},
	})

	if len(bases) != 3 || bases[0] != 1 || bases[1] != 0 || bases[2] != 3 {
		t.Fatalf("bases = %#v", bases)
	}
	if got := targets[1]; got.ObjectID != "left" || got.ObjectIndex != 0 || got.InstanceIndex != 0 {
		t.Fatalf("target 1 = %#v", got)
	}
	if got := targets[2]; got.ObjectID != "left" || got.ObjectIndex != 0 || got.InstanceIndex != 1 {
		t.Fatalf("target 2 = %#v", got)
	}
	if got := targets[3]; got.ObjectID != "right" || got.ObjectIndex != 2 || got.InstanceIndex != 0 {
		t.Fatalf("target 3 = %#v", got)
	}
}

func TestPickResultForIDFallsBackToNumericIndex(t *testing.T) {
	got := pickResultForID(nil, 7)
	if got.ID != 7 || got.ObjectIndex != 6 || got.InstanceIndex != -1 || got.TriangleIndex != -1 {
		t.Fatalf("fallback result = %#v", got)
	}
	bg := pickResultForID(nil, 0)
	if bg.ID != 0 || bg.ObjectIndex != -1 || bg.InstanceIndex != -1 {
		t.Fatalf("background result = %#v", bg)
	}
}

func TestEnrichPickTargetsWithRayAddsPrimitiveHitMetadata(t *testing.T) {
	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 6, FOV: math.Pi / 2, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "hero",
			Kind:          "box",
			InstanceCount: 1,
			Transforms:    identityTransform(),
		}},
	}
	bases, targets := buildPickTargets(b.InstancedMeshes)
	enrichPickTargetsWithRay(targets, b, bases, nil, nil, 100, 100, 201, 201)

	got := targets[1]
	if got.ObjectID != "hero" || got.ObjectIndex != 0 || got.InstanceIndex != 0 {
		t.Fatalf("identity metadata = %#v", got)
	}
	if got.TriangleIndex < 0 || got.PrimitiveIndex != got.TriangleIndex {
		t.Fatalf("triangle metadata = %#v", got)
	}
	if math.Abs(float64(got.Depth-5)) > 0.001 {
		t.Fatalf("depth = %v, want near 5", got.Depth)
	}
	if math.Abs(float64(got.WorldPosition[0])) > 0.001 ||
		math.Abs(float64(got.WorldPosition[1])) > 0.001 ||
		math.Abs(float64(got.WorldPosition[2]-1)) > 0.001 {
		t.Fatalf("world position = %#v, want center of front face", got.WorldPosition)
	}
	if math.Abs(float64(got.LocalPosition[2]-1)) > 0.001 {
		t.Fatalf("local position = %#v, want z=1", got.LocalPosition)
	}
	if got.UV[0] < 0 || got.UV[0] > 1 || got.UV[1] < 0 || got.UV[1] > 1 {
		t.Fatalf("uv = %#v, want normalized coordinates", got.UV)
	}
}

func TestEnrichPickTargetsWithRayAddsSurfaceUVMetadata(t *testing.T) {
	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 6, FOV: math.Pi / 2, Near: 0.1, Far: 100},
		Surfaces: []engine.RenderSurface{{
			ID:          "panel",
			Positions:   testSurfacePositions(),
			UV:          testSurfaceUV(),
			VertexCount: 6,
		}},
	}
	bases, targets, _ := buildSurfacePickTargets(b.Surfaces, 1, nil)
	enrichPickTargetsWithRay(targets, b, nil, nil, bases, 100, 100, 201, 201)

	got := targets[1]
	if got.ObjectID != "panel" || got.ObjectIndex != 0 || got.InstanceIndex != -1 {
		t.Fatalf("surface metadata = %#v", got)
	}
	if got.TriangleIndex < 0 || got.PrimitiveIndex != got.TriangleIndex {
		t.Fatalf("surface triangle metadata = %#v", got)
	}
	if math.Abs(float64(got.Depth-6)) > 0.001 {
		t.Fatalf("surface depth = %v, want near 6", got.Depth)
	}
	if math.Abs(float64(got.UV[0]-0.5)) > 0.01 || math.Abs(float64(got.UV[1]-0.5)) > 0.01 {
		t.Fatalf("surface uv = %#v, want center", got.UV)
	}
}

func TestInstanceRecordBytesPacksMatrixAndPickID(t *testing.T) {
	transforms := append(identityTransform(), translatedTransform(4, 5, 6)...)
	data := instanceRecordBytes(transforms, 2, 11)
	if len(data) != 2*instanceRecordStride {
		t.Fatalf("record bytes len = %d", len(data))
	}
	if got := binary.LittleEndian.Uint32(data[64:68]); got != 11 {
		t.Fatalf("first pick id = %d, want 11", got)
	}
	if got := binary.LittleEndian.Uint32(data[instanceRecordStride+64 : instanceRecordStride+68]); got != 12 {
		t.Fatalf("second pick id = %d, want 12", got)
	}
	x := math.Float32frombits(binary.LittleEndian.Uint32(data[instanceRecordStride+12*4 : instanceRecordStride+13*4]))
	y := math.Float32frombits(binary.LittleEndian.Uint32(data[instanceRecordStride+13*4 : instanceRecordStride+14*4]))
	z := math.Float32frombits(binary.LittleEndian.Uint32(data[instanceRecordStride+14*4 : instanceRecordStride+15*4]))
	if x != 4 || y != 5 || z != 6 {
		t.Fatalf("second translation = (%v,%v,%v)", x, y, z)
	}
}

func TestEnrichPickTargetsWithRayExposesClickRayOnHitsAndBackground(t *testing.T) {
	b := engine.RenderBundle{
		Camera: engine.RenderCamera{Z: 6, FOV: math.Pi / 2, Near: 0.1, Far: 100},
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID:            "hero",
			Kind:          "box",
			InstanceCount: 1,
			Transforms:    identityTransform(),
		}},
	}
	bases, targets := buildPickTargets(b.InstancedMeshes)
	enrichPickTargetsWithRay(targets, b, bases, nil, nil, 100, 100, 201, 201)

	got := targets[1]
	if math.Abs(float64(got.RayOrigin[0])) > 0.001 || math.Abs(float64(got.RayOrigin[1])) > 0.001 || math.Abs(float64(got.RayOrigin[2]-6)) > 0.001 {
		t.Fatalf("ray origin = %#v, want camera position (0,0,6)", got.RayOrigin)
	}
	if math.Abs(float64(got.RayDirection[0])) > 0.001 || math.Abs(float64(got.RayDirection[1])) > 0.001 || math.Abs(float64(got.RayDirection[2]+1)) > 0.001 {
		t.Fatalf("ray direction = %#v, want straight -Z for the center pixel", got.RayDirection)
	}
	background := pickResultForID(targets, 0)
	if background.ObjectIndex != -1 {
		t.Fatalf("background result = %#v", background)
	}
	if background.RayOrigin != got.RayOrigin || background.RayDirection != got.RayDirection {
		t.Fatalf("background click must still carry the ray: %#v", background)
	}
}

func TestPickRayForOrthographicCameraIsParallel(t *testing.T) {
	cam := engine.RenderCamera{Mode: "orthographic", Z: 10, Left: -4, Right: 4, Top: 3, Bottom: -3, Zoom: 1, Near: 0.1, Far: 100}
	center := pickRayForCamera(cam, 100, 100, 201, 201)
	if math.Abs(float64(center.dir[0])) > 1e-6 || math.Abs(float64(center.dir[1])) > 1e-6 || math.Abs(float64(center.dir[2]+1)) > 1e-6 {
		t.Fatalf("ortho center dir = %#v, want -Z", center.dir)
	}
	if math.Abs(float64(center.origin[2]-10)) > 1e-6 || math.Abs(float64(center.origin[0])) > 1e-4 {
		t.Fatalf("ortho center origin = %#v", center.origin)
	}
	edge := pickRayForCamera(cam, 201, 100, 201, 201)
	if edge.dir != center.dir {
		t.Fatalf("ortho rays must be parallel: %#v vs %#v", edge.dir, center.dir)
	}
	if float64(edge.origin[0]) < 3.5 {
		t.Fatalf("ortho edge origin must offset along +X by ~half width, got %#v", edge.origin)
	}
}
