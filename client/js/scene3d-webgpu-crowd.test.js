"use strict";

// Crowd batches on the WebGPU renderer, against the recording fake device. The
// fake runs no shader, so these tests pin what the renderer asks of the GPU:
// the resources it makes, the passes it records, what a dirty range uploads.
// Pixels are checked against WebGL in scripts/scene3d-crowd-webgpu-parity.mjs,
// in a real browser.

const test = require("node:test");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { createBoardWebGPUHarness, flushAsyncWork, mainRenderPasses, renderPreparedFrame } = require("./runtime-test-harness.js");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

function runtimeSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
}

const RECORD_FLOATS = 24;

// The scene runtime runs in its own vm realm, where instanceof Float32Array
// fails for an array made here. Every typed array handed to the planner comes
// from that realm.
let F32 = Float32Array;
let U32 = Uint32Array;

function crowdVertices() {
  const Float32Array = F32;
  const positions = [-0.2, 0, 0.2, 0.2, 0, 0.2, 0, 0.6, 0, 0.2, 0, 0.2, 0.2, 0, -0.2, 0, 0.6, 0];
  const count = positions.length / 3;
  return {
    count, immutable: true, revision: 0, positions: new Float32Array(positions),
    normals: new Float32Array(count * 3).fill(0.5), uvs: new Float32Array(count * 2),
    tangents: new Float32Array(count * 4).map((_v, i) => (i % 4 === 0 || i % 4 === 3 ? 1 : 0)),
    joints: new Float32Array(count * 4), weights: new Float32Array(count * 4).map((_v, i) => (i % 4 === 0 ? 1 : 0)),
  };
}

function crowdAtlas() {
  const Float32Array = F32;
  const width = 4, height = 8, data = new Float32Array(width * height * 4);
  for (let row = 0; row < height; row++) {
    const o = row * width * 4;
    data[o] = data[o + 5] = data[o + 10] = data[o + 15] = 1;
    data[o + 13] = row * 0.03;
  }
  return { id: "test-atlas", width, height, data, clips: new Map([["Run", { start: 1, segments: 6, duration: 1 }]]), missingClips: new Set() };
}

// One crowd of `count` agents the way hydration leaves them: skinned agents with
// _crowdSkin (and _crowdMotion in motion mode), each declared by a parent matrix.
function crowd(api, count, options = {}) {
  const Float32Array = F32;
  const vertices = crowdVertices();
  const atlas = options.atlas || crowdAtlas();
  const bounds = { minX: -0.3, minY: 0, minZ: -0.3, maxX: 0.3, maxY: 1.2, maxZ: 0.3 };
  const agents = [];
  for (let i = 0; i < count; i++) {
    const agent = api.normalizeSceneObject({ id: "p/a" + i + "/0", kind: "mesh", vertices, materialKind: "standard", color: "#d98c5f", roughness: 0.6 }, i);
    Object.assign(agent, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
      static: false, skin: null, pickable: true, castShadow: options.shadows === true, receiveShadow: false, depthWrite: true,
      parentMatrix: new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, (i % 10) * 0.5 - 2, 0, -3 - Math.floor(i / 10), 1]) });
    agent.vertices.immutable = true;
    agent.vertices.revision = 0;
    agent._crowdSkin = { atlas, bounds, rows: new Float32Array([i % 6, (i % 6) + 1, 0.5]), poseRows: null, batchID: "p", rev: 0, group: null,
      parent: options.parent || "" };
    if (options.motion) {
      const record = new Float32Array(RECORD_FLOATS);
      record.set([agent.parentMatrix[12], 0, agent.parentMatrix[14], 0, 0, 0, 1, 1, 1, 0, agent.parentMatrix[12] + 0.2, 0, agent.parentMatrix[14], 0, 0, 0, 1, 1, 1, 1, 1, 0, 1, 1]);
      agent._crowdMotion = { atlas, clipTable: { count: 2, data: new Float32Array([0, 0, 0, 0, 1, 6, 1, 0]) }, record, dirty: true, localRadius: 1,
        bounds: { minX: -2, minY: 0, minZ: -9, maxX: 2, maxY: 1.2, maxZ: -2 } };
    }
    agents.push(agent);
  }
  return { agents, atlas, vertices };
}

