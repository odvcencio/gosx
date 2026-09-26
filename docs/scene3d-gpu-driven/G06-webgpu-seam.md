# G06 — Renderer seam in `webgpu.ts`: frustum, shadows, occlusion, telemetry (repo: gosx)

Depends on: G01 (the cache-owner fix; several anchors below include its
code), G05.

## Goal

Wire the host from G05 into `createSceneWebGPURenderer`. After this task a
bundle with `gpuDriven` set draws every eligible opaque instanced mesh through
the GPU-driven path, culls shadow casters per light, splits the main pass for
two-phase occlusion when asked, and publishes telemetry. A bundle without it
renders exactly as before.

This is 19 find-and-replace edits. They were applied in this order to the
G01 state of `webgpu.ts` and reproduce the validated file byte for byte, so
apply them in order. Several anchors only exist after G01.

Numbers after this task (with G01 applied first): `webgpu.ts` grows from
19 162 to 19 227 lines; the ratchet count stays 1744.

## Frame flow after the edits

```
render()
  var materials / instancedDrawList            (moved up: edit 10)
  gpuDriven.beginFrame(...)                    upload changes, reset args, cull camera (slot 0)
  updateInstancedCullSystems(...)              skips owned meshes (edit 5)
  shadow loop: gpuDriven.lightView(...)        cull casters (slot 2 + light)
               renderShadowPass → drawInstancedShadowMeshes → drawShadowMesh
  gpuDriven.prepareMainPass(descriptor)        occlusion frames only
  mainPass = beginRenderPass(...)
  bundle flags: gpuDrivenSplit                 occlusion frames go direct
  direct branch opaque: drawInstancedMeshes → drawMesh (slot 0)
  mainPass = gpuDriven.splitMainPass(...)      Hi-Z, late cull, late pass, late draws (slot 1)
  … water, alpha, additive, labels, points (unchanged) …
  gpuDriven.finishEncoding(encoder); submit; gpuDriven.endFrame(mount)
```

## The edits

### Edit 1 — `ineligible`

Where: Module scope, in `sceneWebGPUBundleIneligibleReason`.

An occlusion frame splits the main pass in two, and one render bundle cannot straddle two passes. The reason string surfaces as `data-gosx-scene3d-webgpu-bundle-reason`.

Find (must match exactly once):

```js
    if (flags.disabled) return "disabled";
```

Replace with:

```js
    if (flags.disabled) return "disabled";
    if (flags.gpuDrivenSplit) return "gpu-driven-occlusion";
```

### Edit 2 — `inert`

Where: Module scope, right before the comment that introduces `sceneWebGPUDrawListHasDynamicMesh`.

The inert host answers every call with "not handled" until the compute chunk publishes the factory, so `render()` needs no branch. `splitMainPass` returns its second argument through `arguments[1]`, and `endFrame` returns `false` (not `null`): named parameters or a `null` return would each add a noImplicitAny diagnostic (TS7006, TS7011).

Find (must match exactly once):

```js
  // sceneWebGPUDrawListHasDynamicMesh reports whether any object in the three
```

Replace with:

```js
  // SCENE_GPU_DRIVEN_INERT_HOST stands in for the GPU-driven instancing host
  // (indirect-instancing.ts, compute chunk) until that chunk publishes its factory.
  // Every method reports "not handled", so render() needs no branch for it.
  var SCENE_GPU_DRIVEN_INERT_HOST = {
    beginFrame: function() { return false; },
    owns: function() { return false; },
    drawMesh: function() { return false; },
    lightView: function() { return false; },
    drawShadowMesh: function() { return false; },
    splitsMainPass: function() { return false; },
    prepareMainPass: function() { return false; },
    splitMainPass: function() { return arguments[1]; },
    finishEncoding: function() { return false; },
    endFrame: function() { return false; },
    dispose: function() {},
  };

  // sceneWebGPUDrawListHasDynamicMesh reports whether any object in the three
```

### Edit 3 — `state`

Where: Renderer scope, the state block.

A `Map` keeps the host value typed `any`, so every method call on it is free under the ratchet.

Find (must match exactly once):

