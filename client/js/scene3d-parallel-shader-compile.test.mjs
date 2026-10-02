import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const requireRuntime = createRequire(new URL("../runtime/package.json", import.meta.url));
const ts = requireRuntime("typescript");
function runSource(source, context) {
  return vm.runInContext(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
}
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const { FakeWebGLContext, createContext, installManualRAF, runScript, flushAsyncWork,
  bootstrapRuntimeSource, freshFeatureBundleSource, makePointsBundle, makeComputeParticleBundle,
} = require("./runtime-test-harness.js");
const webglSource = readSceneRendererBackendSrc("webgl");

function sourceBetween(source, start, end) {
  const from = source.indexOf(start);
  const to = source.indexOf(end, from);
  assert.ok(from >= 0 && to > from, `missing source range ${start} -> ${end}`);
  return source.slice(from, to);
}

const initialProgramSource = sourceBetween(
  webglSource,
  "const scenePBRInitialPrograms",
  "function scenePBRCompileShader",
);
const baseFactorySource = sourceBetween(
  webglSource,
  "function createScenePBRProgram",
  "function sceneSelenaMaterialLayout",
);
const instancedFactorySource = sourceBetween(
  webglSource,
  "function createScenePBRInstancedProgram",
  "// Initial WebGL2 programs",
);
const contextSource = sourceBetween(
  webglSource,
  "function scenePBRCanvasAntialias",
  "// Try to create a PBR renderer",
);
const rendererFactorySource = sourceBetween(
  webglSource,
  "function createScenePBRRendererOrFallback",
  "if (typeof window !== \"undefined\")",
);

function shaderHarness({ extension = true, linkOK = true, scheduler = false } = {}) {
  let sequence = 0;
  let complete = false;
  let current = true;
  const calls = [];
  const scheduled = new Map();
  let scheduleID = 0;
  const completionStatus = 91;
  const gl = {
    canvas: { dispatchEvent(event) { calls.push(["programReadyEvent", event.type, event.bubbles]); } },
    VERTEX_SHADER: 1,
    FRAGMENT_SHADER: 2,
    LINK_STATUS: 3,
    COMPILE_STATUS: 4,
    createShader(type) {
      const shader = { kind: "shader", id: ++sequence, type };
      calls.push(["createShader", shader.id]);
      return shader;
    },
    shaderSource(shader, source) { calls.push(["shaderSource", shader.id, source]); },
    compileShader(shader) { calls.push(["compileShader", shader.id]); },
    createProgram() {
      const program = { kind: "program", id: ++sequence };
      calls.push(["createProgram", program.id]);
      return program;
    },
    attachShader(program, shader) { calls.push(["attachShader", program.id, shader.id]); },
    linkProgram(program) { calls.push(["linkProgram", program.id]); },
    getProgramParameter(program, parameter) {
      calls.push(["getProgramParameter", program.id, parameter]);
      if (parameter === completionStatus) return complete;
      if (parameter === this.LINK_STATUS) return linkOK;
      throw new Error(`unexpected program parameter ${parameter}`);
    },
    getShaderParameter(shader, parameter) {
      calls.push(["getShaderParameter", shader.id, parameter]);
      return parameter === this.COMPILE_STATUS;
    },
    getProgramInfoLog() { return "link failed"; },
    getShaderInfoLog() { return "compile failed"; },
    deleteProgram(program) { calls.push(["deleteProgram", program.id]); },
    deleteShader(shader) { calls.push(["deleteShader", shader.id]); },
    getExtension(name) {
      calls.push(["getExtension", name]);
      return extension ? { COMPLETION_STATUS_KHR: completionStatus } : null;
    },
    isContextLost() { return false; },
    getAttribLocation(program, name) {
      calls.push(["getAttribLocation", program.id, name]);
      return 1;
    },
    getUniformLocation(program, name) {
      calls.push(["getUniformLocation", program.id, name]);
      return { program, name };
    },
  };
  const context = vm.createContext({
    console,
    WeakMap,
    Promise,
    Date,
    Event,
    SCENE_PBR_VERTEX_SOURCE: "base vertex",
    SCENE_PBR_CROWD_VERTEX_SOURCE: "crowd vertex",
    SCENE_PBR_INSTANCED_VERTEX_SOURCE: "instanced vertex",
    SCENE_PBR_FRAGMENT_SOURCE: "fragment",
    scenePBRFragmentSourceForContext: (_gl, source) => source,
    scenePBRCacheBaseUniforms: (targetGL, program) => ({ model: targetGL.getUniformLocation(program, "u_model") }),
    requestAnimationFrame(callback) {
      const id = ++scheduleID;
      scheduled.set(id, callback);
      return id;
    },
    cancelAnimationFrame(id) { scheduled.delete(id); },
    setTimeout(callback) {
      const id = ++scheduleID;
      scheduled.set(id, callback);
      return id;
    },
    clearTimeout(id) { scheduled.delete(id); },
  });
  runSource(initialProgramSource, context);
  runSource(baseFactorySource, context);
  runSource(instancedFactorySource, context);
  if (scheduler) {
    runSource(sourceBetween(webglSource, "function scenePBRCompileShader", "// --- Light Uniform Upload"), context);
  } else {
  context.scenePBRCompileShader = (targetGL, type) => {
    calls.push(["syncCompile", type]);
    return targetGL.createShader(type);
  };
  context.scenePBRLinkProgram = (targetGL) => {
    calls.push(["syncLink"]);
    return targetGL.createProgram();
  };
  }
  return {
    context,
    gl,
    calls,
    setComplete(value) { complete = value; },
    setCurrent(value) { current = value; },
    isCurrent() { return current; },
    flushScheduled() {
      const first = scheduled.entries().next().value;
      assert.ok(first, "expected a scheduled poll");
      scheduled.delete(first[0]);
      first[1]();
    },
  };
}

function callsNamed(harness, name) {
  return harness.calls.filter(call => call[0] === name);
}

function parallelRendererHarness(extension = true) {
  let complete = false;
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  for (const name of ["scene3d", "scene3d-compute", "scene3d-webgl"]) {
    runScript(freshFeatureBundleSource(name), env.context, name + ".js");
  }
  const canvas = env.document.createElement("canvas");
  canvas.width = 320; canvas.height = 180;
  const gl = new FakeWebGLContext();
  gl.canvas = canvas;
  const getExtension = gl.getExtension.bind(gl);
  const getProgramParameter = gl.getProgramParameter.bind(gl);
  gl.getExtension = name => name === "KHR_parallel_shader_compile"
    ? (extension ? { COMPLETION_STATUS_KHR: 91 } : null) : getExtension(name);
  gl.getProgramParameter = (program, parameter) => parameter === 91 ? complete : getProgramParameter(program, parameter);
  const renderer = env.context.__gosx_scene3d_webgl_api.createScenePBRRendererOrFallback(gl, canvas, {});
  assert.ok(renderer);
  return { env, canvas, gl, renderer, raf, setComplete(value) { complete = value; } };
}

const gpuCreationOps = new Set(["createProgram", "createShader", "createTexture", "createFramebuffer", "createBuffer", "createVertexArray"]);
function gpuCreations(gl) {
  return gl.ops.filter(op => gpuCreationOps.has(op[0])).length;
}

test("ocean locations and draws wait for parallel shader completion", t => {
  const h = parallelRendererHarness();
  t.after(() => h.renderer.dispose());
  h.setComplete(true);
  h.raf.flush(16);
  const bundle = makePointsBundle(null);
  bundle.points = [];
  h.renderer.render(bundle, {width:320,height:180});
  h.setComplete(false);
  const locations = [], draws = [];
  const getUniformLocation = h.gl.getUniformLocation.bind(h.gl), drawArrays = h.gl.drawArrays.bind(h.gl);
  h.gl.getUniformLocation = (program, name) => {
    if (program.attached.some(shader => shader.source.includes("u_ocean[35]"))) locations.push(name);
    return getUniformLocation(program, name);
  };
  h.gl.drawArrays = (mode, first, count) => {
    if (h.gl._activeProgram.attached.some(shader => shader.source.includes("u_ocean[35]"))) draws.push(count);
    drawArrays(mode, first, count);
  };
  bundle.environment.ocean = {level:0,waveHeight:0.8,extent:4000};
  h.renderer.render(bundle, {width:320,height:180});
  assert.deepEqual(locations, [], "pending ocean programs cannot query locations");
  assert.deepEqual(draws, [], "pending ocean programs cannot draw");
  h.setComplete(true);
  h.raf.flush(32);
  h.renderer.render(bundle, {width:320,height:180});
  assert.deepEqual(locations, ["u_viewProj", "u_ocean[0]", "u_bathymetry"]);
  assert.equal(draws.length, 1, "the completed ocean shader draws its grid");
  assert.ok(draws[0] > 3);
});

function cssPostRendererHarness(extension) {
  const h = parallelRendererHarness(extension);
  const bundle = makePointsBundle({ id: "css-point", count: 1, positions: [0, 0, 0] });
  const viewport = { width: 320, height: 180 };
  h.setComplete(true);
  h.raf.flush(16);
  h.renderer.render(bundle, viewport);
  h.raf.flush(32);
  h.renderer.render(bundle, viewport);
  const mount = h.env.document.createElement("div");
  mount.computedStyle = { "--scene-filter": "vignette(intensity 0.5)" };
  mount.__gosxScene3DCSSRevision = 1;
  mount.appendChild(h.canvas);
  h.gl.ops.length = 0;
  return { ...h, bundle, viewport };
}

test("CSS-only post programs prepare before readiness and skip pending draws", t => {
  const h = cssPostRendererHarness(true);
  t.after(() => h.renderer.dispose());
  h.setComplete(false);
  h.renderer.render(h.bundle, h.viewport);
  assert.ok(h.gl.ops.some(op => op[0] === "createProgram"), "CSS submits post programs before checking readiness");
  assert.equal(h.gl.ops.filter(op => /^(useProgram|drawArrays|drawElements|getUniformLocation|getAttribLocation|uniform)/.test(op[0])).length,
    0, "CSS post effects cannot draw or query locations before completion");
  h.setComplete(true);
  h.raf.flush(48);
  h.renderer.render(h.bundle, h.viewport);
  assert.ok(h.gl.ops.some(op => op[0] === "drawArrays" && op[1] === h.gl.TRIANGLE_STRIP));
});

for (const extension of [false, true]) {
  test(`CSS-only post resources are reused across frames (parallel=${extension})`, t => {
    const h = cssPostRendererHarness(extension);
    t.after(() => h.renderer.dispose());
    h.renderer.render(h.bundle, h.viewport);
    h.raf.flush(48);
    h.renderer.render(h.bundle, h.viewport);
    const initial = gpuCreations(h.gl);
    const perFrame = [];
    for (let frame = 0; frame < 3; frame++) {
      const before = gpuCreations(h.gl);
      h.renderer.render(h.bundle, h.viewport);
      h.raf.flush(64 + frame * 16);
      perFrame.push(gpuCreations(h.gl) - before);
    }
    t.diagnostic(`CSS post GPU resource creations: initial=${initial}, subsequent_frames=${perFrame.join(",")}`);
    assert.deepEqual(perFrame, [0, 0, 0], "completed CSS effects retain their programs, buffers, textures and framebuffers");
  });
}

test("a rejected asynchronous base shader recovers through the mounted legacy fallback", async t => {
  let complete = false;
  const contexts = [];
  const env = createContext({ disableCanvas2D: true, createWebGL2Context() {
    const gl = new FakeWebGLContext();
    contexts.push(gl);
    const getExtension = gl.getExtension.bind(gl);
    const getProgramParameter = gl.getProgramParameter.bind(gl);
    gl.getExtension = name => name === "KHR_parallel_shader_compile" ? { COMPLETION_STATUS_KHR: 91 } : getExtension(name);
    gl.getProgramParameter = (program, parameter) => parameter === 91 ? complete
      : (parameter === gl.LINK_STATUS && gl.programShaderSources(program).includes("u_normalUVScale")
        && !gl.programShaderSources(program).includes("a_instanceMatrix") ? false : getProgramParameter(program, parameter));
    return gl;
  } });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  env.context.Event = Event;
  const createElement = env.document.createElement.bind(env.document);
  env.document.createElement = tag => {
    const element = createElement(tag);
    if (tag === "canvas") {
      const dispatch = element.dispatchEvent.bind(element);
      element.dispatchEvent = event => {
        const result = dispatch(event);
        if (event.bubbles && element.parentNode) element.parentNode.dispatchEvent(event);
        return result;
      };
      const getContext = element.getContext.bind(element);
      element.getContext = (...args) => {
        const gl = getContext(...args);
        if (gl instanceof FakeWebGLContext) gl.canvas = element;
        return gl;
      };
    }
    return element;
  };
  const raf = installManualRAF(env.context);
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  let factory;
  const register = env.context.__gosx_register_engine_factory;
  env.context.__gosx_register_engine_factory = (name, value) => {
    if (name === "GoSXScene3D") factory = value;
    register(name, value);
  };
  for (const name of ["scene3d", "scene3d-webgl"]) runScript(freshFeatureBundleSource(name), env.context, name + ".js");
  // Exercise the cold factory path also used for registry creation and restoration.
  env.context.__gosx_scene3d_webgl_api.prepareScenePBRInitialRenderer = null;
  const mount = env.document.createElement("div");
  env.document.body.appendChild(mount);
  const pending = factory({ mount, props: { width: 320, height: 180, forceWebGL: true,
    objects: [{ id: "fallback-box", kind: "box", width: 1, height: 1, depth: 1, color: "#ffffff" }] } });
  for (let frame = 0; frame < 8; frame++) { await flushAsyncWork(); raf.flush(16 + frame * 16); }
  const controller = await pending;
  t.after(() => controller.dispose());
  const initial = contexts.find(gl => gl.programMatching("u_normalUVScale"));
  assert.ok(initial, "the cold renderer submitted its base PBR program");
  assert.equal(initial.ops.filter(op => op[0] === "drawArrays").length, 0, "the pending base program does not draw");
  complete = true;
  for (let frame = 0; frame < 8; frame++) { raf.flush(160 + frame * 16); await flushAsyncWork(); }
  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer-fallback"), "webgl-shader-failed", "required-program failure reaches the mount fallback ladder");
  const fallback = contexts.find(gl => gl !== initial && gl.programs.length > 0);
  assert.ok(fallback, "legacy fallback owns a fresh canvas/context");
  assert.ok(fallback.ops.some(op => op[0] === "drawArrays" || op[0] === "drawElements"), "fallback renders after its own programs complete");
  assert.ok(initial.ops.some(op => op[0] === "deleteBuffer"), "failed PBR resources are released");
});

test("authored compute-particle shaders prepare before readiness and never draw while pending", () => {
  const h = parallelRendererHarness();
  h.setComplete(true);
  h.raf.flush(16);
  // Warm the builtin program so it cannot hide a late authored compilation.
  const viewport = { width: 320, height: 180 };
  const warmBundle = makePointsBundle({ id: "warm", count: 1, positions: [0, 0, 0] });
  h.renderer.render(warmBundle, viewport);
  h.raf.flush(32);
  h.renderer.render(warmBundle, viewport);
  h.gl.ops.length = 0;
  h.setComplete(false);
  const bundle = makeComputeParticleBundle({
    id: "authored-compute", count: 4,
    emitter: { kind: "point", lifetime: 10 }, material: { color: "#ffffff", size: 2 },
    renderVertex: "attribute vec3 a_position; void main() { gl_Position = vec4(a_position, 1.0); gl_PointSize = 2.0; }",
    renderFragment: "precision mediump float; uniform float brightness; void main() { gl_FragColor = vec4(brightness); }",
    renderUniforms: { brightness: 1.25 },
  });
  h.renderer.render(bundle, viewport);
  assert.ok(h.gl.programMatching("uniform float brightness"), "the authored shader is submitted before the frame check");
  assert.equal(h.gl.ops.filter(op => op[0] === "useProgram" || op[0] === "drawArrays").length, 0);
  assert.equal(h.gl.ops.filter(op => op[0] === "getUniformLocation" || op[0] === "getAttribLocation").length, 0);
  h.raf.flush(48);
  h.renderer.render(bundle, viewport);
  assert.equal(h.gl.ops.filter(op => op[0] === "drawArrays").length, 0, "unfinished programs keep the frame parked");
  h.setComplete(true);
  h.raf.flush(64);
  h.renderer.render(bundle, viewport);
  assert.ok(h.gl.ops.some(op => op[0] === "drawArrays" && op[1] === h.gl.POINTS && op[3] === 4));
  assert.ok(h.gl.ops.some(op => op[0] === "uniform1f" && op[1] === "brightness" && op[2] === 1.25));
  h.renderer.dispose();
});

test("imported-mesh batching becomes available after parallel compilation completes", () => {
  for (const extension of [false, true]) {
    const h = parallelRendererHarness(extension);
    if (extension) {
      assert.equal(h.renderer.supportsRigidImportedBatches, false);
      h.setComplete(true);
      h.raf.flush(16);
    }
    assert.equal(h.renderer.supportsRigidImportedBatches, true, "completed programs preserve imported-mesh batching");
    h.renderer.dispose();
  }
});

test("parallel startup waits for completion before link status or location queries", async () => {
  const h = shaderHarness();
  const pending = h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true, isCurrent: h.isCurrent });
  assert.equal(callsNamed(h, "createProgram").length, 2);
  assert.equal(callsNamed(h, "getProgramParameter").every(call => call[2] === 91), true);
  assert.equal(callsNamed(h, "getAttribLocation").length, 0);
  assert.equal(callsNamed(h, "getUniformLocation").length, 0);

  h.setComplete(true);
  h.flushScheduled();
  const owner = await pending;
  assert.ok(owner);
  assert.equal(callsNamed(h, "getProgramParameter").filter(call => call[2] === h.gl.LINK_STATUS).length, 2);
  assert.equal(callsNamed(h, "getAttribLocation").length, 0);
  assert.equal(callsNamed(h, "getUniformLocation").length, 0);

  const base = h.context.createScenePBRProgram(h.gl);
  const crowd = h.context.createScenePBRInstancedProgram(h.gl, true);
  assert.ok(base && crowd);
  assert.equal(base.initialProgramOwner, owner);
  assert.ok(callsNamed(h, "getAttribLocation").length > 0);
  assert.ok(callsNamed(h, "getUniformLocation").length > 0);
});

