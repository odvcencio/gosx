"use strict";
// The WebGL2 executor of the Elio graph runner, on a recording WebGL2 double that tracks state (so restoration is
// checked, not assumed). Graphs are hand-built to the contract over kernels in the generated module.
const test = require("node:test"), assert = require("node:assert/strict");
const { RecordingGL, GLC, runnerContext, floatCaps, testGraph } = require("./elio-graph-test-support.js");

function setup(glOptions, graphEdit) {
  const context = runnerContext(), gl = new RecordingGL(glOptions);
  const g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const graph = testGraph("webgl2");
  if (graphEdit) graphEdit(graph);
  const textureCache = { _sceneTextureEpoch: 0 };
  const failures = [];
  const runner = g.create({ module, backend: "webgl2", graph, host: { gl, caps: floatCaps, textureCache }, onFail: (reason) => failures.push(reason) });
  let time = 10;
  const step = (extra) => runner.run(Object.assign({ time: (time += 1 / 60), dt: 1 / 60, cameraX: 0, cameraY: 30, cameraZ: 0 }, extra));
  // Resources are created in graph order, so the i-th texture is the i-th resource.
  const tex = (name) => gl.created.texture[graph.resources.findIndex((r) => r.name === name)];
  return { context, gl, runner, graph, textureCache, failures, step, tex };
}
const draws = (gl) => gl.draws.filter((d) => d.kind === "draw");
const names = (gl) => gl.draws.map((d) => d.kind === "mipmap" ? "mip:" + d.texture.format : d.kernel === "fft" ? "fft" + d.u32[2] : d.kernel);

test("passes run in graph order; a changed: pass runs on the first frame and when its parameter moves, a param: pass while the flag holds", () => {
  const s = setup();
  assert.equal(s.runner.failed, null);
  assert.equal(s.step(), true);
  assert.deepEqual(names(s.gl), ["evolve", "fft0", "fft1", "assemble", "mip:RGBA16F", "mip:RGBA16F"]);
  s.gl.draws.length = 0;
  s.step();
  assert.deepEqual(names(s.gl), ["fft0", "fft1", "assemble", "mip:RGBA16F", "mip:RGBA16F"], "evolve waits for windSpeed to change");
  s.gl.draws.length = 0;
  s.runner.setValue("windSpeed", 15);
  s.step(); s.step(); s.step();
  assert.equal(names(s.gl).filter((n) => n === "evolve").length, 2, "the tween clock starts on the first frame; the value moves on the next two");
  s.gl.draws.length = 0;
  s.runner.setValue("mips", 0);
  s.step();
  assert.ok(!names(s.gl).some((n) => n.startsWith("mip:")), "param:mips is false");
});

test("kernels write the targets the graph binds: multi-target setup, formats, viewports and elio_width", () => {
  const s = setup();
  s.step();
  const by = (k) => draws(s.gl).find((d) => d.kernel === k);
  const formats = (d) => Object.keys(d.attachments).map((k) => d.attachments[k].format);
  assert.deepEqual(formats(by("evolve")), ["RGBA32F", "RGBA32F"]);
  assert.deepEqual(formats(by("assemble")), ["RGBA16F", "RGBA16F"]);
  assert.deepEqual(formats(by("fft")), ["RGBA32F"]);
  assert.deepEqual([...by("evolve").drawBuffers], [GLC.COLOR_ATTACHMENT0, GLC.COLOR_ATTACHMENT0 + 1]);
  assert.equal(by("assemble").attachments[0], s.tex("disp"));
  assert.equal(by("assemble").attachments[1], s.tex("slope"));
  assert.equal(by("evolve").attachments[0], s.tex("a0"));
  assert.equal(by("evolve").attachments[1], s.tex("b0"));
  for (const d of draws(s.gl)) { assert.deepEqual(d.viewport.slice(2), [64, 64]); assert.equal(d.elioWidth, 64); }
  assert.ok(draws(s.gl).every((d) => Object.keys(d.attachments).length <= 2), "at most 32 bytes per pixel");
  // Buffers are RGBA32F textures `width` wide; the exported textures filter and carry mips.
  assert.deepEqual([s.tex("h0").format, s.tex("h0").w, s.tex("h0").h], ["RGBA32F", 64, 64]);
  const disp = s.tex("disp");
  assert.equal(disp.levels, 5);
  assert.equal(disp.params[GLC.TEXTURE_MIN_FILTER], GLC.LINEAR_MIPMAP_LINEAR);
  assert.equal(s.tex("a0").params[GLC.TEXTURE_MIN_FILTER], GLC.NEAREST, "float32 inputs are fetched, never filtered");
});