```js
    var instancedCacheOwnerEpoch = 0;
```

Replace with:

```js
    var instancedCacheOwnerEpoch = 0;
    var gpuDrivenHosts = new Map(); // "host" → GPU-driven instancing host; a Map keeps it typed any
```

### Edit 4 — `accessor`

Where: Renderer scope, right before `ensureInstancedTransformGPUBuffer`.

The hooks reference only function declarations, module-scope constants and typed values. Do NOT add `frameBindGroupLayout`, `materialBindGroupLayout`, `shadowBindGroupLayout`, `pbrFragmentModule` or `shadowFragmentModule`: they are `var x = null` closure variables, and each read in another function adds TS7005 (verified: +1 per variable). The host builds its own group-equivalent layouts and modules from the factories and WGSL sources instead.

Find (must match exactly once):

```js
    function ensureInstancedTransformGPUBuffer(mesh, data) {
```

Replace with:

```js
    // webGPUGPUDrivenHost returns this renderer's GPU-driven instancing host
    // (indirect-instancing.ts), or the inert host until the compute chunk publishes the
    // factory. The hooks are the renderer's own factories, shader sources and
    // instanced helpers, so the host builds pipelines that match the renderer's.
    function webGPUGPUDrivenHost() {
      var host = gpuDrivenHosts.get("host");
      if (host) return host;
      var api = typeof window !== "undefined" ? window.__gosx_scene3d_api : null;
      if (!device || !api || typeof api.createSceneGPUDrivenHost !== "function") return SCENE_GPU_DRIVEN_INERT_HOST;
      host = api.createSceneGPUDrivenHost(device, {
        createFrameBindGroupLayout: wgpuCreateFrameBindGroupLayout,
        createMaterialBindGroupLayout: wgpuCreateMaterialBindGroupLayout,
        createShadowBindGroupLayout: wgpuCreateShadowBindGroupLayout,
        pbrInstancedVertexWGSL: WGSL_PBR_INSTANCED_VERTEX,
        pbrFragmentWGSL: WGSL_PBR_FRAGMENT,
        shadowInstancedVertexWGSL: WGSL_SHADOW_INSTANCED_VERTEX,
        shadowFragmentWGSL: WGSL_SHADOW_FRAGMENT,
        pbrVertexLayout: WGPU_PBR_VERTEX_LAYOUT,
        shadowVertexLayout: WGPU_SHADOW_VERTEX_LAYOUT,
        blendState: wgpuBlendState,
        instancedMeshCount: instancedMeshCount,
        instancedMeshColorData: instancedMeshColorData,
        getInstancedGeometry: getInstancedGeometry,
        ensureInstancedGeometryGPUBuffer: ensureInstancedGeometryGPUBuffer,
        instancedCullRadius: webGPUInstancedCullRadius,
        drawInstancedMeshes: drawInstancedMeshes,
      });
      gpuDrivenHosts.set("host", host);
      return host;
    }

    function ensureInstancedTransformGPUBuffer(mesh, data) {
```

### Edit 5 — `cull-skip`

Where: `updateInstancedCullSystems` loop.

An owned mesh must not also get a per-mesh cull system.

Find (must match exactly once):

```js
        if (!mesh) continue;
        var wgsl = (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim()) ? mesh.cullKernelWGSL.trim() : null;
```

Replace with:

```js
        if (!mesh || webGPUGPUDrivenHost().owns(mesh)) continue;
        var wgsl = (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim()) ? mesh.cullKernelWGSL.trim() : null;
```

### Edit 6 — `draw`

Where: `drawInstancedMeshes`, right after group 1 is bound.

The host draws owned meshes with its own pipeline, group 2 and `drawIndirect`; everything else falls through to the classic code.

Find (must match exactly once):

```js
        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, webGPUInstancedCacheOwner(mesh.id) || mesh));
```

Replace with:

```js
        pass.setBindGroup(1, createMaterialBindGroup(mat, !!mesh.receiveShadow, webGPUInstancedCacheOwner(mesh.id) || mesh));
        if (webGPUGPUDrivenHost().drawMesh(pass, mesh, depthWrite)) continue;
```

