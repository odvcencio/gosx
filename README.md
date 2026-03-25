# GoSX

A Go-native server-first web platform. Author components in Go with JSX-like syntax, render on the server by default, hydrate interactive islands with WebAssembly.

## Status

GoSX is in active development. The compiler pipeline, server rendering, and island architecture are implemented and tested. Browser-side hydration is structurally complete but not yet validated in a live browser environment.

**What works today (tested):**

- `.gsx` file parsing with JSX-like syntax (elements, fragments, attributes, expressions, spreads)
- `//gosx:island` directive detection on components
- Compiler pipeline: parse → flat-array IR → validate → lower to IslandProgram → serialize
- Body analyzer: compiler extracts signals, computeds, and handlers from `.gsx` source (proven by TestCompilerE2E_CounterFromSource)
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

**What exists but is not yet browser-validated:**

- JS bootstrap with event delegation and program loading (343 lines)
- JS patch applier with focus/cursor preservation (594 lines)
- WASM entry point exporting `__gosx_hydrate`, `__gosx_action`, `__gosx_dispose`
- Dev server with file watching and SSE hot-reload notifications
- Build orchestrator (`gosx build --dev/--prod`)
- Sidecar CSS support (component.gsx + component.css → hashed output)

## Quick Start

```go
package main

import (
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
| `island` | Island renderer and manifest generation |
| `hub` | WebSocket presence, fanout, shared realtime state |
| `engine` | Worker/surface model with capability declarations |
| `hydrate` | Hydration manifest types |
| `highlight` | Syntax highlighting for Go source code |
| `format` | Source code formatter for .gsx files |
| `dev` | Development server with file watching |
| `cmd/gosx` | CLI tool (compile, check, render, fmt, build, dev) |

## Dependencies

One: [gotreesitter](https://github.com/odvcencio/gotreesitter) — a clean-room reimplementation of tree-sitter in Go.

## Testing

```bash
go test ./...
```

14 packages, 287 tests. The end-to-end pipeline test at `test/gsx_pipeline_test.go` proves the full flow from `.gsx` source through to island hydration.

```bash
# Build the WASM runtime (standard Go)
GOOS=js GOARCH=wasm go build -o build/gosx-runtime.wasm ./client/wasm/

# Build with TinyGo for smaller output (1.2MB, ~452KB gz)
tinygo build -o build/gosx-runtime.wasm -target wasm ./client/wasm/

# Run the build pipeline
go run ./cmd/gosx build --dev examples/counter/
```

## License

MIT
