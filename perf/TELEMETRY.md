# GoSX performance telemetry

`gosx perf --json` keeps CPU submission, browser animation cadence, and GPU
work as separate evidence sources:

- `scene.frameStats` and `scene.cpuRenderSubmit` are durations of the
  `scene3d-render` performance measure. They describe main-thread renderer
  planning/build/submission work. The legacy `scene_p50`, `scene_p95`, and
  `scene_p99` budget names continue to resolve to this source.
- `scene.presentation` contains visible-tab `requestAnimationFrame` timestamp
  intervals observed after Scene3D starts. This is display-opportunity cadence,
  not proof that the compositor presented a frame. `estimatedMissedVsyncs` and
  `hitchClusters` are estimates based on the observed cadence; they are never
  inferred from CPU submission duration.
- `scene.gpu` is present only when a renderer publishes actual GPU timestamp
  query values through stable `data-gosx-scene3d-*-gpu-ms` or
  `data-gosx-scene3d-*-gpu-pass-*-ms` attributes. No CPU value substitutes for
  an unavailable GPU query.

Timing series contain `stats`, `cold`, and `warm` distributions. The cold
window is the first 30 observed rAF intervals after the first Scene3D render;
subsequent samples are warm. A zero-count phase means that phase was not
observed.

Each `scene.mounts` entry records the selected backend, renderer, fallback,
quality profile, configured/device/effective DPR, canvas backing-store target,
CSS dimensions, and the complete latest stable Scene3D attribute snapshot.
`scene.geometry`, `scene.pipeline`, and `scene.counters` expose numeric retained
geometry, upload/allocation/retirement, shader/pipeline, cache/warmup, pass,
draw, and dispatch diagnostics when publishers provide them. The generic
attribute collector also captures new renderer and water counters without a
profiler release.

Unavailable sources are explained under `scene.unavailable`. Optional budget
metrics use a `?` suffix:

```text
presented_p95? <= 33
gpu_total_p95? <= 20
gpu_pass_shadow_p95? <= 4
```

An unavailable optional metric is explicitly marked `skipped` and passes the
budget. The same assertion without `?` remains strict and fails when missing.
Useful aliases include `cpu_submit_p95`, `presented_p50|p95|p99`,
`presented_cold_p95`, `presented_warm_p95`, `missed_vsyncs`,
`hitch_intervals`, `hitch_clusters`, `gpu_total_p50|p95|p99`,
`gpu_total_warm_p95`, and `gpu_pass_<name>_p50|p95|p99|max`.

The report schema is [`report.schema.json`](./report.schema.json).