### Edit 7 — `shadow-sig`

Where: `drawInstancedShadowMeshes` signature.

A primitive default keeps the new parameter typed (00 §7.5).

Find (must match exactly once):

```js
    function drawInstancedShadowMeshes(pass, bundle) {
```

Replace with:

```js
    function drawInstancedShadowMeshes(pass, bundle, lightSlot = 0) {
```

### Edit 8 — `shadow-draw`

Where: `drawInstancedShadowMeshes` loop.

After a GPU-driven draw the classic loop must re-bind its own pipeline, so `drew` resets. Keep this on ONE line: README rule 6 forbids a line of `webgpu.ts` matching `/[Cc]ull.*shadow/`; this line contains no "cull".

Find (must match exactly once):

```js
        if (!mesh || mesh.viewCulled || !mesh.castShadow) continue;
        var instanceCount = instancedMeshCount(mesh);
```

Replace with:

```js
        if (!mesh || mesh.viewCulled || !mesh.castShadow) continue;
        if (webGPUGPUDrivenHost().drawShadowMesh(pass, mesh, lightSlot)) { drew = false; continue; }
        var instanceCount = instancedMeshCount(mesh);
```

### Edit 9 — `shadow-call`

Where: `renderShadowPass`, its last draw call.

Pass the light slot through.

Find (must match exactly once):

```js
      drawInstancedShadowMeshes(pass, bundle);
```

Replace with:

```js
      drawInstancedShadowMeshes(pass, bundle, Math.max(0, Math.floor(sceneNumber(shadowResource.lightSlot, 0))));
```

### Edit 10 — `begin`

Where: `render()`, the GPU frustum cull call.

`materials` and `instancedDrawList` move up here (edits 11 and 12 delete their old copies) because the host needs the opaque list before the shadow passes. `var` hoisting keeps every later reference valid.

Find (must match exactly once):

```js
      updateInstancedCullSystems(bundle.instancedMeshes, encoder, scratchSelenaViewProjection);
```

Replace with:

```js
      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      var instancedDrawList = hasInstancedData
        ? buildInstancedDrawList(bundle, materials)
        : { opaque: [], alpha: [], additive: [] };
      var gpuDriven = webGPUGPUDrivenHost();
      gpuDriven.beginFrame(bundle, encoder, { viewProjection: scratchSelenaViewProjection, camera: cam, width: scaledW, height: scaledH, sampleCount: sampleCount, targetFormat: targetFormat, opaque: instancedDrawList.opaque });
      updateInstancedCullSystems(bundle.instancedMeshes, encoder, scratchSelenaViewProjection);
```

### Edit 11 — `materials-move`

Where: `render()`, after `createFrameBindGroup`.

Delete the old `materials` declaration (moved up by edit 10).

Find (must match exactly once):

```js
      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      var waterObjectSceneTextureStats
```

Replace with:

```js
      var waterObjectSceneTextureStats
```

### Edit 12 — `drawlist-move`

Where: `render()`, after `encoder.beginRenderPass(mainPassDescriptor)`.

Delete the old `instancedDrawList` declaration (moved up by edit 10).

Find (must match exactly once):

```js
      var instancedDrawList = hasInstancedData
        ? buildInstancedDrawList(bundle, materials)
        : { opaque: [], alpha: [], additive: [] };
      var drawList = hasPBRData
```

Replace with:

```js
      var drawList = hasPBRData
```

### Edit 13 — `light`

Where: `render()`, shadow loop.

Cull this light's casters right before its shadow pass encodes.

Find (must match exactly once):

```js
        renderShadowPass(encoder, lightMatrix, bundle, { view: shadowSlots[slot].view, lightSlot: slot }, pbrSceneBuffers);
```

Replace with:

```js
        gpuDriven.lightView(encoder, lightMatrix, slot);
        renderShadowPass(encoder, lightMatrix, bundle, { view: shadowSlots[slot].view, lightSlot: slot }, pbrSceneBuffers);
```

### Edit 14 — `prepare`

Where: `render()`, main pass begin.

On an occlusion frame the resolve target and the end timestamp move to the late pass.

