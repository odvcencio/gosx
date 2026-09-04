"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { createBoardWebGPUHarness } = require("./runtime-test-harness.js");

function multiplyMatrix(left, right) {
  const out = new Float32Array(16);
  for (let column = 0; column < 4; column++) {
    for (let row = 0; row < 4; row++) {
      let value = 0;
      for (let k = 0; k < 4; k++) value += left[k * 4 + row] * right[column * 4 + k];
      out[column * 4 + row] = value;
    }
  }
  return Array.from(out);
}

function shadowBundle(harness, lights) {
  const api = harness.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", harness.env.context);
  const U32 = vm.runInContext("Uint32Array", harness.env.context);
  const objects = [{
    id: "retained", kind: "gltf-mesh", materialKind: "standard", color: "#ffffff",
    x: 0.4, y: 0.2, z: -0.3, rotationY: 0.3,
    scaleX: 1.2, scaleY: 0.8, scaleZ: 1.1,
    wireframe: false, castShadow: true, receiveShadow: true,
    vertices: {
      count: 4,
      positions: new F32([-1, -1, 0, 1, -1, 0, 1, 1, 0, -1, 1, 0]),
      normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]),
      uvs: new F32([0, 0, 1, 0, 1, 1, 0, 1]),
      tangents: new F32([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]),
      indices: new U32([0, 1, 2, 0, 2, 3]),
      immutable: true, revision: 0, dynamic: false,
    },
  }, {
    id: "soup", kind: "gltf-mesh", materialKind: "standard", color: "#ffffff",
    scaleX: 1, scaleY: 1, scaleZ: 1, castShadow: true, receiveShadow: true,
    vertices: {
      count: 3,
      positions: new F32([-1, -1, -1, 1, -1, -1, 0, 1, -1]),
      normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1]),
      uvs: new F32([0, 0, 1, 0, 0.5, 1]),
      dynamic: true,
    },
  }];
  const bundle = api.createSceneRenderBundle(
    64, 64, "#000000", { x: 0, y: 0, z: 6, fov: 60, near: 0.05, far: 128 },
    objects.map((object, index) => api.normalizeSceneObject(object, index, null)),
    [], [], [], lights, {}, 0, [], [], [], [], [], 0, false,
    { retainedGeometry: true },
  );
  assert.equal(bundle.meshObjects.length, 2);
  assert.equal(bundle.meshObjects[0].retainedGeometry, true);
  assert.equal(bundle.meshObjects[0].directVertices, true);
  assert.notEqual(bundle.meshObjects[1].retainedGeometry, true);
  assert.notDeepEqual(Array.from(bundle.meshObjects[0].modelMatrix),
    [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  return bundle;
}

// Keep mutable GPU-buffer bytes and read them through the actual draw bindings
// only at queue.submit. Snapshots of writeBuffer arguments would conceal a
// later light overwriting the earlier light's matrices before execution.
function recordDeferredShadows(device) {
  const bytes = new Map();
  const frames = [];
  const originalWrite = device.queue.writeBuffer.bind(device.queue);
  device.queue.writeBuffer = (buffer, offset, data, dataOffset = 0, size) => {
    originalWrite(buffer, offset, data, dataOffset, size);
    const width = data.BYTES_PER_ELEMENT || 1;
    const start = (data.byteOffset || 0) + dataOffset * width;
    const length = size === undefined ? data.byteLength - dataOffset * width : size * width;
    if (!bytes.has(buffer)) bytes.set(buffer, new Uint8Array(buffer.size));
    bytes.get(buffer).set(new Uint8Array(data.buffer || data, start, length), offset);
  };
  const originalEncoder = device.createCommandEncoder.bind(device);
  device.createCommandEncoder = (...args) => {
    const encoder = originalEncoder(...args);
    const shadowPasses = [];
    const originalBegin = encoder.beginRenderPass.bind(encoder);
    encoder.beginRenderPass = (descriptor) => {
      const pass = originalBegin(descriptor);
      if (descriptor.colorAttachments.length !== 0) return pass;
      const draws = [];
      shadowPasses.push(draws);
      let binding;
      const originalBind = pass.setBindGroup.bind(pass);
      pass.setBindGroup = (slot, group, offsets) => {
        originalBind(slot, group, offsets);
        if (slot === 0) {
          const resource = group.desc.entries.find((entry) => entry.binding === 0).resource;
          binding = { buffer: resource.buffer, offset: (resource.offset || 0) + (offsets?.[0] || 0) };
        }
      };
      for (const kind of ["draw", "drawIndexed"]) {
        const originalDraw = pass[kind].bind(pass);
        pass[kind] = (...drawArgs) => {
          originalDraw(...drawArgs);
          draws.push({ ...binding, kind });
        };
      }
      return pass;
    };
    const originalFinish = encoder.finish.bind(encoder);
    encoder.finish = (...finishArgs) => Object.assign(originalFinish(...finishArgs), { shadowPasses });
    return encoder;
  };
  const originalSubmit = device.queue.submit.bind(device.queue);
  device.queue.submit = (commands) => {
    originalSubmit(commands);
    for (const command of commands) {
      frames.push((command.shadowPasses || []).map((draws) => draws.map((draw) => {
        assert.notEqual(draw.buffer.destroyed, true, "encoded shadow arena must remain alive until submit");
        const backing = bytes.get(draw.buffer);
        assert.ok(backing, "draw reads an uploaded buffer");
        return { ...draw, matrix: Array.from(new Float32Array(backing.buffer, draw.offset, 16)) };
      })));
    }
  };
  return frames;
}

const lightPairs = [
  ["directional", "directional"],
  ["directional", "spot"],
  ["spot", "spot"],
];

for (const kinds of lightPairs) {
  test("WebGPU deferred submission isolates " + kinds.join(" + ") + " shadow matrices", async () => {
    const harness = await createBoardWebGPUHarness({ fresh: true });
    // Recreate with a nondefault device alignment before encoding the frame.
    harness.renderer.dispose();
    harness.fake.device.limits.minUniformBufferOffsetAlignment = 512;
    const renderer = harness.env.context.__gosx_scene3d_webgpu_api.createRenderer(harness.canvas, {});
    assert.ok(renderer);
    const frames = recordDeferredShadows(harness.fake.device);
    const lights = [{ kind: "point", castShadow: true }, ...kinds.map((kind, index) => ({
      kind, castShadow: true, x: index ? 2 : -1, y: 4, z: index ? -2 : 2,
      directionX: index ? -0.3 : 0.2, directionY: -1, directionZ: index ? 0.2 : -0.3,
      angle: 0.65, range: 12, shadowSize: 256,
    }))];
    try {
      const bundle = shadowBundle(harness, lights);
      const submitStart = harness.fake.state.submitCount;
      renderer.render(bundle, { width: 64, height: 64 }, { nowMS: 0, active: true });
      assert.equal(harness.fake.state.submitCount - submitStart, 1, "both lights share one submitted frame");
      assert.equal(frames.length, 1);
      assert.equal(frames[0].length, 2, "unsupported light consumes no shadow slot");
      const receiver = harness.fake.state.writeBufferCalls.filter((call) => call.data?.length === 40).at(-1);
      assert.ok(receiver, "frame uploads both receiver light matrices");
      const matrices = [Array.from(receiver.data.slice(0, 16)), Array.from(receiver.data.slice(16, 32))];
      assert.notDeepEqual(matrices[0], matrices[1], "fixture lights produce distinct matrices");
      const slotsPerLight = bundle.meshObjects.length + 1;
      const arenas = new Set();
      const offsets = [];
      for (let light = 0; light < 2; light++) {
        const draws = frames[0][light];
        assert.equal(draws.length, 2, "each light draws retained and world-space casters");
        for (const draw of draws) {
          const retained = draw.kind === "drawIndexed";
          arenas.add(draw.buffer);
          offsets.push(draw.offset);
          assert.equal(draw.offset, (light * slotsPerLight + (retained ? 1 : 0)) * 512);
          assert.deepEqual(draw.matrix, retained
            ? multiplyMatrix(matrices[light], bundle.meshObjects[0].modelMatrix)
            : matrices[light], "submitted draw keeps its own light/model matrix");
        }
      }
      assert.equal(arenas.size, 1, "one arena is reserved before either shadow pass");
      assert.equal(new Set(offsets).size, offsets.length, "all encoded matrix regions are disjoint");
      assert.ok([...arenas][0].size >= slotsPerLight * 2 * 512, "arena covers the full frame");
    } finally {
      renderer.dispose();
    }
  });
}
