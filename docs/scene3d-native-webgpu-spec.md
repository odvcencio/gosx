# Scene3D Native WebGPU Spec

Status: implementation spec + R1/R2 primitive contract implementation pass
Target package family: `scene`, `engine`, `client/enginevm`, `client/wasm`, `render/gpu`, `render/bundle`, `client/js/bootstrap-src`
Design goal: make GoSX Scene3D the default native 3D authoring surface for web applications, not a lighter Three.js wrapper and not a half-rendered demo path.

## 1. Mission

Scene3D must become the place where a GoSX user can author serious 3D scenes, UI-composited 3D applications, product/configurator flows, scientific visualizations, games, collaborative editors, dashboards, and HTML-in-canvas layouts without leaving Go and without importing a JavaScript 3D engine.

The defining claim is:

> GoSX Scene3D is a server-authored, IR-driven, WebGPU-native scene platform that lets authors compose typed Go scene graphs, reactive islands, CSS-stylable visuals, GPU compute, world-space HTML, overlay HTML, and server-driven diffs through one coherent framework contract.

This is deliberately broader than “a renderer.” The renderer is one layer. The platform is the authoring model, IR, asset planner, runtime capability negotiation, native WebGPU implementation, fallback stack, event/signal bridge, and test contract that prevents partial features from shipping.

## 2. Product principles

### 2.1 Native first, not JavaScript-first

Scene3D does not expose a JavaScript scene graph. User-authored scene state lives in Go, lowers into SceneIR, and is consumed by Go/WebGPU, JS/WebGL compatibility, and headless backends. JavaScript can host canvas lifecycle and browser API glue, but it must not become the primary modeling language.

### 2.2 One scene contract across all renderers

Every Scene3D primitive must have one canonical semantic representation before backend selection. WebGPU, WebGL, canvas fallback, headless rendering, testing, asset planning, and live diffs consume the same SceneIR and render bundle model. Backend-specific compromises are declared as capability degradations, not hidden alternate scene graphs.

### 2.3 No half implementation rule

A primitive is not “done” when it has a struct, or when it appears in JSON, or when WebGL can draw it. A primitive is done only when these gates pass:

1. Typed authoring surface exists in `scene`.
2. IR lowering preserves all semantically important fields.
3. Browser/runtime bridge can resolve it into `engine.RenderBundle` or a documented specialized bundle.
4. Native WebGPU path draws it or explicitly marks it unsupported with a capability reason.
5. Compatibility path exists for WebGL/canvas/headless or declares a visible fallback.
6. Picking/event semantics are specified.
7. Material, transform, animation, visibility, lifecycle, and disposal behavior are specified.
8. Tests cover buffer shape, serialization, fallback behavior, and at least one render/frame path where practical.
9. Docs show a minimal example and one production-shape example.

### 2.4 Easy simple scenes, unlimited complex scenes

The first line of authoring should stay small:

```go
scene.Mesh{
    Geometry: scene.SphereGeometry{Radius: 1, Segments: 32},
    Material: scene.StandardMaterial{Color: "#77c6ff", Roughness: 0.35},
}
```

The platform must also scale to scenes with thousands of instanced meshes, compute particle systems, streamed GLB assets, post-processing, server diffing, and HTML overlays without asking users to abandon the primitive.

### 2.5 HTML-in-canvas must feel native

GoSX should make HTML overlays, world-space labels, screen-space HUD, and texture-backed HTML surfaces routine. Users should not hand-wire DOM/canvas sync, z-index hacks, projection math, pointer event tunneling, or accessibility fallbacks.

## 3. Current foundation

The current repo already has the right shape:

- `scene.Props` is the typed Go-side Scene3D surface.
- `scene.Graph` lowers typed nodes into SceneIR.
- `SceneIR` serializes objects, models, points, instanced meshes, GLB instancing, compute particles, animations, labels, sprites, lights, environment, post effects, shadow policy, and post-FX policy.
- `engine.RenderBundle` is the renderer-facing frame payload.
- `render/gpu` abstracts WebGPU-like devices, buffers, textures, pipelines, command encoders, render passes, compute passes, and surfaces.
- `render/bundle.Renderer` owns the native WebGPU render path, including lit/unlit pipelines, shadow passes, culling, particles, bloom, FXAA, picking, textures, materials, skinning, and HDR/post-FX intermediates.
- Browser WASM code wires `__gosx_render_engine_to_canvas` to `bundle.Renderer.Frame` through `client/wasm/render_full.go`.

The key gap this patch starts closing is primitive completeness. The existing `render/bundle/primitive.go` path only generated cube/box, plane, and sphere native geometry. Scene3D’s authoring surface includes more built-in mesh primitives than that, and the renderer should not silently skip them.

## 4. North-star API

Scene3D authoring should converge on five composable layers.

### 4.1 Typed Go scene graph

The primary API remains strongly typed Go:

```go
props := scene.Props{
    Responsive: scene.Bool(true),
    Controls: scene.ControlOrbit,
    Camera: scene.PerspectiveCamera{
        Position: scene.Vec3(0, 1.8, 6),
        FOV: 60,
    },
    Environment: scene.Environment{
        AmbientColor: "#ffffff",
        AmbientIntensity: 0.18,
        SkyColor: "#9cc8ff",
        GroundColor: "#10151f",
    },
    PostFX: scene.PostFX{
        Effects: []scene.PostEffect{
            scene.Bloom{Threshold: 0.9, Strength: 0.35, Radius: 5},
            scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.05},
        },
    },
    Graph: scene.NewGraph(
        scene.Mesh{
            ID: "hero",
            Geometry: scene.TorusGeometry{Radius: 1.2, Tube: 0.28, RadialSegments: 48, TubularSegments: 18},
            Material: scene.StandardMaterial{Color: "#f5c76b", Roughness: 0.28, Metalness: 0.8},
            CastShadow: true,
            ReceiveShadow: true,
        },
        scene.Label{Target: "hero", Text: "Native WebGPU", Priority: 10},
    ),
}
```

### 4.2 Composable GSX scene elements

GSX element authoring should lower into the same scene graph:

```go
return <Scene3D controls="orbit" responsive>
    <PerspectiveCamera position={scene.Vec3(0, 2, 6)} fov={60} />
    <Environment ambientColor="#fff" ambientIntensity={0.2} />
    <Mesh id="hero" castShadow receiveShadow>
        <TorusGeometry radius={1.2} tube={0.28} radialSegments={48} tubularSegments={18} />
        <StandardMaterial color="#f5c76b" roughness={0.28} metalness={0.8} />
    </Mesh>
    <HTML target="hero" mode="world" anchor="top-center">
        <Card>Native WebGPU</Card>
    </HTML>
</Scene3D>
```

The important rule: GSX syntax is sugar over the same typed contract. It must not introduce a separate renderer-only schema.

### 4.3 Server-driven diffs

Live state mutation should use diff commands, not page re-render payloads:

```go
prev := state.Scene.SceneIR()
state.Scene.Graph.Replace("hero", updatedMesh)
next := state.Scene.SceneIR()
hub.Broadcast("scene.diff", scene.DiffCommands(prev, next))
```

Diff support must eventually cover every primitive, material, HTML surface, label, sprite, post-FX setting, and animation binding.