Find (must match exactly once):

```js
      var mainPass = encoder.beginRenderPass(mainPassDescriptor);
```

Replace with:

```js
      gpuDriven.prepareMainPass(mainPassDescriptor);
      var mainPass = encoder.beginRenderPass(mainPassDescriptor);
```

### Edit 15 — `flag`

Where: `render()`, the render-bundle eligibility flags.

Feeds the reason added by edit 1.

Find (must match exactly once):

```js
        hasWater: hasWaterData,
        hasPoints: hasPointsData,
```

Replace with:

```js
        gpuDrivenSplit: gpuDriven.splitsMainPass(),
        hasWater: hasWaterData,
        hasPoints: hasPointsData,
```

### Edit 16 — `split`

Where: `render()`, direct branch, after the opaque section.

Ends the early pass, builds the Hi-Z pyramid, runs the late cull, opens the late pass and draws the owned meshes' late lists. On a frame without occlusion it returns the pass unchanged and does nothing else.

Find (must match exactly once):

```js
        // The water surface writes depth before translucent world surfaces.
```

Replace with:

```js
        // Two-phase occlusion: end the early pass, cull against its depth, and
        // draw the newly visible instances in a late pass that loads it.
        mainPass = gpuDriven.splitMainPass(encoder, mainPass, mainPassDescriptor, frameBindGroup, materials, instancedDrawList.opaque);

        // The water surface writes depth before translucent world surfaces.
```

### Edit 17 — `submit`

Where: `render()`, around submit.

Copy the args for the telemetry readback before submit; map it and publish attributes after. `Object.assign` ignores the inert host's `false`.

Find (must match exactly once):

```js
      endGPUPassTimingFrame(encoder);
      device.queue.submit([encoder.finish()]);
```

Replace with:

```js
      endGPUPassTimingFrame(encoder);
      gpuDriven.finishEncoding(encoder);
      device.queue.submit([encoder.finish()]);
      Object.assign(frameStats, gpuDriven.endFrame(canvas && canvas.parentNode));
```

### Edit 18 — `dispose`

Where: Renderer `dispose()`.

`forEach` with a callback on an untyped `Map` is contextually typed, so it adds no diagnostic. Do not call `webGPUGPUDrivenHost()` here: it would create a host during dispose.

Find (must match exactly once):

```js
      instancedCullSystems.clear();
      instancedCacheOwners.clear();
```

Replace with:

```js
      instancedCullSystems.clear();
      instancedCacheOwners.clear();
      gpuDrivenHosts.forEach(function(host) { host.dispose(); });
      gpuDrivenHosts.clear();
```

### Edit 19 — `depth`

Where: `ensureMainDepth`.

The Hi-Z seed samples the main depth texture, so it needs `TEXTURE_BINDING` (the post-FX depth target already has it).

Find (must match exactly once):

```js
        sampleCount: sampleCount,
        usage: GPUTextureUsage.RENDER_ATTACHMENT,
      });
      mainDepthView = mainDepthTexture.createView();
```

Replace with:

```js
        sampleCount: sampleCount,
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      mainDepthView = mainDepthTexture.createView();
```

## Check the edits before building

```sh
grep -c "webGPUGPUDrivenHost()" client/runtime/scene3d/webgpu.ts          # 5
grep -c "gpuDriven\." client/runtime/scene3d/webgpu.ts                    # 7
grep -nE "wgpuCreateShadow.*[Cc]ull|[Cc]ull.*shadow" client/runtime/scene3d/webgpu.ts   # prints nothing
grep -c "var materials = Array.isArray(bundle.materials)" client/runtime/scene3d/webgpu.ts   # 1
grep -c "var instancedDrawList = hasInstancedData" client/runtime/scene3d/webgpu.ts          # 1
```

## Tests — append parts B and C to `client/js/scene3d-gpu-driven.test.js`

Part B (append to the end of the file, exactly):

