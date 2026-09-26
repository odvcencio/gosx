# G01 — Fix per-frame GPU allocations for instanced meshes (repo: gosx)

Depends on: 01-preflight. This task is independent of the GPU-driven feature.
It lands first because the feature's CPU savings depend on render bundles
actually replaying.

## The defect (reproduced)

`appendSceneInstancedMeshesToBundle` gives the renderer a new shallow copy of
every instanced mesh each frame (00 §2). `drawInstancedMeshes` passes that copy
as the cache owner:

- `createMaterialBindGroup(mat, !!mesh.receiveShadow, mesh)` creates a new
  material uniform buffer and a new bind group every frame.
- `ensureInstancedTransformGPUBuffer(mesh, ...)` and
  `ensureInstancedColorGPUBuffer(mesh, ...)` create new vertex buffers every
  frame on the draw-all path, in the shadow pass, and in the pick pass
  (`instancedTransformBuffer: ensureInstancedTransformGPUBuffer`).
- Every buffer goes into `pointsEntryGPUBuffers`, which is only emptied at
  dispose. GPU memory therefore grows without bound. The new bind group also
  changes the render-bundle token stream, so `data-gosx-scene3d-webgpu-bundle-state`
  stays `encoded` forever and never reaches `replayed`.

Measured with the repo's harness, building a new bundle per frame the way
`mount.ts` does: +1 buffer and +1 bind group per frame for a 300-instance
GPU-culled mesh, and bundle state `encoded` on every frame.

## Fix

Key cache ownership by `mesh.id` in a renderer-scoped `Map`. Sweep owners that
have not drawn for 120 rendered frames.

Apply this patch from the repo root with `git apply`. It was generated against
the baseline commit and dry-run tested. If it does not apply cleanly, make the
five edits listed after it by hand.

```diff
diff --git a/client/runtime/scene3d/webgpu.ts b/client/runtime/scene3d/webgpu.ts
index f44b4a1..fb904d6 100644
--- a/client/runtime/scene3d/webgpu.ts
+++ b/client/runtime/scene3d/webgpu.ts
@@ -6664,6 +6664,8 @@
     var waterSystems = new Map();
     var waterSystemRetireSerial = 0;
     var instancedCullSystems = new Map(); // meshId → { system, signature }
+    var instancedCacheOwners = new Map(); // meshId → owner of that mesh's cached GPU buffers and bind groups
+    var instancedCacheOwnerEpoch = 0;
     var lastComputeParticleTimeSeconds = null;
     var lastWaterTimeSeconds = null;
     var waterClockAPI = (typeof window !== "undefined" && window.__gosx_scene3d_api)
@@ -16112,12 +16114,44 @@
       return wgpuCachedTrackedBuffer(geom, slot, data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, false);
     }

+    // webGPUInstancedCacheOwner returns the object that owns one instanced
+    // mesh's cached GPU buffers and bind groups. The render bundle hands the
+    // renderer a fresh shallow copy of every instanced mesh each frame, so
+    // caching on that copy created a uniform buffer and a bind group per mesh
+    // per frame and kept render bundles from replaying. Key by mesh id.
+    function webGPUInstancedCacheOwner(meshId = "") {
+      if (!meshId) return null;
+      var owner = instancedCacheOwners.get(meshId);
+      if (!owner) {
+        owner = { meshId: meshId, seenEpoch: 0 };
+        instancedCacheOwners.set(meshId, owner);
+      }
+      owner.seenEpoch = instancedCacheOwnerEpoch;
+      return owner;
+    }
+
+    // webGPUSweepInstancedCacheOwners frees the cached buffers of instanced
+    // meshes that have not drawn for 120 rendered frames.
+    function webGPUSweepInstancedCacheOwners() {
+      instancedCacheOwners.forEach(function(owner, meshId) {
+        if (instancedCacheOwnerEpoch - owner.seenEpoch <= 120) return;
+        var slots = ["_gosxWGPUInstanceTransformBuffer", "_gosxWGPUInstanceColorBuffer", "_gosxWGPUMaterialUniform", "_gosxWGPUMaterialShadowUniform"];
+        for (var i = 0; i < slots.length; i++) {
+          var buffer = owner[slots[i]];
+          if (!buffer) continue;
+          pointsEntryGPUBuffers.delete(buffer);
+          destroyRendererGPUResource(buffer);
+        }
+        instancedCacheOwners.delete(meshId);
+      });
+    }
+
     function ensureInstancedTransformGPUBuffer(mesh, data) {
-      return wgpuCachedTrackedBuffer(mesh, "_gosxWGPUInstanceTransformBuffer", data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true);
+      return wgpuCachedTrackedBuffer(webGPUInstancedCacheOwner(mesh && mesh.id) || mesh, "_gosxWGPUInstanceTransformBuffer", data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true);
     }

     function ensureInstancedColorGPUBuffer(mesh, data) {
-      return wgpuCachedTrackedBuffer(mesh, "_gosxWGPUInstanceColorBuffer", data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true);
+      return wgpuCachedTrackedBuffer(webGPUInstancedCacheOwner(mesh && mesh.id) || mesh, "_gosxWGPUInstanceColorBuffer", data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true);
     }

     function buildInstancedDrawList(bundle, materials) {
@@ -16152,7 +16186,7 @@
         if (!geom || geom.vertexCount <= 0) continue;

         /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ var mat = instancedMeshMaterial(mesh, materials);
-        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, mesh));
+        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, webGPUInstancedCacheOwner(mesh.id) || mesh));

         // Indirect draw via GPU cull (D3: ready cull record → drawIndirect;
         // not-ready / no kernel / capability absent → draw-all).
@@ -18067,6 +18101,8 @@
         hasWaterData = Array.isArray(bundle.waterSystems) && bundle.waterSystems.length > 0;
       }
       webGPUBeginRetainedMeshFrame(bundle);
+      instancedCacheOwnerEpoch += 1;
+      webGPUSweepInstancedCacheOwners();
       if (!hasPBRData && !hasPointsData && !hasInstancedData && !hasWorldLines && !hasScreenLines && !hasSurfaces && !hasLabels && !hasWaterData) {
         webGPUSweepRetainedMeshBuffers();
         return;
@@ -18789,6 +18825,7 @@
         }
       }
       instancedCullSystems.clear();
+      instancedCacheOwners.clear();
       waterRenderPipelineCache.clear();
       pointsAuthoredPipelineCache.clear();
       pointsAuthoredLayerFailed.clear();
```