test("a pass samples what the last pass wrote and never the texture it renders into", () => {
  const s = setup();
  s.step();
  const [a, b] = draws(s.gl).filter((d) => d.kernel === "fft");
  assert.equal(a.input, s.tex("a0")); assert.equal(a.attachments[0], s.tex("a1"));
  assert.equal(b.input, s.tex("a1")); assert.equal(b.attachments[0], s.tex("a0"));
  for (const d of draws(s.gl)) for (const i of d.inputs) for (const o of Object.values(d.attachments)) assert.notEqual(i, o);
  // A graph that breaks the rule is disabled, and the host's GL state is still restored.
  const bad = setup(undefined, (g) => { g.passes[1].bindings[1].resource = "a0"; });
  const hostFB = bad.gl.createFramebuffer(); bad.gl.drawFB = hostFB;
  const warn = console.warn; console.warn = () => {};
  try { assert.equal(bad.step(), false); } finally { console.warn = warn; }
  assert.equal(bad.runner.failed, "error");
  assert.match(bad.runner.error.message, /reads a0, which it writes/);
  assert.equal(bad.gl.drawFB, hostFB);
  for (const kind of Object.keys(bad.gl.created)) assert.equal(bad.gl.live(kind).length, kind === "framebuffer" ? 1 : 0, kind + " leaked after the failure (the host's own framebuffer stays)");
  assert.deepEqual(bad.failures, ["error"]);
});

test("uniforms are packed from the layout with the frame's parameter values; constant blocks upload once", () => {
  const s = setup();
  s.step({ time: 1234.5 });
  const evolve = draws(s.gl).find((d) => d.kernel === "evolve");
  assert.ok(Math.abs(evolve.f32[0] - 234.5) < 1e-3, "t = time mod 1000");
  assert.deepEqual(evolve.u32.slice(0, 1), [64]);
  const fftBuffer = s.gl.created.buffer.find((b) => b.data.length === 16 && b.data[0] === 64);
  assert.ok(fftBuffer, "fft stage block holds n = 64");
  const lambdaBlock = () => s.gl.created.buffer[3]; // uniform blocks are created in pass order: evolve, fft-a, fft-b, assemble
  const before = lambdaBlock().data.join();
  s.runner.setValue("lambda", 1);
  s.step();
  assert.notEqual(lambdaBlock().data.join(), before, "the assemble block carries the new lambda");
  assert.equal(new DataView(lambdaBlock().data.buffer).getFloat32(16, true), 1);
});

test("frameSwaps exchange resources after each frame and exports follow the swap", () => {
  const s = setup(undefined, (g) => { g.frameSwaps = [["disp", "slope"]]; });
  const exportBefore = s.runner.export("disp0");
  assert.equal(exportBefore.tex, s.tex("disp"));
  s.step();
  assert.equal(s.runner.export("disp0").tex, s.tex("slope"), "disp now names the other texture");
  assert.equal(s.runner.export("slope0").tex, s.tex("disp"));
  s.gl.draws.length = 0;
  s.step();
  const assemble = draws(s.gl).find((d) => d.kernel === "assemble");
  assert.equal(assemble.attachments[0], s.tex("slope"), "the next frame renders into the swapped texture");
  assert.equal(s.runner.export("disp0").width, 64);
  assert.equal(s.runner.export("nope"), null);
});