```js
function transformsFor(count) {
  const out = [];
  for (let i = 0; i < count; i += 1) out.push(1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, (i % 10) - 5, 0, -Math.floor(i / 10), 1);
  return out;
}

function sceneState(api, gpuDriven, options) {
  const opts = options || {};
  const scene = {
    instancedMeshes: [
      { id: "crates", count: 30, kind: "box", width: 0.5, height: 0.5, depth: 0.5, transforms: transformsFor(30), castShadow: true, colors: new Array(30).fill("#ff8800") },
      { id: "orbs", count: 12, kind: "sphere", radius: 0.3, transforms: transformsFor(12) },
      { id: "glass", count: 4, kind: "box", transforms: transformsFor(4), opacity: 0.4 },
    ],
  };
  if (opts.light) scene.lights = [{ id: "sun", kind: "directional", castShadow: true, x: -1, y: -2, z: -1, intensity: 1 }];
  if (gpuDriven) scene.gpuDriven = gpuDriven;
  return api.createSceneState({ scene }, { tier: "full" });
}

function frameBundle(api, state) {
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000",
    { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 },
    [], [], [], [], api.sceneStateLights(state), {}, 0, [], api.sceneStateInstancedMeshesWithMaterials(state), [], [], [], 0, false,
  );
  bundle.gpuDriven = state.gpuDriven;
  return bundle;
}

async function renderFrames(harness, api, state, frames) {
  for (let frame = 0; frame < frames; frame += 1) {
    harness.fake.state.renderPasses.length = 0;
    harness.fake.state.computePasses.length = 0;
    harness.renderer.render(frameBundle(api, state), { width: 64, height: 64 }, { nowMS: frame * 16, active: true });
    await flushAsyncWork();
  }
}

async function gpuDrivenHarness(options) {
  const opts = options || {};
  const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: Object.assign({ timestampQuery: true }, opts.device || {}) });
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  // The fake device drops buffer labels; keep them so tests can find buffers.
  const createBuffer = harness.fake.device.createBuffer;
  harness.fake.device.createBuffer = function(desc) {
    const buffer = createBuffer.call(this, desc);
    buffer.label = desc && desc.label;
    return buffer;
  };
  return { harness, api: harness.env.context.__gosx_scene3d_api };
}

const label = (buffer) => buffer && buffer.label;
const mainPasses = (fake) => fake.state.renderPasses.filter((pass) => pass.descriptor && pass.descriptor.colorAttachments && pass.descriptor.colorAttachments.length > 0);
const shadowPasses = (fake) => fake.state.renderPasses.filter((pass) => pass.descriptor && pass.descriptor.colorAttachments && pass.descriptor.colorAttachments.length === 0);
const cullPasses = (fake) => fake.state.computePasses.filter((pass) => pass.descriptor && pass.descriptor.label === "gosx-gpu-driven-cull");

test("gpu-driven: without the mode the renderer takes its classic path", async () => {
  const { harness, api } = await gpuDrivenHarness();
  await renderFrames(harness, api, sceneState(api, null), 3);
  assert.equal(cullPasses(harness.fake).length, 0);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-active"), "false");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-reason"), "off");
});

test("gpu-driven: frustum mode culls once and draws owned meshes indirectly", async () => {
  const { harness, api } = await gpuDrivenHarness();
  const state = sceneState(api, { occlusion: false, shadowCulling: true });
  await renderFrames(harness, api, state, 1);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-reason"), "warming", "pipelines resolve after the first frame");
  await renderFrames(harness, api, state, 2);
  const fake = harness.fake;
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-active"), "true");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-meshes"), "2", "the translucent mesh stays classic");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-instances"), "42");
  const culls = cullPasses(fake);
  assert.equal(culls.length, 1);
  assert.equal(culls[0].dispatches[0].workgroupCountX, 1);
  const main = mainPasses(fake)[0];
  const indirect = main.drawIndirects.filter((d) => label(d.buffer) === "gosx.gpu-driven.args");
  assert.deepEqual(indirect.map((d) => d.offset), [0, 64], "slot 0 of mesh 0 and mesh 1");
  assert.equal(indirect[0].pipeline.desc.label, "gosx-gpu-driven-pbr");
  const lists = main.vertexBuffers.filter((v) => v.slot === 4 && label(v.buffer) === "gosx.gpu-driven.visible");
  assert.deepEqual(lists.map((v) => [v.offset, v.size]), [[0, 120], [480, 48]]);
  assert.ok(!fake.state.buffers.some((b) => b.label === "gosx.cull.input"), "owned meshes never build a per-mesh cull system");
  assert.equal(main.draws.length >= 1, true, "the translucent mesh still draws directly");
});

test("gpu-driven: shadow lights cull casters and draw from their own slot", async () => {
  const { harness, api } = await gpuDrivenHarness();
  await renderFrames(harness, api, sceneState(api, {}, { light: true }), 3);
  const fake = harness.fake;
  const culls = cullPasses(fake);
  assert.equal(culls.length, 2, "camera + one shadow light");
  const shadow = shadowPasses(fake).find((pass) => pass.drawIndirects.length > 0);
  assert.ok(shadow, "the shadow pass draws owned casters indirectly");
  assert.deepEqual(shadow.drawIndirects.map((d) => d.offset), [32], "slot 2 of the one owned caster (orbs cast no shadow)");
  assert.equal(shadow.drawIndirects[0].pipeline.desc.label, "gosx-gpu-driven-shadow");
});

test("gpu-driven: frustum mode keeps render bundles replaying and uploads only on change", async () => {
  const { harness, api } = await gpuDrivenHarness({ device: { renderBundles: true } });
  const state = sceneState(api, {});
  await renderFrames(harness, api, state, 3);
  const writesBefore = harness.fake.state.writeBufferCalls.filter((c) => label(c.buffer) === "gosx.gpu-driven.instances").length;
  await renderFrames(harness, api, state, 3);
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-state"), "replayed");
  const writesAfter = harness.fake.state.writeBufferCalls.filter((c) => label(c.buffer) === "gosx.gpu-driven.instances").length;
  assert.equal(writesAfter, writesBefore, "static transforms upload once");
  state.instancedMeshes[1].transforms = transformsFor(12).map((v, i) => (i % 16 === 13 ? v + 1 : v));
  await renderFrames(harness, api, state, 1);
  const changed = harness.fake.state.writeBufferCalls.filter((c) => label(c.buffer) === "gosx.gpu-driven.instances").slice(writesAfter);
  assert.equal(changed.length, 1, "one mesh changed, one range uploaded");
  assert.equal(changed[0].offset, 30 * 96);
});

test("gpu-driven: vertex shader derivations swap every instance input", async () => {
  const { harness, api } = await gpuDrivenHarness();
  await renderFrames(harness, api, sceneState(api, {}), 2);
  const code = (name) => harness.fake.state.shaderModules.find((m) => m.label === name).code;
  const pbr = code("gosx-gpu-driven-pbr-vert");
  assert.ok(pbr.includes("@location(4) gdSlot: u32,"));
  assert.ok(pbr.includes("@group(2) @binding(0) var<storage, read> gdInstances: array<GDInstance>;"));
  assert.ok(pbr.includes("let gdInstance = gdInstances[in.gdSlot];"));
  assert.ok(pbr.includes("out.instanceColor = gdInstance.color;"));
  assert.ok(!pbr.includes("instanceMatrix") && !pbr.includes("@location(8)"));
  const shadow = code("gosx-gpu-driven-shadow-vert");
  assert.ok(shadow.includes("@location(4) gdSlot: u32,"));
  assert.ok(shadow.includes("@group(1) @binding(0) var<storage, read> gdInstances: array<GDInstance>;"));
  assert.ok(shadow.includes("let model = gdInstances[in.gdSlot].model;"));
  assert.ok(!shadow.includes("instanceMatrix"));
  assert.throws(() => api.sceneGPUDrivenPBRVertexWGSL("fn main() {}"), /anchor missing: pbr inputs/);
});
```