function bundleOf(api, objects, extra = {}) {
  const bundle = api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 1.2, z: 3, fov: 62, near: 0.05, far: 100 }, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false,
    { retainedGeometry: true, worldBakedAttributeViews: false, rigidImportedBatches: false, crowdBatches: true });
  bundle.lights = extra.lights || [];
  if (extra.gpuDriven) bundle.gpuDriven = extra.gpuDriven;
  return bundle;
}

async function setup(options = {}) {
  const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: Object.assign({ renderBundles: true }, options.device || {}) });
  // The fake device keeps no labels and drops writeBuffer's data offset and
  // size; record both so a test can find a buffer and see what a write carried.
  harness.writes = [];
  const queue = harness.fake.device.queue;
  const writeBuffer = queue.writeBuffer;
  queue.writeBuffer = function(buffer, offset, data, dataOffset, size) {
    writeBuffer.apply(this, arguments);
    const view = ArrayBuffer.isView(data);
    const unit = view ? data.BYTES_PER_ELEMENT : 1;
    const start = (dataOffset || 0) * unit;
    const length = size === undefined ? data.byteLength - start : size * unit;
    const bytes = new Uint8Array(view ? data.buffer : data, (view ? data.byteOffset : 0) + start, length).slice();
    harness.writes.push({ buffer, offset, data: new Float32Array(bytes.buffer), u32: new Uint32Array(bytes.buffer) });
  };
  const createBuffer = harness.fake.device.createBuffer;
  harness.fake.device.createBuffer = function(desc) {
    const buffer = createBuffer.call(this, desc);
    buffer.label = desc && desc.label;
    return buffer;
  };
  F32 = vm.runInContext("Float32Array", harness.env.context);
  U32 = vm.runInContext("Uint32Array", harness.env.context);
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  harness.api = harness.env.context.__gosx_scene3d_api;
  return harness;
}

let frameNumber = 0;
async function draw(harness, bundle) {
  frameNumber += 1;
  const result = await renderPreparedFrame(harness.renderer, bundle, { width: 64, height: 64 }, { nowMS: frameNumber * 16, sceneTime: 0.5, active: true });
  assert.equal(result, "submitted");
  await flushAsyncWork();
  return harness.mount.__gosxScene3DWebGPUStats || {};
}

// The cull kernels compile asynchronously: the first frames draw unculled, so
// settle the pipelines and draw again until the culling the scene asked for is
// on, as a running scene does. The pass records of the last frame stay.
async function drawCulled(harness, makeBundle) {
  let stats = await draw(harness, makeBundle());
  for (let frame = 0; frame < 6 && !(stats.crowdCulling && (stats.crowdOcclusion || !makeBundle().gpuDriven.occlusion)); frame++) {
    await flushAsyncWork();
    await flushAsyncWork();
    harness.fake.state.renderPasses.length = 0;
    harness.fake.state.computePasses.length = 0;
    stats = await draw(harness, makeBundle());
  }
  return stats;
}

// Every write to one buffer, in call order, with the bytes it carried. The
// fake device drops writeBuffer's offset and size arguments, so setup wraps it.
function writesTo(harness, buffer) {
  return harness.writes.filter((call) => call.buffer === buffer);
}

function bufferWithLabel(harness, label) {
  return harness.fake.state.buffers.filter((buffer) => buffer.label === "gosx.crowd." + label);
}

test("the shader constants match the WebGL and animation sources", () => {
  const animation = runtimeSource("animation.ts");
  const webgl = readSceneRendererBackendSrc("webgl");
  const crowd = readSceneRendererBackendSrc("webgpu");
  const value = (source, name) => Number(new RegExp("const " + name + " = ([0-9.]+);").exec(source)[1]);
  assert.equal(value(crowd, "SCENE_WEBGPU_CROWD_RECORD_FLOATS"), value(animation, "SCENE_CROWD_MOTION_RECORD_FLOATS"));
  assert.equal(value(crowd, "SCENE_WEBGPU_CROWD_RECORD_FLOATS"), value(webgl, "SCENE_CROWD_MOTION_RECORD_FLOATS"));
  assert.equal(value(crowd, "SCENE_WEBGPU_CROWD_EXTRAPOLATION_SECONDS"), value(animation, "SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS"));
  assert.equal(value(crowd, "SCENE_WEBGPU_CROWD_EXTRAPOLATION_SECONDS"), value(webgl, "SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS"));
  assert.equal(value(crowd, "SCENE_WEBGPU_CROWD_MAX_CLIPS"), value(webgl, "SCENE_CROWD_MOTION_MAX_CLIPS"));
  assert.equal(value(crowd, "SCENE_WEBGPU_CROWD_MAX_CLIPS"), value(animation, "SCENE_CROWD_MOTION_CLIP_LIMIT"));
});

