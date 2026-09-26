# G05 — The module: `client/runtime/scene3d/indirect-instancing.ts` (repo: gosx)

Depends on: E2 (the WGSL goldens exist in Elio), G04.

## Goal

Add the complete GPU-driven host module to the compute chunk (and to the
monolith `bootstrap.js`, which carries `compute.ts` inline), plus the Elio
WGSL goldens it embeds, plus the first tests. Nothing calls the host yet; G06
wires it into the renderer.

The whole file is in appendix D, verbatim, with its SHA-256. It is one task on
purpose: the host is a single closure, and copying a verified file is safer
than assembling it in pieces.

## Step 1 — copy the Elio goldens into gosx testdata

```sh
mkdir -p client/js/testdata/elio
cp ../elio/stdlib/gpudriven/cull.wgsl           client/js/testdata/elio/gpudriven_cull.wgsl
cp ../elio/stdlib/gpudriven/hiz_downsample.wgsl client/js/testdata/elio/gpudriven_hiz_downsample.wgsl
sha256sum client/js/testdata/elio/*.wgsl
# 86de419e4baf2067b5c3e26067e34abf1e4657a27d8565fdab379c2aa57ad485  client/js/testdata/elio/gpudriven_cull.wgsl
# d35c9e084df9c15897058429ffdfea3a85fd74a6bf3c48243accd99391cb8428  client/js/testdata/elio/gpudriven_hiz_downsample.wgsl
```

If Elio is not checked out, create the two files from appendix A3 and A4
(each file ends with exactly one newline) and check the same hashes.

## Step 2 — create the module

Create `client/runtime/scene3d/indirect-instancing.ts` with the exact content
of the fenced block in appendix D. Then:

```sh
sha256sum client/runtime/scene3d/indirect-instancing.ts
# e90ab552fac1f0485cee90c11f54574fa1a99f449e3f15f02d7a569aa52e3cc0
cp client/runtime/scene3d/indirect-instancing.ts /tmp/ii-check.js && node --check /tmp/ii-check.js
```

Rules the file already follows (keep them if you ever edit it):

- Plain JavaScript syntax (00 §6; tests run the raw source in `node:vm`).
- Top-level names all start with `SCENE_GPU_DRIVEN_`, `sceneGPUDriven` or
  `createSceneGPUDrivenHost`, because the monolith puts every source in one
  scope.
- It calls `sceneShaderModuleError` and `sceneReportPipelineFailure` from
  `compute.ts` lexically (same chunk scope, guarded by `typeof`).
- Do NOT add the file to `client/runtime/tsconfig.scene3d.json`. A new file in
  that list joins the noImplicitAny ratchet with every untyped parameter
  counted against it (`REGRESSION: ... is new to the noImplicitAny ratchet`).
  `instance-stream.ts` is outside that list for the same reason.
- The name must not contain `gpu-` or the word `gpu` (appendix D explains the
  renderer naming contract that rejects it).

## Step 3 — `cmd/buildbootstrap/main.go`: register it in two chunks

The anchor `sourceFile("../runtime/scene3d/compute.ts"),` appears exactly
twice: once in `bootstrap.js`, once in `bootstrap-feature-scene3d-compute.js`.
After EACH occurrence insert (same indentation):

```go
			sourceFile("../runtime/scene3d/indirect-instancing.ts"),
```

Result:

```go
			sourceFile("../runtime/scene3d/compute.ts"),
			sourceFile("../runtime/scene3d/indirect-instancing.ts"),
```

`cmd/buildbootstrap/host_authority_test.go`
(`TestEveryRuntimeTypeScriptAuthorityIsInTheBuildGraph`) fails for any
`client/runtime/**/*.ts` that no bundle lists, so this step is required.

## Step 4 — rebuild so `chunks.json` lists the file

```sh
make build-bootstrap
grep -c indirect-instancing client/js/bootstrap-src/chunks.json   # 2
```

The fresh test harness reads `chunks.json`; without this step the new file is
invisible to tests (00 §6).

## Step 5 — create `client/js/scene3d-gpu-driven.test.js` (part A)

Exact content. G06 appends parts B and C to this file.

