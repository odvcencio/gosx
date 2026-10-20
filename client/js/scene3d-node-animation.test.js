"use strict";
// glTF node TRS playback: end-to-end coverage for translation/rotation/scale
// clips on non-skinned, no-weight-channel GLB models through the REAL mount,
// hydration, JS motion mixer, live-event controls, renderer upload and
// disposal paths. One WASM test uses STUBBED __gosx_motion_mixer_* exports
// with the REAL sceneAnimWasmDecodePose decoder — it never claims to run a
// real WASM module. Expected coordinates are hand-calculated; no production
// matrix helpers are consulted. The existing runtime gates TRS-only assets
// out of mixer creation, so the playback assertions are expected to fail
// until the feature lands. Assertions are not weakened for the baseline.

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

const S = Math.SQRT1_2;
const BASE_TRI = [0, 0, 0, 1, 0, 0, 0, 1, 0];
// rig (+5x animated) * tri-node (scale 2): (0,0,0),(2,0,0),(0,1,0) then +5x.
const MOVED_TRI = [5, 0, 0, 7, 0, 0, 5, 2, 0];
// tri-node point (2,0,0): scale 2 -> (4,0,0), rig +5x -> (9,0,0).
const MOVED_POINT = [9, 0, 0];
// Static sibling line node authored at (3,0,0): rides the animated ancestor.
const MOVED_LINE = [8, 0, 0, 9, 0, 0];
// Ghost node authored scale [0,0,0] animated to [1,1,1]: valid base tri +5x.
const GHOST_TRI = [5, 0, 0, 6, 0, 0, 5, 1, 0];
// Rotation fixture: node0 animated Rz90 AND animated rig translation +5x,
// node1 authored scale [2,1,1], model translation +5x (total +10x).
// Scale diag(2,1,1) first: (0,0,0),(2,0,0),(0,1,0); then Rz90
// (x,y,z)->(-y,x,z): (0,0,0),(0,2,0),(-1,0,0); then +10x total.
const ROTATED_TRI = [10, 0, 0, 10, 2, 0, 9, 0, 0];
// Normal (1,0,1)/sqrt2: invT(world) = R*(S^-1)^T -> diag(0.5,1,1) gives
// (0.5,0,1)/sqrt2, Rz90 gives (0,0.5,1)/sqrt2 -> (0,1,2)/sqrt5. The plain
// upper-left 3x3 would yield (0,1,1)/sqrt2 instead. Translation drops out
// of the inverse-transpose, so the shared +10x does not affect normals.
const ROTATED_NORMAL = [0, 1 / Math.sqrt(5), 2 / Math.sqrt(5)];
// Instanced fixture: node translation +5x (animated); authored instance
// offsets (2,0,0) and (0,3,0). One mesh carries TRIANGLES, POINTS and LINES
// primitives, so every emitted geometry kind is instanced.
// Triangles (0,0,0),(1,0,0),(0,1,0): inst0 -> +2x +5x; inst1 -> +3y +5x.
const INST0_TRI = [7, 0, 0, 8, 0, 0, 7, 1, 0];
const INST1_TRI = [5, 3, 0, 6, 3, 0, 5, 4, 0];
// POINTS vertex (2,0,0): inst0 -> +2x +5x = (9,0,0); inst1 ->
// +2x +3y +5x = (7,3,0).
const INST0_POINT = [9, 0, 0];
const INST1_POINT = [7, 3, 0];
// LINES (0,0,0)-(1,0,0): inst0 -> base +2x +5x = (7,0,0)-(8,0,0);
// inst1 -> base +3y +5x = (5,3,0)-(6,3,0).
const INST0_LINE = [7, 0, 0, 8, 0, 0];
const INST1_LINE = [5, 3, 0, 6, 3, 0];

function assertClose(actual, expected, label, tolerance) {
  const tol = tolerance == null ? 1e-4 : tolerance;
  assert.ok(actual && actual.length === expected.length,
    label + ": length " + (actual && actual.length) + " want " + expected.length);
  for (let i = 0; i < expected.length; i += 1) {
    assert.ok(Math.abs(Number(actual[i]) - expected[i]) < tol,
      label + "[" + i + "]: " + actual[i] + " ~ " + expected[i]);
  }
}

