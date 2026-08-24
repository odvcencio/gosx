# Correct the Scene3D plane-picking patch envelope

Return one corrected, directly applicable unified git patch for the proven
Scene3D `PlaneGeometry` picking-orientation defect. This is the only correction
attempt. Work against exact task commit
`2791fabc2ec61cc838f6c379ba7430a7c7f89f04`, whose source base is
`4d59d18eae6d2e11c6962f4cfbe03ed6a3dc3d34`.

## Prior-response review

The prior response's core implementation semantics were sound and bounded:

- intersect local `y=0` through `Direction.Y` and `Origin.Y`;
- bound Width on X and Height on Z;
- return face-forward `+Y`/`-Y` normals;
- update the existing Decal regression and add one direct PlaneGeometry test;
- touch only the two requested existing files.

Do not repeat or quote the prior response. It was rejected solely as an
unusable patch artifact:

```text
gate: response content does not begin with diff --git
git apply --check: error: corrupt patch at line 6
```

It began with a Markdown fence, repeated the patch three times with prose
between copies, ended with a fence, used symbolic non-unified hunk headers such
as `@@ func ...` / `@@ -func ...`, and included malformed indentation in one
copy. Correct all envelope and applicability failures by generating a fresh
patch from the exact contexts below. Do not reconstruct the earlier bytes.

## Exact permitted files

- `scene/raycast.go`, current blob
  `430b2fae6d74206585e72e1971c3ebf8ab4c07c5`
- `scene/raycast_coverage_test.go`, current blob
  `849b7fb267d717e3d8289d280ccca664c8b42cb5`

Modify exactly those two existing files. No task packets, evidence, generated
bundles, renderer code, or other paths may appear in the patch.

## Required behavior

1. Make `intersectPlane` intersect local `y=0`.
2. Detect parallel rays with `math.Abs(ray.Direction.Y) < epsilon`.
3. Compute `t := -ray.Origin.Y / ray.Direction.Y`; retain negative-t rejection.
4. Bound `point.X` by Width and `point.Z` by Height.
5. Return `Vector3{Y: 1}` from above and face-forward `Vector3{Y: -1}` from below.
6. Preserve `analytic-plane` and `plane` routing.
7. Update `TestRaycastDecalHitsItsPlane` to place the decal along Y, cast along
   Y, retain distance 5, and prove out-of-bounds X and Z misses.
8. Add compact `TestPlaneRaycastMatchesRenderedXZSurface` coverage for a
   downward hit (distance/point/+Y), upward hit (-Y), parallel/coplanar miss,
   Width-on-X miss, and Height-on-Z miss.

The intended focused commands are:

```text
go test ./scene -run '^(TestPlaneRaycastMatchesRenderedXZSurface|TestRaycastDecalHitsItsPlane|TestRaycastDecalHonorsPickableOnly)$' -count=1
go test ./scene -count=1
```

## Strict output contract

Return only one bare unified git patch.

- The first bytes must be exactly `diff --git`.
- Use real numeric unified hunk ranges, for example `@@ -750,20 +750,20 @@`;
  never use symbolic-only headers such as `@@ func ...`.
- Every line in a hunk must have a valid leading context/addition/deletion
  marker and must match the exact source below.
- Omit Markdown fences, prose, notes, self-corrections, duplicate patches,
  commands, summaries, placeholders, and ellipses.
- The complete response must pass `git apply --check` at the exact task commit.
- Do not redesign the raycaster or change unrelated tests.

## Exact current source contexts

### `scene/raycast.go`

```go
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
```

```go
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
// including both end caps. This rejects the large false-positive corner
// regions produced by the former bounding-box approximation.
func intersectCylinder(ray Ray, radiusTop, radiusBottom, height float64) (RayHit, bool) {
```

### `scene/raycast_coverage_test.go`

The file already imports `math`; no import change is needed.

```go
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
```

## Rendered-plane invariant

The shared Go tessellator is authoritative and already pins the intended axis:

```go
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
```
