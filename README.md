# GoSX

A Go-native web platform. Write components in `.gsx` — Go with embedded markup — compile through a real compiler pipeline, render on the server by default, hydrate interactive islands with WebAssembly. No JavaScript toolchain. Five dependencies.

## What if you never had to leave Go?

GoSX starts from a simple premise: the browser is a render target, not a runtime. Server components are Go functions that return HTML. Interactive components compile to bytecode and run in a shared WASM VM. Everything between those two points — the parser, the compiler, the reconciler, the signal system — is pure Go.

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

## Five Primitives

GoSX provides five execution primitives. A form submission is not a canvas game is not a collaborative document — the framework enforces that distinction.

| Primitive | What it does | Client cost |
|-----------|-------------|-------------|
| **Server** | Renders pages and API responses | None |
| **Action** | Handles mutations (forms, RPCs) with structured validation | None |
| **Island** | Reactive DOM subtrees with signals and event delegation | Shared WASM VM + tiny program payload |
| **Engine** | Heavy client compute — canvas, WebGL, background workers | Dedicated WASM or JS bundle |
| **Hub** | WebSocket presence, fanout, shared state, CRDT sync | WebSocket connection |

Use what you need. A static marketing page uses only Server. A dashboard adds Islands. A game adds an Engine. A collaborative editor adds a Hub. You never pay for what you don't use.

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

## Engines

For work that doesn't fit the island model — canvas rendering, WebGL, background computation:

```go
ctx.Engine(engine.Config{
    Name:         "visualizer",
    Kind:         engine.KindSurface,
    Capabilities: []engine.Capability{engine.CapCanvas, engine.CapAnimation},
    WASMPath:     "/engines/visualizer.wasm",
}, fallbackNode)
```

Engines come in three kinds:

- `surface` — owns a DOM mount for canvas, WebGL, WebGPU, or managed pixel surfaces
- `worker` — background compute with no DOM mount
- `video` — framework-owned managed video playback

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

## Hubs

WebSocket primitives for presence, fanout, and shared state:

```go
h := hub.New("workspace")
h.On("update", func(client *hub.Client, msg hub.Message) {
    h.Broadcast("state", currentState)
})
```

Hubs integrate with the CRDT system for conflict-free collaborative state synchronization across peers.

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
gosx build [--prod] [app]             # Build with hashed assets, optional static prerender
gosx export [app]                     # Pre-render static pages to dist/static/
gosx compile [file.gsx]               # Compile .gsx to IR
gosx check [file.gsx]                 # Parse and validate
gosx render [file.gsx]                # Render component to HTML
gosx fmt [file.gsx]                   # Format source
gosx lsp                              # Language server for editor integration
```

`gosx build --prod` emits a deployable `dist/` bundle with a server binary, hashed assets, prerendered static pages, an ISR manifest, and edge worker support.

## Deploy

Three tiers:

1. **Static** — `gosx export` pre-renders HTML. No server needed.
2. **Server** — Go binary. SSR, actions, hubs, ISR.
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
| `engine` | Worker/surface model with capability declarations |
| `crdt` | Conflict-free replicated data types with sync protocol |
| `client/vm` | Expression VM, tree reconciler, patch generation |
| `client/bridge` | WASM bridge for island/engine lifecycle |
| `client/wasm` | WASM entry point |
| `client/js` | Bootstrap + patch applier (~940 lines total) |
| `render` | Server-side HTML rendering from IR |
| `css` | Component-scoped CSS with `:where()` selectors |
| `textlayout` | Text measurement, line breaking, ellipsis |
| `highlight` | Syntax highlighting for Go, GSX, JS, JSON, Bash |
| `format` | Source formatter for `.gsx` files |
| `lsp` | Language server protocol for editor integration |
| `apptest` | HTTP testing helpers for pages, APIs, and forms |
| `dev` | Development server with file watching |
| `env` | `.env` file loading with mode support |
| `cmd/gosx` | CLI tool |

## Testing

```bash
make test          # Full package test pass
make test-race     # Race detector enabled
make test-js       # Bootstrap + patch under Node test runner
make test-wasm     # WASM runtime through exported functions
make test-e2e      # Playwright browser tests against gosx dev
make ci            # All of the above + build verification
```

Client correctness is verified at four layers: pure Go VM/bridge tests, JS runtime contract tests under Node, compiler-to-bridge integration tests, and live Playwright browser tests against the docs app.

## Dependencies

Five:

- [gotreesitter](https://github.com/odvcencio/gotreesitter) — pure-Go tree-sitter runtime with grammar composition
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket support for hubs
- [rivo/uniseg](https://github.com/rivo/uniseg) — Unicode segmentation for text layout
- [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) — image optimization
- [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) — HTML utilities

No CGo. No JavaScript toolchain. Compiles anywhere `GOOS/GOARCH` can reach, including WASM.

## Built On

GoSX is built on [gotreesitter](https://github.com/odvcencio/gotreesitter), a clean-room reimplementation of the tree-sitter runtime in pure Go. gotreesitter enables in-process grammar composition — GoSX extends Go's own grammar with native markup syntax, which is how `.gsx` files are parsed without code generation or external build tools.

The same compiler infrastructure powers [Arbiter](https://github.com/odvcencio/arbiter) (a governed outcomes language), [Danmuji](https://github.com/odvcencio/danmuji) (a BDD testing language for Go), and [Ferrous Wheel](https://github.com/odvcencio/ferrous-wheel) (Rust-inspired syntax for Go).

## License

MIT