Manual edits, if the patch does not apply (all in `client/runtime/scene3d/webgpu.ts`):

1. Anchor `    var instancedCullSystems = new Map(); // meshId → { system, signature }`:
   add the two lines `var instancedCacheOwners = new Map(); ...` and
   `var instancedCacheOwnerEpoch = 0;` right after it.
2. Anchor `    function ensureInstancedTransformGPUBuffer(mesh, data) {`: insert
   `webGPUInstancedCacheOwner` and `webGPUSweepInstancedCacheOwners`
   immediately before it, exactly as in the patch. In both
   `ensureInstancedTransformGPUBuffer` and `ensureInstancedColorGPUBuffer`,
   replace the first argument `mesh` with `webGPUInstancedCacheOwner(mesh && mesh.id) || mesh`.
3. Anchor `        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, mesh));`:
   replace the last argument with `webGPUInstancedCacheOwner(mesh.id) || mesh`.
4. Anchor `      webGPUBeginRetainedMeshFrame(bundle);` (inside `render`): add
   `instancedCacheOwnerEpoch += 1;` and `webGPUSweepInstancedCacheOwners();`
   right after it. They run before the empty-frame early return, so owners
   still age on empty frames.
5. Anchor `      instancedCullSystems.clear();` (inside `dispose`): add
   `instancedCacheOwners.clear();` right after it.

Why this shape passes the ratchets:

- `webGPUInstancedCacheOwner(meshId = "")` has a string default, so it adds
  no TS7006.
- `instancedCacheOwners.get(...)` is `any`, so there is no TS7005.
- The `forEach` callback is contextually typed.
- `render` gains 2 lines and no branch.

Measured: `webgpu.ts` stays at 1744 ratchet diagnostics.

## Test: create `client/js/scene3d-webgpu-instanced-cache.test.js`

