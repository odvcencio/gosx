# Fix Scene3D plane picking orientation

Implement one focused deterministic Scene3D picking fix against exact base commit `4d59d18eae6d2e11c6962f4cfbe03ed6a3dc3d34`.

## Proven defect

The rendered `PlaneGeometry` contract is consistent across Go and both browser paths: the quad lies in the local XZ plane at `y=0`, spans Width on X and Height/Depth on Z, and carries a `+Y` normal.

Native analytic picking violates that contract. `intersectPlane` currently intersects the local XY plane at `z=0`, bounds X/Y, and returns a ±Z normal. A diagnostic test on this exact base proved the mismatch:

```text
--- FAIL: TestProbePlaneRaycastMatchesRenderedXZSurface
    plane_orientation_probe_test.go:11: downward ray misses the rendered XZ plane
```
`decalAsMesh` intentionally lowers Decal to PlaneGeometry exactly as the renderer does, so its existing raycast test currently encodes the same wrong XY orientation and must be corrected with the implementation.

Canopy identifies `raycastGeometry` and `intersectPlane` in `scene/raycast.go`; the relevant tests are in `scene/raycast_coverage_test.go`. PR #257 does not touch either file.

## Required fix

Modify exactly these two existing files:

- `scene/raycast.go`
- `scene/raycast_coverage_test.go`

Requirements:

1. Make `intersectPlane` intersect local `y=0`.
2. Reject rays parallel to the plane using `ray.Direction.Y`.
3. Compute `t = -ray.Origin.Y / ray.Direction.Y` and reject negative `t`.
4. Bound the hit point with Width on X and Height on Z.
5. Return a `+Y` normal for a ray arriving from above and a `-Y` normal for a ray arriving from below, preserving the existing face-forward behavior.
6. Keep method `analytic-plane` and kind `plane`.
7. Update `TestRaycastDecalHitsItsPlane` to exercise the rendered XZ orientation: position the decal along Y, cast along Y, keep the distance assertion, and test an out-of-bounds X/Z ray.
8. Add a compact focused regression test that directly proves:
   - a downward ray hits PlaneGeometry at `y=0` with the expected distance, point, and `+Y` normal;
   - an upward ray from below returns a `-Y` normal;
   - a ray parallel/coplanar to the XZ plane does not report the old phantom XY hit;
   - Width governs X and Height governs Z.
9. Do not modify renderer geometry, TypeScript, generated bundles, other tests, comments unrelated to this invariant, or any other file.
10. Do not redesign the raycaster or add abstractions beyond this two-file correction.

The patch should make these commands green:

```sh
go test ./scene -run '^(TestPlaneRaycastMatchesRenderedXZSurface|TestRaycastDecalHitsItsPlane|TestRaycastDecalHonorsPickableOnly)$' -count=1
go test ./scene -count=1
```

## Output contract

Return only one unified git patch applying to the exact source snapshot below.

- First bytes: `diff --git`.
- No prose, Markdown fence, preamble, summary, TODO, placeholder, ellipsis, or omitted hunk.
- Touch exactly the two permitted existing files.
- Include complete applicable hunks with real surrounding context.
- Do not include this task packet, generated files, commit messages, or commands.

## Exact current source and test context

### scene/raycast.go

