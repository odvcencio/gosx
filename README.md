# GoSX

A Go-native server-first web platform. Author components in Go with JSX-like syntax, render on the server by default, hydrate interactive islands with WebAssembly.

## Status

GoSX is in active development. The compiler pipeline, server rendering, and island architecture are implemented and tested. Browser-side hydration is structurally complete but not yet validated in a live browser environment.

**What works today (tested):**

- `.gsx` file parsing with JSX-like syntax (elements, fragments, attributes, expressions, spreads)
- `//gosx:island` directive detection on components
- Compiler pipeline: parse → flat-array IR → validate → lower to IslandProgram → serialize
- Server-side HTML rendering via the `Node` API
- Signal system with `Signal[T]`, `Computed[T]`, `Effect`, and `Batch`
- Expression VM evaluating typed opcodes (30 operations)
- Tree reconciler with static subtree skipping and patch op generation
- Island programs in JSON (dev, inspectable) and binary (prod, compact — ~14% of JSON size)
- WASM bridge managing island lifecycle (hydrate, dispatch, dispose)
- Shared WASM runtime compiles to 3.3MB (~800KB gzipped)
- Full end-to-end test: `.gsx` source → parse → IR → island → serialize → hydrate → dispatch → patches

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

### Styling Model

Classes and external CSS are the primary styling path. GoSX does not include CSS-in-JS.

- `class="..."` for all layout, colors, spacing
- External `.css` files linked in page `<head>`
- Sidecar CSS: `component.gsx` + `component.css` pairs are detected and bundled by the build pipeline
- Inline `style=` only for truly dynamic values (computed dimensions, transforms)

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
| `hydrate` | Hydration manifest types |
| `format` | Source code formatter for .gsx files |
| `dev` | Development server with file watching |
| `cmd/gosx` | CLI tool (compile, check, render, fmt, build, dev) |

## Dependencies

One: [gotreesitter](https://github.com/odvcencio/gotreesitter) — a clean-room reimplementation of tree-sitter in Go.

## Testing

```bash
go test ./...
```

13 packages, 220+ tests. The end-to-end pipeline test at `test/gsx_pipeline_test.go` proves the full flow from `.gsx` source through to island hydration.

```bash
# Build the WASM runtime
GOOS=js GOARCH=wasm go build -o build/gosx-runtime.wasm ./client/wasm/

# Run the build pipeline
go run ./cmd/gosx build --dev examples/counter/
```

## License

MIT
