"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const H = require("./runtime-test-harness.js");

const profiles = {
  "desktop-full": { width: 1440, height: 900, dpr: 2, coarse: false },
  "phone-balanced": { width: 390, height: 844, dpr: 2.75, coarse: true },
};
const hero = JSON.parse(fs.readFileSync(path.join(__dirname, "testdata/scene3d-work/hero.json")));
const box = { id: "box", kind: "box", width: 1, height: 1, depth: 1, color: "#d4af37", materialKind: "standard", roughness: 0.3, castShadow: true, receiveShadow: true };
const base = { width: 1440, height: 900, responsive: true, fillHeight: true, background: "#000000", camera: { z: 8, fov: 55 } };
const scenes = {
  hero,
  post: { ...base, scene: { objects: [box], lights: [{ id: "sun", kind: "directional", directionX: 0.3, directionY: -1, directionZ: -0.5, intensity: 1.2, castShadow: true }], postEffects: [{ kind: "bloom", threshold: 0.8, strength: 0.6 }, { kind: "toneMapping", mode: "aces" }] } },
  dense: { ...base, scene: {
    materials: [{ id: "named-gold", name: "named-gold", kind: "standard", color: "#d4af37", roughness: 0.3, metalness: 0.8 }],
    objects: Array.from({ length: 200 }, (_, i) => ({ ...box, kind: "pyramid", id: `mesh-${i}`, x: (i % 20 - 10) * 0.2, y: (Math.floor(i / 20) - 5) * 0.2, z: -i * 0.01,
      ...(i % 3 === 0 ? { material: "named-gold" } : { color: i % 3 === 1 ? "#eeeeee" : "#333333", roughness: i % 3 === 1 ? 0.1 : 0.8 }) })),
  } },
};

// Compile once, then execute the real bootstrap and feature factories per mount.
const scripts = new Map();
function runBundle(name, context) {
  if (!scripts.has(name)) {
    const source = ({ runtime: H.bootstrapRuntimeSource, engines: H.bootstrapFeatureEnginesSource, "scene3d-command": H.bootstrapFeatureScene3DCommandSource, scene3d: H.bootstrapFeatureScene3DSource, "scene3d-compute": H.bootstrapFeatureScene3DComputeSource, "scene3d-webgl": H.bootstrapFeatureScene3DWebGLSource, "scene3d-webgpu": H.bootstrapFeatureScene3DWebGPUSource })[name];
    // Use ordinary JS globals, as a browser does. Contextified VM globals make
    // numeric work tens of times slower and would dominate this counting test.
    const names = ["window", "globalThis", "document", "console", "navigator", "performance", "Date", "Math", "JSON", "requestAnimationFrame", "cancelAnimationFrame", "setTimeout", "clearTimeout", "setInterval", "clearInterval", "fetch", "Image", "ResizeObserver", "IntersectionObserver", "MutationObserver", "getComputedStyle", "matchMedia", "WebGL2RenderingContext", "GPUBufferUsage", "GPUTextureUsage", "GPUShaderStage", "CustomEvent", "Event", "innerWidth", "innerHeight", "devicePixelRatio", "screen"];
    scripts.set(name, { names, run: new Function(...names, source) });
  }
  const { names, run } = scripts.get(name);
  run(...names.map(key => key === "globalThis" ? context : context[key]));
}

const metricNames = ["draws", "programBinds", "pipelineBinds", "framebufferBinds", "renderPasses", "bufferUploadBytes", "textureUploadBytes", "gpuBuffersCreated", "bindGroupsCreated", "getParameter", "getError", "readPixels", "domAttributeWrites"];
function counters() { return Object.fromEntries(metricNames.map(name => [name, 0])); }
function delta(before, after) { return Object.fromEntries(metricNames.map(name => [name, after[name] - before[name]])); }
function wrap(object, name, record) {
  const original = object[name];
  object[name] = function(...args) {
    record(...args);
    return original ? original.apply(this, args) : undefined;
  };
}
function uploadBytes(data, offset = 0, length) {
  if (data == null) return 0;
  if (typeof data === "number") return 0; // allocation without client data
  const elementSize = data.BYTES_PER_ELEMENT || 1;
  assert.ok(Number.isFinite(data.byteLength), "uploads must have a known byte length");
  return length === undefined ? data.byteLength - offset * elementSize : length * elementSize;
}
function texturePixels(size) {
  const [w, h = 1, d = 1] = Array.isArray(size) ? size : [size.width, size.height, size.depthOrArrayLayers];
  return w * h * d;
}

