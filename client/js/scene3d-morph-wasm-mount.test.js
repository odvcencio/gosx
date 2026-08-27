"use strict";
// Animated glTF morph weights through the real Scene3D mount: hydration,
// motion-mixer frames, live per-instance vertices, actual renderer upload
// evidence and disposal. Uses the shared runtime test harness co-located in
// client/js; no GPU hardware required.

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  bootstrapSource,
  FakeElement,
  createContext,
  installManualRAF,
  flushSceneInitialFrameBoundary,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

const BASE_TRIANGLES = [[0, 0, 0], [1, 0, 0], [0, 1, 0]];
const FULL_FOLD = [
  [0.1125, 0.1625, 0.0375],
  [1.1625, 0.1125, 0.0375],
  [0.0375, 1.0375, 0.2375],
];
const FINAL_WEIGHTS = [1, 0.5, -0.25, 2, 0.75];

// One indexed triangle, 5 targets (POSITION everywhere, NORMAL/TANGENT on
// target 0 only) and a LINEAR weight channel over node 0: 0 at t=0, final
// weights from t=0.5 through t=2 (flat tail so a finished mixer holds known
// weights). Chunk headers follow the GLB spec: [length][type] — JSON length
// at 12 / type at 16; BIN length then type.
function buildMorphGLBBytes() {
  const floats = [];
  const accessors = [];
  function pushAccessor(values, type, count) {
    const index = accessors.length;
    accessors.push({ bufferView: 0, byteOffset: floats.length * 4, componentType: 5126, count, type });
    for (const value of values) floats.push(value);
    return index;
  }
  const posAccessor = pushAccessor([0, 0, 0, 1, 0, 0, 0, 1, 0], "VEC3", 3);
  const normalAccessor = pushAccessor([0, 0, 1, 0, 0, 1, 0, 0, 1], "VEC3", 3);
  const tangentAccessor = pushAccessor([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1], "VEC4", 3);
  const uvAccessor = pushAccessor([0, 0, 1, 0, 0, 1], "VEC2", 3);
  const targetPos = [
    [0.5, 0, 0, 0, 0.5, 0, 0, 0, 0.5],
    [0, 0.25, 0, 0.25, 0, 0, 0, 0, 0.25],
    [0.1, 0, 0, 0, 0.1, 0, 0, 0, 0.1],
    [-0.2, 0, 0, 0, -0.2, 0, 0, 0, -0.2],
    [0.05, 0.05, 0.05, 0.05, 0.05, 0.05, 0.05, 0.05, 0.05],
  ];
  const targets = targetPos.map((pos, t) => {
    const target = { POSITION: pushAccessor(pos, "VEC3", 3) };
    if (t === 0) {
      target.NORMAL = pushAccessor([0, 0, 0.25, 0, 0, 0.25, 0, 0, 0.25], "VEC3", 3);
      target.TANGENT = pushAccessor([0, 1, 0, 0, 1, 0, 0, 1, 0], "VEC3", 3);
    }
    return target;
  });
  const timesAccessor = pushAccessor([0, 0.5, 2], "SCALAR", 3);
  const weightValues = [];
  for (let k = 0; k < 3; k += 1) {
    for (let w = 0; w < FINAL_WEIGHTS.length; w += 1) weightValues.push(k === 0 ? 0 : FINAL_WEIGHTS[w]);
  }
  const valuesAccessor = pushAccessor(weightValues, "SCALAR", weightValues.length);
  const indicesAccessor = accessors.length;
  accessors.push({ bufferView: 0, byteOffset: floats.length * 4, componentType: 5123, count: 3, type: "SCALAR" });

  const doc = {
    asset: { version: "2.0" },
    scenes: [{ nodes: [0] }],
    nodes: [{ mesh: 0 }],
    meshes: [{ name: "m", primitives: [{
      attributes: { POSITION: posAccessor, NORMAL: normalAccessor, TANGENT: tangentAccessor, TEXCOORD_0: uvAccessor },
      indices: indicesAccessor,
      mode: 4,
      targets,
    }] }],
    animations: [{
      name: "morph",
      channels: [{ sampler: 0, target: { node: 0, path: "weights" } }],
      samplers: [{ input: timesAccessor, output: valuesAccessor, interpolation: "LINEAR" }],
    }],
    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 0 }],
    accessors,
  };
  const jsonText = JSON.stringify(doc);
  const jsonRaw = Buffer.from(jsonText, "utf8");
  const jsonPadded = (jsonRaw.length + 3) & ~3;
  const json = Buffer.alloc(jsonPadded, 0x20);
  jsonRaw.copy(json, 0);

  const floatBytes = new Uint8Array(new Float32Array(floats).buffer);
  const indexBytes = new Uint8Array(new Uint16Array([0, 1, 2]).buffer);
  const binPadded = (floatBytes.length + indexBytes.length + 3) & ~3;
  const bin = new Uint8Array(binPadded);
  bin.set(floatBytes, 0);
  bin.set(indexBytes, floatBytes.length);
  doc.bufferViews[0].byteLength = binPadded;

  const total = 12 + 8 + jsonPadded + 8 + binPadded;
  const glb = new ArrayBuffer(total);
  const head = new DataView(glb);
  head.setUint32(0, 0x46546C67, true);
  head.setUint32(4, 2, true);
  head.setUint32(8, total, true);
  head.setUint32(12, jsonPadded, true);      // JSON chunk length first
  head.setUint32(16, 0x4E4F534A, true);      // then type
  new Uint8Array(glb, 20, jsonPadded).set(json);
  const binOffset = 20 + jsonPadded;
  head.setUint32(binOffset, binPadded, true);     // BIN length first
  head.setUint32(binOffset + 4, 0x004E4942, true); // then type
  new Uint8Array(glb, binOffset + 8, binPadded).set(bin);
  return new Uint8Array(glb);
}

