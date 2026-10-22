"use strict";

// The vessel cutaway on the WebGPU renderer, against the recording fake device.
// The fake runs no shader, so these tests pin what the renderer hands the GPU:
// the planes in the object uniforms, the WGSL that discards on the cut side, and
// the crowd params that cull agents. They mirror the WebGL u_cut contract in
// scene3d-vessel-cutaway.test.js. Pixels are checked in a real browser.

const test = require("node:test");
const vm = require("node:vm");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { createBoardWebGPUHarness, flushAsyncWork, renderPreparedFrame } = require("./runtime-test-harness.js");

let F32 = Float32Array;

// Distinct values so a copy of the planes is found by content: decks-above plane,
// hull-section plane, fade .25.
const CUT = [0.25, 0.75, 0.5, -3.5, -0.5, 0, 0.5, 1.75, 0.25, 0, 0, 0];

function meshVertices() {
  const Float32Array = F32;
  const positions = [-0.2, 0, 0.2, 0.2, 0, 0.2, 0, 0.6, 0, 0.2, 0, 0.2, 0.2, 0, -0.2, 0, 0.6, 0];
  const count = positions.length / 3;
  return {
    count, immutable: true, revision: 0, positions: new Float32Array(positions),
    normals: new Float32Array(count * 3).fill(0.5), uvs: new Float32Array(count * 2),
    tangents: new Float32Array(count * 4).map((_v, i) => (i % 4 === 0 || i % 4 === 3 ? 1 : 0)),
  };
}

function meshObject(api, id, index, extra = {}) {
  const object = api.normalizeSceneObject({ id, kind: "mesh", vertices: meshVertices(), materialKind: "standard", color: "#d98c5f", roughness: 0.6 }, index);
  Object.assign(object, { x: 0, y: 0, z: 0, rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
    static: false, skin: null, pickable: false, castShadow: false, receiveShadow: false, depthWrite: true }, extra);
  object.vertices.immutable = true;
  object.vertices.revision = 0;
  return object;
}

function bundleOf(api, objects, retained) {
  const bundle = api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 1.2, z: 3, fov: 62, near: 0.05, far: 100 }, objects, [], [], [], [], {}, 0.4, [], [], [], [], [], 0, false,
    { retainedGeometry: retained, worldBakedAttributeViews: false, rigidImportedBatches: false, crowdBatches: true });
  bundle.lights = [];
  return bundle;
}

async function setup() {
  const harness = await createBoardWebGPUHarness({ fresh: true, fakeDeviceOptions: { renderBundles: true } });
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
    harness.writes.push({ buffer, offset, data: new Float32Array(bytes.buffer) });
  };
  const createBuffer = harness.fake.device.createBuffer;
  harness.fake.device.createBuffer = function(desc) {
    const buffer = createBuffer.call(this, desc);
    buffer.label = desc && desc.label;
    return buffer;
  };
  F32 = vm.runInContext("Float32Array", harness.env.context);
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
}

// Index of the first float of `values` found in `data`, or -1.
function find(data, values) {
  for (let i = 0; i + values.length <= data.length; i++) {
    if (values.every((v, k) => data[i + k] === v)) return i;
  }
  return -1;
}

function writesWithCut(harness) {
  return harness.writes.filter((call) => find(call.data, CUT) >= 0);
}

function fragmentCode(harness) {
  const module = harness.fake.state.shaderModules.find((m) => m.label === "pbr-frag");
  assert.ok(module, "the PBR fragment module exists");
  return module.code;
}

for (const retained of [true, false]) {
  test(`the planes of a cut object reach its model slot (retained geometry ${retained})`, async () => {
    const harness = await setup();
    const cut = new F32(CUT);
    const hull = meshObject(harness.api, "ship/hull", 0, { cutaway: cut });
    const plain = meshObject(harness.api, "plain", 1);
    const bundle = bundleOf(harness.api, [hull, plain], retained);
    assert.equal(bundle.meshObjects.find((r) => r.id === "ship/hull").cut, cut, "the planner carries the planes on the record");
    await draw(harness, bundle);
    const writes = writesWithCut(harness);
    assert.ok(writes.length > 0, "a buffer write carries all twelve floats");
    const call = writes.pop();
    const at = find(call.data, CUT);
    // The planes sit straight after the 16-float model matrix, in the same slot.
    assert.deepEqual(Array.from(call.data.slice(at - 16, at - 12)), [1, 0, 0, 0], "model matrix column 0 precedes the planes");
    assert.equal(call.data[at + 8], 0.25, "fade is the first float of the third vec4");
  });
}

test("an object with no cutaway writes no planes, and a lifted cutaway clears them", async () => {
  const harness = await setup();
  const hull = meshObject(harness.api, "ship/hull", 0, { cutaway: new F32(CUT) });
  await draw(harness, bundleOf(harness.api, [hull], true));
  assert.ok(writesWithCut(harness).length > 0);
  harness.writes.length = 0;
  hull.cutaway = null;
  await draw(harness, bundleOf(harness.api, [hull], true));
  assert.equal(writesWithCut(harness).length, 0, "no plane data after the cutaway is off");
});

test("the model slot is bound to both stages and holds the planes", async () => {
  const harness = await setup();
  await draw(harness, bundleOf(harness.api, [meshObject(harness.api, "ship/hull", 0, { cutaway: new F32(CUT) })], true));
  const layouts = harness.fake.state.renderPipelines.map((p) => p.desc.layout && p.desc.layout.desc && p.desc.layout.desc.bindGroupLayouts).filter(Boolean);
  const entries = layouts.flatMap((l) => l.flatMap((g) => (g.desc && g.desc.entries) || [])).filter((e) => e.binding === 19);
  assert.ok(entries.length > 0, "a pipeline layout carries the model slot");
  for (const entry of entries) {
    assert.ok(entry.visibility & 2, "visible to the fragment stage");
    assert.ok(entry.buffer.minBindingSize >= 112, "covers the matrix and three vec4");
  }
});

