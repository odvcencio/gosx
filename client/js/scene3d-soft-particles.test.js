"use strict";
// Soft particles: a blended builtin compute-particle sprite fades as it nears
// the opaque depth behind it (ParticleMaterial.SoftDistance).
const test = require("node:test");
const assert = require("node:assert/strict");
const {
  createBoardWebGPUHarness, makePointsBundle, createWebGLRendererForPost, makeWebGLBundleWithCustomPost, flushAsyncWork,
} = require("./runtime-test-harness.js");

function particles(material, extra = {}) {
  return Object.assign({ id: "spray", count: 12, emitter: { kind: "sphere", radius: 1, lifetime: 2 }, material }, extra);
}

async function renderWebGPU(computeParticles, frames = 4, fresh = true) {
  const h = await createBoardWebGPUHarness({ fresh });
  const bundle = makePointsBundle({ id: "anchor", count: 1, positions: [0, 0, 0] });
  bundle.computeParticles = computeParticles;
  for (let i = 0; i < frames; i++) {
    h.renderer.render(bundle, { width: 320, height: 180 }, { nowMS: 1000 + i * 16 });
    await flushAsyncWork();
  }
  return h;
}

function softPasses(h) {
  return h.fake.state.renderPasses.filter((pass) => pass.descriptor && pass.descriptor.label === "gosx-soft-particles");
}

test("WebGPU draws soft sprites in a read-only depth continuation pass", async () => {
  const h = await renderWebGPU([
    particles({ color: "#ffffff", size: 0.2, blendMode: "alpha", softDistance: 0.5 }),
    particles({ color: "#ff8800", size: 0.2, blendMode: "additive" }, { id: "embers" }),
  ]);
  try {
    const passes = softPasses(h);
    assert.ok(passes.length > 0, "a soft continuation pass opens");
    const pass = passes.at(-1);
    const depth = pass.descriptor.depthStencilAttachment;
    assert.equal(depth.depthReadOnly, true);
    assert.equal(depth.depthLoadOp, undefined, "read-only depth must not carry load/store ops");
    assert.equal(depth.depthStoreOp, undefined);
    assert.ok(pass.descriptor.colorAttachments.every((attachment) => attachment.loadOp === "load"));
    assert.equal(pass.draws.length, 1, "only the soft system draws in the soft pass");
    assert.equal(pass.draws[0].pipeline.desc.fragment.module.label, "points-soft-frag");
    assert.equal(pass.draws[0].pipeline.desc.depthStencil.depthWriteEnabled, false);
    assert.ok(pass.bindGroups.some((record) => record.slot === 3), "the depth texture binds at group 3");
    const stats = h.mount.__gosxScene3DWebGPUStats;
    assert.equal(stats.computeParticleSoftDrawCalls, 1);
    assert.equal(stats.computeParticleDrawCalls, 2, "hard and soft draws both count once");
    const module = h.fake.state.shaderModules.find((m) => m.label === "points-soft-frag");
    for (const piece of ["soft: vec4f", "@builtin(position) fragCoord", "softParticleFade(fragCoord)", "@group(3) @binding(0) var softDepth", "fn pointsShade("]) {
      assert.ok(module.code.includes(piece), "soft fragment carries " + piece);
    }
  } finally { h.renderer.dispose(); }
});

test("the shipped WebGPU chunk keeps the soft shader coherent after shader packing", async () => {
  // The bundle packer renames shader locals and strips indentation. The soft
  // entry point must still declare the extended uniforms and call the shared
  // shading function it was packed beside.
  const h = await renderWebGPU([particles({ blendMode: "alpha", softDistance: 0.5 })], 4, false);
  try {
    const soft = h.fake.state.shaderModules.find((m) => m.label === "points-soft-frag");
    const hard = h.fake.state.shaderModules.find((m) => m.label === "points-frag");
    assert.ok(soft && hard, "both points fragment modules compile from the shipped chunk");
    for (const code of [soft.code, hard.code]) {
      assert.match(code, /fn pointsShade\(in: PointsInput\) -> vec4f/);
      assert.match(code, /return pointsShade\(in\)|pointsShade\(in\);/);
    }
    assert.match(soft.code, /struct PointsUniforms \{[^}]*soft: vec4f,?\s*\}/);
    assert.doesNotMatch(hard.code, /soft: vec4f/);
  } finally { h.renderer.dispose(); }
});