Part C (append after part B, exactly):

```js
test("gpu-driven: two-phase occlusion splits the main pass around a Hi-Z build", async () => {
  const { harness, api } = await gpuDrivenHarness({ device: { renderBundles: true } });
  await renderFrames(harness, api, sceneState(api, { occlusion: true }), 4);
  const fake = harness.fake;
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-occlusion"), "true");
  const passes = mainPasses(fake);
  assert.equal(passes.length, 2, "early + late");
  const late = passes[1];
  assert.equal(late.descriptor.label, "gosx-gpu-driven-late");
  assert.equal(late.descriptor.colorAttachments[0].loadOp, "load");
  assert.equal(late.descriptor.depthStencilAttachment.depthLoadOp, "load");
  assert.equal(passes[0].ended, true);
  assert.equal(passes[0].descriptor.timestampWrites.endOfPassWriteIndex, undefined, "the early pass only stamps the start");
  assert.equal(late.descriptor.timestampWrites.endOfPassWriteIndex, passes[0].descriptor.timestampWrites.beginningOfPassWriteIndex + 1, "the late pass stamps the end");
  assert.deepEqual(late.drawIndirects.map((d) => d.offset), [16, 80], "slot 1 of both owned meshes");
  const hiz = fake.state.computePasses.find((pass) => pass.descriptor && pass.descriptor.label === "gosx-gpu-driven-hiz");
  assert.ok(hiz, "Hi-Z pass ran");
  assert.equal(hiz.dispatches.length, 6, "seed + 5 downsample levels for 64x64");
  assert.equal(cullPasses(fake).length, 2, "early + late camera culls");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-bundle-reason"), "gpu-driven-occlusion");
});

```