function instrumentGL(gl, work) {
  const textures = new Map(), renderbuffers = new Map(), framebuffers = new Map(), boundTextures = new Map();
  let framebuffer = null, renderbuffer = null;
  for (const name of ["drawArrays", "drawElements", "drawArraysInstanced", "drawElementsInstanced"]) wrap(gl, name, () => work.draws++);
  wrap(gl, "useProgram", () => work.programBinds++);
  for (const name of ["getParameter", "getError", "readPixels"]) wrap(gl, name, () => work[name]++);
  wrap(gl, "createBuffer", () => work.gpuBuffersCreated++);
  wrap(gl, "bufferData", (_target, data, _usage, offset, length) => { work.bufferUploadBytes += uploadBytes(data, offset, length); });
  wrap(gl, "bufferSubData", (_target, _dst, data, offset, length) => { work.bufferUploadBytes += uploadBytes(data, offset, length); });
  wrap(gl, "bindTexture", (target, texture) => boundTextures.set(target, texture));
  for (const name of ["texImage2D", "texSubImage2D", "texImage3D", "texSubImage3D"]) wrap(gl, name, (...args) => {
    const image = name.startsWith("texImage"), three = name.endsWith("3D");
    const dataIndex = image ? (three ? 9 : args.length >= 9 ? 8 : 5) : (three ? 10 : args.length >= 9 ? 8 : 6);
    const data = args[dataIndex];
    work.textureUploadBytes += data && data.byteLength !== undefined ? uploadBytes(data, args[dataIndex + 1]) : data && typeof data === "object" ? (data.width || data.naturalWidth) * (data.height || data.naturalHeight) * 4 : 0;
    if (image) {
      const w = args.length >= 9 ? args[3] : data.width || data.naturalWidth;
      const h = args.length >= 9 ? args[4] : data.height || data.naturalHeight;
      const texture = boundTextures.get(args[0]);
      const previous = textures.get(texture) || 0;
      textures.set(texture, args[1] === 0 ? w * h * (three ? args[5] : 1) : previous);
    }
  });
  for (const name of ["compressedTexImage2D", "compressedTexImage3D", "compressedTexSubImage2D", "compressedTexSubImage3D"]) wrap(gl, name, (...args) => {
    const index = args.findIndex(arg => arg && typeof arg.byteLength === "number");
    if (index >= 0) work.textureUploadBytes += uploadBytes(args[index], args[index + 1], args[index + 2]);
    if (name.startsWith("compressedTexImage") && args[1] === 0) textures.set(boundTextures.get(args[0]), args[3] * args[4] * (name.endsWith("3D") ? args[5] : 1));
  });
  wrap(gl, "texStorage2D", (target, _levels, _format, w, h) => textures.set(boundTextures.get(target), w * h));
  wrap(gl, "texStorage3D", (target, _levels, _format, w, h, d) => textures.set(boundTextures.get(target), w * h * d));
  wrap(gl, "deleteTexture", texture => textures.delete(texture));
  wrap(gl, "bindRenderbuffer", (_target, buffer) => { renderbuffer = buffer; });
  wrap(gl, "renderbufferStorage", (_target, _format, w, h) => renderbuffers.set(renderbuffer, w * h));
  wrap(gl, "renderbufferStorageMultisample", (_target, samples, _format, w, h) => renderbuffers.set(renderbuffer, w * h * samples));
  wrap(gl, "deleteRenderbuffer", buffer => renderbuffers.delete(buffer));
  wrap(gl, "bindFramebuffer", (_target, fb) => { work.framebufferBinds++; framebuffer = fb; });
  function attach(attachment, resource) {
    if (!framebuffer) return;
    if (!framebuffers.has(framebuffer)) framebuffers.set(framebuffer, new Map());
    framebuffers.get(framebuffer).set(attachment, resource);
  }
  wrap(gl, "framebufferTexture2D", (_target, attachment, _textarget, texture) => attach(attachment, texture));
  wrap(gl, "framebufferTextureLayer", (_target, attachment, texture) => attach(attachment, texture));
  wrap(gl, "framebufferRenderbuffer", (_target, attachment, _rbtarget, rb) => attach(attachment, rb));
  wrap(gl, "deleteFramebuffer", fb => framebuffers.delete(fb));
  return () => {
    const post = new Set(), shadow = new Set();
    for (const attachments of framebuffers.values()) {
      const set = attachments.has(gl.COLOR_ATTACHMENT0) ? post : shadow;
      for (const resource of attachments.values()) set.add(resource);
    }
    const sum = resources => [...resources].reduce((total, r) => total + (textures.get(r) || renderbuffers.get(r) || 0), 0);
    return { postTargetPixels: sum(post), shadowMapPixels: sum(shadow) };
  };
}