test("WebGPU keeps hard sprites when softness is not requested or cannot apply", async () => {
  for (const [label, entry] of [
    ["no distance", particles({ blendMode: "alpha" })],
    ["opaque blend", particles({ blendMode: "opaque", softDistance: 1 })],
    ["depth write", particles({ blendMode: "alpha", softDistance: 1 }, { depthWrite: true })],
  ]) {
    const h = await renderWebGPU([entry]);
    try {
      assert.equal(softPasses(h).length, 0, label);
      assert.equal(h.fake.state.shaderModules.some((m) => m.label === "points-soft-frag"), false, label);
    } finally { h.renderer.dispose(); }
  }
});

test("WebGPU soft fade linearizes depth for perspective and orthographic projections", async () => {
  const h = await renderWebGPU([particles({ blendMode: "alpha", softDistance: 0.5 })]);
  try {
    const code = h.fake.state.shaderModules.find((m) => m.label === "points-soft-frag").code;
    assert.match(code, /-\(m\[3\]\[2\] - z \* m\[3\]\[3\]\) \/ \(z \* m\[2\]\[3\] - m\[2\]\[2\]\)/);
    // Evaluate the same expression against the [0,1]-remapped matrices the
    // renderer uploads (row 2 = 0.5 * (row 2 + row 3)).
    const distance = (m, z) => -(m[14] - z * m[15]) / (z * m[11] - m[10]);
    const near = 0.05, far = 128;
    const gl = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -(far + near) / (far - near), -1, 0, 0, -2 * far * near / (far - near), 0];
    const ortho = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -2 / (far - near), 0, 0, 0, -(far + near) / (far - near), 1];
    for (const m of [gl, ortho]) {
      m[10] = 0.5 * (m[10] + m[11]); m[14] = 0.5 * (m[14] + m[15]);
      assert.ok(Math.abs(distance(m, 0) - near) < 1e-6, "depth 0 is the near plane");
      assert.ok(Math.abs(distance(m, 1) - far) < 1e-3, "depth 1 is the far plane");
    }
  } finally { h.renderer.dispose(); }
});

function webglHarness(postEffects) {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2");
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  gl.FRAMEBUFFER_COMPLETE = 0x8cd5;
  gl.checkFramebufferStatus = () => gl.FRAMEBUFFER_COMPLETE;
  const sources = [];
  const shaderSource = gl.shaderSource.bind(gl);
  gl.shaderSource = (shader, source) => { sources.push(source); shaderSource(shader, source); };
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = postEffects;
  bundle.computeParticles = [particles({ color: "#ffffff", size: 4, blendMode: "alpha", softDistance: 0.5 })];
  return { ...h, gl, mount, sources, bundle, frame: () => h.renderer.render(bundle, { width: h.canvas.width, height: h.canvas.height }) };
}

test("WebGL2 copies scene depth once and draws soft sprites through the post target", async () => {
  const h = webglHarness([{ kind: "toneMapping" }]);
  try {
    h.frame(); await flushAsyncWork(); h.frame(); await flushAsyncWork(); h.frame();
    const soft = h.sources.find((source) => source.includes("softParticleFade"));
    assert.ok(soft, "the soft points program compiles");
    assert.ok(soft.includes("vec4 pointsShade()") && soft.includes("softParticleFade()"));
    assert.equal(h.mount.__gosxScene3DWebGLStats.softDrawCalls, 1);
    const blits = h.gl.ops.filter((op) => op[0] === "blitFramebuffer" && op[1] === h.gl.DEPTH_BUFFER_BIT);
    assert.ok(blits.length >= 1 && blits.length <= 3, "at most one depth copy per frame");
  } finally { h.renderer.dispose(); }
});

test("WebGL2 draws hard sprites when the scene renders straight to the canvas", async () => {
  const h = webglHarness([]);
  try {
    h.frame(); await flushAsyncWork(); h.frame();
    assert.equal(h.sources.some((source) => source.includes("softParticleFade")), false);
    assert.equal(h.gl.ops.some((op) => op[0] === "blitFramebuffer"), false);
  } finally { h.renderer.dispose(); }
});
