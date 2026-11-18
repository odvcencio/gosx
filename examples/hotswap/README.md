# hotswap

Edit an island and watch it change in the browser without a reload. The
counter's signal state survives each edit, because `gosx dev` recompiles only
the island you changed and ships the new program over the dev socket.

## Run it

For standalone use, install TinyGo and run from the repository root:

```sh
go run ./cmd/gosx build-runtime build
go run ./examples/hotswap
```

Build the runtime first when `gosx build` or `gosx export` starts the example for prerendering. For hot reload, run:

```sh
gosx dev ./examples/hotswap
```

Open the printed URL, click the counter up a few times, then edit
`counter.gsx` and save. Try changing the label, the step size, or a class.

## What to read

- `counter.gsx` is the island, a strict `component` with `//gosx:island`.
- `main.go` compiles it with the same Compile, LowerIsland, EncodeJSON steps
  that `gosx dev` runs on each edit.

## Docs

[Islands](https://gosx.m31labs.dev/docs/islands) and the `gosx dev` section of
the [README](../../README.md).