// Builds a real GLB: rig parent (animated translation), triangle+point mesh
// node (animated scale unless opts.rotation), static-sibling line node, and
// an optional ghost triangle node authored at scale [0,0,0] with a scale
// channel to [1,1,1]. opts.rotation swaps the node1 scale channel for a
// constant Rz90 rotation channel on the rig plus authored nonuniform scale
// [2,1,1] and authored normals (1,0,1)/sqrt2. opts.instanced replaces the
// hierarchy with one EXT_mesh_gpu_instancing node (two instance offsets)
// whose single mesh carries TRIANGLES, POINTS and LINES primitives.
// Clip "move": times [0, 0.2, 10] with a flat tail, so any sample after
// ~0.2s yields the final pose regardless of frame cadence (duration 10,
// looped).
function buildTRSGLBBytes(opts) {
  opts = opts || {};
  const floats = [];
  const accessors = [];
  function acc(values, type, count) {
    const index = accessors.length;
    accessors.push({ bufferView: 0, byteOffset: floats.length * 4, componentType: 5126, count, type });
    for (const v of values) floats.push(v);
    return index;
  }
  const posAcc = acc(BASE_TRI, "VEC3", 3);
  const nrmAcc = opts.normals ? acc([S, 0, S, S, 0, S, S, 0, S], "VEC3", 3) : -1;
  const pointAcc = acc([2, 0, 0], "VEC3", 1);
  const lineAcc = acc([0, 0, 0, 1, 0, 0], "VEC3", 2);
  const timesAcc = acc([0, 0.2, 10], "SCALAR", 3);
  const rigTAcc = acc([0, 0, 0, 5, 0, 0, 5, 0, 0], "VEC3", 3);
  const triSAcc = acc([1, 1, 1, 2, 2, 2, 2, 2, 2], "VEC3", 3);
  const ghostSAcc = acc([0, 0, 0, 1, 1, 1, 1, 1, 1], "VEC3", 3);
  const rotQAcc = acc([0, 0, S, S, 0, 0, S, S, 0, 0, S, S], "VEC4", 3);
  const instTAcc = opts.instanced ? acc([2, 0, 0, 0, 3, 0], "VEC3", 2) : -1;
  const idxAcc = accessors.length;
  accessors.push({ bufferView: 0, byteOffset: floats.length * 4, componentType: 5123, count: 3, type: "SCALAR" });

  const triAttributes = { POSITION: posAcc };
  if (nrmAcc >= 0) triAttributes.NORMAL = nrmAcc;
  const meshes = [
    { name: "tri", primitives: opts.instanced
      ? [
          { attributes: triAttributes, indices: idxAcc, mode: 4 },
          { attributes: { POSITION: pointAcc }, mode: 0 },
          { attributes: { POSITION: lineAcc }, mode: 1 },
        ]
      : [
          { attributes: triAttributes, indices: idxAcc, mode: 4 },
          { attributes: { POSITION: pointAcc }, mode: 0 },
        ] },
    { name: "filament", primitives: [{ attributes: { POSITION: lineAcc }, mode: 1 }] },
    { name: "ghost", primitives: [{ attributes: { POSITION: posAcc }, indices: idxAcc, mode: 4 }] },
  ];

  let nodes;
  if (opts.instanced) {
    nodes = [{
      name: "inst",
      mesh: 0,
      extensions: { EXT_mesh_gpu_instancing: { attributes: { TRANSLATION: instTAcc } } },
    }];
  } else {
    nodes = [
      { name: "rig", children: opts.ghost ? [1, 2, 3] : [1, 2] },
      { name: "tri", mesh: 0 },
      { name: "filament", mesh: 1, translation: [3, 0, 0] },
    ];
    if (opts.rotation) nodes[1].scale = [2, 1, 1];
    if (opts.ghost) nodes.push({ name: "ghost", mesh: 2, scale: [0, 0, 0] });
  }

  const samplers = [];
  const channels = [];
  function channel(output, node, path) {
    samplers.push({ input: timesAcc, output, interpolation: "LINEAR" });
    channels.push({ sampler: samplers.length - 1, target: { node, path } });
  }
  channel(rigTAcc, 0, "translation");
  if (opts.rotation) {
    channel(rotQAcc, 0, "rotation");
  } else if (!opts.instanced) {
    channel(triSAcc, 1, "scale");
  }
  if (opts.ghost) channel(ghostSAcc, 3, "scale");

  const doc = {
    asset: { version: "2.0" },
    scene: 0,
    scenes: [{ nodes: [0] }],
    nodes,
    meshes,
    animations: [{ name: "move", channels, samplers }],
    accessors,
    bufferViews: [{ buffer: 0, byteOffset: 0, byteLength: 0 }],
    buffers: [{ byteLength: 0 }],
  };
  if (opts.instanced) doc.extensionsUsed = ["EXT_mesh_gpu_instancing"];

  const floatBytes = new Uint8Array(new Float32Array(floats).buffer);
  const indexBytes = new Uint8Array(new Uint16Array([0, 1, 2]).buffer);
  const binPadded = (floatBytes.length + indexBytes.length + 3) & ~3;
  const bin = new Uint8Array(binPadded);
  bin.set(floatBytes, 0);
  bin.set(indexBytes, floatBytes.length);
  doc.bufferViews[0].byteLength = binPadded;
  doc.buffers[0].byteLength = binPadded;

  const jsonRaw = Buffer.from(JSON.stringify(doc), "utf8");
  const jsonPadded = (jsonRaw.length + 3) & ~3;
  const json = Buffer.alloc(jsonPadded, 0x20);
  jsonRaw.copy(json, 0);
  const total = 12 + 8 + jsonPadded + 8 + binPadded;
  const glb = Buffer.alloc(total);
  let o = 0;
  glb.writeUInt32LE(0x46546C67, o); o += 4;
  glb.writeUInt32LE(2, o); o += 4;
  glb.writeUInt32LE(total, o); o += 4;
  glb.writeUInt32LE(jsonPadded, o); o += 4;
  glb.writeUInt32LE(0x4E4F534A, o); o += 4;
  json.copy(glb, o); o += jsonPadded;
  glb.writeUInt32LE(binPadded, o); o += 4;
  glb.writeUInt32LE(0x004E4942, o); o += 4;
  Buffer.from(bin.buffer, bin.byteOffset, bin.length).copy(glb, o);
  return Array.from(glb);
}

