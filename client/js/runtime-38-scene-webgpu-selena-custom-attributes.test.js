"use strict";
// Direct WebGPU coverage for Selena custom per-vertex float BufferAttributes,
// mirroring runtime-32's WebGL coverage of the same feature. This
// regression-tests a real bug: the monolith bundle concatenates the two
// renderer backends into one shared scope, so their two copies of
// sceneSelenaAttributeComponents (documented as intentional per-chunk
// duplicates) must stay in sync, or the later-concatenated declaration
// silently shadows the other and widens a scalar "float" stream to a vec3
// fetch.

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

// readSceneRendererBackendSrc is the canonical, shared way other renderer
// unit tests read backend source text (see scene3d-renderer-architecture.test.js).
const webgpuSrc = readSceneRendererBackendSrc("webgpu");

function extractFn(source, name) {
  const start = source.indexOf("function " + name + "(");
  assert.ok(start >= 0, "missing " + name);
  let depth = 0;
  for (let i = source.indexOf("{", start); i < source.length; i++) {
    if (source[i] === "{") depth++;
    else if (source[i] === "}") {
      depth--;
      if (!depth) return source.slice(start, i + 1);
    }
  }
  throw new Error("unbalanced braces in " + name);
}

function makeSandbox() {
  const sandbox = {
    sceneNumber(value, fallback) {
      const n = typeof value === "number" ? value : Number(value);
      return Number.isFinite(n) ? n : fallback;
    },
  };
  const code = [
    "sceneSelenaAttributeComponents",
    "sceneSelenaAttributeSource",
    "sceneSelenaPipelineAttributes",
    "webGPUDirectAttribute",
  ].map((name) => extractFn(webgpuSrc, name)).join("\n");
  vm.createContext(sandbox);
  vm.runInContext(code, sandbox);
  // webGPUDirectAttribute checks `instanceof Float32Array` against this
  // context's own realm, so expose its Float32Array for fixtures to use.
  sandbox.Float32Array = vm.runInContext("Float32Array", sandbox);
  return sandbox;
}

test("webgpu sceneSelenaAttributeComponents: float is one component, never widened to vec3", () => {
  const sandbox = makeSandbox();
  assert.equal(sandbox.sceneSelenaAttributeComponents("float"), 1);
  assert.equal(sandbox.sceneSelenaAttributeComponents("vec2"), 2);
  assert.equal(sandbox.sceneSelenaAttributeComponents("vec3"), 3);
  assert.equal(sandbox.sceneSelenaAttributeComponents("vec4"), 4);
  assert.equal(sandbox.sceneSelenaAttributeComponents("mat4"), 0,
    "unknown types fail closed to 0 components, not a silent vec3 widen");
  assert.equal(sandbox.sceneSelenaAttributeComponents(""), 0);
});

test("webgpu sceneSelenaPipelineAttributes: custom float and vec3 descriptors get exact widths and formats", () => {
  const sandbox = makeSandbox();
  const layout = {
    attributes: [
      { location: 0, name: "position", type: "vec3" },
      { location: 1, name: "normal", type: "vec3" },
      { location: 3, name: "padGlow", type: "float" },
      { location: 4, name: "flowVec", type: "vec3" },
    ],
  };
  const out = sandbox.sceneSelenaPipelineAttributes(layout);
  const byName = Object.fromEntries(out.map((a) => [a.name, a]));

  assert.equal(byName.position.source, "positions");
  assert.equal(byName.normal.source, "normals");

  assert.equal(byName.padGlow.source, "custom");
  assert.equal(byName.padGlow.components, 1,
    "a scalar float custom attribute must resolve to exactly one component");
  assert.equal(byName.padGlow.format, "float32");
  assert.equal(byName.padGlow.shaderLocation, 3);

  assert.equal(byName.flowVec.source, "custom");
  assert.equal(byName.flowVec.components, 3);
  assert.equal(byName.flowVec.format, "float32x3");
  assert.equal(byName.flowVec.shaderLocation, 4);
});

test("webgpu sceneSelenaPipelineAttributes: fails closed on reserved names, bad identifiers, missing locations, and duplicates", () => {
  const sandbox = makeSandbox();

  // A custom attribute reusing a reserved builtin alias is dropped even
  // though sceneSelenaAttributeSource only recognizes bare "position" et al.
  let out = sandbox.sceneSelenaPipelineAttributes({
    attributes: [{ location: 2, name: "tangents", type: "vec3" }],
  });
  assert.equal(out.length, 0, "reserved alias must never become a custom attribute");

  // Not a valid WGSL identifier.
  out = sandbox.sceneSelenaPipelineAttributes({
    attributes: [{ location: 2, name: "3bad-name", type: "vec3" }],
  });
  assert.equal(out.length, 0, "invalid WGSL identifiers are dropped");

  // Custom attribute with no explicit location is dropped (unlike builtins,
  // which may fall back to the running slot index).
  out = sandbox.sceneSelenaPipelineAttributes({
    attributes: [{ name: "padGlow", type: "float" }],
  });
  assert.equal(out.length, 0, "a custom attribute without an explicit location fails closed");

  // Two descriptors claiming the same location: only the first survives.
  out = sandbox.sceneSelenaPipelineAttributes({
    attributes: [
      { location: 3, name: "padGlow", type: "float" },
      { location: 3, name: "flowVec", type: "vec3" },
    ],
  });
  assert.equal(out.length, 1, "a duplicate shader location must be dropped, not silently rebound");
  assert.equal(out[0].name, "padGlow");

  // Unsupported declared type.
  out = sandbox.sceneSelenaPipelineAttributes({
    attributes: [{ location: 3, name: "padGlow", type: "mat4" }],
  });
  assert.equal(out.length, 0, "an unsupported declared type is dropped, not defaulted to some width");
});

test("webgpu webGPUDirectAttribute: resolves a declared custom stream with the exact item size", () => {
  const sandbox = makeSandbox();
  // webGPUDirectAttribute checks `entry.data instanceof Float32Array` inside
  // the sandbox's own realm, so the fixture array must come from the
  // sandbox's Float32Array constructor, not the host realm's.
  const padGlow = new sandbox.Float32Array([0.1, 0.2, 0.3, 0.4]);
  const obj = {
    vertices: {
      attributes: {
        padGlow: { data: padGlow, itemSize: 1 },
      },
    },
  };
  const resolved = sandbox.webGPUDirectAttribute(obj, "custom:padGlow", 4, 1);
  assert.equal(resolved, padGlow, "an exact-length declared stream returns its own Float32Array");
});

test("webgpu webGPUDirectAttribute: fails closed on a component-count mismatch instead of misreading the stream", () => {
  const sandbox = makeSandbox();
  const flowVec = new sandbox.Float32Array([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]);
  const obj = {
    vertices: {
      attributes: {
        // Declared as itemSize 3 (vec3), but the caller (a shader layout
        // widened by the monolith-shadowing bug) requests tupleSize 1.
        flowVec: { data: flowVec, itemSize: 3 },
      },
    },
  };
  assert.equal(sandbox.webGPUDirectAttribute(obj, "custom:flowVec", 4, 1), null,
    "a declared itemSize that disagrees with the requested tupleSize must fail closed");
});

test("webgpu webGPUDirectAttribute: fails closed when the declared custom stream is absent", () => {
  const sandbox = makeSandbox();
  const obj = { vertices: { attributes: {} } };
  assert.equal(sandbox.webGPUDirectAttribute(obj, "custom:padGlow", 4, 1), null,
    "a missing declared stream must skip the draw instead of binding stale or undefined data");
});
