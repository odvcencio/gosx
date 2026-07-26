package bundle

import (
	"math"
	"testing"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene/geom"
)

// This file is the regression test for a silently dropped draw.
//
// normalizePrimitiveKind used to know eight kinds and stop at "torus". A
// torusknot returned the empty kind, primitiveCacheKey returned the empty
// string, ensurePrimitive returned nil, and Frame skipped the draw with no
// diagnostic. The browser drew a knot; the desktop renderer and the headless PNG
// oracle drew nothing, and the honesty gate could not report it, because its
// capability matrix only names webgpu, webgl and canvas2d.

// TestTorusKnotResolvesToRealGeometry covers the whole chain the dropped draw
// broke: the kind name, the cache key, the geometry, and the cull radius.
func TestTorusKnotResolvesToRealGeometry(t *testing.T) {
	for _, spelling := range []string{"torusknot", "torusKnot", "TorusKnotGeometry"} {
		if got := normalizePrimitiveKind(spelling); got != geom.KindTorusKnot {
			t.Fatalf("normalizePrimitiveKind(%q) = %q, want %q", spelling, got, geom.KindTorusKnot)
		}
		params := primitiveParams{Kind: spelling}
		if key := primitiveCacheKey(params); key == "" {
			t.Fatalf("primitiveCacheKey(%q) is empty, so ensurePrimitive would skip the draw", spelling)
		}
		geometry := primitiveForKind(spelling)
		if geometry == nil {
			t.Fatalf("primitiveForKind(%q) returned nil, so the draw disappears", spelling)
		}
		if geometry.vertexCount < 3 || geometry.vertexCount%3 != 0 {
			t.Fatalf("%q: vertexCount %d cannot form triangles", spelling, geometry.vertexCount)
		}
		assertPrimitiveBuffers(t, spelling, geometry)
		if radius := primitiveCullRadius(params); radius <= 0 {
			t.Fatalf("primitiveCullRadius(%q) = %v; a zero radius culls every instance", spelling, radius)
		}
	}
}

// TestTorusKnotVertexCountFollowsItsSegments proves the authored resolution
// reaches the upload. A generator that ignored the numbers would still pass a
// non-nil check.
func TestTorusKnotVertexCountFollowsItsSegments(t *testing.T) {
	coarse := primitiveForParams(primitiveParams{Kind: "torusknot", RadialSegments: 4, TubularSegments: 16})
	fine := primitiveForParams(primitiveParams{Kind: "torusknot", RadialSegments: 8, TubularSegments: 64})
	if coarse == nil || fine == nil {
		t.Fatal("both knots must build")
	}
	if want := 4 * 16 * 6; coarse.vertexCount != want {
		t.Fatalf("coarse vertexCount %d, want %d", coarse.vertexCount, want)
	}
	if want := 8 * 64 * 6; fine.vertexCount != want {
		t.Fatalf("fine vertexCount %d, want %d", fine.vertexCount, want)
	}
	if primitiveCacheKey(primitiveParams{Kind: "torusknot", TubularSegments: 16}) ==
		primitiveCacheKey(primitiveParams{Kind: "torusknot", TubularSegments: 64}) {
		t.Fatal("two knot resolutions share one cache key, so one would draw the other's buffers")
	}
}

