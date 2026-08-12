// GPU picking parity tests for the WebGPU renderer (16a-scene-webgpu.js).
//
// These tests load three bootstrap fragments into ONE VM context, exactly the
// way the shipped bundles concatenate them:
//   - 11-scene-math.js   -> sceneScreenToRay and the ray/triangle helpers
//   - 17-scene-input.js  -> the shared CPU pick contract used by BOTH backends
//   - 16a-scene-webgpu.js -> the WebGPU renderer and its GPU picker
//
// The point of the suite is parity. The WebGPU picker resolves identity on the
// GPU and then derives every geometric field from the same shared CPU raycast
// helpers WebGL2 uses, so a pick must return the same record on both backends.
//
// Headless environments have no WebGPU adapter, so the pass recording and the
// readback run against a recording fake device. That exercises the picker's own
// logic (ID plan, scissor, copy geometry, map, resolve) without a GPU. It does
// NOT validate WGSL compilation or rasterization.

import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const srcDir = path.join(__dirname, "bootstrap-src");

function readSource(name) {
  return fs.readFileSync(name.startsWith("../") ? path.join(__dirname, name) : path.join(srcDir, name), "utf8");
}

// --- Fake WebGPU device -----------------------------------------------------

function createFakeGPU() {
  const log = {
    shaderModules: [],
    pipelines: [],
    textures: [],
    buffers: [],
    writes: [],
    passes: [],
    copies: [],
    bindGroupLayouts: [],
  };
  let pendingMapResolvers = [];

  function makeBuffer(desc) {
    const bytes = new Uint8Array(desc.size || 256);
    const buffer = {
      label: desc.label || "",
      size: desc.size || 0,
      usage: desc.usage || 0,
      destroyed: false,
      mapped: false,
      bytes,
      mapAsync() {
        return new Promise((resolve) => {
          pendingMapResolvers.push(() => {
            buffer.mapped = true;
            resolve();
          });
        });
      },
      getMappedRange(offset = 0, size = bytes.byteLength) {
        return bytes.buffer.slice(offset, offset + size);
      },
      unmap() {
        buffer.mapped = false;
      },
      destroy() {
        buffer.destroyed = true;
      },
    };
    log.buffers.push(buffer);
    return buffer;
  }

  function makePass(desc) {
    const record = {
      label: desc.label || "",
      colorFormats: (desc.colorAttachments || []).map((a) => (a && a.view ? a.view.format : null)),
      loadOp: desc.colorAttachments && desc.colorAttachments[0] ? desc.colorAttachments[0].loadOp : "",
      depthLoadOp: desc.depthStencilAttachment ? desc.depthStencilAttachment.depthLoadOp : "",
      depthStoreOp: desc.depthStencilAttachment ? desc.depthStencilAttachment.depthStoreOp : "",
      scissor: null,
      pipelines: [],
      draws: [],
      drawOffsets: [],
      vertexBuffers: [],
      ended: false,
    };
    log.passes.push(record);
    let currentOffsets = null;
    return {
      setScissorRect(x, y, w, h) { record.scissor = [x, y, w, h]; },
      setPipeline(p) { record.pipelines.push(p && p.label); },
      setBindGroup(index, group, offsets) {
        currentOffsets = offsets ? Array.from(offsets) : null;
      },
      setVertexBuffer(slot, buffer) { record.vertexBuffers.push(slot); },
      draw(vertexCount, instanceCount) {
        record.draws.push([vertexCount, instanceCount === undefined ? 1 : instanceCount]);
        record.drawOffsets.push(currentOffsets ? currentOffsets.slice() : null);
      },
      end() { record.ended = true; },
    };
  }

  const device = {
    createShaderModule(desc) {
      log.shaderModules.push({ label: desc.label, code: desc.code });
      return { label: desc.label, code: desc.code };
    },
    createBindGroupLayout(desc) {
      log.bindGroupLayouts.push({
        label: desc.label,
        hasDynamicOffset: Boolean(desc.entries && desc.entries[0] && desc.entries[0].buffer && desc.entries[0].buffer.hasDynamicOffset),
        minBindingSize: desc.entries && desc.entries[0] && desc.entries[0].buffer ? desc.entries[0].buffer.minBindingSize : 0,
      });
      return { label: desc.label };
    },
    createPipelineLayout() { return {}; },
    createBindGroup() { return {}; },
    createRenderPipeline(desc) {
      log.pipelines.push({
        label: desc.label,
        targets: desc.fragment ? desc.fragment.targets : [],
        cullMode: desc.primitive ? desc.primitive.cullMode : "",
        depthCompare: desc.depthStencil ? desc.depthStencil.depthCompare : "",
        depthWriteEnabled: desc.depthStencil ? desc.depthStencil.depthWriteEnabled : null,
        vertexBuffers: desc.vertex ? desc.vertex.buffers : [],
      });
      return { label: desc.label };
    },
    createBuffer: makeBuffer,
    createTexture(desc) {
      const texture = {
        label: desc.label || "",
        format: desc.format,
        size: desc.size,
        usage: desc.usage,
        destroyed: false,
        createView() { return { format: desc.format }; },
        destroy() { texture.destroyed = true; },
      };
      log.textures.push(texture);
      return texture;
    },
    createCommandEncoder() {
      return {
        beginRenderPass: makePass,
        copyTextureToBuffer(source, destination, size) {
          log.copies.push({
            texture: source.texture && source.texture.label,
            origin: source.origin,
            bytesPerRow: destination.bytesPerRow,
            rowsPerImage: destination.rowsPerImage,
            buffer: destination.buffer,
            size,
          });
        },
        finish() { return {}; },
      };
    },
    queue: {
      writeBuffer(buffer, offset, data, dataOffset, size) {
        const floats = data instanceof Float32Array ? data : new Float32Array(0);
        const words = new Uint32Array(floats.buffer.slice(0));
        log.writes.push({
          label: buffer.label,
          offset,
          size: size === undefined ? floats.length : size,
          // Base ID of slot i lives at word (i * 64) + 16 (byte 256*i + 64).
          slotID(i) { return words[i * 64 + 16]; },
        });
      },
      submit() {},
    },
  };

  return {
    device,
    log,
    // resolveMaps releases every queued mapAsync promise, simulating the GPU
    // finishing the copy.
    resolveMaps() {
      const resolvers = pendingMapResolvers;
      pendingMapResolvers = [];
      for (const resolve of resolvers) resolve();
    },
    pendingMapCount() { return pendingMapResolvers.length; },
  };
}

