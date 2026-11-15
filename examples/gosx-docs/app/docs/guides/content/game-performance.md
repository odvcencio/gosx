# Game performance: fit a GoSX game in a 60 fps frame

A 60 fps frame lasts 16.7 ms, and on the page's main thread that time is shared
by your Go simulation, the Scene3D renderer's JavaScript and the garbage
collector. This page shows how to measure each part, gives a budget, and
summarizes what a large stress scene cost on two GPUs.

## The frame budget

A game that runs its simulation on the main thread splits each frame like
this. The audio synthesis runs on the AudioWorklet thread and is not in it.

| Part | Budget per frame | What it covers |
|---|---|---|
| Simulation | 3 ms | Fixed ticks, including reconciliation replays |
| AI and rules | 2 ms | Decisions for every agent, in every tick that runs |
| Animation | 2 ms | Clip choice, graphs and IK, pose output, the renderer's model animation |
| Render submission (CPU) | 4 ms | Building the frame's render bundle and encoding the GPU commands |
| Garbage collection | 1 ms | Go GC work, on average |
| Headroom | 4.7 ms | Browser work, input, layout, spikes |

Compare budgets with the 95th and 99th percentile of each part, not the mean.
A frame that misses once a second is visible as a stutter.

## Profile your game

### 1. Turn on the renderer's own timings

Set `window.__gosx_scene3d_perf = true` before the GoSX bootstrap runs. The
renderer then adds three `performance.measure` entries to every frame:

| Measure | What it times |
|---|---|
| `scene3d-bundle` | Building the frame's render bundle (the planner walks the scene) |
| `scene3d-model-animations` | Advancing declared model animations |
| `scene3d-render` | The backend's `render()` call: CPU encode and submit; the GPU works later |

Read them with a `PerformanceObserver`, or with
`performance.getEntriesByType("measure")` once per frame, and clear them with
`performance.clearMeasures()`.

### 2. Time the whole main-thread frame

Wrap `requestAnimationFrame` before the bootstrap runs and add up the duration
of every callback that shares a frame timestamp. The Scene3D frame hook runs a
main-thread Go engine inside the renderer's callback (see
[engine-frame-loop.md](engine-frame-loop.md)), so this total includes your Go
frame. With a worker engine, also time the page's `Worker` message handlers:
the frames a worker posts are applied there.

`examples/game-stress/web/harness.go` is a complete harness that does both and
keeps one record per frame.

### 3. Time your Go frame

Measure inside the engine with `performance.now()` (through `syscall/js`):
start the clock when the frame source fires and stop it after your render
callback. To time the start of each frame, wrap the frame source:

```go
type timedSource struct {
	loop.FrameSource
	start float64
}

func (t *timedSource) RequestFrame(cb func(float64)) int {
	return t.FrameSource.RequestFrame(func(ts float64) { t.start = now(); cb(ts) })
}

// Keep the frame owner's alpha: forward it when the inner source takes it.
func (t *timedSource) SetFrameAlpha(alpha float64) {
	if sink, ok := t.FrameSource.(host.FrameAlphaSink); ok {
		sink.SetFrameAlpha(alpha)
	}
}
```

Split the simulation from the AI by timing inside your systems. Count
replayed ticks separately: a client that reconciles runs several ticks in one
frame, and those frames set your 99th percentile.

### 4. Measure the Go garbage collector

Go WebAssembly has one thread, so the collector's marking runs on the same
thread as your game. Read `runtime/metrics` once per frame and keep the
deltas:

| Metric | Use |
|---|---|
| `/cpu/classes/gc/total:cpu-seconds` | GC CPU time (an estimate) |
| `/sched/pauses/total/gc:seconds` | Stop-the-world pauses (a histogram; sum count times bucket midpoint) |
| `/gc/heap/allocs:bytes` | Bytes allocated: the cause of GC work |

A game whose steady-state frame allocates nothing does no GC work in the frame.
`game.Runtime.Step` allocates nothing in steady state; keep your systems the
same way and check it with `testing.AllocsPerRun`.

### 5. Read the GPU time

The renderer measures GPU time where the browser allows it: WebGPU with the
`timestamp-query` feature, WebGL2 with `EXT_disjoint_timer_query_webgl2`. The
mount's debug snapshot carries it as `frameTiming.gpuMS`
(`window.__gosx_scene3d_debug_registry`, `record.snapshot("summary")`). Sample
it every second or two outside a frame: building the snapshot costs time.

### 6. Run the stress benchmark

[`examples/game-stress`](../examples/game-stress) is a scene the size of a
big 3D sports game, and `e2e/game_stress_e2e_test.go` runs it in Chrome and
writes one JSON file per run with every frame's record:

```sh
# On a machine with a GPU-capable Chrome (set GOSX_E2E_CHROME to it)
GOSX_STRESS_BENCH=1 GOSX_STRESS_OUT=/tmp/stress \
  go test -tags e2e -timeout 40m -run TestGameStressBenchmark -v ./e2e
```