test("completed records are context-local and consumed once", async () => {
  const h = shaderHarness();
  const pending = h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true, isCurrent: h.isCurrent });
  h.setComplete(true);
  h.flushScheduled();
  await pending;

  const other = shaderHarness().gl;
  assert.equal(h.context.scenePBRTakeInitialProgram(other, "base"), null);
  assert.ok(h.context.createScenePBRProgram(h.gl));
  assert.equal(callsNamed(h, "syncCompile").length, 0);
  assert.ok(h.context.createScenePBRProgram(h.gl));
  assert.equal(callsNamed(h, "syncCompile").length, 2);
  assert.equal(callsNamed(h, "syncLink").length, 1);
});

test("unsupported extension performs no speculative shader allocation and uses sync factory", async () => {
  const h = shaderHarness({ extension: false });
  assert.equal(await h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true }), false);
  assert.equal(callsNamed(h, "createShader").length, 0);
  assert.ok(h.context.createScenePBRProgram(h.gl));
  assert.equal(callsNamed(h, "syncCompile").length, 2);
});

test("link failure and cancellation delete every owned object exactly once", async () => {
  const failed = shaderHarness({ linkOK: false });
  const failedPending = failed.context.prepareScenePBRInitialPrograms(failed.gl, { crowd: true });
  failed.setComplete(true);
  failed.flushScheduled();
  assert.equal(await failedPending, null);
  assert.equal(callsNamed(failed, "deleteProgram").length, 2);
  assert.equal(callsNamed(failed, "deleteShader").length, 4);
  failed.context.discardScenePBRInitialPrograms(failed.gl);
  assert.equal(callsNamed(failed, "deleteProgram").length, 2);

  const cancelled = shaderHarness();
  const cancelledPending = cancelled.context.prepareScenePBRInitialPrograms(cancelled.gl, {
    crowd: true,
    isCurrent: cancelled.isCurrent,
  });
  cancelled.setCurrent(false);
  cancelled.flushScheduled();
  assert.equal(await cancelledPending, null);
  assert.equal(callsNamed(cancelled, "deleteProgram").length, 2);
  assert.equal(callsNamed(cancelled, "deleteShader").length, 4);
  cancelled.context.discardScenePBRInitialPrograms(cancelled.gl);
  assert.equal(callsNamed(cancelled, "deleteProgram").length, 2);
});

