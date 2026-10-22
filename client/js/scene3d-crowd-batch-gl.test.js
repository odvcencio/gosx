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
    object._crowdSkin = { atlas, bounds, rows: new F32([1, 2, .5]), poseRows: null, batchID: "swarm", rev: 0, group: null };
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
  return { ...h, api, F32, objects, build, gl, atlas };
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

  // Frame three: one agent's pose row changes. The batch uploads its streams again.
  h.objects[5]._crowdSkin.rows[2] = .9;
  h.objects[5]._crowdSkin.rev += 1;
  const mark = h.gl.ops.length;
  const posed = h.build();
  assert.equal(posed.meshObjects[0].crowdRevision, revision + 1);
  h.renderer.render(posed, viewport);
  assert.ok(streamUploads(h.gl, mark) >= 2, "transform and pose streams upload after a revision change");
  assert.equal(drawn(h.gl, mark).length, 1);
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
