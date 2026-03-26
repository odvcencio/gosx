# CMS + Native 3D Showcase

## Summary

This spec ships one cohesive outcome in four linked workstreams:

1. **CMS completion**: bring the CMS demo up to the full GoSX app surface
2. **Framework extensions**: add the VM, type, input, and engine primitives needed for 3D
3. **WebGL renderer**: add `webgl.js` as a thin scene-command applier
4. **Gallery scenes**: ship six showcase scenes inside the CMS at `/demo/3d`

The gallery is the headline feature. The CMS completion is what makes the showcase believable.

### Delivery Order

The work lands in this order:

1. Close the remaining CMS Tranche 1 gaps so the existing file-routed demo feels like a full GoSX app.
2. Add the shared engine substrate in core behind the already-shipped `.gsx` runtime surface: opcodes, value types, engine program shape, scene reconciliation, and input plumbing.
3. Add `webgl.js` as framework infrastructure, not an app escape hatch.
4. Migrate the gallery from today's Canvas-backed `Scene3D` compatibility path onto the real VM → reconciler → renderer architecture.

### Current Baseline (2026-03-26)

The work is not starting from zero. The CMS demo already has:

- file-routed `.gsx` pages for the original app routes plus `/demo/3d` and `/demo/3d/[scene]`
- `page.server.go` loader coverage for the gallery overview
- `app.EnableNavigation()`, `server.Link()`, and `data-gosx-link` in place
- the shared `cms/` extraction done (`blocks`, `store`, `themes`, `runtime`)
- four live gallery scenes already rendering through the built-in Canvas-backed `<Scene3D>` path:
  Geometry Zoo, Blueprint Atrium, Signal Orbit, and Horizon Stacks

### Revised Scorecard

| Slice | Actual | Notes |
|-------|--------|-------|
| CMS Tranche 1 | ~70% | Gallery routing, `.gsx` shells, loaders, navigation, and current Scene3D rendering are working |
| Framework engine surfaces | Done | `<Scene3D>`, `<Surface>`, and `<Worker>` are first-class in file-routed `.gsx` pages |
| Framework 3D primitives | Partial | `box`, `sphere`, `pyramid`, and `plane` exist in the current Canvas-backed renderer |
| ISR | Done | Full ISR plus revalidation is already in the framework |
| Edge bundles | Done | `gosx build --prod` emits edge bundle artifacts |
| Vec3/Mat4 opcodes | Not started | This is part of the real engine substrate, not the current compatibility path |
| EngineProgram / scene reconciler / `webgl.js` | Not started | This is the actual architectural gap between today's demo and the target runtime |
| Gallery runtime completion | ~15% | Four scenes exist visually, but they do not yet run through VM-driven scene commands |

### Actual Blocker

The gallery works today because `<Scene3D>` is a framework-owned Canvas 2D compatibility surface inside `bootstrap.js`. What does not exist yet is the target architecture from this spec:

`opcode VM → scene reconciler → scene commands → webgl.js`

That missing engine substrate is the real blocker. The gallery shell, routes, and `.gsx` authoring path are already in place and waiting for it.

### Global Non-Goals

- This is not a general-purpose user-scriptable JS engine API.
- This is not a production CMS with persistence, uploads, or search.
- This is not a physics engine or a full game engine.
- Touch-first/mobile interaction is deferred unless a scene proves it necessary.

### .gsx-First Authoring Rule

- User-facing pages, layouts, scene shells, and gallery controls are authored in `.gsx`.
- Go code is for loaders, actions, data builders, runtime bindings, and engine-program construction.
- Remaining `gosx.El(...)` helpers in the CMS demo are migration debt, not the target authoring path.

## Architecture

### Core Thesis

The opcode VM stays in control. DOM islands emit DOM patches consumed by `patch.js`. Engine programs emit scene commands consumed by `webgl.js`. Both JS files are translation layers between VM output and browser APIs. Scene logic does not live in JavaScript.

```
Input (gamepad/keyboard/pointer)
  → JS host captures, writes to $ signals via __gosx_set_input_batch
    → opcode VM reads input signals, evaluates Vec3/Mat4 math
      → VM writes results to scene signals
        → engine reconciler diffs scene graph (WASM-side)
          → scene commands applied by webgl.js (thin JS renderer)
```

### Rendering Surfaces

Islands and engines are the same model pointed at different surfaces:

**Islands (DOM):**
```
IslandProgram { Nodes: [div, span, ...], Exprs: [OpSignalGet, OpAdd, ...] }
  → VM evaluates → DOM reconciler diffs → DOM patches
  → patch.js applies (594 lines, browser DOM API calls)
```

**Engines (WebGL):**
```
EngineProgram { Nodes: [mesh, light, camera, ...], Exprs: [OpSignalGet, OpVec3, ...] }
  → VM evaluates → scene reconciler diffs → scene commands
  → webgl.js applies (~600-800 lines, WebGL API calls)
```

