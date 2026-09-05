"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const {
  createBoardWebGPUHarness,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function transformPoint(matrix, point) {
  const [x, y, z] = point;
  const cx = matrix[0] * x + matrix[4] * y + matrix[8] * z + matrix[12];
  const cy = matrix[1] * x + matrix[5] * y + matrix[9] * z + matrix[13];
  const cz = matrix[2] * x + matrix[6] * y + matrix[10] * z + matrix[14];
  const cw = matrix[3] * x + matrix[7] * y + matrix[11] * z + matrix[15];
  return { x: cx / cw, y: cy / cw, z: cz / cw, w: cw };
}

function remapGLClipDepthMatrix(matrix) {
  const out = new Float32Array(matrix);
  out[2] = 0.5 * (matrix[2] + matrix[3]);
  out[6] = 0.5 * (matrix[6] + matrix[7]);
  out[10] = 0.5 * (matrix[10] + matrix[11]);
  out[14] = 0.5 * (matrix[14] + matrix[15]);
  return out;
}

function assertMatrixClose(actual, expected, message) {
  assert.equal(actual.length, expected.length, message + " length");
  for (let i = 0; i < actual.length; i += 1) {
    assert.ok(Math.abs(actual[i] - expected[i]) <= 1e-5, `${message}[${i}] actual=${actual[i]} expected=${expected[i]}`);
  }
}

function assertVectorClose(actual, expected, message) {
  assert.equal(actual.length, expected.length, message + " length");
  for (let i = 0; i < actual.length; i += 1) {
    assert.ok(Math.abs(actual[i] - expected[i]) <= 1e-5, `${message}[${i}] actual=${actual[i]} expected=${expected[i]}`);
  }
}

function multiplyMat4(a, b) {
  const out = new Float32Array(16);
  for (let col = 0; col < 4; col += 1) {
    for (let row = 0; row < 4; row += 1) {
      out[col * 4 + row] =
        a[0 * 4 + row] * b[col * 4 + 0] +
        a[1 * 4 + row] * b[col * 4 + 1] +
        a[2 * 4 + row] * b[col * 4 + 2] +
        a[3 * 4 + row] * b[col * 4 + 3];
    }
  }
  return out;
}

function matrixFromArray(values) {
  const out = new Float32Array(16);
  out.set(values);
  return out;
}

function readMatrixFromWrite(call) {
  return Array.from(call.data.slice(0, 16));
}

function bindGroupEntry(group, binding) {
  return group && group.desc && Array.isArray(group.desc.entries)
    ? group.desc.entries.find((entry) => entry && entry.binding === binding)
    : null;
}

function writeForBuffer(fake, buffer, predicate) {
  return fake.state.writeBufferCalls.find((call) =>
    call.buffer === buffer && (!predicate || predicate(call))
  );
}

