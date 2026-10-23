"use strict";

// The real WebGL renderer over the fake GL context, fed crowd batch records
// from the real bundle builder: one instanced draw per batch, and no stream
// upload on a frame where no agent changed.

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { createWebGLRendererForPost } = require("./runtime-test-harness.js");

const viewport = { cssWidth: 320, cssHeight: 180, width: 320, height: 180 };

function fixture(count, options = {}) {
  const h = createWebGLRendererForPost({ fresh: false });
  const api = h.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", h.env.context);
  const U32 = vm.runInContext("Uint32Array", h.env.context);
  const vertices = { count: 3, immutable: true, revision: 0,
    positions: new F32([-.1, 0, 0, .1, 0, 0, 0, .2, 0]),
    normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1]),
    uvs: new F32([0, 0, 1, 0, .5, 1]),
    tangents: new F32([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]),
    joints: new F32(12), weights: new F32([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]),
    indices: new U32([0, 1, 2]) };
  const atlas = { id: "atlas", width: 4, height: 32, data: new F32(4 * 32 * 4), clips: new Map(), missingClips: new Set() };
  const bounds = { minX: -.2, minY: 0, minZ: -.2, maxX: .2, maxY: .3, maxZ: .2 };
  const objects = Array.from({ length: count }, (_unused, index) => {
    const object = Object.assign({
      id: `swarm/a${index}/0`, kind: "mesh", vertices, pickable: false, visible: true, castShadow: false, receiveShadow: false,
      materialKind: "standard", color: "#669966", opacity: 1, roughness: .5, metalness: 0, wireframe: false,
      blendMode: "opaque", renderPass: "opaque", depthWrite: true, scaleX: 1, scaleY: 1, scaleZ: 1, x: 0, y: 0, z: 0,
      rotationX: 0, rotationY: 0, rotationZ: 0, spinX: 0, spinY: 0, spinZ: 0, shiftX: 0, shiftY: 0, shiftZ: 0,
      skin: null, static: false,
      parentMatrix: new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, index * .2 - 0.2 * count / 2, 0, 0, 1]),
    }, options.object);
    object._crowdSkin = { atlas, bounds, rows: new F32([1, 2, .5]), poseRows: null, batchID: "swarm", rev: 0, group: null,
      tint: options.tint ? options.tint(index) : null };
    return object;
  });
  const build = (camera = { x: 0, y: 0, z: 6, fov: 72, near: .05, far: 128 }, source = objects) =>
    api.createSceneRenderBundle(320, 180, "#000000", camera, source, [], [], [], [], {}, 0, [], [], [], [], [], 0, false,
      { retainedGeometry: true, rigidImportedBatches: true, crowdBatches: typeof h.renderer.prepareCrowdMotionShaders === "function" });
  const gl = h.canvas.getContext("webgl2");
  // The fake context lacks the capability queries the crowd palette makes.
  const limits = { MAX_TEXTURE_SIZE: [0x0D33, 4096], MAX_COMBINED_TEXTURE_IMAGE_UNITS: [0x8B4D, 32],
    MAX_VERTEX_TEXTURE_IMAGE_UNITS: [0x8B4C, 16], MAX_TEXTURE_IMAGE_UNITS: [0x8872, 16] };
  const answers = new Map();
  for (const [name, [code, value]] of Object.entries(limits)) { gl[name] = code; answers.set(code, value); }
  for (const [name, value] of Object.entries({ NO_ERROR: 0, RGBA32F: 0x8814, ACTIVE_TEXTURE: 0x84E0, UNPACK_FLIP_Y_WEBGL: 0x9240, TEXTURE_BINDING_2D: 0x8069 })) {
    if (gl[name] === undefined) gl[name] = value;
  }
  for (const name of ["pixelStorei", "texParameteri", "activeTexture", "deleteTexture"]) {
    if (typeof gl[name] !== "function") gl[name] = () => {};
  }
  if (typeof gl.getError !== "function") gl.getError = () => 0;
  const getParameter = gl.getParameter.bind(gl);
  gl.getParameter = param => (answers.has(param) ? answers.get(param) : getParameter(param));
  // The fake context keeps only the last upload per buffer id; keep every array-buffer upload's values.
  const uploads = [];
  for (const name of ["bufferData", "bufferSubData"]) {
    const original = gl[name].bind(gl);
    gl[name] = (...args) => {
      const data = name === "bufferData" ? args[1] : args[2];
      if (args[0] === gl.ARRAY_BUFFER && data && typeof data.length === "number") uploads.push(Array.from(data));
      return original(...args);
    };
  }
  return { ...h, api, F32, objects, build, gl, atlas, uploads };
}