In both cases, JS translates typed commands into browser calls. It does not decide what exists, how state flows, or what updates next.

| DOM Patch (patch.js)    | Scene Command (webgl.js) |
|-------------------------|--------------------------|
| SetAttr                 | SetTransform             |
| CreateElement           | CreateObject             |
| RemoveElement           | RemoveObject             |
| SetText                 | SetUniform               |
| SetValue                | SetMaterial              |
| Reorder                 | SetDrawOrder             |

### Hard Constraints

- Scene structure is a fixed node tree compiled ahead of time.
- Dynamic behavior comes from signals plus validated opcode math, not user-authored JS.
- `webgl.js` is framework infrastructure, not a user escape hatch.
- Input providers are thin signal writers only.
- The only JS in a GoSX app remains framework-owned infrastructure: `bootstrap.js`, `patch.js`, `webgl.js`, and thin input capture.

### Input Devices as Signal Sources

Input devices produce shared signals. No new opcodes needed for input — just new signal sources managed by the JS host.

```
Gamepad:  $input.gamepad0.leftX (float), $input.gamepad0.buttonA (bool), ...
Keyboard: $input.key.w (bool), $input.key.space (bool), ...
Pointer:  $input.pointer.x (float), $input.pointer.deltaX (float), ...
```

Providers activate only when an engine declares the corresponding capability.

#### Input Signal Performance

Input providers write signals at animation-frame rate (60fps). To avoid per-signal WASM boundary crossings, input updates are batched:

- Each provider collects changed values per frame into a JS object
- A single `__gosx_set_input_batch(jsonMap)` WASM call delivers all input state at once
- The WASM side unpacks the batch and updates shared signals, triggering one notification pass
- This means 1 WASM boundary crossing per frame regardless of how many input signals changed

### Signal Flow

```
┌───────────────────┐                     ┌──────────────────┐
│  Island VM (WASM) │                     │  Engine VM (WASM) │
│                   │                     │                   │
│ OpVec3Add         │  shared $ signals   │ OpVec3Scale       │
│ OpMat4Rotate      │ ◄────────────────── │ OpMat4LookAt      │
│ OpSignalSet       │ ──────────────────► │ OpSignalGet       │
│                   │                     │                   │
│ DOM reconciler    │                     │ Scene reconciler  │
│      ↓            │                     │      ↓            │
│ DOM patches       │                     │ Scene commands    │
└───────┬───────────┘                     └───────┬───────────┘
        │                                         │
        ▼                                         ▼
┌───────────────┐                         ┌───────────────────┐
│  patch.js     │                         │  webgl.js         │
│  (DOM API)    │                         │  (WebGL API)      │
│  ~594 lines   │                         │  ~600-800 lines   │
└───────────────┘                         └───────────────────┘
        ▲                                         ▲
        │                                         │
┌───────────────────────────────────────────────────────────────┐
│  Input Providers (thin JS)                                    │
│  gamepad poll / keydown / pointermove → __gosx_set_input_batch│
└───────────────────────────────────────────────────────────────┘
```

## Workstream 1: CMS Completion

### Delivery Gates

1. CMS routes are file-routed `.gsx` pages using the modern GoSX app surface.
2. One engine-backed scene renders through the new runtime with no app-authored JS logic.
3. Input signals drive visible scene updates through the VM in one frame.
4. All six showcase scenes run inside the CMS and demonstrate distinct capabilities.

Bring the CMS up to the full GoSX feature set.

### Current CMS Status

| Feature | Status | Implementation |
|---------|--------|---------------|
| File-routed `.gsx` shells | Done | Original routes plus `/demo/3d` and `/demo/3d/[scene]` are file-routed |
| Gallery loaders | Done | `page.server.go` loader path is live for the gallery overview |
| Client-side navigation | Done | `app.EnableNavigation()` + `server.Link()` on internal links |
| Navigation + prefetch | Done | Intent-prefetching is already available through GoSX navigation |
| Sidecar CSS | Remaining | `layout.css`, `page.css` sidecars replacing inline styles |
| Metadata sidecars | Remaining | `page.meta.json` for home, 404, and gallery pages |
| Auth | Remaining | Session-backed auth on `/admin/*` using `auth.New()` + in-memory user store |
| Caching | Partial | `CacheRevalidate` on blog, `CacheDynamic` on admin, ETags on API |
| Deferred regions | Remaining | `ctx.Defer()` on blog listing with skeleton fallback |
| Create post | Remaining | Post creation form on `/admin/[tenant]` |
| API endpoints | Remaining | `/api/tenants`, `/api/posts/[tenant]` with cache headers |

### Remaining Tranche 1 Work

The remaining CMS pass is deliberately small and product-shaped:

- add `/login` plus session-backed auth middleware for `/admin/*`
- add cached JSON endpoints for tenants and posts
- add `ctx.Defer()` to the blog listing with a meaningful fallback
- add a create-post form on `/admin/[tenant]`
- move shared styles into sidecar CSS and add static metadata sidecars

### Auth Detail

The CMS uses GoSX's existing `auth` and `session` packages:
- `session.New(secret, session.Options{...})` creates a signed-cookie session manager
- `auth.New(sessions, auth.Options{...})` wraps session-backed authentication
- `authManager.Middleware()` applied to `/admin/*` routes
- `authManager.Require()` redirects unauthenticated users to `/login`
- Login page posts credentials to a session-creating action, redirects to `/admin`
- Hardcoded demo credentials (this is a capability demo, not a production auth system)

### Not in scope

Persistence layer, image upload, comments, search, pagination.

### Route changes

```
/login                  → NEW: auth login page
/api/tenants            → NEW: JSON, cached
/api/posts/[tenant]     → NEW: JSON, cached
/admin/*                → UPDATED: auth-guarded, CacheDynamic
/admin/[tenant]         → UPDATED: add create-post form
/blog/[tenant]          → UPDATED: CacheRevalidate, deferred listing
/blog/[tenant]/[post]   → UPDATED: CacheRevalidate, ETags
```

`/demo/3d` and `/demo/3d/[scene]` are already live and are no longer part of the remaining CMS tranche.

## Workstream 2: Framework Extensions

Changes to `/home/draco/work/gosx` core.

### Current Baseline vs Target

Today, `.gsx` pages can mount `<Scene3D>` because `route/fileprogram.go` lowers it into a native engine config and `client/js/bootstrap.js` renders a framework-owned Canvas 2D scene directly. That is useful, but it is not the target architecture.

The target architecture adds a real engine substrate:

- `EngineProgram` and typed scene nodes
- VM-evaluated Vec3/Mat4 expressions
- scene reconciliation on the WASM side
- renderer-agnostic scene commands
- a WebGL command applier in `webgl.js`

The current Canvas-backed `Scene3D` path stays in place during the transition for three reasons:

- it preserves the already-working CMS gallery
- it gives the engine substrate a low-risk compatibility harness before `webgl.js` is ready
- it remains the graceful fallback when WebGL2 is unavailable

### Minimum Viable Engine Slice

Ship the substrate in this order:

1. `mesh`, `camera`, `light`, and `group` node kinds before `particles`.
2. `box`, `sphere`, `plane`, and `torus` geometry before the full primitive set.
3. `flat`, `lambert`, and `emissive` materials before `phong`.
4. Keyboard and pointer input before gamepad polish.
5. One scene running through the full VM → reconciler → renderer loop before adding the full gallery.

`particles`, the full primitive zoo, and Scene 6 synchronization are showcase-maturity features, not the first acceptance bar.

### Acceptance Bar

Workstream 2 is complete enough to unblock the gallery when all of the following are true:

- A file-routed CMS page can mount one EngineProgram without app-authored JS.
- The engine runtime can hydrate, tick, reconcile, and dispose cleanly.
- At least one scene can respond to pointer or keyboard input through `$input.*` signals.
- Island controls can drive engine state through shared signals in the same frame.
- The renderer-facing command set is stable enough that scenes do not need bespoke transport glue.

### New Opcodes (15 total)

Added to `island/program/program.go`, evaluated in `client/vm/vm.go`.

Vec3 math (7):
- `OpVec3` — construct from (x, y, z)
- `OpVec3Add` — component-wise addition
- `OpVec3Sub` — component-wise subtraction
- `OpVec3Scale` — scalar multiplication
- `OpVec3Normalize` — unit vector
- `OpVec3Cross` — cross product
- `OpVec3Dot` — dot product (returns TypeFloat, not TypeVec3)

Mat4 math (7):
- `OpMat4Identity` — 4x4 identity matrix
- `OpMat4Rotate` — rotation around axis by angle
- `OpMat4Translate` — translation matrix from Vec3
- `OpMat4Scale` — scale matrix from Vec3
- `OpMat4Multiply` — matrix multiplication
- `OpMat4LookAt` — view matrix from eye, target, up
- `OpMat4Perspective` — projection matrix from fov, aspect, near, far

Interpolation (1):
- `OpLerp` — linear interpolation on float, Vec3, and color values

### Value Type Extensions

The VM's `Value` type (`client/vm/value.go`) gains:

- New `ExprType` constants: `TypeVec3`, `TypeMat4`
- New field on `Value`: `Floats []float64`
- Vec3 stored as `Floats` with len 3, Mat4 as `Floats` with len 16
- Existing `Num float64` field continues to serve scalars
- Type tag distinguishes Vec3/Mat4 from regular float arrays

