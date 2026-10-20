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
  FakeWebGLContext,
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
  const floatBytes = new Uint8Array(new Float32Array(floats).buffer);
  const indexBytes = new Uint8Array(new Uint16Array([0, 1, 2]).buffer);
  const binPadded = (floatBytes.length + indexBytes.length + 3) & ~3;
  const bin = new Uint8Array(binPadded);
  bin.set(floatBytes, 0);
  bin.set(indexBytes, floatBytes.length);

  // Serialize the JSON chunk only after bufferViews[0].byteLength is final:
  // the JSON must describe the real padded BIN size, never the placeholder 0.
  doc.bufferViews[0].byteLength = binPadded;

  const jsonRaw = Buffer.from(JSON.stringify(doc), "utf8");
  const jsonPadded = (jsonRaw.length + 3) & ~3;
  const json = Buffer.alloc(jsonPadded, 0x20);
  jsonRaw.copy(json, 0);

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

// bufferUploads maps bufferID -> the LATEST number array uploaded for that
// buffer (no history), so tests snapshot the whole Map at phase boundaries.
function snapshotUploads(gl) {
  const snapshot = new Map();
  for (const [bufferID, data] of gl.bufferUploads) {
    if (data && typeof data.length === "number") snapshot.set(bufferID, Array.from(data));
  }
  return snapshot;
}

// 9-float buffers (3 x VEC3 positions) whose latest upload matches the
// expected triangle, so a test can track one buffer id across phases.
function triangleBufferIds(uploads, expected) {
  const ids = [];
  for (const [bufferID, data] of uploads) {
    if (data.length === 9 && trianglesClose(data, expected, 1e-4)) ids.push(bufferID);
  }
  return ids;
}

function mountMorphEngine(mountId, engineId) {
  const mount = new FakeElement("div", null);
  mount.id = mountId;
  // createContext does not enable a WebGL backend by default: mirror the
  // suite's working WebGL2 pattern (enableWebGL2 + disableCanvas2D here and
  // the WebGL2RenderingContext global installed on the context below).
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
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
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  return { mount, env };
}

// The shared rotation-only stub cannot drive morphs (it emits only a
// quaternion), so this test-local stub emits the packed pose packet the real
// WASM mixer writes: ONE SCALAR RECORD PER WEIGHT, laid out as
//   [targetID, 1000 + weightIndex, ArityScalar(0), value]
// float64 slots — exactly what sceneAnimWasmDecodePose walks (propID
// 1000+i addresses weights[i]; arity 0 means one value float follows; no TRS
// propID is ever used here). The stub writes only complete records that fit
// the packet capacity and returns the REQUIRED float64 slot count for every
// record — the production caller grows the buffer and re-emits at dt=0 when
// that count exceeds buffer.length.
function installWasmMorphMixerStub(context, nodeIndex, weightsForHandle) {
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
    const weights = weightsForHandle(handle);
    const count = Array.isArray(weights) ? weights.length : 0;
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, Math.floor(outU8.byteLength / 8));
    let used = 0;
    for (let i = 0; i < count; i += 1) {
      if (used + 4 > f.length) break; // never emit a partial record
      f[used] = nodeIndex;            // targetID
      f[used + 1] = 1000 + i;         // propID = weight base + weightIndex
      f[used + 2] = 0;                // arity = ArityScalar
      f[used + 3] = weights[i];       // scalar value
      used += 4;
    }
    // Faithfully signal capacity: return the float64 slots REQUIRED for
    // every record, not the number actually written. The production caller
    // grows the buffer and re-emits at dt=0 when this exceeds buffer.length.
    return count * 4;
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
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl", "WebGL2 canvas reports the webgl renderer mode");
  assert.equal(jsMixers, 1, "morph-only model initializes the existing mixer");

  // Real renderer evidence: the mounted canvas exposes its WebGL2 context.
  const gl = mount.children[0] && typeof mount.children[0].getContext === "function"
    ? mount.children[0].getContext("webgl2")
    : null;
  assert.ok(gl, "mounted canvas exposes the FakeWebGLContext via getContext('webgl2')");

  // bufferUploads only keeps each buffer's LATEST upload, so snapshot the
  // base pose before any mixer frame deforms it.
  const baselineUploads = snapshotUploads(gl);
  const baseBufferIds = triangleBufferIds(baselineUploads, BASE_TRIANGLES);
  assert.ok(
    baseBufferIds.length > 0,
    "position buffer uploaded with the base triangle before the first mixer frame",
  );

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

  // Renderer evidence for the changed pose: the SAME buffer id that carried
  // the base triangle at baseline now carries the morphed triangle as its
  // latest upload (bufferUploads keeps one entry per buffer, never a
  // history), so old and new poses can never coexist in the final Map.
  const changedUploads = snapshotUploads(gl);
  const morphedBufferIds = baseBufferIds.filter((bufferID) => {
    const latest = changedUploads.get(bufferID);
    return !!latest && trianglesClose(latest, FULL_FOLD, 1e-4);
  });
  assert.ok(
    morphedBufferIds.length > 0,
    "the same position buffer id now uploads the morphed triangle",
  );
  assert.ok(Array.isArray(gl.ops) && gl.ops.length > 0, "FakeWebGLContext op log records renderer calls");

  // Instance output differs from the immutable parsed source.
  const pristine = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/morph.glb");
  assertTriangle(pristine.objects[0].vertices.positions, BASE_TRIANGLES, "pristine source");
  assert.notEqual(last.positionsRef, pristine.objects[0].vertices.positions);

  assert.equal(env.consoleLogs.error.length, 0);
});

