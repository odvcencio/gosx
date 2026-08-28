"use strict";

// Focused WebGPU masked-shadow regression tests for the Scene3D WebGPU
// backend. Every behavior under test executes the ACTUAL production source
// sliced out of client/runtime/scene3d/webgpu.ts and the bootstrap scene
// fragments inside a node:vm context:
//
//   - shadowObjectMaterial / shadowMaskedCutoff (real material resolution and
//     the real cutoff path including sceneNormalizeMaterialAlphaCutoff and
//     the production sceneCSSVarReference check)
//   - wgpuCreateShadowMaskedPipeline / wgpuCreateShadowMaskedInstancedPipeline
//     (descriptor contract) with the original opaque layouts checked for
//     non-mutation and attribute shader locations cross-checked against the
//     actual masked WGSL blocks
//   - renderShadowPass and drawInstancedShadowMeshes driven through a
//     recording encoder/pass/device
//
// Only ports are stubbed: pipeline getters, cached GPU buffer uploads,
// skinning/morph eligibility, retained binders, material bind-group creation
// and instanced mesh count/geometry lookup. Material lookup, the cutoff
// normalizer, matrix multiplication, the soup vertex binder and the masked
// pipeline factories are the real code. No Proxy fakes, no permissive
// catches, no conditional skips. Native receiver-pixel probes validate
// rendered pixels separately; these tests deliberately make NO native
// instancing certification claims.

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const runtimeDir = path.join(__dirname, "..", "runtime", "scene3d");
const srcDir = path.join(__dirname, "bootstrap-src");

const WEBGPU_SOURCE = fs.readFileSync(path.join(runtimeDir, "webgpu.ts"), "utf8");

const BOOTSTRAP_SOURCES = [
  "10-runtime-primitives.ts",
  "10-runtime-scene-utils.ts",
  "10-runtime-scene-core.ts",
  "11-scene-math.ts",
  "12-scene-geometry.ts",
  "13-scene-material.ts",
].map((name) => [name, fs.readFileSync(path.join(srcDir, name), "utf8")]);

// Recording constants are sufficient; only VERTEX | COPY_DST arithmetic is
// observed by the production code under test.
const GPUBufferUsage = {
  MAP_READ: 0x0001,
  MAP_WRITE: 0x0002,
  COPY_SRC: 0x0004,
  COPY_DST: 0x0008,
  INDEX: 0x0010,
  VERTEX: 0x0020,
  UNIFORM: 0x0040,
  STORAGE: 0x0080,
};

// ---------------------------------------------------------------------------
// Source slicing helpers. Function bodies are located by their
// "function NAME(" markers independent of surrounding whitespace — the
// production indentation of shadowObjectMaterial deliberately differs, so no
// indentation assumptions are made anywhere.

function sliceFunction(source, name, label) {
  const marker = "function " + name + "(";
  const start = source.indexOf(marker);
  assert.ok(start >= 0, (label || name) + " function marker located");
  const open = source.indexOf("{", start);
  assert.ok(open > start, (label || name) + " body opening brace located");
  let depth = 0;
  for (let i = open; i < source.length; i++) {
    if (source[i] === "{") {
      depth += 1;
    } else if (source[i] === "}") {
      depth -= 1;
      if (depth === 0) return source.slice(start, i + 1);
    }
  }
  assert.fail((label || name) + " body closing brace located");
}

function findSceneFunction(name) {
  for (const [fileName, source] of BOOTSTRAP_SOURCES) {
    if (source.indexOf("function " + name + "(") >= 0) {
      return sliceFunction(source, name, fileName);
    }
  }
  assert.fail("scene helper function located: " + name);
}

function sliceBetween(source, startMarker, endMarker, label) {
  const start = source.indexOf(startMarker);
  assert.ok(start >= 0, label + " start marker located");
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.ok(end > start, label + " end marker located");
  return source.slice(start, end);
}

function sliceVarBlock(source, varName, label) {
  return sliceBetween(source, "var " + varName, '].join("\\n");', label);
}

// Layout block: opaque, opaque instanced and the dedicated masked layouts
// (separate arrays that never alias the opaque ones).
const LAYOUTS_SOURCE = sliceBetween(
  WEBGPU_SOURCE,
  "var WGPU_SHADOW_VERTEX_LAYOUT",
  "var WGPU_SCENE_COLOR_VERTEX_LAYOUT",
  "shadow vertex layout block"
);

const WGSL_MASKED_VERTEX_BLOCK = sliceVarBlock(
  WEBGPU_SOURCE, "WGSL_SHADOW_MASKED_VERTEX", "masked shadow vertex WGSL");
const WGSL_MASKED_INSTANCED_VERTEX_BLOCK = sliceVarBlock(
  WEBGPU_SOURCE, "WGSL_SHADOW_MASKED_INSTANCED_VERTEX", "masked instanced shadow vertex WGSL");
const WGSL_MASKED_FRAGMENT_BLOCK = sliceVarBlock(
  WEBGPU_SOURCE, "WGSL_SHADOW_MASKED_FRAGMENT", "masked shadow fragment WGSL");