test("the fragment shader discards on the cut side and dithers the decks above", () => {
  return setup().then(async (harness) => {
    await draw(harness, bundleOf(harness.api, [meshObject(harness.api, "ship/hull", 0, { cutaway: new F32(CUT) })], true));
    const code = fragmentCode(harness);
    assert.match(code, /@group\(1\) @binding\(19\) var<uniform> \w+: \w+;/, "the fragment reads the object block");
    assert.match(code, /dot\(\w+\.cut\[1\]\.xyz, in\.worldPos\) \+ \w+\.cut\[1\]\.w > 0\.0\) \{ discard; \}/, "hull section plane discards");
    assert.match(code, /dot\(\w+\.cut\[0\]\.xyz, in\.worldPos\) \+ \w+\.cut\[0\]\.w > 0\.0 && [^\n]*>= \w+\.cut\[2\]\.x\) \{ discard; \}/, "decks-above plane discards unless the dither keeps the pixel");
    assert.equal(code.split("{").length, code.split("}").length, "braces balance");
    // The cut runs before any texture is sampled, as in the WebGL shader.
    assert.ok(code.indexOf("cut[1]") < code.indexOf("textureSample(albedoTex"));
  });
});

test("fade keeps the stated share of pixels and fade 0 hides the decks above", async () => {
  const harness = await setup();
  await draw(harness, bundleOf(harness.api, [meshObject(harness.api, "ship/hull", 0, { cutaway: new F32(CUT) })], true));
  const code = fragmentCode(harness);
  const m = /fract\(([0-9.]+) \* fract\(dot\(in\.clipPos\.xy, vec2f\(([0-9.]+), ([0-9.]+)\)\)\)\)/.exec(code);
  assert.ok(m, "the dither is the WebGL interleaved gradient noise");
  const [k, a, b] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const fract = (x) => x - Math.floor(x);
  const share = (fade) => {
    let kept = 0;
    const n = 256;
    for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (fract(k * fract(x * a + y * b)) < fade) kept++;
    return kept / (n * n);
  };
  assert.ok(Math.abs(share(0.25) - 0.25) < 0.02, `fade .25 keeps ${share(0.25)}`);
  assert.equal(share(0), 0, "fade 0 keeps nothing");
});

test("a crowd on a cut vessel gets the planes in its params and culls on them", async () => {
  const harness = await setup();
  const api = harness.api;
  const ship = meshObject(api, "ship/hull", 99, { visible: false, parentMatrix: new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]) });
  const crowdCut = new F32([0.25, 0.75, 0.5, -3.5, -0.5, 0, 0.5, 1.75]);
  ship.cutawayCrowd = crowdCut;
  const Float32Array = F32;
  const atlas = { id: "test-atlas", width: 4, height: 8, data: new Float32Array(4 * 8 * 4), clips: new Map([["Run", { start: 1, segments: 6, duration: 1 }]]), missingClips: new Set() };
  const bounds = { minX: -0.3, minY: 0, minZ: -0.3, maxX: 0.3, maxY: 1.2, maxZ: 0.3 };
  const agents = [];
  for (let i = 0; i < 3; i++) {
    const agent = meshObject(api, "p/a" + i + "/0", i, { parentMatrix: new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, i, 0, -3, 1]) });
    agent._crowdSkin = { atlas, bounds, rows: new F32([0, 1, 0.5]), poseRows: null, batchID: "p", rev: 0, group: null, parent: "ship" };
    agents.push(agent);
  }
  harness.renderer.prepareCrowdAtlas(atlas);
  await draw(harness, bundleOf(api, [ship, ...agents], true));
  const params = harness.fake.state.buffers.filter((b) => b.label === "gosx.crowd.params")[0];
  assert.ok(params, "the crowd params buffer exists");
  const write = harness.writes.filter((call) => call.buffer === params).pop();
  const at = find(write.data, Array.from(crowdCut));
  assert.ok(at >= 0, "the eight plane floats are in the params upload");
  assert.equal(at * 4, 624, "after the clip table, where the WGSL struct puts them");
  const color = harness.fake.state.renderPipelines.find((p) => /^gosx-crowd-color/.test(p.desc.label));
  assert.ok(color, "crowd color pipeline");
  const code = color.desc.vertex.module.code;
  assert.match(code, /cut: array<vec4f, 2>/);
  assert.match(code, /dot\(crowdParams\.cut\[0\]\.xyz, \w+\) \+ crowdParams\.cut\[0\]\.w > 0\.0 \|\| dot\(crowdParams\.cut\[1\]\.xyz, \w+\) \+ crowdParams\.cut\[1\]\.w > 0\.0/);
  assert.match(code, /clipPos = vec4f\(2\.0, 2\.0, 2\.0, 1\.0\)/, "a culled agent leaves clip space");
  const shadow = harness.fake.state.shaderModules.find((m) => /crowd-shadow/.test(m.label || "") || false);
  if (shadow) assert.match(shadow.code, /crowdCulled/);
});

test("the cutaway module is a registered renderer source and the backend list names webgpu", () => {
  const architecture = JSON.parse(fs.readFileSync(path.join(__dirname, "testdata", "scene3d-renderer-architecture.json"), "utf8"));
  assert.ok(architecture.sourceSets.webgpu.sources.includes("../runtime/scene3d/webgpu-cutaway.ts"));
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "vessel-cutaway.ts"), "utf8");
  assert.match(source, /BACKENDS = \['webgl', 'webgpu'\]/);
});
