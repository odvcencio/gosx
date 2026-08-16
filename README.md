# GoSX

A Go-native web platform. Write `.gsx` components with strict typed declarations or ordinary Go-function syntax, compile through a real compiler pipeline, render on the server by default, and hydrate interactive islands with WebAssembly. No app-side JavaScript toolchain. No CGo. A deliberately small dependency budget.

Current release: **v0.42.2**. Pre-1.0; breaking changes are documented in [CHANGELOG.md](./CHANGELOG.md).

## Agent Skills

Agents helping someone use GoSX should start with the canonical M31 Labs skill: [using-gosx](https://github.com/odvcencio/m31labs-skills/blob/main/skills/using-gosx/SKILL.md).

For native mobile, editor, admin, and CMS periphery, read: [using-gosx-ecosystem](https://github.com/odvcencio/m31labs-skills/blob/main/skills/using-gosx-ecosystem/SKILL.md).

## What if you never had to leave Go?

GoSX starts from a simple premise: the browser is a render target, not a runtime. Server components are Go functions that return HTML. Interactive components compile to bytecode and run in a shared WASM VM. Everything between those two points — the parser, the compiler, the reconciler, the signal system, the 3D scene graph, the vector store, the collaborative document model — is pure Go.

```gsx
package app

import "m31labs.dev/gosx/signal"

type GreetingProps struct {
    Name string
}

type CounterProps struct {
    Initial int
}

// Strict typed server component: renders to HTML, zero JavaScript.
component Greeting(props: GreetingProps) {
    return <div class="greeting">
        <h1>Hello, {props.Name}!</h1>
        <p>Welcome to GoSX.</p>
    </div>
}

component WelcomePage() {
    return <main><Greeting name="GoSX" /></main>
}

// Legacy Go-function style: still supported, here as an island.
//gosx:island
func Counter(props CounterProps) Node {
    count := signal.New(props.Initial)
    increment := func() { count.Update(func(n int) int { return n + 1 }) }
    decrement := func() { count.Update(func(n int) int { return n - 1 }) }

    return <div class="counter">
        <button onClick={decrement}>-</button>
        <span>{count}</span>
        <button onClick={increment}>+</button>
    </div>
}
```

Both component styles can live in the same file. In v0.39, component calls
stay within their declaration style; this keeps legacy dynamic composition
from bypassing strict prop checks. The strict
`component Name(props: GoType) { ... }` form uses an ordinary Go type as its
prop contract, so the package check catches unknown fields and incompatible
values. `component` supplies the `Node` result type; it does not make returns
implicit. Strict server bodies currently contain one top-level GSX return and
use a deliberately small renderer-safe expression surface. Each expression is
either a quoted string, `true` or `false`, an ungrouped non-negative base-10
integer in the `int64` range, a finite ungrouped decimal float, or one direct
field on `props` whose same-file type is an exact built-in scalar. The original
`func Name(props GoType) Node { ... }` form remains supported for existing and
dynamic components.

Strict same-file calls accept an exported Go field name or its TSX-like
lower-camel alias (`Label`/`label`, `HTMLFor`/`htmlFor`, `URL`/`url`);
ambiguous aliases must use exact Go spelling. Every field the callee renders
must be supplied explicitly, including `0`, `false`, and `""`, so generated Go
and server rendering observe the same zero values.

In v0.39, islands and engines continue to use the legacy Go-function spelling.
The `//gosx:island` directive marks a legacy component for client-side
hydration. The compiler extracts signals, computed values, and handlers from
the Go source, compiles expressions to VM instructions, and serializes an
island program. Server components emit static HTML with no client-side cost.

## Philosophy

GoSX is opinionated about a small number of things and flexible about everything else.

- **The browser is a render target.** Server components are the baseline. Client-side JavaScript is something you opt into, feature by feature, not a default ambient runtime.
- **One language, explicit browser compiler.** You write Go. `go build` produces the server and CLI, `gosx dev` keeps the standard Go WASM loop for local iteration, and production `gosx build --prod` requires TinyGo for browser runtime output. There is no app-side Node, npm, webpack, or bundler config.
- **No JavaScript toolchain is not zero browser cost.** GoSX still ships a measured browser bootstrap, feature chunks, and WASM runtime only when a route needs them. The performance contract is that the compiler and build pipeline justify every shipped runtime slice.
- **No CGo, anywhere.** Every package compiles to WASM and cross-compiles cleanly. The 3D engine runs in pure Go. The vector store runs in pure Go. The CRDT sync protocol runs in pure Go. This is not a portability footnote — it is the design constraint that lets Scene3D, `field`, `vecdb`, and `crdt` ship as ordinary Go libraries that also happen to run in a browser tab.
- **Primitives, not frameworks-within-frameworks.** A form submission is not a canvas game is not a collaborative document. GoSX gives you five distinct execution primitives and enforces the distinction; none of them try to be the others.
- **You pay for what you use.** Static pages are static. Islands ship only when a page has an island. Production manifests choose among capability-linked `core`, `engine`, `collab`, and `full` WASM profiles; the legacy `islands` name remains a read-compatible alias. An app with no islands has no client VM; an app with no engines has no engine bundle.
- **No hidden magic in the hot path.** The compiler pipeline is inspectable (`gosx compile`, `gosx check`). The IR is a flat-array data structure, and the island VM, patch applier, and feature chunks are source you can read. Build manifests report the exact bytes each route ships.
- **Small dependency budget.** That's not marketing — it's a design constraint. Every new transitive dep is a bug surface, a license to audit, and a supply-chain risk. We take that budget seriously.

## Five Primitives

GoSX provides five execution primitives. A form submission is not a canvas game is not a collaborative document — the framework enforces that distinction.

| Primitive | What it does | Client cost |
|-----------|-------------|-------------|
| **Server** | Renders pages and API responses | None |
| **Action** | Handles mutations (forms, RPCs) with structured validation | None |
| **Island** | Reactive DOM subtrees with signals and event delegation | Shared WASM VM + tiny program payload |
| **Engine** | Heavy client compute — canvas, WebGL, WebGPU, background workers | Dedicated WASM or JS bundle |
| **Hub** | WebSocket presence, fanout, shared state, CRDT sync | WebSocket connection |

Use what you need. A static marketing page uses only Server. A dashboard adds Islands. A game adds an Engine. A collaborative editor adds a Hub. You never pay for what you don't use.

Scene3D is the built-in 3D engine primitive: prop-based scenes and composable `<Scene3D><Mesh /><Points /></Scene3D>` authoring both lower toward the same versioned SceneIR contract. The `game` package layers deterministic fixed-step simulation, input actions, ECS-style state, assets, physics, and Scene3D mounting on top of Engine + Hub when an app needs an interactive simulation/game runtime.

## Quick Start

```bash
gosx init my-app
cd my-app
go run .
```

Or scaffold the docs template with nested layouts, auth, and forms:

```bash
gosx init my-docs --template docs
cd my-docs
go run .
```

Minimal server without the CLI:

```go
package main

import (
    "m31labs.dev/gosx"
    "m31labs.dev/gosx/server"
)

func main() {
    app := server.New()
    app.Page("/", func(ctx *server.Context) gosx.Node {
        return gosx.El("h1", gosx.Text("Hello from GoSX"))
    })
    app.ListenAndServe(":8080")
}
```

## File-Based Routing

Routes are discovered from the `app/` directory:

```
app/
  layout.gsx              # Root layout (wraps all pages)
  page.gsx                # /
  page.server.go          # Server module: data loading, actions, metadata
  not-found.gsx           # Custom 404
  error.gsx               # Custom 500
  about/
    page.gsx              # /about
  blog/
    layout.gsx            # Nested layout for /blog/*
    page.gsx              # /blog
    [slug]/
      page.gsx            # /blog/{slug}
      page.server.go      # Per-post data loader
  (marketing)/
    pricing/page.gsx      # /pricing (group ignored in URL)
  docs/
    [...slug]/page.gsx    # /docs/{slug...} (catch-all)
    route.config.json      # Inherited cache/header config
```

Server modules wire Go logic to `.gsx` pages without touching the template:

```go
if err := route.RegisterFileModuleHere(route.FileModuleOptions{
    Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
        post, err := db.GetPost(ctx.Param("slug"))
        if err != nil {
            return nil, route.NotFound("post not found")
        }
        return post, nil
    },
    Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
        post := data.(*Post)
        return server.Metadata{Title: post.Title, Description: post.Summary}, nil
    },
    Actions: route.FileActions{
        "comment": handleComment,
    },
}); err != nil {
    log.Fatal(err)
}
```

## Compilation Pipeline

GSX syntax is parsed by [gotreesitter](https://github.com/odvcencio/gotreesitter), a pure-Go reimplementation of the tree-sitter runtime with grammar composition. GoSX extends Go's grammar with native markup support at the CST level — no templates, no code generation, no separate build step.

```
.gsx source
  -> parse (gotreesitter, extended Go grammar)
  -> lower to IR (flat-array, index-based nodes)
  -> validate (including island subset enforcement)
  -> server components: render to HTML directly
  -> island components:
       -> extract signals, computeds, handlers from Go source
       -> compile expressions to VM instructions
       -> serialize as IslandProgram (JSON dev / binary prod)
       -> browser: shared WASM VM + TypeScript-owned patch and host runtime
```

Island expressions are constrained to what the client VM can evaluate: literals, property and signal access, arithmetic, comparisons, boolean logic, string operations, conditionals, handler dispatch, and list iteration. Goroutines, channels, and arbitrary Go are compile-time errors in islands. The full supported subset — array-method shorthand (`.filter`, `.map`, `.find`, `.slice`, `.append`), string methods, and single-param closures — is specified in [`docs/expressions.md`](docs/expressions.md).

## Reactive State

Signals provide fine-grained reactivity in islands:

```go
count := signal.New(0)                           // mutable state
doubled := signal.Derive(func() int {            // computed
    return count.Get() * 2
})
increment := func() {                            // handler
    count.Update(func(n int) int { return n + 1 })
}
```

Signals prefixed with `$` are shared across all islands on the page. When one island mutates a `$`-signal, every island that references it re-renders automatically:

```
$count   // shared: any island can read/write
$theme   // shared: all islands react to changes
count    // local to the declaring island
```

## Server Features

**Sessions and Auth** — Cookie-backed sessions with HMAC-SHA256 signing, optional AES-GCM encryption, previous-secret rotation, CSRF protection with constant-time token comparison, and flash values. Auth supports sessions, magic links, OAuth 2.0 (GitHub, Google), and WebAuthn/Passkeys.

**Actions** — Named server-side mutation handlers with form/JSON parsing, field-level validation errors, and redirect-safe flash state.

**Caching** — Semantic cache helpers (`ctx.CacheStatic()`, `ctx.CacheRevalidate()`, `ctx.CacheData()`), automatic weak ETags from content hashing, path/tag-based revalidation, and ISR with background regeneration.

**Navigation** — Opt-in client-side page transitions via `app.EnableNavigation()` with managed head swaps and intent-prefetching. Pages render server-first, enhance progressively.

**Streaming** — Deferred page regions via `ctx.Defer()` and component-level `ctx.Suspense()` boundaries render fallback content immediately, then stream resolved content into place as each boundary completes.

**Image and Font Optimization** — Local image handler at `/_gosx/image` with resize, format conversion, immutable caching, auto responsive `server.Image` markup, and a `server.Font` helper for preload plus `@font-face`.

**Routing and Edge** — Server middleware can be annotated with `app.UseEdge`, and `app.UseI18n` adds locale-prefix routing that composes with both `server.App` and file routing.

**Content and Components** — `content` uses mdpp as the canonical Markdown++ content-source layer for `.md`, `.mdx`, and `.mdpp` collections, with typed frontmatter, diagnostics, renderer hooks, and slug indexes. `components` provides a registry for reusable server component libraries and file-route bindings, and `ui` seeds GoSX UI: layout, typography, form, card, tab, table, badge, and stylesheet primitives users can copy from or compose directly.

**Text Layout** — `TextBlock` supports a deterministic approximate server
layout and bootstrap-managed refinement with browser font metrics. Font,
width, line-height, locale, clamping, and ellipsis stay in one framework-level
contract without pretending the approximate first pass is exact.

**Managed Video** — `server.Video`, `ctx.Video`, and the `.gsx` `<Video />` builtin render a real server `<video>` baseline with `<source>` and `<track>` children, then the built-in video engine can layer in HLS fallback (with fatal-error recovery and muted-autoplay retry), selectable audio tracks, text and bitmap subtitle cues, sync, and shared `$video.*` signals when the page needs them.

**Managed Motion** — `server.Motion`, `ctx.Motion`, and the `.gsx` `<Motion />` builtin expose server-authored motion presets that run on the shared bootstrap layer. Preset, trigger, duration, delay, easing, reduced-motion policy, and distance all stay in one declarative contract.

**Managed Background Audio (YouTube)** — a declarative bridge served at `/gosx/youtube-audio.js` (`server.YouTubeAudioBridgePath`, tag helper `server.YouTubeAudioBridgeScriptTag()`). Any element with `data-gosx-youtube-audio="<youtube url>"` becomes a play/pause toggle for one shared hidden player, and the active element carries `data-gosx-youtube-audio-state="playing"` for CSS styling. The bridge lazy-loads the YouTube IFrame API on first activation, so pages without audio toggles load nothing.

**Runtime Surfaces** — `gosx.RuntimeSurface`, `gosx.Action`, and `gosx.Region` describe progressive-enhancement contracts in server HTML (`data-gosx-runtime-surface`), and the shared bootstrap owns discovery, navigation remounting, scoped DOM/query/fetch/listen access, stream-template consumption, and disposal — so rich pages register framework-managed behavior instead of shipping bespoke script tags.

## Engines

For work that doesn't fit the island model — canvas rendering, WebGL, background computation:

```go
ctx.Engine(engine.Config{
    Name:                 "visualizer",
    Kind:                 engine.KindSurface,
    Runtime:              engine.RuntimeGoWASM,
    Capabilities:         []engine.Capability{engine.CapCanvas, engine.CapAnimation},
    RequiredCapabilities: []engine.Capability{engine.CapCanvas, engine.CapWASM},
    WASMPath:             "/engines/visualizer.wasm",
}, fallbackNode)
```

The module is an ordinary `GOOS=js GOARCH=wasm` Go program. It may register one
or more reusable components during synchronous startup:

```go
//go:build js && wasm

package main

import enginewasm "m31labs.dev/gosx/engine/wasm"

func main() {
    if err := enginewasm.Register("visualizer", func(ctx enginewasm.Context) (enginewasm.Handle, error) {
        mount := ctx.Mount()
        // Initialize this instance using ctx.Props(), ctx.Emit(), and the DOM mount.
        return enginewasm.HandleFunc(func() {
            // Release this instance's listeners and resources.
            mount.Set("textContent", "")
        }), nil
    }); err != nil {
        panic(err)
    }
    select {}
}
```

GoSX fetches and boots each exact WASM URL once per browser document, then calls
the selected factory separately for every manifest entry. A high-entropy boot
capability binds registrations to the module instance that received it, so a
late module cannot register into another URL's startup window. Register every
component the module provides before `main` blocks; later page manifests may
reuse any factory published by that first boot. `main` must remain alive (the
example's `select {}` does that). If it exits, GoSX invalidates its factories
and disposes its mounted instances.

Loading uses `WebAssembly.instantiateStreaming` with a byte fallback from the
same response. Serve modules as `application/wasm` to keep the streaming fast
path. Failed or cancelled mounts restore their server-rendered fallback, and
normal engine disposal restores it as well. Page cancellation aborts an
in-flight fetch. A registration timeout permanently closes that boot
capability; once a standard-Go instance has started, browsers provide no
force-termination primitive, so a timed-out instance is quarantined from
registration and mounting but may retain memory until the browser document
unloads.

The loader does not use JavaScript `eval` and does not require app-authored
JavaScript. The isolated standard-Go `wasm_exec` shim is a DOM-loaded managed
script on both initial loads and client-side navigations; it is never fetched
and passed to indirect `eval`. Under a strict Content Security Policy,
WebAssembly compilation still requires `script-src 'wasm-unsafe-eval'` (or the
broader `'unsafe-eval'`) in browsers that enforce that directive.

Engines come in three kinds:

- `surface` — owns a DOM mount for canvas, WebGL, WebGPU, or managed pixel surfaces
- `worker` — background compute with no DOM mount
- `video` — framework-owned managed video playback

`Capabilities` declares what the engine can use. `RequiredCapabilities` is the hard browser gate: if a required API like `webgl`, `webgpu`, or `wasm` is missing, GoSX marks the mount unsupported and does not run the engine factory. WebGPU features can be required precisely with names like `webgpu:timestamp-query`, `webgpu:shader-f16`, `webgpu:indirect-first-instance`, `webgpu:texture-compression-bc`, or `webgpu:subgroups`; the runtime checks the negotiated WebGPU device feature set rather than only checking `navigator.gpu`. WebGPU limit gates use the same contract: `webgpu:limit:maxTextureDimension2D>=4096` checks the created device limits, while `webgpu:adapter-limit:maxTextureDimension2D>=8192` checks the probed adapter ceiling. Go code can build these gates with `engine.RequireWebGPU`, `engine.WebGPUFeature`, `engine.WebGPULimit`, and `engine.WebGPUAdapterLimit`; typed `scene.Props` exposes the same path through `scene.RequireWebGPU`.

The managed video path also has first-class helpers:

```go
ctx.Video(server.VideoProps{
    Poster:   "/media/poster.jpg",
    Controls: true,
    Sources: []server.VideoSource{
        {Src: "/media/promo.webm", Type: "video/webm"},
        {Src: "/media/promo.m3u8", Type: "application/vnd.apple.mpegurl"},
    },
    SubtitleTrack: "en",
    SubtitleTracks: []server.VideoTrack{
        {ID: "en", Language: "en", Title: "English", Src: "/subs/en.vtt"},
    },
}, gosx.El("p", gosx.Text("Download the trailer")))
```

That emits a usable server `<video>` baseline first. When the runtime mounts, it upgrades the existing element in place instead of throwing it away and recreating the player shell in JavaScript.

Supported capability declarations today are:

- `video`
- `canvas`
- `webgl`
- `webgpu`
- `pixel-surface`
- `animation`
- `storage`
- `fetch`
- `clipboard`
- `audio`
- `worker`
- `gamepad`
- `keyboard`
- `pointer`

Kinds choose the mount model. Capabilities declare which browser APIs the engine expects to use. Engines get their own mount point or worker context, communicate through typed message ports, and do not touch island DOM.

## Scene3D — 3D Engine

The `scene` package is a full 3D engine authored in Go. You describe the scene as a typed Go struct tree and the runtime lowers it to a compact IR. Where that IR renders depends on the target, and the split is deliberate:

- **On the web**, two authored TypeScript backends consume the IR: a WebGPU renderer and a WebGL2 renderer. Each ships as a separately fetched chunk, so a WebGPU-capable browser never downloads the WebGL renderer and the reverse also holds.
- **On the desktop**, and for headless rendering, a pure-Go WebGPU pipeline (`render/gpu` + `render/bundle`, with hand-written WGSL) consumes the same IR. It also backs `scene/preview`, which renders to PNG with no browser and no GPU.

Typed Scene3D surfaces declare WebGPU as a default capability, so a capable browser takes the WebGPU path first and falls back through WebGL2 to canvas when the device or the scene needs it. The server computes a per-backend fidelity verdict and ships it with the scene, so a backend that cannot render a scene faithfully is diverted rather than allowed to draw the wrong image. There is no separate engine binary. There is no three.js. There is no JavaScript scene graph.

```go
scene.Props{
    Responsive: scene.Bool(true),
    Controls:   "orbit",
    Camera: scene.PerspectiveCamera{
        Position: scene.Vec3(0, 1.5, 5),
        FOV:      60,
    },
    Environment: scene.Environment{
        AmbientColor:     "#ffffff",
        AmbientIntensity: 0.2,
    },
    Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
    Stats: scene.Bool(true),
    PostFX: scene.PostFX{
        MaxPixels: scene.PostFXMaxPixels1080p,
        Effects: []scene.PostEffect{
            scene.SSAO{Radius: 3, Intensity: 0.35},
            scene.DOF{FocusDistance: 5, Aperture: 0.04, MaxBlur: 8},
            scene.Bloom{Threshold: 0.8, Strength: 0.5, Radius: 6, Scale: 0.25},
            scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
        },
    },
    Graph: scene.NewGraph(
        scene.DirectionalLight{
            Color: "#fff1d6", Intensity: 1.0,
            Direction:  scene.Vec3(0.3, -1, -0.5),
            CastShadow: true,
            ShadowSize: 2048,
        },
        scene.Mesh{
            Geometry: scene.SphereGeometry{Segments: 32},
            Material: scene.StandardMaterial{
                Color: "#D4AF37", Roughness: 0.3, Metalness: 0.9,
                Clearcoat: 0.35, Anisotropy: 0.2,
            },
            Position:      scene.Vec3(0, 0.5, 0),
            CastShadow:    true,
            ReceiveShadow: true,
        },
        scene.LODGroup{ID: "ship-lod", Levels: []scene.LODLevel{
            {Distance: 0, Node: scene.Model{Src: "/assets/ship-high.glb"}},
            {Distance: 12, Node: scene.Model{Src: "/assets/ship-low.glb"}},
        }},
        scene.AxesHelper{ID: "axes", Size: 2},
        scene.GridHelper{ID: "grid", Size: 8, Divisions: 8},
        scene.Model{
            Src:                "/assets/ship.glb",
            Animation:          "idle",
            AnimationSpeed:     scene.Float(1.1),
            AnimationFadeInMS:  scene.Int(120),
            AnimationFadeOutMS: scene.Int(80),
        },
    ),
}
```

### Feature surface

- **Scene graph** — `Group`, `Mesh`, `LODGroup`, `Decal`, `InstancedMesh`, `Points`, `Label`, `Sprite`, `Model`, `ComputeParticles`, per-node transforms, nesting, world-transform lowering
- **Geometry** — `Box`, `Cube`, `Plane`, `Pyramid`, `Sphere`, `Lines`, `Cylinder`, `Torus`, helper-generated axes/grids/boxes/skeletons/gizmos, plus arbitrary geometry from loaded models
- **Materials** — `StandardMaterial` (PBR with roughness/metalness plus clearcoat, sheen, transmission, iridescence, and anisotropy), `FlatMaterial`, `GhostMaterial`, `GlassMaterial`, `GlowMaterial`, `MatteMaterial`, `LineBasicMaterial`, `LineDashedMaterial`, Selena-authored shader materials via `scene.CompileSelenaMaterial` and `scene.CompileSelenaBundle`, typed Selena host uniforms via `scene.SelenaUniforms`, `CustomMaterial` shader hooks, configurable blend modes and render passes
- **Lights** — `AmbientLight`, `DirectionalLight`, `PointLight`, `SpotLight`, `HemisphereLight`, `RectAreaLight`, `LightProbe`; shadow maps on directional lights, with per-light `ShadowSize` and a scene-wide `Shadows.MaxPixels` cap; spot lights render their cone and penumbra but do not yet cast shadows on either browser backend
- **Cameras** — perspective and orthographic cameras with orbit, first-person, fly, drag, pointer-lock, and transition hints plus picking and projection-aware sprites/labels
- **glTF / GLB** — `scene.Model{Src: "/assets/thing.glb"}` loads binary or JSON glTF 2.0 through the in-runtime pure-JS loader (`19-scene-gltf.js`), including animations
- **Build-time asset planning** — `gosx assets plan public` and `gosx build` inventory GLB/glTF, KTX2, HDR/EXR, raster textures, audio, USDZ, and WGSL assets into a low-memory `scene-assets.json` contract. It reads real structure: glTF mesh, material and animation counts, KTX2 headers, and WGSL entry points and bind groups. It then records the optimizations each asset *would* benefit from — KTX2 transcodes, GGX IBL prefiltering, split-sum LUT generation, meshopt or Draco compression, LOD stacks, vertex and animation stream compression — as `candidate` or `planned` actions. **Planning is all it does today. GoSX ships no encoder for any of them**, and the runtime glTF loader cannot consume Draco, meshopt or KTX2 either — it raises a named error instead of building a corrupt mesh
- **Server-driven scene diffs** — `scene.DiffCommands(prev.SceneIR(), next.SceneIR())` emits the browser command protocol for live object, label, sprite, and light replacement, so a Go server can mutate typed scene state and stream compact Scene3D commands over a hub instead of shipping a new page payload
- **Animation** — `AnimationClip` / `AnimationChannel` for node-level keyframe animation, `Spin` convenience for auto-rotation, glTF animation playback with loop, speed, blend weight, fade-in/fade-out, and replay-sequence controls; scene-level `autoRotate` is opt-in and static scenes do not keep a RAF loop alive by default
- **Instancing** — `InstancedMesh` renders repeated geometry through WebGPU instance-rate vertex buffers and WebGL2 `drawArraysInstanced`, with per-instance transforms, colors, material passes, receive shadows, and WebGPU instanced shadow casters
- **World primitives** — helper lines, thick world lines, wire overlays, grids, axes, clip-space guides, and textured plane surfaces use backend-specific pipelines; dashed world lines currently require the Canvas2D fallback rather than either browser 3D backend
- **Particles** — GPU-computed particle systems via `ComputeParticles` with emitter, forces, and material
- **Water** — `WaterSystem` GPU heightfield simulation with box/rounded pool shapes, caustics, reflection, refraction, projected object optics, drop/orbit/object-drag interaction, independent surface mesh topology, and adaptive quality profiles — WebGPU-first with a WebGL path rendered from the same authored Selena sources
- **Environment** — ambient, hemisphere, sky/ground, cubemap IBL, exposure, fog, tonemapping
- **WebGPU presentation** — tier-aware 4x MSAA render targets with resolve-to-canvas/post-FX targets, adapter feature/limit negotiation for timestamp queries, shader-f16, indirect first-instance, compressed textures, subgroups, manifest-driven `requiredFeatures` / `requiredLimits` negotiation, opt-in adapter `powerPreference`, opt-in canvas `alphaMode` / `colorSpace` / `toneMapping`, and diagnostics exposed for tooling through `data-gosx-scene3d-webgpu-*` mount attributes, plus shared SceneIR parity across WebGPU, WebGL2, and headless backends
- **First-content reveal** — an opt-in `data-gosx-scene3d-reveal-class` mount attribute names a CSS class. After the first frame with drawable content, the runtime stamps `data-gosx-scene3d-revealed="true"` on the mount and adds the class to the document element; dispose removes the class again. Pure CSS can fade a static boot placeholder — no app-authored watcher script
- **Lantern inspector** — a read-only Scene3D dev inspector served at `/gosx/devtools-lantern.js` (`server.DevtoolsLanternPath`, tag helper `server.DevtoolsLanternScriptTag()`). Apps include the tag only when devtools are enabled; Shift+D toggles a panel with truthful render FPS, backend and adaptive-quality state, live node-type counts, draw calls, and camera state — all read from the debug registry the production bundle already exposes
- **Post-processing** — `SSAO`, `DOF`, `Bloom`, `Tonemap` (ACES / Reinhard / Filmic), `Vignette`, `ColorGrade`, FXAA 3.11, RGB9E5/HDR intermediate selection, HDR10 presentation when supported, composable chain, with backend-specific passes skipped gracefully when unavailable
- **Editor/debug surfaces** — `AxesHelper`, `GridHelper`, `BoxHelper`, `BoundingBoxHelper`, `SkeletonHelper`, visual `TransformControls`, selected mesh outline styling, dashed/solid line materials, and opt-in `Stats` overlay
- **Native preview & certification** — `scene/preview` renders typed scenes to PNG with no browser or GPU (thumbnails, docs images, deterministic visual tests), and `scene/harness` certifies contract evidence — frame hashes, coverage, Selena artifact hashes, and BVH-accelerated ray/drag traces that are exact for every analytic primitive and every triangle mesh, use a pick radius for points, sprites and line strokes, fall back to a bounds box only for glTF models Go never loads, and name the method behind each hit — into schema-versioned JSON reports suitable for agent-operated authoring workflows
- **Shadow pixel cap** — v0.15.0's `Shadows.MaxPixels` caps each shadow map (default 1024²), preventing multi-megabyte-per-light allocations when individual lights request large shadow sizes
- **Compression & LOD** — per-chunk min/max scalar quantization with bit packing, including a per-lane mat4 deinterleave so translation lanes cannot crush scale lanes, progressive streaming that previews at 2 bits and upgrades to full precision, camera-distance-based LOD switching via `scene.Compression`, plus conventional discrete mesh/model swaps via `scene.LODGroup`
- **Transitions** — declarative enter/exit/state transitions on any scene node via `InState` / `OutState` / `Live`
- **Camera controls** — `orbit`, `first-person`, `fly`, pointer lock, drag-to-rotate, focus targets, pick signals (including the world-space click ray as `$surface.event.rayOriginX/Y/Z` + `rayDirX/Y/Z`), interactive TransformControls gizmo drags that emit `gosx:scene3d:input` `kind:"gizmo-commit"` events and an optional `GizmoOutputSignal`, drag signals, event signals exposed as `$`-signals consumable by surrounding islands
- **Capability tiers** — graceful degradation across WebGPU → WebGL → canvas fallbacks
- **Shared IR across backends** — the JS WebGPU and WebGL2 browser backends, the pure-Go desktop WebGPU pipeline, and the headless software rasterizer all consume the same SceneIR, with feature parity gated by what each target surface actually supports and reported through the capability verdict
- **CSS-stylable 3D** — composable materials, lights, environment, point layers, and post-FX can read `var(--scene-*)` custom properties through the planner, so class changes, media queries, and CSS transitions can drive scene state without authored JavaScript animation code

The scene graph is inspectable Go code. The IR is serializable. The renderer is reproducible. When something goes wrong, the implementation is Go and authored TypeScript rather than a black box.

## Hubs

WebSocket primitives for presence, fanout, and shared state:

```go
h := hub.New("workspace")
h.On("update", func(client *hub.Client, msg hub.Message) {
    h.Broadcast("state", currentState)
})
```

Hubs handle client lifecycle, message framing, broadcast patterns, per-connection state, and typed message dispatch. They integrate cleanly with the `crdt` package for conflict-free collaborative state and with the `sim` package for authoritative game loops.

## Collaboration: CRDT + Workspace

The `crdt` package implements a conflict-free replicated document model with a bloom-filter-based sync protocol, delta-encoded changes, and vector-clock causality tracking. Documents serialize to a compact binary snapshot — interned actor and object tables, zigzag varint deltas, run-length tombstones, and change hashes recomputed on load rather than stored — which costs about 5 bytes per character of text. The chunk framing follows Automerge's shape (magic bytes, ULEB128 length, truncated checksum) but the body is GoSX's own encoding, so treat it as protocol-compatible in structure, not byte-compatible with Automerge. Older JSON snapshots still load. Its convergence contract is covered by partitioned concurrent text edits, large partitioned histories, tombstone save/load merge tests, and sync recovery tests. It's independent of the transport — you can drive it over a `hub`, over Redis, or over raw bytes in a file.

```go
doc := crdt.NewDoc()
doc.Apply(change)               // apply a local or remote change
bytes, _ := doc.Save()          // serialize the full doc state
sync := crdtsync.NewState()    // start a sync session
msg := sync.Generate(doc)      // produce the next sync message to send
```

The `workspace` package layers a distributed semantic collaboration space on top of `crdt` + `hub` + `vecdb`: agents join a workspace, write findings with vector embeddings, query across peers, and persist state. It's used in GoSX's multi-agent tooling and is also the substrate for any app that wants "multiple clients editing shared state with presence and similarity search" without building it from scratch.

## Volumetric Data & Simulation

**`field`** — 3D vector fields. Trilinear sampling, axis-aligned bounding boxes, and a full set of operators (`Advect`, `Curl`, `Divergence`, `Gradient`, `Blur`, `Resample`). Per-component scalar quantization packs each value to a fixed bit width, so the ratio is `32/bitWidth` — a 256³ scalar field drops from 64 MiB to 8 MiB at 4 bits, and a 64³ scalar field to about 192 KiB at 6 bits. Delta encoding tightens the quantized range rather than shrinking the payload. Streaming streaming publish/subscribe over `hub` lets a server-authoritative simulation broadcast field updates to subscribed clients. The package is renderer-agnostic — it's the substrate for volumetric rendering, particle advection, fluid simulation, and anything else that needs structured 3D data at a distance.

**`sim`** — Server-authoritative game simulation. Games implement the `Simulation` interface; a `Runner` drives it at a fixed tick rate, collects per-client inputs from a hub, broadcasts state snapshots, and handles replay and spectator sync. The server is the source of truth; clients submit inputs and render the authoritative state they receive back.

**`game`** — First-class interactive runtime orchestration for games, scientific simulations, and academic visualization. It provides a bounded fixed-step loop, `Update` / `FixedUpdate` / `LateUpdate` / `Render` system phases, ECS-style entity/component storage, input action mapping, asset manifests with capability-gated variants, Scene3D engine configs, positional audio playback events, Scene3D-declared physics world construction, and a `sim.Runner` adapter. The physics bridge includes warm-started contact solving, raycasts, distance constraints, and conservative CCD for fast sphere/capsule bodies against static colliders.

```go
rt := game.New(game.Config{
    Profile: game.ScientificProfile(),
    Scene: func(ctx *game.Context) scene.Props {
        return scene.Props{
            Width:  960,
            Height: 540,
            Graph: scene.NewGraph(scene.Mesh{
                ID:       "sample",
                Geometry: scene.SphereGeometry{Radius: 1},
                Material: scene.StandardMaterial{Color: "#77c6ff"},
            }),
        }
    },
})
node := rt.Mount(ctx, gosx.Text("Loading simulation"))
```

Together these packages (`field`, `game`, `sim`, `hub`) give you a complete server-authoritative interactive simulation stack in pure Go, with no third-party real-time engine.

## Semantic Layer

GoSX ships a vector-native semantic layer for content routing, similarity search, and LLM-adjacent workflows:

- **`vecdb`** — in-memory vector database with k-NN search, cosine / inner-product / L2 metrics, and TurboQuant-backed compression. Safe for concurrent reads and writes.
- **`embed`** — embedding provider abstraction. Implement `Provider` to plug in OpenAI, Cohere, a local model, or a deterministic test encoder.
- **`semantic`** — built on `vecdb` + `embed`, with three production-ready primitives:
  - `semantic.Router` — route a request to the most semantically similar handler
  - `semantic.Cache` — cache responses by embedding similarity rather than exact key match
  - `semantic.ContentIndex` — similarity-driven content discovery and ranking

The math for MSE-optimal quantization lives in [TurboQuant](https://github.com/odvcencio/turboquant), a standalone pure-Go module that GoSX consumes as a dependency. You get the compression ratio of an engineered quantizer without taking on a C library.

## Editor

The `editor` package is a set of Go-native building blocks for building text editors inside GoSX apps: a line-array text model (with a CRDT-backed implementation planned), input bindings (keyboard, IME, mouse, touch), a tree-sitter-driven highlight layer, a toolbar model, a theme system, and a VS Code-grammar compatibility shim. It's the substrate for in-page editing experiences — code snippets, markdown drafts, inline content editors — without importing Monaco or CodeMirror.

The default helper bar includes an `emoji` command backed by the in-tree `internal/emoji` table (GitHub gemoji plus Unicode Emoji), with Slack-ish aliases (`:simple_smile:`, `:slight_smile:`, `:thumbs_up:`, `:red_heart:`) accepted too. Picker UIs can pass the selected shortcode as `ToolbarAction.Value`; without a value, selected text is normalized into a shortcode, then falls back to `:smile:`.

For full CommonMark + Markdown++ rendering (admonitions, footnotes, math, sup/sub, task lists, emoji, syntax-highlighted code fences), the `content` package uses [mdpp](https://github.com/odvcencio/mdpp) as the canonical parser/renderer. Apps can still import mdpp directly when they need lower-level renderer control.

## CSS

Classes and external CSS. No CSS-in-JS.

- Sidecar `page.css` / `layout.css` files are auto-discovered and injected
- Component-scoped CSS via `css.ScopeCSS()` with `:where()` selectors
- Four CSS layers: `global`, `layout`, `page`, `runtime`
- `:global()` escape hatch for unscoped rules

## CLI

```bash
gosx init [name] [--template docs]    # Scaffold a new app
gosx dev [app]                        # Dev server with file watching and SSE reload
gosx desktop [dev] [app]              # Dev server inside a native desktop host
gosx desktop --url <url>              # Direct native desktop host smoke
gosx desktop --bundle dist/offline    # Run a packaged app://gosx bundle
gosx desktop --url <url> --native-bridge
                                      # Direct trusted host with built-in desktop APIs
gosx build [--prod] <app>              # Build with hashed assets, optional static prerender
gosx build --offline <app>             # Stage a versioned offline asset bundle
gosx build --msix <app>                # Stage and package Windows MSIX output
gosx build --sign --msix <app>         # Sign MSIX via signtool
gosx build --appinstaller <uri> <app>  # Emit AppInstaller update feed XML
gosx assets plan [path...]            # Inspect 3D/game assets and planned build optimizations
gosx scene render --out image.png <scene-file>
                                      # Render a typed scene natively to PNG (no browser or GPU)
gosx scene check [--golden baseline.png] <scene-file>
                                      # Validate, cost, render, and prove one scene
gosx scene inspect [--strict] [--budget file] <file-or-dir>...
                                      # Authoring report: surface, assets, memory, fallbacks, budgets
gosx scene validate [--strict] <file-or-dir>...
                                      # Structured schema diagnostics for scene documents
gosx scene schema [--out path]        # Emit the SceneIR JSON schema
gosx export [app]                     # Pre-render static pages to dist/static/
gosx compile <file.gsx>                # Compile .gsx to Go
gosx check <file.gsx>                  # Parse, validate, and check strict props with Go
gosx render <file.gsx> [component]     # Render component to HTML
gosx fmt <file.gsx|dir>                # Format source
gosx lsp                              # Language server for editor integration
gosx perf --json <url>...              # Profile browser runtime performance
gosx perf --budget perf-budget.json <url>...
                                      # Profile and fail when a route exceeds budgets
gosx perf compare base.json next.json # Fail on perf regressions
gosx perf budget perf.json budget.json # Check a saved report
gosx size [--json] dist               # Report exact gzip sizes and feature chunks
```

Production builds require TinyGo on `PATH`, emit capability-linked `core`,
`engine`, `collab`, and `full` runtime profiles (plus the legacy `islands`
compatibility artifact), and write `.gz` sidecars for immutable runtime assets
when compression wins. Dev builds still use standard-Go WASM so local
iteration does not depend on the production compiler.

## Performance Budgets

GoSX treats performance as a framework contract, not a dashboard you check after release. `gosx perf` already records TTFB, DCL, LCP, CLS, long tasks, TBT, network bytes, JS coverage, hub bytes, island hydration, Scene3D frame percentiles, and GPU context information. `gosx perf budget` turns those measurements into a CI gate.

```bash
gosx perf \
  --mobile pixel7 --throttle 4 --coverage \
  --budget perf-budget.json \
  --json \
  http://localhost:3000/ \
  http://localhost:3000/dashboard \
  http://localhost:3000/scene > perf.json

gosx perf budget perf.json perf-budget.json
make perf-budget PERF_URLS="http://localhost:3000/ http://localhost:3000/scene"
```

Example `perf-budget.json`:

```json
{
  "defaultProfile": "basic-island",
  "profiles": {
    "static": {
      "assertions": [
        "js_total_kb == 0",
        "lcp <= 1500",
        "long_tasks == 0"
      ]
    },
    "basic-island": {
      "assertions": [
        "js_total_kb <= 35",
        "lcp <= 1500",
        "tbt <= 50"
      ]
    },
    "scene3d": {
      "assertions": [
        "lcp <= 1500",
        "long_tasks == 0",
        "scene_p95 <= 33",
        "scene_p99 <= 50"
      ]
    }
  },
  "routes": [
    {"url": "/", "profile": "static"},
    {"url": "/scene", "profile": "scene3d", "assertions": ["network_kb <= 250"]}
  ]
}
```

Budget metrics include lifecycle and vitals (`ttfb`, `dcl`, `lcp`, `cls`, `tbt`), main-thread blocking (`long_tasks`, `long_task_total`), network (`network_kb`, `requests`), island/runtime (`island_count`, `hydration_total`, `heap_mb`, `hub_bytes`), JS coverage (`js_total_kb`, `js_used_kb`, `js_unused_kb`, `js_used_pct`), and Scene3D (`scene_p50`, `scene_p95`, `scene_p99`, `scene_dropped_frames`). JS coverage budgets require profiling with `--coverage`. New `gosx init` apps include a starter `perf-budget.json`, and the repository default lives at `perf/budgets/default.json`.

Production builds and static exports also write route capability metadata into `dist/export.json`. Each route records whether the rendered page actually shipped navigation, bootstrap, WASM, islands, engines, hubs, Scene3D, managed video, or motion. That makes the "pay for what you use" contract inspectable from the build artifact, not just from source assumptions.

### WASM size-budget gate

`make wasm-size-budget` (script: `scripts/check-wasm-size.sh`) builds both
flavors of `client/wasm` and asserts the resulting WebAssembly artifacts stay
within budget. CI runs the gate on every PR. Baselines (Phase 1c shipped):

| Flavor | Build tags                    | Shipped (Phase 1c) | Budget |
|--------|-------------------------------|--------------------|--------|
| full   | _(none)_                      | ~1,368 KB          | 5,500 KB |
| tiny   | `gosx_tiny_islands_only`      | ~684 KB            | 3,200 KB |

Override the budget for a planned-growth slice by exporting
`WASM_FULL_BUDGET_KB` and/or `WASM_TINY_BUDGET_KB`. **Any budget increase
greater than 10% over the Phase 1c baseline requires an ADR** explaining what
deliberate growth shipped (e.g. Phase 2's `<CanvasBoard>` primitive, future
opcode-set expansion). The gate fires on incidental regressions so they get
caught at the PR boundary instead of slipping into a release.

`gosx desktop [app]` opens the dev server in the native desktop host. On Windows
it uses WebView2 through the pure-Go `desktop` package; `gosx desktop --url
https://example.com` opens a URL directly for host smoke checks. `gosx desktop
--bundle dist/offline` serves a packaged offline/static bundle from `app://gosx`
and enables the trusted native bridge by default. The Windows host supports hot
reload, typed IPC envelopes, devtools, single-instance forwarding, deep links,
file associations, lifecycle callbacks, tray icons, native menus, context menus,
notifications, file drop, per-monitor DPI awareness, and a minimal accessibility
surface. Multi-window construction has a public API shape, but the current
Windows backend still returns `desktop.ErrUnsupported` for extra windows until
shared WebView2-environment support lands. From WSL or CI, `make
build-desktop-windows` emits `build/gosx-windows-amd64.exe` and
`build/gosx-windows-arm64.exe` for handoff to a Windows host.

Trusted desktop content can call `window.gosxDesktop.app`,
`window.gosxDesktop.window`, `window.gosxDesktop.dialog`,
`window.gosxDesktop.clipboard`, `window.gosxDesktop.shell`, and
`window.gosxDesktop.notification` when `desktop.Options.NativeBridge` or
`gosx desktop --native-bridge` is enabled.

App-owned Go services can be bound onto the same typed bridge without a
JavaScript build step:

```go
type Preferences struct{}

func (Preferences) Load(ctx context.Context, req LoadPrefsRequest) (LoadPrefsResponse, error) {
	return LoadPrefsResponse{Theme: "system"}, nil
}

app, _ := desktop.New(desktop.Options{URL: "app://gosx/static/"})
_, _ = app.Bind("prefs", Preferences{})
```

Trusted desktop content can then call:

```js
const prefs = await window.gosxDesktop.service("prefs").load({ scope: "user" });
```

The `desktop` package also exposes release-time hooks: `App.UpdateCheck()` /
`App.UpdateApply()` consume MSIX AppInstaller feeds, and
`CrashReporterOptions` captures Go panics plus Windows minidumps with optional
user-consented upload.

`gosx build --prod` emits a deployable `dist/` bundle with a server binary,
hashed assets, prerendered static pages, an ISR manifest, and edge worker
support. Add `--offline` to stage `dist/offline/` with a versioned asset
manifest, `--msix` to generate `dist/msix/package/AppxManifest.xml` and
`dist/app.msix` through MakeAppx, `--sign` to run signtool with
`GOSX_CODESIGN_CERT` / `GOSX_CODESIGN_KEY`, and `--appinstaller <uri>` to emit
`dist/app.appinstaller` for AppInstaller-based updates.

## Deploy

Three tiers:

1. **Static** — `gosx export` pre-renders HTML. No server needed.
2. **Server** — Go binary. SSR, actions, hubs, ISR, Scene3D, sim, workspace.
3. **Edge** — Prerendered routes at the edge, dynamic requests fall back to origin.

## Packages

| Package | Purpose |
|---------|---------|
| `gosx` | Node API, grammar, parser, compiler |
| `ir` | Intermediate representation, lowering, validation, expression parser. Experimental pre-1.0: pin an exact gosx version if you compile against it directly. |
| `island` | Island renderer, manifest generation, program serialization |
| `signal` | Reactive state: `Signal[T]`, `Computed[T]`, `Effect`, `Batch` |
| `server` | HTTP server, page rendering, caching, streaming, i18n, edge annotations, assets |
| `route` | File-based routing, layouts, data loaders, modules. Includes an EXPERIMENTAL render-profile hook (`RenderProfile`, gosx#185): pin an exact gosx version if you depend on it directly. |
| `content` | mdpp-backed Markdown/MDX/Markdown++ collection loading with typed metadata, diagnostics, and renderer hooks |
| `components` | Registry and binding adapters for server component libraries |
| `ui` | GoSX UI primitives and registry-backed component library seed |
| `action` | Named mutation handlers with validation |
| `session` | Signed cookie sessions, CSRF, flash state |
| `auth` | Auth middleware, OAuth, magic links, WebAuthn |
| `hub` | WebSocket presence, fanout, shared state |
| `scene` | Scene3D: typed scene graph, PBR, shadows, glTF, particles, PostFX, WebGL + WebGPU runtimes |
| `physics` | Warm-started rigid body contacts, constraints, raycasts, conservative CCD, and Scene3D bridge |
| `field` | 3D vector fields, trilinear sampling, operators, per-component compression, hub streaming |
| `sim` | Server-authoritative game simulation: tick loop, snapshots, replay, spectator sync |
| `crdt` | Conflict-free replicated documents with bloom-filter sync protocol |
| `workspace` | Distributed semantic collaboration (CRDT + hub + vecdb) |
| `vecdb` | In-memory vector database with k-NN search and quantized storage |
| `embed` | Embedding provider abstraction |
| `semantic` | Semantic router, similarity cache, content index |
| `engine` | Worker/surface model with capability declarations |
| `engine/wasm` | Standard-Go WASM engine registration, browser context, and instance lifecycle |
| `editor` | Go-native text editor building blocks (textmodel, input, highlight, toolbar, vscode shim) |
| `highlight` | Syntax highlighting for Go, GSX, JavaScript/TypeScript, JSON, and Bash |
| `client/vm` | Expression VM, tree reconciler, patch generation |
| `client/bridge` | WASM bridge for island/engine lifecycle |
| `client/wasm` | WASM entry point |
| `client/js` | Authored TypeScript browser runtime and generated bundles: bootstrap, patch applier, and feature chunks (islands, hubs, engines, Scene3D WebGL/WebGPU/glTF/animation) |
| `render` | Server-side HTML rendering from IR |
| `css` | Component-scoped CSS with `:where()` selectors |
| `textlayout` | Text measurement, line breaking, ellipsis |
| `format` | Source formatter for `.gsx` files |
| `lsp` | Language server protocol for editor integration |
| `apptest` | HTTP testing helpers for pages, APIs, and forms |
| `islandtest` | Island program testing helpers |
| `dev` | Development server with file watching |
| `desktop` | Native desktop host backed by Windows WebView2, shell integration, native UI, crash reports, and update feed helpers |
| `env` | `.env` file loading with mode support |
| `cmd/gosx` | CLI tool |

## Testing

```bash
make test          # Full package test pass
make test-race     # Race detector enabled
make verify-danmuji # Regenerate .dmj specs and fail if generated Go is stale
make test-fuzz-smoke # Bounded native Go fuzzing for high-risk generated harnesses
make test-js       # Bootstrap + patch under Node test runner
make test-wasm     # WASM runtime through exported functions
make test-wasm-islands # Slim island-only WASM runtime through exported functions
make test-e2e      # chromedp browser tests against gosx dev (go test -tags e2e ./e2e)
make test-desktop  # Desktop package tests plus Windows cross-compile guards
make test-desktop-macos # macOS desktop/cmd cross-compile guardrails
make build-desktop-windows  # Windows desktop-capable CLI binaries
make build-desktop-macos    # macOS CLI binaries; native backend still unsupported
make build-runtime # TinyGo production capability-profile WASM runtime builds
make canopy-index  # Memory-bounded structural index in .canopy/index.json
make canopy-stats  # Inspect the cached structural index
make ci            # All of the above + build verification
```

Use the Make targets for repo-wide canopy work. They apply `.canopyignore`,
limit Go scheduler width, set a Go memory target, and keep a hard timeout/VM
ceiling around the process so generated bundles, `node_modules`, build output,
and the index cache itself cannot turn structural analysis into an OOM risk.

Client correctness is verified at four layers: pure Go VM/bridge tests, JS runtime contract tests under Node, compiler-to-bridge integration tests, and live chromedp browser tests against the docs app. The high-risk domains have explicit proof tests: auth/session tamper rejection, encrypted cookie rotation, and CSRF JSON/header paths; CRDT partition convergence, large-history sync, and tombstone persistence; physics CCD, warm-started stacks, and 10k-collider raycast scale; route specificity across static/dynamic/catch-all patterns; Scene3D 1000-level hierarchy transform propagation; and docs accessibility invariants for landmarks, named controls, duplicate IDs, and ARIA references. Danmuji `.dmj` specs add scenario/property coverage plus generated native Go fuzz harnesses for session cookie decoding, CRDT document loading, physics raycasts, and escaped router paths.

## Dependencies

The discipline is real, but it is per-package rather than per-module, and the distinction matters when you audit it.

**The root `gosx` package pulls exactly one third-party module.** `go list -deps .` returns `gotreesitter` (with its grammar packages) and nothing else. Import `gosx` alone and that is your whole external surface.

The six libraries the framework's runtime paths use:

- [gotreesitter](https://github.com/odvcencio/gotreesitter) — pure-Go tree-sitter runtime with grammar composition
- [turboquant](https://github.com/odvcencio/turboquant) — pure-Go MSE-optimal vector quantizer powering `vecdb`
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket support for hubs
- [rivo/uniseg](https://github.com/rivo/uniseg) — Unicode segmentation for text layout
- [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) — image optimization
- [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) — HTML utilities

**The module graph is larger, and `go.mod` is the honest record.** Beyond those six it carries direct requires for tooling and optional subsystems: `chromedp` and `cdproto` (the `perf` profiler and browser tests), `brotli` (asset compression), `fsnotify` (the dev watcher), `mdpp` (Markdown++ in `content`), `pixelmatch` (visual regression), `go-redis` and `miniredis` (the optional Redis stores and their tests), `selena` (shader compilation), and `golang.org/x/sys`. A `//go:build tools` file additionally pins sibling M31 Labs modules ahead of their migrations, which is why `google.golang.org/grpc` and `protobuf` appear in `go.sum` — they reach no shipped binary, but they do reach a dependency audit.

**No CGo.** `CGO_ENABLED=0 go build ./...` is clean, and `windows/amd64`, `darwin/arm64` and `linux/arm64` all cross-compile clean. `GOOS=js GOARCH=wasm` builds every package, with no exceptions — `make build-wasm-all` is a CI gate, so this sentence fails the build rather than going stale.

**No JavaScript toolchain** — for your app. The framework itself owns a
TypeScript browser-runtime pipeline and vendors `hls.min.js` for HLS playback;
generated bundles are checked against those authored sources. The claim is
about application builds, and there it holds: no Node, npm, or bundler config.

## Built On

GoSX is built on [gotreesitter](https://github.com/odvcencio/gotreesitter), a clean-room reimplementation of the tree-sitter runtime in pure Go. gotreesitter enables in-process grammar composition — GoSX extends Go's own grammar with native markup syntax, which is how `.gsx` files are parsed without code generation or external build tools.

The same compiler infrastructure powers [Arbiter](https://github.com/odvcencio/arbiter) (a governed outcomes language), [Danmuji](https://github.com/odvcencio/danmuji) (a BDD testing language for Go), and [Ferrous Wheel](https://github.com/odvcencio/ferrous-wheel) (Rust-inspired syntax for Go).

## Status

GoSX is pre-1.0. The current release is **v0.42.2**. The five primitives (Server, Action, Island, Engine, Hub) are stable in shape — we do not expect their top-level API to change before 1.0. Subsystems like `scene`, `desktop`, `field`, `sim`, `workspace`, and `semantic` are still under active development and may take breaking changes; each such change is called out explicitly in [CHANGELOG.md](./CHANGELOG.md) with a migration path.

If you're evaluating GoSX for production work, the server + island + route + engine + scene stack has been used in production. The semantic, workspace, and sim layers have production users but are newer.

## License

MIT