const WEBGPU_FUNCTION_NAMES = [
  "shadowObjectMaterial",
  "shadowMaskedCutoff",
  "webGPUMat4MultiplyInto",
  "webGPUBindSceneMeshVertexBuffer",
  "wgpuCreateShadowPipeline",
  "wgpuCreateShadowInstancedPipeline",
  "wgpuCreateShadowMaskedPipeline",
  "wgpuCreateShadowMaskedInstancedPipeline",
  "renderShadowPass",
  "instancedMeshMaterial",
  "instancedMeshTransformData",
  "instancedMeshColorData",
  "ensureInstancedGeometryGPUBuffer",
  "ensureInstancedTransformGPUBuffer",
  "ensureInstancedColorGPUBuffer",
  "drawInstancedShadowMeshes",
];

// The vm context gets the test-realm typed array constructors so instanceof
// checks never cross realms, but object and array literals created inside the
// vm still carry vm-realm prototypes even with Array/Object injected, so
// structural comparisons round-trip actual values through assertData before
// deepEqual against host expectations.
function createHarness() {
  const context = vm.createContext({
    console,
    GPUBufferUsage,
    Float32Array,
    Uint32Array,
    Number,
    Math,
    Array,
    Object,
    Boolean,
    isNaN,
  });

  // Actual scene helpers under/behind the code under test: number coercion,
  // CSS var detection, the alphaCutoff normalizer and the retained-caster
  // mat4 multiply. No copies, no re-implementations.
  const sceneHelpers = [
    findSceneFunction("sceneNumber"),
    findSceneFunction("sceneCSSVarReference"),
    findSceneFunction("sceneNormalizeMaterialAlphaCutoff"),
    findSceneFunction("sceneMat4MultiplyInto"),
  ].join("\n\n");
  vm.runInContext(sceneHelpers, context, { filename: "scene3d-scene-helpers.ts" });

  const webgpuFunctions = WEBGPU_FUNCTION_NAMES
    .map((name) => sliceFunction(WEBGPU_SOURCE, name))
    .join("\n\n");
  vm.runInContext(
    LAYOUTS_SOURCE + "\n\n" + webgpuFunctions,
    context,
    { filename: "scene3d-webgpu-shadow-functions.ts" }
  );

  return context;
}

// ---------------------------------------------------------------------------
// Recording fakes. Only ports are stubbed; everything else is production.

function createRecordingEnvironment(ctx) {
  const rec = {
    queueWrites: [],
    pipelines: [],
    pipelineLayouts: [],
    materialBindGroups: [],
    cachedUploads: [],
    defaultAttributeCalls: [],
    retainedAttributeBinds: [],
    retainedIndexBinds: [],
    capacityRequests: [],
  };
  const device = {
    queue: {
      writeBuffer(buffer, offset, data, dataOffset, size) {
        const start = dataOffset || 0;
        const end = size === undefined ? data.length : start + size;
        // Snapshot: queue writes are immutable once recorded, so a later draw
        // mutating a shared scratch view cannot rewrite an earlier record.
        rec.queueWrites.push({ buffer, offset, data: Array.from(data.slice(start, end)) });
      },
    },
    createBindGroup(desc) {
      return { __bindGroup: true, layout: desc.layout, entries: desc.entries };
    },
    createPipelineLayout(desc) {
      const layout = { __pipelineLayout: true, bindGroupLayouts: desc.bindGroupLayouts };
      rec.pipelineLayouts.push(layout);
      return layout;
    },
    createRenderPipeline(desc) {
      const pipeline = { __pipeline: true, label: desc.label, descriptor: desc };
      rec.pipelines.push(pipeline);
      return pipeline;
    },
  };

  const opaqueShadowPipeline = { id: "shadow-pipeline" };
  const opaqueInstancedShadowPipeline = { id: "shadow-instanced-pipeline" };
  const maskedShadowPipeline = { id: "shadow-masked-pipeline" };
  const maskedInstancedShadowPipeline = { id: "shadow-masked-instanced-pipeline" };

  ctx.device = device;
  ctx.getShadowPipeline = () => opaqueShadowPipeline;
  ctx.getShadowInstancedPipeline = () => opaqueInstancedShadowPipeline;
  ctx.getShadowMaskedPipeline = () => maskedShadowPipeline;
  ctx.getShadowMaskedInstancedPipeline = () => maskedInstancedShadowPipeline;
  ctx.shadowBindGroupLayout = { id: "shadow-bind-group-layout" };
  ctx.shadowFrameBuffer = { id: "shadow-frame-buffer" };
  ctx.shadowFrameBufferStride = 256;
  ctx._shadowCombinedMatrixScratch = null;
  ctx.ensureShadowFrameBufferCapacity = (count) => {
    rec.capacityRequests.push(count);
    return true;
  };
  ctx.gpuPassTimestampWrites = () => null;
  ctx.createMaterialBindGroup = (material, _isColorPass, resource) => {
    const bindGroup = { id: "material-bind-group", material, resource };
    rec.materialBindGroups.push(bindGroup);
    return bindGroup;
  };
  ctx.wgpuCachedTrackedBuffer = (owner, key, data, usage, tracked) => {
    rec.cachedUploads.push({
      owner, key, data: Array.from(data), source: data, usage, tracked,
    });
    return { id: key };
  };
  ctx.webGPUObjectIsSkinned = (obj) => Boolean(obj && obj.__skinned);
  ctx.webGPUObjectComputedMorphDrawRecord = () => null;
  ctx.webGPUBindComputedMorphBuffer = () => false;
  ctx.webGPUBindRetainedMeshAttribute = (pass, slot, obj, key, components) => {
    rec.retainedAttributeBinds.push({ slot, obj, key, components });
    if (obj.__retainedAttributes && obj.__retainedAttributes[key]) {
      pass.setVertexBuffer(
        slot,
        { id: "retained-" + key },
        0,
        obj.__retainedAttributes[key].length * 4
      );
      return true;
    }
    return false;
  };
  ctx.webGPUBindRetainedMeshIndexBuffer = (pass, obj) => {
    rec.retainedIndexBinds.push(obj);
    const count = obj.__indexCount || 0;
    if (count > 0) pass.setIndexBuffer({ id: "retained-index" }, "uint32");
    return count;
  };
  ctx.webGPUDefaultAttributeData = (obj, key, count, tupleSize, defaults) => {
    rec.defaultAttributeCalls.push({
      obj, key, count, tupleSize,
      defaults: defaults ? Array.from(defaults) : null,
    });
    return new Float32Array(count * tupleSize);
  };
  ctx.instancedMeshCount = (mesh) => mesh.instanceCount;
  ctx.getInstancedGeometry = (mesh) => mesh.__geometry || null;
  ctx.clamp01 = (value) => {
    const n = Number(value);
    return Number.isFinite(n) ? Math.min(1, Math.max(0, n)) : 0;
  };

  return {
    rec,
    device,
    opaqueShadowPipeline,
    opaqueInstancedShadowPipeline,
    maskedShadowPipeline,
    maskedInstancedShadowPipeline,
  };
}