This avoids abusing `Items []Value` for numeric vectors and preserves type safety in opcode evaluation.

### EngineProgram Type

New file: `island/program/engine.go`

EngineProgram follows the same structure as IslandProgram:

```go
type EngineProgram struct {
    Nodes []EngineNode    // Scene graph: mesh, light, camera, group, particles
    Exprs []Expr          // Same typed opcodes as islands (plus Vec3/Mat4)
}

type EngineNode struct {
    Kind       string            // "mesh", "light", "camera", "group", "particles"
    Geometry   string            // "box", "sphere", "cylinder", "torus", "plane", "cone"
    Material   string            // "flat", "lambert", "phong", "emissive"
    Props      map[string]ExprID // Expression bindings: "position" → expr, "color" → expr, etc.
    Children   []int             // Child node indices (for groups)
    Static     bool              // If true, reconciler skips diffing (optimization)
}
```

Key differences from IslandProgram:
- Node kinds are scene objects, not DOM elements
- Props map to 3D properties (position, rotation, scale, color, intensity) not HTML attributes
- Expression bindings can produce Vec3/Mat4 values (via the new opcodes)
- No text nodes — scene objects have typed properties, not string content

EnginePrograms are serialized the same way as IslandPrograms: JSON for dev (inspectable), binary for prod (compact). They are compiled from Go code using the same `ir.Lower` pipeline extended with scene node kinds.

### Scene Reconciler

New file: `client/vm/scene_reconcile.go`

The scene reconciler follows the same pattern as the DOM reconciler (`client/vm/reconcile.go`):

- Takes previous scene state + current scene state (from VM evaluation)
- Diffs properties on each node
- Produces typed scene commands:

```go
type SceneCommandKind int

const (
    SceneCmdCreateObject  SceneCommandKind = iota // Create mesh/light/camera with geometry + material
    SceneCmdRemoveObject                          // Remove object by ID
    SceneCmdSetTransform                          // Update position/rotation/scale (Mat4 or Vec3 components)
    SceneCmdSetMaterial                           // Update color/opacity/wireframe/material type
    SceneCmdSetLight                              // Update light position/color/intensity
    SceneCmdSetCamera                             // Update camera position/target/fov
    SceneCmdSetParticles                          // Update particle pool config (count, gravity, color)
)

type SceneCommand struct {
    Kind     SceneCommandKind
    ObjectID int               // Index into EngineProgram.Nodes
    Data     json.RawMessage   // Serialized command payload
}
```

Scene commands are serialized to JSON and passed to `webgl.js` via the same callback pattern as DOM patches (`__gosx_apply_scene_commands`).

### Dynamic Object Management

For scenes with dynamic objects (particles), the EngineProgram's `particles` node kind declares a pool:

- The `particles` node has expression bindings for `count`, `gravity`, `spawnRate`, `color`
- The VM evaluates these per frame, producing pool configuration values
- The scene reconciler emits `SceneCmdSetParticles` when config changes
- `webgl.js` manages the GPU-side particle buffer (allocate/recycle instances)
- Individual particle positions are computed in `webgl.js` per frame using the config parameters — this is rendering-level work (interpolating positions between spawn and death), not scene logic

The line between "scene logic" (opcode VM) and "rendering" (webgl.js) is: the VM decides WHAT exists and its properties. The renderer decides HOW to draw it each frame (vertex interpolation, particle aging, buffer management).

### Input Signal Providers

New section in `bootstrap.js`. Each provider activates when an engine in the manifest declares the corresponding capability.

Gamepad (`CapGamepad`):
- Poll `navigator.getGamepads()` per animation frame
- Batch changed values: `{leftX, leftY, rightX, rightY, buttonA, buttonB, ...}`
- Single `__gosx_set_input_batch` call per frame

Keyboard (`CapKeyboard`):
- keydown/keyup listeners on document
- Maintain key state map in JS
- Batch changed keys per frame

Pointer (`CapPointer`):
- pointermove/pointerdown/pointerup handlers
- Pointer lock support for FPS-style controls
- Write position, delta, and button state

### New Engine Capabilities

Added to `engine/engine.go`:

```
CapGamepad  — Gamepad API polling
CapKeyboard — document key state tracking
CapPointer  — pointer position, delta, lock
```

`CapTouch` is deferred — not needed for the gallery scenes.

`ValidateCapabilities()` in `engine/engine.go` must be updated to recognize these three new constants alongside the existing seven (`canvas`, `webgl`, `animation`, `storage`, `fetch`, `audio`, `worker`).

### Files Touched (framework)