function instrumentGPU(fake, work) {
  const { device } = fake;
  const post = new Set(), shadow = new Set();
  const originalTexture = device.createTexture;
  device.createTexture = function(desc) {
    const texture = originalTexture.call(this, desc);
    const originalView = texture.createView;
    texture.createView = function(...args) { return Object.assign(originalView.apply(this, args), { texture }); };
    return texture;
  };
  wrap(device, "createBuffer", desc => {
    work.gpuBuffersCreated++;
    if (desc.mappedAtCreation) work.bufferUploadBytes += desc.size;
  });
  wrap(device, "createBindGroup", () => work.bindGroupsCreated++);
  wrap(device.queue, "writeBuffer", (_buffer, _dst, data, offset, size) => { work.bufferUploadBytes += uploadBytes(data, offset, size); });
  wrap(device.queue, "writeTexture", (_dst, data) => { work.textureUploadBytes += uploadBytes(data); });
  wrap(device.queue, "copyExternalImageToTexture", (_src, _dst, size) => { work.textureUploadBytes += texturePixels(size) * 4; });
  const originalEncoder = device.createCommandEncoder;
  device.createCommandEncoder = function(...args) {
    const encoder = originalEncoder.apply(this, args);
    for (const kind of ["Render", "Compute"]) {
      const originalPass = encoder[`begin${kind}Pass`];
      encoder[`begin${kind}Pass`] = function(desc) {
        const pass = originalPass.call(this, desc);
        if (kind === "Render") {
          work.renderPasses++;
          const colors = desc.colorAttachments || [];
          // MSAA attachments resolving to the canvas belong to the backing store.
          const present = colors.some(a => (a.resolveTarget || a.view).__kind === "canvasTextureView");
          const resources = [...colors.flatMap(a => [a.view, a.resolveTarget]), desc.depthStencilAttachment?.view].filter(v => v?.texture).map(v => v.texture);
          if (!colors.length) resources.forEach(t => shadow.add(t));
          else if (!present) resources.forEach(t => post.add(t));
          for (const name of ["draw", "drawIndexed", "drawIndirect", "drawIndexedIndirect"]) wrap(pass, name, () => work.draws++);
        }
        wrap(pass, "setPipeline", () => work.pipelineBinds++);
        return pass;
      };
    }
    return encoder;
  };
  // The fake's async path must retain the same descriptor and counters.
  device.createRenderPipelineAsync = desc => Promise.resolve(device.createRenderPipeline(desc));
  return () => {
    const pixels = set => [...set].filter(t => !t.destroyed).reduce((sum, t) => sum + texturePixels(t.desc.size) * (t.desc.sampleCount || 1), 0);
    const samples = Math.max(1, ...fake.state.renderPipelines.map(p => p.desc.multisample?.count || 1));
    return { samples, postTargetPixels: pixels(post), shadowMapPixels: pixels(shadow) };
  };
}

