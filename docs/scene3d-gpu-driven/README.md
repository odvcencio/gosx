# Scene3D GPU-Driven Instancing — Executable Spec

Status: spec only. Nothing in this directory is wired into the build.
Date: 2026-09-25
Repositories: `odvcencio/gosx` (this repo) and `odvcencio/elio` (kernel compiler)
Baseline commits: gosx `e6367b91ed3999d519b472ab97bd77ba84a8cc7c`, elio `8e5538caa30720ad8abe847047a9f1eeaeb8065f`

This spec brings a GPU-driven rendering pipeline to the Scene3D WebGPU
backend. It was prompted by Incandescent Games' video "I Built a Custom
GPU-Driven Rendering Pipeline (10x Faster)"
(<https://www.youtube.com/watch?v=LzivS_KzffY>). The video itself could not be
fetched from this environment. The pipeline below is the canonical design those
engines use (Haar and Aaltonen, *GPU-Driven Rendering Pipelines*, SIGGRAPH
2015; the two-phase occlusion scheme used by Nanite and niagara), fitted to
what Scene3D already has.

The spec is written for a low-cost executor model. Every task file stands
alone. It names exact files, exact anchor strings to search for, exact code for
every non-obvious piece, exact commands, and exact pass criteria. Run tasks in
the order of the index below. Do not skip the verification step of a task.

## What already exists (do not rebuild it)

- `client/runtime/scene3d/compute.ts` already has a per-mesh GPU frustum cull
  (`createSceneInstancedCullSystem`). One compute pass per instanced mesh
  compacts 80-byte records into a vertex buffer. The mesh is then drawn with
  `drawIndirect`. It is used for meshes with an authored `cullKernelWGSL`, or
  meshes with at least 256 instances and no per-instance colours. This spec
  leaves that system in place and does not change its WGSL. Native parity
  tests pin that WGSL.
- The WebGPU renderer has a render-bundle cache. It records indirect draws, so
  GPU-written instance counts never force a re-encode.
- `odvcencio/elio` compiles `.elio` compute kernels to WGSL, GLSL and MSL, and
  also runs them on a CPU interpreter. GoSX already embeds Elio-emitted WGSL for
  skinning (`SCENE_ELIO_SKIN_LBS_SOURCE` in `webgpu.ts`).

## What this spec adds

1. **One scene-wide cull dispatch per view.** All eligible opaque
   `InstancedMesh` entries share one instance buffer. One compute dispatch
   culls every instance for a view. Survivors are written as `u32` instance
   indices into per-(mesh, slot) regions of one visible-list buffer.
2. **Vertex pulling.** New PBR and shadow vertex-shader variants read the
   instance record from a storage buffer. The index comes from an
   instance-rate `uint32` vertex attribute bound at the region's byte offset.
   Per-instance colours now survive culling.
3. **Two-phase hierarchical-Z (Hi-Z) occlusion culling.** This step is
   optional and authored. The main pass splits into an early pass and a late
   pass. The early pass draws what was visible last frame. A Hi-Z pyramid is
   built from that depth, and a late cull draws only what just became
   visible. The result is exact (never culls a visible instance) and has no
   popping.
4. **Shadow-caster culling** against each shadow light's frustum.
5. **Kernels authored once in Elio.** They are emitted to WGSL for the browser
   and run on Elio's CPU interpreter for Go tests.
6. **A fix for a pre-existing leak** (task G01). Instanced meshes allocate a
   new material uniform buffer and bind group every frame, and render bundles
   never replay for instanced scenes. The GPU-driven CPU win depends on that
   replay.
7. Go authoring (`scene.Props.GPUDriven`), telemetry, a benchmark workload,
   and docs.

The feature changes no pixels. Culling is conservative, so WebGL, canvas and
headless backends ignore the new field. Per the repo rule in
`scene/capability/gpucull_test.go` (`TestRenderBundlesAreNotACapabilityCell`),
a performance-only feature gets `data-gosx-scene3d-webgpu-*` diagnostics, not
a capability Matrix row.

## What was proven before this spec was written

Every code block in the G and E task files was applied to a scratch checkout
of the baseline commits and tested there; nothing was committed to either
repository. Results:

| Check | Result |
|---|---|
| Elio `check` + `emit wgsl/glsl/metal` of both kernels | ok |
| WGSL compile on real WebGPU (Tint): cull, Hi-Z downsample, both seed variants, derived PBR and shadow vertex variants linked with the real `WGSL_PBR_FRAGMENT` / `WGSL_SHADOW_FRAGMENT` | 0 errors |
| Hi-Z pyramid vs CPU reference (320×240, 9 levels) | 0 mismatches |
| Hi-Z seed vs per-pixel depth ground truth (1× and 4× MSAA) | 0 mismatches |
| Occlusion conservativeness vs per-pixel ground truth (6 000 instances) | 0 violations; culls 93% of truly hidden instances |
| GPU survivors vs Elio CPU interpreter (frustum, occlusion warm/cold, light view, distance/contribution) | 0 mismatches |
| Elio side (E1–E4): 10 Go tests, full `go test ./...` | pass |
| G01 leak: buffers and bind groups per frame on the mount path | reproduced (+1 each per frame, bundle never replays); fixed |
| Go (G02–G03, G07): `go test ./scene/... ./render/bundle ./scene/capability ./examples/gosx-docs/...` | pass |
| JS, fake device (G04–G06): new tests | 13 pass |
| Full JS suite after G10 | 2046 tests, 2044 pass, 2 skipped, 0 fail |
| noImplicitAny ratchet after every task | unchanged: 4874 (webgpu.ts 1744, mount.ts 335) |
| Integrated renderer on real WebGPU (G09): same scene classic vs GPU-driven, frustum and occlusion, MSAA 1 and 4 | pixel-identical (0 differing bytes); 0 validation errors; occlusion cuts camera survivors 471 → 71 |

## Architecture in one page

```
Go authoring                  SceneIR wire                 Browser (WebGPU only)
scene.Props.GPUDriven  ──►  sceneIR.gpuDriven  ──►  sceneState.gpuDriven ──► bundle.gpuDriven
     (G02, G03)                                          (G04)                    │
                                                                                 ▼
 webgpu.ts render()   (G06)                    indirect-instancing.ts (G05, compute chunk)
  1. gpuDriven.beginFrame(...)      own eligible opaque meshes; upload changed records;
                                    reset indirect args; cull the camera (slot 0)
  2. per shadow light:
       gpuDriven.lightView(...)     cull casters (slot 2 + light), then the shadow pass
                                    draws them with drawIndirect
  3. gpuDriven.prepareMainPass(...) occlusion frames: move resolve target + end stamp
  4. main pass opaque: owned meshes draw the camera list with drawIndirect
  5. gpuDriven.splitMainPass(...)   occlusion frames: end pass → Hi-Z seed + downsample →
                                    late cull (slot 1) → load-pass → draw late lists
  6. everything else (alpha, additive, water, points, ...) unchanged
  7. gpuDriven.finishEncoding / endFrame: readback → data-gosx-scene3d-webgpu-gpu-driven-*
```

Slots per owned mesh: `0` camera early (or single, when occlusion is off),
`1` camera late, `2` shadow light 0, `3` shadow light 1.

An owned mesh is an entry of the renderer's opaque instanced draw list with a
string id, at least one instance, full transforms and no authored
`cullKernelWGSL`. Everything else keeps the classic path.

## Task index

Run the tasks in this order. "Repo" names where the change lands.

| Task | Repo | Summary | Depends on |
|---|---|---|---|
| [00-context](00-context.md) | — | Read first. Facts, contracts, landmines. | — |
| [01-preflight](01-preflight.md) | both | Toolchains, sibling checkout, baseline test run. | 00 |
| [E0](E0-elio-workspace-alignment.md) | elio | Pin gotreesitter v0.47.0 so Elio builds beside gosx. | 01 |
| [E1](E1-elio-kernel-sources.md) | elio | Add `stdlib/gpudriven/*.elio` + Go wrappers. | E0 |
| [E2](E2-elio-wgsl-goldens.md) | elio | Golden WGSL + all-backend emission test. | E1 |
| [E3](E3-elio-cull-conformance.md) | elio | CPU-interpreter conformance tests (cull + Hi-Z). | E1 |
| [G01](G01-fix-instanced-cache-ownership.md) | gosx | Fix the per-frame instanced buffer/bind-group leak. | 01 |
| [G02](G02-go-authoring-surface.md) | gosx | `scene.GPUDriven`, `SceneIR.GPUDriven`, diff policy. | 01 |
| [G03](G03-go-schema.md) | gosx | JSON schema + Go validator for `gpuDriven`. | G02 |
| [G04](G04-client-config-plumbing.md) | gosx | Scene state → render bundle; instance-stream revision stamp. | G02 |
| [G05](G05-module.md) | gosx | The host module `indirect-instancing.ts` (verbatim in appendix D), goldens, first tests. | E2, G04 |
| [G06](G06-webgpu-seam.md) | gosx | 19 edits to `webgpu.ts`: frustum, shadows, occlusion split, telemetry; renderer tests. | G01, G05 |
| [G07](G07-bench-workload.md) | gosx | Renderer Bench `gpu-driven` / `instanced-classic` workloads. | G02 |
| [G08](G08-docs-changelog.md) | gosx | Docs page section + changelog. | G06, G07 |
| [G09](G09-real-webgpu-probe.md) | — | Real-WebGPU acceptance probe (SwiftShader). Scratch only. | G06 |
| [G10](G10-governance-budgets.md) | gosx | Procedures: §A architecture ratchets, §B byte budgets; §C final full run. | called from G01, G04–G06; §C last |
| [E4](E4-elio-gosx-sync.md) | elio | Cross-repo byte-equality test of the WGSL goldens. | E2, G05 |
| [appendix-A](appendix-A-kernels.md) | — | Verbatim kernel sources, emitted WGSL, hand WGSL, derivations. | — |
| [appendix-B](appendix-B-layouts.md) | — | Byte layouts, buffer usages, bind group layouts, uniform packing maps. | — |
| [appendix-C](appendix-C-observations.md) | — | Pre-existing issues found while writing the spec (out of scope). | — |
| [appendix-D](appendix-D-indirect-instancing.md) | — | The complete host module, verbatim, with its SHA-256. | — |

## Executor rules (apply to every task)

1. Do one task at a time. Read `00-context.md` and the whole task file before
   editing anything.
2. When a task says "find this anchor", search for the exact string. If it is
   missing or appears more than once, stop and report. Line numbers in this
   spec are hints from the baseline commit; anchors are authoritative.
3. Never hand-edit generated files: `client/js/bootstrap*.js`, their `.gz`,
   `.br` and `.map` siblings, and `client/js/bootstrap-src/chunks.json`. Only
   `make build-bootstrap` writes them. Run it after every change to a runtime
   source, before running JS tests, and commit its output in its own commit.
4. Runtime sources under `client/runtime/scene3d/` and
   `client/js/bootstrap-src/` are `.ts` files written in **plain JavaScript
   syntax**. No type annotations, `interface`, `as`, `!` non-null, generics,
   `let`/`const`-only rewrites, or classes. Tests execute these files raw in
   `node:vm`, and one annotation breaks every test that loads the chunk.
5. `webgpu.ts` and `mount.ts` are under a noImplicitAny ratchet
   (`client/runtime/scene3d/noimplicitany-baseline.json`) that can only go
   down. In those two files:
   - Never add a function parameter without a default. A primitive default is
     fine (`name = ""`, `count = 0`, `flag = false`); TypeScript then infers a
     real type.
   - Never add a closure-scoped `var x = null;`. Initialise from `new Map()`,
     a number, a string, or a boolean instead.
   - Calling methods on an object obtained from `window.__gosx_scene3d_api` or
     `Map.prototype.get` is free, because those values are `any`.
6. No line of `webgpu.ts` may match `/wgpuCreateShadow.*[Cc]ull|[Cc]ull.*shadow/`.
   Test "gpu-cull T1" in `client/js/runtime-21-scene-gpu-cull-bundles.test.js`
   rejects it. Keep the words "cull" and lowercase "shadow" on different lines
   there.
7. Keep these literal strings in `webgpu.ts` (pinned by
   `scene/capability/gpucull_test.go`): `updateInstancedCullSystems(`,
   `pass.drawIndirect(cullSys.drawArgsBuf, 0)`, `GPUBufferUsage.INDIRECT`
   (in `compute.ts`), and `beginComputePass()` (in `compute.ts`).
8. Go: `gofmt -w` every changed `.go` file; `go vet` the changed packages.
9. Every task ends with its **Verify** block. All commands must pass, and
   `git diff --check` must print nothing. If a check fails, fix it inside the
   task. Never weaken or skip a test.
10. Commit style: `type(scope): lowercase imperative summary` with a `- ` bullet
    body. Examples: `add(scene3d): ...`, `fix(scene3d): ...`,
    `test(scene3d): ...`. Generated bundles always go in their own commit,
    `build(client): rebuild scene3d client bundles`. Budget and metric bumps go
    in their own commit, `test(scene3d): raise size budgets for gpu-driven
    instancing` or `update(testdata): update scene3d renderer architecture
    metrics`.
11. In the elio repo, run `go test ./...` in addition to the task's commands.

## Verification ladder

| Level | What | Where |
|---|---|---|
| L0 | Pure unit tests (Go, Node) | every task |
| L1 | Fake-device renderer harness (`client/js/runtime-test-harness.js`) | G04–G06 |
| L2 | Elio CPU interpreter conformance | E3 |
| L3 | Real WebGPU in headless Chromium + SwiftShader | G09 |
| L4 | Human check on a real GPU: `/demos/scene3d-bench?workload=gpu-driven` vs `instanced-classic` | G07 |

## Glossary

- **Owned mesh**: an `InstancedMesh` the GPU-driven host culls and draws this
  frame (see `sceneGPUDrivenMeshEligible` in G05).
- **Slot**: an index 0..3 selecting one (view, phase) survivor list and one
  indirect-args entry per owned mesh.
- **Early / late**: the two camera phases of two-phase occlusion culling.
- **Hi-Z / HZB**: a hierarchical-Z pyramid. Each texel of level `k` holds the
  maximum (farthest) depth of the 2×2 texels under it in level `k-1`. Level 0 is
  the depth target reduced 2×2.
- **Visibility**: one `u32` per instance. 1 means "drawn visible last frame".
