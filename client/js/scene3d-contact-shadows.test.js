"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createWebGLRendererForPost, makeWebGLBundleWithCustomPost, createBoardWebGPUHarness, makePointsBundle } = require("./runtime-test-harness.js");

test("WebGL contact shadows bind view-space sunlight, bounded parameters, and a separate depth target", () => {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2"); gl.TEXTURE1 = gl.TEXTURE0 + 1; gl.deleteRenderbuffer = () => {};
  const vectors = new Map(); gl.uniform4fv = (loc, value) => vectors.set(loc.name, Array.from(value));
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.lights = [{ kind: "directional", directionX: 0, directionY: -1, directionZ: 0 }];
  bundle.postEffects = [{ kind: "ssao" }, { kind: "contactShadows", distance: 50, thickness: 4, intensity: 0.6, bias: 0.02 }, { kind: "fxaa" }];
  h.renderer.render(bundle, { width: 320, height: 180 });
  assert.deepEqual(vectors.get("u_contactParams"), [10, 2, 0.02, 0.6]);
  assert.ok(gl.ops.some(op => op[0] === "uniform3f" && op[1] === "u_lightDirection" && op[2] === 0 && op[3] === 1 && op[4] === 0));
  assert.equal(gl.ops.filter(op => op[0] === "uniform1i" && op[1] === "u_depthTexture").length, 2);
  h.renderer.dispose();
});

test("WebGPU contact shadows use depth, aligned camera uniforms, and an actual fullscreen dispatch", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.postEffects = [{ kind: "contactShadows", distance: 1.5, thickness: 0.125, intensity: 0.5, bias: 0.01, direction: { x: 0, y: -1, z: 0 } }];
  h.renderer.render(bundle, { width: 320, height: 180 });
  const shader = h.fake.state.shaderModules.find(m => m.label === "post-contactShadows");
  assert.ok(shader, "the contact shadow shader is compiled");
  assert.match(shader.code, /textureLoad\(depthTex/);
  assert.ok(h.fake.state.renderPasses.flatMap(p => p.draws).some(d => d.pipeline?.desc?.fragment?.module === shader));
  h.renderer.dispose();
});
