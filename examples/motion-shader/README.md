# motion-shader

A shader driven by `motion` timelines. The effect is written in Selena
(`tide.sel`), built to `tide.sel.json` ahead of time, and played by GoSX. The
app links no shader compiler and writes no JavaScript.

## Run it

```sh
PORT=8083 go run ./examples/motion-shader
go test ./examples/motion-shader
```

Open <http://127.0.0.1:8083>. After you edit `tide.sel`, rebuild the artifact:

```sh
gosx selena build examples/motion-shader
```

## What to read

- `tide.sel` is the shader; `tide.sel.json` is the checked-in build output.
- `main.go` decodes the artifact and binds it to a motion timeline.

## Docs

[Motion](https://gosx.m31labs.dev/docs/motion) and
[docs/scene3d-selena.md](../../docs/scene3d-selena.md).