function mountTRSEngine(mountId, engineId, modelProps, routes) {
  const mount = new FakeElement("div", null);
  mount.id = mountId;
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: Object.assign({
      "/models/trs.glb": { bytes: buildTRSGLBBytes() },
    }, routes || {}),
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
          models: [Object.assign(
            { id: "trs", src: "/models/trs.glb", animation: "move", loop: true },
            modelProps || {},
          )],
        },
      }],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  return { mount, env };
}

function spyMixers(env) {
  const api = env.context.__gosx_scene3d_animation_api;
  const original = api.createMixer;
  const created = [];
  api.createMixer = function trackedCreateMixer(...args) {
    created.push(original.apply(this, args));
    return created[created.length - 1];
  };
  return created;
}

// One monotonically advancing clock per RAF handle, carried across play
// phases: stop/replay must never observe absolute time going backwards.
async function playFrames(raf, count) {
  if (typeof raf.__trsClock !== "number") raf.__trsClock = 48;
  for (let i = 0; i < count; i += 1) {
    raf.flush(raf.__trsClock);
    await flushAsyncWork();
    raf.__trsClock += 16;
  }
}

function stateObject(mount, id) {
  const state = mount.__gosxScene3DState;
  return state && state.objects && state.objects.get ? state.objects.get(id) : null;
}

function objectPositions(mount, id) {
  const object = stateObject(mount, id);
  return object && object.vertices ? object.vertices.positions : null;
}

function linePointsOf(mount, id) {
  const object = stateObject(mount, id);
  return object && Array.isArray(object.points) ? object.points : null;
}

function pointPositionsOf(mount, id) {
  const state = mount.__gosxScene3DState;
  const entry = state && Array.isArray(state.points)
    ? state.points.find((point) => point.id === id)
    : null;
  return entry ? entry.positions : null;
}

// Read-only accessor over the real mount state. After real disposal
// __gosxScene3DState is deleted, so this returns [] — asserted in the
// disposal tests as teardown evidence.
function modelRecords(mount, modelID) {
  const state = mount.__gosxScene3DState;
  const skins = state && Array.isArray(state._modelSkins) ? state._modelSkins : [];
  return skins.filter((record) => record && record.id === modelID);
}

