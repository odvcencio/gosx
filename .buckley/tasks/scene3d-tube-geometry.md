# Task: add typed Scene3D TubeGeometry

Return one directly applicable Git unified diff and nothing else. The first
bytes must be `diff --git `, do not wrap the diff in Markdown fences, hunk line
counts must exactly match their content, and end the response with a newline.

Repository: GoSX, an MIT-licensed Go web framework. This is a bounded
Three.js-parity slice. Work only in these four new files; do not change any
existing file, generated runtime asset, documentation, module file, or API
outside this task:

1. `scene/geom/tube.go`
2. `scene/geom/tube_test.go`
3. `scene/tube_geometry.go`
4. `scene/tube_geometry_test.go`

Public package `scene` contract:

```go
type TubeGeometryOptions struct {
    Radius         float64
    RadialSegments int
    Closed         bool
}

func TubeGeometry(path []Vector3, opts TubeGeometryOptions) BufferGeometry
```

`Vector3` already has `X`, `Y`, and `Z` float64 fields. The wrapper flattens
the path, calls the canonical `scene/geom` generator, and uses the existing
unexported `bufferFromMesh` and `generatorAttributes` helpers so the result
flows through the established indexed BufferGeometry honesty path.

Internal package `geom` contract:

```go
func Tube(path []float64, radius float64, radialSegments int, closed bool, want Attribute) *Mesh
func TubeVertexCount(pathPoints, radialSegments int, closed bool) int
```

The flat path alternates X, Y, Z. Requirements:

- Reject paths whose length is not divisible by 3, contain non-finite values,
  have fewer than two points when open or three when closed, or contain
  consecutive duplicate points. For a closed path, the final and first point
  are also consecutive and must differ. Return nil; never emit NaN or Inf.
- Resolve a non-positive/non-finite radius to 1 using existing `PositiveOr`.
- Resolve radial segments with existing `ClampInt`: default 8, minimum 3,
  maximum 128.
- An open path emits one ring per path point and connects adjacent rings. A
  closed path emits a duplicate seam ring for the first path point and connects
  all path edges. Each ring contains `radialSegments+1` vertices so U=0 and
  U=1 are distinct seam vertices.
- Use stable parallel-transport frames, not a fixed world-up cross product.
  Compute one-sided endpoint tangents for open paths and centered/wrapped
  tangents otherwise. Choose the initial normal from the coordinate axis least
  aligned with the first tangent. Transport normal/binormal between tangents
  with a finite collinear fallback. For a closed path, distribute any residual
  seam twist across the path so the duplicated final frame matches the first.
- Vertex position is center + radius*(normal*cos(theta)+binormal*sin(theta)).
  Vertex normal is the corresponding unit radial direction.
- UV U runs 0..1 around each ring. UV V follows cumulative centerline distance
  0..1, including V=1 at the duplicated closed seam ring.
- Emit indexed outward-facing quads as two non-degenerate triangles for every
  path segment and radial segment.
- Output must be deterministic. Only allocate normal/UV output streams when
  selected by `want`; PositionsOnly must leave `Mesh.Normals` and `Mesh.UVs`
  nil and must not allocate a per-vertex normal or UV buffer.
- `TubeVertexCount` applies the same radial default/clamp; it returns zero for
  fewer than two open points or three closed points.

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
const PositionsOnly Attribute = 0
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
func addVec(a, b vec3) vec3
func subVec(a, b vec3) vec3
func scaleVec(v vec3, s float64) vec3
func dotVec(a, b vec3) float64
func crossVec(a, b vec3) vec3
func PositiveOr(value, fallback float64) float64
func ClampInt(value, fallback, minimum, maximum int) int
```

Tests must be substantive and deterministic. At minimum prove:

- open and closed vertex/index counts, including defaults and clamps;
- a straight +Y tube has the expected radius/bounds, finite unit radial
  normals, outward triangle winding, U seams, and V endpoints;
- a bent 3D path keeps finite orthonormal frames without a visible frame flip;
- a closed path duplicates the first ring position/normal at V=1 and keeps
  outward winding at the closing edge;
- invalid path rejection and radius defaulting;
- PositionsOnly omits normals and UVs;
- repeated calls are deeply deterministic;
- the public wrapper produces indexed BufferGeometry and existing lowering
  expansion reports the expected drawn vertex count.

Use table-driven Go tests where useful. Keep the implementation focused and
idiomatic. It must pass `gofmt`, `go test ./scene/geom`, and `go test ./scene`.