```
island/program/program.go          ← 15 new opcode constants, TypeVec3/TypeMat4
island/program/engine.go           ← NEW: EngineProgram, EngineNode, SceneCommand types
client/vm/value.go                 ← Floats field, type tags
client/vm/vm.go                    ← Vec3/Mat4 evaluation logic
client/vm/scene_reconcile.go       ← NEW: scene reconciler (diffs scene state → commands)
client/bridge/bridge.go            ← engine hydration, __gosx_apply_scene_commands callback
client/wasm/main.go                ← register engine WASM exports
client/js/bootstrap.js             ← engine program loading, scene command dispatch,
                                     input providers, __gosx_set_input_batch
client/js/webgl.js                 ← NEW: WebGL command applier (~600-800 lines)
engine/engine.go                   ← 3 new capability constants + ValidateCapabilities update
```

## Workstream 3: WebGL Renderer

`client/js/webgl.js` — a command applier analogous to `patch.js`.

This file contains NO scene logic. It translates scene commands from the WASM VM into WebGL API calls. It is framework code, auditable once, shipped with GoSX.

### Renderer MVP

The first acceptable renderer is smaller than the final showcase target:

- WebGL2 context bootstrapping
- `CreateObject`, `RemoveObject`, `SetTransform`, `SetMaterial`, `SetLight`, and `SetCamera`
- Box, sphere, plane, and torus geometry
- Flat, lambert, and emissive materials
- Basic wireframe support
- Resize handling
- Clear fallback UI when WebGL2 is unavailable

Particles, phong, and the full primitive catalog can land after the first scene is proven end to end.

### Responsibilities

1. **Initialization**: create WebGL2 context, compile built-in GLSL shaders, set up depth buffer and backface culling
2. **Geometry generation**: generate vertex/index/normal buffers for built-in primitives (box, sphere, cylinder, torus, plane, cone). Each is a pure function: `(params) → {positions, normals, indices}`
3. **Command application**: receive `SceneCommand[]` from WASM, execute corresponding WebGL calls
4. **Frame rendering**: bind shader, set uniforms (model/view/projection matrices, light positions, material colors), draw each object
5. **Resource management**: track GPU resources (buffers, VAOs) per object ID, clean up on `RemoveObject`

### What It Does NOT Do

- Decide what objects exist (VM decides)
- Compute transforms (VM computes via Mat4 opcodes)
- Handle input (input providers + VM handle)
- Manage scene state (VM + reconciler manage)
- Run game logic of any kind

### Built-in GLSL Shaders

Four shader pairs compiled at initialization:

**Flat vertex + fragment**: pass-through transform, solid color output
**Lambert vertex + fragment**: transform + normal → world space, diffuse N·L per fragment
**Phong vertex + fragment**: transform + normal → world space, diffuse + Blinn-Phong specular per fragment
**Emissive vertex + fragment**: transform only, color output ignoring lights

Shader uniforms:
- `uModel` (mat4) — object transform
- `uView` (mat4) — camera view
- `uProjection` (mat4) — perspective projection
- `uColor` (vec3) — material base color
- `uOpacity` (float) — material opacity
- `uLights[4]` — array of {position, color, intensity, type}
- `uAmbient` (vec3) — ambient light color

### Geometry Generators

Pure functions returning typed arrays:

- `generateBox(width, height, depth)` → 24 vertices (smooth normals per face), 36 indices
- `generateSphere(radius, segments, rings)` → parametric UV sphere
- `generateCylinder(radiusTop, radiusBottom, height, segments)` → capped cylinder
- `generateTorus(radius, tube, radialSegments, tubularSegments)` → torus
- `generatePlane(width, height)` → 4 vertices, 6 indices
- `generateCone(radius, height, segments)` → cylinder with radiusTop=0

### Wireframe Mode

When a material has `wireframe: true`, the renderer draws with `gl.LINES` using an index buffer that traces edges instead of filling triangles. Wireframe indices are generated alongside triangle indices for each primitive.

### WebGL Unavailability

If `canvas.getContext("webgl2")` returns null, the renderer inserts a static HTML message: "WebGL 2.0 not available in this browser." No Canvas 2D fallback — the existing `GoSXScene3D` engine covers that case.

### Viewport Handling

ResizeObserver on the mount element. On resize: update `canvas.width`/`canvas.height`, call `gl.viewport`, and update the aspect ratio used in perspective projection.

## Workstream 4: Gallery Scenes

Route: `/demo/3d` in the CMS. Six scenes, each an EngineProgram with island controls.

### Rollout Order

Build the gallery in two passes:

1. **Foundation scenes**: Geometry Zoo, Lighting Lab, Orbit Controls
2. **Showcase scenes**: Particle Fountain, Gamepad Playground, Synchronized Duo

The foundation pass proves the core runtime, materials, and signal loop. The showcase pass proves breadth.

### Scene Contract

Every scene should follow the same authoring model:

- File-routed `.gsx` page for the shell and controls
- `page.server.go` loader for scene config and metadata
- EngineProgram data built in Go, not hand-authored JS
- Shared-signal wiring between island controls and engine state
- No bespoke browser glue per scene beyond declared capabilities

