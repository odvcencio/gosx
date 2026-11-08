"use strict";
// A synchronous WebGL query (getParameter, isEnabled, getError, ...) waits for the GPU process to answer. When that process
// is busy each one stalls the main thread, so a frame that makes a dozen of them blocks for as long as the GPU is behind.
// These tests wrap the contexts the renderer and the ocean graph draw with, count the synchronous queries made in steady-state
// frames, and fail on any. Queries made while building (the first frames) are allowed.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { createRequire } = require("node:module");
const ts = createRequire(path.join(__dirname, "..", "runtime", "package.json"))("typescript");
const { createWebGLRendererForPost, makePointsBundle } = require("./runtime-test-harness.js");
const { RecordingGL, loadSources, floatCaps } = require("./elio-graph-test-support.js");

const scene3d = path.join(__dirname, "..", "runtime", "scene3d");
const backend = fs.readFileSync(path.join(scene3d, "mount-backend.ts"), "utf8");
const recordSource = backend.slice(backend.indexOf("  function recordSceneWaterFrame("), backend.indexOf("  function sceneDebugBundleCounts("));
const CHUNK = ["ocean-fft-surface.ts", "ocean-fft-surface-gl.ts", "ocean-fft.ts"];

// Calls that return a value from the GPU process (or the driver) and so wait for it. readPixels into client memory and
// clientWaitSync with a timeout are judged separately below. getUniformLocation, getAttribLocation and getUniformBlockIndex are not
// counted: Chrome answers them from a client-side copy of the program's tables after the first call, and the renderer makes them per draw.
const SYNC = new Set(["getParameter", "isEnabled", "getError", "checkFramebufferStatus", "getProgramParameter", "getShaderParameter",
  "getActiveUniform", "getActiveAttrib", "getShaderInfoLog",
  "getProgramInfoLog", "getSupportedExtensions", "getExtension", "getTexParameter", "getBufferParameter", "getQueryParameter",
  "getSyncParameter", "getFramebufferAttachmentParameter", "getRenderbufferParameter", "getUniform", "getVertexAttrib", "getShaderPrecisionFormat"]);

// counter(gl) wraps every method of gl once and returns { reset, calls }, the sync queries since the last reset.
function counter(gl) {
  const calls = [];
  const names = new Set();
  for (let o = gl; o && o !== Object.prototype; o = Object.getPrototypeOf(o)) for (const k of Object.getOwnPropertyNames(o)) names.add(k);
  for (const name of names) {
    const fn = gl[name];
    if (typeof fn !== "function" || name === "constructor") continue;
    gl[name] = function (...args) {
      if (SYNC.has(name)) calls.push(name + (typeof args[0] === "number" ? "(0x" + args[0].toString(16) + ")" : ""));
      else if (name === "readPixels" && !(typeof args[6] === "number" || args[6] === 0)) calls.push("readPixels(client memory)");
      else if (name === "clientWaitSync" && args[2] > 0) calls.push("clientWaitSync(timeout " + args[2] + ")");
      return fn.apply(this, args);
    };
  }
  return { calls, reset() { calls.length = 0; }, summary() { const m = new Map(); for (const c of calls) m.set(c, (m.get(c) || 0) + 1); return [...m].map(([k, v]) => k + " x" + v).join(", "); } };
}

const WARMUP = 4, FRAMES = 30;