function createRecordingPass() {
  const draws = [];
  const log = [];
  const bindGroups = [null, null, null, null];
  const bindOffsets = [null, null, null, null];
  const vertexBuffers = {};
  let currentPipeline = null;
  function snapshotDraw(kind, count, instanceCount) {
    return {
      kind,
      count,
      instances: instanceCount,
      pipeline: currentPipeline,
      bindGroups: bindGroups.slice(),
      bindOffsets: bindOffsets.map((offsets) => (offsets ? offsets.slice() : null)),
      vertexBuffers: Object.assign({}, vertexBuffers),
      logIndex: log.length,
    };
  }
  const pass = {
    setPipeline(pipelineArg) {
      currentPipeline = pipelineArg;
      log.push(["setPipeline", pipelineArg]);
    },
    setBindGroup(index, bindGroup, dynamicOffsets) {
      bindGroups[index] = bindGroup;
      bindOffsets[index] = Array.from(dynamicOffsets || []);
      log.push(["setBindGroup", index, bindGroup, Array.from(dynamicOffsets || [])]);
    },
    setVertexBuffer(slot, buffer, offset, size) {
      const resolvedOffset = offset === undefined ? 0 : offset;
      vertexBuffers[slot] = { buffer, offset: resolvedOffset, size: size === undefined ? null : size };
      log.push(["setVertexBuffer", slot, buffer, resolvedOffset, size === undefined ? null : size]);
    },
    setIndexBuffer(buffer, format) {
      log.push(["setIndexBuffer", buffer, format]);
    },
    draw(count, instanceCount) {
      draws.push(snapshotDraw("draw", count, instanceCount === undefined ? 1 : instanceCount));
    },
    drawIndexed(count) {
      draws.push(snapshotDraw("drawIndexed", count, 1));
    },
    end() {
      log.push(["end"]);
    },
  };
  return { pass, draws, log };
}

function runShadowPass(ctx, bundle, lightMatrix, pbrBuffers, shadowResource) {
  const { pass, draws, log } = createRecordingPass();
  let passDescriptor = null;
  const encoder = {
    beginRenderPass(descriptor) {
      passDescriptor = descriptor;
      log.push(["beginRenderPass", descriptor]);
      return pass;
    },
  };
  const result = ctx.renderShadowPass(
    encoder,
    lightMatrix,
    bundle,
    shadowResource || { view: { id: "shadow-view" } },
    pbrBuffers || null
  );
  return { draws, log, passDescriptor, result };
}

// Independent column-major oracle used only to compute expected matrices for
// the queue-write assertions (the production multiply itself is the real
// sceneMat4MultiplyInto under test).
function multiplyMat4Oracle(a, b) {
  const out = new Array(16).fill(0);
  for (let col = 0; col < 4; col++) {
    for (let row = 0; row < 4; row++) {
      for (let k = 0; k < 4; k++) {
        out[col * 4 + row] += a[k * 4 + row] * b[col * 4 + k];
      }
    }
  }
  return out;
}