function drawn(gl, from = 0) {
  return gl.ops.slice(from).filter(op => op[0] === "drawArraysInstanced" || op[0] === "drawElementsInstanced");
}

function streamUploads(gl, from = 0) {
  return gl.ops.slice(from).filter(op => op[0] === "bufferSubData" || op[0] === "bufferData").length;
}

test("a pose crowd draws as one instanced call and uploads its streams once", () => {
  const h = fixture(40);
  const first = h.build();
  assert.equal(first.meshObjects.length, 1);
  const revision = first.meshObjects[0].crowdRevision;
  h.renderer.render(first, viewport);
  const draws = drawn(h.gl);
  assert.equal(draws.length, 1, "one draw for the whole crowd");
  assert.equal(draws[0][draws[0].length - 1], 40, "forty instances");

  // Frame two: the camera moves, no agent changes. Nothing is repacked or uploaded.
  const before = h.gl.ops.length;
  const moved = h.build({ x: 1, y: .5, z: 6.5, fov: 72, near: .05, far: 128 });
  assert.equal(moved.meshObjects[0], first.meshObjects[0]);
  h.renderer.render(moved, viewport);
  assert.equal(drawn(h.gl, before).length, 1);
  assert.equal(streamUploads(h.gl, before), 0, "an unchanged crowd uploads no stream data");

  // Frame three: only the changed pose word uploads; transforms stay resident.
  h.objects[5]._crowdSkin.rows[2] = .9;
  h.objects[5]._crowdSkin.rev += 1;
  const mark = h.gl.ops.length;
  const posed = h.build();
  assert.equal(posed.meshObjects[0].crowdRevision, revision + 1);
  h.renderer.render(posed, viewport);
  assert.equal(streamUploads(h.gl, mark), 1, "only the changed pose stream uploads");
  const poseUploads = h.gl.ops.slice(mark).filter(op => op[0] === "bufferSubData");
  assert.deepEqual(poseUploads.map(op => op.slice(3)), [[(5 * 3 + 2) * 4, 1]], "one changed pose word");
  assert.equal(drawn(h.gl, mark).length, 1);

  // Frame four: moving that agent must update its transform without resending poses.
  h.objects[5].parentMatrix[12] += 1;
  h.objects[5]._crowdSkin.rev += 1;
  const transformMark = h.gl.ops.length;
  h.renderer.render(h.build(), viewport);
  assert.equal(streamUploads(h.gl, transformMark), 1, "only the changed transform stream uploads");
  const transformUploads = h.gl.ops.slice(transformMark).filter(op => op[0] === "bufferSubData");
  assert.deepEqual(transformUploads.map(op => op.slice(3)), [[(5 * 16 + 12) * 4, 1]], "one changed matrix word");
  assert.equal(drawn(h.gl, transformMark).length, 1);
  h.renderer.dispose();
});

test("a crowd whose batch leaves the view skips the color draw", () => {
  const h = fixture(10);
  const bundle = h.build({ x: 500, y: 0, z: 6, fov: 72, near: .05, far: 128 });
  assert.equal(bundle.meshObjects.length, 1);
  h.renderer.render(bundle, viewport);
  assert.equal(drawn(h.gl).length, 0);
  h.renderer.dispose();
});