test("P5 morph animation: WASM mixer weight records drive real deformation through wasmDecodePose", async () => {
  const { mount, env } = mountMorphEngine("scene-model-morph-wasm-root", "gosx-engine-model-morph-wasm");
  const calls = installWasmMorphMixerStub(env.context, 0, () => FINAL_WEIGHTS);
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
  assert.equal(weightChannel.weightCount, 5, "weightCount lives on the weight channel");
  assert.equal(weightChannel.values.length, weightChannel.weightCount * weightChannel.times.length, "values pack whole keyframes");

  // Manual RAF flush takes ABSOLUTE time — the clock only ever moves forward.
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
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  env.context.__gosx_dispose_engine("gosx-engine-model-morph-dispose");
  await flushAsyncWork();
  assert.equal(env.consoleLogs.error.length, 0);
});

test("P5 morph animation: WASM stub reports required float count for a too-small buffer (no overrun, full re-emission)", async () => {
  const weights = [0.25, 0.5, 0.75];
  const stubContext = {};
  installWasmMorphMixerStub(stubContext, 7, (handle) => (handle === 1 ? weights : []));
  const handle = stubContext.__gosx_motion_mixer_create();

  // Packet with capacity for exactly one complete 4-slot record, cut from a
  // larger store so slots beyond the packet act as overrun canaries.
  const backing = new Float64Array(8);
  const packet = new Uint8Array(backing.buffer, 0, 4 * 8);
  const required = stubContext.__gosx_motion_mixer_update(handle, 1 / 60, 0, packet);
  assert.equal(required, 12); // 3 records x 4 slots — truncation is signalled, not hidden
  assert.ok(required > packet.byteLength / 8);
  assert.equal(backing[0], 7); // targetID
  assert.equal(backing[1], 1000); // propID = 1000 + weightIndex 0
  assert.equal(backing[2], 0); // arity = ArityScalar
  assert.equal(backing[3], weights[0]);
  for (let i = 4; i < backing.length; i += 1) {
    assert.equal(backing[i], 0, `slot ${i} beyond the packet must not be overrun`);
  }

  // Production recovery: grow and re-emit at dt=0 — every record lands.
  const grown = new Float64Array(required);
  const requiredAgain = stubContext.__gosx_motion_mixer_update(handle, 0, 0, new Uint8Array(grown.buffer));
  assert.equal(requiredAgain, required);
  assert.ok(requiredAgain <= grown.length);
  for (let i = 0; i < weights.length; i += 1) {
    const base = i * 4;
    assert.equal(grown[base], 7);
    assert.equal(grown[base + 1], 1000 + i);
    assert.equal(grown[base + 2], 0);
    assert.equal(grown[base + 3], weights[i]);
  }
  stubContext.__gosx_motion_mixer_destroy(handle);
});

