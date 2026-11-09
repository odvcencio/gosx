"use strict";
// Depth of field: both backends run the same single-pass circle-of-confusion
// gather over a golden-angle spiral, from linear view depth, so one
// DOF{FocusDistance, Aperture, MaxBlur} blurs the same way on WebGL2 and WebGPU.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { createBoardWebGPUHarness, makePointsBundle, createWebGLRendererForPost, makeWebGLBundleWithCustomPost, flushAsyncWork } = require("./runtime-test-harness.js");

const read = (name) => fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");

test("both DOF shaders gather over a fixed golden-angle spiral with the same circle of confusion", () => {
  for (const name of ["webgl-dof.ts", "webgpu-dof.ts"]) {
    const source = read(name);
    assert.match(source, /GOLDEN_ANGLE[^=]*= 2\.39996323/, name);
    assert.match(source, /DOF_TAPS[^=]*= 48/, name);
    assert.match(source, /abs\(depth - (u_focusDistance|params\.focusDistance)\) \* (u_aperture|params\.aperture) \* maxBlur, 0\.0, maxBlur\)/, name + ": CoC grows with distance from focus, capped at MaxBlur");
    assert.match(source, /if \(sampleDepth > centerDepth\) \{? ?sampleCoC = min\(sampleCoC, centerCoC \* 2\.0\);/, name + ": background taps cannot spread past twice this pixel's CoC");
    assert.match(source, /smoothstep\(radius - 0\.5, radius \+ 0\.5, sampleCoC\)/, name + ": a tap counts only where its own CoC reaches this pixel");
    assert.match(source, /\(2\.0 \* (u_near|params\.near) \* (u_far|params\.far)\) \/ max\(0\.0001, /, name + ": linear view depth");
  }
});

test("WebGPU DOF uploads the focus distance and the camera clip planes", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
    bundle.camera = { x: 0, y: 0, z: 5, fov: 60, near: 0.25, far: 300 };
    bundle.postEffects = [{ kind: "dof", focusDistance: 6, aperture: 0.08, maxBlur: 12 }];
    for (let i = 0; i < 3; i++) { h.renderer.render(bundle, { width: 320, height: 180 }); await flushAsyncWork(); }
    const write = h.fake.state.writeBufferCalls.filter((c) => c.data && c.data.length === 8 && Math.abs(c.data[0] - 6) < 1e-6).at(-1);
    assert.ok(write, "a DOF parameter upload");
    const d = Array.from(write.data);
    assert.deepEqual([d[0], Math.round(d[1] * 100) / 100, d[2], d[3], d[6]], [6, 0.08, 12, 0.25, 300]);
    const module = h.fake.state.shaderModules.find((m) => typeof m.code === "string" && m.code.includes("DOFParams"));
    assert.ok(module && module.code.includes("GOLDEN_ANGLE"), "the gather shader ships");
  } finally { h.renderer.dispose(); }
});

test("WebGL DOF compiles the gather shader and keeps its uniforms", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2");
  const sources = [];
  const shaderSource = gl.shaderSource.bind(gl);
  gl.shaderSource = (shader, source) => { sources.push(source); shaderSource(shader, source); };
  const uniforms = {};
  const uniform1f = gl.uniform1f.bind(gl);
  gl.uniform1f = (loc, v) => { if (loc && loc.name) uniforms[loc.name] = v; uniform1f(loc, v); };
  try {
    const bundle = makeWebGLBundleWithCustomPost();
    bundle.camera = { x: 0, y: 0, z: 5, fov: 60, near: 0.25, far: 300 };
    bundle.postEffects = [{ kind: "dof", focusDistance: 6, aperture: 0.08, maxBlur: 12 }];
    h.renderer.render(bundle, { width: h.canvas.width, height: h.canvas.height });
    assert.ok(sources.some((s) => s.includes("GOLDEN_ANGLE") && s.includes("u_focusDistance")), "the gather fragment compiles");
    assert.deepEqual([uniforms.u_focusDistance, Math.round(uniforms.u_aperture * 100) / 100, uniforms.u_maxBlur, uniforms.u_near, uniforms.u_far], [6, 0.08, 12, 0.25, 300]);
  } finally { h.renderer.dispose(); }
});
