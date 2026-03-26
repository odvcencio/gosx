# GoSX

A Go-native server-first web platform. Author components in Go with JSX-like syntax, render on the server by default, hydrate interactive islands with WebAssembly.

## Status

GoSX is in active development. The compiler pipeline, server rendering, island architecture, and browser runtime are implemented and exercised through unit, integration, and live-browser checks.

**What works today (tested):**

- `gosx init` scaffolds a runnable app with `/public` assets, `.env` loading, metadata hooks, session-backed form actions, CSRF protection, JSON API routes, custom 404/500 pages, and a file-backed `app/layout.gsx`
- `gosx init --template docs` scaffolds a dogfooded docs site with nested file layouts, scoped docs 404s, page-scoped server modules, sessions, auth, redirects/rewrites, public assets, and colocated JSON endpoints
- `gosx build --prod` emits a deployable `dist/` bundle with hashed assets, a server binary when present, copied `app/` + `public/`, and a `run.sh` launcher
- `gosx dev` fronts a runnable app with a stable dev proxy, staged `/gosx/*` runtime assets, file watching, and SSE reload notifications
- `gosx export` prerenders static file-routed pages into `dist/static`, carries over `/public`, stages `/gosx/*` runtime assets, and writes `dist/export.json`
- opt-in client-side page navigation via `app.EnableNavigation()` plus `server.Link(...)`, with managed head swaps and intent-prefetching
- file-based routing via `route.Router.AddDir(...)`, including `layout.gsx`, `page.gsx`, `index.gsx`, `not-found.gsx`, `error.gsx`, route groups like `(marketing)`, and `[slug]` segment conventions
- file-route server modules via sibling `page.server.go` files and `route.MustRegisterFileModuleHere(...)`, with per-page `Load`, `Metadata`, `Render`, and relative `__actions/<name>` endpoints
- file-routed `.gsx` pages can now render local page-scoped components plus built-in `If`, `Each`, `Link`, and `Image` helpers through the file renderer, with spread-based props for richer loader-fed page composition
- file-routed pages and layouts can now own sibling `page.css` / `layout.css` sidecars, which are injected into the document head automatically during routed rendering
- file-routed pages and layouts can now own sibling `page.meta.json` / `layout.meta.json` sidecars for static title, description, canonical, meta, and link data without manual server-module wiring
- session-backed browser form flows via `action` + `session`, including flashed validation state, redirect-safe success messages, and built-in CSRF protection
- auth middleware via `auth.New(...)`, `authn.Middleware`, and `authn.Require`, with request-scoped `user` context available to routed `.gsx` pages
- declarative redirects and rewrites via `app.Redirect(...)` and `app.Rewrite(...)`
- HTTP caching and revalidation via `ctx.Cache(...)`, automatic ETags, and `app.RevalidatePath(...)` / `app.RevalidateTag(...)`
- app testing helpers via `apptest.Request(...)`, `apptest.App(...)`, `apptest.Router(...)`, and `apptest.FormRequest(...)`
- deferred page regions via `ctx.Defer(...)`, with fallback-first HTML and streamed replacements
- local image optimization via `server.Image(...)` and `/_gosx/image` for responsive raster variants
- `.gsx` file parsing with JSX-like syntax (elements, fragments, hyphenated attributes, expressions, spreads, custom-element tags)
- `//gosx:island` directive detection on components
- Compiler pipeline: parse → flat-array IR → validate → lower to IslandProgram → serialize
- Body analyzer: compiler extracts signals, computeds, and handlers from `.gsx` source (proven by TestCompilerE2E_CounterFromSource)
- Zero-manual-wiring islands: `.gsx` island → `LowerIsland` → `RenderIslandFromProgram` → auto `EventSlot`s and server-rendered `data-gosx-on-*` attributes
- Server-side HTML rendering via the `Node` API
- Signal system with `Signal[T]`, `Computed[T]`, `Effect`, and `Batch`
- Expression VM evaluating typed opcodes (40+ operations)
- Tree reconciler with static subtree skipping and patch op generation
- Island programs in JSON (dev, inspectable) and binary (prod, compact — ~14% of JSON size)
- WASM bridge managing island lifecycle (hydrate, dispatch, dispose)
- WASM runtime compiles to 1.2MB with TinyGo (~452KB gzipped first load)
- Hub primitive: WebSocket presence, fanout, shared state
- Engine primitive: worker/surface model with capability declarations
- Cross-island shared state via `$`-prefixed signals
- `.gsx` editor support via `gosx lsp` plus a bundled VS Code extension scaffold in `editor/vscode`

