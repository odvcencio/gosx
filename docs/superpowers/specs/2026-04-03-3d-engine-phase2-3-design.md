# 3D Engine Phase 2-3: Serious Rendering, Assets, and Animation

**Date:** 2026-04-03
**Status:** Proposed
**Priority:** High
**Scope:** Renderer module split, PBR materials, shadow maps, glTF loading, animation, scene picking, WebGPU backend, postprocessing, skeletal animation
**Depends on:** [Native 3D Framework Design (Phase 1)](2026-04-02-native-3d-framework-design.md)

## Problem

GoSX has a working Phase 1 3D foundation: typed scene graph, WebGL2 renderer, orbit camera, labels, sprites, model loading, and an incremental reconciliation engine. But it is a demo-class system limited to < 10k triangles, vertex-color lighting, 5 hardcoded material kinds, no shadows, no real asset pipeline, and no animation. It cannot compete with three.js for production 3D content.

The current renderer is a 5K LOC monolith (`10-runtime-scene-core.js`) that mixes projection math, geometry generation, material profiling, lighting, pointer handling, and WebGL state management in one file. Extending it for PBR, shadows, or a second GPU backend would compound the complexity.

## Approach

Clean renderer abstraction first (Approach 2 from brainstorming). Split the monolith into focused modules along the boundaries the Phase 1 spec already identified, then build every Phase 2-3 feature on those abstractions. This means PBR, shadows, and every feature after lands in the right place from day one, and WebGPU becomes a mechanical port rather than a second rewrite.

## Priority Order

Features are ordered for maximum value per piece:

1. PBR material system (A)
2. Shadow maps (B)
3. glTF / GLB loading (G)
4. Animation clips and mixers (H)
5. Scene picking and event routing (F)
6. WebGPU backend (C)
7. Postprocessing chain (E)
8. Skeletal animation (I)
9. Instancing (D) — deferred to Phase 4
10. Texture compression / mesh compression (J, K) — deferred to Phase 4

## Section 1: Renderer Module Split

The foundational refactor. Split `10-runtime-scene-core.js` into focused modules before adding any features.

### New Module Boundaries

| Module | Extracted From | Responsibility |
|---|---|---|
| `scene-math.js` | scene-core vec/quat/matrix ops | Vec3, Quat, Mat4, Euler, projection, frustum |
| `scene-geometry.js` | scene-core geometry generation | Vertex generation for all primitive types, normals, UVs, tangents |
| `scene-material.js` | scene-core material profiling | Material resolution, PBR parameter encoding, texture binding |
| `scene-lighting.js` | scene-core lighting calc | Light accumulation, environment probes, shadow map sampling |
| `scene-draw-plan.js` | scene-core pass sorting/batching | Backend-agnostic draw plan: sort by pass, compact by material, frustum cull |
| `scene-webgl.js` | scene-core WebGL state machine + shaders | WebGL2 backend: shader compilation, buffer management, draw execution |
| `scene-input.js` | scene-core pointer handling | Picking, pointer events, raycasting |
| `scene-canvas.js` | scene-core canvas fallback | Canvas 2D fallback renderer |

### Draw Plan Abstraction

The draw plan is the interface between scene logic and GPU backends. It takes a `RenderBundle` and produces an ordered list of backend-agnostic draw commands:

```
DrawPlan {
    shadowPasses: ShadowPass[]     // depth-only passes per casting light
    passes: DrawPass[]             // opaque, alpha, additive
    postEffects: PostEffect[]      // screen-space effect chain
}

DrawPass {
    name: string
    blend: BlendMode
    depth: DepthMode
    commands: DrawCommand[]        // sorted by material, then depth
}

DrawCommand {
    objectID: string
    materialIndex: int
    vertexOffset: int
    vertexCount: int
    textures: TextureBinding[]
    uniforms: UniformBlock
}
```

Backends (`scene-webgl.js`, `scene-webgpu.js`) implement `executeDrawPlan(plan)`. They own GPU resource creation, shader compilation, and state management. They do not own scene logic, sorting, or culling.