// --- Bundle-shaped VM context ----------------------------------------------

function createContext() {
  const sandbox = {
    console: { warn() {}, log() {}, error() {} },
    Math,
    JSON,
    Number,
    Object,
    Array,
    String,
    Boolean,
    Float32Array,
    Uint32Array,
    Uint8Array,
    ArrayBuffer,
    DataView,
    Promise,
    Error,
    performance: { now: () => 0 },
    GPUBufferUsage: { UNIFORM: 1, COPY_DST: 2, MAP_READ: 4, VERTEX: 8, STORAGE: 16, COPY_SRC: 32, INDIRECT: 64 },
    GPUTextureUsage: { RENDER_ATTACHMENT: 1, COPY_SRC: 2, TEXTURE_BINDING: 4, COPY_DST: 8, STORAGE_BINDING: 16 },
    GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 },
    GPUMapMode: { READ: 1, WRITE: 2 },
  };
  sandbox.window = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.__gosx_scene3d_api = {};
  sandbox.setTimeout = (fn) => { fn(); return 0; };
  sandbox.clearTimeout = () => {};

  const context = vm.createContext(sandbox);

  // Helpers the fragments expect from earlier files in the bundle. Kept minimal
  // and faithful: sceneRenderCamera and sceneOrthographicBounds are copied from
  // 10-runtime-scene-core.js so sceneScreenToRay produces production rays.
  const prelude = `
    function sceneNumber(value, fallback) {
      var n = Number(value);
      return Number.isFinite(n) ? n : fallback;
    }
    function sceneBool(value, fallback) { return value == null ? fallback : !!value; }
    function normalizeSceneCameraKind(value, fallback) {
      var text = typeof value === "string" ? value.trim().toLowerCase() : "";
      return text === "orthographic" ? "orthographic" : fallback;
    }
    function normalizeSceneKind(value) {
      return typeof value === "string" ? value.trim().toLowerCase() : "box";
    }
    function sceneRenderCamera(camera) {
      return {
        kind: normalizeSceneCameraKind(camera && camera.kind, "perspective"),
        x: sceneNumber(camera && camera.x, 0),
        y: sceneNumber(camera && camera.y, 0),
        z: sceneNumber(camera && camera.z, 6),
        rotationX: sceneNumber(camera && camera.rotationX, 0),
        rotationY: sceneNumber(camera && camera.rotationY, 0),
        rotationZ: sceneNumber(camera && camera.rotationZ, 0),
        fov: sceneNumber(camera && camera.fov, 75),
        left: sceneNumber(camera && camera.left, 0),
        right: sceneNumber(camera && camera.right, 0),
        top: sceneNumber(camera && camera.top, 0),
        bottom: sceneNumber(camera && camera.bottom, 0),
        zoom: Math.max(0.0001, sceneNumber(camera && camera.zoom, 1)),
        near: sceneNumber(camera && camera.near, 0.05),
        far: sceneNumber(camera && camera.far, 128),
      };
    }
    function sceneOrthographicBounds(camera, width, height) {
      var cam = sceneRenderCamera(camera);
      var aspect = Math.max(0.0001, sceneNumber(width, 1) / Math.max(1, sceneNumber(height, 1)));
      var left = cam.left, right = cam.right, top = cam.top, bottom = cam.bottom;
      if (Math.abs(right - left) <= 0.000001 || Math.abs(top - bottom) <= 0.000001) {
        var hh = 3, hw = 3 * aspect;
        left = -hw; right = hw; top = hh; bottom = -hh;
      }
      var zoom = Math.max(0.0001, cam.zoom);
      var cx = (left + right) * 0.5, cy = (top + bottom) * 0.5;
      var halfW = Math.max(0.000001, Math.abs(right - left) * 0.5 / zoom);
      var halfH = Math.max(0.000001, Math.abs(top - bottom) * 0.5 / zoom);
      return { left: cx - halfW, right: cx + halfW, top: cy + halfH, bottom: cy - halfH };
    }
    function queueInputSignal() {}
    function sceneProjectPoint() { return null; }
    function clamp01(v) { return Math.max(0, Math.min(1, Number(v) || 0)); }
  `;

  vm.runInContext(prelude, context, { filename: "prelude.js" });
  vm.runInContext(readSource("11-scene-math.js"), context, { filename: "11-scene-math.js" });
  vm.runInContext(readSource("17-scene-input.js"), context, { filename: "17-scene-input.js" });
  vm.runInContext(readSource("../runtime/scene3d/webgpu.ts"), context, { filename: "webgpu.ts" });
  return { context, sandbox };
}

