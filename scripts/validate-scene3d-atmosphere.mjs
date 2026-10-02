// Validate actual assembled shaders and MSAA depth variants without a browser.
import { createRequire } from "node:module";
import { mkdirSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import os from "node:os";
const require = createRequire(import.meta.url);
const { createBoardWebGPUHarness, createWebGLRendererForPost, makePointsBundle } = require("../client/js/runtime-test-harness.js");
const directory = "/tmp/gosx-atmosphere-shaders";
mkdirSync(directory, { recursive: true });
const seen = new Set();
let glsl = 0, wgsl = 0;
function validate(code, stage) {
  const key = createHash("sha256").update(stage + code).digest("hex");
  if (seen.has(key)) return;
  seen.add(key);
  const path = `${directory}/${key}.${stage}`;
  writeFileSync(path, code);
  if (stage === "wgsl") {
    execFileSync(`${os.homedir()}/.cargo/bin/naga`, [path], { stdio: "pipe" });
    console.log(`~/.cargo/bin/naga ${path}`); wgsl++;
  } else {
    execFileSync("/usr/bin/glslangValidator", ["-S", stage, path], { stdio: "pipe" });
    console.log(`/usr/bin/glslangValidator -S ${stage} ${path}`); glsl++;
  }
}
function bundle(config) {
  const out = makePointsBundle(null); out.points = [];
  out.environment.sky = { mode: "physical", ...(config.clouds ? { clouds: { coverage: 0.45 } } : {}) };
  out.environment.ocean = config.reflections ? { reflections: { mode: "ssr+planar" } } : {};
  if (config.haze) out.environment.haze = { density: 0.002, heightFalloff: 0.04, sunScatter: 0.25 };
  out.postEffects = [{ kind: "toneMapping", mode: "agx", exposure: 0.7 }, { kind: "grain", intensity: 0.015 }];
  if (config.rays) out.postEffects.unshift({ kind: "godRays", intensity: 0.15 });
  out.msaaSamples = config.samples || 1; return out;
}
function supportGL(gl) {
  gl.isEnabled = () => false; gl.createSampler = () => ({});
  gl.deleteSampler = gl.samplerParameteri = gl.bindSampler = () => {};
  gl.deleteRenderbuffer ||= () => {}; gl.uniform4fv ||= () => {}; gl.blendFuncSeparate ||= () => {};
  const get = gl.getParameter.bind(gl); let bound = null; gl.FRAMEBUFFER_BINDING = 0x8ca6;
  const bind = gl.bindFramebuffer.bind(gl);
  gl.bindFramebuffer = (t, f) => { if (t === gl.FRAMEBUFFER) bound = f; bind(t, f); };
  gl.getParameter = p => p === gl.FRAMEBUFFER_BINDING ? bound : get(p);
}
const configs = [
  ...[false, true].flatMap(clouds => [false, true].map(reflections => ({ clouds, reflections, haze: true, rays: true }))),
  { clouds: true, reflections: true, haze: true, rays: true, samples: 4 },
  { haze: true }, { rays: true },
];
for (const config of configs) {
  const b = bundle(config);
  const h = createWebGLRendererForPost({ fresh: true }), gl = h.canvas.getContext("webgl2"), shaders = [];
  supportGL(gl);
  const src = gl.shaderSource.bind(gl);
  gl.shaderSource = (s, code) => { shaders.push([s.type, code]); src(s, code); };
  h.renderer.render(b, { width: 64, height: 64 });
  for (const [type, code] of shaders) validate(code, type === gl.VERTEX_SHADER ? "vert" : "frag");
  h.renderer.dispose();
  const g = await createBoardWebGPUHarness({ fresh: true }), create = g.fake.device.createRenderPipeline.bind(g.fake.device);
  g.fake.device.createRenderPipeline = d => Object.assign(create(d), { getBindGroupLayout: () => g.fake.device.createBindGroupLayout({ entries: [] }) });
  g.canvas.width = g.canvas.height = 64; g.renderer.render(b, { width: 64, height: 64 });
  for (const module of g.fake.state.shaderModules) {
    if (["gosx-ocean", "gosx-reflection-capture", "gosx-clouds"].includes(module.label) || String(module.label).startsWith("post-atmosphere:")) validate(module.code, "wgsl");
  }
  g.renderer.dispose();
}
console.log(`Validated ${glsl} unique GLSL stages and ${wgsl} WGSL modules, including MSAA 1/4 and every ocean feature variant.`);