### Go-Side Changes

None for the module split itself. However, the split establishes the boundary where the vertex data format changes. See Section 2 for the new vertex buffer layout.

### Rendering Pipeline Transition

The current architecture has two parallel paths that both pre-bake vertex colors with CPU-side lighting:

1. **Go-side** (`runtime.go` `buildRenderBundle`): computes lit vertex colors via `sceneLitColorRGBA`, packs into `Colors []float64`
2. **JS-side** (`createSceneRenderBundle`): also normalizes and builds bundles with per-vertex lighting

PBR requires per-pixel lighting in the fragment shader. The CPU lighting path cannot produce the data PBR needs (normals, tangents, UVs). Therefore:

- The Go-side `buildRenderBundle` CPU lighting path becomes **canvas-fallback only**. It continues to emit pre-baked vertex colors for the Canvas 2D renderer.
- The PBR path (WebGL2, WebGPU) receives **raw geometry data** (positions, normals, UVs) and performs all lighting on the GPU.
- The JS `createSceneRenderBundle` function is replaced by the draw plan pipeline: `scene-draw-plan.js` builds the plan, backends execute it.

This is not incremental — the PBR shader is a new program, not a modification of the existing vertex-color shader. The existing shader is preserved in `scene-canvas.js` as the fallback renderer.

## Section 2: Standard PBR Material System

Replace vertex-color lighting with per-pixel Cook-Torrance BRDF.

### Go API Additions

```go
// scene/scene.go

type StandardMaterial struct {
    Color        string
    Roughness    float64           // 0.0 (mirror) to 1.0 (diffuse), default 0.5
    Metalness    float64           // 0.0 (dielectric) to 1.0 (metal), default 0.0
    NormalMap    string            // URL to normal map texture
    RoughnessMap string            // URL to roughness texture
    MetalnessMap string            // URL to metalness texture
    EmissiveMap  string            // URL to emissive texture
    Emissive     float64
    Opacity      *float64
    BlendMode    MaterialBlendMode
    Wireframe    *bool
}
```

### Legacy Material Preset Mapping

Old material kinds lower to StandardMaterial parameters:

| Legacy Kind | Roughness | Metalness | Opacity | Emissive | Notes |
|---|---|---|---|---|---|
| Flat | 1.0 | 0.0 | 1.0 | 0.0 | Unlit flag set |
| Matte | 0.8 | 0.0 | 1.0 | 0.0 | |
| Glass | 0.1 | 0.0 | 0.3 | 0.0 | Alpha blend |
| Glow | 0.3 | 0.0 | 1.0 | 1.0 | |
| Ghost | 0.5 | 0.0 | 0.15 | 0.0 | Alpha blend |

### StandardMaterial Interface Compliance

`StandardMaterial` implements the existing `Material` interface:

```go
func (StandardMaterial) sceneMaterial() {}
func (m StandardMaterial) legacyMaterial() map[string]any {
    // Maps to the closest legacy kind for backward-compatible transport.
    // Unlit → "flat", metalness > 0.5 → "glow", else roughness-based "matte"/"glass".
    // PBR-aware renderers ignore legacy fields and read PBR fields directly.
}
```

This allows `StandardMaterial` to coexist with legacy materials in the same `Graph`. Legacy renderers see approximate kind mappings. PBR renderers read the PBR fields.

### IR Additions

```go
// engine/render_bundle.go — RenderMaterial additions
Roughness    float64 `json:"roughness,omitempty"`
Metalness    float64 `json:"metalness,omitempty"`
NormalMap    string  `json:"normalMap,omitempty"`
RoughnessMap string  `json:"roughnessMap,omitempty"`
MetalnessMap string  `json:"metalnessMap,omitempty"`
EmissiveMap  string  `json:"emissiveMap,omitempty"`
Unlit        bool    `json:"unlit,omitempty"`
```

### Vertex Buffer Layout Transition

