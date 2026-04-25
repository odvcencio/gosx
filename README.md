# GoSX

A Go-native web platform. Write components in `.gsx` — Go with embedded markup — compile through a real compiler pipeline, render on the server by default, hydrate interactive islands with WebAssembly. No JavaScript toolchain. No CGo. Six dependencies.

Current release: **v0.18.9**. Pre-1.0; breaking changes are documented in [CHANGELOG.md](./CHANGELOG.md).

## What if you never had to leave Go?

GoSX starts from a simple premise: the browser is a render target, not a runtime. Server components are Go functions that return HTML. Interactive components compile to bytecode and run in a shared WASM VM. Everything between those two points — the parser, the compiler, the reconciler, the signal system, the 3D scene graph, the vector store, the collaborative document model — is pure Go.

```go
package app

// Server component: renders to HTML, zero JavaScript.
func Greeting(props GreetingProps) Node {
    return <div class="greeting">
        <h1>Hello, {props.Name}!</h1>
        <p>Welcome to GoSX.</p>
    </div>
}

// Island component: compiles to bytecode, hydrates in the browser.
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

The `//gosx:island` directive marks a component for client-side hydration. The compiler extracts signals, computed values, and handlers from the Go source, compiles expressions to VM opcodes, and serializes the result as a compact island program (~1-10KB). Server components emit static HTML with no client-side cost.

## Philosophy

GoSX is opinionated about a small number of things and flexible about everything else.

- **The browser is a render target.** Server components are the baseline. Client-side JavaScript is something you opt into, feature by feature, not a default ambient runtime.
- **One language, one toolchain.** You write Go. `go build` produces the server, the CLI, the WASM binary, and the client bundle. There is no Node, no npm, no webpack, no bundler config. `gosx dev` is a Go binary watching Go files.
- **No JavaScript toolchain is not zero browser cost.** GoSX still ships a measured browser bootstrap, feature chunks, and WASM runtime only when a route needs them. The performance contract is that the compiler and build pipeline justify every shipped runtime slice.
- **No CGo, anywhere.** Every package compiles to WASM and cross-compiles cleanly. The 3D engine runs in pure Go. The vector store runs in pure Go. The CRDT sync protocol runs in pure Go. This is not a portability footnote — it is the design constraint that lets Scene3D, `field`, `vecdb`, and `crdt` ship as ordinary Go libraries that also happen to run in a browser tab.
- **Primitives, not frameworks-within-frameworks.** A form submission is not a canvas game is not a collaborative document. GoSX gives you five distinct execution primitives and enforces the distinction; none of them try to be the others.
- **You pay for what you use.** Static pages are static. Islands ship only when a page has an island. Engines are opt-in. The shared WASM VM is lazy-loaded. An app with no islands has no client VM; an app with no engines has no engine bundle.
- **No hidden magic in the hot path.** The compiler pipeline is inspectable (`gosx compile`, `gosx check`). The IR is a flat-array data structure. The island VM is ~40 opcodes. The client JS host is ~940 lines. You can read all of it.
- **Six dependencies.** That's not marketing — it's a design budget. Every new transitive dep is a bug surface, a license to audit, and a supply-chain risk. We take that budget seriously.

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

Scene3D is the built-in 3D engine primitive: prop-based scenes and composable `<Scene3D><Mesh /><Points /></Scene3D>` authoring both lower toward the same versioned SceneIR contract.

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
    "net/http"
    "github.com/odvcencio/gosx"
    "github.com/odvcencio/gosx/server"
)