function api(sandbox) {
  return sandbox.__gosx_scene3d_api;
}

// plain strips the VM realm's prototypes so node:assert/strict can compare a
// value the sandbox built against a literal this file built.
function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

// A minimal two-triangle quad centred at the origin in the XY plane, facing +Z.
// The default camera sits at z = 6 looking down -Z, so a ray through the centre
// of the viewport hits it.
function quadBundle(extra = {}) {
  const positions = new Float32Array([
    -1, -1, 0, 1, -1, 0, 1, 1, 0,
    -1, -1, 0, 1, 1, 0, -1, 1, 0,
  ]);
  const uvs = new Float32Array([
    0, 0, 1, 0, 1, 1,
    0, 0, 1, 1, 0, 1,
  ]);
  return Object.assign({
    camera: { x: 0, y: 0, z: 6, fov: 75 },
    worldMeshPositions: positions,
    worldMeshUVs: uvs,
    meshObjects: [{
      id: "quad",
      kind: "box",
      pickable: true,
      vertexOffset: 0,
      vertexCount: 6,
      bounds: { minX: -1, minY: -1, minZ: 0, maxX: 1, maxY: 1, maxZ: 0 },
    }],
    instancedMeshes: [],
    objects: [],
  }, extra);
}

// --- Pickability drift guard ------------------------------------------------

test("sceneWebGPUPickAllowsObject agrees with the WebGL pick predicate", () => {
  const { context, sandbox } = createContext();
  const cases = [
    null,
    {},
    { viewCulled: true, pickable: true },
    { pickable: true },
    { pickable: false, bounds: { minX: -5, maxX: 5, minY: -5, maxY: 5, minZ: -5, maxZ: 5 } },
    { kind: "plane", bounds: { minX: -5, maxX: 5, minY: -5, maxY: 5, minZ: 0, maxZ: 0 } },
    { kind: "box", bounds: { minX: -1, maxX: 1, minY: -1, maxY: 1, minZ: -1, maxZ: 1 } },
    { kind: "box", bounds: { minX: 0, maxX: 0.05, minY: 0, maxY: 0.01, minZ: 0, maxZ: 0 } },
    { kind: "box", bounds: { minX: 0, maxX: 0.5, minY: 0, maxY: 0.05, minZ: 0, maxZ: 0 } },
    { kind: "box" },
  ];
  sandbox.__pickCases = cases;
  const got = vm.runInContext(
    "__pickCases.map(function(c) { return [sceneObjectAllowsPointerPick(c), sceneWebGPUPickAllowsObject(c)]; })",
    context,
  );
  for (let i = 0; i < got.length; i += 1) {
    assert.equal(
      got[i][1],
      got[i][0],
      `case ${i} diverged: WebGL predicate ${got[i][0]}, WebGPU predicate ${got[i][1]}`,
    );
  }
});