The current vertex layout packs pre-baked data per vertex:

```
Current: position(3) + color(4) + material(3) = 10 floats/vertex
```

The PBR layout provides raw geometry data for GPU-side shading:

```
PBR:     position(3) + normal(3) + uv(2) + tangent(4) = 12 floats/vertex
```

Tangents are required for normal mapping. They are computed from position/UV/normal using the MikkTSpace algorithm during geometry generation (JS-side for primitives, at load time for glTF).

The `RenderPassBundle` struct changes:

```go
// engine/render_bundle.go — RenderPassBundle additions/changes
type RenderPassBundle struct {
    Name        string    `json:"name,omitempty"`
    Blend       string    `json:"blend,omitempty"`
    Depth       string    `json:"depth,omitempty"`
    Static      bool      `json:"static,omitempty"`
    CacheKey    string    `json:"cacheKey,omitempty"`
    Positions   []float64 `json:"positions,omitempty"`
    Normals     []float64 `json:"normals,omitempty"`     // new: per-vertex normals
    UVs         []float64 `json:"uvs,omitempty"`         // new: per-vertex texture coords
    Tangents    []float64 `json:"tangents,omitempty"`     // new: per-vertex tangents (vec4)
    Colors      []float64 `json:"colors,omitempty"`       // retained for canvas fallback
    Materials   []float64 `json:"materials,omitempty"`
    VertexCount int       `json:"vertexCount,omitempty"`
}
```

The `RenderBundle` top-level also gains matching arrays:

```go
// engine/render_bundle.go — RenderBundle additions
WorldNormals     []float64 `json:"worldNormals,omitempty"`
WorldUVs         []float64 `json:"worldUVs,omitempty"`
WorldTangents    []float64 `json:"worldTangents,omitempty"`
```

**WebGL2 vertex attribute layout:**

| Location | Attribute | Type | Components |
|---|---|---|---|
| 0 | `a_position` | vec3 | world position |
| 1 | `a_normal` | vec3 | world normal |
| 2 | `a_uv` | vec2 | texture coordinate |
| 3 | `a_tangent` | vec4 | tangent + handedness |

**Uniform interface:**

```glsl
// Per-frame uniforms
uniform mat4 u_viewMatrix;
uniform mat4 u_projectionMatrix;
uniform vec3 u_cameraPosition;

// Per-material uniforms
uniform vec3 u_albedo;
uniform float u_roughness;
uniform float u_metalness;
uniform float u_emissive;
uniform bool u_unlit;
uniform sampler2D u_albedoMap;
uniform sampler2D u_normalMap;
uniform sampler2D u_roughnessMap;
uniform sampler2D u_metalnessMap;
uniform sampler2D u_emissiveMap;
uniform bool u_hasAlbedoMap;
uniform bool u_hasNormalMap;
// ... (one bool per optional map)

// Light array (max 8)
uniform int u_lightCount;
uniform vec3 u_lightPositions[8];
uniform vec3 u_lightDirections[8];
uniform vec3 u_lightColors[8];
uniform float u_lightIntensities[8];
uniform int u_lightTypes[8];       // 0=ambient, 1=directional, 2=point
```

**Fragment shader precision:** `precision highp float` is required for PBR (GGX distribution and Schlick fresnel need full float precision). This is universally supported on WebGL2 desktop; mobile WebGL2 devices that lack highp fall back to the canvas renderer.

### Shader Architecture

- **Vertex shader:** Transforms position, passes normals, tangents, UVs, and world position to fragment stage
- **Fragment shader:** Cook-Torrance BRDF with GGX normal distribution, Schlick fresnel approximation, Smith geometry function
- **Lighting:** Per-pixel evaluation of up to 8 lights (ambient + directional + point)
- **Texture sampling:** Albedo, normal, roughness, metalness, emissive maps; each optional with uniform fallback values

### Environment Lighting (IBL)

