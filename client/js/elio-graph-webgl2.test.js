"use strict";
// The WebGL2 executor of the Elio graph runner, on a recording WebGL2 double that tracks state (so restoration is
// checked, not assumed). Graphs are hand-built to the contract over kernels in the generated module.
const test = require("node:test"), assert = require("node:assert/strict");
const { RecordingGL, GLC, runnerContext, floatCaps, testGraph } = require("./elio-graph-test-support.js");

function setup(glOptions, graphEdit, hostExtra) {
  const context = runnerContext(), gl = new RecordingGL(glOptions);
  const g = context.window.__gosx_elio_graph, module = context.window.__gosx_scene3d_elio_ocean;
  const graph = testGraph("webgl2");
  if (graphEdit) graphEdit(graph);
  const textureCache = { _sceneTextureEpoch: 0 };
  const failures = [];
  const runner = g.create({ module, backend: "webgl2", graph, host: Object.assign({ gl, caps: floatCaps, textureCache }, hostExtra), onFail: (reason) => failures.push(reason) });
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

// A WebGL2 kernel pass reads a buffer at the buffer's own width. A graph may still copy a buffer to lay it out at another width with a copy pass,
// which lays the same elements out `width` texels wide, GPU to GPU through a pixel buffer, and only when the source changed.
function wideGraph(g, extra) {
  const wide = (name) => ({ name, kind: "buffer", elementType: "vec4<f32>", count: 4096, width: 32 });
  g.resources.push(wide("a0_w"), wide("b0_w"));
  const assemble = g.passes.find((p) => p.name === "assemble");
  assemble.target = Object.assign({}, assemble.target, { elioWidth: 32 });
  assemble.bindings = assemble.bindings.map((b) => b.name === "fieldA" ? Object.assign({}, b, { resource: "a0_w" }) : b.name === "fieldB" ? Object.assign({}, b, { resource: "b0_w" }) : b);
  const at = g.passes.indexOf(assemble);
  g.passes.splice(at, 0, { name: "copy-a", type: "copy", from: "a0", to: "a0_w", width: 32, when: "always" }, { name: "copy-b", type: "copy", from: "b0", to: "b0_w", width: 32, when: "always" });
  if (extra) extra(g);
}

test("copy passes lay a buffer out at another width through a pixel buffer, and only when the source changed", () => {
  const s = setup(undefined, wideGraph);
  assert.equal(s.runner.failed, null);
  s.step();
  const reads = () => s.gl.ops.filter((o) => o[0] === "readPixels"), writes = () => s.gl.ops.filter((o) => o[0] === "texSubImage2D");
  assert.equal(reads().length, 2, "a0 and b0 are copied");
  assert.deepEqual(writes().map((o) => o[2] + "x" + o[3]), ["32x128", "32x128"], "the same 4,096 texels, 32 per row");
  assert.deepEqual(reads().map((o) => o[2] + "x" + o[3]), ["64x64", "64x64"]);
  assert.ok(reads().every((o, i) => o[5] && o[5] === writes()[i][5]), "packed and unpacked through the same pixel buffer");
  assert.equal(writes()[0][1], s.tex("a0_w"), "into the copy's own texture");
  const assemble = draws(s.gl).find((d) => d.kernel === "assemble");
  assert.equal(assemble.elioWidth, 32);
  assert.equal(assemble.inputs[0], s.tex("a0_w"));
  assert.equal(assemble.inputs[1], s.tex("b0_w"));
  s.step();
  assert.equal(reads().length, 3, "frame 2: the FFT rewrote a0, nothing rewrote b0 (evolve waits for a parameter change)");
  for (const target of [GLC.PIXEL_PACK_BUFFER, GLC.PIXEL_UNPACK_BUFFER]) assert.equal(s.gl.buffers[target], null);
  s.runner.dispose();
  for (const kind of Object.keys(s.gl.created)) assert.equal(s.gl.live(kind).length, 0, kind + " leaked");
});

test("a pass reads each input buffer at the buffer's own width, set through its width uniform; a copy pass honours its condition", () => {
  const plain = setup(undefined, (g) => { const p = g.passes.find((x) => x.name === "assemble"); p.target = Object.assign({}, p.target, { elioWidth: 32 }); });
  assert.equal(plain.step(), true, "reading a 64-wide buffer in a 32-wide pass is valid");
  const assemble = draws(plain.gl).find((d) => d.kernel === "assemble");
  assert.equal(assemble.elioWidth, 32);
  assert.deepEqual(Object.values(assemble.widths), [64, 64], "both inputs are 64 texels wide whatever the pass width");
  const s = setup(undefined, (g) => wideGraph(g, (x) => { x.passes.find((p) => p.name === "copy-a").when = "param:mips"; }));
  s.runner.setValue("mips", 0);
  s.step();
  assert.equal(s.gl.ops.filter((o) => o[0] === "readPixels").length, 1, "copy-a is skipped while mips is false");
});

test("a copy inside a repeat follows the repeat's swaps", () => {
  const s = setup(undefined, (g) => {
    g.resources.push({ name: "w", kind: "buffer", elementType: "vec4<f32>", count: 4096, width: 32 });
    g.passes = [{ name: "r", type: "repeat", count: 2, index: "i", swap: [["a0", "a1"]], body: [{ name: "c", type: "copy", from: "a0", to: "w", width: 32 }] }];
    g.exports = {};
  });
  s.step();
  assert.equal(s.gl.ops.filter((o) => o[0] === "readPixels").length, 2);
  const reads = s.gl.ops.filter((o) => o[0] === "readPixels");
  assert.deepEqual(reads.map((o) => o[1]), [s.gl.created.framebuffer.find((f) => f.attachments[0] === s.tex("a0")), s.gl.created.framebuffer.find((f) => f.attachments[0] === s.tex("a1"))], "iteration 0 reads a0, iteration 1 reads a1");
});

// A host may run some pass groups at a lower rate (the wake on a slow GPU): matching passes run every n-th frame, with dt the time since their last run.
function throttled(extra) {
  return setup(undefined, (g) => {
    g.params.push({ name: "half", type: "f32", source: { kind: "expr", op: "mul", args: ["param:dt", 0.5] } }, { name: "cellv", type: "f32", source: { kind: "value", value: 0 } },
      { name: "moved", type: "f32", source: { kind: "delta", of: "param:cellv" } });
    const evolve = g.passes.find((p) => p.name === "evolve");
    evolve.when = "always";
    evolve.uniforms = [{ name: "ev", values: { n: 64, t: "param:half", tile: "param:moved", loopPeriod: 1000 } }];
  }, Object.assign({ throttle: [{ match: "^evolve$", every: 2 }] }, extra));
}

test("throttle: a matching pass runs every n-th frame, with the dt of the frames it skipped and the movement since its last run", () => {
  const s = throttled();
  assert.equal(s.runner.failed, null);
  const evolves = () => draws(s.gl).filter((d) => d.kernel === "evolve");
  const frame = (cell) => { s.runner.setValue("cellv", cell); s.runner.run({ time: (frame.t = (frame.t || 10) + 1 / 60), dt: 0.1, cameraX: 0, cameraY: 0, cameraZ: 0 }); };
  frame(0);
  assert.equal(evolves().length, 1, "frame 0 runs");
  frame(2);
  assert.equal(evolves().length, 1, "frame 1 is skipped");
  frame(7);
  assert.equal(evolves().length, 2, "frame 2 runs");
  const last = evolves()[1];
  assert.ok(Math.abs(last.f32[0] - 0.1) < 1e-6, "half of dt = 0.2 s (two frames of 0.1): " + last.f32[0]);
  assert.equal(last.f32[1], 7, "movement since the last run: 0 to 7, not the last frame's 5");
  frame(7); frame(7);
  assert.equal(evolves().length, 3, "every second frame");
  assert.equal(evolves()[2].f32[1], 0, "nothing moved since the last run");
  // Other passes keep their own rate and the ordinary params.
  assert.ok(draws(s.gl).filter((d) => d.kernel === "assemble").length >= 5);
});

test("throttle: a pass whose gating parameter changed runs at once, so a switch-on is never delayed", () => {
  const s = setup(undefined, (g) => {
    g.params.push({ name: "on", type: "bool", source: { kind: "value", value: 0 } });
    g.passes.find((p) => p.name === "evolve").when = "param:on";
  }, { throttle: [{ match: "^evolve$", every: 4 }] });
  const evolves = () => draws(s.gl).filter((d) => d.kernel === "evolve").length;
  const f = (t) => s.runner.run({ time: t, dt: 0.016, cameraX: 0, cameraY: 0, cameraZ: 0 });
  f(1); f(1.02);
  assert.equal(evolves(), 0);
  s.runner.setValue("on", 1);
  f(1.04);
  assert.equal(evolves(), 1, "it runs on the frame it is switched on, not up to 3 frames later");
  f(1.06); f(1.08);
  assert.equal(evolves(), 1);
});

test("profile: a host hook wraps every executed pass, by name, and skipped passes are not reported", () => {
  const seen = [];
  const s = setup(undefined, (g) => { g.passes.find((p) => p.name === "evolve").when = "param:off"; g.params.push({ name: "off", type: "bool", source: { kind: "value", value: 0 } }); },
    { profile: (name, fn) => { seen.push(name); fn(); } });
  s.runner.run({ time: 1, dt: 0.016, cameraX: 0, cameraY: 0, cameraZ: 0 });
  assert.ok(seen.length > 0 && seen.includes("assemble"));
  assert.ok(!seen.includes("evolve"));
  assert.equal(draws(s.gl).length, seen.filter((n) => n !== undefined).length - seen.filter((n) => /copy|mip/.test(n)).length);
});

test("throttle: a frame swap that only a throttled group touches happens only on frames the group runs", () => {
  const s = setup(undefined, (g) => { g.frameSwaps = [["disp", "slope"]]; }, { throttle: [{ match: "^(assemble|mip-)", every: 2 }] });
  const tex = () => s.runner.export("disp0").tex;
  const first = tex();
  const f = () => s.runner.run({ time: 1, dt: 0.016, cameraX: 0, cameraY: 0, cameraZ: 0 });
  f();
  assert.notEqual(tex(), first, "the first frame runs the group and swaps");
  const after = tex();
  f();
  assert.equal(tex(), after, "a skipped frame leaves the pair alone");
  f();
  assert.equal(tex(), first, "the next run swaps back");
});

test("throttle: parameters the group holds read as of its last run, so a surface pairs them with the group's textures", () => {
  const s = throttled({ throttle: [{ match: "^evolve$", every: 2, hold: "^cellv$" }] });
  const go = (cell) => { s.runner.setValue("cellv", cell); s.runner.run({ time: 1, dt: 0.1, cameraX: 0, cameraY: 0, cameraZ: 0 }); };
  go(3);
  assert.equal(s.runner.value("cellv"), 3, "the group ran this frame");
  go(9);
  assert.equal(s.runner.value("cellv"), 3, "skipped frame: the texture still reflects the earlier position");
  assert.equal(s.runner.value("hasNoHold"), 0);
  go(12);
  assert.equal(s.runner.value("cellv"), 12);
});
