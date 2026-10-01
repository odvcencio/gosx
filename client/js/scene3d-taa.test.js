"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createWebGLRendererForPost, makeWebGLBundleWithCustomPost, createBoardWebGPUHarness, makePointsBundle } = require("./runtime-test-harness.js");

function temporalHarness(options = {}) {
  const h = createWebGLRendererForPost({ fresh: true, ...options });
  const gl = h.canvas.getContext("webgl2");
  gl.TEXTURE1 = gl.TEXTURE0 + 1; gl.FRAMEBUFFER_COMPLETE = 0x8cd5;
  gl.checkFramebufferStatus = () => gl.FRAMEBUFFER_COMPLETE; gl.deleteRenderbuffer = () => {};
  if (options.float !== false) {
    const ext = gl.getExtension.bind(gl);
    gl.getExtension = name => name === "EXT_color_buffer_float" ? {} : ext(name);
  }
  const params = [], projections = [];
  const uniform4f = gl.uniform4f.bind(gl), uniformMatrix = gl.uniformMatrix4fv.bind(gl);
  gl.uniform4f = (loc, ...v) => { if (loc.name === "u_temporalParams") params.push(v); uniform4f(loc, ...v); };
  gl.uniformMatrix4fv = (loc, transpose, v) => { if (loc.name === "u_projection") projections.push(Array.from(v)); uniformMatrix(loc, transpose, v); };
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = [{ kind: "toneMapping" }, { kind: "taa", historyWeight: 0.9, clampGamma: 1.25, depthThreshold: 0.01 }];
  return { ...h, gl, params, projections, mount, bundle, frame: () => h.renderer.render(bundle, { width: h.canvas.width, height: h.canvas.height }) };
}

test("TAA jitters the rendered projection and reuses only valid color and depth history", () => {
  const h = temporalHarness();
  h.frame(); h.frame();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "taa");
  assert.deepEqual(h.params.map(p => p[3]), [0, 1]);
  assert.notEqual(h.projections[0][8], h.projections[1][8], "Halton samples move the actual render projection");
  assert.equal(h.gl.ops.filter(op => op[0] === "blitFramebuffer" && op[1] === h.gl.DEPTH_BUFFER_BIT).length, 2);
  h.bundle.camera.x += 10; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "camera cut rejects history");
  h.frame(); assert.equal(h.params.at(-1)[3], 1);
  h.bundle.postEffects[0].exposure = 2; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "effect change rejects history");
  h.canvas.width = 640; h.canvas.height = 360; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "resize rejects history");
  h.bundle.postEffects = []; h.frame();
  h.bundle.postEffects = [{ kind: "taa" }]; h.frame();
  assert.equal(h.params.at(-1)[3], 0, "quality suppression discards history");
  h.renderer.dispose();
});

test("TAA falls back to FXAA without jitter or history if float targets or shader support are absent", () => {
  for (const options of [{ float: false }, { rejectShaderSources: ["u_temporalParams"] }]) {
    const h = temporalHarness(options); h.frame();
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
    assert.deepEqual(h.params, []);
    assert.equal(h.gl.ops.filter(op => op[0] === "blitFramebuffer").length, 0);
    h.renderer.dispose();
  }
});

test("WebGPU accepts a TAA quality rung and dispatches FXAA as its supported fallback", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.postEffects = [{ kind: "taa" }];
  h.renderer.render(bundle, { width: 320, height: 180 });
  const fxaa = h.fake.state.shaderModules.find(m => m.label === "post-fxaa");
  assert.ok(fxaa);
  assert.ok(h.fake.state.renderPasses.flatMap(p => p.draws).some(d => d.pipeline?.desc?.fragment?.module === fxaa));
  h.renderer.dispose();
});

test("disabling TAA keeps FXAA without temporal allocations, reprojection uploads, or history copies", () => {
  const h = temporalHarness();
  h.frame(); h.frame();
  h.bundle.postEffects = [{ kind: "toneMapping" }, { kind: "fxaa" }];
  h.frame();
  const start = h.gl.ops.length, params = h.params.length;
  const extension = h.gl.getExtension;
  let temporalChecks = 0;
  h.gl.getExtension = name => { if (name === "EXT_color_buffer_float") temporalChecks++; return extension(name); };
  h.frame(); h.frame();
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-antialiasing"), "fxaa");
  assert.equal(h.params.length, params);
  assert.equal(temporalChecks, 0);
  assert.equal(h.gl.ops.slice(start).some(op => ["createTexture", "createFramebuffer", "blitFramebuffer"].includes(op[0])), false);
  h.renderer.dispose();
});
