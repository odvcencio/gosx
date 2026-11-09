# Game performance: fit a GoSX game in a 60 fps frame

A 60 fps frame lasts 16.7 ms, and on the page's main thread that time is shared
by your Go simulation, the Scene3D renderer's JavaScript and the garbage
collector. This page shows how to measure each part, gives a budget, and
reports what a large stress scene costs on two GPUs.

The short version, from the measurements below:

- The Go side fits. At full speed on both test machines, simulation, AI and
  animation stay under 1.4, 1.0 and 0.3 ms at the 99th percentile (budgets
  3, 2 and 2 ms), and the Go collector did no work in any 30-second run.
- Render submission is the part that does not fit with a 40,000-figure crowd.
  Against a 4 ms budget it takes 2.7 to 3.4 ms (p50) with WebGL2 at full
  speed, 7.7 to 10.7 ms with WebGPU, and 9.9 to 49.6 ms on either backend at
  a 4x CPU slowdown. Three per-frame scans that grow with the scene cause most
  of it (see "Where the render time goes").
- At a 4x slowdown, our stand-in for an M1 or Iris Xe laptop, replay frames
  push the simulation and AI over their budgets. Running the simulation in a
  Web Worker takes them off the main thread: 2.5 to 9 ms less main-thread time
  at the 95th percentile (35 ms in one run where the main thread fell behind),
  for -2 to +10 ms of median input latency at full speed.

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
# On a machine with a GPU (the GoSX runners: gpu-chrome.sh run -- ..., mac-gpu-run <worktree> ...)
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
| `GOSX_CICADA_FIXTURE` | A directory with Cicada's `fixture.json` and `kernel.wasm`, for live music |

`TestGameStressSmoke` is the small version that CI runs: 800 crowd figures,
both threads, loose budgets, and a check that the simulation reaches the same
state hash in a worker, on the main thread and in a native Go build.

## Measured: the stress scene

The scene is `examples/game-stress` at the jam design's counts: 22 skinned
figures, 40,000 crowd figures in 8 batches, a 1920x1080 canvas at device pixel
ratio 1, live Cicada music in an AudioWorklet, and the synthetic workload
(22 agents at 30 Hz, a 6-tick replay every third frame). Each run measured 30
seconds after an 8-second warm-up, on 9 November 2026, in headless Chrome 157.
Times are milliseconds per frame, p50 / p95 / p99. The budget lines (sim, AI,
animation, render) are main-thread time; in worker runs the simulation and AI
run on the worker and appear in their own column (p50 / p99 of sim plus AI).
Input latency is the time from a key press to the end of the frame that first
draws its effect, over 10 presses, so treat it as a rough figure.

**NVIDIA RTX 5070 Ti** (Windows host, Chrome reached from WSL):

| Backend, thread | fps | Main thread | Sim (3) | AI (2) | Animation (2) | Render CPU (4) | Worker sim + AI | GPU | Input latency |
|---|---|---|---|---|---|---|---|---|---|
| WebGPU, main | 59.9 | 13.1 / 14.7 / 15.4 | 0.2 / 1.1 / 1.2 | 0.1 / 0.7 / 0.8 | 0.1 / 0.2 / 0.3 | 10.7 / 11.6 / 12.1 | – | 2.2 / 2.5 / 2.6 | 49 / 50 / 50 |
| WebGPU, worker | 59.8 | 11.7 / 12.6 / 13.8 | – | – | 0.0 / 0.1 / 0.1 | 9.9 / 10.6 / 11.0 | 0.2 / 2.0 | 2.4 / 3.8 / 3.8 | 48 / 64 / 64 |
| WebGL2, main | 59.9 | 5.5 / 7.0 / 7.4 | 0.1 / 1.1 / 1.2 | 0.1 / 0.7 / 0.8 | 0.1 / 0.2 / 0.3 | 3.2 / 3.6 / 3.9 | – | 3.3 / 6.2 / 6.7 | 33 / 49 / 49 |
| WebGL2, worker | 59.9 | 5.1 / 5.8 / 6.3 | – | – | 0.0 / 0.1 / 0.1 | 3.4 / 3.9 / 4.1 | 0.2 / 2.0 | 3.4 / 5.9 / 6.2 | 39 / 67 / 67 |
| WebGPU, main, 4x | 13.9 | 65.0 / 74.8 / 80.4 | 2.0 / 7.0 / 8.1 | 1.2 / 4.4 / 4.9 | 0.7 / 1.1 / 1.8 | 49.6 / 55.2 / 57.8 | – | 3.9 / 4.3 / 4.4 | 134 / 229 / 229 |
| WebGPU, worker, 4x | 15.3 | 60.0 / 65.6 / 73.3 | – | – | 0.2 / 0.3 / 0.4 | 49.7 / 54.2 / 57.9 | 0.3 / 2.1 | 3.9 / 4.3 / 4.3 | 123 / 147 / 147 |
| WebGL2, main, 4x | 24.5 | 30.9 / 66.2 / 90.3 | 1.0 / 6.3 / 10.4 | 0.6 / 4.2 / 6.7 | 0.7 / 1.7 / 2.6 | 16.7 / 35.4 / 55.2 | – | 6.0 / 10.0 / 10.2 | 63 / 88 / 88 |
| WebGL2, worker, 4x | 31.8 | 27.8 / 31.6 / 34.8 | – | – | 0.2 / 0.3 / 0.3 | 18.7 / 21.4 / 23.5 | 0.2 / 2.0 | 5.9 / 9.7 / 9.8 | 88 / 98 / 98 |