test("the motion shader repeats the WebGL motion math", () => {
  // Same expressions as the WebGL SCENE_CROWD_SKIN_MOTION_GLSL, in WGSL: the
  // factor with extrapolation, the shortest-arc angle lerp, the R = Rz*Ry*Rx
  // columns, the loop wrap and the segment blend.
  const crowd = readSceneRendererBackendSrc("webgpu");
  for (const piece of [
    "return 1.0 + min(now - tNext, crowdParams.timing.y) / span;",
    "d = d - CROWD_TWO_PI * floor((d + CROWD_PI) / CROWD_TWO_PI);",
    "vec4f(cy * cz * scale.x, cy * sz * scale.x, -sy * scale.x, 0.0)",
    "vec4f((sx * sy * cz - cx * sz) * scale.y, (sx * sy * sz + cx * cz) * scale.y, sx * cy * scale.y, 0.0)",
    "vec4f((cx * sy * cz + sx * sz) * scale.z, (cx * sy * sz - sx * cz) * scale.z, cx * cy * scale.z, 0.0)",
    "t = elapsed - duration * floor(elapsed / duration)",
    "return vec3f(clip.x + a, clip.x + min(segments, a + 1.0), frame - a);",
    "m = m + (a * (1.0 - pose.z) + b * pose.z) * w;",
  ]) assert.ok(crowd.includes(piece), "missing: " + piece);
});

test("crowd shaders keep their bindings and vertex inputs consistent", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 4);
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(harness.api, agents));
  const color = harness.fake.state.renderPipelines.find((p) => p.desc.label === "gosx-crowd-color-pose");
  assert.ok(color, "pose color pipeline");
  assert.equal(harness.fake.state.renderPipelines.find((p) => p.desc.label === "gosx-crowd-shadow-pose"), undefined, "no shadow pipeline before a shadow pass");
  const code = color.desc.vertex.module.code;
  assert.match(code, /@group\(2\) @binding\(0\) var<uniform> crowdParams: CrowdParams;/);
  assert.match(code, /@group\(2\) @binding\(1\) var crowdAtlas: texture_2d<f32>;/);
  assert.match(code, /@group\(2\) @binding\(2\) var<storage, read> crowdAgents: array<f32>;/);
  assert.match(code, /@group\(2\) @binding\(3\) var<storage, read> crowdPoses: array<f32>;/);
  assert.match(code, /@group\(2\) @binding\(4\) var<storage, read> crowdTints: array<vec4f>;/);
  assert.match(code, /@location\(4\) crowdSlot: u32,/);
  assert.match(code, /@location\(5\) joints: vec4f,/);
  assert.match(code, /@location\(6\) weights: vec4f,/);
  assert.equal(code.split("{").length, code.split("}").length, "braces balance");
  const layouts = color.desc.layout.desc.bindGroupLayouts;
  assert.equal(layouts.length, 3);
  assert.deepEqual(Array.from(layouts[2].desc.entries, (e) => e.binding), [0, 1, 2, 3, 4]);
  assert.equal(layouts[2].desc.entries[1].texture.sampleType, "unfilterable-float");
  const buffers = color.desc.vertex.buffers;
  assert.deepEqual(Array.from(buffers, (b) => b.stepMode), ["vertex", "vertex", "vertex", "vertex", "instance", "vertex", "vertex"]);
  assert.deepEqual(Array.from(buffers, (b) => b.attributes[0].shaderLocation), [0, 1, 2, 3, 4, 5, 6]);
  assert.equal(buffers[4].attributes[0].format, "uint32");
});

