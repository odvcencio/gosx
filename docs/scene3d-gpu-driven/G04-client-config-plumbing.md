# G04 — Client plumbing: scene state → render bundle; instance-stream stamp (repo: gosx)

Depends on: G02 (defines the wire field). Independent of G03.

## Goal

1. `createSceneState` normalizes `scene.gpuDriven` (or a top-level
   `gpuDriven` prop) into `state.gpuDriven`: `{ occlusion, shadowCulling }` or
   `null`.
2. `mount.ts` copies it onto every frame's render bundle, on both render
   paths, as `bundle.gpuDriven`.
3. `instance-stream.ts` stamps `entry._instanceStreamRevision` when it
   rewrites a batch's transforms in place, because the GPU-driven host detects
   changed transforms by array identity plus this revision (the stream keeps
   one buffer, so identity alone cannot see the change).

## Facts this task relies on (verified at the baseline)

- `createSceneState` is declared `any` in
  `client/runtime/scene3d/bootstrap-bridge.d.ts`, so `sceneState.gpuDriven` in
  `mount.ts` adds no noImplicitAny diagnostic. `10-runtime-scene-core.ts` is
  not type-checked at all.
- `mount.ts` has exactly 3 lines of headroom under its ceiling (3859) and its
  `renderFrame` symbol is governed (`cyclomatic 62`). So the bundle copy is
  appended to two existing one-line statements, and it uses no `||`: adding
  `|| null` would raise `renderFrame`'s cyclomatic count. `state.gpuDriven` is
  already `null` when absent.
- Objects created inside the test harness VM carry that VM's
  `Object.prototype`, so `assert.deepEqual` against a literal fails even when
  the fields match. The test round-trips through JSON first.

## Step 1 — `client/js/bootstrap-src/10-runtime-scene-core.ts`

1a. The normalizer. Anchor (exactly once):

```js
  function sceneCamera(props) {
    const raw = props && props.camera && typeof props.camera === "object" ? props.camera : {};
```

Insert directly BEFORE it (this file uses `const`; keep that style here):

```js
  // sceneGPUDrivenMode normalizes the optional GPU-driven instancing mode:
  // scene.gpuDriven (lowered from Go Props.GPUDriven) or a directly authored
  // top-level gpuDriven prop. It returns null when the scene does not opt in.
  // Only the WebGPU renderer reads it (client/runtime/scene3d/indirect-instancing.ts).
  function sceneGPUDrivenMode(props) {
    const scene = sceneProps(props);
    const raw = scene && sceneIsPlainObject(scene.gpuDriven)
      ? scene.gpuDriven
      : (props && sceneIsPlainObject(props.gpuDriven) ? props.gpuDriven : null);
    if (!raw) {
      return null;
    }
    return {
      occlusion: sceneBool(raw.occlusion, false),
      shadowCulling: sceneBool(raw.shadowCulling, true),
    };
  }

```

1b. The state field. Anchor (exactly once, in `createSceneState`):

```js
      postFXMaxPixels: postFXMaxPixels,
      _deferredPostEffects:
```

Replace with:

```js
      postFXMaxPixels: postFXMaxPixels,
      gpuDriven: sceneGPUDrivenMode(props),
      _deferredPostEffects:
```

## Step 2 — `client/runtime/scene3d/mount.ts` (0 net lines)

2a. Runtime (wasm) path. Anchor (exactly once; one long line):

```js
          effectiveBundle.cameraProximity = sceneCameraProximityValue(sceneState._scrollCamera); effectiveBundle.waterShaderSourcesByID = mountedWaterShaderSources;
```

Replace that line with (same line, one statement appended):

```js
          effectiveBundle.cameraProximity = sceneCameraProximityValue(sceneState._scrollCamera); effectiveBundle.waterShaderSourcesByID = mountedWaterShaderSources; effectiveBundle.gpuDriven = sceneState.gpuDriven;
```

2b. JS path. Anchor (exactly once):

```js
      latestBundle.cameraProximity = sceneCameraProximityValue(sceneState._scrollCamera); latestBundle.waterShaderSourcesByID = mountedWaterShaderSources;
```

Replace with:

```js
      latestBundle.cameraProximity = sceneCameraProximityValue(sceneState._scrollCamera); latestBundle.waterShaderSourcesByID = mountedWaterShaderSources; latestBundle.gpuDriven = sceneState.gpuDriven;
```

`wc -l client/runtime/scene3d/mount.ts` must still print 3856.

## Step 3 — `client/runtime/scene3d/instance-stream.ts`

Anchor (exactly once, in `applyInstanceStreamFrame`):

```js
    revisions.set(frame.batchId, frame.revision);
```

Insert directly after it:

```js
    // The buffer above keeps its identity across frames, so stamp the entry:
    // indirect-instancing.ts re-uploads a batch's instance records when this changes.
    entry._instanceStreamRevision = frame.revision;
```

The render bundle's per-frame mesh copy (`Object.assign({}, sceneMesh, …)`)
carries the stamp to the renderer.

## Step 4 — create `client/js/scene3d-gpu-driven-plumbing.test.js`

Exact content:

```js
"use strict";

// GPU-driven instancing plumbing (spec task G04): the scene state carries the
// normalized gpuDriven mode, and the instance stream stamps a revision on the
// batch it rewrites in place, so the GPU-driven host can detect the change
// without hashing every transform.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const { createBoardWebGPUHarness } = require("./runtime-test-harness.js");

test("gpu-driven plumbing: createSceneState normalizes the gpuDriven mode", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  // JSON round trip: the state is built inside the harness VM, whose objects
  // carry that realm's Object.prototype and never deepStrictEqual a literal.
  const mode = (props) => JSON.parse(JSON.stringify(api.createSceneState(props, { tier: "full" }).gpuDriven));

  assert.equal(mode({ scene: {} }), null, "absent mode is null");
  assert.equal(mode({ scene: { gpuDriven: true } }), null, "a non-object mode is ignored");
  assert.deepEqual(mode({ scene: { gpuDriven: {} } }), { occlusion: false, shadowCulling: true });
  assert.deepEqual(mode({ scene: { gpuDriven: { occlusion: true, shadowCulling: false } } }), { occlusion: true, shadowCulling: false });
  assert.deepEqual(mode({ scene: { gpuDriven: { occlusion: "true", shadowCulling: "false" } } }), { occlusion: true, shadowCulling: false });
  assert.deepEqual(mode({ gpuDriven: { occlusion: true } }), { occlusion: true, shadowCulling: true }, "a top-level prop is read too");
  assert.deepEqual(
    mode({ scene: { gpuDriven: { occlusion: false } }, gpuDriven: { occlusion: true } }),
    { occlusion: false, shadowCulling: true },
    "scene.gpuDriven wins over the top-level prop",
  );
});

test("gpu-driven plumbing: mount.ts copies the mode onto both bundle paths", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "mount.ts"), "utf8");
  assert.match(source, /effectiveBundle\.gpuDriven = sceneState\.gpuDriven;/);
  assert.match(source, /latestBundle\.gpuDriven = sceneState\.gpuDriven;/);
});

function loadInstanceStreamApply() {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "instance-stream.ts"), "utf8");
  const window = {};
  new Function("window", "document", "TextDecoder", source + "\nreturn window;")(window, {}, TextDecoder);
  return window.__gosx_scene3d_instance_stream_apply;
}

// instanceStreamFrame hand-encodes one kind-0 (transform) frame in the wire
// layout scene.InstanceStreamFrame.Encode writes: a 24-byte header, the batch
// id padded to 4 bytes, then count*16 little-endian float32 values.
function instanceStreamFrame(batchId, revision, count) {
  const id = Buffer.from(batchId, "utf8");
  const idPadded = Math.ceil(id.length / 4) * 4;
  const bytes = new Uint8Array(24 + idPadded + count * 64);
  const view = new DataView(bytes.buffer);
  bytes.set([0x47, 0x53, 0x58, 0x49], 0);
  view.setUint8(4, 1);
  view.setUint8(5, 0);
  view.setUint32(8, revision, true);
  view.setUint32(16, count, true);
  view.setUint16(20, id.length, true);
  bytes.set(id, 24);
  for (let i = 0; i < count * 16; i += 1) {
    view.setFloat32(24 + idPadded + i * 4, revision + i, true);
  }
  return bytes;
}

test("gpu-driven plumbing: the instance stream stamps _instanceStreamRevision", () => {
  const apply = loadInstanceStreamApply();
  const entry = { id: "crowd", count: 2 };
  const sceneState = { instancedMeshes: [entry] };
  const mount = { dispatchEvent() {} };

  assert.deepEqual(apply(sceneState, instanceStreamFrame("crowd", 5, 2), () => {}, mount), { applied: true, revision: 5 });
  assert.equal(entry._instanceStreamRevision, 5);
  const buffer = entry.transforms;

  assert.deepEqual(apply(sceneState, instanceStreamFrame("crowd", 7, 2), () => {}, mount), { applied: true, revision: 7 });
  assert.equal(entry._instanceStreamRevision, 7);
  assert.equal(entry.transforms, buffer, "the stream rewrites the same buffer, so identity alone cannot detect the change");

  assert.equal(apply(sceneState, instanceStreamFrame("crowd", 6, 2), () => {}, mount).applied, false);
  assert.equal(entry._instanceStreamRevision, 7, "a stale frame leaves the stamp alone");
});
```

## Step 5 — rebuild bundles, then run the governance procedure

```sh
make build-bootstrap
node --test client/js/scene3d-gpu-driven-plumbing.test.js     # 3 pass
node --test client/js/scene3d-instance-stream.test.mjs client/js/scene3d-instance-stream-bridge.test.mjs
(cd client/runtime && npm run typecheck)                      # ratchet OK, 4874
(cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...)
node --test client/js/bootstrap-size.test.mjs
```

The size test is expected to FAIL here at the baseline order (G01 then G04):

```
Scene3D Chromium route (WebGPU, with labels).gz gzip size 351407 exceeds hard limit 351384
```

Measured after G01 + G04: route `1_305_322 / 351_407 / 296_071`
(raw / gzip / brotli). Apply G10 §B: raise that route's `gzip` target
`335_000 → 335_100` with a ledger comment, in its own commit. Brotli (296_071)
is still under its hard limit (296_100) and does not move.

## Verify

Every command in Step 5 passes after the budget commit, and
`git diff --check` prints nothing.

## Commits

1. `add(scene3d): carry the gpu-driven mode to the render bundle`
   - normalize scene.gpuDriven into scene state and copy it onto both bundle paths
   - stamp _instanceStreamRevision so in-place stream updates are detectable
2. `build(client): rebuild scene3d client bundles`
3. `test(scene3d): raise size budgets for gpu-driven instancing`
