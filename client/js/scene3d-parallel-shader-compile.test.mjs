import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
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

function shaderHarness({ extension = true, linkOK = true } = {}) {
  let sequence = 0;
  let complete = false;
  let current = true;
  const calls = [];
  const scheduled = new Map();
  let scheduleID = 0;
  const completionStatus = 91;
  const gl = {
    VERTEX_SHADER: 1,
    FRAGMENT_SHADER: 2,
    LINK_STATUS: 3,
    COMPILE_STATUS: 4,
    createShader(type) {
      const shader = { kind: "shader", id: ++sequence, type };
      calls.push(["createShader", shader.id]);
      return shader;
    },
    shaderSource(shader) { calls.push(["shaderSource", shader.id]); },
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
  vm.runInContext(initialProgramSource, context);
  vm.runInContext(baseFactorySource, context);
  vm.runInContext(instancedFactorySource, context);
  context.scenePBRCompileShader = (targetGL, type) => {
    calls.push(["syncCompile", type]);
    return targetGL.createShader(type);
  };
  context.scenePBRLinkProgram = (targetGL) => {
    calls.push(["syncLink"]);
    return targetGL.createProgram();
  };
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
  vm.runInContext(rendererFactorySource, h.context);
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
  vm.runInContext(contextSource, context);

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