**What still needs deeper framework passes:**

- a unified hybrid SSR + prerender story in the main build pipeline
- a fully automatic nested `page.server.go` discovery story beyond scaffolded side-effect import buckets
- `.gsx`-first engine surfaces for advanced runtimes like 3D
- a grammar pass that removes the current multi-expression-attribute brittleness in `.gsx` tags instead of relying on spread-based workarounds in file-routed pages
- deeper styling and asset ownership ergonomics

## Quick Start

```bash
gosx init my-app
cd my-app
go run .
```

Or scaffold the dogfooded docs surface:

```bash
gosx init my-docs --template docs
cd my-docs
go run .
```

The generated project includes:

- root-level static assets from `public/`
- `.env`, `.env.local`, and `.env.<mode>` loading via `env.LoadDir`
- file-routed `.gsx` pages under `app/` by default, with automatic nested `layout.gsx` discovery, scoped `not-found.gsx` resolution, and `app.API(...)` for colocated JSON endpoints
- a blank import of `your/module/modules` so sibling and nested `page.server.go` module registrations execute at startup
- sibling `page.server.go` examples that register file-route `Load`, `Metadata`, and `Actions` hooks through `route.MustRegisterFileModuleHere(...)`
- session middleware plus CSRF protection for browser forms
- HTTP caching helpers plus automatic ETags for page and API responses
- opt-in page transitions via `app.EnableNavigation()` and `server.Link(...)`
- `server.Metadata` plus arbitrary head node injection
- customizable 404 and 500 pages
- streamed fallback regions via `ctx.Defer(...)` in page or route handlers

The docs template additionally includes:

- `route.Router.AddDir("./app", ...)` plus automatic `app/layout.gsx` / `app/docs/layout.gsx` composition for file-based page discovery and shell rendering
- `app.Redirect(...)` and `app.Rewrite(...)` examples wired through `server.App`
- a mounted docs router, `/public`-served stylesheet, `/api/meta`, and guarded `/api/me`
- session-backed forms, auth actions, and a protected route under `/labs/secret`
- automatic ETags on `/api/meta` plus static docs-page cache examples that can be invalidated by path or tag
- a sample raster asset plus image optimization examples under `/docs/images`

```go
package main

import (
    "net/http"

    "github.com/odvcencio/gosx"
    "github.com/odvcencio/gosx/server"
)

func main() {
    app := server.New()
    app.Route("/", func(r *http.Request) gosx.Node {
        return gosx.El("h1", gosx.Text("Hello from GoSX"))
    })
    app.ListenAndServe(":8080")
}
```

## .gsx Syntax

GoSX extends Go with JSX-like markup in `.gsx` files:

```go
package app

func Greeting(props GreetingProps) Node {
    return <div class="greeting">
        <h1>Hello, {props.Name}!</h1>
        <p>Welcome to GoSX.</p>
    </div>
}

//gosx:island
func Counter(props CounterProps) Node {
    return <div class="counter">
        <button onClick={decrement}>-</button>
        <span>{count}</span>
        <button onClick={increment}>+</button>
    </div>
}
```

The `//gosx:island` directive marks a component for client-side hydration. Island components are compiled to IslandPrograms — compact, VM-oriented representations with typed expression opcodes. Server components render to static HTML with zero client-side JavaScript.

The fully automatic path is now:

```go
irProg, _ := gosx.Compile(source)
islandProg, _ := ir.LowerIsland(irProg, 0)

renderer := island.NewRenderer("main")
renderer.SetProgramDir("/gosx/islands")
node := renderer.RenderIslandFromProgram(islandProg, nil)
```