```js
"use strict";

// Instanced meshes reach the WebGPU renderer as a fresh shallow copy every
// frame (appendSceneInstancedMeshesToBundle). Their GPU caches must survive
// that copy: no buffer or bind group per frame, and a static scene must
// replay its render bundle. These tests build a NEW bundle per frame the way
// mount.ts does; reusing one bundle object would hide the defect.

const test = require("node:test");
const assert = require("node:assert/strict");

const { createBoardWebGPUHarness, flushAsyncWork } = require("./runtime-test-harness.js");

function instancedSceneState(api, id, count) {
  const transforms = [];
  for (let i = 0; i < count; i += 1) {
    transforms.push(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, (i % 20) - 10, 0, -Math.floor(i / 20), 1);
  }
  return api.createSceneState({
    scene: {
      instancedMeshes: [{ id, count, kind: "box", width: 0.2, height: 0.2, depth: 0.2, transforms, color: "#44aa44" }],
    },
  }, { tier: "full" });
}

function frameBundle(api, instancedMeshes) {
  return api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 },
    [], [], [], [], [], {}, 0, [], instancedMeshes, [], [], [], 0, false,
  );
}

async function renderFrames(harness, api, state, frames, startFrame) {
  const samples = [];
  for (let frame = 0; frame < frames; frame += 1) {
    const meshes = state ? api.sceneStateInstancedMeshesWithMaterials(state) : [];
    harness.renderer.render(frameBundle(api, meshes), { width: 64, height: 64 }, { nowMS: (startFrame + frame) * 16, active: true });
    await flushAsyncWork();
    samples.push({
      buffers: harness.fake.state.buffers.length,
      bindGroups: harness.fake.state.bindGroups.length,
      bundle: harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"),
    });
  }
  return samples;
}

for (const count of [300, 8]) {
  test(`instanced cache: per-frame bundle copies reuse GPU state (count ${count})`, async () => {
    const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { renderBundles: true } });
    const api = harness.env.context.__gosx_scene3d_api;
    harness.canvas.width = 64;
    harness.canvas.height = 64;
    const samples = await renderFrames(harness, api, instancedSceneState(api, "grass", count), 6, 0);
    const detail = JSON.stringify(samples);
    assert.equal(samples[5].buffers, samples[2].buffers, "a GPU buffer was created per frame: " + detail);
    assert.equal(samples[5].bindGroups, samples[2].bindGroups, "a bind group was created per frame: " + detail);
    assert.equal(samples[5].bundle, "replayed", "a static instanced scene must replay its render bundle: " + detail);
  });
}

test("instanced cache: owners of meshes gone for 120 frames are released", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { renderBundles: true } });
  const api = harness.env.context.__gosx_scene3d_api;
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  await renderFrames(harness, api, instancedSceneState(api, "rocks", 8), 3, 0);
  const destroyedBefore = harness.fake.state.buffers.filter((buffer) => buffer.destroyed).length;
  await renderFrames(harness, api, null, 122, 3);
  const destroyedAfter = harness.fake.state.buffers.filter((buffer) => buffer.destroyed).length;
  // The mesh owned a transform, a colour and a material uniform buffer.
  assert.ok(destroyedAfter - destroyedBefore >= 3, `expected the three owner buffers to be destroyed, got ${destroyedAfter - destroyedBefore}`);
});
```

Proof that the test is meaningful. With the test file present and the fix
reverted, all three tests fail. With the fix, all three pass, and
`client/js/runtime-21-scene-gpu-cull-bundles.test.js` still passes 57/57.

## Governance (run the G10 procedure)

Expected effects after the fix (measured on a dry run):

- `webgpu.ts` grows by 37 lines. `files["../runtime/scene3d/webgpu.ts"]`
  19125 → 19162, `sourceSets.webgpu.maxLines` 19330 → 19367,
  `maxFileLines` 19125 → 19162.
- 24 function-exception keys shift (for example `render@17989:5` →
  `render@18023:5`).
- `createSceneWebGPURenderer` symbol and exception: lines 12110 → 12145,
  cyclomatic 4085 → 4095, cognitive 5304 → 5315. `render` symbol lines
  677 → 679.
- Size: the "Scene3D Chromium route (WebGPU, with labels)" gzip rises from
  351156 to 351327 (hard limit 351384) and brotli from 295849 to 296073 (hard
  limit 296100). That still passes, but only by 57 and 27 bytes. No budget edit
  is needed in this task.

## Verify

```sh
make build-bootstrap
node --test client/js/scene3d-webgpu-instanced-cache.test.js          # 3 pass
node --test client/js/runtime-21-scene-gpu-cull-bundles.test.js       # 57 pass
node --test client/js/scene3d-renderer-architecture.test.js client/js/bootstrap-size.test.mjs
(cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...)
(cd client/runtime && npm run typecheck)                              # webgpu.ts still 1744
git diff --check
```

## Commits

1. `fix(scene3d): key instanced webgpu caches by mesh id`, containing
   `webgpu.ts` and the new test.
   - The render bundle copies every instanced mesh each frame, so caches on
     the copy leaked a uniform buffer and a bind group per mesh per frame.
   - Owners now live in a renderer map keyed by mesh id, and meshes absent for
     120 frames release their buffers.
   - Static instanced scenes now replay their render bundle.
2. `update(testdata): update scene3d renderer architecture metrics`, containing
   the architecture JSON.
3. `build(client): rebuild scene3d client bundles`, containing the generated
   files only.