test("prepareCrowdAtlas uploads the palette once and rejects an atlas the device cannot hold", async () => {
  const harness = await setup();
  const atlas = crowdAtlas();
  const before = harness.fake.state.textures.length;
  assert.equal(typeof harness.renderer.prepareCrowdAtlas, "function");
  assert.equal(typeof harness.renderer.prepareCrowdMotionShaders, "function");
  assert.equal(harness.renderer.prepareCrowdMotionShaders(), true);
  harness.renderer.prepareCrowdAtlas(atlas);
  harness.renderer.prepareCrowdAtlas(atlas);
  const made = harness.fake.state.textures.slice(before);
  assert.equal(made.length, 1, "one texture for one atlas");
  assert.equal(made[0].desc.format, "rgba32float");
  assert.deepEqual(Array.from(made[0].desc.size), [atlas.width, atlas.height, 1]);
  const upload = harness.fake.state.writeTextureCalls.find((call) => call.texture === made[0]);
  assert.ok(upload && upload.data.length === atlas.data.length, "the palette data reaches the texture");
  assert.throws(() => harness.renderer.prepareCrowdAtlas({ id: "huge", width: 65536, height: 4, data: new Float32Array(16) }), /texture dimensions/);
  assert.throws(() => harness.renderer.prepareCrowdAtlas({ id: "empty", width: 0, height: 0, data: new Float32Array(0) }), /empty atlas/);
});

test("a pose-mode batch is one draw of every agent", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 12);
  harness.renderer.prepareCrowdAtlas(atlas);
  const bundle = bundleOf(harness.api, agents);
  const records = bundle.meshObjects.filter((record) => record._crowdBatch);
  assert.equal(records.length, 1, "the planner sends one record for the batch");
  const stats = await draw(harness, bundle);
  const passes = mainRenderPasses(harness.fake);
  const color = passes[passes.length - 1];
  const colorDraws = color.draws.filter((d) => d.pipeline && d.pipeline.desc.label === "gosx-crowd-color-pose");
  assert.equal(colorDraws.length, 1);
  assert.equal(colorDraws[0].instanceCount, 12);
  assert.equal(colorDraws[0].vertexCount, 6);
  assert.equal(stats.crowdBatches, 1);
  assert.equal(stats.crowdAgents, 12);
  assert.equal(stats.crowdDraws, 1);
  assert.equal(stats.crowdCulling, false, "no gpuDriven opt-in, no culling");
  assert.equal(stats.meshDrawCalls, 1, "the batch counts as one mesh draw");
  assert.equal(harness.fake.state.computePipelines.filter((p) => /crowd/.test(p.label)).length, 0);
  // The batch took group 2 (agents) and the identity survivor list as instance data.
  const group2 = color.bindGroups.filter((g) => g.slot === 2).pop();
  assert.ok(group2 && group2.group.desc.label === "gosx-crowd");
  const slotBuffer = color.vertexBuffers.find((v) => v.slot === 4);
  assert.equal(slotBuffer.size, 12 * 4);
  assert.equal(color.vertexBuffers.filter((v) => v.slot >= 5).length, 2, "joints and weights");
  // Agents and poses upload as the planner packed them.
  const agentsBuffer = bufferWithLabel(harness, "agents")[0], posesBuffer = bufferWithLabel(harness, "poses")[0];
  assert.deepEqual(Array.from(writesTo(harness, agentsBuffer)[0].data), Array.from(records[0].crowdTransforms));
  assert.deepEqual(Array.from(writesTo(harness, posesBuffer)[0].data), Array.from(records[0].crowdPoses));
});

test("an unchanged revision uploads nothing and a changed pose uploads the pose streams", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 12);
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(harness.api, agents));
  const agentsBuffer = bufferWithLabel(harness, "agents")[0];
  const first = writesTo(harness, agentsBuffer).length;
  await draw(harness, bundleOf(harness.api, agents));
  assert.equal(writesTo(harness, agentsBuffer).length, first, "same revision, no upload");
  agents[3]._crowdSkin.rows.set([5, 6, 0.25]);
  agents[3]._crowdSkin.rev += 1;
  await draw(harness, bundleOf(harness.api, agents));
  assert.equal(writesTo(harness, agentsBuffer).length, first + 1);
  const posesBuffer = bufferWithLabel(harness, "poses")[0];
  const poses = writesTo(harness, posesBuffer).pop().data;
  assert.deepEqual(Array.from(poses.slice(9, 12)), [5, 6, 0.25]);
});

