# Task: add typed Scene3D ParametricGeometry

You are the acting implementation agent. Inspect the repository, implement this
bounded vertical slice directly with your tools, run the required focused
checks, and leave the working tree uncommitted for independent review. Do not
commit, push, edit generated assets, or broaden the task.

Repository: GoSX, an MIT-licensed Go framework. Add the expressive surface
generator provided by Three.js's ParametricGeometry addon without adding any
browser runtime code. Work only in these four new files:

1. `scene/geom/parametric.go`
2. `scene/geom/parametric_test.go`
3. `scene/parametric_geometry.go`
4. `scene/parametric_geometry_test.go`

Do not edit any existing file. Reuse the canonical server-generated indexed
`BufferGeometry` path established in `scene/generators.go`, and study the
existing `scene/geom` generators before choosing implementation details.

## Public contract

Add:

```go
type ParametricSurface func(u, v float64) Vector3

func ParametricGeometry(surface ParametricSurface, slices, stacks int) BufferGeometry
```

The wrapper adapts the callback to the canonical `scene/geom` generator and
uses the existing unexported `bufferFromMesh` and `generatorAttributes`
helpers. A nil surface returns a zero-value BufferGeometry. Do not add a new
SceneIR kind, renderer branch, TypeScript generator, generated asset, or
dependency.

## Internal contract

Add an idiomatic callback type and:

```go
func Parametric(surface SurfaceFunc, slices, stacks int, want Attribute) *Mesh
func ParametricVertexCount(slices, stacks int) int
```

Requirements:

- The callback receives normalized `u` and `v` in [0,1] and returns one XYZ
  position. Choose an internal callback signature that keeps the public wrapper
  small and allocation-free per sample.
- A nil callback returns nil. If any sampled coordinate is NaN or Inf, return
  nil and never emit a partial or non-finite mesh.
- Slices and stacks each default to 8 and clamp to [1, 512] through existing
  `ClampInt`.
- Sample the callback exactly once for every grid point, in stable row-major
  order from `(0,0)` through `(1,1)`. This makes callback cost and determinism
  auditable.
- Emit `(stacks+1)*(slices+1)` shared vertices. Edge rows/columns remain
  distinct even when a periodic surface maps them to identical positions, so
  UV seams stay honest.
- UV is exactly `(u,v)` for every grid vertex.
- Derive normals from finite differences over the sampled grid, not extra
  callback calls: centered differences inside and one-sided differences at
  boundaries. The normal orientation is `du × dv`, matching indexed winding.
  Use a deterministic finite fallback at singular/degenerate samples; never
  emit NaN or Inf.
- Emit two indexed triangles per cell, matching the `du × dv` orientation.
  Regular surfaces must have non-degenerate triangles; singularities caused by
  a caller's surface are permitted but must remain finite.
- Output must be deeply deterministic.
- Only allocate normal/UV output streams selected by `want`; `PositionsOnly`
  leaves `Mesh.Normals` and `Mesh.UVs` nil.
- `ParametricVertexCount` applies the same defaults/clamps and exactly matches
  generator output.

Prefer the existing `newBuilder`/`vertex` pattern. Do not create a second mesh
representation. A temporary sampled position grid inside the generator is
expected and should be sized from the resolved vertex count.

## Evidence required

Tests must be substantive and deterministic. At minimum prove:

- default, minimum, explicit, and clamped grid/index counts;
- callback inputs cover exact endpoints in stable row-major order and the
  callback is invoked exactly once per vertex;
- a tilted plane has exact positions/UVs, finite unit normals with the expected
  `du × dv` orientation, outward/consistent winding, and non-degenerate cells;
- a periodic cylinder-like surface has exact duplicated U seam positions and
  normals while U remains 0 versus 1;
- nil and non-finite callback output rejection;
- a local singular sample still produces finite deterministic normals;
- `PositionsOnly` omits normals and UVs;
- repeated calls are deeply deterministic;
- the public wrapper produces indexed BufferGeometry and existing lowering and
  exact triangle raycasting report expected results.

Use table-driven tests where useful. Keep the implementation focused and
idiomatic. Run and report:

```sh
gofmt -w scene/geom/parametric.go scene/geom/parametric_test.go scene/parametric_geometry.go scene/parametric_geometry_test.go
go test ./scene/geom ./scene
go vet ./scene/geom ./scene
git diff --check
```