function attachDrawStateRecorder(fake) {
  const originalCreateCommandEncoder = fake.device.createCommandEncoder.bind(fake.device);
  fake.device.createCommandEncoder = function createCommandEncoderWithDrawSnapshots(...args) {
    const encoder = originalCreateCommandEncoder(...args);
    const originalBeginRenderPass = encoder.beginRenderPass.bind(encoder);
    encoder.beginRenderPass = function beginRenderPassWithDrawSnapshots(...passArgs) {
      const pass = originalBeginRenderPass(...passArgs);
      const current = {
        pipeline: null,
        bindGroups: new Map(),
        vertexBuffers: new Map(),
        indexBuffer: null,
      };
      pass.__drawSnapshots = [];

      function cloneBindings(map) {
        return new Map(Array.from(map.entries()).map(([slot, record]) => [
          slot,
          Object.assign({}, record, {
            dynamicOffsets: record.dynamicOffsets ? record.dynamicOffsets.slice() : undefined,
          }),
        ]));
      }
      function snapshot(kind, record) {
        pass.__drawSnapshots.push({
          kind,
          record,
          pipeline: current.pipeline,
          bindGroups: cloneBindings(current.bindGroups),
          vertexBuffers: new Map(Array.from(current.vertexBuffers.entries()).map(([slot, record]) => [slot, Object.assign({}, record)])),
          indexBuffer: current.indexBuffer ? Object.assign({}, current.indexBuffer) : null,
        });
      }

      const originalSetPipeline = pass.setPipeline.bind(pass);
      pass.setPipeline = function setPipelineAndTrack(pipeline) {
        current.pipeline = pipeline;
        return originalSetPipeline(pipeline);
      };
      const originalSetBindGroup = pass.setBindGroup.bind(pass);
      pass.setBindGroup = function setBindGroupAndTrack(slot, group, dynamicOffsets) {
        const record = { slot, group };
        if (dynamicOffsets && typeof dynamicOffsets.length === "number") {
          record.dynamicOffsets = Array.from(dynamicOffsets);
        }
        current.bindGroups.set(slot, record);
        return originalSetBindGroup(slot, group, dynamicOffsets);
      };
      const originalSetVertexBuffer = pass.setVertexBuffer.bind(pass);
      pass.setVertexBuffer = function setVertexBufferAndTrack(slot, buffer, offset, size) {
        current.vertexBuffers.set(slot, { slot, buffer, offset, size });
        return originalSetVertexBuffer(slot, buffer, offset, size);
      };
      const originalSetIndexBuffer = pass.setIndexBuffer.bind(pass);
      pass.setIndexBuffer = function setIndexBufferAndTrack(buffer, format, offset, size) {
        current.indexBuffer = { buffer, format, offset, size };
        return originalSetIndexBuffer(buffer, format, offset, size);
      };
      const originalDrawIndexed = pass.drawIndexed.bind(pass);
      pass.drawIndexed = function drawIndexedAndSnapshot(...drawArgs) {
        originalDrawIndexed(...drawArgs);
        snapshot("drawIndexed", pass.drawIndexeds[pass.drawIndexeds.length - 1]);
      };
      const originalDraw = pass.draw.bind(pass);
      pass.draw = function drawAndSnapshot(...drawArgs) {
        originalDraw(...drawArgs);
        snapshot("draw", pass.draws[pass.draws.length - 1]);
      };
      return pass;
    };
    return encoder;
  };
}

function assertThrowsAssertion(name, fn, pattern) {
  assert.throws(fn, pattern, name + " mutation control must fail");
}

function indexedQuad(overrides, Float32Ctor, Uint32Ctor) {
  const F32 = Float32Ctor || Float32Array;
  const U32 = Uint32Ctor || Uint32Array;
  return Object.assign({
    id: "indexed-shadow-caster",
    kind: "gltf-mesh",
    x: 0,
    y: 0,
    z: 0,
    scaleX: 1,
    scaleY: 1,
    scaleZ: 1,
    materialKind: "standard",
    color: "#cccccc",
    wireframe: false,
    castShadow: true,
    receiveShadow: true,
    vertices: {
      count: 4,
      positions: new F32([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]),
      normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]),
      uvs: new F32([0, 0, 1, 0, 1, 1, 0, 1]),
      tangents: new F32([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]),
      indices: new U32([0, 1, 2, 0, 2, 3]),
      immutable: true,
      revision: 0,
      dynamic: false,
    },
  }, overrides || {});
}

function renderBundle(api, objects, lights) {
  return api.createSceneRenderBundle(
    320,
    180,
    "#000000",
    { x: 0, y: 0, z: 6, fov: 36, near: 0.05, far: 128 },
    objects,
    [],
    [],
    [],
    lights,
    {},
    0,
    [],
    [],
    [],
    [],
    [],
    0,
    false,
    { retainedGeometry: true },
  );
}

function unionBounds(objects) {
  const bounds = { minX: Infinity, minY: Infinity, minZ: Infinity, maxX: -Infinity, maxY: -Infinity, maxZ: -Infinity };
  for (const obj of objects) {
    if (!obj || !obj.bounds) continue;
    bounds.minX = Math.min(bounds.minX, obj.bounds.minX);
    bounds.minY = Math.min(bounds.minY, obj.bounds.minY);
    bounds.minZ = Math.min(bounds.minZ, obj.bounds.minZ);
    bounds.maxX = Math.max(bounds.maxX, obj.bounds.maxX);
    bounds.maxY = Math.max(bounds.maxY, obj.bounds.maxY);
    bounds.maxZ = Math.max(bounds.maxZ, obj.bounds.maxZ);
  }
  return bounds;
}

function expandBounds(bounds, point) {
  if (!bounds) {
    return { minX: point.x, minY: point.y, minZ: point.z, maxX: point.x, maxY: point.y, maxZ: point.z };
  }
  bounds.minX = Math.min(bounds.minX, point.x);
  bounds.minY = Math.min(bounds.minY, point.y);
  bounds.minZ = Math.min(bounds.minZ, point.z);
  bounds.maxX = Math.max(bounds.maxX, point.x);
  bounds.maxY = Math.max(bounds.maxY, point.y);
  bounds.maxZ = Math.max(bounds.maxZ, point.z);
  return bounds;
}