test("GL state is saved before the passes and restored after, whatever the host had bound; the texture epoch is bumped", () => {
  const s = setup(), gl = s.gl;
  const hostFB = gl.createFramebuffer(), hostRead = gl.createFramebuffer(), hostProgram = gl.createProgram(), hostVAO = gl.createVertexArray();
  gl.drawFB = hostFB; gl.readFB = hostRead; gl.currentProgram = hostProgram; gl.vao = hostVAO; gl.unit = 2; gl.viewportRect = [3, 4, 500, 300];
  gl.colorMaskValue = [true, false, true, true]; gl.depthMaskValue = false;
  const epoch = s.textureCache._sceneTextureEpoch;
  s.step();
  assert.equal(gl.drawFB, hostFB); assert.equal(gl.readFB, hostRead); assert.equal(gl.currentProgram, hostProgram); assert.equal(gl.vao, hostVAO);
  assert.equal(gl.unit, 2); assert.deepEqual(gl.viewportRect, [3, 4, 500, 300]);
  assert.deepEqual([...gl.enabled].sort(), [GLC.BLEND, GLC.DEPTH_TEST, GLC.CULL_FACE, GLC.SCISSOR_TEST].sort());
  assert.deepEqual(gl.colorMaskValue, [true, false, true, true]); assert.equal(gl.depthMaskValue, false);
  for (const target of [GLC.PIXEL_PACK_BUFFER, GLC.PIXEL_UNPACK_BUFFER, GLC.UNIFORM_BUFFER]) assert.equal(gl.buffers[target], null);
  assert.equal(gl.uboBase, null);
  for (const d of draws(gl)) {
    assert.ok(![GLC.BLEND, GLC.DEPTH_TEST, GLC.CULL_FACE, GLC.SCISSOR_TEST].some((c) => d.enabled.has(c)), "passes run with blend, depth, cull and scissor off");
    assert.notEqual(d.vao, hostVAO);
  }
  assert.ok(s.textureCache._sceneTextureEpoch > epoch, "material bindings are rebound");
  const second = s.textureCache._sceneTextureEpoch;
  s.step();
  assert.ok(s.textureCache._sceneTextureEpoch > second);
});

test("gates: no EXT_color_buffer_float, too few units, or a kernel with no GLSL disable the graph with the reason and touch no GL object", () => {
  const none = setup({ extensions: [] });
  assert.equal(none.runner.failed, "EXT_color_buffer_float");
  assert.equal(none.step(), false);
  for (const kind of Object.keys(none.gl.created)) assert.equal(none.gl.created[kind].length, 0);
  assert.equal(none.textureCache._sceneTextureEpoch, 0);
  assert.equal(setup({ limits: { [GLC.MAX_DRAW_BUFFERS]: 1 } }).runner.failed, "texture-units");
  const context = runnerContext(), g = context.window.__gosx_elio_graph;
  const set = context.window.__gosx_scene3d_elio_ocean;
  const module = { kernels: Object.assign({}, set.kernels, { oceanEvolve: Object.assign({}, set.kernels.oceanEvolve, { glsl: undefined }) }) };
  const r = g.create({ module, backend: "webgl2", graph: testGraph("webgl2"), host: { gl: new RecordingGL(), caps: floatCaps } });
  assert.equal(r.failed, "kernel:ocean_evolve");
});

test("when the driver rejects a multi-target framebuffer, each output gets its own and the kernel runs once per output", () => {
  const s = setup({ incompleteMultiTarget: true });
  assert.equal(s.runner.failed, null);
  s.step();
  const evolves = draws(s.gl).filter((d) => d.kernel === "evolve");
  assert.equal(evolves.length, 2);
  assert.deepEqual(evolves.map((d) => Object.keys(d.attachments).join()), ["0", "1"]);
  assert.deepEqual([...evolves[0].drawBuffers], [GLC.COLOR_ATTACHMENT0]);
  assert.deepEqual([...evolves[1].drawBuffers], [GLC.NONE, GLC.COLOR_ATTACHMENT0 + 1], "output 1 draws only to attachment 1");
  assert.equal(draws(s.gl).filter((d) => d.kernel === "fft").length, 2, "single-output passes are not repeated");
});

test("dispose releases every GL object; a lost context does no work and still disposes cleanly", () => {
  const s = setup();
  s.step();
  assert.ok(s.gl.live("texture").length > 0 && s.gl.live("framebuffer").length > 0);
  s.runner.dispose();
  for (const kind of Object.keys(s.gl.created)) assert.equal(s.gl.live(kind).length, 0, kind + " leaked");
  assert.equal(s.step(), false, "a disposed runner does nothing");
  const lost = setup();
  lost.step();
  lost.gl.contextLost = true;
  const draws0 = lost.gl.draws.length;
  assert.equal(lost.step(), false);
  assert.equal(lost.gl.draws.length, draws0, "no GL work on a lost context");
  assert.equal(lost.runner.failed, null, "a lost context is the renderer's to report");
  lost.runner.dispose();
  for (const kind of Object.keys(lost.gl.created)) assert.equal(lost.gl.live(kind).length, 0, kind + " leaked after loss");
});

