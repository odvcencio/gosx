"use strict";
// WebGL2 TAA motion vectors: moving retained meshes and
// skinned meshes draw a velocity pass the resolve reprojects with; water keeps
// less history.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { createWebGLRendererForPost, makeWebGLBundleWithCustomPost } = require("./runtime-test-harness.js");

// Typed arrays must come from the runtime's realm: the renderer checks
// "instanceof Float32Array" before it binds a cached direct stream.
let F32 = Float32Array;
function identity() { return new F32([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]); }

function mover(extra = {}) {
  return Object.assign({
    id: "mover", kind: "mesh", materialIndex: 0, renderPass: "opaque", static: false, castShadow: false, receiveShadow: false,
    bounds: { minX: -1, minY: -1, minZ: -1, maxX: 1, maxY: 1, maxZ: 1 }, depthNear: 0, depthFar: 0, depthCenter: 0, viewCulled: false,
    vertices: { positions: new F32([0, 1, 0, -1, -1, 0, 1, -1, 0]), normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1]), uvs: new F32([0.5, 1, 0, 0, 1, 0]), count: 3, immutable: true },
    directVertices: true, retainedGeometry: true, geometryRevision: 1, resourceOwner: {}, modelMatrix: identity(), vertexOffset: 0, vertexCount: 3,
  }, extra);
}

function motionHarness(build, environment = {}) {
  const h = createWebGLRendererForPost({ fresh: true });
  F32 = vm.runInContext("Float32Array", h.env.context);
  const objects = build();
  const gl = h.canvas.getContext("webgl2");
  gl.TEXTURE1 = gl.TEXTURE0 + 1; gl.FRAMEBUFFER_COMPLETE = 0x8cd5; gl.MAX_VERTEX_UNIFORM_VECTORS = 0x8dfb;
  gl.checkFramebufferStatus = () => gl.FRAMEBUFFER_COMPLETE; gl.deleteRenderbuffer = () => {};
  const ext = gl.getExtension.bind(gl);
  gl.getExtension = (name) => name === "EXT_color_buffer_float" ? {} : ext(name);
  const getParameter = gl.getParameter.bind(gl);
  gl.getParameter = (p) => p === gl.MAX_VERTEX_UNIFORM_VECTORS ? 1024 : getParameter(p);
  const uniforms = [];
  const uniform4f = gl.uniform4f.bind(gl), uniform4fv = gl.uniform4fv ? gl.uniform4fv.bind(gl) : null, uniformMatrix = gl.uniformMatrix4fv.bind(gl);
  gl.uniform4f = (loc, ...v) => { uniforms.push([loc && loc.name, v]); uniform4f(loc, ...v); };
  gl.uniform4fv = (loc, v) => { uniforms.push([loc && loc.name, Array.from(v)]); if (uniform4fv) uniform4fv(loc, v); };
  gl.uniformMatrix4fv = (loc, t, v) => { uniforms.push([loc && loc.name, Array.from(v).slice(0, 32)]); uniformMatrix(loc, t, v); };
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = [{ kind: "toneMapping" }, { kind: "taa" }];
  bundle.meshObjects = objects;
  bundle.environment = environment;
  const last = (name) => uniforms.filter((u) => u[0] === name).at(-1);
  return { ...h, gl, uniforms, bundle, last, frame: () => { uniforms.length = 0; h.renderer.render(bundle, { width: h.canvas.width, height: h.canvas.height }); } };
}

test("a moving retained mesh draws motion vectors that the resolve reprojects with", () => {
  let obj;
  const h = motionHarness(() => [obj = mover()]);
  try {
    h.frame(); h.frame();
    assert.equal(h.last("u_motion")[1][0], 0, "a still mesh needs no velocity pass");
    obj.modelMatrix = identity(); obj.modelMatrix[12] = 0.5;
    h.frame();
    assert.equal(h.last("u_temporalParams")[1][3], 1, "history is valid");
    assert.equal(h.last("u_motion")[1][0], 1, "the resolve reads the velocity target");
    assert.ok(h.gl.ops.some((op) => op[0] === "texStorage2D" && op[3] === h.gl.RGBA16F), "an RGBA16F velocity target");
    const current = h.last("u_current")[1], previous = h.last("u_previous")[1];
    assert.notDeepEqual(current.slice(12, 16), previous.slice(12, 16), "current and previous transforms differ by the move");
    h.frame();
    assert.equal(h.last("u_motion")[1][0], 0, "the mover stopped, so the pass is skipped again");
  } finally { h.renderer.dispose(); }
});

test("a skinned mesh draws with this frame's and last frame's joint palettes", () => {
  let joints, skin;
  const h = motionHarness(() => {
    joints = new F32(32); joints.set(identity(), 0); joints.set(identity(), 16);
    skin = { jointMatrices: joints };
    const obj = mover({ id: "skinned", retainedGeometry: false, skin });
    obj.vertices.joints = new F32(12); obj.vertices.weights = new F32([1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0]);
    return [obj];
  });
  try {
    h.frame(); h.frame();
    const posed = new F32(joints); posed[12] = 0.25; skin.jointMatrices = posed;
    h.frame();
    assert.equal(h.last("u_motion")[1][0], 1);
    assert.equal(h.last("u_joints[0]")[1][12], 0.25, "this frame's palette");
    assert.equal(h.last("u_previousJoints[0]")[1][12], 0, "last frame's palette");
  } finally { h.renderer.dispose(); }
});

test("the resolve masks water with the ocean band and pool boxes", () => {
  const h = motionHarness(() => [mover()], { ocean: { level: 1, waveHeight: 2 } });
  try {
    h.frame(); h.frame();
    assert.deepEqual(h.last("u_ocean")[1], [1, 1.5, 1, 0]);
  } finally { h.renderer.dispose(); }
});

test("the resolve shader uses a mover's offset only on its own surface and skips depth rejection there", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl-taa-motion.ts"), "utf8");
  assert.match(source, /velocity\.a > 0\.5 && abs\(velocity\.b \+ viewPosition\.z\) <= 0\.02 \* abs\(viewPosition\.z\) \+ 0\.02\) \{ uv = v_uv \+ velocity\.xy; mover = true; \}/);
  assert.match(source, /if \(!mover\) \{\s*float oldDepth/);
  assert.match(source, /float reactive = waterAt\(world\.xyz\) \? 0\.75 : 0\.0;/);
  assert.match(source, /fragColor = vec4\(\(v_previous\.xy \/ v_previous\.w - v_current\.xy \/ v_current\.w\) \* 0\.5, v_current\.w, 1\.0\)/);
});