function unionShadowBounds(bundle) {
  let bounds = null;
  for (const obj of bundle.meshObjects || []) {
    if (!obj || !obj.bounds || !obj.castShadow) continue;
    bounds = expandBounds(bounds, { x: obj.bounds.minX, y: obj.bounds.minY, z: obj.bounds.minZ });
    bounds = expandBounds(bounds, { x: obj.bounds.maxX, y: obj.bounds.maxY, z: obj.bounds.maxZ });
  }
  for (const mesh of bundle.instancedMeshes || []) {
    if (!mesh || !mesh.castShadow || !mesh.transforms || mesh.transforms.length < 16) continue;
    const width = Math.max(0.0001, Number(mesh.width) || 1);
    const height = Math.max(0.0001, Number(mesh.height) || 1);
    const depth = Math.max(0.0001, Number(mesh.depth) || 1);
    const xs = [-width * 0.5, width * 0.5];
    const ys = [-height * 0.5, height * 0.5];
    const zs = [-depth * 0.5, depth * 0.5];
    const count = Math.max(0, Math.floor(Number(mesh.count) || 0));
    for (let i = 0; i < count; i += 1) {
      const matrix = mesh.transforms.slice(i * 16, i * 16 + 16);
      for (const x of xs) {
        for (const y of ys) {
          for (const z of zs) {
            bounds = expandBounds(bounds, transformPoint(matrix, [x, y, z]));
          }
        }
      }
    }
  }
  return bounds;
}

function shadowPassForFrame(fake, passStart) {
  return fake.state.renderPasses.slice(passStart).find((pass) =>
    pass.descriptor && Array.isArray(pass.descriptor.colorAttachments) &&
    pass.descriptor.colorAttachments.length === 0
  );
}

function colorPassForFrame(fake, passStart) {
  return fake.state.renderPasses.slice(passStart).find((pass) =>
    pass.descriptor && Array.isArray(pass.descriptor.colorAttachments) &&
    pass.descriptor.colorAttachments.length > 0
  );
}

function assertRetainedIndexedShadowEvidence(shadowPass) {
  const snapshot = shadowPass.__drawSnapshots.find((item) =>
    item.kind === "drawIndexed" &&
    item.record && item.record.indexCount === 6 &&
    item.bindGroups.get(0) &&
    item.bindGroups.get(0).dynamicOffsets &&
    item.bindGroups.get(0).dynamicOffsets[0] > 0
  );
  assert.ok(snapshot, "retained indexed caster must issue a shadow drawIndexed with a per-caster dynamic matrix slot");
  assert.ok(snapshot.vertexBuffers.get(0) && snapshot.vertexBuffers.get(0).buffer, "retained indexed shadow draw must consume a position vertex buffer");
  assert.ok(snapshot.indexBuffer && snapshot.indexBuffer.buffer, "retained indexed shadow draw must consume an index buffer");
  return snapshot;
}

function assertInstancedShadowEvidence(shadowPass) {
  const snapshot = shadowPass.__drawSnapshots.find((item) =>
    item.kind === "draw" &&
    item.record && item.record.instanceCount > 0 &&
    item.vertexBuffers.get(0) &&
    item.vertexBuffers.get(1) &&
    item.bindGroups.get(0) &&
    item.bindGroups.get(0).dynamicOffsets &&
    item.bindGroups.get(0).dynamicOffsets[0] === 0
  );
  assert.ok(snapshot, "instanced caster must issue a shadow draw with shared light matrix slot");
  assert.ok(snapshot.record.vertexCount > 0, "instanced shadow draw must carry positive geometry vertices");
  return snapshot;
}