test("a shader that does not compile disables the graph and releases what was built", () => {
  const warn = console.warn; console.warn = () => {};
  try {
    const s = setup({}, null);
    const gl = new RecordingGL(); gl.rejectShader = (source) => /OceanAssembleParams_ubo/.test(source);
    const context = runnerContext(), g = context.window.__gosx_elio_graph;
    const r = g.create({ module: context.window.__gosx_scene3d_elio_ocean, backend: "webgl2", graph: testGraph("webgl2"), host: { gl, caps: floatCaps } });
    assert.equal(r.failed, "error");
    assert.match(r.error.message, /did not compile/);
    for (const kind of ["shader", "program"]) assert.equal(gl.live(kind).length, 0, kind + " leaked");
    assert.ok(s.runner);
  } finally { console.warn = warn; }
});

test("repeat passes run their body per iteration with the index in the uniform and the pairs swapped, never sampling their target", () => {
  const s = setup(undefined, (g) => {
    const target = { width: 64, height: 64, elioWidth: 64 };
    g.passes = [{ type: "repeat", name: "stages", count: 4, index: "stage", swap: [["a0", "a1"]], body: [
      { name: "fft", type: "kernel", kernel: "fft_gather_64", entry: "fft_stage", when: "always", target,
        bindings: [{ name: "src", resource: "a0" }, { name: "dst", resource: "a1" }], uniforms: [{ name: "fft", values: { n: 64, stage: "param:stage", axis: 0 } }] }] }];
    g.exports = {};
  });
  assert.equal(s.runner.failed, null);
  s.step();
  const ffts = draws(s.gl).filter((d) => d.kernel === "fft");
  assert.deepEqual(ffts.map((d) => d.u32[1]), [0, 1, 2, 3], "the index parameter is the stage");
  assert.deepEqual(ffts.map((d) => d.input), [s.tex("a0"), s.tex("a1"), s.tex("a0"), s.tex("a1")], "each iteration samples the last one's output");
  assert.deepEqual(ffts.map((d) => d.attachments[0]), [s.tex("a1"), s.tex("a0"), s.tex("a1"), s.tex("a0")]);
  assert.ok(ffts.every((d, i) => d.input !== d.attachments[0]));
});

test("a mip pass may read level L-1 and write level L of one texture; the same level is a feedback loop and disables the graph", () => {
  const mipGraph = (srcLevel, dstLevel) => (g) => {
    g.passes = [{ name: "mip", type: "kernel", kernel: "ocean_mip", entry: "ocean_mip", when: "always", target: { width: 32, height: 32, elioWidth: 32 },
      bindings: [{ name: "src", resource: "disp", level: srcLevel }, { name: "dst", resource: "disp", level: dstLevel }],
      uniforms: [{ name: "mip", values: { srcLevel: srcLevel, dstWidth: 32, dstHeight: 32, pad: 0 } }] }];
  };
  const ok = setup(undefined, mipGraph(0, 1));
  assert.equal(ok.step(), true);
  const d = draws(ok.gl)[0];
  assert.equal(d.attachments[0], ok.tex("disp"));
  assert.equal(ok.gl.attachmentLevels && ok.gl.attachmentLevels[0], 1, "level 1 of the texture is attached");
  const warn = console.warn; console.warn = () => {};
  try {
    const bad = setup(undefined, mipGraph(1, 1));
    assert.equal(bad.step(), false);
    assert.match(bad.runner.error.message, /reads disp, which it writes/);
  } finally { console.warn = warn; }
});

test("a resource fed by a buffer parameter is uploaded from it every frame", () => {
  const s = setup(undefined, (g) => {
    g.params.push({ name: "probes", type: "vec4<f32>[]", count: 2, source: { kind: "value", value: [1, 2, 3, 4, 5, 6, 7, 8] } });
    g.resources[0].feed = "probes";
  });
  s.step(); s.step();
  const uploads = s.gl.ops.filter((o) => o[0] === "texSubImage2D");
  assert.equal(uploads.length, 2);
  assert.equal(uploads[0][1], s.tex("h0"));
});