test("a GPU-motion crowd draws as one call and uploads motion records only after a motion frame", () => {
  const h = fixture(30);
  const table = { count: 1, data: new h.F32(4) };
  for (const [index, object] of h.objects.entries()) {
    const atlas = object._crowdSkin.atlas;
    const x = object.parentMatrix[12];
    object._crowdMotion = { atlas, clipTable: table, record: new h.F32(24), dirty: true,
      bounds: { minX: x - .2, minY: 0, minZ: -.2, maxX: x + .2, maxY: .3, maxZ: .2 } };
    object._crowdMotion.record[0] = index;
  }
  h.renderer.prepareCrowdMotionShaders();
  const first = h.build();
  assert.equal(first.meshObjects.length, 1);
  assert.equal(first.meshObjects[0].crowdMotion, true);
  h.renderer.render(first, viewport);
  const draws = drawn(h.gl);
  assert.equal(draws.length, 1);
  assert.equal(draws[0][draws[0].length - 1], 30);
  assert.ok(h.objects.every(object => object._crowdMotion.dirty === false), "the renderer consumed the dirty flags");

  const before = h.gl.ops.length;
  h.renderer.render(h.build({ x: 2, y: 0, z: 6, fov: 72, near: .05, far: 128 }), viewport);
  assert.equal(drawn(h.gl, before).length, 1);
  assert.equal(streamUploads(h.gl, before), 0, "a camera move uploads nothing");

  // A motion frame touches three agents: only that range uploads.
  for (const object of h.objects.slice(10, 13)) { object._crowdMotion.record[0] += 5; object._crowdMotion.dirty = true; }
  const mark = h.gl.ops.length;
  h.renderer.render(h.build(), viewport);
  const uploads = h.gl.ops.slice(mark).filter(op => op[0] === "bufferSubData");
  assert.equal(uploads.length, 1, "one contiguous dirty range");
  assert.equal(uploads[0][3], 10 * 24 * 4, "the range starts at the first changed agent");
  assert.equal(uploads[0][4], 3 * 24, "and covers only the three changed agents");
  assert.equal(drawn(h.gl, mark).length, 1);
  h.renderer.dispose();
});

// Linear RGBA, as the hydration path stores a palette entry.
const tintOf = (h, r, g, b) => new h.F32([r, g, b, 1]);

function uploadedWith(h, values) {
  return h.uploads.some(data => data.length === values.length && values.every((v, i) => Math.abs(data[i] - v) < 1e-6));
}

function colorBinding(gl, from) {
  const ops = gl.ops.slice(from);
  return {
    pointer: ops.filter(op => op[0] === "vertexAttribPointer" && op[1] === 9),
    divisor: ops.filter(op => op[0] === "vertexAttribDivisor" && op[1] === 9).map(op => op[2]),
    flags: ops.filter(op => op[0] === "uniform1i" && op[1] === "u_hasInstanceColor").map(op => op[2]),
  };
}

test("a tinted pose crowd binds one colour per agent and uploads it only when a colour changes", () => {
  const h = fixture(6);
  const palette = [tintOf(h, 1, 0, 0), tintOf(h, 0, 1, 0), tintOf(h, 0, 0, 1)];
  h.objects.forEach((object, i) => { object._crowdSkin.tint = i === 4 ? null : palette[i % 3]; });
  const expected = [];
  h.objects.forEach((object, i) => expected.push(...(i === 4 ? [1, 1, 1, 1] : palette[i % 3])));

  const first = h.build();
  const [record] = first.meshObjects;
  const revision = record.crowdRevision;
  assert.deepEqual(Array.from(record.crowdColors), expected, "the record carries 4 floats per agent, white for an untinted one");
  h.renderer.render(first, viewport);
  const bound = colorBinding(h.gl, 0);
  assert.equal(bound.pointer.length, 1, "one instance-colour attribute binding for the batch");
  assert.equal(bound.pointer[0][2], 4, "four components");
  assert.deepEqual(bound.divisor.slice(0, 1), [1], "advanced once per instance");
  assert.ok(bound.flags.includes(1), "u_hasInstanceColor is on for the draw");
  assert.ok(uploadedWith(h, expected), "the colour stream reached the GPU");
  assert.equal(drawn(h.gl).length, 1, "tints add no draw call");

  // An unchanged crowd (camera moves) uploads nothing, colours included.
  let mark = h.gl.ops.length;
  h.renderer.render(h.build({ x: 1, y: .5, z: 6.5, fov: 72, near: .05, far: 128 }), viewport);
  assert.equal(streamUploads(h.gl, mark), 0);

  // A pose change repacks transforms and poses but not colours.
  h.objects[2]._crowdSkin.rows[2] = .9;
  h.objects[2]._crowdSkin.rev += 1;
  mark = h.gl.ops.length;
  const posed = h.build();
  assert.equal(posed.meshObjects[0].crowdColorGen, record.crowdColorGen, "poses do not touch colours");
  assert.equal(posed.meshObjects[0].crowdRevision, revision + 1);
  h.renderer.render(posed, viewport);
  const uploadsAfterPose = h.gl.ops.slice(mark).filter(op => op[0] === "bufferSubData" && op[4] === 4 * 6);
  assert.equal(uploadsAfterPose.length, 0, "no 24-float colour upload on a pose change");

  // Recolouring one agent in place repacks colours without a pose revision.
  const posedRevision = record.crowdRevision;
  h.objects[1]._crowdSkin.tint = tintOf(h, .5, .25, .125);
  expected.splice(4, 4, .5, .25, .125, 1);
  mark = h.gl.ops.length;
  const recolored = h.build();
  assert.equal(recolored.meshObjects[0], record);
  assert.equal(record.crowdRevision, posedRevision, "a recolour is not a pose revision");
  assert.deepEqual(Array.from(record.crowdColors), expected);
  h.renderer.render(recolored, viewport);
  // The stream retained from the last frame uploads only the changed span: the three
  // components of agent 1 that differ (the alpha stays 1), at float 4.
  const colorUploads = h.gl.ops.slice(mark).filter(op => op[0] === "bufferSubData" && op[1] === 34962);
  assert.deepEqual(colorUploads.map(op => op.slice(3)), [[4 * 4, 3]], "exactly one colour upload, of the changed span");
  assert.equal(drawn(h.gl, mark).length, 1);
  h.renderer.dispose();
});

