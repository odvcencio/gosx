# Game performance investment ledger

Updated 2026-09-05. Publish small validated batches on the feature branch;
review source and regression evidence before integration. Keep measurements
separate from hypotheses. Do not raise architecture or runtime budgets to pass.

## Buckets

| Bucket | Landed on this branch | Next measurable investment |
| --- | --- | --- |
| Asset visibility | b14cbae9: animation and morph inventory | Account for real animated asset costs |
| GPU submission | 6cc33121: rigid imported colour batching, WebGL and WebGPU | Batched shadows; animated crowd submission |
| CPU scene updates | Older-runtime prototype only; see below | Port retained hydration and stable pose lookup with atomic invalidation tests |
| Style and material caches | Older-runtime prototype only | Port numeric-depth CSS cache fix; profile material preparation |
| Frame pacing and allocation | Initial stress observations | Repeated combat/FX soaks, allocation and worst-frame budgets |
| Measurement | Rigid crowd fixture and recorded observations | Reproducible cross-device and equivalent-engine benchmarks |

## Validated branch batch: rigid imported crowds

A 256-actor, two-primitive indexed GLB fixture plus a floor, shadows disabled,
was sampled for 60 frames in Windows Edge 152 on NVIDIA Blackwell hardware.
Individual WebGL models issued 513 colour draws per frame; batched WebGL and
WebGPU issued 3. Both batched paths created zero GPU buffers during sampling.
This establishes submission behaviour, not a game frame-rate improvement.
Use `go run ./examples/rigid-glb-crowd` to run the fixture.

Prior validation for the two source commits: 1,793 JavaScript tests passed,
including strict TypeScript, generated artifacts and architecture/ABI budgets;
`go test ./assetpipe ./scene/...` and focused asset CLI checks passed. The broad
CLI suite timed out during a TinyGo build and is not counted as passing.

Rigid batching does not include skins, animated morphs/node transforms,
mirrored transforms, authored shader materials or every transparency path.
Shadow submission remains per actor. These are explicit remaining costs.

## Measured prototype: older game runtime, not integrated here

Three changes retain unchanged rigid mesh records across hydration, replace
per-frame structural serialization with validated ID lookups, and keep numeric
render depth from invalidating CSS resolution. They preserve failed/superseded
hydration atomicity and structural/material invalidation.

| Actors | Original frame p50 / p95 | Candidate frame p50 / p95 |
| --- | --- | --- |
| 128 | 14.3 / 18.7 ms | 9.8 / 12.9 ms |
| 512 | 45.1 / 101.5 ms | 28.8 / 47.1 ms |

Windows Edge 152, 1280x800, actual game models, unchanged visual settings;
6 seconds warm-up and 180 sampled rAF intervals with CPU profiling enabled.
Actors orbit synthetically; combat effects and drops are removed by the probe.
This is rendering stress, not complete gameplay or four-player simulation.
The 512-actor candidate repeat was uncontended; equivalent repeated baselines,
GPU timing and hardware/thermal controls remain outstanding. No claim of
sustained 60 FPS or superiority to other engines is supported by these data.

100 selected renderer checks passed across a full selected run and a targeted
rerun after restoring a missing builder fixture. Five new regression checks
cover retention, failure/supersession, pose-key avoidance and CSS invalidation.
The candidate also passed the mouse/controller/pause browser probe. This is
not a full framework-suite result. The source patch applies to the older game
framework snapshot, but does not apply to this branch: port and revalidate it
before merging. Never transplant generated bundles across framework bases.

## Batch acceptance

Each new batch records its bucket, commit, baseline, candidate, workload,
hardware, sample count, visual settings, correctness checks and limitations.
Use Buckley for scoped commits after inspecting staged changes; push the
feature branch after successful validation. Failed or neutral experiments
remain labelled as such. Do not schedule background jobs for this workflow.

Next priority: port the CPU scene-update prototype, then profile per-primitive
transform/bounds/material preparation and shadow submission. Add representative
combat load before enlarging arenas or declaring a frame-time budget met.