test("P5 morph animation: two mounts of one cached GLB keep independent live vertices (no cross-instance deformation)", async () => {
  const mountA = new FakeElement("div", null);
  mountA.id = "scene-model-morph-pair-a";
  const mountB = new FakeElement("div", null);
  mountB.id = "scene-model-morph-pair-b";
  const env = createContext({
    elements: [mountA, mountB],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: { "/models/morph.glb": { bytes: buildMorphGLBBytes() } },
    manifest: {
      engines: [
        {
          id: "gosx-engine-morph-pair-a",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-morph-pair-a",
          props: {
            width: 320,
            height: 180,
            autoRotate: false,
            models: [{ id: "morph-a", src: "/models/morph.glb", animation: "morph", loop: false }],
          },
        },
        {
          id: "gosx-engine-morph-pair-b",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-model-morph-pair-b",
          props: {
            width: 320,
            height: 180,
            autoRotate: false,
            models: [{ id: "morph-b", src: "/models/morph.glb", animation: "morph", loop: false }],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  // Different live playback per instance: the first-created mixer runs the
  // clip to its final weights while the second is held at all-zero weights.
  // Both packets still flow through the real WASM decoder and each mount's
  // real pose application, so the shared cached asset ends at two DIFFERENT
  // live poses — cross-instance deformation would show up as both mounts
  // collapsing onto one pose.
  const calls = installWasmMorphMixerStub(env.context, 0, (handle) => (handle === 1 ? FINAL_WEIGHTS : [0, 0, 0, 0, 0]));
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");

  const gltfApi = env.context.__gosx_scene3d_gltf_api;
  const originalApply = gltfApi.applyMorphPose;
  const ticks = [];
  gltfApi.applyMorphPose = function spiedApplyMorphPose(entries, weights, nodeTransforms) {
    originalApply.call(this, entries, weights, nodeTransforms);
    if (entries.length) {
      ticks.push({
        owner: entries[0].vertices,
        positionsRef: entries[0].vertices.positions,
        positions: Array.from(entries[0].vertices.positions),
      });
    }
  };

  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  assert.equal(mountA.getAttribute("data-gosx-scene3d-mounted"), "true", "first mount hydrated through the real mount path");
  assert.equal(mountB.getAttribute("data-gosx-scene3d-mounted"), "true", "second mount hydrated through the real mount path");
  assert.equal(mountA.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(mountB.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  assert.equal(calls.create, 2, "one WASM mixer per mounted instance");

  // Monotonic absolute RAF timestamps, as the manual harness requires.
  let clock = 48;
  for (let frame = 0; frame < 20; frame += 1) {
    raf.flush(clock);
    await flushAsyncWork();
    clock += 16;
  }

  assert.ok(ticks.length >= 2, "both instances ticked the morph apply seam");

  // Independent output arrays/owners: exactly two distinct live vertices
  // objects — one folded to the final weights, one held at the base pose.
  const perOwner = new Map();
  for (const tick of ticks) {
    perOwner.set(tick.owner, { positionsRef: tick.positionsRef, positions: tick.positions });
  }
  assert.equal(perOwner.size, 2, "each instance owns its own live vertices object");
  const states = Array.from(perOwner.values());
  assert.notEqual(states[0].positionsRef, states[1].positionsRef, "instances never alias one positions array");
  const morphedStates = states.filter((state) => trianglesClose(state.positions, FULL_FOLD));
  const heldStates = states.filter((state) => trianglesClose(state.positions, BASE_TRIANGLES));
  assert.equal(morphedStates.length, 1, "exactly one instance folded to the final weights");
  assert.equal(heldStates.length, 1, "the other instance stayed exactly at the base pose");

  // Renderer evidence per mount: the morphed triangle was uploaded to exactly
  // one instance's WebGL context; the held instance's context still shows the
  // base triangle and never the morphed one.
  const glA = mountA.children[0] && typeof mountA.children[0].getContext === "function"
    ? mountA.children[0].getContext("webgl2")
    : null;
  const glB = mountB.children[0] && typeof mountB.children[0].getContext === "function"
    ? mountB.children[0].getContext("webgl2")
    : null;
  assert.ok(glA && glB, "both mounts expose their own FakeWebGLContext");
  const fullInA = triangleBufferIds(snapshotUploads(glA), FULL_FOLD).length > 0;
  const fullInB = triangleBufferIds(snapshotUploads(glB), FULL_FOLD).length > 0;
  assert.notEqual(fullInA, fullInB, "the morphed triangle was uploaded to exactly one instance's renderer");
  const baseInA = triangleBufferIds(snapshotUploads(glA), BASE_TRIANGLES).length > 0;
  const baseInB = triangleBufferIds(snapshotUploads(glB), BASE_TRIANGLES).length > 0;
  assert.ok(baseInA || baseInB, "the held instance's renderer still shows the base triangle");

  // The shared cached asset stays immutable: deformation never leaks back
  // into the parsed source either instance was built from.
  const pristine = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/morph.glb");
  assertTriangle(pristine.objects[0].vertices.positions, BASE_TRIANGLES, "cached asset stays pristine");
  for (const state of states) {
    assert.notEqual(state.positionsRef, pristine.objects[0].vertices.positions, "instances do not alias the cached asset");
  }

  assert.equal(env.consoleLogs.error.length, 0);
});