```js
"use strict";

// GPU-driven instancing (docs/scene3d-gpu-driven): the embedded Elio
// kernels, the pure helpers in indirect-instancing.ts, and the renderer seam in
// the WebGPU renderer driven through the fake WebGPU device. Every renderer test builds
// a NEW bundle per frame, the way mount.ts does.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const { createBoardWebGPUHarness, flushAsyncWork } = require("./runtime-test-harness.js");

function golden(name) {
  return fs.readFileSync(path.join(__dirname, "testdata", "elio", name), "utf8").replace(/\n$/, "");
}

async function apiOnly() {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  return harness.env.context.__gosx_scene3d_api;
}

test("gpu-driven: embedded kernels equal the Elio goldens", async () => {
  const api = await apiOnly();
  assert.equal(api.SCENE_GPU_DRIVEN_CULL_WGSL, golden("gpudriven_cull.wgsl"));
  assert.equal(api.SCENE_GPU_DRIVEN_HIZ_DOWNSAMPLE_WGSL, golden("gpudriven_hiz_downsample.wgsl"));
  assert.match(api.SCENE_GPU_DRIVEN_HIZ_SEED_WGSL, /var gdDepth: texture_depth_2d;/);
  assert.match(api.SCENE_GPU_DRIVEN_HIZ_SEED_MS_WGSL, /var gdDepth: texture_depth_multisampled_2d;/);
});

test("gpu-driven: pure helpers match appendix B", async () => {
  const api = await apiOnly();
  const hiz = api.sceneGPUDrivenHiZLevels(320, 240);
  assert.equal(hiz.levels.length, 9);
  assert.equal(hiz.total, 25609);
  assert.deepEqual(JSON.parse(JSON.stringify(hiz.levels[4])), { offset: 25500, width: 10, height: 8 });
  assert.equal(api.sceneGPUDrivenHiZLevels(1, 1).levels.length, 1);
  assert.equal(api.sceneGPUDrivenMaxInstances({}), 1398101);
  assert.equal(api.sceneGPUDrivenMaxInstances({ maxStorageBufferBindingSize: 1 << 30 }), 4194240);
  assert.equal(api.sceneGPUDrivenConfig(null), null);
  assert.deepEqual(JSON.parse(JSON.stringify(api.sceneGPUDrivenConfig({}))), { occlusion: false, shadowCulling: true });
  assert.equal(api.sceneGPUDrivenMeshEligible({ id: "a", transforms: new Array(32) }, 2), true);
  assert.equal(api.sceneGPUDrivenMeshEligible({ id: "a", transforms: new Array(16) }, 2), false);
  assert.equal(api.sceneGPUDrivenMeshEligible({ id: "a", cullKernelWGSL: "fn k() {}", transforms: new Array(16) }, 1), false);
  assert.equal(api.sceneGPUDrivenMeshEligible({ transforms: new Array(16) }, 1), false);

  // Identity view-projection: planes are x = ±1, y = ±1, z = 0 and z = 1.
  const planes = api.sceneGPUDrivenFrustumPlanes([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], new Array(24), 0);
  assert.deepEqual(planes, [1, 0, 0, 1, -1, 0, 0, 1, 0, 1, 0, 1, 0, -1, 0, 1, 0, 0, 1, 0, 0, 0, -1, 1]);

  const buffer = new ArrayBuffer(512);
  const f32 = new Float32Array(buffer);
  const u32 = new Uint32Array(buffer);
  api.sceneGPUDrivenPackView(f32, u32, 0, {
    viewProj: [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], width: 320, height: 240,
    eye: { x: 1, y: 2, z: 3 }, total: 7, slot: 1, phase: 1, levels: hiz.levels,
  });
  assert.deepEqual(Array.from(f32.slice(40, 48)), [320, 240, Math.fround(1 / 320), Math.fround(1 / 240), 1, 2, 3, 0]);
  assert.deepEqual(Array.from(u32.slice(52, 56)), [7, 1, 1, 9]);
  assert.deepEqual(Array.from(u32.slice(56 + 8 * 4, 56 + 9 * 4)), [25608, 1, 1, 0]);
  assert.deepEqual(Array.from(u32.slice(56 + 9 * 4, 56 + 10 * 4)), [0, 0, 0, 0]);
});

```

What part A proves: the embedded kernels are byte-identical to the Elio
goldens (minus the final newline, see A8); the Hi-Z layout for 320×240 is 9
levels and 25 609 floats; the max-instance limit follows B15; eligibility
rejects meshes without an id, with an authored kernel, or with short
transforms; planes of the identity matrix are the unit cube's faces with
WebGPU's [0, 1] depth; and a packed view lands every field at the B6 word
index, including the zero fill past the last Hi-Z level.

## Verify

```sh
node --test client/js/scene3d-gpu-driven.test.js          # 2 pass
node --test client/js/runtime-21-scene-gpu-cull-bundles.test.js client/js/scene3d-renderer-architecture.test.js
(cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...)
(cd cmd/buildbootstrap && GOWORK=off go run -tags 'grammar_subset grammar_subset_typescript' . --check)
(cd client/runtime && npm run typecheck)                  # unchanged: 4874
node --test client/js/bootstrap-size.test.mjs
git diff --check
```

The size test fails on the compute chunk (all three metrics) and on
`bootstrap.js` gzip. That is the feature's cost: the compute chunk goes from
about 31 KB to about 61 KB raw (9.4 KB → 17.8 KB gzip), and about half of the
growth is the embedded WGSL. Apply G10 §B in a separate commit: re-baseline
the compute chunk to its measurement rounded up to the next 100 bytes, and
raise `bootstrap.js` gzip by the smallest 100-byte step that passes. Only
scenes with instanced meshes or compute particles fetch the compute chunk.

## Commits

1. `add(scene3d): add the gpu-driven instancing host module`
   - embed the Elio cull and Hi-Z kernels and the Hi-Z seed shaders
   - add the host that owns eligible instanced meshes for one frame at a time
   - register it in the compute chunk and the monolith; nothing calls it yet
2. `build(client): rebuild scene3d client bundles`
3. `test(scene3d): raise size budgets for gpu-driven instancing`