// TestTorusKnotWindsCounterClockwise proves the native pipeline can draw the
// knot at all. The pipelines use CullBack with FrontFaceCCW, so a knot wound the
// other way would show its far wall instead of its near one.
func TestTorusKnotWindsCounterClockwise(t *testing.T) {
	geometry := primitiveForParams(primitiveParams{Kind: "torusknot", RadialSegments: 6, TubularSegments: 24})
	for triangle := 0; triangle*9+9 <= len(geometry.positions); triangle++ {
		base := triangle * 9
		read := func(offset int) [3]float32 {
			return [3]float32{
				geometry.positions[base+offset],
				geometry.positions[base+offset+1],
				geometry.positions[base+offset+2],
			}
		}
		p0, p1, p2 := read(0), read(3), read(6)
		face := triangleNormal(p0, p1, p2)
		shaded := [3]float32{
			geometry.normals[base] + geometry.normals[base+3] + geometry.normals[base+6],
			geometry.normals[base+1] + geometry.normals[base+4] + geometry.normals[base+7],
			geometry.normals[base+2] + geometry.normals[base+5] + geometry.normals[base+8],
		}
		dot := float64(face[0]*shaded[0] + face[1]*shaded[1] + face[2]*shaded[2])
		if dot <= 0 {
			t.Fatalf("triangle %d is wound against its own normals (dot %.4f)", triangle, dot)
		}
	}
}

// TestTorusKnotUploadsThroughTheRenderer proves the resource path builds real
// buffers, not just a CPU mesh.
func TestTorusKnotUploadsThroughTheRenderer(t *testing.T) {
	device := newFakeDevice()
	renderer, err := New(Config{Device: device, Surface: fakeSurface{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mesh := engine.RenderInstancedMesh{
		Kind: "torusknot", Radius: 1, Tube: 0.25, RadialSegments: 6, TubularSegments: 24,
		InstanceCount: 1, Transforms: knotIdentityTransform(),
	}
	resources, err := renderer.ensurePrimitiveForMesh(mesh)
	if err != nil {
		t.Fatalf("ensurePrimitiveForMesh: %v", err)
	}
	if resources == nil {
		t.Fatal("the renderer produced no primitive resources for a torus knot")
	}
	if want := 6 * 24 * 6; resources.vertexCount != want {
		t.Fatalf("uploaded vertexCount %d, want %d", resources.vertexCount, want)
	}
	// A second request must hit the cache instead of uploading again.
	again, err := renderer.ensurePrimitiveForMesh(mesh)
	if err != nil {
		t.Fatalf("second ensurePrimitiveForMesh: %v", err)
	}
	if again != resources {
		t.Fatal("the second request rebuilt the knot instead of reusing the cache")
	}
}

// TestEveryNamedKindBuilds walks the whole kind table. A kind that normalizes to
// a name but builds nothing is the same defect with a different label.
func TestEveryNamedKindBuilds(t *testing.T) {
	kinds := []string{
		geom.KindCube, geom.KindBox, geom.KindPlane, geom.KindPyramid,
		geom.KindSphere, geom.KindCylinder, geom.KindCone, geom.KindTorus, geom.KindTorusKnot,
	}
	for _, kind := range kinds {
		if got := normalizePrimitiveKind(kind); got != kind {
			t.Fatalf("normalizePrimitiveKind(%q) = %q; the canonical name must map to itself", kind, got)
		}
		params := primitiveParams{Kind: kind}
		if primitiveCacheKey(params) == "" {
			t.Fatalf("%q has no cache key", kind)
		}
		geometry := primitiveForKind(kind)
		if geometry == nil || geometry.vertexCount == 0 {
			t.Fatalf("%q built no geometry", kind)
		}
		radius := primitiveCullRadius(params)
		if radius <= 0 {
			t.Fatalf("%q reported the cull radius %v", kind, radius)
		}
		// The cull radius must hold every vertex, or an instance blinks out while
		// it is still on screen.
		for i := 0; i+3 <= len(geometry.positions); i += 3 {
			x, y, z := geometry.positions[i], geometry.positions[i+1], geometry.positions[i+2]
			reach := float32(math.Sqrt(float64(x*x + y*y + z*z)))
			if reach > radius {
				t.Fatalf("%q: vertex %d reaches %v, past the cull radius %v", kind, i/3, reach, radius)
			}
		}
	}
	if got := primitiveCullRadius(primitiveParams{Kind: "nosuchkind"}); got != 2 {
		t.Fatalf("an unknown kind reported the cull radius %v, want the safe fallback 2", got)
	}
}

func knotIdentityTransform() []float64 {
	return []float64{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}