test("WebGPU shadow pass serializes light-space matrices in WebGPU clip depth", async (t) => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  t.after(() => harness.renderer.dispose());
  const api = harness.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", harness.env.context);
  const U32 = vm.runInContext("Uint32Array", harness.env.context);
  const caster = api.normalizeSceneObject(indexedQuad({ z: 0.42, scaleZ: 0.6 }, F32, U32), 0, null);
  const receiver = api.normalizeSceneObject(indexedQuad({ id: "receiver", z: -0.2, castShadow: false, receiveShadow: true }, F32, U32), 1, null);
  const light = api.normalizeSceneLight({
    id: "sun",
    kind: "directional",
    directionX: 0,
    directionY: 0,
    directionZ: -1,
    intensity: 1,
    castShadow: true,
    shadowSize: 512,
  }, 0, null);
  const bundle = renderBundle(api, [caster, receiver], [light]);
  const expectedGL = api.sceneShadowLightSpaceMatrix(light, unionShadowBounds(bundle));
  const expectedWebGPU = remapGLClipDepthMatrix(expectedGL);

  const passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(bundle, { width: 320, height: 180 }, { nowMS: 0, active: true });
  await flushAsyncWork();

  const shadowPass = shadowPassForFrame(harness.fake, passStart);
  assert.ok(shadowPass, "active directional shadow must open a depth-only pass");
  const shadowBuffer = shadowPass.bindGroups[0].group.desc.entries[0].resource.buffer;
  const sharedWrite = harness.fake.state.writeBufferCalls.find((call) =>
    call.buffer === shadowBuffer && call.offset === 0 && call.data && call.data.length === 16
  );
  assert.ok(sharedWrite, "shadow pass must upload the shared light matrix");
  assertMatrixClose(Array.from(sharedWrite.data), Array.from(expectedWebGPU), "shared WebGPU shadow matrix");

  const receiverRecord = bundle.meshObjects.find((obj) => obj.id === "receiver");
  const casterRecord = bundle.meshObjects.find((obj) => obj.id === "indexed-shadow-caster");
  assert.ok(receiverRecord && receiverRecord.bounds, "receiver bundle record must carry bounds");
  assert.ok(casterRecord && casterRecord.bounds, "caster bundle record must carry bounds");
  const receiverCenter = [(receiverRecord.bounds.minX + receiverRecord.bounds.maxX) * 0.5, (receiverRecord.bounds.minY + receiverRecord.bounds.maxY) * 0.5, receiverRecord.bounds.minZ];
  const casterNear = [(casterRecord.bounds.minX + casterRecord.bounds.maxX) * 0.5, (casterRecord.bounds.minY + casterRecord.bounds.maxY) * 0.5, casterRecord.bounds.maxZ];
  const glReceiver = transformPoint(expectedGL, receiverCenter);
  const wgpuReceiver = transformPoint(sharedWrite.data, receiverCenter);
  const wgpuCaster = transformPoint(sharedWrite.data, casterNear);
  assert.ok(glReceiver.z >= -1 && glReceiver.z <= 1, "shared helper still emits GL clip-space depth before WebGPU remap");
  assert.ok(wgpuReceiver.z >= 0 && wgpuReceiver.z <= 1, JSON.stringify({ wgpuReceiver, wgpuCaster }));
  assert.ok(wgpuCaster.z >= 0 && wgpuCaster.z <= 1, JSON.stringify({ wgpuReceiver, wgpuCaster }));
  assert.ok(wgpuCaster.z < wgpuReceiver.z, "nearer shadow caster depth must compare in front of receiver depth");
});

test("WebGPU shadow sampling uses WebGPU depth and top-left texture coordinates", () => {
  const source = readSceneRendererBackendSrc("webgpu");
  assert.equal(
    (source.match(/sceneWebGPUApplyClipDepthRemap\(scratchProjMatrix\)/g) || []).length,
    2,
    "main and reflection camera projections keep the existing single WebGPU clip-depth remap",
  );
  const sampler = source.slice(
    source.indexOf("fn shadowProjectedCoords"),
    source.indexOf("// 4-tap Poisson disk PCF shadow sampling for shadow slot 0."),
  );
  assert.match(sampler, /let projCoords3 = lightSpacePos\.xyz \/ lightSpacePos\.w;/, "sampling reference must divide by w before remapping");
  assert.match(
    sampler,
    /return vec3f\(projCoords3\.x \* 0\.5 \+ 0\.5,\s*0\.5 - projCoords3\.y \* 0\.5,\s*projCoords3\.z\);/,
    "x maps from clip to texture, y flips to WebGPU's top-left texture origin, z is already [0,1]",
  );

  const project = ([x, y, z, w]) => ({
    u: x / w * 0.5 + 0.5,
    v: 0.5 - y / w * 0.5,
    depth: z / w,
  });
  assert.deepEqual(project([0, 0, 0.25, 2]), { u: 0.5, v: 0.5, depth: 0.125 });
  assert.deepEqual(project([1, 1, 1, 2]), { u: 0.75, v: 0.25, depth: 0.5 });
  assert.deepEqual(project([-1, -1, 0, 2]), { u: 0.25, v: 0.75, depth: 0 });
});

