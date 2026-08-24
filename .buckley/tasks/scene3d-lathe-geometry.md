# Task: add typed Scene3D LatheGeometry

Return one directly applicable Git unified diff and nothing else. The first
bytes must be `diff --git `, do not wrap the diff in Markdown fences, and end
the response with a newline.

Repository: GoSX, an MIT-licensed Go web framework. This slice makes Scene3D a
more credible Three.js replacement by closing one concrete geometry gap. Work
only in the four new files named below. Do not change existing files, generated
runtime assets, documentation, module files, or public APIs outside this task.

Implement these four new files:

1. `scene/geom/lathe.go`
2. `scene/geom/lathe_test.go`
3. `scene/lathe_geometry.go`
4. `scene/lathe_geometry_test.go`

The public authoring contract in package `scene` is:

```go
type LathePoint struct {
    Radius float64
    Y      float64
}

func LatheGeometry(profile []LathePoint, segments int, phiStart, phiLength float64) BufferGeometry
```

`LatheGeometry` revolves the authored radius/Y profile around the Y axis and
returns an indexed `BufferGeometry` with positions, normals, UVs, and indices.
It should convert the points to a flat radius/Y list and call the canonical
generator in `scene/geom`; it must use the existing unexported
`bufferFromMesh` and `generatorAttributes` helpers in package `scene` so the
result follows the existing BufferGeometry honesty path.

The internal contract in package `geom` is:

```go
func Lathe(profile []float64, segments int, phiStart, phiLength float64, want Attribute) *Mesh
func LatheVertexCount(profilePoints, segments int) int
```

The flat profile alternates radius, Y. Requirements:

- Reject a profile with fewer than two points, odd length, non-finite values,
  or negative radii by returning nil. Never emit NaN or Inf.
- Resolve `segments` with the existing `ClampInt`: default 12, minimum 3,
  maximum 512.
- Resolve `phiLength` like other swept generators: non-positive or non-finite
  means a full `2*math.Pi` turn; values above a full turn clamp to a full turn.
- Emit `(segments+1) * profilePointCount` vertices so the angular seam has
  separate U=0 and U=1 vertices.
- Emit `segments * (profilePointCount-1) * 2` triangles as indexed geometry.
- Use coordinates `x = radius*cos(phi)`, `z = radius*sin(phi)`, `y = point.Y`.
- Wind every non-degenerate triangle outward. For an increasing-Y cylinder
  profile, face normals and vertex normals must point radially outward.
- Compute smooth unit vertex normals from the profile tangent. Use one-sided
  differences at endpoints and a centered difference inside. Degenerate local
  tangents must fall back to a finite outward radial normal, never a NaN.
- UV U runs from 0 through 1 across the angular sweep. UV V runs from 0 through
  1 along profile order. Exact endpoint values must be stable.
- Output must be deterministic and must only allocate streams selected by
  `want`, following the existing builder contract.
- `LatheVertexCount` applies the same segment default/clamp and returns zero for
  fewer than two profile points.

Existing package contracts available to the new files:

```go
// package scene
func bufferFromMesh(mesh *geom.Mesh) BufferGeometry
const generatorAttributes = geom.AttrNormals | geom.AttrUVs

// package geom
type Attribute uint8
const (
    AttrNormals Attribute = 1 << iota
    AttrUVs
    AttrColors
)
type Mesh struct {
    Positions []float64
    Normals   []float64
    UVs       []float64
    Colors    []float64
    Indices   []int
}
type vec3 struct{ X, Y, Z float64 }
type vec2 struct{ U, V float64 }
type vertex struct {
    position vec3
    normal   vec3
    uv       vec2
    color    vec3
}
func newBuilder(want Attribute, vertexCapacity int) *builder
func (b *builder) emit(v vertex) int
func (b *builder) index(a, c, d int)
func (b *builder) build() *Mesh
func normalize(v vec3) vec3
func ClampInt(value, fallback, minimum, maximum int) int
```

Tests must be substantive and deterministic. At minimum prove:

- default and explicit segment vertex/index counts;
- known cylinder bounds and seam positions;
- outward, unit, finite normals on a cylinder profile;
- UV endpoints and attribute lengths;
- partial-sweep endpoints;
- invalid-profile rejection and degenerate-tangent finiteness;
- `want == PositionsOnly` leaves normals and UVs nil;
- the public wrapper produces an indexed BufferGeometry and survives the
  existing BufferGeometry lowering/expansion contract with the expected drawn
  vertex count.

Use ordinary table-driven Go tests where useful. Keep the implementation
focused and idiomatic. It must pass `gofmt`, `go test ./scene/geom`, and
`go test ./scene`.