// --- ID plan ----------------------------------------------------------------

test("sceneWebGPUPickIDPlan reserves 0 for background and starts at 1", () => {
  const { sandbox } = createContext();
  const plan = api(sandbox).sceneWebGPUPickIDPlan(quadBundle());
  assert.equal(plan.entries.length, 1);
  assert.equal(plan.entries[0].id, 1);
  assert.equal(plan.entries[0].group, "mesh");
  assert.equal(plan.entries[0].index, 0);
  assert.equal(plan.entries[0].count, 1);
  assert.equal(plan.nextID, 2);
  assert.equal(plan.meshBases[0], 1);
});

test("sceneWebGPUPickIDPlan orders mesh objects before instanced meshes", () => {
  const { sandbox } = createContext();
  const bundle = quadBundle({
    instancedMeshes: [{
      id: "cubes",
      kind: "box",
      pickable: true,
      count: 3,
      width: 1, height: 1, depth: 1,
      transforms: new Float32Array(48),
    }],
  });
  const plan = api(sandbox).sceneWebGPUPickIDPlan(bundle);
  assert.equal(plan.entries.length, 2);
  assert.deepEqual(
    plain(plan.entries.map((e) => [e.group, e.id, e.count])),
    [["mesh", 1, 1], ["instanced", 2, 3]],
  );
  assert.equal(plan.nextID, 5);
});

test("sceneWebGPUPickIDPlan skips objects the WebGL pick path would skip", () => {
  const { sandbox } = createContext();
  const bundle = quadBundle({
    meshObjects: [
      { id: "hidden", pickable: true, viewCulled: true, vertexOffset: 0, vertexCount: 6 },
      { id: "opted-out", pickable: false, vertexOffset: 0, vertexCount: 6 },
      { id: "empty", pickable: true, vertexOffset: 0, vertexCount: 0 },
      { id: "good", pickable: true, vertexOffset: 0, vertexCount: 6 },
    ],
    instancedMeshes: [
      { id: "no-transforms", pickable: true, count: 4 },
      { id: "zero-count", pickable: true, count: 0, transforms: new Float32Array(16) },
    ],
  });
  const plan = api(sandbox).sceneWebGPUPickIDPlan(bundle);
  assert.equal(plan.entries.length, 1);
  assert.equal(plan.entries[0].index, 3);
  assert.equal(plan.entries[0].id, 1);
  assert.deepEqual(Array.from(plan.meshBases), [0, 0, 0, 1]);
});

test("sceneWebGPUPickIDPlan clamps instance count to the transform array", () => {
  const { sandbox } = createContext();
  const bundle = quadBundle({
    meshObjects: [],
    instancedMeshes: [{
      id: "claims-ten",
      pickable: true,
      count: 10,
      transforms: new Float32Array(32), // room for 2 matrices only
    }],
  });
  const plan = api(sandbox).sceneWebGPUPickIDPlan(bundle);
  assert.equal(plan.entries[0].count, 2);
  assert.equal(plan.nextID, 3);
});

// --- ID lookup --------------------------------------------------------------

test("sceneWebGPUPickEntryForID maps every ID in an instance span", () => {
  const { sandbox } = createContext();
  const scene = api(sandbox);
  const bundle = quadBundle({
    meshObjects: [],
    instancedMeshes: [{
      id: "cubes", pickable: true, count: 3, transforms: new Float32Array(48),
    }],
  });
  const plan = scene.sceneWebGPUPickIDPlan(bundle);
  for (let i = 0; i < 3; i += 1) {
    const found = scene.sceneWebGPUPickEntryForID(plan, 1 + i);
    assert.equal(found.instanceIndex, i, `ID ${1 + i} should be instance ${i}`);
    assert.equal(found.entry.group, "instanced");
  }
  assert.equal(scene.sceneWebGPUPickEntryForID(plan, 0), null, "ID 0 is background");
  assert.equal(scene.sceneWebGPUPickEntryForID(plan, 4), null, "ID past the span is unknown");
});

// --- Parity with the shared CPU pick ---------------------------------------