function seaBundle(spectrum, extra) {
  const b = makePointsBundle(null);
  b.points = [];
  b.environment.sky = { mode: "physical" };
  b.environment.ocean = Object.assign({ level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1, deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0", clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry: null, spectrum }, extra || {});
  b.camera.y = 30; b.camera.z = 0;
  return b;
}

// The renderer on the harness's fake context and the graph on the recording double, both counted. The graph glue is the real one.
function rig() {
  const h = createWebGLRendererForPost({ fresh: true });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  for (const [name, fn] of Object.entries({ createSampler: () => ({}), deleteSampler() {}, samplerParameteri() {}, bindSampler() {}, uniform4fv() {}, uniform1i() {}, blendFuncSeparate() {}, uniformMatrix4fv() {} })) if (!gl[name]) gl[name] = fn;
  const context = h.env.context;
  context.window = context.window || context;
  for (const name of ["Float32Array", "Float64Array", "Uint32Array", "Uint8Array", "ArrayBuffer", "DataView", "Set", "Map", "Promise", "Error"]) if (!context[name]) context[name] = globalThis[name];
  context.performance = context.performance || { timeOrigin: 1e12, now: () => 0 };
  loadSources(context, ["ocean-fft-kernels.ts", "elio-graph.ts", "elio-graph-gather.ts", "elio-graph-compute.ts", ...CHUNK]);
  const recording = new RecordingGL(), real = context.window.__gosx_scene3d_ocean_fft_gl_api;
  const counts = { renderer: counter(gl), graph: counter(recording) };
  context.window.__gosx_scene3d_ocean_fft_gl_api = { run: (resources, g, host) => {
    const eng = real.run(resources, recording, Object.assign({}, host, { caps: floatCaps }));
    return eng ? { bind() {}, fail: (r) => eng.fail(r), dispose: () => eng.dispose() } : null;
  } };
  vm.runInContext(ts.transpileModule(recordSource, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText + "\nthis.recordFrame = recordSceneWaterFrame;", context);
  return { h, mount, context, gl, recording, counts, fft: context.window.__gosx_scene3d_ocean_fft_api };
}

function steady(r, bundle, frames = FRAMES) {
  const frame = (i) => { r.h.renderer.render(bundle, { width: 320, height: 180 }, { sceneTime: 1 + i / 60 }); r.context.recordFrame(r.mount, bundle, 1 + i / 60, "submitted"); };
  for (let i = 0; i < WARMUP; i++) frame(i);
  r.counts.renderer.reset(); r.counts.graph.reset();
  for (let i = 0; i < frames; i++) frame(WARMUP + i);
  return { renderer: r.counts.renderer.summary(), graph: r.counts.graph.summary(), n: r.counts.renderer.calls.length + r.counts.graph.calls.length };
}

test("steady frames of the spectral ocean scene make no synchronous GL query", () => {
  const r = rig();
  const bundle = seaBundle({ windSpeed: 9 });
  const out = steady(r, bundle);
  assert.ok(r.fft.water(), "the graph ran, so its state save and restore were exercised");
  assert.ok(r.recording.draws.length > 0, "the graph drew passes");
  if (process.env.GL_QUERY_REPORT) console.log("ocean:", JSON.stringify(out));
  assert.equal(out.graph, "", "graph (" + FRAMES + " frames): " + out.graph);
  assert.equal(out.renderer, "", "renderer (" + FRAMES + " frames): " + out.renderer);
  r.h.renderer.dispose();
});

// Reflections, clouds and bloom each draw an extra pass that saves a switch or sets uniforms every frame.
function fullBundle(spectrum) {
  const b = seaBundle(spectrum, { reflections: { mode: "ssr+planar", resolution: 0.5, strength: 1 } });
  b.environment.sky = { mode: "physical", clouds: { coverage: 0.6 } };
  b.postEffects = [{ kind: "bloom", threshold: 0.7, intensity: 0.6 }, { kind: "atmosphere", haze: true, rays: true }];
  return b;
}

test("steady frames of the Gerstner ocean, the full-effects ocean and a plain scene make no synchronous GL query", () => {
  for (const [label, bundle] of [["gerstner ocean", seaBundle(null)], ["full-effects ocean", fullBundle(null)], ["full-effects spectral ocean", fullBundle({ windSpeed: 9 })], ["points scene", makePointsBundle({ id: "p", count: 1, positions: new Float32Array([0, 0, 0]), color: "#ffffff" })]]) {
    const r = rig();
    const out = steady(r, bundle);
    if (process.env.GL_QUERY_REPORT) console.log(label + ":", JSON.stringify(out), [...new Set(r.gl.ops.map((o) => o[0]))].join(","));
    assert.equal(out.renderer, "", label + " (" + FRAMES + " frames): " + out.renderer);
    r.h.renderer.dispose();
  }
});

test("the graph's glState still saves and restores the state it was given, with no query", () => {
  const r = rig(), G = r.context.window.__gosx_elio_graph, gl = new RecordingGL(), c = counter(gl);
  gl.viewport(1, 2, 3, 4); gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  const prog = gl.createProgram(); gl.useProgram(prog); gl.activeTexture(gl.TEXTURE0 + 3); gl.disable(gl.CULL_FACE); gl.enable(gl.BLEND);
  G.glState.save(gl); c.reset();
  const saved = G.glState.save(gl);
  gl.useProgram(null); gl.viewport(0, 0, 9, 9); gl.activeTexture(gl.TEXTURE0); gl.enable(gl.CULL_FACE); gl.disable(gl.BLEND); gl.colorMask(false, true, false, true); gl.depthMask(false);
  G.glState.restore(gl, saved);
  assert.equal(c.summary(), "", "no query");
  assert.equal(gl.currentProgram, prog); assert.deepEqual(gl.viewportRect, [1, 2, 3, 4]); assert.equal(gl.unit, 3);
  assert.equal(gl.enabled.has(gl.CULL_FACE), false); assert.equal(gl.enabled.has(gl.BLEND), true);
  assert.deepEqual(gl.colorMaskValue, [true, true, true, true]); assert.equal(gl.depthMaskValue, true);
});

// The shadow must agree with the context after any sequence of calls through it.
test("the shadow tracks the context through a long random sequence of state calls", () => {
  const r = rig(), gl = new RecordingGL();
  const shadow = r.context.window.__gosx_gl_shadow(gl), arr = (a) => Array.from(a);
  const fbs = [null, gl.createFramebuffer(), gl.createFramebuffer()], progs = [null, gl.createProgram(), gl.createProgram()], vaos = [null, gl.createVertexArray()];
  const caps = [gl.BLEND, gl.DEPTH_TEST, gl.CULL_FACE, gl.SCISSOR_TEST], targets = [gl.FRAMEBUFFER, gl.DRAW_FRAMEBUFFER, gl.READ_FRAMEBUFFER];
  let seed = 12345;
  const rnd = (n) => { seed = (seed * 1103515245 + 12345) & 0x7fffffff; return seed % n; };
  for (let i = 0; i < 2000; i++) {
    switch (rnd(8)) {
      case 0: gl.bindFramebuffer(targets[rnd(3)], fbs[rnd(3)]); break;
      case 1: gl.viewport(rnd(50), rnd(50), 1 + rnd(900), 1 + rnd(900)); break;
      case 2: gl.useProgram(progs[rnd(3)]); break;
      case 3: gl.bindVertexArray(vaos[rnd(2)]); break;
      case 4: gl.activeTexture(gl.TEXTURE0 + rnd(16)); break;
      case 5: (rnd(2) ? gl.enable : gl.disable).call(gl, caps[rnd(4)]); break;
      case 6: gl.colorMask(!!rnd(2), !!rnd(2), !!rnd(2), !!rnd(2)); break;
      case 7: gl.depthMask(!!rnd(2)); break;
    }
    if (i % 17) continue;
    const s = shadow.snapshot();
    assert.equal(s.draw, gl.getParameter(gl.DRAW_FRAMEBUFFER_BINDING)); assert.equal(s.read, gl.getParameter(gl.READ_FRAMEBUFFER_BINDING));
    assert.deepEqual(arr(s.viewport), arr(gl.getParameter(gl.VIEWPORT))); assert.equal(s.program, gl.getParameter(gl.CURRENT_PROGRAM));
    assert.equal(s.vao, gl.getParameter(gl.VERTEX_ARRAY_BINDING)); assert.equal(s.active, gl.getParameter(gl.ACTIVE_TEXTURE));
    assert.deepEqual([s.blend, s.depth, s.cull, s.scissor], caps.map((c) => gl.isEnabled(c)));
    assert.deepEqual(arr(s.colorMask), arr(gl.getParameter(gl.COLOR_WRITEMASK))); assert.equal(s.depthMask, gl.getParameter(gl.DEPTH_WRITEMASK));
  }
  assert.equal(shadow.framebuffer(), gl.drawFB);
  assert.equal(r.context.window.__gosx_gl_shadow(gl), shadow, "one shadow per context");
});

test("the shadow reads the context afresh once after it is lost and restored", () => {
  const r = rig(), gl = new RecordingGL(), listeners = {};
  gl.canvas = { addEventListener: (type, fn) => { listeners[type] = fn; } };
  const shadow = r.context.window.__gosx_gl_shadow(gl);
  gl.viewport(0, 0, 100, 50); gl.enable(gl.CULL_FACE);
  assert.equal(shadow.isEnabled(gl.CULL_FACE), true);
  gl.contextLost = true; listeners.webglcontextlost();
  gl.contextLost = false; gl.enabled.clear(); gl.viewportRect = [0, 0, 300, 150]; listeners.webglcontextrestored(); // the restored context holds defaults
  const c = counter(gl);
  assert.equal(shadow.isEnabled(gl.CULL_FACE), false);
  assert.deepEqual(Array.from(shadow.snapshot().viewport), [0, 0, 300, 150]);
  shadow.snapshot(); shadow.snapshot();
  assert.ok(c.calls.length > 0 && c.calls.length < 14, "one reload, then none: " + c.calls.length);
  c.reset(); shadow.snapshot(); assert.equal(c.calls.length, 0);
});
