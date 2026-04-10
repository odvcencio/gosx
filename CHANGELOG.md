# Changelog

## v0.14.0

### `gosx/field` Module

New `field` package providing a 3D vector field type with trilinear sampling, standard operators (Advect, Curl, Divergence, Gradient, Blur, Resample), per-component scalar quantization at 4–8 bits with optional delta encoding, and `gosx/hub` integration for live field broadcast across WebSocket connections. Designed as the foundation for volumetric rendering, particle advection, fluid simulation, and any consumer that needs structured 3D data — independent of any renderer.

A 64³ scalar field at 6 bits packs to ~200 KB on the wire. A 64³ vec3 field packs to ~600 KB. Delta encoding shrinks subsequent updates further when the field is temporally coherent. The codec reuses the same per-component min/max scalar quantization pattern proven in `scene/compress.go`.

Hub streaming maintains a per-(hub, topic) subscriber registry inside the field package itself. `PublishField` does dual dispatch: the decoded `*Field` flows to local in-process subscribers via Go channels, while the JSON-encoded `Quantized` payload goes to connected WebSocket clients via `hub.Broadcast`. Successive publishes automatically delta-encode against the previous field for that topic.

### Scene3D PostFX Go API

`scene.PostFX` adds a Go-side post-processing effect chain on top of the existing JS-side post-processor. The previously-dormant FBO + ping-pong + ACES tone mapping + bloom infrastructure in the WebGL2 and WebGPU renderers is now driven by typed Go effects: `Tonemap`, `Bloom`, `Vignette`, `ColorGrade`. A typical chain looks like:

```go
scene.PostFX{Effects: []scene.PostEffect{
    scene.Bloom{Threshold: 0.65, Strength: 0.6, Radius: 8},
    scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.0},
}}
```

The new `PostEffect` interface is sealed via an unexported method so external packages cannot define their own effects without coordination with the renderer. Effect chains run in declaration order, ping-ponging through HDR offscreen targets before compositing to the canvas.

### `Environment.ToneMapping` Backwards-Compat Migration

Existing scenes that used `Environment.ToneMapping = "aces"` (and `Environment.Exposure`) keep working unchanged. The compile path now synthesizes a `Tonemap` PostEffect from those fields when `PostFX.Effects` does not already include one — the explicit Go API takes precedence when present, and the legacy fields keep working when it's absent. The synthesized effect routes through the new PostFX pipeline so the inline tonemap branches in the PBR fragment shader stay disabled.

## v0.13.0

### Authoritative Simulation Module

New `sim` package providing server-authoritative game simulation over gosx hubs. Games implement a four-method `Simulation` interface (Tick, Snapshot, Restore, State) and the `sim.Runner` handles everything else: fixed-rate tick loop, input collection from hub clients, state broadcast, 128-frame snapshot ring buffer for rollback, replay recording with full input logs, and spectator sync on join. One line to get tournament-grade netcode: `sim.New(hub, game, sim.Options{TickRate: 60})`.

### Server Gzip Middleware

`server.EnableGzip()` adds response compression with proper Hijacker/Flusher support so WebSocket upgrades and streaming still work. Pooled gzip writers, skips pre-compressed responses and WebSocket upgrades automatically.

### WASM Debug Stripping

Production builds now strip DWARF debug sections from WASM binaries via `wasm-opt --strip-debug --strip-producers`. Debug symbols were 45% of the binary (1.3 MB) and served no purpose in the browser. Result: 1032 KB → 526 KB gzipped, a 49% reduction. Dev builds retain debug symbols.

### WASM Data Section Externalization (experimental)

`wasmExternalizeData` post-processor splits the WASM data section (strings, type tables, reflection metadata) into a separate file for parallel loading. The browser can start compiling WASM code while the data section downloads independently.

## v0.12.0

### Scene3D Per-Component Compression