test("the parent matrix reaches the batch uniform every frame", async () => {
  const harness = await setup();
  const ship = harness.api.normalizeSceneObject({ id: "ship/hull", kind: "mesh", vertices: crowdVertices(), materialKind: "standard", color: "#884422", pickable: false }, 99);
  Object.assign(ship, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
    static: false, skin: null, visible: false, parentMatrix: new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]) });
  const { agents, atlas } = crowd(harness.api, 6, { parent: "ship" });
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(harness.api, [ship, ...agents]));
  const params = bufferWithLabel(harness, "params")[0];
  const first = writesTo(harness, params).pop().data;
  assert.deepEqual(Array.from(first.slice(0, 16)), [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  ship.parentMatrix.set([0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 5, 0.5, -2, 1]);
  await draw(harness, bundleOf(harness.api, [ship, ...agents]));
  const moved = writesTo(harness, params).pop().data;
  assert.deepEqual(Array.from(moved.slice(0, 16)), [0, 0, -1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 5, 0.5, -2, 1]);
  // A moving parent uploads one uniform, not a stream.
  assert.equal(writesTo(harness, bufferWithLabel(harness, "agents")[0]).length, 1);
});

test("a motion batch uploads only the dirty runs of a partial frame", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 40, { motion: true });
  harness.renderer.prepareCrowdAtlas(atlas);
  const stats = await draw(harness, bundleOf(harness.api, agents));
  assert.equal(stats.crowdBatches, 1);
  const buffer = bufferWithLabel(harness, "agents")[0];
  const full = writesTo(harness, buffer);
  assert.equal(full.length, 1);
  assert.equal(full[0].data.length, 40 * RECORD_FLOATS, "the first frame writes every record");
  assert.ok(harness.fake.state.renderPipelines.some((p) => p.desc.label === "gosx-crowd-color-motion"));
  // A partial frame: agents 10..12 and 30 changed.
  for (const i of [10, 11, 12, 30]) {
    agents[i]._crowdMotion.record[10] += 1;
    agents[i]._crowdMotion.dirty = true;
  }
  await draw(harness, bundleOf(harness.api, agents));
  const writes = writesTo(harness, buffer).slice(1);
  assert.deepEqual(Array.from(writes, (w) => [w.offset / (RECORD_FLOATS * 4), w.data.length / RECORD_FLOATS]), [[10, 3], [30, 1]]);
  for (const agent of agents) assert.equal(agent._crowdMotion.dirty, false, "the renderer clears each flag it uploads");
  assert.equal(Array.from(writes[0].data)[10], agents[10]._crowdMotion.record[10]);
  const motionParams = bufferWithLabel(harness, "params")[0];
  const clip = writesTo(harness, motionParams).pop().data;
  assert.deepEqual(Array.from(clip.slice(28, 36)), [0, 0, 0, 0, 1, 6, 1, 0], "the clip table rides in the batch uniform");
  const time = clip.slice(16, 20);
  assert.equal(time[0], 0.5, "the scene clock");
  assert.equal(time[1], 0.25, "the extrapolation limit");
});

test("a shadow pass draws the batch with the crowd shadow pipeline", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 12, { shadows: true });
  harness.renderer.prepareCrowdAtlas(atlas);
  const light = { kind: "directional", directionX: 0.3, directionY: -1, directionZ: -0.4, intensity: 1, castShadow: true, color: "#ffffff", shadowSize: 256 };
  const stats = await draw(harness, bundleOf(harness.api, agents, { lights: [light] }));
  const shadowPass = harness.fake.state.renderPasses.find((pass) => pass.draws.some((d) => d.pipeline && d.pipeline.desc.label === "gosx-crowd-shadow-pose"));
  assert.ok(shadowPass, "a shadow pass drew the crowd");
  assert.equal(shadowPass.descriptor.colorAttachments.length, 0);
  const draws = shadowPass.draws.filter((d) => d.pipeline.desc.label === "gosx-crowd-shadow-pose");
  assert.equal(draws.length, 1);
  assert.equal(draws[0].instanceCount, 12);
  assert.equal(stats.crowdShadowDraws, 1);
  const layouts = draws[0].pipeline.desc.layout.desc.bindGroupLayouts;
  assert.equal(layouts.length, 2, "shadow uniform and crowd group");
  assert.match(draws[0].pipeline.desc.vertex.module.code, /@group\(1\) @binding\(2\) var<storage, read> crowdAgents/);
  const buffers = draws[0].pipeline.desc.vertex.buffers;
  assert.deepEqual(Array.from(buffers, (b) => b.attributes[0].shaderLocation), [0, 4, 5, 6]);
});

