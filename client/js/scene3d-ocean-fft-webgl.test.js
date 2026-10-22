"use strict";
// WebGL2 ocean surface hooks for a spectral ocean: the GLSL patch of the base ocean shaders, the call that runs the
// graph before the ocean draw, and the Gerstner fallbacks. The pass scheduling itself is the generic Elio graph runner
// (elio-graph*.test.js); here a stub stands in for the engine the ocean adapter will supply.
const test = require("node:test"), assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const { execFileSync } = require("node:child_process");
const { createWebGLRendererForPost, makePointsBundle } = require("./runtime-test-harness.js");
const { GLC, loadSources } = require("./elio-graph-test-support.js");

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
  window.__gosx_scene3d_ocean_fft_gl_api = { frame: (resources, g, host) => {
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
  assert.match(all, /uniform vec4 u_sp\[6\]/);
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
