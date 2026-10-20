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