test("a crowd without tints draws with the colour attribute off", () => {
  const h = fixture(5);
  const bundle = h.build();
  assert.equal(bundle.meshObjects[0].crowdColors, null);
  h.renderer.render(bundle, viewport);
  const bound = colorBinding(h.gl, 0);
  assert.equal(bound.pointer.length, 0, "no instance-colour stream");
  assert.ok(bound.flags.length > 0 && bound.flags.every(flag => flag === 0), "u_hasInstanceColor stays off");
  h.renderer.dispose();
});

test("a crowd drawn without tints after a tinted one turns the colour attribute off again", () => {
  const h = fixture(5);
  const green = tintOf(h, 0, 1, 0);
  h.objects.forEach(object => { object._crowdSkin.tint = green; });
  h.renderer.render(h.build(), viewport);
  assert.ok(colorBinding(h.gl, 0).flags.includes(1), "the first draw is tinted");
  h.objects.forEach(object => { object._crowdSkin.tint = null; });
  const mark = h.gl.ops.length;
  const plain = h.build();
  assert.equal(plain.meshObjects[0].crowdColors, null);
  h.renderer.render(plain, viewport);
  const after = colorBinding(h.gl, mark);
  assert.equal(after.pointer.length, 0, "no colour stream on the second draw");
  assert.equal(after.flags[after.flags.length - 1], 0, "u_hasInstanceColor is off for the second draw");
  h.renderer.dispose();
});

test("a tinted GPU-motion crowd binds its colours beside the motion records", () => {
  const h = fixture(8);
  const table = { count: 1, data: new h.F32(4) };
  const red = tintOf(h, 1, 0, 0), blue = tintOf(h, 0, 0, 1);
  for (const [index, object] of h.objects.entries()) {
    const x = object.parentMatrix[12];
    object._crowdMotion = { atlas: object._crowdSkin.atlas, clipTable: table, record: new h.F32(24), dirty: true,
      bounds: { minX: x - .2, minY: 0, minZ: -.2, maxX: x + .2, maxY: .3, maxZ: .2 } };
    object._crowdSkin.tint = index % 2 ? blue : red;
  }
  h.renderer.prepareCrowdMotionShaders();
  const bundle = h.build();
  assert.equal(bundle.meshObjects[0].crowdMotion, true);
  h.renderer.render(bundle, viewport);
  const bound = colorBinding(h.gl, 0);
  assert.equal(bound.pointer.length, 1);
  assert.ok(bound.flags.includes(1), "the motion program turns the colour attribute on");
  const expected = [];
  for (let i = 0; i < 8; i++) expected.push(...(i % 2 ? blue : red));
  assert.ok(uploadedWith(h, expected));
  assert.equal(drawn(h.gl).length, 1);
  h.renderer.dispose();
});

test("an agent that left its batch keeps its tint on the crowd shader", () => {
  const h = fixture(4);
  const green = tintOf(h, 0, 1, 0);
  h.objects.forEach(object => { object._crowdSkin.tint = green; });
  h.objects[2].color = "#ff0000";
  const bundle = h.build();
  assert.equal(bundle.meshObjects.length, 2, "the batch and the agent whose appearance differs");
  h.renderer.render(bundle, viewport);
  assert.equal(colorBinding(h.gl, 0).pointer.length, 2, "both draws bind a colour stream");
  h.renderer.dispose();
});
