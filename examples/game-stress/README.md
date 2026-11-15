# Game stress example

A GoSX scene sized like a big 3D sports game, built to measure whether it fits
a 60 fps frame. It has:

- 22 skinned figures (13 joints, 132 triangles each) with Run and Idle clips,
  posed every frame from a simulation;
- a crowd of up to 40,000 skinned figures (60 triangles each) in eight stand
  sections, animated on the GPU from one motion frame;
- a synthetic, deterministic simulation (package `stress`): 22 agents at a
  fixed 30 Hz with steering, ray casts through package `physics`, a table of 24
  rules per agent, and prediction-style replays of 6 ticks every third frame,
  driven by `game.Runtime`;
- music: a Cicada kernel and score in an AudioWorklet, played into the GoSX
  mixer's music bus, or a silent kernel when no Cicada fixture is available.

Every figure and clip is generated in code (`web/figures.go`); the example
needs no art files. Nothing in it models a particular game.

## Run it

```sh
go run ./examples/game-stress            # builds the engine, serves http://127.0.0.1:8095/
```

Query parameters pick the configuration:

| Parameter | Values | Default |
|---|---|---|
| `backend` | `webgpu`, `webgl` | `webgpu` |
| `thread` | `main` (the engine rides the renderer's frame), `worker` (the simulation runs in a Web Worker) | `main` |
| `crowd`, `players`, `sections` | figure counts | `40000`, `22`, `8` |
| `replayEvery`, `replayTicks` | frames between replays, ticks per replay | `3`, `6` |
| `audio` | `off`, `worklet` (silent kernel), `cicada` | `off` |
| `width`, `height` | canvas size in CSS pixels | `1920`, `1080` |
| `cull` | `0` turns off GPU frustum culling of the crowd on WebGPU | on |
| `occlusion` | `1` adds two-phase Hi-Z occlusion culling to `cull` | off |
| `shadows` | `1` adds a shadow-casting sun | off |

`window.__stress.collect()` returns the per-frame measurements as JSON; see
[docs/game-performance.md](../../docs/game-performance.md) for what each one
means and for measured results.

## Live music

The `cicada` audio mode needs Cicada's browser fixture and kernel:

```sh
cd ../cicada
make build-kernel-wasm
CICADA_BROWSER_FIXTURE=/tmp/cicada-fixture go test -run TestWriteBrowserFixture ./sdk/gosxhost/
cp build/cicada-kernel.wasm /tmp/cicada-fixture/kernel.wasm
cd ../gosx
go run ./examples/game-stress -cicada /tmp/cicada-fixture   # then open /?audio=cicada and click
```

## Layout

| Path | What |
|---|---|
| `stress/` | The deterministic workload and its `game.Runtime` wrapper. Native tests check determinism, a golden hash, replays and zero allocations per tick. |
| `stage/` | The arena and crowd layout and the engine's props, shared by the server and the engine. |
| `web/` | The page, the generated figures, the per-frame instrumentation and the HTTP handler. |
| `engine/` | The Go WASM engine (`GOOS=js GOARCH=wasm`). |