### Scene 1: Geometry Zoo

All six primitive types arranged on a 3x2 grid, slowly rotating. Each object is an EngineNode with a spin signal binding.

Island controls: wireframe toggle, material type switcher (flat/lambert/phong), base color picker. Controls write to `$scene.wireframe` (bool), `$scene.material` (string), `$scene.color` (Vec3) signals. The EngineProgram's expression bindings read these signals.

**Proves**: primitive library, material system, island → engine reactivity via signals through the opcode VM.

### Scene 2: Lighting Lab

Single large sphere centered in the scene. Three point lights (red, green, blue) with orbiting positions computed by the VM via `OpVec3` + `OpMul` + trig-approximation expressions. One directional light from above. Ambient light.

Island controls: per-light intensity sliders, toggle each light on/off, ambient color selector. Controls write to `$light0.intensity`, `$light1.on`, `$ambient.color` signals.

**Proves**: lighting model, real-time parameter updates, VM-computed animation (orbiting lights via opcode math).

### Scene 3: Particle Fountain

An EngineNode of kind `particles` with expression bindings for pool config. Emissive particles spawn from a point, rise with gravity, fade out, and recycle. Default pool: 200 particles, configurable up to 500.

Island controls write to `$particles.count`, `$particles.gravity`, `$particles.spawnRate`, `$particles.color` signals. The VM evaluates these into the EngineNode's properties. The scene reconciler emits `SceneCmdSetParticles`. `webgl.js` manages the GPU-side particle buffer and per-frame position interpolation.

**Proves**: dynamic rendering driven by opcode-evaluated config, emissive materials, the boundary between VM logic and renderer animation.

### Scene 4: Orbit Controls

A cluster of differently-shaped objects at varying depths. Pointer input provider writes `$input.pointer.deltaX/Y` and `$input.pointer.buttons` signals. The EngineProgram has computed expressions that translate pointer deltas into camera orbit (spherical coordinates via Vec3 math).

The island panel reads `$camera.position` and displays live coordinates as text.

**Proves**: pointer input → signal → opcode VM → scene update pipeline, bidirectional data flow (engine state visible in island DOM).

### Scene 5: Gamepad Playground

A single torus in a lit scene. The EngineProgram has expression bindings that read `$input.gamepad0.*` signals:
- Left stick → `OpVec3(leftX, 0, leftY)` → `OpVec3Scale(move, speed)` → `OpVec3Add(position, move)` → object position
- Right stick → camera orbit (same spherical math as Scene 4)
- Triggers → `OpLerp(colorA, colorB, triggerValue)` → object color

Falls back to `$input.key.*` signals (WASD + arrows) when no gamepad connected. Island panel shows raw `$input.gamepad0.*` signal values updating in real-time.

**Proves**: gamepad input signals, keyboard fallback, full input → opcode → signal → scene pipeline.

### Scene 6: Synchronized Duo

Two EngineProgram mounts side by side reading the same `$` signals. Both programs bind camera to `$camera.position`, material to `$scene.material`. Orbit controls in one scene write to `$camera.position`. The other scene reads the same signal and renders from the same viewpoint.

Island panel material switcher writes to `$scene.material` — both scenes update.

**Proves**: cross-engine shared state via `$` signals, the same mechanism that enables multiplayer. Two independent VM instances converging on the same visual output through shared signals.

### Signal Synchronization

Signal change notifications flush synchronously within `signal.Batch()`. Since input batches and the render loop are both driven by `requestAnimationFrame`:

1. rAF fires
2. Input provider collects state, calls `__gosx_set_input_batch` (enters WASM, updates signals in a batch)
3. Batch flushes: all subscribers notified synchronously (island VMs reconcile DOM, engine VMs reconcile scene)
4. Scene reconciler produces commands, passed to `webgl.js`
5. `webgl.js` applies commands and draws frame
6. rAF completes

Same-frame propagation: input → signal → opcode → scene command → render within one animation frame.

## Route Structure

### Gallery routing

The 3D gallery uses file-based routing (`.gsx` pages) with sibling `page.server.go` modules:

- `app/demo/3d/page.gsx` + `app/demo/3d/page.server.go` — gallery overview, loader returns scene listing
- `app/demo/3d/[scene]/page.gsx` + `app/demo/3d/[scene]/page.server.go` — individual scene, loader returns scene config (EngineProgram data, signal names, control definitions)

Each `page.server.go` calls `route.MustRegisterFileModuleHere(...)` from its `init()`. Scene prop builders live in `scenes.go` at the project root and are imported by the page server modules. The `modules/modules.go` auto-import file is regenerated by `gosx dev` / `gosx build` to include the new packages.

## File Structure

### CMS changes