func main() {
    app := server.New()
    app.Page("/", func(r *http.Request) gosx.Node {
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
route.MustRegisterFileModuleHere(route.FileModuleOptions{
    Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
        post, err := db.GetPost(ctx.Param("slug"))
        if err != nil {
            return nil, route.NotFound()
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
})
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
       -> compile expressions to VM opcodes (40+ operations)
       -> serialize as IslandProgram (JSON dev / binary prod)
       -> browser: shared WASM VM + thin JS host (~940 lines)
       -> per-island programs are 1-10KB each
```

Island expressions are constrained to what the client VM can evaluate: literals, property and signal access, arithmetic, comparisons, boolean logic, string operations, conditionals, handler dispatch, and list iteration. Goroutines, channels, and arbitrary Go are compile-time errors in islands.

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

**Sessions and Auth** — Cookie-backed sessions with HMAC-SHA256 signing, CSRF protection, flash values. Auth supports sessions, magic links, OAuth 2.0 (GitHub, Google), and WebAuthn/Passkeys.

**Actions** — Named server-side mutation handlers with form/JSON parsing, field-level validation errors, and redirect-safe flash state.

**Caching** — Semantic cache helpers (`ctx.CacheStatic()`, `ctx.CacheRevalidate()`, `ctx.CacheData()`), automatic weak ETags from content hashing, path/tag-based revalidation, and ISR with background regeneration.

**Navigation** — Opt-in client-side page transitions via `app.EnableNavigation()` with managed head swaps and intent-prefetching. Pages render server-first, enhance progressively.

**Streaming** — Deferred page regions via `ctx.Defer()` render fallback content immediately, then stream resolved content into place.

**Image Optimization** — Local image handler at `/_gosx/image` with resize, format conversion, and immutable caching.

**Text Layout** — `TextBlock` supports both server-measured native rendering with no JavaScript and bootstrap-managed browser refinement. Font, width, line-height, locale, clamping, and ellipsis stay in one framework-level contract.

**Managed Video** — `server.Video`, `ctx.Video`, and the `.gsx` `<Video />` builtin render a real server `<video>` baseline with `<source>` and `<track>` children, then the built-in video engine can layer in HLS fallback, subtitle loading, sync, and shared `$video.*` signals when the page needs them.

**Managed Motion** — `server.Motion`, `ctx.Motion`, and the `.gsx` `<Motion />` builtin expose server-authored motion presets that run on the shared bootstrap layer. Preset, trigger, duration, delay, easing, reduced-motion policy, and distance all stay in one declarative contract.

## Engines

For work that doesn't fit the island model — canvas rendering, WebGL, background computation:

```go
ctx.Engine(engine.Config{
    Name:                 "visualizer",
    Kind:                 engine.KindSurface,
    Capabilities:         []engine.Capability{engine.CapCanvas, engine.CapAnimation},
    RequiredCapabilities: []engine.Capability{engine.CapCanvas, engine.CapWASM},
    WASMPath:             "/engines/visualizer.wasm",
}, fallbackNode)
```

Engines come in three kinds:

- `surface` — owns a DOM mount for canvas, WebGL, WebGPU, or managed pixel surfaces
- `worker` — background compute with no DOM mount
- `video` — framework-owned managed video playback

`Capabilities` declares what the engine can use. `RequiredCapabilities` is the hard browser gate: if a required API like `webgl`, `webgpu`, or `wasm` is missing, GoSX marks the mount unsupported and does not run the engine factory.

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
- `audio`
- `worker`
- `gamepad`
- `keyboard`
- `pointer`

Kinds choose the mount model. Capabilities declare which browser APIs the engine expects to use. Engines get their own mount point or worker context, communicate through typed message ports, and do not touch island DOM.

## Scene3D — 3D Engine

The `scene` package is a full 3D engine authored entirely in Go. You describe the scene as a typed Go struct tree; the runtime lowers it to a compact IR, streams it to the client, and renders it through a pair of pure-Go-authored backends (WebGL and WebGPU) that reach the browser as part of the standard bootstrap bundle. There is no separate engine binary. There is no three.js. There is no JavaScript scene graph.

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
    PostFX: scene.PostFX{
        MaxPixels: scene.PostFXMaxPixels1080p,
        Effects: []scene.PostEffect{
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
            },
            Position:      scene.Vec3(0, 0.5, 0),
            CastShadow:    true,
            ReceiveShadow: true,
        },
        scene.Model{Src: "/assets/ship.glb", Animation: "idle"},
    ),
}
```

### Feature surface

- **Scene graph** — `Group`, `Mesh`, `InstancedMesh`, `Points`, `Label`, `Sprite`, `Html`, `Model`, `ComputeParticles`, per-node transforms, nesting, world-transform lowering
- **Geometry** — `Box`, `Cube`, `Plane`, `Pyramid`, `Sphere`, `Lines`, `Cylinder`, `Torus`, plus arbitrary geometry from loaded models
- **Materials** — `StandardMaterial` (PBR with roughness/metalness workflow), `FlatMaterial`, `GhostMaterial`, `GlassMaterial`, `GlowMaterial`, `MatteMaterial`, configurable blend modes and render passes
- **Lights** — `AmbientLight`, `DirectionalLight`, `PointLight`, `SpotLight`, `HemisphereLight`; shadows on directional and spot with per-light `ShadowSize` and a scene-wide `Shadows.MaxPixels` cap
- **glTF / GLB** — `scene.Model{Src: "/assets/thing.glb"}` loads binary or JSON glTF 2.0 through the in-runtime pure-JS loader (`19-scene-gltf.js`), including animations
- **Animation** — `AnimationClip` / `AnimationChannel` for node-level keyframe animation, `Spin` convenience for auto-rotation, glTF animation playback
- **Particles** — GPU-computed particle systems via `ComputeParticles` with emitter, forces, and material
- **Environment** — ambient, hemisphere, sky/ground, cubemap IBL, exposure, fog, tonemapping
- **Post-processing** — `Bloom`, `Tonemap` (ACES / Reinhard / Filmic), `Vignette`, `ColorGrade`, FXAA 3.11, RGB9E5/HDR intermediate selection, HDR10 presentation when supported, composable chain, runs on both WebGL and WebGPU
- **Shadow pixel cap** — v0.15.0's `Shadows.MaxPixels` caps each shadow map (default 1024²), preventing multi-megabyte-per-light allocations when individual lights request large shadow sizes
- **Compression & LOD** — per-component scalar quantization with delta encoding, progressive streaming, camera-distance-based LOD switching via `scene.Compression`
- **Transitions** — declarative enter/exit/state transitions on any scene node via `InState` / `OutState` / `Live`
- **Camera controls** — `orbit`, `drag-to-rotate`, focus targets, pick signals, drag signals, event signals exposed as `$`-signals consumable by surrounding islands
- **Capability tiers** — graceful degradation across WebGPU → WebGL → canvas fallbacks
- **Backends at parity** — WebGL, first-party WebGPU bundle rendering, and the headless backend honor the same IR, lighting, post-processing, shadow, and particle contracts where their target surface supports it
- **CSS-stylable 3D** — composable materials, lights, environment, point layers, and post-FX can read `var(--scene-*)` custom properties through the planner, so class changes, media queries, and CSS transitions can drive scene state without authored JavaScript animation code

The scene graph is inspectable Go code. The IR is serializable. The renderer is reproducible. You can hold the whole thing in your head, and when something goes wrong you read Go and JavaScript — not a black box.

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

The `crdt` package implements a conflict-free replicated document model with a wire-compatible sync protocol (bloom-filter-based message exchange, delta-encoded changes, vector-clock causality tracking). It's independent of the transport — you can drive it over a `hub`, over Redis, or over raw bytes in a file.

```go
doc := crdt.NewDoc()
doc.Apply(change)               // apply a local or remote change
bytes, _ := doc.Save()          // serialize the full doc state
sync := crdtsync.NewState()    // start a sync session
msg := sync.Generate(doc)      // produce the next sync message to send
```

The `workspace` package layers a distributed semantic collaboration space on top of `crdt` + `hub` + `vecdb`: agents join a workspace, write findings with vector embeddings, query across peers, and persist state. It's used in GoSX's multi-agent tooling and is also the substrate for any app that wants "multiple clients editing shared state with presence and similarity search" without building it from scratch.

## Volumetric Data & Simulation

**`field`** — 3D vector fields. Trilinear sampling, axis-aligned bounding boxes, and a full set of operators (`Advect`, `Curl`, `Divergence`, `Gradient`, `Blur`, `Resample`). Per-component scalar quantization with delta encoding collapses a 256³ float32 vector field from ~64 MB to a few hundred KB, and streaming publish/subscribe over `hub` lets a server-authoritative simulation broadcast field updates to subscribed clients. The package is renderer-agnostic — it's the substrate for volumetric rendering, particle advection, fluid simulation, and anything else that needs structured 3D data at a distance.

**`sim`** — Server-authoritative game simulation. Games implement the `Simulation` interface; a `Runner` drives it at a fixed tick rate, collects per-client inputs from a hub, broadcasts state snapshots, and handles replay and spectator sync. The server is the source of truth; clients submit inputs and render the authoritative state they receive back.

Together these three packages (`field`, `sim`, `hub`) give you a complete server-authoritative multiplayer stack in pure Go, with no third-party real-time engine.

## Semantic Layer

GoSX ships a vector-native semantic layer for content routing, similarity search, and LLM-adjacent workflows:

- **`vecdb`** — in-memory vector database with k-NN search, cosine / inner-product / L2 metrics, and TurboQuant-backed compression. Safe for concurrent reads and writes.
- **`embed`** — embedding provider abstraction. Implement `Provider` to plug in OpenAI, Cohere, a local model, or a deterministic test encoder.
- **`semantic`** — built on `vecdb` + `embed`, with three production-ready primitives:
  - `semantic.Router` — route a request to the most semantically similar handler
  - `semantic.Cache` — cache responses by embedding similarity rather than exact key match
  - `semantic.ContentIndex` — similarity-driven content discovery and ranking

The math for MSE-optimal quantization lives in [TurboQuant](https://github.com/odvcencio/turboquant), a standalone pure-Go module that GoSX consumes as a dependency. You get the compression ratio of an engineered quantizer without taking on a C library.

## Markdown++

The `markdown` package is a pure-Go CommonMark parser with an extension set we call Markdown++:

- **Admonitions** — `:::note`, `:::warning`, `:::tip` blocks with optional titles
- **Footnotes** — Pandoc-style `[^ref]` with automatic back-references
- **Math** — inline `$...$` and display `$$...$$` with MathML output
- **Superscript / subscript** — `^like this^` and `~like this~`
- **Task lists** — GitHub-flavored checkboxes
- **Emoji shortcodes** — `:rocket:` → 🚀
- **Syntax highlighting** — per-language via the `highlight` package, with both native Go and WASM-dispatched highlighters

The renderer is configurable per-document (heading IDs, hard wraps, emoji wrapping, unsafe HTML passthrough, custom image resolvers) and integrates with the `highlight` package for code fences.

## Editor

The `editor` package is a set of Go-native building blocks for building text editors inside GoSX apps: a text model with rope-like document storage, input bindings (keyboard, IME, mouse, touch), a highlight layer, a toolbar model, a theme system, and a VS Code-grammar compatibility shim. It's the substrate for in-page editing experiences — code snippets, markdown drafts, inline content editors — without importing Monaco or CodeMirror.

The default helper bar includes an `emoji` command. It inserts standard Markdown++ emoji shortcodes (`:rocket:`, `:+1:`, `:t-rex:`, `:face_with_spiral_eyes:`) so editor content stays portable and renders through the same GitHub gemoji plus Unicode Emoji table used by the markdown renderer. Slack-ish compatibility aliases such as `:simple_smile:`, `:slight_smile:`, `:thumbs_up:`, and `:red_heart:` are accepted too. Picker UIs can pass the selected shortcode as `ToolbarAction.Value`; without a value, selected text is normalized into a shortcode, then falls back to `:smile:`.

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
gosx build [--prod] [app]             # Build with hashed assets, optional static prerender
gosx build --offline [app]            # Stage a versioned offline asset bundle
gosx build --msix [app]               # Stage and package Windows MSIX output
gosx build --sign --msix [app]        # Sign MSIX via signtool
gosx build --appinstaller <uri> [app] # Emit AppInstaller update feed XML
gosx export [app]                     # Pre-render static pages to dist/static/
gosx compile [file.gsx]               # Compile .gsx to IR
gosx check [file.gsx]                 # Parse and validate
gosx render [file.gsx]                # Render component to HTML
gosx fmt [file.gsx]                   # Format source
gosx lsp                              # Language server for editor integration
gosx perf --json [url...]             # Profile browser runtime performance
gosx perf --budget perf-budget.json [url...]
                                      # Profile and fail when a route exceeds budgets
gosx perf compare base.json next.json # Fail on perf regressions
gosx perf budget perf.json budget.json # Check a saved report
```

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

`gosx desktop [app]` opens the dev server in the native desktop host. On Windows
it uses WebView2 through the pure-Go `desktop` package; `gosx desktop --url
https://example.com` opens a URL directly for host smoke checks. The Windows
host supports hot reload, typed IPC envelopes, devtools, single-instance
forwarding, deep links, file associations, lifecycle callbacks, multi-window
construction, tray icons, native menus, context menus, notifications, file
drop, per-monitor DPI awareness, and a minimal accessibility surface. From WSL
or CI, `make build-desktop-windows` emits `build/gosx-windows-amd64.exe` and
`build/gosx-windows-arm64.exe` for handoff to a Windows host.

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
| `ir` | Intermediate representation, lowering, validation, expression parser |
| `island` | Island renderer, manifest generation, program serialization |
| `signal` | Reactive state: `Signal[T]`, `Computed[T]`, `Effect`, `Batch` |
| `server` | HTTP server, page rendering, caching, streaming, assets |
| `route` | File-based routing, layouts, data loaders, modules |
| `action` | Named mutation handlers with validation |
| `session` | Signed cookie sessions, CSRF, flash state |
| `auth` | Auth middleware, OAuth, magic links, WebAuthn |
| `hub` | WebSocket presence, fanout, shared state |
| `scene` | Scene3D: typed scene graph, PBR, shadows, glTF, particles, PostFX, WebGL + WebGPU runtimes |
| `field` | 3D vector fields, trilinear sampling, operators, per-component compression, hub streaming |
| `sim` | Server-authoritative game simulation: tick loop, snapshots, replay, spectator sync |
| `crdt` | Conflict-free replicated documents with bloom-filter sync protocol |
| `workspace` | Distributed semantic collaboration (CRDT + hub + vecdb) |
| `vecdb` | In-memory vector database with k-NN search and quantized storage |
| `embed` | Embedding provider abstraction |
| `semantic` | Semantic router, similarity cache, content index |
| `engine` | Worker/surface model with capability declarations |
| `markdown` | CommonMark + Markdown++ extensions (admonitions, footnotes, math, sup/sub) |
| `editor` | Go-native text editor building blocks (textmodel, input, highlight, toolbar, vscode shim) |
| `highlight` | Syntax highlighting for Go, GSX, JavaScript, JSON, and Bash |
| `client/vm` | Expression VM, tree reconciler, patch generation |
| `client/bridge` | WASM bridge for island/engine lifecycle |
| `client/enginevm` | Lightweight VM for engine scripting |
| `client/wasm` | WASM entry point |
| `client/js` | Bootstrap + patch applier (~940 lines total) |
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
make test-js       # Bootstrap + patch under Node test runner
make test-wasm     # WASM runtime through exported functions
make test-e2e      # Playwright browser tests against gosx dev
make test-desktop  # Desktop package tests plus Windows cross-compile guards
make build-desktop-windows  # Windows desktop-capable CLI binaries
make ci            # All of the above + build verification
```

Client correctness is verified at four layers: pure Go VM/bridge tests, JS runtime contract tests under Node, compiler-to-bridge integration tests, and live Playwright browser tests against the docs app.

## Dependencies

Six:

- [gotreesitter](https://github.com/odvcencio/gotreesitter) — pure-Go tree-sitter runtime with grammar composition
- [turboquant](https://github.com/odvcencio/turboquant) — pure-Go MSE-optimal vector quantizer powering `vecdb` and `crdt` compression
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket support for hubs
- [rivo/uniseg](https://github.com/rivo/uniseg) — Unicode segmentation for text layout
- [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) — image optimization
- [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) — HTML utilities

No CGo. No JavaScript toolchain. Compiles anywhere `GOOS/GOARCH` can reach, including WASM.

## Built On

GoSX is built on [gotreesitter](https://github.com/odvcencio/gotreesitter), a clean-room reimplementation of the tree-sitter runtime in pure Go. gotreesitter enables in-process grammar composition — GoSX extends Go's own grammar with native markup syntax, which is how `.gsx` files are parsed without code generation or external build tools.

The same compiler infrastructure powers [Arbiter](https://github.com/odvcencio/arbiter) (a governed outcomes language), [Danmuji](https://github.com/odvcencio/danmuji) (a BDD testing language for Go), and [Ferrous Wheel](https://github.com/odvcencio/ferrous-wheel) (Rust-inspired syntax for Go).

## Status

GoSX is pre-1.0. The current release is **v0.18.9**. The five primitives (Server, Action, Island, Engine, Hub) are stable in shape — we do not expect their top-level API to change before 1.0. Subsystems like `scene`, `desktop`, `field`, `sim`, `workspace`, and `semantic` are still under active development and may take breaking changes; each such change is called out explicitly in [CHANGELOG.md](./CHANGELOG.md) with a migration path.

If you're evaluating GoSX for production work, the server + island + route + engine + scene stack has been used in production. The semantic, workspace, and sim layers have production users but are newer.

## License

MIT