test("constructor failure after base consume releases all warm records", async () => {
  const h = shaderHarness();
  const pending = h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true });
  h.setComplete(true);
  h.flushScheduled();
  await pending;
  h.context.WebGL2RenderingContext = class {
    static [Symbol.hasInstance](value) { return value === h.gl; }
  };
  h.context.createScenePBRRenderer = targetGL => {
    assert.ok(h.context.scenePBRTakeInitialProgram(targetGL, "base"));
    throw new Error("constructor failed after consume");
  };
  runSource(rendererFactorySource, h.context);
  assert.equal(h.context.createScenePBRRendererOrFallback(h.gl, {}, {}), null);
  assert.equal(callsNamed(h, "deleteProgram").length, 2);
  assert.equal(callsNamed(h, "deleteShader").length, 4);
  h.context.scenePBRDisposeInitialOwner(h.gl, h.context.scenePBRInitialProgramOwner(h.gl));
  assert.equal(callsNamed(h, "deleteProgram").length, 2);
});

test("an old owner cannot discard a newer preparation on the same context", async () => {
  const h = shaderHarness();
  const firstPending = h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true });
  h.setComplete(true);
  h.flushScheduled();
  const firstOwner = await firstPending;
  assert.ok(firstOwner);

  h.setComplete(false);
  const secondPending = h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true });
  const deletesAfterSupersede = callsNamed(h, "deleteProgram").length;
  h.context.discardScenePBRInitialPrograms(h.gl, firstOwner);
  assert.equal(callsNamed(h, "deleteProgram").length, deletesAfterSupersede);
  h.setComplete(true);
  h.flushScheduled();
  assert.ok(await secondPending);
  assert.ok(h.context.scenePBRTakeInitialProgram(h.gl, "base"));
});

