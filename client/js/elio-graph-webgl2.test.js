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
