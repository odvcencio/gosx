"use strict";
// Auto exposure (Environment.AutoExposure): WebGPU measures with a compute
// pass, WebGL2 with a mip-chain reduction; both multiply the linear HDR scene
// target by the adapted value before the post chain and never read it back
// on the frame path.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const ts = require("../runtime/node_modules/typescript");
const {
  createBoardWebGPUHarness, makePointsBundle, createWebGLRendererForPost, makeWebGLBundleWithCustomPost, flushAsyncWork,
} = require("./runtime-test-harness.js");

// The base chunk keeps the authored object; each renderer's post-effect chunk
// fills the defaults where it reads them (exposure-wgpu.ts, exposure-gl.ts).
function loadNormalizer(file, name) {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", file), "utf8");
  const start = source.indexOf("function " + name + "(");
  const end = source.indexOf("\n}\n", start) + 3;
  const context = {};
  vm.createContext(context);
  vm.runInContext(ts.transpileModule(source.slice(start, end), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nglobalThis.normalize = " + name + ";", context);
  return context.normalize;
}

test("both renderers' normalizers fill defaults and order the bounds", () => {
  for (const [file, name] of [["exposure-wgpu.ts", "wgpuAutoExposureSettings"], ["exposure-gl.ts", "sceneWebGLAutoExposureSettings"]]) {
    const normalize = loadNormalizer(file, name);
    assert.equal(normalize(null), null, file);
    assert.equal(normalize(true), null, file);
    assert.deepEqual(JSON.parse(JSON.stringify(normalize({}))), { minExposure: 0.1, maxExposure: 8, key: 0.18, speedUp: 3, speedDown: 1, compensation: 0 }, file);
    const swapped = normalize({ minExposure: 6, maxExposure: 2, compensation: 40, key: -1 });
    assert.deepEqual([swapped.minExposure, swapped.maxExposure, swapped.compensation, swapped.key], [2, 6, 8, 0.18], file);
  }
});

async function renderWebGPU(environment, frames = 3) {
  const h = await createBoardWebGPUHarness({ fresh: true });
  const bundle = makePointsBundle({ id: "p", count: 1, positions: [0, 0, 0] });
  bundle.postEffects = [{ kind: "toneMapping" }];
  bundle.environment = environment;
  for (let i = 0; i < frames; i++) {
    h.renderer.render(bundle, { width: 320, height: 180 }, { nowMS: 1000 + i * 16 });
    await flushAsyncWork();
  }
  return h;
}

test("WebGPU measures luminance in one compute workgroup and multiplies the scene target", async () => {
  const h = await renderWebGPU({ autoExposure: { minExposure: 0.25, maxExposure: 4, key: 0.18, speedUp: 3, speedDown: 1, compensation: 0 } });
  try {
    const measures = h.fake.state.computePasses.filter((pass) => pass.descriptor && pass.descriptor.label === "gosx-exposure-measure");
    assert.ok(measures.length >= 1, "a measure pass runs");
    assert.equal(measures.at(-1).dispatches.at(-1).workgroupCountX, 1, "one workgroup covers the whole sampling grid");
    const applies = h.fake.state.renderPasses.filter((pass) => pass.descriptor && pass.descriptor.label === "gosx-exposure-apply");
    assert.ok(applies.length >= 1);
    assert.equal(applies.at(-1).descriptor.colorAttachments[0].loadOp, "load", "the multiply keeps the scene");
    const blend = applies.at(-1).draws[0].pipeline.desc.fragment.targets[0].blend;
    assert.deepEqual([blend.color.srcFactor, blend.color.dstFactor, blend.alpha.dstFactor], ["zero", "src", "one"]);
    const kernel = h.fake.state.shaderModules.find((m) => m.label === "exposure-measure").code;
    assert.match(kernel, /@workgroup_size\(256\)/);
    assert.match(kernel, /select\(params\.speedDown, params\.speedUp, target > current\)/, "brightening and darkening rates differ");
    assert.match(kernel, /1\.0 - exp\(-max\(params\.dt, 0\.0\) \* rate\)/, "exponential steps never overshoot, so adaptation cannot oscillate");
    // The first frame resets (adapts at once); the next frames carry dt.
    const params = h.fake.state.writeBufferCalls.filter((call) => call.data && call.data.length === 8 && Math.abs(call.data[1] - 0.25) < 1e-6 && Math.abs(call.data[3] - 0.18) < 1e-6);
    assert.equal(params[0].data[7], 1, "first frame resets");
    assert.ok(params.at(-1).data[7] === 0 && params.at(-1).data[0] > 0, "later frames adapt over dt");
  } finally { h.renderer.dispose(); }
});

test("WebGPU leaves exposure alone when the scene does not opt in", async () => {
  const h = await renderWebGPU({}, 2);
  try {
    assert.equal(h.fake.state.computePasses.some((pass) => pass.descriptor && pass.descriptor.label === "gosx-exposure-measure"), false);
    assert.equal(h.fake.state.shaderModules.some((m) => m.label === "exposure-measure"), false);
  } finally { h.renderer.dispose(); }
});

function webglHarness(environment, postEffects = [{ kind: "toneMapping" }]) {
  const h = createWebGLRendererForPost({ fresh: true });
  const gl = h.canvas.getContext("webgl2");
  const ext = gl.getExtension.bind(gl);
  gl.getExtension = (name) => name === "EXT_color_buffer_float" ? {} : ext(name);
  const bundle = makeWebGLBundleWithCustomPost();
  bundle.postEffects = postEffects;
  bundle.environment = environment;
  return { ...h, gl, bundle, frame: () => h.renderer.render(bundle, { width: h.canvas.width, height: h.canvas.height }) };
}

test("WebGL2 reduces log luminance through a mip chain and multiplies the scene target", async () => {
  const h = webglHarness({ autoExposure: { key: 0.18 } });
  try {
    h.frame(); await flushAsyncWork(); h.frame(); await flushAsyncWork(); h.frame();
    const ops = h.gl.ops;
    assert.ok(ops.some((op) => op[0] === "texStorage2D" && op[3] === h.gl.R16F && op[4] === 64), "a 64 x 64 R16F luminance target");
    assert.ok(ops.some((op) => op[0] === "generateMipmap"), "the mip chain averages luminance");
    assert.ok(ops.some((op) => op[0] === "blendFuncSeparate" && op[1] === h.gl.ZERO && op[2] === h.gl.SRC_COLOR && op[4] === h.gl.ONE), "dst * exposure, alpha kept");
    assert.equal(ops.some((op) => op[0] === "readPixels"), false, "no CPU readback");
  } finally { h.renderer.dispose(); }
});

test("WebGL2 skips auto exposure without float render targets or an opt-in", () => {
  for (const [label, h] of [["no opt-in", webglHarness({})], ["no float", (() => { const x = webglHarness({ autoExposure: {} }); x.gl.getExtension = () => null; return x; })()]]) {
    try {
      h.frame(); h.frame();
      assert.equal(h.gl.ops.some((op) => op[0] === "blendFuncSeparate" && op[2] === h.gl.SRC_COLOR), false, label);
    } finally { h.renderer.dispose(); }
  }
});