function maskVertexInputLocations(wgslBlock) {
  const inputStart = wgslBlock.indexOf("struct MaskVertexInput");
  assert.ok(inputStart >= 0, "MaskVertexInput struct located in WGSL block");
  const inputEnd = wgslBlock.indexOf("};", inputStart);
  assert.ok(inputEnd > inputStart, "MaskVertexInput struct end located");
  const segment = wgslBlock.slice(inputStart, inputEnd);
  const locations = [];
  const pattern = /@location\((\d+)\)/g;
  let match;
  while ((match = pattern.exec(segment))) locations.push(Number(match[1]));
  return locations;
}

// VM realm literals keep their own prototypes even when the host Array/Object
// constructors are injected into the context, so structural deepEqual sites
// JSON-round-trip actual descriptor data before comparing.
function assertData(value) {
  return JSON.parse(JSON.stringify(value));
}

// ---------------------------------------------------------------------------

test("shadowObjectMaterial resolves the bundle material by materialIndex and ignores decoy owners", () => {
  const ctx = createHarness();
  const bundleMaterial = { id: "bundle-material" };
  const decoy = {
    materialIndex: 1,
    resourceOwner: { materials: [{ id: "decoy-resource-owner" }] },
    vertices: { material: { id: "decoy-vertices-material" } },
  };
  assert.equal(
    ctx.shadowObjectMaterial({ materials: [{ id: "m0" }, bundleMaterial] }, decoy),
    bundleMaterial,
    "material resolves through bundle.materials at the object's materialIndex"
  );
  assert.equal(ctx.shadowObjectMaterial({ materials: [] }, decoy), null,
    "missing bundle entry resolves to null");
  assert.equal(ctx.shadowObjectMaterial(null, decoy), null,
    "missing bundle resolves to null");
  assert.equal(ctx.shadowObjectMaterial({ materials: [bundleMaterial] }, null), null,
    "missing object resolves to null");
  assert.equal(
    ctx.shadowObjectMaterial({ materials: [bundleMaterial] }, { materialIndex: undefined }),
    bundleMaterial,
    "missing materialIndex falls back to slot zero"
  );
});

test("shadowMaskedCutoff: unmasked for null/false/empty/NaN/CSS var, masked for zero and numeric strings >= 0", () => {
  const ctx = createHarness();
  assert.equal(ctx.shadowMaskedCutoff(null), null, "null material is unmasked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: null }), null, "null cutoff is unmasked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: false }), null, "false cutoff is unmasked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: "" }), null, "empty cutoff is unmasked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: NaN }), null, "NaN cutoff is unmasked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: "not-a-number" }), null,
    "non-numeric string cutoff is unmasked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: "var(--alpha-cutoff, 0.5)" }), null,
    "CSS var cutoff stays unmasked in the shadow path");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: 0 }), 0, "zero cutoff is masked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: "0.25" }), 0.25,
    "numeric string cutoff >= 0 is masked");
  assert.equal(ctx.shadowMaskedCutoff({ alphaCutoff: 1 }), 1, "one cutoff is masked");
});

