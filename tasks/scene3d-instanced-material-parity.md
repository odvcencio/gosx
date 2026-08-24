# Scene3D instanced material canonical-IR parity

Return one **bare, complete unified diff** and nothing else. Do not use Markdown
fences or commentary. The diff must apply to this exact clean HEAD:

`4d59d18eae6d2e11c6962f4cfbe03ed6a3dc3d34`

## Allowed scope

- Modify only `scene/ir.go` and `scene/ir_test.go`.
- Do not add files.
- Do not touch raycasting, rendering, HTML textures, lazy loading, geometry,
  CapsuleGeometry, or unrelated code.
- Keep the change small and idiomatic. Do not refactor adjacent code.

## Defect

`Props.CanonicalIR()` builds reusable `IRMaterial` entries for ordinary meshes
with `materialFromObjectIR`, but uses `materialFromInstancedIR` for
`InstancedMesh`. The typed instanced record already carries the same physical
PBR, material-state, and custom-shader fields. The canonical conversion drops
many of them.

A baseline probe using the same `StandardMaterial` for an ordinary `Mesh` and
an `InstancedMesh` produced two canonical materials. The ordinary material kept
`clearcoat`, `sheen`, `transmission`, `iridescence`, `anisotropy`, `blendMode`,
and `wireframe`; the instanced material zeroed/omitted them. Equivalent authored
materials therefore fail canonical deduplication, and the browser runtime sees
different material semantics for instanced geometry.

## Required behavior

1. Make `materialFromInstancedIR` preserve every material field that both
   `InstancedMeshIR` and `IRMaterial` expose, matching the established
   `materialFromObjectIR` semantics. This includes the physical PBR extensions,
   material-state fields, and custom shader envelope—not only the fields named
   in the reproducer.
2. Clone map-valued custom shader data exactly as the ordinary-mesh path does;
   do not introduce aliasing.
3. Add a focused regression in `scene/ir_test.go` through the public typed
   lowering path. It must prove an ordinary `Mesh` and `InstancedMesh` authored
   with the same rich material produce one deduplicated canonical material and
   preserve representative physical/material-state values. Include enough
   assertions that the original code fails for the demonstrated defect.
4. Preserve existing zero-value/omitempty behavior and all public APIs.

## Exact relevant source

Current canonical lowering in `scene/ir.go`:

```go
func (p Props) CanonicalIR() IR {
	legacy := p.SceneIR()
	out := IR{
		Version:     IRVersion,
		Camera:      p.cameraToIR(),
		Environment: environmentToIR(p.Background, legacy.Environment),
		Lights:      lightsToIR(legacy.Lights),
		Nodes:       make([]IRNode, 0, len(legacy.Objects)+len(legacy.Models)+len(legacy.Points)+len(legacy.InstancedMeshes)+len(legacy.ComputeParticles)+len(legacy.Sprites)+len(legacy.Labels)+len(legacy.HTML)),
	}
	materialIndexes := map[string]int{}
	for _, object := range legacy.Objects {
		materialIndex := appendIRMaterial(&out.Materials, materialIndexes, materialFromObjectIR(object))
		out.Nodes = append(out.Nodes, objectToIRNode(object, materialIndex))
	}
	for _, model := range legacy.Models {
		materialIndex := appendIRMaterial(&out.Materials, materialIndexes, materialFromObjectIR(model.ObjectIR))
		out.Nodes = append(out.Nodes, modelToIRNode(model, materialIndex))
	}
	for _, points := range legacy.Points {
		out.Nodes = append(out.Nodes, pointsToIRNode(points))
	}
	for _, instanced := range legacy.InstancedMeshes {
		materialIndex := appendIRMaterial(&out.Materials, materialIndexes, materialFromInstancedIR(instanced))
		out.Nodes = append(out.Nodes, instancedToIRNode(instanced, materialIndex))
	}
	// Remaining node kinds omitted from this packet; do not change this loop.
```

Current ordinary and instanced conversions in `scene/ir.go`:

```go
func materialFromObjectIR(object ObjectIR) IRMaterial {
	return IRMaterial{
		Kind:               firstNonEmptySceneString(object.MaterialKind, "standard"),
		Color:              object.Color,
		Texture:            object.Texture,
		Opacity:            derefFloat64(object.Opacity),
		Emissive:           derefFloat64(object.Emissive),
		Roughness:          object.Roughness,
		Metalness:          object.Metalness,
		Clearcoat:          object.Clearcoat,
		Sheen:              object.Sheen,
		Transmission:       object.Transmission,
		Iridescence:        object.Iridescence,
		Anisotropy:         object.Anisotropy,
		NormalMap:          object.NormalMap,
		RoughnessMap:       object.RoughnessMap,
		MetalnessMap:       object.MetalnessMap,
		OcclusionMap:       object.OcclusionMap,
		EmissiveMap:        object.EmissiveMap,
		TextureDescriptors: object.TextureDescriptors,
		BlendMode:          object.BlendMode,
		RenderPass:         object.RenderPass,
		Wireframe:          object.Wireframe,
		DepthWrite:         object.DepthWrite,
		LineDash:           object.LineDash,
		DashSize:           object.DashSize,
		GapSize:            object.GapSize,
		CustomVertex:       object.CustomVertex,
		CustomFragment:     object.CustomFragment,
		CustomVertexWGSL:   object.CustomVertexWGSL,
		CustomFragmentWGSL: object.CustomFragmentWGSL,
		CustomUniforms:     cloneSceneAnyMap(object.CustomUniforms),
		ShaderBackend:      object.ShaderBackend,
		ShaderLayout:       cloneSceneAnyMap(object.ShaderLayout),
		ShaderSource:       object.ShaderSource,
		ShaderSourceFiles:  cloneSceneStringMap(object.ShaderSourceFiles),
	}
}

func materialFromInstancedIR(mesh InstancedMeshIR) IRMaterial {
	return IRMaterial{
		Kind:               firstNonEmptySceneString(mesh.MaterialKind, "standard"),
		Color:              mesh.Color,
		Texture:            mesh.Texture,
		Opacity:            derefFloat64(mesh.Opacity),
		Emissive:           derefFloat64(mesh.Emissive),
		Roughness:          mesh.Roughness,
		Metalness:          mesh.Metalness,
		NormalMap:          mesh.NormalMap,
		RoughnessMap:       mesh.RoughnessMap,
		MetalnessMap:       mesh.MetalnessMap,
		OcclusionMap:       mesh.OcclusionMap,
		EmissiveMap:        mesh.EmissiveMap,
		TextureDescriptors: mesh.TextureDescriptors,
	}
}
```

The material portion of `InstancedMeshIR` in `scene/scene_ir.go` is read-only
context; do not modify that file:

```go
	MaterialKind         string                     `json:"materialKind,omitempty"`
	Color                string                     `json:"color,omitempty"`
	Texture              string                     `json:"texture,omitempty"`
	Opacity              *float64                   `json:"opacity,omitempty"`
	Emissive             *float64                   `json:"emissive,omitempty"`
	BlendMode            string                     `json:"blendMode,omitempty"`
	RenderPass           string                     `json:"renderPass,omitempty"`
	Wireframe            *bool                      `json:"wireframe,omitempty"`
	DepthWrite           *bool                      `json:"depthWrite,omitempty"`
	Roughness            float64                    `json:"roughness,omitempty"`
	Metalness            float64                    `json:"metalness,omitempty"`
	Clearcoat            float64                    `json:"clearcoat,omitempty"`
	Sheen                float64                    `json:"sheen,omitempty"`
	Transmission         float64                    `json:"transmission,omitempty"`
	Iridescence          float64                    `json:"iridescence,omitempty"`
	Anisotropy           float64                    `json:"anisotropy,omitempty"`
	NormalMap            string                     `json:"normalMap,omitempty"`
	RoughnessMap         string                     `json:"roughnessMap,omitempty"`
	MetalnessMap         string                     `json:"metalnessMap,omitempty"`
	OcclusionMap         string                     `json:"occlusionMap,omitempty"`
	EmissiveMap          string                     `json:"emissiveMap,omitempty"`
	TextureDescriptors   MaterialTextureDescriptors `json:"textureDescriptors,omitzero"`
	CustomVertex         string                     `json:"customVertex,omitempty"`
	CustomFragment       string                     `json:"customFragment,omitempty"`
	CustomVertexWGSL     string                     `json:"customVertexWGSL,omitempty"`
	CustomFragmentWGSL   string                     `json:"customFragmentWGSL,omitempty"`
	CustomUniforms       map[string]any             `json:"customUniforms,omitempty"`
	ShaderBackend        string                     `json:"shaderBackend,omitempty"`
	ShaderLayout         map[string]any             `json:"shaderLayout,omitempty"`
	ShaderSource         string                     `json:"shaderSource,omitempty"`
	ShaderSourceFiles    map[string]string          `json:"shaderSourceFiles,omitempty"`
```

`IRMaterial` exposes the same fields. Ordinary lowering already establishes
the desired copying and map-cloning conventions. Use the existing helpers and
the existing test style in `scene/ir_test.go`.

## Validation target

The resulting diff must be gofmt-clean and should pass:

```sh
go test ./scene -run 'Test.*Instanced.*Material.*Parity' -count=20
go test ./scene -count=1
go test -race ./scene -run 'Test.*Instanced.*Material.*Parity' -count=1
```