test("TRS-only clip moves triangle, point and line geometry through the JS mixer; animated ancestor moves the static sibling; authored zero scale becomes nonzero", async (t) => {
  const { mount, env } = mountTRSEngine("scene-trs-root", "gosx-engine-trs", null, {
    "/models/trs.glb": { bytes: buildTRSGLBBytes({ ghost: true }) },
  });
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs"); } catch (error) {} });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  // Spy AFTER runScript (the bootstrap publishes the API object) but BEFORE
  // the first flushAsyncWork, so no mixer creation is missed.
  const mixers = spyMixers(env);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);

  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mixers.length, 1, "a TRS-only model must initialize exactly one motion mixer");

  await playFrames(raf, 20);

  assertClose(Array.from(objectPositions(mount, "trs/tri-prim-0")), MOVED_TRI,
    "animated scale + ancestor translation on triangles");
  assertClose(Array.from(pointPositionsOf(mount, "trs/tri-points-1")), MOVED_POINT,
    "animated scale + ancestor translation on points");
  const line = linePointsOf(mount, "trs/filament-lines-0");
  assert.ok(line && line.length === 2, "line object hydrated");
  assertClose([line[0].x, line[0].y, line[0].z, line[1].x, line[1].y, line[1].z], MOVED_LINE,
    "static sibling rides the animated ancestor");
  assertClose(Array.from(objectPositions(mount, "trs/ghost-prim-0")), GHOST_TRI,
    "authored zero scale must not block a later valid nonzero pose");

  // Point render cache must receive the changed positions, not just metadata.
  const gl = mount.children[0].getContext("webgl2");
  const uploads = Array.from(gl.bufferUploads.values());
  assert.ok(
    uploads.some((data) => data.length === 3
      && Math.abs(data[0] - 9) < 1e-3 && Math.abs(data[1]) < 1e-3 && Math.abs(data[2]) < 1e-3),
    "point position buffer uploaded with animated coordinates",
  );

  assert.equal(env.consoleLogs.error.length, 0);
});