function assertClose(actual, expected, label, tolerance) {
  const tol = tolerance == null ? 1e-5 : tolerance;
  assert.equal(actual.length, expected.length, label + " length");
  for (let i = 0; i < expected.length; i += 1) {
    assert.ok(Math.abs(Number(actual[i]) - expected[i]) < tol, label + "[" + i + "]");
  }
}

function assertTriangle(actual, expected, label, tolerance) {
  assert.equal(actual.length, 9, label + " vertex count");
  for (let v = 0; v < 3; v += 1) {
    assertClose(Array.from(actual.slice(v * 3, v * 3 + 3)), expected[v], label + " v" + v, tolerance);
  }
}

function trianglesClose(actual, expected, tolerance) {
  const tol = tolerance == null ? 1e-5 : tolerance;
  if (!actual || actual.length !== 9) return false;
  for (let i = 0; i < 9; i += 1) {
    if (Math.abs(Number(actual[i]) - expected[Math.floor(i / 3)][i % 3]) >= tol) return false;
  }
  return true;
}

function mountMorphEngine(mountId, engineId) {
  const mount = new FakeElement("div", null);
  mount.id = mountId;
  return {
    mount,
    env: createContext({
      elements: [mount],
      fetchRoutes: { "/models/morph.glb": { bytes: buildMorphGLBBytes() } },
      manifest: {
        engines: [{
          id: engineId,
          component: "GoSXScene3D",
          kind: "surface",
          mountId,
          props: {
            width: 320,
            height: 180,
            autoRotate: false,
            models: [{ id: "morph", src: "/models/morph.glb", animation: "morph", loop: false }],
          },
        }],
      },
    }),
  };
}

// The shared rotation-only stub cannot drive morphs (it emits only a
// quaternion), so this test-local stub emits a packed WEIGHTS record. Layout
// matches the shared stub: [targetID, propID, arity, ...payload] float64
// slots; propID mirrors the motion decoder's property enum (1 = rotation is
// pinned by the shared stub; weights = 3).
const WASM_PROP_WEIGHTS = 3;