test("a crowd is never recorded into a render bundle", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 8);
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(harness.api, agents));
  await draw(harness, bundleOf(harness.api, agents));
  assert.equal(harness.mount.__gosxScene3DWebGPUStats.bundleState, "direct");
  assert.equal(harness.fake.state.renderBundleEncoders.length, 0);
});

test("a tinted batch reads one colour per agent from a storage buffer and uploads it only when a colour changes", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 6);
  harness.renderer.prepareCrowdAtlas(atlas);
  const red = new F32([1, 0, 0, 1]), blue = new F32([0, 0, 1, 1]);
  agents.forEach((agent, i) => { agent._crowdSkin.tint = i === 3 ? null : (i % 2 ? blue : red); });
  const expected = [];
  agents.forEach((agent, i) => expected.push(...(i === 3 ? [1, 1, 1, 1] : (i % 2 ? blue : red))));

  await draw(harness, bundleOf(harness.api, agents));
  const tints = bufferWithLabel(harness, "tints")[0];
  const params = bufferWithLabel(harness, "params")[0];
  assert.ok(tints, "a tint buffer exists");
  assert.deepEqual(Array.from(writesTo(harness, tints)[0].data), expected, "the agents' colours upload as the planner packed them");
  assert.equal(writesTo(harness, params).pop().data[18], 1, "timing.z tells the colour shader to read them");
  const group = harness.fake.state.renderPasses.flatMap((pass) => pass.bindGroups || []).filter((g) => g.group && g.group.desc.label === "gosx-crowd").pop();
  assert.deepEqual(Array.from(group.group.desc.entries, (e) => e.binding), [0, 1, 2, 3, 4]);
  assert.equal(group.group.desc.entries[4].resource.buffer, tints, "binding 4 is the tint buffer");

  await draw(harness, bundleOf(harness.api, agents));
  assert.equal(writesTo(harness, tints).length, 1, "an unchanged crowd uploads no colours");
  agents[1]._crowdSkin.tint = red;
  expected.splice(4, 4, 1, 0, 0, 1);
  await draw(harness, bundleOf(harness.api, agents));
  assert.equal(writesTo(harness, tints).length, 2, "a recolour uploads the colours once");
  assert.deepEqual(Array.from(writesTo(harness, tints)[1].data), expected);
});

test("a batch without tints leaves the colour switch off", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 5);
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(harness.api, agents));
  assert.equal(writesTo(harness, bufferWithLabel(harness, "params")[0]).pop().data[18], 0);
  assert.equal(writesTo(harness, bufferWithLabel(harness, "tints")[0]).length, 0, "no colours uploaded");
});

test("a crowd agent is not a GPU pick target", async () => {
  const api = (await setup()).api;
  const { agents } = crowd(api, 3);
  const bundle = bundleOf(api, agents);
  const plan = api.sceneWebGPUPickIDPlan ? api.sceneWebGPUPickIDPlan(bundle) : null;
  if (plan) assert.equal(plan.entries.filter((entry) => entry.group === "mesh").length, 0);
  assert.ok(bundle.meshObjects.some((record) => record._crowdBatch));
});

test("an agent that left its batch still skins, as a batch of one", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 6);
  agents[4].color = "#5fb0d9";
  harness.renderer.prepareCrowdAtlas(atlas);
  const bundle = bundleOf(harness.api, agents);
  const records = bundle.meshObjects;
  assert.equal(records.filter((record) => record._crowdBatch).length, 1);
  assert.ok(records.some((record) => !record._crowdBatch && record._crowdSkin), "the odd agent keeps its own record");
  const stats = await draw(harness, bundle);
  assert.equal(stats.crowdBatches, 2, "the batch and the odd agent");
  assert.equal(stats.crowdAgents, 6);
  const passes = mainRenderPasses(harness.fake);
  const draws = passes[passes.length - 1].draws.filter((d) => d.pipeline && d.pipeline.desc.label === "gosx-crowd-color-pose");
  assert.deepEqual(Array.from(draws, (d) => d.instanceCount).sort(), [1, 5]);
});