What they prove, all on the fake device with a NEW bundle per frame:

| Test | Proves |
|---|---|
| classic path | no gpuDriven → no cull pass; `reason` = `off` |
| frustum mode | frame 1 `warming` (pipelines resolve after it); then one cull dispatch, two indirect draws at args offsets 0 and 64, visible-list bindings `[0,120]` and `[480,48]` (B4), the translucent mesh stays classic, no per-mesh cull system |
| shadow lights | one extra dispatch per light; the shadow pass draws the one caster from slot 2 (offset 32) with the GPU-driven shadow pipeline |
| render bundles | frustum frames replay; static transforms upload once; replacing one mesh's transforms uploads exactly that mesh's range (offset 30 × 96) |
| derivations | the created vertex modules contain the A7 properties; a bad anchor throws |
| occlusion | early + late main passes; late loads color/depth and carries the end timestamp; the Hi-Z pass has 6 dispatches for 64×64 (seed + 5 levels); two camera culls; the bundle reason is `gpu-driven-occlusion` |

Buffers in the fake device carry no label, so the test wraps
`device.createBuffer` to keep `desc.label`.

## Verify

```sh
make build-bootstrap
node --test client/js/scene3d-gpu-driven.test.js                 # 8 pass
node --test client/js/scene3d-webgpu-instanced-cache.test.js client/js/runtime-21-scene-gpu-cull-bundles.test.js client/js/scene3d-gpu-driven-plumbing.test.js
(cd client/runtime && npm run typecheck)                         # ratchet OK: 4874
node --test client/js/*.test.js client/js/*.test.mjs client/runtime/*/*.test.js
```

The full suite then fails in exactly these governed places, which G10 fixes:

- `renderer source-set and file lines can only decrease …`
  (`webgpu.ts exceeds 19162 physical lines`) → G10 §A.
- The Go ratchets in `cmd/buildbootstrap` (function exception keys shifted,
  symbol ceilings for `createSceneWebGPURenderer` and `render`) → G10 §A.
- `bootstrap-size.test.mjs`: the Chromium WebGPU route (gzip and brotli) and
  possibly `bootstrap.js` gzip → G10 §B.

Nothing else may fail. After G10 §A and §B the whole suite passes (2044 of
2046 at the validation run, 2 skipped, 0 failed).

## Commits

1. `add(scene3d): route eligible instanced meshes through the gpu-driven host`
   - cull owned meshes once per view and draw them with drawIndirect
   - cull shadow casters per light and split the main pass for two-phase occlusion
   - publish data-gosx-scene3d-webgpu-gpu-driven-* telemetry
2. `build(client): rebuild scene3d client bundles`
3. `update(testdata): update scene3d renderer architecture metrics` (G10 §A)
4. `test(scene3d): raise size budgets for gpu-driven instancing` (G10 §B)