```
gosx-CMS-demo/
├── app/
│   ├── layout.css                      ← NEW: sidecar (replaces inline)
│   ├── page.meta.json                  ← NEW: static metadata
│   ├── not-found.meta.json             ← NEW: static metadata
│   ├── login/
│   │   └── page.gsx                    ← NEW
│   ├── demo/
│   │   └── 3d/
│   │       ├── page.gsx               ← NEW: gallery overview
│   │       ├── page.server.go          ← NEW: scene listing loader
│   │       └── [scene]/
│   │           ├── page.gsx           ← NEW: individual scene
│   │           └── page.server.go     ← NEW: scene config loader
│   └── ... (existing)
├── route_login.go                      ← NEW: auth actions
├── scenes.go                           ← NEW: EngineProgram builders for each scene
├── public/
│   ├── gallery.css                     ← NEW: 3d gallery styling
│   └── ... (existing)
└── modules/
    └── modules.go                      ← AUTO-GENERATED: updated by gosx dev
```

### Framework changes

```
gosx/
├── island/program/program.go          ← 15 new opcode constants, TypeVec3/TypeMat4
├── island/program/engine.go           ← NEW: EngineProgram, EngineNode, SceneCommand types
├── client/vm/value.go                 ← Floats field, type tags
├── client/vm/vm.go                    ← Vec3/Mat4 evaluation logic
├── client/vm/scene_reconcile.go       ← NEW: scene graph reconciler
├── client/bridge/bridge.go            ← engine hydration, scene command callback
├── client/wasm/main.go                ← register engine WASM exports (__gosx_hydrate_engine,
│                                        __gosx_engine_tick, __gosx_dispose_engine)
├── client/js/bootstrap.js             ← engine program loading, scene command dispatch,
│                                        input providers, __gosx_set_input_batch
├── client/js/webgl.js                 ← NEW: WebGL command applier (~600-800 lines)
└── engine/engine.go                   ← 3 new capability constants + ValidateCapabilities update
```

## Data Flow

Full pipeline for a gallery scene interaction (Scene 5: Gamepad Playground):

```
1. Server renders page with engine manifest (EngineProgram JSON, capabilities: [webgl, gamepad, keyboard])
2. bootstrap.js fetches EngineProgram, calls __gosx_hydrate_engine(programJSON, propsJSON)
3. WASM-side bridge creates engine VM instance, evaluates initial scene state
4. Scene reconciler produces initial scene commands (CreateObject for torus, lights, camera)
5. webgl.js receives commands: compiles shaders, generates geometry buffers, sets initial transforms
6. Input providers activate (gamepad poller, keyboard listener)
7. Per frame (rAF):
   a. Input provider batches changed values, calls __gosx_set_input_batch
   b. WASM updates $input.* signals in a batch
   c. __gosx_engine_tick(engineID) called
   d. Engine VM re-evaluates expression bindings (Vec3 math on input signals)
   e. Scene reconciler diffs: torus position changed, camera rotated
   f. Scene commands emitted: [SetTransform(torus, newMat4), SetCamera(newPos, newTarget)]
   g. webgl.js applies: gl.uniformMatrix4fv for torus, update view matrix for camera
   h. webgl.js draws frame: clear, bind shader, draw each object
   i. Island VM also re-evaluates: DOM reconciler patches coordinate display text
8. User sees: torus moved, camera rotated, coordinates updated — all in one frame
```

## First Executable Slice

Starting from today's baseline, the next shippable slice should be:

1. Finish the remaining CMS Tranche 1 items: auth, cached APIs, deferred blog listing, create-post flow, sidecar CSS, and metadata sidecars.
2. Add `EngineProgram`, `EngineNode`, and `SceneCommand` plus bridge lifecycle entry points without changing the public `.gsx` authoring surface.
3. Route one scene only, Geometry Zoo, through the new engine-program path while keeping the current Canvas-backed `<Scene3D>` implementation alive as the fallback and compatibility harness.
4. Prove shared-signal updates from island controls into the engine runtime before adding WebGL.
5. Add `webgl.js` only after the data model, lifecycle, and command transport are stable.

That slice is successful when a file-routed CMS page can mount one engine-program scene from `.gsx`, update it through shared signals, survive navigation/disposal/remount cleanly, and still degrade to the existing framework-owned Canvas path when WebGL2 is unavailable.

## Execution Plan

### Tranche 0: Baseline Already Complete

- `/demo/3d` and `/demo/3d/[scene]` already exist as file-routed `.gsx` pages.
- Gallery overview loading already goes through `page.server.go`.
- Four scenes already render through the built-in Canvas-backed `<Scene3D>` runtime.
- The authoring path is already `.gsx`-first for the gallery shell.

This tranche is done. Do not rebuild it.

### Tranche 1A: Finish the Remaining CMS Pass

