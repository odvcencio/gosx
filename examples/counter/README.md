# counter

A standalone island: a counter compiled from `counter.gsx` to a small program
for GoSX's shared browser VM. The server renders the first count (`?count=5`)
and a `<noscript>` pair of links keeps the counter usable without JavaScript.
`counter.css` is the sidecar stylesheet.

This example only serves the server-rendered page. It does not serve the
browser runtime or the island program, so the island does not hydrate under
`go run`. [hotswap](../hotswap) is the same island run under `gosx dev`, which
serves both.

## Run it

```sh
go run ./examples/counter
```

Open <http://localhost:8080>.

## What to read

- `counter.gsx` is the island, written as a strict `component` with a typed
  props struct.
- `main.go` compiles it with the same Compile, LowerIsland, EncodeJSON steps as
  `gosx dev`, and renders the island with `island.Renderer`.

## Docs

[Islands](https://gosx.m31labs.dev/docs/islands) and
[Signals](https://gosx.m31labs.dev/docs/signals).
