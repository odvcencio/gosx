# Correction: return an applicable public-path parity patch

Return one **bare, complete unified diff** and nothing else. No Markdown fences,
commentary, repeated copies, or prose. End the response with a newline. Modify
only `scene/ir.go` and `scene/ir_test.go`; add no files.

The source files are unchanged from the exact source packet in
`tasks/scene3d-instanced-material-parity.md`. Regenerate the entire patch
against the current repository source; do not return an incremental correction.

## What was sound in response 1

The production intent was correct: `materialFromInstancedIR` should copy all
material fields shared by `InstancedMeshIR` and `IRMaterial`, including physical
PBR fields, blend/render/wireframe/depth state, and the custom shader envelope,
using the existing clone helpers for map-valued fields. Retain that behavior.

## Exact applicability failures to correct

1. Both hunks used an invalid bare `@@` header and fake
   `index 0000000..0000000` metadata. Emit valid unified-diff hunk ranges. You may
   omit `index` lines rather than invent hashes.
2. `InstancedMeshIR` has `Kind`, not `Geometry`.
3. `ObjectIR` has `Kind`, not `Geometry`.
4. `Props` has no `InstancedMeshes` or `Objects` fields. The regression must use
   the public typed path: `Props{Graph: NewGraph(Mesh{...}, InstancedMesh{...})}`
   followed by `CanonicalIR()`.
5. `ptrSceneFloat64` does not exist. Existing pointer helpers are `Float(v)` and
   `Bool(v)`.
6. The test must use fields actually authorable on the typed material. A
   `CustomMaterial` embeds `StandardMaterial`, so the same value can exercise
   physical PBR, alpha blend, wireframe, shader strings, uniforms, layout, and
   source-file maps on both node kinds. There is no `BlendMultiply` constant;
   use an existing constant such as `BlendAlpha`.
7. Keep the focused regression in `scene/ir_test.go`, not a synthetic raw-IR
   construction that bypasses typed lowering.

## Exact public construction surface

```go
type Props struct {
	// unrelated fields omitted
	Graph Graph
}

func NewGraph(nodes ...Node) Graph {
	return Graph{Nodes: append([]Node(nil), nodes...)}
}

type Mesh struct {
	ID       string
	Geometry Geometry
	Material Material
	// unrelated fields omitted
}

type InstancedMesh struct {
	ID        string
	Count     int
	Geometry  Geometry
	Material  Material
	Positions []Vector3
	Rotations []Euler
	Scales    []Vector3
	// unrelated fields omitted
}

type StandardMaterial struct {
	Color        string
	Texture      string
	Roughness    float64
	Metalness    float64
	Clearcoat    float64
	Sheen        float64
	Transmission float64
	Iridescence  float64
	Anisotropy   float64
	NormalMap    string
	RoughnessMap string
	MetalnessMap string
	OcclusionMap string
	EmissiveMap  string
	Emissive     float64
	Opacity      *float64
	BlendMode    MaterialBlendMode
	Wireframe    *bool
}

type CustomMaterial struct {
	StandardMaterial
	ShaderBackend     string
	ShaderLayout      map[string]any
	ShaderSource      string
	ShaderSourceFiles map[string]string
	VertexGLSL        string
	FragmentGLSL      string
	VertexWGSL        string
	FragmentWGSL      string
	Uniforms          map[string]any
}

const BlendAlpha MaterialBlendMode = "alpha"

func Bool(value bool) *bool       { return &value }
func Float(value float64) *float64 { return &value }
```

Use concrete non-zero values. Give both nodes the same `CustomMaterial`, for
example a box geometry and one instanced transform. On the original code,
`len(ir.Materials)` must be 2 because the instanced conversion drops fields; on
the corrected code it must be 1. Assert representative physical fields,
`BlendMode`, `Wireframe`, custom shader fields, and map contents from the sole
canonical material.

Also prove the new `materialFromInstancedIR` cloning behavior directly without
bypassing the main regression: construct a minimal `InstancedMeshIR` with
`CustomUniforms`, `ShaderLayout`, and `ShaderSourceFiles`, convert it with
`materialFromInstancedIR`, mutate each source map, and assert the converted maps
retain their original values. This can be a second focused test or a subtest in
the same file.

The complete diff must be gofmt-clean and pass:

```sh
go test ./scene -run 'TestCanonicalIRInstancedMaterialParity|TestMaterialFromInstancedIRClonesShaderMaps' -count=20
go test ./scene -count=1
go test -race ./scene -run 'TestCanonicalIRInstancedMaterialParity|TestMaterialFromInstancedIRClonesShaderMaps' -count=1
```