test("masked shadow pipelines declare depth24plus/front cull/group0 shadow + group1 material/empty targets", () => {
  const ctx = createHarness();
  const env = createRecordingEnvironment(ctx);
  const shadowLayout = { id: "shadow-bgl" };
  const materialLayout = { id: "material-bgl" };
  const vertexModule = { id: "vertex-module" };
  const fragmentModule = { id: "fragment-module" };

  const masked = ctx.wgpuCreateShadowMaskedPipeline(
    env.device, shadowLayout, materialLayout, vertexModule, fragmentModule);
  assert.equal(masked.label, "gosx-shadow-masked");
  const maskedDesc = masked.descriptor;
  assert.deepEqual(Array.from(maskedDesc.layout.bindGroupLayouts), [shadowLayout, materialLayout],
    "pipeline layout carries group 0 shadow and group 1 material layouts");
  assert.equal(maskedDesc.layout.bindGroupLayouts[0], shadowLayout,
    "group 0 is the identical shadow layout object");
  assert.equal(maskedDesc.layout.bindGroupLayouts[1], materialLayout,
    "group 1 is the identical material layout object");
  assert.equal(maskedDesc.vertex.module, vertexModule);
  assert.equal(maskedDesc.vertex.entryPoint, "vertexMain");
  assert.deepEqual(assertData(maskedDesc.vertex.buffers), [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 8, stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 1 }] },
  ], "masked static buffers are position (stride 12, loc 0) + UV (stride 8, loc 1)");
  assert.deepEqual(assertData(maskedDesc.primitive), { topology: "triangle-list", cullMode: "front" });
  assert.deepEqual(assertData(maskedDesc.depthStencil), {
    format: "depth24plus",
    depthWriteEnabled: true,
    depthCompare: "less-equal",
  });
  assert.equal(maskedDesc.fragment.module, fragmentModule,
    "masked fragment stage keeps the identical module object");
  assert.deepEqual(assertData(maskedDesc.fragment), {
    module: fragmentModule,
    entryPoint: "fragmentMain",
    targets: [],
  }, "masked fragment stage is depth-only with no color targets");

  const maskedInstanced = ctx.wgpuCreateShadowMaskedInstancedPipeline(
    env.device, shadowLayout, materialLayout, vertexModule, fragmentModule);
  assert.equal(maskedInstanced.label, "gosx-shadow-masked-instanced");
  const instancedDesc = maskedInstanced.descriptor;
  assert.deepEqual(Array.from(instancedDesc.layout.bindGroupLayouts), [shadowLayout, materialLayout]);
  assert.equal(instancedDesc.layout.bindGroupLayouts[0], shadowLayout,
    "instanced group 0 is the identical shadow layout object");
  assert.equal(instancedDesc.layout.bindGroupLayouts[1], materialLayout,
    "instanced group 1 is the identical material layout object");
  assert.deepEqual(assertData(instancedDesc.vertex.buffers), [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 8, stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 1 }] },
    {
      arrayStride: 64, stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0, shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
      ],
    },
    {
      arrayStride: 16, stepMode: "instance",
      attributes: [{ format: "float32x4", offset: 0, shaderLocation: 8 }],
    },
  ], "masked instanced buffers are pos12 / UV8 / transform64 / color16");
  assert.deepEqual(assertData(instancedDesc.primitive), { topology: "triangle-list", cullMode: "front" });
  assert.deepEqual(assertData(instancedDesc.depthStencil), {
    format: "depth24plus",
    depthWriteEnabled: true,
    depthCompare: "less-equal",
  });
  assert.equal(instancedDesc.fragment.module, fragmentModule,
    "instanced fragment stage keeps the identical module object");
  assert.deepEqual(assertData(instancedDesc.fragment), {
    module: fragmentModule,
    entryPoint: "fragmentMain",
    targets: [],
  });

  // Attribute shader locations must match the actual masked WGSL blocks:
  // static UV at 1; instanced UV at 1, matrix rows 4..7, color at 8.
  assert.match(WGSL_MASKED_VERTEX_BLOCK, /@group\(0\) @binding\(0\) var<uniform> shadowFrame/);
  assert.deepEqual(maskVertexInputLocations(WGSL_MASKED_VERTEX_BLOCK), [0, 1],
    "masked static WGSL inputs are position 0 and uv 1");
  assert.match(WGSL_MASKED_INSTANCED_VERTEX_BLOCK, /@location\(4\) instanceMatrix0/);
  assert.match(WGSL_MASKED_INSTANCED_VERTEX_BLOCK, /@location\(7\) instanceMatrix3/);
  assert.match(WGSL_MASKED_INSTANCED_VERTEX_BLOCK, /@location\(8\) instanceColor/);
  assert.deepEqual(maskVertexInputLocations(WGSL_MASKED_INSTANCED_VERTEX_BLOCK),
    [0, 1, 4, 5, 6, 7, 8],
    "masked instanced WGSL inputs are position 0, uv 1, matrix 4..7, color 8");
  assert.match(WGSL_MASKED_FRAGMENT_BLOCK, /@group\(1\) @binding\(0\)/,
    "masked fragment reads the material uniform at group 1");
  assert.match(WGSL_MASKED_FRAGMENT_BLOCK, /textureSampleLevel\(/,
    "masked fragment samples the albedo texture with an explicit LOD");

  // Original opaque layouts stay untouched and never alias the masked ones.
  const opaque = ctx.wgpuCreateShadowPipeline(env.device, shadowLayout, vertexModule);
  const opaqueInstanced = ctx.wgpuCreateShadowInstancedPipeline(env.device, shadowLayout, vertexModule);
  assert.equal(opaque.descriptor.vertex.buffers, ctx.WGPU_SHADOW_VERTEX_LAYOUT,
    "opaque pipeline passes the original layout array through");
  assert.deepEqual(assertData(opaqueInstanced.descriptor.vertex.buffers), [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    {
      arrayStride: 64, stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0, shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
      ],
    },
  ], "original opaque instanced layout stays position+matrix only (no UV slot 1, no color slot 8)");
  assert.notEqual(ctx.WGPU_SHADOW_MASKED_VERTEX_LAYOUT, ctx.WGPU_SHADOW_VERTEX_LAYOUT,
    "masked static layout is a separate array from the opaque layout");
  assert.notEqual(ctx.WGPU_SHADOW_MASKED_INSTANCED_VERTEX_LAYOUT, ctx.WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT,
    "masked instanced layout is a separate array from the opaque instanced layout");
});