test("a null sync-renderer owner cannot discard a pending preparation", async () => {
  const h = shaderHarness();
  const pending = h.context.prepareScenePBRInitialPrograms(h.gl, { crowd: true });
  h.context.discardScenePBRInitialPrograms(h.gl, null);
  assert.equal(callsNamed(h, "deleteProgram").length, 0);
  h.setComplete(true);
  h.flushScheduled();
  assert.ok(await pending);
  assert.ok(h.context.scenePBRTakeInitialProgram(h.gl, "base"));
});

test("consume rejects a stale owner before any location lookup", async () => {
  const h = shaderHarness();
  const pending = h.context.prepareScenePBRInitialPrograms(h.gl, { isCurrent: h.isCurrent });
  h.setComplete(true);
  h.flushScheduled();
  await pending;
  h.setCurrent(false);
  assert.equal(h.context.createScenePBRProgram(h.gl), null);
  assert.equal(callsNamed(h, "getAttribLocation").length, 0);
  assert.equal(callsNamed(h, "getUniformLocation").length, 0);
  assert.equal(callsNamed(h, "syncCompile").length, 0);
});

test("initial context options are reused and crowd warming is narrowly requested", async () => {
  const context = vm.createContext({
    sceneNumber: (value, fallback) => Number.isFinite(Number(value)) ? Number(value) : fallback,
    sceneBool: (value, fallback) => value == null ? fallback : Boolean(value),
    sceneCanvasAlpha: props => Boolean(props && props.alpha),
    prepareScenePBRInitialPrograms: async gl => ({ gl }),
  });
  runSource(contextSource, context);

  const gl = {};
  const requests = [];
  const canvas = { getContext(kind, options) { requests.push({ kind, options }); return gl; } };
  const prepared = await context.prepareScenePBRInitialRenderer(
    canvas,
    { alpha: true, msaaSamples: 1 },
    { tier: "full" },
    {},
  );
  assert.equal(prepared.gl, gl);
  assert.deepEqual(JSON.parse(JSON.stringify(requests)), [{
    kind: "webgl2",
    options: {
      alpha: true,
      premultipliedAlpha: true,
      antialias: false,
      depth: true,
      powerPreference: "high-performance",
    },
  }]);
  assert.equal(context.scenePBRInitialRequestsCrowd({ scene: { models: [{ src: "/static.glb" }] } }, null), false);
  assert.equal(context.scenePBRInitialRequestsCrowd({
    scene: { instancedGLBMeshes: [{ src: "/rig.glb", instances: [{ animation: "Walk" }] }] },
  }, null), true);
  assert.equal(context.scenePBRInitialRequestsCrowd({
    scene: { instancedGLBMeshes: [{ src: "/rig.glb", instances: [{}] }] },
  }, null), false);
});