test("two mounts of one cached TRS GLB play independently; stop restores authored defaults; replay resumes; cached source stays pristine", async (t) => {
  const mountA = new FakeElement("div", null);
  mountA.id = "scene-trs-pair-a";
  const mountB = new FakeElement("div", null);
  mountB.id = "scene-trs-pair-b";
  const env = createContext({
    elements: [mountA, mountB],
    enableWebGL2: true,
    disableCanvas2D: true,
    fetchRoutes: { "/models/trs.glb": { bytes: buildTRSGLBBytes() } },
    manifest: {
      engines: [
        {
          id: "gosx-engine-trs-a", component: "GoSXScene3D", kind: "surface",
          mountId: "scene-trs-pair-a",
          props: {
            width: 320, height: 180, autoRotate: false,
            models: [{ id: "trs-a", src: "/models/trs.glb", animation: "move", loop: true, live: ["cmd"] }],
          },
        },
        {
          id: "gosx-engine-trs-b", component: "GoSXScene3D", kind: "surface",
          mountId: "scene-trs-pair-b",
          props: {
            width: 320, height: 180, autoRotate: false,
            models: [{ id: "trs-b", src: "/models/trs.glb" }],
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs-a"); } catch (error) {} });
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs-b"); } catch (error) {} });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  // Spy after runScript, before the first flush: both mixers register during
  // async preparation.
  const mixers = spyMixers(env);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await playFrames(raf, 20);

  assert.equal(mountA.getAttribute("data-gosx-scene3d-mounted"), "true");
  assert.equal(mountB.getAttribute("data-gosx-scene3d-mounted"), "true");
  // Preparation registers one record + mixer per model whenever the parsed
  // asset carries clips, regardless of requestedAnimation; only A plays.
  assert.equal(mixers.length, 2, "one mixer per mounted model (clips present)");
  const recordsA = modelRecords(mountA, "trs-a");
  const recordsB = modelRecords(mountB, "trs-b");
  assert.equal(recordsA.length, 1, "exactly one animation record for model A");
  assert.equal(recordsB.length, 1, "exactly one animation record for model B (no duplicates)");
  assert.ok(recordsA[0].mixer && recordsB[0].mixer, "each model record owns its mixer");
  assert.notEqual(recordsA[0].mixer, recordsB[0].mixer, "mixer ownership is independent per mount");
  assert.equal(recordsA[0].animation, "move", "model A plays the requested clip");
  assert.equal(recordsB[0].animation, "", "model B never starts playback");
  assertClose(Array.from(objectPositions(mountA, "trs-a/tri-prim-0")), MOVED_TRI, "mount A plays the clip");
  assertClose(Array.from(objectPositions(mountB, "trs-b/tri-prim-0")), BASE_TRI, "mount B holds authored defaults");

  env.document.dispatchEvent(new env.context.CustomEvent("gosx:hub:event", {
    detail: { event: "cmd", data: { "trs-a": { animation: "" } } },
  }));
  await playFrames(raf, 6);
  assertClose(Array.from(objectPositions(mountA, "trs-a/tri-prim-0")), BASE_TRI,
    "stop restores authored defaults");

  env.document.dispatchEvent(new env.context.CustomEvent("gosx:hub:event", {
    detail: { event: "cmd", data: { "trs-a": { animation: "move", animationSeq: "r2" } } },
  }));
  await playFrames(raf, 20);
  assertClose(Array.from(objectPositions(mountA, "trs-a/tri-prim-0")), MOVED_TRI,
    "replay re-applies the clip");

  const pristine = await env.context.__gosx_scene3d_gltf_api.sceneLoadGLTFModel("/models/trs.glb");
  assertClose(Array.from(pristine.objects[0].vertices.positions), BASE_TRI,
    "cached parsed asset stays pristine");
  assert.notEqual(pristine.objects[0].vertices.positions, objectPositions(mountA, "trs-a/tri-prim-0"),
    "live instances never alias the cached asset streams");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("animated rotation with authored nonuniform scale transforms normals through the inverse transpose", async (t) => {
  const { mount, env } = mountTRSEngine("scene-trs-normals-root", "gosx-engine-trs-normals",
    { x: 5 },
    { "/models/trs.glb": { bytes: buildTRSGLBBytes({ rotation: true, normals: true }) } });
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs-normals"); } catch (error) {} });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  spyMixers(env);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await playFrames(raf, 20);

  const object = stateObject(mount, "trs/tri-prim-0");
  assert.ok(object && object.vertices, "triangle object hydrated");
  assertClose(Array.from(object.vertices.positions), ROTATED_TRI,
    "animated rotation composes with authored nonuniform scale and the model transform");
  assertClose(Array.from(object.vertices.normals.slice(0, 3)), ROTATED_NORMAL,
    "inverse-transpose normal under nonuniform scale");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("gpu-instanced TRS node keeps authored instance offsets under animated translation for triangles, points and lines", async (t) => {
  const { mount, env } = mountTRSEngine("scene-trs-inst-root", "gosx-engine-trs-inst", null, {
    "/models/trs.glb": { bytes: buildTRSGLBBytes({ instanced: true }) },
  });
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs-inst"); } catch (error) {} });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  spyMixers(env);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await playFrames(raf, 20);

  // Primitive IDs come from mesh.name ("tri"), not node.name ("inst"):
  // gltfPrimitiveID = (mesh.name || 'mesh-' + meshIndex) + '-' + channel
  // + '-' + primitiveIndex + suffix.
  assertClose(Array.from(objectPositions(mount, "trs/tri-prim-0-inst-0")), INST0_TRI,
    "instance 0 triangles retain their offset under the animated node translation");
  assertClose(Array.from(objectPositions(mount, "trs/tri-prim-0-inst-1")), INST1_TRI,
    "instance 1 triangles retain their offset under the animated node translation");
  assertClose(Array.from(pointPositionsOf(mount, "trs/tri-points-1-inst-0")), INST0_POINT,
    "instance 0 points retain their offset under the animated node translation");
  assertClose(Array.from(pointPositionsOf(mount, "trs/tri-points-1-inst-1")), INST1_POINT,
    "instance 1 points retain their offset under the animated node translation");
  const instLine0 = linePointsOf(mount, "trs/tri-lines-2-inst-0");
  assert.ok(instLine0 && instLine0.length === 2, "instanced line object hydrated for instance 0");
  assertClose([instLine0[0].x, instLine0[0].y, instLine0[0].z,
    instLine0[1].x, instLine0[1].y, instLine0[1].z], INST0_LINE,
    "instance 0 lines retain their offset under the animated node translation");
  const instLine1 = linePointsOf(mount, "trs/tri-lines-2-inst-1");
  assert.ok(instLine1 && instLine1.length === 2, "instanced line object hydrated for instance 1");
  assertClose([instLine1[0].x, instLine1[0].y, instLine1[0].z,
    instLine1[1].x, instLine1[1].y, instLine1[1].z], INST1_LINE,
    "instance 1 lines retain their offset under the animated node translation");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("WASM motion mixer route (STUBBED mixer exports, real pose decoder) drives TRS playback and disposes", async (t) => {
  const { mount, env } = mountTRSEngine("scene-trs-wasm-root", "gosx-engine-trs-wasm");
  // Explicit labeling: the __gosx_motion_mixer_* exports below are stubs;
  // the packed records they emit are decoded by the REAL
  // sceneAnimWasmDecodePose through the published animation API. No real
  // WASM module executes in this test.
  const calls = { create: 0, play: [], update: 0, destroy: [] };
  env.context.__gosx_motion_wasm = true;
  env.context.__gosx_motion_mixer_create = () => { calls.create += 1; return 1; };
  env.context.__gosx_motion_mixer_add_clip = () => true;
  env.context.__gosx_motion_mixer_play = (handle, name, fadeIn, loop, speed, weight) => {
    calls.play.push({ name, fadeIn, loop, speed, weight });
  };
  env.context.__gosx_motion_mixer_stop = () => {};
  env.context.__gosx_motion_mixer_is_playing = () => true;
  env.context.__gosx_motion_mixer_update = (handle, dt, reduced, outU8) => {
    calls.update += 1;
    const f = new Float64Array(outU8.buffer, outU8.byteOffset, Math.floor(outU8.byteLength / 8));
    // [targetID, propID, arity, comps...]: node0 translation (propID 0),
    // node1 scale (propID 2); arity 2 = vec3.
    f[0] = 0; f[1] = 0; f[2] = 2; f[3] = 5; f[4] = 0; f[5] = 0;
    f[6] = 1; f[7] = 2; f[8] = 2; f[9] = 2; f[10] = 2; f[11] = 2;
    return 12;
  };
  env.context.__gosx_motion_mixer_destroy = (handle) => { calls.destroy.push(handle); };

  // Safety net: the explicit dispose below is the meaningful assertion, but a
  // failed assertion must not leak the mounted engine.
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs-wasm"); } catch (error) {} });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await playFrames(raf, 20);

  assert.equal(calls.create, 1, "one WASM mixer handle created for the TRS-only model");
  assert.ok(calls.update > 0, "per-frame pose ticks the WASM mixer");
  assertClose(Array.from(objectPositions(mount, "trs/tri-prim-0")), MOVED_TRI,
    "decoded packed TRS records move the live geometry");

  env.context.__gosx_dispose_engine("gosx-engine-trs-wasm");
  await flushAsyncWork();
  assert.deepEqual(calls.destroy, [1], "WASM mixer destroyed exactly once on dispose");
  assert.equal(raf.count(), 0, "no RAF handles remain after dispose");
  assert.equal(modelRecords(mount, "trs").length, 0,
    "the animation record is torn down on dispose");
  assert.equal(env.consoleLogs.error.length, 0);
});

test("TRS model disposal through the JS mixer path tears down cleanly", async (t) => {
  const { mount, env } = mountTRSEngine("scene-trs-dispose-root", "gosx-engine-trs-dispose");
  t.after(() => { try { env.context.__gosx_dispose_engine("gosx-engine-trs-dispose"); } catch (error) {} });
  const raf = installManualRAF(env.context);
  runScript(bootstrapSource, env.context, "bootstrap.js");
  spyMixers(env);
  await flushAsyncWork();
  await flushSceneInitialFrameBoundary(raf);
  await playFrames(raf, 4);
  assert.equal(mount.getAttribute("data-gosx-scene3d-mounted"), "true");
  env.context.__gosx_dispose_engine("gosx-engine-trs-dispose");
  await flushAsyncWork();
  assert.equal(raf.count(), 0, "no RAF handles remain after dispose");
  assert.equal(modelRecords(mount, "trs").length, 0,
    "the animation record is torn down on dispose");
  assert.equal(env.consoleLogs.error.length, 0);
});