test("sceneWebGPUPickResolve reproduces the WebGL hit record for a mesh object", () => {
  const { context, sandbox } = createContext();
  const scene = api(sandbox);
  const bundle = quadBundle();
  sandbox.__bundle = bundle;
  sandbox.__w = 400;
  sandbox.__h = 300;
  sandbox.__px = 200;
  sandbox.__py = 150;

  const cpu = vm.runInContext(
    "sceneRaycastPick(__px, __py, __w, __h, __bundle.camera, __bundle)",
    context,
  );
  assert.ok(cpu, "the CPU pick must hit the quad; the fixture is wrong otherwise");

  const ray = vm.runInContext("sceneScreenToRay(__px, __py, __w, __h, __bundle.camera)", context);
  const plan = scene.sceneWebGPUPickIDPlan(bundle);
  const gpu = scene.sceneWebGPUPickResolve(bundle, plan, 1, ray);

  assert.ok(gpu, "the GPU pick must resolve ID 1 to the quad");
  for (const key of [
    "index", "distance", "inside", "depth",
    "instanceIndex", "primitiveIndex", "triangleIndex",
  ]) {
    assert.deepEqual(gpu[key], cpu[key], `field ${key} diverged`);
  }
  for (const key of ["worldPosition", "localPosition", "point", "uv"]) {
    assert.deepEqual(gpu[key], cpu[key], `field ${key} diverged`);
  }
  assert.equal(gpu.object.id, cpu.object.id);
  // The world-space ray fields must be present, because $surface.event.rayOriginX
  // and friends read them through sceneTargetHitSnapshot.
  assert.deepEqual(gpu.ray.origin, cpu.ray.origin);
  assert.deepEqual(gpu.ray.dir, cpu.ray.dir);
});

test("sceneWebGPUPickResolve reports the GPU-selected instance, not the nearest sphere", () => {
  const { context, sandbox } = createContext();
  const scene = api(sandbox);
  // Two instances on the same ray. Instance 0 sits nearer the camera, so the
  // CPU nearest-sphere search would always answer 0. The GPU can legitimately
  // answer 1 (for example when instance 0 is a hollow shell the ray misses), and
  // the resolver must honour that.
  const transforms = new Float32Array(32);
  for (const [slot, z] of [[0, 2], [1, -2]]) {
    const base = slot * 16;
    transforms[base + 0] = 1;
    transforms[base + 5] = 1;
    transforms[base + 10] = 1;
    transforms[base + 15] = 1;
    transforms[base + 14] = z;
  }
  const bundle = quadBundle({
    meshObjects: [],
    worldMeshPositions: new Float32Array(0),
    instancedMeshes: [{
      id: "pair",
      kind: "sphere",
      radius: 0.5,
      pickable: true,
      count: 2,
      transforms,
      bounds: { minX: -1, maxX: 1, minY: -1, maxY: 1, minZ: -3, maxZ: 3 },
    }],
  });
  sandbox.__bundle = bundle;
  sandbox.__w = 400;
  sandbox.__h = 300;
  sandbox.__px = 200;
  sandbox.__py = 150;
  const ray = vm.runInContext("sceneScreenToRay(__px, __py, __w, __h, __bundle.camera)", context);
  const plan = scene.sceneWebGPUPickIDPlan(bundle);

  const first = scene.sceneWebGPUPickResolve(bundle, plan, 1, ray);
  const second = scene.sceneWebGPUPickResolve(bundle, plan, 2, ray);
  assert.equal(first.instanceIndex, 0);
  assert.equal(second.instanceIndex, 1);
  assert.equal(first.object.id, "pair");
  assert.equal(second.object.id, "pair");
  assert.ok(second.distance > first.distance, "instance 1 sits behind instance 0");
  // The clone used for refinement must not leak into the reported object.
  assert.equal(second.object.count, 2);
  assert.equal(second.object.transforms.length, 32);
});

test("sceneWebGPUPickResolve returns null for a background ID with nothing else under the ray", () => {
  const { context, sandbox } = createContext();
  const scene = api(sandbox);
  const bundle = quadBundle();
  sandbox.__bundle = bundle;
  sandbox.__w = 400;
  sandbox.__h = 300;
  sandbox.__px = 0;
  sandbox.__py = 0;
  const ray = vm.runInContext("sceneScreenToRay(__px, __py, __w, __h, __bundle.camera)", context);
  const plan = scene.sceneWebGPUPickIDPlan(bundle);
  assert.equal(scene.sceneWebGPUPickResolve(bundle, plan, 0, ray), null);
});