That emits server HTML with delegated event attributes such as `data-gosx-on-click="increment"` and auto-populates manifest `EventSlot`s and `ProgramRef`.

## Architecture

```
.gsx source
  → parse (gotreesitter + Go grammar extension)
  → lower to compiler IR (flat-array, index-based)
  → validate (including island subset enforcement)
  → body analyzer extracts signals, computeds, and handlers from Go source
  → server components: transpile to Go
  → island components: lower to IslandProgram → serialize (JSON dev / binary prod)
  → shared WASM runtime (loaded once, browser-cached)
  → per-island programs (~1-10KB each)
  → JS host: thin patch applier + event delegation (~940 lines total)
```

### Island Expression Subset

Island expressions are constrained to what the client-side VM can evaluate:

- Literals (string, int, float, bool)
- Property and signal access
- Arithmetic, comparisons, boolean logic
- String concatenation
- Conditionals
- Handler dispatch
- List iteration

Goroutines, channels, and arbitrary Go are compile-time errors in islands.

### Cross-Island Shared State

Signals with names starting with `$` are shared across all islands on the page:

```
$count   — shared counter, any island can read/write
$theme   — shared theme, all islands react to changes
$user    — shared auth state
count    — local to the declaring island
```

When one island mutates a `$`-signal, all other islands that reference it automatically re-render. No Redux, no context providers, no boilerplate. The bridge manages the shared store and subscription lifecycle — disposed islands are automatically unsubscribed.

**Init order:** The first island to declare a `$`-signal sets its type and initial value. Subsequent islands receive the existing signal. This means hydration order matters for shared state initialization — document shared signals explicitly in your manifest or ensure a consistent load order.

### Styling Model

Classes and external CSS are the primary styling path. GoSX does not include CSS-in-JS.

- `class="..."` for all layout, colors, spacing
- External `.css` files linked in page `<head>`
- Sidecar CSS: `component.gsx` + `component.css` pairs are detected and bundled by the build pipeline
- Inline `style=` only for truly dynamic values (computed dimensions, transforms)

### Deploy Strategy

GoSX supports a three-tier deploy strategy:

1. **Static** — pre-rendered HTML, no server needed
2. **Server** — Go binary serving routes, SSR, actions, hubs
3. **Edge** — WASM islands hydrate at the edge, server handles actions/hubs

## Packages

| Package | Purpose |
|---------|---------|
| `gosx` | Core Node API, grammar, parser, compiler |
| `ir` | Intermediate representation, lowering, validation, island lowering, expression parser |
| `island/program` | IslandProgram types, JSON/binary serialization |
| `signal` | Reactive state: Signal[T], Computed[T], Effect, Batch |
| `client/vm` | Expression VM, reconciler, patch ops |
| `client/bridge` | WASM bridge for island lifecycle |
| `client/wasm` | WASM entry point (compiles with GOOS=js GOARCH=wasm) |
| `client/js` | Bootstrap (343 lines) + patch applier (594 lines) |
| `render` | Server-side HTML rendering from IR |
| `server` | Simple HTTP server with routing |
| `route` | Declarative routing with layouts and data loaders |
| `action` | Named server action handlers |
| `session` | Signed cookie sessions, flash state, and CSRF protection |
| `auth` | Session-backed auth middleware and guards |
| `apptest` | Route/app testing helpers for pages, APIs, and form posts |
| `island` | Island renderer and manifest generation |
| `hub` | WebSocket presence, fanout, shared realtime state |
| `engine` | Worker/surface model with capability declarations |
| `hydrate` | Hydration manifest types |
| `highlight` | Syntax highlighting for Go source code |
| `format` | Source code formatter for .gsx files |
| `dev` | Development server with file watching |
| `cmd/gosx` | CLI tool (compile, check, render, fmt, build, dev, export) |

## Dependencies

