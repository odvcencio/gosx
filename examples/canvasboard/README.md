# canvasboard

A 2D editing surface (100 rectangles you can pan, zoom and pick) built on
GoSX's `CanvasBoard` primitive. The server renders a `<canvas>` placeholder with
`data-gosx-surface-kind="canvas2d"`; the browser runtime hydrates it.

This example serves the server-rendered page only. The full hydration path needs
the runtime WASM served beside the page (see `engine/surface.RuntimeHandler`).

## Run it

```sh
go run ./examples/canvasboard
go test ./examples/canvasboard   # checks the rendered surface contract
```

Open <http://localhost:8080> and inspect the `<canvas>` element.

## What to read

- `main.go` builds the page with `gosx.CanvasBoard`.
- `board.gsx` shows the same surface in `.gsx` authoring form.
- `e2e_test.go` checks the server-rendered contract (the `canvas2d` flag and the 100 rectangles).

## Docs

[Engines](https://gosx.m31labs.dev/docs/engines).