test("sceneWebGPUPickResolve merges the CPU-only objects group by nearest distance", () => {
  const { context, sandbox } = createContext();
  const scene = api(sandbox);
  // bundle.objects over bundle.worldPositions has no ID-buffer draw: the WebGPU
  // renderer rasterizes worldPositions as a line list. The resolver must still
  // consider it, exactly like sceneRaycastPick's third group.
  const bundle = quadBundle({
    meshObjects: [],
    worldMeshPositions: new Float32Array(0),
    objects: [{
      id: "legacy",
      kind: "box",
      pickable: true,
      vertexOffset: 0,
      vertexCount: 3,
      bounds: { minX: -1, minY: -1, minZ: 0, maxX: 1, maxY: 1, maxZ: 0 },
    }],
    worldPositions: new Float32Array([-1, -1, 0, 1, -1, 0, 0, 1, 0]),
  });
  sandbox.__bundle = bundle;
  sandbox.__w = 400;
  sandbox.__h = 300;
  sandbox.__px = 200;
  sandbox.__py = 150;
  const ray = vm.runInContext("sceneScreenToRay(__px, __py, __w, __h, __bundle.camera)", context);
  const cpu = vm.runInContext("sceneRaycastPick(__px, __py, __w, __h, __bundle.camera, __bundle)", context);
  const plan = scene.sceneWebGPUPickIDPlan(bundle);
  const gpu = scene.sceneWebGPUPickResolve(bundle, plan, 0, ray);
  assert.ok(cpu, "the CPU pick must hit the legacy triangle");
  assert.ok(gpu, "the resolver must merge the CPU-only group even for ID 0");
  assert.equal(gpu.object.id, "legacy");
  assert.deepEqual(gpu.point, cpu.point);
  assert.deepEqual(gpu.triangleIndex, cpu.triangleIndex);
});

// --- Picker pass + readback -------------------------------------------------

function makePicker(sandbox, gpu, bundle) {
  const geometry = {
    positions: new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]),
    vertexCount: 3,
  };
  return api(sandbox).createSceneWebGPUPicker(gpu.device, {
    viewProjection: () => new Float32Array(16),
    meshPositions: () => ({ buffer: { label: "mesh-positions" }, components: 3 }),
    bindMeshPositions: () => true,
    instancedGeometry: () => geometry,
    instancedGeometryBuffer: () => ({ label: "instanced-positions" }),
    instancedTransformBuffer: () => ({ label: "instanced-transforms" }),
    instancedCount: (mesh) => Math.floor(mesh.count || 0),
    instancedTransforms: (mesh) => mesh.transforms || null,
    onError: () => {},
  });
}

test("the pick pass writes an r32uint attachment and scissors to the pointer pixel", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);

  assert.equal(picker.hasPending(), false);
  assert.equal(picker.queuePick(100, 75, 400, 300, () => {}), true);
  assert.equal(picker.hasPending(), true);

  const encoder = gpu.device.createCommandEncoder();
  assert.equal(picker.recordPickPass(encoder, bundle, 800, 600), true);

  const pass = gpu.log.passes[0];
  assert.deepEqual(plain(pass.colorFormats), ["r32uint"], "the ID attachment must be r32uint");
  assert.equal(pass.loadOp, "clear", "the ID buffer must clear to 0 (background) every pick");
  assert.equal(pass.depthLoadOp, "clear");
  // 100/400 * 800 = 200; 75/300 * 600 = 150.
  assert.deepEqual(pass.scissor, [200, 150, 1, 1]);
  assert.equal(pass.ended, true);
  assert.deepEqual(pass.draws, [[6, 1]], "the quad draws its 6 vertices once");

  const copy = gpu.log.copies[0];
  assert.equal(copy.texture, "gosx-pick-id");
  assert.deepEqual(plain(copy.origin), { x: 200, y: 150, z: 0 });
  assert.equal(copy.bytesPerRow, 256, "WebGPU requires a 256-byte row alignment");
  assert.equal(copy.rowsPerImage, 1);
  assert.deepEqual(plain(copy.size), { width: 1, height: 1, depthOrArrayLayers: 1 });
});

test("the pick pipelines match the main pass depth and cull state", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(100, 75, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 800, 600);

  const labels = gpu.log.pipelines.map((p) => p.label);
  assert.deepEqual(plain(labels), ["gosx-pick", "gosx-pick-instanced"]);
  for (const pipeline of gpu.log.pipelines) {
    assert.deepEqual(plain(pipeline.targets), [{ format: "r32uint" }]);
    // wgpuCreatePBRPipeline uses cullMode "none" and depthCompare "less-equal".
    // The pick pass must agree or it resolves a different surface.
    assert.equal(pipeline.cullMode, "none");
    assert.equal(pipeline.depthCompare, "less-equal");
    assert.equal(pipeline.depthWriteEnabled, true);
  }
  const fragment = gpu.log.shaderModules.find((m) => m.label === "pick-frag");
  assert.match(fragment.code, /-> @location\(0\) u32/, "the fragment must output a u32 ID");
  const instanced = gpu.log.shaderModules.find((m) => m.label === "pick-instanced-vert");
  assert.match(instanced.code, /pick\.baseID \+ instance/, "instances must stamp baseID + instance_index");
});