**Apple M4 Pro** (macOS, Metal). The WebGPU runs turn off GPU crowd culling,
because with it the Metal device is lost (see "Known problems"); the GPU time
column is empty where the browser offered no timer.

| Backend, thread | fps | Main thread | Sim (3) | AI (2) | Animation (2) | Render CPU (4) | Worker sim + AI | GPU | Input latency |
|---|---|---|---|---|---|---|---|---|---|
| WebGPU, main | 60.0 | 9.0 / 10.2 / 10.9 | 0.1 / 0.8 / 1.0 | 0.1 / 0.6 / 0.7 | 0.0 / 0.1 / 0.2 | 7.7 / 8.5 / 8.9 | – | – | 33 / 50 / 50 |
| WebGPU, worker | 59.9 | 9.6 / 10.4 / 10.7 | – | – | 0.0 / 0.1 / 0.1 | 8.5 / 9.1 / 9.4 | 0.2 / 1.7 | – | 43 / 53 / 53 |
| WebGL2, main | 60.0 | 4.1 / 5.8 / 6.1 | 0.2 / 1.3 / 1.4 | 0.1 / 0.8 / 1.0 | 0.1 / 0.2 / 0.2 | 2.7 / 2.9 / 3.0 | – | 9.1 / 11.1 / 11.1 | 35 / 50 / 50 |
| WebGL2, worker | 59.9 | 4.1 / 4.5 / 4.7 | – | – | 0.0 / 0.1 / 0.1 | 2.9 / 3.2 / 3.3 | 0.3 / 2.4 | 8.7 / 10.6 / 10.7 | 33 / 59 / 59 |
| WebGPU, main, 4x | 22.0 | 42.8 / 48.7 / 51.2 | 0.9 / 4.0 / 4.9 | 0.7 / 2.9 / 3.6 | 0.1 / 0.9 / 1.2 | 35.1 / 38.8 / 40.6 | – | – | 90 / 93 / 93 |
| WebGPU, worker, 4x | 24.6 | 39.1 / 41.1 / 42.9 | – | – | 0.0 / 0.1 / 0.8 | 33.9 / 35.6 / 37.1 | 0.2 / 1.7 | – | 84 / 123 / 123 |
| WebGL2, main, 4x | 58.0 | 14.9 / 18.9 / 20.1 | 0.4 / 3.4 / 4.0 | 0.1 / 2.4 / 3.0 | 0.1 / 0.9 / 1.0 | 9.9 / 10.9 / 11.5 | – | 8.3 / 8.8 / 8.8 | 48 / 50 / 50 |
| WebGL2, worker, 4x | 59.0 | 14.8 / 16.4 / 17.6 | – | – | 0.0 / 0.1 / 0.7 | 11.0 / 12.0 / 12.5 | 0.2 / 1.7 | 8.2 / 9.5 / 10.8 | 67 / 71 / 71 |

What the numbers do not include, and how to read them:

- **Garbage collection.** The Go collector ran no cycle in any measured
  window: the engine allocated 0.11 to 0.17 KiB per frame. The V8 collector,
  from a CPU profile of the NVIDIA runs, took 0.03 to 0.06 ms per frame.
- **The 4x runs** use `Emulation.setCPUThrottlingRate(4)`, which slows only
  the page's main thread: a worker's simulation times were the same as at full
  speed. We did not run on an M1 or Iris Xe laptop. The NVIDIA host's 4x runs
  are slower than the Mac's because that host shared its CPU with other
  builds.
- **Replays follow display frames** in this workload (every third frame), so a
  run that drops to 14 fps also replays less: the main-thread simulation time
  in the slow 4x runs understates what a 20 Hz server would cost.
- **Animation** here is clip choice and pose output for 22 figures plus the
  renderer's model-animation pass. An animation graph with IK adds its own
  evaluation, which this scene does not measure.
- **The GPU** is not the limit on these machines (2 to 11 ms). A laptop GPU
  with less throughput than the M4 Pro's may be: we did not measure one.
- Every run reached the same simulation state hash at tick 300 as the native
  amd64 and arm64 builds (`0xde7cc29b81708428`), on both threads, and no
  replay changed the state.

### Where the render time goes

A CPU profile of the main thread (NVIDIA, full speed, 10 seconds,
`GOSX_STRESS_PROFILE=1` with source maps from a `GOSX_BUNDLE_DEBUG=1` build)
puts most of the render submission time in three scans that run every frame
and grow with the scene:

| Function | Where | WebGPU ms per frame | WebGL2 ms per frame |
|---|---|---|---|
| `ingestManifestText`, called through `updateWaterSystems` and `sceneWaterManifestShaderSourceStats` | `client/runtime/scene3d/webgpu.ts` | 6.2 | – |
| `sceneCrowdBatchPartition` | `client/js/bootstrap-src/10-runtime-scene-core.ts` | 2.9 | 3.6 |
| `sceneFocusCandidates`, called through `syncSceneNodeFocusProxies` | `client/runtime/scene3d/mount-scene-focus.ts` | 1.1 | 1.6 |
| The Go engine's whole frame (step, replays, poses) | `stress.wasm` | 0.8 | 1.0 |

1. On a page with no water, WebGPU reads the whole engine manifest's text
   every frame looking for water shaders, because it caches only a non-empty
   result. The crowd's declaration makes that text several megabytes.
2. The crowd batcher visits every agent each frame to check whether its slot
   changed, even when none did.
3. The focus proxies scan every scene object each frame for interactive nodes.

Skipping the first when there is no water, and limiting the third to the
render bundle's records, was tried in a scratch build (not in GoSX): on the
NVIDIA host, WebGPU's main-thread time went from 13.1 / 14.7 / 15.4 to
7.0 / 8.7 / 9.1 ms at full speed, and from 65.0 / 74.8 / 80.4 ms (13.9 fps) to
27.7 / 36.7 / 47.6 ms (29.8 fps) at 4x; with the simulation in a worker,
24.6 / 29.3 / 32.8 ms (35.3 fps) at 4x.

### Known problems

- With `scene.GPUDriven` crowd culling, WebGPU on the Apple M4 Pro loses its
  device about 1.4 s after the first frame ("Metal command buffer failed:
  Insufficient Memory", `kIOGPUCommandBufferCallbackErrorOutOfMemory`), at
  10,000 and at 40,000 crowd figures, and the mount falls back to WebGL. The
  same scene without `GPUDriven` runs at 60 fps. The RTX 5070 Ti is not
  affected.
- Declaring 40,000 crowd figures as `InstancedGLBMesh` instances puts several
  megabytes of JSON in the page manifest, which the page parses at load.

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
interpolation. Measured over the runs above (10 presses
each), the worker path changed the median input latency by -2 to +10 ms and
the 95th percentile by +3 to +18 ms at full speed, and the median by -11 to
+25 ms at a 4x slowdown. It cut main-thread time at the 95th percentile by
2.5 to 9.2 ms under a 4x slowdown (34.6 ms in the NVIDIA WebGL2 run, where the
main-thread engine fell behind and caught up in bursts). At full speed, where
the Go frame is under 2 ms, the saving is within run-to-run noise.