```go
		if current != nil {
			raycastPoints(*current, parent, ray, opts, trace)
		}
	case Sprite:
		raycastSprite(current, parent, ray, opts, trace)
	case *Sprite:
		if current != nil {
			raycastSprite(*current, parent, ray, opts, trace)
		}
	case Decal:
		raycastMesh(decalAsMesh(current), parent, ray, opts, trace)
	case *Decal:
		if current != nil {
			raycastMesh(decalAsMesh(*current), parent, ray, opts, trace)
		}
	case Model:
		raycastModel(current, parent, ray, opts, trace)
	case *Model:
		if current != nil {
			raycastModel(*current, parent, ray, opts, trace)
		}
	}
}

// decalAsMesh mirrors graphLowerer.lowerDecal: a Decal renders as a thin plane,
// so the plane intersection is the exact surface the renderer draws.
func decalAsMesh(decal Decal) Mesh {
	width := positiveOr(decal.Width, 1)
	height := positiveOr(decal.Height, width)
	return Mesh{
		ID:       decal.ID,
		Geometry: PlaneGeometry{Width: width, Height: height},
		Position: decal.Position,
		Rotation: decal.Rotation,
		Pickable: decal.Pickable,
	}
}

func raycastNodes(nodes []Node, parent worldTransform, ray Ray, opts RaycastOptions, trace *RayTrace) {
	for _, child := range nodes {
		raycastNode(child, parent, ray, opts, trace)
//
// Every other surface goes through Tessellate, the shared generator in package
// scene/geom. The torus knot is the one built-in body with no closed form, so its
// soup comes from that call; see torusKnotSoup. Adding a parametric geometry with
// no closed form means adding a generator to scene/geom and a case to
// geometryParams, not a second tessellator here. A second copy is what let the
// native renderer draw a different scene than the browser.
func raycastGeometry(geometry Geometry, ray Ray, localThreshold float64) (RayHit, string, bool) {
	switch g := geometry.(type) {
	case SphereGeometry:
		radius := positiveOr(g.Radius, 1)
		hit, ok := intersectSphere(ray, radius)
		hit.Method = "analytic-sphere"
		return hit, "sphere", ok
	case TorusGeometry:
		hit, ok := intersectTorus(ray, torusRadius(g), torusTube(g))
		hit.Method = "analytic-torus"
		return hit, "torus", ok
	case TorusKnotGeometry:
		// The knot has no closed form, so the test runs against the triangles the
		// renderer draws. torusKnotSoup builds them through Tessellate and caches
		// the hierarchy, so the picker and the renderer read one tessellation.
		soup := torusKnotSoup(g)
		hit, ok := soup.intersect(ray)
		hit.Method = "tessellated-triangle"
		return hit, "torusknot", ok
	case LinesGeometry:
		hit, ok := intersectLines(ray, g, localThreshold)
		hit.Method = "line-threshold"
		return hit, "lines", ok
	case BufferGeometry:
		soup := soupOfBuffer(g)
		hit, ok := soup.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *BufferGeometry:
		if g == nil {
			return RayHit{}, "gltf-mesh", false
		}
		soup := soupOfBuffer(*g)
		hit, ok := soup.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *triangleSoup:
		// SceneAccelerator swaps a triangle mesh for its prebuilt hierarchy. The
		// triangles are the same, so the hit is the same as the walk reports.
		hit, ok := g.intersect(ray)
		hit.Method = "mesh-triangle"
		return hit, "gltf-mesh", ok
	case *segmentSoup:
		// The same swap for a polyline. The strokes are the same, and both paths
		// keep the nearest one with the same tie order.
		hit, ok := g.intersect(ray, localThreshold)
		hit.Method = "line-threshold"
		return hit, "lines", ok
	case CubeGeometry:
		size := positiveOr(g.Size, 1)
		hit, ok := intersectAABB(ray, Vector3{X: -size / 2, Y: -size / 2, Z: -size / 2}, Vector3{X: size / 2, Y: size / 2, Z: size / 2})
		hit.Method = "analytic-aabb"
		return hit, "cube", ok
	case BoxGeometry:
		min, max := boxBounds(g.Width, g.Height, g.Depth)
		hit, ok := intersectAABB(ray, min, max)
		hit.Method = "analytic-aabb"
		return hit, "box", ok
	case PlaneGeometry:
		hit, ok := intersectPlane(ray, positiveOr(g.Width, 1), positiveOr(g.Height, 1))
		hit.Method = "analytic-plane"
		return hit, "plane", ok
	case PyramidGeometry:
		hit, ok := intersectPyramid(ray, positiveOr(g.Width, 1), positiveOr(g.Height, 1), positiveOr(g.Depth, 1))
		hit.Method = "analytic-pyramid"
		return hit, "pyramid", ok
	case CylinderGeometry:
		radiusTop, radiusBottom := cylinderRadii(g.RadiusTop, g.RadiusBottom)
		hit, ok := intersectCylinder(ray, radiusTop, radiusBottom, positiveOr(g.Height, 1))
		hit.Method = "analytic-frustum"
		return hit, "cylinder", ok
	default:
		hit, ok := intersectAABB(ray, Vector3{X: -0.5, Y: -0.5, Z: -0.5}, Vector3{X: 0.5, Y: 0.5, Z: 0.5})
		hit.Method = "fallback-aabb"
		return hit, "cube", ok
	}
}

func intersectSphere(ray Ray, radius float64) (RayHit, bool) {
	oc := ray.Origin
	b := dotVector(oc, ray.Direction)
	c := dotVector(oc, oc) - radius*radius
	discriminant := b*b - c
	if discriminant < 0 {
		return RayHit{}, false
	}
	root := math.Sqrt(discriminant)
	t := -b - root
	if t < 0 {
		t = -b + root
	}
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	return RayHit{Distance: t, Point: point, Normal: normalizeVector(point)}, true
}

func intersectPlane(ray Ray, width, height float64) (RayHit, bool) {
	const epsilon = 1e-9
	if math.Abs(ray.Direction.Z) < epsilon {
		return RayHit{}, false
	}
	t := -ray.Origin.Z / ray.Direction.Z
	if t < 0 {
		return RayHit{}, false
	}
	point := addVectors(ray.Origin, scaleVector(ray.Direction, t))
	if math.Abs(point.X) > width/2 || math.Abs(point.Y) > height/2 {
		return RayHit{}, false
	}
	normal := Vector3{Z: 1}
	if ray.Direction.Z > 0 {
		normal.Z = -1
	}
	return RayHit{Distance: t, Point: point, Normal: normal}, true
}

// intersectCylinder solves a finite Y-axis cylinder or truncated cone,
```

