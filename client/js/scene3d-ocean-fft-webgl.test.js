"use strict";
// WebGL2 ocean surface hooks for a spectral ocean: the GLSL patch of the base ocean shaders, the call that runs the
// graph before the ocean draw, and the Gerstner fallbacks. The pass scheduling itself is the generic Elio graph runner
// (elio-graph*.test.js); the ocean glue (ocean-fft.ts) is tested against the recording WebGL2 double, and the renderer hooks against a stub engine.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { execFileSync } = require("node:child_process");
const { createWebGLRendererForPost, makePointsBundle } = require("./runtime-test-harness.js");
const { GLC, RecordingGL, loadSources, floatCaps, runnerContext } = require("./elio-graph-test-support.js");

function glContext() {
  const context = vm.createContext({ Math, Number, Object, Array, Error, console });
  loadSources(context, ["ocean-fft-surface-gl.ts"]);
  return context;
}

function surfaceGLSL(context, vertex, fragment) { return context.window.__gosx_scene3d_ocean_fft_surface_gl.glsl(vertex, fragment); }

test("the GLSL surface patch names its anchors and fails loudly when one disappears", () => {
  const context = glContext();
  assert.throws(() => surfaceGLSL(context, "void main() {}", "void main() {}"), /anchor not found/);
});

// --- the renderer: hook order, surface variant, fallbacks -----------------------------------------------------------

function oceanRecord(extra) {
  return Object.assign({ level: 0, windDirection: 0, waveHeight: 0.8, waveLength: 18, choppiness: 0.6, speed: 1,
    deepColor: "#03141f", shallowColor: "#1f6f78", scatterColor: "#2fa58f", foamColor: "#e9eef0",
    clarity: 4, roughness: 0.06, foam: 0.6, surf: 0.5, extent: 4000, bathymetry: null }, extra || {});
}
// The repository's fake context is minimal; this adds what the ocean pass calls, over the fake's own bookkeeping.
// engine: what the stub frame() returns (null means no spectral graph this frame).
function rendererWithStub(options = {}) {
  const h = createWebGLRendererForPost({ fresh: options.fresh !== false, rejectShaderSources: options.rejectShaderSources });
  const mount = h.env.document.createElement("div"); mount.appendChild(h.canvas);
  const gl = h.canvas.getContext("webgl2");
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = (c) => enabled.has(c); gl.enable = (c) => enabled.add(c); gl.disable = (c) => enabled.delete(c);
  for (const [name, fn] of Object.entries({ createSampler: () => ({}), deleteSampler() {}, samplerParameteri() {}, bindSampler() {}, uniform4fv() {}, uniform1i() {}, blendFuncSeparate() {}, uniformMatrix4fv() {} })) if (!gl[name]) gl[name] = fn;
  const calls = { frame: [], bind: [], fail: [], dispose: 0 };
  const engine = options.engine === null ? null : { bind: (g, program) => calls.bind.push(program), fail: (reason) => calls.fail.push(reason), dispose: () => { calls.dispose++; } };
  const window = h.env.context.window || h.env.context;
  h.env.context.window = window;
  loadSources(h.env.context, ["ocean-fft-surface-gl.ts"]);
  window.__gosx_scene3d_ocean_fft_gl_api = { run: (resources, g, host) => {
    calls.frame.push(host);
    if (engine) { resources.fft = engine; }
    return engine;
  } };
  return { h, gl, mount, calls, engine };
}
function sceneBundle(spectrum) {
  const b = makePointsBundle(null);
  b.points = [];
  b.environment.sky = { mode: "physical" };
  b.environment.ocean = oceanRecord(spectrum ? { spectrum } : {});
  b.camera.y = 30; b.camera.z = 0;
  return b;
}
const oceanSources = (gl) => { const p = gl.programMatching("u_ocean[35]"); return p ? p.attached.map((s) => String(s.source || "")) : null; };
const render = (h, bundle) => h.renderer.render(bundle, { width: 320, height: 180 });