### 4.4 CSS-stylable scene state

Materials, lights, overlays, and post-FX should read CSS custom properties when requested:

```css
.product-viewer {
  --scene-accent: #77c6ff;
  --scene-bloom-strength: 0.32;
}
@media (prefers-color-scheme: dark) {
  .product-viewer { --scene-bg: #05070b; }
}
```

```go
scene.StandardMaterial{Color: "var(--scene-accent)"}
```

CSS should drive style, not geometry topology. Geometry topology changes should stay in typed Go/IR for predictability.

### 4.5 HTML-in-canvas and canvas-in-HTML

Scene3D needs two HTML composition modes:

1. **DOM overlay mode:** real DOM nodes positioned by the Scene3D projection system. Best for labels, tooltips, forms, panels, and accessible UI.
2. **Texture-backed HTML mode:** measured HTML or declarative UI rendered to an offscreen raster/texture and mapped onto world-space surfaces. Best for diegetic screens, dashboards inside 3D scenes, and VR-like panels.

Both modes must preserve one authoring mental model:

```go
scene.HTMLSurface{
    ID: "control-panel",
    Target: "console",
    Mode: scene.HTMLTexture,
    Width: 512,
    Height: 320,
    Node: ui.Card(...),
}
```

## 5. SceneIR contract

SceneIR is the boundary between authoring and runtime. It must be stable, versionable, and aggressively explicit.

### 5.1 Required top-level fields

SceneIR should continue to carry:

- `objects`: ordinary mesh primitives and non-instanced objects.
- `models`: GLB/glTF model instances.
- `points`: point and sprite-particle layers.
- `instancedMeshes`: GPU-ready repeated primitive geometry.
- `instancedGLBMeshes`: repeated model geometry.
- `computeParticles`: GPU-computed particle systems.
- `animations`: procedural and asset-bound animation clips.
- `labels`: text overlays.
- `sprites`: projected image overlays.
- `html`: future first-class HTML surfaces/overlays.
- `lights`: all light records.
- `environment`: ambient, sky/ground, IBL, fog, exposure.
- `postEffects`: ordered post-FX chain.
- `shadowMaxPixels` and `postFXMaxPixels`: memory caps.
- `capabilities`: author/runtime requirements.
- `diagnostics`: optional backend decision metadata in dev mode.

### 5.2 Versioning

SceneIR should include an optional schema version when the payload is emitted outside same-version server/runtime bundles:

```json
{
  "schema": "gosx.scene3d.ir.v1",
  "objects": [],
  "lights": []
}
```

Runtime should accept missing schema as the current in-repo schema for backward compatibility, but external tooling, asset planning, and editor integrations should require explicit schema.

### 5.3 No semantic loss in lowering

Lowering must not discard fields just because a backend does not yet use them. For example, `TorusGeometry.Tube` should survive SceneIR even if one backend approximates it. Capability degradation is a renderer decision, not an IR omission.

## 6. Native WebGPU renderer contract

### 6.1 Backend selection

Scene3D should prefer WebGPU when all required capabilities are satisfied:

1. Browser exposes `navigator.gpu`.
2. Adapter can be acquired with requested `powerPreference`.
3. Required WebGPU features are present.
4. Required WebGPU limits are present.
5. Canvas/surface can be configured with requested alpha, color space, and tone mapping preferences.
6. Runtime can create the requested pipelines without device loss.

Fallback order:

1. WebGPU native renderer.
2. WebGL2 compatibility renderer.
3. Canvas/pixel fallback for labels, lines, and simple object previews.
4. Server-rendered fallback node.

The runtime must explain fallback with data attributes or diagnostics so users can see “shader-f16 missing” rather than “scene did not render.”

### 6.2 Pipeline categories

Native WebGPU should maintain stable pipeline categories:

- Unlit color pipeline for legacy prebatched passes and debug geometry.
- Lit PBR pipeline for primitive/model meshes.
- Skinned lit pipeline for GLB/skinned asset draws.
- Shadow depth pipeline for casters.
- World-line pipeline for thick lines and helpers.
- Surface texture pipeline for HTML/video/image surfaces.
- Point/particle render pipeline.
- Compute pipelines for particle simulation, culling, skinning precompute, and future meshlet/LOD transforms.
- Post-FX pipelines for bloom, tonemap, FXAA, DOF, SSAO, color grade, vignette.
- Picking/object-ID pipeline or second color attachment.

### 6.3 Primitive vertex layout

Native generated mesh primitives use four buffers:

- `@location(0)` position: `float32x3`.
- `@location(1)` color: `float32x3`.
- `@location(2)` normal: `float32x3`.
- `@location(3)` uv: `float32x2`.

Instanced transforms use matrix columns through the existing instance-rate buffer. Future per-instance material/color overrides must use a dedicated instance attribute buffer or storage buffer, not mutate shared geometry buffers.

### 6.4 Native primitive catalog

This patch upgrades the WebGPU mesh primitive catalog from a partial set to the current built-in mesh primitive set plus compatibility aliases:

| Primitive | Aliases | Geometry | Normals | UVs | Notes |
|---|---|---:|---|---|---|
| Cube/Box | `cube`, `cubeGeometry`, `box`, `boxGeometry` | 36 verts | flat | face | unit envelope `[-1,1]` |
| Plane | `plane`, `planeGeometry`, `quad`, `quadGeometry` | 6 verts | flat +Y | quad | XZ plane at y=0 |
| Pyramid | `pyramid`, `pyramidGeometry` | 18 verts | flat | face | square base, apex at y=1 |
| Sphere | `sphere`, `sphereGeometry`, `uvSphere`, `uvSphereGeometry` | generated | smooth | lat/long | default 32x16 for native path |
| Cylinder | `cylinder`, `cylinderGeometry` | generated | smooth sides, flat caps | wrapped/caps | default 32 segments |
| Cone | `cone`, `coneGeometry` | generated | smooth sides, flat cap | wrapped/cap | compatibility alias via frustum generator |
| Torus | `torus`, `torusGeometry` | generated | smooth | parametric | default 32x16 |

The renderer must not silently skip any primitive emitted by Scene3D’s built-in mesh geometry path.

### 6.5 Future primitive catalog

The next primitives should be added only when their full path is specified:

- Capsule.
- Rounded box.
- Beveled text mesh.
- Extruded SVG/path mesh.
- Terrain grid.
- Volume cube/3D texture slice proxy.
- Meshlet-backed arbitrary geometry.
- Spline/tube curves.
- Decal projection mesh.
- Thick world lines and dashed world lines.

Each must include vertex layout, bounds, picking behavior, material compatibility, shadow behavior, LOD behavior, and fallback behavior.

## 7. HTML-in-canvas flows

### 7.1 DOM overlay mode

DOM overlay mode positions real HTML above the canvas. It is appropriate when the user needs accessibility, text selection, form controls, IME, focus rings, links, menus, and responsive layout.

Contract:

- Overlay root is owned by Scene3D mount.
- Each overlay has a target world position or target object ID.
- Projection runs after camera updates and before browser paint.
- Occlusion can be enabled by reading depth/picking buffer or approximate depth sorting.
- Collision policy can hide, stack, pin, or fade overlapping labels.
- Overlay events can route to islands or scene signals.
- Server fallback emits the overlay content in document order for accessibility.