Image-based lighting (IBL) using environment maps is what makes PBR materials look realistic under ambient conditions. Without it, Cook-Torrance BRDF under only direct lights looks flat.

**Deferred to Phase 4.** IBL requires HDR environment map loading, prefiltered specular cubemap generation, and irradiance map computation — significant asset pipeline work. For Phase 2-3, the existing hemisphere environment model (sky/ground colors + ambient) provides the ambient term. Direct lights carry the visual weight. This is the same approach three.js uses before an environment map is assigned.

### New Geometry Types

All geometries now emit normals and UVs (current ones do not):

- `CylinderGeometry` — top/bottom radius, height, radial segments
- `TorusGeometry` — ring radius, tube radius, radial/tubular segments
- Existing box, sphere, plane, pyramid: extended with normal and UV generation

```go
// scene/scene.go

type CylinderGeometry struct {
    RadiusTop    float64
    RadiusBottom float64
    Height       float64
    Segments     int
}

type TorusGeometry struct {
    Radius         float64
    Tube           float64
    RadialSegments int
    TubularSegments int
}
```

## Section 3: Shadow Maps

Directional light shadows using depth map passes.

### Go API Additions

```go
// scene/scene.go — DirectionalLight additions
type DirectionalLight struct {
    ID         string
    Color      string
    Intensity  float64
    Direction  Vector3
    CastShadow bool    // new
    ShadowBias float64 // new, default 0.005
    ShadowSize int     // new, shadow map resolution, default 1024
}

// scene/scene.go — Mesh additions
type Mesh struct {
    // ... existing fields ...
    CastShadow    bool // new
    ReceiveShadow bool // new
}
```

### Rendering Approach

- Directional light shadow maps only (point light cubic shadow maps deferred to Phase 4)
- One depth-only render pass per shadow-casting light, max 2 shadow-casting directional lights
- Scene rendered from light's perspective into a depth texture
- Main pass samples shadow map with PCF (percentage-closer filtering, 4-tap Poisson disk)
- Shadow map resolution configurable per light, default 1024x1024

### Draw Plan Integration

```
1. Shadow pass per casting light → depth-only FBO, stripped shader
2. Opaque pass → main FBO, PBR shader samples shadow maps
3. Alpha pass → main FBO, PBR shader with transparency
4. Additive pass → main FBO
```

### IR Additions

```go
// engine/render_bundle.go — RenderLight additions
CastShadow bool    `json:"castShadow,omitempty"`
ShadowBias float64 `json:"shadowBias,omitempty"`
ShadowSize int     `json:"shadowSize,omitempty"`

// engine/render_bundle.go — RenderObject additions
CastShadow    bool `json:"castShadow,omitempty"`
ReceiveShadow bool `json:"receiveShadow,omitempty"`
```

### WebGL Specifics

- Shadow maps use `DEPTH_COMPONENT24` textures via WebGL2 native depth textures
- Light space matrix: orthographic projection from light direction, frustum fitted to scene bounds
- PCF with 4-tap Poisson disk sampling
- Canvas fallback: shadows silently disabled

## Section 4: glTF / GLB Loading

Unlock real-world 3D content from Blender, Sketchfab, and other DCC tools.

### Go API

The existing `Model` node gains animation fields:

```go
// scene/scene.go — Model additions
type Model struct {
    // ... existing fields ...
    Animation string // new: named animation clip to play (from glTF)
    Loop      *bool  // new: loop animation, default true
}
```

The `Src` field now accepts `.glb`, `.gltf`, and `.gosx3d.json`:

```go
Model{
    Src:       "/assets/ship.glb",
    Position:  Vec3(1.8, -0.4, 0.9),
    Scale:     Vec3(1, 1, 1),
    Animation: "idle",
    Loop:      Bool(true),
}
```

The `ModelIR` struct gains matching transport fields:

```go
// scene/scene_ir.go — ModelIR additions
Animation string `json:"animation,omitempty"`
Loop      *bool  `json:"loop,omitempty"`
```

### JS Loader Design

