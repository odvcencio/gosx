# Task: add typed Scene3D CapsuleGeometry

You are the acting implementation agent. Inspect the repository, implement this
bounded vertical slice directly with your tools, run the required focused
checks, and leave the working tree uncommitted for independent review. Do not
commit, push, edit generated assets, or broaden the task.

Repository: GoSX, an MIT-licensed Go framework. The goal is useful Three.js
parity without adding browser runtime bytes. Work only in these four new files:

1. `scene/geom/capsule.go`
2. `scene/geom/capsule_test.go`
3. `scene/capsule_geometry.go`
4. `scene/capsule_geometry_test.go`

Do not edit any existing file. Reuse the canonical server-generated indexed
`BufferGeometry` path established in `scene/generators.go`, and study the
existing `scene/geom` generators before choosing implementation details.

## Public contract

Add:

```go
type CapsuleGeometryOptions struct {
    Radius         float64
    Length         float64
    CapSegments    int
    RadialSegments int
}

func CapsuleGeometry(opts CapsuleGeometryOptions) BufferGeometry
```

`Length` is the straight cylindrical body length along Y, matching Three.js
CapsuleGeometry terminology. The capsule is centered at the origin. Its total Y
extent is `Length + 2*Radius`.

The wrapper must call the canonical `scene/geom` generator and use the existing
unexported `bufferFromMesh` and `generatorAttributes` helpers. It must not add a
new SceneIR kind, renderer branch, TypeScript generator, or generated asset.

## Internal contract

Add:

```go
func Capsule(radius, length float64, capSegments, radialSegments int, want Attribute) *Mesh
func CapsuleVertexCount(capSegments, radialSegments int) int
```

Requirements:

- Resolve a non-positive or non-finite radius to 1 using existing `PositiveOr`.
- Resolve a non-positive or non-finite body length to 1. This slice deliberately
  avoids a zero-length body so its two equator rings never form degenerate
  triangles.
- Cap segments default to 4 and clamp to [1, 64]. Radial segments default to 8
  and clamp to [3, 128] through existing `ClampInt`.
- Build a Y-axis capsule from a bottom pole, bottom hemisphere latitude rings,
  two distinct equator rings separated by `Length`, top hemisphere latitude
  rings, and a top pole. Do not emit coincident body rings.
- Every non-pole ring has `radialSegments+1` vertices so U=0 and U=1 remain
  distinct seam vertices. A pole may be represented however best preserves
  finite UVs and non-degenerate indexed triangles.
- Positions, unit normals, and UVs must all be finite. Normals point outward.
  UV U runs 0..1 around the capsule and UV V runs monotonically 0..1 from the
  bottom pole to the top pole.
- Emit indexed counter-clockwise/outward, non-degenerate triangles. There must
  be no positional crack at the longitude seam or either cap/body join.
- Output must be deeply deterministic.
- Only allocate normal/UV output streams selected by `want`; `PositionsOnly`
  leaves `Mesh.Normals` and `Mesh.UVs` nil.
- `CapsuleVertexCount` applies the same segment defaults/clamps and must exactly
  match the generator's shared vertex count.

Prefer extending the existing `newBuilder`/`vertex` pattern locally in the new
file. Do not add a second mesh representation or renderer-specific path.

## Evidence required

Tests must be substantive and deterministic. At minimum prove:

- default, minimum, explicit, and clamped segment vertex/index counts;
- exact Y bounds and radial extent for custom radius and length;
- finite positions/normals/UVs, unit outward normals, UV bounds/endpoints, and
  exact longitude seams;
- outward winding and strictly non-degenerate area for every triangle;
- cap/body join positions are continuous without coincident adjacent rings;
- invalid radius/length default deterministically;
- `PositionsOnly` omits normals and UVs;
- repeated calls are deeply deterministic;
- the public wrapper produces indexed `BufferGeometry`, exact triangle
  raycasting hits the cap and body while missing outside, and existing lowering
  expansion reports the expected drawn vertex count.

Use table-driven tests where useful. Keep the implementation focused and
idiomatic. Run and report:

```sh
gofmt -w scene/geom/capsule.go scene/geom/capsule_test.go scene/capsule_geometry.go scene/capsule_geometry_test.go
go test ./scene/geom ./scene
go vet ./scene/geom ./scene
git diff --check
```