Proposed API:

```go
scene.HTML{
    ID: "tooltip",
    Target: "mesh-42",
    Mode: scene.HTMLDOM,
    Anchor: scene.AnchorTopCenter,
    Collision: scene.CollisionStack,
    Occlude: true,
    Node: gosx.El("button", gosx.Text("Inspect")),
}
```

### 7.2 Texture-backed HTML mode

Texture-backed HTML mode turns HTML into a GPU texture mapped onto a world-space surface. It is appropriate for screens inside 3D, dashboard panels, non-focusable mini UIs, and animated UI textures.

Contract:

- The HTML subtree is measured offscreen.
- Rasterization happens through browser-native APIs where possible; when unavailable, fallback to server prerender or DOM overlay mode.
- Texture updates are dirty-region based.
- Pointer events map from world hit → local UV → DOM-like event coordinate.
- A DOM fallback exists for accessibility and non-WebGPU browsers.
- Texture memory is capped by scene policy and device limits.

Proposed API:

```go
scene.HTMLSurface{
    ID: "hud-panel",
    Mode: scene.HTMLTexture,
    Width: 1024,
    Height: 512,
    Geometry: scene.PlaneGeometry{Width: 2.4, Height: 1.2},
    Material: scene.SurfaceMaterial{Roughness: 0.4, Emissive: 0.3},
    Node: DashboardPanel(model),
}
```

### 7.3 Event routing

Scene3D should expose event signals and typed handlers:

- `$scene.event.type`
- `$scene.event.pointerX`
- `$scene.event.pointerY`
- `$scene.event.targetID`
- `$scene.event.targetIndex`
- `$scene.event.hoverID`
- `$scene.event.downID`
- `$scene.event.selectedID`
- `$scene.event.clickCount`
- `$scene.event.revision`

World/texture HTML events must preserve this chain:

1. Canvas pointer event enters Scene3D.
2. Renderer resolves object ID and local hit metadata.
3. Runtime maps object ID to scene node and optional HTML surface.
4. HTML event is delivered to island/DOM/texture surface handler.
5. Shared signals update for surrounding islands.

## 8. Asset pipeline contract

Scene3D asset planning should be deterministic and low-memory.

### 8.1 Inventory

The planner should inventory:

- `.glb` / `.gltf`
- `.bin` buffers
- KTX2/Basis textures
- PNG/JPEG/WebP/AVIF textures
- HDR/EXR environment maps
- WGSL shader modules
- USDZ/AR exports
- Audio and video surfaces
- HTML texture manifests

### 8.2 Optimization plan

The planner should emit a manifest with:

- Variant targets by capability tier.
- Mesh compression plan.
- Texture transcode plan.
- KTX2 upload metadata.
- Mip chains.
- IBL prefilter plan.
- Split-sum LUT plan.
- Meshopt/Draco plan when enabled.
- LOD stacks.
- Vertex/animation quantization plan.
- Expected GPU memory.
- Expected first-frame upload budget.

### 8.3 Runtime loading

Runtime loading must be incremental:

1. Parse manifest.
2. Load critical mesh/materials first.
3. Render preview LOD as soon as possible.
4. Stream higher LODs and texture mips opportunistically.
5. Upgrade in-place through scene diffs/bundle resource replacement.
6. Report stalled/failed assets through diagnostics.

## 9. Materials and shading

### 9.1 Material model

Standard material should remain roughness/metalness PBR and carry:

- base color and base color map
- normal map
- roughness map
- metalness map
- emissive color/map/strength
- opacity/blend mode/render pass
- clearcoat
- sheen
- transmission
- iridescence
- anisotropy
- optional custom shader hooks

The WebGPU material uniform layout must be documented and tested whenever fields are added.

### 9.2 Custom materials

Custom WGSL hooks should be sandboxed by convention:

- Users can provide vertex displacement and fragment shading fragments.
- Hooks receive stable structs with position, normal, uv, material, environment, time, and custom uniforms.
- Hooks cannot mutate global renderer state.
- Invalid WGSL fails with a clear diagnostic and fallback material.

### 9.3 CSS variables

CSS-driven material fields should resolve during scene planning or runtime style sync. The renderer should consume resolved values, not CSS strings.

## 10. Lighting, shadows, and environment

### 10.1 Lights

All light types must share semantics across backends:

- Ambient light.
- Directional light.
- Point light.
- Spot light.
- Hemisphere light.
- Rect area light.
- Light probe.

Current WebGPU lighting can be approximate for area/probe lights, but IR must preserve fields so better implementations can land without API churn.

### 10.2 Shadows

Shadow policy:

- Scene-level pixel cap controls memory.
- Per-light shadow size declares desired quality.
- Renderer clamps per-light allocation to cap.
- Cascades are explicit renderer policy.
- Shadow casters/receivers are object-level fields.
- Debug overlay can show effective shadow sizes and cascade splits.

### 10.3 Environment

Environment must cover:

- Ambient color/intensity.
- Sky and ground color/intensity.
- Cubemap/HDR IBL.
- Exposure.
- Tone mapping.
- Fog.
- Env rotation.
- Transition/in/out/live state.

## 11. Interaction and picking

### 11.1 Object picking

WebGPU picking should support:

- Object ID.
- Instance ID.
- Optional triangle/primitive ID for advanced tools.
- Local hit position.
- UV hit for texture-backed HTML.
- Depth.

The existing ID-buffer readback path is a good base. It should evolve from integer-only result to a structured result:

```go
type PickResult struct {
    ObjectID string
    ObjectIndex int
    InstanceIndex int
    LocalPosition scene.Vector3
    WorldPosition scene.Vector3
    UV scene.Vector2
    Depth float64
}
```

### 11.2 Controls

Control modes:

- Orbit.
- First-person.
- Fly.
- Drag-to-rotate.
- Pointer lock.
- Transform controls.
- Focus target transitions.

Controls must be independent from rendering backend and must emit the same camera state into SceneIR/render bundle.

## 12. Performance contract

### 12.1 Frame budget

Scene3D should target:

- Static scenes: no RAF when nothing changes.
- Interactive scenes: stable 60 FPS on mainstream hardware for moderate scenes.
- Heavy scenes: adaptive DPR/post-FX quality without content flicker.
- First paint: preview/LOD before full-quality assets where configured.

### 12.2 Memory budget

Memory policy should cap:

- Shadow textures.
- HDR/post-FX intermediates.
- Bloom chain textures.
- HTML textures.
- Texture atlases.
- Particle buffers.
- Instance buffers.
- Readback buffers.

Runtime diagnostics should expose approximate allocations by category.

### 12.3 GPU upload budget

The renderer must avoid repeated uploads:

- Primitive geometry buffers are cached by primitive key.
- Materials are cached by fingerprint.
- Textures are cached by URL + sampler policy.
- Instance buffers grow one-way or use pooled slabs.
- Static meshes retain GPU buffers across frames.

## 13. Diagnostics and tooling

Scene3D should expose:

- Backend selected.
- Adapter name/type where browser permits.
- Required features/limits and which failed.
- Render scale/DPR.
- Frame time and moving average.
- Draw call count.
- Instance count.
- Triangle count.
- Texture memory estimate.
- Shadow memory estimate.
- Post-FX memory estimate.
- Pipeline compilation errors.
- Asset loading errors.
- Fallback reasons.

Diagnostics should appear in development mode through mount data attributes, optional stats overlay, and a structured JS/Go debug API.

## 14. Testing strategy

### 14.1 Unit tests

- Primitive generators: non-empty buffers, correct lengths, finite positions, finite UVs, unit normals, expected vertex counts.
- Material fingerprinting and uniform packing.
- SceneIR marshaling.
- Diff commands.
- Capability parsing.
- Asset planner manifests.

### 14.2 Headless renderer tests

- Frame path creates expected buffers/textures/passes.
- Primitive cache reuse.
- Resize behavior.
- Shadow pass participation.
- Post-FX chain pass count.
- Device lost handling.
- Picking request lifecycle.

### 14.3 Browser tests

- WebGPU mounts and renders first frame.
- WebGPU fallback reason is shown when unavailable.
- Orbit controls move camera.
- Labels track world positions.
- HTML overlay receives click/focus.
- Texture-backed HTML maps pointer hits to UV/local coordinates.
- GLB loading and animation playback.
- Server diff update replaces an object without remounting scene.

### 14.4 Golden tests

Goldens should cover:

- Basic primitive scene.
- PBR material scene.
- Shadow scene.
- Point/particle scene.
- HTML overlay scene.
- Post-FX scene.
- GLB scene.

Goldens should be backend-aware: WebGPU and WebGL can have small image differences, but semantic buffers and diagnostics should match.

## 15. Implementation roadmap

### R1: Native primitive mesh completeness

Implemented in this pass:

- Expand `render/bundle/primitive.go` from cube/box + plane + sphere to cube/box, plane/quad, pyramid, sphere, cylinder, cone, and torus.
- Preserve non-indexed buffer contract.
- Generate positions, colors, normals, and UVs for every primitive.
- Add aliases matching Scene3D type names and legacy lowercase names.
- Add tests for buffer lengths, vertex counts, finite values, and unit normals.

### R2: SceneIR parameter preservation for native primitives

Implemented in this pass:

- Ensure primitive-specific parameters (`Width`, `Height`, `Depth`, `Radius`, `Segments`, `Tube`, `RadialSegments`, `TubularSegments`) are preserved into render-bundle keys.
- Cache primitive geometry by `(kind, parameters)` instead of kind only.
- Respect authored segment counts while clamping to backend/device budgets.
- Add bounds per primitive for culling rather than fixed default radius.
- Preserve the same parameter fields through `scene.IR`, legacy SceneIR compatibility maps, `engine.RenderBundle`, runtime JS normalization, scene-planner hashing, WebGL compatibility geometry, and WebGPU instanced geometry lookup.
- Generate WebGL/WebGPU browser-side PBR geometry for pyramid, cylinder, cone, and torus through the shared `generateInstancedGeometry` path so the browser fallback stack no longer treats those primitives as box-shaped.
- Include primitive parameters in prepared-scene signatures so parameter-only edits rebuild draw plans and GPU geometry instead of reusing stale primitive buffers.
- Add canvas/wire fallback segments for cylinder, cone, and torus so non-PBR fallback views still expose the authored topology visibly.

### R3: Native line/surface pipelines

- Implement WebGPU thick world-line pipeline with screen-space expansion.
- Implement dashed line material in WebGPU rather than WebGL escape hatch.
- Implement helper primitives through the line pipeline: axes, grids, box helpers, skeleton helpers, transform controls.
- Implement textured surface pipeline for decals, sprites, image planes, video planes, and HTML texture surfaces.

### R4: HTML surface system

Started in follow-on passes:

- Add typed `scene.HTML` and `scene.HTMLSurface` nodes. `HTMLSurface` currently lowers as an explicit texture-mode DOM fallback until the GPU texture manager lands.
- Add DOM overlay manager.
- Start texture-backed HTML manager metadata by preserving target IDs and explicit fallback diagnostics through IR, bundles, and DOM attributes.
- Add pointer mapping from pick result to local UV and dispatch texture-surface pointer events to the DOM fallback surface.
- Add HTML texture keys, texture dimensions, surface world dimensions, per-surface texture memory budgets, DOM diagnostics, render-bundle surface metadata, and a ready-texture path that emits texture-backed HTML as ordinary render surfaces.
- Add accessibility fallback rendering.
- Add dirty-region texture uploads.

### R5: Material and post-FX coverage

- Add native WebGPU SSAO and DOF passes.
- Complete clearcoat/sheen/transmission/iridescence/anisotropy in WebGPU material path.
- Add custom WGSL hooks with compile diagnostics.
- Add material variants by capability tier.

### R6: Asset/LOD/streaming coverage

- Parameterized mesh cache.
- HTML texture manifest inventory and upload-budget planning.
- Meshlet/LOD support.
- KTX2 native upload paths.
- HDR/IBL runtime integration.
- Progressive asset streaming and upgrade.

### R7: Editor/devtool coverage

- Scene inspector.
- Object picking overlay.
- Live material editing.
- Camera bookmark capture.
- Performance flame chart for Scene3D passes.
- Asset memory panel.

## 16. Acceptance criteria

Scene3D reaches the intended standard when:

1. A user can build a product configurator with GLB assets, PBR materials, shadows, post-FX, labels, forms, and variant selection without JavaScript.
2. A user can build a scientific visualization with instancing, compute particles, field data, camera controls, and UI overlays without JavaScript.
3. A user can build an in-canvas dashboard with real HTML overlays and texture-backed panels without manual DOM projection code.
4. Every built-in primitive has WebGPU, WebGL/headless, docs, and tests.
5. Fallbacks are explicit and diagnostic-rich.
6. Static scenes do not spin a render loop.
7. Dynamic scenes expose measurable frame/memory/upload budgets.
8. Server-driven scene diffs can update objects/materials/labels/HTML without remounting.
9. The public docs never need to say “this works only in the JS renderer” except for explicitly deprecated compatibility features.

## 17. Immediate patch summary

This implementation pass removes the most visible primitive incompleteness in the native WebGPU bundle path. It makes the native renderer recognize and generate GPU-ready geometry for the built-in mesh primitives that authors expect to work: pyramid, cylinder, cone, and torus join cube/box, plane, and sphere. The implementation keeps the renderer’s existing non-indexed vertex-buffer contract, which minimizes risk: no pipeline layouts change, no shader inputs change, and the existing `ensurePrimitive` upload path continues to work.

This pass also executes the next required primitive parameter step. Native primitive cache keys now include the semantic shape parameters, authored segment counts are clamped rather than discarded, bounds use the effective primitive dimensions, and browser runtime paths preserve the same fields into WebGL/WebGPU geometry generation and scene-planner hashes.

## 18. Execution ledger

The implementation surface covered by this pass is:

- Go authoring and IR: typed primitive parameters on `InstancedMesh`, `InstancedMeshIR`, compatibility lowering, and `IRInstancedMesh`.
- Render bundle: parameter fields on `engine.RenderInstancedMesh` and parameter-aware primitive cache keys.
- Native renderer: WebGPU/headless primitive generation for cube/box, plane/quad, pyramid, sphere, cylinder, cone, and torus; per-primitive cull radius; per-mesh cull inputs for shadow passes.
- Browser runtime: Scene3D kind alias normalization, parameter preservation, planner hashes, WebGL shared primitive geometry generation, WebGPU primitive geometry lookup, and canvas wire fallback segments.
- Tests: Go primitive buffer tests, Go SceneIR lowering tests, native frame/shadow pass tests, JS runtime parameter preservation tests, and bootstrap source bridge tests.

## 19. Structured pick/event execution pass

This follow-on pass starts the bridge required by sections 7 and 11:

- JS raycast picking now returns structured hit metadata for mesh triangles: object index, instance index when known, triangle/primitive index, world position, local position, UV, and depth.
- `SceneEventSignals` now declares scalar event fields for `targetInstanceIndex`, `targetPrimitiveIndex`, `targetTriangleIndex`, `worldX/Y/Z`, `localX/Y/Z`, `uvX/Y`, and `depth`.
- The browser event namespace publishes those fields through `$scene.event.*`, and `gosx:engine:scene-interaction` details carry the same metadata.
- Bounds fallback picking publishes explicit defaults for unsupported fields and still exposes depth.
- The WASM native pick bridge now publishes explicit defaults for the structured fields until GPU readback evolves beyond object ID.
- Runtime tests cover structured raycast UV interpolation and event signal/detail publication.

## 20. Native pick identity execution pass

This pass moves the Go/WebGPU-facing picker from raw ID readback toward the section 11 `PickResult` contract:

- `render/bundle.PickResult` and `PickResultCallback` now exist alongside the legacy `PickCallback`.
- `Renderer.QueuePickResult` returns structured identity metadata while `QueuePick` remains backward-compatible and still returns only the numeric ID.
- Native instance records now pack a matrix plus `vec4<u32>` pick metadata, so the culling compute pass preserves stable pick IDs while compacting visible instances.
- The lit and skinned lit pipelines consume that pick metadata and write nonzero IDs to the R32Uint pick attachment.
- Pick IDs map back to `ObjectID`, object index, and instance index from the submitted `engine.RenderBundle`.
- The WASM bridge now forwards object IDs and instance indices from `PickResult` into `$scene.event.*` when the native renderer path is used.
- Tests cover pick-target mapping, fallback IDs, instance-record packing, pipeline vertex layout, and wasm compile-time bridge compatibility.

## 21. Native primitive hit reconstruction pass

This pass fills the next part of the native `PickResult` contract without adding heavyweight GPU attachments:

- `QueuePickResult` snapshots the current render bundle and reconstructs the pick ray from camera, viewport, and requested pixel.
- Native primitive meshes are ray-tested on the CPU against the same generated primitive geometry used for WebGPU uploads.
- Structured hit metadata now includes triangle/primitive index, interpolated UV, local hit position, world hit position, and ray depth for built-in primitive meshes.
- The GPU ID buffer remains the authoritative visibility source; CPU reconstruction enriches the resolved ID rather than choosing a different target.
- Tests cover center-pixel primitive ray hits, depth, local/world coordinates, UV bounds, and identity preservation.

Remaining work:

- Add DOM accessibility fallbacks and dirty-region texture upload accounting for `HTMLSurface`.

## 22. HTML texture-surface event routing pass

This pass closes the first end-to-end event-routing gap for section 7.2:

- `scene.HTML` and `scene.HTMLSurface` now preserve `Target` into `HTMLIR`, legacy SceneIR maps, and canonical `IRHTMLNode` records instead of using it only for anchored projection.
- `HTMLSurface` fallback lowering now marks the degradation explicitly as `fallback: "dom-overlay"` with reason `html-texture-manager-unavailable`.
- JS Scene3D normalization and render-bundle construction preserve `target`, `fallback`, and `fallbackReason` for `html` entries.
- The DOM overlay manager exposes those fields as `data-gosx-scene-html-target`, `data-gosx-scene-html-fallback`, and `data-gosx-scene-html-fallback-reason`.
- Structured pick details now route into texture-mode HTML entries whose `target` matches the picked object ID.
- Matched texture entries receive a `gosx:scene-html-texture-pointer` event with the source scene event, UV coordinates, local pixel coordinates, target ID, pointer position, and fallback diagnostics.
- The DOM fallback element also receives hit attributes and CSS variables for the latest texture pointer coordinate so islands and CSS can inspect the projected hit without custom projection code.
- Go and JS tests cover target preservation, fallback metadata, rendered DOM attributes, and texture pointer dispatch from declarative Scene3D picking.

Remaining work:

- Replace the DOM fallback for `HTMLSurface` with a real measured/rasterized HTML texture manager where browser capability permits.
- Add dirty-region uploads, texture memory accounting, and accessibility mirror rendering for texture-backed HTML.

## 23. HTML texture-surface budget and metadata pass

This pass makes texture-backed HTML explicit enough for the future raster/upload manager to attach without another schema change:

- `scene.HTML` and `scene.HTMLSurface` now preserve `TextureKey`, `TextureWidth`, `TextureHeight`, `MaxTexturePixels`, `SurfaceWidth`, and `SurfaceHeight` through SceneIR, legacy maps, canonical IR, JS normalization, and engine render bundles.
- `HTMLSurface.Width` and `HTMLSurface.Height` remain accepted as legacy texture-pixel dimensions; `SurfaceWidth` and `SurfaceHeight` now describe the world-space plane size. Small legacy sizes still map to world dimensions for compatibility.
- The scene package exposes named HTML texture pixel caps for 512, 1024, and 2048 square textures, plus an unbounded sentinel for explicit opt-in.
- The browser runtime computes per-surface texture bytes, cap bytes, over-budget state, and ready state. Over-budget texture surfaces stay in DOM fallback mode with reason `html-texture-memory-cap`.
- Ready texture-mode HTML entries emit ordinary render-bundle surfaces with source metadata, texture dimensions, memory accounting, alpha material routing, UVs, bounds, and fallback diagnostics.
- WebGL and WebGPU surface paths skip pending HTML texture surfaces; ready surfaces use the existing native textured surface pipeline.
- DOM overlays expose per-entry texture diagnostics and aggregate layer diagnostics through `data-gosx-scene-html-texture-*` attributes.
- Tests cover Go lowering defaults, cap constants, DOM diagnostics, ready render-surface emission, over-budget suppression, and bootstrap size budgets.

Remaining work:

- Implement browser measurement/rasterization, dirty-region texture upload, and texture cache disposal for HTML textures.
- Mirror texture-mode HTML into accessible DOM in document order when it is not already visible as the fallback element.

## 24. HTML texture lifecycle diagnostics pass

This pass adds the lifecycle accounting needed before a real browser raster/upload manager lands:

- Mounted Scene3D instances now keep per-texture HTML records keyed by HTML ID or texture key.
- Runtime diagnostics track texture revision, dirty state, dirty bytes, pending upload bytes, disposal count, and disposed bytes.
- Texture entries are marked dirty when their key, size, budget, byte count, or markup signature changes.
- Ready texture entries clear dirty/pending upload state; over-budget entries do not schedule pending uploads.
- Removed entries are disposed from the mount-local lifecycle registry and counted in aggregate diagnostics.
- Per-entry and aggregate lifecycle state is exposed through `data-gosx-scene-html-texture-*` attributes on the fallback DOM and HTML overlay layer.
- Tests cover dirty and pending-upload diagnostics for texture-mode DOM fallback entries while preserving bootstrap size budgets.