function installWasmMorphMixerStub(context, nodeIndex, weights) {
  const calls = { create: 0, addClip: [], play: [], stop: [], update: 0, isPlaying: 0, destroy: [] };
  let nextHandle = 1;
  context.__gosx_motion_wasm = true;
  context.__gosx_motion_mixer_create = () => { calls.create += 1; return nextHandle++; };
  context.__gosx_motion_mixer_add_clip = (handle, name, clipJSON) => {
    calls.addClip.push({ handle, name, clipJSON });
    return true;
  };
  context.__gosx_motion_mixer_play = (handle, name, fadeIn, loop, speed, weight) => {
    calls.play.push({ handle, name, fadeIn, loop, speed, weight });
  };
  context.__gosx_motion_mixer_stop = (handle, name, fadeOut) => {
    calls.stop.push({ handle, name, fadeOut });
  };
  context.__gosx_motion_mixer_is_playing = () => {
    calls.isPlaying += 1;
    return true;
  };
  context.__gosx_motion_mixer_update = (handle, dt, reduced, outU8) => {
    calls.update += 1;
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, Math.floor(outU8.byteLength / 8));
    f[0] = nodeIndex;
    f[1] = WASM_PROP_WEIGHTS;
    f[2] = weights.length;
    for (let i = 0; i < weights.length; i += 1) f[3 + i] = weights[i];
    return 3 + weights.length;
  };
  context.__gosx_motion_mixer_destroy = (handle) => {
    calls.destroy.push(handle);
  };
  return calls;
}

test("P5 morph animation: hydration + JS mixer frames fold sampled weights into the live instance vertices", async () => {
  const { mount, env } = mountMorphEngine("scene-model-morph-root", "gosx-engine-model-morph");
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");

  // The split bundle's suffix chunk runs last and republishes the API object;
  // applyMorphPose must survive it.
  const gltfApi = env.context.__gosx_scene3d_gltf_api;
  assert.equal(typeof gltfApi.applyMorphPose, "function", "split-bundle suffix keeps applyMorphPose published");

  // No WASM flag installed: the morph-only model must initialize the
  // existing JS mixer.
  const animationApi = env.context.__gosx_scene3d_animation_api;
  const originalCreateMixer = animationApi.createMixer;
  let jsMixers = 0;
  animationApi.createMixer = function trackedCreateMixer(...args) {
    jsMixers += 1;
    return originalCreateMixer.apply(this, args);
  };

  // Spy the frame-time morph apply seam; it exposes the live vertices
  // objects the render bundle reads.
  const originalApply = gltfApi.applyMorphPose;
  const ticks = [];
  gltfApi.applyMorphPose = function spiedApplyMorphPose(entries, weights, nodeTransforms) {
    originalApply.call(this, entries, weights, nodeTransforms);
    ticks.push({
      entries,
      owner: entries.length ? entries[0].vertices : null,
      positionsRef: entries.length ? entries[0].vertices.positions : null,
      positions: entries.length ? Array.from(entries[0].vertices.positions) : [],
    });
  };

  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(jsMixers, 1, "morph-only model initializes the existing mixer");

  // Manual RAF flush takes ABSOLUTE time — advance the clock every frame.
  // ~1.47s total: the clip is flat at its final weights from 0.5s to 2s.
  let clock = 48;
  for (let frame = 0; frame < 90; frame += 1) {
    raf.flush(clock);
    await flushAsyncWork();
    clock += 16;
  }

  assert.ok(ticks.length > 1, "morph pose ticks ran");
  const first = ticks[0];
  const last = ticks[ticks.length - 1];
  assert.equal(first.entries.length, 1, "morph entry survived normalization");
  assert.equal(first.entries[0].meta.nodeIndex, 0);

  // Stable owner vertices object across every tick (tracked GPU buffers keep
  // their owner), with replaced stream identities once weights change.
  assert.equal(new Set(ticks.map((tick) => tick.owner)).size, 1, "vertices owner object never replaced");
  assert.notEqual(last.positionsRef, first.positionsRef, "changed pose installs a new stream identity");

  // The first tick samples ~16ms into the clip's 0→0.5s ramp (or runs before
  // the first mixer update), so the pose is near-default but not exactly base.
  assertTriangle(first.positions, BASE_TRIANGLES, "first tick near-default", 0.05);
  assertTriangle(last.positions, FULL_FOLD, "final fold");

  // Actual renderer upload evidence (FakeWebGLContext.bufferUploads / ops),
  // in addition to the live-vertices assertions above.
  const gl = mount.children[0] && typeof mount.children[0].getContext === "function"
    ? mount.children[0].getContext("webgl")
    : null;
  assert.ok(gl, "mounted canvas exposes the FakeWebGLContext");
  const uploadedTriangles = [];
  for (const upload of gl.bufferUploads.values()) {
    const data = upload && upload.data ? upload.data : upload;
    if (data && typeof data.length === "number" && data.length === 9) {
      uploadedTriangles.push(Array.from(data));
    }
  }
  assert.ok(
    uploadedTriangles.some((verts) => trianglesClose(verts, BASE_TRIANGLES, 1e-4)),
    "renderer received the base triangle upload",
  );
  assert.ok(
    uploadedTriangles.some((verts) => trianglesClose(verts, FULL_FOLD, 1e-4)),
    "renderer received the morphed triangle upload after pose ticks",
  );
  const opCount = !gl.ops ? 0
    : typeof gl.ops.length === "number" ? gl.ops.length
    : typeof gl.ops.size === "number" ? gl.ops.size : 0;
  assert.ok(opCount > 0, "FakeWebGLContext op log records renderer calls");

  // Instance output differs from the immutable parsed source.
  const pristine = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/morph.glb");
  assertTriangle(pristine.objects[0].vertices.positions, BASE_TRIANGLES, "pristine source");
  assert.notEqual(last.positionsRef, pristine.objects[0].vertices.positions);

  assert.equal(env.consoleLogs.error.length, 0);
});