test("renderShadowPass soup path: masked/unmasked pipeline switching and vertexOffset-derived byte offsets", () => {
  const ctx = createHarness();
  const env = createRecordingEnvironment(ctx);
  const lightMatrix = Float32Array.from({ length: 16 }, (_, i) => i + 1);
  const positionsRecord = { buffer: { id: "soup-positions" }, components: 3 };
  const uvsRecord = { buffer: { id: "soup-uvs" }, components: 2 };
  const materials = [{ alphaCutoff: 0.5 }, {}];
  const bundle = {
    meshObjects: [
      { castShadow: true, viewCulled: false, vertexOffset: 0, vertexCount: 3, materialIndex: 0 },
      { castShadow: true, viewCulled: false, vertexOffset: 4, vertexCount: 6, materialIndex: 0 },
      { castShadow: true, viewCulled: false, vertexOffset: 2, vertexCount: 3, materialIndex: 1 },
    ],
    materials,
  };

  const shadow = runShadowPass(ctx, bundle, lightMatrix,
    { positions: positionsRecord, uvs: uvsRecord });

  assert.deepEqual(assertData(shadow.passDescriptor.depthStencilAttachment), {
    view: { id: "shadow-view" },
    depthLoadOp: "clear",
    depthClearValue: 1.0,
    depthStoreOp: "store",
  });
  assert.deepEqual(assertData(shadow.passDescriptor.colorAttachments), []);
  assert.equal(shadow.draws.length, 3, "three soup draws, no instanced draws");

  const first = shadow.draws[0];
  assert.equal(first.pipeline, env.maskedShadowPipeline,
    "first masked draw carries the masked pipeline at draw time");
  assert.equal(first.bindGroups[1].material, materials[0],
    "first masked draw carries its material bind group at draw time");
  assert.deepEqual(first.bindOffsets[0], [0],
    "soup draws read the shared light matrix at dynamic offset 0");
  assert.equal(first.vertexBuffers[0].buffer.id, "soup-positions");
  assert.deepEqual([first.vertexBuffers[0].offset, first.vertexBuffers[0].size], [0, 36]);
  assert.equal(first.vertexBuffers[1].buffer.id, "soup-uvs");
  assert.deepEqual([first.vertexBuffers[1].offset, first.vertexBuffers[1].size], [0, 24]);

  const second = shadow.draws[1];
  assert.equal(second.pipeline, env.maskedShadowPipeline,
    "consecutive masked draws keep the masked pipeline");
  assert.equal(second.vertexBuffers[0].offset, 4 * 3 * 4,
    "position soup offset is vertexOffset * 3 * 4 bytes");
  assert.equal(second.vertexBuffers[1].offset, 4 * 2 * 4,
    "UV soup offset is vertexOffset * 2 * 4 bytes");
  assert.equal(shadow.log.slice(first.logIndex, second.logIndex)
    .some((call) => call[0] === "setPipeline"), false,
    "no redundant pipeline switch between the two masked draws");

  const third = shadow.draws[2];
  assert.equal(third.pipeline, env.opaqueShadowPipeline,
    "unmasked soup draw switches back to the opaque shadow pipeline");
  assert.equal(third.bindGroups[1].material, materials[0],
    "stale recorder state: no new material group is set for the unmasked draw");
  const betweenSecondAndThird = shadow.log.slice(second.logIndex, third.logIndex);
  assert.equal(betweenSecondAndThird.some(
    (call) => call[0] === "setBindGroup" && call[1] === 1), false,
    "unmasked soup draw never sets a material bind group");
  assert.equal(betweenSecondAndThird.some(
    (call) => call[0] === "setVertexBuffer" && call[1] === 1), false,
    "unmasked soup draw binds position only (no UV slot)");
  assert.equal(third.vertexBuffers[0].offset, 2 * 3 * 4);
});