Format detection by extension in `scene-mount.js`. The existing `loadSceneModelAsset` function gains a format-dispatch layer: `.glb` and `.gltf` fetch via `arrayBuffer()` (binary) or `json()` respectively, while `.gosx3d.json` continues through the existing JSON path.

- `.glb` → binary glTF (preferred — single file, no URI resolution)
- `.gltf` → JSON glTF with external buffer/texture URIs
- `.gosx3d.json` → legacy format (preserved)

Built-in minimal parser (~1500 LOC), not a library dependency. Covers accessor traversal, buffer view stride, multi-primitive meshes, and material extraction.

### glTF Feature Extraction

| glTF Feature | Maps To |
|---|---|
| Meshes + primitives | `RenderObject` with position/normal/UV/tangent vertex data |
| PBR metallic-roughness material | `StandardMaterial` |
| Textures (embedded or URI) | Texture loading pipeline, cached by URL |
| Node hierarchy + transforms | Scene graph groups |
| Animations | Animation clips (Section 5) |
| Skins | Stored for skeletal animation (Section 8) |

### Supported Subset

**Included:**
- Triangle mesh primitives
- `pbrMetallicRoughness` materials
- PNG and JPEG textures (embedded or external)
- Node hierarchy with TRS transforms
- Animation channels: translation, rotation, scale with linear/step interpolation
- All accessor component types (float, ubyte, ushort) with byte stride

**Excluded (Phase 4):**
- Extensions (KHR_draco_mesh_compression, KHR_texture_basisu)
- Morph targets
- Cameras and lights from glTF (GoSX uses its own)
- Sparse accessors
- Multiple scenes (scene 0 only)

### Binary GLB Parsing

```
Header: magic(4 bytes) + version(4) + length(4)
Chunk 0: JSON metadata
Chunk 1: Binary buffer
```

### Texture Pipeline

- Loaded via `Image()` with `crossOrigin` for route-relative URLs
- Cached by URL — shared across materials
- Uploaded as `TEXTURE_2D` with `generateMipmap()`
- Anisotropic filtering via `EXT_texture_filter_anisotropic` when available
- Route-relative asset resolution through existing public asset pipeline
- Static export copies model files automatically

**Texture lifecycle:**
- Textures are created on first reference and cached for the lifetime of the scene mount
- Released on scene unmount (all GPU textures deleted)
- Loading is async: meshes render with fallback uniform values (albedo = material color, normal = flat, roughness/metalness = uniform values) until textures arrive, then re-render
- Texture load failures are silent (logged as warnings) — the fallback uniform values remain
- No memory budget or LRU eviction in Phase 2-3; scenes are expected to stay under ~50 textures

## Section 5: Animation Clips and Mixers

Keyframe-based animation for loaded models and authored scenes.

### Go API Additions

```go
// scene/scene.go

type AnimationClip struct {
    Name     string
    Channels []AnimationChannel
}

type AnimationChannel struct {
    TargetNode    string
    Property      AnimationProperty
    Keyframes     []Keyframe
    Interpolation InterpolationMode
}

type AnimationProperty string
const (
    AnimatePosition AnimationProperty = "position"
    AnimateRotation AnimationProperty = "rotation"
    AnimateScale    AnimationProperty = "scale"
)

type Keyframe struct {
    Time  float64
    Value []float64 // 3 floats for pos/scale, 4 for quaternion rotation
}

type InterpolationMode string
const (
    InterpolateLinear InterpolationMode = "linear"
    InterpolateStep   InterpolationMode = "step"
)
```

### Two Authoring Paths

**1. Model-embedded animations** (from glTF):

```go
Model{
    Src:       "/assets/character.glb",
    Animation: "idle",
    Loop:      true,
}
```

**2. Programmatic animations** via the builder:

```go
b.AnimateFloat(target, property, from, to, duration, loop)
b.AnimateVec3(target, property, keyframes, duration, loop)
```

Builder animations compile to engine program expressions evaluated by the existing signal/expression VM.