Position arrays are now deinterleaved before quantization so each axis gets its own min/max codebook. Previously, interleaved XYZ data shared one range, destroying precision on axes with smaller extents (e.g., a flat galaxy's Y axis spanning 10 units quantized against X/Z spanning 400 units). The client-side decompressor reinterleaves after dequantization. Controlled by the existing `Compression.BitWidth` setting — no API changes needed. The `positionStride` field is plumbed through the IR and legacy serialization path automatically.

### Particle Emitter Rotation

`ParticleEmitter` now accepts a `Rotation Euler` field. The spiral, disc, and sphere emitters apply the rotation to emitted particle positions so compute particle systems can match a tilted parent geometry. Both the WebGPU (WGSL) and CPU fallback paths apply the ZYX Euler rotation matrix. This fixes the horizontal-band artifact when a spiral emitter is used with a tilted galaxy scene.

### Editor Module

New `editor` package providing a rich text editing foundation: toolbar component with default markdown actions, undo/redo history with coalescing, `LocalDocument` text model, tree-sitter based syntax highlighting for markdown, input system with IME support, and signal-driven reactivity. Build-time factory registry for engine lockdown after init.

### Scene3D Improvements

Lighting, input, mount, glTF, and WebGPU rendering improvements across the Scene3D pipeline. Transition system additions for smooth state morphing.

## v0.11.0

### Selective Runtime Bootstrap

GoSX now ships a real selective bootstrap path for non-scene runtime pages instead of the previous full-vs-lite split.

**Selective runtime bundle**: Added `bootstrap-runtime.js` plus feature chunks for islands, engines, and hubs. Pages now load only the runtime features they declare:
- **Lite pages** keep `bootstrap-lite.js`
- **Islands / hubs / non-Scene3D engines** use `bootstrap-runtime.js` plus the matching feature chunk(s)
- **Scene3D pages** keep the full `bootstrap.js`

**Shared wasm runtime gating**: The page runtime now emits `runtime.wasm` and `wasm_exec.js` only when they are actually needed. Hub-only pages, managed video pages, and native JS engine pages no longer pay for the shared Go wasm bridge.

**Runtime asset pipeline**: Build manifests, compat asset serving, static export, and `gosx build` / `gosx export` / `gosx dev` now understand `bootstrap-runtime.js` and the `bootstrap-feature-*` assets, so selective bootstrap works in source builds, hashed build manifests, and static-export compatibility copies.

**Document/runtime contract cleanup**: Default `server.App` rendering now injects managed runtime head assets correctly before the document is finalized, and the document contract now distinguishes bootstrap from shared-runtime usage instead of treating every `full` bootstrap page as a shared wasm page.

### Dependency Wiring

**Published TurboQuant dependency**: Replaced the local `replace github.com/odvcencio/turboquant => ../turboquant` workflow with the published `github.com/odvcencio/turboquant v0.1.0`, so `cmd/gosx` build/export integration tests can resolve wasm runtime dependencies from clean starter apps.

## v0.10.0

### TurboQuant Vector Intelligence

**`quant` package**: Pure-Go implementation of the TurboQuant algorithm (arXiv:2504.19874). Compresses high-dimensional vectors to 1-8 bits per coordinate, within ~2.7x of information-theoretic optimum. MSE-optimal quantizer via random rotation + Lloyd-Max codebook. Inner-product-optimal quantizer via MSE + 1-bit QJL residual with unbiased estimation. Deterministic via `NewWithSeed`. Zero-alloc inner product in rotated domain. `PrepareQuery` amortizes O(d²) projection for search workloads. 53μs quantize, 52μs inner product at dim=384.

**`vecdb` package**: In-memory quantized vector search index. Zero indexing time (data-oblivious quantization). Add/Remove/Search with `PrepareQuery` + `InnerProductPrepared` scan and min-heap top-k selection. Thread-safe via `sync.RWMutex`. O(1) swap-and-pop removal. 45ms search over 1K vectors at dim=64.

**`embed` package**: Provider interface for external embedding APIs (OpenAI, Cohere, etc.) plus BPE tokenizer with vocabulary/merge loading, special token support, and auto-detection of Ġ space-prefix convention.

**`semantic` package**: Three AI-native primitives:
- **SemanticCache** — cache responses by vector similarity instead of exact URL. Similar queries share cached responses above a configurable threshold.
- **SemanticRouter** — match requests to handlers by embedding similarity. Route by meaning, not URL pattern. Primary consumer: AI agents calling APIs without needing schemas.
- **ContentIndex** — index page content for related-page discovery and semantic search.

### CRDT Vector Compression

**`VectorValue` type**: Store high-dimensional vectors in CRDT documents using TurboQuant compression. All replicas use a deterministic seed for byte-identical output, preventing spurious merge conflicts. 16x bandwidth reduction at 2-bit (96 bytes for 384-dim vs 1,536 raw). Participates in existing LWW merge semantics.

### Scene3D Compression Pipeline

**Scalar quantization for vertex transport**: Per-chunk min/max quantization compresses positions, sizes, transforms, and animation keyframes. Metadata is 8 bytes per chunk (min + max) instead of a rotation matrix.

**Client-side JS dequantizer**: Complete end-to-end pipeline — Go quantizes during IR lowering, JS dequantizes at scene init before the render loop. Base64 decode → unpack b-bit indices (1/2/4/8-bit fast paths) → scalar dequantize → vertex buffers.

**Progressive mesh loading**: `Compression{BitWidth: 4, Progressive: true}` ships 2-bit preview alongside 4-bit full resolution. Client renders preview immediately, upgrades to full after first paint via `requestIdleCallback`. No loading spinner.

**Quantization-based LOD**: `Compression{LOD: true}` stores both preview and full resolution. Per-frame, each object's camera distance determines which resolution renders. Objects beyond `LODThreshold` (default 20 units) use the preview. Crossing the threshold triggers buffer rebuild.

**Animation keyframe compression**: `AnimationClip` and `AnimationChannel` scene graph nodes with compressed `Times` and `Values` arrays. 32-joint skeleton at 60 keyframes, 4-bit: 92% compression (15KB vs 182KB). Progressive preview keyframes for instant playback.

## v0.9.0

### Bug Fixes

**Route handler error propagation**: Route handlers that encounter render errors no longer panic. Added `ctx.SetHandlerError(err)` to `RouteContext` and a check in `buildHandler` that dispatches through the error handler chain. The `defer recover()` safety net remains for unexpected panics, but normal render errors now flow through the error page system without crashing the process.

**CRDT API safety**: `Doc.Save()`, `Doc.Fork()`, and `Doc.Commit()` now return errors instead of panicking on encode failures. `Save()` returns `([]byte, error)`, `Fork()` returns `(*Doc, error)`, `Commit()` returns `(ChangeHash, error)`. Internal `commitPending` and `flushPendingForSnapshot` propagate errors. All callers in `crdt/`, `hub/`, and `client/bridge/` updated.

### Documentation Site Redesign

Complete ground-up rebuild of the gosx-docs example app. The old Paper & Ink site is gone. The new site is a maximalist, dual-mode experience that proves GoSX by being built with GoSX at full power.

**Design system**: Dual-mode token architecture (dark immersive + light editorial) built on the m31labs.dev visual language. Space Grotesk + Inter + JetBrains Mono font stack. Fluid clamp() typography and spacing. Glass morphism, chrome text gradient, gold accent family. WCAG 2.2 AA minimum with verified contrast ratios. Forced-colors override for Windows High Contrast Mode.

**Showroom homepage**: Full-viewport 3D hero scene (PBR meshes, 2000 GPU compute particles, orbit controls, ACES tone mapping). Three-statement pitch section. Eight capability showcase sections alternating dark/light. Proof point stat cards with TextBlock server-measured values. Scroll reveal with IntersectionObserver stagger.

**17 reference pages** where the docs ARE live demos:
- Light mode (11): Getting Started, Routing, Forms, Auth, Runtime, Images, Text Layout, Motion, Streaming, Compiler, Deployment
- Dark mode (5): Islands, Signals, Engines, 3D Engine (with inline PBR scene), Hubs & CRDT

**3 standalone demos**: Galaxy (2800 GPU compute particles), Geometry Zoo (PBR primitives with orbit controls), CMS Editor (block editor with publish action).

**Navigation**: Floating pill nav (translucent over dark, solid over light). Page-scoped TOC rail on reference pages. Full-screen overlay with focus trap on mobile.

**Accessibility**: Skip links (content + navigation + TOC). `prefers-reduced-motion` global kill switch with static 3D fallbacks. Focus-visible gold rings on all interactive elements. ARIA landmarks, live regions, descriptive labels. 44px minimum touch targets. 400% zoom reflow.

**Micro-interactions**: Link underline draw (left-to-right, gold). Card hover lift with chrome gradient border. Button press scale. Code block hover highlight. Page transitions (fade out 150ms, fade in 300ms). Glass panel tooltips with spring easing.

## v0.8.0

### Tier 2: Visual Quality

**HDR exposure control**: `Environment.Exposure` scales scene brightness before tone mapping. `Environment.ToneMapping` selects ACES filmic (default), Reinhard, or linear output. Postprocessing automatically disables built-in tone mapping.

**Spot lights**: `SpotLight` with cone angle, penumbra (soft edge), distance attenuation, and shadow casting. PBR shader computes inner/outer cone cutoff with smooth falloff.

**Hemisphere lights**: `HemisphereLight` with sky/ground color blend based on surface normal direction. Provides ambient lighting that varies with orientation.

**MSAA**: Canvas-level multisampling verified working for non-postprocessing scenes via the existing `antialias: true` context flag.

**5 light types** in the PBR shader: ambient, directional, point, spot, hemisphere.

## v0.7.0

### Tier 1: WebGPU, Instancing, GPU Compute Particles

**WebGPU backend** (2,447 lines): Full PBR renderer ported to WGSL — Cook-Torrance BRDF, shadow maps with comparison sampling, postprocessing chain, exponential fog. Points rendered as instanced billboard quads (WebGPU has no gl_PointSize). Async device initialization, pipeline caching by material+geometry signature, storage buffer light arrays, bind group architecture (per-frame / per-material / per-object).

**Instanced rendering**: `InstancedMesh` node draws N copies of one geometry in a single draw call. WebGL2 via `drawArraysInstanced` + `vertexAttribDivisor`, WebGPU via native instancing. Per-instance mat4 transforms from Go-declared positions/rotations/scales. Geometry generation for box, plane, sphere with normals/UVs/tangents. Geometry cache by kind+dimensions. Performance target: 10K instances at 60fps.

**GPU compute particles** (664 lines): `ComputeParticles` node declares a particle system that simulates entirely on GPU. WGSL compute shader with 4 emitter types (point, sphere, disc, spiral), 5 force types (gravity, wind, turbulence, orbit, drag), deterministic hash RNG, lifetime/respawn, color/size/opacity interpolation over lifetime. CPU fallback for WebGL2 (capped at 10K particles). Go API: one struct configures 100K particles at zero CPU cost.

**Auto backend selection**: WebGPU → WebGL2 PBR → WebGL2 legacy → Canvas 2D. Transparent to scene authors.

**Go API**: `InstancedMesh`, `ComputeParticles`, `ParticleEmitter`, `ParticleForce`, `ParticleMaterial` types with full IR lowering and render bundle transport.

## v0.6.0

### 3D Engine Phase 2-3

Production-grade 3D rendering platform built into GoSX's native Scene3D.

**Renderer**: PBR WebGL2 backend with Cook-Torrance BRDF, per-pixel lighting (8 lights), shadow maps (PCF), postprocessing (bloom, tone mapping, vignette, color grading), exponential fog. Draw-plan abstraction for future WebGPU backend. Split 5K LOC monolith into 10 focused modules.

**Points primitive**: GL_POINTS particle system with per-vertex size/color, size attenuation, additive blending, depth write control, pinwheel spin animation, scroll-driven camera.

**Asset pipeline**: Built-in glTF/GLB loader (meshes, PBR materials, textures, animations, skins). Animation mixer with keyframe interpolation, quaternion slerp, crossfading. Skeletal animation with vertex skinning (64 joints).

**Interaction**: Raycast scene picking (Moller-Trumbore ray-triangle with AABB broad phase) replacing bounds-based detection.

**Go API**: `StandardMaterial`, `CylinderGeometry`, `TorusGeometry`, `Points`, shadow fields, animation fields, `ScrollCameraStart`/`ScrollCameraEnd`, fog, `EffectComposer`.

**Performance**: Zero per-frame allocations in animation hot path, cached vertex data, pre-allocated scratch buffers, binary keyframe search, persistent GPU buffers.

**Galaxy demo**: 2,800-particle galaxy with spiral arms, scroll camera, fog — replaces three.js on m31labs.dev with 62% less JS over the wire (80KB gzipped savings).

## v0.3.2

- **fix(isr):** stale ISR pages now snapshot the existing artifact before background regeneration starts, so the first stale response cannot race ahead and serve the freshly regenerated HTML.
- **fix(test):** hardened hub websocket bootstrap tests to wait for the `__welcome` event without assuming it always arrives before every join broadcast once multiple clients connect.

## v0.3.1

- **fix(eval):** `nil == ""` now returns `true` in template expressions, so unbound variables like `flash.contact` compare equal to empty string instead of rendering conditional branches incorrectly.
- **release:** rolls the `v0.2.4` template-eval fix forward onto the `v0.3.x` line without backing out the native text layout, managed video, or managed motion work shipped in `v0.3.0`.

## v0.3.0

- **feat(text-layout):** `TextBlock` now supports explicit native server-measured layout with `mode="native"` / `TextBlockModeNative`, keeping pages off the bootstrap layer when final wrapped HTML should come from the server.
- **feat(video):** `server.Video`, `ctx.Video`, and `<Video />` now emit a real server `<video>` baseline with authored `<source>` and `<track>` children, and the managed runtime upgrades that baseline in place for HLS, sync, and shared media signals.
- **feat(motion):** added `server.Motion`, `ctx.Motion`, and the `.gsx` `<Motion />` builtin for bootstrap-managed DOM motion presets with `load`/`view` triggers, timing controls, and reduced-motion awareness.
- **docs:** refreshed the README and gosx-docs coverage for engines, video, motion, and native-vs-bootstrap text layout.
- **fix(ci):** restored `make fmt-check` on `main` by formatting `server/runtime_assets.go`.

## v0.2.4

- **fix(eval):** `nil == ""` now returns `true` in template expressions — unbound variables like `flash.contact` compare equal to empty string instead of being a distinct nil that renders `<If>` bodies incorrectly

## v0.2.3

- **fix(css):** Global selectors (`body`, `html`, `:root`, `*`, `*::before`, `*::after`, `::selection`, `::placeholder`) in sidecar CSS are no longer scoped — they pass through to the document directly
- **fix(server):** `/gosx/bootstrap.js` serves a minimal stub script when `gosx build` has not been run, instead of returning 404. The stub initializes the gosx runtime namespace and auto-mounts registered engines from the page manifest.

## v0.2.2

- **feat(route):** Added underscore param convention as Go-compatible alternative to bracket syntax
  - `_slug` directory → `{slug}` route parameter (single underscore = dynamic param)
  - `__path` directory → `{path...}` route parameter (double underscore = catch-all)
  - Valid Go package names, works with `go mod tidy`, `go build ./...`, and module imports
  - Legacy `[slug]` bracket syntax still supported for backward compatibility

## v0.2.1

- **fix(ir):** HTML entities (`&rarr;`, `&mdash;`, etc.) in `.gsx` text are now decoded to UTF-8 characters instead of being double-escaped
- **fix(ir):** `<script>` and `<style>` element content is now treated as raw text, preventing HTML escaping of `&&`, `<`, etc. inside inline scripts
- **fix(build):** `gosx build` no longer generates invalid Go import paths for `[slug]` dynamic route directories
- **fix(route):** Pattern conflicts between `__actions` and wildcard siblings now produce a clear diagnostic error instead of a raw panic

## v0.2.0

### Authentication

- added session-backed auth manager with `auth.New()`, `Require` middleware, and `Current()` user context
- added GitHub OAuth provider (`auth.GitHubProvider`) with automatic email fetching
- added Google OAuth provider (`auth.GoogleProvider`)
- added custom OAuth providers with `OAuthUserResolverFunc`
- added magic link passwordless auth with configurable resolver
- added WebAuthn/passkey auth with register and login flows
- added role-based access control via `RequireRole` middleware

### Engine System

- added Surface engine primitive for dedicated canvas/WebGL/WebGPU workloads
- added Scene3D engine with server-side render bundle generation
- added pixel-surface engine runtime with managed GPU framebuffers
- added 3D perspective rendering with world-space coordinates, frustum culling, and depth sorting
- added material system with presets (flat, glow, ghost, glass, matte) and blend modes
- added fluent `Builder` API for program construction
- added box, sphere, pyramid, and plane 3D primitives
- added scene label support with collision avoidance, occlusion, and priority layout
- added shared engine runtime with program-driven scenes
- added input providers with batched signal delivery (pointer, keyboard)

### Islands and Reactivity

- added shared signals across islands (prefixed with `$`)
- added `Each`/`For` rendering with resolved tree reconciliation
- added keyed reconciliation with auto-key stabilization
- added event field access in island handlers
- added string concatenation in VM expressions
- added raw props object access in island expressions

### File-Based Routing

- added directory-scoped modules (`DirModule`) with inherited middleware
- added file-route metadata sidecars (`page.server.go`)
- added sidecar CSS ownership and layer tracking
- added `route.config.json` for directory-scoped cache and header config
- added spread attribute support in file renderer
- added auto-resolved Go components from `Funcs`, `Values`, and dotted paths
- added not-found error handling for data loaders

### Server and Runtime

- added client-side page navigation with head/body swaps and scroll restoration
- added managed form submission via fetch with navigation runtime
- added ISR serving for prerendered bundles
- added static export (`gosx export`) with prerender pipeline
- added production build pipeline with hashed runtime assets and edge bundle artifacts
- added CSS layer architecture with ownership metadata and global styles
- added JSON request observer for structured logging
- added path traversal protection and runtime manifest caching
- added CSRF token injection in form fetch requests

### Text Layout

- added browser-backed text measurement and layout engine
- added grapheme-aware text layout with word segmentation
- added `TextBlock` primitive for framework-level text layout
- added server-side sizing hints, tab stops, soft hyphens, and CJK punctuation
- added `maxLines` and overflow support for text truncation
- added i18n and vertical writing mode support
- added text layout caching with font loading invalidation

### CRDT

- added document core, sync messages, and hub transport for real-time collaboration

### CLI

- added `gosx fmt` with `--check` mode
- added `gosx check` integration test for `.gsx` files
- added `gosx lsp` language server
- added `gosx init --template docs` scaffold

### Docs and Examples

- added gosx-docs reference app with CMS demo, Scene3D demo, auth flows, streaming, and ISR
- rewrote README with clearer structure and user focus
- renamed JSX terminology to native GSX throughout

### Internal

- modularized `bootstrap.js` with split source files and build step
- extracted helper functions across client, route, and VM packages for readability
- optimized WebGL state with cached blend/depth and static geometry buffers
- added capability-aware rendering and resilient WebGL fallback
- added `prefers-reduced-motion` and scroll handling
- added environment, document, and presentation observation APIs

## v0.1.0

- formalized the initial GoSX release line with a repo-level `gosx.Version`
- completed the zero-manual-wiring island path for compiled `.gsx` islands
- added build-manifest loading for hashed runtime and island assets
- hardened actions, hubs, server timeouts, HTML escaping, and build failure handling
- added repeatable repo tooling with `make test`, `make test-race`, `make test-wasm`, `make build-runtime`, and `make ci`
- added CI coverage for format checks, race tests, js/wasm runtime tests, CLI build, and WASM runtime build
- added js/wasm runtime tests that compile `.gsx` islands, hydrate them through `__gosx_hydrate`, dispatch via `__gosx_action`, and assert the client patch stream