### scene/raycast_coverage_test.go

```go
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
```

### scene/geom/primitives.go

```go
	}
	return b.build()
}

// buildPlane produces a quad in the XZ plane at y=0 with a +Y normal. UVs tile
// once over the quad. The winding is clockwise about +Y, which every other
// generator in GoSX uses for a ground-facing surface.
func buildPlane(width, height float64, want Attribute) *Mesh {
	hx := PositiveOr(width, 2) * 0.5
	hz := PositiveOr(height, 2) * 0.5
	corners := [4]vec3{{-hx, 0, -hz}, {hx, 0, -hz}, {hx, 0, hz}, {-hx, 0, hz}}
	cornerUVs := [4]vec2{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	normal := vec3{0, 1, 0}
	color := vec3{0.7, 0.72, 0.75}
	tris := [][3]int{{0, 2, 1}, {0, 3, 2}}
	b := newBuilder(want, 6)
	for _, tri := range tris {
		for _, idx := range tri {
			b.emit(vertex{position: corners[idx], normal: normal, uv: cornerUVs[idx], color: color})
		}
	}
	return b.build()
}

// buildPyramid produces a square pyramid centered on the origin. The base sits
// at -height/2 and the apex at +height/2, which matches the unit envelope of
// the box and the sphere.
func buildPyramid(width, height, depth float64, want Attribute) *Mesh {
	hx := PositiveOr(width, 2) * 0.5
	hy := PositiveOr(height, 2) * 0.5
	hz := PositiveOr(depth, 2) * 0.5
	base := [4]vec3{{-hx, -hy, -hz}, {hx, -hy, -hz}, {hx, -hy, hz}, {-hx, -hy, hz}}
	apex := vec3{0, hy, 0}
```

### client/js/bootstrap-src/12-scene-geometry.ts

```typescript
    // height 0 gave four points that all share z = -depth/2: a zero-area strip
    // instead of a plane. Indices 0, 1, 5 and 4 are the corners that span x
    // and z.
    //
    // The four corners run clockwise about the +y normal, so the fan runs 0, 2, 1
    // and 0, 3, 2. That winds both triangles counter-clockwise seen from above,
    // which is where the +y normal points. generateInstancedPlaneGeometry in
    // 16c-scene-shared-pbr.js measures +1.000000 for the same quad.
    const vertices = planeQuadVertices(object.width, object.depth);
    const out = scenePrimitiveMeshBuilder();
    const normal = { x: 0, y: 1, z: 0 };
    scenePushMeshTriangle(out, vertices[0], vertices[2], vertices[1], normal, { x: 0, y: 1 }, { x: 1, y: 0 }, { x: 1, y: 1 });
    scenePushMeshTriangle(out, vertices[0], vertices[3], vertices[2], normal, { x: 0, y: 1 }, { x: 0, y: 0 }, { x: 1, y: 0 });
    return sceneFinalizePrimitiveMesh(out);
  }

  function sphereTriangleMesh(object) {
    const radius = scenePositiveNumber(object && object.radius, 0.5);
    const segments = scenePrimitiveSegmentResolution(object && object.segments, 32, 6, 128);
    const rings = Math.max(3, Math.floor(segments / 2));
    const out = scenePrimitiveMeshBuilder();
    function point(lat, lon) {
      const theta = Math.PI * lat / rings;
```

### client/js/bootstrap-src/16c-scene-shared-pbr.ts

```typescript

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a plane (quad) with the given width and depth, lying in the XZ plane.
  // 6 vertices (2 triangles), face normal pointing up (+Y).
  function generateInstancedPlaneGeometry(w, d) {
    var hw = w * 0.5, hd = d * 0.5;
    var vertexCount = 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var corners = [[-hw, 0, hd], [hw, 0, hd], [hw, 0, -hd], [-hw, 0, -hd]];
    var cornerUVs = [[0, 0], [1, 0], [1, 1], [0, 1]];
    var triIndices = [0, 1, 2, 0, 2, 3];

    for (var i = 0; i < 6; i++) {
      var ci = triIndices[i];
      var p = corners[ci];
      positions[i * 3] = p[0]; positions[i * 3 + 1] = p[1]; positions[i * 3 + 2] = p[2];
      normals[i * 3] = 0; normals[i * 3 + 1] = 1; normals[i * 3 + 2] = 0;
      uvs[i * 2] = cornerUVs[ci][0]; uvs[i * 2 + 1] = cornerUVs[ci][1];
      tangents[i * 4] = 1; tangents[i * 4 + 1] = 0; tangents[i * 4 + 2] = 0; tangents[i * 4 + 3] = 1;
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a UV sphere with the given radius and segment count.
  function generateInstancedSphereGeometry(radius, segments) {
    var slices = instancedSegmentCount(segments, 32, 3, 256);
```