test("gpuDriven culls the camera view on the GPU and draws the survivors indirectly", async () => {
  const harness = await setup({ device: { timestampQuery: false } });
  const { agents, atlas } = crowd(harness.api, 200);
  harness.renderer.prepareCrowdAtlas(atlas);
  const bundle = () => bundleOf(harness.api, agents, { gpuDriven: { occlusion: false, shadowCulling: true } });
  const stats = await drawCulled(harness, bundle);
  const cull = harness.fake.state.computePipelines.find((p) => p.label === "gosx-crowd-cull");
  assert.ok(cull, "the cull kernel is a compute pipeline");
  assert.equal(stats.crowdCulling, true);
  assert.equal(stats.crowdOcclusion, false);
  const compute = harness.fake.state.computePasses.filter((pass) => pass.dispatches.some((d) => d.pipeline === cull));
  assert.equal(compute.length, 1, "one camera dispatch");
  assert.equal(compute[0].dispatches[0].workgroupCountX, Math.ceil(200 / 64));
  const passes = mainRenderPasses(harness.fake);
  const main = passes[passes.length - 1];
  assert.equal(main.draws.filter((d) => d.pipeline && /crowd/.test(d.pipeline.desc.label)).length, 0, "no direct draw");
  const indirect = main.drawIndirects.filter((d) => d.pipeline && d.pipeline.desc.label === "gosx-crowd-color-pose");
  assert.equal(indirect.length, 1);
  assert.equal(indirect[0].offset, 0, "early camera list is slot 0");
  const args = harness.fake.state.buffers.find((b) => b.label === "gosx.crowd.args");
  assert.equal(indirect[0].buffer, args);
  // Args are reset every frame: vertex count per slot, zero instances.
  const reset = writesTo(harness, args)[0].u32;
  assert.deepEqual(Array.from([reset[0], reset[1], reset[8], reset[9], reset[16], reset[24]]), [6, 0, 6, 0, 6, 6]);
  // The survivor list is the instance-rate vertex buffer.
  const visible = harness.fake.state.buffers.find((b) => b.label === "gosx.crowd.visible");
  const list = main.vertexBuffers.find((v) => v.slot === 4);
  assert.equal(list.buffer, visible);
  assert.equal(list.size, 200 * 4);
  assert.ok(visible.usage & 0x20 && visible.usage & 0x80, "storage and vertex");
  // The renderer submitted 200 agents and drew none of them on the CPU.
  assert.equal(stats.crowdAgents, 200);
});

test("gpuDriven with occlusion splits the main pass and builds a Hi-Z pyramid from its depth", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 100);
  harness.renderer.prepareCrowdAtlas(atlas);
  const bundle = () => bundleOf(harness.api, agents, { gpuDriven: { occlusion: true } });
  const stats = await drawCulled(harness, bundle);
  assert.equal(stats.crowdOcclusion, true);
  const names = harness.fake.state.computePipelines.map((p) => p.label);
  for (const want of ["gosx-crowd-cull", "gosx-crowd-downsample", "gosx-crowd-seed"]) assert.ok(names.includes(want), want);
  const passes = mainRenderPasses(harness.fake);
  const frame = passes.slice(-2);
  assert.equal(frame[0].descriptor.depthStencilAttachment.depthLoadOp, "clear", "early pass");
  assert.equal(frame[1].descriptor.label, "gosx-crowd-late", "late pass");
  assert.equal(frame[1].descriptor.colorAttachments[0].loadOp, "load");
  assert.equal(frame[1].descriptor.depthStencilAttachment.depthLoadOp, "load");
  assert.equal(frame[0].ended, true);
  const early = frame[0].drawIndirects.filter((d) => d.pipeline.desc.label === "gosx-crowd-color-pose");
  const late = frame[1].drawIndirects.filter((d) => d.pipeline.desc.label === "gosx-crowd-color-pose");
  assert.deepEqual([early.length, late.length], [1, 1]);
  assert.deepEqual([early[0].offset, late[0].offset], [0, 32], "early list, then late list");
  // Cull dispatches: early camera, then late camera after the pyramid.
  const hiz = harness.fake.state.computePasses.filter((pass) => pass.dispatches.some((d) => d.pipeline.label === "gosx-crowd-seed"));
  assert.equal(hiz.length, 1);
  assert.ok(hiz[0].dispatches.length > 1, "seed, then one downsample per level");
});