async function mountScene(props, profileName, backend, options = {}) {
  const profile = profiles[profileName];
  let now = 0, renders = 0, seed = 1;
  const work = counters();
  const mount = new H.FakeElement("div", null);
  mount.id = "scene-work"; mount.width = profile.width; mount.height = profile.height;
  mount.clientWidth = profile.width; mount.clientHeight = profile.height;
  const gl = new H.FakeWebGLContext();
  const glTargets = instrumentGL(gl, work);
  const fake = H.makeFakeGPUDevice();
  const gpuTargets = instrumentGPU(fake, work);
  const env = H.createContext({
    elements: [mount], disableCanvas2D: true, enableWebGPU: backend === "webgpu", webgpuDevice: fake.device,
    createWebGL2Context: () => gl, devicePixelRatio: profile.dpr,
    visualViewportWidth: profile.width, visualViewportHeight: profile.height,
    maxTouchPoints: profile.coarse ? 5 : 0, userAgent: profile.coarse ? "Android Mobile" : "Desktop",
    matchMedia: { "(pointer: coarse)": profile.coarse, "(pointer: fine)": !profile.coarse, "(hover: hover)": !profile.coarse },
    performanceNow: () => now,
    fetchRoutes: { "/gosx/bootstrap-feature-engines.js": { text: H.bootstrapFeatureEnginesSource }, "/gosx/bootstrap-feature-scene3d-command.js": { text: H.bootstrapFeatureScene3DCommandSource } },
    manifest: { engines: [{ id: "work-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id, props: structuredClone(props) }] },
  });
  const c = env.context;
  c.WebGL2RenderingContext = H.FakeWebGLContext;
  Object.assign(c, { innerWidth: profile.width, innerHeight: profile.height, screen: { width: profile.width, height: profile.height },
    GPUBufferUsage: { MAP_READ: 1, MAP_WRITE: 2, COPY_SRC: 4, COPY_DST: 8, INDEX: 16, VERTEX: 32, UNIFORM: 64, STORAGE: 128, INDIRECT: 256, QUERY_RESOLVE: 512 },
    GPUTextureUsage: { COPY_SRC: 1, COPY_DST: 2, TEXTURE_BINDING: 4, STORAGE_BINDING: 8, RENDER_ATTACHMENT: 16 }, GPUShaderStage: { VERTEX: 1, FRAGMENT: 2, COMPUTE: 4 } });
  c.Date = class extends Date { static now() { return now; } };
  c.Math = Object.create(Math);
  c.Math.random = () => ((seed = Math.imul(seed, 1664525) + 1013904223 >>> 0) / 4294967296);
  c.__gosx_scene3d_webgpu_probe = () => ({ adapter: {}, device: fake.device, ready: true });
  const raf = H.installManualRAF(c);
  H.installManualTimers(c);
  const trackDOM = element => {
    for (const name of ["setAttribute", "removeAttribute"]) wrap(element, name, () => work.domAttributeWrites++);
    return element;
  };
  trackDOM(mount);
  const createElement = env.document.createElement.bind(env.document);
  env.document.createElement = (...args) => {
    const element = trackDOM(createElement(...args));
    if (element.tagName === "CANVAS") {
      // A real width:100% canvas keeps its CSS box when its backing size changes.
      element.clientWidth = profile.width; element.clientHeight = profile.height;
      element.getBoundingClientRect = () => ({ left: 0, top: 0, width: profile.width, height: profile.height, right: profile.width, bottom: profile.height });
    }
    return element;
  };
  // Lazy chunks use the same ordinary execution realm as the initial chunks.
  const defaultLoader = env.document.scriptLoader;
  env.document.scriptLoader = function(url, element) {
    const names = { "/gosx/bootstrap-feature-engines.js": "engines", "/gosx/bootstrap-feature-scene3d-command.js": "scene3d-command" };
    const name = names[url];
    if (!name) return defaultLoader(url, element);
    env.fetchCalls.push({ url, init: {} });
    queueMicrotask(() => {
      try { runBundle(name, c); element.onload?.({}); }
      catch (error) { element.onerror?.(error); }
    });
  };
  runBundle("runtime", c);
  runBundle("scene3d", c);
  runBundle("scene3d-compute", c);
  runBundle(`scene3d-${backend}`, c);
  const registry = c.__gosx_scene3d_api.sceneBackendRegistry;
  if (options.directDraw) c.__gosx_scene3d_webgpu_render_bundles = false;
  const entry = registry.list().find(entry => entry.kind === backend);
  assert.ok(entry, "the requested renderer chunk must register its backend");
  let warming = true;
  const create = entry.create;
  entry.create = function(...args) {
    const renderer = create.apply(this, args);
    assert.ok(renderer, "the PBR factory must succeed; legacy fallback invalidates the measurement");
    if (backend === "webgl") assert.equal(renderer.type, "webgl-pbr");
    const render = renderer.render;
    renderer.render = function(...frameArgs) {
      const outcome = render.apply(this, frameArgs);
      if (warming && outcome === "pending") return outcome;
      assert.equal(outcome, "submitted", "each counted frame must submit real renderer work");
      renders++;
      return outcome;
    };
    return renderer;
  };
  await H.flushAsyncWork();
  async function step(ms, force = false) {
    now += ms;
    if (force) mount.__gosxScene3DHandle.updateSceneProps({});
    raf.flush(now);
    await Promise.resolve(); await Promise.resolve();
    // Recording doubles otherwise retain millions of unrelated call records.
    gl.ops.length = 0;
    for (const key of ["renderPasses", "computePasses", "writeBufferCalls", "writeTextureCalls", "copyExternalCalls", "copyBufferToTextureCalls"]) fake.state[key].length = 0;
  }
  // Include lazy initialization, shader readiness and the first-frame boundary.
  for (let i = 0; i < 8; i++) {
    await step(1000 / 30);
    if (i === 0 || i === 7) await H.flushAsyncWork();
  }
  warming = false;
  assert.ok(mount.__gosxScene3DHandle, `bootstrap must mount Scene3D (${profileName}/${backend}): ${JSON.stringify(env.consoleLogs)}`);
  const canvas = mount.children.find(child => child.tagName === "CANVAS");
  assert.ok(canvas);
  assert.ok(renders > 0, `PBR must render (${profileName}/${backend}): ${JSON.stringify(env.consoleLogs)}`);
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), backend);
  assert.equal(Number(mount.getAttribute("data-gosx-scene3d-css-width")), profile.width);
  assert.equal(Number(mount.getAttribute("data-gosx-scene3d-css-height")), profile.height);
  assert.equal(mount.getAttribute("data-gosx-scene3d-capability-tier"), profile.coarse ? "balanced" : "full");
  assert.deepEqual(env.consoleLogs.error, []);
  assert.ok(!env.consoleLogs.warn.some(line => /failed|fallback/i.test(String(line))), "no renderer failure or fallback");
  const config = backend === "webgpu" ? canvas.getContext("webgpu").configured : null;
  const contextAttributes = config ? { format: config.format, alphaMode: config.alphaMode, usage: config.usage ?? 16 } : canvas.contextCalls.filter(call => call.kind === "webgl2").map(call => call.options).find(Boolean) || {};
  const targets = backend === "webgl" ? glTargets() : gpuTargets();
  // Model the fake GL's default drawing-buffer MSAA as four samples.
  const samples = backend === "webgl" ? (contextAttributes.antialias ? 4 : 1) : targets.samples;
  const mountWork = { backingPixels: canvas.width * canvas.height, samples, backingPixelSamples: canvas.width * canvas.height * samples,
    postTargetPixels: targets.postTargetPixels, shadowMapPixels: targets.shadowMapPixels, contextAttributes };
  return { env, mount, work, mountWork, step, renders: () => renders, dispose: () => mount.__gosxScene3DHandle.dispose() };
}

async function measureAll() {
  const cases = {};
  for (const profile of Object.keys(profiles)) for (const backend of ["webgl", "webgpu"]) {
    for (const [name, props] of Object.entries(scenes)) {
      const h = await mountScene(props, profile, backend);
      try {
        // Settle the default governor after the shader warmup, then measure
        // work at the normal 60 Hz cadence. A forced 30 Hz cadence measures
        // demotion and target reallocation rather than steady frame work.
        for (let warm = 0; warm < 32; warm++) await h.step(1000 / 60, true);
        const steady = counters(), steadyTotal = counters(), beforeRenders = h.renders();
        for (let frame = 0; frame < 120; frame++) {
          const before = { ...h.work };
          await h.step(1000 / 60, true);
          const counts = delta(before, h.work);
          for (const metric of metricNames) {
            steady[metric] = Math.max(steady[metric], counts[metric]);
            steadyTotal[metric] += counts[metric];
          }
        }
        assert.equal(h.renders() - beforeRenders, 120, "every work sample must render a real frame");
        assert.deepEqual(h.env.consoleLogs.error, []);
        cases[`${name}/${profile}/${backend}`] = { mount: h.mountWork, steady, steadyTotal };
      } finally { h.dispose(); }
    }
    for (const mode of ["static", "ambient", "hidden", "offscreen"]) {
      const props = { ...base, scene: { objects: [{ ...box, ...(mode === "static" ? {} : { spinY: 0.003 }) }] } };
      const h = await mountScene(props, profile, backend);
      try {
        if (mode === "hidden") {
          h.env.document.visibilityState = "hidden";
          h.env.document.dispatchEvent({ type: "visibilitychange" });
        }
        if (mode === "offscreen") {
          assert.ok(h.env.intersectionObservers.length);
          for (const observer of h.env.intersectionObservers) observer.trigger([{ target: h.mount, isIntersecting: false, intersectionRatio: 0 }]);
        }
        const start = h.renders();
        for (let i = 0; i < 120; i++) await h.step(1000 / 60);
        const frames = h.renders() - start;
        if (mode === "ambient") assert.ok(frames > 0, "ambient animation must keep rendering");
        cases[`${mode}/${profile}/${backend}`] = { framesInTwoSeconds: frames };
      } finally { h.dispose(); }
    }
  }
  return cases;
}

async function measureDirectDraws({ objects = 10000, frames = 30 } = {}) {
  const clock = require("node:perf_hooks").performance;
  const props = { ...base, scene: { objects: Array.from({ length: objects }, (_, i) => ({
    ...box, id: `direct-${i}`, kind: "pyramid", castShadow: false, receiveShadow: false,
    color: `#${(0x100000 + i).toString(16)}`,
  })) } };
  const h = await mountScene(props, "desktop-full", "webgpu", { directDraw: true });
  try {
    const start = { ...h.work }, renders = h.renders(), timings = [];
    for (let frame = 0; frame < frames; frame++) {
      const before = clock.now();
      await h.step(1000 / 30, true);
      timings.push(clock.now() - before);
    }
    assert.equal(h.renders() - renders, frames);
    const work = delta(start, h.work);
    assert.equal(work.draws, objects * frames, "the benchmark must encode each object directly");
    timings.sort((a, b) => a - b);
    return { objects, frames, medianMS: timings[Math.floor(frames / 2)], p95MS: timings[Math.floor(frames * 0.95)], work };
  } finally { h.dispose(); }
}

module.exports = { measureAll, measureDirectDraws, metricNames, profiles, scenes, uploadBytes };