### JS AnimationMixer

```
AnimationMixer {
    clips: Map<name, AnimationClip>
    active: { clip, time, weight, loop, speed }[]

    play(name, opts)   → start or crossfade to clip
    stop(name)         → stop clip
    update(deltaTime)  → advance all active clips, interpolate, apply to nodes
}
```

Each scene mount owns one mixer. `update()` runs before render bundle generation each frame.

**Crossfading:** Switching clips blends weights over configurable fade duration (default 0.3s).

**Existing spin/drift:** Preserved as-is. Simple sinusoidal motion does not use the clip system.

### IR Additions

```go
// engine/render_bundle.go

type RenderAnimation struct {
    Name     string                   `json:"name"`
    Channels []RenderAnimationChannel `json:"channels"`
    Duration float64                  `json:"duration"`
}

type RenderAnimationChannel struct {
    TargetID      string    `json:"targetID"`
    Property      string    `json:"property"`
    Times         []float64 `json:"times"`
    Values        []float64 `json:"values"`
    Interpolation string    `json:"interpolation,omitempty"`
}

// RenderBundle additions
Animations []RenderAnimation `json:"animations,omitempty"`
```

## Section 6: Scene Picking and Event Routing

Replace bounds-based pointer detection with raycast hit testing and scene graph event propagation.

### Raycasting

1. Pointer position → unproject through camera → world-space ray (origin + direction)
2. Broad phase: test ray against each pickable object's AABB
3. Narrow phase: test ray against mesh triangles for AABB hits
4. Return closest hit: object ID, world position, face normal, distance

Triangle data is a typed view into the same vertex buffers used for rendering — no duplication.

### Event Model

```
pointerdown  → raycast → fire targetID.down signal
pointerup    → raycast → fire targetID.selected (if same target as down)
pointermove  → raycast → fire enter/leave transitions, update hoverID
click        → synthetic from down+up on same target, increment clickCount
```

Events propagate up the scene graph (bubble). The existing `SceneEventSignals` and `SceneObjectSignals` in the builder already model this correctly — the source of truth switches from bounds overlap to raycast.

### Go API

```go
// Mesh gains event handler signal namespaces
type Mesh struct {
    // ... existing fields ...
    OnClick       string
    OnPointerOver string
    OnPointerOut  string
}
```

### Performance Guardrails

- Raycasting runs only on pointer events, not every frame
- Broad phase AABB rejects most objects before triangle testing
- Max 1 raycast per event (no multi-touch in Phase 2)
- Static meshes cache world-space triangles; dynamic meshes rebuild per event

### Draw Plan Additions

```js
PickGeometry {
    objectID: string
    positions: Float32Array  // world-space triangle vertices (reference, not copy)
    triangleCount: int
}
```

Owned by `scene-input.js`.

## Section 7: WebGPU Backend

Mechanical port enabled by the draw plan abstraction from Section 1.

### Implementation

`scene-webgpu.js` implements the same `executeDrawPlan(plan)` interface as `scene-webgl.js`:

- Shaders rewritten in WGSL (same math as GLSL, different syntax)
- Bind groups instead of individual uniform calls — fewer state changes
- Render pipelines precompiled and cached by material+geometry signature
- Compute shaders available for future GPU-side frustum culling

### Backend Selection

```
1. navigator.gpu available && !ForceWebGL → WebGPU
2. WebGL2 available → WebGL2
3. Canvas 2D fallback
```

Existing `capabilityTier` and fallback-reason reporting extends naturally. No Go-side changes.

## Section 8: Postprocessing Chain

Screen-space effects via offscreen framebuffer rendering.

### Go API

```go
// scene/scene.go

type EffectComposer struct {
    Effects []PostEffect
}

type PostEffect struct {
    Kind      string             // "bloom", "vignette", "colorGrade"
    Intensity float64
    Threshold float64            // bloom: luminance threshold
    Radius    float64            // bloom: blur radius
    Params    map[string]float64 // kind-specific overrides
}
```