test("P5 morph animation: WASM mixer weight records drive real deformation through wasmDecodePose", async () => {
  const { mount, env } = mountMorphEngine("scene-model-morph-wasm-root", "gosx-engine-model-morph-wasm");
  const calls = installWasmMorphMixerStub(env.context, 0, FINAL_WEIGHTS);
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const gltfApi = env.context.__gosx_scene3d_gltf_api;
  assert.equal(typeof gltfApi.applyMorphPose, "function", "split-bundle suffix keeps applyMorphPose published");
  const originalApply = gltfApi.applyMorphPose;
  const ticks = [];
  gltfApi.applyMorphPose = function spiedApplyMorphPose(entries, weights, nodeTransforms) {
    originalApply.call(this, entries, weights, nodeTransforms);
    if (entries.length) {
      ticks.push({ owner: entries[0].vertices, positions: Array.from(entries[0].vertices.positions) });
    }
  };

  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(calls.create, 1, "WASM mixer created for the morph-only model");

  assert.equal(calls.addClip.length, 1);
  const clip = JSON.parse(calls.addClip[0].clipJSON);
  const weightChannel = (clip.channels || []).find((channel) => channel.property === "weights");
  assert.ok(weightChannel, "weight channel transmitted to the WASM mixer");
  assert.deepEqual(weightChannel.times, [0, 0.5, 2]);
  assert.equal(weightChannel.values.length, 15, "3 keyframes x 5 weights");
  const weightCount = clip.weightCount != null ? clip.weightCount : weightChannel.weightCount;
  assert.equal(weightCount, 5, "weightCount transmitted");

  const updatesBefore = calls.update;
  let clock = 48;
  for (let frame = 0; frame < 10; frame += 1) {
    raf.flush(clock);
    await flushAsyncWork();
    clock += 16;
  }
  assert.ok(calls.update > updatesBefore, "per-frame pose ticks the WASM mixer");
  assert.ok(ticks.length > 0, "pose ticks reach the morph apply seam");

  // Deformation, not call counts: the decoded packed weight record must drive
  // the fold all the way into the live vertices the render bundle reads.
  assert.equal(new Set(ticks.map((tick) => tick.owner)).size, 1, "vertices owner stable");
  assertTriangle(ticks[ticks.length - 1].positions, FULL_FOLD, "decoded weights deform the live vertices");

  env.context.__gosx_dispose_engine("gosx-engine-model-morph-wasm");
  await flushAsyncWork();
  assert.deepEqual(calls.destroy, [1], "WASM mixer destroyed exactly once on dispose");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("P5 morph animation: morph model disposal tears down cleanly", async () => {
  const { mount, env } = mountMorphEngine("scene-model-morph-dispose-root", "gosx-engine-model-morph-dispose");
  installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  env.context.__gosx_dispose_engine("gosx-engine-model-morph-dispose");
  await flushAsyncWork();
  assert.equal(env.consoleLogs.error.length, 0);
});