- Add `/login` and session-backed auth middleware for `/admin/*`.
- Add `/api/tenants` and `/api/posts/[tenant]` with explicit cache directives.
- Add `ctx.Defer()` to the blog listing with a real skeleton or placeholder.
- Add a create-post form on `/admin/[tenant]`.
- Move shared presentation into sidecar CSS and add static metadata sidecars.

Done when:

- The CMS demo can credibly stand on its own as a GoSX app.
- The remaining app-surface gaps from Workstream 1 are closed without backing away from `.gsx` page shells.

### Tranche 2A: Engine Data Model and Lifecycle

- Add `EngineProgram`, `EngineNode`, and `SceneCommand`.
- Add engine hydration, tick, and dispose entry points in the bridge/WASM runtime.
- Add new engine capabilities for pointer, keyboard, and gamepad input.
- Add `__gosx_set_input_batch` and the runtime-side signal update path.

Done when:

- One engine instance can hydrate, tick, and dispose through generic runtime APIs.
- Runtime transport is scene-agnostic and does not require bespoke JS per gallery page.

### Tranche 2B: VM Math and Scene Reconciliation

- Add Vec3/Mat4 value support plus the first math opcodes.
- Add scene reconciliation for create, remove, transform, material, light, and camera updates.
- Feed the resulting scene commands into a compatibility backend first so the transport can be verified before WebGL exists.
- Port Geometry Zoo detail rendering onto this path.

Done when:

- Geometry Zoo is driven by `EngineProgram` data rather than ad hoc scene props.
- Island controls and pointer input can mutate scene state through signals in one frame.
- The current Canvas-backed path still works as fallback and regression harness.

### Tranche 3: Minimal WebGL Runtime

- Add `client/js/webgl.js`.
- Support one mount, one camera, basic lights, box/sphere/plane/torus geometry, and flat/lambert/emissive materials.
- Switch the command target from the compatibility backend to WebGL2 while preserving the same `EngineProgram` contract.

Done when:

- The same Geometry Zoo scene can render through WebGL2 with no page-level JS changes.
- Command application is renderer-driven, not scene-driven.
- Fallback UI appears cleanly when WebGL2 is unavailable.

### Tranche 4: Foundation Scenes

- Finish Geometry Zoo on the real runtime.
- Add Lighting Lab and Orbit Controls.
- Use shared signals for live material/light/camera updates.

Done when:

- The three foundation scenes are stable and pleasant.
- Pointer and keyboard input are proven on the real engine path.

### Tranche 5: Showcase Breadth

- Add Particle Fountain, Gamepad Playground, and Synchronized Duo.
- Close the remaining material/input/scene gaps only as those scenes prove them necessary.
- Keep the scene authoring contract stable instead of adding scene-specific runtime seams.

Done when:

- All six scenes reuse the same engine substrate.
- The gallery feels like one coherent framework capability, not six demos glued together.

## Test Plan

### Core

- `go test ./...`
- Unit coverage for new VM value types and opcodes
- Unit coverage for scene reconciliation diffs
- Unit coverage for engine bridge lifecycle: hydrate, tick, dispose

### Browser Runtime

- `node --test client/js/runtime.test.js`
- New tests for engine manifest loading and scene-command dispatch
- New tests for WebGL fallback behavior
- New tests for input batching into `__gosx_set_input_batch`

### App Flow

- `node --test e2e/gosx_docs_e2e.test.mjs`
- Add CMS-demo E2E coverage for gallery overview and one scene page
- Verify navigation away/back disposes and remounts the engine cleanly

## Resolved Decisions

- The first scene is Geometry Zoo, not a gamepad or particle scene.
- The first input path is pointer or island-control driven, not gamepad.
- The first renderer target is WebGL2 only, with a static fallback message.
- `webgl.js` is framework-owned and scene-agnostic.
- Gallery pages are file-routed `.gsx` pages with `page.server.go` loaders.

## Success Criteria

### Required

- CMS uses the real GoSX app surface: navigation, auth, cache, defer, sidecar CSS, metadata sidecars, and API endpoints.
- At least the three foundation scenes run through the VM → reconciler → `webgl.js` path with no app-authored JS logic.
- Island controls update scene state in real time through shared signals.
- Pointer and keyboard input drive visible 3D interaction through `$input.*` signals.
- The opcode vocabulary stays closed: `webgl.js` remains a command applier, not a scene-authoring escape hatch.
- WebGL renderer degrades cleanly when WebGL2 is unavailable.
- `go test ./...` passes for the framework changes.

### Showcase-Complete

- All six gallery scenes render smoothly on modern hardware.
- Gamepad input and keyboard fallback both work in Scene 5.
- Cross-engine signal sharing works in Scene 6.
- `webgl.js` stays within the intended size budget and remains auditable.
- Per-scene particle/object budget stays within a range that preserves smooth interaction without bespoke scene tuning.