test("a spectral ocean asks the graph for a frame first, then draws the patched surface and binds the graph's textures", () => {
  const { h, gl, mount, calls, engine } = rendererWithStub();
  render(h, sceneBundle({ windSpeed: 12 }));
  assert.equal(calls.frame.length, 1);
  const host = calls.frame[0];
  assert.deepEqual([typeof host.caps, host.ocean.spectrum.windSpeed, Boolean(host.camera), Boolean(host.textureCache)], ["function", 12, true, true]);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface+spectrum");
  const all = oceanSources(gl).join("\n");
  assert.match(all, /textureLod\(u_spDisp0/);
  assert.match(all, /uniform vec4 u_sp\[10\]/);
  assert.doesNotMatch(all, /for \(int i = 0; i < 6; i\+\+\) \{\n    if \(i >= waves\)/, "the Gerstner loop is gone");
  assert.equal(calls.bind.length, 1, "the ocean program gets the cascade samplers");
  assert.equal(calls.bind[0], gl.programMatching("u_ocean[35]"));
  assert.ok(engine);
  h.renderer.dispose();
  assert.equal(calls.dispose, 1, "renderer dispose (also the context-loss path) disposes the graph");
});

test("when the graph gives nothing (no float targets, a failure), the Gerstner ocean draws and nothing is bound", () => {
  const { h, gl, mount, calls } = rendererWithStub({ engine: null });
  render(h, sceneBundle({ windSpeed: 12 }));
  assert.equal(calls.frame.length, 1);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  assert.doesNotMatch(oceanSources(gl).join("\n"), /u_spDisp0/);
  assert.equal(calls.bind.length, 0);
  h.renderer.dispose();
});

test("scenes without a spectrum never call the graph", () => {
  const { h, mount, calls } = rendererWithStub();
  render(h, sceneBundle(null));
  assert.equal(calls.frame.length, 0);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  h.renderer.dispose();
});

test("a surface patch that cannot be applied reports 'surface-patch' and draws Gerstner waves", () => {
  const { h, mount, calls } = rendererWithStub();
  h.env.context.window.__gosx_scene3d_ocean_fft_surface_gl.glsl = () => { throw new Error("anchor not found: injected"); };
  render(h, sceneBundle({}));
  assert.deepEqual(calls.fail, ["surface-patch"]);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  h.renderer.dispose();
});

test("a patched shader the driver rejects reports 'surface-compile' and the plain shaders are built instead", () => {
  const { h, gl, mount, calls } = rendererWithStub({ rejectShaderSources: ["u_spDisp0"] });
  render(h, sceneBundle({}));
  assert.deepEqual(calls.fail, ["surface-compile"]);
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface");
  assert.ok(oceanSources(gl), "the plain ocean program exists");
  assert.doesNotMatch(oceanSources(gl).join("\n"), /u_spDisp0/);
  h.renderer.dispose();
});

function glslangPath() {
  try { execFileSync("glslangValidator", ["--version"], { stdio: "ignore" }); return "glslangValidator"; } catch (_e) { return ""; }
}
const glslang = glslangPath();
function validateGLSL(code, stage, name) {
  const dir = fs.mkdtempSync(path.join(require("node:os").tmpdir(), "ocean-fft-gl-"));
  const file = path.join(dir, name + "." + stage);
  fs.writeFileSync(file, code);
  try { execFileSync(glslang, [file], { stdio: "pipe" }); } catch (e) { throw new Error(name + ": " + String(e.stdout)); } finally { fs.rmSync(dir, { recursive: true, force: true }); }
}

test("both patched ocean shaders pass glslangValidator as GLSL ES 3.00", { skip: glslang ? false : "glslangValidator is not available" }, () => {
  const { h, gl } = rendererWithStub();
  render(h, sceneBundle({}));
  const sources = oceanSources(gl);
  const vertex = sources.find((s) => /gl_VertexID \/ 6/.test(s)), fragment = sources.find((s) => /fragColor/.test(s));
  assert.ok(vertex && fragment);
  validateGLSL(vertex, "vert", "ocean-fft-vertex");
  validateGLSL(fragment, "frag", "ocean-fft-fragment");
  h.renderer.dispose();
});

test("the shipped (packed) WebGL bundle still accepts the spectral surface patch", () => {
  const { h, mount, calls } = rendererWithStub({ fresh: false });
  render(h, sceneBundle({ windSpeed: 9 }));
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface+spectrum");
  assert.deepEqual(calls.fail, []);
  h.renderer.dispose();
});

// --- the ocean glue on the recording WebGL2 double -----------------------------------------------------------------

const CHUNK = ["ocean-fft-surface.ts", "ocean-fft-surface-gl.ts", "ocean-fft.ts"];
function glue(glOptions) {
  const context = runnerContext();
  loadSources(context, CHUNK);
  const gl = new RecordingGL(glOptions), api = context.window.__gosx_scene3d_ocean_fft_gl_api, ocean = context.window.__gosx_scene3d_ocean_fft_api;
  const mount = { attrs: {}, setAttribute(k, v) { this.attrs[k] = v; } }, textureCache = { _sceneTextureEpoch: 0 };
  const resources = {};
  const frame = (env, extra) => api.run(resources, gl, Object.assign({ ocean: env, tier: "high", time: 0, camera: { x: 0, y: 30, z: 0 }, textureCache, mount, caps: floatCaps }, extra));
  return { context, gl, api, ocean, mount, textureCache, resources, frame };
}
const spectral = (extra) => Object.assign({ level: 0, spectrum: { windSpeed: 9 }, graphParams: { windSpeed: 9, foamThreshold: 0.3, driftX: 2, driftZ: 0 }, graphTween: 0 }, extra);
const drawsOf = (gl) => gl.draws.filter((d) => d.kind === "draw");

test("glue (WebGL2): without float render targets nothing is created and the reason is on the mount", () => {
  const g = glue({ extensions: [] });
  assert.equal(g.frame(spectral()), null);
  assert.equal(g.resources.fftDisabled, "EXT_color_buffer_float");
  assert.equal(g.mount.attrs["data-gosx-scene3d-ocean-fft-fallback"], "EXT_color_buffer_float");
  for (const kind of Object.keys(g.gl.created)) assert.equal(g.gl.created[kind].length, 0);
  assert.equal(g.frame(spectral()), null, "remembered: no retry every frame");
});

test("glue (WebGL2): the graph runs on the first frame, graphParams reach it, and bind() hands the surface its textures and uniform", () => {
  const g = glue();
  const engine = g.frame(spectral(), { time: 100 });
  assert.ok(engine, "engine");
  assert.ok(drawsOf(g.gl).length > 60, "the gather passes ran: " + drawsOf(g.gl).length);
  assert.ok(g.textureCache._sceneTextureEpoch >= 1);
  for (let i = 1; i <= 60; i++) g.frame(spectral(), { time: 100 + i / 60 });
  const water = g.ocean.water();
  assert.ok(Math.abs(water.x - 2) < 1e-6, "driftX 2 m/s for one second: " + water.x);
  const program = g.gl.createProgram();
  program.shaders.push({ source: "uniform vec4 u_sp[10]; uniform sampler2D u_spDisp0, u_spDisp1, u_spDisp2, u_spDisp3, u_spSlope0, u_spSlope1, u_spSlope2, u_spSlope3, u_spFoam;" });
  engine.bind(g.gl, program);
  const d = Array.from(program.uniforms.get("u_sp[0]"));
  assert.equal(d.length, 40);
  assert.deepEqual([d[3], d[7], d[11], d[15]], [1, 1, 1, 1]);
  assert.equal(Math.fround(d[18]), Math.fround(0.3), "foam threshold from graphParams");
  assert.ok(Math.abs(d[24] - 2) < 1e-4 && d[25] === 0, "cascade 0's wrapped water offset");
  assert.deepEqual([0, 1, 2, 3].map((i) => program.uniforms.get("u_spDisp" + i)), [4, 5, 6, 7]);
  assert.deepEqual([0, 1, 2, 3].map((i) => program.uniforms.get("u_spSlope" + i)), [8, 9, 10, 11]);
  assert.equal(program.uniforms.get("u_spFoam"), 12);
  engine.dispose();
  for (const kind of Object.keys(g.gl.created)) assert.equal(g.gl.live(kind).filter((o) => o !== program).length, kind === "program" ? 0 : 0, kind + " leaked");
});

test("glue (WebGL2): the low tier builds three cascades; a tier change rebuilds the graph; a lost context does no work", () => {
  const g = glue();
  const low = g.frame(spectral(), { tier: "low" });
  const program = g.gl.createProgram();
  program.shaders.push({ source: "uniform vec4 u_sp[10]; uniform sampler2D u_spDisp0, u_spDisp1, u_spDisp2, u_spDisp3, u_spSlope0, u_spSlope1, u_spSlope2, u_spSlope3, u_spFoam;" });
  low.bind(g.gl, program);
  assert.deepEqual([3, 7, 11, 15].map((i) => program.uniforms.get("u_sp[0]")[i]), [1, 1, 1, 0]);
  const high = g.frame(spectral(), { tier: "high" });
  assert.notEqual(high, low);
  assert.ok(low.disposed, "the old graph is released");
  g.gl.contextLost = true;
  const draws = g.gl.draws.length;
  assert.equal(g.frame(spectral(), { tier: "high" }), null);
  assert.equal(g.gl.draws.length, draws);
});

test("glue (WebGL2): a surface that cannot be built disables the graph with the reason", () => {
  const g = glue();
  const engine = g.frame(spectral());
  engine.fail("surface-compile");
  assert.equal(g.frame(spectral()), null);
  assert.equal(g.resources.fftDisabled, "surface-compile");
  assert.equal(g.mount.attrs["data-gosx-scene3d-ocean-fft-fallback"], "surface-compile");
  assert.equal(g.resources.fft, null);
  for (const kind of Object.keys(g.gl.created)) assert.equal(g.gl.live(kind).length, 0, kind + " leaked");
  assert.equal(g.ocean.water(), null);
});

test("glue (WebGL2): there is no probe on WebGL2; the vessel's points are recorded but never answered, and graphParams the graph lacks are ignored", () => {
  const g = glue();
  const env = spectral({ graphParams: { windSpeed: 9, probeOriginX: 10, probeOriginZ: 5, probeCount: 4 } });
  const engine = g.frame(env, { time: 1 });
  assert.ok(engine, "the WebGL2 graph has no probe parameters; feeding them must not disable it");
  g.ocean.probe.begin();
  assert.equal(g.ocean.probe.sample(1, 1, 0, 0), null);
  assert.ok(g.frame(env, { time: 1.02 }));
  assert.equal(g.resources.fft.runner.failed, null);
  assert.ok(g.gl.ops.filter((o) => o[0] === "readPixels").every((o) => o[5]), "the only readPixels calls are the foam relayout's GPU-side copies into a pixel buffer: no readback to the CPU on WebGL2");
});

// The lazy WebGL chunk is minified with property mangling that the monolith and the fresh sources do not get. The two chunks share
// the glue's API by property name, so the shipped lazy chunks themselves must work together.
test("the shipped lazy WebGL chunk and the shipped ocean chunk agree on the glue's API names", () => {
  const { createContext, runScript } = require("./runtime-test-harness.js");
  const read = (name) => fs.readFileSync(path.join(__dirname, name), "utf8");
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = RecordingGL;
  for (const name of ["bootstrap-runtime.js", "bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-compute.js", "bootstrap-feature-scene3d-webgl.js", "bootstrap-feature-scene3d-ocean-fft.js"]) runScript(read(name), env.context, name);
  const glue = env.context.__gosx_scene3d_ocean_fft_gl_api;
  assert.deepEqual(Object.keys(glue).sort(), ["fail", "run"]);
  const webgl = read("bootstrap-feature-scene3d-webgl.js");
  assert.ok(!/__gosx_scene3d_ocean_fft_gl_api[^;]{0,200}\.frame\(/.test(webgl), "the WebGL chunk must not call a method its minifier renames");
  assert.match(webgl, /__gosx_scene3d_ocean_fft_gl_api[,;][^;]{0,160}\.run\(/, "it calls run");
  for (const key of ["run", "fail"]) assert.ok(new RegExp("[{,]" + key + ":").test(read("bootstrap-feature-scene3d-ocean-fft.js")), key + " is published unmangled");
});

test("the shipped lazy chunks render a spectral ocean on WebGL2: the packed GLSL still takes the surface patch and the graph runs", () => {
  const { createContext, runScript, FakeWebGLContext } = require("./runtime-test-harness.js");
  const read = (name) => fs.readFileSync(path.join(__dirname, name), "utf8");
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  for (const name of ["bootstrap-runtime.js", "bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-compute.js", "bootstrap-feature-scene3d-webgl.js", "bootstrap-feature-scene3d-ocean-fft.js"]) runScript(read(name), env.context, name);
  const api = env.context.__gosx_scene3d_api;
  const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
  const canvas = env.document.createElement("canvas"); canvas.width = 320; canvas.height = 180;
  const mount = env.document.createElement("div"); mount.appendChild(canvas);
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  const gl = canvas.getContext("webgl2");
  const extensions = new Set(["EXT_color_buffer_float"]);
  const getExtension = gl.getExtension.bind(gl), getParameter = gl.getParameter.bind(gl);
  gl.getExtension = (name) => extensions.has(name) ? {} : getExtension(name);
  gl.getParameter = (p) => ({ [GLC.MAX_VERTEX_TEXTURE_IMAGE_UNITS]: 16, [GLC.MAX_TEXTURE_IMAGE_UNITS]: 16, [GLC.MAX_DRAW_BUFFERS]: 8, [GLC.UNIFORM_BUFFER_OFFSET_ALIGNMENT]: 256,
    [GLC.COLOR_WRITEMASK]: [true, true, true, true], [GLC.DEPTH_WRITEMASK]: true })[p] ?? getParameter(p);
  const enabled = new Set([gl.CULL_FACE]);
  gl.isEnabled = (c) => enabled.has(c); gl.enable = (c) => enabled.add(c); gl.disable = (c) => enabled.delete(c);
  for (const [name, fn] of Object.entries({ createSampler: () => ({}), deleteSampler() {}, samplerParameteri() {}, bindSampler() {}, uniform4fv() {}, uniform1i() {}, uniform1ui() {}, blendFuncSeparate() {}, uniformMatrix4fv() {},
    getUniformBlockIndex: () => 0, uniformBlockBinding() {}, bindBufferBase() {}, bindBufferRange() {}, colorMask() {}, readPixels() {}, texSubImage2D() {}, bufferSubData() {}, texStorage2D() {}, framebufferTexture2D() {}, generateMipmap() {},
    getUniformLocation: (p, n) => ({ n }) })) if (!gl[name]) gl[name] = fn;
  Object.assign(gl, Object.fromEntries(Object.entries(GLC).filter(([k]) => gl[k] === undefined)));
  const bundle = sceneBundle({ windSpeed: 9 });
  bundle.environment.ocean.graphParams = { windSpeed: 9 };
  renderer.render(bundle, { width: 320, height: 180 });
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean-fft-fallback"), null, "no fallback reason: " + mount.getAttribute("data-gosx-scene3d-ocean-fft-fallback"));
  assert.equal(mount.getAttribute("data-gosx-scene3d-ocean"), "surface+spectrum");
  const draws = gl.ops.filter((op) => op[0] === "drawArrays" && op[3] === 3).length;
  assert.ok(draws > 60, "the gather passes ran: " + draws);
  renderer.dispose();
});

test("glue (WebGL2): a graph that fails after its first frame returns null, releases its GL objects, and stays off", () => {
  const warn = console.warn; console.warn = () => {};
  try {
    const g = glue();
    assert.ok(g.frame(spectral(), { time: 1 }));
    g.gl.drawArrays = () => { throw new Error("device error"); };
    assert.equal(g.frame(spectral(), { time: 2 }), null);
    assert.equal(g.resources.fftDisabled, "error");
    assert.equal(g.mount.attrs["data-gosx-scene3d-ocean-fft-fallback"], "error");
    assert.equal(g.resources.fft, null);
    for (const kind of Object.keys(g.gl.created)) assert.equal(g.gl.live(kind).length, 0, kind + " leaked");
    assert.equal(g.ocean.water(), null);
    assert.equal(g.frame(spectral(), { time: 3 }), null, "it does not retry every frame");
  } finally { console.warn = warn; }
});