test("culling, cull history and Hi-Z restart when the layout changes", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 100);
  harness.renderer.prepareCrowdAtlas(atlas);
  const bundle = () => bundleOf(harness.api, agents, { gpuDriven: { occlusion: true } });
  await drawCulled(harness, bundle);
  const visibility = harness.fake.state.buffers.find((b) => b.label === "gosx.crowd.visibility");
  const resets = () => writesTo(harness, visibility).length;
  const before = resets();
  await draw(harness, bundle());
  assert.equal(resets(), before, "history persists across steady frames");
  agents.splice(10, 5);
  await draw(harness, bundle());
  assert.ok(resets() > before, "a membership change restarts history at all-visible");
  const last = writesTo(harness, visibility).pop();
  assert.ok(last.u32.every((v) => v === 1));
  assert.equal(last.u32.length, 95);
});

test("the batch buffers are released when the batch leaves and when the renderer is disposed", async () => {
  const harness = await setup();
  const { agents, atlas } = crowd(harness.api, 10);
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(harness.api, agents));
  const live = () => harness.fake.state.buffers.filter((b) => /^gosx\.crowd\./.test(b.label || "") && !b.destroyed).length;
  assert.ok(live() >= 5);
  const box = harness.api.normalizeSceneObject({ id: "box", kind: "box", width: 1, height: 1, depth: 1, color: "#888888" }, 0);
  await draw(harness, bundleOf(harness.api, [box]));
  assert.equal(harness.fake.state.buffers.filter((b) => /^gosx\.crowd\.(agents|poses|tints|params|visible|visibility|identity)$/.test(b.label || "") && !b.destroyed).length, 0);
  await draw(harness, bundleOf(harness.api, agents));
  harness.renderer.dispose();
  assert.equal(live(), 0);
  assert.ok(harness.fake.state.textures.filter((t) => t.desc.format === "rgba32float").every((t) => t.destroyed));
});

// The bundle build strips the indentation of the renderer's WGSL and renames its
// locals. The shipped chunks must still build the crowd pipelines and the
// GPU-driven instancing pipelines from that text.
test("the shipped chunks build crowd and GPU-driven instancing pipelines", async () => {
  const harness = await createBoardWebGPUHarness({ fresh: false, fakeDeviceOptions: { renderBundles: true } });
  harness.canvas.width = 64;
  harness.canvas.height = 64;
  F32 = vm.runInContext("Float32Array", harness.env.context);
  U32 = vm.runInContext("Uint32Array", harness.env.context);
  const api = harness.env.context.__gosx_scene3d_api;
  const { agents, atlas } = crowd(api, 10);
  harness.renderer.prepareCrowdAtlas(atlas);
  const result = await renderPreparedFrame(harness.renderer, bundleOf(api, agents, { gpuDriven: { occlusion: false } }), { width: 64, height: 64 }, { nowMS: 0, active: true });
  assert.equal(result, "submitted");
  assert.ok(harness.fake.state.renderPipelines.some((p) => p.desc.label === "gosx-crowd-color-pose"));
  const state = api.createSceneState({ scene: { instancedMeshes: [{ id: "grass", count: 100, kind: "box", width: 0.2, height: 0.2, depth: 0.2,
    transforms: Array.from({ length: 100 }, (_v, i) => [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, i % 10, 0, -Math.floor(i / 10), 1]).flat(), color: "#44aa44" }] } }, { tier: "full" });
  for (let frame = 0; frame < 4; frame++) {
    const meshes = api.sceneStateInstancedMeshesWithMaterials(state);
    const bundle = api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 2, z: 12, fov: 60, near: 0.05, far: 200 }, [], [], [], [], [], {}, 0, [], meshes, [], [], [], 0, false);
    bundle.gpuDriven = { occlusion: false };
    assert.notEqual(harness.renderer.render(bundle, { width: 64, height: 64 }, { nowMS: frame * 16, active: true }), "failed");
    await flushAsyncWork();
  }
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-webgpu-gpu-driven-reason"), "frustum");
});

test("a scene that prefers WebGPU hydrates its InstancedGLB crowds as batches", () => {
  // Hydration takes the crowd path only for a scene that asked for it. The WebGPU
  // renderer now draws crowd batches, so a scene that prefers WebGPU asks too.
  const mount = runtimeSource("mount.ts");
  assert.match(mount, /sceneState\._crowdWebGLRequested = sceneForcesWebGL\(props\) \|\| sceneRequiresWebGL\(props\) \|\| scenePrefersWebGPU\(props\);/);
});