One: [gotreesitter](https://github.com/odvcencio/gotreesitter) — a clean-room reimplementation of tree-sitter in Go.

## Testing and Tooling

The repo now exposes a repeatable local/CI surface through `make`:

```bash
# Full package test pass
make test

# Data-race pass across the repo
make test-race

# Run shipped bootstrap.js and patch.js against a minimal Node DOM harness
make test-js

# Run js/wasm tests against the shipped client/wasm entrypoint
make test-wasm

# CI-grade verification: format check, tests, race tests, JS contract tests, WASM tests, CLI build, WASM build
make ci
```

Key checks:

- `make test` runs `go test ./...` across the compiler, runtime, routing, server, actions, hubs, and end-to-end pipeline tests.
- `make test-race` runs the same suite with the Go race detector enabled.
- `make test-js` runs the shipped `client/js/bootstrap.js` and `client/js/patch.js` files under Node's built-in test runner with a minimal DOM harness, covering hydration orchestration, delegated events, disposal, and patch application.
- `make test-wasm` runs `GOOS=js GOARCH=wasm go test ./client/wasm` so client correctness is exercised through the actual exported WASM runtime functions.
- `make build-cli` ensures `cmd/gosx` continues to compile.
- `make build-runtime` builds the shared WASM runtime from `client/wasm`.
- `.github/workflows/ci.yml` runs the same contract on every push and pull request.
- `npm run test:e2e` launches the real `gosx dev ./examples/gosx-docs` path behind Playwright and verifies client navigation, scoped 404s, forms, auth redirects, and the protected route.

Editor tooling:

- `gosx lsp` starts a stdio language server for `.gsx` diagnostics and formatting.
- `editor/vscode` contains a VS Code extension scaffold that wires `.gsx` syntax highlighting to `gosx lsp`.

Action-specific hardening is covered by regression tests for:

- JSON bodies with `application/json; charset=utf-8`
- invalid JSON requests
- oversized JSON requests
- oversized form submissions
- path fallback when router `PathValue` support is not present

Client correctness is covered at three layers:

- pure Go VM and bridge tests in `client/vm` and `client/bridge`
- shipped JS runtime contract tests in `client/js/runtime.test.js`
- end-to-end compiler-to-bridge tests in `test/frontend_pipeline_test.go`
- js/wasm runtime tests in `client/wasm/main_test.go` that compile `.gsx` islands, hydrate through `__gosx_hydrate`, dispatch through `__gosx_action`, and assert the emitted patch stream
- live-browser verification in `e2e/gosx_docs_e2e.test.mjs`, which boots `gosx dev`, drives Chromium through the docs app, and checks the real browser/runtime contract

Additional manual commands:

```bash
# Build the WASM runtime (standard Go)
GOOS=js GOARCH=wasm go build -o build/gosx-runtime.wasm ./client/wasm/

# Build with TinyGo for smaller output (1.2MB, ~452KB gz)
tinygo build -o build/gosx-runtime.wasm -target wasm ./client/wasm/

# Run the build pipeline
go run ./cmd/gosx build --dev examples/counter/

# Run the dev proxy against the dogfooded docs app
go run ./cmd/gosx dev ./examples/gosx-docs

# Pre-render static output for a file-routed app
go run ./cmd/gosx export ./examples/gosx-docs

# Run the browser E2E harness
npm run test:e2e
```

Build output and deployment:

- `gosx build --prod my-app` writes `dist/build.json`, `dist/assets/`, `dist/app/`, `dist/public/`, and when the target is runnable, `dist/server/app` plus `dist/run.sh`
- `gosx export my-app` writes `dist/static/` plus `dist/export.json` for pre-rendered file-routed pages and copied `/public` assets
- file-routed apps stay deployable because the runtime bundle now carries `app/` alongside the binary instead of assuming source-tree access
- `gosx dev`, `gosx build`, and `gosx export` resolve the shared runtime from the app's Go module graph, so scaffolded apps work outside the GoSX repo instead of assuming repo-local `client/` sources
- scaffolded apps and the docs template resolve their runtime root through `server.ResolveAppRoot(thisFile)`, so they can run from source, from `dist/`, or with `GOSX_APP_ROOT` set explicitly
- `dist/README.md` describes the bundle contract and launch model

## License

MIT