Remaining work:

- Connect this lifecycle registry to a real measured HTML rasterizer, GPU texture cache, dirty-rectangle upload queue, and renderer texture resolver.
- Add accessibility mirror bookkeeping for ready texture surfaces whose DOM fallback is visually hidden.

## 25. HTML texture manifest inventory pass

This pass closes the first R6 inventory gap for HTML texture surfaces:

- `assetpipe` now recognizes `html-texture-manifest` assets through `.htmltex`, `.htmltexture`, and path-patterned JSON manifests such as `*.html-textures.json`.
- The planner probes manifests conservatively, counting surfaces, texture pixels, estimated RGBA bytes, max texture pixel budgets, dirty-region entries, and accessibility fallback declarations.
- Report totals now expose `htmlTextureManifest`.
- Planner actions now include `measure-html-textures`, `enforce-html-texture-budgets`, `dirty-region-upload-plan`, and `accessibility-mirror-dom`.
- Tests cover classification, probe totals, action status, and action targets.

Remaining work:

- Feed HTML texture manifests into runtime loading so measured/rasterized texture assets can stream and upgrade in place.
- Add manifest-backed LOD/mip selection for HTML textures alongside model and image texture variants.

## 26. Native WebGPU SSAO and DOF pass

This pass starts the R5 post-FX work with real WebGPU depth-backed effects:

- The WebGPU post processor now creates a sampleable depth attachment for post-FX scenes.
- `SCENE_POST_SSAO` is handled as a depth-backed fullscreen post pass instead of being preserved but ignored.
- The SSAO WGSL pass samples the rendered depth buffer, applies radius/intensity/bias parameters, and darkens the color input by screen-space ambient occlusion.
- `SCENE_POST_DOF` now runs as a depth-backed fullscreen post pass using focus distance, aperture, and max blur parameters.
- Both passes participate in the existing post-FX ping-pong chain, so SSAO and DOF can be ordered with bloom, tonemap, vignette, and color grade.
- WebGPU frame diagnostics now expose post-effect count, SSAO pass count, and DOF pass count through `data-gosx-scene3d-webgpu-post-*` attributes.
- The WebGPU feature bundle size budget was raised to account for the intentional post-FX shader and pass machinery; gzip and brotli budgets still hold.
- JS tests cover the depth texture binding, SSAO/DOF shaders, switch cases, pass counters, diagnostics attributes, and size budget.

Remaining work:

- Keep browser and Go-native SSAO/DOF output visually aligned through backend-aware screenshot/golden coverage.

## 27. Native bundle material, post-FX, and animation preservation pass

This pass closes several semantic-loss gaps between SceneIR, the shared engine VM, and the native Go render bundle:

- `engine.RenderBundle` now carries structured diagnostics so backend degradations can be asserted by tests and surfaced by tooling.
- `engine.RenderPostEffect` preserves effect `mode` plus arbitrary numeric parameters for native bundle consumers.
- `client/enginevm` now preserves Scene3D post effects into `engine.RenderBundle.PostEffects`.
- The native engine VM marks unsupported post-FX as explicit `native-postfx-unsupported` diagnostics instead of silently dropping them; later passes have moved SSAO, DOF, vignette, color grade, and tone mapping into the supported native set.
- `client/enginevm` now preserves PBR material fields into `engine.RenderMaterial`: roughness, metalness, normal map, roughness map, metalness map, and emissive map.
- Material profile registration can provide those PBR fields as defaults, and authored object fields override them.
- Material equality and material cache keys now include those PBR fields, preventing two semantically different native materials from sharing one cache entry.
- Top-level scene animations are preserved into `engine.RenderBundle.Animations`, including target ID, property, interpolation, times, values, and duration.
- Go tests cover render-bundle JSON round trips, native engine VM post-FX preservation/degradation diagnostics, PBR material propagation, material cache keys, and animation propagation.

Remaining work:

- Extend native Go post-FX with additional effects such as SSAO variants, DOF quality tiers, SSAO/DOF visual tuning, and future SSAO/DOF golden coverage.
- Extend procedural animation beyond instanced primitive transforms to materials, labels, sprites, HTML, and skeletal clip blending.

## 28. Server-driven diff coverage pass

This pass expands server-driven Scene3D updates toward the no-remount contract:

- `scene.DiffCommands` now emits collection replacement commands for models, instanced GLB meshes, animations, environment, particles, compute particles, instanced primitive meshes, post-FX, and compatibility records.
- `scene.DiffIRCommands` now emits camera, environment, and canonical material commands.
- The browser command bridge now applies model, instanced GLB, animation, environment, particle, instanced primitive, material, and post-effect commands.
- Model and instanced-GLB commands trigger asynchronous model hydration and remove previous model-owned objects, points, labels, sprites, HTML overlays, and lights before installing replacements.
- Instanced GLB batches expand through the existing model hydration path as a compatibility implementation, preserving source URL, per-instance transforms, material overrides, pickability, and static flags.
- Top-level animation commands update normalized runtime clip state.
- Environment commands update ambient, sky, ground, exposure, tone mapping, fog, and IBL fields through the same normalizer used at mount.
- JS tests cover model replacement/removal, instanced GLB expansion, animation command application, environment command application, and generated bootstrap bundle budgets.
- Go tests cover model, instanced GLB, animation, environment, canonical camera, and material command emission.

Remaining work:

- Replace compatibility instanced-GLB expansion with a true native instanced model draw path once model mesh storage can be shared per source asset.
- Add granular model subresource replacement when assets stream or upgrade LOD in place.
- Add environment memory diagnostics for IBL/HDR resources and shadow/post-FX interactions.

## 29. SceneIR schema identity pass

This pass makes external SceneIR payloads explicitly versionable without breaking existing same-version runtime bundles:

- `scene.SceneIRSchema` defines the current compatibility schema identifier: `gosx.scene3d.ir.v1`.
- `scene.SceneIR` now preserves an optional `schema` field in direct JSON and legacy scene maps.
- Missing schema remains accepted for backward compatibility.
- The browser SceneIR validator accepts missing schema, accepts `gosx.scene3d.ir.v1`, and rejects unknown schemas with a concrete diagnostic.
- The shared Scene3D JS API exports `SCENE_IR_SCHEMA` for tooling and tests.
- Go and JS tests cover schema preservation and validation.

Remaining work:

- Require explicit schema for standalone editor, asset-planning, or external-tooling payloads while continuing to accept missing schema for same-version in-repo bundles.

## 30. Native Go SSAO and DOF execution pass

This pass ports the first depth-backed post-FX work from the browser WebGPU path into the native Go `render/bundle` backend:

- `render/bundle` now builds native SSAO and DOF fullscreen pipelines alongside bloom, FXAA, shadow, lit, unlit, surface, particle, line, and picking pipelines.
- The main native depth attachment is created with texture-binding usage so post-FX shaders can read scene depth after the primary render pass.
- Native post-FX resources maintain a secondary HDR scratch target plus dedicated uniform buffers and bind groups for SSAO and DOF.
- SSAO executes as a depth-backed color pass using authored radius, intensity, and bias parameters.
- DOF executes as a depth-backed color pass using authored focus distance, aperture, and max blur parameters.
- SSAO and DOF run before bloom and final presentation, and the bloom/present bind groups are rebound to whichever HDR target is final after the native post-FX chain.
- `client/enginevm` now treats `bloom`, `ssao`, and `dof` as supported native post effects. Later passes add vignette, color grade, and tone mapping to the native-supported set.
- Go tests cover native pipeline creation, depth texture usage flags, pass ordering, post-FX pass labels, no-depth fullscreen pass attachments, uniform packing, post-effect preservation, and unsupported-effect diagnostics.

Remaining work:

- Tune the native SSAO and DOF WGSL visually against the browser WebGPU path with image-level browser/device coverage.
- Keep native SSAO/DOF resource usage within post-FX memory budgets and add browser/device visual comparisons.

## 31. Native Go textured surface execution pass

This pass closes the first native surface-pipeline gap in the Go WebGPU bundle renderer:

- `render/bundle` now builds dedicated textured surface pipelines for opaque, alpha, and additive render passes.
- Native surfaces consume the existing render-bundle contract: world-space `positions`, `uv`, `vertexCount`, `materialIndex`, `renderPass`, `depthCenter`, `textureKey`, `textureReady`, and `viewCulled`.
- Surface shaders sample the material base-color texture, multiply by resolved material color and opacity, and apply emissive boost for HTML/diegetic panels.
- Opaque surfaces depth-test and depth-write. Alpha and additive surfaces depth-test without depth writes.
- Alpha surfaces sort back-to-front by `depthCenter`, matching the browser WebGPU/WebGL compatibility order.
- Pending texture-mode HTML surfaces remain skipped until `textureReady` is true, so the native path does not accidentally draw invisible or placeholder HTML panels.
- Surface position and UV buffers are cached per current surface identity and rewritten when dynamic surface geometry changes; removed surfaces are pruned from the cache.
- Surface material bind groups are created per surface pipeline layout while reusing the existing material fingerprint, material uniform, and texture cache machinery.
- Go tests cover pipeline construction, blend/depth state, native surface draw dispatch, alpha ordering, pending HTML texture suppression, and texture-cache resolution.

Remaining work:

- Complete JS host-side decoded image/canvas/video/HTML texture registration through the WASM bridge.
- Route surface picking IDs and UV/local-hit enrichment through the browser event bridge so texture-backed HTML can receive native Go renderer pointer events.
- Add visual browser coverage for opaque, alpha, additive, image, video, and HTML texture surfaces across WebGPU and fallback paths.

## 32. Native Go vignette and color-grade execution pass

This pass shrinks the remaining native post-FX degradation set:

- `render/bundle` now builds native HDR fullscreen pipelines for vignette and color grade alongside SSAO and DOF.
- Vignette uses the existing authored `intensity` parameter and applies the same screen-edge darkening curve as the browser WebGPU/WebGL paths.
- Color grade uses authored `exposure`, `contrast`, and `saturation` parameters and applies the same multiply/contrast/luma-mix sequence as the browser WebGPU/WebGL paths.
- Vignette and color grade participate in the same HDR ping-pong chain as SSAO and DOF, preserving authored post-effect order before bloom and final presentation.
- Native post-FX resources now allocate per-effect uniform buffers and bind groups for SSAO, DOF, vignette, and color grade.
- `client/enginevm` now treats `bloom`, `ssao`, `dof`, `vignette`, and `colorGrade` as supported native Go post effects. A later pass adds tone mapping to the supported native set.
- Go tests cover native pass ordering, uniform packing, construction counts, and the diagnostic boundary around the supported native post-FX set.

Remaining work:

- Keep authored tone-map mode and exposure visually aligned with browser WebGPU/WebGL output.
- Add browser/device visual comparisons for post-FX ordering when SSAO, DOF, vignette, color grade, bloom, and FXAA are all active.

## 33. Native Go procedural animation application pass

This pass moves top-level animation clips from “preserved for tooling” into the native render path for instanced primitive meshes:

- `render/bundle.Renderer.Frame` now applies `RenderBundle.Animations` before pick target preparation, culling, shadow passes, and main-pass draws.
- Animation channels can target instanced mesh IDs, zero-based mesh indexes, or one-based mesh indexes.
- Supported properties are `translation`/`position`, `rotationX`, `rotationY`, `rotationZ`, scalar or vector `scale`, `scaleX`, `scaleY`, `scaleZ`, and full `matrix`/`transform`.
- Linear and step interpolation are supported; clip duration wraps sampling time when supplied.
- Animated transforms are applied as an additional local transform over each authored instance transform, preserving base placement and allowing procedural overlays.
- The implementation clones only changed mesh records and transform slices, so calling code does not see its `RenderBundle` mutated by `Frame`.
- Native culling and draw upload now consume animated instance matrices, so shadows, visibility, main rendering, and structured pick ray enrichment share the same animated state.
- Go tests cover transform application through the actual frame path and verify the caller-owned transform slice is not mutated.

Remaining work:

- Extend animation application to non-instanced object bundles and textured surfaces where stable object IDs are available.
- Add material, post-FX, label, sprite, and HTML animation channels through the diff/runtime layer.
- Add skeletal clip scheduling and blending on top of the existing bone-palette upload path.

## 34. Native Go world-line execution pass

This pass makes world-space line buffers visible in the native Go WebGPU bundle renderer:

- `render/bundle` now builds a native `bundle.worldLine` pipeline using `line-list` topology.
- The pipeline consumes `RenderBundle.WorldPositions`, `WorldColors`, and `WorldVertexCount` directly instead of relying on legacy triangle-bundle pass data.
- World lines use the scene view-projection uniform, depth-test against scene depth, skip depth writes, alpha-blend color, and write pick ID `0` so they do not interfere with mesh picking.
- The renderer owns a reusable world-line buffer cache and rewrites positions/colors when line data changes.
- Odd vertex counts are clamped down to complete line pairs.
- Go tests cover pipeline construction, topology, depth state, frame dispatch, cache population, and RGBA color uploads.

Remaining work:

- Add native thick-line expansion so authored `LinesGeometry.Width` and helper widths render with screen-space thickness instead of backend hairlines.
- Preserve and consume per-segment line width, dash, dash-size, gap-size, and render-pass buckets in the shared `engine.RenderBundle` schema.
- Add dashed-line WGSL and helper-specific tests for axes, grids, box helpers, skeleton helpers, and transform controls.

## 35. Native Go physical material parity pass

This pass closes the missing native material fields that were already present in the typed Scene3D and SceneIR contracts:

- `engine.RenderMaterial` now preserves `clearcoat`, `sheen`, `transmission`, `iridescence`, and signed `anisotropy`.
- `client/enginevm` resolves those fields from authored mesh props and registered material profiles, clamps them to their semantic ranges, and includes them in render-material equality and cache keys.
- Native material fingerprints include the physical PBR fields so two visually different standard materials no longer share one uniform buffer or bind group.
- The native material uniform grows to seven `vec4` records: base color, PBR scalars, emissive, two texture-flag vectors, and two physical-material vectors.
- The native lit shader consumes the fields with the same pragmatic approximations used by the browser WebGPU path: anisotropy adjusts effective roughness, clearcoat adds a glancing highlight, sheen adds fabric-like rim tint, iridescence adds view-angle color shift, and transmission blends toward ambient/transmitted base color for dielectric materials.
- Surface pipelines share the expanded material uniform layout so texture-backed surfaces remain compatible with the unified material cache.
- Go tests cover JSON round trips, engine VM propagation, material-key participation, uniform packing, and native shader/material construction.

Remaining work:

- Add image-level parity coverage against the browser WebGPU implementation for high-clearcoat, high-sheen, iridescent, transmissive, and anisotropic materials.
- Replace the approximation with a fuller physical model when native IBL, prefiltered environment maps, and transmission buffers are complete.

## 36. Native Go world-object mesh execution pass

This pass removes a major native renderer gap for non-instanced mesh/model buffers:

- `render/bundle` now recognizes `RenderBundle.Objects` that reference world-space triangle mesh buffers through `WorldPositions`, `WorldNormals`, `WorldUVs`, and `WorldColors`.
- Drawable object meshes use the existing native lit PBR pipeline, material cache, texture bindings, scene uniforms, depth buffer, and pick-ID color attachment.
- Each native object mesh receives an identity instance record so it can share the lit and shadow pipeline vertex layout with instanced primitive meshes without adding another shader input contract.
- Object meshes can participate in cascaded shadow passes when `CastShadow` is set.
- Native object pick IDs are allocated alongside instanced-mesh pick IDs; CPU ray enrichment can resolve triangle index, world/local hit position, UV, and depth for world-object mesh triangles.
- Legacy engine-VM line/helper objects are explicitly guarded out of the triangle path. They continue through the native world-line pipeline, so helper line buffers are not accidentally drawn as mesh triangles.
- The world-line upload path now filters out native object-mesh ranges when `RenderBundle.Objects` is present, preventing duplicate line rendering over model triangles.
- Go tests cover shadow/main-pass dispatch, lit pipeline usage, object vertex-buffer upload, non-mesh world-line preservation, and nonzero object pick-ID upload.

Remaining work:

- Extend native object drawing to indexed buffers once the render-bundle contract carries index buffers.
- Add per-object opaque/alpha/additive depth-write variants instead of sharing the current lit pipeline depth-write policy.
- Wire GLB/model hydration directly into shared native object mesh buffers so loaded model assets do not need compatibility pre-expansion.

## 37. Native Go decoded texture ingestion pass

This pass turns the native texture path from “checkerboard only unless KTX2 was pre-registered” into a real decoded-pixel upload surface:

- `render/bundle.Renderer.RegisterRGBATexture` lets hosts register already-decoded RGBA8 pixels under a stable material/HTML texture key.
- `render/bundle.Renderer.LoadImageTexture` decodes PNG, JPEG, and GIF bytes through Go's standard `image` decoders and uploads them as mipmapped RGBA textures.
- Material texture resolution now accepts raster `data:` URLs when they decode successfully, caches the uploaded texture by the original key, and keeps the procedural checker fallback for unsupported formats such as SVG/WebP/AVIF.
- Local file paths and `file://` URLs can be decoded in native/headless contexts where the file is available.
- Remote HTTP(S), blob, and `gosx-html://` keys remain explicit host-managed inputs so the browser/WASM layer can fetch or rasterize them and then call the decoded-pixel registration path.
- Existing KTX2 registration remains the GPU-native path for compressed and cubemap textures.
- Go tests cover data-URL decode/upload, mip upload metadata, and explicit RGBA texture registration/reuse.

Remaining work:

- Wire concrete JS host producers for decoded image/canvas/video/HTML texture pixels through the WASM registration bridge.
- Add WebP/AVIF decode through browser-hosted pixels or optional decoders rather than growing the core Go dependency surface.
- Carry texture diagnostics into `RenderBundle.Diagnostics` when a source falls back to checkerboard because the host did not provide decoded bytes.

## 38. Native Go surface picking and UV hit pass

This pass connects textured surfaces to the native picking/event contract instead of rendering them as unpickable color-only quads:

- Surface pipelines now carry a per-vertex `uint32` pick-ID buffer and write that ID into the native object-ID attachment.
- Surface pick IDs are allocated after instanced meshes and world-object meshes, sharing the same readback result table.
- Pending texture-mode HTML surfaces remain excluded from pick allocation until they are drawable, matching the render path.
- CPU ray enrichment now intersects surface triangles and fills triangle index, world/local hit position, UV coordinate, and depth.
- Texture-backed HTML, decals, image planes, and video planes can now receive native UV/local hit metadata once the host event bridge routes the surface pick result.
- Surface resource caching tracks pick-ID buffer size and rewrites it as IDs change between frames.
- Go tests cover pick-ID upload and surface UV enrichment through the same pick-ray path used by instanced primitives.

Remaining work:

- Route native surface pick results through the browser/WASM event bridge to texture-backed HTML local coordinates.
- Add world-line/helper picking and local segment hit metadata for transform controls and measurement tools.

## 39. Native Go tone-map mode pass

This pass removes the last preserved-but-unsupported post-FX effect from the native engine VM list:

- The native present/compose shader now supports authored tone-map modes instead of always applying ACES.
- Supported modes are `aces`, `reinhard`, and `filmic`, matching the typed Scene3D `Tonemap` modes.
- Tone-map exposure is resolved from the `toneMapping` post effect when present, otherwise from `RenderEnvironment.Exposure`, and defaults to `1`.
- `render/bundle` uploads a dedicated present uniform for tone-map mode and exposure alongside the existing bloom uniforms.
- `client/enginevm` now treats `toneMapping`, `tone-mapping`, and `tonemap` as native-supported post effects, so authored tone mapping no longer emits a fallback diagnostic.
- Go tests cover the native tone-map uniform and the engine VM diagnostic boundary.

Remaining work:

- Add image-level parity tests for ACES, Reinhard, and filmic output against the browser WebGPU/WebGL paths.
- Plumb browser color-space and HDR10 presentation preferences into native tone-map selection when host capabilities expose them.

## 40. WASM decoded texture upload bridge pass

This pass connects the decoded-texture native renderer API to the browser host boundary:

- `client/wasm` now exposes `__gosx_register_engine_rgba_texture(engineID, key, width, height, rgbaBytes)`.
- Browser code can fetch, decode, rasterize, or render an image/video/HTML/canvas surface into a `Uint8Array`, register it under the same key used by `RenderMaterial.Texture` or `RenderSurface.TextureKey`, and let `render/bundle` handle mip generation and GPU upload.
- The bridge validates argument count, byte-copy completeness, renderer existence, texture dimensions, and RGBA byte length through `RegisterRGBATexture`.
- This keeps browser-only decode APIs out of the core Go renderer while still making the native WebGPU path consume real decoded pixels.

Remaining work:

- Add JS host calls that decode `<img>`, `ImageBitmap`, canvas, video, and HTML texture raster outputs and feed this bridge before the next native frame.
- Surface upload diagnostics in mount data attributes when a texture key is referenced but no decoded bytes were registered.