test("retained masked casters: distinct 256-byte-aligned matrix slots via the actual matrix multiply, soup resets to offset 0", () => {
  const ctx = createHarness();
  const env = createRecordingEnvironment(ctx);
  const lightMatrix = Float32Array.from({ length: 16 }, (_, i) => i + 1);
  const modelA = Float32Array.from([2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 2, 0, 0, 0, 0, 1]);
  const modelB = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 5, 0, 0, 1]);
  const material = { alphaCutoff: 0.5 };
  const casterA = {
    castShadow: true, viewCulled: false,
    retainedGeometry: true, directVertices: { id: "retained-a" },
    vertices: { indices: Uint32Array.from([0, 1, 2, 2, 1, 0]) },
    modelMatrix: modelA,
    vertexOffset: 0, vertexCount: 3, materialIndex: 0,
    __retainedAttributes: { positions: new Float32Array(9), uvs: new Float32Array(6) },
    __indexCount: 6,
  };
  const casterB = {
    castShadow: true, viewCulled: false,
    retainedGeometry: true, directVertices: { id: "retained-b" },
    vertices: { indices: Uint32Array.from([0, 1, 2, 2, 1, 0]) },
    modelMatrix: modelB,
    vertexOffset: 3, vertexCount: 3, materialIndex: 0,
    __retainedAttributes: { positions: new Float32Array(9), uvs: new Float32Array(6) },
    __indexCount: 6,
  };
  const soupCaster = {
    castShadow: true, viewCulled: false,
    vertexOffset: 0, vertexCount: 3, materialIndex: 0,
  };
  const bundle = {
    meshObjects: [casterA, casterB, soupCaster],
    materials: [material],
  };

  const shadow = runShadowPass(ctx, bundle, lightMatrix,
    { positions: { buffer: { id: "soup-positions" }, components: 3 },
      uvs: { buffer: { id: "soup-uvs" }, components: 2 } });

  assert.deepEqual(env.rec.queueWrites.map((w) => w.offset), [0, 256, 512],
    "light matrix at slot 0, each retained caster at its own slot");
  assert.ok(env.rec.queueWrites.slice(1).every((w) => w.offset % 256 === 0),
    "retained caster matrix offsets are 256-byte aligned");
  assert.deepEqual(env.rec.queueWrites[0].data, Array.from(lightMatrix),
    "slot 0 carries the shared light matrix");
  const expectedA = multiplyMat4Oracle(Array.from(lightMatrix), Array.from(modelA));
  const expectedB = multiplyMat4Oracle(Array.from(lightMatrix), Array.from(modelB));
  assert.deepEqual(env.rec.queueWrites[1].data, expectedA,
    "caster A slot carries light * modelA from the actual matrix multiply");
  assert.deepEqual(env.rec.queueWrites[2].data, expectedB,
    "caster B slot carries light * modelB");
  assert.notDeepEqual(env.rec.queueWrites[1].data, env.rec.queueWrites[2].data,
    "distinct casters get distinct matrices — no queue aliasing");

  const retainedDraws = shadow.draws.filter((d) => d.kind === "drawIndexed");
  assert.deepEqual(retainedDraws.map((d) => d.count), [6, 6],
    "retained casters draw indexed with their index counts");
  assert.deepEqual(retainedDraws.map((d) => d.bindOffsets[0]), [[256], [512]],
    "each retained draw reads its own matrix slot");
  assert.equal(retainedDraws[0].bindGroups[0].layout, ctx.shadowBindGroupLayout);
  assert.equal(retainedDraws[0].bindGroups[1].material, material,
    "retained masked casters carry the material bind group");
  assert.equal(retainedDraws[1].pipeline, env.maskedShadowPipeline);
  assert.equal(env.rec.retainedAttributeBinds.filter((b) => b.key === "uvs").length, 2,
    "retained UVs come from the retained attribute binder when present");
  assert.equal(env.rec.defaultAttributeCalls.length, 0,
    "no default UV upload when retained UVs exist");

  const soupDraw = shadow.draws[shadow.draws.length - 1];
  assert.equal(soupDraw.kind, "draw");
  assert.deepEqual(soupDraw.bindOffsets[0], [0],
    "soup draw after retained casters resets the group-0 dynamic offset to 0");
  assert.equal(soupDraw.pipeline, env.maskedShadowPipeline);
});

test("skinned masked casters: UVs from the webGPUDefaultAttributeData port and _gosxWGPUSkinnedUVs cache key", () => {
  const ctx = createHarness();
  const env = createRecordingEnvironment(ctx);
  const lightMatrix = Float32Array.from({ length: 16 }, (_, i) => i + 1);
  const skinnedObject = {
    castShadow: true, viewCulled: false,
    __skinned: true,
    _gosxWGPUElioSkinOutputBuffer: { id: "skinned-positions" },
    vertexOffset: 0, vertexCount: 4, materialIndex: 0,
  };
  const bundle = {
    meshObjects: [skinnedObject],
    materials: [{ alphaCutoff: 0.5 }],
  };

  const shadow = runShadowPass(ctx, bundle, lightMatrix, null);
  assert.equal(shadow.draws.length, 1);
  assert.deepEqual(env.rec.defaultAttributeCalls, [{
    obj: skinnedObject, key: "uvs", count: 4, tupleSize: 2, defaults: [0, 0],
  }], "skinned masked UVs come from the webGPUDefaultAttributeData port with the zero default");
  const uvUpload = env.rec.cachedUploads.find((u) => u.key === "_gosxWGPUSkinnedUVs");
  assert.ok(uvUpload, "skinned masked UVs upload through the _gosxWGPUSkinnedUVs cache key");
  assert.equal(uvUpload.usage, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST);
  assert.equal(env.rec.retainedAttributeBinds.length, 0,
    "skinned masked UVs never touch the retained attribute helper");

  const draw = shadow.draws[0];
  assert.equal(draw.pipeline, env.maskedShadowPipeline);
  assert.equal(draw.kind, "draw");
  assert.equal(draw.count, 4);
  assert.equal(draw.vertexBuffers[0].buffer.id, "skinned-positions",
    "skinned position buffer binds at slot 0");
  assert.equal(draw.vertexBuffers[1].buffer.id, "_gosxWGPUSkinnedUVs",
    "skinned masked UV buffer binds at slot 1");
  assert.equal(draw.bindGroups[1].material, bundle.materials[0]);
  assert.deepEqual(draw.bindOffsets[0], [0]);
});