test("mount fences private hydration state at the new async abandonment boundary", () => {
  const mountSource = fs.readFileSync(new URL("../runtime/scene3d/mount.ts", import.meta.url), "utf8");
  assert.match(mountSource, /await prepareSceneInitialWebGLRenderer\([\s\S]*?if \(!scene3DFactoryOwned\(\)\) \{[\s\S]*?discardSceneInitialWebGLRenderer\(initialShaderPreparation\);[\s\S]*?invalidateSceneModelHydration\(sceneState\);[\s\S]*?settleSceneModelTextureVariantScope/);
  assert.match(mountSource, /catch \(error\) \{\s*discardSceneInitialWebGLRenderer\(initialShaderPreparation\);/);
});

function submitProgram(h, label) {
  const vertex = h.context.scenePBRCompileShader(h.gl, h.gl.VERTEX_SHADER, label + " vertex");
  const fragment = h.context.scenePBRCompileShader(h.gl, h.gl.FRAGMENT_SHADER, label + " fragment");
  return h.context.scenePBRLinkProgram(h.gl, vertex, fragment, label);
}

function pointsHarness(h) {
  for (const name of ["enable", "disable", "useProgram", "uniformMatrix4fv", "uniform1f", "uniform1i",
    "uniform3f", "uniform4f", "depthMask", "depthFunc", "bindBuffer", "enableVertexAttribArray",
    "vertexAttribPointer", "disableVertexAttribArray", "vertexAttrib1f", "vertexAttrib4f", "drawArrays"]) {
    h.gl[name] = (...args) => h.calls.push([name, ...args]);
  }
  h.gl.POINTS = 100;
  Object.assign(h.context, {
    gl: h.gl, program: {}, pointsAuthoredGLPrograms: new Map(),
    sceneWebGLNormalizeCustomShaderSource: source => source,
    sceneNumber: (value, fallback) => typeof value === "number" ? value : fallback,
    sceneColorRGBA: (_value, fallback) => fallback,
    clamp01: value => Math.max(0, Math.min(1, value)),
    scenePointStyleCode: () => 0,
    sceneEulerMatrixInto() {}, sceneWebGLUploadPointModelMatrix() {},
    ensureStaticPointVBO: () => ({}),
    webglRenderTruthStats: { pointsSubmitted: 0, pointInstancesSubmitted: 0, pointsDrawn: 0, pointInstancesDrawn: 0 },
    webglComputeParticleDrawStats: { drawEntries: 0, drawInstances: 0, drawCalls: 0,
      authoredDrawEntries: 0, authoredDrawInstances: 0, authoredDrawCalls: 0 },
    ensurePointsProgram: () => { throw new Error("authored particles must prepare their own program"); },
  });
  runSource(sourceBetween(webglSource, "function scenePointsProgramInfo", "// Compile the instanced PBR vertex shader"), h.context);
  runSource(sourceBetween(webglSource, "function failPointsAuthoredGLProgram", "function disposeComputeParticleSystemRecord"), h.context);
  runSource(sourceBetween(webglSource, "function drawPointsEntries(gl, pointsArray", "// Ensure the instanced PBR program"), h.context);
  return {
    ensurePointsAuthoredGLProgram: h.context.ensurePointsAuthoredGLProgram,
    ensurePointsProgram: h.context.ensurePointsProgram,
    skyResources: {},
  };
}

test("compute-particle preparation submits authored programs before frame readiness", () => {
  const h = shaderHarness({ scheduler: true });
  const hooks = pointsHarness(h);
  const entries = ["sparks", ""].map(id => ({ id, renderVertex: "particle vertex",
    renderFragment: "particle fragment", renderUniforms: { brightness: 1.25 } }));
  h.context.scenePBRPrepareBundlePrograms(h.gl, { computeParticles: entries }, hooks);
  assert.equal(callsNamed(h, "linkProgram").length, 2);
  assert.deepEqual(callsNamed(h, "shaderSource").map(call => call[2]),
    ["particle vertex", "particle fragment", "particle vertex", "particle fragment"]);
  assert.ok(h.context.pointsAuthoredGLPrograms.has("sparks"));
  assert.ok(h.context.pointsAuthoredGLPrograms.has("scene-compute-points-1"));
  assert.equal(h.context.scenePBRFrameProgramsReady(h.gl, {}), false);
  h.flushScheduled();
  assert.equal(callsNamed(h, "getAttribLocation").length, 0);
  assert.equal(callsNamed(h, "getUniformLocation").length, 0);
  assert.equal(h.context.scenePBRFrameProgramsReady(h.gl, {}), false);
  h.setComplete(true);
  h.flushScheduled();
  assert.equal(h.context.scenePBRFrameProgramsReady(h.gl, {}), true);
  assert.ok(callsNamed(h, "getAttribLocation").length > 0);
});

test("points draw skips a late authored program until completion without querying uniforms", () => {
  const h = shaderHarness({ scheduler: true });
  pointsHarness(h);
  const entry = { id: "sparks", count: 1, customVertex: "particle vertex", customFragment: "particle fragment",
    customUniforms: { brightness: 1.25 }, _cachedPos: new Float32Array([0, 0, 0]), _computeParticlesSynthetic: true };
  const draw = () => h.context.drawPointsEntries(h.gl, [entry], {}, new Float32Array(16), new Float32Array(16), 2, 180);
  draw();
  assert.equal(callsNamed(h, "useProgram").some(call => call[1]?.kind === "program"), false);
  assert.equal(callsNamed(h, "uniformMatrix4fv").length, 0);
  assert.equal(callsNamed(h, "getUniformLocation").length, 0);
  assert.equal(callsNamed(h, "drawArrays").length, 0);
  h.flushScheduled();
  draw();
  assert.equal(callsNamed(h, "drawArrays").length, 0);
  h.setComplete(true);
  h.flushScheduled();
  draw();
  assert.equal(callsNamed(h, "linkProgram").length, 1, "later draws reuse the completed program");
  assert.equal(callsNamed(h, "drawArrays").length, 1);
  assert.ok(callsNamed(h, "uniform1f").some(call => call[1]?.name === "brightness" && call[2] === 1.25));
  assert.equal(h.context.webglComputeParticleDrawStats.authoredDrawCalls, 1);
});

function importedBatchCapabilityHarness(h) {
  Object.assign(h.context, {
    gl: h.gl, instancedProgram: null, instancedProgramFailed: false,
    createSceneWebGLFrameTimer: () => ({}),
    prepareCrowdAtlas() {}, prepareCrowdMotionShaders() {}, diagnostics() {}, textureVariantContext: {},
  });
  runSource(sourceBetween(webglSource, "function ensureInstancedProgram()", "// Look up or generate geometry for an instanced mesh entry."), h.context);
  runSource("function createCapabilityRenderer() {" + sourceBetween(webglSource,
    "function renderSurfaces(bundle, target)", "function sceneWebGLCommandSequence"), h.context);
  return h.context.createCapabilityRenderer();
}

test("imported-mesh batching becomes available after instanced program completion", () => {
  const h = shaderHarness({ scheduler: true });
  const renderer = importedBatchCapabilityHarness(h);
  assert.equal(renderer.supportsRigidImportedBatches, false);
  assert.equal(callsNamed(h, "getAttribLocation").length, 0);
  h.flushScheduled();
  assert.equal(renderer.supportsRigidImportedBatches, false);
  h.setComplete(true);
  h.flushScheduled();
  assert.equal(renderer.supportsRigidImportedBatches, true);
  assert.equal(callsNamed(h, "linkProgram").length, 1);
});

test("imported-mesh batching preserves synchronous readiness and rejects failed parallel links", () => {
  const sync = shaderHarness({ scheduler: true, extension: false });
  assert.equal(importedBatchCapabilityHarness(sync).supportsRigidImportedBatches, true);
  const failed = shaderHarness({ scheduler: true, linkOK: false });
  const renderer = importedBatchCapabilityHarness(failed);
  assert.equal(renderer.supportsRigidImportedBatches, false);
  failed.setComplete(true);
  failed.flushScheduled();
  assert.equal(renderer.supportsRigidImportedBatches, false);
});

function waterPassHarness(h, program) {
  for (const name of ["bindFramebuffer", "viewport", "disable", "useProgram", "activeTexture", "bindTexture", "bindVertexArray", "drawArrays"]) {
    h.gl[name] = (...args) => h.calls.push([name, ...args]);
  }
  Object.assign(h.context, {
    gl: h.gl, programs: { simulation: { program, descriptor: {} } },
    states: [{ tex: {}, fbo: {} }, { tex: {}, fbo: {} }], current: 0,
    resolution: 64, emptyVAO: {}, sceneWaterApplyPassUniforms() {},
  });
  runSource(sourceBetween(webglSource, "function runPass(name, values)", "var normalDirty"), h.context);
}

test("all programs submit before polling and defer locations until completion", () => {
  const h = shaderHarness({ scheduler: true });
  const programs = ["shadow", "points", "Selena"].map(label => submitProgram(h, label));
  const info = h.context.scenePBRDeferredProgramInfo(h.gl, programs[1], () => ({
    program: programs[1], attributes: { position: h.gl.getAttribLocation(programs[1], "a_position") },
    uniforms: { model: h.gl.getUniformLocation(programs[1], "u_model") },
  }));
  const attributes = info.attributes, uniforms = info.uniforms;
  assert.equal(callsNamed(h, "linkProgram").length, 3);
  assert.equal(callsNamed(h, "getProgramParameter").length, 0);
  assert.equal(callsNamed(h, "getShaderParameter").length, 0);
  assert.equal(callsNamed(h, "getAttribLocation").length, 0);
  h.flushScheduled();
  assert.equal(callsNamed(h, "getProgramParameter").length, 3);
  assert.ok(callsNamed(h, "getProgramParameter").every(call => call[2] === 91));
  assert.ok(programs.every(program => !h.context.scenePBRProgramReady(h.gl, program)));
  assert.equal(callsNamed(h, "programReadyEvent").length, 0);
  h.setComplete(true);
  h.flushScheduled();
  assert.ok(programs.every(program => h.context.scenePBRProgramReady(h.gl, program)));
  assert.equal(info.attributes, attributes);
  assert.equal(info.uniforms, uniforms);
  assert.equal(info.attributes.position, 1);
  assert.ok(info.uniforms.model);
  assert.equal(h.context.scenePBRProgramsPending(h.gl), false);
  assert.deepEqual(callsNamed(h, "programReadyEvent"), [["programReadyEvent", "gosx:scene3d:program-ready", true]]);
});

test("water does not bind or draw an unfinished program and draws after ready polling", () => {
  const h = shaderHarness({ scheduler: true });
  const program = submitProgram(h, "water");
  waterPassHarness(h, program);
  assert.equal(h.context.runPass("simulation", {}), false);
  assert.equal(callsNamed(h, "useProgram").length, 0);
  assert.equal(callsNamed(h, "drawArrays").length, 0);
  h.flushScheduled();
  assert.equal(h.context.runPass("simulation", {}), false);
  h.setComplete(true);
  h.flushScheduled();
  assert.equal(h.context.runPass("simulation", {}), true);
  assert.equal(callsNamed(h, "drawArrays").length, 1);
});

test("scheduler retains synchronous compilation without the extension", () => {
  const h = shaderHarness({ scheduler: true, extension: false });
  const program = submitProgram(h, "fallback");
  assert.equal(callsNamed(h, "getShaderParameter").length, 2);
  assert.equal(callsNamed(h, "getProgramParameter").filter(call => call[2] === h.gl.LINK_STATUS).length, 1);
  assert.equal(h.context.scenePBRProgramReady(h.gl, program), true);
  assert.equal(h.context.scenePBRProgramsPending(h.gl), false);
  waterPassHarness(h, program);
  assert.equal(h.context.runPass("simulation", {}), true);
});

test("failed links and disposed queues never draw", () => {
  for (const dispose of [false, true]) {
    const h = shaderHarness({ scheduler: true, linkOK: false });
    const program = submitProgram(h, "failure");
    waterPassHarness(h, program);
    if (dispose) h.context.scenePBRDisposeProgramQueue(h.gl);
    else { h.setComplete(true); h.flushScheduled(); }
    assert.equal(h.context.runPass("simulation", {}), false);
    assert.equal(callsNamed(h, "drawArrays").length, 0);
    assert.equal(h.context.scenePBRProgramsPending(h.gl), false);
  }
});

test("custom post link failures report once after completion", () => {
  const h = shaderHarness({ scheduler: true, linkOK: false });
  const warnings = [];
  const records = [];
  h.context.console = { warn: (...args) => warnings.push(args) };
  const program = submitProgram(h, "custom post");
  const failures = {};
  const truth = { record: (...args) => records.push(args) };
  const report = name => h.context.scenePBRReportCustomPostFailure(name, failures, truth);
  const pass = { program };
  assert.equal(h.context.scenePBRCustomPostPassReady(h.gl, pass, "lens", report), false);
  assert.equal(records.length, 0);
  h.setComplete(true);
  h.flushScheduled();
  assert.equal(h.context.scenePBRCustomPostPassReady(h.gl, pass, "lens", report), false);
  assert.equal(h.context.scenePBRCustomPostPassReady(h.gl, pass, "lens", report), false);
  assert.deepEqual(records, [["post-compile-failed", "webgl customPost lens"]]);
  assert.equal(warnings.filter(args => args[0].includes("custom post pass")).length, 1);
  assert.equal(failures.lens, true);
});