test("the readback resolves the callback with the hit for the read-back ID", async () => {
  const { context, sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);
  sandbox.__bundle = bundle;

  let result = "unset";
  picker.queuePick(200, 150, 400, 300, (hit) => { result = hit; });
  const encoder = gpu.device.createCommandEncoder();
  picker.recordPickPass(encoder, bundle, 400, 300);

  // Stage ID 1 in the copy target, as the GPU would.
  const staging = gpu.log.copies[0].buffer;
  new Uint32Array(staging.bytes.buffer)[0] = 1;

  assert.equal(picker.finishReadback(), true);
  assert.equal(result, "unset", "the callback must not run before the map resolves");
  gpu.resolveMaps();
  await Promise.resolve();
  await Promise.resolve();

  assert.ok(result, "the callback must receive a hit for ID 1");
  assert.equal(result.object.id, "quad");
  const cpu = vm.runInContext(
    "sceneRaycastPick(200, 150, 400, 300, __bundle.camera, __bundle)",
    context,
  );
  assert.deepEqual(result.point, cpu.point);
  assert.deepEqual(result.triangleIndex, cpu.triangleIndex);
  assert.equal(staging.destroyed, true, "the staging buffer must be released");
  assert.equal(picker.hasPending(), false);
  assert.equal(picker.diagnostics().pickLastID, 1);
});

test("a background ID resolves the callback with null", async () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);

  let called = false;
  let result = "unset";
  picker.queuePick(200, 150, 400, 300, (hit) => { called = true; result = hit; });
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);
  // Leave the staging buffer at 0 — the cleared ID attachment.
  picker.finishReadback();
  gpu.resolveMaps();
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(called, true);
  assert.equal(result, null);
});

test("an out-of-bounds pick reports background without recording a pass", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);

  let result = "unset";
  picker.queuePick(9999, 150, 400, 300, (hit) => { result = hit; });
  assert.equal(picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300), false);
  assert.equal(result, null);
  assert.equal(gpu.log.passes.length, 0, "no pass may be recorded for an off-surface pick");
  assert.equal(picker.hasPending(), false);
});

test("a newer pick replaces an unsubmitted one and only the newest callback fires", async () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);

  const calls = [];
  picker.queuePick(10, 10, 400, 300, () => calls.push("stale"));
  picker.queuePick(200, 150, 400, 300, () => calls.push("fresh"));
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);
  new Uint32Array(gpu.log.copies[0].buffer.bytes.buffer)[0] = 1;
  picker.finishReadback();
  gpu.resolveMaps();
  await Promise.resolve();
  await Promise.resolve();

  assert.deepEqual(calls, ["fresh"], "the superseded request must not call back");
  assert.equal(picker.diagnostics().pickDrops, 1);
  assert.equal(gpu.log.passes.length, 1, "only one pass for two queued picks");
});

test("instanced meshes draw one instanced call carrying every instance ID", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle({
    meshObjects: [],
    instancedMeshes: [{
      id: "cubes", pickable: true, count: 3, transforms: new Float32Array(48),
    }],
  });
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(200, 150, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);

  const pass = gpu.log.passes[0];
  assert.deepEqual(pass.draws, [[3, 3]], "3 geometry vertices across 3 instances");
  assert.deepEqual(pass.pipelines, ["gosx-pick-instanced"]);
  // The base ID uniform must carry the plan's base for the mesh.
  const uniformWrite = gpu.log.writes[gpu.log.writes.length - 1];
  assert.equal(uniformWrite.slotID(0), 1, "baseID must be the plan's first ID");
});

test("dispose releases the pick textures and the uniform buffer", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(200, 150, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);
  picker.dispose();

  const idTexture = gpu.log.textures.find((t) => t.label === "gosx-pick-id");
  const depthTexture = gpu.log.textures.find((t) => t.label === "gosx-pick-depth");
  assert.equal(idTexture.destroyed, true);
  assert.equal(depthTexture.destroyed, true);
  assert.equal(picker.hasPending(), false);
});

test("the ID texture allocates COPY_SRC so the 1x1 readback is legal", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(200, 150, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);

  const idTexture = gpu.log.textures.find((t) => t.label === "gosx-pick-id");
  assert.equal(idTexture.format, "r32uint");
  assert.ok(idTexture.usage & sandbox.GPUTextureUsage.COPY_SRC, "the ID texture needs COPY_SRC");
  assert.ok(idTexture.usage & sandbox.GPUTextureUsage.RENDER_ATTACHMENT, "the ID texture needs RENDER_ATTACHMENT");

  const staging = gpu.log.copies[0].buffer;
  assert.ok(staging.usage & sandbox.GPUBufferUsage.MAP_READ, "the staging buffer needs MAP_READ");
  assert.ok(staging.usage & sandbox.GPUBufferUsage.COPY_DST, "the staging buffer needs COPY_DST");
  assert.equal(staging.size, 256);
});