| Variable | Effect |
|---|---|
| `GOSX_STRESS_RUNS` | Runs to make, such as `webgpu/main,webgl/worker,webgpu/main@4` (`@4` is a 4x CPU slowdown through `Emulation.setCPUThrottlingRate`) |
| `GOSX_STRESS_SECONDS` | Measured seconds per run (30) |
| `GOSX_STRESS_CROWD` | Crowd size (40000) |
| `GOSX_STRESS_OUT` | Directory for the JSON and a screenshot of each run |
| `GOSX_STRESS_PROFILE=1` | Also write a main-thread CPU profile (`.cpuprofile`, opens in Chrome DevTools) |
| `GOSX_STRESS_CULL=0` | Turn off GPU crowd culling (`scene.GPUDriven`) on the WebGPU runs |
| `GOSX_STRESS_CLIENT_ROOT` | A directory whose `client/js` holds the browser runtime to serve, for example another build's bundles to compare |
| `GOSX_CICADA_FIXTURE` | A directory with Cicada's `fixture.json` and `kernel.wasm`, for live music |

`TestGameStressSmoke` is the small version that CI runs: 300 crowd figures,
both threads, loose budgets, and a check that the simulation reaches the same
state hash in a worker, on the main thread and in a native Go build.

## What to expect

We measured a stress scene with 22 skinned figures, a 40,000-figure crowd in 8
batches, a 1920x1080 canvas and live music, on an NVIDIA RTX 5070 Ti and an
Apple M4 Pro in Chrome 157. We did not measure an M1 or Iris Xe laptop. The
numbers below are the 95th percentile of main-thread time per frame.

- **The Go side fits.** At full speed the simulation, AI and animation stayed
  within their budgets (3, 2 and 2 ms) on both machines, and the Go garbage
  collector ran no cycle in the 30-second window.
- **Render submission was the cost that grew with the scene.** Before
  GoSX v0.60.0 skipped three per-frame scans (the water manifest text, the crowd
  batch partition and the focus proxies) when nothing they read had changed, a
  40,000-figure crowd spent 9 to 13 ms per frame on render submission on
  WebGPU. After, it spends 1 to 2 ms at full speed.
- **At a 4x CPU slowdown** (`Emulation.setCPUThrottlingRate(4)`, which slows
  only the page's main thread) the same scene held 55 to 60 fps on both
  machines with the fixes in place, against 14 to 51 fps before.
- **A worker** (see below) keeps simulation spikes off the main thread. It cut
  the main-thread p95 by 2.5 to 9.2 ms in the slow runs, at a cost of up to 10 ms
  of input latency.
- **GPU crowd culling** (`scene.GPUDriven`) works on Apple Silicon.
- **Determinism.** Every run reached the same simulation state hash as the
  native amd64 and arm64 builds, on both threads.

Measure your own game with the steps above; the budgets are a starting point,
not a promise for your scene.


## Run the simulation in a Web Worker

When the simulation and AI do not fit the main thread, move them to a worker
engine (`Thread: engine.ThreadWorker`, see [engine-worker.md](engine-worker.md);
it needs no `SharedArrayBuffer` and no cross-origin isolation). A worker cannot
write poses inside the renderer's frame, so it sends one **timed pose frame**
per tick, and the page interpolates between them:

```go
// In the worker, after each tick:
step := rt.FixedStep()
at := time.Duration(tick) * step // the simulation time these poses hold
err := scene.DispatchTimedPoseFrame("arena", frame, at, step)
```

The page keeps the newest frames for the mount and installs the mount's frame
hook. Before each frame it draws, it applies the poses interpolated at

```text
renderAt = frame time - offset - delay
```

`offset` maps simulation time onto the page clock: it is the smallest
`arrival - at` over the last 60 frames, so one late message does not move it.
`delay` is one tick plus 2 ms (`scene.DispatchTimedPoseFrameWith` and
`TimedPoseOptions.Delay` change it). Position and scale blend linearly, each
rotation angle takes the shortest arc, and the clip time advances linearly
while the clip stays the same. Send ever-growing clip times with
`AnimationLoop` set; the renderer wraps them. A frame stamped at or before the
newest one (after a rollback) replaces the frames from that time on.

Rules:

1. Keep each batch's members and their order the same from frame to frame:
   instances pair up by position.
2. Do not also drive the same mount with `host.SceneFrameSource`: timed frames
   take the mount's frame hook.
3. The call works on the main thread too, which helps a game that runs either
   way (`ctx.Thread()` tells you which).

`handle.__gosxTimedPoseStats` on the mount's handle counts frames, applied
poses, holds (frames drawn with no newer pose to blend toward) and rewinds,
and reports the current offset and delay.

**Determinism.** The worker runs the same Go code as the main thread, so the
simulation keeps its guarantees: the stress benchmark checks that both reach
the state hash a native amd64 and arm64 build reaches at tick 300. Inputs
reach the worker from the page (see "Input" in engine-worker.md), and the
simulation samples them per tick as it does on the main thread.

**Latency.** The page draws one tick behind the newest frame it has, so the
worker path adds about one tick of delay on top of the main-thread path's
interpolation. In the stress runs (10 presses
each), the worker path changed the median input latency by -2 to +10 ms and
the 95th percentile by +3 to +18 ms at full speed, and the median by -11 to
+25 ms at a 4x slowdown. It cut main-thread time at the 95th percentile by
2.5 to 9.2 ms under a 4x slowdown (34.6 ms in one run where the main-thread engine fell behind and caught up in bursts). At full speed, where
the Go frame is under 2 ms, the saving is within run-to-run noise.