test("WebGPU retained and instanced shadow paths consume the same remapped light-space convention", async (t) => {
  const harness = await createBoardWebGPUHarness({ fresh: true });
  t.after(() => harness.renderer.dispose());
  attachDrawStateRecorder(harness.fake);
  const api = harness.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", harness.env.context);
  const U32 = vm.runInContext("Uint32Array", harness.env.context);
  const retained = api.normalizeSceneObject(indexedQuad({ id: "retained", x: -0.4, y: 0.1, z: 0.25, rotationY: 0.2 }, F32, U32), 0, null);
  const instanceMatrix = matrixFromArray([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0.45, 0.05, 0.35, 1]);
  const instanced = {
    id: "instanced",
    kind: "box",
    width: 0.5,
    height: 0.5,
    depth: 0.5,
    count: 1,
    transforms: Array.from(instanceMatrix),
    materialIndex: 0,
    castShadow: true,
    receiveShadow: false,
    viewCulled: true,
  };
  const light = api.normalizeSceneLight({
    id: "sun",
    kind: "directional",
    directionX: 0,
    directionY: 0,
    directionZ: -1,
    intensity: 1,
    castShadow: true,
    shadowSize: 512,
  }, 0, null);
  const bundle = api.createSceneRenderBundle(
    320, 180, "#000000",
    { x: 0, y: 0, z: 6, fov: 36, near: 0.05, far: 128 },
    [retained], [], [], [], [light], {}, 0, [], [instanced], [], [], [], 0, false,
    { retainedGeometry: true },
  );
  assert.equal(bundle.instancedMeshes.length, 1, "fixture must reach the primitive instanced mesh path");
  assert.equal(bundle.instancedMeshes[0].count, 1, "fixture must preserve an instance count for shadow drawing");
  assert.equal(bundle.instancedMeshes[0].transforms.length, 16, "fixture must preserve one transform matrix for shadow drawing");
  const expectedWebGPU = remapGLClipDepthMatrix(api.sceneShadowLightSpaceMatrix(light, unionShadowBounds(bundle)));

  const passStart = harness.fake.state.renderPasses.length;
  harness.renderer.render(bundle, { width: 320, height: 180 }, { nowMS: 0, active: true });
  await flushAsyncWork();

  const shadowPass = shadowPassForFrame(harness.fake, passStart);
  assert.ok(shadowPass, "retained/instanced casters must share a shadow pass");
  const shadowBuffer = shadowPass.bindGroups[0].group.desc.entries[0].resource.buffer;
  const sharedWrite = harness.fake.state.writeBufferCalls.find((call) =>
    call.buffer === shadowBuffer && call.offset === 0 && call.data && call.data.length === 16
  );
  assert.ok(sharedWrite, "shadow pass must upload the shared remapped matrix");
  assertMatrixClose(Array.from(sharedWrite.data), Array.from(expectedWebGPU), "instanced shared WebGPU shadow matrix");

  const retainedDraw = assertRetainedIndexedShadowEvidence(shadowPass);
  const retainedOffset = retainedDraw.bindGroups.get(0).dynamicOffsets[0];
  const retainedWrite = writeForBuffer(harness.fake, shadowBuffer, (call) =>
    call.offset === retainedOffset && call.data && call.data.length === 16
  );
  assert.ok(retainedWrite, "retained indexed shadow draw must upload a per-caster combined matrix");
  const retainedRecord = bundle.meshObjects.find((obj) => obj.id === "retained");
  assert.ok(retainedRecord && retainedRecord.modelMatrix && retainedRecord.modelMatrix.length === 16, "retained bundle record must carry its model matrix");
  const expectedRetainedCombined = multiplyMat4(sharedWrite.data, retainedRecord.modelMatrix);
  assertMatrixClose(readMatrixFromWrite(retainedWrite), Array.from(expectedRetainedCombined), "retained dynamic-slot light*model matrix");
  const retainedLocal = [0.25, -0.25, 0];
  const retainedWorld = transformPoint(retainedRecord.modelMatrix, retainedLocal);
  const retainedViaCombined = transformPoint(retainedWrite.data, retainedLocal);
  const retainedViaShared = transformPoint(sharedWrite.data, [retainedWorld.x, retainedWorld.y, retainedWorld.z]);
  assertVectorClose(
    [retainedViaCombined.x, retainedViaCombined.y, retainedViaCombined.z],
    [retainedViaShared.x, retainedViaShared.y, retainedViaShared.z],
    "retained sample point under uploaded light*model matrix must match shared world-space projection",
  );
  assert.ok(retainedViaCombined.z >= 0 && retainedViaCombined.z <= 1, "retained sample point under uploaded matrix must land in WebGPU [0,1]");
  assertThrowsAssertion(
    "retained missing draw",
    () => assertRetainedIndexedShadowEvidence(Object.assign({}, shadowPass, { __drawSnapshots: shadowPass.__drawSnapshots.filter((item) => item.kind !== "drawIndexed") })),
    /retained indexed caster must issue/,
  );
  const corruptedRetained = Array.from(retainedWrite.data);
  corruptedRetained[10] += 0.125;
  assertThrowsAssertion(
    "retained corrupt combined matrix",
    () => assertMatrixClose(corruptedRetained, Array.from(expectedRetainedCombined), "retained corrupt control"),
    /retained corrupt control\[10\]/,
  );

  const instancedDraw = assertInstancedShadowEvidence(shadowPass);
  assert.equal(instancedDraw.bindGroups.get(0).group, shadowPass.bindGroups[0].group, "instanced shadow draw must consume the same shadow bind group as the shared matrix upload");
  const transformBuffer = instancedDraw.vertexBuffers.get(1).buffer;
  const transformWrite = writeForBuffer(harness.fake, transformBuffer, (call) =>
    call.offset === 0 && call.data && call.data.length >= 16
  );
  assert.ok(transformWrite, "instanced shadow draw must upload instance transforms to the bound vertex buffer");
  assertMatrixClose(Array.from(transformWrite.data.slice(0, 16)), Array.from(instanceMatrix), "instanced transform buffer bytes");
  const instancedLocal = [0.1, 0.2, -0.15];
  const instancedWorld = transformPoint(instanceMatrix, instancedLocal);
  const instancedViaShared = transformPoint(sharedWrite.data, [instancedWorld.x, instancedWorld.y, instancedWorld.z]);
  assert.ok(instancedViaShared.z >= 0 && instancedViaShared.z <= 1, "instanced sample point under uploaded matrix must land in WebGPU [0,1]");
  assertThrowsAssertion(
    "instanced missing draw",
    () => assertInstancedShadowEvidence(Object.assign({}, shadowPass, { __drawSnapshots: shadowPass.__drawSnapshots.filter((item) => item !== instancedDraw) })),
    /instanced caster must issue/,
  );
  const corruptedInstance = Array.from(transformWrite.data.slice(0, 16));
  corruptedInstance[12] += 0.25;
  assertThrowsAssertion(
    "instanced corrupt transform upload",
    () => assertMatrixClose(corruptedInstance, Array.from(instanceMatrix), "instanced corrupt control"),
    /instanced corrupt control\[12\]/,
  );

  const colorPass = colorPassForFrame(harness.fake, passStart);
  assert.ok(colorPass, "PBR receiver pass must execute after shadow depth pass");
  const frameBindSnapshot = colorPass.__drawSnapshots.find((item) => item.bindGroups.get(0));
  assert.ok(frameBindSnapshot, "PBR draw must bind the frame bind group");
  const shadowUniformEntry = bindGroupEntry(frameBindSnapshot.bindGroups.get(0).group, 8);
  assert.ok(shadowUniformEntry && shadowUniformEntry.resource && shadowUniformEntry.resource.buffer, "PBR frame bind group must carry the shadow uniform buffer");
  const shadowUniformWrite = writeForBuffer(harness.fake, shadowUniformEntry.resource.buffer, (call) =>
    call.offset === 0 && call.data && call.data.length >= 40
  );
  assert.ok(shadowUniformWrite, "PBR shadow receiver uniforms must be uploaded");
  assertMatrixClose(Array.from(shadowUniformWrite.data.slice(0, 16)), Array.from(sharedWrite.data), "PBR shadow uniform slot must match shadow depth pass matrix");
  const corruptedUniform = Array.from(shadowUniformWrite.data.slice(0, 16));
  corruptedUniform[14] += 0.125;
  assertThrowsAssertion(
    "PBR corrupt shadow uniform",
    () => assertMatrixClose(corruptedUniform, Array.from(sharedWrite.data), "PBR corrupt control"),
    /PBR corrupt control\[14\]/,
  );

  const shaderSource = readSceneRendererBackendSrc("webgpu");
  assert.match(shaderSource, /out\.clipPos = shadowFrame\.lightViewProjection \* model \* vec4f\(in\.position, 1\.0\);/);
  assert.match(shaderSource, /sceneMat4MultiplyInto\(_shadowCombinedMatrixScratch, lightMatrix, casterModel\)/);
});