// Regression guard. An earlier draft wrote the base ID with queue.writeBuffer
// once per draw into a single-slot uniform buffer. queue.writeBuffer lands
// BEFORE the submitted commands run, so every draw in the pass read the LAST
// base ID and every object reported the last object's identity. The fix is one
// 256-byte uniform slot per draw plus a dynamic offset, uploaded in one write
// before the pass opens.
test("each pick draw reads its own base ID through a dynamic offset", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle({
    worldMeshPositions: new Float32Array(18),
    worldMeshUVs: new Float32Array(12),
    meshObjects: [
      { id: "a", kind: "box", pickable: true, vertexOffset: 0, vertexCount: 3 },
      { id: "b", kind: "box", pickable: true, vertexOffset: 3, vertexCount: 3 },
      { id: "c", kind: "box", pickable: true, vertexOffset: 6, vertexCount: 3 },
    ],
  });
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(200, 150, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);

  const layout = gpu.log.bindGroupLayouts[0];
  assert.equal(layout.hasDynamicOffset, true, "the pick bind group needs a dynamic offset");
  assert.equal(layout.minBindingSize, 144, "PickUniforms is 2 mat4x4f + 4 u32 = 144 bytes");

  // Exactly one upload for the whole pass.
  const uniformWrites = gpu.log.writes.filter((w) => w.label === "gosx-pick-uniforms");
  assert.equal(uniformWrites.length, 1, "all slots must upload in one writeBuffer");

  // Distinct base IDs, one per object, in plan order.
  assert.equal(uniformWrites[0].slotID(0), 1);
  assert.equal(uniformWrites[0].slotID(1), 2);
  assert.equal(uniformWrites[0].slotID(2), 3);

  // Distinct dynamic offsets, 256 bytes apart, one per draw.
  const pass = gpu.log.passes[0];
  assert.equal(pass.draws.length, 3);
  assert.deepEqual(pass.drawOffsets, [[0], [256], [512]]);
});

test("a plan larger than the uniform slot cap drops draws instead of misdrawing", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  // 4097 pickable mesh objects: one past SCENE_WEBGPU_PICK_MAX_SLOTS.
  const meshObjects = [];
  for (let i = 0; i < 4097; i += 1) {
    meshObjects.push({ id: "m" + i, kind: "box", pickable: true, vertexOffset: 0, vertexCount: 3 });
  }
  const bundle = quadBundle({ meshObjects });
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(200, 150, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);

  const pass = gpu.log.passes[0];
  assert.equal(pass.draws.length, 4096, "draws must stop at the slot cap");
  assert.equal(picker.diagnostics().pickSkippedDraws, 1, "the dropped draw must be counted");
  // The last recorded offset must still be inside the buffer.
  assert.deepEqual(pass.drawOffsets[4095], [4095 * 256]);
});

// Regression guard. An earlier draft dropped a superseded-but-submitted request
// on the floor: `pending` moved to the new request, so finishReadback never
// mapped the old staging buffer and it leaked one 256-byte MAP_READ buffer per
// superseded pick. Superseded requests now go to a retired list that
// finishReadback drains.
test("a superseded submitted pick still frees its staging buffer", async () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);

  const calls = [];
  picker.queuePick(200, 150, 400, 300, () => calls.push("first"));
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);
  const firstStaging = gpu.log.copies[0].buffer;

  // Supersede AFTER the copy is submitted but BEFORE the readback starts.
  picker.queuePick(100, 100, 400, 300, () => calls.push("second"));
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);
  const secondStaging = gpu.log.copies[1].buffer;
  assert.notEqual(firstStaging, secondStaging);

  picker.finishReadback();
  gpu.resolveMaps();
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(firstStaging.destroyed, true, "the superseded staging buffer must be freed");
  assert.equal(secondStaging.destroyed, true);
  assert.deepEqual(calls, ["second"], "only the newest pick may call back");
});

test("dispose frees a staging buffer that never got read", () => {
  const { sandbox } = createContext();
  const gpu = createFakeGPU();
  const bundle = quadBundle();
  const picker = makePicker(sandbox, gpu, bundle);
  picker.queuePick(200, 150, 400, 300, () => {});
  picker.recordPickPass(gpu.device.createCommandEncoder(), bundle, 400, 300);
  const staging = gpu.log.copies[0].buffer;
  picker.dispose();
  assert.equal(staging.destroyed, true, "dispose must free an unread staging buffer");
});