### IR Transport

```go
// engine/render_bundle.go

type RenderPostEffect struct {
    Kind      string             `json:"kind"`
    Intensity float64            `json:"intensity,omitempty"`
    Threshold float64            `json:"threshold,omitempty"`
    Radius    float64            `json:"radius,omitempty"`
    Params    map[string]float64 `json:"params,omitempty"`
}

// RenderBundle additions
PostEffects []RenderPostEffect `json:"postEffects,omitempty"`
```

The draw plan reads `PostEffects` from the render bundle and appends them to the effect chain. Backends allocate FBOs and execute the fullscreen passes.

### Phase 2-3 Effects

| Effect | Technique |
|---|---|
| Tone mapping | ACES filmic, applied first |
| Bloom | Threshold bright pixels, gaussian blur, additive composite |
| Vignette | Radial edge darkening |
| Color grading | Exposure, contrast, saturation in single pass |

DOF deferred to Phase 4 (requires depth buffer readback).

### Implementation

Each effect is a fullscreen quad fragment shader pass. FBO ping-pong between two offscreen textures. Chain reads from previous pass output.

## Section 9: Skeletal Animation

Extends Section 5's animation mixer with vertex skinning.

### What It Adds

- **Skin data** from glTF: joint hierarchy, inverse bind matrices, vertex weights (up to 4 joints per vertex)
- **Joint matrices** computed per frame from animation clip pose
- **Vertex skinning** in vertex shader

### Vertex Shader Additions

```glsl
attribute vec4 a_joints;   // 4 joint indices
attribute vec4 a_weights;  // 4 joint weights
uniform mat4 u_jointMatrices[MAX_JOINTS]; // max 64

mat4 skin = a_weights.x * u_jointMatrices[int(a_joints.x)]
          + a_weights.y * u_jointMatrices[int(a_joints.y)]
          + a_weights.z * u_jointMatrices[int(a_joints.z)]
          + a_weights.w * u_jointMatrices[int(a_joints.w)];
```

Max 64 joints per skeleton (covers humanoid + props). Uniform buffer on WebGL2, storage buffer on WebGPU.

### Go API

No new types. Skeletal data is embedded in glTF models and extracted by the loader. If a model has skins, the animation mixer drives joints automatically.

## Shader Fallback Chain

PBR shaders are more complex and may fail to compile on older or constrained GPUs. The fallback chain:

1. **PBR shader compiles** → use PBR renderer (WebGPU or WebGL2)
2. **PBR shader fails** → fall back to legacy vertex-color shader (WebGL2 only), report reason via capability tier
3. **WebGL2 unavailable** → fall back to Canvas 2D renderer

Shader compile failures are surfaced through the existing `capabilityTier` and fallback-reason system. The legacy shader is preserved in `scene-webgl.js` specifically for this fallback.

## Compatibility

All changes are additive to the existing Phase 1 contract:

1. Legacy material kinds (flat/ghost/glass/glow/matte) become StandardMaterial presets. Note: the visual appearance will change slightly — the current fragment shader applies a per-kind tone curve (`mix(0.9, 1.0, tone)`) that is not replicated in PBR. This is an intentional improvement. The `Unlit` flag on `Flat` materials preserves the unshaded look.
2. Existing `Model.Src` accepts new formats alongside `.gosx3d.json`
3. Existing spin/drift fields coexist with animation clips
4. Existing bounds-based picking upgrades to raycast transparently
5. SceneEventSignals and SceneObjectSignals are unchanged
6. Canvas fallback silently disables shadows, postprocessing, and skeletal animation

## Success Criteria

- A production scene can load a glTF model with PBR materials and cast shadows
- Animation clips from glTF play with crossfading
- Clicking a mesh fires the expected signal chain through the scene graph
- The same scene runs on WebGPU and WebGL2 without author changes
- Postprocessing bloom makes emissive materials visually pop
- A skinned character model animates correctly
- Legacy demos continue to work without modification