test("drawInstancedShadowMeshes: masked pos0/UV1/transform2/color3 + group 1, unmasked pos0/transform1, pipeline switch, colors passed unchanged", () => {
  const ctx = createHarness();
  const env = createRecordingEnvironment(ctx);
  const materials = [{ alphaCutoff: 0.5 }, {}];
  const maskedGeom = {
    vertexCount: 4,
    positions: Float32Array.from({ length: 12 }, (_, i) => i + 1),
    uvs: Float32Array.from({ length: 8 }, (_, i) => i),
  };
  const plainGeom = {
    vertexCount: 3,
    positions: Float32Array.from([9, 8, 7, 6, 5, 4, 3, 2, 1]),
  };
  const colorData = Float32Array.from([1, 0, 0, 0, 0, 1, 0, 0.5, 1, 1, 1, 1]);
  const maskedMesh = {
    castShadow: true, viewCulled: false, materialIndex: 0, instanceCount: 3,
    transforms: Float32Array.from({ length: 48 }, (_, i) => i + 1),
    colors: colorData,
    __geometry: maskedGeom,
  };
  const plainMesh = {
    castShadow: true, viewCulled: false, materialIndex: 1, instanceCount: 2,
    transforms: Float32Array.from({ length: 32 }, (_, i) => i + 1),
    __geometry: plainGeom,
  };
  const maskedWithoutUVs = {
    castShadow: true, viewCulled: false, materialIndex: 0, instanceCount: 1,
    transforms: Float32Array.from({ length: 16 }, (_, i) => i + 1),
    __geometry: { vertexCount: 3, positions: new Float32Array(9) },
  };

  const { pass, draws, log } = createRecordingPass();
  ctx.drawInstancedShadowMeshes(pass, {
    instancedMeshes: [maskedMesh, plainMesh, maskedWithoutUVs],
    materials,
  });

  assert.equal(draws.length, 2,
    "two instanced draws; the masked mesh without UVs is skipped");
  assert.equal(draws[0].pipeline, env.maskedInstancedShadowPipeline,
    "masked instanced mesh takes the masked instanced pipeline");
  assert.equal(draws[0].count, 4);
  assert.equal(draws[0].instances, 3);
  assert.equal(draws[0].vertexBuffers[0].buffer.id, "_gosxWGPUInstancedShadowPositionBuffer");
  assert.equal(draws[0].vertexBuffers[1].buffer.id, "_gosxWGPUInstancedShadowUVBuffer");
  assert.equal(draws[0].vertexBuffers[2].buffer.id, "_gosxWGPUInstanceTransformBuffer");
  assert.equal(draws[0].vertexBuffers[3].buffer.id, "_gosxWGPUInstanceColorBuffer",
    "masked instanced binds position 0, UV 1, transform 2, color 3");
  assert.equal(draws[0].bindGroups[1].material, materials[0],
    "masked instanced draw sets the material bind group at group 1");

  const betweenDraws = log.slice(draws[0].logIndex, draws[1].logIndex);
  assert.equal(betweenDraws.filter((call) => call[0] === "setPipeline").length, 1,
    "exactly one pipeline switch between the masked and unmasked instanced draws");
  assert.equal(betweenDraws.some(
    (call) => call[0] === "setVertexBuffer" && (call[1] === 2 || call[1] === 3)), false,
    "unmasked instanced draw binds no UV (slot 1 swap) / transform-slot-2 / color-slot-3");
  assert.equal(betweenDraws.some(
    (call) => call[0] === "setBindGroup" && call[1] === 1), false,
    "unmasked instanced draw sets no material bind group");
  assert.equal(draws[1].pipeline, env.opaqueInstancedShadowPipeline,
    "unmasked instanced mesh takes the opaque instanced pipeline");
  assert.equal(draws[1].count, 3);
  assert.equal(draws[1].instances, 2);
  assert.equal(draws[1].vertexBuffers[1].buffer.id, "_gosxWGPUInstanceTransformBuffer",
    "unmasked instanced keeps the transform at slot 1");

  const colorUpload = env.rec.cachedUploads.find((u) => u.key === "_gosxWGPUInstanceColorBuffer");
  assert.ok(colorUpload, "instance colors upload through the color buffer cache key");
  assert.equal(colorUpload.source, colorData,
    "instance color data is passed through unchanged (identity), including alpha 0 / 0.5 / 1");
  assert.deepEqual(colorUpload.data, [1, 0, 0, 0, 0, 1, 0, 0.5, 1, 1, 1, 1]);
  const transformUpload = env.rec.cachedUploads.find((u) => u.key === "_gosxWGPUInstanceTransformBuffer");
  assert.ok(transformUpload, "instance transforms upload through the transform cache key");
  assert.deepEqual(transformUpload.data, Array.from({ length: 48 }, (_, i) => i + 1));
});
